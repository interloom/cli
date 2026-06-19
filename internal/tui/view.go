package tui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
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
	mAuthor   = "author"
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
		b := clamp(w*33/100, 16, 46) // notes & files
		c := w - a - b               // thread
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

// layoutHeader/layoutFooter/layoutMinBox are the fixed costs of the chrome. A
// bordered pane needs minBox lines (2 borders + a title + at least one row).
const (
	layoutHeaderH = 3
	layoutFooterH = 1
	layoutMinBox  = 4
)

func (m model) layout() layoutInfo {
	w, h := m.width, m.height
	colW := m.columnWidths(w)

	bodyH := h - layoutHeaderH - layoutFooterH
	if bodyH < 0 {
		bodyH = 0
	}

	// Partition the body between the list columns (top) and the detail pane
	// (bottom). Both are bordered boxes, so colsH+detailH must equal bodyH
	// exactly — otherwise the view renders more lines than the terminal has,
	// which scrolls the top off and desyncs the renderer (stale/duplicated
	// rows that only a resize clears).
	var colsH, detailH int
	if bodyH < 2*layoutMinBox {
		// Not enough room for two panes: give it all to the columns.
		colsH, detailH = bodyH, 0
	} else {
		colsH = bodyH * 45 / 100
		if colsH < layoutMinBox {
			colsH = layoutMinBox
		}
		if bodyH-colsH < layoutMinBox {
			colsH = bodyH - layoutMinBox
		}
		detailH = bodyH - colsH
	}

	li := layoutInfo{
		colW:      colW,
		colTop:    layoutHeaderH,
		colsH:     colsH,
		detailTop: layoutHeaderH + colsH,
		detailH:   detailH,
	}
	li.detailInnerW = max(10, w-4)
	li.detailInnerH = max(1, detailH-3)
	return li
}

// applyLayout resizes the viewport and (re)builds the markdown renderer.
func (m *model) applyLayout() {
	l := m.layout()
	m.vp.SetWidth(l.detailInnerW)
	m.vp.SetHeight(l.detailInnerH)
	if m.md == nil || m.mdWidth != l.detailInnerW {
		m.mdWidth = l.detailInnerW
		style := "light"
		if hasDarkBackground {
			style = "dark"
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStylePath(style),
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
	m.attachmentsOff = windowOffset(m.attachmentsCur, m.attachmentsOff, m.attachmentsLen(), rows)
	m.th.off = windowOffset(m.th.cur, m.th.off, m.th.len(), rows)
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
	mouse := msg.Mouse()
	switch mouse.Button {
	case tea.MouseWheelUp:
		return m.handleWheel(msg, l, -1)
	case tea.MouseWheelDown:
		return m.handleWheel(msg, l, 1)
	case tea.MouseLeft:
		return m.handleClick(msg, l)
	}
	return m, nil
}

// handleWheel scrolls the detail pane when hovering it, else moves the list
// under the cursor.
func (m model) handleWheel(msg tea.MouseMsg, l layoutInfo, dir int) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Y >= l.detailTop {
		if dir > 0 {
			m.vp.ScrollDown(2)
		} else {
			m.vp.ScrollUp(2)
		}
		return m, nil
	}
	if f, ok := m.columnAt(mouse.X, l); ok {
		m.setFocus(f)
		return m.moveCursor(dir)
	}
	return m, nil
}

// handleClick focuses the clicked pane and selects the clicked row.
func (m model) handleClick(msg tea.MouseMsg, l layoutInfo) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Y >= l.detailTop {
		m.setFocus(focusDetail)
		return m, m.refreshDetail()
	}
	f, ok := m.columnAt(mouse.X, l)
	if !ok {
		return m, nil
	}
	m.setFocus(f)

	visRows := l.colsH - 3
	row := mouse.Y - (l.colTop + 2) // y of the first item row
	if row < 0 || row >= visRows {
		// Clicking the pane (not a row) still loads its detail.
		cmd := m.focusLoadCmd()
		return m, tea.Batch(cmd, m.refreshDetail())
	}
	return m.selectRow(f, row, visRows, l)
}

