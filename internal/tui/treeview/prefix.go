package treeview

import "strings"

// Guide glyphs. Each is exactly two cells wide, which is what makes a
// prefix's total width scale in fixed steps and every row at a given depth
// line up (pinned by TestPrefixWidthIsUniformPerDepth).
const (
	guideVertical = "│ "
	guideBranch   = "├─"
	guideLast     = "└─"
	guideBlank    = "  "

	markerExpanded  = "▾ "
	markerCollapsed = "▸ "
	markerLeaf      = "  "
)

// Prefix renders the guides preceding a row's title: one 2-cell vertical-bar
// unit per ancestor, this row's own branch connector, and its expansion
// marker.
//
// ancestors carries one entry per ancestor of this node, outermost (the
// root) first, including the immediate parent, so len(ancestors) ==
// node.Depth. A root passes nil. ancestors[i] reports whether the ancestor
// at depth i has a later sibling — and therefore whether its guide must
// continue past this row.
//
// EVERY entry draws a bar, including the immediate parent's own. An earlier
// draft excluded the parent's slot on the theory that "the connector goes
// there instead" — but the connector is an additional unit, not one of the
// ancestor slots, and dropping the parent's entry shifts every bar one level
// up: a node's direct children would then track its own continuation as if
// it were their grandparent's, and the deepest indent level would never draw
// a bar at all. Getting the included range wrong — continuing a bar past a
// subtree that already finished — is the classic way a tree renderer looks
// broken; that is what ancestors[i] == false guards against.
func Prefix(ancestors []bool, isLast, expandable, expanded bool) string {
	var b strings.Builder

	for _, continues := range ancestors {
		if continues {
			b.WriteString(guideVertical)
		} else {
			b.WriteString(guideBlank)
		}
	}

	// A root (depth 0, no ancestors) never draws a connector, even when it
	// is one of several roots in a forest: there is no shared, visible trunk
	// for it to branch from the way a real child branches from its parent's
	// own row. Drawing one only for a non-lone root would also make depth-0
	// rows disagree in width depending on isLast, breaking the very
	// uniform-width invariant TestPrefixWidthIsUniformPerDepth exists to
	// pin — verified by that test failing at depth 0 (2 vs 4 cells) before
	// this guard was narrowed from `len(ancestors) > 0 || !isLast`.
	if len(ancestors) > 0 {
		if isLast {
			b.WriteString(guideLast)
		} else {
			b.WriteString(guideBranch)
		}
	}

	switch {
	case !expandable:
		// A leaf still consumes the marker's width in blanks, or its title
		// would sit two cells left of every sibling that does have one.
		b.WriteString(markerLeaf)
	case expanded:
		b.WriteString(markerExpanded)
	default:
		b.WriteString(markerCollapsed)
	}

	return b.String()
}
