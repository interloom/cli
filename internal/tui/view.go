package tui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/interloom/cli/internal/api"
)

// Detail metadata keys.
const (
	mCreated  = "created"
	mUpdated  = "updated"
	mDue      = "due"
	mAssignee = "assignee"
	mSize     = "size"
)

// ---- layout ----

type layoutInfo struct {
	colW               []int // one width per visible list pane
	colTop, colsH      int
	detailTop, detailH int
	detailInnerW       int
	detailInnerH       int
}

// columnWidths sizes the visible list panes to span the full width.
func (m model) columnWidths(w int) []int {
	if m.phase == phaseCase {
		a := clamp(w*34/100, 18, 46) // sub-cases
		b := clamp(w*30/100, 16, 40) // files
		c := w - a - b               // notes
		for c < 16 && (a > 18 || b > 16) {
			if a > 18 {
				a--
			} else {
				b--
			}
			c = w - a - b
		}
		return []int{a, b, c}
	}
	a := clamp(w*28/100, 16, 36) // spaces
	return []int{a, w - a}       // cases takes the rest
}

func (m model) layout() layoutInfo {
	w, h := m.width, m.height
	colW := m.columnWidths(w)

	const headerH, footerH = 3, 1
	bodyH := h - headerH - footerH
	if bodyH < 8 {
		bodyH = 8
	}
	colsH := bodyH * 42 / 100
	if colsH < 7 {
		colsH = 7
	}
	if bodyH-colsH < 6 {
		colsH = bodyH - 6
	}
	if colsH < 5 {
		colsH = bodyH
	}
	detailH := bodyH - colsH

	li := layoutInfo{
		colW:      colW,
		colTop:    headerH,
		colsH:     colsH,
		detailTop: headerH + colsH,
		detailH:   detailH,
	}
	li.detailInnerW = max(10, w-4)
	li.detailInnerH = max(1, detailH-3)
	return li
}

// applyLayout resizes the viewport and (re)builds the markdown renderer.
func (m *model) applyLayout() {
	l := m.layout()
	m.vp.Width = l.detailInnerW
	m.vp.Height = l.detailInnerH
	if m.md == nil || m.mdWidth != l.detailInnerW {
		m.mdWidth = l.detailInnerW
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(max(20, l.detailInnerW)),
		)
		if err == nil {
			m.md = r
		}
	}
}

func (m *model) syncOffsets(l layoutInfo) {
	rows := l.colsH - 3
	m.sp.off = windowOffset(m.sp.cur, m.sp.off, m.sp.len(), rows)
	m.cs.off = windowOffset(m.cs.cur, m.cs.off, m.cs.len(), rows)
	m.scs.off = windowOffset(m.scs.cur, m.scs.off, m.scs.len(), rows)
	m.fl.off = windowOffset(m.fl.cur, m.fl.off, m.fl.len(), rows)
	m.nt.off = windowOffset(m.nt.cur, m.nt.off, m.nt.len(), rows)
}

func windowOffset(cur, off, count, rows int) int {
	if rows <= 0 || count <= 0 {
		return 0
	}
	if off > count-rows {
		off = count - rows
	}
	if off < 0 {
		off = 0
	}
	if cur < off {
		off = cur
	}
	if cur >= off+rows {
		off = cur - rows + 1
	}
	if off < 0 {
		off = 0
	}
	return off
}

// columnAt maps an x coordinate to the focus of the list pane under it.
func (m model) columnAt(x int, l layoutInfo) (focus, bool) {
	panes := m.listPanes()
	acc := 0
	for i, f := range panes {
		if i >= len(l.colW) {
			break
		}
		acc += l.colW[i]
		if x < acc {
			return f, true
		}
	}
	return 0, false
}

// ---- mouse ----

func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.phase == phaseIntro || m.cfgPickerOpen {
		return m, nil
	}
	l := m.layout()
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		return m.handleWheel(msg, l, -1)
	case msg.Button == tea.MouseButtonWheelDown:
		return m.handleWheel(msg, l, 1)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		return m.handleClick(msg, l)
	}
	return m, nil
}

// handleWheel scrolls the detail pane when hovering it, else moves the list
// under the cursor.
func (m model) handleWheel(msg tea.MouseMsg, l layoutInfo, dir int) (tea.Model, tea.Cmd) {
	if msg.Y >= l.detailTop {
		if dir > 0 {
			m.vp.ScrollDown(2)
		} else {
			m.vp.ScrollUp(2)
		}
		return m, nil
	}
	if f, ok := m.columnAt(msg.X, l); ok {
		m.setFocus(f)
		return m.moveCursor(dir)
	}
	return m, nil
}

// handleClick focuses the clicked pane and selects the clicked row.
func (m model) handleClick(msg tea.MouseMsg, l layoutInfo) (tea.Model, tea.Cmd) {
	if msg.Y >= l.detailTop {
		m.setFocus(focusDetail)
		m.refreshDetail()
		return m, nil
	}
	f, ok := m.columnAt(msg.X, l)
	if !ok {
		return m, nil
	}
	m.setFocus(f)

	visRows := l.colsH - 3
	row := msg.Y - (l.colTop + 2) // y of the first item row
	if row < 0 || row >= visRows {
		// Clicking the pane (not a row) still loads its detail.
		cmd := m.focusLoadCmd()
		m.refreshDetail()
		return m, cmd
	}
	return m.selectRow(f, row, visRows, l)
}

