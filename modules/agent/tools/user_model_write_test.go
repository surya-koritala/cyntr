package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// userModelCapture records the last upsert call sent to the bus so tests can
// assert routing.
type userModelCapture struct {
	mu      sync.Mutex
	topic   string
	payload map[string]string
}

func (c *userModelCapture) last() (string, map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.payload))
	for k, v := range c.payload {
		out[k] = v
	}
	return c.topic, out
}

func startUserModelWriteBus(t *testing.T) (*ipc.Bus, *userModelCapture) {
	t.Helper()
	bus := ipc.NewBus()
	cap := &userModelCapture{}

	handler := func(topic string) ipc.Handler {
		return func(msg ipc.Message) (ipc.Message, error) {
			cap.mu.Lock()
			cap.topic = topic
			if pm, ok := msg.Payload.(map[string]string); ok {
				cap.payload = pm
			}
			cap.mu.Unlock()
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
		}
	}
	bus.Handle("usermodel", usermodel.TopicUpsertProfile, handler(usermodel.TopicUpsertProfile))
	bus.Handle("usermodel", usermodel.TopicUpsertPreferences, handler(usermodel.TopicUpsertPreferences))
	t.Cleanup(func() { bus.Close() })
	return bus, cap
}

func TestUserModelWriteName(t *testing.T) {
	if NewUserModelWriteTool(nil).Name() != "user_model_write" {
		t.Fatal("wrong name")
	}
}

func TestUserModelWriteProfileRoutesToUpsertProfile(t *testing.T) {
	bus, cap := startUserModelWriteBus(t)
	tool := NewUserModelWriteTool(bus)

	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	out, err := tool.Execute(ctx, map[string]string{
		"section": "profile",
		"content": "Alice prefers metric units.",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected ok status, got: %s", out)
	}
	topic, payload := cap.last()
	if topic != usermodel.TopicUpsertProfile {
		t.Fatalf("wrong topic: %q", topic)
	}
	if payload["tenant"] != "acme" || payload["user"] != "alice" {
		t.Fatalf("wrong tenant/user: %+v", payload)
	}
	if payload["md"] != "Alice prefers metric units." {
		t.Fatalf("wrong md: %q", payload["md"])
	}
}

func TestUserModelWritePreferencesRoutesToUpsertPreferences(t *testing.T) {
	bus, cap := startUserModelWriteBus(t)
	tool := NewUserModelWriteTool(bus)

	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	_, err := tool.Execute(ctx, map[string]string{
		"section": "preferences",
		"content": "- short bullets",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	topic, _ := cap.last()
	if topic != usermodel.TopicUpsertPreferences {
		t.Fatalf("wrong topic: %q", topic)
	}
}

func TestUserModelWriteInvalidSection(t *testing.T) {
	bus, _ := startUserModelWriteBus(t)
	tool := NewUserModelWriteTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	_, err := tool.Execute(ctx, map[string]string{"section": "bogus", "content": "x"})
	if err == nil || !strings.Contains(err.Error(), "section must be") {
		t.Fatalf("expected section error, got: %v", err)
	}
}

func TestUserModelWriteSizeCap(t *testing.T) {
	bus, _ := startUserModelWriteBus(t)
	tool := NewUserModelWriteTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "assistant", "alice")
	big := strings.Repeat("z", usermodel.MaxSectionBytes+1)
	_, err := tool.Execute(ctx, map[string]string{"section": "profile", "content": big})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size error, got: %v", err)
	}
}

func TestUserModelWriteRequiresToolCaller(t *testing.T) {
	bus, _ := startUserModelWriteBus(t)
	tool := NewUserModelWriteTool(bus)
	// No WithToolCaller — should reject.
	_, err := tool.Execute(context.Background(), map[string]string{"section": "profile", "content": "x"})
	if err == nil || !strings.Contains(err.Error(), "no tenant/user") {
		t.Fatalf("expected tenant/user error, got: %v", err)
	}
}
