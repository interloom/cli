// Package client is a thin, resource-agnostic transport over the Interloom
// public REST API. Every resource shares the same URL shape and cursor
// pagination, so one implementation drives all of them. Responses are passed
// through as raw JSON to stay lossless for agent consumers.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiPrefix = "/api/v1/public"

// Client talks to a single Interloom instance.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
}

// New returns a client for the given base URL (origin, no path) and API key.
func New(baseURL, apiKey string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
	}
}

// APIError is returned for any non-2xx response.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Body       json.RawMessage
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Message, e.StatusCode)
	}
	return fmt.Sprintf("request failed (HTTP %d)", e.StatusCode)
}

// do performs a request and returns the raw body for 2xx, or an *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	u := c.baseURL + apiPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	return nil, parseAPIError(resp.StatusCode, raw)
}

func parseAPIError(status int, raw []byte) *APIError {
	e := &APIError{StatusCode: status, Body: raw}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
	}
	return e
}

// List returns one page as the raw API response ({data, has_more, next_cursor}).
func (c *Client) List(ctx context.Context, resource string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, "/"+resource, query, nil, "")
}

// ListAll follows next_cursor and returns an aggregated {"data": [...]} object.
func (c *Client) ListAll(ctx context.Context, resource string, query url.Values) (json.RawMessage, error) {
	if query == nil {
		query = url.Values{}
	}
	var all []json.RawMessage
	for {
		raw, err := c.do(ctx, http.MethodGet, "/"+resource, query, nil, "")
		if err != nil {
			return nil, err
		}
		var p struct {
			Data       []json.RawMessage `json:"data"`
			HasMore    bool              `json:"has_more"`
			NextCursor *string           `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("decoding page: %w", err)
		}
		all = append(all, p.Data...)
		if !p.HasMore || p.NextCursor == nil || *p.NextCursor == "" {
			break
		}
		query.Set("cursor", *p.NextCursor)
	}
	if all == nil {
		all = []json.RawMessage{}
	}
	return json.Marshal(struct {
		Data []json.RawMessage `json:"data"`
	}{all})
}

// Get fetches a single resource by id.
func (c *Client) Get(ctx context.Context, resource, id string) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, "/"+resource+"/"+url.PathEscape(id), nil, nil, "")
}

// Create posts a JSON body to the resource collection.
func (c *Client) Create(ctx context.Context, resource string, body []byte) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPost, "/"+resource, nil, bytes.NewReader(body), "application/json")
}

// Update patches a single resource with a JSON body.
func (c *Client) Update(ctx context.Context, resource, id string, body []byte) (json.RawMessage, error) {
	return c.do(ctx, http.MethodPatch, "/"+resource+"/"+url.PathEscape(id), nil, bytes.NewReader(body), "application/json")
}

// Delete removes a single resource. A 204 yields an empty body.
func (c *Client) Delete(ctx context.Context, resource, id string) (json.RawMessage, error) {
	return c.do(ctx, http.MethodDelete, "/"+resource+"/"+url.PathEscape(id), nil, nil, "")
}

// Upload posts a multipart/form-data request with the file under the "file"
// field plus any non-empty string fields (e.g. space_id, case_id).
func (c *Client) Upload(ctx context.Context, resource, filePath string, fields map[string]string) (json.RawMessage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := mw.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, "/"+resource, nil, &buf, mw.FormDataContentType())
}

// FetchTo streams the body of an arbitrary GET URL to w. Used for the
// short-lived signed download URLs returned on file objects; no auth header is
// sent since the URL is already signed.
func (c *Client) FetchTo(ctx context.Context, rawURL string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}
