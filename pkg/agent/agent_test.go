package agent

import (
	"context"
	"errors"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/llm"
	"coren/pkg/modelinfo"
	"coren/pkg/session"
	"coren/pkg/tools"
)

// scriptedAdapter replays canned turns, one per Stream call.
type scriptedAdapter struct {
	turns []turn
	calls int
}

type turn struct {
	chunks []llm.Chunk
	err    error
}

func (p *scriptedAdapter) Name() string { return "scripted" }

func (p *scriptedAdapter) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	if p.calls >= len(p.turns) {
		return nil, errors.New("no more scripted turns")
	}
	t := p.turns[p.calls]
	p.calls++
	if t.err != nil {
		return nil, t.err
	}
	ch := make(chan llm.Chunk, len(t.chunks))
	for _, c := range t.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// echoTool returns its arguments, used to verify the tool loop.
type echoTool struct{}

func (echoTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "echo", Description: "echo", Parameters: map[string]any{}}
}

func (echoTool) Run(_ context.Context, arguments string) (string, error) {
	return "echoed: " + arguments, nil
}

// newTestKernel boots a kernel with the llm registry, tools registry, a scripted
// adapter, and the echo tool. Optional plugins are applied last.
func newTestKernel(t *testing.T, adapter llm.Adapter, extra ...coren.Plugin) *coren.Kernel {
	t.Helper()
	k := coren.NewKernel(context.Background())
	plugins := []coren.Plugin{
		coren.PluginFunc{Name: "llm", Mount: func(ctx coren.Context) error {
			ctx.Provide(llm.Key, llm.NewRegistry())
			return nil
		}},
		coren.PluginFunc{Name: "tools", Mount: func(ctx coren.Context) error {
			ctx.Provide(tools.Key, tools.NewRegistry())
			return nil
		}},
		coren.PluginFunc{Name: "adapter", Needs: []string{llm.Key}, Mount: func(ctx coren.Context) error {
			svc, _ := coren.UnwrapKey[llm.Service](ctx, llm.Key)
			svc.Register(adapter)
			return nil
		}},
		coren.PluginFunc{Name: "echo-tool", Needs: []string{tools.Key}, Mount: func(ctx coren.Context) error {
			svc, _ := coren.UnwrapKey[tools.Service](ctx, tools.Key)
			svc.Register(echoTool{})
			return nil
		}},
	}
	plugins = append(plugins, extra...)
	if err := k.Boot(plugins...); err != nil {
		t.Fatal(err)
	}
	return k
}

func collect(events <-chan Event) []Event {
	var out []Event
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestAgentReturnsTextWhenNoToolCalls(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{{chunks: []llm.Chunk{
		{TextDelta: "hello "}, {TextDelta: "world"},
		{Done: true, Usage: &llm.Usage{InputTokens: 3, OutputTokens: 2}},
	}}}}
	k := newTestKernel(t, adapter)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")
	events := collect(ag.Send(context.Background(), sess, "hi"))

	var text string
	done := false
	for _, ev := range events {
		if ev.Err != nil {
			t.Fatalf("unexpected error: %v", ev.Err)
		}
		text += ev.TextDelta
		if ev.Done {
			done = true
			if ev.Usage == nil || ev.Usage.OutputTokens != 2 {
				t.Errorf("usage = %+v", ev.Usage)
			}
		}
	}
	if text != "hello world" {
		t.Errorf("text = %q", text)
	}
	if !done {
		t.Error("expected Done event")
	}
	if len(sess.Messages()) != 2 {
		t.Errorf("session messages = %d", len(sess.Messages()))
	}
}

func TestAgentExecutesToolThenFinishes(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{
		{chunks: []llm.Chunk{
			{ToolCall: &llm.ToolCall{ID: "c1", Name: "echo", Arguments: `{"x":1}`}},
			{Done: true},
		}},
		{chunks: []llm.Chunk{{TextDelta: "final answer"}, {Done: true}}},
	}}
	k := newTestKernel(t, adapter)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")

	var toolRan, finished bool
	for ev := range ag.Send(context.Background(), sess, "use the tool") {
		if ev.Err != nil {
			t.Fatalf("error: %v", ev.Err)
		}
		if ev.ToolCallResult != nil && ev.ToolCallResult.Output == `echoed: {"x":1}` {
			toolRan = true
		}
		if ev.Done {
			finished = true
		}
	}
	if !toolRan {
		t.Error("tool did not run with expected output")
	}
	if !finished {
		t.Error("agent did not finish after tool call")
	}
	if got := len(sess.Messages()); got != 4 {
		t.Errorf("session messages = %d, want 4", got)
	}
}

