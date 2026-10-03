package coren

import (
	"context"
	"errors"
	"testing"
)

func TestProvideAndResolveService(t *testing.T) {
	k := NewKernel(context.Background())

	provider := PluginFunc{
		Name: "provider",
		Mount: func(ctx Context) error {
			ctx.Provide("greeting", "hello")
			return nil
		},
	}
	consumer := PluginFunc{
		Name: "consumer",
		Mount: func(ctx Context) error {
			if _, err := ctx.Require("greeting"); err != nil {
				t.Errorf("consumer should see greeting: %v", err)
			}
			return nil
		},
	}
	if err := k.Boot(provider, consumer); err != nil {
		t.Fatal(err)
	}

	svc, ok := k.Context().Service("greeting")
	if !ok || svc != "hello" {
		t.Fatalf("service = %v, ok = %v", svc, ok)
	}
}

func TestTypedUnwrap(t *testing.T) {
	k := NewKernel(context.Background())
	type counter struct{ n int }
	_ = k.Boot(PluginFunc{
		Name: "svc",
		Mount: func(ctx Context) error {
			ctx.Provide(ServiceTools, &counter{n: 5})
			return nil
		},
	})

	svc, ok := UnwrapKey[*counter](k.Context(), ServiceTools)
	if !ok || svc.n != 5 {
		t.Fatalf("unwrap = %v, ok = %v", svc, ok)
	}
	if _, ok := UnwrapKey[string](k.Context(), ServiceTools); ok {
		t.Fatal("wrong type should not unwrap")
	}
}

func TestMissingDependencyFailsBoot(t *testing.T) {
	k := NewKernel(context.Background())
	err := k.Boot(PluginFunc{
		Name:  "needs",
		Needs: []string{"nope"},
		Mount: func(context Context) error { return nil },
	})
	if err == nil {
		t.Fatal("expected boot to fail on missing dependency")
	}
	if got := len(k.AppliedPlugins()); got != 0 {
		t.Fatalf("applied plugins = %d, want 0 after rollback", got)
	}
}

func TestDependencySatisfiedByEarlierPlugin(t *testing.T) {
	k := NewKernel(context.Background())
	provider := PluginFunc{
		Name:  "llm",
		Mount: func(ctx Context) error { ctx.Provide(ServiceLLM, "adapter"); return nil },
	}
	consumer := PluginFunc{
		Name:  "agent",
		Needs: []string{ServiceLLM},
		Mount: func(ctx Context) error {
			svc, err := ctx.Require(ServiceLLM)
			if err != nil || svc != "adapter" {
				t.Errorf("llm = %v, err = %v", svc, err)
			}
			return nil
		},
	}
	if err := k.Boot(provider, consumer); err != nil {
		t.Fatal(err)
	}
}

func TestApplyErrorRollsBackPreviousPlugins(t *testing.T) {
	k := NewKernel(context.Background())
	good := PluginFunc{
		Name:  "good",
		Mount: func(ctx Context) error { ctx.Provide("x", 1); return nil },
	}
	bad := PluginFunc{
		Name:  "bad",
		Mount: func(Context) error { return errors.New("boom") },
	}
	if err := k.Boot(good, bad); err == nil {
		t.Fatal("expected error")
	}
	if _, ok := k.Context().Service("x"); ok {
		t.Fatal("service from rolled-back plugin should be gone")
	}
}

func TestShutdownUnwindsRegistrations(t *testing.T) {
	k := NewKernel(context.Background())
	var order []string
	_ = k.Boot(
		PluginFunc{
			Name: "a",
			Mount: func(ctx Context) error {
				ctx.Provide("a", 1)
				ctx.EffectFunc("a-effect", func() error { order = append(order, "a"); return nil })
				return nil
			},
		},
		PluginFunc{
			Name: "b",
			Mount: func(ctx Context) error {
				ctx.Provide("b", 2)
				ctx.EffectFunc("b-effect", func() error { order = append(order, "b"); return nil })
				return nil
			},
		},
	)

	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if _, ok := k.Context().Service("a"); ok {
		t.Error("service a should be unwound")
	}
	if _, ok := k.Context().Service("b"); ok {
		t.Error("service b should be unwound")
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Errorf("teardown order = %v, want reverse (b, a)", order)
	}
}

func TestStartableLifecycle(t *testing.T) {
	k := NewKernel(context.Background())
	var started, stopped bool
	_ = k.Boot(PluginFunc{
		Name:    "life",
		Mount:   func(Context) error { return nil },
		OnStart: func(Context) error { started = true; return nil },
		OnStop:  func(Context) error { stopped = true; return nil },
	})
	if err := k.Start(); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Error("Start not called")
	}
	if err := k.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("Stop not called")
	}
}

func TestPluginCannotBootstrapItsOwnDependency(t *testing.T) {
	// A plugin that needs a service only it provides can never be mounted: the
	// kernel checks dependencies before Apply.
	k := NewKernel(context.Background())
	err := k.Boot(PluginFunc{
		Name:  "self",
		Needs: []string{"self"},
		Mount: func(ctx Context) error { ctx.Provide("self", 1); return nil },
	})
	if err == nil {
		t.Fatal("expected boot to fail: dependency must be satisfied before Apply")
	}
}
