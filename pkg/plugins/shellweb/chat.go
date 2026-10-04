package shellweb

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleChat streams one agent turn as server-sent events.
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
				"id":        ev.ToolCallStart.ID,
				"name":      ev.ToolCallStart.Name,
				"arguments": ev.ToolCallStart.Arguments,
			})
		case ev.ToolCallResult != nil:
			payload := map[string]any{
				"type":   "tool_result",
				"id":     ev.ToolCallResult.ID,
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
