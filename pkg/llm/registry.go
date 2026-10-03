package llm

import "sort"

// Key is the service key for the llm service.
const Key = "llm"

// Registry is the default in-memory implementation of Service.
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

func (r *Registry) Register(adapter Adapter) {
	if adapter == nil {
		return
	}
	r.adapters[adapter.Name()] = adapter
}

func (r *Registry) Get(name string) (Adapter, bool) {
	a, ok := r.adapters[name]
	return a, ok
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
