package delivery

import (
	"context"
	"errors"
	"testing"
)

type fakeReviewer struct {
	decision Decision
	err      error
	seen     Deliverable
}

func (f *fakeReviewer) Review(_ context.Context, d Deliverable) (Decision, error) {
	f.seen = d
	return f.decision, f.err
}

func TestApprovedPassesThrough(t *testing.T) {
	g := New()
	g.Reviewer = &fakeReviewer{decision: Decision{Verdict: Approved}}
	d, err := g.Submit(context.Background(), Deliverable{Title: "plan"})
	if err != nil || !d.Approved() {
		t.Fatalf("decision = %+v, err = %v", d, err)
	}
	if d.By != "user" {
		t.Errorf("by = %q", d.By)
	}
}

func TestNoReviewerRejectsByDefault(t *testing.T) {
	g := New()
	d, _ := g.Submit(context.Background(), Deliverable{Title: "plan"})
	if d.Approved() {
		t.Error("no reviewer should reject by default")
	}
	if d.By != "default" {
		t.Errorf("by = %q", d.By)
	}
}

func TestNoReviewerAutoApprovesWhenConfigured(t *testing.T) {
	g := New()
	g.AutoApprove = true
	d, _ := g.Submit(context.Background(), Deliverable{Title: "plan"})
	if !d.Approved() || d.By != "default" {
		t.Errorf("decision = %+v", d)
	}
}

func TestResolveProvidesReviewer(t *testing.T) {
	rev := &fakeReviewer{decision: Decision{Verdict: Revise, Feedback: "add tests"}}
	g := New()
	g.Resolve = func() (Reviewer, bool) { return rev, true }
	d, _ := g.Submit(context.Background(), Deliverable{Title: "doc"})
	if d.Verdict != Revise || d.Feedback != "add tests" {
		t.Errorf("decision = %+v", d)
	}
}

func TestReviewerErrorRejects(t *testing.T) {
	g := New()
	g.Reviewer = &fakeReviewer{err: errors.New("boom")}
	d, err := g.Submit(context.Background(), Deliverable{})
	if err == nil || d.Approved() {
		t.Error("reviewer error should reject")
	}
}

func TestDeliverableCarriesAttachments(t *testing.T) {
	rev := &fakeReviewer{decision: Decision{Verdict: Approved}}
	g := New()
	g.Reviewer = rev
	_, _ = g.Submit(context.Background(), Deliverable{
		Title:       "review",
		Attachments: []Attachment{{Path: "a.txt", Text: "hi"}},
	})
	if len(rev.seen.Attachments) != 1 {
		t.Errorf("attachments not passed through: %+v", rev.seen)
	}
}
