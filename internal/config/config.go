// Package config loads Coren settings with layered precedence:
// defaults < user config file < project config file < environment variables.
package config

import (
	"coren/pkg/modelinfo"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the per-project configuration file name.
const FileName = "coren.json"

// Config holds runtime settings for the CLI and server.
type Config struct {
	// Profile names the composition to boot; empty uses the launcher default.
	Profile string `json:"profile,omitempty"`
	// Plugins, when non-empty, replaces the profile's plugin list entirely.
	// Each entry is a plugin id (e.g. "shell.web", "memory").
	Plugins []string `json:"plugins,omitempty"`
	// AddPlugins appends plugin ids to the profile's list.
	AddPlugins []string `json:"add_plugins,omitempty"`
	// RemovePlugins drops plugin ids from the profile's list.
	RemovePlugins []string `json:"remove_plugins,omitempty"`
	// ExternalPlugins lists out-of-process plugins the host launches.
	ExternalPlugins []ExternalPlugin `json:"external_plugins,omitempty"`
	// API selects the provider flavour: "chat" or "responses".
	API string `json:"api"`
	// BaseURL is the OpenAI-compatible endpoint root, e.g. https://api.openai.com/v1.
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	// System is the default system prompt.
	System string `json:"system"`
	// WorkDir scopes file/shell tools.
	WorkDir string `json:"work_dir"`
	// Addr is the WebUI/API listen address.
	Addr string `json:"addr"`
	// Password gates non-loopback access; empty disables authentication. When the
	// listener is not loopback, an empty password makes startup fail.
	Password string `json:"password,omitempty"`
	// SessionTTLHours is how long a login session stays valid; zero uses 168 (7d).
	SessionTTLHours int `json:"session_ttl_hours,omitempty"`
	// SessionDir stores the append-only session logs; empty disables persistence.
	SessionDir string `json:"session_dir"`
	// SkillsDir is the skill directory to scan; empty disables file discovery.
	SkillsDir string `json:"skills_dir"`
	// Temperature and MaxTokens are optional generation controls.
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	// MaxSteps bounds tool-call round trips per turn.
	MaxSteps int `json:"max_steps,omitempty"`
	// MCP lists remote Model Context Protocol servers to connect at boot.
	MCP []MCPServer `json:"mcp,omitempty"`
	// ContextFiles configures project instruction-file discovery.
	ContextFiles ContextFiles `json:"context_files,omitempty"`
	// MemoryPath is the project memory file; empty uses MEMORY.md in WorkDir,
	// and a single "-" disables memory.
	MemoryPath string `json:"memory_path,omitempty"`
	// TodoPath is the task list file; empty uses TODO.md in WorkDir, and a
	// single "-" disables the todo tool.
	TodoPath string `json:"todo_path,omitempty"`
	// Guidelines are extra bullet lines appended to the system prompt.
	Guidelines []string `json:"guidelines,omitempty"`
	// AppendSystemPrompt is free text appended to the system prompt.
	AppendSystemPrompt []string `json:"append_system_prompt,omitempty"`
	// CustomSystemPrompt replaces the default system prompt when non-empty.
	CustomSystemPrompt string `json:"custom_system_prompt,omitempty"`
	// CacheRetention controls prompt caching: "short" (default), "long", "none".
	CacheRetention string `json:"cache_retention,omitempty"`
	// Reasoning is the thinking level: auto (default), off, minimal, low, medium, high.
	Reasoning string `json:"reasoning,omitempty"`
	// Authz is the authorization level: readonly, trusted (default), full.
	Authz string `json:"authz,omitempty"`
	// DeliverAutoApprove lets deliverables pass without a reviewer (non-interactive
	// shells). When false, a deliverable with no reviewer is rejected.
	DeliverAutoApprove bool `json:"deliver_auto_approve,omitempty"`
	// RiskRules adds or overrides risk rules; defaults are used unless disabled.
	RiskRules []RiskRule `json:"risk_rules,omitempty"`
	// DisableDefaultRiskRules omits the built-in risk rules.
	DisableDefaultRiskRules bool `json:"disable_default_risk_rules,omitempty"`
	// ToolTimeout bounds each tool call in seconds; zero uses per-tool defaults.
	ToolTimeout int `json:"tool_timeout,omitempty"`
	// MaxRetries is the provider retry count for transient failures; zero uses a default.
	MaxRetries int `json:"max_retries,omitempty"`
	// AutoModelInfo fetches model metadata from the provider at boot.
	AutoModelInfo bool `json:"auto_model_info,omitempty"`
	// CompactAfter, when > 0, summarizes the session once it exceeds this many
	// messages to bound token use.
	CompactAfter int `json:"compact_after,omitempty"`
	// Models overrides per-model capabilities by model id.
	Models map[string]ModelOverride `json:"models,omitempty"`
	// CatalogPath overrides the cached models.dev catalog location.
	CatalogPath string `json:"catalog_path,omitempty"`
	// RefreshCatalog fetches a fresh catalog at boot.
	RefreshCatalog bool `json:"refresh_catalog,omitempty"`
}

// RiskRule is a configurable risk rule.
type RiskRule struct {
	Name     string   `json:"name"`
	Severity string   `json:"severity"`
	Tools    []string `json:"tools,omitempty"`
	Pattern  string   `json:"pattern"`
	Reason   string   `json:"reason,omitempty"`
}

// ModelOverride lets configuration pin or correct a model's capabilities.
type ModelOverride struct {
	Reasoning  *bool                 `json:"reasoning,omitempty"`
	ToolCall   *bool                 `json:"tool_call,omitempty"`
	Attachment *bool                 `json:"attachment,omitempty"`
	Modalities *modelinfo.Modalities `json:"modalities,omitempty"`
	Limit      *modelinfo.Limits     `json:"limit,omitempty"`
	Cost       *modelinfo.Cost       `json:"cost,omitempty"`
}

// ContextFiles configures instruction-file discovery.
type ContextFiles struct {
	// Disabled turns off all discovery.
	Disabled bool `json:"disabled,omitempty"`
	// FixedPaths are explicit files relative to WorkDir, checked first.
	FixedPaths []string `json:"fixed_paths,omitempty"`
}

// fileConfig mirrors Config but every field is a pointer so that an unset key in
// a file does not overwrite a lower layer. It also accepts a template file.
type fileConfig struct {
	API             *string          `json:"api"`
	BaseURL         *string          `json:"base_url"`
	APIKey          *string          `json:"api_key"`
	Model           *string          `json:"model"`
	Profile         *string          `json:"profile,omitempty"`
	Plugins         []string         `json:"plugins,omitempty"`
	AddPlugins      []string         `json:"add_plugins,omitempty"`
	RemovePlugins   []string         `json:"remove_plugins,omitempty"`
	ExternalPlugins []ExternalPlugin `json:"external_plugins,omitempty"`
	System          *string          `json:"system"`
	WorkDir         *string          `json:"work_dir"`
	Addr            *string          `json:"addr"`
	SessionDir      *string          `json:"session_dir"`
	SkillsDir       *string          `json:"skills_dir"`
	Password        *string          `json:"password,omitempty"`
	SessionTTLHours *int             `json:"session_ttl_hours,omitempty"`
	Temperature     *float64         `json:"temperature"`
	MaxTokens       *int             `json:"max_tokens"`
	MaxSteps        *int             `json:"max_steps"`
	MCP             []MCPServer      `json:"mcp,omitempty"`

	ContextFiles            *ContextFiles            `json:"context_files,omitempty"`
	MemoryPath              *string                  `json:"memory_path,omitempty"`
	TodoPath                *string                  `json:"todo_path,omitempty"`
	Guidelines              []string                 `json:"guidelines,omitempty"`
	AppendSystemPrompt      []string                 `json:"append_system_prompt,omitempty"`
	CustomSystemPrompt      *string                  `json:"custom_system_prompt,omitempty"`
	CacheRetention          *string                  `json:"cache_retention,omitempty"`
	Reasoning               *string                  `json:"reasoning,omitempty"`
	Authz                   *string                  `json:"authz,omitempty"`
	DeliverAutoApprove      *bool                    `json:"deliver_auto_approve,omitempty"`
	RiskRules               []RiskRule               `json:"risk_rules,omitempty"`
	DisableDefaultRiskRules *bool                    `json:"disable_default_risk_rules,omitempty"`
	ToolTimeout             *int                     `json:"tool_timeout,omitempty"`
	MaxRetries              *int                     `json:"max_retries,omitempty"`
	AutoModelInfo           *bool                    `json:"auto_model_info,omitempty"`
	CompactAfter            *int                     `json:"compact_after,omitempty"`
	Models                  map[string]ModelOverride `json:"models,omitempty"`
	CatalogPath             *string                  `json:"catalog_path,omitempty"`
	RefreshCatalog          *bool                    `json:"refresh_catalog,omitempty"`
}

// MCPServer describes one remote MCP server in configuration.
type MCPServer struct {
	Name      string            `json:"name"`
	URL       string            `json:"url"`
	Transport string            `json:"transport,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
}

// ExternalPlugin describes one out-of-process plugin for the plugin host.
type ExternalPlugin struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
	Env      []string `json:"env,omitempty"`
	Dir      string   `json:"dir,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// UserConfigPath returns the platform user config file path.
func UserConfigPath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "coren", FileName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "coren", FileName)
}

