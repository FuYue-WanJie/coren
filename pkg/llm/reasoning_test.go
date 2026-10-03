package llm

import "testing"

func TestParseReasoningLevel(t *testing.T) {
	cases := []struct {
		in    string
		want  ReasoningLevel
		valid bool
	}{
		{"", ReasoningAuto, true},
		{"auto", ReasoningAuto, true},
		{"AUTO", ReasoningAuto, true},
		{"off", ReasoningOff, true},
		{"none", ReasoningOff, true},
		{"minimal", ReasoningMinimal, true},
		{"low", ReasoningLow, true},
		{"medium", ReasoningMedium, true},
		{"high", ReasoningHigh, true},
		{" high ", ReasoningHigh, true},
		{"bogus", ReasoningAuto, false},
	}
	for _, c := range cases {
		got, ok := ParseReasoningLevel(c.in)
		if got != c.want || ok != c.valid {
			t.Errorf("ParseReasoningLevel(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestReasoningEffortValue(t *testing.T) {
	if ReasoningAuto.EffortValue() != "" {
		t.Error("auto should not send an effort value")
	}
	if ReasoningOff.EffortValue() != "" {
		t.Error("off should not send an effort value")
	}
	if ReasoningHigh.EffortValue() != "high" {
		t.Errorf("high effort = %q", ReasoningHigh.EffortValue())
	}
}

func TestWantsReasoning(t *testing.T) {
	if ReasoningOff.WantsReasoning() {
		t.Error("off should not want reasoning")
	}
	if !ReasoningMedium.WantsReasoning() {
		t.Error("medium should want reasoning")
	}
	if !ReasoningAuto.WantsReasoning() {
		t.Error("auto should leave reasoning enabled by default")
	}
}
