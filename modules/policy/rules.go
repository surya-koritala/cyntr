package policy

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

type RuleSet struct {
	Rules []PolicyRule
}

func LoadRuleSet(path string) (*RuleSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}
	var cfg PolicyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}
	for i := range cfg.Rules {
		d, err := parseDecision(cfg.Rules[i].DecisionStr)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", cfg.Rules[i].Name, err)
		}
		cfg.Rules[i].Decision = d
	}
	sort.Slice(cfg.Rules, func(i, j int) bool {
		return cfg.Rules[i].Priority > cfg.Rules[j].Priority
	})
	return &RuleSet{Rules: cfg.Rules}, nil
}

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
	return CheckResponse{Decision: Deny, Rule: "", Reason: "no matching rule — deny by default"}
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
