package tui

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"regexp"
	"strings"
)

// maxTextBytes caps how much of a file we'll pull down and render as text.
const maxTextBytes int64 = 2 << 20 // 2 MiB

// textContent is a file rendered for the detail pane: either a text body or a
// flag saying the bytes looked binary and shouldn't be shown. eml marks an
// email message, which renders as plain text (markdown off) by default.
type textContent struct {
	body   string
	binary bool
	eml    bool
}

// textMimes are non-text/* MIME types we still treat as text.
var textMimes = map[string]bool{
	"message/rfc822":                    true,
	"application/json":                  true,
	"application/ld+json":               true,
	"application/x-ndjson":              true,
	"application/xml":                   true,
	"application/javascript":            true,
	"application/ecmascript":            true,
	"application/x-sh":                  true,
	"application/x-shellscript":         true,
	"application/x-yaml":                true,
	"application/yaml":                  true,
	"application/toml":                  true,
	"application/sql":                   true,
	"application/csv":                   true,
	"application/x-www-form-urlencoded": true,
}

// textExts are file extensions we treat as text when the MIME type is missing
// or unhelpfully generic.
var textExts = map[string]bool{
	".txt": true, ".text": true, ".log": true, ".md": true, ".markdown": true,
	".json": true, ".jsonl": true, ".ndjson": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".env": true,
	".xml": true, ".html": true, ".htm": true, ".csv": true, ".tsv": true,
	".sql": true, ".sh": true, ".bash": true, ".zsh": true, ".py": true,
	".rb": true, ".go": true, ".rs": true, ".js": true, ".ts": true, ".jsx": true,
	".tsx": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true,
	".java": true, ".kt": true, ".swift": true, ".php": true, ".pl": true,
	".lua": true, ".r": true, ".scala": true, ".clj": true, ".ex": true,
	".exs": true, ".erl": true, ".hs": true, ".dart": true, ".vue": true,
	".css": true, ".scss": true, ".less": true, ".svg": true, ".gradle": true,
	".properties": true, ".dockerfile": true, ".makefile": true, ".eml": true,
	".diff": true, ".patch": true, ".gitignore": true, ".editorconfig": true,
}

// normalizeMime lowercases a MIME type and drops any parameters (charset etc.).
func normalizeMime(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	return m
}

// isTextMime reports whether a MIME type denotes text we can render.
func isTextMime(m string) bool {
	m = normalizeMime(m)
	return strings.HasPrefix(m, "text/") || textMimes[m]
}

// isTextExt reports whether a filename's extension suggests text content.
func isTextExt(name string) bool {
	return textExts[strings.ToLower(filepath.Ext(name))]
}

// isEML reports whether a file is an RFC 822 email message.
func isEML(m, name string) bool {
	return normalizeMime(m) == "message/rfc822" ||
		strings.EqualFold(filepath.Ext(name), ".eml")
}

// textCandidate decides whether a file is worth downloading to render as text:
// small enough, and either a known text type/extension or a generic type that
// content-sniffing can confirm.
func textCandidate(mimeType, name string, size int) bool {
	if size < 0 || int64(size) > maxTextBytes {
		return false
	}
	m := normalizeMime(mimeType)
	switch {
	case strings.HasPrefix(m, "image/"),
		strings.HasPrefix(m, "audio/"),
		strings.HasPrefix(m, "video/"),
		strings.HasPrefix(m, "font/"):
		return false
	}
	if isTextMime(m) || isTextExt(name) {
		return true
	}
	// Unknown/generic types still get a chance; looksLikeText decides later.
	return m == "" || m == "application/octet-stream" || m == "binary/octet-stream"
}

// looksLikeText applies the classic heuristic: a NUL byte in the first 8 KiB,
// or too many control characters, means the data is binary.
func looksLikeText(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	ctrl := 0
	for _, b := range sample {
		switch {
		case b == '\n', b == '\r', b == '\t', b == '\f':
		case b < 0x20, b == 0x7f:
			ctrl++
		}
	}
	return ctrl*100/len(sample) < 10
}

