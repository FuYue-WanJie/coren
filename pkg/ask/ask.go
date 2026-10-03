// Package ask defines the human-in-the-loop clarification contract.
//
// A model sometimes needs input it cannot infer: a preference, a missing fact,
// or a decision between options. The ask seam lets a tool put a question to the
// user and receive an answer. Shells implement Asker when an interactive user is
// present; shells without one simply do not, and the tool reports that clearly.
package ask

import "context"

// Key is the service key for the mounted asker.
const Key = "ask"

// Asker puts a question to the user and returns the answer.
type Asker interface {
	// Ask blocks until the user answers, the context is cancelled, or the
	// implementation's own timeout elapses.
	Ask(ctx context.Context, question string) (string, error)
}
