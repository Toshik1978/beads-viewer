package beads

import "strings"

// IssueType classifies an issue. Open for the same reason as Status.
type IssueType string

// The issue types br defines.
const (
	TypeTask     IssueType = "task"
	TypeBug      IssueType = "bug"
	TypeFeature  IssueType = "feature"
	TypeEpic     IssueType = "epic"
	TypeChore    IssueType = "chore"
	TypeDocs     IssueType = "docs"
	TypeQuestion IssueType = "question"
)

// IsKnown reports whether the type is one br defines.
func (t IssueType) IsKnown() bool {
	switch t {
	case TypeTask, TypeBug, TypeFeature, TypeEpic, TypeChore, TypeDocs, TypeQuestion:
		return true
	default:
		return false
	}
}

// Display returns the type capitalised for the UI, leaving unknown values
// untouched so a custom type reads the way its author spelled it — the same
// rule Status.Display follows.
//
// The IsKnown guard is load-bearing, not stylistic. Capitalising by byte
// (`s[:1]`) is only safe because every known type is ASCII; applied to a
// non-ASCII custom type it splits the leading rune and emits invalid UTF-8:
// "任务" becomes "�\xbb\xbb务". That corrupts the rendered row and gives
// the display-width truncation in Tasks 4.1 and 5.2 undefined bytes to measure.
func (t IssueType) Display() string {
	if !t.IsKnown() {
		return sanitizeDisplay(string(t))
	}

	return strings.ToUpper(string(t)[:1]) + string(t)[1:]
}
