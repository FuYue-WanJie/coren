// Package prompt assembles the system prompt from composable parts.
//
// The order is deliberate and stable across turns so provider prompt caches can
// hit: identity, guidelines, project context, skills, then caller appendices.
// Only the tail changes between turns, keeping the cached prefix large.
package prompt

import (
	"strings"

	"coren/pkg/ctxfiles"
)

// Options controls assembly.
type Options struct {
	// Base is the product identity paragraph. Ignored when Custom is set.
	Base string
	// Custom replaces the entire base prompt when non-empty.
	Custom string
	// Guidelines are bullet lines appended after the identity.
	Guidelines []string
	// Append is free text appended at the end of the prompt.
	Append []string
	// ContextFiles are project instruction files.
	ContextFiles []ctxfiles.File
	// SkillsSection, when non-empty, is appended verbatim.
	SkillsSection string
}

// Build renders the system prompt.
func Build(opts Options) string {
	var out strings.Builder

	if opts.Custom != "" {
		out.WriteString(strings.TrimRight(opts.Custom, "\n"))
	} else if opts.Base != "" {
		out.WriteString(strings.TrimRight(opts.Base, "\n"))
	}

	writeBullets(&out, "## Guidelines", opts.Guidelines)

	if section := ctxfiles.Format(opts.ContextFiles); section != "" {
		out.WriteString("\n\n")
		out.WriteString(section)
	}

	if strings.TrimSpace(opts.SkillsSection) != "" {
		out.WriteString("\n\n")
		out.WriteString(strings.TrimSpace(opts.SkillsSection))
	}

	for _, a := range opts.Append {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		out.WriteString("\n\n")
		out.WriteString(a)
	}

	return strings.TrimSpace(out.String())
}

// writeBullets renders a titled bullet list, skipping blank entries.
func writeBullets(out *strings.Builder, title string, items []string) {
	kept := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		return
	}
	out.WriteString("\n\n")
	out.WriteString(title)
	out.WriteString("\n")
	for _, item := range kept {
		out.WriteString("- ")
		out.WriteString(item)
		out.WriteString("\n")
	}
}
