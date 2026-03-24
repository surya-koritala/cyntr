package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTransportSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","result":{"tools":[]},"id":1}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL)
	resp, err := transport.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"tools/list","id":1}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("empty response")
	}
}

func TestHTTPTransportClose(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:9999")
	if err := transport.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
