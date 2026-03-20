package federation

import (
	"fmt"
	"sync"
)

// ResidencyPolicy tracks data residency rules per tenant.
type ResidencyPolicy struct {
	mu    sync.RWMutex
	rules map[string]string // tenant -> node name (where data must stay)
}

func NewResidencyPolicy() *ResidencyPolicy {
	return &ResidencyPolicy{rules: make(map[string]string)}
}

// SetRule locks a tenant's data to a specific node.
func (rp *ResidencyPolicy) SetRule(tenant, node string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	rp.rules[tenant] = node
}

// RemoveRule removes a residency restriction.
func (rp *ResidencyPolicy) RemoveRule(tenant string) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	delete(rp.rules, tenant)
}

// Check returns whether a data operation is allowed.
// localNode is the current node. Returns nil if allowed, error if residency violation.
func (rp *ResidencyPolicy) Check(tenant, localNode string) error {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	requiredNode, exists := rp.rules[tenant]
	if !exists {
		return nil // no residency rule — allowed anywhere
	}

	if requiredNode != localNode {
		return fmt.Errorf("data residency violation: tenant %q data must stay on node %q (current: %q)", tenant, requiredNode, localNode)
	}

	return nil
}

// GetRule returns the residency node for a tenant, if set.
func (rp *ResidencyPolicy) GetRule(tenant string) (string, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	node, ok := rp.rules[tenant]
	return node, ok
}

// ListRules returns all residency rules.
func (rp *ResidencyPolicy) ListRules() map[string]string {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	result := make(map[string]string, len(rp.rules))
	for k, v := range rp.rules {
		result[k] = v
	}
	return result
}
