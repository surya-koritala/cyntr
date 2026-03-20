package agent

import "testing"

func TestMessageRoleString(t *testing.T) {
	tests := []struct {
		r    Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleSystem, "system"},
		{RoleTool, "tool"},
		{Role(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("Role(%d).String() = %q, want %q", int(tt.r), got, tt.want)
		}
	}
}

func TestAgentConfigDefaults(t *testing.T) {
	cfg := AgentConfig{
		Name:   "test-agent",
		Tenant: "finance",
		Model:  "claude",
	}
	if cfg.Name != "test-agent" {
		t.Fatalf("expected test-agent, got %q", cfg.Name)
	}
}

func TestAgentConfigSecrets(t *testing.T) {
	cfg := AgentConfig{
		Name: "bot", Tenant: "t", Model: "mock",
		Secrets: map[string]string{"GITHUB_TOKEN": "ghp_test", "JIRA_TOKEN": "jira_test"},
	}
	if len(cfg.Secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(cfg.Secrets))
	}
	if cfg.Secrets["GITHUB_TOKEN"] != "ghp_test" {
		t.Fatal("wrong secret")
	}
}
