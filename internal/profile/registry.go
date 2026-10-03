package profile

import (
	"fmt"
	"sort"
	"sync"

	"coren/pkg/coren"
)

// Factory builds a plugin from runtime configuration.
//
// External code registers factories so a profile can name plugins the framework
// does not ship, keeping composition open for extension.
type Factory func(cfg any) (coren.Plugin, error)

// Registry maps plugin ids to factories.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// Register adds or replaces a factory.
func (r *Registry) Register(id string, factory Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[id] = factory
}

// Build instantiates a registered plugin.
func (r *Registry) Build(id string, cfg any) (coren.Plugin, error) {
	r.mu.RLock()
	factory, ok := r.factories[id]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("profile: no factory registered for plugin %q", id)
	}
	return factory(cfg)
}

// Has reports whether a factory is registered.
func (r *Registry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[id]
	return ok
}

// IDs lists registered plugin ids, sorted.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.factories))
	for id := range r.factories {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
