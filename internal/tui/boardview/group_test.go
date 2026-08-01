package boardview_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/tui/boardview"
)

func TestBoardview(t *testing.T) {
	suite.Run(t, new(groupTestSuite))
	suite.Run(t, new(boardTestSuite))
}

// idsOf collects issue ids in column order, for compact assertions against a
// column's contents.
func idsOf(issues []*beads.Issue) []string {
	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}

	return ids
}

type groupTestSuite struct {
	suite.Suite
}

func (s *groupTestSuite) sample() *beads.Snapshot {
	return beads.NewSnapshot([]beads.Issue{
		{
			ID: "a", Title: "a", Status: beads.StatusOpen, Priority: beads.PriorityHigh,
			IssueType: beads.TypeBug, Assignee: "anton",
		},
		{
			ID: "b", Title: "b", Status: beads.StatusInProgress, Priority: beads.PriorityMedium,
			IssueType: beads.TypeTask, Assignee: "anton",
		},
		{
			ID: "c", Title: "c", Status: beads.StatusClosed, Priority: beads.PriorityLow,
			IssueType: beads.TypeTask,
		},
		{
			ID: "d", Title: "d", Status: beads.StatusOpen, Priority: beads.PriorityMedium,
			IssueType: beads.IssueType("spike"),
			Dependencies: []beads.Dependency{
				{IssueID: "d", DependsOnID: "a", Type: beads.DepBlocks},
			},
		},
		{ID: "e", Title: "e", Status: beads.Status("triaged"), Priority: beads.PriorityBacklog},
		{ID: "f", Title: "f", Status: beads.StatusDeferred, Priority: beads.PriorityMedium},
	})
}

// TestGroupingIsTotal is the load-bearing test: in every mode, every issue
// lands in exactly one column. A board that drops issues cannot be trusted.
func (s *groupTestSuite) TestGroupingIsTotal() {
	snap := s.sample()
	for _, lane := range []boardview.SwimLane{
		boardview.LaneStatus, boardview.LanePriority,
		boardview.LaneAssignee, boardview.LaneType,
	} {
		s.Run(lane.Name(), func() {
			// Keyed by pointer identity, not issue ID: beads.NewSnapshot
			// deliberately tolerates duplicate ids and keeps every record in
			// Issues() and Len() (see Snapshot's doc comment). Keying by ID
			// would make this assertion fail against a dup-id snapshot even
			// when Group has placed every record correctly, because two
			// distinct records would collapse into one map entry.
			placements := map[*beads.Issue]int{}
			for _, col := range boardview.Group(snap, lane) {
				for _, issue := range col.Issues {
					placements[issue]++
				}
			}
			s.Len(placements, snap.Len(), "every issue must be placed")
			for issue, count := range placements {
				s.Equal(1, count, "%s placed %d times", issue.ID, count)
			}
		})
	}
}

// TestGroupingIsTotalWithDuplicateIDs pins the pointer-identity requirement
// directly: a placements map keyed by ID would collapse two distinct
// same-id records into one entry and fail this assertion even though Group
// placed both correctly.
func (s *groupTestSuite) TestGroupingIsTotalWithDuplicateIDs() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "dup", Title: "first", Status: beads.StatusOpen},
		{ID: "dup", Title: "second", Status: beads.StatusClosed},
	})

	for _, lane := range []boardview.SwimLane{
		boardview.LaneStatus, boardview.LanePriority,
		boardview.LaneAssignee, boardview.LaneType,
	} {
		s.Run(lane.Name(), func() {
			placements := map[*beads.Issue]int{}
			for _, col := range boardview.Group(snap, lane) {
				for _, issue := range col.Issues {
					placements[issue]++
				}
			}
			s.Len(placements, snap.Len(), "every issue record must be placed, dup id or not")
		})
	}
}

