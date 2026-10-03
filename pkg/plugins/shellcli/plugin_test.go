package shellcli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestAskReadsAnswerFromInput(t *testing.T) {
	var out bytes.Buffer
	sh := &Shell{
		lines: newLineReader(strings.NewReader("blue\n")),
		out:   &out,
	}

	answer, err := sh.Ask(context.Background(), "favorite color?")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "blue" {
		t.Errorf("answer = %q", answer)
	}
	if !strings.Contains(out.String(), "favorite color?") {
		t.Errorf("prompt not written: %q", out.String())
	}
}

func TestAskTimesOutWhenNoInput(t *testing.T) {
	// A reader that never produces a line.
	sh := &Shell{
		lines: newLineReader(blockingReader{}),
		out:   &bytes.Buffer{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := sh.Ask(ctx, "anything?"); err == nil {
		t.Fatal("expected timeout error")
	}
}

// blockingReader blocks forever on Read.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	select {} // never returns
}
