package channel

import "testing"

func TestInboundMessageFields(t *testing.T) {
	msg := InboundMessage{
		Channel:   "slack",
		ChannelID: "C1234",
		UserID:    "U5678",
		Text:      "Hello agent",
		Tenant:    "marketing",
		Agent:     "assistant",
	}
	if msg.Channel != "slack" {
		t.Fatalf("expected slack, got %q", msg.Channel)
	}
	if msg.Text != "Hello agent" {
		t.Fatalf("expected message text, got %q", msg.Text)
	}
}

func TestOutboundMessageFields(t *testing.T) {
	msg := OutboundMessage{
		Channel:   "slack",
		ChannelID: "C1234",
		Text:      "Hello user!",
	}
	if msg.Text != "Hello user!" {
		t.Fatalf("expected text, got %q", msg.Text)
	}
}
