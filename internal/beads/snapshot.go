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
	// aliases maps an id an issue used to carry to the id it carries now. br
	// renames an issue when it gains a parent — the old id becomes a tombstone
	// and the survivor records it in former_ids — so stale ids outlive what
	// they named, in commit messages, in prose and in other issues' edges.
	// Kept out of byID so that indexing can normalise edge targets through it
	// deliberately, rather than resolving them by accident.
	aliases map[string]string
	// unfiltered is the snapshot Filter.Apply narrowed this one from, and nil
	// when this snapshot is itself unnarrowed. It is what the derivations in
	// derive.go resolve against, because they answer questions about the
	// workspace rather than about what a filter left on screen: an issue whose
	// only blocker is closed is not blocked, and hide-closed removing that
	// blocker must not make it read as one — blocks() has no way to tell a
	// filtered-out target from a genuinely missing id, and is right to treat a
	// missing one as unresolved. Display stays this snapshot's own job; only
	// derivation delegates. Filter.Apply always points this at the unnarrowed
	// origin rather than at its immediate input, so chained filtering cannot
	// leave a derivation resolving against another filter's leftovers.
	unfiltered *Snapshot
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
		aliases:    make(map[string]string),
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
	snap.indexAliases()
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
// is the more recent write. It also resolves an id the issue used to carry to
// the one it carries now — see canonical — with a live id always winning over
// a historical one.
//
// It exists because a caller cannot reproduce that resolution from Issues():
// that slice is sorted, so the input order the policy is defined in terms of
// is no longer observable. Scanning it and taking the last match agrees only
// when the duplicate records happen to tie on every sort key, which nothing
// guarantees — two writes of one issue routinely differ in priority.
func (s *Snapshot) ByID(id string) (*Issue, bool) {
	issue, ok := s.byID[s.canonical(id)]

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

// RelatedTo returns every issue joined to id by a relation edge — any of the
// seven types IsRelation reports true for — in either direction, with no issue
// appearing twice and id itself never appearing.
//
// Both directions, because the edge's direction is a fact about which record
// holds the row rather than about which issue is the useful context: a reader
// looking at the issue a spike was discovered from wants to see the spike, and
// so does a reader looking at the spike.
func (s *Snapshot) RelatedTo(id string) []*Issue {
	return slices.Clone(s.relatives[id])
}

// RelationTo reports the relation edge between focus and other, and whether
// focus is the end that declares it.
//
// Only relation edges are considered: a blocks or parent-child row between the
// same pair reports nothing, because those are answered by Blockers and
// Children and would otherwise show up twice under a misleading label.
//
// Focus's own claim wins, except when focus only says "related"/"relates-to"
// and the other end says something more specific — then the specific claim
// wins and focus is reported as the receiving end. Two different specific
// claims are not weighed against each other: each end simply reports its own,
// so a pair that claims both "supersedes" and "caused-by" doesn't have one
// side's word silently overridden by the other's.
func (s *Snapshot) RelationTo(focusID, otherID string) (DepType, bool) {
	own := s.relationEdge(s.byID[focusID], otherID)
	theirs := s.relationEdge(s.byID[otherID], focusID)

	switch {
	case ownPrevails(own, theirs):
		return own, true
	case theirs != "":
		return theirs, false
	default:
		return "", false
	}
}

// indexAliases maps every id an issue used to carry to the id it carries now.
//
// A *live* issue still using an id another issue merely used to have wins, and
// no alias is recorded: hiding a real record behind a historical name would be
// worse than not following the rename. A tombstone does not win, because a
// rename is exactly what leaves one at the old id — treating it as a live
// claimant would mean this index never fires for the case it exists for.
//
// Run after sortIssues, so that when two issues claim the same former id the
// winner is the one that sorts first rather than whichever line happened to
// come first in the file.
func (s *Snapshot) indexAliases() {
	for _, issue := range s.issues {
		for _, former := range issue.FormerIDs {
			if existing, exists := s.byID[former]; exists && !existing.IsTombstone() {
				continue
			}
			if _, taken := s.aliases[former]; taken {
				continue
			}
			s.aliases[former] = issue.ID
		}
	}
}

// canonical resolves an id that may be a former one to the id in use now,
// returning it unchanged when it is already current or unknown.
//
// A tombstone at this id is deliberately not treated as current: br's rename
// leaves one behind, so following the alias past it is the entire point. When
// no successor claims the id, the tombstone is still returned — a deleted
// issue that was never renamed stays findable under its own id.
func (s *Snapshot) canonical(id string) string {
	if issue, exists := s.byID[id]; exists && !issue.IsTombstone() {
		return id
	}
	if current, aliased := s.aliases[id]; aliased {
		return current
	}

	return id
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
	// Resolved before the self-edge guard, not after: an issue naming its own
	// former id is naming itself, and the guard below is what keeps that from
	// filing the issue as its own parent. Resolving afterwards would also file
	// children under a historical key that Children() never reads, leaving
	// them in Issues() but unreachable from any Roots()/Children() walk.
	dep.DependsOnID = s.canonical(dep.DependsOnID)

	if dep.DependsOnID == issue.ID {
		return
	}

	switch {
	case dep.Type == DepParentChild:
		s.indexParentEdge(issue, dep, ownParent)
	case dep.Type.Blocks():
		s.dependents[dep.DependsOnID] = append(s.dependents[dep.DependsOnID], issue)
	case dep.Type.IsRelation():
		// Both directions. The edge is directional as data, but a reader
		// asking "what is related to this" wants the other end regardless of
		// which record happens to hold the row. Which *word* names the
		// relation from each end is depsview's problem, not this index's.
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

// relationEdge returns the type of the relation edge issue declares on target,
// or "" when there is none. A nil issue has no edges, which is the not-in-this
// -snapshot case rather than an error.
//
// dep.DependsOnID is resolved through canonical before the comparison, for the
// same reason indexEdge resolves it when building the reverse index: the row
// names whatever id target used to carry, and RelationTo must agree with
// RelatedTo about which pair of issues that row connects.
func (s *Snapshot) relationEdge(issue *Issue, target string) DepType {
	if issue == nil {
		return ""
	}
	for _, dep := range issue.Dependencies {
		if s.canonical(dep.DependsOnID) == target && dep.Type.IsRelation() {
			return dep.Type
		}
	}

	return ""
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

// isGenericRelation reports whether a type is one of the two that mean only
// "these are connected" — the claims any more specific type outranks.
func isGenericRelation(d DepType) bool {
	return d == DepRelated || d == DepRelatesTo
}

// ownPrevails reports whether RelationTo should report own rather than
// theirs. own loses only in the one case where it is outranked outright: own
// is the generic fallback and theirs is a specific claim. Every other
// combination — own specific, or theirs empty, or theirs itself only
// generic — leaves own's own claim standing, including when both ends
// declare different specific types: neither outranks the other, so each end
// is left reporting its own.
func ownPrevails(own, theirs DepType) bool {
	if own == "" {
		return false
	}

	return !isGenericRelation(own) || theirs == "" || isGenericRelation(theirs)
}

// cloneIssue copies an Issue and every field through which a caller could
// reach into the snapshot's memory, so the snapshot never aliases anything the
// caller still holds: Labels, FormerIDs, Dependencies and Comments are slices
// that a shallow struct copy would leave pointing at the caller's backing
// arrays, and ClosedAt/DueAt/DeferUntil/DeletedAt are pointers a shallow copy
// would leave pointing at the caller's time.Time values. A later append,
// in-place edit, or dereferencing assignment on the caller's side would
// otherwise leak straight through into what is meant to be an immutable
// snapshot.
func cloneIssue(issue Issue) Issue {
	issue.Labels = slices.Clone(issue.Labels)
	issue.FormerIDs = slices.Clone(issue.FormerIDs)
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
