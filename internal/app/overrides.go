package app

import (
	"fmt"
	"time"

	"coren/internal/config"
	"coren/internal/profile"
	hostplugin "coren/pkg/plugins/host"
	"coren/pkg/webauth"
)

// applyPluginOverrides rewrites a profile's plugin list from configuration.
//
// Precedence, low to high:
//  1. the profile's built-in plugin list,
//  2. RemovePlugins (drop ids),
//  3. AddPlugins (append ids not already present),
//  4. Plugins (replace the list entirely).
//
// Every id must resolve to a known built-in plugin or an externally registered
// factory; an unknown id fails boot rather than silently running a partial
// composition.
func applyPluginOverrides(prof profile.Profile, cfg config.Config) (profile.Profile, error) {
	if len(cfg.Plugins) == 0 && len(cfg.AddPlugins) == 0 && len(cfg.RemovePlugins) == 0 {
		return prof, nil
	}

	var plugins []string
	if len(cfg.Plugins) > 0 {
		plugins = append([]string(nil), cfg.Plugins...)
	} else {
		remove := make(map[string]bool, len(cfg.RemovePlugins))
		for _, id := range cfg.RemovePlugins {
			remove[id] = true
		}
		for _, id := range prof.Plugins {
			if !remove[id] {
				plugins = append(plugins, id)
			}
		}
		seen := make(map[string]bool, len(plugins))
		for _, id := range plugins {
			seen[id] = true
		}
		for _, id := range cfg.AddPlugins {
			if !seen[id] {
				plugins = append(plugins, id)
				seen[id] = true
			}
		}
	}

	out := prof
	out.Plugins = plugins
	return out, nil
}

// validatePlugins reports the first plugin id that no factory can build.
func validatePlugins(plugins []string, opts Options) error {
	for _, id := range plugins {
		if builtins.has(id) {
			continue
		}
		if opts.Registry != nil && opts.Registry.Has(id) {
			continue
		}
		return fmt.Errorf("app: unknown plugin %q", id)
	}
	return nil
}

// externalPlugins converts configuration entries to host plugin configs.
func externalPlugins(in []config.ExternalPlugin) []hostplugin.PluginConfig {
	out := make([]hostplugin.PluginConfig, 0, len(in))
	for _, p := range in {
		out = append(out, hostplugin.PluginConfig{
			Name:     p.Name,
			Command:  p.Command,
			Args:     p.Args,
			Env:      p.Env,
			Dir:      p.Dir,
			Disabled: p.Disabled,
		})
	}
	return out
}

// webAuth builds the session store from configuration, or nil when auth is off.
func webAuth(cfg config.Config) *webauth.Store {
	if cfg.Password == "" {
		return nil
	}
	return webauth.New(cfg.Password, time.Duration(cfg.SessionTTLHours)*time.Hour)
}
