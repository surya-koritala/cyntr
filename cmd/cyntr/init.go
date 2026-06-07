package main

import (
	"bufio"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"gopkg.in/yaml.v3"
)

// runInit is the CLI entry point (main.go: case "init": runInit()). It drives
// the guided onboarding wizard against the real stdin/stdout in the current
// working directory. The actual logic lives in runInitWizard so tests can feed
// scripted answers and assert on the generated files.
func runInit() {
	if err := runInitWizard(os.Stdin, os.Stdout, "."); err != nil {
		fmt.Fprintln(os.Stderr, "  init failed:", err)
		os.Exit(1)
	}
}

// wiz is the wizard's shared I/O context. Prompts read a line from in and echo
// to out so the flow is fully drivable from a test (feed stdin -> assert files).
type wiz struct {
	sc  *bufio.Scanner
	out io.Writer
	dir string
}

func (w *wiz) printf(format string, a ...any) { fmt.Fprintf(w.out, format, a...) }
func (w *wiz) println(a ...any)               { fmt.Fprintln(w.out, a...) }

// ask prompts for free-form text, showing defaultVal in brackets. Empty input
// keeps the default. This is the idempotency primitive: on a re-run we pass the
// previously-configured value as defaultVal, so just hitting enter preserves it.
func (w *wiz) ask(label, defaultVal string) string {
	if defaultVal != "" {
		w.printf("%s [%s]: ", label, defaultVal)
	} else {
		w.printf("%s: ", label)
	}
	if !w.sc.Scan() {
		return defaultVal
	}
	val := strings.TrimSpace(w.sc.Text())
	if val == "" {
		return defaultVal
	}
	return val
}

// askYN prompts for a yes/no answer. defaultYes controls what an empty answer
// means and which letter is capitalized. Any non y/n input re-prompts rather
// than silently picking a side.
func (w *wiz) askYN(label string, defaultYes bool) bool {
	suffix := "(y/N)"
	if defaultYes {
		suffix = "(Y/n)"
	}
	for {
		w.printf("%s %s: ", label, suffix)
		if !w.sc.Scan() {
			return defaultYes
		}
		switch strings.ToLower(strings.TrimSpace(w.sc.Text())) {
		case "":
			return defaultYes
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			w.println("    Please answer y or n.")
		}
	}
}

// askChoice prompts for a numbered menu choice in [1, max]. Out-of-range or
// non-numeric input re-prompts instead of crashing.
func (w *wiz) askChoice(label, defaultVal string, max int) string {
	for {
		v := w.ask(label, defaultVal)
		n := 0
		ok := len(v) > 0
		for _, r := range v {
			if r < '0' || r > '9' {
				ok = false
				break
			}
			n = n*10 + int(r-'0')
		}
		if ok && n >= 1 && n <= max {
			return v
		}
		w.printf("    Please choose a number between 1 and %d.\n", max)
	}
}

func (w *wiz) path(name string) string { return filepath.Join(w.dir, name) }

