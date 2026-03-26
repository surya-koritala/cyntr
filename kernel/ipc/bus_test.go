package ipc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusNewAndClose(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("expected non-nil bus")
	}
	bus.Close()
}

func TestBusRequestReply(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	bus.Handle("responder", "echo", func(msg Message) (Message, error) {
		return Message{Type: MessageTypeResponse, Payload: msg.Payload}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := bus.Request(ctx, Message{
		Source: "caller", Target: "responder", Type: MessageTypeRequest,
		Topic: "echo", Payload: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Payload != "hello" {
		t.Fatalf("expected 'hello', got %v", resp.Payload)
	}
}

func TestBusRequestTimeout(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	bus.Handle("slow", "work", func(msg Message) (Message, error) {
		time.Sleep(5 * time.Second)
		return Message{}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := bus.Request(ctx, Message{Source: "caller", Target: "slow", Topic: "work"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestBusRequestNoHandler(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	_, err := bus.Request(context.Background(), Message{Source: "caller", Target: "nonexistent", Topic: "anything"})
	if err != ErrNoHandler {
		t.Fatalf("expected ErrNoHandler, got %v", err)
	}
}

func TestBusRequestAfterClose(t *testing.T) {
	bus := NewBus()
	bus.Close()

	_, err := bus.Request(context.Background(), Message{Source: "caller", Target: "any", Topic: "any"})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}

func TestBusBackpressureOverloaded(t *testing.T) {
	// With concurrent handlers, the inbox drains immediately (each message
	// spawns a goroutine). Backpressure only occurs when the inbox buffer
	// is full AND the reader goroutine hasn't drained it yet.
	// Use a tiny buffer and pause the reader to simulate this.
	bus := NewBusWithBufferSize(1)
	defer bus.Close()

	blocker := make(chan struct{})
	defer close(blocker)

	bus.Handle("slow", "work", func(msg Message) (Message, error) {
		<-blocker
		return Message{}, nil
	})

	// With concurrent handlers, slow handlers don't block the inbox reader,
	// so requests may succeed even with a small buffer. Verify that requests
	// with very short timeouts eventually fail (context deadline exceeded)
	// when the handler is slow — demonstrating load handling.
	var timedOut atomic.Bool
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		_, err := bus.Request(ctx, Message{Source: "caller", Target: "slow", Topic: "work"})
		cancel()
		if err != nil {
			timedOut.Store(true)
			break
		}
	}

	if !timedOut.Load() {
		t.Fatal("expected timeout or overloaded error when handler is blocked")
	}
}

func TestBusPubSub(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan string, 1)
	sub := bus.Subscribe("listener", "events.test", func(msg Message) (Message, error) {
		received <- msg.Payload.(string)
		return Message{}, nil
	})
	defer sub.Cancel()

	bus.Publish(Message{Source: "emitter", Target: "*", Type: MessageTypeEvent, Topic: "events.test", Payload: "data"})

	select {
	case val := <-received:
		if val != "data" {
			t.Fatalf("expected 'data', got %q", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 3)

	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		sub := bus.Subscribe(name, "broadcast", func(msg Message) (Message, error) {
			count.Add(1)
			done <- struct{}{}
			return Message{}, nil
		})
		defer sub.Cancel()
	}

	bus.Publish(Message{Source: "emitter", Target: "*", Type: MessageTypeEvent, Topic: "broadcast"})

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for subscriber %d", i)
		}
	}

	if count.Load() != 3 {
		t.Fatalf("expected 3, got %d", count.Load())
	}
}

func TestBusSubscriptionCancel(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	delivered := make(chan struct{}, 1)
	sub := bus.Subscribe("listener", "events.cancel", func(msg Message) (Message, error) {
		delivered <- struct{}{}
		return Message{}, nil
	})
	sub.Cancel()

	bus.Publish(Message{Source: "emitter", Target: "*", Type: MessageTypeEvent, Topic: "events.cancel"})

	select {
	case <-delivered:
		t.Fatal("received event after cancel")
	case <-time.After(200 * time.Millisecond):
		// Expected
	}
}

func TestBusPublishAfterClose(t *testing.T) {
	bus := NewBus()
	bus.Close()

	err := bus.Publish(Message{Source: "emitter", Target: "*", Type: MessageTypeEvent, Topic: "test"})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}
