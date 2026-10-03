package session

import (
	"sort"
	"strings"
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
	// Title returns the session title: the latest session/meta title when set,
	// otherwise a title derived from the first user message.
	Title() string
	// SetTitle records a title as a session/meta event.
	SetTitle(title string)
}

// Service creates and looks up sessions.
type Service interface {
	// Get returns an existing session or creates one.
	Get(id string) Session
	// Delete removes a session. With a persister it soft-deletes, moving the
	// log to a recovery area instead of destroying it.
	Delete(id string)
	// IDs lists known session ids, including persisted ones.
	IDs() []string
	// Summaries returns metadata for every known session, newest first.
	Summaries() []Summary
}

// Summary is lightweight session metadata for listings.
type Summary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Turns        int64     `json:"turns"`
	MessageCount int       `json:"message_count"`
	UpdatedAt    time.Time `json:"updated_at"`
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
	// List returns the ids of all persisted sessions.
	List() ([]string, error)
	// Delete soft-deletes a session's durable log, allowing later recovery.
	Delete(sessionID string) error
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

// Delete removes a session from memory and soft-deletes its durable log.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	if s.persist != nil {
		_ = s.persist.Delete(id)
	}
}

// IDs lists every known session id: those loaded in memory plus those persisted
// on disk, so a restart still sees prior conversations.
func (s *Store) IDs() []string {
	s.mu.Lock()
	seen := make(map[string]bool, len(s.sessions))
	for id := range s.sessions {
		seen[id] = true
	}
	s.mu.Unlock()

	if s.persist != nil {
		if persisted, err := s.persist.List(); err == nil {
			for _, id := range persisted {
				seen[id] = true
			}
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

// Summaries returns metadata for every known session, newest first. Persisted
// sessions are loaded lazily so their titles and counts are accurate.
func (s *Store) Summaries() []Summary {
	summaries := make([]Summary, 0)
	for _, id := range s.IDs() {
		sess := s.Get(id)
		summaries = append(summaries, summarize(sess))
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries
}

// summarize derives listing metadata from a session's log.
func summarize(sess Session) Summary {
	events := sess.Events()
	out := Summary{ID: sess.ID(), Title: sess.Title()}
	for _, e := range events {
		out.Turns = e.Turn
		if !e.Time.IsZero() {
			out.UpdatedAt = e.Time
		}
		switch e.Type {
		case EventUserMsg, EventAssistant:
			out.MessageCount++
		}
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = time.Now().UTC()
	}
	if out.Title == "" {
		out.Title = titleFromEvents(events)
	}
	return out
}

// titleFromEvents derives a title from the first user message.
func titleFromEvents(events []Event) string {
	for _, e := range events {
		if e.Type == EventUserMsg && strings.TrimSpace(e.Text) != "" {
			return truncateTitle(strings.TrimSpace(e.Text))
		}
	}
	return "新会话"
}

// titleLimit bounds a derived title's length.
const titleLimit = 30

// truncateTitle shortens a title on a rune boundary.
func truncateTitle(s string) string {
	runes := []rune(s)
	if len(runes) <= titleLimit {
		return s
	}
	return string(runes[:titleLimit]) + "…"
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

// Title returns the latest session/meta title, or one derived from the first
// user message when no title was set.
func (s *session) Title() string {
	events := s.Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == EventMeta && strings.TrimSpace(events[i].Title) != "" {
			return events[i].Title
		}
	}
	return titleFromEvents(events)
}

// SetTitle records a title as a session/meta event.
func (s *session) SetTitle(title string) {
	s.Append(Event{Type: EventMeta, Title: strings.TrimSpace(title)})
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
