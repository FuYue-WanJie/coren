// Package agents defines the agents service: a registry of live agent handles
// bound to sessions, plus the agent-loop service contract.
package agents

import (
	"fmt"
	"sort"
	"sync"

	"coren/pkg/agent"
	"coren/pkg/coren"
	"coren/pkg/session"
)

// Key is the service key for the agents service.
const Key = "agents"

// LoopKey is the service key for the agent-loop service.
const LoopKey = "agent-loop"

// Handle is one live agent bound to a session.
type Handle struct {
	// ID uniquely identifies this agent instance.
	ID string
	// SessionID is the conversation this agent drives.
	SessionID string
}

// Service tracks live agents.
type Service interface {
	// Create registers a new agent handle for a session.
	Create(sessionID string) (Handle, error)
	// Get returns a handle by id.
	Get(id string) (Handle, bool)
	// List returns all handles sorted by id.
	List() []Handle
	// Close removes a handle.
	Close(id string) error
}

// Loop drives a turn for one agent: it consumes input, runs steps against an
// llm adapter and the tools service, and emits typed events. The default
// implementation wraps agent.Agent; alternative drivers may replace it.
type Loop interface {
	// Send runs one turn for the given session and streams agent events.
	Send(ctx coren.Context, sess session.Session, input string) <-chan agent.Event
}

// Store is the default in-memory implementation of Service.
type Store struct {
	mu     sync.Mutex
	nextID int
	live   map[string]Handle
}

// NewStore creates an empty agent registry.
func NewStore() *Store {
	return &Store{live: map[string]Handle{}}
}

func (s *Store) Create(sessionID string) (Handle, error) {
	if sessionID == "" {
		return Handle{}, fmt.Errorf("agents: session id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	h := Handle{ID: fmt.Sprintf("agent-%d", s.nextID), SessionID: sessionID}
	s.live[h.ID] = h
	return h, nil
}

func (s *Store) Get(id string) (Handle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.live[id]
	return h, ok
}

func (s *Store) List() []Handle {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Handle, 0, len(s.live))
	for _, h := range s.live {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) Close(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[id]; !ok {
		return fmt.Errorf("agents: unknown agent %q", id)
	}
	delete(s.live, id)
	return nil
}
