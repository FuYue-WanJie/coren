package main

import (
	"fmt"
	"os"
	"strings"

	"coren/internal/config"
)

const configUsage = `Usage: coren config <action>

Actions:
  init    Create ./coren.json with current effective settings (refuses to overwrite).
  show    Print the effective configuration with secrets masked.
  path    Print the config file paths in precedence order.
`

func runConfig(args []string) error {
	if len(args) == 0 {
		fmt.Print(configUsage)
		return nil
	}
	switch args[0] {
	case "init":
		return configInit()
	case "show":
		return configShow()
	case "path":
		return configPath()
	case "help", "-h", "--help":
		fmt.Print(configUsage)
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown config action %q\n\n", args[0])
		fmt.Print(configUsage)
		return fmt.Errorf("unknown action %q", args[0])
	}
}

func configInit() error {
	wd, _ := os.Getwd()
	path := config.ProjectConfigPath(wd)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; edit it directly or remove it first", path)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := config.WriteFile(path, cfg); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	fmt.Println("edit api_key / model / base_url there; environment variables still override it.")
	return nil
}

func configShow() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("api          = %s\n", cfg.API)
	fmt.Printf("base_url     = %s\n", cfg.BaseURL)
	fmt.Printf("api_key      = %s\n", maskSecret(cfg.APIKey))
	fmt.Printf("model        = %s\n", cfg.Model)
	fmt.Printf("work_dir     = %s\n", cfg.WorkDir)
	fmt.Printf("addr         = %s\n", cfg.Addr)
	fmt.Printf("reasoning    = %s\n", orDefault(cfg.Reasoning, "auto"))
	fmt.Printf("cache        = %s\n", orDefault(cfg.CacheRetention, "short"))
	fmt.Printf("authz        = %s\n", orDefault(cfg.Authz, "trusted"))
	if cfg.Temperature != nil {
		fmt.Printf("temperature  = %v\n", *cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		fmt.Printf("max_tokens   = %d\n", *cfg.MaxTokens)
	}
	if cfg.MaxSteps > 0 {
		fmt.Printf("max_steps    = %d\n", cfg.MaxSteps)
	}
	if cfg.System != "" {
		fmt.Printf("system       = %s\n", cfg.System)
	}
	return nil
}

func configPath() error {
	wd, _ := os.Getwd()
	fmt.Printf("project : %s\n", config.ProjectConfigPath(wd))
	fmt.Printf("user    : %s\n", config.UserConfigPath())
	fmt.Println("env     : COREN_CONFIG, COREN_API_KEY, COREN_MODEL, ... (highest precedence)")
	return nil
}

// maskSecret reveals only the last four characters of a secret.
func maskSecret(s string) string {
	if s == "" {
		return "(unset)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
