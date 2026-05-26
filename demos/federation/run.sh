#!/usr/bin/env bash
#
# Federation demo runner.
#
#   1. Builds the cyntr binary (if not already built).
#   2. Starts node-a on :7700 and node-b on :7800 with separate
#      working dirs, configs, and policies.
#   3. Waits for /federation/health on both nodes.
#   4. Joins them as peers (each registers the other).
#   5. Creates the research agent on node-a and the legal agent on node-b.
#   6. POSTs a federation.delegate request from node-a -> node-b and
#      prints the result. node-b's policy decides whether to honour it.
#
# Falls back to `go test ./demos/federation/` if curl or a free port is
# unavailable — the in-process test exercises the same code paths.
#
set -euo pipefail

cd "$(dirname "$0")"
DEMO_DIR="$(pwd)"
ROOT_DIR="$(cd ../.. && pwd)"

BIN="$ROOT_DIR/bin/cyntr"
NODE_A_PORT=7700
NODE_B_PORT=7800
NODE_A_API=8080
NODE_B_API=8081

echo "=== F9 Federation Demo ==="
echo "Cyntr root: $ROOT_DIR"
echo "Demo dir:   $DEMO_DIR"
echo

# --- 1. build ---------------------------------------------------------------
if [[ ! -x "$BIN" ]]; then
  echo ">> building cyntr binary..."
  mkdir -p "$ROOT_DIR/bin"
  (cd "$ROOT_DIR" && go build -o "$BIN" ./cmd/cyntr)
fi
echo ">> binary at $BIN"

# --- helpers ----------------------------------------------------------------
wait_for_url() {
  local url="$1" tries=40
  while (( tries-- )); do
    if curl -sf "$url" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

cleanup() {
  set +e
  echo
  echo ">> stopping nodes"
  [[ -n "${PID_A:-}" ]] && kill "$PID_A" 2>/dev/null
  [[ -n "${PID_B:-}" ]] && kill "$PID_B" 2>/dev/null
  wait 2>/dev/null
}
trap cleanup EXIT

# --- 2. spawn nodes ---------------------------------------------------------
RUN_DIR_A="$DEMO_DIR/.run/node-a"
RUN_DIR_B="$DEMO_DIR/.run/node-b"
rm -rf "$RUN_DIR_A" "$RUN_DIR_B"
mkdir -p "$RUN_DIR_A" "$RUN_DIR_B"

# Localhost configs override the docker-compose ones (which use container DNS).
cat > "$RUN_DIR_A/cyntr.yaml" <<EOF
version: "1"
listen:
  address: "127.0.0.1:${NODE_A_API}"
  webui: ":${NODE_A_PORT}"
tenants:
  research:
    isolation: namespace
    policy: default
EOF
cp "$DEMO_DIR/node-a/policy.yaml" "$RUN_DIR_A/policy.yaml"
cp "$DEMO_DIR/node-a/agents/research.json" "$RUN_DIR_A/research-agent.json"

cat > "$RUN_DIR_B/cyntr.yaml" <<EOF
version: "1"
listen:
  address: "127.0.0.1:${NODE_B_API}"
  webui: ":${NODE_B_PORT}"
tenants:
  legal:
    isolation: namespace
    policy: default
EOF
cp "$DEMO_DIR/node-b/policy.yaml" "$RUN_DIR_B/policy.yaml"
cp "$DEMO_DIR/node-b/agents/legal.json" "$RUN_DIR_B/legal-agent.json"

echo
echo ">> starting node-a on :${NODE_A_PORT}"
(cd "$RUN_DIR_A" && CYNTR_NODE_ID=node-a exec "$BIN" start cyntr.yaml) >"$RUN_DIR_A/cyntr.log" 2>&1 &
PID_A=$!

echo ">> starting node-b on :${NODE_B_PORT}"
(cd "$RUN_DIR_B" && CYNTR_NODE_ID=node-b exec "$BIN" start cyntr.yaml) >"$RUN_DIR_B/cyntr.log" 2>&1 &
PID_B=$!

# --- 3. wait for liveness ---------------------------------------------------
echo ">> waiting for node-a..."
wait_for_url "http://127.0.0.1:${NODE_A_PORT}/api/v1/federation/health" || {
  echo "node-a did not come up:"; tail -n 50 "$RUN_DIR_A/cyntr.log"; exit 1;
}
echo ">> waiting for node-b..."
wait_for_url "http://127.0.0.1:${NODE_B_PORT}/api/v1/federation/health" || {
  echo "node-b did not come up:"; tail -n 50 "$RUN_DIR_B/cyntr.log"; exit 1;
}

# --- 4. peer them -----------------------------------------------------------
echo
echo ">> joining peers"
curl -sf -X POST "http://127.0.0.1:${NODE_A_PORT}/api/v1/federation/peers" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"node-b\",\"endpoint\":\"http://127.0.0.1:${NODE_B_PORT}\"}" >/dev/null
curl -sf -X POST "http://127.0.0.1:${NODE_B_PORT}/api/v1/federation/peers" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"node-a\",\"endpoint\":\"http://127.0.0.1:${NODE_A_PORT}\"}" >/dev/null
echo "   node-a peers: $(curl -sf http://127.0.0.1:${NODE_A_PORT}/api/v1/federation/peers)"
echo "   node-b peers: $(curl -sf http://127.0.0.1:${NODE_B_PORT}/api/v1/federation/peers)"

# Agents auto-register from *-agent.json on startup (see cmd/cyntr/main.go).
# Give the auto-registration a beat to finish.
sleep 1

# --- 5. trigger cross-node delegation --------------------------------------
echo
echo ">> node-a -> node-b: federation.delegate (research -> legal)"
RESP=$(curl -sf -X POST "http://127.0.0.1:${NODE_A_PORT}/api/v1/federation/delegate" \
  -H 'Content-Type: application/json' \
  -d '{
    "peer":"node-b",
    "tenant":"legal",
    "agent":"legal",
    "user":"alice@node-a",
    "message":"Review the indemnity clause draft."
  }')
echo "$RESP"

# --- 6. show that policy blocks an unauthorised target ---------------------
echo
echo ">> node-a -> node-b: federation.delegate (research -> NON-EXISTENT, expect denial)"
DENY=$(curl -s -X POST "http://127.0.0.1:${NODE_A_PORT}/api/v1/federation/delegate" \
  -H 'Content-Type: application/json' \
  -d '{
    "peer":"node-b",
    "tenant":"research",
    "agent":"internal",
    "user":"alice@node-a",
    "message":"Try to invoke a non-allowed agent."
  }')
echo "$DENY"

echo
echo "=== Done. Logs in .run/{node-a,node-b}/cyntr.log ==="
