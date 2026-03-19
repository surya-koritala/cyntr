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
	Name        string   `yaml:"name"`
	Tenant      string   `yaml:"tenant"`
	Action      string   `yaml:"action"`
	Tool        string   `yaml:"tool"`
	Agent       string   `yaml:"agent"`
	Decision    Decision `yaml:"-"`
	DecisionStr string   `yaml:"decision"`
	Priority    int      `yaml:"priority"`
}

type PolicyConfig struct {
	Rules []PolicyRule `yaml:"rules"`
}
