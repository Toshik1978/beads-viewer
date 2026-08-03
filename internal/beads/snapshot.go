package beads

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// Snapshot is an immutable view of a workspace at one moment.
//
// The watcher builds a new Snapshot off the UI thread and the app swaps the
// pointer, so nothing is ever mutated in place and no lock is needed between
// the two. Construction clones every field of an Issue through which a
// caller could reach shared memory — its slices and its *time.Time pointers —
// so the snapshot never aliases memory the caller still holds, and every
// accessor that returns a slice clones it again on the way out, so mutating
// what one caller received cannot corrupt what the next caller sees.
type Snapshot struct {
	issues   []*Issue
	byID     map[string]*Issue
	children map[string][]*Issue
	parentOf map[string]string
	roots    []*Issue
	// dependents and relatives are the reverse of what an Issue's own
	// Dependencies express. Every edge row is stored on the *dependent* issue
	// and points at its target, so "what does this issue block" and "what is
	// related to this issue" are answerable only by scanning every issue —
	// which is exactly what detail.renderBlocks used to do on every frame.
	// Both are keyed by the depended-on id.
	dependents map[string][]*Issue
	relatives  map[string][]*Issue
}

// NewSnapshot indexes a decoded issue set.
//
// The single pass builds every index the UI needs, so no view re-scans the
// whole set on each frame.
//
// A duplicate id is not data loss: every record stays in Issues(), so Len()
// still matches the input and nothing silently vanishes. ByID needs one
// deterministic answer, though, so the index resolves to the record that
// appears last in the input — issues.jsonl is append-only, so a later line
// is the more recent write.
func NewSnapshot(issues []Issue) *Snapshot {
	snap := &Snapshot{
		issues:     make([]*Issue, 0, len(issues)),
		byID:       make(map[string]*Issue, len(issues)),
		children:   make(map[string][]*Issue),
		parentOf:   make(map[string]string, len(issues)),
		dependents: make(map[string][]*Issue),
		relatives:  make(map[string][]*Issue),
	}

	stored := make([]Issue, len(issues))
	for i := range issues {
		stored[i] = cloneIssue(issues[i])
	}

	for i := range stored {
		issue := &stored[i]
		snap.issues = append(snap.issues, issue)
		snap.byID[issue.ID] = issue
	}

	sortIssues(snap.issues)
	snap.indexHierarchy()

	return snap
}

// LoadSnapshot reads and indexes a workspace.
//
// A workspace whose issues.jsonl does not exist yields an empty snapshot
// rather than an error: br init creates .beads/ before anything is written
// into it, and that should open as an empty viewer.
func LoadSnapshot(ws Workspace) (*Snapshot, error) {
	issues, err := LoadIssues(ws.IssuesPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return NewSnapshot(nil), nil
		}

		return nil, fmt.Errorf("load snapshot: %w", err)
	}

	return NewSnapshot(issues), nil
}

// Issues returns every issue in the canonical order.
func (s *Snapshot) Issues() []*Issue { return slices.Clone(s.issues) }

// Len returns the issue count.
func (s *Snapshot) Len() int { return len(s.issues) }

// Children returns an issue's children in canonical order.
func (s *Snapshot) Children(id string) []*Issue { return slices.Clone(s.children[id]) }

// ByID returns the issue this snapshot resolves id to, and whether there is
// one. Duplicate ids resolve to the record that appears last in the input,
// per NewSnapshot's own policy — issues.jsonl is append-only, so a later line
// is the more recent write.
//
// It exists because a caller cannot reproduce that resolution from Issues():
// that slice is sorted, so the input order the policy is defined in terms of
// is no longer observable. Scanning it and taking the last match agrees only
// when the duplicate records happen to tie on every sort key, which nothing
// guarantees — two writes of one issue routinely differ in priority.
func (s *Snapshot) ByID(id string) (*Issue, bool) {
	issue, ok := s.byID[id]

	return issue, ok
}

