package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// pageSize is how many items a single list request fetches.
const pageSize = 30

// loadThreshold prefetches the next page once the cursor is within this many
// rows of the end of the loaded items.
const loadThreshold = 6

// listKind identifies which pane a page response belongs to. Cases and
// sub-cases share the same item type, so the kind disambiguates them.
type listKind int

const (
	kindSpaces listKind = iota
	kindCases
	kindSubcases
	kindFiles
	kindNotes
)

// paged is the cursor-paginated, lazily-loaded state for one list pane. The
// generic parameter is the list item type (space/case/note list item).
//
// Items are fetched and stored in server order; any filtering (e.g. the case
// status filter) happens server-side, so the loaded items are exactly what the
// pane shows and the cursor/offset operate directly on them.
type paged[T any] struct {
	items    []T // all fetched items, in server order
	cur, off int // cursor/scroll offset into the list

	cursor  string // server cursor for the next page ("" when exhausted)
	hasMore bool

	loading     bool // the first page is in flight
	loadingMore bool // a subsequent page is in flight

	parentID string // the space/case these items belong to ("" for spaces)

	pagesFetched int // pages folded in since the last reset (bounds auto-fill)

	// gen rises on every request so stale responses can be dropped, and cancel
	// aborts the in-flight request when a newer one supersedes it.
	gen    int
	cancel context.CancelFunc
}

// len is the number of loaded items.
func (p *paged[T]) len() int {
	return len(p.items)
}

// at returns the i-th item, if it exists.
func (p *paged[T]) at(i int) (T, bool) {
	if i >= 0 && i < len(p.items) {
		return p.items[i], true
	}
	var zero T
	return zero, false
}

// reset cancels any in-flight load and clears the list for a new parent.
// Bumping gen makes the reset itself an invalidation event, so any response
// already in flight is rejected even if no replacement request follows.
func (p *paged[T]) reset(parentID string) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.gen++
	p.items = nil
	p.cur, p.off = 0, 0
	p.cursor = ""
	p.hasMore = false
	p.loading = false
	p.loadingMore = false
	p.parentID = parentID
	p.pagesFetched = 0
}

// move shifts the cursor and reports whether it actually changed.
func (p *paged[T]) move(delta int) bool {
	old := p.cur
	p.cur = clamp(p.cur+delta, 0, p.len()-1)
	return p.cur != old
}

func (p *paged[T]) jump(toStart bool) {
	if toStart {
		p.cur = 0
		return
	}
	p.cur = max(0, p.len()-1)
}

// selectAt moves the cursor to idx when it is a valid, different row; reports
// whether the selection actually changed.
func (p *paged[T]) selectAt(idx int) bool {
	if idx >= 0 && idx < p.len() && idx != p.cur {
		p.cur = idx
		return true
	}
	return false
}

// current returns the selected (visible) item, if any.
func (p *paged[T]) current() (T, bool) {
	return p.at(p.cur)
}

// needMore is true when the cursor is near the end of the visible list and
// another page exists. With a filter active, scrolling near the end pulls more
// raw pages so the filtered list can keep growing.
func (p *paged[T]) needMore() bool {
	return p.hasMore && !p.loadingMore && !p.loading && p.cur >= p.len()-loadThreshold
}

// setLoading marks the list as loading a first page or a subsequent page,
// clearing the cursor/hasMore when starting fresh.
func (p *paged[T]) setLoading(appendPage bool) {
	if appendPage {
		p.loadingMore = true
		return
	}
	p.loading = true
	p.cursor, p.hasMore = "", false
}

// applyPage folds a fetched page into the list state.
func (p *paged[T]) applyPage(items []T, cursor string, hasMore, appendPage bool) {
	p.loading = false
	p.loadingMore = false
	if appendPage {
		p.items = append(p.items, items...)
	} else {
		p.items = items
		p.cur, p.off = 0, 0
		p.pagesFetched = 0
	}
	p.pagesFetched++
	p.cursor = cursor
	p.hasMore = hasMore
}

// pageFetcher loads a single page given a cursor. Returns items, the next
// cursor, and whether more pages remain.
type pageFetcher[T any] func(ctx context.Context, cursor string) (items []T, next string, hasMore bool, err error)

// pageResult is the message produced by a page load. Spaces and notes each map
// to a distinct instantiation; cases and sub-cases share one, so kind tells
// them apart in the Update switch.
type pageResult[T any] struct {
	kind       listKind
	gen        int
	reqID      int // debug-log entry this page belongs to (0 == untracked)
	parentID   string
	items      []T
	cursor     string
	hasMore    bool
	appendPage bool
	err        error
}

// loadPage builds a command that fetches one page and reports a pageResult.
func loadPage[T any](ctx context.Context, kind listKind, gen, reqID int, parentID, cursor string, appendPage bool, fetch pageFetcher[T]) tea.Cmd {
	return func() tea.Msg {
		items, next, hasMore, err := fetch(ctx, cursor)
		return pageResult[T]{
			kind:       kind,
			gen:        gen,
			reqID:      reqID,
			parentID:   parentID,
			items:      items,
			cursor:     next,
			hasMore:    hasMore,
			appendPage: appendPage,
			err:        err,
		}
	}
}
