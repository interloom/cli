// Package output renders command results and errors. The CLI is agent-first:
// results are JSON on stdout, errors are a JSON envelope on stderr, and the
// process exit code is stable and machine-checkable.
package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/interloom/cli/internal/client"
)

// Stable exit codes.
const (
	ExitOK         = 0
	ExitError      = 1 // generic / network
	ExitUsage      = 2 // bad invocation (set by cobra)
	ExitAuth       = 3 // 401 / 403
	ExitNotFound   = 4 // 404
	ExitValidation = 5 // 400 / 422
)

// JSON pretty-prints a raw JSON document to w with a trailing newline.
// An empty body (e.g. HTTP 204) prints nothing.
func JSON(w io.Writer, raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not valid JSON; emit verbatim rather than losing data.
		_, err := fmt.Fprintln(w, string(raw))
		return err
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// EmitError writes a JSON error envelope to stderr and returns the exit code.
func EmitError(w io.Writer, err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		// The API already returns {"error": {...}}; pass it through verbatim.
		if len(bytes.TrimSpace(apiErr.Body)) > 0 && json.Valid(apiErr.Body) {
			_ = JSON(w, apiErr.Body)
		} else {
			writeEnvelope(w, apiErr.StatusCode, apiErr.Code, apiErr.Error())
		}
		return exitForStatus(apiErr.StatusCode)
	}
	writeEnvelope(w, 0, "", err.Error())
	return ExitError
}

func writeEnvelope(w io.Writer, status int, code, message string) {
	type env struct {
		Status  int    `json:"status,omitempty"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	}
	payload := struct {
		Error env `json:"error"`
	}{env{Status: status, Code: code, Message: message}}
	data, _ := json.MarshalIndent(payload, "", "  ")
	_, _ = fmt.Fprintln(w, string(data))
}

func exitForStatus(status int) int {
	switch status {
	case 401, 403:
		return ExitAuth
	case 404:
		return ExitNotFound
	case 400, 422:
		return ExitValidation
	default:
		return ExitError
	}
}
