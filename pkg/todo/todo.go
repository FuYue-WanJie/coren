// Package todo maintains a project task list as a Markdown checklist file.
//
// The list lives in TODO.md as GitHub-style checkboxes, so it is readable and
// editable by humans as well as the model. Tasks carry a stable id derived from
// their position so the model can mark items done without quoting text.
package todo

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// DefaultFileName is the task list file at the project root.
const DefaultFileName = "TODO.md"

// Task is one checklist item.
type Task struct {
	// ID is a stable index assigned when the list is read (1-based).
	ID int `json:"id"`
	// Text is the task description.
	Text string `json:"text"`
	// Done reports whether the checkbox is checked.
	Done bool `json:"done"`
}

var taskLine = regexp.MustCompile(`^\s*-\s*\[([ xX])\]\s*(.*)$`)

// Store reads and writes a TODO.md file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a store for path.
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the task file path.
func (s *Store) Path() string { return s.path }

// List returns the current tasks in file order.
func (s *Store) List() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Add appends a task and returns the new list.
func (s *Store) Add(text string) ([]Task, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("todo: task text is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.read()
	if err != nil {
		return nil, err
	}
	tasks = append(tasks, Task{Text: text})
	if err := s.write(tasks); err != nil {
		return nil, err
	}
	return s.read()
}

// Done marks a task complete by id. When id is 0, it matches the first open task
// whose text contains the given substring.
func (s *Store) Done(id int, text string) ([]Task, Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.read()
	if err != nil {
		return nil, Task{}, err
	}
	idx := findTask(tasks, id, text)
	if idx < 0 {
		return nil, Task{}, fmt.Errorf("todo: no matching task")
	}
	tasks[idx].Done = true
	if err := s.write(tasks); err != nil {
		return nil, Task{}, err
	}
	updated, err := s.read()
	if idx < len(updated) {
		return updated, updated[idx], nil
	}
	return updated, Task{}, nil
}

// Remove deletes a task by id or text match.
func (s *Store) Remove(id int, text string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.read()
	if err != nil {
		return nil, err
	}
	idx := findTask(tasks, id, text)
	if idx < 0 {
		return nil, fmt.Errorf("todo: no matching task")
	}
	tasks = append(tasks[:idx], tasks[idx+1:]...)
	if err := s.write(tasks); err != nil {
		return nil, err
	}
	return s.read()
}

// Clear removes every task.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(nil)
}

// findTask returns the index of a task by exact id, else by substring text.
func findTask(tasks []Task, id int, text string) int {
	if id > 0 {
		for i, t := range tasks {
			if t.ID == id {
				return i
			}
		}
		return -1
	}
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return -1
	}
	for i, t := range tasks {
		if strings.Contains(strings.ToLower(t.Text), text) {
			return i
		}
	}
	return -1
}

// read parses the file, assigning sequential ids. A missing file is empty.
func (s *Store) read() ([]Task, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, line := range strings.Split(string(data), "\n") {
		m := taskLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tasks = append(tasks, Task{
			ID:   len(tasks) + 1,
			Text: strings.TrimSpace(m[2]),
			Done: strings.EqualFold(m[1], "x"),
		})
	}
	return tasks, nil
}

// write renders the list back to Markdown.
func (s *Store) write(tasks []Task) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# TODO\n\n")
	for _, t := range tasks {
		box := " "
		if t.Done {
			box = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", box, t.Text)
	}
	return os.WriteFile(s.path, []byte(b.String()), 0o644)
}

// Format renders the list for the model: open tasks first, then done.
func Format(tasks []Task) string {
	if len(tasks) == 0 {
		return "no tasks"
	}
	var open, done []Task
	for _, t := range tasks {
		if t.Done {
			done = append(done, t)
		} else {
			open = append(open, t)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d open, %d done\n", len(open), len(done))
	for _, t := range open {
		fmt.Fprintf(&b, "- [%d] %s\n", t.ID, t.Text)
	}
	for _, t := range done {
		fmt.Fprintf(&b, "- [%d] %s (done)\n", t.ID, t.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// IDOf is a helper for tests and callers formatting ids.
func IDOf(id int) string { return strconv.Itoa(id) }
