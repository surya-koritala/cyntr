package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeServerless struct {
	mu         sync.Mutex
	provisions int
	execs      int
}

func (f *fakeServerless) Provider() string { return "fake" }
func (f *fakeServerless) Provision(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provisions++
	return "h" + strconv.Itoa(f.provisions), nil
}
func (f *fakeServerless) Exec(_ context.Context, handle, cmd string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs++
	return "ran:" + cmd + "@" + handle, nil
}

func TestServerlessColdThenWarmReuse(t *testing.T) {
	fc := &fakeServerless{}
	b := NewServerlessBackend(fc, time.Hour)
	cur := time.Unix(1000, 0)
	b.nowFn = func() time.Time { return cur }

	out, err := b.Run(context.Background(), "acme", "echo a", 0)
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	if !strings.Contains(out, "@h1") {
		t.Fatalf("cold run should use first handle: %q", out)
	}
	cur = cur.Add(time.Minute) // still within TTL
	b.Run(context.Background(), "acme", "echo b", 0)

	if fc.provisions != 1 {
		t.Fatalf("warm reuse should not re-provision: provisions=%d", fc.provisions)
	}
	if fc.execs != 2 {
		t.Fatalf("execs=%d, want 2", fc.execs)
	}
}

func TestServerlessReprovisionsAfterIdle(t *testing.T) {
	fc := &fakeServerless{}
	b := NewServerlessBackend(fc, time.Minute)
	cur := time.Unix(1000, 0)
	b.nowFn = func() time.Time { return cur }

	b.Run(context.Background(), "acme", "echo a", 0) // provision h1
	cur = cur.Add(2 * time.Minute)                   // hibernated past TTL
	b.Run(context.Background(), "acme", "echo b", 0) // must wake -> provision h2

	if fc.provisions != 2 {
		t.Fatalf("idle sandbox should be re-provisioned (woken): provisions=%d", fc.provisions)
	}
}

func TestServerlessPerTenantHandles(t *testing.T) {
	fc := &fakeServerless{}
	b := NewServerlessBackend(fc, time.Hour)
	cur := time.Unix(1000, 0)
	b.nowFn = func() time.Time { return cur }
	b.Run(context.Background(), "acme", "x", 0)
	b.Run(context.Background(), "globex", "y", 0)
	if fc.provisions != 2 {
		t.Fatalf("distinct tenants should get distinct sandboxes: provisions=%d", fc.provisions)
	}
}

func TestReplaceHandle(t *testing.T) {
	if got := replaceHandle("/v1/sandboxes/{handle}/exec", "sb1"); got != "/v1/sandboxes/sb1/exec" {
		t.Fatalf("replaceHandle = %q", got)
	}
}

func TestModalClientRoundTrip(t *testing.T) {
	assertServerlessClient(t, func(base string, c *http.Client) ServerlessClient {
		return NewModalClient(base, "tok", c)
	}, "/v1/sandboxes", "/v1/sandboxes/sb1/exec")
}

func TestDaytonaClientRoundTrip(t *testing.T) {
	assertServerlessClient(t, func(base string, c *http.Client) ServerlessClient {
		return NewDaytonaClient(base, "tok", c)
	}, "/workspaces", "/workspaces/sb1/exec")
}

func assertServerlessClient(t *testing.T, build func(base string, c *http.Client) ServerlessClient, provPath, execPath string) {
	t.Helper()
	var provisioned, execed bool
	mux := http.NewServeMux()
	mux.HandleFunc(provPath, func(w http.ResponseWriter, r *http.Request) {
		provisioned = true
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer token")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["scope"] != "acme" {
			t.Errorf("provision scope = %v", body["scope"])
		}
		json.NewEncoder(w).Encode(map[string]any{"handle": "sb1"})
	})
	mux.HandleFunc(execPath, func(w http.ResponseWriter, r *http.Request) {
		execed = true
		json.NewEncoder(w).Encode(map[string]any{"output": "hello-world"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := build(srv.URL, srv.Client())
	handle, err := c.Provision(context.Background(), "acme")
	if err != nil || handle != "sb1" {
		t.Fatalf("provision: handle=%q err=%v", handle, err)
	}
	out, err := c.Exec(context.Background(), handle, "echo hi")
	if err != nil || out != "hello-world" {
		t.Fatalf("exec: out=%q err=%v", out, err)
	}
	if !provisioned || !execed {
		t.Fatalf("expected provision + exec calls (prov=%v exec=%v)", provisioned, execed)
	}
}