// normalizeText makes raw bytes safe to display: valid UTF-8 and Unix newlines.
func normalizeText(data []byte) string {
	s := strings.ToValidUTF8(string(data), "\uFFFD")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// prepareText turns downloaded bytes into renderable content, special-casing
// .eml messages.
func prepareText(data []byte, mimeType, name string) textContent {
	if isEML(mimeType, name) {
		return textContent{body: normalizeText([]byte(renderEML(data))), eml: true}
	}
	if !looksLikeText(data) {
		return textContent{binary: true}
	}
	return textContent{body: normalizeText(data)}
}

// ---- very basic .eml (message/rfc822) rendering ----

// renderEML formats an email message: a small header block followed by the
// best available text body. Falls back to the raw bytes if parsing fails.
func renderEML(data []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return string(data)
	}

	dec := new(mime.WordDecoder)
	decode := func(v string) string {
		if out, derr := dec.DecodeHeader(v); derr == nil {
			return out
		}
		return v
	}

	var b strings.Builder
	for _, h := range []string{"Subject", "From", "To", "Cc", "Date"} {
		if v := msg.Header.Get(h); v != "" {
			b.WriteString(h + ": " + decode(v) + "\n")
		}
	}
	b.WriteString(strings.Repeat("─", 48) + "\n\n")

	plain, html := emlBody(msg.Header, msg.Body)
	switch {
	case strings.TrimSpace(plain) != "":
		b.WriteString(plain)
	case strings.TrimSpace(html) != "":
		b.WriteString(stripHTML(html))
	default:
		b.WriteString("(no readable text body)")
	}
	return b.String()
}

// emlBody returns the first text/plain and text/html bodies found. Multipart
// containers are walked a few levels deep; a single-part body is decoded and
// routed by its own content type.
func emlBody(header mail.Header, body io.Reader) (plain, html string) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err == nil && strings.HasPrefix(mediaType, "multipart/") {
		walkParts(multipart.NewReader(body, params["boundary"]), &plain, &html, 0)
		return plain, html
	}
	raw, _ := io.ReadAll(io.LimitReader(body, maxTextBytes))
	decoded := decodeCTE(raw, header.Get("Content-Transfer-Encoding"))
	if mediaType == "text/html" {
		return "", decoded
	}
	return decoded, ""
}

func walkParts(mr *multipart.Reader, plain, html *string, depth int) {
	if depth > 4 {
		return
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			return
		}
		mediaType, params, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			walkParts(multipart.NewReader(p, params["boundary"]), plain, html, depth+1)
			continue
		}
		content := decodePart(p)
		switch mediaType {
		case "text/plain":
			if *plain == "" {
				*plain = content
			}
		case "text/html":
			if *html == "" {
				*html = content
			}
		}
	}
}

// decodePart reads a MIME part, applying its transfer encoding.
func decodePart(p *multipart.Part) string {
	raw, _ := io.ReadAll(io.LimitReader(p, maxTextBytes))
	return decodeCTE(raw, p.Header.Get("Content-Transfer-Encoding"))
}

// decodeCTE decodes base64 / quoted-printable bodies, passing others through.
func decodeCTE(data []byte, encoding string) string {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		if dec, err := base64.StdEncoding.DecodeString(
			strings.ReplaceAll(strings.ReplaceAll(string(data), "\r", ""), "\n", ""),
		); err == nil {
			return string(dec)
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data))); err == nil {
			return string(dec)
		}
	}
	return string(data)
}

var (
	htmlTagRe   = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlBlankRe = regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)+`)
)

// stripHTML is a crude HTML-to-text fallback: drop scripts/styles, turn block
// tags into newlines, remove remaining tags and unescape a few entities.
func stripHTML(s string) string {
	s = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)</(p|div|tr|li|h[1-6])>`).ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&apos;", "'",
	)
	s = r.Replace(s)
	s = htmlBlankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
