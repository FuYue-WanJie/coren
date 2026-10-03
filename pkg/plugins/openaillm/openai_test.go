package openaillm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coren/pkg/llm"
)

func collectChunks(t *testing.T, ch <-chan llm.Chunk) []llm.Chunk {
	t.Helper()
	var chunks []llm.Chunk
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		chunks = append(chunks, c)
	}
	return chunks
}

func TestChatAdapterParsesTextAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frames := []string{
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"a.txt\"}"}}]}}]}`,
			`{"choices":[{"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		}
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &ChatAdapter{BaseURL: srv.URL, APIKey: "test-key"}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectChunks(t, ch)

	var text string
	var call *llm.ToolCall
	done := false
	for _, c := range chunks {
		text += c.TextDelta
		if c.ToolCall != nil {
			call = c.ToolCall
		}
		if c.Done {
			done = true
		}
	}
	if text != "Hello" {
		t.Errorf("text = %q", text)
	}
	if call == nil {
		t.Fatal("no tool call parsed")
	}
	if call.Name != "read_file" || call.Arguments != `{"path":"a.txt"}` || call.ID != "call_1" {
		t.Errorf("tool call = %+v", call)
	}
	if !done {
		t.Error("missing done")
	}
}

func TestChatAdapterSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := &ChatAdapter{BaseURL: srv.URL}
	if _, err := p.Stream(context.Background(), llm.Request{Model: "m"}); err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestResponsesAdapterParsesTextAndToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frames := []string{
			`{"type":"response.output_text.delta","delta":"Hi "}`,
			`{"type":"response.output_text.delta","delta":"there"}`,
			`{"type":"response.output_item.added","item_id":"it_1","call_id":"call_9","name":"run_shell"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"it_1","delta":"{\"command\":"}`,
			`{"type":"response.function_call_arguments.delta","item_id":"it_1","delta":"\"ls\"}"}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":7}}}`,
			`[DONE]`,
		}
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &ResponsesAdapter{BaseURL: srv.URL}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectChunks(t, ch)

	var text string
	var call *llm.ToolCall
	var usage *llm.Usage
	for _, c := range chunks {
		text += c.TextDelta
		if c.ToolCall != nil {
			call = c.ToolCall
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if text != "Hi there" {
		t.Errorf("text = %q", text)
	}
	if call == nil || call.Name != "run_shell" || call.Arguments != `{"command":"ls"}` || call.ID != "call_9" {
		t.Errorf("tool call = %+v", call)
	}
	if usage == nil || usage.InputTokens != 5 || usage.OutputTokens != 7 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestChatRequestIncludesPromptCacheKey(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := &ChatAdapter{BaseURL: srv.URL}
	ch, err := p.Stream(context.Background(), llm.Request{
		Model:          "m",
		SessionID:      "session-abc",
		CacheRetention: "long",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if body["prompt_cache_key"] != "session-abc" {
		t.Errorf("prompt_cache_key = %v", body["prompt_cache_key"])
	}
	if body["prompt_cache_retention"] != "24h" {
		t.Errorf("prompt_cache_retention = %v", body["prompt_cache_retention"])
	}
}

func TestChatRequestOmitsCacheWhenDisabled(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	p := &ChatAdapter{BaseURL: srv.URL}
	ch, _ := p.Stream(context.Background(), llm.Request{
		Model: "m", SessionID: "s", CacheRetention: "none",
	})
	for range ch {
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Errorf("cache key should be omitted when disabled: %v", body)
	}
}

func TestChatAdapterParsesReasoningDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frames := []string{
			`{"choices":[{"delta":{"reasoning_content":"let me "}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"think"}}]}`,
			`{"choices":[{"delta":{"content":"answer"}}]}`,
			`[DONE]`,
		}
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &ChatAdapter{BaseURL: srv.URL}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}

	var reasoning, text string
	for _, c := range collectChunks(t, ch) {
		reasoning += c.ReasoningDelta
		text += c.TextDelta
	}
	if reasoning != "let me think" {
		t.Errorf("reasoning = %q", reasoning)
	}
	if text != "answer" {
		t.Errorf("text = %q", text)
	}
}

func TestResponsesAdapterParsesReasoningDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		frames := []string{
			`{"type":"response.reasoning_summary_text.delta","delta":"why "}`,
			`{"type":"response.reasoning_summary_text.delta","delta":"not"}`,
			`{"type":"response.output_text.delta","delta":"done"}`,
			`[DONE]`,
		}
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &ResponsesAdapter{BaseURL: srv.URL}
	ch, err := p.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}

	var reasoning, text string
	for _, c := range collectChunks(t, ch) {
		reasoning += c.ReasoningDelta
		text += c.TextDelta
	}
	if reasoning != "why not" {
		t.Errorf("reasoning = %q", reasoning)
	}
	if text != "done" {
		t.Errorf("text = %q", text)
	}
}
