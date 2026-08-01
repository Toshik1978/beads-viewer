// Package beads models the issue records br writes to .beads/issues.jsonl and
// derives the state a viewer needs from them. It imports nothing from the UI
// layer, so every rule here is testable without a terminal.
package beads

import "time"

// Comment is a note attached to an issue.
type Comment struct {
	ID        int       `json:"id"`
	IssueID   string    `json:"issue_id"`
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// Issue is one record from issues.jsonl.
//
// Field names and JSON tags mirror br's serialized set. Optional fields are
// pointers or zero-valued rather than required, because br omits empty ones
// (serde skip_serializing_if), so absence is normal and not an error.
//
// The struct carries 28 fields, above the Global Constraints' 20-field
// tripwire for behavioural god objects; that limit targets state-heavy
// types, and a flat DTO mirroring an external schema is the intended
// exception, so the record is kept whole rather than split.
type Issue struct {
	ID                 string       `json:"id"`
	Title              string       `json:"title"`
	Description        string       `json:"description"`
	Design             string       `json:"design"`
	AcceptanceCriteria string       `json:"acceptance_criteria"`
	Notes              string       `json:"notes"`
	Status             Status       `json:"status"`
	Priority           Priority     `json:"priority"`
	IssueType          IssueType    `json:"issue_type"`
	Assignee           string       `json:"assignee"`
	Owner              string       `json:"owner"`
	EstimatedMinutes   int          `json:"estimated_minutes"`
	CreatedAt          time.Time    `json:"created_at"`
	CreatedBy          string       `json:"created_by"`
	UpdatedAt          time.Time    `json:"updated_at"`
	ClosedAt           *time.Time   `json:"closed_at"`
	CloseReason        string       `json:"close_reason"`
	DueAt              *time.Time   `json:"due_at"`
	DeferUntil         *time.Time   `json:"defer_until"`
	ExternalRef        string       `json:"external_ref"`
	SourceRepo         string       `json:"source_repo"`
	DeletedAt          *time.Time   `json:"deleted_at"`
	Pinned             bool         `json:"pinned"`
	Ephemeral          bool         `json:"ephemeral"`
	IsTemplate         bool         `json:"is_template"`
	Labels             []string     `json:"labels"`
	Dependencies       []Dependency `json:"dependencies"`
	Comments           []Comment    `json:"comments"`
}

// IsTombstone reports whether the issue is a deletion marker rather than live
// work. br records these two ways, and either is authoritative.
func (i Issue) IsTombstone() bool {
	return i.Status == StatusTombstone || i.DeletedAt != nil
}
