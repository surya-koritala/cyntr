package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR escape sequences so view snapshots are comparable
// regardless of the terminal color profile.
func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestStreamingGoldenView asserts the transcript region of the view contains
// the expected lines after a full turn streams in. We compare the stripped
// transcript lines (not the bordered input/status chrome, which depends on
// terminal width math) for a stable golden.
func TestStreamingGoldenView(t *testing.T) {
	m, _, _ := newTestModel()
	m = typeString(m, "what is 2+2")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)

	for _, c := range []string{"The ", "answer ", "is ", "4."} {
		mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "text", Content: c}})
		m = mm.(Model)
	}
	mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "done"}})
	m = mm.(Model)

	view := stripANSI(m.View())

	// Golden expectations: the user line and the fully assembled agent line.
	wantLines := []string{
		"you: what is 2+2",
		"agent: The answer is 4.",
	}
	for _, line := range wantLines {
		if !strings.Contains(view, line) {
			t.Errorf("view missing golden line %q\n---view---\n%s", line, view)
		}
	}
}

// TestPartialStreamGolden verifies that mid-stream (before done) the partial
// assistant text is rendered incrementally and labeled as the agent line.
func TestPartialStreamGolden(t *testing.T) {
	m, _, _ := newTestModel()
	m = typeString(m, "hi")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)

	mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "text", Content: "par"}})
	m = mm.(Model)
	v1 := stripANSI(m.View())
	if !strings.Contains(v1, "agent: par") {
		t.Errorf("partial view missing 'agent: par':\n%s", v1)
	}

	mm, _ = m.Update(streamMsg{seq: m.seq, event: StreamEvent{Type: "text", Content: "tial"}})
	m = mm.(Model)
	v2 := stripANSI(m.View())
	if !strings.Contains(v2, "agent: partial") {
		t.Errorf("partial view missing 'agent: partial':\n%s", v2)
	}
}
