// Package approval decides whether a guarded action may proceed.
//
// It is a separate service so policy, prompting, and audit stay independent of
// the tools that request approval. With an interactive requester mounted (the
// CLI shell), the user is asked; without one, the decision falls back to the
// authorization level.
package approval

import (
	"context"
	"strings"

	"coren/pkg/authz"
)

// Key is the service key for the approval service.
const Key = "approval"

// RequesterKey is the service key for a mounted human approver.
const RequesterKey = "approval.requester"

// Request is a pending approval decision.
type Request struct {
	// Tool is the tool requesting to run.
	Tool string
	// Action classifies the tool's effect.
	Action authz.Action
	// Summary is a short human-readable description of what will happen.
	Summary string
	// Detail is optional extra context (e.g. a diff, the command).
	Detail string
	// Reason explains why approval is required, when policy flagged it.
	Reason string
}

// Decision is the outcome of an approval.
type Decision struct {
	// Approved reports whether the action may proceed.
	Approved bool
	// By records who decided: "policy", "user", or "default".
	By string
	// Note carries the user's reason or the policy explanation.
	Note string
}

// Requester asks a human to decide. Implemented by interactive shells.
type Requester interface {
	// RequestApproval puts a decision to the user and returns their answer.
	RequestApproval(ctx context.Context, req Request) (Decision, error)
}

// Service decides whether actions proceed.
type Service interface {
	// Level returns the active authorization level.
	Level() authz.Level
	// Approve asks for a decision on a request.
	Approve(ctx context.Context, req Request) (Decision, error)
}

// Guard is the default implementation.
type Guard struct {
	// Authz is the active authorization level.
	Authz authz.Level
	// Requester, when set, is consulted for actions that need confirmation.
	Requester Requester
	// Resolve, when set, finds a requester lazily (a shell may mount one after
	// this guard is created). It takes precedence over Requester.
	Resolve func() (Requester, bool)
}

// New creates a guard at the given level.
func New(level authz.Level) *Guard {
	return &Guard{Authz: level}
}

func (g *Guard) Level() authz.Level { return g.Authz }

// requester returns the active approver, if any.
func (g *Guard) requester() Requester {
	if g.Resolve != nil {
		if r, ok := g.Resolve(); ok {
			return r
		}
	}
	return g.Requester
}

// Approve applies the authorization level, then optional human confirmation.
func (g *Guard) Approve(ctx context.Context, req Request) (Decision, error) {
	// The level is the hard gate: an action below the level is denied outright.
	if !g.Authz.Permits(req.Action) {
		return Decision{
			Approved: false,
			By:       "policy",
			Note:     "authorization level " + string(g.Authz) + " does not permit " + string(req.Action) + " actions",
		}, nil
	}

	// Full level skips confirmation entirely.
	if g.Authz == authz.Full {
		return Decision{Approved: true, By: "policy", Note: "full authorization"}, nil
	}

	// No interactive requester: allow, since the level already permitted it.
	// Risky actions reach here only because policy flagged them, and policy is
	// enforced by the risk plugin's own deny rules.
	requester := g.requester()
	if requester == nil {
		return Decision{Approved: true, By: "default", Note: "no interactive approver"}, nil
	}

	decision, err := requester.RequestApproval(ctx, req)
	if err != nil {
		return Decision{Approved: false, By: "default", Note: err.Error()}, err
	}
	if decision.By == "" {
		decision.By = "user"
	}
	return decision, nil
}

// Summarize builds a compact one-line summary for approval prompts.
func Summarize(req Request) string {
	var b strings.Builder
	b.WriteString(req.Tool)
	if req.Summary != "" {
		b.WriteString(": ")
		b.WriteString(req.Summary)
	}
	if req.Reason != "" {
		b.WriteString(" [")
		b.WriteString(req.Reason)
		b.WriteString("]")
	}
	return b.String()
}
