# Devpost submission draft — Cyntr Guardian

**Competition:** Nebius × NVIDIA Global AI Hackathon 2026  
**Track:** Best Apps and Agents  
**Project:** Cyntr Guardian  
**Tagline:** An AI incident responder that proves the recovery, or safely rolls back.  
**Prize target:** $20,000 USD overall grand prize (not guaranteed).

> This is a DRAFT and is NOT proof of registration or submitted entry.
> Remove all placeholders and verify all claims before using on Devpost.

## The problem

Incident responders waste time moving between alerts, deployment state,
runbooks, access approval and validation. Existing AI copilots can describe
incidents but often stop at a recommendation, leaving the risky execution
and verification steps disconnected.

## What Cyntr Guardian does

Guardian is an opt-in feature of Cyntr, an Apache-2.0, single-binary Go
agent platform. It starts with actual Kubernetes Deployment state, makes
a live model-planning request to an NVIDIA Nemotron model via Nebius Token
Factory, validates the proposal against deterministic guardrails, asks for
explicit action-bound operator authorization, and atomically applies a
strictly bounded replica restoration. It then waits for readiness and
either reports verified recovery or attempts a guarded rollback.

The first supported scenario is deliberately narrow: a Deployment with
zero desired and zero ready replicas. The model **cannot** invent shell
commands, delete resources, or authorize its own changes.

### Exact role of Nebius and NVIDIA

- Nebius Token Factory provides live `/v1/chat/completions` inference.
- NVIDIA Nemotron provides the incident-specific structured repair proposal.
- The Go Guardian engine independently validates the returned action and
  target replica count, then drives constrained observation and execution.
- The operator can inspect model/provider identification, token usage,
  deployment identities and outcome in JSON and optional HTML evidence.

No model-generated text is directly executed.

## Why this is a significant new feature

Cyntr pre-dates the contest and its June 7, 2026 release included a
read-only Cloud Ops pack capable of investigation and suggestions. Its
`demos/cloud-ops/README.md` stated that auto-remediation was not included.

During the competition period we added:
- a Token Factory/Nemotron planning path;
- a policy-bounded zero-replica recovery state machine;
- a UID and replica guarded Kubernetes JSON Patch;
- approval tied to the resource UID and target replica count;
- post-action readiness polling and conditional rollback;
- defensive concurrency handling and a failure timeline;
- deterministic unit tests plus standalone judge evidence reporting.

Compare against baseline commit
`83a674dea5e87fc232a3f8beffd5d08c6551073d` on the new feature branch.

## Judge instructions

1. Install Go 1.26 and kubectl, plus a disposable Kubernetes test cluster.
2. Follow `docs/hackathons/nebius-guardian-2026.md` step by step.
3. Set a valid `NEBIUS_API_KEY` with NVIDIA model access in Token Factory.
4. Run a dry-run to obtain the exact UID-bound approval string.
5. Authorize the action on the disposable deployment and inspect the
   resulting JSON/HTML status, event timeline and readiness.
6. Run `go test ./modules/guardian -race` to check denied actions,
   optimistic-concurrency requirements and rollback behavior.

Hosted, no-credential-required public judge instance: **[TODO]**.
Public YouTube video under three minutes: **[TODO]**.
Repository: https://github.com/surya-koritala/cyntr/tree/feat/nebius-guardian-2026
PR / code changes: https://github.com/surya-koritala/cyntr/pull/76

## Verification data to fill after real tests

| Result | Real measured value | Evidence |
|---|---|---|
| Live NVIDIA Nemotron call successful | TODO | Redacted API trace |
| Kubernetes restored to Ready=Desired | TODO | Run JSON + cluster output |
| Verification-failure rollback | TODO | Run JSON + cluster output |
| Median recovery duration (n≥10) | TODO | Aggregate real run logs |
| Unauthorized writes in policy tests | TODO | CI results |
| Token Factory input/output tokens | TODO | Report output |
| Estimated model cost | TODO | Current Nebius pricing × measured tokens |

**Do not report simulated results as live measurements.**

## Known limitations

The workflow handles one specific failure mode and verifies Kubernetes
Deployment readiness rather than an application-level SLO. The CLI's exact
action approval is not connected to Cyntr centralized identity or SSO.
Automatic retry across model/tool errors, generalized incident diagnosis,
production-grade access control and hosted judgment access are future work.

## Mandatory competition tasks outstanding

Devpost membership and submission, free judge-accessible demo/test build
through December 15, actual inference with an NVIDIA open-source model
on Nebius, public YouTube video, feedback on Nebius and NVIDIA, and
explicit documentation of the significant updates remain to be completed
and verified. The current draft does not satisfy those tasks by itself.
