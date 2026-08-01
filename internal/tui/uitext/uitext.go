// Package uitext holds small text-shaping helpers shared by more than one
// view: truncating a string to a display-cell width, and stripping the
// control characters bv's own hand-edited, br-rejectable JSONL is not
// guaranteed to be free of. listview needed both first; detail is the
// second consumer, and the tree and board views (Tasks 5.2 and 6.2) are
// slated to need truncation too, which is what makes a shared package worth
// it rather than three private copies drifting apart.
package uitext

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ellipsis marks a truncated string. One cell wide, unlike "...".
const ellipsis = "…"

// Truncate shortens s to at most width terminal cells.
//
// Width is measured in display cells, not bytes or runes: CJK ideographs and
// most emoji occupy two cells each. A rune-based truncation renders roughly
// twice the requested width for a CJK title, and the overflow wraps and
// shifts every row below it.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}

	return ansi.Truncate(s, width, ellipsis)
}

// Sanitize drops control characters (runes below 0x20, plus 0x7f/DEL) from s.
//
// bv renders rather than validates hand-edited and br-rejected JSONL, so a
// title, label or comment is not guaranteed to be free of a raw newline or
// escape byte. Only control bytes are removed — every printable multi-byte
// rune (CJK, emoji) passes through untouched, which is what keeps this from
// repeating the byte-vs-cell mistake Truncate exists to avoid.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, s)
}
