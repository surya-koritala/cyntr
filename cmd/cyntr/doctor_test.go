package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunDoctorChecksWorstSeverity proves the returned severity (which becomes
// the process exit code) equals the worst status across all checks.
func TestRunDoctorChecksWorstSeverity(t *testing.T) {
	mk := func(name string, s Status) Check {
		return Check{Name: name, Run: func() CheckResult {
			return CheckResult{Status: s, Message: "x"}
		}}
	}
	tests := []struct {
		name   string
		checks []Check
		want   Status
	}{
		{"all pass", []Check{mk("a", StatusPass), mk("b", StatusPass)}, StatusPass},
		{"warn dominates pass", []Check{mk("a", StatusPass), mk("b", StatusWarn)}, StatusWarn},
		{"fail dominates warn", []Check{mk("a", StatusWarn), mk("b", StatusFail), mk("c", StatusPass)}, StatusFail},
		{"fail dominates regardless of order", []Check{mk("a", StatusFail), mk("b", StatusPass)}, StatusFail},
		{"single warn", []Check{mk("only", StatusWarn)}, StatusWarn},
		{"empty is pass", nil, StatusPass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			got := runDoctorChecks(tc.checks, &buf)
			if got != tc.want {
				t.Fatalf("worst severity = %v (exit %d), want %v (exit %d)", got, int(got), tc.want, int(tc.want))
			}
			// Exit-code contract: fail>warn>pass numerically.
			if int(StatusFail) <= int(StatusWarn) || int(StatusWarn) <= int(StatusPass) {
				t.Fatalf("status ordering broken: pass=%d warn=%d fail=%d", StatusPass, StatusWarn, StatusFail)
			}
		})
	}
}

// TestRunDoctorChecksReportLines confirms each check emits a labeled line with
// the correct symbol, and the summary counts are accurate.
func TestRunDoctorChecksReportLines(t *testing.T) {
	checks := []Check{
		{Name: "alpha", Run: func() CheckResult { return CheckResult{StatusPass, "ok"} }},
		{Name: "beta", Run: func() CheckResult { return CheckResult{StatusWarn, "meh"} }},
		{Name: "gamma", Run: func() CheckResult { return CheckResult{StatusFail, "bad"} }},
	}
	var buf bytes.Buffer
	runDoctorChecks(checks, &buf)
	out := buf.String()
	for _, want := range []string{"alpha", "beta", "gamma", "✓", "⚠", "✗", "1 pass, 1 warn, 1 fail", "overall: fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

// TestSecretRedaction proves no check ever prints a secret VALUE. We set every
// known secret env var to a sentinel and assert it never appears in output,
// while the provider/channel checks still report the credential as present.
func TestSecretRedaction(t *testing.T) {
	const sentinel = "SUPERSECRETVALUE-do-not-print-1234567890"

	allSecretEnvs := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "AZURE_OPENAI_API_KEY",
		"GEMINI_API_KEY", "OPENROUTER_API_KEY", "OLLAMA_URL",
		"SLACK_BOT_TOKEN", "TEAMS_APP_SECRET", "WHATSAPP_ACCESS_TOKEN",
		"TELEGRAM_BOT_TOKEN", "DISCORD_BOT_TOKEN", "GOOGLE_CHAT_WEBHOOK_URL",
		"MATTERMOST_WEBHOOK_URL", "SIGNAL_CLI_URL", "MATRIX_ACCESS_TOKEN",
		"TWILIO_AUTH_TOKEN", "CYNTR_OIDC_CLIENT_SECRET", "CYNTR_OIDC_CLIENT_ID",
	}
	for _, e := range allSecretEnvs {
		t.Setenv(e, sentinel)
	}
	// Give OIDC a complete config so it reaches the success message that names
	// the fields — proving even the "configured" path never echoes values.
	t.Setenv("CYNTR_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CYNTR_OIDC_REDIRECT_URL", "https://app.example.com/callback")

	var buf bytes.Buffer
	runDoctorChecks(doctorChecks(), &buf)
	out := buf.String()

	if strings.Contains(out, sentinel) {
		t.Fatalf("secret value leaked into doctor output:\n%s", out)
	}
	// Sanity: the provider/channel checks should still acknowledge presence.
	if !strings.Contains(out, "provider(s) configured") {
		t.Errorf("expected provider presence to be reported\n%s", out)
	}
	if !strings.Contains(out, "channel(s) configured") {
		t.Errorf("expected channel presence to be reported\n%s", out)
	}
}

// TestCheckPolicyParses covers the valid, invalid, and missing policy.yaml
// cases against the same loader the engine uses.
func TestCheckPolicyParses(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Missing -> fail.
	if r := checkPolicyParses().Run(); r.Status != StatusFail {
		t.Errorf("missing policy: got %v, want fail", r.Status)
	}

	// Valid -> pass.
	writeFile(t, dir, "policy.yaml", `rules:
  - name: allow-read
    tenant: acme
    action: tool_call
    tool: file_read
    decision: allow
    priority: 10
`)
	if r := checkPolicyParses().Run(); r.Status != StatusPass {
		t.Errorf("valid policy: got %v (%s), want pass", r.Status, r.Message)
	}

	// Invalid YAML -> fail.
	writeFile(t, dir, "policy.yaml", "rules: [this is : not : valid")
	if r := checkPolicyParses().Run(); r.Status != StatusFail {
		t.Errorf("invalid policy: got %v, want fail", r.Status)
	}
}

// TestCheckRiskyPolicyRules flags blanket wildcard allow rules.
func TestCheckRiskyPolicyRules(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	writeFile(t, dir, "policy.yaml", `rules:
  - name: god-mode
    tenant: "*"
    action: "*"
    tool: "*"
    decision: allow
    priority: 1
`)
	if r := checkRiskyPolicyRules().Run(); r.Status != StatusWarn {
		t.Errorf("wildcard allow: got %v, want warn", r.Status)
	}
	if !strings.Contains(checkRiskyPolicyRules().Run().Message, "god-mode") {
		t.Errorf("expected offending rule name in message")
	}

	writeFile(t, dir, "policy.yaml", `rules:
  - name: scoped
    tenant: acme
    action: tool_call
    tool: file_read
    decision: allow
    priority: 1
`)
	if r := checkRiskyPolicyRules().Run(); r.Status != StatusPass {
		t.Errorf("scoped allow: got %v, want pass", r.Status)
	}
}

// TestCheckRiskyDMPolicy covers open (risky) vs pairing (safe) postures.
func TestCheckRiskyDMPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		policies string
		want     Status
	}{
		{"unset defaults open", "", "", StatusWarn},
		{"explicit open", "open", "", StatusWarn},
		{"pairing safe", "pairing", "", StatusPass},
		{"closed safe", "closed", "", StatusPass},
		{"per-channel open overrides", "pairing", "slack=open", StatusWarn},
		{"per-channel all gated", "pairing", "slack=pairing,web=closed", StatusPass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CYNTR_DM_POLICY", tc.policy)
			t.Setenv("CYNTR_DM_POLICIES", tc.policies)
			if r := checkRiskyDMPolicy().Run(); r.Status != tc.want {
				t.Errorf("got %v (%s), want %v", r.Status, r.Message, tc.want)
			}
		})
	}
}

