# Cyntr Guardian — Nebius × NVIDIA Global AI Hackathon 2026

**Track:** Best Apps and Agents  
**Base project:** Cyntr v1.3.0 (baseline commit `83a674dea5e87fc232a3f8beffd5d08c6551073d`, June 7, 2026)  
**Submission period:** August 26 – October 30, 2026  
**New code:** `modules/guardian/`, `cmd/cyntr/guardian.go`, `cmd/cyntr/main.go` dispatcher, tests and this documentation.  
**Public code:** https://github.com/surya-koritala/cyntr  
**License:** Apache-2.0 (repository root LICENSE)

## What is substantially new?

The existing Cloud Ops pack can diagnose AWS/Kubernetes problems, return runbooks,
and request human approvals. It **cannot perform and verify remediation**
(`demos/cloud-ops/README.md` explicitly lists auto-remediation as missing).

Guardian adds an opt-in, closed-loop recovery capability:

1. Read a Kubernetes Deployment in an **explicit context and namespace**.
2. Send that observed state and incident symptom to a real NVIDIA Nemotron
   model through **Nebius Token Factory chat completions**.
3. Parse a strict JSON repair proposal and reject any action outside a
   bounded, hardcoded `restore_replicas` allowlist.
4. Default to a **no-write dry run**. For a mutation, require `--execute`
   and an **exact workload UID-bound approval string** from the operator.
5. Execute a Kubernetes JSON Patch that atomically tests Deployment UID
   and its current replica count before replacing `spec.replicas`.
6. Poll Kubernetes for ready replicas. If readiness is not reached in the
   verification window, **restore the previous replica count** using another
   UID/replica-guarded JSON Patch. If state drifted, fail closed and ask
   for human review instead of overwriting a concurrent operator change.
7. Emit structured JSON showing the observed state, proposal, decisions,
   outcome, model ID, provider token usage, duration and event timeline. Optionally write a safe self-contained HTML
   evidence report with `--report guardian-recovery.html`.

**Deliberate scope:** This release handles only a Deployment with both
desired replicas and ready replicas equal to zero. It never applies model-
generated shell commands or Kubernetes YAML. No arbitrary patching, deletion,
namespace creation, security-group updates, or production auto-healing.

**Important:** Action-bound `--approve` is a local CLI human-authorization
gate, not an integration with Cyntr's centralized approval queue or SSO.
Do not describe it as centrally authenticated or suitable for unattended
production use. Never run on a production cluster for a hackathon demo.

## Live prerequisites

- Go version from root `go.mod` (currently 1.26).
- `kubectl`, with a disposable **kind** or other test-only Kubernetes cluster.
- An authenticated Nebius Token Factory account with access to NVIDIA Nemotron.
- `NEBIUS_API_KEY` set in the environment. Do **not** commit credentials.
- A real `kubectl` context such as `kind-guardian`.
- Permission to `get` and `patch` a test Deployment within only the demo namespace.

Nebius documents the OpenAI-compatible endpoint:
`https://api.tokenfactory.nebius.com/v1`. The default NVIDIA model is
`nvidia/nemotron-3-super-120b-a12b`; model availability may vary.
Set `--model` to the exact NVIDIA model ID available in your account.

## Build and unit tests

```bash
go test ./modules/guardian -count=1 -v
go build ./cmd/cyntr
go test ./cmd/cyntr
```

Tests use a fake cluster and HTTP test server; passing them does **not**
prove live Token Factory inference or real Kubernetes recovery.

## Disposable demonstration (operator-controlled)

Create a `kind` cluster first if needed:

```bash
kind create cluster --name guardian
kubectl config current-context # must say kind-guardian
kubectl --context kind-guardian create namespace guardian-demo
kubectl --context kind-guardian -n guardian-demo create deployment web --image=nginx:stable
kubectl --context kind-guardian -n guardian-demo scale deployment/web --replicas=0
```

After getting your Nebius key through the official console:

