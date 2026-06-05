package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// ServerlessClient provisions (or wakes) a remote sandbox and runs commands in
// it. Modal and Daytona both fit this shape; concrete clients build the right
// HTTP calls. Provision is expected to be idempotent-ish: calling it after the
// sandbox has hibernated wakes it.
type ServerlessClient interface {
	Provider() string
	Provision(ctx context.Context, key string) (handle string, err error)
	Exec(ctx context.Context, handle, command string) (string, error)
}

// ServerlessBackend runs shell commands on a serverless sandbox that hibernates
// when idle and wakes on demand (Modal, Daytona) — near-zero cost between
// sessions. It caches one handle per scope key with a TTL: within the TTL the
// warm handle is reused; past it the sandbox is assumed hibernated and is
// re-provisioned (woken) on the next call.
type ServerlessBackend struct {
	client ServerlessClient
	ttl    time.Duration

	mu    sync.Mutex
	cache map[string]cachedHandle
	nowFn func() time.Time // injectable clock for tests
}

type cachedHandle struct {
	handle   string
	lastUsed time.Time
}

// NewServerlessBackend wraps a ServerlessClient. ttl<=0 defaults to 10 minutes.
func NewServerlessBackend(client ServerlessClient, ttl time.Duration) *ServerlessBackend {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ServerlessBackend{
		client: client,
		ttl:    ttl,
		cache:  make(map[string]cachedHandle),
		nowFn:  time.Now,
	}
}

// Run executes command on the (tenant-scoped) serverless sandbox, provisioning
// or waking it as needed.
func (b *ServerlessBackend) Run(ctx context.Context, tenant, command string, timeout time.Duration) (string, error) {
	if b == nil || b.client == nil {
		return "", fmt.Errorf("serverless backend not configured")
	}
	if timeout <= 0 {
		timeout = shellDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	handle, err := b.acquire(runCtx, tenant)
	if err != nil {
		return "", fmt.Errorf("%s backend: provision: %w", b.client.Provider(), err)
	}
	out, err := b.client.Exec(runCtx, handle, command)
	if err != nil {
		return truncateOutput(out), fmt.Errorf("command failed: %w", err)
	}
	return truncateOutput(out), nil
}

// acquire returns a warm handle for key, provisioning (or waking) the sandbox
// when there is no cached handle or it has gone idle past the TTL.
func (b *ServerlessBackend) acquire(ctx context.Context, key string) (string, error) {
	now := b.nowFn()
	b.mu.Lock()
	cached, ok := b.cache[key]
	fresh := ok && now.Sub(cached.lastUsed) <= b.ttl
	b.mu.Unlock()

	if fresh {
		b.touch(key, now)
		return cached.handle, nil
	}

	handle, err := b.client.Provision(ctx, key)
	if err != nil {
		return "", err
	}
	b.mu.Lock()
	b.cache[key] = cachedHandle{handle: handle, lastUsed: b.nowFn()}
	b.mu.Unlock()
	return handle, nil
}

func (b *ServerlessBackend) touch(key string, now time.Time) {
	b.mu.Lock()
	if c, ok := b.cache[key]; ok {
		c.lastUsed = now
		b.cache[key] = c
	}
	b.mu.Unlock()
}

// ---- Concrete HTTP clients ----

// HTTPServerlessClient is a configurable client for serverless sandbox APIs.
// Modal and Daytona are constructed as presets below; both follow the same
// provision-then-exec shape over HTTP with a bearer token.
type HTTPServerlessClient struct {
	provider     string
	baseURL      string
	token        string
	provisionURL string // relative path
	execURL      string // relative path; {handle} is substituted
	http         *http.Client
}

func (c *HTTPServerlessClient) Provider() string { return c.provider }

func (c *HTTPServerlessClient) do(ctx context.Context, path string, body any) (map[string]any, error) {
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: status %d: %s", c.provider, resp.StatusCode, string(data))
	}
	var out map[string]any
	if len(data) > 0 {
		json.Unmarshal(data, &out)
	}
	return out, nil
}

func (c *HTTPServerlessClient) httpClient() *http.Client {
	if c.http != nil {
		return c.http
	}
	return http.DefaultClient
}

func (c *HTTPServerlessClient) Provision(ctx context.Context, key string) (string, error) {
	out, err := c.do(ctx, c.provisionURL, map[string]any{"scope": key})
	if err != nil {
		return "", err
	}
	if h, ok := out["handle"].(string); ok && h != "" {
		return h, nil
	}
	return "", fmt.Errorf("%s: provision returned no handle", c.provider)
}

func (c *HTTPServerlessClient) Exec(ctx context.Context, handle, command string) (string, error) {
	out, err := c.do(ctx, replaceHandle(c.execURL, handle), map[string]any{"handle": handle, "command": command})
	if err != nil {
		return "", err
	}
	if s, ok := out["output"].(string); ok {
		return s, nil
	}
	return "", nil
}

func replaceHandle(tmpl, handle string) string {
	out := ""
	for i := 0; i < len(tmpl); i++ {
		if i+8 <= len(tmpl) && tmpl[i:i+8] == "{handle}" {
			out += handle
			i += 7
			continue
		}
		out += string(tmpl[i])
	}
	return out
}

// NewModalClient builds a client for Modal's sandbox API.
func NewModalClient(baseURL, token string, httpc *http.Client) *HTTPServerlessClient {
	if baseURL == "" {
		baseURL = "https://api.modal.com"
	}
	return &HTTPServerlessClient{
		provider:     "modal",
		baseURL:      baseURL,
		token:        token,
		provisionURL: "/v1/sandboxes",
		execURL:      "/v1/sandboxes/{handle}/exec",
		http:         httpc,
	}
}

// NewDaytonaClient builds a client for Daytona's workspace API.
func NewDaytonaClient(baseURL, token string, httpc *http.Client) *HTTPServerlessClient {
	if baseURL == "" {
		baseURL = "https://app.daytona.io/api"
	}
	return &HTTPServerlessClient{
		provider:     "daytona",
		baseURL:      baseURL,
		token:        token,
		provisionURL: "/workspaces",
		execURL:      "/workspaces/{handle}/exec",
		http:         httpc,
	}
}
