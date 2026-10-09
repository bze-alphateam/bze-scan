package reindex

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// ErrSelection: the selector flags are missing, combined or invalid.
var ErrSelection = errors.New("invalid selection")

// Kind is the selector a reindex was given.
type Kind int

const (
	// KindHeights is --heights: an explicit list.
	KindHeights Kind = iota + 1
	// KindRange is --from/--to: every height of an inclusive range.
	KindRange
	// KindFailed is --failed: the open index_failures rows.
	KindFailed
)

// Flags are the selector flags as given on the command line; a nil pointer
// or an empty string is a flag that was not given.
type Flags struct {
	Heights  string
	From, To *int64
	Failed   bool
	Source   string
}

// Selection is a validated selector.
type Selection struct {
	Kind Kind
	// Heights of KindHeights, highest first, without duplicates.
	Heights []int64
	// From and To bound KindRange, both included.
	From, To int64
	// Source narrows KindFailed to one index_failures.source; empty is
	// every source.
	Source string
}

// failureSources are the values of index_failures.source.
var failureSources = []string{writer.FailureSourceLive, writer.FailureSourceBackfill, writer.FailureSourceReindex}

// Parse validates the selector flags: exactly one of --heights, --from with
// --to, or --failed; --source only with --failed.
func Parse(f Flags) (Selection, error) {
	given := 0
	if f.Heights != "" {
		given++
	}
	if f.From != nil || f.To != nil {
		given++
	}
	if f.Failed {
		given++
	}
	switch {
	case given == 0:
		return Selection{}, fmt.Errorf("%w: one of --heights, --from/--to or --failed is required", ErrSelection)
	case given > 1:
		return Selection{}, fmt.Errorf("%w: --heights, --from/--to and --failed are mutually exclusive", ErrSelection)
	case f.Source != "" && !f.Failed:
		return Selection{}, fmt.Errorf("%w: --source narrows --failed only", ErrSelection)
	}

	switch {
	case f.Heights != "":
		hs, err := parseHeights(f.Heights)
		if err != nil {
			return Selection{}, err
		}
		return Selection{Kind: KindHeights, Heights: hs}, nil
	case f.Failed:
		if f.Source != "" && !slices.Contains(failureSources, f.Source) {
			return Selection{}, fmt.Errorf("%w: --source must be one of %s (got %q)",
				ErrSelection, strings.Join(failureSources, ", "), f.Source)
		}
		return Selection{Kind: KindFailed, Source: f.Source}, nil
	default:
		if f.From == nil || f.To == nil {
			return Selection{}, fmt.Errorf("%w: --from and --to go together", ErrSelection)
		}
		if *f.From < 1 {
			return Selection{}, fmt.Errorf("%w: --from must be a height of at least 1 (got %d)", ErrSelection, *f.From)
		}
		if *f.From > *f.To {
			return Selection{}, fmt.Errorf("%w: --from %d is above --to %d", ErrSelection, *f.From, *f.To)
		}
		return Selection{Kind: KindRange, From: *f.From, To: *f.To}, nil
	}
}

func parseHeights(list string) ([]int64, error) {
	seen := map[int64]bool{}
	var out []int64
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		h, err := strconv.ParseInt(item, 10, 64)
		if err != nil || h < 1 {
			return nil, fmt.Errorf("%w: --heights takes heights of at least 1 separated by commas (got %q)", ErrSelection, item)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	slices.SortFunc(out, func(a, b int64) int { return cmp.Compare(b, a) })
	return out, nil
}

// Plan is a selection resolved to heights, dispatched highest first like the
// backfill.
type Plan struct {
	// List holds the heights of a list or of the failures; nil for a range.
	List []int64
	// From and To bound a range, both included.
	From, To int64
	isRange  bool
}

// Count is the number of heights planned.
func (p Plan) Count() int64 {
	if p.isRange {
		return p.To - p.From + 1
	}
	return int64(len(p.List))
}

// Lowest and Highest are the extreme heights planned; 0 when empty.
func (p Plan) Lowest() int64 {
	switch {
	case p.isRange:
		return p.From
	case len(p.List) == 0:
		return 0
	default:
		return p.List[len(p.List)-1]
	}
}

func (p Plan) Highest() int64 {
	switch {
	case p.isRange:
		return p.To
	case len(p.List) == 0:
		return 0
	default:
		return p.List[0]
	}
}

// Describe is the dry run's output: a summary line, then each height of a
// list on its own line.
func (p Plan) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "reindex selection heights=%d", p.Count())
	if p.Count() > 0 {
		fmt.Fprintf(&b, " lowest=%d highest=%d", p.Lowest(), p.Highest())
	}
	b.WriteByte('\n')
	for _, h := range p.List {
		fmt.Fprintf(&b, "%d\n", h)
	}
	return b.String()
}

func (p Plan) source() backfill.HeightSource {
	if p.isRange {
		return backfill.Descending(p.To, p.From, nil)
	}
	return &listSource{heights: p.List}
}

// listSource dispatches a list in its order.
type listSource struct {
	heights []int64
	next    int
}

func (s *listSource) Next(context.Context) (int64, bool, error) {
	if s.next >= len(s.heights) {
		return 0, false, nil
	}
	h := s.heights[s.next]
	s.next++
	return h, true, nil
}
