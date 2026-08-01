package beads_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/beads"
)

type jsonlTestSuite struct {
	suite.Suite
}

func (s *jsonlTestSuite) TestDecodeSample() {
	f, err := os.Open(filepath.Join("testdata", "sample.jsonl"))
	s.Require().NoError(err)
	defer f.Close()

	issues, err := beads.DecodeJSONL(f)
	s.Require().NoError(err)
	s.Require().Len(issues, 3)

	s.Equal("bv-1", issues[0].ID)
	s.Equal(beads.StatusOpen, issues[0].Status)
	s.Equal([]string{"alpha"}, issues[0].Labels)

	s.Require().Len(issues[1].Dependencies, 1)
	s.Equal("bv-1", issues[1].Dependencies[0].DependsOnID)
	s.Equal(beads.DepBlocks, issues[1].Dependencies[0].Type)
	s.Require().NotNil(issues[1].ClosedAt)

	s.Require().Len(issues[2].Comments, 1)
	s.Equal("anton", issues[2].Comments[0].Author)
}

func (s *jsonlTestSuite) TestDecodeIsLenient() {
	f, err := os.Open(filepath.Join("testdata", "hostile.jsonl"))
	s.Require().NoError(err)
	defer f.Close()

	issues, err := beads.DecodeJSONL(f)
	// The whole point: none of this is an error.
	s.Require().NoError(err)
	s.Require().Len(issues, 5, "the blank line must be skipped, not counted")

	byID := map[string]beads.Issue{}
	for i := range issues {
		byID[issues[i].ID] = issues[i]
	}

	// Unknown enum values survive verbatim.
	s.Equal(beads.Status("triaged"), byID["bv-h1"].Status)
	s.False(byID["bv-h1"].Status.IsKnown())
	s.Equal(beads.IssueType("spike"), byID["bv-h2"].IssueType)

	// An unknown field is ignored, so a future br field cannot break an
	// older bv.
	s.Equal("Unknown field", byID["bv-h3"].Title)

	// Labels are never validated. br rejects this one; bv shows it.
	s.Equal([]string{"needs review!", "ok-label"}, byID["bv-h4"].Labels)

	// A record with only required fields decodes to usable zero values.
	s.Empty(string(byID["bv-h5"].Status))
	s.Empty(byID["bv-h5"].Labels)
}

func (s *jsonlTestSuite) TestDecodeEdgeCaseInputs() {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"empty input", "", 0},
		{"only whitespace", "   \n\n\t\n", 0},
		{"no trailing newline", `{"id":"a","title":"A"}`, 1},
		{"trailing newline", `{"id":"a","title":"A"}` + "\n", 1},
		{"crlf line endings", `{"id":"a","title":"A"}` + "\r\n" + `{"id":"b","title":"B"}` + "\r\n", 2},
		// br writes UTF-8 without a BOM, but a JSONL file that has passed
		// through a Windows editor can acquire one, and it would otherwise
		// make the first record — and only the first — fail to parse.
		{"utf-8 bom", "\ufeff" + `{"id":"a","title":"A"}` + "\n", 1},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			issues, err := beads.DecodeJSONL(strings.NewReader(tc.input))
			s.Require().NoError(err)
			s.Len(issues, tc.want)
		})
	}
}

func (s *jsonlTestSuite) TestDecodeReportsTheFailingLine() {
	input := `{"id":"a","title":"A"}` + "\n" + `{"id":"b",BROKEN}` + "\n"

	_, err := beads.DecodeJSONL(strings.NewReader(input))
	s.Require().Error(err)
	s.Require().ErrorIs(err, beads.ErrMalformed)
	s.Contains(err.Error(), "line 2", "the error must say which line failed")
}

// TestLoadIssuesDecodeErrorOmitsTheWorkspacePath is I3's fix pinned: a
// decode failure's error used to carry the workspace path ("decode <path>:
// %w"), which on a one-line status bar pushed the reason — the only part
// actually worth reading — past whatever width the terminal had. The
// workspace is already implied by the bv instance showing the error.
func (s *jsonlTestSuite) TestLoadIssuesDecodeErrorOmitsTheWorkspacePath() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "issues.jsonl")
	s.Require().NoError(os.WriteFile(path, []byte(`{"id":"a",BROKEN}`+"\n"), 0o600))

	_, err := beads.LoadIssues(path)

	s.Require().Error(err)
	s.NotContains(err.Error(), dir, "the workspace path must not crowd out the reason")
	s.Contains(err.Error(), "malformed jsonl", "the actual decode reason must still show")
}

func (s *jsonlTestSuite) TestDecodeHandlesVeryLongLines() {
	// bufio.Scanner's 64KB default would fail here. A bead whose design field
	// holds a full spec routinely exceeds it — this project's own epic does.
	long := strings.Repeat("x", 512*1024)
	input := `{"id":"a","title":"A","design":"` + long + `"}` + "\n"

	issues, err := beads.DecodeJSONL(strings.NewReader(input))
	s.Require().NoError(err)
	s.Require().Len(issues, 1)
	s.Len(issues[0].Design, 512*1024)
}
