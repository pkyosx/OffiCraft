#!/usr/bin/env bash
# conformance/run.sh — one-shot runner for the language-agnostic black-box suite.
#
#   conformance/run.sh --target go   # isolated ocserverd (:8795, throwaway SQLite)
#
# go is the ONLY target since the Python retirement (rollback anchor: git tag
# py-final); the flag stays so the target vocabulary remains explicit. The run:
# temp oc.toml (via $OC_CONFIG) + temp SQLite → build ocserverd fresh → goose
# migrate → serve on an ISOLATED port → pytest with OC_TARGET_URL injected →
# teardown that kills ONLY the captured listener pid. Prod (:8770 officraft /
# :8766 vibe) and the e2e port (:8791) are never touched. Mirrors
# e2e_test/{setup,teardown}.sh discipline; kept self-contained because the
# lifecycles differ (temp config/db here vs repo oc.toml + var/data there).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"

# ── args ─────────────────────────────────────────────────────────────────────
TARGET=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --target) TARGET="${2:-}"; shift 2 ;;
    *) echo "[conformance] unknown arg: $1 (usage: run.sh --target go)" >&2; exit 64 ;;
  esac
done
if [[ "$TARGET" != "go" ]]; then
  echo "[conformance] usage: conformance/run.sh --target go" >&2
  echo "[conformance] (the py target retired with the Python backend — rollback anchor: git tag py-final)" >&2
  exit 64
fi

# ── black-box iron-rule gate (always, before anything runs) ──────────────────
# Conformance test code must NEVER import a server-implementation module —
# otherwise it stops being a language-agnostic behaviour definition. Same grep
# the ci.sh conformance gate runs (the forbidden names are the retired Python
# packages). *.py only (this script necessarily spells the forbidden names).
blackbox_hits="$(grep -RInE --include='*.py' \
  '^[[:space:]]*(import|from)[[:space:]]+(backend|service|dal|domain|plumbing)([.[:space:]]|$)' \
  "$HERE" || true)"
if [[ -n "$blackbox_hits" ]]; then
  echo "[conformance] FAIL — black-box violation: conformance/ imports backend modules:" >&2
  printf '  %s\n' "$blackbox_hits" >&2
  exit 1
fi
echo "[conformance] black-box lint OK (no backend imports in conformance/)"

# ── target scaffolding ────────────────────────────────────────────────────────
# Isolated, non-prod port: 8795 (e2e owns 8791; prod owns 8770/8766 — refused).
CONF_PORT="${OC_CONF_PORT:-8795}"
for _p in 8770 8766; do
  if [[ "$CONF_PORT" == "$_p" ]]; then
    echo "[conformance] FATAL: OC_CONF_PORT=$CONF_PORT is a PROD port — refuse." >&2
    exit 2
  fi
done
BASE="http://127.0.0.1:${CONF_PORT}"

# Suite venv: pytest+httpx ONLY (never a server-implementation stack — black-box).
CVENV="$HERE/.venv"
if [[ ! -x "$CVENV/bin/python" ]]; then
  echo "[conformance] creating suite venv (pytest+httpx only)"
  if command -v uv >/dev/null 2>&1; then
    uv venv --seed "$CVENV" >/dev/null
  else
    python3 -m venv "$CVENV"
  fi
fi
if ! "$CVENV/bin/python" -c "import pytest, httpx" >/dev/null 2>&1; then
  if command -v uv >/dev/null 2>&1; then
    uv pip install -q -r "$HERE/requirements.txt" --python "$CVENV/bin/python"
  else
    "$CVENV/bin/python" -m pip install -q -r "$HERE/requirements.txt"
  fi
fi

# routes_manifest.json is a FROZEN committed snapshot (it was mechanically
# extracted from the retired Python route table — tag py-final — and is now
# wire-freeze material alongside spec/*.json: change it spec-first, through the
# owner). The suite itself pins it against the live server: test_openapi_covers_
# manifest locks manifest ≡ spec operations, and the auth matrix locks every
# row's requires against live behaviour — a drifted manifest goes red in the run.

# Leftover guard — never stomp whatever already listens on the port.
if lsof -nP -iTCP:"$CONF_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[conformance] FATAL: :$CONF_PORT already in use — refuse to stomp it." >&2
  exit 2
fi

# Throwaway world: temp dir holds oc.toml + SQLite; nothing in the repo tree.
# The oc.toml is the post-B2 effective schema (port + dsn only — [auth] is
# retired); the owner password is seeded into the DB as a hash via the
# `ocserverd set-password` harness seam below (a real fresh install seeds
# none — the owner sets it through the first-run claim flow).
WORK="$(mktemp -d -t oc-conformance.XXXXXX)"
OWNER_PASSWORD="conf-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
DB_URL="sqlite:///$WORK/conformance.db"
cat >"$WORK/oc.toml" <<EOF
[server]
port = $CONF_PORT

[storage]
dsn = "$DB_URL"
EOF

