# Federation

Cyntr federation lets multiple independent Cyntr nodes — each with its own
tenants, agents, policies, and audit log — cooperate at runtime. An agent on
one node can delegate to an agent on another node, and the receiving node
enforces its own policy on every inbound request.

This is the feature most worth understanding before deploying Cyntr at
multi-team or multi-org scale. It's also the one capability no other open
agent platform ships.

For a runnable demo see [demos/federation/](../demos/federation/).

## What "federation" is and isn't

Cyntr federation is **not**:

- A clustering layer. Nodes don't share state, don't elect a leader, don't
  replicate sessions or memory.
- A high-availability solution. If node-b is down, federated calls to it
  fail; there's no failover.
- A way to make one logical Cyntr instance scale across hosts.

Cyntr federation **is**:

- A way to wire two or more independently-operated Cyntr instances together
  so their agents can call each other under explicit policy.
- A trust boundary. Each node decides what it will accept from each peer.
- A policy-sync and cross-site-audit substrate. Policy versions can be
  propagated; audit indexes can be queried across peers; data-residency
  rules can pin a tenant to a node.

The mental model: it's federation in the *organisational* sense (separate
owners, separate policies, cooperating by agreement), not in the
*distributed-systems* sense.

## When to use it

- **Multi-team / multi-business-unit.** Each team runs its own Cyntr node,
  with its own LLM keys and its own access controls. Cross-team workflows
  delegate across nodes; each team's policy decides what it exposes.
- **Cross-region data residency.** A tenant whose data must stay in EU
  gets pinned to the EU node via `ResidencyPolicy`; agents on other nodes
  must come to it rather than pulling its data out.
- **Tiered trust.** A high-trust node handles privileged ops (cloud writes,
  secrets); other nodes delegate to it under approval policy. The
  privileged node never has to expose its credentials to the calling node.
- **Centralised audit roll-up.** A SIEM node queries audit indexes from
  every Cyntr node it knows about, producing a single timeline.

## Architecture

```
   ┌─────────────────────────┐                ┌─────────────────────────┐
   │ node-a (research org)   │                │ node-b (legal org)      │
   │                         │                │                         │
   │  agent: research        │  HTTP POST     │  /api/v1/federation/    │
   │     │                   │  ─────────────►│   inbound/delegate      │
   │     │  delegate_agent   │                │     │                   │
   │     │  peer=node-b      │                │     │  policy.check     │
   │     ▼                   │                │     │  action:          │
   │  federation module      │                │     │  federation_      │
   │     │                   │                │     │  inbound          │
   │     ▼                   │                │     ▼                   │
   │  Transport.Delegate ─── │                │  agent_runtime.chat     │
   │     │                   │                │     │                   │
   │     │  (response)       │   HTTP 200     │     ▼                   │
   │     ◄──────────────────────────────────  │  ChatResponse           │
   │                         │                │                         │
   └─────────────────────────┘                └─────────────────────────┘
            │                                            │
            ▼                                            ▼
       audit log                                    audit log
       (local)                                      (local)
```

Key properties:

- **Peer registry is local.** Each node keeps its own list of peers
  (`POST /api/v1/federation/peers`). There's no global directory.
- **Each node enforces its own policy on inbound delegation.** This is
  the trust boundary. A misbehaving peer cannot bypass it.
- **No state replicates by default.** Sessions, memories, and audit
  entries stay on the node that produced them. `PolicySync` and
  `FederatedQuery` are explicit, separate opt-ins.

## The four federation surfaces

### 1. Peer management

```
GET    /api/v1/federation/peers
POST   /api/v1/federation/peers          { name, endpoint, secret? }
DELETE /api/v1/federation/peers/{name}
```

IPC topics: `federation.join`, `federation.peers`, `federation.remove`.
The federation module also exposes a programmatic `AddPeer(Peer)` for tests
and embedded use.

### 2. Cross-node delegation (the new bit shipped with F9)

```
POST /api/v1/federation/delegate          { peer, tenant, agent, user, message }
POST /api/v1/federation/inbound/delegate  { peer, tenant, agent, user, message, caller }
GET  /api/v1/federation/health
```

IPC topics: `federation.delegate` (outbound), `federation.delegate.inbound`
(received-from-peer). Inbound requests always go through `policy.check`
with `action=federation_inbound` before reaching the agent runtime.

The transport layer is pluggable (`federation.Transport` interface). The
default is HTTP. Tests can inject an in-process transport — see
`demos/federation/federation_test.go`.

### 3. Policy sync

IPC topics: `federation.sync`.
HTTP endpoint: `POST /federation/sync` (declared in `sync.go`; not yet
wired in `web/api/server.go`).

