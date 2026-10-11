package archive

import (
	"strings"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// undouble returns events without the second copy of every run that is
// immediately followed by an identical run: v8.0.x's doubled block events
// (see the package documentation).
//
// At each position the longest such run wins, then the scan resumes after
// the dropped copy. Longest first keeps the repeats inside one wrapped call
// (the same participant losing twice in one raffle draw: u u v u u v keeps
// u u v), and resuming after the copy keeps two identical calls apart (A A A
// A keeps A A). A sequence of identical calls that itself repeats (A A B B A
// A B B) is ambiguous on the wire and keeps its first half.
func undouble(events []node.Event) []node.Event {
	if len(events) < 2 {
		return events
	}
	keys := make([]string, len(events))
	for i, ev := range events {
		keys[i] = eventKey(ev)
	}
	out := make([]node.Event, 0, len(events))
	for i := 0; i < len(events); {
		k := longestRepeat(keys, i)
		if k == 0 {
			out = append(out, events[i])
			i++
			continue
		}
		out = append(out, events[i:i+k]...)
		i += 2 * k
	}
	return out
}

// longestRepeat returns the longest k with keys[i:i+k] equal to
// keys[i+k:i+2k], or 0.
func longestRepeat(keys []string, i int) int {
	for k := (len(keys) - i) / 2; k >= 1; k-- {
		if keys[i] != keys[i+k] {
			continue
		}
		same := true
		for j := 1; j < k; j++ {
			if keys[i+j] != keys[i+k+j] {
				same = false
				break
			}
		}
		if same {
			return k
		}
	}
	return 0
}

// eventKey identifies an event by its type and every attribute, in order.
func eventKey(ev node.Event) string {
	var b strings.Builder
	b.WriteString(ev.Type)
	for _, a := range ev.Attributes {
		b.WriteByte(0)
		b.WriteString(a.Key)
		b.WriteByte(1)
		b.WriteString(a.Value)
	}
	return b.String()
}
