package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"coren/pkg/webauth"
)

func testServer(auth *webauth.Store) *Server {
	return &Server{auth: auth, static: map[string]http.Handler{}}
}

func TestGuardRejectsMissingToken(t *testing.T) {
	h := testServer(webauth.New("pw", time.Hour)).guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestGuardAcceptsValidToken(t *testing.T) {
	store := webauth.New("pw", time.Hour)
	token, _, _ := store.Login("pw")
	h := testServer(store).guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/chat", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}

func TestGuardDisabledWithoutAuth(t *testing.T) {
	h := testServer(nil).guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 when auth disabled", rec.Code)
	}
}

func TestLoginEndpoint(t *testing.T) {
	s := testServer(webauth.New("pw", time.Hour))

	rec := httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"bad"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password code = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleLogin(rec, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"pw"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("correct password code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("login body = %s", rec.Body.String())
	}
}

func TestAuthCheckReportsRequirement(t *testing.T) {
	s := testServer(webauth.New("pw", time.Hour))
	rec := httptest.NewRecorder()
	s.handleAuthCheck(rec, httptest.NewRequest(http.MethodGet, "/api/authcheck", nil))
	if !strings.Contains(rec.Body.String(), `"required":true`) {
		t.Fatalf("authcheck body = %s", rec.Body.String())
	}
}

func TestHandlerServesHealthAndStatic(t *testing.T) {
	s := testServer(nil)
	s.RegisterStatic("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ui"))
	}))
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("health = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "ui" {
		t.Fatalf("static root = %q", rec.Body.String())
	}
}