func TestPreExecuteWaterfallRewritesArguments(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{
		{chunks: []llm.Chunk{
			{ToolCall: &llm.ToolCall{ID: "c1", Name: "echo", Arguments: `{"orig":true}`}},
			{Done: true},
		}},
		{chunks: []llm.Chunk{{TextDelta: "ok"}, {Done: true}}},
	}}
	rewrite := coren.PluginFunc{Name: "policy", Needs: []string{tools.Key}, Mount: func(ctx coren.Context) error {
		ctx.OnWaterfall(coren.EventToolsPreExecute, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
			if p, ok := payload.(*PreExecutePayload); ok {
				p.Arguments = `{"rewritten":true}`
			}
			return next(payload)
		}, false)
		return nil
	}}
	k := newTestKernel(t, adapter, rewrite)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")

	var output string
	for ev := range ag.Send(context.Background(), sess, "go") {
		if ev.ToolCallResult != nil {
			output = ev.ToolCallResult.Output
		}
	}
	if output != `echoed: {"rewritten":true}` {
		t.Errorf("output = %q, want rewritten arguments", output)
	}
}

func TestRequestWaterfallRewritesModel(t *testing.T) {
	var seenModel string
	adapter := &scriptedAdapter{turns: []turn{{chunks: []llm.Chunk{{TextDelta: "ok"}, {Done: true}}}}}
	rewrite := coren.PluginFunc{Name: "model-policy", Mount: func(ctx coren.Context) error {
		ctx.OnWaterfall(coren.EventAgentRequest, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
			if p, ok := payload.(*RequestPayload); ok {
				p.Request.Model = "rewritten-model"
			}
			return next(payload)
		}, false)
		return nil
	}}
	k := newTestKernel(t, adapter, rewrite)
	defer k.Shutdown()

	// Wrap adapter to capture the model.
	recording := &recordingAdapter{inner: adapter, onRequest: func(r llm.Request) { seenModel = r.Model }}
	svc, _ := coren.UnwrapKey[llm.Service](k.Context(), llm.Key)
	svc.Register(recording)

	ag := &Agent{Context: k.Context(), Model: "original", AdapterName: "recording"}
	sess := session.NewStore().Get("t")
	for range ag.Send(context.Background(), sess, "hi") {
	}
	if seenModel != "rewritten-model" {
		t.Errorf("model = %q, want rewritten-model", seenModel)
	}
}

type recordingAdapter struct {
	inner     llm.Adapter
	onRequest func(llm.Request)
}

func (a *recordingAdapter) Name() string { return "recording" }
func (a *recordingAdapter) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if a.onRequest != nil {
		a.onRequest(req)
	}
	return a.inner.Stream(ctx, req)
}

func TestAgentStopsAtMaxSteps(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{
		{chunks: []llm.Chunk{{ToolCall: &llm.ToolCall{ID: "c1", Name: "echo", Arguments: "{}"}}, {Done: true}}},
		{chunks: []llm.Chunk{{ToolCall: &llm.ToolCall{ID: "c2", Name: "echo", Arguments: "{}"}}, {Done: true}}},
		{chunks: []llm.Chunk{{ToolCall: &llm.ToolCall{ID: "c3", Name: "echo", Arguments: "{}"}}, {Done: true}}},
	}}
	k := newTestKernel(t, adapter)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test", MaxSteps: 4}
	sess := session.NewStore().Get("t")
	var gotErr error
	for ev := range ag.Send(context.Background(), sess, "loop") {
		if ev.Err != nil {
			gotErr = ev.Err
		}
	}
	if gotErr == nil {
		t.Error("expected max-steps error")
	}
}

func TestProviderErrorSurfaces(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{{err: errors.New("boom")}}}
	k := newTestKernel(t, adapter)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")
	var gotErr error
	for ev := range ag.Send(context.Background(), sess, "hi") {
		if ev.Err != nil {
			gotErr = ev.Err
		}
	}
	if gotErr == nil || gotErr.Error() != "boom" {
		t.Errorf("error = %v", gotErr)
	}
}