func (m model) selectRow(f focus, row, visRows int, l layoutInfo) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch f {
	case focusSpaces:
		m.saveActiveStatusFilter()
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
	case focusAttachments:
		// Always (re)load the clicked row's preview: file previews aren't
		// auto-loaded on focus, so a click on the already selected file must
		// still trigger the load. previewLoad is idempotent.
		m.selectAttachmentAt(windowOffset(m.attachmentsCur, m.attachmentsOff, m.attachmentsLen(), visRows) + row)
		cmd = tea.Batch(m.maybeMoreAttachments(), m.previewLoad(focusAttachments))
	case focusThread:
		if m.th.selectAt(windowOffset(m.th.cur, m.th.off, m.th.len(), visRows) + row) {
			cmd = tea.Batch(m.maybeMoreThread(), m.previewLoad(focusThread))
		}
	}
	m.syncOffsets(l)
	return m, tea.Batch(cmd, m.refreshDetail())
}

// ---- view ----

func (m model) View() tea.View {
	v := tea.NewView(m.viewString())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) viewString() string {
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
	if l.colsH < 3 {
		// Too short to render a usable pane; show a centered notice that fills
		// the screen exactly (no overflow).
		msg := mutedSt.Render("⌜ terminal too small ⌟")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	parts := []string{m.renderHeader(), m.renderColumns(l)}
	if l.detailH > 0 { // the detail pane is dropped when the body is too short
		bottom := m.renderDetail(l)
		if m.debug {
			bottom = m.renderDebugPanel(l)
		}
		parts = append(parts, bottom)
	}
	parts = append(parts, m.renderFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// ---- intro animation helpers ----
//
// The splash is fully time-based: every element is a continuous function of the
// elapsed time t (seconds), so motion stays smooth at any frame rate and loops
// seamlessly until the user presses Enter.

// clamp01 constrains x to the unit interval [0, 1].
func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}

// easeOutCubic eases a 0→1 progress so motion decelerates into place.
func easeOutCubic(t float64) float64 {
	u := 1 - t
	return 1 - u*u*u
}

// gradientHex interpolates across evenly-spaced "#RRGGBB" stops at t in [0,1].
func gradientHex(stops []string, t float64) string {
	switch {
	case t <= 0:
		return stops[0]
	case t >= 1:
		return stops[len(stops)-1]
	}
	seg := t * float64(len(stops)-1)
	i := int(seg)
	if i >= len(stops)-1 {
		return stops[len(stops)-1]
	}
	return lerpHex(stops[i], stops[i+1], seg-float64(i))
}

// lerpHex linearly interpolates between two "#RRGGBB" colors at t in [0,1].
func lerpHex(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	lerp := func(x, y int) int { return int(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return fmt.Sprintf("#%02X%02X%02X", lerp(ar, br), lerp(ag, bg), lerp(ab, bb))
}

// hexRGB splits a "#RRGGBB" color into its 8-bit components.
func hexRGB(s string) (r, g, b int) {
	v, _ := strconv.ParseInt(strings.TrimPrefix(s, "#"), 16, 32)
	return int(v>>16) & 0xFF, int(v>>8) & 0xFF, int(v) & 0xFF
}

// glyphStyle paints a single bold splash glyph in the given hex color.
func glyphStyle(hex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true)
}

// renderIntro draws the animated startup splash, centered in the terminal: the
// logo fades up with a sheen sweeping across it, the wordmark resolves letter by
// letter with a looping shimmer, an accent rule grows out, and the tagline and
// loading hint ease in — all continuous functions of elapsed time.
func (m model) renderIntro() string {
	t := float64(m.introFrame) * introInterval.Seconds()
	wordmark, wordW := renderWordmark(t)
	block := lipgloss.JoinVertical(
		lipgloss.Center,
		renderLogoSheen(t), "",
		wordmark, renderRule(t, wordW), "",
		renderTagline(t), "",
		m.introLoading(t),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, block)
}

// renderLogoSheen draws the half-block logo as a steady violet that eases up
// from dark on first paint, with a faint highlight slowly drifting across it.
// Every line is padded to the logo's full width so it centers as one block.
func renderLogoSheen(t float64) string {
	lines := strings.Split(logoArt, "\n")
	w := 0
	for _, ln := range lines {
		if n := len([]rune(ln)); n > w {
			w = n
		}
	}
	rev := easeOutCubic(clamp01(t / 1.1)) // gentle fade-in envelope
	const period = 5.0
	hx := -0.3 + math.Mod(t, period)/period*1.6 // highlight center drifts across with margins
	out := make([]string, len(lines))
	for li, ln := range lines {
		runes := []rune(ln)
		var b strings.Builder
		for x := 0; x < w; x++ {
			if x >= len(runes) || runes[x] == ' ' {
				b.WriteByte(' ')
				continue
			}
			base := gradientHex([]string{"#2E2E4A", cBrandDark}, rev) // dark → brand fade-in
			d := float64(x)/float64(w-1) - hx
			hi := math.Exp(-(d * d) / (2 * 0.1 * 0.1)) // defined highlight band
			hex := lerpHex(base, "#E6DEFF", 0.85*hi*rev)
			b.WriteString(glyphStyle(hex).Render(string(runes[x])))
		}
		out[li] = b.String()
	}
	return strings.Join(out, "\n")
}

// wordRevealStops emerge each letter from dim, with a faint glow, then settle.
var wordRevealStops = []string{cBorderDark, cBrandHiDark, cBrandDark}

// renderWordmark resolves "interloom" letter by letter out of dim dots; once
// settled a faint highlight drifts across it on a slow loop. Unrevealed letters
// render as dots so the wordmark never changes width (no horizontal jitter).
func renderWordmark(t float64) (string, int) {
	letters := []rune("interloom")
	const start, per, fade = 0.7, 0.1, 0.4
	settle := start + float64(len(letters)-1)*per + fade

	sweepPos := -5.0 // off-screen until the shimmer begins
	if t > settle+0.5 {
		const sweepPeriod = 6.0
		sweepPos = math.Mod(t-settle-0.5, sweepPeriod)/sweepPeriod*float64(len(letters)+6) - 3
	}

	var b strings.Builder
	for i, r := range letters {
		if i > 0 {
			b.WriteByte(' ')
		}
		lp := clamp01((t - start - float64(i)*per) / fade)
		if lp <= 0 {
			b.WriteString(dimStyle.Render("·"))
			continue
		}
		hex := gradientHex(wordRevealStops, lp)
		if lp >= 1 {
			d := float64(i) - sweepPos
			sh := math.Exp(-(d * d) / (2 * 0.9 * 0.9))
			hex = lerpHex(cBrandDark, "#E6DEFF", 0.9*sh)
		}
		b.WriteString(glyphStyle(hex).Render(string(r)))
	}
	s := b.String()
	return s, lipglossWidth(s)
}

// renderRule grows an accent underline out from the center to the wordmark width.
func renderRule(t float64, wordW int) string {
	const start, dur = 1.0, 0.6
	p := easeOutCubic(clamp01((t - start) / dur))
	n := int(math.Round(p * float64(wordW)))
	if n <= 0 {
		return ""
	}
	hex := gradientHex([]string{cBorderDark, cBrandDark}, p)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(strings.Repeat("─", n))
}

// renderTagline fades the tagline up from the background once the mark has set.
func renderTagline(t float64) string {
	const start, dur = 1.45, 0.6
	p := clamp01((t - start) / dur)
	if p <= 0 {
		return ""
	}
	hex := gradientHex([]string{cBorderDark, cDimDark, "#9CA3AF"}, p)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("Supercharged Operations")
}

// introLoading renders the splash's bottom status line: a spinner while the
// workspace loads, then a softly breathing "press enter" hint once it is ready.
func (m model) introLoading(t float64) string {
	if t < 0.5 {
		return ""
	}
	if m.sp.loading {
		return m.spin.View() + mutedSt.Render(" loading workspace…")
	}
	v := 0.5 - 0.5*math.Cos(t*2.2) // continuous grey breathing
	hex := gradientHex([]string{"#4B5563", "#9CA3AF"}, v)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("press enter to continue")
}

func (m model) renderHeader() string {
	left := titleStyle.Render(logoMark + " interloom")
	if m.cfgName != "" {
		left += dimStyle.Render(" · ") + accentSt.Render(m.cfgName)
	}
	if m.phase == phaseCase {
		left += "  " + m.caseBreadcrumb()
	}
	content := lineLR(left, m.headerStatus(), m.width-4)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBrand).
		Width(m.width).
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
			title: "CASES", titleSuffix: m.caseFilterTitleSuffix(), focused: m.focus == focusCases, w: w, colsH: colsH,
			count: m.cs.len(), off: off, list: m.cs.meta(), emptyMsg: m.casesEmptyMsg(),
		}, func(i, w int) string {
			cs, _ := m.cs.at(i)
			glyph, gc, tc := statusParts(cs.Status)
			return m.renderCaseRow(cs, glyph, gc, tc, i == m.cs.cur, w)
		})
	case focusSubcases:
		off := windowOffset(m.scs.cur, m.scs.off, m.scs.len(), rows)
		return m.renderColumn(colSpec{
			title: "SUB-CASES", titleSuffix: m.caseFilterTitleSuffix(), focused: m.focus == focusSubcases, w: w, colsH: colsH,
			count: m.scs.len(), off: off, list: m.scs.meta(), emptyMsg: m.subcasesEmptyMsg(),
		}, func(i, w int) string {
			cs, _ := m.scs.at(i)
			glyph, gc, tc := statusParts(cs.Status)
			return m.renderCaseRow(cs, glyph, gc, tc, i == m.scs.cur, w)
		})
	case focusAttachments:
		off := windowOffset(m.attachmentsCur, m.attachmentsOff, m.attachmentsLen(), rows)
		return m.renderColumn(colSpec{
			title: "NOTES & FILES", focused: m.focus == focusAttachments, w: w, colsH: colsH,
			count: m.attachmentsLen(), off: off, list: m.attachmentsMeta(), emptyMsg: "No notes or files",
		}, func(i, w int) string {
			row, _ := m.attachmentAt(i)
			if row.kind == attachmentNote {
				return renderRow("▪", cBrandHi, row.note.Title, cFg, i == m.attachmentsCur, w)
			}
			return renderRow("▤", cAccent, row.file.Name, cFg, i == m.attachmentsCur, w)
		})
	case focusThread:
		off := windowOffset(m.th.cur, m.th.off, m.th.len(), rows)
		return m.renderColumn(colSpec{
			title: "THREAD", focused: m.focus == focusThread, w: w, colsH: colsH,
			count: m.th.len(), off: off, list: m.th.meta(), emptyMsg: "No thread messages",
		}, func(i, w int) string {
			tm, _ := m.th.at(i)
			text := strings.TrimSpace(firstLine(m.resolveMentions(tm.msg.Text)))
			if text == "" {
				text = "(empty message)"
			}
			return renderRow("✉", cBrandHi, text, cFg, i == m.th.cur, w)
		})
	}
	return ""
}

