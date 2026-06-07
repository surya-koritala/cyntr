// Package tui implements `cyntr tui`, a rich terminal client for the local
// cyntr gateway. It is a pure REST client (bubbletea + bubbles + lipgloss
// only) and never imports the kernel: it talks to the same HTTP API the
// existing `cyntr chat` command uses (CYNTR_API_URL / CYNTR_API_KEY).
package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Run is the entry point invoked from main.go: `cyntr tui <tenant> <agent>`.
// It returns a process exit code.
func Run(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cyntr tui <tenant> <agent>")
		fmt.Fprintln(os.Stderr, "  Rich terminal client. Talks to the local gateway over REST.")
		fmt.Fprintln(os.Stderr, "  Env: CYNTR_API_URL (default http://localhost:7700), CYNTR_API_KEY")
		return 2
	}
	tenant, agent := args[0], args[1]

	client := NewClient()

	// fetchCommands loads the slash-command set from the tool/skill registry.
	fetch := func() tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return commandsMsg{commands: client.FetchCommands(ctx)}
		}
	}

	// p is set after construction; startStream needs it to push events.
	var p *tea.Program

	// startStream launches a streaming turn. It returns a tea.Cmd that runs
	// the stream in the background, pushing each event into the program via
	// p.Send, plus a cancel func wired to the request context so Ctrl-C
	// aborts the in-flight HTTP request immediately.
	startStream := func(seq int, message string) (tea.Cmd, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := func() tea.Msg {
			err := client.Stream(ctx, tenant, agent, message, func(ev StreamEvent) {
				p.Send(streamMsg{seq: seq, event: ev})
			})
			if err != nil && ctx.Err() == nil {
				p.Send(streamErrMsg{seq: seq, err: err})
			}
			return nil
		}
		return cmd, cancel
	}

	model := NewModel(tenant, agent, startStream, fetch)
	p = tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}
	return 0
}
