package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/interloom/cli/internal/api"
)

// Palette — a sophisticated indigo/violet scheme with a teal accent.
var (
	cBrand   = lipgloss.Color("#A78BFA") // violet
	cBrandHi = lipgloss.Color("#C4B5FD") // light violet
	cAccent  = lipgloss.Color("#5EEAD4") // teal
	cFg      = lipgloss.Color("#E5E7EB") // near-white
	cMuted   = lipgloss.Color("#9CA3AF") // gray
	cDim     = lipgloss.Color("#6B7280") // dim gray
	cBorder  = lipgloss.Color("#3B3B52") // blurred border
	cBg      = lipgloss.Color("#312E81") // selection background (indigo)

	// Status colors.
	cOpen      = lipgloss.Color("#93C5FD") // blue
	cStarted   = lipgloss.Color("#FCD34D") // amber
	cBlocked   = lipgloss.Color("#F87171") // red
	cCompleted = lipgloss.Color("#34D399") // green
	cCancelled = cDim
)

// logoArt is the interloom brand mark in ASCII: outer chevrons ❯ ❮ drawing the
// inner threads )( together on a loom. Every line is the same display width so
// it centers cleanly.
const logoArt = "╲   ╲ ╱   ╱\n" +
	" ╲  │ │  ╱ \n" +
	" ╱  │ │  ╲ \n" +
	"╱   ╱ ╲   ╲"

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
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#1F2937")).
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
