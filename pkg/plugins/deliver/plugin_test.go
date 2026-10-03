package deliver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/delivery"
	"coren/pkg/tools"
)

type fakeReviewer struct {
	decision delivery.Decision
}

func (f fakeReviewer) Review(context.Context, delivery.Deliverable) (delivery.Decision, error) {
	return f.decision, nil
}

func mount(t *testing.T, reviewer delivery.Reviewer, workDir string) (*coren.Kernel, tools.Service) {
	t.Helper()
	k := coren.NewKernel(context.Background())
	plugins := []coren.Plugin{
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		ProviderPlugin{},
		Plugin{Config: Config{WorkDir: workDir}},
	}
	if reviewer != nil {
		plugins = append(plugins, coren.PluginFunc{Name: "reviewer", Mount: func(ctx coren.Context) error {
			ctx.Provide(delivery.ReviewerKey, reviewer)
			return nil
		}})
	}
	if err := k.Boot(plugins...); err != nil {
		t.Fatal(err)
	}
	svc, _ := coren.UnwrapKey[tools.Service](k.Context(), tools.Key)
	return k, svc
}

func TestToolRegistered(t *testing.T) {
	k, svc := mount(t, fakeReviewer{decision: delivery.Decision{Verdict: delivery.Approved}}, t.TempDir())
	defer k.Shutdown()
	if _, ok := svc.Get("deliver"); !ok {
		t.Fatal("deliver tool missing")
	}
}

func TestApprovedReportsProceed(t *testing.T) {
	k, svc := mount(t, fakeReviewer{decision: delivery.Decision{Verdict: delivery.Approved}}, t.TempDir())
	defer k.Shutdown()
	out, err := svc.RunText(context.Background(), "deliver", `{"kind":"plan","title":"P","content":"do x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "approved") {
		t.Errorf("output = %q", out)
	}
}

func TestRejectedReportsStop(t *testing.T) {
	k, svc := mount(t, fakeReviewer{decision: delivery.Decision{Verdict: delivery.Rejected, Feedback: "no"}}, t.TempDir())
	defer k.Shutdown()
	out, err := svc.RunText(context.Background(), "deliver", `{"kind":"plan","title":"P","content":"do x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rejected") || !strings.Contains(out, "do not continue") {
		t.Errorf("output = %q", out)
	}
}

func TestReviseReportsFeedback(t *testing.T) {
	k, svc := mount(t, fakeReviewer{decision: delivery.Decision{Verdict: delivery.Revise, Feedback: "add tests"}}, t.TempDir())
	defer k.Shutdown()
	out, err := svc.RunText(context.Background(), "deliver", `{"kind":"document","title":"D","content":"body"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "revise") || !strings.Contains(out, "add tests") {
		t.Errorf("output = %q", out)
	}
}

func TestAttachmentsRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("file body"), 0o644); err != nil {
		t.Fatal(err)
	}

	var captured delivery.Deliverable
	rev := captureReviewer{onReview: func(d delivery.Deliverable) { captured = d }}
	k, svc := mount(t, rev, dir)
	defer k.Shutdown()

	_, err := svc.RunText(context.Background(), "deliver",
		`{"kind":"review","title":"R","content":"see file","attachments":["a.txt"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.Attachments) != 1 || captured.Attachments[0].Text != "file body" {
		t.Errorf("attachments = %+v", captured.Attachments)
	}
}

func TestMissingAttachmentRecordsError(t *testing.T) {
	var captured delivery.Deliverable
	rev := captureReviewer{onReview: func(d delivery.Deliverable) { captured = d }}
	k, svc := mount(t, rev, t.TempDir())
	defer k.Shutdown()

	_, _ = svc.RunText(context.Background(), "deliver",
		`{"kind":"review","title":"R","content":"x","attachments":["nope.txt"]}`)
	if len(captured.Attachments) != 1 || captured.Attachments[0].Error == "" {
		t.Errorf("missing attachment should record an error: %+v", captured.Attachments)
	}
}

func TestEmptyContentRejected(t *testing.T) {
	k, svc := mount(t, fakeReviewer{decision: delivery.Decision{Verdict: delivery.Approved}}, t.TempDir())
	defer k.Shutdown()
	if _, err := svc.RunText(context.Background(), "deliver", `{"kind":"plan","title":"P","content":"  "}`); err == nil {
		t.Fatal("empty content should error")
	}
}

type captureReviewer struct {
	onReview func(delivery.Deliverable)
}

func (c captureReviewer) Review(_ context.Context, d delivery.Deliverable) (delivery.Decision, error) {
	c.onReview(d)
	return delivery.Decision{Verdict: delivery.Approved}, nil
}
