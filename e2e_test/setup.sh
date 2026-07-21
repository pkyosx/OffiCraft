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

# 3. start serve in the background (ambient fleet env stripped).
echo "[setup] starting isolated serve…"
oc_env nohup "$STATE_DIR/ocserverd" serve > "$STATE_DIR/serve.log" 2>&1 &
echo $! > "$STATE_DIR/serve.launch.pid"

# 4. wait for health.
ok=""
for _ in $(seq 1 30); do
  if curl -sf "$OC_E2E_BASE/api/version" >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
if [ -z "$ok" ]; then
  echo "[setup] FATAL: serve did not become healthy in 30s." >&2
  tail -20 "$STATE_DIR/serve.log" >&2
  exit 1
fi
SHA=$(curl -s "$OC_E2E_BASE/api/version" | py -c 'import sys,json;print(json.load(sys.stdin)["git_sha"])')
echo "[setup] serve healthy — git_sha=$SHA"

# 4b. record the ACTUAL listener pid — the socket holder can differ from the
#     nohup launch pid. teardown must kill THIS.
LISTEN_PID=$(lsof -nP -tiTCP:"$OC_E2E_PORT" -sTCP:LISTEN 2>/dev/null | head -1)
echo "${LISTEN_PID:-}" > "$STATE_DIR/serve.pid"
echo "[setup] listener pid=${LISTEN_PID:-unknown} (launch pid=$(cat "$STATE_DIR/serve.launch.pid"))"

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
