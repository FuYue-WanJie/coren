// Package memsession provides the sessions service as a plugin.
//
// With Dir set, the append-only log is mirrored to JSONL files and resumed on
// next start; without it the store is process-local memory.
package memsession

import (
	"fmt"

	"coren/pkg/coren"
	"coren/pkg/session"
)

// Plugin provides the sessions service.
type Plugin struct {
	// Dir enables JSONL persistence under this directory when non-empty.
	Dir string
}

func (Plugin) ID() string       { return "sessions" }
func (Plugin) Inject() []string { return nil }

func (p Plugin) Apply(ctx coren.Context) error {
	if p.Dir == "" {
		ctx.Provide(session.Key, session.NewStore())
		return nil
	}
	persister, err := session.NewJSONLPersister(p.Dir)
	if err != nil {
		return fmt.Errorf("memsession: %w", err)
	}
	ctx.Provide(session.Key, session.NewStore(session.WithPersister(persister)))
	return nil
}
