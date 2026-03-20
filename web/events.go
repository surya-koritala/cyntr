package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// EventBroker manages SSE connections for real-time updates.
type EventBroker struct {
	mu      sync.RWMutex
	clients map[chan string]bool
}

func NewEventBroker() *EventBroker {
	return &EventBroker{clients: make(map[chan string]bool)}
}

// Subscribe adds a client channel.
func (b *EventBroker) Subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a client channel.
func (b *EventBroker) Unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	close(ch)
	b.mu.Unlock()
}

// Broadcast sends an event to all connected clients.
func (b *EventBroker) Broadcast(eventType string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	event := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(jsonData))

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- event:
		default: // skip slow clients
		}
	}
}

// ClientCount returns the number of connected SSE clients.
func (b *EventBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ServeHTTP handles SSE connections.
func (b *EventBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"ok\"}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprint(w, event)
			flusher.Flush()
		}
	}
}