func (s *groupTestSuite) TestStatusColumns() {
	cols := boardview.Group(s.sample(), boardview.LaneStatus)

	byTitle := map[string][]string{}
	for _, col := range cols {
		for _, issue := range col.Issues {
			byTitle[col.Title] = append(byTitle[col.Title], issue.ID)
		}
	}

	s.Equal([]string{"a"}, byTitle["Open"])
	s.Equal([]string{"b"}, byTitle["In Progress"])
	// d is status open but blocked by open issue a. Derived blockedness wins,
	// because the blocker is the actionable fact.
	s.Equal([]string{"d"}, byTitle["Blocked"])
	s.Equal([]string{"c"}, byTitle["Closed"])
	// A custom status and deferred both land in the catch-all.
	s.ElementsMatch([]string{"e", "f"}, byTitle["Other"])
}

// TestStatusColumnTitleOrder pins the full column sequence. A permutation
// that still contains all five titles (for example: Other, Closed, Blocked,
// In Progress, Open) would satisfy every other assertion in this file.
func (s *groupTestSuite) TestStatusColumnTitleOrder() {
	cols := boardview.Group(s.sample(), boardview.LaneStatus)
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}
	s.Equal([]string{"Open", "In Progress", "Blocked", "Closed", "Other"}, titles)
}

// TestLiteralBlockedStatusLandsInBlocked covers a status field literally set
// to `blocked` with no live dependency. The column means "blocked" in the
// reader's sense, and both derived and declared blockedness belong there —
// Other is for statuses that are not a form of blocked at all.
func (s *groupTestSuite) TestLiteralBlockedStatusLandsInBlocked() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "1", Title: "1", Status: beads.StatusBlocked},
	})

	for _, col := range boardview.Group(snap, boardview.LaneStatus) {
		if col.Title == "Blocked" {
			s.Equal([]string{"1"}, idsOf(col.Issues))

			return
		}
	}
	s.Fail("Blocked column must exist")
}

// TestInProgressAndBlockedLandsInBlocked pins a headline rule of the brief:
// an issue that is both in_progress and derived-blocked goes in Blocked, not
// In Progress, because the blocker is what a reader needs to act on. The
// shared sample fixture's in_progress issue has no dependencies, so nothing
// else in this suite exercises the ordering between the IsBlocked and
// in_progress cases.
func (s *groupTestSuite) TestInProgressAndBlockedLandsInBlocked() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "opener", Title: "opener", Status: beads.StatusOpen},
		{
			ID: "worker", Title: "worker", Status: beads.StatusInProgress,
			Dependencies: []beads.Dependency{
				{IssueID: "worker", DependsOnID: "opener", Type: beads.DepBlocks},
			},
		},
	})

	byTitle := map[string][]string{}
	for _, col := range boardview.Group(snap, boardview.LaneStatus) {
		byTitle[col.Title] = idsOf(col.Issues)
	}
	s.Equal([]string{"worker"}, byTitle["Blocked"])
	s.Empty(byTitle["In Progress"])
}

func (s *groupTestSuite) TestAssigneeColumnsIncludeUnassigned() {
	cols := boardview.Group(s.sample(), boardview.LaneAssignee)

	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}
	s.Contains(titles, "anton")
	s.Contains(titles, "Unassigned")
	s.Equal("Unassigned", titles[len(titles)-1], "unassigned sorts last")
}

// TestAssigneeCatchallDistinguishesFromRealColumnWithSameTitle covers a real
// assignee literally named "Unassigned" colliding in title with the
// catch-all Group always appends. Group does not rename either side — that
// would just move the collision — so Catchall is what lets a renderer (Task
// 6.2) tell them apart.
func (s *groupTestSuite) TestAssigneeCatchallDistinguishesFromRealColumnWithSameTitle() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "1", Title: "1", Status: beads.StatusOpen, Assignee: "Unassigned"},
		{ID: "2", Title: "2", Status: beads.StatusOpen},
	})

	cols := boardview.Group(snap, boardview.LaneAssignee)
	s.Require().Len(cols, 2)

	var named, catchall *boardview.Column
	for i := range cols {
		if cols[i].Catchall {
			catchall = &cols[i]
		} else {
			named = &cols[i]
		}
	}
	s.Require().NotNil(named)
	s.Require().NotNil(catchall)
	s.Equal("Unassigned", named.Title)
	s.Equal("Unassigned", catchall.Title)
	s.Equal([]string{"1"}, idsOf(named.Issues))
	s.Equal([]string{"2"}, idsOf(catchall.Issues))
}

