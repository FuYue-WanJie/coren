package risk

import "testing"

func engine(t *testing.T, rules []Rule) *Engine {
	t.Helper()
	e, err := NewEngine(rules)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDefaultRulesDenyRecursiveDelete(t *testing.T) {
	e := engine(t, DefaultRules())
	f := e.Evaluate("run_shell", `{"command":"rm -rf /tmp/x"}`)
	if !f.Denied() {
		t.Errorf("rm -rf should be denied: %+v", f)
	}
}

func TestDefaultRulesConfirmPlainDelete(t *testing.T) {
	e := engine(t, DefaultRules())
	f := e.Evaluate("run_shell", `{"command":"rm file.txt"}`)
	if !f.NeedsConfirmation() || f.Denied() {
		t.Errorf("rm should require confirmation: %+v", f)
	}
}

func TestPrivilegeEscalationConfirms(t *testing.T) {
	e := engine(t, DefaultRules())
	f := e.Evaluate("run_shell", `{"command":"sudo apt update"}`)
	if f.Severity != Confirm {
		t.Errorf("sudo should confirm: %+v", f)
	}
}

func TestBenignCommandMatchesNothing(t *testing.T) {
	e := engine(t, DefaultRules())
	f := e.Evaluate("run_shell", `{"command":"ls -la"}`)
	if f.NeedsConfirmation() {
		t.Errorf("ls should be clean: %+v", f)
	}
}

func TestRuleScopedToTool(t *testing.T) {
	e := engine(t, []Rule{{Name: "only-shell", Severity: Deny, Tools: []string{"run_shell"}, Pattern: "danger"}})
	// Same regex, different tool: no match.
	if f := e.Evaluate("write_file", `{"content":"danger"}`); f.NeedsConfirmation() {
		t.Errorf("scoped rule should not apply to other tools: %+v", f)
	}
	if f := e.Evaluate("run_shell", `{"command":"echo danger"}`); !f.Denied() {
		t.Errorf("scoped rule should apply to run_shell: %+v", f)
	}
}

func TestDenyDominatesConfirm(t *testing.T) {
	e := engine(t, []Rule{
		{Name: "confirm", Severity: Confirm, Pattern: "x"},
		{Name: "deny", Severity: Deny, Pattern: "x"},
	})
	f := e.Evaluate("run_shell", `{"command":"x"}`)
	if !f.Denied() {
		t.Errorf("deny should dominate: %+v", f)
	}
}

func TestInvalidPatternFails(t *testing.T) {
	if _, err := NewEngine([]Rule{{Name: "bad", Pattern: "("}}); err == nil {
		t.Fatal("invalid regex should fail compilation")
	}
}
