// Package guardian implements a narrowly-scoped, auditable Kubernetes recovery
// workflow. Models propose an action; deterministic code controls execution.
package guardian

import (
    "context"
    "errors"
    "fmt"
    "regexp"
    "time"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

// Deployment is the minimum observable state needed for optimistic, reversible repair.
type Deployment struct {
    Name string `json:"name"`
    Namespace string `json:"namespace"`
    UID string `json:"uid"`
    Replicas int32 `json:"replicas"`
    Ready int32 `json:"ready"`
}

type Cluster interface {
    Read(context.Context, string, string) (Deployment, error)
    // Scale MUST atomically test both UID and current replicas before writing.
    Scale(context.Context, string, string, string, int32, int32) error
}

type Incident struct {
    Deployment Deployment `json:"deployment"`
    Symptom string `json:"symptom"`
}

type Proposal struct {
    Action string `json:"action"`
    DesiredReplicas int32 `json:"desired_replicas"`
    Explanation string `json:"explanation"`
}

type Planner interface {
    Propose(context.Context, Incident) (Proposal, error)
}

type Event struct {
    Time time.Time `json:"time"`
    Phase string `json:"phase"`
    Detail string `json:"detail"`
}

type Result struct {
    Status string `json:"status"`
    Before Deployment `json:"before"`
    After Deployment `json:"after"`
    Proposal Proposal `json:"proposal"`
    ApprovalRequired string `json:"approval_required,omitempty"`
    Events []Event `json:"events"`
    Error string `json:"error,omitempty"`
}

// Runner is fail-closed; it never runs model-produced shell or kubectl commands.
type Runner struct {
    Cluster Cluster
    Planner Planner
    Namespace string
    Deployment string
    Symptom string
    MaxReplicas int32
    Approval string
    Execute bool
    PollInterval time.Duration
    VerifyTimeout time.Duration
    Now func() time.Time
}

func ValidName(name string) bool {
    return len(name) > 0 && len(name) <= 63 && dnsLabel.MatchString(name)
}

func (r *Runner) Run(ctx context.Context) (res Result, err error) {
    if r.Cluster == nil || r.Planner == nil {
        return res, errors.New("cluster and planner are required")
    }
    if !ValidName(r.Namespace) || !ValidName(r.Deployment) {
        return res, errors.New("namespace and deployment must be DNS labels")
    }
    max := r.MaxReplicas
    if max == 0 { max = 3 }
    if max < 1 || max > 10 {
        return res, errors.New("max replicas must be between 1 and 10")
    }
    now := r.Now
    if now == nil { now = time.Now }
    note := func(phase, detail string) {
        res.Events = append(res.Events, Event{Time: now().UTC(), Phase: phase, Detail: detail})
    }
    before, err := r.Cluster.Read(ctx, r.Namespace, r.Deployment)
    if err != nil { return res, fmt.Errorf("read deployment: %w", err) }
    res.Before = before
    res.After = before
    note("observe", fmt.Sprintf("replicas=%d ready=%d uid=%s", before.Replicas, before.Ready, before.UID))
    if before.UID == "" || before.Namespace != r.Namespace || before.Name != r.Deployment {
        return res, errors.New("deployment identity mismatch")
    }
    // No broad remediation. This version is ONLY for a healthy deployment
    // accidentally reduced to zero replicas.
    if before.Replicas != 0 || before.Ready != 0 {
        return res, fmt.Errorf("unsupported state: expected zero desired and ready replicas")
    }
    proposal, err := r.Planner.Propose(ctx, Incident{Deployment: before, Symptom: r.Symptom})
    if err != nil { return res, fmt.Errorf("Nebius planning failed: %w", err) }
    res.Proposal = proposal
    note("plan", fmt.Sprintf("%s replicas=%d: %s", proposal.Action, proposal.DesiredReplicas, proposal.Explanation))
    if proposal.Action != "restore_replicas" || proposal.DesiredReplicas < 1 || proposal.DesiredReplicas > max {
        res.Status = "rejected"
        note("deny", "model proposal outside repair allowlist")
        return res, errors.New("model proposal violates guarded action policy")
    }
    res.Status = "planned"
    // Human approval is explicit and bound to the exact workload/action.
    expectedApproval := fmt.Sprintf("scale:%s/%s:%d:%s", r.Namespace, r.Deployment, proposal.DesiredReplicas, before.UID)
    res.ApprovalRequired = expectedApproval
    if !r.Execute {
        note("dry_run", "no cluster mutation; use --execute and the exact approval string")
        return res, nil
    }
    if r.Approval != expectedApproval {
        res.Status = "denied"
        note("deny", "missing or mismatched explicit action approval")
        return res, errors.New("explicit action-bound approval required")
    }
    // The Kubernetes JSON Patch uses atomic test operations so intervening
    // changes in UID or replica count cause a failure rather than an overwrite.
    if err := r.Cluster.Scale(ctx, r.Namespace, r.Deployment, before.UID, before.Replicas, proposal.DesiredReplicas); err != nil {
        res.Status = "write_rejected"
        note("write_rejected", err.Error())
        return res, fmt.Errorf("optimistic guarded scale: %w", err)
    }
    res.Status = "verifying"
    note("apply", fmt.Sprintf("guarded scale 0 -> %d", proposal.DesiredReplicas))
    interval := r.PollInterval
    if interval <= 0 { interval = 2 * time.Second }
    timeout := r.VerifyTimeout
    if timeout <= 0 { timeout = 60 * time.Second }
    verifyCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    var lastErr error
    for {
        observed, readErr := r.Cluster.Read(verifyCtx, r.Namespace, r.Deployment)
        if readErr == nil {
            res.After = observed
            if observed.UID != before.UID || observed.Replicas != proposal.DesiredReplicas {
                res.Status = "manual_review"
                note("drift", "identity/replicas changed during verification; refusing unsafe rollback")
                return res, errors.New("deployment changed concurrently; manual review required")
            }
            if observed.Ready >= proposal.DesiredReplicas {
                res.Status = "recovered"
                note("verify", "desired replicas are ready")
                return res, nil
            }
        } else {
            lastErr = readErr
        }
        timer := time.NewTimer(interval)
        select {
        case <-verifyCtx.Done():
            if !timer.Stop() { select { case <-timer.C: default: } }
            goto rollback
        case <-timer.C:
        }
    }
rollback:
    // Re-read before rollback. Do NOT mutate if state is ambiguous.
    // A canceled parent context means we cannot safely prove what changed.
    if ctx.Err() != nil {
        res.Status = "manual_review"
        return res, fmt.Errorf("verification interrupted; manual review required: %w", ctx.Err())
    }
    rollbackCtx, rollbackCancel := context.WithTimeout(ctx, 10*time.Second)
    defer rollbackCancel()
    observed, readErr := r.Cluster.Read(rollbackCtx, r.Namespace, r.Deployment)
    if readErr != nil || observed.UID != before.UID || observed.Replicas != proposal.DesiredReplicas {
        res.Status = "manual_review"
        note("rollback_aborted", "could not verify unchanged workload identity and replicas")
        return res, fmt.Errorf("verification timed out; rollback precondition unavailable (read: %v)", readErr)
    }
    if err := r.Cluster.Scale(rollbackCtx, r.Namespace, r.Deployment, before.UID, proposal.DesiredReplicas, before.Replicas); err != nil {
        res.Status = "rollback_failed"
        note("rollback_failed", err.Error())
        return res, fmt.Errorf("verification timed out, rollback failed: %w", err)
    }
    res.Status = "rolled_back"
    res.After = before
    note("rollback", "readiness check failed; restored original replica count")
    if lastErr != nil {
        return res, fmt.Errorf("verification timed out (last read error: %v); rollback successful", lastErr)
    }
    return res, errors.New("verification timed out; rollback successful")
}
