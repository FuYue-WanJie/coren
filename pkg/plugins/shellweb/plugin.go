// Package shellweb serves the agent over HTTP with an embedded WebUI.
package shellweb

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"

	"coren/pkg/agent"
	"coren/pkg/agents"
	"coren/pkg/coren"
	"coren/pkg/session"
	"coren/pkg/shell"
)

//go:embed web
var webFS embed.FS

// Plugin mounts the web shell on the shell service.
type Plugin struct {
	// AgentConfig sets the loop this shell runs (model, system prompt, limits).
	AgentConfig agent.Agent
	// Addr is the listen address, e.g. "127.0.0.1:8787".
	Addr string
}

func (Plugin) ID() string       { return "shell.web" }
func (Plugin) Inject() []string { return []string{session.Key, agents.Key, agents.LoopKey} }

func (p Plugin) Apply(ctx coren.Context) error {
	sessions, ok := coren.UnwrapKey[session.Service](ctx, session.Key)
	if !ok {
		return fmt.Errorf("shellweb: sessions service missing")
	}
	ag := p.AgentConfig
	ag.Context = ctx
	ctx.Provide(shell.Key, &Shell{agent: &ag, sessions: sessions, addr: p.Addr})
	return nil
}

// Shell is the HTTP/WebUI shell.
type Shell struct {
	agent    *agent.Agent
	sessions session.Service
	addr     string
}

func (s *Shell) Name() string { return "web" }

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Shell) Run(ctx context.Context) error {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.Handle("/", http.FileServer(http.FS(static)))

	server := &http.Server{Addr: s.addr, Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Coren listening on http://%s", s.addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return server.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (s *Shell) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func (s *Shell) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	enc := json.NewEncoder(w)
	send := func(v any) {
		_, _ = fmt.Fprint(w, "data: ")
		_ = enc.Encode(v)
		_, _ = fmt.Fprint(w, "\n")
		flusher.Flush()
	}

	sess := s.sessions.Get(req.SessionID)
	for ev := range s.agent.Send(r.Context(), sess, req.Message) {
		switch {
		case ev.Err != nil:
			send(map[string]any{"type": "error", "error": ev.Err.Error()})
		case ev.Rejected != nil:
			send(map[string]any{"type": "rejected", "reason": ev.Rejected.Reason})
		case ev.TextDelta != "":
			send(map[string]any{"type": "text", "delta": ev.TextDelta})
		case ev.ReasoningDelta != "":
			send(map[string]any{"type": "reasoning", "delta": ev.ReasoningDelta})
		case ev.ToolCallStart != nil:
			send(map[string]any{
				"type":      "tool_call",
				"name":      ev.ToolCallStart.Name,
				"arguments": ev.ToolCallStart.Arguments,
			})
		case ev.ToolCallResult != nil:
			payload := map[string]any{
				"type":   "tool_result",
				"name":   ev.ToolCallResult.Name,
				"output": ev.ToolCallResult.Output,
			}
			if ev.ToolCallResult.Err != nil {
				payload["error"] = ev.ToolCallResult.Err.Error()
			}
			send(payload)
		case ev.Done:
			send(map[string]any{"type": "done"})
		}
	}
}
