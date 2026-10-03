package coren

import (
	"context"
	"errors"
	"sync"
)

// Event names. Keep them centralized so plugins cannot drift on spelling.
const (
	// Agent lifecycle and turn flow.
	EventAgentPreStep      = "agent/pre-step"      // waterfall: rewrite or reject claimed input
	EventAgentRequest      = "agent/request"       // waterfall: rewrite the outgoing request
	EventAgentTurnStopping = "agent/turn-stopping" // serial: policy hook before a turn closes

	// Tool pipeline.
	EventToolsPreExecute  = "tools/pre-execute"  // waterfall: intercept or rewrite arguments
	EventToolsPostExecute = "tools/post-execute" // waterfall: process a tool result

	// Model streaming.
	EventLLMStream = "llm/stream" // waterfall: wrap the streamed response

	// Model usage, emitted per turn with token counts. Observational.
	EventModelUsage = "model/usage"

	// Plugin lifecycle (observational).
	EventPluginLoaded   = "plugin/loaded"
	EventPluginUnloaded = "plugin/unloaded"
)

// Handler receives an event payload. Its meaning depends on the dispatch mode.
// Waterfall handlers must call next to delegate; the returned value is the
// possibly-wrapped result.
type Handler func(ctx context.Context, payload any, next func(any) (any, error)) (any, error)

// Observer is a simple listener for emit/parallel events (no next, no result).
type Observer func(ctx context.Context, payload any)

type listener struct {
	id       uint64
	name     string
	handler  Handler
	observer Observer
	prepend  bool
}

// eventBus holds listeners per event name.
type eventBus struct {
	mu     sync.RWMutex
	nextID uint64
	names  map[string]bool
	byName map[string][]listener
}

func newEventBus() *eventBus {
	return &eventBus{names: map[string]bool{}, byName: map[string][]listener{}}
}

// declare registers an event name. Names must be declared before use so typos
// surface as errors rather than silently doing nothing.
func (b *eventBus) declare(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.names[name] = true
}

func (b *eventBus) declared(name string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.names[name]
}

// on registers a handler and returns a disposer that removes it by unique id.
func (b *eventBus) on(name string, l listener) Disposer {
	b.mu.Lock()
	b.nextID++
	l.id = b.nextID
	handlers := b.byName[name]
	if l.prepend {
		handlers = append([]listener{l}, handlers...)
	} else {
		handlers = append(handlers, l)
	}
	b.byName[name] = handlers
	id := l.id
	b.mu.Unlock()

	return func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		current := b.byName[name]
		for i, existing := range current {
			if existing.id == id {
				b.byName[name] = append(current[:i], current[i+1:]...)
				break
			}
		}
		return nil
	}
}

// listeners returns a snapshot of the current listeners.
func (b *eventBus) listeners(name string) []listener {
	b.mu.RLock()
	defer b.mu.RUnlock()
	src := b.byName[name]
	out := make([]listener, len(src))
	copy(out, src)
	return out
}

// emit dispatches to observers in registration order. No return value.
func (b *eventBus) emit(ctx context.Context, name string, payload any) {
	for _, l := range b.listeners(name) {
		if l.observer != nil {
			l.observer(ctx, payload)
		}
	}
}

// waterfall runs around-middleware: each handler receives next to delegate.
func (b *eventBus) waterfall(ctx context.Context, name string, payload any) (any, error) {
	ls := b.listeners(name)
	var run func(i int, current any) (any, error)
	run = func(i int, current any) (any, error) {
		if i >= len(ls) {
			return current, nil
		}
		l := ls[i]
		if l.handler == nil {
			return run(i+1, current)
		}
		return l.handler(ctx, current, func(next any) (any, error) {
			return run(i+1, next)
		})
	}
	return run(0, payload)
}

// serial runs handlers in registration order, threading a value through.
func (b *eventBus) serial(ctx context.Context, name string, payload any) (any, error) {
	current := payload
	for _, l := range b.listeners(name) {
		if l.handler == nil {
			continue
		}
		nextCalled := false
		v, err := l.handler(ctx, current, func(next any) (any, error) {
			nextCalled = true
			current = next
			return next, nil
		})
		if err != nil {
			return current, err
		}
		if nextCalled {
			continue
		}
		current = v
	}
	return current, nil
}

// bail stops at the first handler that returns a non-nil result without delegating.
func (b *eventBus) bail(ctx context.Context, name string, payload any) (any, error) {
	for _, l := range b.listeners(name) {
		if l.handler == nil {
			continue
		}
		delegated := false
		v, err := l.handler(ctx, payload, func(next any) (any, error) {
			delegated = true
			return next, nil
		})
		if err != nil {
			return nil, err
		}
		if !delegated && v != nil {
			return v, nil
		}
	}
	return nil, nil
}

// ErrEventNotDeclared is returned when dispatching an undeclared event.
var ErrEventNotDeclared = errors.New("event not declared")
