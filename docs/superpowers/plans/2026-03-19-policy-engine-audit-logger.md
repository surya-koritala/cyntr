# Policy Engine + Audit Logger Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Policy Engine (deny-by-default rule evaluation) and Audit Logger (tamper-evident, append-only logging with hash chains) as kernel modules that communicate via IPC.

**Architecture:** Two kernel modules implementing the `kernel.Module` interface. The Policy Engine handles synchronous `policy.check` IPC requests — given an action, it evaluates YAML-defined rules and returns allow/deny/require_approval. The Audit Logger subscribes to `audit.write` IPC events and persists entries to append-only SQLite with hash chains. It handles synchronous `audit.query` requests for searching logs. Both modules are fail-closed: if the Policy Engine is down, all actions are denied.

**Tech Stack:** Go 1.22+, SQLite via `modernc.org/sqlite` (pure Go, no CGO), YAML via `gopkg.in/yaml.v3` (already a dep).

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Sections 2.1, 2.6, Data Storage)

**Dependencies:** Kernel foundation (Plan 1) must be implemented. This plan works on the `feat/kernel-foundation` branch.

**Deferred to future plans:**
- Asymmetric signing (Ed25519) — this plan uses HMAC-SHA256 for local tamper detection. Asymmetric signing for federation cross-verification comes with Plan 8 (Federation).
- Daily audit DB rotation — this plan creates a single `audit.db`. Daily rotation comes with the CLI `cyntr audit` subcommands.
- Human-in-the-loop approval flow — `RequireApproval` decision is supported in evaluation but the actual approval workflow (dashboard, notifications) comes with Plan 6 (Channel Manager) and Plan 9 (Web UI).
- Policy Engine and Audit Logger have no dependency on each other. They boot independently. Callers (e.g., Proxy Gateway) are responsible for checking policy first, then publishing audit events.

---

## File Structure

```
modules/
├── policy/
│   ├── types.go            # Decision, CheckRequest, CheckResponse, PolicyRule
│   ├── rules.go            # RuleSet: load from YAML, match against requests
│   ├── engine.go           # Module impl: Init, Start (register IPC handler), evaluate
│   ├── types_test.go       # Decision string tests
│   ├── rules_test.go       # Rule matching tests with real YAML
│   └── engine_test.go      # Module + IPC integration tests
├── audit/
│   ├── types.go            # Entry struct matching spec JSON
│   ├── writer.go           # AppendWriter: SQLite, hash chain, signing
│   ├── query.go            # QueryEntries: filter by tenant, time, action
│   ├── logger.go           # Module impl: Init, Start (subscribe events, register query handler)
│   ├── types_test.go       # Entry serialization tests
│   ├── writer_test.go      # Writer tests with real SQLite
│   ├── query_test.go       # Query tests with real SQLite
│   └── logger_test.go      # Module + IPC integration tests
```

---

## Chunk 1: Policy Engine Types + Rule Loading

### Task 1: Define Policy Types

**Files:**
- Create: `modules/policy/types.go`
- Create: `modules/policy/types_test.go`

- [ ] **Step 1: Write failing test for Decision.String()**

Create `modules/policy/types_test.go`:
```go
package policy

import "testing"

func TestDecisionString(t *testing.T) {
	tests := []struct {
		d    Decision
		want string
	}{
		{Allow, "allow"},
		{Deny, "deny"},
		{RequireApproval, "require_approval"},
		{Decision(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("Decision(%d).String() = %q, want %q", int(tt.d), got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -v -count=1`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement policy types**

Create `modules/policy/types.go`:
```go
package policy

import "fmt"

// Decision is the outcome of a policy evaluation.
type Decision int

const (
	Deny            Decision = iota // default — deny if no rule matches
	Allow
	RequireApproval
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case RequireApproval:
		return "require_approval"
	default:
		return fmt.Sprintf("unknown(%d)", int(d))
	}
}

// CheckRequest is sent to the Policy Engine via IPC topic "policy.check".
type CheckRequest struct {
	Tenant string // which tenant is performing the action
	Action string // action type: "tool_call", "model_call", "file_read", "shell_exec", etc.
	Tool   string // specific tool name (e.g., "shell_exec", "http_request")
	Agent  string // agent performing the action
	User   string // user on whose behalf the agent acts
}

// CheckResponse is returned by the Policy Engine.
type CheckResponse struct {
	Decision Decision
	Rule     string // name of the rule that matched (empty if default deny)
	Reason   string // human-readable explanation
}

// PolicyRule defines a single rule loaded from YAML.
type PolicyRule struct {
	Name     string   `yaml:"name"`
	Tenant   string   `yaml:"tenant"`   // "*" matches all tenants
	Action   string   `yaml:"action"`   // "*" matches all actions
	Tool     string   `yaml:"tool"`     // "*" matches all tools
	Agent    string   `yaml:"agent"`    // "*" matches all agents
	Decision Decision `yaml:"-"`        // parsed from DecisionStr
	DecisionStr string `yaml:"decision"` // "allow", "deny", "require_approval"
	Priority int      `yaml:"priority"` // higher = more specific, checked first
}

// PolicyConfig is the top-level YAML structure for policy files.
type PolicyConfig struct {
	Rules []PolicyRule `yaml:"rules"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/policy/types.go modules/policy/types_test.go
