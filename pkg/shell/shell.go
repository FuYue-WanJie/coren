// Package shell defines the user-facing shell contract.
//
// A shell is whatever presents the agent to a human: the terminal, a browser
// WebUI, or a headless runner. It is mounted as a service so a profile can pick
// one without the rest of the framework knowing which.
package shell

import "context"

// Key is the service key for the mounted shell.
const Key = "shell"

// Shell runs a user-facing interface until it stops.
type Shell interface {
	// Name identifies the shell, e.g. "web" or "cli".
	Name() string
	// Run blocks until the shell exits or ctx is cancelled.
	Run(ctx context.Context) error
}

// Prompter is an optional capability for shells that can run a single prompt
// non-interactively, so the launcher can support `coren run "..."` generically.
type Prompter interface {
	Prompt(ctx context.Context, input string) error
}
