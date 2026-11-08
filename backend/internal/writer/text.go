package writer

import (
	"bytes"
	"strings"
)

// PostgreSQL stores no NUL character: jsonb input refuses the \u0000 escape
// and text refuses the byte, so one such character in a memo, a message field
// or an event attribute would fail the write of its whole block. Both
// functions store U+FFFD in its place, which keeps the value visibly altered
// instead of joining the text around it.
const replacementChar = "\uFFFD"

var (
	nulEscape         = []byte(`\u0000`)
	replacementEscape = []byte(`\ufffd`)
)

// storableJSON replaces every NUL escape of the marshalled JSON payload, in
// place. Escapes are walked in pairs so that \\u0000, an escaped backslash
// followed by plain text, is left alone.
func storableJSON(payload []byte) []byte {
	if !bytes.Contains(payload, nulEscape) {
		return payload
	}
	for i := 0; i+1 < len(payload); {
		switch {
		case payload[i] != '\\':
			i++
		case bytes.HasPrefix(payload[i:], nulEscape):
			copy(payload[i:], replacementEscape)
			i += len(nulEscape)
		default:
			i += 2
		}
	}
	return payload
}

// storableText makes s valid for a text column.
func storableText(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", replacementChar), replacementChar)
}
