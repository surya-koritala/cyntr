package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// turnState is the lifecycle of a single chat turn.
type turnState int

const (
	stateIdle      turnState = iota // waiting for user input
	stateStreaming                  // an assistant turn is in flight
)

// streamMsg carries a single decoded stream event into the bubbletea loop.
type streamMsg struct {
	seq   int // generation guard: ignore events from a cancelled turn
	event StreamEvent
}

// streamErrMsg signals the in-flight stream failed.
type streamErrMsg struct {
	seq int
	err error
}

// commandsMsg delivers the autocomplete command set fetched from the registry.
type commandsMsg struct {
	commands []Command
}

// streamStarter abstracts how a turn is launched so tests can inject a fake.
// It returns a tea.Cmd that runs the stream and a cancel func wired to ctx.
type streamStarter func(seq int, message string) (tea.Cmd, context.CancelFunc)

// commandFetcher abstracts loading the autocomplete command set.
type commandFetcher func() tea.Cmd

// Model is the root bubbletea model for `cyntr tui`. It is a pure client:
// state transitions live entirely here and are exercised by table-driven
// tests; all I/O is injected via startStream / fetchCommands.
type Model struct {
	tenant string
	agent  string

	input    textInput
	viewport viewport.Model
	ready    bool
	width    int
	height   int

	// scrollback holds the rendered transcript lines.
	transcript []string
	// streaming is the assistant's in-progress message for the current turn.
	streaming strings.Builder

	state turnState
	// seq increments every time a turn starts or is interrupted; stale
	// streamMsg/streamErrMsg with an older seq are dropped. This is the core
	// of Ctrl-C interrupt-and-redirect.
	seq    int
	cancel context.CancelFunc

	commands []Command
	// suggestions is the current autocomplete list for the input prefix.
	suggestions []Command

	status string
	err    error

	startStream   streamStarter
	fetchCommands commandFetcher
}

// NewModel builds a Model. startStream and fetchCommands are injected so the
// Update/View logic is testable without a live gateway. Either may be nil in
// tests that don't exercise streaming.
func NewModel(tenant, agent string, startStream streamStarter, fetchCommands commandFetcher) Model {
	ta := newTextInput()
	vp := viewport.New(80, 20)

	return Model{
		tenant:        tenant,
		agent:         agent,
		input:         ta,
		viewport:      vp,
		state:         stateIdle,
		startStream:   startStream,
		fetchCommands: fetchCommands,
		status:        fmt.Sprintf("%s/%s — ready", tenant, agent),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.fetchCommands != nil {
		return m.fetchCommands()
	}
	return nil
}

// Update implements tea.Model. It is the single source of truth for state
// transitions and is covered by table-driven tests.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.ready = true
		m.refreshViewport()
		return m, nil

	case commandsMsg:
		m.commands = msg.commands
		m.suggestions = Autocomplete(m.commands, m.input.Value())
		return m, nil

	case streamMsg:
		if msg.seq != m.seq || m.state != stateStreaming {
			return m, nil // stale event from an interrupted turn
		}
		return m.applyStreamEvent(msg.event)

	case streamErrMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.finishTurn()
		m.err = msg.err
		m.transcript = append(m.transcript, errStyle.Render("error: "+msg.err.Error()))
		m.refreshViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward other messages to the viewport (the custom input only reacts to
	// key messages, handled above).
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// handleKey processes key input: Ctrl-C interrupt-and-redirect, Enter to send,
// scrollback navigation, and autocomplete refresh.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.state == stateStreaming {
			// Interrupt-and-redirect: cancel the in-flight turn, bump the
			// generation guard so any buffered events are ignored, and return
			// to idle so the user can immediately type a new message.
			m.interrupt()
			m.transcript = append(m.transcript, hintStyle.Render("[interrupted — type a new message]"))
			m.refreshViewport()
			return m, nil
		}
		// Idle: a second Ctrl-C exits.
		return m, tea.Quit

	case "ctrl+d":
		return m, tea.Quit

	case "enter":
		return m.submit()

	case "shift+enter", "alt+enter":
		// Insert a newline into the multiline input instead of submitting.
		m.input.InsertRune('\n')
		return m, nil

	case "backspace":
		m.input.Backspace()
		m.suggestions = Autocomplete(m.commands, m.input.Value())
		return m, nil

	case "delete":
		m.input.Delete()
		m.suggestions = Autocomplete(m.commands, m.input.Value())
		return m, nil

	case "left":
		m.input.Left()
		return m, nil
	case "right":
		m.input.Right()
		return m, nil
	case "home", "ctrl+a":
		m.input.Home()
		return m, nil
	case "end", "ctrl+e":
		m.input.End()
		return m, nil

	case "tab":
		// Accept the top autocomplete suggestion.
		if len(m.suggestions) > 0 {
			m.input.Reset()
			m.input.InsertString(m.suggestions[0].Name + " ")
			m.suggestions = Autocomplete(m.commands, m.input.Value())
		}
		return m, nil

	case "pgup", "pgdown", "ctrl+u", "ctrl+b", "ctrl+f", "up", "down":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case "space":
		m.input.InsertRune(' ')
		m.suggestions = Autocomplete(m.commands, m.input.Value())
		return m, nil
	}

	// Default: printable runes go into the input, then refresh autocomplete.
	if len(msg.Runes) > 0 {
		for _, r := range msg.Runes {
			m.input.InsertRune(r)
		}
		m.suggestions = Autocomplete(m.commands, m.input.Value())
	}
	return m, nil
}

