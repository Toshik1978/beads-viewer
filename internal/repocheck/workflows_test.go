package repocheck_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/repocheck"
)

// workflowsTestSuite enforces the one CI invariant that is otherwise only a
// comment: ci.yml skips a list of prose files, and docs.yml runs the licensing
// sweep on exactly that list. A path in one and not the other is a path
// nothing checks, and the failure is silent — CI stays green while a rule
// stops being enforced. That is precisely the shape of defect this repository
// treats as worth a test rather than a warning in a comment.
type workflowsTestSuite struct {
	suite.Suite

	root string
}

func (s *workflowsTestSuite) SetupSuite() {
	root, err := repocheck.RepoRoot(s.T().Context())
	s.Require().NoError(err)
	s.root = root
}

// TestSkippedPathsAreExactlyTheOnesDocsChecks is the complement assertion.
func (s *workflowsTestSuite) TestSkippedPathsAreExactlyTheOnesDocsChecks() {
	ignored := s.triggerPaths("ci.yml", "paths-ignore")
	checked := s.triggerPaths("docs.yml", "paths")

	s.Require().NotEmpty(ignored, "ci.yml declares no paths-ignore")
	s.ElementsMatch(ignored, checked,
		"ci.yml's paths-ignore and docs.yml's paths must be complements — "+
			"a path in one and not the other is a path nothing checks")
}

// TestChangelogIsNeverSkipped pins the one file that must stay outside both
// lists. ci.yml's release-config job reads CHANGELOG.md, so ignoring a change
// to it would skip the job that validates the release notes; and docs.yml
// listing it would run the sweep in place of that job rather than beside it.
func (s *workflowsTestSuite) TestChangelogIsNeverSkipped() {
	s.NotContains(s.triggerPaths("ci.yml", "paths-ignore"), "CHANGELOG.md")
	s.NotContains(s.triggerPaths("docs.yml", "paths"), "CHANGELOG.md")
}

// TestCommitLintHasNoPathFilter guards a rule that looks like an oversight and
// is easy to "tidy up" into a regression. A docs-only change still carries a
// commit subject, and .cliff.toml builds the CHANGELOG from those subjects, so
// a path filter here would let a malformed subject reach main and vanish from
// the release notes.
func (s *workflowsTestSuite) TestCommitLintHasNoPathFilter() {
	for _, event := range []string{"push", "pull_request"} {
		for _, key := range []string{"paths", "paths-ignore"} {
			s.Empty(s.paths("commit-lint.yml", event, key),
				"commit-lint.yml must never filter by path")
		}
	}
}

// TestEveryActionUsesAMovingMajorTag pins the assumption .github/dependabot.yml
// is built on: its ignore rule silences minor and patch updates because every
// action here is pinned to an alias like `@v7`. An exact `x.y.z` pin would
// need those updates, and would then never receive them.
func (s *workflowsTestSuite) TestEveryActionUsesAMovingMajorTag() {
	dir := filepath.Join(s.root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	s.Require().NoError(err)

	seen := 0
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		s.Require().NoError(err)

		var doc map[string]any
		s.Require().NoError(yaml.Unmarshal(data, &doc), entry.Name())

		for _, job := range asMap(doc["jobs"]) {
			for _, step := range asSlice(asMap(job)["steps"]) {
				uses, ok := asMap(step)["uses"].(string)
				if !ok {
					continue
				}
				seen++
				s.Regexp(`@v[0-9]+$`, uses,
					entry.Name()+": pin actions to a moving major tag, or "+
						"revisit the ignore rule in .github/dependabot.yml")
			}
		}
	}
	s.Positive(seen, "no `uses:` steps found — the walk is broken, not clean")
}

// TestCoverageFloorsAgree pins the number to one value across the two places
// that enforce it. ci.yml's coverage gate and Taskfile.yml's coverage target
// each carry their own MIN_COVERAGE, and each carries a comment saying to
// move both together — which is an instruction to a reader who happens to
// read it, not a check. Drifting apart is silent and asymmetric: a local
// `task coverage` that passes below CI's floor sends a red build to a
// reviewer, and one above it lets CI accept work the author's own gate
// rejected.
func (s *workflowsTestSuite) TestCoverageFloorsAgree() {
	inCI := s.workflowCoverageFloors()
	local := s.taskfileCoverageFloor()

	s.Require().NotEmpty(inCI,
		"no MIN_COVERAGE found in ci.yml — the walk is broken, not the workflow clean")
	s.Require().NotEmpty(local, "no MIN_COVERAGE found in Taskfile.yml's coverage task")

	for _, floor := range inCI {
		s.Equal(local, floor,
			"ci.yml and Taskfile.yml must enforce the same coverage floor")
	}
}

// workflowCoverageFloors returns every MIN_COVERAGE any step in ci.yml sets.
// All of them rather than the first: two steps disagreeing with each other is
// the same defect as a step disagreeing with the Taskfile, and taking the
// first would hide it.
func (s *workflowsTestSuite) workflowCoverageFloors() []string {
	doc := s.decode(filepath.Join(s.root, ".github", "workflows", "ci.yml"))

	var floors []string
	for _, job := range asMap(doc["jobs"]) {
		for _, step := range asSlice(asMap(job)["steps"]) {
			if floor, ok := asMap(asMap(step)["env"])["MIN_COVERAGE"]; ok {
				floors = append(floors, fmt.Sprint(floor))
			}
		}
	}

	return floors
}

// taskfileCoverageFloor returns the coverage task's own MIN_COVERAGE.
//
// Compared as text, because the two files spell the same floor differently —
// ci.yml quotes it into a shell env var, the Taskfile leaves it a YAML
// integer — and it is the number they enforce that has to match, not the type
// the decoder happened to infer.
func (s *workflowsTestSuite) taskfileCoverageFloor() string {
	doc := s.decode(filepath.Join(s.root, "Taskfile.yml"))

	floor, ok := asMap(asMap(asMap(asMap(doc["tasks"])["coverage"])["vars"]))["MIN_COVERAGE"]
	if !ok {
		return ""
	}

	return fmt.Sprint(floor)
}

// triggerPaths returns key's value under both push and pull_request, asserting
// the two agree. They always should: a filter that differs between the two
// events means a PR and its own merge to main run different checks.
func (s *workflowsTestSuite) triggerPaths(file, key string) []string {
	push := s.paths(file, "push", key)
	pull := s.paths(file, "pull_request", key)
	s.ElementsMatch(push, pull,
		file+": push and pull_request must filter on the same "+key)

	return push
}

func (s *workflowsTestSuite) paths(file, event, key string) []string {
	doc := s.decode(filepath.Join(s.root, ".github", "workflows", file))

	// `on` survives as the string "on" through this decoder; a YAML 1.1 reader
	// would hand back the boolean true instead, so this lookup is worth a
	// second glance if the parser is ever swapped.
	var out []string
	for _, raw := range asSlice(asMap(asMap(doc["on"])[event])[key]) {
		if path, ok := raw.(string); ok {
			out = append(out, path)
		}
	}
	slices.Sort(out)

	return out
}

// decode reads one YAML file into an untyped tree. Untyped because these
// tests read a handful of keys out of files whose full schemas belong to
// GitHub and Task, and modelling either would be a maintenance burden for no
// extra assertion.
func (s *workflowsTestSuite) decode(path string) map[string]any {
	data, err := os.ReadFile(path)
	s.Require().NoError(err)

	var doc map[string]any
	s.Require().NoError(yaml.Unmarshal(data, &doc), path)

	return doc
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)

	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)

	return s
}
