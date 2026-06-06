package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
)

var logger = log.Default().WithModule("policy")

type Engine struct {
	policyPath string
	regoPath   string
	ruleSet    *RuleSet
	rego       *RegoEvaluator
	bus        *ipc.Bus
	approvals  *ApprovalQueue
}

// NewEngine constructs a policy engine.
//
//   - yamlPath: path to the YAML PolicyConfig (required)
//   - regoPath: path to a .rego file or directory of .rego files; "" disables Rego.
//
// When both are loaded, YAML is evaluated first. If YAML returns Allow and
// Rego denies, the deny wins (fail-closed composition). YAML denies short-circuit.
func NewEngine(yamlPath, regoPath string) *Engine {
	return &Engine{policyPath: yamlPath, regoPath: regoPath}
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
	rg, err := LoadRegoPolicy(e.regoPath)
	if err != nil {
		return fmt.Errorf("policy engine init (rego): %w", err)
	}
	e.rego = rg
	e.approvals = NewApprovalQueue(0)
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.bus.Handle("policy", "policy.check", e.handleCheck)
	e.bus.Handle("policy", "policy.list", e.handleListRules)
	e.bus.Handle("policy", "policy.approval.submit", e.handleApprovalSubmit)
	e.bus.Handle("policy", "approval.list", e.handleApprovalList)
	e.bus.Handle("policy", "approval.approve", e.handleApprovalApprove)
	e.bus.Handle("policy", "approval.deny", e.handleApprovalDeny)
	e.bus.Handle("policy", "approval.get", e.handleApprovalGet)
	e.bus.Subscribe("policy", "config.reloaded", e.handleConfigReload)
	return nil
}

func (e *Engine) Stop(ctx context.Context) error { return nil }

func (e *Engine) Health(ctx context.Context) kernel.HealthStatus {
	if e.ruleSet == nil {
		return kernel.HealthStatus{Healthy: false, Message: "rules not loaded"}
	}
	msg := fmt.Sprintf("%d rules loaded", len(e.ruleSet.Rules))
	if e.rego != nil {
		msg += fmt.Sprintf(" + rego (%d files)", len(e.rego.Sources()))
	}
	return kernel.HealthStatus{Healthy: true, Message: msg}
}

func (e *Engine) handleListRules(msg ipc.Message) (ipc.Message, error) {
	if e.ruleSet == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []PolicyRule{}}, nil
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: e.ruleSet.Rules}, nil
}

// authorizedDecisionSources is the fail-closed allowlist of bus sources that
// may approve/deny or enumerate approvals. These privileged decisions are
// reachable on the in-process bus by any sender, so we restrict them to the
// authenticated front doors (API/CLI) that establish the caller's identity and
// authorization before forwarding. Arbitrary modules/tools must not decide
// approvals on their own behalf.
var authorizedDecisionSources = map[string]bool{
	"api": true,
	"cli": true,
}

// authorizedStatusSources may read an approval's status (non-mutating). The
// agent runtime polls this while a turn is blocked waiting for a decision.
var authorizedStatusSources = map[string]bool{
	"api":           true,
	"cli":           true,
	"agent_runtime": true,
}

func (e *Engine) handleApprovalList(msg ipc.Message) (ipc.Message, error) {
	if !authorizedDecisionSources[msg.Source] {
		return ipc.Message{}, fmt.Errorf("approval.list: not authorized for source %q", msg.Source)
	}
	pending := e.approvals.ListPending()
	if pending == nil {
		pending = []ApprovalRequest{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: pending}, nil
}

func (e *Engine) handleApprovalApprove(msg ipc.Message) (ipc.Message, error) {
	if !authorizedDecisionSources[msg.Source] {
		return ipc.Message{}, fmt.Errorf("approval.approve: not authorized for source %q", msg.Source)
	}
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	// decided_by must identify the authenticated principal that the front door
	// resolved; never allow an anonymous/unattributable decision.
	decidedBy := params["decided_by"]
	if decidedBy == "" {
		return ipc.Message{}, fmt.Errorf("approval.approve: decided_by (authenticated principal) is required")
	}
	if err := e.approvals.Approve(params["id"], decidedBy); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "approved"}, nil
}

