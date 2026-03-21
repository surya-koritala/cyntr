package policy

import "fmt"

type Decision int

const (
	Deny            Decision = iota
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

type CheckRequest struct {
	Tenant string
	Action string
	Tool   string
	Agent  string
	User   string
}

type CheckResponse struct {
	Decision Decision
	Rule     string
	Reason   string
}

type PolicyRule struct {
	Name        string   `yaml:"name" json:"name"`
	Tenant      string   `yaml:"tenant" json:"tenant"`
	Action      string   `yaml:"action" json:"action"`
	Tool        string   `yaml:"tool" json:"tool"`
	Agent       string   `yaml:"agent" json:"agent"`
	Decision    Decision `yaml:"-" json:"decision"`
	DecisionStr string   `yaml:"decision" json:"decision_str"`
	Priority    int      `yaml:"priority" json:"priority"`
}

type PolicyConfig struct {
	Rules          []PolicyRule `yaml:"rules"`
	SecretPatterns []string     `yaml:"secret_patterns"` // additional regex patterns for secret masking
}
