package uitext_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/Toshik1978/beads-viewer/internal/tui/uitext"
)

// TestUitext is this package's single entry point; Truncate's own behaviour
// is exercised where it was originally proven correct, in
// internal/tui/listview's suite — moving the implementation here must not
// discard that coverage, so listview_test.go now calls uitext.Truncate
// directly rather than duplicating the table here. Sanitize had no direct
// coverage before the move (only indirect, through listview's delegate), so
// it gets its own suite below.
func TestUitext(t *testing.T) {
	suite.Run(t, new(sanitizeTestSuite))
}

type sanitizeTestSuite struct {
	suite.Suite
}

func (s *sanitizeTestSuite) TestDropsControlCharactersButKeepsPrintableBytes() {
	got := uitext.Sanitize("red\x1b[41malert\ttab\rcr")

	s.Equal("red[41malerttabcr", got)
	s.NotContains(got, "\x1b")
	s.NotContains(got, "\t")
	s.NotContains(got, "\r")
}

func (s *sanitizeTestSuite) TestPreservesMultibyteRunes() {
	//nolint:gosmopolitan // CJK width fixture, not locale text
	const title = "日本語のタイトル"

	s.Equal(title, uitext.Sanitize(title))
}

func (s *sanitizeTestSuite) TestEmptyStringStaysEmpty() {
	s.Empty(uitext.Sanitize(""))
}
