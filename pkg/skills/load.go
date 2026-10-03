package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillFileName is the manifest file that marks a directory as a skill.
const SkillFileName = "SKILL.md"

// LoadDir discovers skills under root.
//
// A skill is any directory containing a SKILL.md. Root-level and nested
// directories are both scanned; the shallowest skill wins when names collide,
// so a project override beats a vendored copy.
func LoadDir(root string) ([]Skill, []error) {
	var (
		loaded []Skill
		errs   []error
	)
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // missing directory is not an error
		}
		return nil, []error{err}
	}
	if !info.IsDir() {
		return nil, []error{fmt.Errorf("skills: %s is not a directory", root)}
	}

	// Collect manifest paths, shallowest first.
	var manifests []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == SkillFileName {
			manifests = append(manifests, path)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	sort.Slice(manifests, func(i, j int) bool {
		return strings.Count(manifests[i], string(filepath.Separator)) <
			strings.Count(manifests[j], string(filepath.Separator))
	})

	seen := map[string]bool{}
	for _, manifest := range manifests {
		skill, err := LoadFile(manifest)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[skill.Name] {
			continue // shallowest wins
		}
		seen[skill.Name] = true
		loaded = append(loaded, skill)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Name < loaded[j].Name })
	return loaded, errs
}

// LoadFile parses a single SKILL.md manifest.
func LoadFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	dir := filepath.Dir(path)
	name := filepath.Base(dir)

	front, body := splitFrontmatter(string(data))
	meta := parseFrontmatter(front)
	if meta.name != "" {
		name = meta.name
	}
	if meta.description == "" {
		return Skill{}, fmt.Errorf("skills: %s is missing a description", path)
	}
	return Skill{
		Name:                   name,
		Description:            meta.description,
		Content:                strings.TrimSpace(body),
		Dir:                    dir,
		DisableModelInvocation: meta.disableModelInvocation,
		Source:                 path,
	}, nil
}

// splitFrontmatter separates a leading YAML frontmatter block from the body.
func splitFrontmatter(content string) (front, body string) {
	trimmed := strings.TrimLeft(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return "", content
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", content
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n")
		}
	}
	return "", content // unterminated: treat the whole file as body
}

// frontmatter is the subset of skill metadata Coren understands.
type frontmatter struct {
	name                   string
	description            string
	disableModelInvocation bool
}

// parseFrontmatter reads simple `key: value` pairs. It intentionally supports a
// small subset so skills stay portable and dependency-free.
func parseFrontmatter(block string) frontmatter {
	var meta frontmatter
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "name":
			meta.name = value
		case "description":
			meta.description = value
		case "disable-model-invocation":
			meta.disableModelInvocation = value == "true"
		}
	}
	return meta
}
