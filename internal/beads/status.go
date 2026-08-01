package beads

import "strings"

// Status is an issue's workflow state.
//
// It is deliberately a string rather than an integer enum: br models status as
// an open set (a Custom(String) variant), so a project can define its own
// statuses and a future br release can add one. An unrecognised value must
// survive a decode as data — a viewer that rejected it would hide issues.
type Status string

// The statuses br defines. Others are valid and are kept verbatim.
const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusDeferred   Status = "deferred"
	StatusDraft      Status = "draft"
	StatusClosed     Status = "closed"
	StatusTombstone  Status = "tombstone"
	StatusPinned     Status = "pinned"
)

// IsTerminal reports whether the status means the issue is done with.
//
// This is the predicate the blocker rule uses: a dependency on a terminal
// issue does not block. Unknown statuses are non-terminal, so a status br
// adds later blocks conservatively rather than silently unblocking work.
func (s Status) IsTerminal() bool {
	return s == StatusClosed || s == StatusTombstone
}

// IsKnown reports whether the status is one br defines.
func (s Status) IsKnown() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusBlocked, StatusDeferred,
		StatusDraft, StatusClosed, StatusTombstone, StatusPinned:
		return true
	default:
		return false
	}
}

// Display returns the status in title case for the UI, leaving unknown values
// untouched so a custom status reads the way its author spelled it.
func (s Status) Display() string {
	if !s.IsKnown() {
		return sanitizeDisplay(string(s))
	}

	words := strings.Split(string(s), "_")
	for i, word := range words {
		if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}

	return strings.Join(words, " ")
}

// sanitizeDisplay drops control characters before Display's open-enum
// fallback hands them to the renderer verbatim (I6). Duplicated, not
// imported, from uitext.Sanitize: this package sits below it.
func sanitizeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, s)
}
