package coren

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Kernel assembles a plugin tree onto a root Context.
type Kernel struct {
	root  Context
	mu    sync.Mutex
	order []string // applied plugin ids in application order
	plugs map[string]pluginState
}

type pluginState struct {
	plugin     Plugin
	ctx        Context
	disposable Disposer
}

// NewKernel creates a kernel with a fresh root context.
func NewKernel(goCtx context.Context) *Kernel {
	root := NewContext(goCtx)
	if impl, ok := root.(*contextImpl); ok {
		impl.declareBuiltinEvents()
	}
	return &Kernel{root: root, plugs: map[string]pluginState{}}
}

// Context returns the root context.
func (k *Kernel) Context() Context { return k.root }

// Boot applies plugins in dependency order. A plugin whose Inject keys are not
// satisfied by the time it is reached (after ordering) causes Boot to fail and
// roll back everything already applied.
func (k *Kernel) Boot(plugins ...Plugin) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, p := range plugins {
		if p == nil {
			return errors.New("nil plugin")
		}
		if err := k.applyRecursive(p, map[string]bool{}); err != nil {
			_ = k.teardownLocked()
			return err
		}
	}
	return nil
}

// applyRecursive applies a plugin after its declared dependencies.
func (k *Kernel) applyRecursive(p Plugin, visiting map[string]bool) error {
	id := p.ID()
	if _, done := k.plugs[id]; done {
		return nil
	}
	if visiting[id] {
		return fmt.Errorf("plugin dependency cycle at %q", id)
	}
	visiting[id] = true
	defer delete(visiting, id)

	// Dependencies must already be mounted. Plugins are supplied in Boot order,
	// so a dependency referenced before its provider is a declaration error.
	for _, key := range p.Inject() {
		if _, ok := k.root.Service(key); !ok {
			return fmt.Errorf("plugin %q requires service %q, which is not registered", id, key)
		}
	}

	child := k.root.Scope(id)
	if err := p.Apply(child); err != nil {
		_ = child.(*contextImpl).dispose()
		return fmt.Errorf("applying plugin %q: %w", id, err)
	}

	k.plugs[id] = pluginState{
		plugin:     p,
		ctx:        child,
		disposable: func() error { return child.(*contextImpl).dispose() },
	}
	k.order = append(k.order, id)

	k.root.Emit(EventPluginLoaded, id)
	return nil
}

// Start runs Start on every Startable plugin in application order.
func (k *Kernel) Start() error {
	k.mu.Lock()
	order := append([]string(nil), k.order...)
	states := make(map[string]pluginState, len(k.plugs))
	for id, st := range k.plugs {
		states[id] = st
	}
	k.mu.Unlock()

	for _, id := range order {
		st := states[id]
		if s, ok := st.plugin.(Startable); ok {
			if err := s.Start(st.ctx); err != nil {
				return fmt.Errorf("starting plugin %q: %w", id, err)
			}
		}
	}
	return nil
}

// Shutdown stops plugins in reverse order, then unwinds every registration.
func (k *Kernel) Shutdown() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.teardownLocked()
}

func (k *Kernel) teardownLocked() error {
	var errs []error

	// Stop in reverse application order.
	for i := len(k.order) - 1; i >= 0; i-- {
		id := k.order[i]
		st, ok := k.plugs[id]
		if !ok {
			continue
		}
		if s, ok := st.plugin.(Startable); ok {
			if err := s.Stop(st.ctx); err != nil {
				errs = append(errs, fmt.Errorf("stopping plugin %q: %w", id, err))
			}
		}
	}

	// Dispose effects in reverse order.
	for i := len(k.order) - 1; i >= 0; i-- {
		id := k.order[i]
		st, ok := k.plugs[id]
		if !ok {
			continue
		}
		if st.disposable != nil {
			if err := st.disposable(); err != nil {
				errs = append(errs, err)
			}
		}
		k.root.Emit(EventPluginUnloaded, id)
	}

	k.order = nil
	k.plugs = map[string]pluginState{}
	return errors.Join(errs...)
}

// AppliedPlugins returns plugin ids in application order.
func (k *Kernel) AppliedPlugins() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.order...)
}
