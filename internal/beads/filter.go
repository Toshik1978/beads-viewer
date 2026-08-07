package beads

import (
	"fmt"
	"slices"
	"strings"
)

// Filter narrows a snapshot.
//
// It lives in the domain and is applied once, in the app, with the result
// handed to every view. That is what makes a query mean the same thing in the
// list, the tree and the board — the alternative, filtering per view, is how
// the pre-rewrite tree ended up with three subtly different behaviours.
type Filter struct {
	// Text matches case-insensitively against id, title and labels.
	Text string
	// Status, when set, must match exactly.
	Status Status
	// Labels is conjunctive: an issue must carry all of them.
	Labels []string
	// HideClosed drops terminal statuses.
	HideClosed bool
	// ShowTombstones surfaces deletion markers, hidden by default.
	ShowTombstones bool
}

// IsZero reports whether the filter leaves the default view untouched, so the
// UI can hide its "filter active" affordance.
//
// ShowTombstones counts, even though it widens rather than narrows: a view
// showing deletion markers is not the default view, and a consumer that gated
// a clear-filter control on IsZero would otherwise leave the user no way back.
func (f Filter) IsZero() bool {
	return strings.TrimSpace(f.Text) == "" &&
		f.Status == "" &&
		len(f.Labels) == 0 &&
		!f.HideClosed &&
		!f.ShowTombstones
}

// Matches reports whether one issue survives the filter.
func (f Filter) Matches(issue *Issue) bool {
	if issue.IsTombstone() && !f.ShowTombstones {
		return false
	}
	if f.HideClosed && issue.Status.IsTerminal() {
		return false
	}
	if f.Status != "" && issue.Status != f.Status {
		return false
	}
	if !hasAllLabels(issue, f.Labels) {
		return false
	}

	return f.matchesText(issue)
}

// Any reports whether at least one issue in s survives f, short-circuiting
// on the first match rather than building the full matching set Apply would.
// A caller that only needs to know "is this empty?" — internal/tui's
// Model.body, checking whether the active filter leaves anything to show —
// should call this instead of len(f.Apply(s).Issues()) == 0: benchmarked on
// a 758-record fixture, Apply alone costs 149us/818KB/1,655 allocs per call
// and re-runs every frame, while Any allocates nothing and returns as soon
// as it finds one match.
func (f Filter) Any(s *Snapshot) bool {
	return slices.ContainsFunc(s.issues, f.Matches)
}

// Apply returns a new snapshot containing only matching issues.
//
// Re-indexing through NewSnapshot rather than copying the parent's indexes is
// deliberate: the narrowed set has its own hierarchy, and a stale children map
// would point at issues the filter removed.
//
// That is the right answer for what the views draw and the wrong one for what
// they derive — an issue's blockers are a fact about the workspace, not about
// what survived a filter — so the result also records where it came from, and
// derive.go's rules resolve against that instead. See Snapshot's unfiltered
// field. Narrowing an already-narrowed snapshot records the same origin rather
// than the intermediate, so the rule holds however many filters are composed.
func (f Filter) Apply(s *Snapshot) *Snapshot {
	kept := make([]Issue, 0, s.Len())
	for _, issue := range s.issues {
		if f.Matches(issue) {
			kept = append(kept, *issue)
		}
	}

	narrowed := NewSnapshot(kept)
	narrowed.unfiltered = s.origin()

	return narrowed
}

// Describe renders the active criteria for the status bar, empty when nothing
// is set.
func (f Filter) Describe() string {
	var parts []string
	if text := strings.TrimSpace(f.Text); text != "" {
		parts = append(parts, fmt.Sprintf("%q", text))
	}
	if f.Status != "" {
		parts = append(parts, string(f.Status))
	}
	if len(f.Labels) > 0 {
		parts = append(parts, strings.Join(f.Labels, "+"))
	}
	if f.HideClosed {
		// "hide closed", not "open only": KeyMap.HideClosed's own label (keys.go).
		parts = append(parts, "hide closed")
	}
	if f.ShowTombstones {
		parts = append(parts, "+tombstones")
	}

	return strings.Join(parts, " · ")
}

// matchesText reports whether the free-text term appears in the issue.
//
// Substring rather than fuzzy matching: a filter box should be predictable,
// and fuzzy ranking over several hundred issues produces an order the user
// cannot explain or reproduce.
func (f Filter) matchesText(issue *Issue) bool {
	term := strings.ToLower(strings.TrimSpace(f.Text))
	if term == "" {
		return true
	}
	if strings.Contains(strings.ToLower(issue.ID), term) ||
		strings.Contains(strings.ToLower(issue.Title), term) {
		return true
	}
	for _, label := range issue.Labels {
		if strings.Contains(strings.ToLower(label), term) {
			return true
		}
	}

	// A pre-rename id is what a reader has in hand from a commit message or a
	// stale note; it should find the issue under the id it carries now.
	for _, former := range issue.FormerIDs {
		if strings.Contains(strings.ToLower(former), term) {
			return true
		}
	}

	return false
}

// hasAllLabels reports whether the issue carries every required label.
func hasAllLabels(issue *Issue, required []string) bool {
	for _, want := range required {
		found := false
		for _, have := range issue.Labels {
			if strings.EqualFold(have, want) {
				found = true

				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
