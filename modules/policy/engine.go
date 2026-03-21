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
	ruleSet    *RuleSet
	bus        *ipc.Bus
	approvals  *ApprovalQueue
}

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
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("%d rules loaded", len(e.ruleSet.Rules))}
}

func (e *Engine) handleListRules(msg ipc.Message) (ipc.Message, error) {
	if e.ruleSet == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []PolicyRule{}}, nil
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: e.ruleSet.Rules}, nil
}

func (e *Engine) handleApprovalList(msg ipc.Message) (ipc.Message, error) {
	pending := e.approvals.ListPending()
	if pending == nil {
		pending = []ApprovalRequest{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: pending}, nil
}

func (e *Engine) handleApprovalApprove(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	if err := e.approvals.Approve(params["id"], params["decided_by"]); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "approved"}, nil
}

func (e *Engine) handleApprovalDeny(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	if err := e.approvals.Deny(params["id"], params["decided_by"]); err != nil {
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
	return ipc.Message{}, nil
}

func (e *Engine) handleCheck(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(CheckRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected CheckRequest, got %T", msg.Payload)
	}
	start := time.Now()
	resp := e.ruleSet.Evaluate(req)
	dur := time.Since(start)
	if dur > 100*time.Millisecond {
		logger.Warn("slow policy evaluation", map[string]any{
			"tenant": req.Tenant, "tool": req.Tool, "duration_ms": dur.Milliseconds(),
		})
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}
