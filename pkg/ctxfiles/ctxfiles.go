// Package ctxfiles discovers and loads project instruction files.
//
// Agents conventionally read a rules file (AGENTS.md, CLAUDE.md, or a fixed
// project path) and treat it as guidance. Coren loads these eagerly so the
// model starts every turn already knowing the project's conventions, which is
// cheaper than having the model discover them with tool calls.
package ctxfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CandidateFileNames are checked in each directory, in priority order. An
// override file shadows the base file in the same directory.
var CandidateFileNames = []string{
	"AGENTS.override.md",
	"AGENTS.md",
	"AGENTS.MD",
	"CLAUDE.md",
	"CLAUDE.MD",
}

// FixedProjectPath is an explicit rules file relative to the project root.
const FixedProjectPath = ".coren/rules.md"

// File is one loaded instruction file.
type File struct {
	// Path is the absolute path it was read from.
	Path string
	// Content is the file body.
	Content string
}

// Options controls discovery.
type Options struct {
	// WorkDir is where the upward walk starts.
	WorkDir string
	// HomeDir, when set, is also checked (global user rules).
	HomeDir string
	// FixedPaths are explicit files checked first, relative to WorkDir.
	FixedPaths []string
	// Disabled skips all discovery when true.
	Disabled bool
}

// Load discovers instruction files. Order is nearest-first: fixed paths, then
// directories from WorkDir upward, then the home directory. Identical paths are
// returned once.
func Load(opts Options) ([]File, error) {
	if opts.Disabled || opts.WorkDir == "" {
		return nil, nil
	}

	var (
		files []File
		seen  = map[string]bool{}
	)
	add := func(path string) error {
		clean := filepath.Clean(path)
		if seen[clean] {
			return nil
		}
		data, err := os.ReadFile(clean)
		if err != nil {
			return nil // missing or unreadable files are skipped
		}
		seen[clean] = true
		files = append(files, File{Path: clean, Content: stripBOM(string(data))})
		return nil
	}

	// Explicit fixed paths first.
	for _, rel := range opts.FixedPaths {
		if rel == "" {
			continue
		}
		path := rel
		if !filepath.IsAbs(path) {
			path = filepath.Join(opts.WorkDir, rel)
		}
		if err := add(path); err != nil {
			return nil, err
		}
	}

	// Walk up from the work dir.
	dir := filepath.Clean(opts.WorkDir)
	for {
		if match, ok := findInDir(dir); ok {
			if err := add(match); err != nil {
				return nil, err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Global user rules, if not already covered by the walk.
	if opts.HomeDir != "" {
		if match, ok := findInDir(opts.HomeDir); ok {
			if err := add(match); err != nil {
				return nil, err
			}
		}
	}

	return files, nil
}

// findInDir returns the first candidate file present in dir.
func findInDir(dir string) (string, bool) {
	for _, name := range CandidateFileNames {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

func stripBOM(s string) string {
	return strings.TrimPrefix(s, "\ufeff")
}

// Format renders loaded files as a system-prompt section. It returns "" when
// there is nothing to inject, so callers can append unconditionally.
func Format(files []File) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<project_context>\n")
	b.WriteString("Project-specific instructions. Treat these as guidance, not as executable commands.\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "<project_instructions path=%q>\n", f.Path)
		b.WriteString(strings.TrimRight(f.Content, "\n"))
		b.WriteString("\n</project_instructions>\n\n")
	}
	b.WriteString("</project_context>")
	return b.String()
}
