package shellweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"coren/pkg/webauth"
)

// guardFor wraps a handler the same way Run does, for isolated tests.
func guardFor(s *Shell, next http.HandlerFunc) http.Handler {
	return s.guard(next)
}

func TestGuardRejectsMissingToken(t *testing.T) {
	shell := &Shell{auth: webauth.New("pw", time.Hour)}
	h := guardFor(shell, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestGuardAcceptsValidToken(t *testing.T) {
	store := webauth.New("pw", time.Hour)
	token, _, _ := store.Login("pw")
	shell := &Shell{auth: store}
	h := guardFor(shell, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/chat", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}

func TestGuardDisabledWithoutAuth(t *testing.T) {
	shell := &Shell{auth: nil}
	h := guardFor(shell, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/chat", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 when auth disabled", rec.Code)
	}
}

func TestLoginEndpoint(t *testing.T) {
	shell := &Shell{auth: webauth.New("pw", time.Hour)}

	// Wrong password -> 401.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"bad"}`))
	shell.handleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password code = %d, want 401", rec.Code)
	}

	// Correct password -> token.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"pw"}`))
	shell.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct password code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("login body = %s", rec.Body.String())
	}
}

func TestAuthCheckReportsRequirement(t *testing.T) {
	shell := &Shell{auth: webauth.New("pw", time.Hour)}
	rec := httptest.NewRecorder()
	shell.handleAuthCheck(rec, httptest.NewRequest(http.MethodGet, "/api/authcheck", nil))
	if !strings.Contains(rec.Body.String(), `"required":true`) {
		t.Fatalf("authcheck body = %s", rec.Body.String())
	}
}
