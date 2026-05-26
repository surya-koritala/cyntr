package agent

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// DistillProviderAdapter adapts an agent.ModelProvider to the leaner
// usermodel.LLMProvider interface. Lives in the agent package because that's
// where the provider concept already exists — usermodel itself stays free
// of any agent imports so the dependency direction holds (agent -> usermodel).
type DistillProviderAdapter struct {
	Provider ModelProvider
}

// NewDistillProviderAdapter wraps p so it satisfies usermodel.LLMProvider.
func NewDistillProviderAdapter(p ModelProvider) *DistillProviderAdapter {
	return &DistillProviderAdapter{Provider: p}
}

func (a *DistillProviderAdapter) Name() string {
	if a.Provider == nil {
		return ""
	}
	return a.Provider.Name()
}

func (a *DistillProviderAdapter) DistillChat(ctx context.Context, msgs []usermodel.DistillMessage) (string, int, int, error) {
	if a.Provider == nil {
		return "", 0, 0, fmt.Errorf("distill adapter: no provider")
	}
	converted := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		var role Role
		switch m.Role {
		case "system":
			role = RoleSystem
		case "assistant":
			role = RoleAssistant
		default:
			role = RoleUser
		}
		converted = append(converted, Message{Role: role, Content: m.Content})
	}
	resp, err := a.Provider.Chat(ctx, converted, nil)
	if err != nil {
		return "", 0, 0, err
	}
	return resp.Content, resp.InputTokens, resp.OutputTokens, nil
}

// BusAuditEmitter adapts the audit emitter (which publishes to the IPC
// "audit.write" topic) to the usermodel.AuditEmitter interface the
// distiller expects.
type BusAuditEmitter struct {
	Bus      *ipc.Bus
	Instance string
}

// NewBusAuditEmitter constructs a BusAuditEmitter. instance is the cyntr
// node id — recorded on every audit entry to disambiguate federated
// deployments.
func NewBusAuditEmitter(bus *ipc.Bus, instance string) *BusAuditEmitter {
	return &BusAuditEmitter{Bus: bus, Instance: instance}
}

// Emit publishes an audit entry for a distill operation. The entry uses the
// "usermodel.distill" action type so audit queries can pull just narrative
// operations without trawling chat events.
func (e *BusAuditEmitter) Emit(action, tenant, user, status string, detail map[string]string) {
	if e == nil || e.Bus == nil {
		return
	}
	em := audit.NewEmitter(e.Bus, e.Instance)
	em.Emit(audit.Entry{
		Tenant: tenant,
		Principal: audit.Principal{User: user, Role: "system"},
		Action: audit.Action{
			Type:   action,
			Module: "usermodel",
			Detail: detail,
		},
		Result: audit.Result{Status: status},
	})
}
