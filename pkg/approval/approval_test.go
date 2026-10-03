package approval

import (
	"context"
	"errors"
	"testing"

	"coren/pkg/authz"
)

type fakeRequester struct {
	decision Decision
	err      error
	called   bool
}

func (f *fakeRequester) RequestApproval(context.Context, Request) (Decision, error) {
	f.called = true
	return f.decision, f.err
}

func TestReadonlyDeniesWrite(t *testing.T) {
	g := New(authz.Readonly)
	d, err := g.Approve(context.Background(), Request{Tool: "write_file", Action: authz.ActionWrite})
	if err != nil {
		t.Fatal(err)
	}
	if d.Approved {
		t.Error("readonly must deny write")
	}
	if d.By != "policy" {
		t.Errorf("decider = %q", d.By)
	}
}

func TestFullSkipsRequester(t *testing.T) {
	req := &fakeRequester{decision: Decision{Approved: false}}
	g := New(authz.Full)
	g.Requester = req
	d, _ := g.Approve(context.Background(), Request{Tool: "run_shell", Action: authz.ActionExecute})
	if !d.Approved {
		t.Error("full should approve without asking")
	}
	if req.called {
		t.Error("requester should not be consulted at full level")
	}
}

func TestTrustedConsultsRequester(t *testing.T) {
	req := &fakeRequester{decision: Decision{Approved: true}}
	g := New(authz.Trusted)
	g.Requester = req
	d, _ := g.Approve(context.Background(), Request{Tool: "run_shell", Action: authz.ActionExecute})
	if !d.Approved || !req.called {
		t.Errorf("trusted should consult the requester: %+v", d)
	}
	if d.By != "user" {
		t.Errorf("decider = %q, want user", d.By)
	}
}

func TestTrustedWithoutRequesterAllows(t *testing.T) {
	g := New(authz.Trusted)
	d, _ := g.Approve(context.Background(), Request{Tool: "run_shell", Action: authz.ActionExecute})
	if !d.Approved || d.By != "default" {
		t.Errorf("no requester should default-allow permitted actions: %+v", d)
	}
}

func TestResolveOverridesRequester(t *testing.T) {
	resolved := &fakeRequester{decision: Decision{Approved: true}}
	g := New(authz.Trusted)
	g.Resolve = func() (Requester, bool) { return resolved, true }
	d, _ := g.Approve(context.Background(), Request{Tool: "run_shell", Action: authz.ActionExecute})
	if !d.Approved || !resolved.called {
		t.Error("lazy resolver should provide the requester")
	}
}

func TestRequesterErrorDenies(t *testing.T) {
	g := New(authz.Trusted)
	g.Requester = &fakeRequester{err: errors.New("boom")}
	d, err := g.Approve(context.Background(), Request{Tool: "run_shell", Action: authz.ActionExecute})
	if err == nil || d.Approved {
		t.Error("requester error should deny")
	}
}
