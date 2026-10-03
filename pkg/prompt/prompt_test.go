package prompt

import (
	"strings"
	"testing"

	"coren/pkg/ctxfiles"
)

func TestBuildCustomReplacesBase(t *testing.T) {
	out := Build(Options{Base: "default identity", Custom: "custom identity"})
	if !strings.Contains(out, "custom identity") {
		t.Errorf("out = %q", out)
	}
	if strings.Contains(out, "default identity") {
		t.Errorf("custom should replace base: %q", out)
	}
}

func TestBuildOrdersSections(t *testing.T) {
	out := Build(Options{
		Base:       "identity",
		Guidelines: []string{"be brief"},
		ContextFiles: []ctxfiles.File{
			{Path: "/p/AGENTS.md", Content: "project rule"},
		},
		Append: []string{"appendix text"},
	})

	identityIdx := strings.Index(out, "identity")
	guidelineIdx := strings.Index(out, "be brief")
	contextIdx := strings.Index(out, "project rule")
	appendIdx := strings.Index(out, "appendix text")

	if !(identityIdx < guidelineIdx && guidelineIdx < contextIdx && contextIdx < appendIdx) {
		t.Errorf("section order wrong:\n%s", out)
	}
}

func TestBuildSkipsBlankItems(t *testing.T) {
	out := Build(Options{Base: "x", Guidelines: []string{"  ", ""}, Append: []string{"  "}})
	if strings.Contains(out, "## Guidelines") {
		t.Errorf("blank guidelines should not create a section: %q", out)
	}
}

func TestBuildEmptyWhenNothingGiven(t *testing.T) {
	if got := Build(Options{}); got != "" {
		t.Errorf("empty options should build empty string, got %q", got)
	}
}