func (m model) selectRow(f focus, row, visRows int, l layoutInfo) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch f {
	case focusSpaces:
		if m.sp.selectAt(windowOffset(m.sp.cur, m.sp.off, m.sp.len(), visRows) + row) {
			cmd = tea.Batch(m.maybeMoreSpaces(), m.onSpaceChange())
		}
	case focusCases:
		if m.cs.selectAt(windowOffset(m.cs.cur, m.cs.off, m.cs.len(), visRows) + row) {
			cmd = tea.Batch(m.maybeMoreCases(), m.previewLoad(focusCases))
		}
	case focusSubcases:
		if m.scs.selectAt(windowOffset(m.scs.cur, m.scs.off, m.scs.len(), visRows) + row) {
			cmd = tea.Batch(m.maybeMoreSubcases(), m.previewLoad(focusSubcases))
		}
	case focusFiles:
		// Always (re)load the clicked file's preview: unlike the other panes,
		// file previews aren't auto-loaded on focus, so a click on the already
		// selected file must still trigger the load. previewLoad is idempotent.
		m.fl.selectAt(windowOffset(m.fl.cur, m.fl.off, m.fl.len(), visRows) + row)
		cmd = tea.Batch(m.maybeMoreFiles(), m.previewLoad(focusFiles))
	case focusNotes:
		if m.nt.selectAt(windowOffset(m.nt.cur, m.nt.off, m.nt.len(), visRows) + row) {
			cmd = tea.Batch(m.maybeMoreNotes(), m.previewLoad(focusNotes))
		}
	}
	m.syncOffsets(l)
	m.refreshDetail()
	return m, cmd
}

// ---- view ----

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		// Size is seeded at construction, so this is only a defensive net:
		// never fall back to a bare line — show the splash at a default size.
		m.width, m.height = 80, 24
	}
	if m.phase == phaseIntro {
		return m.renderIntro()
	}
	if m.cfgPickerOpen {
		return m.renderConfigPicker()
	}
	l := m.layout()
	bottom := m.renderDetail(l)
	if m.debug {
		bottom = m.renderDebugPanel(l)
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderHeader(),
		m.renderColumns(l),
		bottom,
		m.renderFooter(),
	)
}

// introPulse cycles the splash logo through brand shades for a soft glow.
var introPulse = []lipgloss.Color{cDim, cMuted, cBrand, cBrandHi, cAccent, cBrandHi, cBrand, cMuted}

// introHint is a smooth grey "breathing" ramp for the press-enter hint: it
// fades up from a dim grey to a soft grey and back, one shade per frame.
var introHint = breatheRamp("#4B5563", "#9CA3AF", 18)

// breatheRamp builds a ping-pong gradient between two "#RRGGBB" colors
// (from→to→from) so that stepping through it one entry per frame produces a
// smooth pulse.
func breatheRamp(from, to string, steps int) []lipgloss.Color {
	up := make([]lipgloss.Color, steps)
	for i := range up {
		up[i] = lerpHex(from, to, float64(i)/float64(steps-1))
	}
	ramp := append([]lipgloss.Color{}, up...)
	for i := steps - 2; i > 0; i-- { // mirror back down, skipping the endpoints
		ramp = append(ramp, up[i])
	}
	return ramp
}

