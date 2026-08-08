package licensing_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/licensing"
)

func TestLicensing(t *testing.T) {
	suite.Run(t, new(licensingTestSuite))
	suite.Run(t, new(workflowsTestSuite))
	suite.Run(t, new(sizesTestSuite))
}

// The pattern lists are suite fields rather than package-level vars: the
// `gochecknoglobals` linter is NOT relaxed in `_test.go` (only dupl, errcheck,
// funcorder, goconst, gocyclo, gosec, lll, makezero, unparam and unused are),
// so a `var disallowedPatterns = []string{…}` fails the gate Task 1.4 turns on.
type licensingTestSuite struct {
	suite.Suite

	root  string
	files []string

	// upstreamNames name the projects that inspired this one. README.md may
	// contain them — its Acknowledgements section is where that belongs — and
	// so may this test, which has to spell them out in order to search.
	upstreamNames []string
	// externalPaths name a sibling checkout on the author's own machine.
	// Nothing may contain these but this test, README.md included: naming a
	// project is a courtesy, naming a directory on someone's laptop is a leak,
	// and the two were previously exempted together for no better reason than
	// that one file happened to need both.
	externalPaths []string

	// namesExempt and pathsExempt are the two exemption sets, kept apart so
	// that widening one cannot silently widen the other.
	namesExempt map[string]bool
	pathsExempt map[string]bool
}

func (s *licensingTestSuite) SetupSuite() {
	root, err := licensing.RepoRoot(s.T().Context())
	s.Require().NoError(err)
	s.root = root

	files, err := licensing.TrackedFiles(s.T().Context(), root)
	s.Require().NoError(err)
	// Guard every sweep at once. A sweep over zero files passes forever while
	// protecting nothing, and that is the failure mode this package exists to
	// avoid — so the check belongs here, not in one of the sweeps.
	s.Require().NotEmpty(files, "git ls-files returned nothing — wrong root?")
	s.files = files

	s.upstreamNames = []string{
		"beads_rust",
		"BeadsRust",
		"beads_viewer",
		"BeadsViewer",
		"Dicklesworthstone",
	}
	s.externalPaths = []string{
		"/data/projects",
		"../beads",
		// The reference tree actually lives at ../_beads/beads_viewer — a
		// comment naming just "../_beads/" would evade the pattern above.
		"_beads",
	}

	const self = "internal/licensing/licensing_test.go"

	s.namesExempt = map[string]bool{
		"README.md": true,
		self:        true,
	}
	s.pathsExempt = map[string]bool{self: true}
}

// sweep reads every tracked file that is neither exempt nor inside .beads/,
// and reports "<file>: contains <pattern>" for each pattern it contains. Both
// sweeps share it so that adding a third rule costs a call rather than another
// copy of the walk.
func (s *licensingTestSuite) sweep(patterns []string, exempt map[string]bool) []string {
	var violations []string

	for _, rel := range s.files {
		if exempt[rel] || strings.HasPrefix(rel, ".beads/") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.root, rel))
		if err != nil || isBinary(data) {
			continue
		}

		for _, pattern := range patterns {
			if strings.Contains(string(data), pattern) {
				violations = append(violations, rel+": contains "+pattern)
			}
		}
	}

	return violations
}

// TestLicenseIsPlainMIT pins the license in both directions.
//
// The positive half alone would pass on a file that is MIT plus something
// else appended, which is exactly the state this project moved away from: it
// previously carried an OpenAI/Anthropic rider inherited from the viewer that
// inspired it, on the mistaken premise that this code was a derivative work.
// It is not — no upstream file ever entered this repository's history — so the
// rider was dropped and the copyright is the author's alone. The NotContains
// assertions are what stop either one reappearing unnoticed.
func (s *licensingTestSuite) TestLicenseIsPlainMIT() {
	data, err := os.ReadFile(filepath.Join(s.root, "LICENSE"))
	s.Require().NoError(err)
	text := string(data)

	s.Contains(text, "The MIT License (MIT)")
	s.Contains(text, "Copyright (c) 2026 Anton Krivenko")
	// The permission grant itself, so that a file reduced to just a title and
	// a copyright line cannot pass.
	s.Contains(text, "Permission is hereby granted, free of charge",
		"the MIT grant itself must be present, not only the title")

	s.NotContains(text, "ADDITIONAL RIDER",
		"the license is plain MIT; a rider must not be reintroduced")
	s.NotContains(text, "Jeffrey Emanuel",
		"no upstream copyright applies: this is an independent implementation")
}

func (s *licensingTestSuite) TestNoUpstreamNamesOutsideTheAcknowledgement() {
	violations := s.sweep(s.upstreamNames, s.namesExempt)

	s.Empty(violations,
		"name an upstream project only in README.md's Acknowledgements section")
}

// TestNoExternalPathsAnywhere is deliberately stricter than the name sweep:
// README.md is exempt from that one and not from this one. A published
// repository naming a directory on the author's own machine leaks something
// no reader can use and no acknowledgement requires.
func (s *licensingTestSuite) TestNoExternalPathsAnywhere() {
	violations := s.sweep(s.externalPaths, s.pathsExempt)

	s.Empty(violations,
		"describe a sibling checkout's behaviour instead of naming its path")
}

// isBinary reports whether data looks like a binary file, the same way `grep -I`
// decides: a NUL byte in the leading window.
func isBinary(data []byte) bool {
	window := min(len(data), 8000)

	return bytes.IndexByte(data[:window], 0) >= 0
}
