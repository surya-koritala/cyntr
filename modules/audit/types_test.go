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