git commit -m "feat(policy): define policy types — Decision, CheckRequest, CheckResponse, PolicyRule"
```

---

### Task 2: Implement Rule Loading and Matching

**Files:**
- Create: `modules/policy/rules.go`
- Create: `modules/policy/rules_test.go`

- [ ] **Step 1: Write failing tests for rule loading and matching**

Create `modules/policy/rules_test.go`:
```go
package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicy(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func TestRuleSetLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
`)

	rs, err := LoadRuleSet(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rs.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rs.Rules))
	}
	if rs.Rules[0].Name != "deny-shell" {
		t.Fatalf("expected highest priority first, got %q", rs.Rules[0].Name)
	}
}

func TestRuleSetLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `{{{invalid`)
	_, err := LoadRuleSet(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestRuleSetLoadInvalidDecision(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `
rules:
  - name: bad
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: maybe
    priority: 1
`)
	_, err := LoadRuleSet(path)
	if err == nil {
		t.Fatal("expected error for invalid decision")
	}
}

func TestRuleSetEvaluateDenyByDefault(t *testing.T) {
	rs := &RuleSet{Rules: []PolicyRule{}}
	resp := rs.Evaluate(CheckRequest{
		Tenant: "finance",
		Action: "tool_call",
		Tool:   "shell_exec",
	})
	if resp.Decision != Deny {
		t.Fatalf("expected deny-by-default, got %s", resp.Decision)
	}
	if resp.Rule != "" {
		t.Fatalf("expected empty rule name for default deny, got %q", resp.Rule)
	}
}

func TestRuleSetEvaluateExactMatch(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "allow-http", Tenant: "*", Action: "tool_call", Tool: "http_request", Agent: "*", Decision: Allow, Priority: 10},
		},
	}
	resp := rs.Evaluate(CheckRequest{
		Tenant: "marketing",
		Action: "tool_call",
		Tool:   "http_request",
		Agent:  "assistant",
	})
	if resp.Decision != Allow {
		t.Fatalf("expected allow, got %s", resp.Decision)
	}
	if resp.Rule != "allow-http" {
		t.Fatalf("expected rule 'allow-http', got %q", resp.Rule)
	}
}

func TestRuleSetEvaluateHigherPriorityWins(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "deny-shell", Tenant: "*", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Deny, Priority: 20},
			{Name: "allow-all-tools", Tenant: "*", Action: "tool_call", Tool: "*", Agent: "*", Decision: Allow, Priority: 5},
		},
	}
	resp := rs.Evaluate(CheckRequest{
		Tenant: "finance",
		Action: "tool_call",
		Tool:   "shell_exec",
	})
	if resp.Decision != Deny {
		t.Fatalf("expected deny (higher priority), got %s", resp.Decision)
	}
}

func TestRuleSetEvaluateTenantSpecific(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "finance-deny-shell", Tenant: "finance", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Deny, Priority: 30},
			{Name: "allow-shell", Tenant: "*", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Allow, Priority: 10},
		},
	}

	// Finance: denied
	resp := rs.Evaluate(CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Deny {
		t.Fatalf("finance should be denied, got %s", resp.Decision)
	}

	// Marketing: allowed
	resp = rs.Evaluate(CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Allow {
		t.Fatalf("marketing should be allowed, got %s", resp.Decision)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -v -count=1`
Expected: FAIL — `LoadRuleSet`, `RuleSet`, `Evaluate` not defined

- [ ] **Step 3: Implement RuleSet**

Create `modules/policy/rules.go`:
```go
package policy

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// RuleSet holds loaded policy rules sorted by priority (highest first).
type RuleSet struct {
	Rules []PolicyRule
}

// LoadRuleSet reads a YAML policy file and returns a sorted RuleSet.
func LoadRuleSet(path string) (*RuleSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}

	var cfg PolicyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}

	// Parse decision strings and validate
	for i := range cfg.Rules {
		d, err := parseDecision(cfg.Rules[i].DecisionStr)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", cfg.Rules[i].Name, err)
		}
		cfg.Rules[i].Decision = d
	}

	// Sort by priority descending (highest first)
	sort.Slice(cfg.Rules, func(i, j int) bool {
		return cfg.Rules[i].Priority > cfg.Rules[j].Priority
	})

	return &RuleSet{Rules: cfg.Rules}, nil
}

// Evaluate checks a request against all rules and returns the decision.
// Rules are checked in priority order. First match wins. Default: deny.
func (rs *RuleSet) Evaluate(req CheckRequest) CheckResponse {
	for _, rule := range rs.Rules {
		if matches(rule, req) {
			return CheckResponse{
				Decision: rule.Decision,
				Rule:     rule.Name,
				Reason:   fmt.Sprintf("matched rule %q", rule.Name),
			}
		}
	}

	return CheckResponse{
		Decision: Deny,
		Rule:     "",
		Reason:   "no matching rule — deny by default",
	}
}

func matches(rule PolicyRule, req CheckRequest) bool {
	if rule.Tenant != "*" && rule.Tenant != req.Tenant {
		return false
	}
	if rule.Action != "*" && rule.Action != req.Action {
		return false
	}
	if rule.Tool != "*" && rule.Tool != req.Tool {
		return false
	}
	if rule.Agent != "*" && rule.Agent != req.Agent {
		return false
	}
	return true
}

func parseDecision(s string) (Decision, error) {
	switch s {
	case "allow":
		return Allow, nil
	case "deny":
		return Deny, nil
	case "require_approval":
		return RequireApproval, nil
	default:
		return Deny, fmt.Errorf("invalid decision %q: must be allow, deny, or require_approval", s)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/policy/rules.go modules/policy/rules_test.go
git commit -m "feat(policy): implement RuleSet with YAML loading and priority-based evaluation"
```

---

## Chunk 2: Policy Engine Module

### Task 3: Implement Policy Engine as Kernel Module

**Files:**
- Create: `modules/policy/engine.go`
- Create: `modules/policy/engine_test.go`

- [ ] **Step 1: Write failing test for the engine module**

Create `modules/policy/engine_test.go`:
```go
package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestEngineImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Engine)(nil)
}

func TestEngineHandlesPolicyCheck(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
`), 0644)

	bus := ipc.NewBus()
	defer bus.Close()

	engine := NewEngine(policyPath)
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	if err := engine.Init(ctx, svc); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer engine.Stop(ctx)

	// Test: allow http_request
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy",
		Target: "policy",
		Topic:  "policy.check",
		Payload: CheckRequest{
			Tenant: "marketing",
			Action: "tool_call",
			Tool:   "http_request",
			Agent:  "assistant",
		},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	checkResp, ok := resp.Payload.(CheckResponse)
	if !ok {
		t.Fatalf("expected CheckResponse, got %T", resp.Payload)
	}
	if checkResp.Decision != Allow {
		t.Fatalf("expected allow for http_request, got %s", checkResp.Decision)
	}

	// Test: deny shell_exec
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source:  "proxy",
		Target:  "policy",
		Topic:   "policy.check",
		Payload: CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	checkResp = resp.Payload.(CheckResponse)
	if checkResp.Decision != Deny {
		t.Fatalf("expected deny for shell_exec, got %s", checkResp.Decision)
	}
}

func TestEngineHealthy(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte("rules: []\n"), 0644)

	bus := ipc.NewBus()
	defer bus.Close()

	engine := NewEngine(policyPath)
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	engine.Init(ctx, svc)
	engine.Start(ctx)
	defer engine.Stop(ctx)

	health := engine.Health(ctx)
	if !health.Healthy {
		t.Fatal("expected healthy engine")
	}
}

