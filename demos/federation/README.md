# Federation Demo

Two Cyntr nodes. Two tenants. One cross-node delegation that node-b's policy
gets to approve or deny.

This is the moat. No other open agent platform lets you run independent
nodes — each with its own tenants, agents, and policies — and have agents
delegate work across the boundary while every node keeps control of what
it will accept.

## What this demo proves

| Claim                                                | How this demo demonstrates it                                                                                          |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Cyntr nodes can run independently with separate policy. | `node-a/policy.yaml` and `node-b/policy.yaml` are different files loaded by different `cyntr start` processes.         |
| An agent on one node can delegate to an agent on another. | `node-a` posts to `/api/v1/federation/delegate` with `peer=node-b`; node-b returns the response from its `legal` agent. |
| The receiving node enforces its own policy on inbound delegation. | `node-b`'s policy allows `legal/legal` but denies anything else. The second call in `run.sh` is rejected before the agent runtime runs. |
| Federation is real, not a stub.                      | The whole path is exercised by `federation_test.go` in this directory, which spins up two kernels in one process with a custom in-process transport — no HTTP mock. |

## Topology

```
   ┌──────────────────────┐                  ┌──────────────────────┐
   │      node-a          │  federation      │      node-b          │
   │                      │  /delegate       │                      │
   │  tenant: research    │ ───────────────► │  tenant: legal       │
   │  agent:  research    │                  │  agent:  legal       │
   │                      │ ◄─── response ── │                      │
   │  policy.yaml         │                  │  policy.yaml         │
   │  allow-all           │                  │  allow legal/legal,  │
   │                      │                  │  deny everything     │
   │                      │                  │  else over federation│
   └──────────────────────┘                  └──────────────────────┘
```

Both nodes use the `mock` model provider so the demo is fully self-contained
and deterministic — the point is the **routing and policy boundary**, not the
LLM output.

## Run it

### Option A: in-process Go test (zero infra)

```bash
go test ./demos/federation/ -v
```

This is what CI runs. Two kernels boot in the same process with separate
buses; a custom `Transport` routes federation requests directly between
them. The federation module, policy engine, and agent runtime are the real
implementations.

### Option B: two real `cyntr start` processes

```bash
./run.sh
```

Builds the binary, starts node-a on `:7700` and node-b on `:7800`, joins
them as peers via the HTTP API, then makes two delegate calls. See
[expected-output.md](expected-output.md).

### Option C: docker-compose (multi-host shape)

```bash
docker compose up --build
# in another shell:
./run.sh   # joins peers + triggers the delegate via the dockerised endpoints
```

Note: `run.sh` defaults to localhost endpoints. Update `NODE_A_PORT` /
`NODE_B_PORT` if you remap container ports.

## File map

| Path                       | What it is                                                            |
| -------------------------- | --------------------------------------------------------------------- |
| `node-a/cyntr.yaml`        | node-a config: tenant `research`, no peers declared statically.       |
| `node-a/policy.yaml`       | node-a permissive policy — outbound delegation allowed.               |
| `node-a/agents/research.json` | The research agent definition (mock model, can call `delegate_agent`). |
| `node-b/cyntr.yaml`        | node-b config: tenant `legal`.                                        |
| `node-b/policy.yaml`       | node-b restrictive federation policy: only `legal/legal` allowed inbound. |
| `node-b/agents/legal.json` | The legal agent definition.                                           |
| `docker-compose.yml`       | Two-node stack with a shared bridge network.                          |
| `run.sh`                   | Launches both nodes locally, peers them, runs the delegations.        |
| `federation_test.go`       | In-process Go test exercising the same code paths without HTTP.       |
| `expected-output.md`       | What `run.sh` should print on success.                                |

## How node-b's policy boundary works

`node-b/policy.yaml` defines two rules that fire on the
`federation_inbound` action:

```yaml
- name: allow-federated-legal
  tenant: legal
  action: federation_inbound
  agent: legal
  decision: allow
  priority: 50

- name: deny-federated-other
  tenant: "*"
  action: federation_inbound
  agent: "*"
  decision: deny
  priority: 40
```

When node-a hits `/api/v1/federation/inbound/delegate` on node-b, the
federation module asks the local policy engine
`policy.check{action: federation_inbound, tenant, agent, user}` **before**
dispatching to the agent runtime. If the decision is `deny`, the request
never reaches the agent.

This is the trust model: **the receiving node always decides**. The
sender can ask for anything; only the receiver's policy controls whether
it's honoured.

## What about the LLM response?

The `mock` provider returns a static string so we can demonstrate the
plumbing in isolation. In production you would swap `model: "mock"` in
`legal.json` for `claude`, `gpt`, etc., and node-b would pay for the
tokens it serves — another reason a node operator wants strict inbound
policy.

## Related docs

- [docs/federation.md](../../docs/federation.md) — full federation feature explainer.
- [modules/federation/](../../modules/federation/) — the federation module source.
