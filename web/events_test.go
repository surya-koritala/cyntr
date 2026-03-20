package web

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBrokerBroadcast(t *testing.T) {
	broker := NewEventBroker()
	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	broker.Broadcast("test", map[string]string{"message": "hello"})

	select {
	case event := <-ch:
		if !strings.Contains(event, "event: test") {
			t.Fatalf("wrong event: %q", event)
		}
		if !strings.Contains(event, "hello") {
			t.Fatalf("missing data: %q", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestEventBrokerClientCount(t *testing.T) {
	broker := NewEventBroker()
	if broker.ClientCount() != 0 {
		t.Fatal("expected 0")
	}

	ch1 := broker.Subscribe()
	ch2 := broker.Subscribe()
	if broker.ClientCount() != 2 {
		t.Fatal("expected 2")
	}

	broker.Unsubscribe(ch1)
	broker.Unsubscribe(ch2)
	if broker.ClientCount() != 0 {
		t.Fatal("expected 0 after unsub")
	}
}

func TestEventBrokerSSEEndpoint(t *testing.T) {
	broker := NewEventBroker()

	server := httptest.NewServer(broker)
	defer server.Close()

	// Connect to SSE
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}

	// Read the initial connected event
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		if line == "" && len(lines) > 1 {
			break // end of first event
		}
	}

	found := false
	for _, l := range lines {
		if strings.Contains(l, "connected") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing connected event: %v", lines)
	}
}

func TestEventBrokerSlowClient(t *testing.T) {
	broker := NewEventBroker()
	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	// Fill the buffer (64 messages)
	for i := 0; i < 100; i++ {
		broker.Broadcast("flood", i)
	}

	// Should not panic — slow clients are skipped
	if broker.ClientCount() != 1 {
		t.Fatal("client should still be subscribed")
	}
}
