package shellweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"coren/pkg/session"
)

func sessionShell() *Shell {
	return &Shell{sessions: session.NewStore()}
}

func TestCreateSessionReturnsID(t *testing.T) {
	s := sessionShell()
	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader("{}")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["id"] == "" {
		t.Fatalf("empty id: %s", rec.Body.String())
	}
}

func TestListSessions(t *testing.T) {
	s := sessionShell()
	s.sessions.Get("a").Append(session.Event{Type: session.EventUserMsg, Text: "会话A"})
	s.sessions.Get("b").Append(session.Event{Type: session.EventUserMsg, Text: "会话B"})

	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "会话A") || !strings.Contains(body, "会话B") {
		t.Fatalf("body = %s", body)
	}
}

func TestGetSessionHistory(t *testing.T) {
	s := sessionShell()
	sess := s.sessions.Get("h")
	sess.Append(session.Event{Type: session.EventUserMsg, Text: "你好"})
	sess.Append(session.Event{Type: session.EventAssistant, Text: "你好，有什么可以帮你？"})

	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/h", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `"role":"user"`) || !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("history = %s", body)
	}
}

func TestDeleteSession(t *testing.T) {
	s := sessionShell()
	s.sessions.Get("d").Append(session.Event{Type: session.EventUserMsg, Text: "x"})

	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest(http.MethodDelete, "/api/sessions/d", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d", rec.Code)
	}
	if len(s.sessions.IDs()) != 0 {
		t.Fatalf("session not deleted: %v", s.sessions.IDs())
	}
}

func TestRenameSession(t *testing.T) {
	s := sessionShell()
	s.sessions.Get("r").Append(session.Event{Type: session.EventUserMsg, Text: "原标题"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/r", strings.NewReader(`{"title":"新标题"}`))
	s.handleSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := s.sessions.Get("r").Title(); got != "新标题" {
		t.Fatalf("title = %q", got)
	}
}

func TestSessionsUnavailableWithoutService(t *testing.T) {
	s := &Shell{}
	rec := httptest.NewRecorder()
	s.handleSessions(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}
