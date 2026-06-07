package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/policy"
)

// Status is the severity of a single diagnostic check. Higher value == worse.
// The process exit code equals the worst severity observed across all checks,
// so callers (CI, scripts) can gate on `cyntr doctor` failing.
type Status int

const (
	StatusPass Status = iota // ✓ everything is fine
	StatusWarn               // ⚠ non-fatal: optional/insecure config
	StatusFail               // ✗ fatal: required config missing or broken
)

func (s Status) String() string {
	switch s {
	case StatusFail:
		return "fail"
	case StatusWarn:
		return "warn"
	default:
		return "pass"
	}
}

func (s Status) symbol() string {
	switch s {
	case StatusFail:
		return "✗"
	case StatusWarn:
		return "⚠"
	default:
		return "✓"
	}
}

// CheckResult is the outcome of running a single Check.
type CheckResult struct {
	Status  Status
	Message string
}

// Check is one pluggable diagnostic. Run must never panic, never print, and —
// critically — never include a raw secret value in its message (callers print
// the message verbatim). Use redact()/secretPresent() helpers for that.
type Check struct {
	Name string
	Run  func() CheckResult
}

// pass/warn/fail are small constructors so check bodies stay readable.
func pass(format string, a ...any) CheckResult {
	return CheckResult{Status: StatusPass, Message: fmt.Sprintf(format, a...)}
}
func warn(format string, a ...any) CheckResult {
	return CheckResult{Status: StatusWarn, Message: fmt.Sprintf(format, a...)}
}
func fail(format string, a ...any) CheckResult {
	return CheckResult{Status: StatusFail, Message: fmt.Sprintf(format, a...)}
}

// secretPresent reports whether the named env var is set without ever exposing
// its value. Doctor checks MUST go through this rather than os.Getenv when the
// var holds a credential.
func secretPresent(env string) bool {
	return strings.TrimSpace(os.Getenv(env)) != ""
}

// runDoctor is the CLI entry point. It runs every check, prints a pass/warn/fail
// line for each, prints a summary, and exits with a code equal to the worst
// severity (0=pass, 1=warn, 2=fail) so it doubles as a CI gate.
func runDoctor() {
	worst := runDoctorChecks(doctorChecks(), os.Stdout)
	os.Exit(int(worst))
}

// runDoctorChecks runs checks in order, writes a report to w, and returns the
// worst severity seen. Split out from runDoctor (which adds os.Exit) so tests
// can assert on output + worst severity without killing the test process.
func runDoctorChecks(checks []Check, w io.Writer) Status {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Cyntr Doctor — Checking your setup")
	fmt.Fprintln(w)

	worst := StatusPass
	counts := map[Status]int{}
	for _, c := range checks {
		res := c.Run()
		if res.Status > worst {
			worst = res.Status
		}
		counts[res.Status]++
		fmt.Fprintf(w, "  %s %s — %s\n", res.Status.symbol(), c.Name, res.Message)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d pass, %d warn, %d fail — overall: %s\n",
		counts[StatusPass], counts[StatusWarn], counts[StatusFail], worst)
	if worst == StatusFail {
		fmt.Fprintln(w, "  Run 'cyntr init' to scaffold missing config.")
	}
	fmt.Fprintln(w)
	return worst
}

// doctorChecks assembles the full diagnostic suite. Each check is independent
// and reads live process state (env vars, filesystem, network).
func doctorChecks() []Check {
	return []Check{
		checkProviderKeys(),
		checkChannelTokens(),
		checkDBWritable(),
		checkConfigFile(),
		checkPolicyParses(),
		checkRiskyDMPolicy(),
		checkRiskyPolicyRules(),
		checkOIDCSanity(),
		checkPortConflict(),
		checkSandboxBackend(),
	}
}

// --- individual checks ---------------------------------------------------

// providerKeyEnvs lists the env vars that, if set, indicate a configured LLM
// provider. Names only — values are never read.
var providerKeyEnvs = []struct{ env, name string }{
	{"ANTHROPIC_API_KEY", "Claude"},
	{"OPENAI_API_KEY", "OpenAI"},
	{"AZURE_OPENAI_API_KEY", "Azure OpenAI"},
	{"GEMINI_API_KEY", "Gemini"},
	{"OPENROUTER_API_KEY", "OpenRouter"},
	{"OLLAMA_URL", "Ollama"},
	{"OLLAMA_HOST", "Ollama"},
}

