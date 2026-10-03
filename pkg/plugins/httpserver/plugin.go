// Package httpserver hosts the Coren HTTP API on its own, decoupled from the
// WebUI. It owns the listening socket, authentication, and the /api/* routes;
// shells or UIs may register additional HTTP handlers (for example static
// assets) through the Server service.
//
// Splitting the API from the UI lets Coren run as a headless backend that any
// frontend can reverse-proxy to, while the bundled WebUI remains just one
// possible client.
package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"coren/pkg/agent"
	"coren/pkg/coren"
	"coren/pkg/session"
	"coren/pkg/webauth"
)

// Key is the service key for the HTTP server.
const Key = coren.ServiceHTTP

// Plugin mounts the HTTP API server.
type Plugin struct {
	// Addr is the listen address, e.g. "127.0.0.1:8787".
	Addr string
	// AgentConfig sets the loop the /api/chat endpoint drives.
	AgentConfig agent.Agent
	// Auth gates API access; nil disables authentication.
	Auth *webauth.Store
	// NoServe, when true, only provides the Server service without listening.
	// Used by compositions where another component owns the process lifetime.
	NoServe bool
}

func (Plugin) ID() string       { return "http-server" }
func (Plugin) Inject() []string { return []string{session.Key} }

func (p Plugin) Apply(ctx coren.Context) error {
	sessions, ok := coren.UnwrapKey[session.Service](ctx, session.Key)
	if !ok {
		return fmt.Errorf("httpserver: sessions service missing")
	}
	ag := p.AgentConfig
	ag.Context = ctx
	srv := &Server{
		agent:    &ag,
		sessions: sessions,
		addr:     p.Addr,
		auth:     p.Auth,
		static:   map[string]http.Handler{},
	}
	ctx.Provide(Key, srv)
	return nil
}

// Start launches the HTTP server in the background and returns once the socket
// is listening (or fails). The process lifetime is owned by the shell or the
// launcher, not by this plugin.
func (p Plugin) Start(ctx coren.Context) error {
	if p.NoServe {
		return nil
	}
	srv, ok := coren.UnwrapKey[*Server](ctx, Key)
	if !ok {
		return fmt.Errorf("httpserver: server service missing")
	}
	return srv.Start(ctx.GoContext())
}

// Stop shuts the server down.
func (p Plugin) Stop(ctx coren.Context) error {
	if p.NoServe {
		return nil
	}
	if srv, ok := coren.UnwrapKey[*Server](ctx, Key); ok {
		return srv.Shutdown()
	}
	return nil
}

// Server owns the API mux, the auth guard, and any registered extra handlers.
type Server struct {
	agent    *agent.Agent
	sessions session.Service
	addr     string
	auth     *webauth.Store
	httpSrv  *http.Server
	serveErr chan error

	static map[string]http.Handler
}

// Start begins listening in a goroutine and returns after the listener is up.
func (s *Server) Start(ctx context.Context) error {
	s.httpSrv = &http.Server{Addr: s.addr, Handler: s.Handler()}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.serveErr = make(chan error, 1)
	log.Printf("Coren listening on http://%s", s.addr)
	if s.auth != nil && s.auth.Required() {
		log.Printf("web authentication is enabled; log in with your password")
	}
	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.serveErr <- err
		}
	}()
	// Stop the server when the kernel context is cancelled.
	go func() {
		<-ctx.Done()
		_ = s.httpSrv.Shutdown(context.Background())
	}()
	return nil
}

// Shutdown stops the server.
func (s *Server) Shutdown() error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(context.Background())
}

// RegisterStatic mounts a handler at a path prefix. Used by UI shells to attach
// their assets to the shared server.
func (s *Server) RegisterStatic(prefix string, h http.Handler) {
	if s.static == nil {
		s.static = map[string]http.Handler{}
	}
	s.static[prefix] = h
}

// Handler builds the full mux. Exposed for tests and embedding.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.Handle("/api/authcheck", s.guard(http.HandlerFunc(s.handleAuthCheck)))
	mux.Handle("/api/chat", s.guard(http.HandlerFunc(s.handleChat)))
	// Static prefixes registered by UI shells.
	for prefix, h := range s.static {
		mux.Handle(prefix, h)
	}
	return mux
}

// Addr returns the configured listen address.
func (s *Server) Addr() string { return s.addr }

// guard requires a valid session token on API requests when auth is enabled.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth != nil && s.auth.Required() && !s.auth.Validate(bearerToken(r)) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// bearerToken extracts the token from the Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// loginRequest carries the password to exchange for a session token.
type loginRequest struct {
	Password string `json:"password"`
}

// handleLogin validates the password and returns a session token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.auth == nil {
		http.Error(w, "authentication disabled", http.StatusNotFound)
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	token, expires, ok := s.auth.Login(req.Password)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid password"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":      token,
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}

// handleLogout revokes the caller's token.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.auth.Logout(bearerToken(r))
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthCheck reports whether auth is required; guard already validated.
func (s *Server) handleAuthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	required := s.auth != nil && s.auth.Required()
	_ = json.NewEncoder(w).Encode(map[string]bool{"required": required, "authenticated": true})
}
