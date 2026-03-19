package audit

import "time"

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
	PrevHash  string         `json:"prev_hash"`
}

type Principal struct {
	User  string `json:"user"`
	Agent string `json:"agent"`
	Role  string `json:"role"`
}

type Action struct {
	Type   string            `json:"type"`
	Module string            `json:"module"`
	Detail map[string]string `json:"detail"`
}

type PolicyDecision struct {
	Rule         string `json:"rule"`
	Decision     string `json:"decision"`
	DecidedBy    string `json:"decided_by"`
	EvaluationMs int    `json:"evaluation_ms"`
}

type Result struct {
	Status     string `json:"status"`
	DurationMs int    `json:"duration_ms"`
}

type ChainInfo struct {
	ParentEvent string `json:"parent_event"`
	Session     string `json:"session"`
}

type QueryFilter struct {
	Tenant     string
	ActionType string
	User       string
	Agent      string
	Since      time.Time
	Until      time.Time
	Limit      int
}
