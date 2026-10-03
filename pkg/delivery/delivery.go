// Package delivery defines deliverables the agent submits for user review.
//
// A deliverable is an artifact the agent produces before acting on it: a plan,
// a document, or a code-change summary, plus optional attached files. Submitting
// one pauses the turn until the user approves, requests revision, or rejects.
// This is the "plan, confirm, then execute" workflow.
package delivery

import "context"

// Key is the service key for the delivery service.
const Key = "delivery"

// ReviewerKey is the service key for a mounted deliverable reviewer.
const ReviewerKey = "delivery.reviewer"

// Kind classifies a deliverable.
type Kind string

const (
	// KindPlan is an action plan awaiting approval before execution.
	KindPlan Kind = "plan"
	// KindDocument is produced documentation or writing.
	KindDocument Kind = "document"
	// KindReview is a change summary or review request.
	KindReview Kind = "review"
)

// Attachment is a file delivered alongside the body.
type Attachment struct {
	// Path is the file path.
	Path string `json:"path"`
	// Size is the file size in bytes, when known.
	Size int64 `json:"size,omitempty"`
	// Text is the file's content when it is small text; empty for large/binary
	// files, which the user can open by path.
	Text string `json:"text,omitempty"`
	// Error records why the file could not be read, when applicable.
	Error string `json:"error,omitempty"`
}

// Deliverable is what the agent submits for approval.
type Deliverable struct {
	// Kind is plan, document, or review.
	Kind Kind `json:"kind"`
	// Title is a short heading.
	Title string `json:"title"`
	// Content is the main body.
	Content string `json:"content"`
	// Attachments are files delivered with the body.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Verdict is the user's decision on a deliverable.
type Verdict string

const (
	// Approved means proceed.
	Approved Verdict = "approve"
	// Revise means change it and resubmit.
	Revise Verdict = "revise"
	// Rejected means stop.
	Rejected Verdict = "reject"
)

// Decision is the outcome of a review.
type Decision struct {
	// Verdict is the user's choice.
	Verdict Verdict
	// Feedback carries revision notes or a rejection reason.
	Feedback string
	// By records who decided: "user", "policy", or "default".
	By string
}

// Approved reports whether the deliverable may proceed.
func (d Decision) Approved() bool { return d.Verdict == Approved }

// Reviewer presents a deliverable and returns the user's decision.
type Reviewer interface {
	// Review shows the deliverable and blocks for a decision.
	Review(ctx context.Context, d Deliverable) (Decision, error)
}

// Service reviews deliverables.
type Service interface {
	// Submit presents a deliverable and returns the user's decision.
	Submit(ctx context.Context, d Deliverable) (Decision, error)
}

// Guard is the default implementation.
type Guard struct {
	// Reviewer, when set, presents the deliverable to the user.
	Reviewer Reviewer
	// Resolve finds a reviewer lazily (a shell may mount one after creation).
	Resolve func() (Reviewer, bool)
	// AutoApprove is the fallback when no reviewer exists (no interactive shell).
	AutoApprove bool
}

// New creates a delivery guard.
func New() *Guard { return &Guard{} }

func (g *Guard) reviewer() Reviewer {
	if g.Resolve != nil {
		if r, ok := g.Resolve(); ok {
			return r
		}
	}
	return g.Reviewer
}

// Submit presents the deliverable, or auto-approves when no reviewer exists.
func (g *Guard) Submit(ctx context.Context, d Deliverable) (Decision, error) {
	reviewer := g.reviewer()
	if reviewer == nil {
		if g.AutoApprove {
			return Decision{Verdict: Approved, By: "default"}, nil
		}
		return Decision{Verdict: Rejected, By: "default", Feedback: "no interactive reviewer available"}, nil
	}
	decision, err := reviewer.Review(ctx, d)
	if err != nil {
		return Decision{Verdict: Rejected, By: "default", Feedback: err.Error()}, err
	}
	if decision.By == "" {
		decision.By = "user"
	}
	return decision, nil
}
