package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client is a thin, pure REST client for the local cyntr gateway. It mirrors
// the conventions used by cmd/cyntr/chat.go and cli.go (CYNTR_API_URL /
// CYNTR_API_KEY env vars, the {data,meta,error} envelope, and the SSE stream
// shape emitted by GET .../agents/{name}/stream). The TUI is a pure client:
// it never imports the kernel and only talks HTTP.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient builds a Client from the standard env vars, falling back to the
// same defaults as the existing CLI (http://localhost:7700, no key).
func NewClient() *Client {
	base := os.Getenv("CYNTR_API_URL")
	if base == "" {
		base = "http://localhost:7700"
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		APIKey:  os.Getenv("CYNTR_API_KEY"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// envelope is the standard API response wrapper used by web/api.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// getEnvelope performs a GET and unwraps the standard envelope, returning the
// raw Data payload.
func (c *Client) getEnvelope(ctx context.Context, path string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		// Not an envelope (e.g. auth middleware plain error) — surface status.
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil, err
	}
	if env.Error != nil {
		return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
	}
	return env.Data, nil
}

// skillInfo mirrors the shape returned by GET /api/v1/skills.
type skillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// toolDef mirrors the relevant fields returned by GET /api/v1/tools.
type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// FetchCommands queries the tool + skill registries over REST and returns the
// derived set of slash commands. Built-in commands are always included. When
// the gateway is unreachable the built-ins are still returned so the TUI is
// usable offline (graceful degradation, never fails closed on display).
func (c *Client) FetchCommands(ctx context.Context) []Command {
	cmds := append([]Command(nil), builtinCommands...)

	if data, err := c.getEnvelope(ctx, "/api/v1/skills"); err == nil {
		var skills []skillInfo
		if json.Unmarshal(data, &skills) == nil {
			for _, sk := range skills {
				if sk.Name == "" {
					continue
				}
				cmds = append(cmds, Command{
					Name:        "/skill:" + sk.Name,
					Description: firstNonEmpty(sk.Description, "skill"),
					Kind:        KindSkill,
				})
			}
		}
	}

	if data, err := c.getEnvelope(ctx, "/api/v1/tools"); err == nil {
		var tools []toolDef
		if json.Unmarshal(data, &tools) == nil {
			for _, t := range tools {
				if t.Name == "" {
					continue
				}
				cmds = append(cmds, Command{
					Name:        "/tool:" + t.Name,
					Description: firstNonEmpty(t.Description, "tool"),
					Kind:        KindTool,
				})
			}
		}
	}

	return cmds
}

// StreamEvent is one decoded server-sent event from the chat stream.
type StreamEvent struct {
	Type    string `json:"type"`    // thinking | progress | text | tools_used | error | done
	Content string `json:"content"` // text chunk for "text" events
	Event   string `json:"event"`   // sub-type for "progress" events
	Detail  string `json:"detail"`  // detail for "progress" events
}

// Stream opens the SSE chat stream for (tenant, agent) and invokes onEvent for
// each decoded event until the stream ends, the context is cancelled (Ctrl-C
// interrupt-and-redirect), or an error occurs. Cancelling ctx aborts the
// in-flight HTTP request immediately so the caller can start a new turn.
func (c *Client) Stream(ctx context.Context, tenant, agent, message string, onEvent func(StreamEvent)) error {
	q := url.Values{}
	q.Set("message", message)
	if c.APIKey != "" {
		q.Set("key", c.APIKey)
	}
	streamURL := fmt.Sprintf("%s/api/v1/tenants/%s/agents/%s/stream?%s",
		c.BaseURL, url.PathEscape(tenant), url.PathEscape(agent), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	// No client timeout for streaming — cancellation is driven by ctx.
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseSSE(resp.Body, onEvent)
}

// parseSSE reads an SSE body and dispatches decoded events. Split out so it is
// directly unit-testable without a live server.
func parseSSE(r io.Reader, onEvent func(StreamEvent)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data: "):
			payload := line[len("data: "):]
			var ev StreamEvent
			if json.Unmarshal([]byte(payload), &ev) == nil && ev.Type != "" {
				onEvent(ev)
			}
		case line == "event: done", line == "data: [DONE]":
			onEvent(StreamEvent{Type: "done"})
			return nil
		case strings.HasPrefix(line, "event: error"):
			// the following data line carries the message; handled above
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	onEvent(StreamEvent{Type: "done"})
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