// lerpHex linearly interpolates between two "#RRGGBB" colors at t in [0,1].
func lerpHex(a, b string, t float64) lipgloss.Color {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	lerp := func(x, y int) int { return int(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// hexRGB splits a "#RRGGBB" color into its 8-bit components.
func hexRGB(s string) (r, g, b int) {
	v, _ := strconv.ParseInt(strings.TrimPrefix(s, "#"), 16, 32)
	return int(v>>16) & 0xFF, int(v>>8) & 0xFF, int(v) & 0xFF
}

// renderIntro draws the animated startup splash, centered in the terminal.
// The wordmark resolves letter-by-letter out of dim dots, a teal highlight
// sweeps across it, an accent rule grows from the center, and the tagline and
// loading line fade in — all driven by m.introFrame.
func (m model) renderIntro() string {
	const word = "interloom"
	letters := []rune(word)
	f := m.introFrame

	revealed := clamp(f-1, 0, len(letters)) // one letter per frame after a beat

	// Shimmer position once fully revealed. After the first pass it loops with a
	// short gap so the wordmark keeps glinting while we wait for Enter.
	sweep := f - (len(letters) + 3)
	if revealComplete := f >= len(letters)+3; revealComplete {
		cycle := len(letters) + 8
		sweep = (f - (len(letters) + 3)) % cycle
	}

	var b strings.Builder
	for i, r := range letters {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch {
		case i >= revealed:
			b.WriteString(dimStyle.Render("·"))
		case i == revealed-1 && revealed < len(letters):
			b.WriteString(introLetter(r, cBrandHi)) // freshly revealed letter glows
		case i == sweep || i == sweep-1:
			b.WriteString(introLetter(r, cAccent)) // shimmer sweep
		default:
			b.WriteString(introLetter(r, cBrand))
		}
	}
	wordmark := b.String()
	wordW := lipglossWidth(wordmark)

	logo := lipgloss.NewStyle().Foreground(introPulse[(f/3)%len(introPulse)]).
		Bold(true).Render(logoArt)

	ruleLen := clamp((f-4)*2, 0, wordW) // grows from the center outward
	rule := brandSt.Render(strings.Repeat("─", ruleLen))

	tagline := ""
	if f >= len(letters)+2 {
		ramp := []lipgloss.Color{cBorder, cDim, cMuted}
		ti := clamp((f-(len(letters)+2))/2, 0, len(ramp)-1)
		tagline = lipgloss.NewStyle().Foreground(ramp[ti]).
			Render("Supercharged Operations")
	}

	block := lipgloss.JoinVertical(
		lipgloss.Center,
		logo, "", wordmark, rule, "", tagline, "", m.introLoading(f),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}

// introLoading renders the splash's bottom status line: a spinner while the
// workspace loads, then a gently pulsing "press enter" hint once it is ready.
func (m model) introLoading(f int) string {
	if f < 4 {
		return ""
	}
	if m.sp.loading || m.sp.len() == 0 {
		return m.spin.View() + mutedSt.Render(" loading workspace…")
	}
	// Step one shade per frame through the breathing ramp for a smooth pulse.
	return lipgloss.NewStyle().Foreground(introHint[f%len(introHint)]).
		Render("press enter to continue")
}

func introLetter(r rune, c lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r))
}

func (m model) renderHeader() string {
	left := titleStyle.Render(logoMark + " interloom")
	if m.cfgName != "" {
		left += dimStyle.Render(" · ") + accentSt.Render(m.cfgName)
	}
	if m.phase == phaseCase {
		left += "  " + m.caseBreadcrumb()
	}
	if m.statusFilter != "" {
		left += "   " + dimStyle.Render("filter ") + statusBadge(m.statusFilter)
	}
	content := lineLR(left, m.headerStatus(), m.width-4)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBrand).
		Width(m.width-2).
		Padding(0, 1).
		Render(content)
}

// headerStatus is the right-aligned header indicator: just the debug and error
// flags (the old item-count indicator was removed).
func (m model) headerStatus() string {
	var parts []string
	if m.debug {
		parts = append(parts, lipgloss.NewStyle().Foreground(cStarted).Render("● debug"))
	}
	if m.err != nil {
		parts = append(parts, lipgloss.NewStyle().Foreground(cBlocked).Render("● error"))
	}
	return strings.Join(parts, "  ")
}

func (m model) renderColumns(l layoutInfo) string {
	panes := m.listPanes()
	cols := make([]string, 0, len(panes))
	for i, f := range panes {
		cols = append(cols, m.renderPane(f, l.colW[i], l.colsH))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// renderPane draws one list pane by focus identity.
func (m model) renderPane(f focus, w, colsH int) string {
	rows := colsH - 3
	switch f {
	case focusSpaces:
		off := windowOffset(m.sp.cur, m.sp.off, m.sp.len(), rows)
		return m.renderColumn(colSpec{
			title: "SPACES", focused: m.focus == focusSpaces, w: w, colsH: colsH,
			count: m.sp.len(), off: off, list: m.sp.meta(), emptyMsg: "No spaces",
		}, func(i, w int) string {
			s, _ := m.sp.at(i)
			glyph, gc, tc := "◆", cBrand, cFg
			if s.IsSystem {
				glyph, gc, tc = "▩", cDim, cMuted
			}
			return renderRow(glyph, gc, s.Name, tc, i == m.sp.cur, w)
		})
	case focusCases:
		off := windowOffset(m.cs.cur, m.cs.off, m.cs.len(), rows)
		return m.renderColumn(colSpec{
			title: "CASES", focused: m.focus == focusCases, w: w, colsH: colsH,
			count: m.cs.len(), off: off, list: m.cs.meta(), emptyMsg: m.casesEmptyMsg(),
		}, func(i, w int) string {
			cs, _ := m.cs.at(i)
			glyph, gc, tc := statusParts(cs.Status)
			return renderRow(glyph, gc, cs.Title, tc, i == m.cs.cur, w)
		})
	case focusSubcases:
		off := windowOffset(m.scs.cur, m.scs.off, m.scs.len(), rows)
		return m.renderColumn(colSpec{
			title: "SUB-CASES", focused: m.focus == focusSubcases, w: w, colsH: colsH,
			count: m.scs.len(), off: off, list: m.scs.meta(), emptyMsg: m.subcasesEmptyMsg(),
		}, func(i, w int) string {
			cs, _ := m.scs.at(i)
			glyph, gc, tc := statusParts(cs.Status)
			return renderRow(glyph, gc, cs.Title, tc, i == m.scs.cur, w)
		})
	case focusFiles:
		off := windowOffset(m.fl.cur, m.fl.off, m.fl.len(), rows)
		return m.renderColumn(colSpec{
			title: "FILES", focused: m.focus == focusFiles, w: w, colsH: colsH,
			count: m.fl.len(), off: off, list: m.fl.meta(), emptyMsg: "No files",
		}, func(i, w int) string {
			fi, _ := m.fl.at(i)
			return renderRow("▤", cAccent, fi.Name, cFg, i == m.fl.cur, w)
		})
	case focusNotes:
		off := windowOffset(m.nt.cur, m.nt.off, m.nt.len(), rows)
		return m.renderColumn(colSpec{
			title: "NOTES", focused: m.focus == focusNotes, w: w, colsH: colsH,
			count: m.nt.len(), off: off, list: m.nt.meta(), emptyMsg: "No notes",
		}, func(i, w int) string {
			n, _ := m.nt.at(i)
			return renderRow("▪", cBrandHi, n.Title, cFg, i == m.nt.cur, w)
		})
	}
	return ""
}

func (m model) casesEmptyMsg() string {
	if m.sp.len() == 0 {
		return ""
	}
	if m.statusFilter != "" {
		return "No " + string(m.statusFilter) + " cases"
	}
	return "No cases in this space"
}

func (m model) subcasesEmptyMsg() string {
	if m.statusFilter != "" {
		return "No " + string(m.statusFilter) + " sub-cases"
	}
	return "No sub-cases"
}

// colSpec carries the pagination-aware metadata needed to render a list pane.
type colSpec struct {
	title    string
	focused  bool
	w, colsH int
	count    int
	off      int
	list     listMeta
	emptyMsg string
}

// listMeta is the slice of paged state the renderer needs (kept non-generic so
// one renderColumn can draw every pane type).
type listMeta struct {
	loading     bool
	loadingMore bool
	hasMore     bool
}

func (p *paged[T]) meta() listMeta {
	return listMeta{loading: p.loading, loadingMore: p.loadingMore, hasMore: p.hasMore}
}

// renderColumn draws one bordered list pane with a title and selectable rows.
func (m model) renderColumn(c colSpec, rowFn func(i, innerW int) string) string {
	innerW := c.w - 2
	innerH := c.colsH - 2
	visRows := innerH - 1

	ts := colTitleStyle
	bs := boxStyle
	if c.focused {
		ts = colTitleFocus
		bs = boxFocusStyle
	}
	titleLine := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).
		Render(ts.Render(c.title) + " " + dimStyle.Render(m.colCount(c)))

	lines := m.columnLines(c, rowFn, innerW, visRows)
	for len(lines) < visRows {
		lines = append(lines, strings.Repeat(" ", innerW))
	}

	content := titleLine + "\n" + strings.Join(lines, "\n")
	return bs.Width(innerW).Height(innerH).MaxWidth(c.w).MaxHeight(c.colsH).Render(content)
}

// columnLines builds the body rows of a list pane: a loading/empty placeholder,
// or the visible window of items with a lazy-load hint at the bottom.
func (m model) columnLines(c colSpec, rowFn func(i, innerW int) string, innerW, visRows int) []string {
	switch {
	case c.list.loading:
		return []string{mutedSt.Render(m.spin.View() + " loading…")}
	case c.count == 0 && c.list.loadingMore:
		// Filtered list is empty but more raw pages are still being scanned.
		return []string{mutedSt.Render(m.spin.View() + " filtering…")}
	case c.count == 0:
		if c.emptyMsg == "" {
			return nil
		}
		return []string{dimStyle.Italic(true).Render(truncate(c.emptyMsg, innerW))}
	}

	lines := make([]string, 0, visRows)
	for i := 0; i < visRows; i++ {
		idx := c.off + i
		if idx >= c.count {
			break
		}
		lines = append(lines, rowFn(idx, innerW))
	}
	return m.appendMoreCue(lines, c, innerW, visRows)
}

// appendMoreCue adds a "more below" hint when there are rows past the visible
// window: already-loaded rows below it, a fetchable next page, or a load in
// flight. With a spare body row the hint gets its own line; on a full column it
// is overlaid on the right of the last row so the cue never disappears.
func (m model) appendMoreCue(lines []string, c colSpec, innerW, visRows int) []string {
	moreBelow := c.off+visRows < c.count || c.list.hasMore || c.list.loadingMore
	if !moreBelow {
		return lines
	}
	cue := dimStyle.Render("↓ more")
	if c.list.loadingMore {
		cue = mutedSt.Render(m.spin.View() + " more…")
	}
	if len(lines) < visRows {
		return append(lines, "  "+cue)
	}
	if len(lines) > 0 {
		lines[len(lines)-1] = overlayRight(lines[len(lines)-1], cue, innerW)
	}
	return lines
}

// overlayRight right-aligns cue onto line, truncating line so the cue always
// fits within width w. Used to keep a "more below" hint visible on a full row.
func overlayRight(line, cue string, w int) string {
	if w <= 0 {
		return ""
	}
	cueW := lipglossWidth(cue)
	if cueW >= w {
		return truncate(cue, w)
	}
	left := truncate(line, w-cueW-1)
	gap := w - lipglossWidth(left) - cueW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + cue
}

// colCount renders the item count in the pane title, with a "+" when more
// pages are available.
func (m model) colCount(c colSpec) string {
	s := fmt.Sprintf("%d", c.count)
	if c.list.hasMore {
		s += "+"
	}
	return s
}

// renderRow renders a single list line: pointer, colored glyph, text, padding.
func renderRow(glyph string, glyphColor lipgloss.Color, text string, textColor lipgloss.Color, selected bool, w int) string {
	avail := max(0, w-4) // pointer(2) + glyph(1) + space(1)
	text = truncate(text, avail)
	used := 4 + lipglossWidth(text)
	pad := max(0, w-used)

	if selected {
		base := lipgloss.NewStyle().Foreground(cBrandHi).Background(cBg).Bold(true)
		g := lipgloss.NewStyle().Foreground(glyphColor).Background(cBg)
		return base.Render("▸ ") + g.Render(glyph) + base.Render(" "+text+strings.Repeat(" ", pad))
	}
	g := lipgloss.NewStyle().Foreground(glyphColor)
	t := lipgloss.NewStyle().Foreground(textColor)
	return "  " + g.Render(glyph) + " " + t.Render(text) + strings.Repeat(" ", pad)
}

func (m model) renderDetail(l layoutInfo) string {
	focused := m.focus == focusDetail
	bs := boxStyle
	ts := colTitleStyle
	if focused {
		bs = boxFocusStyle
		ts = colTitleFocus
	}
	mode := "rendered"
	if !m.detailMdOn {
		mode = "raw"
	}
	right := dimStyle.Render(mode) + mutedSt.Render(fmt.Sprintf("  ↕ %3.0f%%", m.vp.ScrollPercent()*100))
	titleLine := lineLR(ts.Render(m.detailLabel()), right, l.detailInnerW)

	content := titleLine + "\n" + m.vp.View()
	return bs.Width(m.width-2).Height(l.detailH-2).MaxHeight(l.detailH).Padding(0, 1).Render(content)
}

func (m model) detailLabel() string {
	switch m.effectiveDetailSource() {
	case focusSpaces:
		return "SPACE"
	case focusCases:
		return "CASE"
	case focusSubcases:
		return "SUB-CASE"
	case focusFiles:
		return "FILE"
	case focusNotes:
		return "NOTE"
	default:
		return "CASE"
	}
}

func (m model) renderFooter() string {
	hints := [][2]string{{"↑/↓", "move"}, {"←/→", "panes"}}
	switch {
	case m.phase != phaseCase:
		hints = append(hints, [2]string{keyEnter, "open"})
	case m.focus == focusFiles || m.focus == focusNotes:
		hints = append(hints, [2]string{keyEnter, "read"})
	case m.focus == focusSubcases:
		hints = append(hints, [2]string{keyEnter, "drill"})
	}
	if m.phase == phaseCase {
		hints = append(hints, [2]string{keyEsc, "back"})
	}
	if _, ok := m.focusedFile(); ok {
		hints = append(hints, [2]string{"o", "browser"})
	}
	if _, ok := m.focusedImageFile(); ok {
		hints = append(hints, [2]string{"v", "view image"})
	}
	hints = append(
		hints,
		[2]string{"f", "status"},
		[2]string{"m", "raw/md"},
		[2]string{"c", "config"},
		[2]string{"d", "debug"},
		[2]string{"r", "reload"},
		[2]string{"q", "quit"},
	)
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, helpKey.Render(h[0])+" "+helpDesc.Render(h[1]))
	}
	line := " " + strings.Join(parts, helpSep)
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(line)
}

