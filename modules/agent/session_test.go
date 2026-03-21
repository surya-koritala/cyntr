package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

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

// A2. Sliding Window
func TestSlidingWindow(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t", MaxHistory: 5}
	s := NewSession("test", cfg)

	for i := 0; i < 10; i++ {
		s.AddMessage(Message{Role: RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	history := s.History()
	if len(history) != 5 {
		t.Fatalf("expected 5 messages after sliding window, got %d", len(history))
	}
	// Should have the last 5 messages (msg 5-9)
	if history[0].Content != "msg 5" {
		t.Fatalf("expected oldest to be msg 5, got %q", history[0].Content)
	}
}

func TestSlidingWindowDisabled(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t", MaxHistory: 0}
	s := NewSession("test", cfg)
	for i := 0; i < 20; i++ {
		s.AddMessage(Message{Role: RoleUser, Content: "msg"})
	}
	if len(s.History()) != 20 {
		t.Fatalf("expected all 20 messages when MaxHistory=0, got %d", len(s.History()))
	}
}

// A1. Auto-summarization
func TestCompactHistory(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t"}
	s := NewSession("test", cfg)
	for i := 0; i < 20; i++ {
		s.AddMessage(Message{Role: RoleUser, Content: fmt.Sprintf("msg %d", i)})
	}

	s.CompactHistory(5)
	history := s.History()

	// Should have 1 summary + 5 recent = 6
	if len(history) != 6 {
		t.Fatalf("expected 6 messages after compact, got %d", len(history))
	}
	if history[0].Role != RoleSystem {
		t.Fatal("first message should be system summary")
	}
	if history[len(history)-1].Content != "msg 19" {
		t.Fatalf("last message should be msg 19, got %q", history[len(history)-1].Content)
	}
}

func TestCompactHistoryNoOp(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t"}
	s := NewSession("test", cfg)
	s.AddMessage(Message{Role: RoleUser, Content: "hello"})
	s.CompactHistory(10) // threshold > history length
	if len(s.History()) != 1 {
		t.Fatal("compact should be no-op when history < keepRecent")
	}
}

// A5. System Prompt Templates
func TestExpandTemplateVars(t *testing.T) {
	result := expandTemplateVars("Hello {{user}}, today is {{date}}", "alice", "acme", "bot")
	if result == "" {
		t.Fatal("empty result")
	}
	if !strings.Contains(result, "alice") {
		t.Fatalf("expected user substitution, got %q", result)
	}
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(result, today) {
		t.Fatalf("expected date substitution, got %q", result)
	}
}

func TestExpandAllVars(t *testing.T) {
	result := expandTemplateVars("{{user}} {{tenant}} {{agent}} {{date}} {{time}} {{datetime}}", "u", "t", "a")
	if strings.Contains(result, "{{") {
		t.Fatalf("unexpanded variables remain: %q", result)
	}
}

func TestSetLastUser(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t", SystemPrompt: "Hello {{user}}"}
	s := NewSession("test", cfg)
	s.SetLastUser("alice")

	ctx := s.AssembleContext()
	if len(ctx) == 0 {
		t.Fatal("empty context")
	}
	if !strings.Contains(ctx[0].Content, "alice") {
		t.Fatalf("expected user in system prompt, got %q", ctx[0].Content)
	}
}

func TestSkillInstructionsInContext(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t", SystemPrompt: "You are helpful"}
	s := NewSession("test", cfg)
	s.SetSkillInstructions("## Active Skills\n\n### Skill: test-skill\n\nDo the thing.\n")

	ctx := s.AssembleContext()
	if len(ctx) == 0 {
		t.Fatal("empty context")
	}
	if !strings.Contains(ctx[0].Content, "Active Skills") {
		t.Fatalf("expected skill instructions in context, got %q", ctx[0].Content)
	}
	if !strings.Contains(ctx[0].Content, "Do the thing") {
		t.Fatalf("expected skill content in context, got %q", ctx[0].Content)
	}
}

func TestSkillInstructionsEmpty(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t", SystemPrompt: "You are helpful"}
	s := NewSession("test", cfg)
	// Don't set skill instructions

	ctx := s.AssembleContext()
	if strings.Contains(ctx[0].Content, "Active Skills") {
		t.Fatal("should not contain skills when none set")
	}
}

// ClearHistory
func TestClearHistory(t *testing.T) {
	cfg := AgentConfig{Name: "bot", Tenant: "t"}
	s := NewSession("test", cfg)
	s.AddMessage(Message{Role: RoleUser, Content: "hello"})
	s.AddMessage(Message{Role: RoleAssistant, Content: "hi"})

	s.ClearHistory()
	if len(s.History()) != 0 {
		t.Fatalf("expected empty history after clear, got %d", len(s.History()))
	}
}
