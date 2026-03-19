package ipc

// Bus is the in-process message bus for inter-module communication.
// Full implementation in a later task.
type Bus struct{}

func NewBus() *Bus { return &Bus{} }