// attachmentsMeta synthesizes list metadata for the combined pane from its two
// backing lists: loading while either first page is in flight with no rows yet,
// and "more" available when either segment has more pages.
func (m model) attachmentsMeta() listMeta {
	count := m.attachmentsLen()
	return listMeta{
		loading:     count == 0 && (m.nt.loading || m.fl.loading),
		loadingMore: m.nt.loadingMore || m.fl.loadingMore,
		hasMore:     m.nt.hasMore || m.fl.hasMore,
	}
}

// mentionRe matches the mention tokens embedded in thread messages, e.g.
// <@U0192d8ea-2009-789a-9df2-8d903e4a4fdb>. The leading letter is the entity
// type (U = user); the remainder is the referenced object's UUID.
var mentionRe = regexp.MustCompile(`<@[A-Za-z]?([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})>`)

// resolveMentions rewrites user mention tokens as @Name using the loaded user
// directory, leaving unknown or non-user mentions untouched.
func (m model) resolveMentions(s string) string {
	if !strings.Contains(s, "<@") {
		return s
	}
	return mentionRe.ReplaceAllStringFunc(s, func(tok string) string {
		sub := mentionRe.FindStringSubmatch(tok)
		if sub == nil {
			return tok
		}
		if u, ok := m.users[strings.ToLower(sub[1])]; ok {
			if name := strings.TrimSpace(u.Name); name != "" {
				return "@" + name
			}
		}
		return tok
	})
}

