package subagents

import (
	"context"
	"testing"
)

type fakeProvider struct{ name string }

func (f fakeProvider) Name() string                                     { return f.name }
func (fakeProvider) Start(_ context.Context, _ Request) (Result, error) { return Result{}, nil }

func TestRegistryRegisterGetDefault(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Default(); ok {
		t.Error("empty registry should have no default")
	}

	r.Register(fakeProvider{name: "first"})
	r.Register(fakeProvider{name: "second"})

	if _, ok := r.Get("first"); !ok {
		t.Error("first not found")
	}
	def, ok := r.Default()
	if !ok || def.Name() != "first" {
		t.Errorf("default = %v, want first-registered", def)
	}
	names := r.Names()
	if len(names) != 2 || names[0] != "first" || names[1] != "second" {
		t.Errorf("names = %v", names)
	}
}

func TestRegistryReplaceKeepsOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeProvider{name: "a"})
	r.Register(fakeProvider{name: "b"})
	r.Register(fakeProvider{name: "a"}) // replace

	names := r.Names()
	if len(names) != 2 || names[0] != "a" {
		t.Errorf("names = %v, want [a b] with a still first", names)
	}
}
