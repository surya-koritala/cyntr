package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// recordingProvider captures the message list passed to Chat so a test can
// verify what was injected into the system context.
type recordingProvider struct {
	mu       sync.Mutex
	captured []Message
	reply    string
}

func (p *recordingProvider) Name() string { return "rec" }

func (p *recordingProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error) {
	p.mu.Lock()
	p.captured = make([]Message, len(messages))
	copy(p.captured, messages)
	p.mu.Unlock()
	return Message{Role: RoleAssistant, Content: p.reply}, nil
}

func (p *recordingProvider) lastMessages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Message, len(p.captured))
	copy(out, p.captured)
	return out
}

// TestUserModelProfileInjectedIntoSessionContext spins up a runtime backed by
// a fake usermodel handler and verifies the profile is prepended to the
// session's system prompt before the model is invoked.
func TestUserModelProfileInjectedIntoSessionContext(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	bus.Handle("usermodel", usermodel.TopicGet, func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: usermodel.UserProfile{
			Tenant:        "acme",
			User:          "alice",
			ProfileMD:     "Alice prefers terse answers.",
			PreferencesMD: "- metric units",
		}}, nil
	})

	rec := &recordingProvider{reply: "ok"}
	rt := NewRuntime()
	rt.RegisterProvider(rec)
	rt.SetToolRegistry(NewToolRegistry())

	ctx := context.Background()
	if err := rt.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if _, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{
			Name: "asst", Tenant: "acme", Model: "rec",
			SystemPrompt: "You are helpful.", MaxTurns: 2,
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "asst", Tenant: "acme", User: "alice", Message: "hi"},
	}); err != nil {
		t.Fatalf("chat: %v", err)
	}

	msgs := rec.lastMessages()
	if len(msgs) == 0 {
		t.Fatal("provider never invoked")
	}
	if msgs[0].Role != RoleSystem {
		t.Fatalf("first message should be system, got %v", msgs[0].Role)
	}
	sys := msgs[0].Content
	if !strings.Contains(sys, "User profile:") {
		t.Fatalf("expected profile header in system prompt, got: %q", sys)
	}
	if !strings.Contains(sys, "Alice prefers terse answers.") {
		t.Fatalf("expected profile body in system prompt, got: %q", sys)
	}
	if !strings.Contains(sys, "User preferences:") {
		t.Fatalf("expected preferences header in system prompt, got: %q", sys)
	}
	if !strings.Contains(sys, "metric units") {
		t.Fatalf("expected preferences body in system prompt, got: %q", sys)
	}
	// The original system prompt should also still be present.
	if !strings.Contains(sys, "You are helpful.") {
		t.Fatalf("original system prompt was lost: %q", sys)
	}
}

// TestUserModelMissingHandlerDoesNotBreakChat verifies that when the
// usermodel module is *not* registered (ipc.ErrNoHandler), chat still
// completes normally — the profile is just absent from the system prompt.
func TestUserModelMissingHandlerDoesNotBreakChat(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	// No usermodel.get handler registered.

	rec := &recordingProvider{reply: "ok"}
	rt := NewRuntime()
	rt.RegisterProvider(rec)
	rt.SetToolRegistry(NewToolRegistry())

	ctx := context.Background()
	if err := rt.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if _, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{
			Name: "asst", Tenant: "acme", Model: "rec",
			SystemPrompt: "You are helpful.", MaxTurns: 2,
		},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "asst", Tenant: "acme", User: "alice", Message: "hi"},
	})
	if err != nil {
		t.Fatalf("chat: %v (ipc.ErrNoHandler from usermodel should be swallowed)", err)
	}
	chatResp, ok := resp.Payload.(ChatResponse)
	if !ok {
		t.Fatalf("expected ChatResponse, got %T", resp.Payload)
	}
	if chatResp.Content != "ok" {
		t.Fatalf("chat did not complete normally: %+v", chatResp)
	}

	// System prompt should NOT mention the user profile.
	msgs := rec.lastMessages()
	if len(msgs) == 0 || msgs[0].Role != RoleSystem {
		t.Fatal("expected system message")
	}
	if strings.Contains(msgs[0].Content, "User profile:") {
		t.Fatalf("user-profile block should be absent when handler missing, got: %q", msgs[0].Content)
	}
}