// Sentinels used to mark resolved mentions so they can be recolored after the
// markdown renderer runs. They are private-use code points that survive glamour
// untouched; the interior space is a non-breaking space so word-wrap keeps a
// multi-word @Name on a single line (letting one regex recolor the whole span).
const (
	mentionOpen  = "\ue000"
	mentionClose = "\ue001"
	nbsp         = "\u00a0"
)

var mentionSpanRe = regexp.MustCompile(mentionOpen + "([^" + mentionClose + "]*)" + mentionClose)

// resolveMentionsMarked resolves mentions like resolveMentions but wraps each
// resolved @Name in sentinels (and joins its words with NBSP) so the rendered
// detail body can highlight the mention via highlightMentions / stripMentionMarks.
func (m model) resolveMentionsMarked(s string) string {
	if !strings.Contains(s, "<@") {
		return s
	}
	return mentionRe.ReplaceAllStringFunc(s, func(tok string) string {
		sub := mentionRe.FindStringSubmatch(tok)
		if sub == nil {
			return tok
		}
		if u, ok := m.users[strings.ToLower(sub[1])]; ok {
			if name := strings.TrimSpace(u.Name); name != "" {
				return mentionOpen + "@" + strings.ReplaceAll(name, " ", nbsp) + mentionClose
			}
		}
		return tok
	})
}