// submit handles Enter: routes built-in slash commands locally, otherwise
// starts a streaming turn. No-op while a turn is already streaming.
func (m Model) submit() (tea.Model, tea.Cmd) {
	if m.state == stateStreaming {
		return m, nil // ignore Enter mid-stream; Ctrl-C interrupts instead
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}

	// Local slash commands that never hit the network.
	if strings.HasPrefix(text, "/") {
		if done, model, cmd := m.handleLocalCommand(text); done {
			return model, cmd
		}
	}

	m.input.Reset()
	m.suggestions = nil
	m.err = nil
	m.transcript = append(m.transcript, youStyle.Render("you: ")+text)
	m.streaming.Reset()

	if m.startStream == nil {
		// No transport wired (e.g. in tests without streaming) — echo only.
		m.refreshViewport()
		return m, nil
	}

	m.seq++
	m.state = stateStreaming
	m.status = fmt.Sprintf("%s/%s — streaming…", m.tenant, m.agent)
	cmd, cancel := m.startStream(m.seq, text)
	m.cancel = cancel
	m.refreshViewport()
	return m, cmd
}

// handleLocalCommand processes client-side slash commands. Returns done=true
// when the command was fully handled locally (so it should not be sent to the
// gateway).
func (m Model) handleLocalCommand(text string) (bool, tea.Model, tea.Cmd) {
	switch strings.Fields(text)[0] {
	case "/quit", "/exit", "/q":
		return true, m, tea.Quit
	case "/clear", "/reset":
		m.transcript = nil
		m.input.Reset()
		m.suggestions = nil
		m.status = fmt.Sprintf("%s/%s — cleared", m.tenant, m.agent)
		m.refreshViewport()
		return true, m, nil
	case "/help":
		m.input.Reset()
		m.suggestions = nil
		var b strings.Builder
		b.WriteString(hintStyle.Render("commands:") + "\n")
		for _, c := range m.commands {
			b.WriteString(fmt.Sprintf("  %-18s %s\n", c.Name, c.Description))
		}
		m.transcript = append(m.transcript, strings.TrimRight(b.String(), "\n"))
		m.refreshViewport()
		return true, m, nil
	case "/skills":
		m.input.Reset()
		m.suggestions = nil
		m.transcript = append(m.transcript, hintStyle.Render(listKind(m.commands, KindSkill, "skills")))
		m.refreshViewport()
		return true, m, nil
	case "/tools":
		m.input.Reset()
		m.suggestions = nil
		m.transcript = append(m.transcript, hintStyle.Render(listKind(m.commands, KindTool, "tools")))
		m.refreshViewport()
		return true, m, nil
	}
	return false, m, nil
}

