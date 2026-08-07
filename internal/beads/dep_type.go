package beads

// DepType is the kind of a dependency edge.
type DepType string

// The dependency types br defines. br 1.4.0 validates this field against
// exactly this list — `br dep add A B -t zzz` prints it — so an unrecognised
// value can only reach bv through hand-edited JSONL.
const (
	DepBlocks            DepType = "blocks"
	DepParentChild       DepType = "parent-child"
	DepRelated           DepType = "related"
	DepDiscoveredFrom    DepType = "discovered-from"
	DepConditionalBlocks DepType = "conditional-blocks"
	DepWaitsFor          DepType = "waits-for"
	DepRepliesTo         DepType = "replies-to"
	DepRelatesTo         DepType = "relates-to"
	DepDuplicates        DepType = "duplicates"
	DepSupersedes        DepType = "supersedes"
	DepCausedBy          DepType = "caused-by"
)

// Blocks reports whether an edge of this type can block the issue it is
// stored on.
//
// parent-child is deliberately excluded. br's idx_dependencies_blocking index
// lists it, which invites the opposite conclusion, but parent-child does not
// block: including it would mark every child of an open epic as blocked.
//
// The set is unchanged in br 1.4.0, and it is checkable without reading br's
// source: give ten issues one edge each of a distinct type and run
// `br blocked`. Only the blocks, conditional-blocks and waits-for sources are
// reported. That recipe is recorded instead of a line number in another
// project's source, which a reader here cannot re-check and which has already
// had one release to rot in.
func (d DepType) Blocks() bool {
	return d == DepBlocks || d == DepConditionalBlocks || d == DepWaitsFor
}

// IsRelation reports whether an edge of this type connects two issues without
// blocking or nesting them.
//
// These are the edges the dependency view's "related" column is built from.
// Unknown types are deliberately not relations: bv renders rather than
// validates, but an edge whose meaning is unknown has neither a direction to
// preserve nor a label to render, so it stays unclassified rather than being
// guessed at.
//
// Written as an ||-chain rather than a switch on purpose, matching Blocks()
// above and depsview's relationWords: golangci-lint's exhaustive rule fires on
// a switch over DepType unless every one of the eleven types is listed, and
// listing the four that fall through then trips revive for identical
// branches. A future "cleanup" back into a switch will fail the gate for
// exactly this reason.
func (d DepType) IsRelation() bool {
	return d == DepRelated || d == DepDiscoveredFrom || d == DepRelatesTo ||
		d == DepDuplicates || d == DepSupersedes || d == DepCausedBy ||
		d == DepRepliesTo
}

// Dependency is one edge. The row is stored on the dependent issue: a row
// {IssueID: A, DependsOnID: B} means A depends on B. For parent-child the row
// lives on the child and points at the parent.
type Dependency struct {
	IssueID     string  `json:"issue_id"`
	DependsOnID string  `json:"depends_on_id"`
	Type        DepType `json:"type"`
}
