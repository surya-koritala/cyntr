package ipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const defaultBufferSize = 1024

type Bus struct {
	mu          sync.RWMutex
	handlers    map[string]map[string]handlerEntry
	subscribers map[string][]subscriberEntry
	closed      bool
	bufferSize  int
}

type handlerEntry struct {
	handler Handler
	inbox   chan requestEnvelope
}

type requestEnvelope struct {
	msg     Message
	replyCh chan replyEnvelope
}

type replyEnvelope struct {
	msg Message
	err error
}

type subscriberEntry struct {
	module  string
	handler Handler
	id      string
}

func NewBus() *Bus {
	return NewBusWithBufferSize(defaultBufferSize)
}

func NewBusWithBufferSize(bufferSize int) *Bus {
	return &Bus{
		handlers:    make(map[string]map[string]handlerEntry),
		subscribers: make(map[string][]subscriberEntry),
		bufferSize:  bufferSize,
	}
}

func (b *Bus) Handle(module, topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.handlers[module] == nil {
		b.handlers[module] = make(map[string]handlerEntry)
	}

	// Re-registering a handler overwrites the map entry; close the previous
	// inbox so its consumer goroutine exits instead of leaking.
	if prev, ok := b.handlers[module][topic]; ok {
		close(prev.inbox)
	}

	entry := handlerEntry{
		handler: h,
		inbox:   make(chan requestEnvelope, b.bufferSize),
	}
	b.handlers[module][topic] = entry

	go func(fn Handler) {
		for req := range entry.inbox {
			go func(r requestEnvelope) {
				// A handler panic must never crash the process. Recover and
				// surface it as an error on the reply channel.
				defer func() {
					if rec := recover(); rec != nil {
						r.replyCh <- replyEnvelope{err: fmt.Errorf("ipc: handler panicked: %v", rec)}
					}
				}()
				resp, err := fn(r.msg)
				if err != nil {
					r.replyCh <- replyEnvelope{err: err}
				} else {
					resp.ID = r.msg.ID
					resp.Source = r.msg.Target
					resp.Target = r.msg.Source
					resp.TraceID = r.msg.TraceID // propagate trace ID
					r.replyCh <- replyEnvelope{msg: resp}
				}
			}(req)
		}
	}(entry.handler)
}

func (b *Bus) Request(ctx context.Context, msg Message) (Message, error) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return Message{}, ErrBusClosed
	}

	topics, ok := b.handlers[msg.Target]
	if !ok {
		b.mu.RUnlock()
		return Message{}, ErrNoHandler
	}

	entry, ok := topics[msg.Topic]
	if !ok {
		b.mu.RUnlock()
		return Message{}, ErrNoHandler
	}

	if msg.ID == "" {
		msg.ID = generateID()
	}
	if msg.TraceID == "" {
		msg.TraceID = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if deadline, ok := ctx.Deadline(); ok {
		msg.Deadline = deadline
	}

	replyCh := make(chan replyEnvelope, 1)
	env := requestEnvelope{msg: msg, replyCh: replyCh}

	// Send while still holding the read lock so Close (which takes the write
	// lock before closing inboxes) cannot close entry.inbox underneath us. The
	// recover is belt-and-suspenders against any future regression.
	sendErr := func() (err error) {
		defer func() {
			if recover() != nil {
				err = ErrBusClosed
			}
		}()
		select {
		case entry.inbox <- env:
			return nil
		default:
			return ErrModuleOverloaded
		}
	}()
	b.mu.RUnlock()
	if sendErr != nil {
		return Message{}, sendErr
	}

	select {
	case reply := <-replyCh:
		return reply.msg, reply.err
	case <-ctx.Done():
		return Message{}, ErrTimeout
	}
}

func (b *Bus) Subscribe(module, topic string, h Handler) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Reject subscriptions on a closed bus. Return a non-nil handle with a
		// no-op cancel so callers that store it and later call Cancel are safe.
		return &Subscription{Topic: topic, Module: module, cancelFn: func() {}}
	}

	id := generateID()
	b.subscribers[topic] = append(b.subscribers[topic], subscriberEntry{
		module: module, handler: h, id: id,
	})

	return &Subscription{
		Topic: topic, Module: module,
		cancelFn: func() { b.unsubscribe(topic, id) },
	}
}

func (b *Bus) Publish(msg Message) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	subs := make([]subscriberEntry, len(b.subscribers[msg.Topic]))
	copy(subs, b.subscribers[msg.Topic])
	b.mu.RUnlock()

	if msg.ID == "" {
		msg.ID = generateID()
	}

	for _, sub := range subs {
		sub := sub
		// Each subscriber runs in its own goroutine so a slow or panicking
		// consumer can never block the publisher or crash the process. Events
		// are fire-and-forget: a handler panic is contained here and its
		// return value/error is intentionally discarded.
		go func() {
			defer func() { _ = recover() }()
			sub.handler(msg)
		}()
	}
	return nil
}

func (b *Bus) unsubscribe(topic, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[topic]
	for i, sub := range subs {
		if sub.id == id {
			b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, topics := range b.handlers {
		for _, entry := range topics {
			close(entry.inbox)
		}
	}
}

func generateID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand should never fail; fall back to a time-based value so we
		// never return an all-zero (collision-prone) ID.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
