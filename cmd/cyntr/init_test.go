package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"gopkg.in/yaml.v3"
)

// answers joins scripted wizard answers into a single newline-terminated stdin
// stream. An empty string ("") accepts the prompt's default.
func answers(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func loadYAML(t *testing.T, dir string) config.CyntrConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "cyntr.yaml"))
	if err != nil {
		t.Fatalf("read cyntr.yaml: %v", err)
	}
	var cfg config.CyntrConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("cyntr.yaml does not parse: %v\n%s", err, data)
	}
	return cfg
}

// fullRun is a complete happy-path script: basics, Anthropic provider, safe DM
// default, no channels, assistant agent.
func fullRun(org string) string {
	return answers(
		org,                        // org name
		"127.0.0.1:9090",           // API address
		":7700",                    // webui
		"1",                        // provider: Anthropic
		"sk-ant-test",              // anthropic key
		"claude-sonnet-4-20250514", // model
		"",                         // safe DM pairing default? (Y)
		"n",                        // Slack?
		"n",                        // Telegram?
		"n",                        // Discord?
		"1",                        // first agent: assistant
	)
}

// TestRunInitWizardWritesValidConfig drives the full flow and asserts a valid,
// parseable cyntr.yaml plus the expected side-effect files.
func TestRunInitWizardWritesValidConfig(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInitWizard(strings.NewReader(fullRun("acme")), &out, dir); err != nil {
		t.Fatalf("wizard returned error: %v", err)
	}

	cfg := loadYAML(t, dir)
	if cfg.Version != "1" {
		t.Errorf("version = %q, want 1", cfg.Version)
	}
	if cfg.Listen.Address != "127.0.0.1:9090" {
		t.Errorf("listen address = %q", cfg.Listen.Address)
	}
	if _, ok := cfg.Tenants["acme"]; !ok {
		t.Errorf("tenant 'acme' missing: %+v", cfg.Tenants)
	}
	if cfg.Tenants["acme"].Isolation != "namespace" {
		t.Errorf("tenant isolation = %q, want namespace", cfg.Tenants["acme"].Isolation)
	}

	// .env should carry the provider key and the safe DM default.
	env := readEnvFile(filepath.Join(dir, ".env"))
	if env["ANTHROPIC_API_KEY"] != "sk-ant-test" {
		t.Errorf("ANTHROPIC_API_KEY = %q", env["ANTHROPIC_API_KEY"])
	}
	if env["CYNTR_DM_POLICY"] != "pairing" {
		t.Errorf("CYNTR_DM_POLICY = %q, want pairing", env["CYNTR_DM_POLICY"])
	}
	if env["CYNTR_API_KEY"] == "" {
		t.Errorf("CYNTR_API_KEY not generated")
	}

	// policy.yaml + agent file should exist.
	if _, err := os.Stat(filepath.Join(dir, "policy.yaml")); err != nil {
		t.Errorf("policy.yaml missing: %v", err)
	}
	agentData, err := os.ReadFile(filepath.Join(dir, "assistant-agent.json"))
	if err != nil {
		t.Fatalf("assistant-agent.json missing: %v", err)
	}
	if !strings.Contains(string(agentData), `"tenant": "acme"`) {
		t.Errorf("agent file missing tenant scoping:\n%s", agentData)
	}
	if !strings.Contains(string(agentData), `"model": "claude"`) {
		t.Errorf("agent file missing model:\n%s", agentData)
	}
}

// TestRunInitWizardDoctorRunsAtEnd asserts the doctor report is printed at the
// end of the wizard.
func TestRunInitWizardDoctorRunsAtEnd(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runInitWizard(strings.NewReader(fullRun("acme")), &out, dir); err != nil {
		t.Fatalf("wizard error: %v", err)
	}
	s := out.String()
	for _, want := range []string{"Cyntr Doctor", "overall:", "cyntr.yaml"} {
		if !strings.Contains(s, want) {
			t.Errorf("doctor output missing %q\n---\n%s", want, s)
		}
	}
}

