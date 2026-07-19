#!/usr/bin/env bash
# e2e_test/teardown.sh — tear the isolated service down to a clean slate.
#   stop serve (ONLY our captured pid) -> drop isolated DB + state -> verify.
# Best-effort (no `set -e`): keeps going even if a step is a no-op.
# NEVER pkill/killall. NEVER touch prod :8770/:8766.
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"

echo "[teardown] base=$OC_E2E_BASE"

# 1. stop serve — kill BOTH the recorded listener pid AND the launch pid (the
#    socket holder can differ from the nohup launch pid, so killing only the
#    launch pid could leave a stray listener). Only pids WE recorded.
for f in serve.pid serve.launch.pid; do
  if [ -f "$STATE_DIR/$f" ]; then
    PID=$(cat "$STATE_DIR/$f" 2>/dev/null || true)
    if [ -n "${PID:-}" ] && ps -p "$PID" >/dev/null 2>&1; then
      kill "$PID" && echo "[teardown] stopped $f pid=$PID"
    fi
    rm -f "$STATE_DIR/$f"
  fi
done

# 2. poll until the port is actually released (up to ~8s).
released=""
for _ in $(seq 1 16); do
  if ! lsof -nP -iTCP:"$OC_E2E_PORT" -sTCP:LISTEN >/dev/null 2>&1; then released=1; break; fi
  sleep 0.5
done
if [ -z "$released" ]; then
  # Last resort: the listener is provably ours — the port is a guaranteed
  # NON-PROD port (common.sh refuses prod) and we confirm the command
  # signature (the ocserverd WE built into .state/) before killing. This is
  # not a blind pkill.
  STRAY=$(lsof -nP -tiTCP:"$OC_E2E_PORT" -sTCP:LISTEN 2>/dev/null | head -1)
  if [ -n "${STRAY:-}" ] && ps -p "$STRAY" -o command= 2>/dev/null \
       | grep -Eq "$STATE_DIR/ocserverd"; then
    kill "$STRAY" && echo "[teardown] killed stray isolated listener pid=$STRAY (e2e serve on :$OC_E2E_PORT)"
    sleep 1
  fi
  for _ in $(seq 1 10); do
    lsof -nP -iTCP:"$OC_E2E_PORT" -sTCP:LISTEN >/dev/null 2>&1 || { released=1; break; }
    sleep 0.5
  done
fi
if [ -n "$released" ]; then
  echo "[teardown] :$OC_E2E_PORT released"
else
  echo "[teardown] WARN: :$OC_E2E_PORT still listening — inspect manually (not force-killing a process we did not launch)." >&2
fi

# 3. NOTE: when ocwarden-install specs (C2) are added, bootout the isolated warden
#    here by its EXACT launchctl label + rm its tokfile/plist. The minimal
#    skeleton installs no warden, so there is nothing to bootout yet.

# 4. drop isolated DB + run state (self-created only).
rm -rf "$REPO_ROOT/var/data"
rm -f "$STATE_DIR/owner.tok" "$STATE_DIR/env" "$STATE_DIR/serve.log" "$STATE_DIR/ocserverd"
echo "[teardown] dropped isolated DB + state"

# 4b. restore server/ocserverd/webdist/ to pristine (.gitkeep only). The go leg
# stages the SPA there for go:embed; the COMMITTED prebuilt bin/ocserverd must
# always be built from a pristine webdist (server/CLAUDE.md) — leaving the
# staged dist behind would bait a later rebuild into embedding it.
WEBDIST="$REPO_ROOT/server/ocserverd/webdist"
if [ -d "$WEBDIST" ]; then
  find "$WEBDIST" -mindepth 1 -not -name '.gitkeep' -delete 2>/dev/null
  echo "[teardown] restored server/ocserverd/webdist to pristine (.gitkeep only)"
fi

echo "[teardown] prod :8770/:8766 — not managed by this harness (untouched)"
echo "[teardown] ✅ clean"