```bash
export NEBIUS_API_KEY="YOUR_TOKEN_FACTORY_KEY" # do not paste into recordings

# Real Nemotron call; safe dry-run. Review JSON proposal and chosen count.
go run ./cmd/cyntr guardian \
  --context kind-guardian --namespace guardian-demo --deployment web \
  --symptom 'web service returns 503; zero endpoints' \\
  --report guardian-plan.html

# Example only: if the model proposed TWO replicas, copy the exact `approval_required`\nvalue from the dry-run JSON (includes the real Deployment UID) and use it:
go run ./cmd/cyntr guardian \
  --context kind-guardian --namespace guardian-demo --deployment web \
  --symptom 'web service returns 503; zero endpoints' \
  --execute --approve 'scale:guardian-demo/web:2'

kubectl --context kind-guardian -n guardian-demo get deployment web
```

**Caution:** Guardian makes another real model call on execution; if its
proposal changes, the approval will no longer match and nothing is written.
Review the planned action before approving. Use test resources only.

### Reproducible test cases

| Scenario | Expected outcome | Evidence |
|---|---|---|
| Zero replicas, no `--execute` | Plan only, zero writes | JSON events plus get deployment |
| Zero replicas, valid approval | Guarded scale, ready replicas verified | JSON `recovered` |
| Wrong or missing approval | `denied`, zero writes | Test `TestGuardianRequiresActionBoundApproval` |
| Unsupported action, replica target above cap | `rejected`, zero writes | Test `TestGuardianRefusesInvalidProposals` |
| No readiness before timeout | `rolled_back` to prior replicas | Test `TestGuardianRollbackAfterVerificationFailure` |
| Deployment UID changes during recovery | `manual_review`, no blind rollback | Test `TestGuardianRefusesRollbackOnConcurrentIdentityDrift` |
| Nebius API unavailable / malformed response | Fail closed, zero writes | Nebius parser/error tests |

For a real-cluster rollback demonstration, scale a test Deployment whose
container image does not become ready. Do not claim the cluster recovered:
it correctly reports an unsuccessful recovery and rollback.

## Before versus after (baseline comparison)

Before: Cyntr Cloud Ops supplied read-only diagnosis and suggestions.
After: Guarded, model-planned, approval-gated replica restoration with
readiness verification, rollback and timeline. All additions are within
the 2026 hackathon window on the feature branch.

Compare `83a674dea5e87fc232a3f8beffd5d08c6551073d` with the
new Guardian branch in GitHub's Compare view. Preserve build/test output,
runtime API trace (redacted), and demonstration recordings.

## Evidence and submission status (do not assert completion prematurely)

- [x] Feature-branch commits dated inside hackathon window
- [x] Real Token Factory inference implementation in source
- [x] Constrained Kubernetes executor, verification, rollback
- [x] Unit tests authored for key paths
- [x] Optional standalone judge-friendly HTML evidence report
- [x] GitHub Actions Go race tests and CLI build passed on the feature branch (re-check the latest commit before submission)
- [ ] Verified actual Nebius Token Factory inference with your key
- [ ] Verified success and rollback on a disposable Kubernetes cluster
- [ ] Free public judge-accessible hosted app or runnable test build
- [ ] Before/after measured timings and success-rate benchmark
- [ ] Public YouTube demo shorter than 3 minutes
- [ ] Devpost entry, third-party feedback, and track submission

**Eligibility note:** A live Nebius call using an NVIDIA open-source model is
mandatory. Unit tests using a mock HTTP server are not enough. Judges need
a functional test build and free access through the judging period. The
existing Apache-2.0 license must appear detected on the public GitHub page.

## Suggested under-three-minute video

- 0:00–0:25: show existing Cloud Ops gap, present Guardian.
- 0:25–0:55: show failing deployment and real Nebius/Nemotron API planning call.
- 0:55–1:25: show action-bound human approval and policy rejection case.
- 1:25–2:05: show guarded scale and healthy replica verification.
- 2:05–2:35: show failed deployment, verification timeout and rollback.
- 2:35–2:55: show commit diff, test evidence and takeaways.

**Demo claims must match real runs.** Don't present mocked tests as Nebius usage
or use a pasted output as if it were live inference.

## Relevant public rules

https://nebiusglobalaihackathon.devpost.com/rules
https://nebiusglobalaihackathon.devpost.com/updates/46204-here-s-how-judging-works

Deadline: October 30, 2026 at 10:00 AM Pacific / 1:00 PM Toronto time.
