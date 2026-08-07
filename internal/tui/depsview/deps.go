// Package depsview answers, for one issue, what is holding it up and what is
// waiting on it — as four board columns (this file), rendered as a bubbletea
// view with a two-dimensional cursor and a re-rooting history (depsview.go,
// nav.go).
//
// Column construction stays pure and free of bubbletea, mirroring
// boardview/group.go's split from its own renderer.
package depsview

import (
	"github.com/Toshik1978/beads-viewer/internal/beads"
)

// Relation names why an entry appears in the column it is in. It is rendered
// beside the card, because the four sources of the blocked-by column answer
// genuinely different questions and a reader has to be able to tell "this
// dependency is unfinished" from "this blocker no longer exists".
type Relation string

// The reasons an issue can appear in a column. RelationFocus is the empty
// string: the middle column's single card is the subject, not a relation to
// it, so there is nothing to label it with.
const (
	RelationFocus        Relation = ""
	RelationBlocker      Relation = "blocker"
	RelationDangling     Relation = "not in workspace"
	RelationInherited    Relation = "via parent"
	RelationOpenChild    Relation = "open child"
	RelationBlocks       Relation = "blocks"
	RelationRelated      Relation = "related"
	RelationDiscovered   Relation = "discovered from"
	RelationLedTo        Relation = "led to"
	RelationDuplicates   Relation = "duplicates"
	RelationDuplicatedBy Relation = "duplicated by"
	RelationSupersedes   Relation = "supersedes"
	RelationSupersededBy Relation = "superseded by"
	RelationCausedBy     Relation = "caused by"
	RelationCaused       Relation = "caused"
	RelationRepliesTo    Relation = "replies to"
	RelationReply        Relation = "reply"
	// RelationNotInView labels the middle column when the focused id has
	// left the current snapshot — the active filter narrowed it out, or a
	// reload dropped it — but the view is still about it. See focusEntries.
	RelationNotInView Relation = "not in current view"
)

// The four column titles, in render order.
const (
	titleBlockedBy = "blocked by"
	titleFocused   = "focused"
	titleBlocks    = "blocks"
	titleRelated   = "related"
)

// Entry is one card's worth of content.
//
// ID is always set; Issue is nil only for a dangling blocker — an id this
// workspace has no issue for. Snapshot.DanglingBlockers returns raw ids for
// exactly that reason, and dropping them would hide the most confusing kind of
// blocker there is.
type Entry struct {
	Issue    *beads.Issue
	ID       string
	Relation Relation
}

// Column is one of the four columns.
type Column struct {
	Title   string
	Entries []Entry
}

// Columns builds the four columns for focusID.
//
// It always returns exactly four, in a fixed order, whatever the inputs: a
// board that can silently omit a column renders "nothing here" and "the
// grouping has a gap" identically, which is the same totality rule
// boardview.Group holds itself to. A nil snapshot is treated as empty rather
// than panicking.
func Columns(snap *beads.Snapshot, focusID string) []Column {
	if snap == nil {
		snap = beads.NewSnapshot(nil)
	}

	return []Column{
		{Title: titleBlockedBy, Entries: blockedByEntries(snap, focusID)},
		{Title: titleFocused, Entries: focusEntries(snap, focusID)},
		{Title: titleBlocks, Entries: entriesFor(snap.Dependents(focusID), RelationBlocks)},
		{Title: titleRelated, Entries: relatedEntries(snap, focusID)},
	}
}

// blockedByEntries collects every reason this issue is held up, not only the
// ones Snapshot.Blockers can name.
//
// Blockers reports the issue's own dependency rows, so it is empty for an issue
// blocked through its parent chain and for an epic held by its own open
// children — its doc comment says so directly. Asking each source separately is
// what lets several be true at once and none stand in for another.
func blockedByEntries(snap *beads.Snapshot, focusID string) []Entry {
	entries := entriesFor(snap.Blockers(focusID), RelationBlocker)

	for _, missing := range snap.DanglingBlockers(focusID) {
		entries = append(entries, Entry{ID: missing, Relation: RelationDangling})
	}
	if ancestor, ok := snap.BlockedAncestor(focusID); ok {
		entries = append(entries, Entry{Issue: ancestor, ID: ancestor.ID, Relation: RelationInherited})
	}
	if snap.BlockedByOpenChild(focusID) {
		for _, child := range snap.Children(focusID) {
			if child.Status.IsTerminal() || child.IsTemplate {
				continue
			}
			entries = append(entries, Entry{Issue: child, ID: child.ID, Relation: RelationOpenChild})
		}
	}

	return dedupeEntries(withoutID(entries, focusID))
}

