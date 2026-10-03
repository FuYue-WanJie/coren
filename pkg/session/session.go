package session

import (
	"sync"
	"time"

	"coren/pkg/llm"
)

// Key is the service key for the sessions service.
const Key = "sessions"

// Session is one conversation backed by an append-only event log.
type Session interface {
	// ID identifies the session.
	ID() string
	// Events returns a copy of the full event log.
	Events() []Event
	// Append adds events to the log.
	Append(events ...Event)
	// Messages projects the model-visible history from the log.
	Messages() []llm.Message
	// Status returns the current state-machine status.
	Status() Status
	// SetStatus records a state transition.
	SetStatus(status Status)
	// Reset clears the log.
	Reset()
}

// Service creates and looks up sessions.
type Service interface {
	// Get returns an existing session or creates one.
	Get(id string) Session
	// Delete removes a session.
	Delete(id string)
	// IDs lists known session ids.
	IDs() []string
}

// Store is the default in-memory implementation of Service. It may be backed by
// a persistence layer that mirrors the log to disk.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*session
	// persist, when set, receives the session id and each appended event.
	persist Persister
}

// Persister mirrors appended events to durable storage.
type Persister interface {
	// Load returns previously persisted events for a session.
	Load(sessionID string) ([]Event, error)
	// Append durably records events for a session.
	Append(sessionID string, events []Event) error
}

// Option customizes a Store.
type Option func(*Store)

// WithPersister stores the log through p.
func WithPersister(p Persister) Option {
	return func(s *Store) { s.persist = p }
}

// NewStore creates an empty session store.
func NewStore(opts ...Option) *Store {
	s := &Store{sessions: map[string]*session{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Store) Get(id string) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess := &session{id: id, persist: s.persist}
	if s.persist != nil {
		if events, err := s.persist.Load(id); err == nil && len(events) > 0 {
			sess.replay(events)
		}
	}
	s.sessions[id] = sess
	return sess
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *Store) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

type session struct {
	mu      sync.Mutex
	id      string
	seq     int64
	turn    int64
	events  []Event
	persist Persister
}

func (s *session) ID() string { return s.id }

func (s *session) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.events...)
}

// Append assigns sequence and turn numbers, stores events, and mirrors them.
func (s *session) Append(events ...Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range events {
		e := &events[i]
		s.seq++
		e.Seq = s.seq
		if e.Type == EventTurnStart {
			s.turn++
		}
		if e.Turn == 0 {
			e.Turn = s.turn
		}
		if e.Time.IsZero() {
			e.Time = time.Now().UTC()
		}
		s.events = append(s.events, *e)
	}
	if s.persist != nil {
		_ = s.persist.Append(s.id, events)
	}
}

func (s *session) Messages() []llm.Message {
	return DeriveMessages(s.Events())
}

func (s *session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
	s.seq = 0
	s.turn = 0
}

// replay loads persisted events without re-persisting them.
func (s *session) replay(events []Event) {
	for _, e := range events {
		if e.Seq > s.seq {
			s.seq = e.Seq
		}
		if e.Turn > s.turn {
			s.turn = e.Turn
		}
		s.events = append(s.events, e)
	}
}

// CurrentTurn returns the turn number of the latest turn.
func (s *session) CurrentTurn() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn
}
