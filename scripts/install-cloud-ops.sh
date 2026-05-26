#!/usr/bin/env bash
# install-cloud-ops.sh
#
# Installs the Cyntr "Cloud Ops" skill pack:
#   - 3 pre-built agent skills (cost-anomaly-investigator, k8s-troubleshooter,
#     security-audit-runner)
#   - 5 runbooks ingested into the knowledge base
#
# Idempotent: re-running is safe — already-installed skills and already-ingested
# runbooks are skipped.
#
# Usage:
#   CYNTR_URL=http://localhost:7700 CYNTR_API_KEY=... ./scripts/install-cloud-ops.sh
#
# Falls back to the `cyntr` CLI on PATH if CYNTR_URL is unset.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PACK_DIR="$REPO_ROOT/skills/cloud-ops"
RUNBOOK_DIR="$PACK_DIR/runbooks"

SKILLS=(
  "cost-anomaly-investigator"
  "k8s-troubleshooter"
  "security-audit-runner"
)

RUNBOOKS=(
  "aws-cost-spike-checklist.md"
  "k8s-pod-crashloopbackoff.md"
  "k8s-pending-pod.md"
  "aws-s3-public-access.md"
  "aws-iam-mfa-enforcement.md"
)

CYNTR_URL="${CYNTR_URL:-}"
CYNTR_API_KEY="${CYNTR_API_KEY:-}"

log()  { printf '[install-cloud-ops] %s\n' "$*"; }
warn() { printf '[install-cloud-ops] WARN: %s\n' "$*" >&2; }
fail() { printf '[install-cloud-ops] ERROR: %s\n' "$*" >&2; exit 1; }

# --- pre-flight --------------------------------------------------------------

[ -d "$PACK_DIR" ]    || fail "pack directory not found: $PACK_DIR"
[ -d "$RUNBOOK_DIR" ] || fail "runbook directory not found: $RUNBOOK_DIR"

# Validate every artifact exists before talking to the API.
for skill in "${SKILLS[@]}"; do
  [ -f "$PACK_DIR/$skill.yaml" ] || fail "missing skill: $PACK_DIR/$skill.yaml"
done
for rb in "${RUNBOOKS[@]}"; do
  [ -f "$RUNBOOK_DIR/$rb" ] || fail "missing runbook: $RUNBOOK_DIR/$rb"
done

# --- transport: HTTP via curl, or fall back to CLI ---------------------------

USE_HTTP=0
if [ -n "$CYNTR_URL" ]; then
  command -v curl >/dev/null 2>&1 || fail "curl required when CYNTR_URL is set"
  USE_HTTP=1
elif command -v cyntr >/dev/null 2>&1; then
  USE_HTTP=0
else
  fail "neither CYNTR_URL nor 'cyntr' CLI available — set CYNTR_URL or install cyntr"
fi

api_post() {
  # api_post <path> <json-body>
  local path="$1" body="$2"
  local auth_header=()
  [ -n "$CYNTR_API_KEY" ] && auth_header=(-H "Authorization: Bearer $CYNTR_API_KEY")
  curl -sS -w '\n%{http_code}' -X POST \
    -H "Content-Type: application/json" \
    "${auth_header[@]}" \
    --data "$body" \
    "$CYNTR_URL$path"
}

api_get() {
  local path="$1"
  local auth_header=()
  [ -n "$CYNTR_API_KEY" ] && auth_header=(-H "Authorization: Bearer $CYNTR_API_KEY")
  curl -sS "${auth_header[@]}" "$CYNTR_URL$path"
}

# --- skill installation ------------------------------------------------------

skill_already_installed() {
  local name="$1"
  if [ "$USE_HTTP" -eq 1 ]; then
    api_get "/api/v1/skills" 2>/dev/null | grep -q "\"$name\"" && return 0 || return 1
  else
    cyntr skill list 2>/dev/null | grep -q "^$name\b" && return 0 || return 1
  fi
}

install_skill() {
  local name="$1"
  local skill_path="$PACK_DIR/$name.yaml"

  if skill_already_installed "$name"; then
    log "skill '$name' already installed — skipping"
    return 0
  fi

  log "installing skill '$name' from $skill_path"
  if [ "$USE_HTTP" -eq 1 ]; then
    local resp http_code
    resp=$(api_post "/api/v1/skills" "{\"path\":\"$skill_path\"}") || {
      warn "install failed for $name"; return 1
    }
    http_code=$(printf '%s' "$resp" | tail -n1)
    case "$http_code" in
      201|200) log "  ok ($http_code)" ;;
      409)     log "  already installed (409) — skipping" ;;
      *)       warn "  install returned HTTP $http_code"; return 1 ;;
    esac
  else
    cyntr skill install "$skill_path" || {
      warn "cyntr skill install failed for $name"
      return 1
    }
  fi
}

# --- runbook ingestion -------------------------------------------------------

runbook_title() {
  # human title for the runbook
  local file="$1"
  printf 'runbook: %s' "$(basename "$file" .md)"
}

runbook_already_ingested() {
  local title="$1"
  if [ "$USE_HTTP" -eq 1 ]; then
    api_get "/api/v1/knowledge/search?q=$(printf '%s' "$title" | sed 's/ /%20/g')" 2>/dev/null \
      | grep -q "\"$title\"" && return 0 || return 1
  fi
  return 1  # CLI path: can't easily check; rely on re-ingest being idempotent server-side
}

ingest_runbook() {
  local file="$1"
  local path="$RUNBOOK_DIR/$file"
  local title; title="$(runbook_title "$file")"

  if runbook_already_ingested "$title"; then
    log "runbook '$title' already in knowledge base — skipping"
    return 0
  fi

  log "ingesting runbook '$title'"
  if [ "$USE_HTTP" -eq 1 ]; then
    # Use file_path so the server reads it directly — no JSON-escaping the markdown.
    local body resp http_code
    body=$(printf '{"title":"%s","tags":"runbook,cloud-ops","file_path":"%s"}' "$title" "$path")
    resp=$(api_post "/api/v1/knowledge" "$body") || {
      warn "ingest failed for $title"; return 1
    }
    http_code=$(printf '%s' "$resp" | tail -n1)
    case "$http_code" in
      201|200) log "  ok ($http_code)" ;;
      409)     log "  already exists (409) — skipping" ;;
      *)       warn "  ingest returned HTTP $http_code"; return 1 ;;
    esac
  else
    warn "CLI path: knowledge ingest not yet exposed via 'cyntr' subcommand."
    warn "         Set CYNTR_URL to use the HTTP API for runbook ingestion."
    return 1
  fi
}

# --- run ---------------------------------------------------------------------

log "installing 3 cloud-ops skills + 5 runbooks"
log "target: ${CYNTR_URL:-cyntr CLI}"

fail_count=0

for skill in "${SKILLS[@]}"; do
  install_skill "$skill" || fail_count=$((fail_count + 1))
done

for rb in "${RUNBOOKS[@]}"; do
  ingest_runbook "$rb" || fail_count=$((fail_count + 1))
done

if [ "$fail_count" -gt 0 ]; then
  warn "$fail_count operation(s) failed — see log above"
  exit 1
fi

log "done."
log "try: 'Our AWS bill jumped \$2,800 this week vs last. What changed?' in Slack"
