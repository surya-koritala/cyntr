package policy

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

type Engine struct {
	policyPath string
	ruleSet    *RuleSet
	bus        *ipc.Bus
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
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.bus.Handle("policy", "policy.check", e.handleCheck)
	return nil
}

func (e *Engine) Stop(ctx context.Context) error { return nil }

func (e *Engine) Health(ctx context.Context) kernel.HealthStatus {
	if e.ruleSet == nil {
		return kernel.HealthStatus{Healthy: false, Message: "rules not loaded"}
	}
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("%d rules loaded", len(e.ruleSet.Rules))}
}

func (e *Engine) handleCheck(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(CheckRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected CheckRequest, got %T", msg.Payload)
	}
	resp := e.ruleSet.Evaluate(req)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}
