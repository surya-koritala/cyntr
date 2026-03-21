package tools

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

type SendMessageTool struct {
	bus *ipc.Bus
}

func NewSendMessageTool(bus *ipc.Bus) *SendMessageTool {
	return &SendMessageTool{bus: bus}
}

func (t *SendMessageTool) Name() string { return "send_message" }
func (t *SendMessageTool) Description() string {
	return "Send a message to a channel (Slack, Teams, email, etc.). Use this to proactively notify users, deliver reports, or send alerts."
}
func (t *SendMessageTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"channel":    {Type: "string", Description: "Channel adapter name: slack, teams, email, discord, telegram, googlechat", Required: true},
		"channel_id": {Type: "string", Description: "Platform-specific channel/room ID (e.g., Slack channel ID like C1234567890)", Required: true},
		"text":       {Type: "string", Description: "Message text to send", Required: true},
	}
}

func (t *SendMessageTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	ch := input["channel"]
	chID := input["channel_id"]
	text := input["text"]

	if ch == "" || chID == "" || text == "" {
		return "", fmt.Errorf("channel, channel_id, and text are required")
	}

	_, err := t.bus.Request(ctx, ipc.Message{
		Source: "tool_send_message", Target: "channel", Topic: "channel.send",
		Payload: channel.OutboundMessage{
			Channel:   ch,
			ChannelID: chID,
			Text:      text,
		},
	})
	if err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	return fmt.Sprintf("Message sent to %s channel %s", ch, chID), nil
}
