// Package supply serves a denom's total and circulating supply in display
// units, the way the BZE aggregator's /api/supply routes do: the bank
// supply of the one denom, minus the community pool for the bond denom,
// with exactly 2 decimals rounded half up. A read is kept for FreshFor and
// served for BackupFor more while the node fails.
package supply

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

const (
	// FreshFor is how long a read is served before the node is asked again:
	// the aggregator's cache TTL.
	FreshFor = 600 * time.Second
	// BackupFor is how long a read is served while the node fails.
	BackupFor = 24 * time.Hour
	// MaxExponent is the highest display exponent a supply is formatted
	// with, as the aggregator checks it.
	MaxExponent = 18
)

// Bank is the part of the bank query client the service uses;
// banktypes.QueryClient satisfies it.
type Bank interface {
	SupplyOf(ctx context.Context, in *banktypes.QuerySupplyOfRequest, opts ...grpc.CallOption) (*banktypes.QuerySupplyOfResponse, error)
}

// Distribution is the part of the distribution query client the service
// uses; distrtypes.QueryClient satisfies it.
type Distribution interface {
	CommunityPool(ctx context.Context, in *distrtypes.QueryCommunityPoolRequest, opts ...grpc.CallOption) (*distrtypes.QueryCommunityPoolResponse, error)
}

// Denoms reads a denom's display exponent from explorer.denoms; found is
// false when the explorer does not know the denom.
type Denoms interface {
	Exponent(ctx context.Context, denom string) (exponent int, found bool, err error)
}

// Clock tells the time; the real clock when nil.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Deps is what the service reads from.
type Deps struct {
	Bank         Bank
	Distribution Distribution
	Denoms       Denoms
	Clock        Clock
	Log          log.FieldLogger
}

// BadDenomError is a request the aggregator answers with a 400: Error is its
// exact plain-text message.
type BadDenomError struct{ msg string }

func (e *BadDenomError) Error() string { return e.msg }

// ErrUnavailable is returned when the supply cannot be read and no read of
// the last BackupFor is held.
var ErrUnavailable = errors.New("supply is unavailable, try again later")

// Service serves supplies from memory, one entry per denom and kind.
type Service struct {
	deps   Deps
	flight singleflight.Group

	mu      sync.Mutex
	held    map[string]entry
	failing bool
}

type entry struct {
	value  string
	readAt time.Time
}

// New returns a service over deps.
func New(deps Deps) *Service {
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	if deps.Log == nil {
		deps.Log = log.StandardLogger()
	}
	return &Service{deps: deps, held: map[string]entry{}}
}

// Total returns denom's bank supply in display units.
func (s *Service) Total(ctx context.Context, denom string) (string, error) {
	return s.get(ctx, denom, false)
}

// Circulating returns the bond denom's supply minus the community pool, in
// display units; for any other denom, its total.
func (s *Service) Circulating(ctx context.Context, denom string) (string, error) {
	return s.get(ctx, denom, denom == chain.BondDenom)
}

