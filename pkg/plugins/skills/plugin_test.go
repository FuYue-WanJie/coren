package skillsplugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/skills"
	"coren/pkg/tools"
)

func mountSkills(t *testing.T, dir string) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		ProviderPlugin{Dir: dir},
		ToolsPlugin{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func writeSkill(t *testing.T, root, dir, manifest string, extra map[string]string) {
	t.Helper()
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, skills.SkillFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range extra {
		if err := os.WriteFile(filepath.Join(full, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillsServiceAndToolsRegistered(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet", "---\ndescription: Say hello.\n---\nGreet warmly.\n", nil)

	k := mountSkills(t, root)
	defer k.Shutdown()

	svc, ok := coren.UnwrapKey[skills.Service](k.Context(), skills.Key)
	if !ok {
		t.Fatal("skills service missing")
	}
	if _, ok := svc.Get("greet"); !ok {
		t.Error("greet skill not loaded")
	}

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, ok := toolSvc.Get("list_skills"); !ok {
		t.Error("list_skills tool missing")
	}
	if _, ok := toolSvc.Get("use_skill"); !ok {
		t.Error("use_skill tool missing")
	}
}

func TestListSkillsTool(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet", "---\ndescription: Say hello.\n---\nbody\n", nil)
	writeSkill(t, root, "hidden", "---\ndescription: secret.\ndisable-model-invocation: true\n---\nbody\n", nil)

	k := mountSkills(t, root)
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	out, err := toolSvc.RunText(context.Background(), "list_skills", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "greet: Say hello.") {
		t.Errorf("output = %q", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("disabled skill should be hidden: %q", out)
	}
}

func TestUseSkillToolLoadsContent(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet", "---\ndescription: Say hello.\n---\nGreet warmly.\n", nil)

	k := mountSkills(t, root)
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	out, err := toolSvc.RunText(context.Background(), "use_skill", `{"name":"greet"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Greet warmly.") || !strings.Contains(out, `name="greet"`) {
		t.Errorf("output = %q", out)
	}
}

func TestUseSkillToolReadsResource(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet",
		"---\ndescription: Say hello.\n---\nSee reference.txt.\n",
		map[string]string{"reference.txt": "say hi politely"},
	)

	k := mountSkills(t, root)
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	out, err := toolSvc.RunText(context.Background(), "use_skill", `{"name":"greet","resource":"reference.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "say hi politely" {
		t.Errorf("resource = %q", out)
	}
}

func TestUseSkillToolRejectsUnknown(t *testing.T) {
	k := mountSkills(t, t.TempDir())
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := toolSvc.RunText(context.Background(), "use_skill", `{"name":"nope"}`); err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestUseSkillToolRejectsResourceEscape(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "greet", "---\ndescription: d.\n---\nbody\n", nil)

	k := mountSkills(t, root)
	defer k.Shutdown()

	toolSvc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	if _, err := toolSvc.RunText(context.Background(), "use_skill", `{"name":"greet","resource":"../../etc/passwd"}`); err == nil {
		t.Fatal("expected resource escape to be rejected")
	}
}

func TestPluginCanRegisterSkillDirectly(t *testing.T) {
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		ProviderPlugin{},
		coren.PluginFunc{Name: "contributes-skill", Needs: []string{skills.Key}, Mount: func(ctx coren.Context) error {
			svc, _ := coren.UnwrapKey[skills.Service](ctx, skills.Key)
			svc.Register(skills.Skill{Name: "from-code", Description: "Registered by a plugin.", Content: "hi"})
			return nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Shutdown()

	svc, _ := coren.UnwrapKey[skills.Service](k.Context(), skills.Key)
	if _, ok := svc.Get("from-code"); !ok {
		t.Error("plugin-registered skill missing")
	}
}