func checkProviderKeys() Check {
	return Check{Name: "provider keys", Run: func() CheckResult {
		var found []string
		for _, p := range providerKeyEnvs {
			if secretPresent(p.env) {
				found = append(found, p.name)
			}
		}
		if len(found) == 0 {
			return warn("no LLM provider configured — set ANTHROPIC_API_KEY or another provider key")
		}
		return pass("%d provider(s) configured: %s", len(found), strings.Join(dedup(found), ", "))
	}}
}

// channelTokenEnvs lists the credential env var per messaging channel. Names
// only; values are never printed.
var channelTokenEnvs = []struct{ env, name string }{
	{"SLACK_BOT_TOKEN", "Slack"},
	{"TEAMS_APP_SECRET", "Teams"},
	{"WHATSAPP_ACCESS_TOKEN", "WhatsApp"},
	{"TELEGRAM_BOT_TOKEN", "Telegram"},
	{"DISCORD_BOT_TOKEN", "Discord"},
	{"GOOGLE_CHAT_WEBHOOK_URL", "Google Chat"},
	{"MATTERMOST_WEBHOOK_URL", "Mattermost"},
	{"SIGNAL_CLI_URL", "Signal"},
	{"MATRIX_ACCESS_TOKEN", "Matrix"},
	{"TWILIO_AUTH_TOKEN", "SMS (Twilio)"},
}

func checkChannelTokens() Check {
	return Check{Name: "channel tokens", Run: func() CheckResult {
		var found []string
		for _, c := range channelTokenEnvs {
			if secretPresent(c.env) {
				found = append(found, c.name)
			}
		}
		if len(found) == 0 {
			return warn("no messaging channels configured")
		}
		return pass("%d channel(s) configured: %s", len(found), strings.Join(dedup(found), ", "))
	}}
}

// checkDBWritable confirms the data directory accepts writes — durable state
// (SQLite stores) lives there, so a read-only mount is fatal.
func checkDBWritable() Check {
	return Check{Name: "DB writable", Run: func() CheckResult {
		dir := os.Getenv("CYNTR_DATA_DIR")
		if dir == "" {
			dir = "."
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail("cannot create data dir %s: %v", dir, err)
		}
		f, err := os.CreateTemp(dir, ".cyntr-doctor-*.tmp")
		if err != nil {
			return fail("data dir %s is not writable: %v", dir, err)
		}
		name := f.Name()
		f.Close()
		os.Remove(name)
		return pass("data dir %s is writable", dir)
	}}
}

func checkConfigFile() Check {
	return Check{Name: "cyntr.yaml", Run: func() CheckResult {
		if _, err := os.Stat("cyntr.yaml"); err != nil {
			return fail("cyntr.yaml not found — run 'cyntr init'")
		}
		return pass("found")
	}}
}

// checkPolicyParses verifies policy.yaml exists and parses with the same loader
// the engine uses at boot, so a syntactically broken policy is caught here
// instead of at first request.
func checkPolicyParses() Check {
	return Check{Name: "policy.yaml parses", Run: func() CheckResult {
		if _, err := os.Stat("policy.yaml"); err != nil {
			return fail("policy.yaml not found — run 'cyntr init'")
		}
		rs, err := policy.LoadRuleSet("policy.yaml")
		if err != nil {
			return fail("policy.yaml does not parse: %v", err)
		}
		return pass("parsed, %d rule(s)", len(rs.Rules))
	}}
}

// checkRiskyDMPolicy flags an open DM-pairing posture. CYNTR_DM_POLICY (and
// per-channel CYNTR_DM_POLICIES) gate untrusted inbound DMs; "open" (or unset,
// which defaults to open for back-compat) means anyone can DM the agent.
func checkRiskyDMPolicy() Check {
	return Check{Name: "DM pairing policy", Run: func() CheckResult {
		def := strings.ToLower(strings.TrimSpace(os.Getenv("CYNTR_DM_POLICY")))
		var risky []string
		if def == "" {
			risky = append(risky, "default (unset → open)")
		} else if def == "open" {
			risky = append(risky, "default=open")
		}
		for _, entry := range strings.Split(os.Getenv("CYNTR_DM_POLICIES"), ",") {
			kv := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(kv) == 2 && kv[0] != "" && strings.EqualFold(strings.TrimSpace(kv[1]), "open") {
				risky = append(risky, strings.TrimSpace(kv[0])+"=open")
			}
		}
		if len(risky) > 0 {
			return warn("open DM pairing — anyone can message the agent: %s; set CYNTR_DM_POLICY=pairing", strings.Join(risky, ", "))
		}
		return pass("DM pairing gated (policy=%s)", def)
	}}
}