// highlightMentions recolors sentinel-wrapped mention spans with mentionSt,
// restoring the spaces inside multi-word names. It is a no-op when no marked
// mentions are present, so it is safe to call on any rendered body.
func highlightMentions(s string) string {
	if !strings.Contains(s, mentionOpen) {
		return s
	}
	return mentionSpanRe.ReplaceAllStringFunc(s, func(span string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(span, mentionOpen), mentionClose)
		return mentionSt.Render(strings.ReplaceAll(name, nbsp, " "))
	})
}

// stripMentionMarks removes mention sentinels, leaving plain @Name text. Used
// for raw/unstyled bodies (and as a safety net) so sentinels never leak to the
// screen as invisible glyphs.
func stripMentionMarks(s string) string {
	if !strings.Contains(s, mentionOpen) {
		return s
	}
	return mentionSpanRe.ReplaceAllStringFunc(s, func(span string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(span, mentionOpen), mentionClose)
		return strings.ReplaceAll(name, nbsp, " ")
	})
}

// firstLine returns the first non-empty line of s, collapsing internal runs of
// whitespace so a multi-line message renders as a single tidy row.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return strings.Join(strings.Fields(t), " ")
		}
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

func (m model) caseFilterTitleSuffix() string {
	if m.statusFilter == "" {
		return ""
	}
	return dimStyle.Render("filter ") + statusBadge(m.statusFilter)
}