SERVE_PID=""
LISTEN_PID=""
cleanup() {
  # Kill ONLY captured pids (launch pid + actual listener) — never a pattern kill.
  for pid in "$LISTEN_PID" "$SERVE_PID"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  # Grace, then hard-kill a survivor.
  for _ in 1 2 3 4 5; do
    { [[ -z "$SERVE_PID" ]] || ! kill -0 "$SERVE_PID" 2>/dev/null; } && break
    sleep 1
  done
  for pid in "$LISTEN_PID" "$SERVE_PID"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$WORK"
  echo "[conformance] teardown done (workdir removed)"
}
trap cleanup EXIT

# Strip ambient fleet env (same hazard e2e_test/lib/common.sh guards): OC_ID /
# OC_TOKEN / OC_BASE must never leak the isolated serve toward the prod server.
#
# OC_RELEASE_API_BASE (t-dc68): re-point the GitHub Releases update check at an
# unroutable loopback address — the black-box run must never reach the real
# api.github.com (hermeticity + the anonymous 60/hour rate limit); every check
# fails FAST and the wire answers its honest degraded faces deterministically.
oc_env() { env -u OC_ID -u OC_TOKEN -u OC_BASE OC_CONFIG="$WORK/oc.toml" \
             OC_DATABASE_URL="$DB_URL" OC_RELEASE_API_BASE="http://127.0.0.1:1" "$@"; }

# T-e731: the seed .md files, the prebuilt ocwarden/ocagent, and the frozen MCP
# catalog are served EMBED-ONLY (server/ocserverd/assets.go + api_machines.go —
# no disk fallback). The fresh ocserverd below therefore boots off its embed
# ALONE (running with CWD = repo root no longer feeds it disk assets), so it
# MUST be built with seedsdist/bindist STAGED or it boots with an empty embed
# (no seeds → boot 500s, no catalog → tools/list fails). Idempotent + guarded:
# skip when already staged (bin/ci.sh stages before invoking this step) so a
# standalone run.sh self-stages without a redundant ocwarden/ocagent rebuild.
if ! ls "$REPO_ROOT"/server/ocserverd/seedsdist/*.md >/dev/null 2>&1; then
  echo "[conformance] staging seedsdist (embed-only seeds)"
  bash "$REPO_ROOT/bin/build-seedsdist"
fi
if [[ ! -f "$REPO_ROOT/server/ocserverd/bindist/ocwarden" ]]; then
  echo "[conformance] staging bindist (embed-only binaries + frozen catalog)"
  bash "$REPO_ROOT/bin/build-bindist"
fi
# Build ocserverd fresh from source into the throwaway dir, then migrate +
# serve against the temp oc.toml/SQLite.
echo "[conformance] building ocserverd (go build from server/ocserverd)"
(cd "$REPO_ROOT/server/ocserverd" && go build -o "$WORK/ocserverd" .)

echo "[conformance] migrate (ocserverd migrate → $DB_URL)"
(cd "$REPO_ROOT" && oc_env "$WORK/ocserverd" migrate >/dev/null)

echo "[conformance] seeding owner password (ocserverd set-password, hash → DB settings)"
(cd "$REPO_ROOT" && oc_env env OC_NEW_PASSWORD="$OWNER_PASSWORD" "$WORK/ocserverd" set-password >/dev/null)

echo "[conformance] starting isolated ocserverd on $BASE"
(cd "$REPO_ROOT" && oc_env nohup "$WORK/ocserverd" serve >"$WORK/serve.log" 2>&1) &
SERVE_PID=$!

ok=""
for _ in $(seq 1 30); do
  if curl -sf "$BASE/api/version" >/dev/null 2>&1; then ok=1; break; fi
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then break; fi
  sleep 1
done
if [[ -z "$ok" ]]; then
  echo "[conformance] FATAL: serve did not become healthy in 30s. serve.log tail:" >&2
  tail -20 "$WORK/serve.log" >&2 || true
  exit 1
fi
# The ACTUAL listener pid can differ from the launch pid — capture the socket holder.
LISTEN_PID="$(lsof -nP -tiTCP:"$CONF_PORT" -sTCP:LISTEN 2>/dev/null | head -1 || true)"
echo "[conformance] serve healthy (launch pid=$SERVE_PID listener pid=${LISTEN_PID:-$SERVE_PID})"

echo "[conformance] pytest (OC_TARGET_URL=$BASE)"
set +e
env -u OC_ID -u OC_TOKEN -u OC_BASE \
  OC_TARGET_URL="$BASE" OC_OWNER_PASSWORD="$OWNER_PASSWORD" \
  "$CVENV/bin/python" -m pytest "$HERE" -q
RC=$?
set -e

if [[ $RC -eq 0 ]]; then
  echo "[conformance] all green (target=$TARGET base=$BASE)"
else
  echo "[conformance] FAILED (target=$TARGET exit=$RC)" >&2
fi
exit $RC
