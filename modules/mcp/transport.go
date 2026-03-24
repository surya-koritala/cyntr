package mcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Transport is the interface for MCP communication.
type Transport interface {
	Send(ctx context.Context, request []byte) ([]byte, error)
	Close() error
}

// StdioTransport communicates with an MCP server via subprocess stdin/stdout.
type StdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	return &StdioTransport{cmd: cmd, stdin: stdin, scanner: scanner}, nil
}

func (t *StdioTransport) Send(ctx context.Context, request []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.stdin.Write(request); err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return nil, fmt.Errorf("read stdout: %w", err)
		}
		return nil, fmt.Errorf("MCP server process closed stdout")
	}

	return t.scanner.Bytes(), nil
}

func (t *StdioTransport) Close() error {
	t.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()
	select {
	case <-time.After(5 * time.Second):
		t.cmd.Process.Kill()
		return fmt.Errorf("process killed after timeout")
	case err := <-done:
		return err
	}
}

// HTTPTransport communicates with an MCP server via HTTP POST.
type HTTPTransport struct {
	url    string
	client *http.Client
}

func NewHTTPTransport(url string) *HTTPTransport {
	return &HTTPTransport{
		url:    url,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *HTTPTransport) Send(ctx context.Context, request []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return body, nil
}

func (t *HTTPTransport) Close() error { return nil }