// colSpec carries the pagination-aware metadata needed to render a list pane.
type colSpec struct {
	title       string
	titleSuffix string
	focused     bool
	w, colsH    int
	count       int
	off         int
	list        listMeta
	emptyMsg    string
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
	title := ts.Render(c.title)
	if c.titleSuffix != "" {
		title += " " + c.titleSuffix
	}
	titleLine := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).
		Render(title + " " + dimStyle.Render(m.colCount(c)))

	lines := m.columnLines(c, rowFn, innerW, visRows)
	for len(lines) < visRows {
		lines = append(lines, strings.Repeat(" ", innerW))
	}

	content := titleLine + "\n" + strings.Join(lines, "\n")
	return bs.Width(c.w).Height(c.colsH).Render(content)
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
// Rows arrive padded with trailing spaces, so an ellipsis is only added when
// real content (not padding) is clipped — otherwise the trim is silent.
func overlayRight(line, cue string, w int) string {
	if w <= 0 {
		return ""
	}
	cueW := lipglossWidth(cue)
	if cueW >= w {
		return truncate(cue, w)
	}
	avail := w - cueW - 1 // reserve at least one space before the cue
	var left string
	if lipglossWidth(strings.TrimRight(ansi.Strip(line), " ")) > avail {
		left = truncate(line, avail) // real content clipped → ellipsis
	} else {
		left = ansi.Truncate(line, avail, "") // only padding clipped → silent
	}
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
func renderRow(glyph string, glyphColor terminalColor, text string, textColor terminalColor, selected bool, w int) string {
	avail := max(0, w-4) // pointer(2) + glyph(1) + space(1)
	text = truncate(text, avail)
	used := 4 + lipglossWidth(text)
	pad := max(0, w-used)

	if selected {
		base := lipgloss.NewStyle().Foreground(cOnColor).Background(cBg).Bold(true)
		g := lipgloss.NewStyle().Foreground(cOnColor).Background(cBg)
		return base.Render("▸ ") + g.Render(glyph) + base.Render(" "+text+strings.Repeat(" ", pad))
	}
	g := lipgloss.NewStyle().Foreground(glyphColor)
	t := lipgloss.NewStyle().Foreground(textColor)
	return "  " + g.Render(glyph) + " " + t.Render(text) + strings.Repeat(" ", pad)
}

// renderCaseRow adds the assignee's name at the right edge while preserving the
// fixed-width row contract used by the pane renderer.
func (m model) renderCaseRow(cs api.CaseListItem, glyph string, glyphColor, textColor terminalColor, selected bool, w int) string {
	name := m.assigneeName(cs.Assignee)
	if name == "" {
		return renderRow(glyph, glyphColor, cs.Title, textColor, selected, w)
	}

	// Keep enough room for the case title in narrow panes. Assignee names are
	// secondary information, so they use at most one third of the row.
	name = truncate(name, max(1, w/3))
	nameStyle := lipgloss.NewStyle().Foreground(cMuted)
	gapStyle := lipgloss.NewStyle()
	if selected {
		nameStyle = nameStyle.Background(cBg)
		gapStyle = gapStyle.Background(cBg)
	}
	renderedName := nameStyle.Render(name)
	baseW := max(0, w-lipglossWidth(renderedName)-1)
	return renderRow(glyph, glyphColor, cs.Title, textColor, selected, baseW) + gapStyle.Render(" ") + renderedName
}

func (m model) assigneeName(link *api.ResourceLink) string {
	if link == nil {
		return ""
	}
	if user, ok := m.users[link.Id.String()]; ok {
		return strings.TrimSpace(user.Name)
	}
	return ""
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
	return bs.Width(m.width).Height(l.detailH).Padding(0, 1).Render(content)
}

func (m model) detailLabel() string {
	switch m.effectiveDetailSource() {
	case focusSpaces:
		return "SPACE"
	case focusCases:
		return "CASE"
	case focusSubcases:
		return "SUB-CASE"
	case focusAttachments:
		if row, ok := m.currentAttachment(); ok && row.kind == attachmentFile {
			return "FILE"
		}
		return "NOTE"
	case focusThread:
		return "MESSAGE"
	default:
		return "CASE"
	}
}

func (m model) renderFooter() string {
	hints := [][2]string{{"↑/↓", "move"}, {"←/→", "panes"}}
	switch {
	case m.phase != phaseCase:
		hints = append(hints, [2]string{keyEnter, "open"})
	case m.focus == focusAttachments || m.focus == focusThread:
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
		[2]string{keyCtrlC, "quit"},
	)
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, helpKey.Render(h[0])+" "+helpDesc.Render(h[1]))
	}
	// Keep the footer to a single line: truncating (rather than wrapping to a
	// second line) preserves the fixed footer height the layout assumes.
	line := " " + strings.Join(parts, helpSep)
	return truncate(line, m.width)
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

