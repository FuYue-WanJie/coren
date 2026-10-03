package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMissingIsEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "MEMORY.md"))
	got, err := s.Read()
	if err != nil || got != "" {
		t.Errorf("read = %q, err = %v", got, err)
	}
}

func TestWriteThenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "MEMORY.md")
	s := New(path)
	if err := s.Write("hello"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Read()
	if got != "hello" {
		t.Errorf("read = %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestAppendAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MEMORY.md")
	s := New(path)
	if err := s.Append("first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("second"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Read()
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("content = %q", got)
	}
}

func TestAppendRejectsEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "MEMORY.md"))
	if err := s.Append("   "); err == nil {
		t.Fatal("expected error for empty append")
	}
}

func TestSectionFormats(t *testing.T) {
	out := Section("Conventions", "use gofmt")
	if !strings.HasPrefix(out, "## Conventions") || !strings.Contains(out, "use gofmt") {
		t.Errorf("section = %q", out)
	}
	if Section("", "body") != "body" {
		t.Errorf("empty title should yield body only")
	}
}
