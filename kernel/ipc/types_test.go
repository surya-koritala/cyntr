package ipc

import "testing"

func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		mt   MessageType
		want string
	}{
		{MessageTypeRequest, "request"},
		{MessageTypeResponse, "response"},
		{MessageTypeEvent, "event"},
		{MessageType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		got := tt.mt.String()
		if got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", int(tt.mt), got, tt.want)
		}
	}
}