func (m *model) refreshDetail() tea.Cmd {
	m.inlineImageRaw = ""
	key, header, md := m.detailParts()
	combined := fmt.Sprintf("%s|raw=%v|w=%d", key, m.rawMode, m.mdWidth)
	if combined == m.lastDetailKey {
		return nil
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

	// Any mention sentinels still present (raw mode, or markdown unavailable)
	// are stripped to plain @Name so they never surface as invisible glyphs.
	body = stripMentionMarks(body)

	content := header
	if body != "" {
		content += "\n" + body
	}
	m.vp.SetContent(content)
	m.vp.GotoTop()
	if m.inlineImageRaw != "" {
		return tea.Raw(m.inlineImageRaw)
	}
	return nil
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
	return highlightMentions(linkifyURLs(strings.TrimRight(out, "\n"))), true
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
	case focusAttachments:
		if m.attachmentsLen() == 0 {
			return focusCurrentCase
		}
		return focusAttachments
	case focusThread:
		if m.th.len() == 0 {
			return focusCurrentCase
		}
		return focusThread
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
	case focusAttachments:
		return m.attachmentDetailParts()
	case focusThread:
		return m.threadDetailParts()
	case focusCurrentCase:
		return m.currentCaseDetailParts()
	default:
		return m.spaceDetailParts()
	}
}

// attachmentDetailParts renders the combined pane's selected note or file.
func (m *model) attachmentDetailParts() (string, string, string) {
	row, ok := m.currentAttachment()
	if !ok {
		return "attachments|none", dimStyle.Render("No notes or files on this case."), ""
	}
	if row.kind == attachmentNote {
		return m.noteDetailPartsFor(row.note)
	}
	return m.fileDetailPartsFor(row.file)
}

// threadDetailParts renders the selected thread message: author + timestamps in
// the header and the message text (markdown) as the body.
func (m *model) threadDetailParts() (string, string, string) {
	row, ok := m.th.current()
	if !ok {
		return "thread|none", dimStyle.Render("No thread messages."), ""
	}
	author := row.event.Author.Id.String()
	if u, ok := m.users[row.event.Author.Id.String()]; ok && strings.TrimSpace(u.Name) != "" {
		author = u.Name
	}
	header := m.caseBreadcrumb() + "\n" +
		metaLine(map[string]string{
			mAuthor:  author,
			mCreated: relTime(row.event.CreatedAt),
		}, mAuthor, mCreated) + "\n" +
		dimStyle.Render(row.event.Id.String())

	md := m.resolveMentionsMarked(row.msg.Text)
	if strings.TrimSpace(md) == "" {
		md = "*Empty message.*"
	}
	key := fmt.Sprintf("thread|%s|payload=%d", row.event.Id.String(), row.payloadIndex)
	return key, header, md
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
		if user, ok := m.users[li.Assignee.Id.String()]; ok {
			meta[mAssignee] = user.Name
		}
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

// fileDetailPartsFor builds the detail panel for a specific file (selected in
// the combined notes+files pane).
func (m *model) fileDetailPartsFor(f api.File) (string, string, string) {
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
	maxRows := m.vp.Height() - headerRows - 1 // leave a blank line above the image
	maxCols := m.vp.Width()
	if maxRows < 2 || maxCols < 4 {
		return "file|" + id + "|img=ready", header, "" // pane too short to inline
	}

	cols, rows := fitCells(img.w, img.h, maxCols, maxRows)
	imageID := kittyImageID(id)
	var raw strings.Builder
	kittyAppendVirtualImage(&raw, img.png, imageID, cols, rows)
	m.inlineImageRaw = raw.String()

	var b strings.Builder
	b.WriteByte('\n') // blank line between header and image
	b.WriteString(kittyPlaceholderGrid(imageID, cols, rows))
	key := fmt.Sprintf("fileimage|%s|%dx%d|pane=%dx%d", id, cols, rows, m.vp.Width(), m.vp.Height())
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

// noteDetailPartsFor builds the detail panel for a specific note (selected in
// the combined notes+files pane). The full body is fetched lazily and cached.
func (m *model) noteDetailPartsFor(li api.NoteListItem) (string, string, string) {
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

func chip(text string, c terminalColor) string {
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

func statusParts(s api.CaseStatus) (glyph string, glyphColor, textColor terminalColor) {
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