// ---- config switcher modal ----

// renderConfigPicker draws the centered "switch config" modal.
func (m model) renderConfigPicker() string {
	innerW := lipglossWidth("SWITCH CONFIG")
	for _, n := range m.cfgNames {
		if w := lipglossWidth(n) + 4; w > innerW { // +4 for "▸ ● "
			innerW = w
		}
	}
	innerW = clamp(innerW, 28, max(28, m.width-10))

	lines := []string{colTitleFocus.Render("SWITCH CONFIG"), ""}
	if len(m.cfgNames) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render(
			truncate("No saved configs — run `interloom auth login`.", innerW),
		))
	}
	for i, name := range m.cfgNames {
		lines = append(lines, configRow(name, innerW, i == m.cfgCur, name == m.cfgName))
	}
	if m.cfgErr != nil {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(cBlocked).
			Render(truncate(m.cfgErr.Error(), innerW)))
	}
	lines = append(
		lines,
		"",
		dimStyle.Render(strings.Repeat("─", innerW)),
		pickerHints(),
	)

	box := boxFocusStyle.Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// configRow renders one config line: a pointer for the selection, a teal dot
// for the active config, and the name padded to the modal width.
func configRow(name string, w int, selected, active bool) string {
	dot := "  "
	if active {
		dot = accentSt.Render("●") + " "
	}
	label := truncate(name, max(1, w-4))
	pad := max(0, w-4-lipglossWidth(label))
	if selected {
		base := lipgloss.NewStyle().Foreground(cBrandHi).Bold(true)
		return base.Render("▸ ") + dot + base.Render(label) + strings.Repeat(" ", pad)
	}
	return "  " + dot + lipgloss.NewStyle().Foreground(cFg).Render(label) + strings.Repeat(" ", pad)
}

