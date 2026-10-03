package coren

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Context is a repository of services plus an event bus. Plugins locate
// capabilities by key rather than importing concrete implementations.
//
// A Context forms a parent chain: a scoped child sees its own registrations plus
// its ancestors', which lets one plugin's registrations be isolated and unwound
// without touching the rest of the tree.
type Context interface {
	// Service looks up a service by key, searching parents.
	Service(key string) (any, bool)
	// Provide registers a service under key and returns a disposer.
	Provide(key string, service any) Disposer
	// Require looks up a service and returns an error when absent.
	Require(key string) (any, error)

	// On subscribes an observer to an event.
	On(event string, observer Observer) Disposer
	// OnWaterfall subscribes an around-middleware handler.
	OnWaterfall(event string, handler Handler, prepend bool) Disposer
	// Emit broadcasts to observers.
	Emit(name string, payload any)
	// Waterfall dispatches around-middleware handlers.
	Waterfall(name string, payload any) (any, error)
	// Serial runs handlers in order.
	Serial(name string, payload any) (any, error)
	// Bail stops at the first handler that returns a non-nil decision.
	Bail(name string, payload any) (any, error)
	// DeclareEvent registers an event name so dispatch can validate it.
	DeclareEvent(name string)

	// Effect registers a reversible side effect on this context.
	Effect(name string, disposable Disposable) Disposer
	// EffectFunc registers a reversible function.
	EffectFunc(name string, fn Disposer) Disposer

	// Scope returns a child context bound to a plugin id.
	Scope(pluginID string) Context

	// GoContext returns the base context used for dispatch.
	GoContext() context.Context
}

// ErrServiceMissing is returned by Require when a service is not registered.
var ErrServiceMissing = errors.New("service not available")

type contextImpl struct {
	mu       sync.RWMutex
	services map[string]any

	parent Context
	bus    *eventBus
	stack  *effectStack
	goCtx  context.Context

	pluginID string
}

// NewContext creates a root context with an empty service repository.
func NewContext(goCtx context.Context) Context {
	if goCtx == nil {
		goCtx = context.Background()
	}
	return &contextImpl{
		services: map[string]any{},
		bus:      newEventBus(),
		stack:    &effectStack{},
		goCtx:    goCtx,
	}
}

// declareBuiltinEvents marks the framework's events as valid.
func (c *contextImpl) declareBuiltinEvents() {
	for _, name := range []string{
		EventAgentPreStep, EventAgentRequest, EventAgentTurnStopping,
		EventToolsPreExecute, EventToolsPostExecute, EventLLMStream,
		EventModelUsage,
		EventPluginLoaded, EventPluginUnloaded,
	} {
		c.bus.declare(name)
	}
}

func (c *contextImpl) Service(key string) (any, bool) {
	c.mu.RLock()
	svc, ok := c.services[key]
	c.mu.RUnlock()
	if ok {
		return svc, true
	}
	if c.parent != nil {
		return c.parent.Service(key)
	}
	return nil, false
}

func (c *contextImpl) Require(key string) (any, error) {
	if svc, ok := c.Service(key); ok {
		return svc, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrServiceMissing, key)
}

func (c *contextImpl) Provide(key string, service any) Disposer {
	// Scoped contexts promote services to the root so sibling plugins can resolve
	// them, while the disposer stays on this (child) stack for clean unwinding.
	if c.parent != nil {
		if root, ok := rootOf(c).(*contextImpl); ok {
			return c.provideInto(root, key, service)
		}
	}
	return c.provideInto(c, key, service)
}

// rootOf walks to the topmost context.
func rootOf(c *contextImpl) Context {
	cur := c
	for cur.parent != nil {
		if next, ok := cur.parent.(*contextImpl); ok {
			cur = next
			continue
		}
		break
	}
	return cur
}

// provideInto writes the service into target (usually an ancestor) and registers
// the disposer on the calling context's stack, so unwinding the child removes the
// promoted service from the parent too.
func (c *contextImpl) provideInto(target *contextImpl, key string, service any) Disposer {
	target.mu.Lock()
	previous, hadPrevious := target.services[key]
	target.services[key] = service
	target.mu.Unlock()

	disposer := func() error {
		target.mu.Lock()
		defer target.mu.Unlock()
		if hadPrevious {
			target.services[key] = previous
		} else {
			delete(target.services, key)
		}
		return nil
	}
	c.stack.add("provide:"+key, disposer)
	return disposer
}

func (c *contextImpl) On(event string, observer Observer) Disposer {
	d := c.bus.on(event, listener{observer: observer})
	c.stack.add("on:"+event, d)
	return d
}

func (c *contextImpl) OnWaterfall(event string, handler Handler, prepend bool) Disposer {
	d := c.bus.on(event, listener{handler: handler, prepend: prepend})
	c.stack.add("on-waterfall:"+event, d)
	return d
}

func (c *contextImpl) DeclareEvent(name string) {
	c.bus.declare(name)
}

func (c *contextImpl) Emit(name string, payload any) {
	c.bus.emit(c.goCtx, name, payload)
}

func (c *contextImpl) Waterfall(name string, payload any) (any, error) {
	return c.bus.waterfall(c.goCtx, name, payload)
}

func (c *contextImpl) Serial(name string, payload any) (any, error) {
	return c.bus.serial(c.goCtx, name, payload)
}

func (c *contextImpl) Bail(name string, payload any) (any, error) {
	return c.bus.bail(c.goCtx, name, payload)
}

func (c *contextImpl) Effect(name string, disposable Disposable) Disposer {
	if disposable == nil {
		return func() error { return nil }
	}
	d := func() error { return disposable.Dispose() }
	c.stack.add(name, d)
	return d
}

func (c *contextImpl) EffectFunc(name string, fn Disposer) Disposer {
	if fn == nil {
		return func() error { return nil }
	}
	c.stack.add(name, fn)
	return fn
}

func (c *contextImpl) Scope(pluginID string) Context {
	return &contextImpl{
		services: map[string]any{},
		parent:   c,
		bus:      c.bus, // shared bus: child listeners join the same dispatch chain
		stack:    &effectStack{},
		goCtx:    c.goCtx,
		pluginID: pluginID,
	}
}

func (c *contextImpl) GoContext() context.Context { return c.goCtx }

// dispose tears down a scoped context's effects in reverse order.
func (c *contextImpl) dispose() error { return c.stack.disposeAll() }

// UnwrapKey retrieves a typed service from a context.
func UnwrapKey[T any](c Context, key string) (T, bool) {
	var zero T
	svc, ok := c.Service(key)
	if !ok {
		return zero, false
	}
	typed, ok := svc.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}