// focusEntries is the middle column: the subject itself, or — when the
// active filter has removed it from this snapshot while the view is still
// about it — a bare tagged id, rendered the same way a dangling blocker is
// (renderEntry's e.Issue == nil path). Without this, "focused (0)" sat
// beside a populated "blocks" or "related" column with nothing on screen
// naming what those columns were even about; the bare id keeps the pane
// honest about its own subject instead of reading as an error.
//
// An empty focusID — nothing has ever been revealed — still renders
// nothing: there is no subject to name yet.
func focusEntries(snap *beads.Snapshot, focusID string) []Entry {
	if focusID == "" {
		return nil
	}
	issue, ok := snap.ByID(focusID)
	if !ok {
		return []Entry{{ID: focusID, Relation: RelationNotInView}}
	}

	return []Entry{{Issue: issue, ID: issue.ID, Relation: RelationFocus}}
}

// relatedEntries tags each of RelatedTo's results with the edge that produced
// it, in the direction this focus sees it. RelatedTo answers which issues are
// connected but not how, and "duplicates" and "caused by" mean very different
// things to a reader.
//
// Direction matters because most of these edges are asymmetric. Labelling both
// ends "supersedes" would state the opposite of the truth on the superseded
// issue — a bug discovered-from has had all along, fixed here by the same rule.
func relatedEntries(snap *beads.Snapshot, focusID string) []Entry {
	entries := make([]Entry, 0, len(snap.RelatedTo(focusID)))
	for _, other := range snap.RelatedTo(focusID) {
		depType, forward := snap.RelationTo(focusID, other.ID)
		entries = append(entries, Entry{
			Issue:    other,
			ID:       other.ID,
			Relation: relationLabel(depType, forward),
		})
	}

	return withoutID(entries, focusID)
}

// relationLabel names one edge from the end that is looking at it.
func relationLabel(depType beads.DepType, forward bool) Relation {
	fwd, rev := relationWords(depType)
	if forward {
		return fwd
	}

	return rev
}

// relationWords returns an edge's forward and reverse labels. Split from
// relationLabel so the direction choice stays one line and this stays a flat
// table; gochecknoglobals rules out expressing it as a package-level map.
//
// Written as an if-chain rather than a switch on purpose. golangci-lint's
// exhaustive rule fires on a switch over beads.DepType unless every one of
// the eleven types is listed, and listing the eight that share the fallback
// then trips revive for identical branches. Task 3 hit the same wall in
// DepType.IsRelation and resolved it the same way.
//
// The fallthrough covers "related", "relates-to" and anything unrecognised:
// all mean only that two issues are connected, which reads the same from
// either end.
func relationWords(depType beads.DepType) (Relation, Relation) {
	if depType == beads.DepDiscoveredFrom {
		return RelationDiscovered, RelationLedTo
	}
	if depType == beads.DepDuplicates {
		return RelationDuplicates, RelationDuplicatedBy
	}
	if depType == beads.DepSupersedes {
		return RelationSupersedes, RelationSupersededBy
	}
	if depType == beads.DepCausedBy {
		return RelationCausedBy, RelationCaused
	}
	if depType == beads.DepRepliesTo {
		return RelationRepliesTo, RelationReply
	}

	return RelationRelated, RelationRelated
}

// entriesFor wraps a plain issue slice as entries under one relation.
func entriesFor(issues []*beads.Issue, relation Relation) []Entry {
	entries := make([]Entry, 0, len(issues))
	for _, issue := range issues {
		entries = append(entries, Entry{Issue: issue, ID: issue.ID, Relation: relation})
	}

	return entries
}

// withoutID drops the focused issue from a column. A self-edge is expressible
// in hand-edited JSONL, and bv renders rather than validates — so it must be
// tolerated, and shown once in the middle column rather than twice.
func withoutID(entries []Entry, id string) []Entry {
	kept := entries[:0]
	for _, e := range entries {
		if e.ID != id {
			kept = append(kept, e)
		}
	}

	return kept
}

// dedupeEntries drops an exact (ID, Relation) repeat while preserving order.
//
// Snapshot.Blockers is not deduped at the source (unlike Snapshot.dependents,
// compacted once at build time — see snapshot.go's indexHierarchy): an issue
// that declares both a "blocks" and a "waits-for" edge to the same target
// gets that target back twice, and blockedByEntries can also produce a
// direct blocker that is separately the BlockedAncestor, filing one id
// twice under RelationBlocker alone. Only the identical (id, relation) pair
// collapses — the same id appearing under two *different* relations (a
// direct blocker that is also inherited through the parent chain) is two
// genuine facts about the issue, and the labels are what tell them apart, so
// that case is left alone.
func dedupeEntries(entries []Entry) []Entry {
	type key struct {
		id       string
		relation Relation
	}

	seen := make(map[key]struct{}, len(entries))
	kept := entries[:0]
	for _, e := range entries {
		k := key{id: e.ID, relation: e.Relation}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		kept = append(kept, e)
	}

	return kept
}
