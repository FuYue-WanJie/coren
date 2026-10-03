package session

import "coren/pkg/llm"

// DeriveMessages projects the model-visible history from a session log.
//
// Only durable message facts become model messages; turn/step framing events are
// ignored. Tool results are never dropped, even when a turn ended abnormally,
// so a resumed conversation always shows the model what its tools returned.
func DeriveMessages(events []Event) []llm.Message {
	// A compaction event supersedes everything before it: the summary stands in
	// for the dropped history, keeping the model's context bounded.
	start := 0
	for i, e := range events {
		if e.Type == EventCompaction {
			start = i
		}
	}

	var messages []llm.Message
	for _, e := range events[start:] {
		switch e.Type {
		case EventCompaction:
			messages = append(messages, llm.Message{
				Role: llm.RoleSystem,
				Text: "Summary of earlier conversation:\n" + e.Text,
			})
		case EventUserMsg:
			if e.Text == "" {
				continue
			}
			messages = append(messages, llm.Message{Role: llm.RoleUser, Text: e.Text})
		case EventAssistant:
			if e.Text == "" && len(e.ToolCalls) == 0 {
				continue
			}
			messages = append(messages, llm.Message{
				Role:      llm.RoleAssistant,
				Text:      e.Text,
				ToolCalls: append([]llm.ToolCall(nil), e.ToolCalls...),
			})
		case EventToolResult:
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				Name:       e.Name,
				ToolCallID: e.CallID,
				Text:       toolResultText(e),
				Parts:      append([]llm.ContentPart(nil), e.Parts...),
			})
		}
	}
	return messages
}

// toolResultText reconstructs a tool result's model-visible text.
func toolResultText(e Event) string {
	if e.Error == "" {
		return e.Text
	}
	if e.Text == "" {
		return "error: " + e.Error
	}
	return e.Text + "\nerror: " + e.Error
}

// Fork returns a new event slice containing events up to and including the given
// sequence number, for seeding a branched session.
func Fork(events []Event, upToSeq int64) []Event {
	var out []Event
	for _, e := range events {
		if e.Seq > upToSeq {
			break
		}
		out = append(out, e)
	}
	return out
}