// checkRiskyPolicyRules scans policy.yaml for overly broad allow rules:
// wildcard '*' on tool/action/agent combined with an allow decision lets an
// agent reach any tool. These are the classic foot-guns; flag (don't fail).
func checkRiskyPolicyRules() Check {
	return Check{Name: "risky policy rules", Run: func() CheckResult {
		rs, err := policy.LoadRuleSet("policy.yaml")
		if err != nil {
			// checkPolicyParses already reports the parse failure as fail;
			// don't double-fail here.
			return warn("skipped — policy.yaml unreadable")
		}
		var risky []string
		for _, r := range rs.Rules {
			if r.Decision != policy.Allow {
				continue
			}
			if r.Tool == "*" && r.Action == "*" {
				name := r.Name
				if name == "" {
					name = "(unnamed)"
				}
				risky = append(risky, name)
			}
		}
		if len(risky) > 0 {
			return warn("wildcard allow rule(s) grant blanket access: %s", strings.Join(risky, ", "))
		}
		return pass("no blanket wildcard allow rules")
	}}
}

// checkOIDCSanity validates OIDC SSO config is internally consistent: either
// fully configured or fully absent. A partial config (issuer but no client, or
// client id but no secret) silently breaks login, so flag it.
func checkOIDCSanity() Check {
	return Check{Name: "OIDC config", Run: func() CheckResult {
		issuer := strings.TrimSpace(os.Getenv("CYNTR_OIDC_ISSUER"))
		clientID := strings.TrimSpace(os.Getenv("CYNTR_OIDC_CLIENT_ID"))
		clientSecretSet := secretPresent("CYNTR_OIDC_CLIENT_SECRET")
		redirect := strings.TrimSpace(os.Getenv("CYNTR_OIDC_REDIRECT_URL"))

		anySet := issuer != "" || clientID != "" || clientSecretSet || redirect != ""
		if !anySet {
			return pass("OIDC not configured (SSO disabled)")
		}
		var missing []string
		if issuer == "" {
			missing = append(missing, "CYNTR_OIDC_ISSUER")
		} else if !strings.HasPrefix(issuer, "https://") {
			return warn("CYNTR_OIDC_ISSUER should be an https:// URL")
		}
		if clientID == "" {
			missing = append(missing, "CYNTR_OIDC_CLIENT_ID")
		}
		if !clientSecretSet {
			missing = append(missing, "CYNTR_OIDC_CLIENT_SECRET")
		}
		if redirect == "" {
			missing = append(missing, "CYNTR_OIDC_REDIRECT_URL")
		}
		if len(missing) > 0 {
			return fail("OIDC partially configured — missing: %s", strings.Join(missing, ", "))
		}
		return pass("OIDC fully configured (issuer present, client id/secret/redirect set)")
	}}
}

// checkPortConflict verifies the web UI port (:7700) is free to bind. A
// conflict means 'cyntr start' will fail to serve the dashboard.
func checkPortConflict() Check {
	return Check{Name: "port :7700 free", Run: func() CheckResult {
		addr := os.Getenv("CYNTR_WEBUI_ADDR")
		if addr == "" {
			addr = ":7700"
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return warn("%s is already in use — 'cyntr start' web UI may conflict: %v", addr, err)
		}
		ln.Close()
		return pass("%s is available", addr)
	}}
}

// checkSandboxBackend reports whether the Docker sandbox backend is reachable.
// Container isolation for shell exec is optional, so absence is a warning.
func checkSandboxBackend() Check {
	return Check{Name: "sandbox backend", Run: func() CheckResult {
		if _, err := exec.LookPath("docker"); err != nil {
			return warn("docker not installed — container-isolated shell exec unavailable (in-process fallback)")
		}
		cmd := exec.Command("docker", "info")
		// Bound the probe so a hung daemon doesn't stall doctor.
		done := make(chan error, 1)
		if err := cmd.Start(); err != nil {
			return warn("docker present but not runnable: %v", err)
		}
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				return warn("docker installed but daemon not reachable — start Docker for sandboxing")
			}
			return pass("docker daemon reachable — sandbox available")
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			return warn("docker info timed out — daemon may be unhealthy")
		}
	}}
}

// dedup removes duplicate strings while preserving first-seen order.
func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
