package policy

import (
	"fmt"
	"sync"
)

// SpendingTracker tracks API call costs per tenant.
type SpendingTracker struct {
	mu      sync.RWMutex
	usage   map[string]float64 // "tenant" or "tenant/agent" -> total cost
	budgets map[string]float64 // "tenant" or "tenant/agent" -> max budget
}

func NewSpendingTracker() *SpendingTracker {
	return &SpendingTracker{
		usage:   make(map[string]float64),
		budgets: make(map[string]float64),
	}
}

// SetBudget sets the maximum spend for a tenant or tenant/agent.
func (st *SpendingTracker) SetBudget(key string, budget float64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.budgets[key] = budget
}

// RecordCost adds a cost to the tracker. Returns error if budget exceeded.
func (st *SpendingTracker) RecordCost(tenant, agent string, cost float64) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Check tenant budget
	tenantKey := tenant
	tenantBudget, hasTenantBudget := st.budgets[tenantKey]
	if hasTenantBudget && st.usage[tenantKey]+cost > tenantBudget {
		return fmt.Errorf("tenant %q budget exceeded (%.2f/%.2f)", tenant, st.usage[tenantKey]+cost, tenantBudget)
	}

	// Check agent budget
	if agent != "" {
		agentKey := tenant + "/" + agent
		agentBudget, hasAgentBudget := st.budgets[agentKey]
		if hasAgentBudget && st.usage[agentKey]+cost > agentBudget {
			return fmt.Errorf("agent %q budget exceeded (%.2f/%.2f)", agentKey, st.usage[agentKey]+cost, agentBudget)
		}
		st.usage[agentKey] += cost
	}

	st.usage[tenantKey] += cost
	return nil
}

// Usage returns the current spend for a key.
func (st *SpendingTracker) Usage(key string) float64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.usage[key]
}

// Budget returns the budget for a key (0 means unlimited).
func (st *SpendingTracker) Budget(key string) float64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.budgets[key]
}

// Reset clears all usage (e.g., for monthly reset).
func (st *SpendingTracker) Reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.usage = make(map[string]float64)
}
