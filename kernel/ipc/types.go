package ipc

import (
	"errors"
	"fmt"
	"time"
)

type MessageType int

const (
	MessageTypeRequest MessageType = iota
	MessageTypeResponse
	MessageTypeEvent
)

func (t MessageType) String() string {
	switch t {
	case MessageTypeRequest:
		return "request"
	case MessageTypeResponse:
		return "response"
	case MessageTypeEvent:
		return "event"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

type Message struct {
	ID       string
	Source   string
	Target   string
	Type     MessageType
	Topic    string
	Payload  any
	Deadline time.Time
	TraceID  string
}

var (
	ErrModuleOverloaded = errors.New("ipc: target module overloaded")
	ErrModuleNotFound   = errors.New("ipc: target module not found")
	ErrTimeout          = errors.New("ipc: request timed out")
	ErrBusClosed        = errors.New("ipc: bus is closed")
	ErrNoHandler        = errors.New("ipc: no handler registered for topic")
)

type Handler func(msg Message) (Message, error)

type Subscription struct {
	Topic    string
	Module   string
	cancelFn func()
}

func (s *Subscription) Cancel() {
	if s.cancelFn != nil {
		s.cancelFn()
	}
}
