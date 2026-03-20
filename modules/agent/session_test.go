package agent

import "testing"

func TestSessionAddAndGetHistory(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:         "test-agent",
		Tenant:       "finance",
		Model:        "mock",
		SystemPrompt: "You are a helpful assistant.",
		MaxTurns:     10,
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})
	s.AddMessage(Message{Role: RoleAssistant, Content: "Hi there!"})

	history := s.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Role != RoleUser {
		t.Fatalf("expected user, got %s", history[0].Role)
	}
}

func TestSessionContextAssembly(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:         "test-agent",
		Tenant:       "finance",
		Model:        "mock",
		SystemPrompt: "You are a helpful assistant.",
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})

	ctx := s.AssembleContext()
	// System prompt should be first
	if len(ctx) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(ctx))
	}
	if ctx[0].Role != RoleSystem {
		t.Fatalf("expected system first, got %s", ctx[0].Role)
	}
	if ctx[0].Content != "You are a helpful assistant." {
		t.Fatalf("expected system prompt, got %q", ctx[0].Content)
	}
	if ctx[1].Role != RoleUser {
		t.Fatalf("expected user second, got %s", ctx[1].Role)
	}
}

func TestSessionNoSystemPrompt(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:   "test-agent",
		Tenant: "finance",
		Model:  "mock",
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})

	ctx := s.AssembleContext()
	if len(ctx) != 1 {
		t.Fatalf("expected 1 message (no system prompt), got %d", len(ctx))
	}
}

func TestSessionID(t *testing.T) {
	s := NewSession("sess_abc", AgentConfig{Name: "test"})
	if s.ID() != "sess_abc" {
		t.Fatalf("expected sess_abc, got %q", s.ID())
	}
}

func TestSessionConfig(t *testing.T) {
	cfg := AgentConfig{Name: "test", Tenant: "finance", Model: "claude", MaxTurns: 5}
	s := NewSession("sess_001", cfg)
	if s.Config().MaxTurns != 5 {
		t.Fatalf("expected 5, got %d", s.Config().MaxTurns)
	}
}
