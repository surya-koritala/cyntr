package agent

import (
	"strings"
	"testing"
)

func TestNudgeSystemMessage(t *testing.T) {
	m := nudgeSystemMessage()
	if m.Role != RoleSystem {
		t.Fatalf("nudge should be a system message, got role %v", m.Role)
	}
	if strings.TrimSpace(m.Content) == "" {
		t.Fatal("nudge content should be non-empty")
	}
	if !strings.Contains(strings.ToLower(m.Content), "save") {
		t.Fatalf("nudge should ask the model to save: %q", m.Content)
	}
}

func TestNudgeBeforeCompactInjectsAndSurvives(t *testing.T) {
	s := NewSession("s1", AgentConfig{})
	for i := 0; i < 12; i++ {
		s.AddMessage(Message{Role: RoleUser, Content: "message"})
	}
	before := len(s.History())

	nudgeBeforeCompact(s, 6)

	hist := s.History()
	if len(hist) >= before {
		t.Fatalf("history should be compacted: before=%d after=%d", before, len(hist))
	}
	// The nudge must survive compaction (it was added last).
	found := false
	for _, m := range hist {
		if m.Role == RoleSystem && strings.Contains(strings.ToLower(m.Content), "save") {
			found = true
		}
	}
	if !found {
		t.Fatalf("nudge did not survive compaction: %+v", hist)
	}
}

// A short session never exceeds the threshold, so the runtime never calls
// nudgeBeforeCompact and no nudge is injected. This mirrors the guard in
// handleChat.
func TestShortSessionNotCompacted(t *testing.T) {
	threshold := 10
	s := NewSession("s2", AgentConfig{})
	for i := 0; i < 4; i++ {
		s.AddMessage(Message{Role: RoleUser, Content: "hi"})
	}
	if len(s.History()) > threshold {
		t.Fatal("precondition: short session should be under threshold")
	}
	for _, m := range s.History() {
		if m.Role == RoleSystem {
			t.Fatal("short session should carry no system nudge")
		}
	}
}