Each `SyncMessage` carries a version. `AcceptSync` rejects messages whose
version is not newer than the last accepted version from that peer+type
pair, so out-of-order or replayed syncs are ignored.

### 4. Federated audit query

IPC topic: `federation.query`.
HTTP endpoint: `POST /federation/audit/query` (declared in `query.go`; not
yet wired in `web/api/server.go`).

`FederatedQuery.Query` fans out a `FederatedQueryRequest` (tenant,
action_type, time window, limit) to every registered peer in parallel and
returns successful responses. Peers that fail or time out are dropped from
the result; this is intentional — federated audit is best-effort.

## Trust model

Cyntr federation is **peer-to-peer with mutual independence**. There is no
shared trust root, no certificate authority, no shared session.

The contract between two peers is:

1. **Both sides agree on a shared secret** (the `Peer.Secret` field). The
   sender includes it as the `X-Federation-Secret` header on every
   outbound delegate. (The default HTTP server does not yet verify this —
   add an auth middleware in production. See "Security considerations"
   below.)
2. **The receiver enforces its own policy.** Even a fully-authenticated
   peer can only do what the receiver's `policy.yaml` permits with
   `action=federation_inbound`.
3. **Audit is local.** Every accepted delegation is recorded on the
   receiver. The sender records the outbound. There is no shared trace,
   only correlation by message ID.

This means: adding a peer is a *bilateral* operation. node-a knows about
node-b; node-b knows about node-a. Either side can revoke the other at any
time without coordination.

## Limits

- **No backpressure across the federation boundary.** A flood of inbound
  delegations from a peer will saturate the local agent runtime's rate
  limits before federation notices. If you expose federation publicly,
  put a reverse-proxy with per-peer rate-limiting in front.
- **No request streaming.** Each `federation.delegate` is one full
  request / one full response. No SSE, no incremental tokens. Cyntr's
  internal streaming is per-channel, not federated.
- **No transitive policy.** If node-a delegates to node-b which delegates
  to node-c, each hop applies its own policy independently — node-a's
  policy does not influence the b→c call. Federated workflows are easy
  to reason about because they're explicit at every step, but you cannot
  express "node-c should treat this as a node-a call" without convention.
- **No automatic peer discovery.** Peers are configured statically (via
  `cyntr.yaml`) or at runtime (via the HTTP API / IPC bus). There is no
  gossip, no DHT, no central registry.
- **`PolicySync` and `FederatedQuery` HTTP endpoints exist in code but
  are not wired into `web/api/server.go` as of F9.** The IPC paths are
  fully functional and tested. Adding the HTTP handlers is straightforward
  — see `web/api/federation.go` for the pattern.

## Security considerations

- **Authenticate peers.** The `Peer.Secret` field exists and is sent as
  `X-Federation-Secret` by the HTTP transport, but the inbound handler
  does not yet verify it. Wrap the `/api/v1/federation/inbound/*` routes
  with the existing `webapi.AuthMiddleware` (or a custom middleware that
  checks the header against the registered peer set) before exposing to
  any untrusted network.
- **TLS terminates outside cyntr.** The HTTP server does not do TLS. Use
  Caddy, nginx, Cloudflare Tunnel, or a service mesh in front of every
  federated node.
- **Policy is the only inbound gate.** A missing or permissive
  `federation_inbound` rule means any peer with the secret can invoke any
  agent on any tenant. The
  [demo policy](../demos/federation/node-b/policy.yaml) shows the
  recommended pattern: a high-priority `allow` for explicit
  agent/tenant pairs, then a high-priority `deny` for everything else
  with `action: federation_inbound`.
- **Audit the inbound.** Every accepted federation delegation should
  produce an audit entry tagged with the calling node ID. The federation
  module passes `User="federation:<caller>:<original_user>"` to the agent
  runtime — make sure your audit retention covers federated traffic.
- **Mock provider is for demos.** Don't run the demo policy in production
  with a real LLM provider until you've verified your `federation_inbound`
  rules: a permissive default could let a peer rack up token costs on
  your account.

## See also

- [demos/federation/](../demos/federation/) — runnable two-node demo.
- [modules/federation/](../modules/federation/) — source.
- [modules/federation/peer.go](../modules/federation/peer.go) — peer registry.
- [modules/federation/delegate.go](../modules/federation/delegate.go) — transport + DelegateRequest/Response types.
- [modules/federation/sync.go](../modules/federation/sync.go) — policy sync.
- [modules/federation/query.go](../modules/federation/query.go) — federated audit query.
- [modules/federation/residency.go](../modules/federation/residency.go) — data-residency policy.
