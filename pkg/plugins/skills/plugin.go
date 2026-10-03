// Package skills provides the skills service and the tools that let a model
// discover and load skills.
//
// It owns two responsibilities: mounting the skills registry (with directory
// discovery) and exposing that registry to the model through tools. Other
// plugins may also register skills directly on the same service.
package skillsplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/skills"
	"coren/pkg/tools"
)

// ProviderPlugin provides the skills service and loads skills from Dir.
type ProviderPlugin struct {
	// Dir is a skill directory to scan; empty disables file discovery.
	Dir string
	// ExtraDirs lists additional directories scanned after Dir.
	ExtraDirs []string
}

func (ProviderPlugin) ID() string       { return "skills" }
func (ProviderPlugin) Inject() []string { return nil }

func (p ProviderPlugin) Apply(ctx coren.Context) error {
	registry := skills.NewRegistry()

	dirs := append([]string{p.Dir}, p.ExtraDirs...)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		found, errs := skills.LoadDir(dir)
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "warning: skill load: %v\n", err)
		}
		for _, skill := range found {
			registry.Register(skill)
		}
	}
	ctx.Provide(skills.Key, registry)
	return nil
}

// ToolsPlugin registers the skill-discovery and skill-loading tools.
type ToolsPlugin struct{}

func (ToolsPlugin) ID() string       { return "skills.tools" }
func (ToolsPlugin) Inject() []string { return []string{skills.Key, tools.Key} }

func (ToolsPlugin) Apply(ctx coren.Context) error {
	registry, ok := coren.UnwrapKey[skills.Service](ctx, skills.Key)
	if !ok {
		return fmt.Errorf("skills.tools: skills service missing")
	}
	toolRegistry, ok := coren.UnwrapKey[tools.Service](ctx, tools.Key)
	if !ok {
		return fmt.Errorf("skills.tools: tools service missing")
	}
	toolRegistry.Register(listSkillsTool{registry: registry})
	toolRegistry.Register(useSkillTool{registry: registry})
	return nil
}

// listSkillsTool reports the skills available for discovery.
type listSkillsTool struct {
	registry skills.Service
}

func (listSkillsTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "list_skills",
		Description: "List available skills. Each entry has a name and description; use use_skill to load one.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t listSkillsTool) Run(_ context.Context, _ string) (string, error) {
	visible := t.registry.Visible()
	if len(visible) == 0 {
		return "no skills available", nil
	}
	var b strings.Builder
	for _, s := range visible {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// useSkillTool loads a skill's full instructions.
type useSkillTool struct {
	registry skills.Service
}

func (useSkillTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "use_skill",
		Description: "Load a skill's full instructions and any resource it names. Call list_skills first to see what is available.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name from list_skills."},
				"resource": map[string]any{
					"type":        "string",
					"description": "Optional path to a resource file inside the skill directory.",
				},
			},
			"required": []string{"name"},
		},
	}
}

func (t useSkillTool) Run(_ context.Context, arguments string) (string, error) {
	var args struct {
		Name     string `json:"name"`
		Resource string `json:"resource"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	skill, ok := t.registry.Get(strings.TrimSpace(args.Name))
	if !ok {
		return "", fmt.Errorf("unknown skill %q", args.Name)
	}
	if args.Resource != "" {
		data, err := readResource(skill.Dir, args.Resource)
		if err != nil {
			return "", err
		}
		return data, nil
	}
	return formatSkill(skill), nil
}

// formatSkill wraps a skill body with its name and location.
func formatSkill(skill skills.Skill) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<skill name=%q>\n", skill.Name)
	if skill.Dir != "" {
		fmt.Fprintf(&b, "References are relative to %s.\n\n", skill.Dir)
	}
	b.WriteString(skill.Content)
	b.WriteString("\n</skill>")
	return b.String()
}

// readResource reads a file relative to a skill directory, rejecting escapes.
func readResource(dir, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("resource path must be relative")
	}
	joined := filepath.Join(dir, rel)
	cleanDir := filepath.Clean(dir)
	if joined != cleanDir && !strings.HasPrefix(joined, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("resource %q escapes the skill directory", rel)
	}
	data, err := os.ReadFile(joined)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
