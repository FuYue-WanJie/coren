package shellweb

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"coren/pkg/webui"
)

func newManager(t *testing.T, files map[string]string, version string) *webui.Manager {
	t.Helper()
	src := fstest.MapFS{}
	for name, body := range files {
		src[name] = &fstest.MapFile{Data: []byte(body)}
	}
	m, err := webui.New(src, filepath.Join(t.TempDir(), "webui"), version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWebUIStatusDisabledWithoutManager(t *testing.T) {
	shell := &Shell{}
	rec := httptest.NewRecorder()
	shell.handleWebUIStatus(rec, httptest.NewRequest(http.MethodGet, "/api/webui/status", nil))
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWebUIStatusReportsUpdate(t *testing.T) {
	// Extract v1, then wrap with a v2 manager pointing at the same dir.
	m1 := newManager(t, map[string]string{"index.html": "v1"}, "1.0.0")
	m2, err := webui.New(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("v2")}}, m1.Dir, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	shell := &Shell{manager: m2}
	rec := httptest.NewRecorder()
	shell.handleWebUIStatus(rec, httptest.NewRequest(http.MethodGet, "/api/webui/status", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"update_available":true`) {
		t.Fatalf("body = %s", body)
	}
	if !strings.Contains(body, `"disk_version":"1.0.0"`) || !strings.Contains(body, `"builtin_version":"2.0.0"`) {
		t.Fatalf("versions missing: %s", body)
	}
}

func TestWebUIUpdateEndpoint(t *testing.T) {
	m1 := newManager(t, map[string]string{"index.html": "v1"}, "1.0.0")
	if err := os.WriteFile(filepath.Join(m1.Dir, "index.html"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2, _ := webui.New(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("v2")}}, m1.Dir, "2.0.0")
	shell := &Shell{manager: m2}

	rec := httptest.NewRecorder()
	shell.handleWebUIUpdate(rec, httptest.NewRequest(http.MethodPost, "/api/webui/update", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "backup") {
		t.Fatalf("expected backup path: %s", rec.Body.String())
	}
	got, _ := os.ReadFile(filepath.Join(m1.Dir, "index.html"))
	if string(got) != "v2" {
		t.Fatalf("update did not overwrite: %q", got)
	}
}

func TestWebUIUpdateRejectsGet(t *testing.T) {
	shell := &Shell{manager: newManager(t, map[string]string{"index.html": "v1"}, "1.0.0")}
	rec := httptest.NewRecorder()
	shell.handleWebUIUpdate(rec, httptest.NewRequest(http.MethodGet, "/api/webui/update", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
}
