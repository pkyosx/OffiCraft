#!/usr/bin/env bash
# e2e_test/setup.sh — bring up an ISOLATED officraft service for e2e.
#   fresh DB -> migrate -> serve (:8791) -> health -> login -> persist state.
# Refuses if :8791 is already in use (won't stomp an existing serve).
# All prod-safety guards live in lib/common.sh.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
source "$(dirname "${BASH_SOURCE[0]}")/lib/tmux.sh"

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
# The private tmux state is another leftover guard.  Reusing a state file could
# make a later teardown target an earlier run's session, so refuse before the
# teardown is armed and before any new resource is created.
if [ -e "$STATE_DIR/tmux.socket" ] || [ -e "$STATE_DIR/tmux.session" ]; then
  echo "[setup] FATAL: isolated tmux state already exists in $STATE_DIR — run teardown.sh first; refusing to overwrite the earlier session identity." >&2
  exit 2
fi
# A plain nohup child is reaped by the agent executor when this script's exec
# ends.  tmux is an explicit prerequisite for the independent-exec contract;
# fail here, before arming/mutating, rather than later as a misleading refused
# connection to :$OC_E2E_PORT.
if ! oc_e2e_tmux_require; then
  exit 2
fi

# 1b. ARM THE TEARDOWN (T-ff8a). This line is the boundary of the script:
#     everything above it is a REFUSAL gate that has created nothing, everything
#     below it creates or destroys. run_all.sh's EXIT trap tears down only an
#     ARMED run, so a refusal above (each of which `exit 2`s) now leaves the trap
#     with nothing to do — instead of, as before, ending in the very
#     `rm -rf "$REPO_ROOT/var/data"` the guard had just refused to allow.
#     It is armed BEFORE the first mutation, not after setup succeeds: a setup
#     that dies half-way through HAS created things and must still be cleaned up.
oc_e2e_arm_teardown

# Per-run tmux names are state, not a shared/default socket.  uuidgen is already
# required below for the per-run owner password, and the prefix is validated by
# lib/tmux.sh before either start or stop can touch tmux.
TMUX_RUN_ID="$(uuidgen | tr -d '-' | tr '[:upper:]' '[:lower:]')"
TMUX_SOCKET="oc-e2e-$TMUX_RUN_ID"
TMUX_SESSION="oc-e2e-$TMUX_RUN_ID"
printf '%s\n' "$TMUX_SOCKET" > "$STATE_DIR/tmux.socket"
printf '%s\n' "$TMUX_SESSION" > "$STATE_DIR/tmux.session"

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

# 2b2. stage the product-guide docs for go:embed. UNCONDITIONAL (not gated on
#      OC_E2E_SKIP_BUILD): it is a directory copy, needs no npm, and the `go
#      build` right below is what bakes the embed in — skipping it would hand
#      the API specs a server whose every doc read 404s.
#
#      This step was MISSING until T-68f1 round 2. bin/build, bin/ci.sh and
#      conformance/run.sh all stage docsdist; only this harness did not. Because
#      doc reads are embed-only (assets.go, no disk fallback), anyone following
#      the README straight into run_all.sh got a server whose 使用說明 page was
#      EMPTY and whose /api/docs/<slug> all 404'd — unless a previous bin/build
#      happened to have left docsdist populated. That is the structural reason
#      the REAL embedded docs had never once been rendered end to end.
echo "[setup] staging product-guide docs (docs/guide → docsdist, go:embed)…"
bash "$REPO_ROOT/bin/build-docsdist" \
  || { echo "[setup] FATAL: build-docsdist failed" >&2; exit 1; }

# 2b3. stage the remaining embed-only server assets. A plain go build with
#      empty seedsdist/bindist produces a binary that starts but cannot boot an
#      agent persona, serve the MCP catalog, or provide warden self-update
#      binaries. That silent partial build makes real-runtime E2E misleading:
#      Codex can complete its MCP handshake, then receive zero tools because
#      tools/list fails with "catalog unavailable". Match bin/ci.sh and
#      conformance/run.sh by staging both trees unconditionally before build.
echo "[setup] staging seeds and bindist assets (go:embed)…"
bash "$REPO_ROOT/bin/build-seedsdist" \
  || { echo "[setup] FATAL: build-seedsdist failed" >&2; exit 1; }
bash "$REPO_ROOT/bin/build-bindist" \
  || { echo "[setup] FATAL: build-bindist failed" >&2; exit 1; }

# 2c. build ocserverd fresh from source (with all staged assets baked in), then
#     migrate. The daemon runs with CWD = repo root so its oc.toml / DSN resolve
#     exactly like bin/serve (conformance/run.sh --target go convention).
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

# 3. start serve in a detached, per-run tmux session (ambient fleet env stripped).
echo "[setup] starting isolated serve in tmux (socket=$TMUX_SOCKET session=$TMUX_SESSION)…"
if ! SERVE_LAUNCH_PID="$(oc_e2e_tmux_start "$TMUX_SOCKET" "$TMUX_SESSION" "$REPO_ROOT" "$STATE_DIR/ocserverd" "$STATE_DIR/serve.log")"; then
  echo "[setup] FATAL: could not start isolated serve in tmux (socket=$TMUX_SOCKET session=$TMUX_SESSION)." >&2
  exit 1
fi
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
    # `|| true` is load-bearing under this file's `set -euo pipefail`, and it
    # is the same guard conformance/run.sh's twin of this line already had. If
    # the pipeline ever returns non-zero (SIGPIPE 141, `py` missing), `set -e`
    # would kill setup.sh AT THE ASSIGNMENT — no message, exit 1 — and the
    # identity check below would never run. That silent-death shape is exactly
    # what T-a3ba fixed in run.sh's prod-port guard; this line had the same
    # hazard and was left asymmetric by the same ticket. An empty GOT_SHA is
    # not swallowed: the check below treats it as a mismatch and FATALs.
    GOT_SHA=$(printf '%s' "$RESP" | py -c 'import sys,json
try:
    print(json.load(sys.stdin).get("git_sha",""))
except Exception:
    print("")' 2>/dev/null || true)
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
  echo "[setup] FATAL: health 200 but identity mismatch — /api/version reported git_sha='${GOT_SHA:-<empty>}', expected '$EXPECTED_SHA' (this checkout's HEAD). launch pid=$SERVE_LAUNCH_PID listener pid=$LISTEN_PID. Either the 200 came from a DIFFERENT process (a leftover listener from an earlier run, or someone else's server), or THIS CHECK IS WRONG (e.g. the server's gitSHA() probe timed out and reported 'unknown') and the listener is the ocserverd we just built. Do NOT assume the former and go hunting: run 'bash teardown.sh', which decides by evidence — it kills the port holder only if its command line is this run's own binary ($STATE_DIR/ocserverd), and leaves anything else alone. If teardown reports it left the listener alone, THEN it is not ours — find and stop it, then re-run." >&2
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
