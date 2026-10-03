package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"coren/pkg/llm"
)

func TestDeriveMessagesProjectsUserAssistantTool(t *testing.T) {
	events := []Event{
		{Type: EventTurnStart, Seq: 1},
		{Type: EventUserMsg, Seq: 2, Text: "hi"},
		{Type: EventAssistant, Seq: 3, Text: "thinking", ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: "{}"}}},
		{Type: EventToolCall, Seq: 4, CallID: "c1", Name: "echo", Arguments: "{}"},
		{Type: EventToolResult, Seq: 5, CallID: "c1", Name: "echo", Text: "ok"},
		{Type: EventAssistant, Seq: 6, Text: "done"},
		{Type: EventStepEnd, Seq: 7},
		{Type: EventTurnEnd, Seq: 8},
	}

	messages := DeriveMessages(events)

	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(messages), messages)
	}
	if messages[0].Role != llm.RoleUser || messages[0].Text != "hi" {
		t.Errorf("messages[0] = %+v", messages[0])
	}
	if messages[1].Role != llm.RoleAssistant || len(messages[1].ToolCalls) != 1 {
		t.Errorf("messages[1] = %+v", messages[1])
	}
	if messages[2].Role != llm.RoleTool || messages[2].ToolCallID != "c1" || messages[2].Text != "ok" {
		t.Errorf("messages[2] = %+v", messages[2])
	}
	if messages[3].Role != llm.RoleAssistant || messages[3].Text != "done" {
		t.Errorf("messages[3] = %+v", messages[3])
	}
}

func TestDeriveMessagesKeepsFailedToolResult(t *testing.T) {
	events := []Event{
		{Type: EventToolResult, CallID: "c1", Name: "run_shell", Error: "command failed"},
	}
	messages := DeriveMessages(events)
	if len(messages) != 1 {
		t.Fatalf("messages = %d", len(messages))
	}
	if messages[0].Text != "error: command failed" {
		t.Errorf("text = %q", messages[0].Text)
	}
}

func TestAppendAssignsSequenceAndTurn(t *testing.T) {
	s := NewStore().Get("s")
	s.Append(Event{Type: EventTurnStart})
	s.Append(Event{Type: EventUserMsg, Text: "a"})
	s.Append(Event{Type: EventTurnStart})
	s.Append(Event{Type: EventUserMsg, Text: "b"})

	events := s.Events()
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d seq = %d, want %d", i, e.Seq, i+1)
		}
	}
	if events[0].Turn != 1 || events[1].Turn != 1 || events[2].Turn != 2 || events[3].Turn != 2 {
		t.Errorf("turn numbers wrong: %+v", events)
	}
}

func TestJSONLPersisterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p, err := NewJSONLPersister(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(WithPersister(p))
	sess := store.Get("conv")
	sess.Append(Event{Type: EventTurnStart})
	sess.Append(Event{Type: EventUserMsg, Text: "hello"})
	sess.Append(Event{Type: EventAssistant, Text: "world"})

	// A fresh store over the same directory must resume the history.
	store2 := NewStore(WithPersister(p))
	resumed := store2.Get("conv")
	messages := resumed.Messages()
	if len(messages) != 2 {
		t.Fatalf("resumed messages = %d, want 2: %+v", len(messages), messages)
	}
	if messages[0].Text != "hello" || messages[1].Text != "world" {
		t.Errorf("resumed messages = %+v", messages)
	}
}

func TestJSONLPersisterDropsTornTail(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewJSONLPersister(dir)

	// Write two good lines then a partial (torn) line.
	path := p.path("x")
	content := `{"type":"user/message","seq":1,"text":"ok"}` + "\n" +
		`{"type":"assistant/message","seq":2,"text":"fine"}` + "\n" +
		`{"type":"assistant/message","seq":3,"te`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := p.Load("x")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (torn tail dropped)", len(events))
	}
}

func TestForkTruncatesAtSequence(t *testing.T) {
	events := []Event{
		{Type: EventUserMsg, Seq: 1, Text: "a"},
		{Type: EventAssistant, Seq: 2, Text: "b"},
		{Type: EventUserMsg, Seq: 3, Text: "c"},
	}
	forked := Fork(events, 2)
	if len(forked) != 2 || forked[1].Text != "b" {
		t.Errorf("forked = %+v", forked)
	}
}

func TestStoreDeleteAndIDs(t *testing.T) {
	store := NewStore()
	store.Get("a")
	store.Get("b")
	if len(store.IDs()) != 2 {
		t.Errorf("ids = %v", store.IDs())
	}
	store.Delete("a")
	if len(store.IDs()) != 1 {
		t.Errorf("ids after delete = %v", store.IDs())
	}
}

func TestPersistedFileLivesUnderDir(t *testing.T) {
	dir := t.TempDir()
	p, _ := NewJSONLPersister(dir)
	store := NewStore(WithPersister(p))
	store.Get("s").Append(Event{Type: EventUserMsg, Text: "x"})

	if _, err := os.Stat(filepath.Join(dir, "s.jsonl")); err != nil {
		t.Errorf("expected session file: %v", err)
	}
}

func TestCompactionSupersedesEarlierHistory(t *testing.T) {
	events := []Event{
		{Type: EventUserMsg, Seq: 1, Text: "old question"},
		{Type: EventAssistant, Seq: 2, Text: "old answer"},
		{Type: EventCompaction, Seq: 3, Text: "earlier: discussed Q&A"},
		{Type: EventUserMsg, Seq: 4, Text: "new question"},
	}
	msgs := DeriveMessages(events)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2 (summary + new)", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "earlier: discussed Q&A") {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Text != "new question" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	// The dropped history must not appear.
	for _, m := range msgs {
		if strings.Contains(m.Text, "old question") {
			t.Fatal("superseded history leaked into projection")
		}
	}
}
