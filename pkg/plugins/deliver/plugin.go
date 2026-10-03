// Package deliver exposes the deliverable-approval workflow to the model.
//
// A single `deliver` tool submits a plan, document, or review (optionally with
// attached files) and blocks until the user decides. On approval the model
// proceeds; on revision it resubmits; on rejection it stops. This implements
// "plan, confirm, then execute".
package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/delivery"
	"coren/pkg/llm"
	"coren/pkg/tools"
)

// Config configures the deliver plugin.
type Config struct {
	// WorkDir scopes attachment paths.
	WorkDir string
	// MaxAttachBytes caps inline attachment text; zero uses a 64 KiB default.
	MaxAttachBytes int
}

// ProviderPlugin provides the delivery service.
type ProviderPlugin struct {
	// AutoApprove lets deliverables pass when no interactive reviewer exists.
	AutoApprove bool
}

func (ProviderPlugin) ID() string       { return "delivery" }
func (ProviderPlugin) Inject() []string { return nil }

func (p ProviderPlugin) Apply(ctx coren.Context) error {
	guard := delivery.New()
	guard.AutoApprove = p.AutoApprove
	guard.Resolve = func() (delivery.Reviewer, bool) {
		return coren.UnwrapKey[delivery.Reviewer](ctx, delivery.ReviewerKey)
	}
	ctx.Provide(delivery.Key, guard)
	return nil
}

// Plugin registers the deliver tool.
type Plugin struct {
	Config Config
}

func (Plugin) ID() string       { return "deliver.tool" }
func (Plugin) Inject() []string { return []string{delivery.Key, tools.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	service, ok := coren.UnwrapKey[delivery.Service](ctx, delivery.Key)
	if !ok {
		return fmt.Errorf("deliver: delivery service missing")
	}
	registry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("deliver: tools service missing")
	}
	registry.Register(deliverTool{service: service, config: p.Config, ctx: ctx})
	return nil
}

type deliverTool struct {
	service delivery.Service
	config  Config
	ctx     coren.Context
}

func (deliverTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name: "deliver",
		Description: "Submit a deliverable (plan, document, or review) for user approval before acting. " +
			"Use it to present a plan and wait for confirmation. On 'revise' you must resubmit an improved " +
			"deliverable; on 'reject' you must stop and not proceed. Never continue past a rejected or pending deliverable.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"plan", "document", "review"},
					"description": "Deliverable kind.",
				},
				"title":   map[string]any{"type": "string", "description": "Short heading."},
				"content": map[string]any{"type": "string", "description": "Main body (Markdown)."},
				"attachments": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional file paths delivered with the body.",
				},
			},
			"required": []string{"kind", "title", "content"},
		},
	}
}

func (t deliverTool) Run(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Kind        string   `json:"kind"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Attachments []string `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Content) == "" {
		return "", fmt.Errorf("content is required")
	}

	d := delivery.Deliverable{
		Kind:        delivery.Kind(normalizeKind(args.Kind)),
		Title:       args.Title,
		Content:     args.Content,
		Attachments: t.readAttachments(args.Attachments),
	}

	decision, err := t.service.Submit(ctx, d)
	if err != nil {
		return "", err
	}

	switch decision.Verdict {
	case delivery.Approved:
		return "approved: the user approved this deliverable; you may proceed.", nil
	case delivery.Revise:
		t.signalPending()
		return "revise: the user requested changes and did not approve. Do not proceed; revise and resubmit.\nfeedback: " +
			strings.TrimSpace(decision.Feedback), nil
	default:
		t.signalPending()
		return "rejected: the user rejected this deliverable. Stop here and do not continue.\nfeedback: " +
			strings.TrimSpace(decision.Feedback), nil
	}
}

// signalPending tells the agent loop to pause the turn: the deliverable was not
// approved, so no further steps should run until the user responds.
func (t deliverTool) signalPending() {
	if t.ctx != nil {
		t.ctx.Emit(coren.DeliveryPending, nil)
	}
}

// readAttachments loads files relative to the work dir.
func (t deliverTool) readAttachments(paths []string) []delivery.Attachment {
	if len(paths) == 0 {
		return nil
	}
	limit := t.config.MaxAttachBytes
	if limit <= 0 {
		limit = 64 << 10
	}
	var out []delivery.Attachment
	for _, rel := range paths {
		path := rel
		if !filepath.IsAbs(path) && t.config.WorkDir != "" {
			path = filepath.Join(t.config.WorkDir, rel)
		}
		info, err := os.Stat(path)
		if err != nil {
			out = append(out, delivery.Attachment{Path: rel, Error: err.Error()})
			continue
		}
		attachment := delivery.Attachment{Path: rel, Size: info.Size()}
		if info.Size() <= int64(limit) {
			if data, err := os.ReadFile(path); err == nil {
				attachment.Text = string(data)
			}
		}
		out = append(out, attachment)
	}
	return out
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "plan", "":
		return string(delivery.KindPlan)
	case "document", "doc":
		return string(delivery.KindDocument)
	case "review":
		return string(delivery.KindReview)
	default:
		return string(delivery.KindPlan)
	}
}
