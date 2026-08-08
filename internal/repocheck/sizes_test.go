package repocheck_test

// This file enforces the size tripwires CLAUDE.md argues for. They were prose
// alone until now, and prose is checked only when someone happens to count:
// internal/tui/app.go sat 26 lines over the per-file cap through six releases,
// and was found while budgeting an unrelated epic rather than by anything that
// was watching. A tripwire nothing measures is a note, not a limit.
//
// The numbers live here rather than in CLAUDE.md. CLAUDE.md keeps the
// reasoning — why each cap exists, and the record of every time one moved —
// which is the part no test can hold and the part worth reading before
// raising one.

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/repocheck"
)

// The caps, in lines. maxFileLines is the general one; maxMainLines is the
// tighter cap the composition root carries because a main that grows is a
// main that has started making decisions; maxTotalLines is the whole-codebase
// figure, a proxy for the 100-field Model this project was written to avoid.
const (
	maxFileLines  = 500
	maxMainLines  = 150
	maxTotalLines = 9000
)

// mainPath is the one file with a cap of its own.
const mainPath = "cmd/bv/main.go"

// sizesTestSuite measures every git-tracked non-test Go file once, in
// SetupSuite, and lets each test read the same measurements. Tracked files
// rather than a filesystem walk, for the reason repocheck.TrackedFiles gives
// itself: a walk would sweep untracked scratch directories and descend into
// build output, and neither is code this project is accountable for.
type sizesTestSuite struct {
	suite.Suite

	// lines maps a repository-relative path to its line count.
	lines map[string]int
}

func (s *sizesTestSuite) SetupSuite() {
	root, err := repocheck.RepoRoot(s.T().Context())
	s.Require().NoError(err)

	tracked, err := repocheck.TrackedFiles(s.T().Context(), root)
	s.Require().NoError(err)

	s.lines = map[string]int{}
	for _, name := range tracked {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(root, name))
		s.Require().NoError(err)

		// Newlines, which is what `wc -l` counts and therefore what every
		// figure recorded in CLAUDE.md was measured with. A count that
		// disagreed with the tool the numbers were taken with would police a
		// different quantity than the prose argues about.
		s.lines[name] = bytes.Count(data, []byte("\n"))
	}

	s.Require().NotEmpty(s.lines,
		"no non-test Go files found — the enumeration is broken, not the tree clean")
}

// TestNoSourceFileExceedsTheLineCap is the cap that matters most of the three.
// The others bound the codebase; this one bounds a single reviewer's working
// set, which is the thing that actually degrades when a file absorbs
// responsibilities that were never meant to sit together.
//
// Every file is checked before the test reports, and the paths are walked in
// sorted order, so a failure names all of them in a stable order rather than
// whichever one the map happened to yield first.
func (s *sizesTestSuite) TestNoSourceFileExceedsTheLineCap() {
	for _, name := range slices.Sorted(maps.Keys(s.lines)) {
		s.LessOrEqual(s.lines[name], maxFileLines,
			name+" is over the per-file cap — split it by responsibility rather than raising the cap")
	}
}

// TestMainStaysSmall pins the composition root. It asserts the path is tracked
// before measuring it: a cap on a file that no longer exists passes forever
// while checking nothing, which is the failure mode this whole file exists to
// remove.
func (s *sizesTestSuite) TestMainStaysSmall() {
	count, tracked := s.lines[mainPath]
	s.Require().True(tracked,
		mainPath+" is not a tracked non-test file — the path moved and this test now checks nothing")
	s.LessOrEqual(count, maxMainLines, mainPath+" is over its own cap")
}

// TestNonTestGoStaysUnderTheTotal is the whole-codebase figure. It is the
// loosest of the three and the one most often argued with, so the failure
// message states the overrun rather than only the totals: the decision it
// prompts — split, delete, or raise the cap and record why — depends on how
// far over the tree actually is.
func (s *sizesTestSuite) TestNonTestGoStaysUnderTheTotal() {
	total := 0
	for _, count := range s.lines {
		total += count
	}

	s.LessOrEqual(total, maxTotalLines,
		"non-test Go is %d lines, %d over the cap — see CLAUDE.md's size tripwires before raising it",
		total, total-maxTotalLines)
}
