package beads

import (
	"slices"
	"strings"
	"time"
)

// externalPrefix marks a dependency on an issue in another system. br stores
// these but never treats them as blockers, because it cannot know their state.
const externalPrefix = "external:"

// wispMarker appears in the ids of throwaway issues, which are never ready.
const wispMarker = "-wisp-"

// Counts summarises a snapshot for the status bar.
type Counts struct {
	Total int
	// Open counts every non-terminal status, not only StatusOpen — br stats
	// breaks Open and In Progress out separately, so this is their sum.
	Open    int
	Ready   int
	Blocked int
	Closed  int
	// Tombstones is the deletion markers inside Closed. They are hidden with
	// hide-closed off too, so a caller reporting what it hides subtracts them.
	Tombstones int
}

// IsBlocked reports whether anything prevents work on the issue.
//
// Three rules, each mirroring one br uses. The first is the direct blocker
// rule, which contains a detail that is easy to get wrong and is checkable
// without reading br's source: give an issue a blocks edge on an id absent
// from the workspace and run `br blocked` — it still lists the issue, on the
// principle that an unresolvable blocker is not a satisfied one. The second,
// blockedByOpenChild, is a close-ordering check specific to epics: an open
// child counts as blocking one too.
//
// The third, BlockedAncestor, is why the parent-child edge is at once
// excluded and not. The edge itself never blocks — DepType.Blocks() filters it
// out, so "my parent is unfinished" is not a blocker, and it must not be: that
// is the ordinary condition of every subtask, and the child is usually the
// work that finishes the parent. What does block is inherited: a parent held
// up by a dependency of its own is waiting on something outside the subtree,
// which no progress on the child can resolve, so the wait reaches down to the
// child too, and on down the chain. Only the dependency rule is inherited —
// propagating blockedByOpenChild would mark every child of an open epic as
// blocked by itself, which is the reading DepType.Blocks() already rejects,
// and which br rejects too by stripping that marker before it reaches a child.
//
// The three do not rest on equal evidence, and it is the third that is worth
// distrusting. The first two were established the same reproducible way: run
// `br blocked` against a small workspace built for the purpose and read off
// which issues it reports. The third was not: no such recipe states it, so it
// was inferred black-box, from which issues br reports as blocked across a
// subtree. Its edge cases are therefore this project's own to get right, and
// one of them was wrong for as long as the rule existed — a closed ancestor
// still carrying an unsatisfied blocks edge propagated that block to every
// live descendant beneath it, which over-reported Blocked in the status bar
// and named a finished issue as the cause in the detail pane. blockedAncestor
// checks the ancestor's status now, and inherit_test.go's h-shut-* fixtures
// pin both halves of the correction: a closed ancestor hands nothing down,
// and a closed link mid-chain does not hide a live blocked ancestor above it.
// Every rule here reads the workspace, not the filtered view of it, which is
// what the origin() call at the top of each derivation is for; see Snapshot's
// unfiltered field for why derivation and display part company at exactly this
// line.
func (s *Snapshot) IsBlocked(id string) bool {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return false
	}

	return s.blocked(issue)
}

// Blockers returns the live blocking issues present in this snapshot.
//
// It answers "which dependency row is stopping this issue", so it names only
// what the issue's own edges point at. Two of IsBlocked's three rules
// therefore cannot appear here at all, and a third can be true with nothing
// to return, so a caller rendering "blocked by …" must not read an empty
// result as "not blocked". BlockedAncestor, BlockedByOpenChild and
// DanglingBlockers below exist so such a caller can say which of the three
// applies without re-deriving any of them for itself.
func (s *Snapshot) Blockers(id string) []*Issue {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return nil
	}

	var blockers []*Issue
	for _, dep := range issue.Dependencies {
		if !s.blocks(dep) {
			continue
		}
		// Resolved through canonical, same as s.blocks itself: the row may
		// name an id its target used to carry, and this must name the same
		// issue s.blocks just judged rather than fail to find it at all.
		if target, exists := s.byID[s.canonical(dep.DependsOnID)]; exists {
			blockers = append(blockers, target)
		}
	}

	return blockers
}

