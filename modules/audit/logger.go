package audit

import (
	"context"
	"fmt"
	"os"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

type Logger struct {
	dbPath   string
	instance string
	secret   string
	writer   *Writer
	bus      *ipc.Bus
	sub      *ipc.Subscription
}

func NewLogger(dbPath, instance, secret string) *Logger {
	return &Logger{dbPath: dbPath, instance: instance, secret: secret}
}

func (l *Logger) Name() string           { return "audit" }
func (l *Logger) Dependencies() []string { return nil }

func (l *Logger) Init(ctx context.Context, svc *kernel.Services) error {
	l.bus = svc.Bus
	w, err := NewWriter(l.dbPath, l.instance, l.secret)
	if err != nil {
		return fmt.Errorf("audit logger init: %w", err)
	}
	l.writer = w
	return nil
}

func (l *Logger) Start(ctx context.Context) error {
	l.sub = l.bus.Subscribe("audit", "audit.write", l.handleWrite)
	l.bus.Handle("audit", "audit.query", l.handleQuery)
	return nil
}

func (l *Logger) Stop(ctx context.Context) error {
	if l.sub != nil {
		l.sub.Cancel()
	}
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

func (l *Logger) Health(ctx context.Context) kernel.HealthStatus {
	if l.writer == nil {
		return kernel.HealthStatus{Healthy: false, Message: "writer not initialized"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "audit logger running"}
}

func (l *Logger) handleWrite(msg ipc.Message) (ipc.Message, error) {
	entry, ok := msg.Payload.(Entry)
	if !ok {
		fmt.Fprintf(os.Stderr, "audit: expected Entry, got %T\n", msg.Payload)
		return ipc.Message{}, nil
	}
	if err := l.writer.Write(entry); err != nil {
		fmt.Fprintf(os.Stderr, "audit: write failed: %v\n", err)
		return ipc.Message{}, nil
	}
	return ipc.Message{}, nil
}

func (l *Logger) handleQuery(msg ipc.Message) (ipc.Message, error) {
	filter, ok := msg.Payload.(QueryFilter)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected QueryFilter, got %T", msg.Payload)
	}
	entries, err := QueryEntries(l.writer.db, filter)
	if err != nil {
		return ipc.Message{}, fmt.Errorf("query audit: %w", err)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: entries}, nil
}
