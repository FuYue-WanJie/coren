package shellweb

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"coren/pkg/session"
)

// newSessionID returns a short random, time-sortable session id.
func newSessionID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "sess-" + time.Now().UTC().Format("20060102150405")
	}
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

// sessionsHandler routes /api/sessions and /api/sessions/<id>.
func (s *Shell) handleSessions(w http.ResponseWriter, r *http.Request) {
	// Path after /api/sessions, e.g. "" or "/<id>".
	rest := strings.TrimPrefix(r.URL.Path, "/api/sessions")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.listSessions(w)
		case http.MethodPost:
			s.createSession(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	id := rest
	switch r.Method {
	case http.MethodGet:
		s.getSession(w, id)
	case http.MethodDelete:
		s.deleteSession(w, id)
	case http.MethodPatch:
		s.renameSession(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// sessionsEnabled reports whether the shell has a session service.
func (s *Shell) sessionsEnabled(w http.ResponseWriter) bool {
	if s.sessions == nil {
		http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// listSessions returns metadata for every known session.
func (s *Shell) listSessions(w http.ResponseWriter) {
	if !s.sessionsEnabled(w) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	summaries := s.sessions.Summaries()
	if summaries == nil {
		summaries = []session.Summary{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": summaries})
}

// createSession mints a new session id and returns it.
func (s *Shell) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.sessionsEnabled(w) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := strings.TrimSpace(body.ID)
	if id == "" {
		id = newSessionID()
	}
	s.sessions.Get(id) // ensure it exists in the store
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// sessionMessage is a renderable history item.
type sessionMessage struct {
	Role       string `json:"role"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// getSession returns the renderable history for one session.
func (s *Shell) getSession(w http.ResponseWriter, id string) {
	if !s.sessionsEnabled(w) {
		return
	}
	sess := s.sessions.Get(id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":       id,
		"title":    sess.Title(),
		"messages": history(sess),
	})
}

// history projects a session log into renderable messages.
func history(sess session.Session) []sessionMessage {
	out := make([]sessionMessage, 0)
	for _, e := range sess.Events() {
		switch e.Type {
		case session.EventUserMsg:
			out = append(out, sessionMessage{Role: "user", Text: e.Text})
		case session.EventAssistant:
			if strings.TrimSpace(e.Text) != "" {
				out = append(out, sessionMessage{Role: "assistant", Text: e.Text})
			}
		case session.EventToolCall:
			out = append(out, sessionMessage{Role: "tool_call", ToolName: e.Name, ToolCallID: e.CallID, Text: e.Arguments})
		case session.EventToolResult:
			out = append(out, sessionMessage{Role: "tool_result", ToolName: e.Name, ToolCallID: e.CallID, Text: e.Text, Error: e.Error})
		}
	}
	return out
}

// deleteSession soft-deletes a session.
func (s *Shell) deleteSession(w http.ResponseWriter, id string) {
	if !s.sessionsEnabled(w) {
		return
	}
	s.sessions.Delete(id)
	w.WriteHeader(http.StatusNoContent)
}

// renameSession sets a session title.
func (s *Shell) renameSession(w http.ResponseWriter, r *http.Request, id string) {
	if !s.sessionsEnabled(w) {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		http.Error(w, "title required", http.StatusBadRequest)
		return
	}
	sess := s.sessions.Get(id)
	sess.SetTitle(body.Title)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "title": sess.Title()})
}
