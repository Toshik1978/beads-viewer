// Package theme resolves the colour scheme and exposes the styles the views
// render with.
//
// A Theme is a value, constructed once at startup and passed down. The
// pre-rewrite code instead kept the profile and the preference in package
// variables set from init(), and pinned the global lipgloss renderer — which
// the lint gate forbids, and which makes the resolution untestable.
package theme

import (
	"charm.land/lipgloss/v2"

	"github.com/Toshik1978/beads-viewer/internal/beads"
	"github.com/Toshik1978/beads-viewer/internal/config"
)

// DetectedBackground is what the terminal reported, including the common case
// of reporting nothing at all.
type DetectedBackground int

// The possible detection outcomes.
const (
	// BackgroundUnknown means the terminal did not answer. This is normal over
	// SSH, inside tmux and screen, and in CI — it is a distinct state, not a
	// synonym for dark.
	BackgroundUnknown DetectedBackground = iota
	BackgroundLight
	BackgroundDark
)

// Scheme is the resolved colour scheme a Theme is built for.
type Scheme int

// The resolvable schemes.
const (
	SchemeDark Scheme = iota
	SchemeLight
	// SchemeAgnostic assumes neither a light nor a dark background. It exists
	// because a bool cannot carry three outcomes, and folding "the terminal
	// never answered" into SchemeDark — even under a named branch — hands a
	// light-terminal user the dark-tuned palette: near-white text on white.
	SchemeAgnostic
)

// Theme carries the resolved styles.
type Theme struct {
	// Scheme records which of the three schemes was resolved, so views and
	// the markdown renderer cannot disagree about it.
	Scheme Scheme

	Base      lipgloss.Style
	Muted     lipgloss.Style
	Accent    lipgloss.Style
	Selected  lipgloss.Style
	Border    lipgloss.Style
	StatusBar lipgloss.Style
	Error     lipgloss.Style
	Title     lipgloss.Style

	priorities [5]lipgloss.Style
}

// New builds the theme for a preference and a terminal's reported background.
//
// Detection itself belongs to the caller, not this package: cmd/bv asks
// bubbletea for the terminal's background and passes the answer in here,
// which keeps Resolve pure and this whole package testable without a
// terminal. New takes the detected background directly rather than a bool,
// because Resolve has three possible outcomes and a bool cannot carry a third.
func New(pref config.ThemePreference, detected DetectedBackground) Theme {
	return build(Resolve(pref, detected))
}

// Resolve decides the scheme from the preference and what was detected.
//
// An explicit preference always wins, which is the escape hatch for a
// terminal that answers wrongly. Under auto with no answer the result is
// SchemeAgnostic, not SchemeDark: dark terminals being the majority does not
// make dark a safe guess, because the cost of guessing wrong is unreadable
// text, and that guess is exactly what rendered near-white text on light
// terminals over SSH in the pre-rewrite tree. Assuming neither background is
// the only answer that is safe on both.
func Resolve(pref config.ThemePreference, detected DetectedBackground) Scheme {
	switch pref {
	case config.ThemeDark:
		return SchemeDark
	case config.ThemeLight:
		return SchemeLight
	case config.ThemeAuto:
		return resolveAuto(detected)
	}

	// An invalid preference cannot reach here in practice — config.Load
	// validates it — but Resolve must still be total, and assuming nothing is
	// the same principle applied to an unknown terminal background below.
	return SchemeAgnostic
}

// GlamourStyle names the markdown stylesheet matching the resolved scheme.
//
// glamour v2 removed WithAutoStyle, so the caller supplies the name. Deriving
// it here guarantees the detail pane and the surrounding chrome agree.
// SchemeAgnostic maps to "notty" rather than reusing "dark" or "light":
// glamour's two built-in stylesheets both bake in a background assumption
// this scheme exists to avoid making.
func (t Theme) GlamourStyle() string {
	switch t.Scheme {
	case SchemeDark:
		return "dark"
	case SchemeLight:
		return "light"
	case SchemeAgnostic:
		return "notty"
	}

	return "notty"
}

// Priority returns the style for a priority, clamped so an out-of-range value
// from hand-edited JSONL cannot index past the table.
func (t Theme) Priority(p beads.Priority) lipgloss.Style {
	return t.priorities[p.Clamp()]
}