// ProjectConfigPath returns the project config file path for a directory.
func ProjectConfigPath(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load reads configuration with layered precedence.
//
//	COREN_API       chat | responses        (default chat)
//	COREN_BASE_URL  OpenAI-compatible root  (default https://api.openai.com/v1)
//	COREN_API_KEY   bearer token            (optional)
//	COREN_MODEL     model name              (default gpt-4o-mini)
//	COREN_SYSTEM    system prompt           (optional)
//	COREN_WORKDIR   tool working directory  (default current directory)
//	COREN_ADDR      server listen address   (default 127.0.0.1:8787)
//	COREN_CONFIG    explicit config file path
func Load() (Config, error) {
	wd, _ := os.Getwd()

	cfg := defaults(wd)

	// Project config (lowest file precedence), then user config.
	applyFile(&cfg, ProjectConfigPath(wd))
	applyFile(&cfg, UserConfigPath())
	if explicit := strings.TrimSpace(os.Getenv("COREN_CONFIG")); explicit != "" {
		applyFile(&cfg, explicit)
	}

	applyEnv(&cfg)
	return cfg, nil
}

func defaults(wd string) Config {
	return Config{
		API:        "chat",
		BaseURL:    "https://api.openai.com/v1",
		Model:      "gpt-4o-mini",
		System:     defaultSystem,
		WorkDir:    wd,
		Addr:       "127.0.0.1:8787",
		SessionDir: filepath.Join(wd, ".coren", "sessions"),
		SkillsDir:  filepath.Join(wd, ".coren", "skills"),
	}
}

// applyFile merges a JSON config file into cfg; missing files are ignored.
func applyFile(cfg *Config, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid config %s: %v\n", path, err)
		return
	}
	if fc.API != nil {
		cfg.API = *fc.API
	}
	if fc.BaseURL != nil {
		cfg.BaseURL = *fc.BaseURL
	}
	if fc.APIKey != nil {
		cfg.APIKey = *fc.APIKey
	}
	if fc.Model != nil {
		cfg.Model = *fc.Model
	}
	if fc.Profile != nil {
		cfg.Profile = *fc.Profile
	}
	if len(fc.Plugins) > 0 {
		cfg.Plugins = fc.Plugins
	}
	if len(fc.AddPlugins) > 0 {
		cfg.AddPlugins = append(cfg.AddPlugins, fc.AddPlugins...)
	}
	if len(fc.RemovePlugins) > 0 {
		cfg.RemovePlugins = append(cfg.RemovePlugins, fc.RemovePlugins...)
	}
	if len(fc.ExternalPlugins) > 0 {
		cfg.ExternalPlugins = fc.ExternalPlugins
	}
	if fc.System != nil {
		cfg.System = *fc.System
	}
	if fc.WorkDir != nil {
		cfg.WorkDir = *fc.WorkDir
	}
	if fc.Addr != nil {
		cfg.Addr = *fc.Addr
	}
	if fc.SessionDir != nil {
		cfg.SessionDir = *fc.SessionDir
	}
	if fc.SkillsDir != nil {
		cfg.SkillsDir = *fc.SkillsDir
	}
	if fc.Password != nil {
		cfg.Password = *fc.Password
	}
	if fc.SessionTTLHours != nil {
		cfg.SessionTTLHours = *fc.SessionTTLHours
	}
	if len(fc.MCP) > 0 {
		cfg.MCP = fc.MCP
	}
	if fc.ContextFiles != nil {
		cfg.ContextFiles = *fc.ContextFiles
	}
	if fc.MemoryPath != nil {
		cfg.MemoryPath = *fc.MemoryPath
	}
	if fc.TodoPath != nil {
		cfg.TodoPath = *fc.TodoPath
	}
	if len(fc.Guidelines) > 0 {
		cfg.Guidelines = fc.Guidelines
	}
	if len(fc.AppendSystemPrompt) > 0 {
		cfg.AppendSystemPrompt = fc.AppendSystemPrompt
	}
	if fc.CustomSystemPrompt != nil {
		cfg.CustomSystemPrompt = *fc.CustomSystemPrompt
	}
	if fc.CacheRetention != nil {
		cfg.CacheRetention = *fc.CacheRetention
	}
	if fc.Reasoning != nil {
		cfg.Reasoning = *fc.Reasoning
	}
	if fc.Authz != nil {
		cfg.Authz = *fc.Authz
	}
	if fc.DeliverAutoApprove != nil {
		cfg.DeliverAutoApprove = *fc.DeliverAutoApprove
	}
	if len(fc.RiskRules) > 0 {
		cfg.RiskRules = fc.RiskRules
	}
	if fc.DisableDefaultRiskRules != nil {
		cfg.DisableDefaultRiskRules = *fc.DisableDefaultRiskRules
	}
	if fc.ToolTimeout != nil {
		cfg.ToolTimeout = *fc.ToolTimeout
	}
	if fc.MaxRetries != nil {
		cfg.MaxRetries = *fc.MaxRetries
	}
	if fc.AutoModelInfo != nil {
		cfg.AutoModelInfo = *fc.AutoModelInfo
	}
	if fc.CompactAfter != nil {
		cfg.CompactAfter = *fc.CompactAfter
	}
	if len(fc.Models) > 0 {
		cfg.Models = fc.Models
	}
	if fc.CatalogPath != nil {
		cfg.CatalogPath = *fc.CatalogPath
	}
	if fc.RefreshCatalog != nil {
		cfg.RefreshCatalog = *fc.RefreshCatalog
	}
	if fc.Temperature != nil {
		cfg.Temperature = fc.Temperature
	}
	if fc.MaxTokens != nil {
		cfg.MaxTokens = fc.MaxTokens
	}
	if fc.MaxSteps != nil {
		cfg.MaxSteps = *fc.MaxSteps
	}
}