func pickerHints() string {
	hints := [][2]string{{"↑/↓", "move"}, {keyEnter, "switch"}, {keyEsc, "cancel"}}
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, helpKey.Render(h[0])+" "+helpDesc.Render(h[1]))
	}
	return strings.Join(parts, helpSep)
}

// ---- detail content ----

func (m *model) refreshDetail() {
	key, header, md := m.detailParts()
	combined := fmt.Sprintf("%s|raw=%v|w=%d", key, m.rawMode, m.mdWidth)
	if combined == m.lastDetailKey {
		return
	}
	m.lastDetailKey = combined

	body := strings.TrimSpace(md)
	// detailMdOn is the *intended* markdown state (used for the title label),
	// independent of whether there is a body to render. Notes and descriptions
	// follow rawMode; text files default to markdown and emails to plain text,
	// with the m toggle flipping whichever default applies.
	switch {
	case strings.HasPrefix(key, "fileimage|"):
		m.detailMdOn = false
	case strings.HasPrefix(key, "filetext|"):
		m.detailMdOn = m.rawMode == m.fileText[strings.TrimPrefix(key, "filetext|")].eml
	default:
		m.detailMdOn = !m.rawMode
	}

	switch {
	case body == "":
		body = ""
	case strings.HasPrefix(key, "fileimage|"):
		body = md // pre-rendered Kitty transmit + placeholder grid; emit verbatim
	case m.detailMdOn:
		if out, ok := m.renderMarkdown(md); ok {
			body = out
		} else if strings.HasPrefix(key, "filetext|") {
			body = renderPlainText(md, m.mdWidth) // markdown unavailable: fall back to wrapped text
		}
	case strings.HasPrefix(key, "filetext|"):
		body = renderPlainText(md, m.mdWidth) // faithful, wrapped plain text
	default:
		body = dimStyle.Render(md) // raw notes/descriptions
	}

	content := header
	if body != "" {
		content += "\n" + body
	}
	m.vp.SetContent(content)
	m.vp.GotoTop()
}

