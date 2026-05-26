# ADR 0001: Open-core line and commercial model

**Status:** Proposed — pending Surya's decision
**Date:** 2026-05-26
**Owner:** Surya

## Context

Cyntr is Apache-2.0 licensed today. The platform is technically complete (13 features shipped, May 2026) but has no revenue path and no formal commercial offering. Before launch (T1.2), we need to commit to a model — what's free, what's paid, where the line falls — so the launch narrative, pricing page, and licensing footer are coherent on day one.

The cost of indecision is real:
- Launching as pure open source with no commercial story means no path to fund development beyond Surya's nights and weekends.
- Launching with an unclear commercial story (or vague "enterprise tier coming soon") shrinks the trust pool: enterprise buyers walk away, OSS contributors get nervous about license bait-and-switch.
- Competitors have made their picks. Dify chose source-available with anti-SaaS clause. Letta/Hermes/Flowise chose Apache + hosted SaaS. Bedrock AgentCore + Foundry chose closed cloud-only. Each picks shapes how the project is perceived from day one.

## Options considered

### A. Apache-2.0 + hosted SaaS (recommended)

- All code stays Apache-2.0, including future features
- Commercial offering: managed `cyntr.cloud` with paid tiers
- Possible paid features in hosted-only: multi-region federation, dedicated support, SSO+, advanced audit retention, prebuilt vertical packs as "Cyntr for X" SKUs
- **Pros:** Maximum trust + adoption. Aligns with Dify/Letta/Langflow precedent. Lowest legal complexity. OSS community contributions stay simple.
- **Cons:** Anyone (including hyperscalers) can host Cyntr commercially. AWS could ship "Cyntr on Bedrock" tomorrow with zero recourse.
- **Real risk:** low in practice — hyperscaler-hosted commodity is a 3+ year out problem; by then we want to win on cyntr.cloud being faster + cheaper + better than any reseller.

### B. Open core — Apache for core, commercial license for advanced features

- Core stays Apache-2.0: agent runtime, tools, basic policy, single-tenant
- Commercial license required for: multi-tenant + RBAC, OIDC/SSO, federation, advanced audit, OPA/Rego policy
- **Pros:** Direct upsell path. Clear "you must pay for the enterprise version."
- **Cons:** Cyntr's whole differentiation is the enterprise primitives. Putting them behind a license **kills the open-source pitch** that wins distribution. Splits the community. Risks "enterprise version" being underwhelming because the free version is too gutted.
- **Verdict:** wrong for Cyntr's stage. Open-core makes sense when you already have an OSS community and a paid funnel; we have neither.

### C. Source-available (BSL / Elastic / SSPL-style)

- All code visible, but a license clause prevents reselling as a managed service
- Examples: Dify's "anti-SaaS" Apache, MongoDB SSPL, Elastic ELv2
- **Pros:** Protects against hyperscaler clone. Allows source distribution.
- **Cons:** OSI does not recognize these as open source — many devs, distros, and procurement teams refuse to depend on source-available code. Hacker News and OSS community sentiment is hostile post-2021 license-change cycles. Cyntr's launch narrative ("the only open-source enterprise AI agent platform") would be technically false and would be called out within hours of Show HN.
- **Verdict:** the optics damage outweighs the protection.

### D. AGPL + commercial dual license

- AGPL is OSI-approved, copyleft, network-use triggers source disclosure
- Anyone using Cyntr in a SaaS must open-source their code OR buy a commercial license
- **Pros:** Strong protection against hyperscaler clones AND a clear commercial upsell.
- **Cons:** Enterprise procurement *strongly* avoids AGPL — many companies have blanket "no AGPL dependencies" policies. Slack/Discord usage clauses already complicate channel integrations. Most adopters would choose a competitor over reading AGPL terms.
- **Verdict:** unblocks a commercial path but at the cost of the enterprise buyer segment we actually want.

## Recommendation: Option A — Apache-2.0 + hosted SaaS

Pick A. Specifically:

1. **Keep everything Apache-2.0.** Including all 13 shipped features. Including future features. No bait-and-switch later.
2. **Commercial offering is hosted-only.** `cyntr.cloud` with three tiers:
   - **Free** — single tenant, 50k tokens/day, community support. The "try it" surface.
   - **Pro** — $49/mo per seat — multi-tenant (≤ 5), email support, eval-CI run on Cyntr's CI, 1M tokens/day pooled.
   - **Enterprise** — custom — SAML SSO, dedicated cluster, SLA, federation across customer regions, custom skill packs, audit-export to S3/GCS.
3. **Premium vertical packs sold separately.** Cloud Ops Pack, Customer Support Pack, Compliance Pack — each $X/mo on top of any tier. Open source the *interfaces*; sell the curated content + audit hardening + support.
4. **Never license-change.** Bake "Apache-2.0 forever for the platform" into a project promise file. Letta did this and it built trust quickly. Mongo/Hashi/Elastic each lost meaningful goodwill at re-license moments.

## What this means tactically

- **Today**: ship the launch with Apache-2.0, no commercial offering live yet, but a "Pro coming this quarter" page with email capture.
- **2 weeks post-launch**: stand up cyntr.cloud free tier (T2.5 ships the infra). Free is real; Pro is "waitlist."
- **6 weeks post-launch**: ship Pro billing (Stripe), promote first 10 design-partner customers from Show HN/PH traffic onto Pro at a founder discount.
- **3-6 months**: Enterprise sales motion starts with the first Pro customers asking about SSO/dedicated.

## What to *not* do

- Don't promise SOC2 before it's real. Say "in progress" or "available on request" — Type 1 attestation is achievable in 6-9 months once we have customer pull.
- Don't ship "Enterprise" before Pro has paying customers. Premature enterprise-tier pricing pages signal desperation.
- Don't sell SLAs on free tier hosted. Even implicit ones. Free is best-effort.
- Don't run skill marketplace commerce (paid skills with revenue share) until cyntr.cloud has ≥1k MAU. Marketplace economics require liquidity.

## Decision

[ ] Approve Option A as written
[ ] Approve Option A with edits (note below)
[ ] Pick different option (note below)
[ ] Defer — discuss further

**Notes:**

---

*Once chosen, this ADR becomes the canonical reference for licensing/commercial questions. Future deviations should be new ADRs that explicitly supersede this one.*
