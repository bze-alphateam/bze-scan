package candles

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/jackc/pgx/v5"
)

// PGStore implements Store over a transaction: the trades come from
// explorer.order_fills (the numbered ones) and explorer.swaps.
type PGStore struct {
	tx Tx
}

// NewPGStore returns a store over tx.
func NewPGStore(tx Tx) *PGStore {
	return &PGStore{tx: tx}
}

// Lock implements Store with a transaction-level advisory lock per market.
func (s *PGStore) Lock(ctx context.Context, markets []string) error {
	for _, m := range markets {
		if _, err := s.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('explorer.candles:' || $1))`, m); err != nil {
			return fmt.Errorf("candles of %s: lock: %w", m, err)
		}
	}
	return nil
}

// Trades implements Store. A swap without a base amount, which the
// aggregator never stores, is left out.
func (s *PGStore) Trades(ctx context.Context, market string, from, to time.Time) ([]Trade, error) {
	rows, err := s.tx.Query(ctx, `SELECT trade_id, time, price::text, amount::text, NULL::text
		  FROM explorer.order_fills
		 WHERE market_id = $1 AND time >= $2 AND time < $3 AND trade_id IS NOT NULL
		UNION ALL
		SELECT trade_id, time, NULL, base_amount::text, quote_amount::text
		  FROM explorer.swaps
		 WHERE pool_id = $1 AND time >= $2 AND time < $3`, market, from, to)
	if err != nil {
		return nil, fmt.Errorf("trades of %s: %w", market, err)
	}
	var (
		id           int64
		at           time.Time
		price, quote *string
		amount       string
		out          []Trade
	)
	_, err = pgx.ForEachRow(rows, []any{&id, &at, &price, &amount, &quote}, func() error {
		if price != nil {
			t, err := FillTrade(id, at, *price, amount)
			if err != nil {
				return err
			}
			out = append(out, t)
			return nil
		}
		if t, err := SwapTrade(id, at, amount, *quote); err == nil {
			out = append(out, t)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("trades of %s: %w", market, err)
	}
	return out, nil
}

// candleRow is the JSON shape of the candles writes.
type candleRow struct {
	Minutes      int       `json:"minutes"`
	StartAt      time.Time `json:"start_at"`
	Open         string    `json:"open"`
	High         string    `json:"high"`
	Low          string    `json:"low"`
	Close        string    `json:"close"`
	BaseVolume   string    `json:"base_volume"`
	QuoteVolume  string    `json:"quote_volume"`
	FirstTradeID int64     `json:"first_trade_id"`
	LastTradeID  int64     `json:"last_trade_id"`
}

const (
	// deleteOthersSQL drops market's candles in [$2, $3) that $4 does not
	// hold.
	deleteOthersSQL = `DELETE FROM explorer.candles c
		 WHERE c.market_id = $1 AND c.start_at >= $2 AND c.start_at < $3
		   AND NOT EXISTS (SELECT 1 FROM jsonb_to_recordset($4::jsonb) AS r(minutes smallint, start_at timestamptz)
		                    WHERE r.minutes = c.minutes AND r.start_at = c.start_at)`
	// upsertSQL writes the candles $2 of market $1, leaving equal rows alone.
	upsertSQL = `INSERT INTO explorer.candles AS c (market_id, minutes, start_at, open, high, low, close,
			base_volume, quote_volume, first_trade_id, last_trade_id)
		SELECT $1, r.minutes, r.start_at, r.open, r.high, r.low, r.close, r.base_volume, r.quote_volume,
		       r.first_trade_id, r.last_trade_id
		  FROM jsonb_to_recordset($2::jsonb) AS r(minutes smallint, start_at timestamptz, open numeric, high numeric,
		       low numeric, close numeric, base_volume numeric, quote_volume numeric, first_trade_id bigint,
		       last_trade_id bigint)
		ON CONFLICT (market_id, minutes, start_at) DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high,
		       low = EXCLUDED.low, close = EXCLUDED.close, base_volume = EXCLUDED.base_volume,
		       quote_volume = EXCLUDED.quote_volume, first_trade_id = EXCLUDED.first_trade_id,
		       last_trade_id = EXCLUDED.last_trade_id
		 WHERE (c.open, c.high, c.low, c.close, c.base_volume, c.quote_volume, c.first_trade_id, c.last_trade_id)
		       IS DISTINCT FROM (EXCLUDED.open, EXCLUDED.high, EXCLUDED.low, EXCLUDED.close, EXCLUDED.base_volume,
		       EXCLUDED.quote_volume, EXCLUDED.first_trade_id, EXCLUDED.last_trade_id)`
)

// Replace implements Store.
func (s *PGStore) Replace(ctx context.Context, market string, from, to time.Time, candles []Candle) error {
	rows := make([]candleRow, len(candles))
	for i, c := range candles {
		rows[i] = candleRow{Minutes: c.Minutes, StartAt: c.Start, Open: c.Open.String(), High: c.High.String(),
			Low: c.Low.String(), Close: c.Close.String(), BaseVolume: c.BaseVolume.String(),
			QuoteVolume: c.QuoteVolume.String(), FirstTradeID: c.FirstTradeID, LastTradeID: c.LastTradeID}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("candles of %s: %w", market, err)
	}
	if _, err := s.tx.Exec(ctx, deleteOthersSQL, market, from, to, payload); err != nil {
		return fmt.Errorf("candles of %s: delete: %w", market, err)
	}
	if len(rows) == 0 {
		return nil
	}
	if _, err := s.tx.Exec(ctx, upsertSQL, market, payload); err != nil {
		return fmt.Errorf("candles of %s: write: %w", market, err)
	}
	return nil
}

// Span implements Store.
func (s *PGStore) Span(ctx context.Context, market string) (first, last time.Time, ok bool, err error) {
	var lo, hi *time.Time
	rows, err := s.tx.Query(ctx, `SELECT min(time), max(time) FROM (
		SELECT time FROM explorer.order_fills WHERE market_id = $1 AND trade_id IS NOT NULL
		UNION ALL
		SELECT time FROM explorer.swaps WHERE pool_id = $1) t`, market)
	if err == nil {
		_, err = pgx.ForEachRow(rows, []any{&lo, &hi}, func() error { return nil })
	}
	if err != nil {
		return first, last, false, fmt.Errorf("trades of %s: span: %w", market, err)
	}
	if lo == nil || hi == nil {
		return first, last, false, nil
	}
	return *lo, *hi, true, nil
}

// Pending implements Store: the row 'candles' of explorer.pending_rewrites.
func (s *PGStore) Pending(ctx context.Context) (bool, error) {
	var pending bool
	rows, err := s.tx.Query(ctx, `SELECT EXISTS (SELECT 1 FROM explorer.pending_rewrites WHERE name = $1)`, pendingRow)
	if err == nil {
		_, err = pgx.ForEachRow(rows, []any{&pending}, func() error { return nil })
	}
	if err != nil {
		return false, fmt.Errorf("candles: pending build: %w", err)
	}
	return pending, nil
}

// Touched implements Store: one touch per market and UTC day.
func (s *PGStore) Touched(ctx context.Context, lo, hi int64) ([]Touch, error) {
	rows, err := s.tx.Query(ctx, `SELECT market_id, date_bin('1 day', time, timestamptz '1970-01-01 00:00:00Z')
		  FROM explorer.order_fills WHERE height >= $1 AND height < $2 AND trade_id IS NOT NULL
		UNION
		SELECT pool_id, date_bin('1 day', time, timestamptz '1970-01-01 00:00:00Z')
		  FROM explorer.swaps WHERE height >= $1 AND height < $2`, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("trades %d..%d: %w", lo, hi-1, err)
	}
	var t Touch
	var out []Touch
	_, err = pgx.ForEachRow(rows, []any{&t.Market, &t.Time}, func() error {
		out = append(out, t)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("trades %d..%d: %w", lo, hi-1, err)
	}
	return out, nil
}

// ReadDB is the part of a pgx pool PGReader needs; *pgxpool.Pool satisfies
// it.
type ReadDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGReader implements Reader over explorer.candles and the trades.
type PGReader struct {
	db ReadDB
}

// NewPGReader returns a reader over db.
func NewPGReader(db ReadDB) *PGReader {
	return &PGReader{db: db}
}

// Candles implements Reader.
func (r *PGReader) Candles(ctx context.Context, market string, minutes int, from time.Time, limit int) ([]Candle, error) {
	rows, err := r.db.Query(ctx, `SELECT start_at, open::text, high::text, low::text, close::text, base_volume::text,
			quote_volume::text, first_trade_id, last_trade_id
		  FROM explorer.candles WHERE market_id = $1 AND minutes = $2 AND start_at >= $3
		 ORDER BY start_at DESC LIMIT $4`, market, minutes, from, limit)
	if err != nil {
		return nil, fmt.Errorf("candles of %s: %w", market, err)
	}
	var (
		c                              = Candle{Market: market, Minutes: minutes}
		open, high, low, cls, base, qt string
		out                            = []Candle{}
	)
	_, err = pgx.ForEachRow(rows, []any{&c.Start, &open, &high, &low, &cls, &base, &qt, &c.FirstTradeID, &c.LastTradeID},
		func() error {
			var ok1, ok2 bool
			var err error
			if c.Open, err = sdkmath.LegacyNewDecFromStr(open); err != nil {
				return err
			}
			if c.High, err = sdkmath.LegacyNewDecFromStr(high); err != nil {
				return err
			}
			if c.Low, err = sdkmath.LegacyNewDecFromStr(low); err != nil {
				return err
			}
			if c.Close, err = sdkmath.LegacyNewDecFromStr(cls); err != nil {
				return err
			}
			c.BaseVolume, ok1 = sdkmath.NewIntFromString(base)
			c.QuoteVolume, ok2 = sdkmath.NewIntFromString(qt)
			if !ok1 || !ok2 {
				return fmt.Errorf("volumes %q / %q are not integers", base, qt)
			}
			c.Start = c.Start.UTC()
			out = append(out, c)
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("candles of %s: %w", market, err)
	}
	return out, nil
}

// First implements Reader: a fill makes an order book market, a swap a
// pool, with its denoms.
func (r *PGReader) First(ctx context.Context, market string) (First, bool, error) {
	rows, err := r.db.Query(ctx, `(SELECT time, '', '' FROM explorer.order_fills
		 WHERE market_id = $1 AND trade_id IS NOT NULL ORDER BY time LIMIT 1)
		UNION ALL
		(SELECT time, base, quote FROM explorer.swaps WHERE pool_id = $1 ORDER BY time LIMIT 1)`, market)
	if err != nil {
		return First{}, false, fmt.Errorf("first trade of %s: %w", market, err)
	}
	var (
		f     First
		found bool
		got   First
	)
	_, err = pgx.ForEachRow(rows, []any{&got.Time, &got.Base, &got.Quote}, func() error {
		if !found || got.Time.Before(f.Time) {
			f, found = got, true
		}
		return nil
	})
	if err != nil {
		return First{}, false, fmt.Errorf("first trade of %s: %w", market, err)
	}
	return f, found, nil
}
