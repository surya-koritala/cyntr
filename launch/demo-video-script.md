# Cyntr demo video script — 3 minutes

> Three-minute screencap. Voiceover over terminal + dashboard. No talking
> head. Output: an `.mp4` for the README, the blog post, and the Show HN
> link. Text only — recording happens later.

**Goal of the video:** in 180 seconds, prove three things:
1. Cyntr installs as a single binary in under a minute (install).
2. Federation across two nodes is real, not a mock (federation demo).
3. Policy + eval are not slideware — a denied tool call and a failing eval CI run, both on camera (policy + eval).

**Tone:** flat, technical, no breathy hype. Imagine a 38-year-old SRE
narrating a screen recording over coffee. Cuts are tight; no flourishes.

**Recording notes (for the editor, not the voiceover):**
- Terminal font: JetBrains Mono 18pt, dark background.
- Dashboard captures at 1440×900, scaled to 1080p frame.
- Highlight the active line of output with a thin yellow underline on
  cut-ins (only when needed — sparingly).
- No music. Light keyboard sound. Voiceover only.
- Caption every URL and every command in lower-third subtitle.

---

## Section 1 — Install (0:00 – 0:45)

### 0:00 — Title card

**On screen:** black background, white text.
```
Cyntr
open-source AI agent platform
github.com/surya-koritala/cyntr
```
**Voiceover:** "Cyntr is an open-source AI agent platform. Single Go binary, OPA Rego policy-as-code, federation across nodes. Here's three minutes on what it does."

### 0:06 — Clone and build

**On screen:** terminal, prompt at `~/`. Type at ~12 cps:
```bash
$ git clone https://github.com/surya-koritala/cyntr.git
$ cd cyntr
$ go build -o cyntr ./cmd/cyntr
```
Output scrolls; build completes in ~8 seconds. Cursor on the prompt after.

**Voiceover:** "One repo, one `go build`. No Python venv, no node_modules, no Docker required. The binary is around forty megabytes."

### 0:20 — Init wizard

**On screen:** `./cyntr init` — 5-step wizard. Fast-forward through prompts (2x speed in the video), accepting defaults except: choose Anthropic as provider, paste a key (blurred in capture), enable Slack-bot off, finish.
```bash
$ ./cyntr init
Step 1/5: choose AI provider [anthropic]
Step 2/5: paste API key
Step 3/5: messaging channels [skip]
Step 4/5: cloud CLI access [skip]
Step 5/5: security policy [default-restrictive]

wrote cyntr.yaml
wrote policy.yaml
wrote .env
generated CYNTR_API_KEY
```

**Voiceover:** "`cyntr init` walks through provider, channels, cloud access, and policy. Five steps. Defaults are sane, so most teams skip through it."

### 0:32 — Start the server

**On screen:**
```bash
$ set -a && source .env && set +a
$ ./cyntr start
[12:01:04] kernel boot: 13 modules ready in 412ms
[12:01:04] dashboard: http://localhost:7700
[12:01:04] api:       http://localhost:8080
```

Open browser, navigate to `http://localhost:7700`. Dashboard appears: health cards green, 13 modules listed, audit timeline empty, "cloud-ops" agent pre-registered.

**Voiceover:** "Start the server, dashboard at port 7700, API at port 8080. Thirteen modules booted, all green. We're live."

---

## Section 2 — Federation demo (0:45 – 2:15)

### 0:45 — Topology card

**On screen:** Animated diagram (or static image, held for 4 seconds).
```
   ┌──────────────────────┐                  ┌──────────────────────┐
   │      node-a          │  /federation/    │      node-b          │
   │   tenant: research   │   delegate       │   tenant: legal      │
   │   agent:  research   │ ───────────────► │   agent:  legal      │
   │                      │                  │                      │
   │   policy: allow-all  │                  │   policy: allow only │
   │                      │                  │   legal/legal        │
   └──────────────────────┘                  └──────────────────────┘
```

**Voiceover:** "Federation. Two independent Cyntr nodes, two different orgs, two different policies. The research org wants to delegate a task to the legal org's agent. The legal node's policy decides whether to honour it."

### 0:55 — Start the demo

**On screen:** terminal, run the bundled demo.
```bash
$ ./demos/federation/run.sh
=== Cyntr Federation Demo ===
>> building cyntr binary...
>> starting node-a on :7700
>> starting node-b on :7800
>> joining peers
   node-a peers: [{"name":"node-b","endpoint":"http://127.0.0.1:7800",...}]
   node-b peers: [{"name":"node-a","endpoint":"http://127.0.0.1:7700",...}]
```

