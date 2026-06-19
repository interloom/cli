package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/interloom/cli/internal/api"
)

const (
	cBrandDark   = "#A78BFA"
	cBrandHiDark = "#C4B5FD"
	cDimDark     = "#6B7280"
	cBorderDark  = "#3B3B52"
)

func adaptiveColor(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// Palette with light/dark variants for terminal background contrast.
var (
	cBrand    = adaptiveColor("#6D28D9", cBrandDark)   // violet
	cBrandHi  = adaptiveColor("#4C1D95", cBrandHiDark) // light/deep violet
	cAccent   = adaptiveColor("#0F766E", "#5EEAD4")    // teal
	cFg       = adaptiveColor("#111827", "#E5E7EB")    // primary text
	cMuted    = adaptiveColor("#4B5563", "#9CA3AF")    // gray
	cDim      = adaptiveColor("#374151", cDimDark)     // dim gray
	cBorder   = adaptiveColor("#6B7280", cBorderDark)  // soft border
	cBg       = lipgloss.Color("#312E81")              // selection background (indigo)
	cOnColor  = lipgloss.Color("#F9FAFB")              // text on selection
	cOnStatus = adaptiveColor("#F9FAFB", "#1F2937")    // text on status badges

	// Status colors.
	cOpen      = adaptiveColor("#1D4ED8", "#93C5FD") // blue
	cStarted   = adaptiveColor("#A16207", "#FCD34D") // amber
	cBlocked   = adaptiveColor("#B91C1C", "#F87171") // red
	cCompleted = adaptiveColor("#047857", "#34D399") // green
	cCancelled = cDim
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

	// mentionSt highlights resolved @user mentions inside thread messages.
	mentionSt = lipgloss.NewStyle().Foreground(cAccent).Bold(true)

	helpKey  = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	helpDesc = lipgloss.NewStyle().Foreground(cDim)
	helpSep  = dimStyle.Render(" · ")
)

// statusBadge renders a small colored status label, e.g. for the detail header.
func statusBadge(s api.CaseStatus) string {
	var c lipgloss.TerminalColor
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
	return lipgloss.NewStyle().Foreground(cOnStatus).
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