// TestWhitespaceOnlyValuesFoldIntoCatchall covers a hand-edited " " assignee
// or type, which must not open a visually blank column of its own.
func (s *groupTestSuite) TestWhitespaceOnlyValuesFoldIntoCatchall() {
	s.Run("Assignee", func() {
		snap := beads.NewSnapshot([]beads.Issue{
			{ID: "1", Title: "1", Status: beads.StatusOpen, Assignee: "   "},
		})
		cols := boardview.Group(snap, boardview.LaneAssignee)
		s.Require().Len(cols, 1, "a whitespace-only assignee must not open its own column")
		s.Equal("Unassigned", cols[0].Title)
		s.True(cols[0].Catchall)
		s.Equal([]string{"1"}, idsOf(cols[0].Issues))
	})

	s.Run("Type", func() {
		snap := beads.NewSnapshot([]beads.Issue{
			{ID: "1", Title: "1", Status: beads.StatusOpen, IssueType: beads.IssueType("   ")},
		})
		cols := boardview.Group(snap, boardview.LaneType)
		s.Require().Len(cols, 1, "a whitespace-only type must not open its own column")
		s.Equal("Other", cols[0].Title)
		s.True(cols[0].Catchall)
		s.Equal([]string{"1"}, idsOf(cols[0].Issues))
	})
}

func (s *groupTestSuite) TestPriorityColumnsAreAlwaysAllFive() {
	// Priority columns are fixed so the board's shape does not jump around as
	// issues are filtered.
	cols := boardview.Group(beads.NewSnapshot(nil), boardview.LanePriority)
	s.Len(cols, 5)
	s.Equal("P0", cols[0].Title)
	s.Equal("P4", cols[4].Title)
}

// TestPriorityColumnsPlaceByIssue pins placement to each issue's actual
// priority rather than only counting columns:
// `idx := 4 - int(issue.Priority.Clamp())` also produces five populated
// columns and would pass TestPriorityColumnsAreAlwaysAllFive.
func (s *groupTestSuite) TestPriorityColumnsPlaceByIssue() {
	cols := boardview.Group(s.sample(), boardview.LanePriority)
	s.Require().Len(cols, 5)

	s.Equal([]string{"a"}, idsOf(cols[1].Issues), "PriorityHigh issue a must be in cols[1] (P1)")
	s.ElementsMatch([]string{"b", "d", "f"}, idsOf(cols[2].Issues), "PriorityMedium issues in cols[2] (P2)")
	s.Equal([]string{"c"}, idsOf(cols[3].Issues), "PriorityLow issue c must be in cols[3] (P3)")
	s.Equal([]string{"e"}, idsOf(cols[4].Issues), "PriorityBacklog issue e must be in cols[4] (P4)")
}

func (s *groupTestSuite) TestTypeColumnsCoverUnknownTypes() {
	cols := boardview.Group(s.sample(), boardview.LaneType)

	found := false
	for _, col := range cols {
		for _, issue := range col.Issues {
			if issue.ID == "d" {
				found = true
			}
		}
	}
	s.True(found, "an issue with a custom type must still appear")
}

// TestTypeColumnTitleOrder pins the full type-lane column sequence,
// including that the trailing Other catch-all sorts last even though "spike"
// would sort after it alphabetically among lowercase strings.
func (s *groupTestSuite) TestTypeColumnTitleOrder() {
	cols := boardview.Group(s.sample(), boardview.LaneType)
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.Title
	}
	// The bug and task types come from known values, capitalised by Display;
	// "spike" is unknown so Display returns it verbatim; Other is the
	// trailing catch-all for e and f, which declare no type at all.
	s.Equal([]string{"Bug", "Task", "spike", "Other"}, titles)
	s.Require().Len(cols, 4)
	s.False(cols[0].Catchall)
	s.False(cols[1].Catchall)
	s.False(cols[2].Catchall)
	s.True(cols[3].Catchall)
}