// TestCheckOIDCSanity covers absent, partial, and complete configs.
func TestCheckOIDCSanity(t *testing.T) {
	clear := func() {
		for _, e := range []string{"CYNTR_OIDC_ISSUER", "CYNTR_OIDC_CLIENT_ID", "CYNTR_OIDC_CLIENT_SECRET", "CYNTR_OIDC_REDIRECT_URL"} {
			t.Setenv(e, "")
		}
	}

	clear()
	if r := checkOIDCSanity().Run(); r.Status != StatusPass {
		t.Errorf("absent OIDC: got %v, want pass", r.Status)
	}

	clear()
	t.Setenv("CYNTR_OIDC_ISSUER", "https://issuer.example.com")
	if r := checkOIDCSanity().Run(); r.Status != StatusFail {
		t.Errorf("partial OIDC: got %v, want fail", r.Status)
	}

	clear()
	t.Setenv("CYNTR_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CYNTR_OIDC_CLIENT_ID", "client-123")
	t.Setenv("CYNTR_OIDC_CLIENT_SECRET", "secret-xyz")
	t.Setenv("CYNTR_OIDC_REDIRECT_URL", "https://app.example.com/callback")
	if r := checkOIDCSanity().Run(); r.Status != StatusPass {
		t.Errorf("complete OIDC: got %v (%s), want pass", r.Status, r.Message)
	}

	clear()
	t.Setenv("CYNTR_OIDC_ISSUER", "http://insecure.example.com")
	t.Setenv("CYNTR_OIDC_CLIENT_ID", "client-123")
	t.Setenv("CYNTR_OIDC_CLIENT_SECRET", "secret-xyz")
	t.Setenv("CYNTR_OIDC_REDIRECT_URL", "https://app.example.com/callback")
	if r := checkOIDCSanity().Run(); r.Status != StatusWarn {
		t.Errorf("non-https issuer: got %v, want warn", r.Status)
	}
}

// TestCheckDBWritable confirms a writable temp dir passes.
func TestCheckDBWritable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CYNTR_DATA_DIR", dir)
	if r := checkDBWritable().Run(); r.Status != StatusPass {
		t.Errorf("writable dir: got %v (%s), want pass", r.Status, r.Message)
	}
}

// TestCheckProviderKeys covers the empty and configured cases.
func TestCheckProviderKeys(t *testing.T) {
	for _, p := range providerKeyEnvs {
		t.Setenv(p.env, "")
	}
	if r := checkProviderKeys().Run(); r.Status != StatusWarn {
		t.Errorf("no provider: got %v, want warn", r.Status)
	}
	t.Setenv("ANTHROPIC_API_KEY", "x")
	if r := checkProviderKeys().Run(); r.Status != StatusPass {
		t.Errorf("provider set: got %v, want pass", r.Status)
	}
}

// --- helpers -------------------------------------------------------------

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