// runInitWizard is the testable core. It is IDEMPOTENT: existing cyntr.yaml and
// .env values are loaded and offered as defaults, never blindly clobbered. It
// always finishes by running the doctor checks and printing their report.
func runInitWizard(in io.Reader, out io.Writer, dir string) error {
	w := &wiz{sc: bufio.NewScanner(in), out: out, dir: dir}

	w.println()
	w.println("  ┌───────────────────────────────────────┐")
	w.println("  │   Cyntr Setup Wizard                   │")
	w.println("  │   Enterprise AI Agent Platform         │")
	w.println("  └───────────────────────────────────────┘")
	w.println()

	// Load existing config (idempotent re-run). A malformed file is treated as
	// empty defaults rather than aborting, so the wizard can repair it.
	cfg := config.DefaultConfig()
	configExisted := false
	if data, err := os.ReadFile(w.path("cyntr.yaml")); err == nil {
		configExisted = true
		var existing config.CyntrConfig
		if yaml.Unmarshal(data, &existing) == nil {
			cfg = existing
		}
		if cfg.Version == "" {
			cfg.Version = "1"
		}
		if cfg.Tenants == nil {
			cfg.Tenants = map[string]config.TenantConfig{}
		}
		w.println("  Existing cyntr.yaml found — your current values are offered as")
		w.println("  defaults. Press enter to keep them.")
		w.println()
	}

	// Load existing .env so secrets/settings survive a re-run.
	env := readEnvFile(w.path(".env"))

	// --- Step 1: basics --------------------------------------------------
	w.println("  Step 1 of 5: Basic Configuration")
	w.println("  ─────────────────────────────────")
	w.println()

	tenantName := pickTenant(cfg)
	tenantName = w.ask("  Organization/team name", tenantName)
	listenAddr := w.ask("  API address", orDefault(cfg.Listen.Address, "127.0.0.1:8080"))
	webui := w.ask("  Dashboard webui (host:port)", orDefault(cfg.Listen.WebUI, ":7700"))

	// --- Step 2: provider (incl. OAuth, D20) -----------------------------
	w.println()
	w.println("  Step 2 of 5: AI Model Provider")
	w.println("  ──────────────────────────────")
	w.println()
	w.println("  1) Anthropic (Claude)      — API key")
	w.println("  2) OpenAI (GPT)            — API key")
	w.println("  3) Azure OpenAI            — API key")
	w.println("  4) Google (Gemini)         — API key")
	w.println("  5) OpenRouter              — API key")
	w.println("  6) Ollama (local)          — no key")
	w.println("  7) OAuth subscription login — ChatGPT/Codex etc. (no API key)")
	w.println("  8) Skip for now")
	w.println()

	defProvider := detectProviderDefault(env)
	agentModelName := "mock"
	providerChoice := w.askChoice("  Choose", defProvider, 8)

	switch providerChoice {
	case "1":
		if key := w.ask("  Anthropic API key", masked(env["ANTHROPIC_API_KEY"])); key != "" && !isMask(key) {
			env["ANTHROPIC_API_KEY"] = key
		}
		if env["ANTHROPIC_API_KEY"] != "" {
			env["ANTHROPIC_MODEL"] = w.ask("  Model", orDefault(env["ANTHROPIC_MODEL"], "claude-sonnet-4-20250514"))
			agentModelName = "claude"
		}
	case "2":
		if key := w.ask("  OpenAI API key", masked(env["OPENAI_API_KEY"])); key != "" && !isMask(key) {
			env["OPENAI_API_KEY"] = key
		}
		if env["OPENAI_API_KEY"] != "" {
			env["OPENAI_MODEL"] = w.ask("  Model", orDefault(env["OPENAI_MODEL"], "gpt-4o"))
			agentModelName = "gpt"
		}
	case "3":
		if key := w.ask("  Azure API key", masked(env["AZURE_OPENAI_API_KEY"])); key != "" && !isMask(key) {
			env["AZURE_OPENAI_API_KEY"] = key
		}
		if env["AZURE_OPENAI_API_KEY"] != "" {
			ep := w.ask("  Endpoint (base URL only)", env["AZURE_OPENAI_ENDPOINT"])
			if idx := strings.Index(ep, "/openai"); idx > 0 {
				ep = ep[:idx]
			}
			if idx := strings.Index(ep, "?"); idx > 0 {
				ep = ep[:idx]
			}
			env["AZURE_OPENAI_ENDPOINT"] = strings.TrimRight(ep, "/")
			env["AZURE_OPENAI_DEPLOYMENT"] = w.ask("  Deployment name", orDefault(env["AZURE_OPENAI_DEPLOYMENT"], "gpt-4o"))
			env["AZURE_OPENAI_API_VERSION"] = w.ask("  API version", orDefault(env["AZURE_OPENAI_API_VERSION"], "2024-08-01-preview"))
			agentModelName = "azure-openai"
		}
	case "4":
		if key := w.ask("  Gemini API key", masked(env["GEMINI_API_KEY"])); key != "" && !isMask(key) {
			env["GEMINI_API_KEY"] = key
		}
		if env["GEMINI_API_KEY"] != "" {
			env["GEMINI_MODEL"] = w.ask("  Model", orDefault(env["GEMINI_MODEL"], "gemini-1.5-pro"))
			agentModelName = "gemini"
		}
	case "5":
		if key := w.ask("  OpenRouter API key", masked(env["OPENROUTER_API_KEY"])); key != "" && !isMask(key) {
			env["OPENROUTER_API_KEY"] = key
		}
		if env["OPENROUTER_API_KEY"] != "" {
			env["OPENROUTER_MODEL"] = w.ask("  Model", orDefault(env["OPENROUTER_MODEL"], "anthropic/claude-3.5-sonnet"))
			agentModelName = "openrouter"
		}
	case "6":
		env["OLLAMA_URL"] = w.ask("  Ollama URL", orDefault(env["OLLAMA_URL"], "http://localhost:11434"))
		env["OLLAMA_MODEL"] = w.ask("  Model", orDefault(env["OLLAMA_MODEL"], "llama3"))
		agentModelName = "ollama"
	case "7":
		// OAuth subscription login (D20). The wizard records the OAuth client
		// config; the running server exchanges the code via providers.OAuthManager.
		w.println()
		w.println("    OAuth lets a tenant authenticate with a subscription login")
		w.println("    instead of an API key. Enter your provider's OAuth client config.")
		w.println("    The access token is obtained at runtime, not stored here.")
		w.println()
		prov := w.ask("  Provider name (e.g. chatgpt)", orDefault(env["OAUTH_PROVIDER"], "chatgpt"))
		env["OAUTH_PROVIDER"] = prov
		env["OAUTH_TOKEN_URL"] = w.ask("  Token URL", orDefault(env["OAUTH_TOKEN_URL"], "https://auth.openai.com/oauth/token"))
		if id := w.ask("  OAuth client ID", masked(env["OAUTH_CLIENT_ID"])); id != "" && !isMask(id) {
			env["OAUTH_CLIENT_ID"] = id
		}
		if sec := w.ask("  OAuth client secret (optional)", masked(env["OAUTH_CLIENT_SECRET"])); sec != "" && !isMask(sec) {
			env["OAUTH_CLIENT_SECRET"] = sec
		}
		env["OAUTH_REDIRECT_URL"] = w.ask("  Redirect URL", orDefault(env["OAUTH_REDIRECT_URL"], "http://localhost:7700/oauth/callback"))
		agentModelName = prov
	}

	// --- Step 3: channels with safe DM pairing defaults (B12) ------------
	w.println()
	w.println("  Step 3 of 5: Messaging Channels (optional)")
	w.println("  ───────────────────────────────────────────")
	w.println()
	w.println("  Inbound DMs are untrusted. Cyntr defaults to PAIRING: an unknown")
	w.println("  sender must be approved by an operator before reaching the agent.")
	w.println()

	// Default DM policy. On a re-run keep whatever was set; otherwise the safe
	// default is "pairing" (never "open").
	defDM := orDefault(env["CYNTR_DM_POLICY"], "pairing")
	if w.askYN("  Use safe DM pairing default (recommended)?", strings.EqualFold(defDM, "pairing")) {
		env["CYNTR_DM_POLICY"] = "pairing"
	} else {
		w.println("    1) open    — anyone may message the agent (NOT recommended)")
		w.println("    2) closed  — no inbound is processed")
		switch w.askChoice("    DM policy", map[bool]string{true: "1", false: "2"}[strings.EqualFold(defDM, "open")], 2) {
		case "1":
			env["CYNTR_DM_POLICY"] = "open"
		case "2":
			env["CYNTR_DM_POLICY"] = "closed"
		}
	}

	type chanDef struct {
		name      string
		label     string
		tokenEnv  string
		tokenName string
		extra     func()
	}
	channels := []chanDef{
		{name: "slack", label: "Slack", tokenEnv: "SLACK_BOT_TOKEN", tokenName: "Bot token (xoxb-...)"},
		{name: "telegram", label: "Telegram", tokenEnv: "TELEGRAM_BOT_TOKEN", tokenName: "Bot token"},
		{name: "discord", label: "Discord", tokenEnv: "DISCORD_BOT_TOKEN", tokenName: "Bot token"},
	}
	for _, ch := range channels {
		already := env[ch.tokenEnv] != ""
		if !w.askYN("  Enable "+ch.label+"?", already) {
			continue
		}
		if tok := w.ask("    "+ch.tokenName, masked(env[ch.tokenEnv])); tok != "" && !isMask(tok) {
			env[ch.tokenEnv] = tok
		}
		if env[ch.tokenEnv] == "" {
			continue
		}
		upper := strings.ToUpper(ch.name)
		env[upper+"_TENANT"] = tenantName
		env[upper+"_AGENT"] = w.ask("    Agent to handle "+ch.label+" messages", orDefault(env[upper+"_AGENT"], "assistant"))
		// Per-channel DM policy override (safe default = pairing).
		env[upper+"_DM_POLICY"] = orDefault(env[upper+"_DM_POLICY"], "pairing")
	}

	// --- Step 4: first agent ---------------------------------------------
	w.println()
	w.println("  Step 4 of 5: First Agent")
	w.println("  ────────────────────────")
	w.println()
	w.println("  1) General Assistant  — all-purpose, all tools")
	w.println("  2) Cloud Ops          — read-only infra troubleshooting")
	w.println("  3) Code Reviewer      — PR review, bug detection")
	w.println("  4) Skip")
	w.println()

	type agentTemplate struct {
		Name         string   `json:"name"`
		Tenant       string   `json:"tenant"`
		Model        string   `json:"model"`
		SystemPrompt string   `json:"system_prompt"`
		Tools        []string `json:"tools"`
		Skills       []string `json:"skills"`
		MaxTurns     int      `json:"max_turns"`
	}
	templates := map[string]agentTemplate{
		"assistant": {
			Name: "assistant", MaxTurns: 20, Tools: []string{"*"},
			SystemPrompt: "You are a helpful AI assistant with access to all tools. Execute commands directly when asked. Be concise and actionable.",
		},
		"cloud-ops": {
			Name: "cloud-ops", MaxTurns: 20,
			Tools:        []string{"shell_exec", "http_request", "web_search", "file_read"},
			Skills:       []string{"aws-infrastructure-audit", "incident-commander", "log-analyzer"},
			SystemPrompt: "You are a read-only cloud infrastructure agent. ONLY use read/describe/list/get commands. Never modify resources.",
		},
		"code-reviewer": {
			Name: "code-reviewer", MaxTurns: 15,
			Tools:        []string{"file_read", "file_search", "shell_exec", "github"},
			Skills:       []string{"code-reviewer-pro", "test-generator"},
			SystemPrompt: "You are an expert code reviewer. Analyze code for bugs, security issues, and style. Provide specific, actionable feedback.",
		},
	}
	agentName := ""
	switch w.askChoice("  Choose", "1", 4) {
	case "1":
		agentName = "assistant"
	case "2":
		agentName = "cloud-ops"
	case "3":
		agentName = "code-reviewer"
	}
	if agentName != "" {
		tmpl := templates[agentName]
		tmpl.Tenant = tenantName
		tmpl.Model = agentModelName
		data, _ := json.MarshalIndent(tmpl, "", "  ")
		fname := agentName + "-agent.json"
		if err := os.WriteFile(w.path(fname), data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", fname, err)
		}
		w.printf("  ✓ %s\n", fname)
	}

	// Generate (or preserve) the dashboard/API key.
	if env["CYNTR_API_KEY"] == "" {
		keyBuf := make([]byte, 32)
		crand.Read(keyBuf)
		env["CYNTR_API_KEY"] = "cyntr_" + hex.EncodeToString(keyBuf)
	}

	// --- Persist cyntr.yaml (merge, don't clobber) -----------------------
	cfg.Version = orDefault(cfg.Version, "1")
	cfg.Listen.Address = listenAddr
	cfg.Listen.WebUI = webui
	if cfg.Tenants == nil {
		cfg.Tenants = map[string]config.TenantConfig{}
	}
	tc := cfg.Tenants[tenantName] // preserve existing tenant settings if present
	if tc.Isolation == "" {
		tc.Isolation = "namespace"
	}
	if tc.Policy == "" {
		tc.Policy = "default"
	}
	cfg.Tenants[tenantName] = tc

	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal cyntr.yaml: %w", err)
	}
	if err := os.WriteFile(w.path("cyntr.yaml"), yamlBytes, 0o644); err != nil {
		return fmt.Errorf("write cyntr.yaml: %w", err)
	}
	w.println()
	w.println("  Generating configuration...")
	w.println()
	w.println("  ✓ cyntr.yaml")

	// policy.yaml — only scaffold if absent (don't clobber operator edits).
	if _, statErr := os.Stat(w.path("policy.yaml")); os.IsNotExist(statErr) {
		if err := os.WriteFile(w.path("policy.yaml"), []byte(defaultPolicyYAML), 0o644); err != nil {
			return fmt.Errorf("write policy.yaml: %w", err)
		}
		w.println("  ✓ policy.yaml")
	} else {
		w.println("  · policy.yaml (kept existing)")
	}

	// .env — write the merged map (secrets preserved, new values added).
	if err := writeEnvFile(w.path(".env"), env); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	w.println("  ✓ .env")

	_ = configExisted

	// --- Step 5: doctor checks (F26) -------------------------------------
	w.println()
	w.println("  Step 5 of 5: Verifying your setup")
	w.println("  ──────────────────────────────────")

	// Doctor checks read live process env + the cwd. Apply the .env we just
	// wrote into this process and run from the wizard's dir so the checks see
	// the freshly-written config. Restore afterwards.
	restore := applyEnv(env)
	defer restore()
	prevDir, _ := os.Getwd()
	if w.dir != "." && w.dir != "" {
		if err := os.Chdir(w.dir); err == nil {
			defer os.Chdir(prevDir)
		}
	}
	runDoctorChecks(doctorChecks(), w.out)

	w.println("  Setup complete. Start Cyntr with:")
	if len(env) > 0 {
		w.println("    set -a && source .env && set +a")
	}
	w.println("    cyntr start")
	w.println()
	return nil
}