// TestTypeColumnsMergeDisplayCaseVariants covers I2: "task" is a known type,
// so Display capitalises it to "Task"; "Task" is already that shape; "TASK"
// is unknown, so Display returns it verbatim. Keying columns by the raw
// string instead of Display would open two columns both headed "Task" —
// indistinguishable to a reader, and the exact failure totality exists to
// rule out.
func (s *groupTestSuite) TestTypeColumnsMergeDisplayCaseVariants() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "1", Title: "1", Status: beads.StatusOpen, IssueType: beads.IssueType("task")},
		{ID: "2", Title: "2", Status: beads.StatusOpen, IssueType: beads.IssueType("Task")},
		{ID: "3", Title: "3", Status: beads.StatusOpen, IssueType: beads.IssueType("TASK")},
	})

	cols := boardview.Group(snap, boardview.LaneType)

	byTitle := map[string][]string{}
	for _, col := range cols {
		byTitle[col.Title] = idsOf(col.Issues)
	}
	s.ElementsMatch([]string{"1", "2"}, byTitle["Task"], "known-type case variants must merge into one column")
	s.ElementsMatch([]string{"3"}, byTitle["TASK"], "an unknown-cased type keeps its own column")

	seen := map[string]bool{}
	for _, col := range cols {
		s.False(seen[col.Title], "duplicate column title %q", col.Title)
		seen[col.Title] = true
	}
}

// TestDistinctColumnsHaveNoDuplicateTitles pins that the distinct-value set
// backing Assignee and Type columns is actually a set: collecting values into
// a slice without deduplicating first would open one column per issue
// sharing an assignee rather than one column per distinct assignee.
func (s *groupTestSuite) TestDistinctColumnsHaveNoDuplicateTitles() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "1", Title: "1", Status: beads.StatusOpen, Assignee: "zoe"},
		{ID: "2", Title: "2", Status: beads.StatusOpen, Assignee: "zoe"},
		{ID: "3", Title: "3", Status: beads.StatusOpen, Assignee: "zoe"},
	})

	cols := boardview.Group(snap, boardview.LaneAssignee)
	s.Require().Len(cols, 2, "one column for zoe, one trailing Unassigned catch-all")
	s.Equal("zoe", cols[0].Title)
	s.ElementsMatch([]string{"1", "2", "3"}, idsOf(cols[0].Issues))
}

func (s *groupTestSuite) TestStats() {
	cols := boardview.Group(s.sample(), boardview.LaneStatus)

	for _, col := range cols {
		s.Equal(len(col.Issues), col.Stats.Count)
		s.LessOrEqual(col.Stats.Blocked, col.Stats.Count)
		s.LessOrEqual(col.Stats.Ready, col.Stats.Count)
		s.GreaterOrEqual(col.Stats.OldestDays, 0)
	}
}

// AMENDMENT — TestStats above only checks bounds, and every bound it checks is
// satisfied by Stats{} zero values. A Group that never populated Blocked,
// Ready or OldestDays at all would pass it. These pin the actual numbers
// against the fixture, so the fields have to be computed.
func (s *groupTestSuite) TestStatsCountBlockedAndReady() {
	byTitle := map[string]boardview.Stats{}
	for _, col := range boardview.Group(s.sample(), boardview.LaneStatus) {
		byTitle[col.Title] = col.Stats
	}

	// d is the only derived-blocked issue in the fixture, and it is the sole
	// occupant of the Blocked column.
	s.Equal(1, byTitle["Blocked"].Count)
	s.Equal(1, byTitle["Blocked"].Blocked)
	s.Equal(0, byTitle["Blocked"].Ready, "a blocked issue is not ready")

	// a is open with no unmet dependency, so it is ready.
	s.Equal(1, byTitle["Open"].Count)
	s.Equal(1, byTitle["Open"].Ready)
	s.Equal(0, byTitle["Open"].Blocked)
}

