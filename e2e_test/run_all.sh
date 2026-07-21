#!/usr/bin/env bash
# e2e_test/run_all.sh — one-shot: setup -> playwright specs -> teardown.
# teardown ALWAYS runs (EXIT trap), even if a spec fails or setup aborts.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib/common.sh"

cleanup() { echo; echo "[run_all] === TEARDOWN ==="; bash "$HERE/teardown.sh" || true; }
trap cleanup EXIT

echo "[run_all] === SETUP ==="
if ! bash "$HERE/setup.sh"; then
  echo "[run_all] setup failed — aborting (teardown will still run)." >&2
  exit 1
fi

# Export persisted state (OC_E2E_BASE / OC_E2E_TOKEN) + password for the specs
# (setup.sh seeded a per-run random password into the DB and persisted it).
# Each prerequisite below aborts EXPLICITLY. This script runs without `-e` (it
# must survive a failing spec long enough to report the rc), so nothing aborts
# implicitly here — the abort has to be written out, and loudly. Until T-d41a
# these steps died silently on an `-e` leaked in from lib/common.sh.
set -a; source "$STATE_DIR/env" || { echo "[run_all] FATAL: cannot source $STATE_DIR/env" >&2; set +a; exit 1; }; set +a
OC_E2E_PASSWORD=$(cat "$STATE_DIR/owner.password") || { echo "[run_all] FATAL: cannot read $STATE_DIR/owner.password" >&2; exit 1; }
export OC_E2E_PASSWORD

echo "[run_all] === E2E (playwright) ==="
# Warden spec (05) prerequisite: build BOTH cli binaries IN-TREE so the warden's
# spawn shim can resolve ocagent. resolveOcAgentBin walks three parents up from
# the ocwarden executable to find <repoRoot>/cli/ocagent/ocagent — the spec's
# default ocwarden path (REPO_ROOT/../ocwarden) walks to /Users and symlinks a
# BROKEN ocagent into the spawned agent's workdir (a deaf agent that only comes
# online if claude self-rescues in time — the presence-timeout flake). In-tree
# builds restore the dev layout the resolver is written for. Both artifacts are
# gitignored.
echo "[run_all] building in-tree cli binaries (ocagent + ocwarden) for spec 05…"
(cd "$REPO_ROOT/cli/ocagent" && go build -o ocagent .) || { echo "[run_all] FATAL: go build cli/ocagent failed — spec 05 would flake on a stale/absent binary." >&2; exit 1; }
(cd "$REPO_ROOT/cli/ocwarden" && go build -o ocwarden .) || { echo "[run_all] FATAL: go build cli/ocwarden failed." >&2; exit 1; }
export OC_E2E_OCWARDEN="$REPO_ROOT/cli/ocwarden/ocwarden"
cd "$HERE"
# Broken nvm lazy-load workaround: unset shell funcs, use homebrew binaries.
NPM=/opt/homebrew/bin/npm
NPX=/opt/homebrew/bin/npx
if [ ! -d "$HERE/node_modules/@playwright/test" ]; then
  echo "[run_all] installing @playwright/test (first run)…"
  ( unset -f node npm 2>/dev/null; "$NPM" install --no-audit --no-fund )
fi
# Browser render specs (B1/B6) need a real Chromium; install is idempotent (fast
# no-op once cached). API-only specs don't use it but this keeps run_all complete.
( unset -f node npm npx 2>/dev/null; "$NPX" playwright install chromium )
( unset -f node npm npx 2>/dev/null; "$NPX" playwright test )
RC=$?
echo "[run_all] specs exit=$RC"
exit $RC
