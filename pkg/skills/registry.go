package skills

import "sort"

// Registry is the default in-memory implementation of Service.
type Registry struct {
	byName map[string]Skill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Skill{}}
}

func (r *Registry) Register(skill Skill) {
	if skill.Name == "" {
		return
	}
	r.byName[skill.Name] = skill
}

func (r *Registry) Get(name string) (Skill, bool) {
	s, ok := r.byName[name]
	return s, ok
}

func (r *Registry) List() []Skill {
	out := make([]Skill, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Visible() []Skill {
	out := make([]Skill, 0, len(r.byName))
	for _, s := range r.byName {
		if s.DisableModelInvocation {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