// TestStatsOldestDaysIsMeasured pins OldestDays against a known timestamp.
// `GreaterOrEqual(…, 0)` above is true of the zero value, so on its own it
// proves the field exists rather than that it is computed.
func (s *groupTestSuite) TestStatsOldestDaysIsMeasured() {
	old := time.Now().AddDate(0, 0, -30)
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "old", Title: "old", Status: beads.StatusOpen, CreatedAt: old},
	})

	found := false
	for _, col := range boardview.Group(snap, boardview.LaneStatus) {
		if col.Title == "Open" {
			found = true
			s.InDelta(30, col.Stats.OldestDays, 1,
				"OldestDays must reflect the oldest issue's age")
		}
	}
	// Without this, the assertion above is vacuously green if "Open" ever
	// stopped being a column title.
	s.True(found, "Open column must exist")
}

// TestOldestDaysSkipsZeroTimestamps covers I3: a missing CreatedAt (the zero
// time.Time, from hand-edited JSONL that omits the field) means unknown age,
// not "the oldest possible issue". Testing oldest.IsZero() alone as the "not
// yet set" sentinel — without first skipping zero CreatedAt values — lets a
// genuinely zero-valued issue win against a real 10-day-old one and report
// OldestDays in the hundreds of thousands.
func (s *groupTestSuite) TestOldestDaysSkipsZeroTimestamps() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "zero", Title: "zero", Status: beads.StatusOpen},
		{ID: "ten", Title: "ten", Status: beads.StatusOpen, CreatedAt: time.Now().AddDate(0, 0, -10)},
	})

	for _, col := range boardview.Group(snap, boardview.LaneStatus) {
		if col.Title == "Open" {
			s.InDelta(10, col.Stats.OldestDays, 1)

			return
		}
	}
	s.Fail("Open column must exist")
}

// TestOldestDaysClampsFutureCreatedAt covers I4: a CreatedAt in the future
// must floor at 0 rather than go negative, which 6.2 would otherwise render
// as e.g. "-4d".
func (s *groupTestSuite) TestOldestDaysClampsFutureCreatedAt() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "future", Title: "future", Status: beads.StatusOpen, CreatedAt: time.Now().AddDate(0, 0, 5)},
	})

	for _, col := range boardview.Group(snap, boardview.LaneStatus) {
		if col.Title == "Open" {
			s.Equal(0, col.Stats.OldestDays, "a future CreatedAt must clamp to 0, not go negative")

			return
		}
	}
	s.Fail("Open column must exist")
}

func (s *groupTestSuite) TestSwimLaneCycles() {
	lane := boardview.LaneStatus
	seen := map[boardview.SwimLane]bool{}
	for range 4 {
		seen[lane] = true
		lane = lane.Next()
	}
	s.Len(seen, 4, "Next must visit all four modes")
	s.Equal(boardview.LaneStatus, lane, "and return to the start")
}

// TestSwimLaneNextOrder pins the cycle direction. TestSwimLaneCycles is
// satisfied by any 4-cycle, including a reversed one
// (Status -> Type -> Assignee -> Priority -> Status), which would leave the
// key Task 6.2 binds to Next() traveling backwards from what a user expects.
func (s *groupTestSuite) TestSwimLaneNextOrder() {
	s.Equal(boardview.LanePriority, boardview.LaneStatus.Next())
	s.Equal(boardview.LaneAssignee, boardview.LanePriority.Next())
	s.Equal(boardview.LaneType, boardview.LaneAssignee.Next())
	s.Equal(boardview.LaneStatus, boardview.LaneType.Next())
}

