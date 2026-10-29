package trades

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The cursor walks past heights that have no blocks row yet: a hole the
// backfill fills later, possibly the previous release's backfill during a
// deploy's overlap, whose blocks carry no trade ids and no swaps rows. A
// pass keeps those heights as holes, inclusive ranges in indexer_state under
// HolesKey, and the next passes number the heights that have appeared in
// them since and cut them out. The lookup is a primary-key range scan of
// explorer.blocks over the holes only: what stays a hole for good (the
// heights below the live floor of a database never backfilled) costs an
// empty index range per pass, a backfill under way only the heights it
// wrote since the last pass. Every change of the holes runs under FOR
// UPDATE on their row, which exists before any pass reads it (seedHoles,
// loadHoles), so concurrent passes (two serves) never lose one.
//
// The first run of a release that tracks holes finds no row: the heights
// below the cursor were passed without being recorded, so the row starts as
// [0, cursor − 1] and that first pass checks every stored height below the
// cursor once (the rows already there are kept, the ids are the same). A
// database whose cursor is still at 0 starts with no hole.

// HolesKey is the indexer_state key of the heights below the cursor that had
// no block when the cursor passed them.
const HolesKey = "trades_holes"

// HoleBatch bounds the heights one transaction takes out of the holes.
const HoleBatch = 100_000

// hole is an inclusive range of heights.
type hole [2]int64

// seedHoles creates the holes row, [0, cursor − 1] (none at 0), unless it
// exists; a statement of its own, so it is there before any transaction of
// the pass locks it.
func seedHoles(ctx context.Context, db DB, cursor int64) error {
	seed := "[]"
	if cursor > 0 {
		seed = fmt.Sprintf("[[0,%d]]", cursor-1)
	}
	if _, err := db.Exec(ctx, insertHolesSQL, HolesKey, seed); err != nil {
		return fmt.Errorf("trades holes: seed: %w", err)
	}
	return nil
}

// insertHolesSQL creates the holes row when it is missing.
const insertHolesSQL = `INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ($1, $2, now())
	ON CONFLICT (key) DO NOTHING`

// loadHoles reads the holes in tx, locking their row, which it creates
// empty first when missing so the lock always holds a row.
func loadHoles(ctx context.Context, tx Tx) ([]hole, error) {
	if _, err := tx.Exec(ctx, insertHolesSQL, HolesKey, "[]"); err != nil {
		return nil, fmt.Errorf("trades holes: %w", err)
	}
	var raw string
	err := tx.QueryRow(ctx, `SELECT value FROM explorer.indexer_state WHERE key = $1 FOR UPDATE`, HolesKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("trades holes: %w", err)
	}
	var holes []hole
	if err := json.Unmarshal([]byte(raw), &holes); err != nil {
		return nil, fmt.Errorf("trades holes %q: %w", raw, err)
	}
	return holes, nil
}

// saveHoles writes the holes in tx.
func saveHoles(ctx context.Context, tx Tx, holes []hole) error {
	if holes == nil {
		holes = []hole{}
	}
	raw, err := json.Marshal(holes)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO explorer.indexer_state (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, HolesKey, string(raw))
	if err != nil {
		return fmt.Errorf("trades holes: %w", err)
	}
	return nil
}

// presentIn returns the stored heights of [lo, hi], ascending, at most limit.
func presentIn(ctx context.Context, tx Tx, lo, hi int64, limit int) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT height FROM explorer.blocks WHERE height >= $1 AND height <= $2
		ORDER BY height LIMIT $3`, lo, hi, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// gaps returns the heights of h not in present (ascending, inside h), as
// holes; when present was cut at a limit, everything above its last height
// stays a hole.
func gaps(h hole, present []int64) []hole {
	var out []hole
	next := h[0]
	for _, p := range present {
		if p > next {
			out = append(out, hole{next, p - 1})
		}
		next = p + 1
	}
	if next <= h[1] {
		out = append(out, hole{next, h[1]})
	}
	return out
}

// runs groups ascending heights into consecutive [lo, hi) ranges.
func runs(heights []int64) [][2]int64 {
	var out [][2]int64
	for _, h := range heights {
		if n := len(out); n > 0 && out[n-1][1] == h {
			out[n-1][1] = h + 1
			continue
		}
		out = append(out, [2]int64{h, h + 1})
	}
	return out
}

// merge adds add to holes, which stay ascending and disjoint, adjacent ranges
// joined.
func merge(holes []hole, add ...hole) []hole {
	all := append(append([]hole(nil), holes...), add...)
	for i := 1; i < len(all); i++ { // insertion sort: few holes
		for j := i; j > 0 && all[j][0] < all[j-1][0]; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	var out []hole
	for _, h := range all {
		if n := len(out); n > 0 && h[0] <= out[n-1][1]+1 {
			out[n-1][1] = max(out[n-1][1], h[1])
			continue
		}
		out = append(out, h)
	}
	return out
}

// fillHoles numbers the heights stored in the holes since they were
// recorded and takes them out, HoleBatch at a time, one transaction each.
func fillHoles(ctx context.Context, db DB, res *Result) error {
	for {
		var done bool
		err := retry(func() error {
			var err error
			done, err = fillHolesOnce(ctx, db, res)
			return err
		})
		if err != nil || done {
			return err
		}
	}
}

// fillHolesOnce runs one transaction of fillHoles and reports whether the
// holes held nothing more.
func fillHolesOnce(ctx context.Context, db DB, res *Result) (bool, error) {
	tx, err := begin(ctx, db)
	if err != nil {
		return false, fmt.Errorf("trades holes: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	holes, err := loadHoles(ctx, tx)
	if err != nil || len(holes) == 0 {
		return true, err
	}
	var (
		left  []hole
		found []int64
	)
	budget := HoleBatch
	for _, h := range holes {
		if budget == 0 {
			left = append(left, h)
			continue
		}
		present, err := presentIn(ctx, tx, h[0], h[1], budget)
		if err != nil {
			return false, fmt.Errorf("trades holes %d..%d: %w", h[0], h[1], err)
		}
		budget -= len(present)
		found = append(found, present...)
		if len(present) > 0 && budget == 0 {
			// Cut at the limit: above the last height is unexamined.
			left = append(left, gaps(hole{h[0], present[len(present)-1]}, present)...)
			if last := present[len(present)-1]; last < h[1] {
				left = append(left, hole{last + 1, h[1]})
			}
			continue
		}
		left = append(left, gaps(h, present)...)
	}
	if len(found) == 0 {
		return true, nil
	}
	heights := 0
	for _, r := range runs(found) {
		n, err := Range(ctx, tx, r[0], r[1])
		if err != nil {
			return false, err
		}
		heights += n
	}
	if err := saveHoles(ctx, tx, merge(nil, left...)); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("trades holes: commit: %w", err)
	}
	res.Ranges++
	res.Heights += heights
	return budget > 0, nil
}

// recordHoles adds the heights of [lo, hi) without a block to the holes, in
// the cursor's transaction.
func recordHoles(ctx context.Context, tx Tx, lo, hi int64) error {
	present, err := presentIn(ctx, tx, lo, hi-1, int(hi-lo))
	if err != nil {
		return fmt.Errorf("trades %d..%d: blocks: %w", lo, hi-1, err)
	}
	missing := gaps(hole{lo, hi - 1}, present)
	if len(missing) == 0 {
		return nil
	}
	holes, err := loadHoles(ctx, tx)
	if err != nil {
		return err
	}
	return saveHoles(ctx, tx, merge(holes, missing...))
}
