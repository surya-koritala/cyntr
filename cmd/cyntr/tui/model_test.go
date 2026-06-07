package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel builds a Model wired with a no-op stream starter that records
// the message it was asked to send. The returned *string captures the last
// message and the *bool reports whether the injected cancel was invoked.
func newTestModel() (Model, *string, *bool) {
	var lastMsg string
	var cancelled bool
	starter := func(seq int, message string) (tea.Cmd, context.CancelFunc) {
		lastMsg = message
		cancel := func() { cancelled = true }
		// A no-op command; the test drives stream events manually.
		return func() tea.Msg { return nil }, cancel
	}
	m := NewModel("acme", "assistant", starter, nil)
	m.commands = builtinCommands
	// Mark ready with a window size so View/refresh don't short-circuit.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return mm.(Model), &lastMsg, &cancelled
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeString(m Model, s string) Model {
	for _, r := range s {
		mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	return m
}

func TestUpdateStateTransitions(t *testing.T) {
	tests := []struct {
		name      string
		drive     func(t *testing.T, m Model) Model
		wantState turnState
		check     func(t *testing.T, m Model)
	}{
		{
			name: "typing stays idle and updates input",
			drive: func(t *testing.T, m Model) Model {
				return typeString(m, "hello")
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				if m.input.Value() != "hello" {
					t.Errorf("input = %q, want hello", m.input.Value())
				}
			},
		},
		{
			name: "enter on non-empty input starts streaming",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "do a thing")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return mm.(Model)
			},
			wantState: stateStreaming,
			check: func(t *testing.T, m Model) {
				if m.input.Value() != "" {
					t.Errorf("input should be cleared after send, got %q", m.input.Value())
				}
			},
		},
		{
			name: "enter on empty input stays idle",
			drive: func(t *testing.T, m Model) Model {
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return mm.(Model)
			},
			wantState: stateIdle,
		},
		{
			name: "done event returns to idle and appends transcript",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "hi")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = mm.(Model)
				mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "text", Content: "world"}})
				m = mm.(Model)
				mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "done"}})
				return mm.(Model)
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				joined := strings.Join(m.transcript, "\n")
				if !strings.Contains(joined, "world") {
					t.Errorf("transcript missing streamed text: %q", joined)
				}
			},
		},
		{
			name: "ctrl+c during stream interrupts and returns to idle",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "hi")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = mm.(Model)
				if m.state != stateStreaming {
					t.Fatalf("precondition: expected streaming, got %v", m.state)
				}
				mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
				return mm.(Model)
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				joined := strings.Join(m.transcript, "\n")
				if !strings.Contains(joined, "interrupted") {
					t.Errorf("expected interrupted marker in transcript: %q", joined)
				}
			},
		},
		{
			name: "stale stream event after interrupt is dropped",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "hi")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = mm.(Model)
				staleSeq := m.seq
				mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
				m = mm.(Model)
				// Event from the cancelled turn must be ignored.
				mm, _ = m.Update(streamMsg{seq: staleSeq, event: StreamEvent{Type: "text", Content: "ghost"}})
				return mm.(Model)
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				if strings.Contains(m.streaming.String(), "ghost") {
					t.Errorf("stale event was applied: %q", m.streaming.String())
				}
			},
		},
		{
			name: "clear command empties transcript",
			drive: func(t *testing.T, m Model) Model {
				m.transcript = []string{"old line"}
				m = typeString(m, "/clear")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return mm.(Model)
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				if len(m.transcript) != 0 {
					t.Errorf("transcript not cleared: %v", m.transcript)
				}
			},
		},
		{
			name: "error event returns to idle",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "hi")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = mm.(Model)
				mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "error", Content: "boom"}})
				return mm.(Model)
			},
			wantState: stateIdle,
			check: func(t *testing.T, m Model) {
				if m.err == nil {
					t.Error("expected error to be set")
				}
			},
		},
		{
			name: "enter mid-stream is ignored",
			drive: func(t *testing.T, m Model) Model {
				m = typeString(m, "hi")
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				m = mm.(Model)
				// Now streaming; type and press enter again — should be ignored.
				m = typeString(m, "second")
				mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return mm.(Model)
			},
			wantState: stateStreaming,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, _ := newTestModel()
			m = tt.drive(t, m)
			if m.state != tt.wantState {
				t.Errorf("state = %v, want %v", m.state, tt.wantState)
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestInterruptCancelsContext(t *testing.T) {
	m, _, cancelled := newTestModel()
	m = typeString(m, "go")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = mm
	if !*cancelled {
		t.Error("expected the in-flight turn's cancel func to be invoked on Ctrl-C")
	}
}

func TestCtrlCIdleQuits(t *testing.T) {
	m, _, _ := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected a quit command on Ctrl-C while idle")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestTabAcceptsSuggestion(t *testing.T) {
	m, _, _ := newTestModel()
	m = typeString(m, "/he")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(Model)
	if !strings.HasPrefix(m.input.Value(), "/help") {
		t.Errorf("tab did not complete to /help, got %q", m.input.Value())
	}
}

func TestStreamingIncrementalRender(t *testing.T) {
	// Streaming text should appear incrementally in the rendered view.
	m, _, _ := newTestModel()
	m = typeString(m, "hi")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)

	chunks := []string{"Hel", "lo, ", "world"}
	var lastView string
	for i, c := range chunks {
		mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "text", Content: c}})
		m = mm.(Model)
		view := m.View()
		want := strings.Join(chunks[:i+1], "")
		if !strings.Contains(view, want) {
			t.Fatalf("after %d chunks, view missing %q\nview:\n%s", i+1, want, view)
		}
		lastView = view
	}
	if !strings.Contains(lastView, "Hello, world") {
		t.Errorf("final view missing full text:\n%s", lastView)
	}
}
