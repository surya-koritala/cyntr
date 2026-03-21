package tools

import (
	"testing"
)

func TestSendMessageToolName(t *testing.T) {
	tool := NewSendMessageTool(nil)
	if tool.Name() != "send_message" {
		t.Fatalf("expected send_message, got %q", tool.Name())
	}
}

func TestSendMessageToolParams(t *testing.T) {
	tool := NewSendMessageTool(nil)
	params := tool.Parameters()
	for _, required := range []string{"channel", "channel_id", "text"} {
		p, ok := params[required]
		if !ok {
			t.Fatalf("missing param %q", required)
		}
		if !p.Required {
			t.Fatalf("param %q should be required", required)
		}
	}
}

func TestSendMessageToolMissingParams(t *testing.T) {
	tool := NewSendMessageTool(nil)
	_, err := tool.Execute(nil, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestSendMessageToolDescription(t *testing.T) {
	tool := NewSendMessageTool(nil)
	if tool.Description() == "" {
		t.Fatal("description should not be empty")
	}
}
