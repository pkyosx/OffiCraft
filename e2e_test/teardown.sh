#!/usr/bin/env bash
# e2e_test/teardown.sh — tear the isolated service down to a clean slate.
#   stop serve (ONLY our captured pid) -> drop isolated DB + state -> verify.
# Best-effort (no `set -e`): keeps going even if a step is a no-op.
# NEVER pkill/killall. NEVER touch a prod port — the set is PROD_PORTS from
# lib/common.sh (sourced below), whose CURRENT officraft entry is read at run
# time from server/ocserverd/config.go's defaultPort. Do NOT re-spell the ports
# as literals here: this header used to say ":8770/:8766", naming only a RETIRED
# default and a foreign product while the real prod port went unnamed (T-191d).
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
source "$(dirname "${BASH_SOURCE[0]}")/lib/tmux.sh"

echo "[teardown] base=$OC_E2E_BASE"

# 1. stop the exact per-run tmux session first.  A tmux pane can outlive the
#    setup shell, so killing only the recorded listener pid could leave a live
#    tmux server/session behind even after the port is released.
TMUX_SOCKET=""
TMUX_SESSION=""
if [ -f "$STATE_DIR/tmux.socket" ]; then
  TMUX_SOCKET="$(cat "$STATE_DIR/tmux.socket" 2>/dev/null || true)"
fi
if [ -f "$STATE_DIR/tmux.session" ]; then
  TMUX_SESSION="$(cat "$STATE_DIR/tmux.session" 2>/dev/null || true)"
fi
if [ -n "$TMUX_SOCKET" ] || [ -n "$TMUX_SESSION" ]; then
  if [ -n "$TMUX_SOCKET" ] && [ -n "$TMUX_SESSION" ]; then
    oc_e2e_tmux_stop "$TMUX_SOCKET" "$TMUX_SESSION" || true
  else
    echo "[teardown] WARN: incomplete tmux state (socket='${TMUX_SOCKET:-<empty>}' session='${TMUX_SESSION:-<empty>}') — refusing to guess a session." >&2
  fi
  oc_e2e_destroy "$STATE_DIR/tmux.socket" "$STATE_DIR/tmux.session"
fi

# 2. stop serve — kill BOTH the recorded listener pid AND the launch pid (the
#    socket holder can differ from the tmux pane/launch pid, so killing only the
#    launch pid could leave a stray listener). Only pids WE recorded.
for f in serve.pid serve.launch.pid; do
  if [ -f "$STATE_DIR/$f" ]; then
    PID=$(cat "$STATE_DIR/$f" 2>/dev/null || true)
    if [ -n "${PID:-}" ] && ps -p "$PID" >/dev/null 2>&1; then
      kill "$PID" && echo "[teardown] stopped $f pid=$PID"
    fi
    oc_e2e_destroy "$STATE_DIR/$f"
  fi
done

# 3. poll until the port is actually released (up to ~8s).
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

# 4. NOTE: when ocwarden-install specs (C2) are added, bootout the isolated warden
#    here by its EXACT launchctl label + rm its tokfile/plist. The minimal
#    skeleton installs no warden, so there is nothing to bootout yet.

# 5. drop isolated DB + run state (self-created only).
# T-ff8a: every delete here goes through oc_e2e_destroy (lib/common.sh) — it
# writes each target to $OC_E2E_DESTROY_RECORD before dispatching to a
# REPLACEABLE impl. Two reasons, both structural: (a) "what did this run delete"
# becomes an artifact a test can assert on instead of a question only a destroyed
# filesystem can answer, and (b) the T-ff8a regression test can run this REAL
# script with the recording impl, which it must, because that test deliberately
# breaks the guard it is testing and a test that leaned on that guard for its own
# safety would delete a real DB at exactly that moment.
oc_e2e_destroy "$REPO_ROOT/var/data"
oc_e2e_destroy "$STATE_DIR/owner.tok" "$STATE_DIR/env" "$STATE_DIR/serve.log" "$STATE_DIR/ocserverd"
echo "[teardown] dropped isolated DB + state"

# 5b. restore server/ocserverd/webdist/ to pristine (.gitkeep only). The go leg
# stages the SPA there for go:embed; the COMMITTED prebuilt bin/ocserverd must
# always be built from a pristine webdist (server/CLAUDE.md) — leaving the
# staged dist behind would bait a later rebuild into embedding it.
WEBDIST="$REPO_ROOT/server/ocserverd/webdist"
# best-effort (teardown runs without set -e): a failed/partial cleanup now prints
# a loud WARN to stderr instead of being swallowed by 2>/dev/null. See
# oc_restore_webdist_pristine in lib/common.sh for why a silent failure here is
# dangerous (stray SPA baked into the committed binary).
oc_restore_webdist_pristine "$WEBDIST" || true

# Closing reassurance to the operator. It MUST name the port prod is actually on
# — this line used to read "prod :8770/:8766 — untouched", which named only a
# RETIRED officraft default and a foreign product's port. Reassurance that names
# the wrong port is worse than none: it tells the operator the live station was
# spared while never mentioning it (T-191d, the message-level form of the same
# defect as the prod-port refusal lists). Derived from common.sh's PROD_PORTS /
# PROD_OFFICRAFT_PORT (SSOT: server/ocserverd/config.go's defaultPort) — never a
# second hand-copied constant; common.sh FATALs if that parse fails, so this can
# never degrade to a silent empty string. Retired/foreign ports stay listed
# (added to, not swapped for, the current one).
# Guarded by e2e_test/tests_guard/run.sh case (17).
echo "[teardown] prod ports — NOT managed by this harness (untouched): current officraft :$PROD_OFFICRAFT_PORT (server/ocserverd/config.go defaultPort); full refusal set: ${PROD_PORTS[*]}"
# T-ff8a: this run's resources are gone, so the arming that authorised deleting
# them is spent. Leaving it set would hand the NEXT run's EXIT trap an
# authorisation it never earned — the same "the trap runs anyway" shape, one run
# later. Last, not first: an arm file that outlives a half-failed teardown is the
# safe direction to be wrong in.
oc_e2e_disarm_teardown

echo "[teardown] ✅ clean"
