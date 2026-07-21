#!/usr/bin/env bash
# e2e_test/setup.sh — bring up an ISOLATED officraft service for e2e.
#   fresh DB -> migrate -> serve (:8791) -> health -> login -> persist state.
# Refuses if :8791 is already in use (won't stomp an existing serve).
# All prod-safety guards live in lib/common.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

cd "$REPO_ROOT"
mkdir -p "$STATE_DIR"
echo "[setup] repo=$REPO_ROOT  base=$OC_E2E_BASE  target=$OC_E2E_TARGET"

# 0. oc.toml must exist and point at the non-prod port (gitignored; from example).
#    No [auth] needed since B2 — the owner password is seeded into the fresh DB
#    below (step 2d) via the `ocserverd set-password` seam.
if [ ! -f "$REPO_ROOT/oc.toml" ]; then
  echo "[setup] FATAL: oc.toml missing — cp oc.toml.example oc.toml, set" \
       "[server].port=$OC_E2E_PORT and a repo-local [storage].dsn (e.g. sqlite:///var/data/e2e.db)." >&2
  exit 1
fi
if ! grep -Eq "port[[:space:]]*=[[:space:]]*$OC_E2E_PORT" "$REPO_ROOT/oc.toml"; then
  echo "[setup] FATAL: oc.toml port != $OC_E2E_PORT — refuse (prod guard)." >&2
  exit 2
fi
# The DSN convention default is the CANONICAL ~/.officraft/server DB since
# B2 — an e2e oc.toml without an explicit repo-local sqlite:///var/… DSN would
# aim the isolated serve (and the fresh-DB wipe below) at a real install.
E2E_DSN=$(py -c 'import tomllib,sys;print(tomllib.load(open(sys.argv[1],"rb")).get("storage",{}).get("dsn",""))' "$REPO_ROOT/oc.toml")
case "$E2E_DSN" in
  sqlite:///var/*) : ;;
  *)
    echo "[setup] FATAL: oc.toml [storage].dsn must be an explicit repo-local sqlite:///var/… path (got '${E2E_DSN:-unset}') — refuse (prod-DB guard)." >&2
    exit 2 ;;
esac

# 1. leftover guard — never stomp whatever is already on the port.
if lsof -nP -iTCP:"$OC_E2E_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[setup] FATAL: :$OC_E2E_PORT already in use — run teardown.sh first." >&2
  exit 2
fi

# 2. fresh DB (migrate runs in 2c, after the build steps).
rm -rf "$REPO_ROOT/var/data"

# 2b. build the SPA (real API, NOT mock) so the browser render specs have a
#     mounted cockpit. ocserverd bakes the SPA into the binary via go:embed, so
#     dist/ must be staged into server/ocserverd/webdist/ BEFORE `go build`.
#     API-only specs don't need it, but building unconditionally keeps run_all
#     a complete big-version smoke. Set OC_E2E_SKIP_BUILD=1 to skip when
#     running only API specs.
if [ "${OC_E2E_SKIP_BUILD:-}" != "1" ]; then
  echo "[setup] building frontend SPA (VITE_USE_MOCK=false)…"
  (
    cd "$REPO_ROOT/frontend"
    # Broken nvm lazy-load workaround: drop shell funcs, then resolve npm
    # portably (PATH first, common-location fallback) instead of a hardcoded
    # homebrew abspath — see oc_resolve_bin in lib/common.sh.
    unset -f node npm 2>/dev/null || true
    NPM="$(oc_resolve_bin npm)" || { echo "[setup] FATAL: npm not found (checked PATH + common locations) — cannot build SPA." >&2; exit 1; }
    if [ ! -d node_modules ]; then "$NPM" install --no-audit --no-fund; fi
    VITE_USE_MOCK=false "$NPM" run build
  ) || { echo "[setup] FATAL: frontend SPA build failed" >&2; exit 1; }
  # Stage dist → webdist for go:embed (bin/build-webdist's staging step; the
  # npm build itself already ran above with the nvm workaround).
  WEBDIST="$REPO_ROOT/server/ocserverd/webdist"
  rm -rf "$WEBDIST" && mkdir -p "$WEBDIST"
  cp -R "$REPO_ROOT/frontend/dist/." "$WEBDIST/"
  touch "$WEBDIST/.gitkeep"
  echo "[setup] staged frontend/dist → server/ocserverd/webdist"