func TestEngineInitFailsBadPolicy(t *testing.T) {
	engine := NewEngine("/nonexistent/policy.yaml")
	bus := ipc.NewBus()
	defer bus.Close()

	ctx := context.Background()
	err := engine.Init(ctx, &kernel.Services{Bus: bus})
	if err == nil {
		t.Fatal("expected error for missing policy file")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -run TestEngine -v -count=1`
Expected: FAIL — `Engine`, `NewEngine` not defined

- [ ] **Step 3: Implement engine module**

Create `modules/policy/engine.go`:
```go
package policy

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Engine is the Policy Engine kernel module.
// It evaluates policy rules for every action in the system.
type Engine struct {
	policyPath string
	ruleSet    *RuleSet
	bus        *ipc.Bus
}

// NewEngine creates a new Policy Engine module.
func NewEngine(policyPath string) *Engine {
	return &Engine{policyPath: policyPath}
}

func (e *Engine) Name() string           { return "policy" }
func (e *Engine) Dependencies() []string { return nil }

func (e *Engine) Init(ctx context.Context, svc *kernel.Services) error {
	e.bus = svc.Bus

	rs, err := LoadRuleSet(e.policyPath)
	if err != nil {
		return fmt.Errorf("policy engine init: %w", err)
	}
	e.ruleSet = rs
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.bus.Handle("policy", "policy.check", e.handleCheck)
	return nil
}

func (e *Engine) Stop(ctx context.Context) error {
	return nil
}

func (e *Engine) Health(ctx context.Context) kernel.HealthStatus {
	if e.ruleSet == nil {
		return kernel.HealthStatus{Healthy: false, Message: "rules not loaded"}
	}
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d rules loaded", len(e.ruleSet.Rules)),
	}
}

func (e *Engine) handleCheck(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(CheckRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected CheckRequest, got %T", msg.Payload)
	}

	resp := e.ruleSet.Evaluate(req)

	return ipc.Message{
		Type:    ipc.MessageTypeResponse,
		Payload: resp,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/policy/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/policy/engine.go modules/policy/engine_test.go
git commit -m "feat(policy): implement Policy Engine kernel module with IPC handler"
```

---

## Chunk 3: Audit Logger Types + Writer

### Task 4: Define Audit Entry Types

**Files:**
- Create: `modules/audit/types.go`
- Create: `modules/audit/types_test.go`

- [ ] **Step 1: Write failing test**

Create `modules/audit/types_test.go`:
```go
package audit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEntryMarshalJSON(t *testing.T) {
	e := Entry{
		ID:        "evt_test123",
		Timestamp: time.Date(2026, 3, 19, 14, 32, 1, 0, time.UTC),
		Instance:  "cyntr-test",
		Tenant:    "finance",
		Principal: Principal{User: "jane@corp.com", Agent: "analyst", Role: "team_lead"},
		Action:    Action{Type: "tool_call", Module: "agent_runtime", Detail: map[string]string{"tool": "shell_exec"}},
		Policy:    PolicyDecision{Rule: "finance-readonly", Decision: "deny", DecidedBy: "policy_engine", EvaluationMs: 2},
		Result:    Result{Status: "denied", DurationMs: 0},
		Chain:     ChainInfo{ParentEvent: "", Session: "sess_abc"},
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded["id"] != "evt_test123" {
		t.Fatalf("expected id 'evt_test123', got %v", decoded["id"])
	}
	if decoded["tenant"] != "finance" {
		t.Fatalf("expected tenant 'finance', got %v", decoded["tenant"])
	}
}

func TestQueryFilterString(t *testing.T) {
	q := QueryFilter{Tenant: "finance", ActionType: "tool_call"}
	if q.Tenant != "finance" {
		t.Fatal("expected tenant finance")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement audit types**

Create `modules/audit/types.go`:
```go
package audit

import "time"

// Entry is a single audit log entry matching the spec's JSON structure.
type Entry struct {
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Instance  string         `json:"instance"`
	Tenant    string         `json:"tenant"`
	Principal Principal      `json:"principal"`
	Action    Action         `json:"action"`
	Policy    PolicyDecision `json:"policy"`
	Result    Result         `json:"result"`
	Chain     ChainInfo      `json:"chain"`
	Signature string         `json:"signature"`
	PrevHash  string         `json:"prev_hash"` // hash of previous entry for chain
}

// Principal identifies who performed the action.
type Principal struct {
	User  string `json:"user"`
	Agent string `json:"agent"`
	Role  string `json:"role"`
}

// Action describes what was done.
type Action struct {
	Type   string            `json:"type"`
	Module string            `json:"module"`
	Detail map[string]string `json:"detail"`
}

// PolicyDecision records the policy evaluation result.
type PolicyDecision struct {
	Rule         string `json:"rule"`
	Decision     string `json:"decision"`
	DecidedBy    string `json:"decided_by"`
	EvaluationMs int    `json:"evaluation_ms"`
}

// Result records what happened after the action.
type Result struct {
	Status     string `json:"status"`
	DurationMs int    `json:"duration_ms"`
}

// ChainInfo links this entry to its parent event and session.
type ChainInfo struct {
	ParentEvent string `json:"parent_event"`
	Session     string `json:"session"`
}

// QueryFilter defines search criteria for audit log queries.
type QueryFilter struct {
	Tenant     string
	ActionType string
	User       string
	Agent      string
	Since      time.Time
	Until      time.Time
	Limit      int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/audit/types.go modules/audit/types_test.go
git commit -m "feat(audit): define audit entry types matching spec JSON structure"
```

---

### Task 5: Implement Append-Only Writer with Hash Chain

**Files:**
- Create: `modules/audit/writer.go`
- Create: `modules/audit/writer_test.go`

- [ ] **Step 1: Add SQLite dependency**

Run: `cd /Users/suryakoritala/Cyntr && go get modernc.org/sqlite`

- [ ] **Step 2: Write failing tests for the writer**

Create `modules/audit/writer_test.go`:
```go
package audit

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterCreateAndWrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	entry := Entry{
		ID:        "evt_001",
		Timestamp: time.Now().UTC(),
		Instance:  "test",
		Tenant:    "finance",
		Principal: Principal{User: "jane@corp.com", Agent: "analyst", Role: "user"},
		Action:    Action{Type: "tool_call", Module: "runtime", Detail: map[string]string{"tool": "shell"}},
		Policy:    PolicyDecision{Rule: "test-rule", Decision: "allow", DecidedBy: "policy_engine"},
		Result:    Result{Status: "success", DurationMs: 100},
	}

	if err := w.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWriterHashChain(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	for i := 0; i < 3; i++ {
		entry := Entry{
			ID:        fmt.Sprintf("evt_%03d", i),
			Timestamp: time.Now().UTC(),
			Tenant:    "finance",
			Action:    Action{Type: "test"},
		}
		if err := w.Write(entry); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Verify chain integrity
	if err := w.VerifyChain(); err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}
}

func TestWriterChainDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	for i := 0; i < 3; i++ {
		w.Write(Entry{
			ID:     fmt.Sprintf("evt_%03d", i),
			Timestamp: time.Now().UTC(),
			Tenant: "finance",
			Action: Action{Type: "test"},
		})
	}

	// Tamper with the data column directly in SQLite to break the hash chain
	_, err = w.db.Exec("UPDATE audit_entries SET data = replace(data, 'finance', 'hacked') WHERE id = 'evt_001'")
	if err != nil {
		t.Fatalf("tamper: %v", err)
	}

	// Verify should detect the tampering
	err = w.VerifyChain()
	if err == nil {
		t.Fatal("expected chain verification to detect tampering")
	}
}

func TestWriterAppendOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	w.Write(Entry{ID: "evt_001", Timestamp: time.Now().UTC(), Tenant: "t", Action: Action{Type: "test"}})

	// Count entries
	var count int
	w.db.QueryRow("SELECT COUNT(*) FROM audit_entries").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 entry, got %d", count)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -run TestWriter -v -count=1`
Expected: FAIL — `NewWriter`, `Writer` not defined

- [ ] **Step 4: Implement the writer**

Create `modules/audit/writer.go`:
```go
package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// Writer is an append-only audit log writer with hash chain integrity.
type Writer struct {
	mu       sync.Mutex
	db       *sql.DB
	instance string
	secret   []byte    // HMAC key for signing entries
	prevHash string    // hash of most recent entry
}

// NewWriter creates a new audit log writer backed by SQLite.
func NewWriter(dbPath, instance, secret string) (*Writer, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// Create table if not exists
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_entries (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			instance TEXT NOT NULL,
			tenant TEXT NOT NULL,
			data TEXT NOT NULL,
			signature TEXT NOT NULL,
			prev_hash TEXT NOT NULL,
			entry_hash TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	// Load the last entry's hash to continue the chain
	var prevHash string
	err = db.QueryRow("SELECT entry_hash FROM audit_entries ORDER BY rowid DESC LIMIT 1").Scan(&prevHash)
	if err == sql.ErrNoRows {
		prevHash = "genesis"
	} else if err != nil {
		db.Close()
		return nil, fmt.Errorf("load last hash: %w", err)
	}

	return &Writer{
		db:       db,
		instance: instance,
		secret:   []byte(secret),
		prevHash: prevHash,
	}, nil
}

// Write appends an entry to the audit log with hash chain and signature.
func (w *Writer) Write(entry Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry.Instance = w.instance
	entry.PrevHash = w.prevHash

	// Serialize entry for hashing (without signature and hash fields)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	// Compute hash: SHA256(prev_hash + entry_data)
	h := sha256.New()
	h.Write([]byte(w.prevHash))
	h.Write(data)
	entryHash := hex.EncodeToString(h.Sum(nil))

	// Sign the hash with HMAC
	mac := hmac.New(sha256.New, w.secret)
	mac.Write([]byte(entryHash))
	signature := hex.EncodeToString(mac.Sum(nil))

	entry.Signature = signature

	// Re-marshal with signature included for storage
	data, err = json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal signed entry: %w", err)
	}

	// Insert into SQLite
	_, err = w.db.Exec(
		"INSERT INTO audit_entries (id, timestamp, instance, tenant, data, signature, prev_hash, entry_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		entry.ID,
		entry.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		entry.Instance,
		entry.Tenant,
		string(data),
		signature,
		w.prevHash,
		entryHash,
	)
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}

	w.prevHash = entryHash
	return nil
}

// VerifyChain checks the integrity of the hash chain.
// Returns nil if intact, error describing the first broken link.
func (w *Writer) VerifyChain() error {
	rows, err := w.db.Query("SELECT id, data, prev_hash, entry_hash FROM audit_entries ORDER BY rowid ASC")
	if err != nil {
		return fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	expectedPrev := "genesis"

	for rows.Next() {
		var id, data, prevHash, entryHash string
		if err := rows.Scan(&id, &data, &prevHash, &entryHash); err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		if prevHash != expectedPrev {
			return fmt.Errorf("chain broken at %s: prev_hash mismatch (expected %s, got %s)", id, expectedPrev, prevHash)
		}

		// Recompute hash
		h := sha256.New()
		h.Write([]byte(prevHash))
		h.Write([]byte(data))
		computed := hex.EncodeToString(h.Sum(nil))

		if computed != entryHash {
			return fmt.Errorf("chain broken at %s: entry_hash mismatch (computed %s, stored %s)", id, computed, entryHash)
		}

		expectedPrev = entryHash
	}

	return rows.Err()
}

// Close closes the underlying database.
func (w *Writer) Close() error {
	return w.db.Close()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -run TestWriter -v -count=1 -race`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add modules/audit/writer.go modules/audit/writer_test.go go.mod go.sum
git commit -m "feat(audit): implement append-only writer with hash chain and tamper detection"
```

---

## Chunk 4: Audit Query + Logger Module

### Task 6: Implement Audit Query

**Files:**
- Create: `modules/audit/query.go`
- Create: `modules/audit/query_test.go`

- [ ] **Step 1: Write failing tests for query**

Create `modules/audit/query_test.go`:
```go
package audit

import (
	"path/filepath"
	"testing"
	"time"
)

func seedEntries(t *testing.T, w *Writer) {
	t.Helper()
	entries := []Entry{
		{ID: "evt_001", Timestamp: time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC), Tenant: "finance", Principal: Principal{User: "jane@corp.com"}, Action: Action{Type: "tool_call"}},
		{ID: "evt_002", Timestamp: time.Date(2026, 3, 19, 11, 0, 0, 0, time.UTC), Tenant: "finance", Principal: Principal{User: "bob@corp.com"}, Action: Action{Type: "model_call"}},
		{ID: "evt_003", Timestamp: time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC), Tenant: "marketing", Principal: Principal{User: "jane@corp.com"}, Action: Action{Type: "tool_call"}},
	}
	for _, e := range entries {
		if err := w.Write(e); err != nil {
			t.Fatalf("seed %s: %v", e.ID, err)
		}
	}
}

func TestQueryByTenant(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)

	results, err := QueryEntries(w.db, QueryFilter{Tenant: "finance"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 finance entries, got %d", len(results))
	}
}

func TestQueryByActionType(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)

	results, err := QueryEntries(w.db, QueryFilter{ActionType: "tool_call"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tool_call entries, got %d", len(results))
	}
}

func TestQueryByTimeRange(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)

	results, err := QueryEntries(w.db, QueryFilter{
		Since: time.Date(2026, 3, 19, 10, 30, 0, 0, time.UTC),
		Until: time.Date(2026, 3, 19, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry in time range, got %d", len(results))
	}
	if results[0].ID != "evt_002" {
		t.Fatalf("expected evt_002, got %s", results[0].ID)
	}
}

func TestQueryWithLimit(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)

	results, err := QueryEntries(w.db, QueryFilter{Limit: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 entry with limit, got %d", len(results))
	}
}

func TestQueryNoResults(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)

	results, err := QueryEntries(w.db, QueryFilter{Tenant: "nonexistent"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -run TestQuery -v -count=1`
Expected: FAIL — `QueryEntries` not defined

- [ ] **Step 3: Implement query**

Create `modules/audit/query.go`:
```go
package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// QueryEntries searches audit log entries matching the given filter.
func QueryEntries(db *sql.DB, filter QueryFilter) ([]Entry, error) {
	var conditions []string
	var args []any

	if filter.Tenant != "" {
		conditions = append(conditions, "tenant = ?")
		args = append(args, filter.Tenant)
	}
	if filter.ActionType != "" {
		conditions = append(conditions, "json_extract(data, '$.action.type') = ?")
		args = append(args, filter.ActionType)
	}
	if filter.User != "" {
		conditions = append(conditions, "json_extract(data, '$.principal.user') = ?")
		args = append(args, filter.User)
	}
	if filter.Agent != "" {
		conditions = append(conditions, "json_extract(data, '$.principal.agent') = ?")
		args = append(args, filter.Agent)
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if !filter.Until.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.Until.UTC().Format("2006-01-02T15:04:05.000Z"))
	}

	query := "SELECT data FROM audit_entries"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY rowid ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		var entry Entry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			return nil, fmt.Errorf("unmarshal entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if entries == nil {
		entries = []Entry{}
	}

	return entries, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -run TestQuery -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/audit/query.go modules/audit/query_test.go
git commit -m "feat(audit): implement query with filtering by tenant, action, time, and user"
```

---

### Task 7: Implement Audit Logger as Kernel Module

**Files:**
- Create: `modules/audit/logger.go`
- Create: `modules/audit/logger_test.go`

- [ ] **Step 1: Write failing tests for the logger module**

Create `modules/audit/logger_test.go`:
```go
package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestLoggerImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Logger)(nil)
}

func TestLoggerWritesViaIPC(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	bus := ipc.NewBus()
	defer bus.Close()

	logger := NewLogger(dbPath, "test-instance", "test-secret")
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	if err := logger.Init(ctx, svc); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := logger.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer logger.Stop(ctx)

	// Publish an audit event
	bus.Publish(ipc.Message{
		Source: "policy",
		Target: "*",
		Type:   ipc.MessageTypeEvent,
		Topic:  "audit.write",
		Payload: Entry{
			ID:        "evt_ipc_001",
			Timestamp: time.Now().UTC(),
			Tenant:    "finance",
			Action:    Action{Type: "policy_check"},
			Policy:    PolicyDecision{Decision: "allow"},
		},
	})

	// Give the async subscriber time to process
	time.Sleep(200 * time.Millisecond)

	// Query via IPC
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source:  "cli",
		Target:  "audit",
		Topic:   "audit.query",
		Payload: QueryFilter{Tenant: "finance"},
	})
	if err != nil {
		t.Fatalf("query request: %v", err)
	}

	entries, ok := resp.Payload.([]Entry)
	if !ok {
		t.Fatalf("expected []Entry, got %T", resp.Payload)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "evt_ipc_001" {
		t.Fatalf("expected evt_ipc_001, got %s", entries[0].ID)
	}
}

func TestLoggerHealthy(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	bus := ipc.NewBus()
	defer bus.Close()

	logger := NewLogger(dbPath, "test-instance", "secret")
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	logger.Init(ctx, svc)
	logger.Start(ctx)
	defer logger.Stop(ctx)

	health := logger.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy, got: %s", health.Message)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -run TestLogger -v -count=1`
Expected: FAIL — `Logger`, `NewLogger` not defined

- [ ] **Step 3: Implement logger module**

Create `modules/audit/logger.go`:
```go
package audit

import (
	"context"
	"fmt"
	"os"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Logger is the Audit Logger kernel module.
// It subscribes to audit events and persists them to an append-only store.
type Logger struct {
	dbPath   string
	instance string
	secret   string
	writer   *Writer
	bus      *ipc.Bus
	sub      *ipc.Subscription
}

// NewLogger creates a new Audit Logger module.
func NewLogger(dbPath, instance, secret string) *Logger {
	return &Logger{
		dbPath:   dbPath,
		instance: instance,
		secret:   secret,
	}
}

func (l *Logger) Name() string           { return "audit" }
func (l *Logger) Dependencies() []string { return nil }

func (l *Logger) Init(ctx context.Context, svc *kernel.Services) error {
	l.bus = svc.Bus

	w, err := NewWriter(l.dbPath, l.instance, l.secret)
	if err != nil {
		return fmt.Errorf("audit logger init: %w", err)
	}
	l.writer = w
	return nil
}

func (l *Logger) Start(ctx context.Context) error {
	// Subscribe to audit.write events (async)
	l.sub = l.bus.Subscribe("audit", "audit.write", l.handleWrite)

	// Register query handler (sync request/reply)
	l.bus.Handle("audit", "audit.query", l.handleQuery)

	return nil
}

func (l *Logger) Stop(ctx context.Context) error {
	if l.sub != nil {
		l.sub.Cancel()
	}
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

func (l *Logger) Health(ctx context.Context) kernel.HealthStatus {
	if l.writer == nil {
		return kernel.HealthStatus{Healthy: false, Message: "writer not initialized"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "audit logger running"}
}

func (l *Logger) handleWrite(msg ipc.Message) (ipc.Message, error) {
	entry, ok := msg.Payload.(Entry)
	if !ok {
		// Log error — pub/sub return values are discarded by the bus
		fmt.Fprintf(os.Stderr, "audit: expected Entry, got %T\n", msg.Payload)
		return ipc.Message{}, nil
	}

	if err := l.writer.Write(entry); err != nil {
		// Log error — pub/sub return values are discarded by the bus
		fmt.Fprintf(os.Stderr, "audit: write failed: %v\n", err)
		return ipc.Message{}, nil
	}

	return ipc.Message{}, nil
}

func (l *Logger) handleQuery(msg ipc.Message) (ipc.Message, error) {
	filter, ok := msg.Payload.(QueryFilter)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected QueryFilter, got %T", msg.Payload)
	}

	entries, err := QueryEntries(l.writer.db, filter)
	if err != nil {
		return ipc.Message{}, fmt.Errorf("query audit: %w", err)
	}

	return ipc.Message{
		Type:    ipc.MessageTypeResponse,
		Payload: entries,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/audit/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/audit/logger.go modules/audit/logger_test.go
git commit -m "feat(audit): implement Audit Logger kernel module with IPC event subscription and query handler"
```

---

## Chunk 5: Integration Test + Final Verification

### Task 8: Policy + Audit Integration Test

**Files:**
- Create: `tests/integration/policy_audit_test.go`

- [ ] **Step 1: Write integration test where policy check triggers audit log**

Create `tests/integration/policy_audit_test.go`:
```go
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestPolicyCheckAndAuditLog(t *testing.T) {
	dir := t.TempDir()

	// Write policy file
	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
`), 0644)

	// Write config file
	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	// Set up kernel
	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Create modules
	policyEngine := policy.NewEngine(policyPath)
	auditLogger := audit.NewLogger(
		filepath.Join(dir, "audit.db"),
		"test-instance",
		"test-secret",
	)

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(auditLogger); err != nil {
		t.Fatalf("register audit: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	// Get the bus from a module — we need to access it directly
	// Simulate what Proxy Gateway would do: check policy, then log result
	bus := getBus(t, k)

	// Step 1: Check policy
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy",
		Target: "policy",
		Topic:  "policy.check",
		Payload: policy.CheckRequest{
			Tenant: "marketing",
			Action: "tool_call",
			Tool:   "http_request",
			Agent:  "assistant",
			User:   "alice@corp.com",
		},
	})
	if err != nil {
		t.Fatalf("policy check: %v", err)
	}

	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}

	// Step 2: Log the policy decision to audit
	bus.Publish(ipc.Message{
		Source: "proxy",
		Target: "*",
		Type:   ipc.MessageTypeEvent,
		Topic:  "audit.write",
		Payload: audit.Entry{
			ID:        "evt_integration_001",
			Timestamp: time.Now().UTC(),
			Tenant:    "marketing",
			Principal: audit.Principal{User: "alice@corp.com", Agent: "assistant"},
			Action:    audit.Action{Type: "tool_call", Detail: map[string]string{"tool": "http_request"}},
			Policy:    audit.PolicyDecision{Rule: checkResp.Rule, Decision: checkResp.Decision.String(), DecidedBy: "policy_engine"},
			Result:    audit.Result{Status: "success"},
		},
	})

	// Wait for async write
	time.Sleep(300 * time.Millisecond)

	// Step 3: Query audit log
	auditResp, err := bus.Request(reqCtx, ipc.Message{
		Source:  "cli",
		Target:  "audit",
		Topic:   "audit.query",
		Payload: audit.QueryFilter{Tenant: "marketing"},
	})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}

	entries := auditResp.Payload.([]audit.Entry)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].Policy.Decision != "allow" {
		t.Fatalf("expected policy decision 'allow' in audit, got %q", entries[0].Policy.Decision)
	}
	if entries[0].Principal.User != "alice@corp.com" {
		t.Fatalf("expected user alice, got %q", entries[0].Principal.User)
	}
}

