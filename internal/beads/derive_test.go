package beads_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

type deriveTestSuite struct {
	suite.Suite

	snap *beads.Snapshot
}

func (s *deriveTestSuite) SetupSuite() {
	issues, err := beads.LoadIssues(filepath.Join("testdata", "derive.jsonl"))
	s.Require().NoError(err)
	s.snap = beads.NewSnapshot(issues)
}

func (s *deriveTestSuite) TestIsBlocked() {
	cases := []struct {
		id   string
		want bool
		why  string
	}{
		{"d-open", false, "no dependencies at all"},
		{"d-blocked-by-open", true, "blocks dependency on a live issue"},
		{"d-blocked-by-closed", false, "a closed blocker is satisfied"},
		{"d-blocked-by-tomb", false, "a tombstoned blocker is satisfied"},
		{"d-dangling", true, "a missing target blocks (br LEFT JOINs with OR i.id IS NULL)"},
		{"d-external", false, "external: targets are excluded — br cannot know their status"},
		{"d-template-dep", true, "is_template is no longer read, so its blocker blocks"},
		{"d-child", false, "parent-child never blocks"},
		{"d-related", false, "related never blocks"},
		{"d-conditional", true, "conditional-blocks blocks"},
		{"d-waits", true, "waits-for blocks"},
		{"d-blocked-by-unknown", true, "an unknown status is not terminal, so it still blocks"},
		{"d-epic-open-child", true, "an epic with an open child is blocked — br's :child-open marker"},
		{"d-epic-all-closed", false, "an epic whose children are all closed is not blocked by them"},
		{"d-feature-open-child", false, "the open-child rule is epic-only; a feature parent is unaffected"},
	}
	for _, tc := range cases {
		s.Run(tc.id+": "+tc.why, func() {
			s.Equal(tc.want, s.snap.IsBlocked(tc.id))
		})
	}
}

func (s *deriveTestSuite) TestBlockersNamesTheLiveOnes() {
	blockers := s.snap.Blockers("d-blocked-by-open")
	s.Require().Len(blockers, 1)
	s.Equal("d-open", blockers[0].ID)

	s.Empty(s.snap.Blockers("d-blocked-by-closed"))
	// A dangling target blocks but cannot be named, since it is not in the set.
	s.Empty(s.snap.Blockers("d-dangling"))
	s.True(s.snap.IsBlocked("d-dangling"))

	// The epic open-child rule is close-ordering, not a dependency edge, so it
	// must never surface here even though it makes IsBlocked true.
	s.Empty(s.snap.Blockers("d-epic-open-child"))
	s.True(s.snap.IsBlocked("d-epic-open-child"))
}

func (s *deriveTestSuite) TestIsReady() {
	cases := []struct {
		id   string
		want bool
		why  string
	}{
		{"d-open", true, "open and unblocked"},
		{"d-closed", false, "not open"},
		{"d-inprogress", false, "in_progress means already claimed"},
		{"d-unknown-status", false, "ready is hardcoded to status open"},
		{"d-blocked-by-open", false, "blocked"},
		{"d-blocked-by-closed", true, "blocker is satisfied"},
		{"d-deferred-past", true, "the defer window has elapsed"},
		{"d-deferred-future", false, "still deferred"},
		{"d-pinned", true, "the pinned field is no longer read"},
		{"d-ephemeral", true, "the ephemeral field is no longer read"},
		{"d-wisp-xyz", false, "ids containing -wisp- are excluded"},
		{"d-template", true, "is_template is no longer read, so it is an ordinary issue"},
		{"d-child", true, "a parent does not block its child"},
		{"d-epic-open-child", false, "an epic is not ready while it still has an open child"},
		{"d-epic-open-child-kid", true, "the rule does not propagate downward: the child stays ready"},
		{"d-epic-all-closed", true, "an epic is ready once every child is closed"},
		{"d-feature-open-child", true, "a non-epic parent with an open child stays ready"},
	}
	for _, tc := range cases {
		s.Run(tc.id+": "+tc.why, func() {
			s.Equal(tc.want, s.snap.IsReady(tc.id))
		})
	}
}

func (s *deriveTestSuite) TestCounts() {
	counts := s.snap.Counts()

	s.Equal(s.snap.Len(), counts.Total)
	// Closed counts closed and tombstoned — both are terminal. d-closed,
	// d-tomb and d-epic-all-closed-kid (the epic-rule fixture's closed child).
	s.Equal(3, counts.Closed)
	// Tombstones is the deletion markers inside that 3 (d-tomb alone), broken
	// out so a caller reporting what hide-closed hides can subtract them —
	// they are hidden with the toggle off too.
	s.Equal(1, counts.Tombstones)
	s.Positive(counts.Open)
	s.Positive(counts.Ready)
	s.Positive(counts.Blocked)
	s.LessOrEqual(counts.Ready, counts.Open, "ready is a subset of open")
}

func (s *deriveTestSuite) TestUnknownIDIsNeitherBlockedNorReady() {
	s.False(s.snap.IsBlocked("does-not-exist"))
	s.False(s.snap.IsReady("does-not-exist"))
	s.Empty(s.snap.Blockers("does-not-exist"))
}

