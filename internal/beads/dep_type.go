package beads

// DepType is the kind of a dependency edge.
type DepType string

// The dependency types br defines.
const (
	DepBlocks            DepType = "blocks"
	DepParentChild       DepType = "parent-child"
	DepRelated           DepType = "related"
	DepDiscoveredFrom    DepType = "discovered-from"
	DepConditionalBlocks DepType = "conditional-blocks"
	DepWaitsFor          DepType = "waits-for"
)

// Blocks reports whether an edge of this type can block the issue it is
// stored on.
//
// parent-child is deliberately excluded. br's idx_dependencies_blocking index
// lists it, which invites the opposite conclusion, but the query that actually
// computes blockers (src/storage/sqlite.rs:7175) filters on
// ('blocks', 'conditional-blocks', 'waits-for') only. Including parent-child
// would mark every child of an open epic as blocked.
func (d DepType) Blocks() bool {
	return d == DepBlocks || d == DepConditionalBlocks || d == DepWaitsFor
}

// Dependency is one edge. The row is stored on the dependent issue: a row
// {IssueID: A, DependsOnID: B} means A depends on B. For parent-child the row
// lives on the child and points at the parent.
type Dependency struct {
	IssueID     string  `json:"issue_id"`
	DependsOnID string  `json:"depends_on_id"`
	Type        DepType `json:"type"`
}