// get serves the held read while younger than FreshFor, else a new one.
// When the new read fails, the held read is served while younger than
// BackupFor, and ErrUnavailable after that. A denom the request gets wrong
// is a *BadDenomError and is never held.
func (s *Service) get(ctx context.Context, denom string, lessPool bool) (string, error) {
	if err := sdk.ValidateDenom(denom); err != nil {
		return "", notFound(denom)
	}
	key := "total:" + denom
	if lessPool {
		key = "circulating:" + denom
	}
	if e, ok := s.entry(key); ok && s.deps.Clock.Now().Sub(e.readAt) < FreshFor {
		return e.value, nil
	}
	v, err, _ := s.flight.Do(key, func() (any, error) {
		value, err := s.read(context.WithoutCancel(ctx), denom, lessPool)
		var bad *BadDenomError
		if errors.As(err, &bad) {
			return nil, err
		}
		s.note(err)
		now := s.deps.Clock.Now()
		s.mu.Lock()
		defer s.mu.Unlock()
		if err == nil {
			s.held[key] = entry{value: value, readAt: now}
			return value, nil
		}
		if e, ok := s.held[key]; ok && now.Sub(e.readAt) < BackupFor {
			return e.value, nil
		}
		return nil, ErrUnavailable
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (s *Service) entry(key string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.held[key]
	return e, ok
}

// read asks the explorer for the exponent and the node for the supply (and
// the community pool when lessPool) and formats the result.
func (s *Service) read(ctx context.Context, denom string, lessPool bool) (string, error) {
	exp, found, err := s.deps.Denoms.Exponent(ctx, denom)
	if err != nil {
		return "", fmt.Errorf("supply: exponent of %s: %w", denom, err)
	}
	if !found && denom == chain.BondDenom {
		exp, found = chain.BondDenomExponent, true
	}
	if exp < 0 || exp > MaxExponent {
		s.deps.Log.Errorf("supply: unexpected display exponent %d for denom %s (expected 0 to %d)", exp, denom, MaxExponent)
		return "", &BadDenomError{msg: denom + " has an unsupported display exponent"}
	}
	res, err := s.deps.Bank.SupplyOf(ctx, &banktypes.QuerySupplyOfRequest{Denom: denom})
	if err != nil {
		return "", fmt.Errorf("supply: bank SupplyOf %s: %w", denom, err)
	}
	amount := res.Amount.Amount
	if amount.IsNil() {
		return "", fmt.Errorf("supply: bank SupplyOf %s: no amount", denom)
	}
	// A denom the explorer has not synced yet but the chain has minted is
	// served with exponent 0, the explorer's default.
	if !found && amount.IsZero() {
		return "", notFound(denom)
	}
	if !lessPool {
		return format(amount.BigInt(), exp), nil
	}

	pool, err := s.deps.Distribution.CommunityPool(ctx, &distrtypes.QueryCommunityPoolRequest{})
	if err != nil {
		return "", fmt.Errorf("supply: distribution CommunityPool: %w", err)
	}
	// The pool is a decimal with 18 places: subtract it from the supply
	// scaled by 10^18, exactly.
	scaled := new(big.Int).Mul(amount.BigInt(), pow10(sdkPrecision))
	scaled.Sub(scaled, pool.Pool.AmountOf(denom).BigInt())
	if scaled.Sign() < 0 {
		return "", fmt.Errorf("supply: the community pool exceeds the supply of %s", denom)
	}
	return format(scaled, exp+sdkPrecision), nil
}

// sdkPrecision is the number of decimal places of a LegacyDec.
const sdkPrecision = 18

// format writes n / 10^scale with exactly 2 decimals, rounding half up with
// integer arithmetic, as the aggregator formats a supply. n is not negative.
func format(n *big.Int, scale int) string {
	divisor := pow10(scale)
	cents, rem := new(big.Int).QuoRem(new(big.Int).Mul(n, big.NewInt(100)), divisor, new(big.Int))
	if rem.Lsh(rem, 1).Cmp(divisor) >= 0 {
		cents.Add(cents, big.NewInt(1))
	}
	whole, frac := new(big.Int).QuoRem(cents, big.NewInt(100), new(big.Int))
	return fmt.Sprintf("%s.%02d", whole, frac.Int64())
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// notFound is the aggregator's answer for a denom it does not know.
func notFound(denom string) error {
	return &BadDenomError{msg: fmt.Sprintf("denom %s not found in registry", denom)}
}

// note logs a failure when one starts and the recovery when it ends, not
// every read.
func (s *Service) note(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err != nil && !s.failing:
		s.failing = true
		s.deps.Log.WithError(err).Warn("supply: reads fail, the last reads are served for up to 24 hours")
	case err == nil && s.failing:
		s.failing = false
		s.deps.Log.Info("supply: reads answer again")
	}
}