func (s *deriveTestSuite) TestBlockingFollowsAFormerID() {
	// br leaves a tombstone at the renamed id. Reading that tombstone as a
	// satisfied blocker made Dependents and Blockers contradict each other:
	// the reverse index said b-new blocks a, the forward derivation said
	// nothing blocked a.
	snap := beads.NewSnapshot([]beads.Issue{
		{ID: "a", Status: beads.StatusOpen, Dependencies: []beads.Dependency{
			{IssueID: "a", DependsOnID: "b-old", Type: beads.DepBlocks},
		}},
		{ID: "b-new", Status: beads.StatusOpen, FormerIDs: []string{"b-old"}},
		{ID: "b-old", Status: beads.StatusTombstone},
	})

	s.True(snap.IsBlocked("a"), "the renamed blocker is still open")
	s.False(snap.IsReady("a"))

	blockers := snap.Blockers("a")
	s.Require().Len(blockers, 1)
	s.Equal("b-new", blockers[0].ID)

	s.Empty(snap.DanglingBlockers("a"), "a resolvable former id is not dangling")

	dependents := snap.Dependents("b-new")
	s.Require().Len(dependents, 1)
	s.Equal("a", dependents[0].ID, "reverse index agrees with the forward derivation")
}

// TestMatchesBrReady is the anchor test: it re-derives readiness over a real
// workspace and compares against br itself. If this drifts, one of the two is
// wrong and it is worth knowing which.
//
// The workspace is this repository's own, found by walking upward with the
// package's own FindWorkspace — which also dogfoods Task 2.3. It is used
// rather than a sibling checkout because a compacted workspace collapses to a
// handful of summary records, and comparing two empty sets is a test that
// cannot fail. Both sides read the same live state at the same moment, so the
// comparison stays valid however the tracker's contents change.
func (s *deriveTestSuite) TestMatchesBrReady() {
	if _, err := exec.LookPath("br"); err != nil {
		s.T().Skip("br not on PATH")
	}
	ws, err := beads.FindWorkspace("")
	if err != nil {
		s.T().Skip("no workspace found above the test directory")
	}

	cmd := exec.CommandContext(context.Background(), "br", "ready", "--json")
	cmd.Dir = filepath.Dir(ws.Dir)
	out, err := cmd.Output()
	if err != nil {
		s.T().Skip("br ready failed in that workspace")
	}

	// br 1.4.0 wraps list output in an envelope: {"issues":[...],"total":n,...}.
	// Decoded strictly rather than tolerating the older bare array, because an
	// anchor test that silently absorbs a contract change has stopped anchoring.
	var reported struct {
		Issues []struct {
			ID string `json:"id"`
		} `json:"issues"`
	}
	s.Require().NoError(json.Unmarshal(out, &reported))

	want := make([]string, len(reported.Issues))
	for i, r := range reported.Issues {
		want[i] = r.ID
	}

	snap, err := beads.LoadSnapshot(ws)
	s.Require().NoError(err)

	var got []string
	for _, issue := range snap.Issues() {
		if snap.IsReady(issue.ID) {
			got = append(got, issue.ID)
		}
	}

	slices.Sort(want)
	slices.Sort(got)
	// ElementsMatch rather than Equal, for the same reason as the blocked
	// anchor below: `want` is non-nil even when empty, `got` is nil until
	// something is appended, and Equal treats those as different. A workspace
	// with nothing ready is an ordinary state, not a failure.
	s.ElementsMatch(want, got, "derivation must agree with br ready")
}

func (s *deriveTestSuite) TestMatchesBrBlocked() {
	if _, err := exec.LookPath("br"); err != nil {
		s.T().Skip("br not on PATH")
	}
	ws, err := beads.FindWorkspace("")
	if err != nil {
		s.T().Skip("no workspace found above the test directory")
	}

	cmd := exec.CommandContext(context.Background(), "br", "blocked", "--json")
	cmd.Dir = filepath.Dir(ws.Dir)
	out, err := cmd.Output()
	if err != nil {
		s.T().Skip("br blocked failed in that workspace")
	}

	// Same envelope as TestMatchesBrReady; see the note there. `br blocked`
	// additionally defaults to limit: 50, unlike `br ready` (limit: 0, i.e.
	// unbounded), so HasMore is decoded and checked below — otherwise a
	// workspace with more than 50 blocked issues would compare one page of
	// `want` against the full derivation in `got` and fail for a pagination
	// artifact that has nothing to do with the derivation being tested.
	var reported struct {
		Issues []struct {
			ID string `json:"id"`
		} `json:"issues"`
		HasMore bool `json:"has_more"`
	}
	s.Require().NoError(json.Unmarshal(out, &reported))
	s.Require().False(reported.HasMore, "br blocked paginated; the comparison below would be against one page")

	want := make([]string, 0, len(reported.Issues))
	for _, r := range reported.Issues {
		want = append(want, r.ID)
	}

	snap, err := beads.LoadSnapshot(ws)
	s.Require().NoError(err)

	var got []string
	for _, issue := range snap.Issues() {
		// br blocked lists live work that is blocked, not every issue with an
		// unsatisfied edge — a closed issue's stale blocker is not interesting.
		// "Live" is every non-terminal status, not StatusOpen alone: br reports
		// an in_progress issue whose blocker is unsatisfied, and filtering on
		// StatusOpen here silently dropped it from the comparison.
		if !issue.Status.IsTerminal() && snap.IsBlocked(issue.ID) {
			got = append(got, issue.ID)
		}
	}

	slices.Sort(want)
	slices.Sort(got)
	// ElementsMatch rather than Equal: `want` is built with make, so it is an
	// empty non-nil slice when br reports nothing, while `got` is only ever
	// appended to and stays nil in that case. Equal distinguishes the two and
	// fails on a workspace where nothing is blocked — which is the normal
	// state of a finished project, and was the state this repository reached
	// the moment its own last issue closed. The values agree; the assertion
	// did not.
	s.ElementsMatch(want, got, "derivation must agree with br blocked")
}
