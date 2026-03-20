package audit

import (
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestEmitterEmit(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	received := make(chan Entry, 1)
	bus.Subscribe("test", "audit.write", func(msg ipc.Message) (ipc.Message, error) {
		received <- msg.Payload.(Entry)
		return ipc.Message{}, nil
	})

	emitter := NewEmitter(bus, "test-node")
	emitter.Emit(Entry{Tenant: "finance", Action: Action{Type: "test_action"}})

	select {
	case entry := <-received:
		if entry.Tenant != "finance" {
			t.Fatalf("got %q", entry.Tenant)
		}
		if entry.Instance != "test-node" {
			t.Fatalf("got %q", entry.Instance)
		}
		if entry.ID == "" {
			t.Fatal("expected auto-generated ID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestEmitterPolicyCheck(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	received := make(chan Entry, 1)
	bus.Subscribe("test", "audit.write", func(msg ipc.Message) (ipc.Message, error) {
		received <- msg.Payload.(Entry)
		return ipc.Message{}, nil
	})

	emitter := NewEmitter(bus, "test")
	emitter.EmitPolicyCheck("finance", "jane@corp.com", "bot", "tool_call", "shell_exec", "deny-shell", "deny", 2)

	select {
	case entry := <-received:
		if entry.Policy.Decision != "deny" {
			t.Fatalf("got %q", entry.Policy.Decision)
		}
		if entry.Principal.User != "jane@corp.com" {
			t.Fatalf("got %q", entry.Principal.User)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestEmitterAgentChat(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	received := make(chan Entry, 1)
	bus.Subscribe("test", "audit.write", func(msg ipc.Message) (ipc.Message, error) {
		received <- msg.Payload.(Entry)
		return ipc.Message{}, nil
	})

	emitter := NewEmitter(bus, "test")
	emitter.EmitAgentChat("demo", "bob", "assistant", "Hello", []string{"browse_web"}, 1500)

	select {
	case entry := <-received:
		if entry.Action.Type != "agent_chat" {
			t.Fatalf("got %q", entry.Action.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
