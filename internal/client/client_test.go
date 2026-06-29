package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

const testManifestBody = "{\"title\":\"Case\"}\n"

func TestUploadFileUsesCustomFileField(t *testing.T) {
	manifestPath := writeTestManifest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCustomUploadRequest(t, r)
		_, _ = w.Write([]byte("{\"id\":\"ingestion-1\"}"))
	}))
	defer server.Close()

	raw, err := New(server.URL, "test-key").UploadFile(context.Background(), "case-ingestions", "manifest", manifestPath, map[string]string{
		"space_id": "space-1",
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if string(raw) != "{\"id\":\"ingestion-1\"}" {
		t.Fatalf("raw = %s", raw)
	}
}

func writeTestManifest(t *testing.T) string {
	t.Helper()
	manifest, err := os.CreateTemp(t.TempDir(), "manifest-*.jsonl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, writeErr := manifest.WriteString(testManifestBody); writeErr != nil {
		t.Fatalf("WriteString: %v", writeErr)
	}
	if closeErr := manifest.Close(); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	return manifest.Name()
}

func assertCustomUploadRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Method, http.MethodPost; got != want {
		t.Errorf("method = %q, want %q", got, want)
	}
	if got, want := r.URL.Path, "/api/v1/public/case-ingestions"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if parseErr := r.ParseMultipartForm(1 << 20); parseErr != nil {
		t.Fatalf("ParseMultipartForm: %v", parseErr)
	}
	if got, want := r.FormValue("space_id"), "space-1"; got != want {
		t.Errorf("space_id = %q, want %q", got, want)
	}
	if _, _, fileErr := r.FormFile("file"); fileErr == nil {
		t.Fatalf("unexpected file field %q", "file")
	}
	assertUploadedManifest(t, r)
}

func assertUploadedManifest(t *testing.T, r *http.Request) {
	t.Helper()
	f, _, err := r.FormFile("manifest")
	if err != nil {
		t.Fatalf("manifest file missing: %v", err)
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != testManifestBody {
		t.Fatalf("manifest body = %q", body)
	}
}
