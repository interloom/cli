package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// maxReqLog bounds the debug request log so a long session can't grow without
// limit; the oldest entries are dropped first.
const maxReqLog = 100

type reqStatus int

const (
	reqPending reqStatus = iota
	reqOK
	reqError
	reqCanceled
)

// reqEntry is one tracked API request shown in the debug panel.
type reqEntry struct {
	id      int
	label   string
	note    string // extra detail, e.g. "30 items +more" for a page load
	started time.Time
	elapsed time.Duration
	status  reqStatus
	errMsg  string
}

// logStart records a new in-flight request and returns its id. The id is
// threaded through the load command so logFinish can resolve it on completion.
func (m *model) logStart(label string) int {
	m.reqSeq++
	id := m.reqSeq
	m.reqLog = append(m.reqLog, reqEntry{
		id:      id,
		label:   label,
		started: time.Now(),
		status:  reqPending,
	})
	if len(m.reqLog) > maxReqLog {
		m.reqLog = m.reqLog[len(m.reqLog)-maxReqLog:]
	}
	return id
}

// logFinish resolves a tracked request by id, recording how long it took and
// whether it succeeded, errored, or was canceled (e.g. superseded by a newer
// request). Untracked ids (0) and already-trimmed entries are ignored.
func (m *model) logFinish(id int, err error) {
	m.logFinishNote(id, err, "")
}

// logFinishNote is logFinish with an extra detail string (e.g. the number of
// items a page returned and whether more pages remain).
func (m *model) logFinishNote(id int, err error, note string) {
	if id == 0 {
		return
	}
	for i := range m.reqLog {
		if m.reqLog[i].id != id {
			continue
		}
		m.reqLog[i].elapsed = time.Since(m.reqLog[i].started)
		m.reqLog[i].note = note
		switch {
		case err == nil:
			m.reqLog[i].status = reqOK
		case errors.Is(err, context.Canceled):
			m.reqLog[i].status = reqCanceled
		default:
			m.reqLog[i].status = reqError
			m.reqLog[i].errMsg = err.Error()
		}
		return
	}
}

// pageNote summarizes a page response for the debug log.
func pageNote(n int, hasMore bool) string {
	if hasMore {
		return fmt.Sprintf("%d items +more", n)
	}
	return fmt.Sprintf("%d items · end", n)
}

// pageLabel builds a debug label for a list page request, e.g. "cases:019565da"
// or "notes:019565da · page 2".
func pageLabel(name, parentID string, appendPage bool, pagesFetched int) string {
	label := name
	if parentID != "" {
		label += ":" + shortID(parentID)
	}
	if appendPage {
		label += fmt.Sprintf(" · page %d", pagesFetched+1)
	}
	return label
}

// shortID is the leading segment of a UUID, enough to tell requests apart.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// fmtDur formats a request duration compactly (µs / ms / s).
func fmtDur(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// renderDebugPanel draws the request log in place of the detail pane, newest
// first, with a live pending/total summary. Pending rows show their elapsed
// time so far and update on every spinner tick.
func (m model) renderDebugPanel(l layoutInfo) string {
	pending := 0
	for i := range m.reqLog {
		if m.reqLog[i].status == reqPending {
			pending++
		}
	}
	title := colTitleFocus.Render("REQUEST LOG")
	summary := mutedSt.Render(fmt.Sprintf("%d pending · %d total", pending, len(m.reqLog)))
	titleLine := lineLR(title, summary, l.detailInnerW)

	rows := l.detailInnerH
	lines := make([]string, 0, rows)
	for i := len(m.reqLog) - 1; i >= 0 && len(lines) < rows; i-- {
		lines = append(lines, m.renderReqEntry(m.reqLog[i], l.detailInnerW))
	}
	if len(lines) == 0 {
		lines = append(lines, dimStyle.Italic(true).Render("No requests yet."))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}

	content := titleLine + "\n" + strings.Join(lines, "\n")
	return boxFocusStyle.Width(m.width-2).Height(l.detailH-2).MaxHeight(l.detailH).Padding(0, 1).Render(content)
}

// renderReqEntry renders one request-log row: timestamp, status glyph, label,
// optional error, and duration (live for pending requests).
func (m model) renderReqEntry(e reqEntry, w int) string {
	var (
		glyph string
		gc    lipgloss.Color
		dur   string
	)
	switch e.status {
	case reqPending:
		glyph, gc = "◌", cStarted
		dur = fmtDur(time.Since(e.started))
	case reqOK:
		glyph, gc = "✓", cCompleted
		dur = fmtDur(e.elapsed)
	case reqError:
		glyph, gc = "✗", cBlocked
		dur = fmtDur(e.elapsed)
	case reqCanceled:
		glyph, gc = "⊘", cDim
		dur = fmtDur(e.elapsed)
	}

	left := dimStyle.Render(e.started.Format("15:04:05")) + " " +
		lipgloss.NewStyle().Foreground(gc).Render(glyph) + " " +
		lipgloss.NewStyle().Foreground(cFg).Render(e.label)
	switch {
	case e.status == reqError && e.errMsg != "":
		left += "  " + lipgloss.NewStyle().Foreground(cBlocked).Render(e.errMsg)
	case e.note != "":
		left += "  " + dimStyle.Render(e.note)
	}
	return lineLR(left, mutedSt.Render(dur), w)
}
