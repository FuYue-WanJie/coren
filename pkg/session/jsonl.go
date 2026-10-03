package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// JSONLPersister stores each session as an append-only JSONL file:
// <dir>/<sessionID>.jsonl. It is safe for concurrent use across sessions.
type JSONLPersister struct {
	dir string
	mu  sync.Mutex
}

// NewJSONLPersister creates a persister rooted at dir, creating it if needed.
func NewJSONLPersister(dir string) (*JSONLPersister, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &JSONLPersister{dir: dir}, nil
}

func (p *JSONLPersister) path(sessionID string) string {
	safe := strings.ReplaceAll(sessionID, string(filepath.Separator), "_")
	return filepath.Join(p.dir, safe+".jsonl")
}

// Load reads all events for a session. A missing file yields no events.
func (p *JSONLPersister) Load(sessionID string) ([]Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	file, err := os.Open(p.path(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// A torn tail (partial write during a crash) is dropped: the log
			// remains valid up to the last complete line.
			break
		}
		events = append(events, e)
	}
	return events, nil
}

// Append durably appends events as JSON lines, one per event.
func (p *JSONLPersister) Append(sessionID string, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	file, err := os.OpenFile(p.path(sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, e := range events {
		data, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("session: marshal event: %w", err)
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	return file.Sync()
}
