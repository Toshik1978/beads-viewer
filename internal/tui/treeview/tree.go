// Package treeview turns a beads.Snapshot into a node tree, filters it, and
// flattens the visible portion into rows for a tree-shaped view. It is pure
// tree construction: no rendering and no bubbletea live here.
package treeview

import "github.com/Toshik1978/beads-viewer/internal/beads"

// Node is one issue's place in the tree.
//
// MatchesFilter records whether this node matched the last Retain call on its
// own — not whether it survived, since an ancestor kept only to reach a
// matching descendant survives too. Build never sets it; Retain is the only
// writer.
type Node struct {
	Issue         *beads.Issue
	Depth         int
	Children      []*Node
	Expanded      bool
	MatchesFilter bool
}

// Build constructs the node tree from a snapshot's parent-child edges.
//
// A path-local visited set bounds the recursion. Hand-edited JSONL can express
// a → b → a, and an unguarded walk would recurse until the stack gives out.
func Build(snap *beads.Snapshot) []*Node {
	var roots []*Node
	for _, issue := range snap.Roots() {
		if node := buildNode(snap, issue, 0, map[string]bool{}); node != nil {
			roots = append(roots, node)
		}
	}

	return roots
}

// Retain prunes the tree to the nodes matching f and the ancestors needed to
// reach them, setting MatchesFilter on each survivor.
//
// A zero filter is not the identity: beads.Filter.Matches hides tombstones
// unless ShowTombstones is set, so Retain(roots, Filter{}) still drops every
// tombstone leaf (and any tombstone subtree with no live descendant) even
// though every live issue is kept and marked matched. Do not special-case
// f.IsZero() to skip Retain — that would let deletion markers appear on
// screen whenever no filter is set.
// It mutates the nodes in place, which is safe only because Build returns a
// fresh tree on every call; a caller that reuses a built tree across frames
// must copy before calling Retain again.
func Retain(roots []*Node, f beads.Filter) []*Node {
	var kept []*Node
	for _, root := range roots {
		if n := retainNode(root, f); n != nil {
			kept = append(kept, n)
		}
	}

	return kept
}

// FindNode searches the tree for the node with the given issue id.
func FindNode(roots []*Node, id string) (*Node, bool) {
	for _, root := range roots {
		if root.Issue.ID == id {
			return root, true
		}
		if found, ok := FindNode(root.Children, id); ok {
			return found, true
		}
	}

	return nil, false
}

// buildNode constructs one node and its subtree, refusing to revisit an
// issue already on the current path.
func buildNode(snap *beads.Snapshot, issue *beads.Issue, depth int, path map[string]bool) *Node {
	if path[issue.ID] {
		return nil
	}
	path[issue.ID] = true
	defer delete(path, issue.ID)

	// MatchesFilter is deliberately left false here. Build knows nothing about
	// any filter; Retain is the only writer of that field.
	node := &Node{Issue: issue, Depth: depth, Expanded: depth == 0}
	for _, kid := range snap.Children(issue.ID) {
		if child := buildNode(snap, kid, depth+1, path); child != nil {
			node.Children = append(node.Children, child)
		}
	}

	return node
}

// retainNode applies Retain's bottom-up rule to one node: keep it if it
// matches f itself, or if any descendant survives the same rule.
func retainNode(n *Node, f beads.Filter) *Node {
	var kids []*Node
	for _, kid := range n.Children {
		if k := retainNode(kid, f); k != nil {
			kids = append(kids, k)
		}
	}

	matched := f.Matches(n.Issue)
	if !matched && len(kids) == 0 {
		return nil
	}
	n.Children = kids
	n.MatchesFilter = matched

	return n
}