// urlRe matches visible http(s) URLs in already-rendered output. It stops at
// whitespace and ANSI escape introducers so it never swallows styling codes.
var urlRe = regexp.MustCompile(`https?://[^\s\x1b]+`)

// renderMarkdown renders md through the glamour renderer and linkifies URLs.
// It reports ok=false when the renderer is unavailable or rendering fails, so
// callers can fall back to a plain-text view.
func (m *model) renderMarkdown(md string) (string, bool) {
	if m.md == nil {
		return "", false
	}
	out, err := m.md.Render(md)
	if err != nil {
		return "", false
	}
	return linkifyURLs(strings.TrimRight(out, "\n")), true
}

// renderPlainText prepares raw file text for the detail viewport: hard-wrapped
// to the pane width (so long lines stay on screen), softly styled, and with
// clickable URLs.
func renderPlainText(s string, width int) string {
	if width < 10 {
		width = 10
	}
	wrapped := ansi.Hardwrap(strings.TrimRight(s, "\n"), width, false)
	return linkifyURLs(lipgloss.NewStyle().Foreground(cFg).Render(wrapped))
}

// errLine renders a one-line error with a warning glyph, trimmed to width.
func errLine(err error, width int) string {
	return lipgloss.NewStyle().Foreground(cBlocked).
		Render("⚠ " + truncate(err.Error(), max(10, width-2)))
}

// linkifyURLs wraps visible http(s) URLs with OSC 8 hyperlink escapes so
// terminals that support them render clickable links. Terminals without OSC 8
// support ignore the escapes and show the URL text exactly as before.
func linkifyURLs(s string) string {
	return urlRe.ReplaceAllStringFunc(s, func(u string) string {
		// Keep trailing punctuation outside the clickable target.
		var trail string
		for len(u) > 0 {
			switch u[len(u)-1] {
			case ')', '.', ',', ';', ':', '!', '?', '\'', '"', ']', '}', '>':
				trail = string(u[len(u)-1]) + trail
				u = u[:len(u)-1]
				continue
			}
			break
		}
		if u == "" {
			return trail
		}
		return ansi.SetHyperlink(u) + u + ansi.ResetHyperlink() + trail
	})
}

// effectiveDetailSource resolves what the detail pane should render, falling
// back to the current case when the selected child pane is empty.
func (m model) effectiveDetailSource() focus {
	if m.phase != phaseCase {
		if m.detailSource == focusCases {
			return focusCases
		}
		return focusSpaces
	}
	switch m.detailSource {
	case focusSubcases:
		if m.scs.len() == 0 {
			return focusCurrentCase
		}
		return focusSubcases
	case focusFiles:
		if m.fl.len() == 0 {
			return focusCurrentCase
		}
		return focusFiles
	case focusNotes:
		if m.nt.len() == 0 {
			return focusCurrentCase
		}
		return focusNotes
	default:
		return focusCurrentCase
	}
}

func (m *model) detailParts() (key, header, md string) {
	switch m.effectiveDetailSource() {
	case focusCases:
		return m.caseDetailParts()
	case focusSubcases:
		return m.subcaseDetailParts()
	case focusFiles:
		return m.fileDetailParts()
	case focusNotes:
		return m.noteDetailParts()
	case focusCurrentCase:
		return m.currentCaseDetailParts()
	default:
		return m.spaceDetailParts()
	}
}

func (m *model) spaceDetailParts() (string, string, string) {
	s, ok := m.sp.current()
	if !ok {
		return "space|none", dimStyle.Render("Select a space."), ""
	}
	var chips []string
	if s.IsSystem {
		chips = append(chips, chip("system", cBrand))
	}
	if s.IsPublic {
		chips = append(chips, chip("public", cAccent))
	} else {
		chips = append(chips, chip("private", cDim))
	}
	header := breadcrumb(s.Name) + "\n" +
		strings.Join(chips, " ") + "\n" +
		metaLine(map[string]string{
			mCreated: relTime(s.CreatedAt),
			mUpdated: relTime(s.UpdatedAt),
		}, mCreated, mUpdated) + "\n" +
		dimStyle.Render(s.Id.String())
	return "space|" + s.Id.String(), header, ""
}

