// Package webauth implements password login with in-memory session tokens.
//
// A client posts a password to /api/login and receives an opaque token that
// authenticates subsequent requests until it expires. Tokens live only in
// memory, so restarting the server invalidates every session. Passwords are
// compared in constant time to avoid timing side channels.
package webauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"
)

// DefaultTTL is the session lifetime when none is configured.
const DefaultTTL = 7 * 24 * time.Hour

// Store issues and validates session tokens for a single password.
type Store struct {
	password string
	ttl      time.Duration

	mu     sync.Mutex
	tokens map[string]time.Time // token -> expiry
	now    func() time.Time
}

// New creates a store. An empty password means no authentication is required;
// Validate then accepts any token. A zero ttl uses DefaultTTL.
func New(password string, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{
		password: password,
		ttl:      ttl,
		tokens:   map[string]time.Time{},
		now:      time.Now,
	}
}

// Required reports whether a password was configured.
func (s *Store) Required() bool { return s.password != "" }

// Login checks the password and, on success, returns a fresh token and its
// expiry. An empty password always succeeds (auth disabled).
func (s *Store) Login(password string) (token string, expires time.Time, ok bool) {
	if s.password != "" && !constantTimeEquals(password, s.password) {
		return "", time.Time{}, false
	}
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, false
	}
	expires = s.now().Add(s.ttl)
	s.mu.Lock()
	s.tokens[token] = expires
	s.mu.Unlock()
	return token, expires, true
}

// Validate reports whether a token is currently valid. When no password is
// configured, every token (including the empty one) is accepted.
func (s *Store) Validate(token string) bool {
	if s.password == "" {
		return true
	}
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.tokens[token]
	if !ok {
		return false
	}
	if s.now().After(expires) {
		delete(s.tokens, token)
		return false
	}
	return true
}

// Logout revokes a token.
func (s *Store) Logout(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

// purgeExpired removes timed-out tokens; callers may invoke it periodically.
func (s *Store) purgeExpired() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, expires := range s.tokens {
		if now.After(expires) {
			delete(s.tokens, token)
		}
	}
}

// newToken returns a 256-bit random hex token.
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// constantTimeEquals compares two strings without leaking length-independent
// timing. Different lengths short-circuit, which is unavoidable and harmless
// for high-entropy secrets.
func constantTimeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
