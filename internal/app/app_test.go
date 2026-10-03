package app

import (
	"context"
	"testing"

	"coren/internal/config"
	"coren/pkg/agent"
	"coren/pkg/agents"
	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/skills"
	"coren/pkg/subagents"
	"coren/pkg/tools"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		API:     "chat",
		BaseURL: "https://example.invalid/v1",
		Model:   "test-model",
		// A work dir enables context-file and memory discovery.
		WorkDir: t.TempDir(),
	}
}

func TestAppBootsServices(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	if _, ok := coren.UnwrapKey[llm.Service](a.Kernel.Context(), llm.Key); !ok {
		t.Error("llm service missing")
	}
	if _, ok := coren.UnwrapKey[tools.Service](a.Kernel.Context(), tools.Key); !ok {
		t.Error("tools service missing")
	}
	if _, ok := coren.UnwrapKey[session.Service](a.Kernel.Context(), session.Key); !ok {
		t.Error("sessions service missing")
	}
	if _, ok := coren.UnwrapKey[agents.Service](a.Kernel.Context(), agents.Key); !ok {
		t.Error("agents service missing")
	}
	if _, ok := coren.UnwrapKey[agents.Loop](a.Kernel.Context(), agents.LoopKey); !ok {
		t.Error("agent-loop service missing")
	}

	names := a.AdapterNames()
	if len(names) != 1 || names[0] != "openai-chat" {
		t.Errorf("adapters = %v", names)
	}

	toolSvc, _ := coren.UnwrapKey[tools.Service](a.Kernel.Context(), tools.Key)
	specs := toolSvc.Specs()
	// 4 builtin + list_skills + use_skill + task + ask_user + memory x2 + todo
	if len(specs) != 12 {
		t.Errorf("tools = %d, want 12", len(specs))
	}
	if _, ok := coren.UnwrapKey[skills.Service](a.Kernel.Context(), skills.Key); !ok {
		t.Error("skills service missing")
	}
	if _, ok := coren.UnwrapKey[subagents.Service](a.Kernel.Context(), subagents.Key); !ok {
		t.Error("subagents service missing")
	}
}

func TestAppResponsesVariant(t *testing.T) {
	cfg := testConfig(t)
	cfg.API = "responses"
	a, err := New(context.Background(), cfg, Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	names := a.AdapterNames()
	if len(names) != 1 || names[0] != "openai-responses" {
		t.Errorf("adapters = %v", names)
	}
}

func TestAppRejectsUnknownAPI(t *testing.T) {
	cfg := testConfig(t)
	cfg.API = "bogus"
	if _, err := New(context.Background(), cfg, Options{Profile: "headless"}); err == nil {
		t.Fatal("expected boot to fail for unknown api")
	}
}

func TestAppCloseUnwindsPlugins(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Kernel.Context().Service(tools.Key); ok {
		t.Error("tools service should be unwound after Close")
	}
}

func TestAppSessionPersistenceAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.SessionDir = dir

	a1, err := New(context.Background(), cfg, Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	sess := a1.Session.Get("conv")
	sess.Append(session.Event{Type: session.EventUserMsg, Text: "remember me"})
	if err := a1.Close(); err != nil {
		t.Fatal(err)
	}

	// A second app over the same dir resumes the conversation.
	a2, err := New(context.Background(), cfg, Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()

	msgs := a2.Session.Get("conv").Messages()
	if len(msgs) != 1 || msgs[0].Text != "remember me" {
		t.Errorf("resumed messages = %+v", msgs)
	}
}

func TestWebProfileMountsShell(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), Options{Profile: "web"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Shell == nil || a.Shell.Name() != "web" {
		t.Fatalf("shell = %v", a.Shell)
	}
}

func TestCliProfileMountsShell(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), Options{Profile: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Shell == nil || a.Shell.Name() != "cli" {
		t.Fatalf("shell = %v", a.Shell)
	}
}

func TestHeadlessProfileHasNoShell(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), Options{Profile: "headless"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Shell != nil {
		t.Errorf("headless profile should mount no shell, got %v", a.Shell)
	}
}

func TestUnknownPluginInProfileFails(t *testing.T) {
	// Directly exercise buildPlugin's default branch.
	if _, err := buildPlugin("does.not.exist", testConfig(t), agent.Agent{}, Options{Profile: "x"}); err == nil {
		t.Fatal("expected unknown plugin to fail")
	}
}
