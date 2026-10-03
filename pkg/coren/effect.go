// Package coren is the plugin kernel for the Coren agent framework.
//
// Everything in Coren is a plugin: the model adapters, the tool registry, the
// session log, and the agent loop itself. A plugin contributes services, event
// listeners, and other reversible effects to a shared Context. Nothing is
// privileged; behaviour is extended by mounting a plugin beside the others, and
// every registration unwinds when its plugin unloads.
package coren

import (
	"errors"
	"sync"
)

// Disposer reverses a single registration.
type Disposer func() error

// Disposable is implemented by anything that can undo its effect.
type Disposable interface {
	Dispose() error
}

// effectStack collects disposers in registration order and runs them in reverse,
// so teardown mirrors setup. It is safe for concurrent use.
type effectStack struct {
	mu     sync.Mutex
	items  []namedDisposer
	closed bool
}

type namedDisposer struct {
	name string
	fn   Disposer
}

func (s *effectStack) add(name string, fn Disposer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		// Already torn down: dispose immediately so the caller never leaks.
		if fn != nil {
			_ = fn()
		}
		return
	}
	s.items = append(s.items, namedDisposer{name: name, fn: fn})
}

// disposeAll runs every disposer in reverse registration order, collecting errors.
func (s *effectStack) disposeAll() error {
	s.mu.Lock()
	items := s.items
	s.items = nil
	s.closed = true
	s.mu.Unlock()

	var errs []error
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].fn == nil {
			continue
		}
		if err := items[i].fn(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
