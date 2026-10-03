// Package guard enforces authorization, risk rules, and approval on tool calls.
//
// It subscribes to the tools/pre-execute waterfall, so it guards every tool
// uniformly without the tools or the agent loop knowing about policy. Denied
// calls are rejected; risky calls are put to the approval service.
package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"coren/pkg/agent"
	"coren/pkg/approval"
	"coren/pkg/authz"
	"coren/pkg/coren"
	"coren/pkg/risk"
)

// Config configures the guard.
type Config struct {
	// Authz is the authorization level.
	Authz authz.Level
	// Rules are extra risk rules; built-ins are included unless DisableDefaults.
	Rules []risk.Rule
	// DisableDefaults omits the built-in risk rules.
	DisableDefaults bool
}

// ProviderPlugin provides the approval service.
type ProviderPlugin struct {
	Authz authz.Level
}

func (ProviderPlugin) ID() string       { return "approval" }
func (ProviderPlugin) Inject() []string { return nil }

func (p ProviderPlugin) Apply(ctx coren.Context) error {
	guard := approval.New(p.Authz)
	// Resolve the human approver lazily: a shell mounts one after this plugin.
	guard.Resolve = func() (approval.Requester, bool) {
		return coren.UnwrapKey[approval.Requester](ctx, approval.RequesterKey)
	}
	ctx.Provide(approval.Key, guard)
	return nil
}

// Plugin enforces policy on the pre-execute waterfall.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "guard" }
func (Plugin) Inject() []string { return []string{approval.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	service, ok := coren.UnwrapKey[approval.Service](ctx, approval.Key)
	if !ok {
		return fmt.Errorf("guard: approval service missing")
	}

	rules := pRules(p.Config)
	engine, err := risk.NewEngine(rules)
	if err != nil {
		return fmt.Errorf("guard: %w", err)
	}

	ctx.OnWaterfall(coren.EventToolsPreExecute, func(c context.Context, payload any, next func(any) (any, error)) (any, error) {
		call, ok := payload.(*agent.PreExecutePayload)
		if !ok {
			return next(payload)
		}

		action := authz.ActionForTool(call.Name)
		findings := engine.Evaluate(call.Name, call.Arguments)

		// Deny rules are hard safety blocks: they apply at every level, so no
		// authorization setting can be used to run them.
		if findings.Denied() {
			return nil, fmt.Errorf("tool %q denied: %s", call.Name, firstNonEmpty(findings.Reason, "blocked by risk rule"))
		}

		// File writes get a diff review at Trusted: the user sees what changes
		// before approving. At Full this is skipped (handled by Approve).
		if call.Name == "write_file" && service.Level() != authz.Full {
			if summary, changed := fileDiff(call.Arguments); changed {
				decision, err := service.Approve(c, approval.Request{
					Tool:    call.Name,
					Action:  action,
					Summary: summary,
					Detail:  call.Arguments,
					Reason:  "file contents change",
				})
				if err != nil {
					return nil, fmt.Errorf("guard: approval failed: %w", err)
				}
				if !decision.Approved {
					return nil, fmt.Errorf("tool %q denied: %s", call.Name, firstNonEmpty(decision.Note, "edit not approved"))
				}
				return next(payload)
			}
		}

		reason := findings.Reason
		if reason == "" && !service.Level().Permits(action) {
			reason = "authorization level " + string(service.Level()) + " blocks " + string(action) + " actions"
		}

		// Fast path: permitted and not flagged.
		if !findings.NeedsConfirmation() && service.Level().Permits(action) {
			return next(payload)
		}

		decision, err := service.Approve(c, approval.Request{
			Tool:    call.Name,
			Action:  action,
			Summary: summarizeCall(call),
			Detail:  call.Arguments,
			Reason:  reason,
		})
		if err != nil {
			return nil, fmt.Errorf("guard: approval failed: %w", err)
		}
		if !decision.Approved {
			return nil, fmt.Errorf("tool %q denied: %s", call.Name, firstNonEmpty(decision.Note, reason, "not approved"))
		}
		return next(payload)
	}, false)
	return nil
}

func summarizeCall(call *agent.PreExecutePayload) string {
	if call.Name == "run_shell" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err == nil && args.Command != "" {
			return args.Command
		}
	}
	return truncate(call.Arguments, 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func pRules(cfg Config) []risk.Rule {
	var rules []risk.Rule
	if !cfg.DisableDefaults {
		rules = append(rules, risk.DefaultRules()...)
	}
	rules = append(rules, cfg.Rules...)
	return rules
}

// fileDiff summarizes how a write_file call changes a file: created, unchanged,
// or a compact line-count diff. changed is false when the content is identical,
// so no review is needed.
func fileDiff(arguments string) (summary string, changed bool) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", false
	}
	existing, err := os.ReadFile(args.Path)
	if err != nil {
		return fmt.Sprintf("create %s (%d bytes)", args.Path, len(args.Content)), true
	}
	oldText := string(existing)
	if oldText == args.Content {
		return "", false
	}
	add, del := countLineChanges(oldText, args.Content)
	return fmt.Sprintf("modify %s (+%d/-%d lines)", args.Path, add, del), true
}

// countLineChanges reports added and removed line counts via a simple LCS.
func countLineChanges(oldText, newText string) (added, removed int) {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	// Trim a trailing empty line from each split for stable counts.
	if n := len(oldLines); n > 0 && oldLines[n-1] == "" {
		oldLines = oldLines[:n-1]
	}
	if n := len(newLines); n > 0 && newLines[n-1] == "" {
		newLines = newLines[:n-1]
	}

	lcs := make([][]int, len(oldLines)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	common := lcs[0][0]
	added = len(newLines) - common
	removed = len(oldLines) - common
	return added, removed
}
