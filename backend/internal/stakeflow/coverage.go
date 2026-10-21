package stakeflow

import (
	"sort"
	"time"
)

// Span is a run of consecutive indexed heights with the times of its ends.
type Span struct {
	Lo, Hi         int64
	LoTime, HiTime time.Time
}

// coverage is the set of indexed heights the set has seen, as sorted,
// disjoint and non-adjacent spans. It tells which heights were already
// applied (a flush retried after a rollback, a height the load had) and
// whether the window is fully indexed.
type coverage struct {
	spans []Span
}

// find returns the index of the first span whose Hi is at least h.
func (c *coverage) find(h int64) int {
	return sort.Search(len(c.spans), func(i int) bool { return c.spans[i].Hi >= h })
}

func (c *coverage) has(h int64) bool {
	i := c.find(h)
	return i < len(c.spans) && c.spans[i].Lo <= h
}

// add records height h at time t; a height already covered is a no-op.
func (c *coverage) add(h int64, t time.Time) {
	i := c.find(h)
	if i < len(c.spans) && c.spans[i].Lo <= h {
		return
	}
	joinsPrev := i > 0 && c.spans[i-1].Hi == h-1
	joinsNext := i < len(c.spans) && c.spans[i].Lo == h+1
	switch {
	case joinsPrev && joinsNext:
		c.spans[i-1].Hi, c.spans[i-1].HiTime = c.spans[i].Hi, c.spans[i].HiTime
		c.spans = append(c.spans[:i], c.spans[i+1:]...)
	case joinsPrev:
		c.spans[i-1].Hi, c.spans[i-1].HiTime = h, t
	case joinsNext:
		c.spans[i].Lo, c.spans[i].LoTime = h, t
	default:
		c.spans = append(c.spans, Span{})
		copy(c.spans[i+1:], c.spans[i:])
		c.spans[i] = Span{Lo: h, Hi: h, LoTime: t, HiTime: t}
	}
}

// addSpan records every height of s.
func (c *coverage) addSpan(s Span) {
	if s.Lo > s.Hi {
		return
	}
	// Adding height by height would be slow for long runs: the ends are
	// added (joining any adjacent span) and everything between them merged.
	c.add(s.Lo, s.LoTime)
	c.add(s.Hi, s.HiTime)
	i, j := c.find(s.Lo), c.find(s.Hi)
	merged := c.spans[i]
	merged.Hi, merged.HiTime = c.spans[j].Hi, c.spans[j].HiTime
	c.spans = append(c.spans[:i+1], c.spans[j+1:]...)
	c.spans[i] = merged
}

// prune drops the spans that end before cutoff, all but the highest: that
// one is enough to prove the window reaches past its start once the newest
// span grows down to it.
func (c *coverage) prune(cutoff time.Time) {
	n := sort.Search(len(c.spans), func(i int) bool { return !c.spans[i].HiTime.Before(cutoff) })
	if n > 1 {
		c.spans = append(c.spans[:0], c.spans[n-1:]...)
	}
}

// complete reports whether every height from before cutoff up to the newest
// height is indexed: the newest span starts before cutoff.
func (c *coverage) complete(cutoff time.Time) bool {
	if len(c.spans) == 0 {
		return false
	}
	return c.spans[len(c.spans)-1].LoTime.Before(cutoff)
}

func (c *coverage) top() (Span, bool) {
	if len(c.spans) == 0 {
		return Span{}, false
	}
	return c.spans[len(c.spans)-1], true
}