// TestRunInitWizardIdempotent re-runs the wizard accepting all defaults and
// proves existing values are preserved, not clobbered, and the secret is not
// re-typed (mask accepted as default).
func TestRunInitWizardIdempotent(t *testing.T) {
	dir := t.TempDir()

	// First run: set everything up.
	var out1 bytes.Buffer
	if err := runInitWizard(strings.NewReader(fullRun("acme")), &out1, dir); err != nil {
		t.Fatalf("first run error: %v", err)
	}
	cfg1 := loadYAML(t, dir)
	env1 := readEnvFile(filepath.Join(dir, ".env"))

	// Second run: accept ALL defaults (every answer blank). Provider menu
	// pre-selects Anthropic (1) because the key is present; the masked key is
	// accepted as default so it is not re-typed.
	second := answers(
		"", // org -> keep acme
		"", // API address -> keep
		"", // webui -> keep
		"", // provider -> default detected (1, anthropic)
		"", // anthropic key -> keep (masked)
		"", // model -> keep
		"", // safe DM default? -> Y (keep pairing)
		"", // Slack? -> default N (not configured)
		"", // Telegram?
		"", // Discord?
		"", // first agent -> default 1
	)
	var out2 bytes.Buffer
	if err := runInitWizard(strings.NewReader(second), &out2, dir); err != nil {
		t.Fatalf("second run error: %v", err)
	}

	cfg2 := loadYAML(t, dir)
	env2 := readEnvFile(filepath.Join(dir, ".env"))

	if cfg2.Listen.Address != cfg1.Listen.Address {
		t.Errorf("listen address changed: %q -> %q", cfg1.Listen.Address, cfg2.Listen.Address)
	}
	if _, ok := cfg2.Tenants["acme"]; !ok {
		t.Errorf("tenant clobbered on re-run: %+v", cfg2.Tenants)
	}
	// Secret must survive a re-run (mask accepted, not overwritten with literal).
	if env2["ANTHROPIC_API_KEY"] != env1["ANTHROPIC_API_KEY"] {
		t.Errorf("secret changed on re-run: %q -> %q", env1["ANTHROPIC_API_KEY"], env2["ANTHROPIC_API_KEY"])
	}
	if env2["ANTHROPIC_API_KEY"] == maskToken {
		t.Errorf("mask placeholder leaked into .env")
	}
	// API key stable across re-run.
	if env2["CYNTR_API_KEY"] != env1["CYNTR_API_KEY"] {
		t.Errorf("API key regenerated on re-run: %q -> %q", env1["CYNTR_API_KEY"], env2["CYNTR_API_KEY"])
	}
}

// TestRunInitWizardInvalidInputReprompts feeds garbage to the numeric and y/n
// prompts and confirms the wizard recovers (re-prompts) and still produces a
// valid config rather than crashing.
func TestRunInitWizardInvalidInputReprompts(t *testing.T) {
	dir := t.TempDir()
	script := answers(
		"acme",        // org
		"",            // API address default
		"",            // webui default
		"99",          // provider: out of range -> reprompt
		"abc",         // provider: not a number -> reprompt
		"1",           // provider: valid (Anthropic)
		"sk-ant-test", // key
		"",            // model default
		"maybe",       // DM safe default y/n: invalid -> reprompt
		"y",           // DM safe default: yes
		"n",           // Slack
		"n",           // Telegram
		"n",           // Discord
		"7",           // first agent: out of range -> reprompt
		"2",           // first agent: cloud-ops
	)
	var out bytes.Buffer
	if err := runInitWizard(strings.NewReader(script), &out, dir); err != nil {
		t.Fatalf("wizard crashed on bad input: %v", err)
	}
	cfg := loadYAML(t, dir)
	if _, ok := cfg.Tenants["acme"]; !ok {
		t.Fatalf("expected valid config after reprompts: %+v", cfg)
	}
	s := out.String()
	if !strings.Contains(s, "choose a number between 1 and") {
		t.Errorf("expected numeric reprompt message in output")
	}
	if !strings.Contains(s, "Please answer y or n") {
		t.Errorf("expected y/n reprompt message in output")
	}
	// cloud-ops agent should have been written.
	if _, err := os.Stat(filepath.Join(dir, "cloud-ops-agent.json")); err != nil {
		t.Errorf("cloud-ops-agent.json missing: %v", err)
	}
}

