package profile

import "testing"

func TestResolveDefaultsToWeb(t *testing.T) {
	p, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "web" {
		t.Errorf("name = %q, want web", p.Name)
	}
}

func TestResolveUnknownFails(t *testing.T) {
	if _, err := Resolve("nope"); err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestWebProfileIncludesShell(t *testing.T) {
	p, _ := Resolve("web")
	if !p.Has(PluginShellWeb) {
		t.Error("web profile should include the web shell")
	}
	if p.Has(PluginShellCLI) {
		t.Error("web profile should not include the cli shell")
	}
}

func TestCliProfileIncludesShell(t *testing.T) {
	p, _ := Resolve("cli")
	if !p.Has(PluginShellCLI) {
		t.Error("cli profile should include the cli shell")
	}
}

func TestHeadlessProfileHasNoShell(t *testing.T) {
	p, _ := Resolve("headless")
	if p.Has(PluginShellWeb) || p.Has(PluginShellCLI) {
		t.Error("headless profile should include no shell")
	}
}

func TestAllProfilesListed(t *testing.T) {
	names := Names()
	if len(names) < 4 {
		t.Errorf("names = %v, want at least core/web/cli/headless", names)
	}
	// Each canonical profile must be present.
	for _, want := range []string{"core", "web", "cli", "headless"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("profile %q missing from %v", want, names)
		}
	}
	// Names must be sorted for stable output.
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("names not sorted: %v", names)
		}
	}
}

func TestClockProfileIncludesSamplePlugin(t *testing.T) {
	p, err := Resolve("cli-clock")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Has(PluginToolsClock) {
		t.Error("cli-clock profile should include the example clock tool")
	}
	if !p.Has(PluginShellCLI) {
		t.Error("cli-clock profile should include the cli shell")
	}
}
