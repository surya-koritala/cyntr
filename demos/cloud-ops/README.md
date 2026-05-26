# Cyntr for Cloud Ops

> Three pre-built agents that diagnose AWS cost spikes, troubleshoot Kubernetes,
> and audit security posture — talking to your SRE team in Slack threads, with
> every tool call logged and policy-checked.

---

## The 30-second value prop

Your SRE channel gets pinged for the same kinds of things every week:

- "Why did our AWS bill go up $2,800?"
- "Checkout-api is in CrashLoopBackOff again."
- "Compliance wants a quarterly audit by Friday."

These questions have a predictable shape. A senior engineer answers them in
half an hour by running the same five commands, reading the output, and
synthesizing the same kind of summary. Cyntr for Cloud Ops bottles that
half-hour into a Slack `/ask cyntr` reply that lands in thirty seconds — with
the real numbers, pulled from your real AWS account, gated by your real
policy engine.

You install three agents:

| Agent                          | Asks it answers                                |
|--------------------------------|------------------------------------------------|
| **cost-anomaly-investigator**  | Why did spend change. What changed. What to do.|
| **k8s-troubleshooter**         | What's wrong with this pod / deployment / namespace. |
| **security-audit-runner**      | Where is our AWS posture broken right now.     |

They share five backing tools (`aws_cross_account`, `aws_cost_explorer`,
`kubectl`, `knowledge_search`, `send_message`), five embedded runbooks the
agents cite by name, and the same policy engine that gates every other Cyntr
agent: read-only by default, approval-required for anything that mutates.

---

## What it looks like in Slack

Three scripted conversations live in
[example-slack-conversations.md](./example-slack-conversations.md). Quick teaser:

```
@sre-onduty:   /ask cost-anomaly-investigator
               our prod AWS bill jumped $2,847 this week. what changed?

@cyntr:        looking into prod account 123456789012... running aws_cost_explorer
               for May 19-25 vs May 12-18, grouped by SERVICE.

               (1) What changed
                   Total spend May 19-25: $14,201 (+$2,847 / +25% vs prior week)
                   Top drivers:
                     - EC2:           +$1,920  (a single r6i.8xlarge launched May 21)
                     - Data Transfer: +$612    (cross-AZ in us-east-1)
                     - CloudWatch:    +$310    (new log group, no retention)
               (2) Likely cause
                   i-0c3f...a91 was launched 2026-05-21 14:02 UTC in us-east-1b
                   tagged Owner=anitha, Project=ml-eval-q2. r6i.8xlarge ≈ $1.93/hr.
               (3) Recommended action
                   Confirm the instance is still needed; if it's a leftover eval
                   node, request termination. (Cyntr will not modify EC2 without
                   approval — runbook: aws-cost-spike-checklist, step 6.)
```

---

## How it works under the hood

Every reply above is the product of four enforced layers:

1. **Skill activation.** When the Slack message routes to a Cyntr agent, the
   `skill_router` resolves "cost-anomaly-investigator" from the local catalog,
   loads its system prompt, and narrows the agent's tool surface to the five
   declared in the skill YAML. Tools the skill doesn't ask for stay invisible
   to the model.
2. **Policy check.** Each tool call (e.g. `aws_cost_explorer.GetCostAndUsage`)
   is evaluated against the YAML policy engine. The default cloud-ops policy
   allows read APIs and routes any mutating verb (`aws iam delete-*`,
   `kubectl apply`, etc.) through the approval queue.
3. **Audit chain.** Every prompt, every tool input/output, and every model
   token count is appended to the SHA-256 audit chain. Compliance can replay
   any conversation byte-for-byte three quarters later.
4. **Slack-native delivery.** Block Kit formatting, thread replies, a typing
   indicator while the agent works, and reaction-based approvals for any
   pending action — all of that is wiring Cyntr already provides; the cloud-ops
   pack inherits it.

---

## Install

```bash
# 1. clone & build (Cyntr core)
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr && go build -o cyntr ./cmd/cyntr

# 2. interactive 5-step setup (AI provider, Slack, AWS CLI, policy)
./cyntr init && set -a && source .env && set +a && ./cyntr start &

# 3. install the cloud-ops pack (3 skills + 5 runbooks)
CYNTR_URL=http://localhost:7700 \
CYNTR_API_KEY="$CYNTR_API_KEY" \
./scripts/install-cloud-ops.sh
```

The install script is idempotent — re-running skips skills and runbooks that
are already present.

---

## Requirements

- **AWS CLI configured** on the host (or per-account STS role for
  `aws_cross_account`). Cyntr never asks for keys; it uses what's already on
  the box.
- **kubectl context** for any cluster you want `k8s-troubleshooter` to read.
  Read-only RBAC is sufficient — and recommended.
- **Slack workspace + bot token** for the conversational surface.
- (Optional) A Cost Explorer-enabled payer account for cost-anomaly-investigator.

---

## What's NOT included (yet)

- **Auto-remediation.** Every skill in this pack is read-only. Mutating
  remediations are listed as suggestions; a human approves them in Slack via
  reaction or the dashboard. We will ship an opt-in `cloud-ops-remediate` pack
  in a future release.
- **Azure / GCP equivalents.** The cost-anomaly skill is AWS-only today. The
  k8s-troubleshooter works against any cluster `kubectl` can reach (EKS, AKS,
  GKE, on-prem).
- **Custom org runbooks.** The 5 runbooks shipped are the well-known patterns.
  Use `cyntr knowledge ingest` to add your own — the agents will find them
  via `knowledge_search` and `runbook_search`.

---

## What you can build next

The cloud-ops pack is the template for any vertical Cyntr can address.
Same shape — pre-built skills, runbook content, a Slack-native surface — works
for:

- **Customer Support** — Zendesk-aware triage + KB lookup + draft reply.
- **Compliance** — quarterly evidence collection across AWS, GitHub, and Okta.
- **Data Quality** — anomaly investigation on warehouse-side metrics.

Each is a YAML pack, not a fork.
