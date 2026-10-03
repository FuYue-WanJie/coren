package coren

import (
	"context"
	"errors"
	"testing"
)

func TestEmitObserversInOrder(t *testing.T) {
	k := NewKernel(context.Background())
	var calls []string
	_ = k.Boot(
		PluginFunc{Name: "a", Mount: func(ctx Context) error {
			ctx.On(EventPluginLoaded, func(context.Context, any) { calls = append(calls, "a") })
			return nil
		}},
		PluginFunc{Name: "b", Mount: func(ctx Context) error {
			ctx.On(EventPluginLoaded, func(context.Context, any) { calls = append(calls, "b") })
			return nil
		}},
	)
	calls = nil // ignore load events from boot
	k.Context().Emit(EventPluginLoaded, "x")
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Errorf("calls = %v", calls)
	}
}

func TestWaterfallWrapsValue(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/value")
	_ = k.Boot(
		PluginFunc{Name: "outer", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/value", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				v, err := next(payload)
				if err != nil {
					return nil, err
				}
				return v.(string) + "+outer", nil
			}, false)
			return nil
		}},
		PluginFunc{Name: "inner", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/value", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				return next(payload.(string) + "+inner")
			}, false)
			return nil
		}},
	)

	got, err := k.Context().Waterfall("test/value", "base")
	if err != nil {
		t.Fatal(err)
	}
	if got != "base+inner+outer" {
		t.Errorf("got %v", got)
	}
}

func TestWaterfallShortCircuit(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/decide")
	_ = k.Boot(PluginFunc{Name: "policy", Mount: func(ctx Context) error {
		ctx.OnWaterfall("test/decide", func(_ context.Context, _ any, _ func(any) (any, error)) (any, error) {
			return "rejected", nil // no next(): short-circuit
		}, false)
		return nil
	}})

	got, err := k.Context().Waterfall("test/decide", "input")
	if err != nil {
		t.Fatal(err)
	}
	if got != "rejected" {
		t.Errorf("got %v", got)
	}
}

func TestWaterfallErrorPropagates(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/err")
	_ = k.Boot(PluginFunc{Name: "boom", Mount: func(ctx Context) error {
		ctx.OnWaterfall("test/err", func(context.Context, any, func(any) (any, error)) (any, error) {
			return nil, errors.New("inner failure")
		}, false)
		return nil
	}})
	if _, err := k.Context().Waterfall("test/err", nil); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestSerialThreadsValue(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/serial")
	_ = k.Boot(
		PluginFunc{Name: "one", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/serial", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				return next(payload.(int) + 1)
			}, false)
			return nil
		}},
		PluginFunc{Name: "two", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/serial", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				return next(payload.(int) * 10)
			}, false)
			return nil
		}},
	)
	got, err := k.Context().Serial("test/serial", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 20 { // (1+1)*10
		t.Errorf("got %v, want 20", got)
	}
}

func TestBailStopsAtFirstDecision(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/bail")
	_ = k.Boot(
		PluginFunc{Name: "first", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/bail", func(_ context.Context, _ any, _ func(any) (any, error)) (any, error) {
				return "first decides", nil
			}, false)
			return nil
		}},
		PluginFunc{Name: "second", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/bail", func(_ context.Context, _ any, _ func(any) (any, error)) (any, error) {
				return "should not run", nil
			}, false)
			return nil
		}},
	)
	got, err := k.Context().Bail("test/bail", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "first decides" {
		t.Errorf("got %v", got)
	}
}

func TestWaterfallPrependRunsFirst(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/prepend")
	var order []string
	_ = k.Boot(
		PluginFunc{Name: "normal", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/prepend", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				order = append(order, "normal")
				return next(payload)
			}, false)
			return nil
		}},
		PluginFunc{Name: "urgent", Mount: func(ctx Context) error {
			ctx.OnWaterfall("test/prepend", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
				order = append(order, "urgent")
				return next(payload)
			}, true)
			return nil
		}},
	)
	_, _ = k.Context().Waterfall("test/prepend", nil)
	if len(order) != 2 || order[0] != "urgent" || order[1] != "normal" {
		t.Errorf("order = %v", order)
	}
}

func TestListenerRemovedOnShutdown(t *testing.T) {
	k := NewKernel(context.Background())
	k.Context().DeclareEvent("test/remove")
	var count int
	_ = k.Boot(PluginFunc{Name: "listener", Mount: func(ctx Context) error {
		ctx.OnWaterfall("test/remove", func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
			count++
			return next(payload)
		}, false)
		return nil
	}})

	_, _ = k.Context().Waterfall("test/remove", nil)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	_ = k.Shutdown()
	_, _ = k.Context().Waterfall("test/remove", nil)
	if count != 1 {
		t.Errorf("listener survived shutdown: count = %d", count)
	}
}