// applyStreamEvent folds a single stream event into model state and renders
// incrementally. Returns to idle on a "done" or terminal "error" event.
func (m Model) applyStreamEvent(ev StreamEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case "thinking":
		m.status = fmt.Sprintf("%s/%s — thinking…", m.tenant, m.agent)
	case "progress":
		detail := ev.Event
		if ev.Detail != "" {
			detail = ev.Event + ": " + ev.Detail
		}
		m.status = fmt.Sprintf("%s/%s — %s", m.tenant, m.agent, detail)
	case "text":
		m.streaming.WriteString(ev.Content)
	case "tools_used":
		// purely informational; ignore for the transcript
	case "error":
		m.finishTurn()
		errText := ev.Content
		if errText == "" {
			errText = ev.Detail
		}
		m.err = fmt.Errorf("%s", errText)
		m.transcript = append(m.transcript, errStyle.Render("error: "+errText))
		m.refreshViewport()
		return m, nil
	case "done":
		final := m.streaming.String()
		if strings.TrimSpace(final) != "" {
			m.transcript = append(m.transcript, agentStyle.Render("agent: ")+final)
		}
		m.finishTurn()
		m.refreshViewport()
		return m, nil
	}
	m.refreshViewport()
	return m, nil
}

// interrupt cancels the in-flight turn and bumps the generation guard.
func (m *Model) interrupt() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	// Preserve any partial text already streamed.
	if partial := m.streaming.String(); strings.TrimSpace(partial) != "" {
		m.transcript = append(m.transcript, agentStyle.Render("agent: ")+partial+hintStyle.Render(" …"))
	}
	m.streaming.Reset()
	m.seq++ // any in-flight events now carry a stale seq and are dropped
	m.state = stateIdle
	m.status = fmt.Sprintf("%s/%s — ready", m.tenant, m.agent)
}

// finishTurn returns to idle after a turn completes normally.
func (m *Model) finishTurn() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.streaming.Reset()
	m.state = stateIdle
	m.status = fmt.Sprintf("%s/%s — ready", m.tenant, m.agent)
}

// layout sizes the sub-components to the current terminal dimensions.
func (m *Model) layout() {
	inputHeight := 3
	statusHeight := 1
	suggestHeight := 0
	vpHeight := m.height - inputHeight - statusHeight - suggestHeight - 2
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.input.SetWidth(m.width)
	m.input.SetHeight(inputHeight)
}

// refreshViewport rebuilds the viewport content from the transcript plus the
// in-progress streaming buffer (incremental render).
func (m *Model) refreshViewport() {
	lines := append([]string(nil), m.transcript...)
	if m.state == stateStreaming {
		partial := m.streaming.String()
		if partial != "" {
			lines = append(lines, agentStyle.Render("agent: ")+partial)
		}
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if len(m.suggestions) > 0 {
		b.WriteString(m.renderSuggestions())
		b.WriteString("\n")
	}
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(statusStyle.Render(m.status))
	return b.String()
}

// renderSuggestions renders the autocomplete dropdown (capped to a few rows).
func (m Model) renderSuggestions() string {
	const max = 6
	var parts []string
	for i, c := range m.suggestions {
		if i >= max {
			parts = append(parts, hintStyle.Render(fmt.Sprintf("…+%d more", len(m.suggestions)-max)))
			break
		}
		parts = append(parts, suggestStyle.Render(c.Name))
	}
	return strings.Join(parts, "  ")
}

func listKind(cmds []Command, kind CommandKind, label string) string {
	var names []string
	for _, c := range cmds {
		if c.Kind == kind {
			names = append(names, strings.TrimPrefix(strings.TrimPrefix(c.Name, "/skill:"), "/tool:"))
		}
	}
	if len(names) == 0 {
		return "no " + label + " available"
	}
	return label + ": " + strings.Join(names, ", ")
}

// Styles — lipgloss only.
var (
	youStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	agentStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	hintStyle    = lipgloss.NewStyle().Faint(true)
	statusStyle  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("8"))
	suggestStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
)
