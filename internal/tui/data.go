package tui

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/interloom/cli/internal/api"
	"github.com/interloom/cli/internal/client"
)

// listPage is the shape of a single list response.
type listPage[T any] struct {
	Data       []T     `json:"data"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

const (
	querySort      = "sort"
	queryDirection = "direction"
	queryStatus    = "status"
	sortUpdatedAt  = "updated_at"
	sortPosition   = "position"
	directionDesc  = "desc"
	directionAsc   = "asc"
)

func decodePage[T any](raw json.RawMessage, err error) ([]T, string, bool, error) {
	if err != nil {
		return nil, "", false, err
	}
	var p listPage[T]
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", false, err
	}
	next := ""
	if p.NextCursor != nil {
		next = *p.NextCursor
	}
	return p.Data, next, p.HasMore, nil
}

func pageQuery(cursor string, extra url.Values) url.Values {
	q := url.Values{}
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("limit", strconv.Itoa(pageSize))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return q
}

// fetchSpacesPage loads one page of spaces (API order).
func fetchSpacesPage(ctx context.Context, c *client.Client, cursor string) ([]api.SpaceListItem, string, bool, error) {
	raw, err := c.List(ctx, "spaces", pageQuery(cursor, nil))
	return decodePage[api.SpaceListItem](raw, err)
}

// newestFirst returns list params filtering by key=val, sorted by newest update.
func newestFirst(key, val string) url.Values {
	return url.Values{
		key:            {val},
		querySort:      {sortUpdatedAt},
		queryDirection: {directionDesc},
	}
}

// positionOrder returns list params filtering by key=val, sorted by the
// case-tree position so the TUI follows the user-defined task order.
func positionOrder(key, val string) url.Values {
	return url.Values{
		key:            {val},
		querySort:      {sortPosition},
		queryDirection: {directionAsc},
	}
}

// withStatus adds a server-side status filter to q when status is non-empty.
func withStatus(q url.Values, status string) url.Values {
	if status != "" {
		q.Set(queryStatus, status)
	}
	return q
}

// fetchCasesPage loads one page of cases in a space, position order,
// filtered server-side by status when set.
func fetchCasesPage(ctx context.Context, c *client.Client, spaceID, status, cursor string) ([]api.CaseListItem, string, bool, error) {
	q := withStatus(positionOrder("space_id", spaceID), status)
	raw, err := c.List(ctx, "cases", pageQuery(cursor, q))
	return decodePage[api.CaseListItem](raw, err)
}

// fetchSubCasesPage loads one page of a case's child cases, position order,
// filtered server-side by status when set.
func fetchSubCasesPage(ctx context.Context, c *client.Client, parentCaseID, status, cursor string) ([]api.CaseListItem, string, bool, error) {
	q := withStatus(positionOrder("parent_case_id", parentCaseID), status)
	raw, err := c.List(ctx, "cases", pageQuery(cursor, q))
	return decodePage[api.CaseListItem](raw, err)
}

// fetchUsersPage loads one page of the organization user directory. Case list
// items only carry an assignee ID, so the TUI builds a lookup table from all
// pages to render names without issuing a request for every visible row.
func fetchUsersPage(ctx context.Context, c *client.Client, cursor string) ([]api.User, string, bool, error) {
	q := pageQuery(cursor, nil)
	q.Set("limit", strconv.Itoa(userPageSize))
	raw, err := c.List(ctx, "users", q)
	return decodePage[api.User](raw, err)
}

// fetchFilesPage loads one page of a case's files, newest update first. The
// files list returns full File objects, so no separate detail fetch is needed.
func fetchFilesPage(ctx context.Context, c *client.Client, caseID, cursor string) ([]api.File, string, bool, error) {
	raw, err := c.List(ctx, "files", pageQuery(cursor, newestFirst("case_id", caseID)))
	return decodePage[api.File](raw, err)
}

// fetchNotesPage loads one page of notes on a case, newest update first.
func fetchNotesPage(ctx context.Context, c *client.Client, caseID, cursor string) ([]api.NoteListItem, string, bool, error) {
	raw, err := c.List(ctx, "notes", pageQuery(cursor, newestFirst("case_id", caseID)))
	return decodePage[api.NoteListItem](raw, err)
}

// threadMessage is a single message payload of a thread event, flattened for
// display. The thread pane only renders message payloads (file payloads are
// skipped for now), so one event can yield zero or more message rows.
type threadMessage struct {
	event        api.ThreadEvent
	payloadIndex int
	msg          api.MessagePayload
}

// fetchThreadMessagesPage loads one page of a thread's events (oldest first, the
// API default) and flattens them into message rows, dropping file payloads. The
// page's cursor/has_more are preserved so pagination still walks whole events.
func fetchThreadMessagesPage(ctx context.Context, c *client.Client, threadID, cursor string) ([]threadMessage, string, bool, error) {
	raw, err := c.List(ctx, "threads/"+url.PathEscape(threadID)+"/events", pageQuery(cursor, nil))
	events, next, hasMore, err := decodePage[api.ThreadEvent](raw, err)
	if err != nil {
		return nil, "", false, err
	}
	var rows []threadMessage
	for _, ev := range events {
		for i, p := range ev.Payloads {
			disc, derr := p.Discriminator()
			if derr != nil || disc != "message" {
				continue
			}
			msg, merr := p.AsMessagePayload()
			if merr != nil || strings.TrimSpace(msg.Text) == "" {
				continue // skip empty messages entirely
			}
			rows = append(rows, threadMessage{event: ev, payloadIndex: i, msg: msg})
		}
	}
	return rows, next, hasMore, nil
}

// fetchCase loads a single case with its full detail (description, summary).
func fetchCase(ctx context.Context, c *client.Client, id string) (*api.Case, error) {
	raw, err := c.Get(ctx, "cases", id)
	if err != nil {
		return nil, err
	}
	var cs api.Case
	if err := json.Unmarshal(raw, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// fetchNote loads a single note with its full body (markdown).
func fetchNote(ctx context.Context, c *client.Client, id string) (*api.Note, error) {
	raw, err := c.Get(ctx, "notes", id)
	if err != nil {
		return nil, err
	}
	var n api.Note
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	return &n, nil
}