// TestRunInitWizardOAuthProvider drives the OAuth (D20) provider path and
// asserts the OAuth client config lands in .env.
func TestRunInitWizardOAuthProvider(t *testing.T) {
	dir := t.TempDir()
	script := answers(
		"acme",                                 // org
		"",                                     // API addr
		"",                                     // webui
		"7",                                    // provider: OAuth
		"chatgpt",                              // provider name
		"https://auth.example.com/oauth/token", // token URL
		"client-abc",                           // client id
		"secret-xyz",                           // client secret
		"http://localhost:7700/oauth/callback", // redirect
		"",                                     // DM safe default
		"n", "n", "n",                          // channels
		"4", // first agent: skip
	)
	var out bytes.Buffer
	if err := runInitWizard(strings.NewReader(script), &out, dir); err != nil {
		t.Fatalf("oauth wizard error: %v", err)
	}
	env := readEnvFile(filepath.Join(dir, ".env"))
	if env["OAUTH_PROVIDER"] != "chatgpt" {
		t.Errorf("OAUTH_PROVIDER = %q", env["OAUTH_PROVIDER"])
	}
	if env["OAUTH_CLIENT_ID"] != "client-abc" {
		t.Errorf("OAUTH_CLIENT_ID = %q", env["OAUTH_CLIENT_ID"])
	}
	if env["OAUTH_TOKEN_URL"] != "https://auth.example.com/oauth/token" {
		t.Errorf("OAUTH_TOKEN_URL = %q", env["OAUTH_TOKEN_URL"])
	}
	// No agent file when skipped.
	if _, err := os.Stat(filepath.Join(dir, "assistant-agent.json")); err == nil {
		t.Errorf("agent file written despite skip")
	}
}

// TestRunInitWizardChannelDMDefault confirms enabling a channel writes a safe
// per-channel pairing policy and tenant scoping.
func TestRunInitWizardChannelDMDefault(t *testing.T) {
	dir := t.TempDir()
	script := answers(
		"acme", // org
		"", "", // API addr, webui
		"8",         // provider: skip
		"",          // DM safe default
		"y",         // Slack? yes
		"xoxb-123",  // slack token
		"assistant", // slack agent
		"n", "n",    // telegram, discord
		"4", // agent: skip
	)
	var out bytes.Buffer
	if err := runInitWizard(strings.NewReader(script), &out, dir); err != nil {
		t.Fatalf("channel wizard error: %v", err)
	}
	env := readEnvFile(filepath.Join(dir, ".env"))
	if env["SLACK_BOT_TOKEN"] != "xoxb-123" {
		t.Errorf("SLACK_BOT_TOKEN = %q", env["SLACK_BOT_TOKEN"])
	}
	if env["SLACK_TENANT"] != "acme" {
		t.Errorf("SLACK_TENANT = %q, want acme", env["SLACK_TENANT"])
	}
	if env["SLACK_DM_POLICY"] != "pairing" {
		t.Errorf("SLACK_DM_POLICY = %q, want pairing (safe default)", env["SLACK_DM_POLICY"])
	}
}

// TestReadWriteEnvFileRoundTrip covers quoting of values containing spaces.
func TestReadWriteEnvFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	in := map[string]string{
		"PLAIN":   "value",
		"SPACED":  "two words",
		"EMPTY":   "", // dropped
		"SPECIAL": "a&b?c",
	}
	if err := writeEnvFile(path, in); err != nil {
		t.Fatal(err)
	}
	got := readEnvFile(path)
	if got["PLAIN"] != "value" {
		t.Errorf("PLAIN = %q", got["PLAIN"])
	}
	if got["SPACED"] != "two words" {
		t.Errorf("SPACED = %q", got["SPACED"])
	}
	if got["SPECIAL"] != "a&b?c" {
		t.Errorf("SPECIAL = %q", got["SPECIAL"])
	}
	if _, ok := got["EMPTY"]; ok {
		t.Errorf("empty value should be dropped")
	}
}