**Voiceover:** "One script. It builds the binary, starts both nodes on different ports, joins them as peers."

### 1:10 — First delegation succeeds

**On screen:** continues scrolling.
```bash
>> node-a -> node-b: federation.delegate (research -> legal)
{"data":{"peer_id":"node-b","agent":"legal",
         "content":"Default mock response","decision":"allow"}}
```

Pause on the output. Highlight `peer_id: node-b` and `decision: allow` with the yellow underline. Hold 3 seconds.

**Voiceover:** "First call: research on node-a delegates to legal on node-b. The response comes back. The `decision: allow` field tells you node-b's policy explicitly authorised it — not that node-a was trusted to ask. The receiver always decides."

### 1:30 — Second delegation denied

**On screen:**
```bash
>> node-a -> node-b: federation.delegate (research -> NON-EXISTENT)
{"error":{"code":"DELEGATE_FAILED",
          "message":"remote peer node-b: federation_inbound denied by
                     policy: matched rule \"deny-federated-other\""}}
```

Highlight the error message. Hold 4 seconds.

**Voiceover:** "Second call: same path, but targeting an agent node-b's policy doesn't allow. Denied — by name, by the rule that fired, before the agent runtime ever ran. This is policy-as-code on the federation boundary."

### 1:50 — Show the policy file

**On screen:** open `demos/federation/node-b/policy.yaml` in a side-by-side view with the previous output still visible. Highlight the two rules.
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

**Voiceover:** "Here's the policy. Eight lines of YAML. Or Rego, if your security team prefers OPA — same evaluator, same `CheckRequest`. Edit the policy, SIGHUP the process, no restart."

---

## Section 3 — Policy + Eval (2:15 – 3:00)

### 2:15 — Policy denies a tool call live

**On screen:** dashboard, chat interface for `cloud-ops` agent. Type into the chat:
```
User: rm -rf /tmp/old-logs
```

Response appears:
```
Cyntr: I'm not able to run `shell_exec` for `rm` commands under
       the current policy. Rule "deny-destructive-shell" matched.
       Logged: req_id=8f3e21a4.
```

**Voiceover:** "Same policy engine, different surface. The cloud-ops agent tries to run a destructive shell command; policy says no; the audit log captured it. The agent can't bypass the gate by re-prompting itself."

### 2:30 — Eval CI run fails on regression

**On screen:** terminal.
```bash
$ cyntr eval ./evals/ --threshold 90
running 12 cases against agent=assistant tenant=ops
  c01 ✓ contains "4"
  c02 ✓ tool=aws
  c03 ✗ contains "us-east-1"   (got: "us-west-2")
  ...
result: 10/12 passed (83.3%)
threshold: 90.0%
FAIL — regression detected
$ echo $?
1
```

Highlight the FAIL line and the `$?` of 1.

**Voiceover:** "Evals run from CI. Twelve cases, pass rate dips below the threshold, exit code one. Drop this into GitHub Actions and your model upgrades stop being silent."

### 2:48 — Closing card

**On screen:** black background, white text, no animation.
```
Cyntr v1.1 — Apache 2.0

  github.com/surya-koritala/cyntr
  docs:    cyntr.dev/docs
  demo:    demos/federation/
  hosted:  try.cyntr.dev

  ★ star us on GitHub
```

Hold 8 seconds.

**Voiceover:** "Cyntr v1.1. Apache two-point-oh. Federation demo, docs, and a hosted sandbox all linked from the repo. If this is the shape of agent platform your team has been waiting for, star us, try it, tell us what's missing."

### 3:00 — End

---

## Delivery checklist for the editor

- Total runtime target: 2:58 – 3:02. Trim filler before adding any.
- Voiceover read-through should be timed against this script before
  recording; tighten any beat that runs over.
- Every command shown must be runnable from a clean clone. Test the
  full script on a fresh laptop before publishing.
- No motion graphics other than the highlight underline. We want the
  video to feel like watching a senior engineer demo, not a launch
  trailer.
- Captions on (en) — many viewers will watch muted on a phone.
- Export 1080p mp4, h264, ~12 Mbps. Also export a 5:4 vertical
  re-cut for LinkedIn (Section 2 only, 90 seconds).
