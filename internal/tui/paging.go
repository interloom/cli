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

// maxFilterPages caps how many pages auto-fill will pull while a status filter
// is leaving the pane empty, so a rare status can't page through everything.
const maxFilterPages = 10

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
// Items are fetched and stored raw (unfiltered). An optional client-side
// filter defines the *visible* subset: view holds the indices of items that
// pass it, and the cursor/offset and all navigation operate on that visible
// list. Changing the filter only rebuilds view — it never refetches — so the
// status filter stays responsive while paging through a long list.
type paged[T any] struct {
	items    []T          // all fetched items, in server order (raw)
	filter   func(T) bool // nil == show everything
	view     []int        // indices into items that pass filter (filter != nil)
	cur, off int          // cursor/scroll offset into the visible list

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

// len is the number of *visible* items (after the filter).
func (p *paged[T]) len() int {
	if p.filter == nil {
		return len(p.items)
	}
	return len(p.view)
}

// at returns the i-th visible item (mapping through the filter view).
func (p *paged[T]) at(i int) (T, bool) {
	var zero T
	if p.filter == nil {
		if i >= 0 && i < len(p.items) {
			return p.items[i], true
		}
		return zero, false
	}
	if i >= 0 && i < len(p.view) {
		return p.items[p.view[i]], true
	}
	return zero, false
}

// rebuildView recomputes the visible index set from items and the filter.
func (p *paged[T]) rebuildView() {
	if p.filter == nil {
		p.view = nil
		return
	}
	p.view = p.view[:0]
	for i := range p.items {
		if p.filter(p.items[i]) {
			p.view = append(p.view, i)
		}
	}
}

// setFilter swaps the filter, rebuilds the visible view and resets browsing to
// the top of the new view (cur/off are coordinates in the filtered view, so a
// filter change invalidates them). It does not touch the fetched items, so no
// network request is needed.
func (p *paged[T]) setFilter(f func(T) bool) {
	p.filter = f
	p.rebuildView()
	p.cur, p.off = 0, 0
}

// reset cancels any in-flight load and clears the list for a new parent.
// Bumping gen makes the reset itself an invalidation event, so any response
// already in flight is rejected even if no replacement request follows. The
// filter is preserved so a fresh list keeps the active status filter.
func (p *paged[T]) reset(parentID string) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.gen++
	p.items = nil
	p.view = nil
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
	p.rebuildView()
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