// IsReady reports whether the issue is open, unblocked and actionable now.
//
// bv hardcodes the ready status group to {open}. br allows a project to widen
// it via workflow.status_groups.ready in .beads/policy.yaml; bv ignores that
// file, which is a documented divergence rather than an oversight.
func (s *Snapshot) IsReady(id string) bool {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return false
	}

	return s.ready(issue, s.blocked(issue))
}

// BlockedAncestor returns the nearest ancestor whose own dependency is what
// blocks this issue, and whether there is one.
//
// This is the inherited rule of IsBlocked, exported so a caller can name the
// cause. It reports the ancestor that actually holds the unsatisfied edge
// rather than the nearest blocked one, because that is the issue a reader has
// to go and look at: an intermediate parent that is itself only blocked by
// inheritance explains nothing the child does not already know.
func (s *Snapshot) BlockedAncestor(id string) (*Issue, bool) {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return nil, false
	}

	return s.blockedAncestor(issue)
}

// BlockedByOpenChild reports whether the epic close-ordering rule is what
// blocks this issue. See blockedByOpenChild for what that rule is and why it
// is neither a dependency edge nor inherited downward.
func (s *Snapshot) BlockedByOpenChild(id string) bool {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return false
	}

	return s.blockedByOpenChild(issue)
}

// BlockingChildren returns the open children holding this issue back under
// the epic close-ordering rule: exactly the ones that make BlockedByOpenChild
// true, and empty in every case where it is false.
//
// It exists because the predicate on its own cannot be rendered. A caller
// told only that an epic is held by live work beneath it has to go and find
// that work itself, and the obvious way — reading Children off the snapshot
// it was handed — is wrong the moment that snapshot is filtered. The
// predicate answers from the workspace (see origin), while Children belongs
// to whatever the filter left, so a query hiding an epic's only open child
// left the epic correctly marked blocked with nothing on screen naming why.
func (s *Snapshot) BlockingChildren(id string) []*Issue {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return nil
	}

	var holding []*Issue
	for _, child := range s.closeOrderingChildren(issue) {
		if !child.Status.IsTerminal() {
			holding = append(holding, child)
		}
	}

	return holding
}

// DanglingBlockers returns the ids this issue declares a blocking edge on
// that no issue in this snapshot answers to — genuinely missing, not merely
// renamed. An id that resolves through canonical to a live issue is not
// dangling even though the literal id in the row is absent from byID; only an
// id that canonical leaves unresolved (or resolves to a tombstone with no
// successor) still is.
//
// Each dangling id makes IsBlocked true (see there) while contributing
// nothing to Blockers, since there is no issue to return. Returning the raw
// ids is the point: an id is all there is to say about a blocker that is not
// here, and it is what a reader needs in order to go and find out where it
// went.
//
// The filter is s.blocks itself rather than a restatement of its guards. That
// composes exactly, because s.blocks answers true on the absent-target branch
// before it looks at the target's status — so within an edge s.blocks
// accepts, "the target is not in byID" is the whole of what makes it
// dangling. Restating the guards instead would work today and quietly stop
// working the moment a fourth exclusion joins s.blocks: this list would keep
// reporting the edges that rule now excludes, and nothing would fail.
func (s *Snapshot) DanglingBlockers(id string) []string {
	s = s.origin()

	issue, ok := s.byID[id]
	if !ok {
		return nil
	}

	var missing []string
	for _, dep := range issue.Dependencies {
		if _, exists := s.byID[s.canonical(dep.DependsOnID)]; exists || !s.blocks(dep) {
			continue
		}
		missing = append(missing, dep.DependsOnID)
	}

	return missing
}

