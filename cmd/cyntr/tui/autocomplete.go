package tui

import (
	"sort"
	"strings"
)

// CommandKind classifies the origin of a slash command.
type CommandKind string

const (
	KindBuiltin CommandKind = "builtin"
	KindSkill   CommandKind = "skill"
	KindTool    CommandKind = "tool"
)

// Command is a single slash-command autocomplete entry.
type Command struct {
	Name        string // includes the leading slash, e.g. "/help"
	Description string
	Kind        CommandKind
}

// builtinCommands is the static fallback list, always available even when the
// gateway is unreachable. Mirrors the in-session commands of cmd/cyntr/chat.go.
var builtinCommands = []Command{
	{Name: "/help", Description: "Show available commands", Kind: KindBuiltin},
	{Name: "/clear", Description: "Clear the conversation history", Kind: KindBuiltin},
	{Name: "/quit", Description: "Exit the TUI", Kind: KindBuiltin},
	{Name: "/skills", Description: "List available skills", Kind: KindBuiltin},
	{Name: "/tools", Description: "List available tools", Kind: KindBuiltin},
}

// Autocomplete returns the commands whose name matches the given prefix,
// sorted deterministically (by name). A prefix that does not start with "/"
// yields no results — slash commands only trigger on a leading slash. An empty
// or bare "/" prefix returns every command.
//
// Matching is case-insensitive and prefix-based.
func Autocomplete(commands []Command, prefix string) []Command {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		return nil
	}
	lp := strings.ToLower(prefix)

	var out []Command
	for _, c := range commands {
		if lp == "" || lp == "/" || strings.HasPrefix(strings.ToLower(c.Name), lp) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
