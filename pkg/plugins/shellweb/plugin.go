// Package shellweb serves the embedded WebUI as a shell.
//
// Since the HTTP API moved to the httpserver plugin, this shell only provides
// static assets. It registers them with an existing httpserver when one is
// mounted, and otherwise starts a minimal static-only server so a UI can still
// run on its own.
package shellweb

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"

	"coren/pkg/coren"
	"coren/pkg/plugins/httpserver"
	"coren/pkg/shell"
)

//go:embed web
var webFS embed.FS

// Plugin mounts the WebUI shell.
type Plugin struct {
	// Addr is the fallback listen address when no httpserver is present.
	Addr string
}

func (Plugin) ID() string       { return "shell.web" }
func (Plugin) Inject() []string { return nil }

func (p Plugin) Apply(ctx coren.Context) error {
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	s := &Shell{addr: p.Addr, static: http.FileServer(http.FS(static))}

	// If an HTTP server is mounted, attach the UI to it and take no socket.
	if srv, ok := coren.UnwrapKey[*httpserver.Server](ctx, httpserver.Key); ok {
		srv.RegisterStatic("/", s.static)
		s.attached = true
	}
	ctx.Provide(shell.Key, s)
	return nil
}

// Shell serves the WebUI static assets.
type Shell struct {
	addr     string
	static   http.Handler
	attached bool
}

func (s *Shell) Name() string { return "web" }

// Run blocks until ctx is cancelled. When attached to an httpserver it idles,
// since that server already owns the socket and serves the assets.
func (s *Shell) Run(ctx context.Context) error {
	if s.attached {
		<-ctx.Done()
		return nil
	}
	// Standalone fallback: serve static assets only (no API).
	mux := http.NewServeMux()
	mux.Handle("/", s.static)
	server := &http.Server{Addr: s.addr, Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Coren WebUI (static only) listening on http://%s", s.addr)
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

// Static returns the static file handler, for tests.
func (s *Shell) Static() http.Handler { return s.static }
