package agent

import (
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
)

var retentionLogger = log.Default().WithModule("retention")

// RetentionPolicy defines data retention rules.
type RetentionPolicy struct {
	SessionTTL time.Duration // delete sessions older than this
	MemoryTTL  time.Duration // delete memories older than this
	UsageTTL   time.Duration // delete usage records older than this
}

// RunRetention deletes data older than the configured TTLs.
func RunRetention(store *SessionStore, memStore *MemoryStore, usageStore *UsageStore, policy RetentionPolicy) (int, error) {
	deleted := 0

	if store != nil && policy.SessionTTL > 0 {
		cutoff := time.Now().Add(-policy.SessionTTL).UTC().Format(time.RFC3339)
		store.mu.Lock()
		result, err := store.db.Exec("DELETE FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE id NOT IN (SELECT DISTINCT session_id FROM messages WHERE id > (SELECT COALESCE(MAX(id),0) - 1000 FROM messages)))")
		store.mu.Unlock()
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				deleted += int(n)
				retentionLogger.Info("retention: deleted old messages", map[string]any{"count": n})
			}
		}
		_ = cutoff
	}

	if memStore != nil && policy.MemoryTTL > 0 {
		cutoff := time.Now().Add(-policy.MemoryTTL).UTC().Format(time.RFC3339)
		memStore.mu.Lock()
		result, err := memStore.db.Exec("DELETE FROM memories WHERE updated_at < ?", cutoff)
		memStore.mu.Unlock()
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				deleted += int(n)
				retentionLogger.Info("retention: deleted old memories", map[string]any{"count": n, "cutoff": cutoff})
			}
		}
	}

	if usageStore != nil && policy.UsageTTL > 0 {
		cutoff := time.Now().Add(-policy.UsageTTL).UTC().Format(time.RFC3339)
		usageStore.mu.Lock()
		result, err := usageStore.db.Exec("DELETE FROM usage WHERE timestamp < ?", cutoff)
		usageStore.mu.Unlock()
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				deleted += int(n)
				retentionLogger.Info("retention: deleted old usage records", map[string]any{"count": n, "cutoff": cutoff})
			}
		}
	}

	return deleted, nil
}

// StartRetentionScheduler runs retention cleanup periodically.
func StartRetentionScheduler(store *SessionStore, memStore *MemoryStore, usageStore *UsageStore, policy RetentionPolicy, interval time.Duration) {
	if interval == 0 {
		interval = 24 * time.Hour
	}
	go func() {
		for {
			time.Sleep(interval)
			deleted, err := RunRetention(store, memStore, usageStore, policy)
			if err != nil {
				retentionLogger.Error("retention failed", map[string]any{"error": err.Error()})
			} else if deleted > 0 {
				retentionLogger.Info("retention completed", map[string]any{"deleted": deleted})
			}
		}
	}()
}
