package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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

// TrashSuffix marks a soft-deleted log file; the timestamp follows it.
const TrashSuffix = ".trash-"

// List returns the ids of all persisted sessions, excluding trashed ones.
func (p *JSONLPersister) List() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, TrashSuffix) {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".jsonl"))
	}
	return ids, nil
}

// Delete soft-deletes a log by renaming it into the recovery area, so it can be
// restored until the retention window passes.
func (p *JSONLPersister) Delete(sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	src := p.path(sessionID)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}
	dst := fmt.Sprintf("%s%s%d", src, TrashSuffix, time.Now().Unix())
	return os.Rename(src, dst)
}

// PurgeTrash removes trashed logs older than ttl. It returns the number removed.
func (p *JSONLPersister) PurgeTrash(ttl time.Duration) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-ttl)
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		idx := strings.LastIndex(name, TrashSuffix)
		if entry.IsDir() || idx < 0 {
			continue
		}
		stamp, err := strconv.ParseInt(name[idx+len(TrashSuffix):], 10, 64)
		if err != nil {
			continue
		}
		if time.Unix(stamp, 0).After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(p.dir, name)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