fi

# 2c. build ocserverd fresh from source (with the staged SPA baked in), then
#     migrate. The daemon runs with CWD = repo root so its oc.toml / DSN /
#     repo-file assets resolve exactly like bin/serve (conformance/run.sh
#     --target go convention).
echo "[setup] building ocserverd (go build from server/ocserverd)…"
(cd "$REPO_ROOT/server/ocserverd" && go build -o "$STATE_DIR/ocserverd" .) \
  || { echo "[setup] FATAL: ocserverd build failed" >&2; exit 1; }
echo "[setup] migrate (ocserverd migrate, goose)…"
oc_env "$STATE_DIR/ocserverd" migrate

# 2d. seed the owner password into the fresh DB (hash via set-password — the
#     post-B2 fresh-install seam; oc.toml carries no [auth]). Random per run,
#     persisted for run_all/specs.
PW="e2e-$(uuidgen | tr '[:upper:]' '[:lower:]')"
echo "[setup] seeding owner password (ocserverd set-password, hash → DB settings)…"
oc_env env OC_NEW_PASSWORD="$PW" "$STATE_DIR/ocserverd" set-password >/dev/null
printf '%s\n' "$PW" > "$STATE_DIR/owner.password"

# 2e. leftover guard, re-checked (TOCTOU close, T-a3ba): step 1's guard ran
#     before the frontend build (2b) + go build (2c) + migrate + seed (2d) —
#     tens of seconds of window in which nothing re-checked the port. A
#     listener that grabbed :$OC_E2E_PORT during that window would go
#     undetected by step 1, and (before this change) would have been
#     indistinguishable from our own serve by the health-check loop below —
#     its 200 would satisfy `ok=1` just as well as ours. Re-check immediately
#     before we actually bind.
if lsof -nP -iTCP:"$OC_E2E_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[setup] FATAL: :$OC_E2E_PORT became occupied during build/migrate/seed (TOCTOU window) — refuse to stomp it. Find and stop that listener, then re-run." >&2
  exit 2
fi

# 3. start serve in the background (ambient fleet env stripped).
echo "[setup] starting isolated serve…"
oc_env nohup "$STATE_DIR/ocserverd" serve > "$STATE_DIR/serve.log" 2>&1 &
SERVE_LAUNCH_PID="$!"
echo "$SERVE_LAUNCH_PID" > "$STATE_DIR/serve.launch.pid"

# Expected build identity: gitSHA() (server/ocserverd/server.go) is unstamped
# here (plain `go build`, no -ldflags) so its boot-time fallback runs
# `git rev-parse --short HEAD` in CWD — and serve's CWD is $REPO_ROOT (setup.sh
# line 9: `cd "$REPO_ROOT"`). Compute the same probe from the shell so we have
# something to compare the responder's self-report against.
EXPECTED_SHA=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)

