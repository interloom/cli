package tui

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/interloom/cli/internal/api"
	"github.com/interloom/cli/internal/client"
)

// listPage is the shape of a single list response.
type listPage[T any] struct {
	Data       []T     `json:"data"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

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
		key:         {val},
		"sort":      {"updated_at"},
		"direction": {"desc"},
	}
}

// fetchCasesPage loads one page of cases in a space, newest update first.
func fetchCasesPage(ctx context.Context, c *client.Client, spaceID, cursor string) ([]api.CaseListItem, string, bool, error) {
	raw, err := c.List(ctx, "cases", pageQuery(cursor, newestFirst("space_id", spaceID)))
	return decodePage[api.CaseListItem](raw, err)
}

// fetchSubCasesPage loads one page of a case's child cases, newest first.
func fetchSubCasesPage(ctx context.Context, c *client.Client, parentCaseID, cursor string) ([]api.CaseListItem, string, bool, error) {
	raw, err := c.List(ctx, "cases", pageQuery(cursor, newestFirst("parent_case_id", parentCaseID)))
	return decodePage[api.CaseListItem](raw, err)
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
