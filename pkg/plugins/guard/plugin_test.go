package guard

import (
	"context"
	"strings"
	"testing"

	"coren/pkg/agent"
	"coren/pkg/approval"
	"coren/pkg/authz"
	"coren/pkg/coren"
	"coren/pkg/tools"
)

// stubRequester records approvals and returns a fixed decision.
type stubRequester struct {
	approved bool
	asked    int
}

func (s *stubRequester) RequestApproval(context.Context, approval.Request) (approval.Decision, error) {
	s.asked++
	return approval.Decision{Approved: s.approved, By: "user"}, nil
}

// mountGuard boots the tools service, guard provider, and guard, plus an
// optional requester.
func mountGuard(t *testing.T, level authz.Level, req approval.Requester) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	plugins := []coren.Plugin{
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		ProviderPlugin{Authz: level},
		Plugin{},
	}
	if req != nil {
		plugins = append(plugins, coren.PluginFunc{Name: "requester", Mount: func(ctx coren.Context) error {
			ctx.Provide(approval.RequesterKey, req)
			return nil
		}})
	}
	if err := k.Boot(plugins...); err != nil {
		t.Fatal(err)
	}
	return k
}

// runPreExecute dispatches the waterfall for a call.
func runPreExecute(t *testing.T, k *coren.Kernel, name, args string) error {
	t.Helper()
	_, err := k.Context().Waterfall(coren.EventToolsPreExecute, &agent.PreExecutePayload{
		Name: name, Arguments: args,
	})
	return err
}

func TestReadonlyBlocksExecute(t *testing.T) {
	k := mountGuard(t, authz.Readonly, nil)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "run_shell", `{"command":"ls"}`); err == nil {
		t.Fatal("readonly should block execute")
	}
}

func TestReadonlyAllowsRead(t *testing.T) {
	k := mountGuard(t, authz.Readonly, nil)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "read_file", `{"path":"x"}`); err != nil {
		t.Errorf("readonly should allow read: %v", err)
	}
}

func TestDenyRuleBlocksDangerousCommand(t *testing.T) {
	k := mountGuard(t, authz.Trusted, nil)
	defer k.Shutdown()
	err := runPreExecute(t, k, "run_shell", `{"command":"rm -rf /"}`)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("recursive delete should be denied: %v", err)
	}
}

func TestConfirmRuleAsksRequester(t *testing.T) {
	req := &stubRequester{approved: true}
	k := mountGuard(t, authz.Trusted, req)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "run_shell", `{"command":"rm file.txt"}`); err != nil {
		t.Errorf("approved risky command should proceed: %v", err)
	}
	if req.asked == 0 {
		t.Error("requester should have been asked")
	}
}

func TestConfirmRuleDeniedByUser(t *testing.T) {
	req := &stubRequester{approved: false}
	k := mountGuard(t, authz.Trusted, req)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "run_shell", `{"command":"sudo rm file.txt"}`); err == nil {
		t.Fatal("user denial should block the call")
	}
}

func TestFullLevelSkipsConfirmation(t *testing.T) {
	// A confirm-level risky command (not a deny) proceeds at full without asking.
	k := mountGuard(t, authz.Full, nil)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "run_shell", `{"command":"sudo rm file.txt"}`); err != nil {
		t.Errorf("full level should skip confirmation: %v", err)
	}
}

func TestDenyRuleAppliesEvenAtFull(t *testing.T) {
	// Deny rules are hard blocks: full authorization must not bypass them.
	k := mountGuard(t, authz.Full, nil)
	defer k.Shutdown()
	if err := runPreExecute(t, k, "run_shell", `{"command":"rm -rf /"}`); err == nil {
		t.Fatal("deny rules must apply at every level")
	}
}
