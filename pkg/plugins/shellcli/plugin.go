// Package shellcli runs an interactive terminal session.
package shellcli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"coren/pkg/agent"
	"coren/pkg/agents"
	"coren/pkg/approval"
	"coren/pkg/ask"
	"coren/pkg/coren"
	"coren/pkg/delivery"
	"coren/pkg/session"
	"coren/pkg/shell"
)

// Plugin mounts the terminal shell on the shell service.
type Plugin struct {
	// AgentConfig sets the loop this shell runs.
	AgentConfig agent.Agent
	// SessionID names the conversation to resume or create.
	SessionID string
	// Input and Output default to os.Stdin / os.Stdout when nil.
	Input  io.Reader
	Output io.Writer
}

func (Plugin) ID() string       { return "shell.cli" }
func (Plugin) Inject() []string { return []string{session.Key, agents.Key, agents.LoopKey} }

func (p Plugin) Apply(ctx coren.Context) error {
	sessions, ok := coren.UnwrapKey[session.Service](ctx, session.Key)
	if !ok {
		return fmt.Errorf("shellcli: sessions service missing")
	}
	ag := p.AgentConfig
	ag.Context = ctx
	id := p.SessionID
	if id == "" {
		id = "cli"
	}
	in := p.Input
	if in == nil {
		in = os.Stdin
	}
	out := p.Output
	if out == nil {
		out = os.Stdout
	}
	shellInstance := &Shell{
		agent:     &ag,
		sessions:  sessions,
		sessionID: id,
		lines:     newLineReader(in),
		out:       out,
	}
	ctx.Provide(shell.Key, shellInstance)
	// Expose the same shell as the asker so the ask_user tool can prompt here.
	ctx.Provide(ask.Key, shellInstance)
	// And as the approval requester so guarded actions can ask the user.
	ctx.Provide(approval.RequesterKey, approvalRequester{shell: shellInstance})
	// And as the deliverable reviewer so `deliver` can pause for approval.
	ctx.Provide(delivery.ReviewerKey, deliveryReviewer{shell: shellInstance})
	return nil
}

// deliveryReviewer adapts the shell to delivery.Reviewer.
type deliveryReviewer struct{ shell *Shell }

func (r deliveryReviewer) Review(ctx context.Context, d delivery.Deliverable) (delivery.Decision, error) {
	return r.shell.Review(ctx, d)
}

// approvalRequester adapts the shell to approval.Requester.
type approvalRequester struct{ shell *Shell }

func (a approvalRequester) RequestApproval(ctx context.Context, req approval.Request) (approval.Decision, error) {
	return a.shell.RequestApproval(ctx, req)
}

// Shell is the interactive terminal shell.
type Shell struct {
	agent     *agent.Agent
	sessions  session.Service
	sessionID string
	lines     *lineReader
	out       io.Writer
}

func (s *Shell) Name() string { return "cli" }