func (e *Engine) handleApprovalDeny(msg ipc.Message) (ipc.Message, error) {
	if !authorizedDecisionSources[msg.Source] {
		return ipc.Message{}, fmt.Errorf("approval.deny: not authorized for source %q", msg.Source)
	}
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	decidedBy := params["decided_by"]
	if decidedBy == "" {
		return ipc.Message{}, fmt.Errorf("approval.deny: decided_by (authenticated principal) is required")
	}
	if err := e.approvals.Deny(params["id"], decidedBy); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "denied"}, nil
}

func (e *Engine) handleApprovalSubmit(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	id := e.approvals.Submit(ApprovalRequest{
		Tenant: params["tenant"],
		Agent:  params["agent"],
		User:   params["user"],
		Tool:   params["tool"],
		Action: params["action"],
	})
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: id}, nil
}

func (e *Engine) handleApprovalGet(msg ipc.Message) (ipc.Message, error) {
	if !authorizedStatusSources[msg.Source] {
		return ipc.Message{}, fmt.Errorf("approval.get: not authorized for source %q", msg.Source)
	}
	id, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}
	req, found := e.approvals.Get(id)
	if !found {
		return ipc.Message{}, fmt.Errorf("approval %q not found", id)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: req.Status.String()}, nil
}

func (e *Engine) handleConfigReload(msg ipc.Message) (ipc.Message, error) {
	rs, err := LoadRuleSet(e.policyPath)
	if err != nil {
		logger.Error("policy reload failed", map[string]any{"error": err.Error()})
		return ipc.Message{}, nil
	}
	e.ruleSet = rs
	logger.Info("policy rules reloaded", map[string]any{"count": len(rs.Rules)})

	rg, err := LoadRegoPolicy(e.regoPath)
	if err != nil {
		logger.Error("rego policy reload failed", map[string]any{"error": err.Error()})
		// keep existing rego evaluator on failure — fail closed against broken reload
		return ipc.Message{}, nil
	}
	e.rego = rg
	if rg != nil {
		logger.Info("rego policy reloaded", map[string]any{"files": len(rg.Sources())})
	} else {
		logger.Info("rego policy disabled after reload", nil)
	}
	return ipc.Message{}, nil
}

func (e *Engine) handleCheck(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(CheckRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected CheckRequest, got %T", msg.Payload)
	}
	start := time.Now()
	resp := e.evaluate(req)
	dur := time.Since(start)
	if dur > 100*time.Millisecond {
		logger.Warn("slow policy evaluation", map[string]any{
			"tenant": req.Tenant, "tool": req.Tool, "duration_ms": dur.Milliseconds(),
		})
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}

// evaluate composes YAML and Rego decisions with fail-closed semantics:
//   - YAML evaluates first.
//   - If YAML denies → short-circuit, return YAML deny.
//   - If YAML allows or requires approval AND a Rego evaluator is loaded,
//     run Rego. If Rego denies, deny wins. Otherwise the stricter of the
//     two decisions stands (RequireApproval > Allow).
func (e *Engine) evaluate(req CheckRequest) CheckResponse {
	yamlResp := e.ruleSet.Evaluate(req)
	if yamlResp.Decision == Deny {
		return yamlResp
	}
	if e.rego == nil {
		return yamlResp
	}
	regoResp := e.rego.Evaluate(context.Background(), req)
	if regoResp.Decision == Deny {
		return regoResp
	}
	// Both non-deny: pick the stricter (RequireApproval wins over Allow).
	if regoResp.Decision == RequireApproval || yamlResp.Decision == RequireApproval {
		if yamlResp.Decision == RequireApproval {
			return yamlResp
		}
		return regoResp
	}
	return yamlResp
}