// Counts tallies the snapshot for the status bar.
//
// The loop reads s.issues — what a filter left to show, which is what the
// totals are about — while the blocked and ready tallies are derived against
// the origin snapshot, which is what those questions are about. See
// Snapshot.unfiltered for why the two part company here.
func (s *Snapshot) Counts() Counts {
	origin := s.origin()
	counts := Counts{Total: len(s.issues)}

	for _, issue := range s.issues {
		switch {
		case issue.Status.IsTerminal():
			counts.Closed++
			if issue.Status == StatusTombstone {
				counts.Tombstones++
			}
		default:
			counts.Open++
			origin.tallyOpen(issue.ID, &counts)
		}
	}

	return counts
}

// origin returns the snapshot every derivation above resolves against: the
// unnarrowed one this was filtered from, or s itself when s is already that.
// It is idempotent by construction — Filter.Apply is the only writer of the
// field and always records the origin rather than its own input — so the
// derivations can reassign their receiver through it once and then read the
// indexes directly, with no second hop to consider.
func (s *Snapshot) origin() *Snapshot {
	if s.unfiltered != nil {
		return s.unfiltered
	}

	return s
}

// blocked is IsBlocked's three rules, applied to a record already resolved
// against the origin snapshot. Named separately so that a caller which needs
// blockedness *and* readiness for the same issue can derive it once and pass
// it on — see tallyOpen — rather than going through IsReady, which derives it
// again.
func (s *Snapshot) blocked(issue *Issue) bool {
	if s.blockedByDependency(issue) || s.blockedByOpenChild(issue) {
		return true
	}
	_, inherited := s.blockedAncestor(issue)

	return inherited
}

// ready is IsReady's rules with blockedness supplied rather than derived.
//
// Taking it as a parameter rather than calling s.blocked keeps the rules in
// one place while letting the caller decide how many times the expensive one
// runs. The parameter is not an optimisation hook a caller may lie through:
// passing anything but s.blocked(issue) reports readiness for an issue that
// does not exist.
func (s *Snapshot) ready(issue *Issue, blocked bool) bool {
	switch {
	case issue.Status != StatusOpen,
		strings.Contains(issue.ID, wispMarker),
		issue.DeferUntil != nil && issue.DeferUntil.After(time.Now()):
		return false
	default:
		return !blocked
	}
}

// tallyOpen adds one open issue to the blocked and ready tallies, deriving
// blockedness once and handing it to the readiness rules.
//
// Counts used to ask IsBlocked and then IsReady, and IsReady ends in a second
// IsBlocked: two index lookups and two walks of the parent chain per open
// issue, on every frame, because the status bar recomputes its counts on each
// render rather than caching them. Measured at roughly 0.4 ms a frame across
// 10,000 issues — not enough to be felt on its own, which is why this stood
// as an accepted duplication for several releases.
//
// What settled it is the other half. Two independent derivations can disagree
// in a way one cannot: Blocked and Ready are disjoint only because IsReady
// happens to end in !IsBlocked, so the moment those two resolve an issue
// differently — a stale index, a divergence in how each finds its record —
// the status bar could report one issue in both tallies and nothing would
// fail. Sharing the derivation makes the two answers describe the same
// record by construction, and the arithmetic guard in
// inherit_test.go's TestCountsDoNotDoubleCount stops being the only thing
// standing between the counts and that.
//
// s is the origin snapshot; the caller resolves it once for the whole loop.
func (s *Snapshot) tallyOpen(id string, counts *Counts) {
	issue, ok := s.byID[id]
	if !ok {
		return
	}

	blocked := s.blocked(issue)
	if blocked {
		counts.Blocked++
	}
	if s.ready(issue, blocked) {
		counts.Ready++
	}
}

