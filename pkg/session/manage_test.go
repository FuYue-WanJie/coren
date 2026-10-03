package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTitleDerivesFromFirstUserMessage(t *testing.T) {
	s := NewStore().Get("s")
	s.Append(Event{Type: EventUserMsg, Text: "帮我写一个 Go 的 WebSocket 服务"})
	if got := s.Title(); got != "帮我写一个 Go 的 WebSocket 服务" {
		t.Fatalf("title = %q", got)
	}
}

func TestTitleTruncatesLongMessages(t *testing.T) {
	s := NewStore().Get("s")
	long := ""
	for i := 0; i < 50; i++ {
		long += "字"
	}
	s.Append(Event{Type: EventUserMsg, Text: long})
	got := []rune(s.Title())
	if len(got) > titleLimit+1 { // titleLimit runes + ellipsis
		t.Fatalf("title not truncated: %d runes", len(got))
	}
}

func TestSetTitleOverridesDerived(t *testing.T) {
	s := NewStore().Get("s")
	s.Append(Event{Type: EventUserMsg, Text: "原始消息"})
	s.SetTitle("我的会话")
	if got := s.Title(); got != "我的会话" {
		t.Fatalf("title = %q", got)
	}
	// A later title wins.
	s.SetTitle("改名了")
	if got := s.Title(); got != "改名了" {
		t.Fatalf("title = %q", got)
	}
}

func TestSummariesSortedNewestFirst(t *testing.T) {
	s := NewStore()
	older := s.Get("old")
	older.Append(Event{Type: EventUserMsg, Text: "旧", Time: time.Now().Add(-time.Hour)})
	newer := s.Get("new")
	newer.Append(Event{Type: EventUserMsg, Text: "新", Time: time.Now()})

	summaries := s.Summaries()
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d", len(summaries))
	}
	if summaries[0].ID != "new" {
		t.Fatalf("expected newest first, got %v", summaries)
	}
	if summaries[0].MessageCount != 1 {
		t.Fatalf("message count = %d", summaries[0].MessageCount)
	}
}

func TestIDsDiscoverPersistedSessions(t *testing.T) {
	dir := t.TempDir()
	p, err := NewJSONLPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a prior run writing a session.
	if err := p.Append("persisted", []Event{{Type: EventUserMsg, Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	// A fresh store should still see it.
	store := NewStore(WithPersister(p))
	ids := store.IDs()
	if len(ids) != 1 || ids[0] != "persisted" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestDeleteSoftRemovesFromListAndDisk(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewJSONLPersister(dir)
	store := NewStore(WithPersister(p))
	store.Get("gone").Append(Event{Type: EventUserMsg, Text: "bye"})

	store.Delete("gone")
	if len(store.IDs()) != 0 {
		t.Fatalf("ids after delete = %v", store.IDs())
	}
	// The original file is gone, but a trashed copy remains.
	if _, err := os.Stat(filepath.Join(dir, "gone.jsonl")); !os.IsNotExist(err) {
		t.Fatal("original log should be gone")
	}
	entries, _ := os.ReadDir(dir)
	trashed := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), TrashSuffix) {
			trashed++
		}
	}
	if trashed != 1 {
		t.Fatalf("expected one trashed copy, dir = %v", entries)
	}
}

func TestPurgeTrashRemovesOld(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewJSONLPersister(dir)
	// Create a trash file with an old timestamp.
	old := filepath.Join(dir, "s.jsonl.trash-1000000000") // year 2001
	if err := os.WriteFile(old, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And a fresh one.
	fresh := filepath.Join(dir, "s.jsonl.trash-"+strconv.FormatInt(time.Now().Unix(), 10))
	if err := os.WriteFile(fresh, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := p.PurgeTrash(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old trash should be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh trash should be kept")
	}
}