// caseDetailParts previews the selected browse case.
func (m *model) caseDetailParts() (string, string, string) {
	li, ok := m.cs.current()
	if !ok {
		return "case|none", dimStyle.Render("No case selected."), ""
	}
	return m.renderCaseDetail(li, "case", breadcrumb(spaceName(m), li.Title))
}

// currentCaseDetailParts renders the case being viewed in the case screen.
func (m *model) currentCaseDetailParts() (string, string, string) {
	li, ok := m.currentCaseItem()
	if !ok {
		return "curcase|none", dimStyle.Render("No case."), ""
	}
	return m.renderCaseDetail(li, "curcase", m.caseBreadcrumb())
}

func (m *model) subcaseDetailParts() (string, string, string) {
	li, ok := m.scs.current()
	if !ok {
		return "subcase|none", dimStyle.Render("No sub-cases."), ""
	}
	return m.renderCaseDetail(li, "subcase", m.childBreadcrumb(li.Title))
}

// renderCaseDetail builds the detail panel for any case-shaped item. The full
// object (description/summary) is fetched lazily and cached in caseDetail.
func (m *model) renderCaseDetail(li api.CaseListItem, kind, crumb string) (string, string, string) {
	id := li.Id.String()
	full := m.caseDetail[id]

	chips := []string{statusBadge(li.Status)}
	if li.Tags != nil {
		for _, t := range *li.Tags {
			chips = append(chips, chip(t, cAccent))
		}
	}
	meta := map[string]string{
		mCreated: relTime(li.CreatedAt),
		mUpdated: relTime(li.UpdatedAt),
	}
	order := []string{mCreated, mUpdated}
	if li.DueAt != nil {
		meta[mDue] = relTime(*li.DueAt)
		order = append(order, mDue)
	}
	if li.Assignee != nil {
		meta[mAssignee] = li.Assignee.Id.String()
		order = append(order, mAssignee)
	}
	header := crumb + "\n" + strings.Join(chips, " ") + "\n" + metaLine(meta, order...)

	var md strings.Builder
	if full != nil {
		if full.Summary != nil && strings.TrimSpace(*full.Summary) != "" {
			md.WriteString("**Summary** — " + *full.Summary + "\n\n")
		}
		if full.Description != nil && strings.TrimSpace(*full.Description) != "" {
			md.WriteString(*full.Description)
		}
		if md.Len() == 0 {
			md.WriteString("*No description.*")
		}
	} else {
		header += "\n" + mutedSt.Render(m.spin.View()+" loading details…")
	}
	return fmt.Sprintf("%s|%s|loaded=%v", kind, id, full != nil), header, md.String()
}

func (m *model) fileDetailParts() (string, string, string) {
	f, ok := m.fl.current()
	if !ok {
		return "file|none", dimStyle.Render("No files on this case."), ""
	}
	chips := []string{chip(f.MimeType, cAccent)}
	if f.Tags != nil {
		for _, t := range *f.Tags {
			chips = append(chips, chip(t, cBrand))
		}
	}
	header := m.childBreadcrumb(f.Name) + "\n" +
		strings.Join(chips, " ") + "\n" +
		metaLine(map[string]string{
			mSize:    humanSize(f.Size),
			mCreated: relTime(f.CreatedAt),
			mUpdated: relTime(f.UpdatedAt),
		}, mSize, mCreated, mUpdated) + "\n" +
		accentSt.Render("o") + mutedSt.Render("  open in browser")

	id := f.Id.String()
	if isImageMime(f.MimeType) {
		return m.fileImageDetail(id, header)
	}
	if textCandidate(f.MimeType, f.Name, f.Size) {
		return m.fileTextDetail(id, header)
	}
	return "file|" + id, header, ""
}

// fileImageDetail renders the image inline in the detail pane (Kitty graphics),
// or its loading/error/unsupported state. The image bytes are auto-loaded when
// the file is selected; the full-screen viewer is still available via ↵ / v.
func (m *model) fileImageDetail(id, header string) (string, string, string) {
	switch {
	case !imageSupported():
		header += "\n" + dimStyle.Render("image preview needs a graphics terminal (kitty/ghostty/wezterm)")
		return "file|" + id + "|img=unsupported", header, ""
	case m.imgErr[id] != nil:
		header += "\n" + errLine(m.imgErr[id], m.mdWidth)
		return "file|" + id + "|img=error", header, ""
	}
	img, ok := m.imgCache[id]
	if !ok {
		header += "\n" + mutedSt.Render(m.spin.View()+" loading image…")
		return "file|" + id + "|img=loading", header, ""
	}

	header += "\n" + accentSt.Render("↵ / v") + mutedSt.Render("  full screen")
	headerRows := strings.Count(header, "\n") + 1
	maxRows := m.vp.Height - headerRows - 1 // leave a blank line above the image
	maxCols := m.vp.Width
	if maxRows < 2 || maxCols < 4 {
		return "file|" + id + "|img=ready", header, "" // pane too short to inline
	}

	cols, rows := fitCells(img.w, img.h, maxCols, maxRows)
	imageID := kittyImageID(id)
	var b strings.Builder
	b.WriteByte('\n') // blank line between header and image
	kittyAppendVirtualImage(&b, img.png, imageID, cols, rows)
	b.WriteString(kittyPlaceholderGrid(imageID, cols, rows))
	key := fmt.Sprintf("fileimage|%s|%dx%d|pane=%dx%d", id, cols, rows, m.vp.Width, m.vp.Height)
	return key, header, b.String()
}