func TestPreStepWaterfallCanReject(t *testing.T) {
	adapter := &scriptedAdapter{turns: nil}
	reject := coren.PluginFunc{Name: "guard", Mount: func(ctx coren.Context) error {
		ctx.OnWaterfall(coren.EventAgentPreStep, func(_ context.Context, payload any, _ func(any) (any, error)) (any, error) {
			p := payload.(*PreStepResult)
			p.Accepted = false
			p.Reason = "blocked by policy"
			return p, nil // no next: short-circuit
		}, false)
		return nil
	}}
	k := newTestKernel(t, adapter, reject)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")

	var rejected *Rejection
	done := false
	for ev := range ag.Send(context.Background(), sess, "bad input") {
		if ev.Rejected != nil {
			rejected = ev.Rejected
		}
		if ev.Done {
			done = true
		}
	}
	if rejected == nil || rejected.Reason != "blocked by policy" {
		t.Errorf("rejection = %+v", rejected)
	}
	if !done {
		t.Error("expected Done after rejection")
	}
	if len(sess.Messages()) != 0 {
		t.Errorf("rejected input must not enter history, got %d messages", len(sess.Messages()))
	}
	if adapter.calls != 0 {
		t.Errorf("model must not be called on rejection, calls = %d", adapter.calls)
	}
}

func TestPreStepWaterfallCanRewriteInput(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{{chunks: []llm.Chunk{{TextDelta: "ok"}, {Done: true}}}}}
	rewrite := coren.PluginFunc{Name: "normalizer", Mount: func(ctx coren.Context) error {
		ctx.OnWaterfall(coren.EventAgentPreStep, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
			p := payload.(*PreStepResult)
			p.Input = "rewritten: " + p.Input
			return next(p)
		}, false)
		return nil
	}}
	k := newTestKernel(t, adapter, rewrite)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test"}
	sess := session.NewStore().Get("t")
	for range ag.Send(context.Background(), sess, "hello") {
	}

	msgs := sess.Messages()
	if len(msgs) == 0 || msgs[0].Text != "rewritten: hello" {
		t.Errorf("user message = %+v", msgs)
	}
}

func TestTurnStoppingHookFiresOnMaxSteps(t *testing.T) {
	adapter := &scriptedAdapter{turns: []turn{
		{chunks: []llm.Chunk{{ToolCall: &llm.ToolCall{ID: "c1", Name: "echo", Arguments: "{}"}}, {Done: true}}},
		{chunks: []llm.Chunk{{ToolCall: &llm.ToolCall{ID: "c2", Name: "echo", Arguments: "{}"}}, {Done: true}}},
	}}
	var fired bool
	var reason TurnStoppingReason
	hook := coren.PluginFunc{Name: "watchdog", Mount: func(ctx coren.Context) error {
		ctx.OnWaterfall(coren.EventAgentTurnStopping, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
			fired = true
			if p, ok := payload.(*TurnStoppingPayload); ok {
				reason = p.Reason
			}
			return next(payload)
		}, false)
		return nil
	}}
	k := newTestKernel(t, adapter, hook)
	defer k.Shutdown()

	ag := &Agent{Context: k.Context(), Model: "test", MaxSteps: 2}
	sess := session.NewStore().Get("t")
	for range ag.Send(context.Background(), sess, "loop") {
	}
	if !fired {
		t.Fatal("turn-stopping hook did not fire")
	}
	if reason != TurnStoppingMaxSteps {
		t.Errorf("reason = %q", reason)
	}
}

func TestEffectiveReasoningAutoUsesMediumWhenSupported(t *testing.T) {
	ag := &Agent{ModelInfo: modelinfo.Info{ID: "m", Reasoning: true}}
	if got := ag.effectiveReasoning(); got != llm.ReasoningMedium {
		t.Errorf("auto+reasoning support = %q, want medium", got)
	}
}

func TestEffectiveReasoningAutoStaysAutoWhenUnsupported(t *testing.T) {
	ag := &Agent{ModelInfo: modelinfo.Info{ID: "m", Reasoning: false}}
	if got := ag.effectiveReasoning(); got != llm.ReasoningAuto {
		t.Errorf("auto without support = %q, want auto", got)
	}
}

func TestEffectiveReasoningExplicitWins(t *testing.T) {
	ag := &Agent{
		ModelInfo:      modelinfo.Info{ID: "m", Reasoning: true},
		ReasoningLevel: llm.ReasoningOff,
	}
	if got := ag.effectiveReasoning(); got != llm.ReasoningOff {
		t.Errorf("explicit off = %q, want off", got)
	}
}

func TestEffectiveReasoningUnknownModelStaysAuto(t *testing.T) {
	ag := &Agent{ModelInfo: modelinfo.Info{}}
	if got := ag.effectiveReasoning(); got != llm.ReasoningAuto {
		t.Errorf("unknown model = %q, want auto", got)
	}
}
