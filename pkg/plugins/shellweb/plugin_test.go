package shellweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"coren/pkg/coren"
	"coren/pkg/plugins/httpserver"
	"coren/pkg/session"
	"coren/pkg/shell"
)

// These tests assert the shell attaches its static assets to a mounted HTTP
// server. Authentication coverage lives in the httpserver package.

func TestShellWebAttachesToHTTPServer(t *testing.T) {
	kernel := coren.NewKernel(context.Background())
	if err := kernel.Boot(
		sessionProvider{},
		httpserver.Plugin{Addr: "127.0.0.1:0", NoServe: true},
		Plugin{},
	); err != nil {
		t.Fatal(err)
	}
	defer kernel.Shutdown()
	if err := kernel.Start(); err != nil {
		t.Fatal(err)
	}

	srv, ok := coren.UnwrapKey[*httpserver.Server](kernel.Context(), httpserver.Key)
	if !ok {
		t.Fatal("httpserver service missing")
	}
	if _, ok := coren.UnwrapKey[shell.Shell](kernel.Context(), shell.Key); !ok {
		t.Fatal("shell service missing")
	}

	// The static UI should be served at "/".
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
}

// sessionProvider mounts a minimal session service.
type sessionProvider struct{}

func (sessionProvider) ID() string       { return "sessions.test" }
func (sessionProvider) Inject() []string { return nil }
func (sessionProvider) Apply(ctx coren.Context) error {
	ctx.Provide(session.Key, session.NewStore())
	return nil
}
