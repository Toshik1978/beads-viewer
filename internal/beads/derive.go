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
}

// IsBlocked reports whether an open dependency prevents work on the issue.
//
// The rule mirrors br's blocker query (src/storage/sqlite.rs:7175) exactly,
// including two details that are easy to get wrong: parent-child edges do not
// block, and a dependency on an id that is absent from the snapshot does
// block — br's LEFT JOIN keeps those rows via `OR i.id IS NULL`, on the
// principle that an unresolvable blocker is not a satisfied one. A third
// rule, blockedByOpenChild below, adds a close-ordering check specific to
// epics: an open, non-template child counts as blocking one too.
func (s *Snapshot) IsBlocked(id string) bool {
	issue, ok := s.byID[id]
	if !ok {
		return false
	}

	return slices.ContainsFunc(issue.Dependencies, s.blocks) || s.blockedByOpenChild(issue)
}

// Blockers returns the live blocking issues present in this snapshot.
//
// A dangling blocker makes IsBlocked true but cannot appear here, since there
// is no issue to return. Callers rendering "blocked by …" should fall back to
// IsBlocked rather than assuming a non-empty result.
func (s *Snapshot) Blockers(id string) []*Issue {
	issue, ok := s.byID[id]
	if !ok {
		return nil
	}

	var blockers []*Issue
	for _, dep := range issue.Dependencies {
		if !s.blocks(dep) {
			continue
		}
		if target, exists := s.byID[dep.DependsOnID]; exists {
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
	issue, ok := s.byID[id]
	if !ok {
		return false
	}

	switch {
	case issue.Status != StatusOpen,
		issue.Pinned,
		issue.Ephemeral,
		issue.IsTemplate,
		strings.Contains(issue.ID, wispMarker),
		issue.DeferUntil != nil && issue.DeferUntil.After(time.Now()):
		return false
	default:
		return !s.IsBlocked(id)
	}
}

// Counts tallies the snapshot for the status bar.
func (s *Snapshot) Counts() Counts {
	counts := Counts{Total: len(s.issues)}

	for _, issue := range s.issues {
		switch {
		case issue.Status.IsTerminal():
			counts.Closed++
		default:
			counts.Open++
			if s.IsBlocked(issue.ID) {
				counts.Blocked++
			}
			if s.IsReady(issue.ID) {
				counts.Ready++
			}
		}
	}

	return counts
}

// blockedByOpenChild reports whether an epic is withheld purely because work
// under it is still open.
//
// This mirrors br's `:child-open` cache marker (src/storage/sqlite.rs, an
// epic-scoped query beside the direct blocker query at :7175): a
// close-ordering constraint — an epic should not be closed while a child
// remains open — not a dependency-graph prerequisite. It is therefore kept
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
	if issue.IssueType != TypeEpic {
		return false
	}

	return slices.ContainsFunc(s.children[issue.ID], func(child *Issue) bool {
		return !child.Status.IsTerminal() && !child.IsTemplate
	})
}

// blocks reports whether one dependency edge currently blocks its holder.
func (s *Snapshot) blocks(dep Dependency) bool {
	if !dep.Type.Blocks() {
		return false
	}
	if strings.HasPrefix(dep.DependsOnID, externalPrefix) {
		return false
	}

	target, exists := s.byID[dep.DependsOnID]
	if !exists {
		return true
	}

	return !target.IsTemplate && !target.Status.IsTerminal()
}