// applyEnv overrides cfg from environment variables (highest precedence).
func applyEnv(cfg *Config) {
	cfg.API = envOr("COREN_API", cfg.API)
	cfg.BaseURL = envOr("COREN_BASE_URL", cfg.BaseURL)
	if v := strings.TrimSpace(os.Getenv("COREN_API_KEY")); v != "" {
		cfg.APIKey = v
	}
	cfg.Model = envOr("COREN_MODEL", cfg.Model)
	if v := os.Getenv("COREN_SYSTEM"); strings.TrimSpace(v) != "" {
		cfg.System = v
	}
	cfg.WorkDir = envOr("COREN_WORKDIR", cfg.WorkDir)
	cfg.Addr = envOr("COREN_ADDR", cfg.Addr)
	if v := strings.TrimSpace(os.Getenv("COREN_SESSION_DIR")); v != "" {
		cfg.SessionDir = v
	}
	if v := strings.TrimSpace(os.Getenv("COREN_SKILLS_DIR")); v != "" {
		cfg.SkillsDir = v
	}
	if v := os.Getenv("COREN_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := strings.TrimSpace(os.Getenv("COREN_REASONING")); v != "" {
		cfg.Reasoning = v
	}
	if v := strings.TrimSpace(os.Getenv("COREN_AUTHZ")); v != "" {
		cfg.Authz = v
	}
	if v := strings.TrimSpace(os.Getenv("COREN_CACHE_RETENTION")); v != "" {
		cfg.CacheRetention = v
	}
	// COREN_NO_PERSIST=1 disables session persistence entirely.
	if os.Getenv("COREN_NO_PERSIST") != "" {
		cfg.SessionDir = ""
	}

	if v, err := EnvFloat("COREN_TEMPERATURE"); err == nil && v != nil {
		cfg.Temperature = v
	}
	if v := strings.TrimSpace(os.Getenv("COREN_MAX_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxTokens = &n
		}
	}
	if v := strings.TrimSpace(os.Getenv("COREN_MAX_STEPS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxSteps = n
		}
	}
}

// WriteFile writes cfg to path as JSON, creating parent directories.
func WriteFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// EnvFloat parses an optional float environment variable.
func EnvFloat(key string) (*float64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return &v, nil
}

const defaultSystem = "You are Coren, a concise and capable assistant. " +
	"Use the available tools when they help answer the user's request, and explain your results briefly."
