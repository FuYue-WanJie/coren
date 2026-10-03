package webui

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func srcFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

func TestEnsureDirExtractsOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webui")
	src := srcFS(map[string]string{"index.html": "a", "app.js": "b"})
	m, err := New(src, dir, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	extracted, err := m.EnsureDir()
	if err != nil {
		t.Fatal(err)
	}
	if !extracted {
		t.Fatal("first EnsureDir should extract")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Fatalf("index.html not written: %v", err)
	}

	// Second call is a no-op; local edits must survive.
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	extracted, err = m.EnsureDir()
	if err != nil {
		t.Fatal(err)
	}
	if extracted {
		t.Fatal("second EnsureDir should not re-extract")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(got) != "edited" {
		t.Fatalf("local edit lost: %q", got)
	}
}

func TestStatusNoUpdateWhenUnchanged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webui")
	src := srcFS(map[string]string{"index.html": "a"})
	m, _ := New(src, dir, "1.0.0")
	if _, err := m.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	st := m.Status()
	if !st.Extracted {
		t.Fatal("expected extracted")
	}
	if st.UpdateAvailable {
		t.Fatal("no update expected for identical assets")
	}
	if st.DiskVersion != "1.0.0" {
		t.Fatalf("disk version = %q", st.DiskVersion)
	}
}

func TestStatusDetectsUpdate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webui")

	// Extract version 1.
	src1 := srcFS(map[string]string{"index.html": "v1"})
	m1, _ := New(src1, dir, "1.0.0")
	if _, err := m1.EnsureDir(); err != nil {
		t.Fatal(err)
	}

	// New binary ships version 2.
	src2 := srcFS(map[string]string{"index.html": "v2"})
	m2, _ := New(src2, dir, "2.0.0")
	st := m2.Status()
	if !st.UpdateAvailable {
		t.Fatal("expected update available")
	}
	if st.DiskVersion != "1.0.0" || st.BuiltinVersion != "2.0.0" {
		t.Fatalf("versions = disk %q builtin %q", st.DiskVersion, st.BuiltinVersion)
	}
}

func TestUpdateBacksUpAndOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webui")
	src1 := srcFS(map[string]string{"index.html": "v1", "app.js": "old"})
	m1, _ := New(src1, dir, "1.0.0")
	if _, err := m1.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	// User edits a file.
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	src2 := srcFS(map[string]string{"index.html": "v2", "app.js": "new"})
	m2, _ := New(src2, dir, "2.0.0")
	backup, err := m2.Update()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected a backup path")
	}
	// Backup preserves the user's edit.
	got, err := os.ReadFile(filepath.Join(backup, "app.js"))
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(got) != "mine" {
		t.Fatalf("backup content = %q, want user edit", got)
	}
	// Live directory now has new content.
	got, _ = os.ReadFile(filepath.Join(dir, "app.js"))
	if string(got) != "new" {
		t.Fatalf("updated content = %q", got)
	}
	// Status is now clean.
	if m2.Status().UpdateAvailable {
		t.Fatal("update should be resolved after Update")
	}
}

func TestFSReturnsDiskWhenPresent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "webui")
	src := srcFS(map[string]string{"index.html": "builtin"})
	m, _ := New(src, dir, "1.0.0")

	// Before extraction, FS falls back to the built-in source.
	if got, err := fs.ReadFile(m.FS(), "index.html"); err != nil || string(got) != "builtin" {
		t.Fatalf("fallback FS = %q, %v", got, err)
	}
	if _, err := m.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(m.FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "disk" {
		t.Fatalf("FS should prefer disk, got %q", got)
	}
}
