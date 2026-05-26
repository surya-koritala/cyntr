[Cyntr Docs](../README.md) > How-to > Add a channel

# Add a channel

Two paths: configure one of the 9 built-in adapters, or write a Go module for a new platform.

## Configure a built-in adapter

Each adapter is enabled by setting its env vars. The most common is Slack:

```bash
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_SIGNING_SECRET=...
export SLACK_ROUTES="C0123=cloud-ops,C0456=assistant"   # channel → agent map
export SLACK_USE_THREADS=true
export SLACK_APPROVAL_CHANNEL=C0789                     # where approval prompts go
./cyntr start
```

| Adapter | Env vars |
|---------|----------|
| Slack | `SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`, `SLACK_ROUTES`, `SLACK_USE_THREADS`, `SLACK_APPROVAL_CHANNEL` |
| Microsoft Teams | `TEAMS_APP_ID`, `TEAMS_APP_PASSWORD` |
| Discord | `DISCORD_BOT_TOKEN` |
| Telegram | `TELEGRAM_BOT_TOKEN` |
| WhatsApp | `WHATSAPP_ACCESS_TOKEN`, `WHATSAPP_PHONE_NUMBER_ID` |
| Email | `EMAIL_SMTP_HOST`, `EMAIL_SMTP_PORT`, `EMAIL_USERNAME`, `EMAIL_PASSWORD` |
| Google Chat | `GOOGLE_CHAT_WEBHOOK_URL` |
| Webhook | `WEBHOOK_INBOUND_SECRET` |
| Signal | `SIGNAL_PHONE_NUMBER`, `SIGNAL_API_URL` |

Full list of env vars: [reference/env-vars.md](../reference/env-vars.md#channels).

## Write a new adapter

Each adapter is a Go module under `modules/channel/<name>/`. The contract:

```go
type Adapter interface {
    Name() string
    Start(ctx context.Context, bus *ipc.Bus) error
    Stop(ctx context.Context) error
}
```

On inbound message, publish an `incoming_message` event on the bus. The agent runtime picks it up, runs the chat turn, and emits an `outgoing_message` event that your adapter subscribes to.

Use `modules/channel/discord/` as the smallest end-to-end reference.

## Related

- [Concepts: Architecture](../concepts/architecture.md) — module lifecycle.
- [Reference: Env vars](../reference/env-vars.md#channels)
