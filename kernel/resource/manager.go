package resource

import (
	"errors"
	"sync"
)

type ResourceType int

const (
	ResourceGoroutines      ResourceType = iota
	ResourceFileDescriptors
	ResourceAPICalls
)

var ErrLimitExceeded = errors.New("resource: limit exceeded")

type UsageEntry struct {
	Current int64
	Limit   int64
}

type Manager struct {
	mu     sync.RWMutex
	usage  map[string]map[ResourceType]int64
	limits map[string]map[ResourceType]int64
}

func NewManager() *Manager {
	return &Manager{
		usage:  make(map[string]map[ResourceType]int64),
		limits: make(map[string]map[ResourceType]int64),
	}
}

func (m *Manager) SetLimit(tenant string, res ResourceType, limit int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limits[tenant] == nil {
		m.limits[tenant] = make(map[ResourceType]int64)
	}
	m.limits[tenant][res] = limit
}

func (m *Manager) Acquire(tenant string, res ResourceType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usage[tenant] == nil {
		m.usage[tenant] = make(map[ResourceType]int64)
	}
	current := m.usage[tenant][res]
	limit := m.getLimit(tenant, res)
	if limit > 0 && current >= limit {
		return ErrLimitExceeded
	}
	m.usage[tenant][res] = current + 1
	return nil
}

func (m *Manager) Release(tenant string, res ResourceType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usage[tenant] != nil && m.usage[tenant][res] > 0 {
		m.usage[tenant][res]--
	}
}

func (m *Manager) Usage(tenant string, res ResourceType) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.usage[tenant] == nil {
		return 0
	}
	return m.usage[tenant][res]
}

func (m *Manager) Snapshot(tenant string) map[ResourceType]UsageEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[ResourceType]UsageEntry)
	if limits, ok := m.limits[tenant]; ok {
		for res, limit := range limits {
			var current int64
			if m.usage[tenant] != nil {
				current = m.usage[tenant][res]
			}
			result[res] = UsageEntry{Current: current, Limit: limit}
		}
	}
	if usage, ok := m.usage[tenant]; ok {
		for res, current := range usage {
			if _, exists := result[res]; !exists {
				result[res] = UsageEntry{Current: current, Limit: 0}
			}
		}
	}
	return result
}

func (m *Manager) getLimit(tenant string, res ResourceType) int64 {
	if m.limits[tenant] == nil {
		return 0
	}
	return m.limits[tenant][res]
}