const defaultPolicyYAML = `rules:
  - name: allow-model-calls
    tenant: "*"
    action: model_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 10

  - name: require-approval-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: require_approval
    priority: 20

  - name: allow-tools
    tenant: "*"
    action: tool_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 5
`

// pickTenant returns the first configured tenant name (deterministically) or
// "default" so a re-run pre-fills the existing tenant.
func pickTenant(cfg config.CyntrConfig) string {
	names := make([]string, 0, len(cfg.Tenants))
	for n := range cfg.Tenants {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 0 {
		return names[0]
	}
	return "default"
}

// detectProviderDefault maps already-set provider env vars to the menu choice
// so a re-run pre-selects the configured provider.
func detectProviderDefault(env map[string]string) string {
	switch {
	case env["ANTHROPIC_API_KEY"] != "":
		return "1"
	case env["OPENAI_API_KEY"] != "":
		return "2"
	case env["AZURE_OPENAI_API_KEY"] != "":
		return "3"
	case env["GEMINI_API_KEY"] != "":
		return "4"
	case env["OPENROUTER_API_KEY"] != "":
		return "5"
	case env["OLLAMA_URL"] != "":
		return "6"
	case env["OAUTH_CLIENT_ID"] != "" || env["OAUTH_PROVIDER"] != "":
		return "7"
	default:
		return "1"
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// masked renders a stored secret as a fixed placeholder so the wizard can offer
// "keep existing" as a default without echoing the real value to the terminal.
const maskToken = "********"

func masked(v string) string {
	if v == "" {
		return ""
	}
	return maskToken
}
func isMask(v string) bool { return v == maskToken }

// --- .env file handling --------------------------------------------------

// readEnvFile parses a KEY=value .env file into a map. Values may be wrapped in
// single or double quotes (as writeEnvFile emits them). Missing file -> empty.
func readEnvFile(path string) map[string]string {
	m := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if len(val) >= 2 {
			if (val[0] == '\'' && val[len(val)-1] == '\'') || (val[0] == '"' && val[len(val)-1] == '"') {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			m[key] = val
		}
	}
	return m
}

// writeEnvFile writes the env map sorted, quoting values that need it. 0600 —
// it holds credentials.
func writeEnvFile(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k, v := range env {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := env[k]
		if strings.ContainsAny(v, " \t&?#'\"") {
			v = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// applyEnv sets each non-empty entry into the process environment and returns a
// function that restores the prior values. Used so the end-of-wizard doctor run
// sees the config we just wrote.
func applyEnv(env map[string]string) func() {
	type prev struct {
		val string
		ok  bool
	}
	saved := map[string]prev{}
	for k, v := range env {
		if v == "" {
			continue
		}
		old, ok := os.LookupEnv(k)
		saved[k] = prev{old, ok}
		os.Setenv(k, v)
	}
	return func() {
		for k, p := range saved {
			if p.ok {
				os.Setenv(k, p.val)
			} else {
				os.Unsetenv(k)
			}
		}
	}
}