// fileTextDetail renders an inline text/eml preview, or its loading/error/
// binary state, for a file's panel.
func (m *model) fileTextDetail(id, header string) (string, string, string) {
	switch {
	case m.fileTextLoading[id]:
		header += "\n" + mutedSt.Render(m.spin.View()+" loading text…")
		return "file|" + id + "|txt=loading", header, ""
	case m.fileTextErr[id] != nil:
		header += "\n" + errLine(m.fileTextErr[id], m.mdWidth)
		return "file|" + id + "|txt=error", header, ""
	}
	tc, ok := m.fileText[id]
	switch {
	case ok && tc.binary:
		header += "\n" + dimStyle.Render("binary file — not shown")
		return "file|" + id + "|txt=binary", header, ""
	case ok:
		return "filetext|" + id, header, tc.body
	}
	return "file|" + id, header, ""
}

func (m *model) noteDetailParts() (string, string, string) {
	li, ok := m.nt.current()
	if !ok {
		return "note|none", dimStyle.Render("No notes on this case."), ""
	}
	id := li.Id.String()
	full := m.noteDetail[id]

	crumb := m.childBreadcrumb(li.Title)
	var chips []string
	if li.Tags != nil {
		for _, t := range *li.Tags {
			chips = append(chips, chip(t, cAccent))
		}
	}
	header := crumb
	if len(chips) > 0 {
		header += "\n" + strings.Join(chips, " ")
	}
	header += "\n" + metaLine(map[string]string{
		mCreated: relTime(li.CreatedAt),
		mUpdated: relTime(li.UpdatedAt),
	}, mCreated, mUpdated)

	var md string
	loaded := full != nil
	if loaded {
		if full.Body != nil {
			md = *full.Body
		}
		if strings.TrimSpace(md) == "" {
			md = "*This note has no body.*"
		}
	} else {
		header += "\n" + mutedSt.Render(m.spin.View()+" loading note…")
	}
	return fmt.Sprintf("note|%s|loaded=%v", id, loaded), header, md
}

// ---- small helpers ----

func spaceName(m *model) string {
	if s, ok := m.sp.current(); ok {
		return s.Name
	}
	return "—"
}

// currentCaseItem returns the case at the top of the stack (the one in view).
func (m model) currentCaseItem() (api.CaseListItem, bool) {
	if len(m.caseStack) == 0 {
		var zero api.CaseListItem
		return zero, false
	}
	return m.caseStack[len(m.caseStack)-1], true
}

// caseBreadcrumb is Space › root › … › current case.
func (m model) caseBreadcrumb() string {
	segs := make([]string, 0, len(m.caseStack)+1)
	segs = append(segs, spaceName(&m))
	for _, c := range m.caseStack {
		segs = append(segs, c.Title)
	}
	return breadcrumb(segs...)
}

// childBreadcrumb is the case breadcrumb with one more (child) segment.
func (m model) childBreadcrumb(title string) string {
	segs := make([]string, 0, len(m.caseStack)+2)
	segs = append(segs, spaceName(&m))
	for _, c := range m.caseStack {
		segs = append(segs, c.Title)
	}
	segs = append(segs, title)
	return breadcrumb(segs...)
}

func breadcrumb(segs ...string) string {
	parts := make([]string, len(segs))
	sep := dimStyle.Render(" › ")
	for i, s := range segs {
		if i == len(segs)-1 {
			parts[i] = titleStyle.Render(s)
		} else {
			parts[i] = mutedSt.Render(s)
		}
	}
	return strings.Join(parts, sep)
}

func chip(text string, c lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(c).
		Render("⟨" + text + "⟩")
}

func metaLine(kv map[string]string, order ...string) string {
	parts := make([]string, 0, len(order))
	for _, k := range order {
		v, ok := kv[k]
		if !ok || v == "" {
			continue
		}
		parts = append(parts, dimStyle.Render(k+" ")+mutedSt.Render(v))
	}
	return strings.Join(parts, dimStyle.Render("  ·  "))
}

func statusParts(s api.CaseStatus) (glyph string, glyphColor, textColor lipgloss.Color) {
	switch s {
	case api.Open:
		return "○", cOpen, cFg
	case api.Started:
		return "◐", cStarted, cFg
	case api.Blocked:
		return "⊘", cBlocked, cFg
	case api.Completed:
		return "✓", cCompleted, cMuted
	case api.Cancelled:
		return "✗", cCancelled, cDim
	default:
		return "•", cDim, cFg
	}
}

func lineLR(left, right string, w int) string {
	gap := w - lipglossWidth(left) - lipglossWidth(right)
	if gap < 1 {
		return truncate(left, max(0, w))
	}
	return left + strings.Repeat(" ", gap) + right
}

func humanSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	val := float64(n)
	units := []string{"KB", "MB", "GB", "TB"}
	i := -1
	for val >= 1024 && i < len(units)-1 {
		val /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", val, units[i])
}

func relTime(t time.Time) string {
	d := time.Since(t)
	suffix := "ago"
	if d < 0 {
		d = -d
		suffix = "from now"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm %s", int(d.Minutes()), suffix)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %s", int(d.Hours()), suffix)
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd %s", int(d.Hours()/24), suffix)
	default:
		return t.Format("2006-01-02")
	}
}