// Parent returns an issue's parent, if it declares one that exists.
func (s *Snapshot) Parent(id string) (*Issue, bool) {
	parentID, ok := s.parentOf[id]
	if !ok {
		return nil, false
	}
	parent, ok := s.byID[parentID]

	return parent, ok
}

// Roots returns issues with no parent present in this snapshot.
//
// An issue whose declared parent is absent counts as a root. br permits a
// dependency on an id that no longer exists, and dropping such an issue would
// silently hide live work.
func (s *Snapshot) Roots() []*Issue { return slices.Clone(s.roots) }

// Dependents returns every issue declaring a blocking edge on id — the reverse
// of Blockers, which reports what blocks a given issue.
//
// The index is keyed by id, so it shares Parent's limitation with duplicate ids
// (see NewSnapshot): two records answering to one id have their inbound edges
// conflated here. That is the same compromise every id-keyed index on this type
// makes, and issues.jsonl being append-only makes it rare.
func (s *Snapshot) Dependents(id string) []*Issue { return slices.Clone(s.dependents[id]) }

// RelatedTo returns every issue joined to id by a related or discovered-from
// edge, in either direction, with no issue appearing twice and id itself never
// appearing.
//
// Both directions, because the edge's direction is a fact about which record
// holds the row rather than about which issue is the useful context: a reader
// looking at the issue a spike was discovered from wants to see the spike, and
// so does a reader looking at the spike.
func (s *Snapshot) RelatedTo(id string) []*Issue {
	return slices.Clone(s.relatives[id])
}

// indexHierarchy fills children, parentOf and roots from parent-child edges.
//
// Each record's parent-child membership is resolved from its own
// Dependencies only, keyed by the record's identity rather than its id. Two
// records can share an id (see NewSnapshot's duplicate-id policy), and if the
// lookup were keyed by id, one record's declared edge would leak onto its
// same-id sibling — a child with no parent of its own could be excluded from
// Roots() merely because another record with the same id has a parent. That
// is the same silent-subtree-loss the missing-parent case guards against, so
// each *Issue is judged strictly on its own edges.
func (s *Snapshot) indexHierarchy() {
	ownParent := make(map[*Issue]string, len(s.issues))

	for _, issue := range s.issues {
		for _, dep := range issue.Dependencies {
			s.indexEdge(issue, dep, ownParent)
		}
	}

	for _, issue := range s.issues {
		parentID, hasParent := ownParent[issue]
		if !hasParent {
			s.roots = append(s.roots, issue)

			continue
		}

		// Parent(id) is keyed by id, so it can only report one answer per id;
		// make it agree with ByID's own duplicate-id policy by recording the
		// edge only for the record ByID actually resolves to.
		if issue == s.byID[issue.ID] {
			s.parentOf[issue.ID] = parentID
		}
	}

	// Both kinds of edge can be declared between the same pair, and the
	// both-directions rule above can also file the same issue twice into
	// relatives. A single issue can likewise declare two different
	// blocking-type edges (blocks and waits-for, say) to the same target,
	// filing it twice into dependents. Compact both once here rather than in
	// the accessors, which the dependency view calls on every frame.
	for id, issues := range s.relatives {
		s.relatives[id] = dedupeIssues(issues)
	}
	for id, issues := range s.dependents {
		s.dependents[id] = dedupeIssues(issues)
	}
}