// TestSwimLaneNextNormalisesOutOfRange covers the lane-normalisation request:
// SwimLane(-2).Next() must not stay negative. Model persistence (Task 6.2)
// restores a SwimLane from disk, so a corrupt stored value must recover in
// exactly one Next call.
func (s *groupTestSuite) TestSwimLaneNextNormalisesOutOfRange() {
	s.False(boardview.SwimLane(-2).Valid())
	s.False(boardview.SwimLane(4).Valid())
	s.True(boardview.LaneStatus.Valid())
	s.True(boardview.LaneType.Valid())

	s.Equal(boardview.LaneStatus, boardview.SwimLane(-2).Next(),
		"an out-of-range value must recover in one Next call, not stay negative")
	s.Equal(boardview.LaneStatus, boardview.SwimLane(99).Next())
}

func (s *groupTestSuite) TestSwimLaneNamesAreDistinctAndNonEmpty() {
	seen := map[string]bool{}
	for _, lane := range []boardview.SwimLane{
		boardview.LaneStatus, boardview.LanePriority,
		boardview.LaneAssignee, boardview.LaneType,
	} {
		name := lane.Name()
		s.NotEmpty(name, "lane %d must have a name", lane)
		s.False(seen[name], "name %q reused by more than one lane", name)
		seen[name] = true
	}
}

func (s *groupTestSuite) TestEmptySnapshot() {
	for _, lane := range []boardview.SwimLane{
		boardview.LaneStatus, boardview.LanePriority,
		boardview.LaneAssignee, boardview.LaneType,
	} {
		s.Run(lane.Name(), func() {
			s.NotPanics(func() { _ = boardview.Group(beads.NewSnapshot(nil), lane) })
		})
	}
}

// TestNilSnapshotDoesNotPanic covers M1: Group(nil, ...) is not reachable
// today, but listview.SetSnapshot nil-guards its own snapshot and app.go
// assigns msg.Snapshot without a nil check, so a nil *beads.Snapshot
// reaching Group is not purely hypothetical.
func (s *groupTestSuite) TestNilSnapshotDoesNotPanic() {
	for _, lane := range []boardview.SwimLane{
		boardview.LaneStatus, boardview.LanePriority,
		boardview.LaneAssignee, boardview.LaneType,
	} {
		s.Run(lane.Name(), func() {
			var got []boardview.Column
			s.NotPanics(func() { got = boardview.Group(nil, lane) })
			s.NotNil(got)
		})
	}
}

// TestColumnOrderIsDeterministic uses a fixture with three distinct
// assignees and three distinct types, each declared out of sorted order
// (zoe, adam, mel), so a missing or reversed sort is visible in the exact
// title sequence rather than only in cross-call equality — a fixture with
// one distinct value cannot move regardless of whether sorting happens.
func (s *groupTestSuite) TestColumnOrderIsDeterministic() {
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "z", Title: "z", Status: beads.StatusOpen, Assignee: "zoe", IssueType: beads.TypeBug},
		{ID: "y", Title: "y", Status: beads.StatusOpen, Assignee: "adam", IssueType: beads.TypeChore},
		{ID: "x", Title: "x", Status: beads.StatusOpen, Assignee: "mel", IssueType: beads.TypeDocs},
	})

	s.Run("Assignee", func() {
		want := []string{"adam", "mel", "zoe", "Unassigned"}
		for range 20 {
			cols := boardview.Group(snap, boardview.LaneAssignee)
			titles := make([]string, len(cols))
			for i, c := range cols {
				titles[i] = c.Title
			}
			s.Equal(want, titles, "column order must not track declaration or map iteration order")
		}
	})

	s.Run("Type", func() {
		want := []string{"Bug", "Chore", "Docs", "Other"}
		for range 20 {
			cols := boardview.Group(snap, boardview.LaneType)
			titles := make([]string, len(cols))
			for i, c := range cols {
				titles[i] = c.Title
			}
			s.Equal(want, titles, "column order must not track declaration or map iteration order")
		}
	})
}
