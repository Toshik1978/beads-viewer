package uitext_test

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	suite.Run(t, new(relativeAgeTestSuite))
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

type relativeAgeTestSuite struct {
	suite.Suite
}

func (s *relativeAgeTestSuite) TestRelativeAge() {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		then time.Time
		want string
	}{
		{"just now", now, "now"},
		{"seconds", now.Add(-30 * time.Second), "now"},
		{"one minute", now.Add(-time.Minute), "1m"},
		{"fifty nine minutes", now.Add(-59 * time.Minute), "59m"},
		{"one hour", now.Add(-time.Hour), "1h"},
		{"twenty three hours", now.Add(-23 * time.Hour), "23h"},
		{"one day", now.Add(-24 * time.Hour), "1d"},
		{"six days", now.Add(-6 * 24 * time.Hour), "6d"},
		{"one week", now.Add(-7 * 24 * time.Hour), "1w"},
		{"one month", now.Add(-30 * 24 * time.Hour), "1mo"},
		{"eleven months", now.Add(-334 * 24 * time.Hour), "11mo"},
		{"one year", now.Add(-365 * 24 * time.Hour), "1y"},
		{"nine years", now.Add(-9 * 365 * 24 * time.Hour), "9y"},
		// A zero UpdatedAt is what a hand-edited or partial record produces.
		// Rendering it as "56y" would be worse than saying nothing.
		{"zero time", time.Time{}, ""},
		// Clock skew between the machine that wrote the record and this one
		// is ordinary, not a corruption; it must not render as a negative.
		{"future", now.Add(time.Hour), "now"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, uitext.RelativeAge(now, tc.then))
		})
	}
}

func (s *relativeAgeTestSuite) TestRelativeAgeNeverExceedsItsColumn() {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	for days := range 4000 {
		age := uitext.RelativeAge(now, now.AddDate(0, 0, -days))
		s.LessOrEqual(ansi.StringWidth(age), uitext.AgeWidth,
			"age %q from %d days exceeds the column", age, days)
	}
}
