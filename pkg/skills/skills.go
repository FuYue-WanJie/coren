// Package skills defines the Agent Skills contract and the skills service.
//
// A skill is a directory containing a SKILL.md file with YAML frontmatter
// (name, description) plus optional resource files. Skills are discovered from
// directories or registered directly by plugins, so both file-based and
// code-contributed skills use one registry. The model sees only name and
// description until it loads a skill, keeping context small (progressive
// disclosure).
package skills

// Key is the service key for the skills service.
const Key = "skills"

// Skill is one discoverable capability package.
type Skill struct {
	// Name identifies the skill and is how the model refers to it.
	Name string
	// Description is shown to the model for discovery.
	Description string
	// Content is the full SKILL.md body, loaded only when the skill is used.
	Content string
	// Dir is the skill directory; references inside Content are relative to it.
	Dir string
	// DisableModelInvocation hides the skill from model-facing discovery while
	// keeping it available to explicit callers.
	DisableModelInvocation bool
	// Source records where the skill came from (a directory path, or "plugin").
	Source string
}

// Service is the pluggable skill registry exposed as ctx.Service(skills.Key).
type Service interface {
	// Register adds or replaces a skill by name.
	Register(skill Skill)
	// Get returns a skill by name.
	Get(name string) (Skill, bool)
	// List returns all skills, sorted by name.
	List() []Skill
	// Visible returns skills the model may discover, sorted by name.
	Visible() []Skill
}