// Frame returns the style a pane's border is drawn with. The colour is the
// whole focus indicator, so the two must stay visibly different: Accent when
// focused, Border otherwise.
//
// The colours are read back off the existing styles with GetForeground rather
// than stored as two more fields, so there is exactly one place each colour is
// chosen — the palette — and no way for a frame to drift from the text it
// surrounds.
func (t Theme) Frame(focused bool) lipgloss.Style {
	edge := t.Border.GetForeground()
	if focused {
		edge = t.Accent.GetForeground()
	}

	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(edge)
}

// TypeGlyph returns the single-width marker for an issue type. Unknown types
// get a neutral glyph rather than an empty string, because a missing glyph
// shifts every column on that row.
func (t Theme) TypeGlyph(k beads.IssueType) string {
	switch k {
	case beads.TypeBug:
		return "!"
	case beads.TypeFeature:
		return "+"
	case beads.TypeEpic:
		return "#"
	case beads.TypeChore:
		return "~"
	case beads.TypeDocs:
		return "="
	case beads.TypeQuestion:
		return "?"
	case beads.TypeTask:
		return "-"
	default:
		return "·"
	}
}

// resolveAuto is the terminal-detection branch of Resolve, split out so each
// outcome — including no answer — is its own named case rather than an
// implicit default.
func resolveAuto(detected DetectedBackground) Scheme {
	switch detected {
	case BackgroundLight:
		return SchemeLight
	case BackgroundDark:
		return SchemeDark
	case BackgroundUnknown:
		// The terminal never answered the OSC 11 background query — normal
		// over SSH and inside tmux. This is named as its own case, not folded
		// into BackgroundDark, precisely because the two must not resolve to
		// the same scheme: doing so is the bug this package exists to avoid
		// repeating.
		return SchemeAgnostic
	}

	return SchemeAgnostic
}

// build assembles the styles for a resolved scheme.
func build(scheme Scheme) Theme {
	if scheme == SchemeAgnostic {
		return buildAgnostic()
	}

	return buildHue(scheme)
}

// buildHue assembles SchemeDark or SchemeLight: every colour is a fixed hex
// value chosen for contrast against that specific background.
func buildHue(scheme Scheme) Theme {
	p := huePalette(scheme == SchemeDark)

	return Theme{
		Scheme: scheme,
		Base:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.base)),
		Muted:  lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)),
		Accent: lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)),
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.selectedFg)).
			Background(lipgloss.Color(p.selectedBg)).
			Bold(true),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(p.border)),
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color(p.statusBarFg)).
			Background(lipgloss.Color(p.statusBarBg)),
		Error: lipgloss.NewStyle().Foreground(lipgloss.Color(p.errorColor)).Bold(true),
		Title: lipgloss.NewStyle().Foreground(lipgloss.Color(p.title)).Bold(true),

		priorities: buildPriorities(p),
	}
}

// buildAgnostic assembles SchemeAgnostic: colours come only from the ANSI
// 0-15 semantic slots, which every terminal maps to its own theme, and Base
// sets neither a foreground nor a background so plain text inherits whatever
// the terminal already renders — the one pair guaranteed legible when nothing
// about the background is known.
func buildAgnostic() Theme {
	p := agnosticPalette()

	return Theme{
		Scheme:    SchemeAgnostic,
		Base:      lipgloss.NewStyle(),
		Muted:     lipgloss.NewStyle().Faint(true),
		Accent:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)),
		Selected:  lipgloss.NewStyle().Reverse(true).Bold(true),
		Border:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.border)),
		StatusBar: lipgloss.NewStyle().Reverse(true),
		Error:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.errorColor)).Bold(true),
		Title:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.title)).Bold(true),

		priorities: buildPriorities(p),
	}
}

// buildPriorities renders one style per priority level, in br's P0..P4 order.
// Critical is bolded in addition to its colour, since colour alone is not a
// reliable signal on every terminal.
func buildPriorities(p palette) [5]lipgloss.Style {
	var out [5]lipgloss.Style
	for i, code := range p.priorities {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(code))
		if beads.Priority(i) == beads.PriorityCritical {
			style = style.Bold(true)
		}
		out[i] = style
	}

	return out
}
