package coreagent

import (
	"context"
	"testing"

	"coren/pkg/agents"
	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/session"
	"coren/pkg/tools"
)

func kernelWithDeps(t *testing.T) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		coren.PluginFunc{Name: "llm", Mount: func(ctx coren.Context) error {
			ctx.Provide(llm.Key, llm.NewRegistry())
			return nil
		}},
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		ProviderPlugin{},
		LoopPlugin{Config: Config{Model: "test"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestProviderAndLoopPluginsProvideServices(t *testing.T) {
	k := kernelWithDeps(t)
	defer k.Shutdown()

	if _, ok := coren.UnwrapKey[agents.Service](k.Context(), agents.Key); !ok {
		t.Error("agents service missing")
	}
	if _, ok := coren.UnwrapKey[agents.Loop](k.Context(), agents.LoopKey); !ok {
		t.Error("agent-loop service missing")
	}
}

func TestLoopPluginFailsWithoutToolService(t *testing.T) {
	k := coren.NewKernel(context.Background())
	err := k.Boot(
		coren.PluginFunc{Name: "llm", Mount: func(ctx coren.Context) error {
			ctx.Provide(llm.Key, llm.NewRegistry())
			return nil
		}},
		ProviderPlugin{},
		LoopPlugin{Config: Config{}},
	)
	if err == nil {
		t.Fatal("expected boot to fail: tools service missing")
	}
}

func TestDefaultLoopSendDrivesAgent(t *testing.T) {
	k := kernelWithDeps(t)
	defer k.Shutdown()

	loop, _ := coren.UnwrapKey[agents.Loop](k.Context(), agents.LoopKey)
	sess := session.NewStore().Get("t")

	// No adapter registered: the loop should surface an error event, proving it
	// reached the agent and attempted resolution.
	var sawEvent bool
	for ev := range loop.Send(k.Context(), sess, "hi") {
		sawEvent = true
		if ev.Err == nil {
			t.Error("expected error without a registered adapter")
		}
	}
	if !sawEvent {
		t.Error("loop produced no events")
	}
}
