package mcp

import (
	"bufio"
	"io"
	"net/url"
	"strings"
)

// sseScanner reads Server-Sent Events from a stream, returning (event, data)
// pairs. It supports multi-line data fields and ignores comments.
type sseScanner struct {
	reader *bufio.Scanner
}

func newSSEScanner(r io.Reader) *sseScanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &sseScanner{reader: s}
}

// next returns the next event's type and data. The event type defaults to
// "message" when no `event:` field is present, per the SSE spec.
func (s *sseScanner) next() (event string, data string, err error) {
	var dataLines []string
	event = "message"
	for s.reader.Scan() {
		line := s.reader.Text()
		if line == "" {
			// Blank line dispatches the event.
			if len(dataLines) > 0 {
				return event, strings.Join(dataLines, "\n"), nil
			}
			event = "message"
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := s.reader.Err(); err != nil {
		return "", "", err
	}
	if len(dataLines) > 0 {
		return event, strings.Join(dataLines, "\n"), nil
	}
	return "", "", io.EOF
}

// readSSEMessage reads the first complete SSE message from a stream; used for
// Streamable HTTP responses that answer with an event-stream.
func readSSEMessage(r io.Reader) ([]byte, error) {
	scanner := newSSEScanner(r)
	for {
		_, data, err := scanner.next()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(data) != "" {
			return []byte(data), nil
		}
	}
}

// resolveEndpoint resolves a possibly relative SSE endpoint against the base URL.
func resolveEndpoint(base, endpoint string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return endpoint
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	return baseURL.ResolveReference(ref).String()
}
