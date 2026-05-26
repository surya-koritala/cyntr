[Cyntr Docs](../README.md) > Concepts > Federation

# Federation

Federation lets two or more independently-operated Cyntr nodes wire their agents together at runtime. An agent on node A can delegate to an agent on node B; node B enforces its own policy on the inbound call.

This is the cooperation primitive between independent Cyntr deployments — not a clustering layer for one logical instance.

## What it is and isn't

**It is not:**
- A clustering layer. Nodes don't share state or replicate sessions.
- High availability. If node B is down, calls to it fail; no failover.
- A way to scale one Cyntr instance across hosts.

**It is:**
- A way to wire independently-operated Cyntr instances so their agents can call each other under explicit policy.
- A trust boundary — each node decides what it accepts from each peer.
- A policy-sync and cross-site-audit substrate.

## When you'd use it

- Two teams in the same org each run their own Cyntr instance, with their own policies, but want shared workflows where team A's agents can ask team B's agents for help.
- Two companies with a B2B integration where each side controls its own agent policy.
- A multi-region deploy where each region has data-residency obligations but agents need to talk across them.

## Full explainer

The architecture, trust model, and security considerations are written up at [docs/federation.md](../federation.md). The page you're reading is the concept overview; that page is the full reference.

## Runnable demo

[`demos/federation/`](../../demos/federation/) — two cyntr nodes, two tenants, one cross-node delegation. The receiving node's policy explicitly authorises the call; a second call to a non-allowed agent is denied before the agent runtime runs.

```bash
# in-process, no infra
go test ./demos/federation/ -v

# two real cyntr processes
./demos/federation/run.sh

# docker
cd demos/federation && docker compose up --build
```

## Related

- [Full federation docs](../federation.md)
- [Concepts: Policy](policy.md) — peer-enforced policy on inbound delegations.
- [Reference: API — federation](../reference/api.md#federation)
