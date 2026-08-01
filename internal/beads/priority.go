package beads

// Priority runs 0 (critical) to 4 (backlog), matching br's encoding.
type Priority int

// The priority levels br defines.
const (
	PriorityCritical Priority = 0
	PriorityHigh     Priority = 1
	PriorityMedium   Priority = 2
	PriorityLow      Priority = 3
	PriorityBacklog  Priority = 4
)

// Label renders the priority as P0..P4.
func (p Priority) Label() string {
	return [...]string{"P0", "P1", "P2", "P3", "P4"}[p.Clamp()]
}

// Clamp confines the priority to 0..4. Hand-edited JSONL can carry anything,
// and every colour and label table in the UI is indexed by this value.
func (p Priority) Clamp() Priority {
	switch {
	case p < PriorityCritical:
		return PriorityCritical
	case p > PriorityBacklog:
		return PriorityBacklog
	default:
		return p
	}
}
