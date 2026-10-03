package todo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func store(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "TODO.md"))
}

func TestAddAndList(t *testing.T) {
	s := store(t)
	if _, err := s.Add("write tests"); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.Add("ship feature")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	if tasks[0].ID != 1 || tasks[0].Text != "write tests" || tasks[0].Done {
		t.Errorf("task[0] = %+v", tasks[0])
	}
}

func TestAddRejectsEmpty(t *testing.T) {
	if _, err := store(t).Add("   "); err == nil {
		t.Fatal("empty task should be rejected")
	}
}

func TestDoneByID(t *testing.T) {
	s := store(t)
	_, _ = s.Add("a")
	_, _ = s.Add("b")

	tasks, task, err := s.Done(2, "")
	if err != nil {
		t.Fatal(err)
	}
	if !task.Done || task.ID != 2 {
		t.Errorf("completed task = %+v", task)
	}
	if !tasks[1].Done || tasks[0].Done {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestDoneByText(t *testing.T) {
	s := store(t)
	_, _ = s.Add("deploy the service")
	_, task, err := s.Done(0, "deploy")
	if err != nil {
		t.Fatal(err)
	}
	if !task.Done || !strings.Contains(task.Text, "deploy") {
		t.Errorf("task = %+v", task)
	}
}

func TestDoneNoMatch(t *testing.T) {
	s := store(t)
	_, _ = s.Add("a")
	if _, _, err := s.Done(99, ""); err == nil {
		t.Fatal("expected no-match error")
	}
}

func TestRemove(t *testing.T) {
	s := store(t)
	_, _ = s.Add("a")
	_, _ = s.Add("b")
	tasks, err := s.Remove(0, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Text != "b" {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestClear(t *testing.T) {
	s := store(t)
	_, _ = s.Add("a")
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	tasks, _ := s.List()
	if len(tasks) != 0 {
		t.Errorf("tasks = %+v after clear", tasks)
	}
}

func TestPersistsAsMarkdownChecklist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "TODO.md")
	s := New(path)
	_, _ = s.Add("first")
	_, _, _ = s.Done(1, "")
	_, _ = s.Add("second")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "- [x] first") {
		t.Errorf("done task not persisted as checked: %q", content)
	}
	if !strings.Contains(content, "- [ ] second") {
		t.Errorf("open task not persisted: %q", content)
	}
}

func TestReadMissingFileIsEmpty(t *testing.T) {
	tasks, err := store(t).List()
	if err != nil || len(tasks) != 0 {
		t.Errorf("missing file should be empty, got %+v, %v", tasks, err)
	}
}

func TestFormatGroupsOpenFirst(t *testing.T) {
	out := Format([]Task{
		{ID: 1, Text: "done one", Done: true},
		{ID: 2, Text: "open two"},
	})
	if !strings.Contains(out, "1 open, 1 done") {
		t.Errorf("summary missing: %q", out)
	}
	openIdx := strings.Index(out, "open two")
	doneIdx := strings.Index(out, "done one")
	if openIdx > doneIdx {
		t.Errorf("open tasks should come first: %q", out)
	}
}
