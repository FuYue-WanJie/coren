package authz

import "testing"

func TestParseLevels(t *testing.T) {
	cases := []struct {
		in    string
		want  Level
		valid bool
	}{
		{"", Trusted, true},
		{"trusted", Trusted, true},
		{"readonly", Readonly, true},
		{"read-only", Readonly, true},
		{"ro", Readonly, true},
		{"full", Full, true},
		{"bogus", Trusted, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if got != c.want || ok != c.valid {
			t.Errorf("Parse(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestLevelPermits(t *testing.T) {
	if Readonly.Permits(ActionWrite) || Readonly.Permits(ActionExecute) {
		t.Error("readonly must not permit write/execute")
	}
	if !Readonly.Permits(ActionRead) {
		t.Error("readonly should permit read")
	}
	if !Trusted.Permits(ActionWrite) || !Trusted.Permits(ActionExecute) {
		t.Error("trusted should permit write/execute")
	}
	if !Full.Permits(ActionExecute) {
		t.Error("full should permit everything")
	}
}

func TestActionForTool(t *testing.T) {
	cases := map[string]Action{
		"read_file":    ActionRead,
		"write_file":   ActionWrite,
		"run_shell":    ActionExecute,
		"http_request": ActionNetwork,
		"unknown-tool": ActionExecute, // conservative
	}
	for tool, want := range cases {
		if got := ActionForTool(tool); got != want {
			t.Errorf("ActionForTool(%q) = %v, want %v", tool, got, want)
		}
	}
}

func TestLevelAllowsOrdering(t *testing.T) {
	if !Full.Allows(Readonly) || !Trusted.Allows(Readonly) {
		t.Error("higher levels should allow lower requirements")
	}
	if Readonly.Allows(Trusted) {
		t.Error("readonly must not allow trusted requirement")
	}
}