// indexEdge files one dependency row into whichever reverse index it belongs
// to. Split out of indexHierarchy's loop so that function stays inside the
// statement and complexity limits as the edge kinds grow.
//
// Self-edges are dropped before dispatch, for every branch including
// parent-child: A depends-on A is expressible in hand-edited JSONL, and bv
// renders rather than validates, so it must be tolerated. For the
// blocking/relatives branches, keeping it would list an issue among its own
// blockers or relatives — a rendering bug, not fidelity to the data. For
// parent-child specifically the stakes are higher than cosmetic: without this
// guard, an issue that declares itself its own parent would occupy its own
// slot in ownParent, so it would never hit indexHierarchy's "no parent" case
// and be added to Roots() — yet it would also never appear in any other
// issue's Children(), since the only edge pointing at it is its own. That
// issue would sit in Issues() but be unreachable from any Roots()->Children()
// walk: present in the data, invisible in the tree. Dropping the self-edge
// instead makes it surface as its own root, which is what "render rather
// than validate" requires for malformed data — shown, not hidden.
func (s *Snapshot) indexEdge(issue *Issue, dep Dependency, ownParent map[*Issue]string) {
	if dep.DependsOnID == issue.ID {
		return
	}

	switch {
	case dep.Type == DepParentChild:
		s.indexParentEdge(issue, dep, ownParent)
	case dep.Type.Blocks():
		s.dependents[dep.DependsOnID] = append(s.dependents[dep.DependsOnID], issue)
	case dep.Type == DepRelated, dep.Type == DepDiscoveredFrom:
		// Both directions. The edge is directional as data, but a reader
		// asking "what is related to this" wants the other end regardless of
		// which record happens to hold the row.
		s.relatives[dep.DependsOnID] = append(s.relatives[dep.DependsOnID], issue)
		if target, exists := s.byID[dep.DependsOnID]; exists {
			s.relatives[issue.ID] = append(s.relatives[issue.ID], target)
		}
	}
}

// indexParentEdge records one parent-child row, preserving indexHierarchy's
// original rules: a parent absent from this snapshot is ignored, and the first
// declared parent wins because br's model is a tree and a second edge on the
// same record is malformed data — picking one deterministically beats
// rendering the issue twice.
func (s *Snapshot) indexParentEdge(issue *Issue, dep Dependency, ownParent map[*Issue]string) {
	if _, exists := s.byID[dep.DependsOnID]; !exists {
		return
	}
	if _, seen := ownParent[issue]; seen {
		return
	}
	ownParent[issue] = dep.DependsOnID
	s.children[dep.DependsOnID] = append(s.children[dep.DependsOnID], issue)
}

// dedupeIssues removes repeated *Issue pointers while preserving order.
// Identity, not id: two records can share an id, and both are real issues that
// should each appear.
func dedupeIssues(issues []*Issue) []*Issue {
	seen := make(map[*Issue]struct{}, len(issues))
	out := issues[:0]
	for _, issue := range issues {
		if _, dup := seen[issue]; dup {
			continue
		}
		seen[issue] = struct{}{}
		out = append(out, issue)
	}

	return out
}

// cloneIssue copies an Issue and every field through which a caller could
// reach into the snapshot's memory, so the snapshot never aliases anything
// the caller still holds: Labels, Dependencies and Comments are slices that a
// shallow struct copy would leave pointing at the caller's backing arrays,
// and ClosedAt/DueAt/DeferUntil/DeletedAt are pointers a shallow copy would
// leave pointing at the caller's time.Time values. A later append, in-place
// edit, or dereferencing assignment on the caller's side would otherwise leak
// straight through into what is meant to be an immutable snapshot.
func cloneIssue(issue Issue) Issue {
	issue.Labels = slices.Clone(issue.Labels)
	issue.Dependencies = slices.Clone(issue.Dependencies)
	issue.Comments = slices.Clone(issue.Comments)
	issue.ClosedAt = clonePtr(issue.ClosedAt)
	issue.DueAt = clonePtr(issue.DueAt)
	issue.DeferUntil = clonePtr(issue.DeferUntil)
	issue.DeletedAt = clonePtr(issue.DeletedAt)

	return issue
}

// clonePtr copies the value a pointer addresses into a new allocation, so the
// snapshot's copy and the caller's original no longer share memory. A nil
// pointer stays nil.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p

	return &v
}

// sortIssues orders in place: priority ascending, then newest first, then id.
//
// The id tiebreak is what makes the order total. Without it two issues created
// in the same second swap places between runs, which makes golden renderings
// flap for reasons unrelated to the code under test.
func sortIssues(issues []*Issue) {
	slices.SortStableFunc(issues, func(a, b *Issue) int {
		if c := int(a.Priority.Clamp()) - int(b.Priority.Clamp()); c != 0 {
			return c
		}
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}
