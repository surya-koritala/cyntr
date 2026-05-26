package agent

import (
	"context"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/quota"
)

// quotaCheck issues a quota.check IPC request for a single dimension.
//
// It returns (allowed, reason). When the quota module is not registered
// (ipc.ErrNoHandler), the check is treated as "allowed" so deployments without
// quota enforcement continue to function unchanged. Any other transport error
// is also treated as allowed (fail-open) so a transient bus issue can't take
// down the agent runtime — quota enforcement is a guard-rail, not a hard
// dependency.
func quotaCheck(bus *ipc.Bus, tenant, kind string, amount int64) (bool, string) {
	if bus == nil {
		return true, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "agent_runtime", Target: quota.ModuleName, Topic: quota.TopicCheck,
		Payload: quota.CheckRequest{Tenant: tenant, Kind: kind, Amount: amount},
	})
	if err != nil {
		// ErrNoHandler == quota module not registered → unlimited.
		// Any other error fails open (don't block the agent).
		return true, ""
	}
	cr, ok := resp.Payload.(quota.CheckResponse)
	if !ok {
		return true, ""
	}
	if !cr.Allowed {
		return false, cr.Reason
	}
	return true, ""
}

// quotaAcquireSlot reserves a concurrent-agent slot via the quota module.
//
// When the quota module is not registered, returns a no-op release function so
// callers can `defer release()` unconditionally.
func quotaAcquireSlot(bus *ipc.Bus, tenant string) (release func(), allowed bool, reason string) {
	if bus == nil {
		return func() {}, true, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "agent_runtime", Target: quota.ModuleName, Topic: quota.TopicSlotAcquire,
		Payload: tenant,
	})
	if err != nil {
		// Quota module absent or transient transport error → fail open.
		return func() {}, true, ""
	}
	sr, ok := resp.Payload.(quota.SlotResponse)
	if !ok {
		return func() {}, true, ""
	}
	if !sr.Allowed {
		return func() {}, false, sr.Reason
	}
	slotID := sr.SlotID
	return func() {
		relCtx, relCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer relCancel()
		_, _ = bus.Request(relCtx, ipc.Message{
			Source: "agent_runtime", Target: quota.ModuleName, Topic: quota.TopicSlotRelease,
			Payload: slotID,
		})
	}, true, ""
}

// quotaRecord publishes a token-usage record event (fire-and-forget).
func quotaRecord(bus *ipc.Bus, tenant string, tokens int64) {
	if bus == nil || tokens <= 0 {
		return
	}
	// Use Publish so the call is non-blocking; the quota module subscribes via
	// its bus.Handle on quota.record (Request semantics also work; Publish is
	// fire-and-forget so it cannot stall the agent runtime).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, _ = bus.Request(ctx, ipc.Message{
			Source: "agent_runtime", Target: quota.ModuleName, Topic: quota.TopicRecord,
			Payload: quota.RecordRequest{Tenant: tenant, Kind: quota.KindTokens, Amount: tokens},
		})
	}()
}