# 4. wait for health.
ok=""
GOT_SHA=""
for _ in $(seq 1 30); do
  if RESP=$(curl -sf "$OC_E2E_BASE/api/version" 2>/dev/null); then
    GOT_SHA=$(printf '%s' "$RESP" | py -c 'import sys,json
try:
    print(json.load(sys.stdin).get("git_sha",""))
except Exception:
    print("")' 2>/dev/null)
    ok=1
    break
  fi
  sleep 1
done
if [ -z "$ok" ]; then
  echo "[setup] FATAL: serve did not become healthy in 30s." >&2
  tail -20 "$STATE_DIR/serve.log" >&2
  exit 1
fi

# 4a. record the ACTUAL listener pid — the socket holder can differ from the
#     nohup launch pid. teardown must kill THIS. AMBIGUOUS (0 or >1
#     candidates) is a hard failure, never a silent `head -1` pick — mirrors
#     e2e_test/a1_zombie_e2e.sh's listener_pid_of, which treats "none or >1"
#     as empty/refuse rather than guessing.
LISTEN_CANDIDATES=()
while IFS= read -r _cand; do
  [ -n "$_cand" ] && LISTEN_CANDIDATES+=("$_cand")
done < <(lsof -nP -tiTCP:"$OC_E2E_PORT" -sTCP:LISTEN 2>/dev/null || true)
if [ "${#LISTEN_CANDIDATES[@]}" -ne 1 ]; then
  # bash-3.2-safe empty-array expansion (same hazard as oc_lifecycle.sh's
  # `reasons` array) — never bare "${LISTEN_CANDIDATES[*]}" under set -u.
  _cand_list=""
  for _c in ${LISTEN_CANDIDATES[@]+"${LISTEN_CANDIDATES[@]}"}; do
    _cand_list="$_cand_list $_c"
  done
  echo "[setup] FATAL: health check got HTTP 200 on :$OC_E2E_PORT but the listener pid is AMBIGUOUS (${#LISTEN_CANDIDATES[@]} candidates:${_cand_list:- none}) — refusing to guess which one answered us (launch pid=$SERVE_LAUNCH_PID). Investigate and stop the extra listener(s), then re-run." >&2
  exit 1
fi
LISTEN_PID="${LISTEN_CANDIDATES[0]}"

# Identity check #1 (content-level): the responder must self-report the
# git_sha we expect — this data was already being fetched above (as `SHA`
# used to be, printed but never compared); now it gates.
if [ -z "$GOT_SHA" ] || [ "$GOT_SHA" != "$EXPECTED_SHA" ]; then
  echo "[setup] FATAL: health 200 but identity mismatch — /api/version reported git_sha='${GOT_SHA:-<empty>}', expected '$EXPECTED_SHA' (this checkout's HEAD). launch pid=$SERVE_LAUNCH_PID listener pid=$LISTEN_PID. The 200 almost certainly came from a DIFFERENT process (a leftover listener from an earlier run, or someone else's server) — not the ocserverd we just built. Find and stop that listener, then re-run." >&2
  exit 1
fi

# Identity check #2 (process-level): the listener's own command line must be
# the exact binary we just built for THIS run ($STATE_DIR/ocserverd) — the
# check that would catch a leftover/foreign binary at the same path from an
# earlier run whose serve never got torn down cleanly (candidate #1 in
# T-a3ba recon), even when it happens to be running the same commit.
LISTEN_CMD=$(ps -p "$LISTEN_PID" -o command= 2>/dev/null || true)
case "$LISTEN_CMD" in
  "$STATE_DIR/ocserverd"*) : ;;
  *)
    echo "[setup] FATAL: health 200 but identity mismatch — listener pid=$LISTEN_PID's command ('${LISTEN_CMD:-<unknown>}') is not our binary ($STATE_DIR/ocserverd), even though git_sha matched. launch pid=$SERVE_LAUNCH_PID. Find and stop the other process, then re-run." >&2
    exit 1
    ;;
esac

SHA="$GOT_SHA"
echo "${LISTEN_PID:-}" > "$STATE_DIR/serve.pid"
echo "[setup] serve healthy AND identity-verified — git_sha=$SHA listener pid=$LISTEN_PID (launch pid=$SERVE_LAUNCH_PID)"

# 5. login -> owner token (the password seeded in 2d).
TOKEN=$(curl -sf -X POST "$OC_E2E_BASE/api/login" -H 'content-type: application/json' \
  -d "{\"password\":\"$PW\"}" | py -c 'import sys,json;print(json.load(sys.stdin)["token"])')
if [ -z "${TOKEN:-}" ]; then
  echo "[setup] FATAL: login failed." >&2
  exit 1
fi
echo "$TOKEN" > "$STATE_DIR/owner.tok"

# 6. persist state for the spec runner.
cat > "$STATE_DIR/env" <<EOF
OC_E2E_BASE=$OC_E2E_BASE
OC_E2E_TOKEN=$TOKEN
EOF
echo "[setup] ✅ ready — base=$OC_E2E_BASE  token→$STATE_DIR/owner.tok"
