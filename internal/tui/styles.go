package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/interloom/cli/internal/api"
)

type tuiPalette struct {
	brand, brandHi, accent, fg, muted, dim, border, bg lipgloss.Color
	open, started, blocked, completed, cancelled       lipgloss.Color
	badgeFg, introBase, introSheen, introPressA        lipgloss.Color
	introPressB                                        lipgloss.Color
}

var darkPalette = tuiPalette{
	brand:       lipgloss.Color("#A78BFA"), // violet
	brandHi:     lipgloss.Color("#C4B5FD"), // light violet
	accent:      lipgloss.Color("#5EEAD4"), // teal
	fg:          lipgloss.Color("#E5E7EB"), // near-white
	muted:       lipgloss.Color("#9CA3AF"), // gray
	dim:         lipgloss.Color("#6B7280"), // dim gray
	border:      lipgloss.Color("#3B3B52"), // blurred border
	bg:          lipgloss.Color("#312E81"), // selection background (indigo)
	open:        lipgloss.Color("#93C5FD"), // blue
	started:     lipgloss.Color("#FCD34D"), // amber
	blocked:     lipgloss.Color("#F87171"), // red
	completed:   lipgloss.Color("#34D399"), // green
	cancelled:   lipgloss.Color("#6B7280"),
	badgeFg:     lipgloss.Color("#1F2937"),
	introBase:   lipgloss.Color("#2E2E4A"),
	introSheen:  lipgloss.Color("#E6DEFF"),
	introPressA: lipgloss.Color("#4B5563"),
	introPressB: lipgloss.Color("#9CA3AF"),
}

var lightPalette = tuiPalette{
	brand:       lipgloss.Color("#6D28D9"),
	brandHi:     lipgloss.Color("#4C1D95"),
	accent:      lipgloss.Color("#0F766E"),
	fg:          lipgloss.Color("#07040D"),
	muted:       lipgloss.Color("#584875"),
	dim:         lipgloss.Color("#8A7AA8"),
	border:      lipgloss.Color("#B2A2D1"),
	bg:          lipgloss.Color("#E5DFF1"),
	open:        lipgloss.Color("#1D4ED8"),
	started:     lipgloss.Color("#A16207"),
	blocked:     lipgloss.Color("#B91C1C"),
	completed:   lipgloss.Color("#047857"),
	cancelled:   lipgloss.Color("#8A7AA8"),
	badgeFg:     lipgloss.Color("#FFFFFF"),
	introBase:   lipgloss.Color("#E5DFF1"),
	introSheen:  lipgloss.Color("#4C1D95"),
	introPressA: lipgloss.Color("#8A7AA8"),
	introPressB: lipgloss.Color("#584875"),
}

var palette = darkPalette

func activePalette() tuiPalette {
	if lipgloss.HasDarkBackground() {
		return darkPalette
	}
	return lightPalette
}

// Palette — an indigo/violet scheme with a teal accent, adapted for terminal
// background brightness.
var (
	cBrand   = palette.brand
	cBrandHi = palette.brandHi
	cAccent  = palette.accent
	cFg      = palette.fg
	cMuted   = palette.muted
	cDim     = palette.dim
	cBorder  = palette.border
	cBg      = palette.bg

	// Status colors.
	cOpen      = palette.open
	cStarted   = palette.started
	cBlocked   = palette.blocked
	cCompleted = palette.completed
	cCancelled = palette.cancelled

	cBadgeFg     = palette.badgeFg
	cIntroBase   = palette.introBase
	cIntroSheen  = palette.introSheen
	cIntroPressA = palette.introPressA
	cIntroPressB = palette.introPressB
)

// logoArt is the interloom brand mark rendered as a half-block silhouette of
// the real logo: two inward chevrons > < framing the loom's twin curved
// threads. It is rasterized from the brand SVG so it reads as the filled mark
// rather than thin strokes.
const logoArt = "       ▄▄▄     ▄▄\n" +
	"      ████     ████\n" +
	"       ████   ████\n" +
	" ▄▄    ▀███   ███     ▄▄\n" +
	"█████▄  ███▄ ████  ▄█████\n" +
	"▀▀▀████▄ ███ ███ ▄████▀▀\n" +
	"    ▀███ ███ ███ ███\n" +
	"  ▄▄████ ███ ███ ████▄▄\n" +
	"██████▀ ▄██▀ ███▄ ▀██████\n" +
	"▀█▀▀   ▄███  ▀███    ▀▀█▀\n" +
	"       ████   ████\n" +
	"      ▄███    ▀███▄\n" +
	"      ▀▀██     ██▀▀"

// logoMark is the compact one-line mark used in the header.
const logoMark = ">)(<"

var (
	titleStyle = lipgloss.NewStyle().Foreground(cBrandHi).Bold(true)

	colTitleStyle = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	colTitleFocus = lipgloss.NewStyle().Foreground(cBrand).Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder)
	boxFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBrand)

	dimStyle = lipgloss.NewStyle().Foreground(cDim)
	mutedSt  = lipgloss.NewStyle().Foreground(cMuted)
	accentSt = lipgloss.NewStyle().Foreground(cAccent)
	brandSt  = lipgloss.NewStyle().Foreground(cBrand)

	helpKey  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	helpDesc = lipgloss.NewStyle().Foreground(cDim)
	helpSep  = dimStyle.Render(" · ")
)

func applyPalette(p tuiPalette) {
	palette = p

	cBrand = palette.brand
	cBrandHi = palette.brandHi
	cAccent = palette.accent
	cFg = palette.fg
	cMuted = palette.muted
	cDim = palette.dim
	cBorder = palette.border
	cBg = palette.bg

	cOpen = palette.open
	cStarted = palette.started
	cBlocked = palette.blocked
	cCompleted = palette.completed
	cCancelled = palette.cancelled

	cBadgeFg = palette.badgeFg
	cIntroBase = palette.introBase
	cIntroSheen = palette.introSheen
	cIntroPressA = palette.introPressA
	cIntroPressB = palette.introPressB

	titleStyle = lipgloss.NewStyle().Foreground(cBrandHi).Bold(true)
	colTitleStyle = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	colTitleFocus = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder)
	boxFocusStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBrand)
	dimStyle = lipgloss.NewStyle().Foreground(cDim)
	mutedSt = lipgloss.NewStyle().Foreground(cMuted)
	accentSt = lipgloss.NewStyle().Foreground(cAccent)
	brandSt = lipgloss.NewStyle().Foreground(cBrand)
	helpKey = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	helpDesc = lipgloss.NewStyle().Foreground(cDim)
	helpSep = dimStyle.Render(" · ")
}

// statusBadge renders a small colored status label, e.g. for the detail header.
func statusBadge(s api.CaseStatus) string {
	var c lipgloss.Color
	switch s {
	case api.Open:
		c = cOpen
	case api.Started:
		c = cStarted
	case api.Blocked:
		c = cBlocked
	case api.Completed:
		c = cCompleted
	case api.Cancelled:
		c = cCancelled
	default:
		c = cDim
	}
	return lipgloss.NewStyle().Foreground(cBadgeFg).
		Background(c).Bold(true).Padding(0, 1).Render(string(s))
}

// truncate shortens text to width w (display cells) with an ellipsis. It uses
// the same width logic as lipgloss so padding math elsewhere stays exact and
// rows never wrap inside their pane.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// lipglossWidth reports the rendered (ANSI-aware) display width of s.
func lipglossWidth(s string) int { return lipgloss.Width(s) }
