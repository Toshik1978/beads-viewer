package theme

// Colours reused across more than one field of a single scheme, pulled out to
// satisfy goconst rather than repeating the literal.
const (
	colorAccentDark = "#7AA2F7" // dark scheme: accent, priority P3
	colorBorderDark = "#565F89" // dark scheme: border, priority P4
	colorErrorDark  = "#F7768E" // dark scheme: error, priority P0
	colorErrorLight = "#B00020" // light scheme: error, priority P0

	// ANSI 0-15 semantic slots for the agnostic scheme. Every terminal maps
	// these to its own theme, so — unlike the hex constants above — they
	// carry no assumption about which background they will land on.
	ansiRed         = "1" // agnostic: error, priority P0
	ansiBlue        = "4" // agnostic: accent, priority P3
	ansiBrightBlack = "8" // agnostic: border, priority P4
)

// palette holds raw colours for one resolved scheme — hex for SchemeDark and
// SchemeLight, ANSI 0-15 indices for SchemeAgnostic — kept separate from
// Theme so the exported type stays lipgloss.Style values rather than strings.
// Unexported, so it does not count against the exported-type cap, but it is
// still kept under the 20-field limit that applies to any type.
//
// base, muted, selectedFg, selectedBg, statusBarFg and statusBarBg are unused
// by the agnostic palette: buildAgnostic builds those fields from style
// attributes (Faint, Reverse) or leaves them unset entirely, not from a
// colour, because assuming any colour is exactly what SchemeAgnostic exists
// to avoid.
type palette struct {
	base        string
	muted       string
	accent      string
	selectedFg  string
	selectedBg  string
	border      string
	statusBarFg string
	statusBarBg string
	errorColor  string
	title       string

	priorities [5]string
}

// huePalette picks the raw hex colours for SchemeDark or SchemeLight. Values
// differ between the two, not just in polarity, because a colour tuned for
// contrast on a dark background is frequently unreadable on a light one and
// vice versa — halving saturation is not enough. It is written as a single
// literal, selected field by field with pick, rather than two near-identical
// darkPalette/lightPalette functions, which the linter's duplicate-code
// check flags as a maintenance hazard: the two would drift apart silently.
func huePalette(dark bool) palette {
	return palette{
		base:        pick(dark, "#E4E4E4", "#1A1B26"),
		muted:       pick(dark, "#787C99", "#6C6C6C"),
		accent:      pick(dark, colorAccentDark, "#2E5C9A"),
		selectedFg:  pick(dark, "#FFFFFF", "#000000"),
		selectedBg:  pick(dark, "#3B4261", "#D0D7E8"),
		border:      pick(dark, colorBorderDark, "#A0A0A0"),
		statusBarFg: pick(dark, "#C0CAF5", "#1A1B26"),
		statusBarBg: pick(dark, "#1F2335", "#E8E8E8"),
		errorColor:  pick(dark, colorErrorDark, colorErrorLight),
		title:       pick(dark, "#BB9AF7", "#5B2A86"),

		priorities: [5]string{
			pick(dark, colorErrorDark, colorErrorLight), // P0 critical
			pick(dark, "#FF9E64", "#C2410C"),            // P1 high
			pick(dark, "#E0AF68", "#A16207"),            // P2 medium
			pick(dark, colorAccentDark, "#1D4ED8"),      // P3 low
			pick(dark, colorBorderDark, "#6B7280"),      // P4 backlog
		},
	}
}

// agnosticPalette picks the ANSI 0-15 colours for SchemeAgnostic. base, muted
// and the selection/status-bar pairs are left as the zero value on purpose —
// see the palette doc comment.
func agnosticPalette() palette {
	return palette{
		accent:     ansiBlue,
		border:     ansiBrightBlack,
		errorColor: ansiRed,
		title:      "5", // magenta

		priorities: [5]string{
			ansiRed,         // P0 critical
			"9",             // P1 high (bright red)
			"3",             // P2 medium (yellow)
			ansiBlue,        // P3 low
			ansiBrightBlack, // P4 backlog
		},
	}
}

// pick chooses between a dark-scheme and a light-scheme colour.
func pick(dark bool, darkVal, lightVal string) string {
	if dark {
		return darkVal
	}

	return lightVal
}