// Run reads lines from input and streams each turn to output.
func (s *Shell) Run(ctx context.Context) error {
	sess := s.sessions.Get(s.sessionID)
	fmt.Fprintln(s.out, "Coren interactive session. Ctrl-D to exit.")

	for {
		fmt.Fprint(s.out, "\n> ")
		line, ok := s.lines.readLine()
		if !ok {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "/exit" || line == "/quit" {
			break
		}
		s.streamTurn(ctx, sess, line)
	}
	fmt.Fprintln(s.out)
	return s.lines.err()
}

// Ask prompts the user on the terminal and returns their answer (ask.Asker).
func (s *Shell) Ask(ctx context.Context, question string) (string, error) {
	return s.readAnswer(ctx, question)
}

// Review presents a deliverable and blocks for approve/revise/reject
// (delivery.Reviewer). Attachments are listed and can be opened by number.
func (s *Shell) Review(ctx context.Context, d delivery.Deliverable) (delivery.Decision, error) {
	s.renderDeliverable(d)

	if len(d.Attachments) > 0 {
		if err := s.reviewAttachments(ctx, d); err != nil {
			return delivery.Decision{By: "user", Feedback: err.Error()}, err
		}
	}

	for {
		answer, err := s.readAnswer(ctx, "批准？[y=approve / r=revise / n=reject]")
		if err != nil {
			return delivery.Decision{Verdict: delivery.Rejected, By: "user", Feedback: err.Error()}, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes", "approve", "ok", "是", "好":
			return delivery.Decision{Verdict: delivery.Approved, By: "user"}, nil
		case "r", "revise", "修改":
			feedback, _ := s.readAnswer(ctx, "修改意见")
			return delivery.Decision{Verdict: delivery.Revise, By: "user", Feedback: feedback}, nil
		case "n", "no", "reject", "拒绝":
			feedback, _ := s.readAnswer(ctx, "拒绝原因（可空）")
			return delivery.Decision{Verdict: delivery.Rejected, By: "user", Feedback: feedback}, nil
		default:
			fmt.Fprintln(s.out, "请输入 y / r / n")
		}
	}
}

// renderDeliverable prints the deliverable header and body.
func (s *Shell) renderDeliverable(d delivery.Deliverable) {
	fmt.Fprintf(s.out, "\n========== 交付物 [%s] %s ==========\n", d.Kind, d.Title)
	fmt.Fprintln(s.out, d.Content)
	if len(d.Attachments) > 0 {
		fmt.Fprintln(s.out, "\n---- 附件 ----")
		for i, a := range d.Attachments {
			status := fmt.Sprintf("%d bytes", a.Size)
			if a.Error != "" {
				status = "读取失败: " + a.Error
			} else if a.Text == "" {
				status += " (太大，需按路径查看)"
			}
			fmt.Fprintf(s.out, "  [%d] %s (%s)\n", i+1, a.Path, status)
		}
	}
	fmt.Fprintln(s.out, "======================================")
}

// reviewAttachments lets the user open an attachment by number before deciding.
func (s *Shell) reviewAttachments(ctx context.Context, d delivery.Deliverable) error {
	for {
		answer, err := s.readAnswer(ctx, "输入附件编号查看内容，或直接回车进入审批")
		if err != nil {
			return err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return nil
		}
		var n int
		if _, err := fmt.Sscanf(answer, "%d", &n); err != nil || n < 1 || n > len(d.Attachments) {
			fmt.Fprintln(s.out, "无效编号")
			continue
		}
		a := d.Attachments[n-1]
		if a.Error != "" {
			fmt.Fprintf(s.out, "无法读取 %s: %s\n", a.Path, a.Error)
			continue
		}
		if a.Text == "" {
			fmt.Fprintf(s.out, "%s 内联内容不可用，请按路径打开。\n", a.Path)
			continue
		}
		fmt.Fprintf(s.out, "\n----- %s -----\n%s\n---------------\n", a.Path, a.Text)
	}
}

// RequestApproval asks the user to approve a guarded action (approval.Requester).
func (s *Shell) RequestApproval(ctx context.Context, req approval.Request) (approval.Decision, error) {
	prompt := req.Tool
	if req.Summary != "" {
		prompt += ": " + req.Summary
	}
	if req.Reason != "" {
		prompt += "\n  reason: " + req.Reason
	}
	prompt += "\n  allow? [y/N]"

	answer, err := s.readAnswer(ctx, prompt)
	if err != nil {
		return approval.Decision{Approved: false, By: "user", Note: err.Error()}, err
	}
	if approves(answer) {
		return approval.Decision{Approved: true, By: "user"}, nil
	}
	note := "denied by user"
	if answer != "" {
		note = "denied: " + answer
	}
	return approval.Decision{Approved: false, By: "user", Note: note}, nil
}

// approves reports whether an answer means yes.
func approves(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "ok", "allow", "是", "好", "可以":
		return true
	default:
		return false
	}
}

// readAnswer prompts and reads one line, honoring context cancellation.
func (s *Shell) readAnswer(ctx context.Context, question string) (string, error) {
	fmt.Fprintf(s.out, "\n[?] %s\n%s ", question, "> ")

	type result struct {
		line string
		ok   bool
	}
	done := make(chan result, 1)
	go func() {
		line, ok := s.lines.readLine()
		done <- result{line: line, ok: ok}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		if !r.ok {
			return "", fmt.Errorf("no input available")
		}
		return strings.TrimSpace(r.line), nil
	}
}

// Prompt runs a single turn and returns when it completes.
func (s *Shell) Prompt(ctx context.Context, input string) error {
	sess := s.sessions.Get(s.sessionID)
	s.streamTurn(ctx, sess, input)
	return nil
}

func (s *Shell) streamTurn(ctx context.Context, sess session.Session, input string) {
	for ev := range s.agent.Send(ctx, sess, input) {
		switch {
		case ev.Err != nil:
			fmt.Fprintf(s.out, "error: %v\n", ev.Err)
		case ev.Rejected != nil:
			fmt.Fprintf(s.out, "[rejected] %s\n", ev.Rejected.Reason)
		case ev.TextDelta != "":
			fmt.Fprint(s.out, ev.TextDelta)
		case ev.ReasoningDelta != "":
			fmt.Fprint(s.out, "\x1b[2m"+ev.ReasoningDelta+"\x1b[0m")
		case ev.ToolCallStart != nil:
			fmt.Fprintf(s.out, "\n[tool] %s(%s)\n", ev.ToolCallStart.Name, ev.ToolCallStart.Arguments)
		case ev.ToolCallResult != nil:
			if ev.ToolCallResult.Err != nil {
				fmt.Fprintf(s.out, "[tool] %s error: %v\n", ev.ToolCallResult.Name, ev.ToolCallResult.Err)
			} else {
				fmt.Fprintf(s.out, "[tool] %s done\n", ev.ToolCallResult.Name)
			}
		case ev.Done:
			fmt.Fprintln(s.out)
		}
	}
}

// lineReader serializes reads from one input stream so the REPL loop and the
// ask_user tool can share stdin without racing on the same scanner.
type lineReader struct {
	mu      sync.Mutex
	scanner *bufio.Scanner
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{scanner: bufio.NewScanner(r)}
}

// readLine reads one line, returning ok=false at EOF.
func (l *lineReader) readLine() (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.scanner.Scan() {
		return "", false
	}
	return l.scanner.Text(), true
}

func (l *lineReader) err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scanner.Err()
}
