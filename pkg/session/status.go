package session

// Status is the session state machine's current state.
//
// A session is normally Active. When the agent hands a deliverable to the user
// for approval, it moves to AwaitingApproval and stops; it returns to Active
// once the user approves, requests revision, or rejects.
type Status string

const (
	// Active means the session is ready for more turns.
	Active Status = "active"
	// AwaitingApproval means the agent delivered something and is paused until
	// the user decides.
	AwaitingApproval Status = "awaiting_approval"
)

// Status returns the current session status, derived from the event log.
func (s *session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Active
	for _, e := range s.events {
		if e.Type == EventStatus && e.Status != "" {
			status = Status(e.Status)
		}
	}
	return status
}

// SetStatus appends a status transition to the log.
func (s *session) SetStatus(status Status) {
	s.Append(Event{Type: EventStatus, Status: string(status)})
}
