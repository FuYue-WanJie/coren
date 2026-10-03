// Package memsession provides the sessions service as a plugin.
//
// With Dir set, the append-only log is mirrored to JSONL files and resumed on
// next start; without it the store is process-local memory.
package memsession

import (
	"fmt"
	"time"

	"coren/pkg/coren"
	"coren/pkg/session"
)

// TrashTTL is how long a soft-deleted session stays recoverable.
const TrashTTL = 24 * time.Hour

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
	// Drop trashed logs past the retention window on startup.
	_, _ = persister.PurgeTrash(TrashTTL)
	ctx.Provide(session.Key, session.NewStore(session.WithPersister(persister)))
	return nil
}