// blockedByOpenChild reports whether an epic is withheld purely because work
// under it is still open.
//
// This mirrors br's own close-ordering check: an epic with a live child still
// reports as blocked by `br blocked` even when none of its own dependency
// edges are unsatisfied, which is the observable half of what the
// `:child-open` cache marker names — an epic should not be closed while a
// child remains open, not a dependency-graph prerequisite. It is therefore kept
// out of blocks and Blockers, which answer "what edge is stopping this
// issue," not "should this epic's children finish first" — folding it in
// there would make Blockers list a child as though it were a genuine
// blocker, which br itself does not do. br also does not propagate this
// downward (it strips the mirror-image `:parent-blocked` marker before it
// reaches a child): a child with no blocker of its own stays ready even
// while its parent epic does not, because the child is frequently exactly
// the work that unblocks the epic. Gating on issue_type == epic matches br
// exactly — a plain task or feature with open children is unaffected.
func (s *Snapshot) blockedByOpenChild(issue *Issue) bool {
	return slices.ContainsFunc(s.closeOrderingChildren(issue), func(child *Issue) bool {
		return !child.Status.IsTerminal()
	})
}

// closeOrderingChildren returns the children the close-ordering rule
// considers at all — an epic's own, and none for anything else.
//
// Split from the predicate so BlockingChildren can name the offenders
// without restating which issues are even eligible to have any. It returns
// the stored slice rather than a copy, so the predicate above stays
// allocation-free: Counts runs it for every open issue on every frame.
func (s *Snapshot) closeOrderingChildren(issue *Issue) []*Issue {
	if issue.IssueType != TypeEpic {
		return nil
	}

	return s.children[issue.ID]
}

// blockedByDependency reports whether the issue holds an unsatisfied blocking
// edge of its own. This is the only rule that is inherited downward, so it is
// named separately from IsBlocked rather than inlined there.
func (s *Snapshot) blockedByDependency(issue *Issue) bool {
	return slices.ContainsFunc(issue.Dependencies, s.blocks)
}

// blockedAncestor finds the nearest ancestor whose own unsatisfied dependency
// reaches down to this issue. See IsBlocked for why it is the dependency rule
// alone that travels, and why it travels the whole chain rather than one hop.
//
// A terminal ancestor propagates nothing: a stale blocks edge on a finished
// issue is history, not a live wait, and every sibling rule checks a status
// too. The walk continues past one, so a closed link hides no ancestor above.
// The walk carries a visited set because .beads/issues.jsonl is hand-editable
// and bv renders rather than validates: a parent chain that closes into a
// cycle is malformed data the viewer must still open, and an unguarded walk
// would hang the UI instead of failing a decode. An issue reachable from
// itself is treated as unblocked by this rule — its own edges are already
// covered by blockedByDependency, so the cycle contributes nothing new.
func (s *Snapshot) blockedAncestor(issue *Issue) (*Issue, bool) {
	var visited []string

	current := issue
	for {
		parent, ok := s.Parent(current.ID)
		if !ok || parent.ID == issue.ID || slices.Contains(visited, parent.ID) {
			return nil, false
		}
		if !parent.Status.IsTerminal() && s.blockedByDependency(parent) {
			return parent, true
		}
		visited = append(visited, parent.ID)
		current = parent
	}
}

// blocks reports whether one dependency edge currently blocks its holder.
//
// dep.DependsOnID is resolved through canonical before the byID lookup, after
// the external: check. br's rename leaves a tombstone at the old id, and
// looking that tombstone up directly would read a renamed-but-still-open
// blocker as satisfied — the row still blocks, it just now points at an id
// its target no longer answers to. An external: id is never an id this
// snapshot's issues use, so canonical would leave it unresolved regardless;
// checking the prefix first just avoids the wasted lookup.
func (s *Snapshot) blocks(dep Dependency) bool {
	if !dep.Type.Blocks() {
		return false
	}
	if strings.HasPrefix(dep.DependsOnID, externalPrefix) {
		return false
	}

	target, exists := s.byID[s.canonical(dep.DependsOnID)]
	if !exists {
		return true
	}

	return !target.Status.IsTerminal()
}
