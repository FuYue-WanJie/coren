// Package shellweb serves the agent over HTTP with the WebUI.
//
// The WebUI assets are embedded in the binary and, when a WebDir is configured,
// extracted to disk on first run. The extracted copy is preferred, so local
// edits take effect without rebuilding; an update prompt lets the user replace
// it when the binary ships a newer built-in UI.
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
	"coren/pkg/webui"
)

//go:embed web
var webFS embed.FS

// Plugin mounts the web shell on the shell service.
type Plugin struct {
	// AgentConfig sets the loop this shell runs (model, system prompt, limits).
	AgentConfig agent.Agent
	// Addr is the listen address, e.g. "127.0.0.1:8787".
	Addr string
	// WebDir is where the WebUI is extracted; empty keeps it embedded only.
	WebDir string
	// AppVersion is recorded with the extracted assets.
	AppVersion string
	// UpdatePrompt asks the shell to check for a stale WebUI at startup.
	UpdatePrompt bool
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

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("shellweb: embedded assets missing: %w", err)
	}

	var manager *webui.Manager
	if p.WebDir != "" {
		manager, err = webui.New(static, p.WebDir, p.AppVersion)
		if err != nil {
			return fmt.Errorf("shellweb: webui manager: %w", err)
		}
		if _, err := manager.EnsureDir(); err != nil {
			log.Printf("shellweb: could not extract WebUI to %s: %v", p.WebDir, err)
		}
	}

	served := static
	if manager != nil {
		served = manager.FS()
	}

	ctx.Provide(shell.Key, &Shell{
		agent:    &ag,
		sessions: sessions,
		addr:     p.Addr,
		static:   http.FS(served),
		manager:  manager,
	})
	return nil
}

// Shell is the HTTP/WebUI shell.
type Shell struct {
	agent    *agent.Agent
	sessions session.Service
	addr     string
	static   http.FileSystem
	manager  *webui.Manager
}

func (s *Shell) Name() string { return "web" }

// WebUIStatus reports the extracted copy's staleness (webui.StatusProvider).
func (s *Shell) WebUIStatus() (webui.Status, bool) {
	if s.manager == nil {
		return webui.Status{}, false
	}
	return s.manager.Status(), true
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Shell) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/webui/status", s.handleWebUIStatus)
	mux.HandleFunc("/api/webui/update", s.handleWebUIUpdate)
	mux.Handle("/", noCache(http.FileServer(s.static)))

	server := &http.Server{Addr: s.addr, Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Coren listening on http://%s", s.addr)
		if s.manager != nil {
			if st := s.manager.Status(); st.UpdateAvailable {
				log.Printf("WebUI update available: local %s (built on %s), bundled %s; open the WebUI or POST /api/webui/update to refresh",
					shortHash(st.DiskHash), st.DiskVersion, shortHash(st.BuiltinHash))
			}
		}
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

// shortHash abbreviates a hash for display.
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// noCache disables caching so edited UI files take effect on refresh.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		next.ServeHTTP(w, r)
	})
}

func (s *Shell) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleWebUIStatus reports whether the extracted UI is stale.
func (s *Shell) handleWebUIStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.manager == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": false})
		return
	}
	st := s.manager.Status()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":          true,
		"extracted":        st.Extracted,
		"update_available": st.UpdateAvailable,
		"disk_version":     st.DiskVersion,
		"disk_hash":        st.DiskHash,
		"builtin_version":  st.BuiltinVersion,
		"builtin_hash":     st.BuiltinHash,
	})
}

// handleWebUIUpdate replaces the extracted UI with the built-in version.
func (s *Shell) handleWebUIUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.manager == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "webui extraction disabled"})
		return
	}
	backup, err := s.manager.Update()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	// Serve the refreshed copy from now on.
	s.static = http.FS(s.manager.FS())
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": true, "backup": backup})
}

// chatRequest and the chat handler live in chat.go.
type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}
