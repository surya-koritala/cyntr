package ipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
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

	entry := handlerEntry{
		handler: h,
		inbox:   make(chan requestEnvelope, b.bufferSize),
	}
	b.handlers[module][topic] = entry

	go func() {
		for req := range entry.inbox {
			resp, err := entry.handler(req.msg)
			if err != nil {
				req.replyCh <- replyEnvelope{err: err}
			} else {
				resp.ID = req.msg.ID
				resp.Source = req.msg.Target
				resp.Target = req.msg.Source
				req.replyCh <- replyEnvelope{msg: resp}
			}
		}
	}()
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
	b.mu.RUnlock()

	if msg.ID == "" {
		msg.ID = generateID()
	}
	if deadline, ok := ctx.Deadline(); ok {
		msg.Deadline = deadline
	}

	replyCh := make(chan replyEnvelope, 1)
	env := requestEnvelope{msg: msg, replyCh: replyCh}

	select {
	case entry.inbox <- env:
	default:
		return Message{}, ErrModuleOverloaded
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
		go func() { sub.handler(msg) }()
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
	rand.Read(buf)
	return hex.EncodeToString(buf)
}
