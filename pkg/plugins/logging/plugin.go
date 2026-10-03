// Package logging traces agent activity through kernel events.
//
// It is a pure observer: it subscribes to events and never mutates them, so it
// can be mounted or dropped without affecting behaviour. Output is one line per
// event, with timestamps, so a transcript can be reconstructed and timed.
package logging

import (
	"context"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"coren/pkg/agent"
	"coren/pkg/coren"
	"coren/pkg/llm"
)

// Plugin logs model requests, tool execution (with duration), and token usage.
type Plugin struct {
	// Output defaults to os.Stderr.
	Output io.Writer
	// Prefix is prepended to each line; defaults to "coren".
	Prefix string
}

func (Plugin) ID() string       { return "logging" }
func (Plugin) Inject() []string { return nil }

func (p Plugin) Apply(ctx coren.Context) error {
	out := p.Output
	if out == nil {
		out = os.Stderr
	}
	prefix := p.Prefix
	if prefix == "" {
		prefix = "coren"
	}
	logger := log.New(out, prefix+": ", log.LstdFlags)

	// Track per-tool start times to report durations on completion.
	var mu sync.Mutex
	starts := map[string]time.Time{}
	callKey := func(id, name string) string {
		if id != "" {
			return id
		}
		return name
	}

	ctx.OnWaterfall(coren.EventAgentRequest, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
		if req, ok := payload.(*agent.RequestPayload); ok {
			logger.Printf("request model=%s messages=%d tools=%d",
				req.Request.Model, len(req.Request.Messages), len(req.Request.Tools))
		}
		return next(payload)
	}, false)

	ctx.OnWaterfall(coren.EventToolsPreExecute, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
		if call, ok := payload.(*agent.PreExecutePayload); ok {
			mu.Lock()
			starts[callKey("", call.Name)] = time.Now()
			mu.Unlock()
			logger.Printf("tool start name=%s", call.Name)
		}
		return next(payload)
	}, false)

	ctx.OnWaterfall(coren.EventToolsPostExecute, func(_ context.Context, payload any, next func(any) (any, error)) (any, error) {
		if res, ok := payload.(*agent.PostExecutePayload); ok {
			mu.Lock()
			start, seen := starts[callKey("", res.Name)]
			delete(starts, callKey("", res.Name))
			mu.Unlock()

			status := "ok"
			if res.Err != nil {
				status = "error"
			}
			if seen {
				logger.Printf("tool done name=%s status=%s duration=%s",
					res.Name, status, time.Since(start).Round(time.Millisecond))
			} else {
				logger.Printf("tool done name=%s status=%s", res.Name, status)
			}
		}
		return next(payload)
	}, false)

	// Token usage, reported by the agent on the terminal event.
	ctx.On(coren.EventModelUsage, func(_ context.Context, payload any) {
		if u, ok := payload.(*llm.Usage); ok {
			logger.Printf("usage input=%d output=%d", u.InputTokens, u.OutputTokens)
		}
	})

	ctx.On(coren.EventPluginLoaded, func(_ context.Context, payload any) {
		if id, ok := payload.(string); ok {
			logger.Printf("plugin loaded id=%s", id)
		}
	})
	return nil
}
