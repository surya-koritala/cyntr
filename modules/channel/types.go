package channel

import "context"

// ChannelAdapter is the interface for messaging platform integrations.
// Each platform (Slack, Teams, webhook, etc.) implements this.
type ChannelAdapter interface {
	// Name returns the adapter name (e.g., "slack", "teams", "webhook").
	Name() string

	// Start begins listening for inbound messages.
	// The handler is called for each inbound message.
	Start(ctx context.Context, handler InboundHandler) error

	// Stop shuts down the adapter.
	Stop(ctx context.Context) error

	// Send sends a message through this channel.
	Send(ctx context.Context, msg OutboundMessage) error
}

// InboundHandler is called when a message arrives from a channel.
type InboundHandler func(msg InboundMessage) (string, error)

// InboundMessage represents a message received from a messaging platform.
type InboundMessage struct {
	Channel   string // adapter name: "slack", "teams", "webhook"
	ChannelID string // platform-specific channel/room ID
	UserID    string // platform-specific user ID
	Text      string // message content
	Tenant    string // resolved tenant
	Agent     string // target agent name
}

// OutboundMessage represents a message to send through a channel.
type OutboundMessage struct {
	Channel   string // adapter name
	ChannelID string // platform-specific channel/room ID
	Text      string // message content
}
