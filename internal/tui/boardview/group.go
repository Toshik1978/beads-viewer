// Package boardview groups issues into swimlane columns and summarises each
// column (this file), and renders that grouping as a bubbletea view with a
// two-dimensional cursor (boardview.go, card.go). Grouping stays pure and
// import-free of bubbletea; the renderer is what turns it into a View.
package boardview

import (
	"slices"
	"strings"
	"time"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// SwimLane selects how Group partitions a snapshot into columns.
type SwimLane int

// The four swimlane modes. Next cycles through them in this order and wraps
// back to LaneStatus.
const (
	LaneStatus SwimLane = iota
	LanePriority
	LaneAssignee
	LaneType
)

// laneCount is how many modes Next cycles through.
const laneCount = 4

// Column is one board column: a title, the issues placed in it, and a
// summary of them.
type Column struct {
	Title  string
	Issues []*beads.Issue
	Stats  Stats
	// Catchall marks a column that collects everything a mode's other
	// columns don't claim (Unassigned, Other) rather than one built from a
	// distinct value actually present in the data. The two can share a
	// title — an issue genuinely typed "Other" collides with the type
	// catch-all — so the renderer needs this to tell them apart even though
	// Group cannot rename either without breaking the other invariant.
	Catchall bool
}

// Stats summarises a column so the board can show more than a bare count.
type Stats struct {
	Count      int
	Blocked    int
	Ready      int
	OldestDays int
}

// Name renders the mode for the board's swimlane indicator. It never returns
// empty: SwimLane is a closed set of four values, and every one of them must
// have a label, including a value reached through Next's wraparound.
//
// Nothing outside this package's own tests calls Name yet, and the board
// itself renders no active-lane indicator, so Tab currently reshuffles the
// columns with nothing on screen saying why. That is expected, not dead
// code: Task 7.1's status bar is the intended caller, once it exists.
func (l SwimLane) Name() string {
	switch l {
	case LaneStatus:
		return "Status"
	case LanePriority:
		return "Priority"
	case LaneAssignee:
		return "Assignee"
	case LaneType:
		return "Type"
	}

	// Reached only by a SwimLane value outside the four known modes — not
	// producible through Next, but SwimLane is an exported int, so a caller
	// can construct one directly. Falling back rather than panicking keeps
	// Name total the same way Group itself falls back to status grouping.
	return "Status"
}

// Valid reports whether l is one of the four defined swimlane modes.
func (l SwimLane) Valid() bool {
	return l >= LaneStatus && l <= LaneType
}

// Next cycles to the following mode, wrapping back to LaneStatus after
// LaneType.
//
// An out-of-range receiver normalises to LaneStatus before stepping, rather
// than incrementing whatever invalid value it holds: SwimLane(-2).Next()
// would otherwise stay negative (-2+1 = -1), so a corrupted persisted value
// could take more than one Next call to recover. Normalising first means one
// call is always enough, and the model this feeds can never hold an
// out-of-range value past that call.
func (l SwimLane) Next() SwimLane {
	if !l.Valid() {
		return LaneStatus
	}

	return (l + 1) % laneCount
}

// Group assigns every issue in snap to exactly one column for lane.
//
// Grouping is total: a board that silently drops an issue is worse than no
// board, because "no work here" and "the grouping has a gap" render
// identically. Each mode below has an explicit catch-all column rather than a
// switch whose default falls through, and Group's own default — any lane
// value outside the four known modes — falls back to status grouping rather
// than returning nothing. A nil snapshot is treated as empty rather than
// panicking on snap.Issues(), matching the doc comment's claim to totality.
func Group(snap *beads.Snapshot, lane SwimLane) []Column {
	if snap == nil {
		snap = beads.NewSnapshot(nil)
	}

	switch lane {
	case LaneStatus:
		return groupByStatus(snap)
	case LanePriority:
		return groupByPriority(snap)
	case LaneAssignee:
		return groupByAssignee(snap)
	case LaneType:
		return groupByType(snap)
	}

	// Reached only by a SwimLane value outside the four known modes; see
	// Name's identical fallback for why this cannot be a switch default.
	return groupByStatus(snap)
}

// groupByStatus assigns each issue to exactly one status column.
//
// Blocked collects both derived and declared blockedness. Derived: an issue
// whose dependency is still open is blocked whatever its status field
// claims, and the blocker is what a reader needs to act on — this also
// covers an issue that is simultaneously in_progress and derived-blocked,
// which lands here rather than in In Progress. Declared: an issue a user
// hand-set to the literal `blocked` status belongs in the column named
// exactly that, not in Other — Other is for statuses that aren't a form of
// blocked at all. The Other column is not decoration — br's status set is
// open, so deferred, draft, pinned and any project-defined status must land
// somewhere.
func groupByStatus(snap *beads.Snapshot) []Column {
	cols := []Column{
		{Title: "Open"},
		{Title: "In Progress"},
		{Title: "Blocked"},
		{Title: "Closed"},
		{Title: "Other", Catchall: true},
	}

	for _, issue := range snap.Issues() {
		var idx int
		switch {
		case issue.Status.IsTerminal():
			idx = 3
		case snap.IsBlocked(issue.ID):
			idx = 2
		case issue.Status == beads.StatusBlocked:
			idx = 2
		case issue.Status == beads.StatusInProgress:
			idx = 1
		case issue.Status == beads.StatusOpen:
			idx = 0
		default:
			idx = 4
		}
		cols[idx].Issues = append(cols[idx].Issues, issue)
	}

	return withStats(snap, cols)
}

// groupByPriority assigns each issue to its clamped priority column.
//
// The five columns always exist, even against an empty snapshot: the board's
// shape must not jump around as issues are filtered in and out, and Clamp
// already confines a hand-edited priority to 0..4, so every issue has a home.
func groupByPriority(snap *beads.Snapshot) []Column {
	cols := []Column{
		{Title: "P0"}, {Title: "P1"}, {Title: "P2"}, {Title: "P3"}, {Title: "P4"},
	}

	for _, issue := range snap.Issues() {
		idx := int(issue.Priority.Clamp())
		cols[idx].Issues = append(cols[idx].Issues, issue)
	}

	return withStats(snap, cols)
}

// groupByAssignee assigns each issue to a column named after its assignee,
// sorted, with every unassigned issue collected into a trailing Unassigned
// column. Sorting the distinct names first — rather than ranging the set
// directly — is what keeps the column order identical across calls; map
// iteration order is not. A whitespace-only assignee is treated the same as
// an empty one, so a hand-edited " " does not produce a visually untitled
// column of its own.
func groupByAssignee(snap *beads.Snapshot) []Column {
	issues := snap.Issues()

	names := distinctSorted(issues, func(issue *beads.Issue) string { return issue.Assignee })
	index := make(map[string]int, len(names))
	cols := make([]Column, 0, len(names)+1)
	for i, name := range names {
		index[name] = i
		cols = append(cols, Column{Title: name})
	}
	unassigned := len(cols)
	cols = append(cols, Column{Title: "Unassigned", Catchall: true})

	for _, issue := range issues {
		if strings.TrimSpace(issue.Assignee) == "" {
			cols[unassigned].Issues = append(cols[unassigned].Issues, issue)

			continue
		}
		i := index[issue.Assignee]
		cols[i].Issues = append(cols[i].Issues, issue)
	}

	return withStats(snap, cols)
}

// groupByType assigns each issue to a column named after its type's Display
// form, sorted, with every issue lacking a type collected into a trailing
// Other column. Keying by Display rather than the raw string is load-bearing:
// br's type set is open, so a hand-typed "TASK" or "Task" must not open a
// second column next to the canonical "Task" that "task" resolves to — two
// columns with the same header are indistinguishable from a board that
// dropped one issue's type entirely, which is exactly the failure totality
// exists to prevent. An unrecognised type still gets its own column rather
// than being folded into Other — Other exists only for the absence of a
// type, not for an unfamiliar one.
func groupByType(snap *beads.Snapshot) []Column {
	issues := snap.Issues()

	displays := distinctSorted(issues, func(issue *beads.Issue) string { return issue.IssueType.Display() })
	index := make(map[string]int, len(displays))
	cols := make([]Column, 0, len(displays)+1)
	for i, d := range displays {
		index[d] = i
		cols = append(cols, Column{Title: d})
	}
	other := len(cols)
	cols = append(cols, Column{Title: "Other", Catchall: true})

	for _, issue := range issues {
		display := issue.IssueType.Display()
		if strings.TrimSpace(display) == "" {
			cols[other].Issues = append(cols[other].Issues, issue)

			continue
		}
		i := index[display]
		cols[i].Issues = append(cols[i].Issues, issue)
	}

	return withStats(snap, cols)
}

// distinctSorted collects the distinct values key returns across issues,
// sorted, for building a deterministic column set from open-ended data whose
// value set cannot be known in advance. A value that is empty or entirely
// whitespace is excluded — it belongs to the caller's catch-all column, not
// to a column of its own with a blank or invisible header.
func distinctSorted(issues []*beads.Issue, key func(*beads.Issue) string) []string {
	set := make(map[string]struct{})
	for _, issue := range issues {
		if v := key(issue); strings.TrimSpace(v) != "" {
			set[v] = struct{}{}
		}
	}

	values := make([]string, 0, len(set))
	for v := range set {
		values = append(values, v)
	}
	slices.Sort(values)

	return values
}

// withStats fills in each column's Stats from its already-placed issues.
func withStats(snap *beads.Snapshot, cols []Column) []Column {
	for i := range cols {
		cols[i].Stats = columnStats(snap, cols[i].Issues)
	}

	return cols
}

// columnStats summarises one column's issues: how many are derived-blocked,
// how many are ready to start, and the age in days of the oldest.
//
// An issue whose CreatedAt is the zero time.Time (hand-edited JSONL omitting
// the field) is skipped when finding the oldest rather than treated as
// infinitely old: a missing timestamp means unknown age, not the year 1, and
// letting it win would report an OldestDays in the hundreds of thousands.
// OldestDays stays at 0 if no issue in the column has a real timestamp, and
// is floored at 0 for a CreatedAt in the future rather than going negative.
//
// This calls snap.IsBlocked(issue.ID), which resolves through Snapshot's
// byID index to the LAST record sharing that id (see NewSnapshot's
// duplicate-id policy) — so if the column holds more than one same-id
// record, every one of them inherits the last record's blockedness here,
// not its own.
func columnStats(snap *beads.Snapshot, issues []*beads.Issue) Stats {
	stats := Stats{Count: len(issues)}

	var oldest time.Time
	for _, issue := range issues {
		if snap.IsBlocked(issue.ID) {
			stats.Blocked++
		}
		if snap.IsReady(issue.ID) {
			stats.Ready++
		}
		if issue.CreatedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || issue.CreatedAt.Before(oldest) {
			oldest = issue.CreatedAt
		}
	}
	if !oldest.IsZero() {
		stats.OldestDays = max(int(time.Since(oldest).Hours()/24), 0)
	}

	return stats
}
