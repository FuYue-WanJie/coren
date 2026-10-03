package ctxfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWalksUpAndNearestFirst(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	write(t, root, "AGENTS.md", "root rules")
	write(t, filepath.Join(root, "a"), "AGENTS.md", "a rules")
	write(t, sub, "CLAUDE.md", "sub rules")

	files, err := Load(Options{WorkDir: sub})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %d, want 3", len(files))
	}
	// Nearest first: sub, then a, then root.
	if !strings.Contains(files[0].Content, "sub rules") {
		t.Errorf("files[0] = %q", files[0].Content)
	}
	if !strings.Contains(files[1].Content, "a rules") {
		t.Errorf("files[1] = %q", files[1].Content)
	}
	if !strings.Contains(files[2].Content, "root rules") {
		t.Errorf("files[2] = %q", files[2].Content)
	}
}

func TestOverrideShadowsBaseInSameDir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "AGENTS.md", "base")
	write(t, dir, "AGENTS.override.md", "override")

	files, _ := Load(Options{WorkDir: dir})
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	if !strings.Contains(files[0].Content, "override") {
		t.Errorf("content = %q, want override to win", files[0].Content)
	}
}

func TestFixedPathCheckedFirst(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "AGENTS.md", "agents")
	write(t, filepath.Join(dir, ".coren"), "rules.md", "fixed rules")

	files, _ := Load(Options{
		WorkDir:    dir,
		FixedPaths: []string{".coren/rules.md"},
	})
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if !strings.Contains(files[0].Content, "fixed rules") {
		t.Errorf("files[0] = %q, want fixed path first", files[0].Content)
	}
}

func TestDisabledReturnsNothing(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "AGENTS.md", "rules")
	files, _ := Load(Options{WorkDir: dir, Disabled: true})
	if len(files) != 0 {
		t.Errorf("files = %d, want 0 when disabled", len(files))
	}
}

func TestFormatEmptyIsBlank(t *testing.T) {
	if Format(nil) != "" {
		t.Error("empty files should format to empty string")
	}
}

func TestFormatWrapsWithPath(t *testing.T) {
	out := Format([]File{{Path: "/p/AGENTS.md", Content: "do x"}})
	if !strings.Contains(out, "<project_context>") || !strings.Contains(out, "do x") {
		t.Errorf("formatted = %q", out)
	}
	if !strings.Contains(out, `path="/p/AGENTS.md"`) {
		t.Errorf("path missing: %q", out)
	}
}