// getBus extracts the IPC bus from the kernel for testing.
// In production, modules access it via Services. For integration tests,
// we need direct access to simulate other modules.
func getBus(t *testing.T, k *kernel.Kernel) *ipc.Bus {
	t.Helper()
	return k.Bus()
}
```

- [ ] **Step 2: Add Bus() accessor to kernel**

Add to `kernel/kernel.go`:
```go
// Bus returns the IPC bus for integration testing and CLI use.
func (k *Kernel) Bus() *ipc.Bus {
	return k.services.Bus
}
```

- [ ] **Step 3: Run integration test**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tests/integration/ -v -count=1 -race`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add tests/integration/policy_audit_test.go kernel/kernel.go
git commit -m "feat: add integration test — policy check triggers audit log via IPC"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -v -count=1 -race`
Expected: All PASS across all packages

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`
Expected: No issues

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`

- [ ] **Step 4: Verify file structure**

Run: `cd /Users/suryakoritala/Cyntr && find . -name '*.go' -path '*/modules/*' -o -name '*.go' -path '*/tests/*' | sort`
Expected:
```
./modules/audit/logger.go
./modules/audit/logger_test.go
./modules/audit/query.go
./modules/audit/query_test.go
./modules/audit/types.go
./modules/audit/types_test.go
./modules/audit/writer.go
./modules/audit/writer_test.go
./modules/policy/engine.go
./modules/policy/engine_test.go
./modules/policy/rules.go
./modules/policy/rules_test.go
./modules/policy/types.go
./modules/policy/types_test.go
./tests/integration/policy_audit_test.go
```

- [ ] **Step 5: Verify clean git status**

Run: `git status`
Expected: Clean working tree
