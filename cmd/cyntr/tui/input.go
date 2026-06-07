package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// textInput is a minimal multiline input buffer. The bubbles/textarea and
// /textinput components both pull in github.com/atotto/clipboard, which is not
// present in this module's go.sum (and we are not allowed to add new deps), so
// we provide a small self-contained editor that depends only on lipgloss.
//
// It supports the editing operations the TUI needs: rune insertion, backspace,
// newline insertion (Shift+Enter), and cursor movement left/right. The cursor
// is a byte offset into value, kept on rune boundaries by the edit ops.
type textInput struct {
	value       []rune
	cursor      int // rune index into value
	width       int
	height      int
	placeholder string
	prompt      string
}

func newTextInput() textInput {
	return textInput{
		prompt:      "┃ ",
		height:      3,
		width:       80,
		placeholder: "Type a message (Enter to send, Shift+Enter for newline). /help for commands.",
	}
}

func (t *textInput) Value() string { return string(t.value) }

func (t *textInput) Reset() {
	t.value = t.value[:0]
	t.cursor = 0
}

func (t *textInput) SetWidth(w int) {
	if w > 0 {
		t.width = w
	}
}

func (t *textInput) SetHeight(h int) {
	if h > 0 {
		t.height = h
	}
}

// InsertRune inserts r at the cursor.
func (t *textInput) InsertRune(r rune) {
	t.value = append(t.value, 0)
	copy(t.value[t.cursor+1:], t.value[t.cursor:])
	t.value[t.cursor] = r
	t.cursor++
}

// InsertString inserts each rune of s at the cursor.
func (t *textInput) InsertString(s string) {
	for _, r := range s {
		t.InsertRune(r)
	}
}

// Backspace deletes the rune before the cursor.
func (t *textInput) Backspace() {
	if t.cursor == 0 {
		return
	}
	t.value = append(t.value[:t.cursor-1], t.value[t.cursor:]...)
	t.cursor--
}

// Delete removes the rune at the cursor.
func (t *textInput) Delete() {
	if t.cursor >= len(t.value) {
		return
	}
	t.value = append(t.value[:t.cursor], t.value[t.cursor+1:]...)
}

func (t *textInput) Left() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *textInput) Right() {
	if t.cursor < len(t.value) {
		t.cursor++
	}
}

func (t *textInput) Home() { t.cursor = 0 }
func (t *textInput) End()  { t.cursor = len(t.value) }

// View renders the input box with the prompt and a visible cursor.
func (t *textInput) View() string {
	content := string(t.value)
	if content == "" {
		return inputBoxStyle.Width(t.width - 2).Render(t.prompt + hintStyle.Render(t.placeholder))
	}
	// Render with a block cursor at the cursor position.
	var b strings.Builder
	for i, r := range t.value {
		if i == t.cursor {
			b.WriteString(cursorStyle.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	if t.cursor >= len(t.value) {
		b.WriteString(cursorStyle.Render(" "))
	}
	// Prefix each line with the prompt for the first line.
	rendered := t.prompt + b.String()
	return inputBoxStyle.Width(t.width - 2).Render(rendered)
}

var (
	inputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	cursorStyle   = lipgloss.NewStyle().Reverse(true)
)
