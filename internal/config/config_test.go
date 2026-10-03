package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(`{
		"api": "responses",
		"model": "custom-model",
		"base_url": "https://example.test/v1"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := defaults(dir)
	applyFile(&cfg, path)

	if cfg.API != "responses" {
		t.Errorf("api = %q", cfg.API)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("model = %q", cfg.Model)
	}
	if cfg.BaseURL != "https://example.test/v1" {
		t.Errorf("base_url = %q", cfg.BaseURL)
	}
	// Unset keys keep their defaults.
	if cfg.Addr != "127.0.0.1:8787" {
		t.Errorf("addr = %q, want default", cfg.Addr)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(`{"model":"from-file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COREN_MODEL", "from-env")

	cfg := defaults(dir)
	applyFile(&cfg, path)
	applyEnv(&cfg)

	if cfg.Model != "from-env" {
		t.Errorf("model = %q, want env to win", cfg.Model)
	}
}

func TestEnvTemperature(t *testing.T) {
	t.Setenv("COREN_TEMPERATURE", "0.3")
	cfg := defaults(t.TempDir())
	applyEnv(&cfg)
	if cfg.Temperature == nil || *cfg.Temperature != 0.3 {
		t.Errorf("temperature = %v", cfg.Temperature)
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", FileName)
	want := defaults(dir)
	want.APIKey = "secret"
	if err := WriteFile(path, want); err != nil {
		t.Fatal(err)
	}

	got := defaults(dir)
	applyFile(&got, path)
	if got.APIKey != "secret" {
		t.Errorf("api_key = %q", got.APIKey)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestInvalidConfigFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := defaults(dir)
	applyFile(&cfg, path) // must not panic; keeps defaults
	if cfg.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want default after invalid file", cfg.Model)
	}
}
