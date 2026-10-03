package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coren/internal/config"
	"coren/pkg/modelinfo"
	"coren/pkg/plugins/openaillm"
)

const modelsUsage = `Usage: coren models [action] [--filter SUBSTR]...

Actions:
  (none)            List models from the provider (GET /models).
  --info <id>       Show resolved capabilities for a model: context window,
                    tool/reasoning support, modalities, pricing.
  --refresh         Fetch a fresh capability catalog snapshot from models.dev.

--filter restricts the list to ids containing the substring; repeatable.
`

// runModels handles listing models and their capabilities.
func runModels(args []string) error {
	var (
		filters []string
		infoID  string
		refresh bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--filter", "-f":
			if i+1 >= len(args) {
				return fmt.Errorf("--filter needs a value")
			}
			filters = append(filters, args[i+1])
			i++
		case "--info", "-i":
			if i+1 >= len(args) {
				return fmt.Errorf("--info needs a model id")
			}
			infoID = args[i+1]
			i++
		case "--refresh":
			refresh = true
		case "help", "-h", "--help":
			fmt.Print(modelsUsage)
			return nil
		default:
			filters = append(filters, args[i])
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	path := catalogPath(cfg)

	if refresh {
		fmt.Println("fetching capability catalog from", modelinfo.CatalogURL)
		if err := modelinfo.FetchCatalog(context.Background(), modelinfo.CatalogURL, path); err != nil {
			return err
		}
		fmt.Println("saved to", path)
		// Refresh is a complete action on its own unless a filter/info follows.
		if infoID == "" && len(filters) == 0 {
			return nil
		}
	}

	if infoID != "" {
		return showModelInfo(cfg, path, infoID)
	}
	return listModels(cfg, filters)
}

func listModels(cfg config.Config, filters []string) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("no base_url configured")
	}
	models, err := openaillm.ListModels(context.Background(), cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return err
	}
	filtered := openaillm.FilterModels(models, filters)
	if len(filtered) == 0 {
		fmt.Println("no models matched")
		return nil
	}
	for _, m := range filtered {
		if m.OwnedBy != "" {
			fmt.Printf("%-40s %s\n", m.ID, m.OwnedBy)
		} else {
			fmt.Println(m.ID)
		}
	}
	return nil
}

// showModelInfo resolves and prints a model's capabilities.
func showModelInfo(cfg config.Config, path, modelID string) error {
	resolver := &modelinfo.Resolver{Overrides: map[string]modelinfo.Override{}}
	for id, ov := range cfg.Models {
		resolver.Overrides[id] = modelinfo.Override{
			Reasoning:  ov.Reasoning,
			ToolCall:   ov.ToolCall,
			Attachment: ov.Attachment,
			Modalities: ov.Modalities,
			Limit:      ov.Limit,
			Cost:       ov.Cost,
		}
	}
	if catalog, err := modelinfo.LoadCatalog(path); err == nil {
		resolver.Catalog = catalog
	}

	info := resolver.Resolve(context.Background(), cfg.BaseURL, cfg.APIKey, modelID)
	fmt.Printf("model        = %s\n", info.ID)
	if info.Name != "" {
		fmt.Printf("name         = %s\n", info.Name)
	}
	if info.Provider != "" {
		fmt.Printf("provider     = %s\n", info.Provider)
	}
	fmt.Printf("source       = %s\n", orNone(info.Source))
	fmt.Printf("reasoning    = %v\n", info.Reasoning)
	fmt.Printf("tool_call    = %v\n", info.ToolCall)
	fmt.Printf("modalities   = in[%s] out[%s]\n",
		joinOrDash(info.Modalities.Input), joinOrDash(info.Modalities.Output))
	fmt.Printf("context      = %s\n", humanTokens(info.Limit.Context))
	fmt.Printf("max_output   = %s\n", humanTokens(info.Limit.Output))
	if info.Cost != (modelinfo.Cost{}) {
		fmt.Printf("cost(USD/M)  = input %.3f output %.3f cache_read %.3f\n",
			info.Cost.Input, info.Cost.Output, info.Cost.CacheRead)
	}
	return nil
}

// catalogPath mirrors the app's catalog resolution for the CLI.
func catalogPath(cfg config.Config) string {
	if cfg.CatalogPath != "" {
		return cfg.CatalogPath
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return modelinfo.CachePath(filepath.Join(dir, "coren"))
}

func orNone(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

func humanTokens(n int) string {
	if n <= 0 {
		return "(unknown)"
	}
	if n >= 1000 {
		return fmt.Sprintf("%d (~%dk)", n, n/1000)
	}
	return fmt.Sprintf("%d", n)
}
