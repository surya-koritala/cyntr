package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSSE(t *testing.T) {
	body := strings.Join([]string{
		`event: message`,
		`data: {"type":"thinking","content":""}`,
		``,
		`event: message`,
		`data: {"type":"progress","event":"tool_call","detail":"http"}`,
		``,
		`event: message`,
		`data: {"type":"text","content":"Hello "}`,
		``,
		`event: message`,
		`data: {"type":"text","content":"world"}`,
		``,
		`event: done`,
	}, "\n")

	var got []StreamEvent
	err := parseSSE(strings.NewReader(body), func(ev StreamEvent) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("parseSSE error: %v", err)
	}

	wantTypes := []string{"thinking", "progress", "text", "text", "done"}
	if len(got) != len(wantTypes) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(wantTypes), got)
	}
	for i, wt := range wantTypes {
		if got[i].Type != wt {
			t.Errorf("event %d type = %q, want %q", i, got[i].Type, wt)
		}
	}
	// Reassemble text.
	var b strings.Builder
	for _, ev := range got {
		if ev.Type == "text" {
			b.WriteString(ev.Content)
		}
	}
	if b.String() != "Hello world" {
		t.Errorf("reassembled text = %q, want %q", b.String(), "Hello world")
	}
}

func TestFetchCommandsFromRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/skills":
			w.Write([]byte(`{"data":[{"name":"summarize","description":"summarize text"}],"meta":{},"error":null}`))
		case "/api/v1/tools":
			w.Write([]byte(`{"data":[{"name":"http","description":"http tool"}],"meta":{},"error":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	cmds := c.FetchCommands(context.Background())

	have := map[string]CommandKind{}
	for _, cmd := range cmds {
		have[cmd.Name] = cmd.Kind
	}
	if have["/skill:summarize"] != KindSkill {
		t.Errorf("missing skill command, have: %v", have)
	}
	if have["/tool:http"] != KindTool {
		t.Errorf("missing tool command, have: %v", have)
	}
	// Built-ins must still be present.
	if have["/help"] != KindBuiltin {
		t.Errorf("missing builtin /help, have: %v", have)
	}
}

func TestFetchCommandsGatewayDownReturnsBuiltins(t *testing.T) {
	// Point at a closed server so requests fail; built-ins must still come back.
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: srvClientWithShortTimeout()}
	cmds := c.FetchCommands(context.Background())
	if len(cmds) != len(builtinCommands) {
		t.Fatalf("expected only %d builtins when gateway down, got %d", len(builtinCommands), len(cmds))
	}
}

func srvClientWithShortTimeout() *http.Client {
	return &http.Client{}
}

func TestGetEnvelopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"data":null,"meta":{},"error":{"code":"SKILL_ERROR","message":"boom"}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.getEnvelope(context.Background(), "/api/v1/skills")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected envelope error, got %v", err)
	}
}
