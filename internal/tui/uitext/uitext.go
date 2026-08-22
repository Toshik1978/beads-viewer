// Package uitext holds small text-shaping helpers shared by more than one
// view: truncating a string to a display-cell width, and stripping the
// control characters bv's own hand-edited, br-rejectable JSONL is not
// guaranteed to be free of. listview needed both first; detail is the
// second consumer, and the tree and board views (Tasks 5.2 and 6.2) are
// slated to need truncation too, which is what makes a shared package worth
// it rather than three private copies drifting apart.
package uitext

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ellipsis marks a truncated string. One cell wide, unlike "...".
const ellipsis = "…"

// AgeWidth is the width of the widest string RelativeAge returns, so a caller
// can reserve the column before knowing what will go in it. Four is right, but
// not because of "11mo": the "mo" arm runs to the 365-day cutover, so days
// 360-364 render "12mo" — four cells too, and nothing wider is reachable.
const AgeWidth = 4

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

// TruncateLeft shortens s to at most width terminal cells by cutting from
// the head rather than the tail, marking the cut with the same ellipsis.
//
// It is Truncate's mirror, for the one place where the end of a string is
// the part worth keeping: a text buffer being edited, where the tail is
// where the next character lands. A row label or a title is the other way
// round — its head is what identifies it — which is why both exist.
func TruncateLeft(s string, width int) string {
	if width <= 0 {
		return ""
	}

	cells := ansi.StringWidth(s)
	if cells <= width {
		return s
	}

	// One cell of the budget goes to the ellipsis itself, so the cut has to
	// be that much deeper than the raw overflow.
	cut := cells - width + 1
	out := ansi.TruncateLeft(s, cut, ellipsis)

	// ansi.TruncateLeft drops whole graphemes, so a cut landing inside a
	// two-cell glyph keeps that glyph and overshoots the budget by exactly
	// the part that did not fit. Deepening the cut by the overshoot moves the
	// boundary to that glyph'"'"'s own end, so one correction always lands — and
	// can only leave the result narrower than asked, never wider.
	if over := ansi.StringWidth(out) - width; over > 0 {
		out = ansi.TruncateLeft(s, cut+over, ellipsis)
	}

	return out
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

// RelativeAge renders how long before now then was, in at most AgeWidth
// cells: "now", "12m", "3h", "2d", "4w" (never "5w" — the 30-day cutover
// bounds it), "8mo", "2y".
//
// now is a parameter rather than time.Now() read here, which is what makes
// this pure and its table test deterministic — and gochecknoglobals rules out
// a package-level clock anyway.
//
// A zero then returns "" rather than an age measured from year 1: that is
// what a record with no updated_at produces, and "2025y" would be both wrong
// and too wide. A then in the future returns "now" — clock skew between the
// machine that wrote the record and this one is ordinary.
func RelativeAge(now, then time.Time) string {
	if then.IsZero() {
		return ""
	}

	d := now.Sub(then)
	if d < time.Minute {
		return "now"
	}

	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}
