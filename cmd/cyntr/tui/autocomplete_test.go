package tui

import (
	"reflect"
	"testing"
)

func names(cmds []Command) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}

func TestAutocomplete(t *testing.T) {
	cmds := []Command{
		{Name: "/help", Kind: KindBuiltin},
		{Name: "/clear", Kind: KindBuiltin},
		{Name: "/quit", Kind: KindBuiltin},
		{Name: "/skill:summarize", Kind: KindSkill},
		{Name: "/skill:translate", Kind: KindSkill},
		{Name: "/tool:http", Kind: KindTool},
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{
			name:   "empty returns all sorted",
			prefix: "",
			want:   []string{"/clear", "/help", "/quit", "/skill:summarize", "/skill:translate", "/tool:http"},
		},
		{
			name:   "bare slash returns all sorted",
			prefix: "/",
			want:   []string{"/clear", "/help", "/quit", "/skill:summarize", "/skill:translate", "/tool:http"},
		},
		{
			name:   "prefix h matches help",
			prefix: "/h",
			want:   []string{"/help"},
		},
		{
			name:   "prefix skill matches both skills sorted",
			prefix: "/skill",
			want:   []string{"/skill:summarize", "/skill:translate"},
		},
		{
			name:   "case insensitive",
			prefix: "/SKILL:T",
			want:   []string{"/skill:translate"},
		},
		{
			name:   "non-slash prefix returns nothing",
			prefix: "hello",
			want:   nil,
		},
		{
			name:   "no match returns empty",
			prefix: "/zzz",
			want:   nil,
		},
		{
			name:   "tool prefix",
			prefix: "/tool",
			want:   []string{"/tool:http"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(Autocomplete(cmds, tt.prefix))
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Autocomplete(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestBuiltinCommandsAlwaysPresent(t *testing.T) {
	got := Autocomplete(builtinCommands, "/")
	if len(got) != len(builtinCommands) {
		t.Fatalf("expected %d builtin commands, got %d", len(builtinCommands), len(got))
	}
	want := map[string]bool{"/help": true, "/clear": true, "/quit": true, "/skills": true, "/tools": true}
	for _, c := range got {
		if !want[c.Name] {
			t.Errorf("unexpected builtin command %q", c.Name)
		}
	}
}
