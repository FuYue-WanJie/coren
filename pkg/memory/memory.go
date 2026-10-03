// Package memory implements a simple durable project memory.
//
// Memory is a markdown file the model can read and append to, giving it a place
// to record facts it should remember across sessions (conventions, decisions,
// user preferences) without re-deriving them each turn — a token saving over
// rediscovery and repeated explanation.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultFileName is the memory file name at the project root.
const DefaultFileName = "MEMORY.md"

// Store reads and appends to a memory file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a store for path. The file is created lazily on first write.
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the memory file path.
func (s *Store) Path() string { return s.path }

// Read returns the current memory contents. A missing file yields "".
func (s *Store) Read() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Write replaces the memory contents.
func (s *Store) Write(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, []byte(content), 0o644)
}

// Append adds a section to the memory file under a dated heading.
func (s *Store) Append(entry string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("memory: nothing to append")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Add a separating newline if the file is non-empty and lacks a trailing one.
	if info, err := file.Stat(); err == nil && info.Size() > 0 {
		if _, err := file.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = file.WriteString(entry + "\n")
	return err
}

// Section formats a memory entry with a heading for readability.
func Section(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return body
	}
	return "## " + title + "\n\n" + body
}
