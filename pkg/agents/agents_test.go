package agents

import (
	"testing"
)

func TestStoreCreateGetListClose(t *testing.T) {
	s := NewStore()

	h1, err := s.Create("session-1")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := s.Create("session-2")
	if h1.ID == h2.ID {
		t.Error("handles must have distinct ids")
	}

	got, ok := s.Get(h1.ID)
	if !ok || got.SessionID != "session-1" {
		t.Errorf("get = %+v, ok = %v", got, ok)
	}

	list := s.List()
	if len(list) != 2 || list[0].ID != h1.ID || list[1].ID != h2.ID {
		t.Errorf("list = %+v", list)
	}

	if err := s.Close(h1.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(h1.ID); ok {
		t.Error("closed handle should be gone")
	}
	if err := s.Close("nope"); err == nil {
		t.Error("closing unknown agent should error")
	}
}

func TestStoreRejectsEmptySessionID(t *testing.T) {
	if _, err := NewStore().Create(""); err == nil {
		t.Error("empty session id should be rejected")
	}
}
