package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, dir, manifest string) {
	t.Helper()
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, SkillFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFileParsesFrontmatterAndBody(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet", "---\nname: greet\ndescription: Say hello.\n---\n\nGreet warmly.\n")

	skill, err := LoadFile(filepath.Join(root, "greet", SkillFileName))
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "greet" {
		t.Errorf("name = %q", skill.Name)
	}
	if skill.Description != "Say hello." {
		t.Errorf("description = %q", skill.Description)
	}
	if skill.Content != "Greet warmly." {
		t.Errorf("content = %q", skill.Content)
	}
}

func TestLoadFileDefaultsNameToDirectory(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "my-skill", "---\ndescription: No explicit name.\n---\nbody\n")

	skill, err := LoadFile(filepath.Join(root, "my-skill", SkillFileName))
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "my-skill" {
		t.Errorf("name = %q, want directory name", skill.Name)
	}
}

func TestLoadFileRequiresDescription(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "nodesc", "---\nname: nodesc\n---\nbody\n")
	if _, err := LoadFile(filepath.Join(root, "nodesc", SkillFileName)); err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestLoadFileHonorsDisableModelInvocation(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "secret", "---\ndescription: hidden.\ndisable-model-invocation: true\n---\nbody\n")
	skill, err := LoadFile(filepath.Join(root, "secret", SkillFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !skill.DisableModelInvocation {
		t.Error("disable-model-invocation not parsed")
	}
}

func TestLoadDirDiscoversNestedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "---\ndescription: A.\n---\na body\n")
	writeSkill(t, root, "nested/b", "---\ndescription: B.\n---\nb body\n")

	loaded, errs := LoadDir(root)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded = %d, want 2", len(loaded))
	}
	if loaded[0].Name != "a" || loaded[1].Name != "b" {
		t.Errorf("names = %q, %q", loaded[0].Name, loaded[1].Name)
	}
}

func TestLoadDirShallowestWinsOnNameCollision(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "dup", "---\ndescription: top level.\n---\ntop\n")
	writeSkill(t, root, "vendor/dup", "---\ndescription: vendored.\n---\nbottom\n")

	loaded, _ := LoadDir(root)
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d, want 1", len(loaded))
	}
	if loaded[0].Description != "top level." {
		t.Errorf("description = %q, want the shallower skill", loaded[0].Description)
	}
}

func TestLoadDirMissingIsNotAnError(t *testing.T) {
	loaded, errs := LoadDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if loaded != nil || errs != nil {
		t.Errorf("missing dir should yield nothing, got loaded=%v errs=%v", loaded, errs)
	}
}
