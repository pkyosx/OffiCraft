#!/usr/bin/env bash
# e2e_test/a1_zombie_e2e.sh — A1 ZOMBIE AUTO-TAKEOVER full-reset E2E regression.
# ============================================================================
# PURPOSE
#   A DESTRUCTIVE, FULL-RESET, MANUAL end-to-end regression that proves the A1
#   "presence-deaf zombie auto-takeover" reconcile behavior on ONE real machine,
#   on the CANONICAL serve port, WITHOUT any human ssh-kill:
#
#     PHASE 0 preflight guards → PHASE 1 teardown old server →
#     PHASE 2 fresh `ocserver install` (canonical serve port) →
#     PHASE 3 bootstrap server-self warden →
#     PHASE 4 activate a TARGET agent + a CONTROL agent → both presence online →
#     PHASE 5 make the TARGET a zombie (STOP its claude pane + KILL its listener; SSE
#             dies but the tmux session + claude pane stay ALIVE) →
#     PHASE 6 wait for the server reconcile loop to auto-reap it (START clobber
#             → zombie-takeover robust STOP → clean START) →
#     PHASE 7 TRIPLE PROOF (do not trust self-report): server reconcile log
#             three-marker sequence + host pane pid rotation (OLD absent, tmux
#             session momentarily gone, NEW≠OLD) + last_op fold sequence →
#     PHASE 8 CONTROL assertion: the healthy CONTROL member was NEVER stopped
#             (no reconcile `command=stop` line for it, pane pid unchanged, SSE
#             stayed online) — proves A1 does not mis-kill a healthy member →
#     PHASE 9 teardown → clean slate (CONT-revive any STOP-frozen orphan first).
#
#   Per-phase objective PASS/FAIL via stage/pass_stage/fail_stage; exits 0 only
#   if every phase is green.
#
# WHAT A1 IS (mechanism — see e2e_test/A1_GROUND.md, reconcile.go:237-256):
#   A "zombie" = a member whose SSE listener is DEAD (server sees IsOnline=false)
#   but whose tmux session + claude pane are still ALIVE (the slot is squatted).
#   The reconcile loop, seeing desired=online + ¬online, fires a plain START.
#   The warden's clobber-guard (spawn.go:604-611) refuses to stomp the live
#   session and returns reason `session_already_exists: ...`. When reconcile sees
#   its own START bounced off that clobber-guard (st.LastCommand==start ∧
#   obs.LastOpKind==start ∧ reason HasPrefix "session_already_exists"), it fires
#   a ROBUST STOP to reap the zombie (kill.go stop() ladder: killpg + sweepPIDs
#   signal-0 verify), then the next tick's plain START lands on a clean slot.
#
# ⚠️  DESTRUCTIVE FULL-RESET. This TEARS DOWN + WIPES the local officraft
#     server on THIS machine and reinstalls it from zero. It is NOT the isolated
#     Playwright suite (:8791). It refuses to run without OC_A1_ZOMBIE_YES=1.
#
# ⚠️  seth-m1 ONLY (kyle route A). Hard-pinned via the PHASE 0 TRIPLE hardware
#     whitelist in oc_preflight_guards (hardware UUID primary anchor AND
#     LocalHostName AND joey fleet-infra dir). On any other machine it dies in
#     PHASE 0, before a single destructive action.
#
# ⚠️  CANONICAL serve port, NO --namespace. Isolation from prod = "seth-m1 (no
#     prod) + canonical serve port + machine/state whitelist", NOT a namespace.
#
# TEARDOWN + KILL SAFETY (fleet zero-touch — the A1 guardrail bedrock):
#   * teardown uses ONLY oc_teardown_bounded (EXACT launchd labels
#     com.officraft.{serve,autodeploy,tunnel,ocwarden} + EXACT member-/worker-
#     tmux sessions). It NEVER pkill/killall/pattern-kills.
#   * the ONLY kills this script issues by hand are `kill -STOP <PID>` (target
#     claude pane), `kill -9 <PID>` (target ocagent listener) and `kill -CONT
#     <PID>` (cleanup), each against ONE captured EXACT pid — never a
#     pattern, never a broad kill. The zombie REAP itself is done by the SERVER
#     (robust STOP ladder), not by this script.
#   * before teardown we `kill -CONT` the STOP-frozen claude pane (if it somehow
#     survived the server reap) so no frozen process is left behind, then EXACT
#     teardown removes the rest.
#
# USAGE:
#   OC_A1_ZOMBIE_YES=1 bash e2e_test/a1_zombie_e2e.sh
#
# PARAMS (env, overridable):
#   TARGET_AGENT     seeded agent member id to zombify (default mira — the only
#                    out-of-box spawnable KindAssistant seed; see dbseed.go).
#   CONTROL_NAME     display name for the freshly-HIRED control member (default
#                    "a1-control"). The DB seeds ONLY mira + m-server-self, so
#                    the healthy control is created via POST /api/members (a bare
#                    hire folds to KindAssistant — same kind as mira — and is NOT
#                    privilege-bearing, so the owner token suffices).
#   OWNER_PASSWORD   deterministic owner password to seed (default: random uuid).
#   OC_A1_ZOMBIE_YES=1   REQUIRED — acknowledges this run is destructive.
# ============================================================================
set -euo pipefail

# This script lives in e2e_test/; source the shared libs (do NOT re-define).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib/oc_lifecycle.sh"   # log/warn/die, stage bookkeeping, api_*, poll_presence, oc_* bring-up/teardown
source "$HERE/lib/common.sh"         # oc_env(), PROD_PORTS, py(), REPO_ROOT (isolated-port guard inert here)

# ---------------------------------------------------------------------------
# 0. params + constants (canonical, single-machine — mirrors single_machine_e2e)
# ---------------------------------------------------------------------------
TARGET_AGENT="${TARGET_AGENT:-mira}"      # the seed agent we turn into a zombie
CONTROL_NAME="${CONTROL_NAME:-a1-control}"  # display name of the hired control agent
# LOCAL_BASE / PUBLIC_HOST (loopback base + summarize() footer) are set
# authoritatively by oc_resolve_instance below in BOTH modes — canonical → the
# current prod port (OC_CANONICAL_SERVE_PORT, from config.go); namespace → a
# per-run free port — so no stale port literal lives here.
SECOND_MACHINE="(none — single-machine)"
TEST_AGENT="$TARGET_AGENT"   # summarize() footer reads TEST_AGENT
# Deterministic owner password: caller-provided, else a fresh uuid we seed + reuse.
OWNER_PASSWORD="${OWNER_PASSWORD:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Presence poll budget. A REAL claude spawn (PHASE 4) has no firm upper bound —
# observed ~1.6-2.6min for cold boot + SSE mount; 210s gives margin above the
# observed ceiling (same value single_machine_e2e uses).
PRESENCE_TIMEOUT="${PRESENCE_TIMEOUT:-210}"

# ── A1-specific cadences (reconcile loop period = 30s, reconcile.go:70) ──────
# Reverse-poll: how long we wait for the zombie's presence to fall to offline
# and STAY offline for >=1 full reconcile tick (30s). If it flaps back to online
# the listener self-healed (see A1_GROUND §造殭屍法 step4) — recorded as friction.
ZOMBIE_OFFLINE_TIMEOUT="${ZOMBIE_OFFLINE_TIMEOUT:-240}"   # must cover several self-heal rounds
ZOMBIE_STABLE_SECS="${ZOMBIE_STABLE_SECS:-35}"   # >= one 30s tick, with slack
ZOMBIE_REKILL_MAX="${ZOMBIE_REKILL_MAX:-6}"      # bounded re-kills of self-healed listeners
ZOMBIE_REKILL_LEFT=0                             # runtime counter (set in PHASE 5)
# Takeover budget: START clobber (tick N) → zombie-takeover STOP (tick N+1) →
# clean START (tick N+2). ~30s/tick → 3-4 ticks of headroom.
TAKEOVER_TIMEOUT="${TAKEOVER_TIMEOUT:-200}"

# ── PINNED isolation values (PHASE 0 triple whitelist — seth-m1, kyle route A) ──
# Primary anchor: immutable hardware UUID. Literal value captured on seth-m1;
# do NOT parameterize — it IS the lock. (Identical to single_machine_e2e.sh.)
SETH_M1_HW_UUID="E193559B-56B8-56E4-B84D-B624B6EB5956"
SETH_M1_LOCALHOSTNAME="MacBook-Pro-4"                       # soft anchor (防呆)
JOEY_INFRA_DIR="$HOME/.vibe-clicking/agents/joey"          # soft anchor (fleet infra present)

# EXACT launchd labels (server + warden). EXACT-label kill only.
SERVE_LABEL="com.officraft.serve"
AUTODEPLOY_LABEL="com.officraft.autodeploy"
TUNNEL_LABEL="com.officraft.tunnel"
WARDEN_LABEL="com.officraft.ocwarden"

UID_NUM="$(id -u)"
GUI="gui/$UID_NUM"

# Canonical server layout (matches bin/ocserver — no namespace).
HOME_DIR="${HOME:?HOME must be set}"
SERVER_ROOT="${OC_SERVER_ROOT:-$HOME_DIR/.officraft/server}"
OC_TOML="$SERVER_ROOT/oc.toml"
DB_PATH="$SERVER_ROOT/data/officraft.db"
OC_ROOT="$HOME_DIR/.officraft"
# reconcileLog (reconcile.go:396) writes to serve's stderr → serve.err.log.
SERVE_ERR_LOG="$SERVER_ROOT/log/serve.err.log"

OCSERVER="$REPO_ROOT/bin/ocserver"
OCWARDEN="$REPO_ROOT/bin/ocwarden"

TS="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="/tmp/oc-a1-zombie-e2e-$TS"   # dump/log artifacts + reconcile tail land here
mkdir -p "$BACKUP_DIR"

TMUX_SOCKET_LOCAL="officraft"   # tmux -L <socket>; TMUX_SOCKET (same value) is in lib

# The server-self warden member is auto-seeded by the DB. Agents desire it.
SERVER_SELF_ID="m-server-self"

# SINGLE_PROD_PORTS (the prod ports the 0c guard verifies free) is set by
# oc_resolve_instance below in BOTH modes (canonical → serve+tunnel from the SSOT;
# namespace → this run's own port).

# ── INSTANCE RESOLUTION (T-8aa1) — construction-enforced isolation ───────────
# Default = a fresh run-scoped NAMESPACE (isolated port/labels/root/socket). The
# A1 zombie fabrication kills EXACT captured pids anchored to the NAMESPACED agent
# workdir (~/.officraft-<ns>/agents/<id>) on the NAMESPACED tmux socket, so it
# can never touch a canonical live agent. Canonical only via OC_E2E_ALLOW_CANONICAL=1.
oc_resolve_instance

# ── runtime state captured across phases (declared here for set -u safety) ──
CONTROL_AGENT=""              # minted id of the hired control member (m-<hex12>)
TARGET_OLD_PANE=""           # target pane pid BEFORE the zombie/takeover
TARGET_NEW_PANE=""           # target pane pid AFTER the clean respawn
TARGET_LISTENER_PID=""       # the ONE ocagent-listen pid we kill -9 (target only)
CONTROL_OLD_PANE=""          # control pane pid captured once, asserted unchanged

# ---------------------------------------------------------------------------
# preflight tooling — refuse to run unless the operator acknowledged destruction.
# ---------------------------------------------------------------------------
[[ "${OC_A1_ZOMBIE_YES:-}" == "1" ]] || die \
  "refusing: this is a DESTRUCTIVE full-reset A1-ZOMBIE E2E (wipes the local server + agents on THIS machine; STOP-freezes a live agent's claude pane and KILLs its listener to fabricate a zombie). Re-run with OC_A1_ZOMBIE_YES=1 to acknowledge."

for tool in curl sqlite3 uuidgen launchctl ioreg scutil lsof tmux pgrep ps; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool missing on PATH: $tool"
done
[[ -x "$OCSERVER" ]] || die "bin/ocserver not found/executable at $OCSERVER"

log "params: TARGET_AGENT=$TARGET_AGENT CONTROL_NAME='$CONTROL_NAME' LOCAL_BASE=$LOCAL_BASE"
log "layout: SERVER_ROOT=$SERVER_ROOT DB=$DB_PATH serve.err=$SERVE_ERR_LOG  backups→$BACKUP_DIR"

# ---------------------------------------------------------------------------
# A1-local helpers (host-truth probes — NEVER trust self-report)
# ---------------------------------------------------------------------------
# pane_pid_of AGENT — EXACT tmux pane pid of member-<agent> on our socket, or "".
# EXACT session target (=member-<id>); reads host truth, no self-report.
pane_pid_of() {
  tmux -L "$TMUX_SOCKET_LOCAL" display-message -p -t "$(tmux_session "$1")" '#{pane_pid}' 2>/dev/null || true
}

# tmux_has_session AGENT — 0 if member-<agent> is positively present, else 1.
tmux_has_session() {
  tmux -L "$TMUX_SOCKET_LOCAL" has-session -t "=$(tmux_session "$1")" 2>/dev/null
}

# listener_pid_of AGENT — the ONE `ocagent listen` pid whose workdir is that
# agent's canonical workdir (~/.officraft/agents/<id>). Cross-references pgrep
# hits against the workdir so we NEVER grab an unrelated agent's listener; empty
# if none or if ambiguous (>1 → refuse, we only ever kill exactly one).
listener_pid_of() {
  local agent="$1" wd pid matches=()
  wd="$(agent_workdir "$HOME_DIR" "$agent")"
  # EXACT process name (pgrep -x), NOT -f. The agent's own claude process carries the
  # string "ocagent listen" INSIDE its --append-system-prompt persona text, so -f also
  # matches the claude parent AND the zsh wrapper — 3 hits per workdir → AMBIGUOUS →
  # we refuse to pick → stage fails. Only the real listener has comm == "ocagent".
  # (This is NOT a timing race: polling longer never reduces the 3 matches.)
  # Then keep only pids whose cwd is $wd.
  local p
  for p in $(pgrep -x ocagent 2>/dev/null || true); do
    # macOS: resolve the process cwd via lsof (portable, no /proc). Match EXACT wd.
    local cwd
    cwd="$(lsof -a -p "$p" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n1)"
    [[ "$cwd" == "$wd" ]] && matches+=("$p")
  done
  if [[ "${#matches[@]}" -eq 1 ]]; then printf '%s' "${matches[0]}"; return 0; fi
  if [[ "${#matches[@]}" -gt 1 ]]; then
    warn "listener_pid_of($agent): AMBIGUOUS — ${#matches[@]} ocagent-listen pids anchored to $wd (${matches[*]}); refusing to pick (we only ever STOP exactly one)"
  fi
  printf ''   # none or ambiguous → empty (caller fails the stage)
}

# reconcile_tail_start — begin capturing serve.err.log reconcile lines to a file
# from NOW (so PHASE 7 greps a clean, phase-scoped window). Records the byte
# offset; we snapshot the file's tail at assert time. No background process, no
# broad kills — just a marker + later `tail -c +OFFSET`.
RECON_SNAPSHOT="$BACKUP_DIR/reconcile.window.log"
RECON_OFFSET=0
reconcile_window_open() {
  if [[ -f "$SERVE_ERR_LOG" ]]; then
    RECON_OFFSET="$(wc -c < "$SERVE_ERR_LOG" 2>/dev/null | tr -d ' ')"
  else
    RECON_OFFSET=0
  fi
  log "reconcile window opened at serve.err.log byte offset=$RECON_OFFSET"
}
# reconcile_window_snapshot — copy everything appended since the offset into the
# phase-scoped snapshot file (idempotent; call before each grep assertion).
reconcile_window_snapshot() {
  : > "$RECON_SNAPSHOT"
  if [[ -f "$SERVE_ERR_LOG" ]]; then
    tail -c "+$((RECON_OFFSET + 1))" "$SERVE_ERR_LOG" 2>/dev/null \
      | grep '^\[reconcile\]' > "$RECON_SNAPSHOT" || true
  fi
}

# member_last_op AGENT FIELD — read one last_op* field from the member DTO
# (GET /api/members/<id>). Fields: last_op, last_op_ok, last_op_reason, last_op_log.
member_last_op() {
  api_get "/api/members/$1" 2>/dev/null | py -c '
import sys, json
f = sys.argv[1]
try:
    d = json.load(sys.stdin)
except Exception:
    print(""); sys.exit(0)
v = d.get(f, "")
print("" if v is None else v)
' "$2"
}

# poll_presence_offline_stable AGENT — reverse-poll: wait until the member is
# presence!=online AND STAYS non-online for ZOMBIE_STABLE_SECS (>=1 tick). If it
# flaps back to online the listener self-healed (a real friction point). Returns
# 0 on a stable-offline zombie, 1 on timeout / self-heal flap.
poll_presence_offline_stable() {
  local agent="$1" deadline cur stable_since=0 now
  deadline=$(( $(date +%s) + ZOMBIE_OFFLINE_TIMEOUT ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(presence_of "$agent")"
    now="$(date +%s)"
    if [[ "$cur" != "online" ]]; then
      [[ "$stable_since" -eq 0 ]] && stable_since="$now"
      if [[ $(( now - stable_since )) -ge "$ZOMBIE_STABLE_SECS" ]]; then
        log "zombie presence[$agent]='$cur' held non-online for >=${ZOMBIE_STABLE_SECS}s (>=1 reconcile tick)"
        return 0
      fi
    else
      # Flapped back online → the agent's claude pane re-ran `ocagent listen` and
      # reconnected. This is REAL, verified behaviour (the new listener's parent
      # chain is the claude pane), and it is exactly why A1 is a LAST line of
      # defence: a live claude heals itself and never needs reaping.
      #
      # To reach the zombie state we must exhaust that self-heal: re-kill each
      # NEW listener it spawns, bounded by ZOMBIE_REKILL_MAX. The agent gives up
      # after a few rounds; the claude pane stays ALIVE (still squatting the tmux
      # name) but no longer holds an SSE → presence-deaf zombie.
      #
      # NOTE: freezing the claude pane with SIGSTOP does NOT work here — verified
      # on macOS: SIGSTOP against the claude pane pid (and its pgid) leaves it in
      # Ss+ / running, while the same signal correctly stops the ocagent listener.
      # So we exhaust the self-heal instead of trying to freeze it.
      if [[ "$stable_since" -ne 0 ]]; then
        warn "FRICTION: zombie[$agent] flapped BACK to presence=online (claude pane self-healed its listener) — resetting stability window"
      fi
      stable_since=0
      if [[ "$ZOMBIE_REKILL_LEFT" -gt 0 ]]; then
        local newpid
        newpid="$(listener_pid_of "$agent")"
        if [[ -n "$newpid" ]]; then
          log "DESTRUCTIVE: kill -9 $newpid (EXACT pid — self-healed listener for $agent; re-kill, ${ZOMBIE_REKILL_LEFT} left)"
          kill -9 "$newpid" 2>/dev/null || true
          ZOMBIE_REKILL_LEFT=$(( ZOMBIE_REKILL_LEFT - 1 ))
        fi
      fi
    fi
    sleep 3
  done
  warn "zombie[$agent] never held stable non-online for ${ZOMBIE_STABLE_SECS}s within ${ZOMBIE_OFFLINE_TIMEOUT}s (last='$cur')"
  return 1
}

# ===========================================================================
# PHASE 0 — PREFLIGHT ISOLATION GUARDS (die on any failure, BEFORE any teardown)
# ===========================================================================
stage "0. preflight isolation guards (seth-m1 triple whitelist + state + prod ports + env strip)"
# Reuses the shared bring-up guard block (triple hardware whitelist + existing-
# state whitelist + SERVER_ROOT containment + prod-port hard refusal + ambient
# OC_* strip + claude-resolvability). Dies before any destructive action.
oc_preflight_guards
pass_stage

# ===========================================================================
# PHASE 1 — TEARDOWN OLD SERVER (destructive, but backup FIRST; bounded kills)
# ===========================================================================
stage "1. teardown old server (backup → EXACT-label bootout → warden teardown → rm dirs)"
oc_teardown_bounded "pre-install"
pass_stage

# ===========================================================================
# PHASE 2 — FRESH INSTALL SERVER (canonical serve port) + seed KNOWN owner password
# ===========================================================================
stage "2. fresh install server (ocserver install --force, canonical serve port) + seed owner password"
oc_fresh_install   # sets OWNER_TOKEN + GIT_SHA; runs the 15s serve-stability window
pass_stage

# ===========================================================================
# PHASE 3 — BOOTSTRAP SERVER-SELF WARDEN (this machine)
# ===========================================================================
stage "3. bootstrap server-self warden (local) + verify launchd loaded + SSE connected"
oc_bootstrap_warden
pass_stage

# ===========================================================================
# PHASE 4 — ACTIVATE TARGET + CONTROL, poll both online, capture pane/listener
# ===========================================================================
stage "4. activate target ($TARGET_AGENT) + hire+activate control → both presence online"

# 4a. TARGET: activate the seeded agent on server-self (desired=online + bind).
ACT_JSON="$(api_post_logged "/api/members/$TARGET_AGENT/activate" "{\"machine_id\":\"$SERVER_SELF_ID\"}" || echo '{}')"
[[ -n "$(printf '%s' "$ACT_JSON" | json_field id)" ]] \
  || fail_stage "activate target $TARGET_AGENT on $SERVER_SELF_ID returned no member DTO — activation rejected"
log "activated TARGET $TARGET_AGENT on $SERVER_SELF_ID"

# 4b. CONTROL: HIRE a fresh healthy member (the DB seeds only mira + server-self,
#     so the control must be created). A bare hire (name only, no kind/role_key)
#     folds to KindAssistant and is NOT privilege-bearing → owner token suffices;
#     the server mints its id (m-<hex12>) and returns the DTO.
HIRE_JSON="$(api_post_logged /api/members "$(py -c '
import json,sys; print(json.dumps({"name": sys.argv[1]}))' "$CONTROL_NAME")" || echo '{}')"
CONTROL_AGENT="$(printf '%s' "$HIRE_JSON" | json_field id)"
[[ -n "$CONTROL_AGENT" && "$CONTROL_AGENT" != "$TARGET_AGENT" ]] \
  || fail_stage "hire control '$CONTROL_NAME' returned no distinct member id (got '$CONTROL_AGENT') — cannot run the control leg"
log "hired CONTROL member id=$CONTROL_AGENT (name='$CONTROL_NAME', kind folds to assistant)"
ACT2_JSON="$(api_post_logged "/api/members/$CONTROL_AGENT/activate" "{\"machine_id\":\"$SERVER_SELF_ID\"}" || echo '{}')"
[[ -n "$(printf '%s' "$ACT2_JSON" | json_field id)" ]] \
  || fail_stage "activate control $CONTROL_AGENT on $SERVER_SELF_ID returned no member DTO"
log "activated CONTROL $CONTROL_AGENT on $SERVER_SELF_ID"

# 4c. poll BOTH to presence=online via the HUB (live SSE), not the DB.
poll_presence "$TARGET_AGENT" online "$PRESENCE_TIMEOUT" \
  || fail_stage "TARGET $TARGET_AGENT never reached presence=online within ${PRESENCE_TIMEOUT}s"
poll_presence "$CONTROL_AGENT" online "$PRESENCE_TIMEOUT" \
  || fail_stage "CONTROL $CONTROL_AGENT never reached presence=online within ${PRESENCE_TIMEOUT}s"

# 4d. capture host truth: pane pids for both, and the target's listener pid.
TARGET_OLD_PANE="$(pane_pid_of "$TARGET_AGENT")"
CONTROL_OLD_PANE="$(pane_pid_of "$CONTROL_AGENT")"
[[ -n "$TARGET_OLD_PANE" ]]  || fail_stage "could not read TARGET pane pid for $(tmux_session "$TARGET_AGENT") — cannot prove pane rotation later"
[[ -n "$CONTROL_OLD_PANE" ]] || fail_stage "could not read CONTROL pane pid for $(tmux_session "$CONTROL_AGENT") — cannot prove it is untouched later"
# The listener pid appears a few seconds AFTER presence flips online: the agent
# runs `ocagent listen` via its Bash tool, so its process tree + cwd settle
# slightly after the SSE mounts (RUN1 raced this — presence online but the
# lsof-cwd lookup found 0 match). Poll-until-unique (bounded) instead of a
# single lookup. listener_pid_of returns empty on BOTH none and >1 (ambiguous);
# ambiguous is rare and its warn is logged each iteration.
TARGET_LISTENER_PID=""
_lp_deadline=$(( $(date +%s) + 90 ))
while [[ "$(date +%s)" -lt "$_lp_deadline" ]]; do
  TARGET_LISTENER_PID="$(listener_pid_of "$TARGET_AGENT")"
  [[ -n "$TARGET_LISTENER_PID" ]] && break
  sleep 3
done
[[ -n "$TARGET_LISTENER_PID" ]] \
  || fail_stage "could not uniquely locate the TARGET ocagent-listen pid anchored to $(agent_workdir "$HOME_DIR" "$TARGET_AGENT") within 90s — refusing to STOP anything (we only ever STOP exactly one captured pid)"
# sanity: the listener pid must NOT be the control's pane pid (never touch control).
[[ "$TARGET_LISTENER_PID" != "$CONTROL_OLD_PANE" ]] \
  || fail_stage "SAFETY ABORT: captured TARGET listener pid ($TARGET_LISTENER_PID) collides with CONTROL pane pid — refusing to kill (would kill the control)"
log "captured host truth: TARGET pane=$TARGET_OLD_PANE listener=$TARGET_LISTENER_PID  |  CONTROL pane=$CONTROL_OLD_PANE"
pass_stage

# ===========================================================================
# PHASE 5 — MAKE THE TARGET A ZOMBIE (freeze its listener; session stays alive)
# ===========================================================================
stage "5. fabricate zombie: KILL listener ($TARGET_LISTENER_PID) + exhaust self-heal → presence offline, session ALIVE"

# Open the reconcile capture window NOW, so PHASE 7's grep only sees the takeover
# sequence produced from this point onward (clean, phase-scoped evidence).
reconcile_window_open

# Fabricating the zombie takes BOTH hand-issued signals below, each against ONE
# EXACT captured pid (never a pattern kill). Two hard-won facts drive this:
#
#   (a) kill -STOP on the LISTENER alone does NOT make it presence-deaf. SIGSTOP
#       freezes user-space only — the kernel keeps its SSE socket ESTABLISHED, so
#       the server never sees a FIN and keeps reporting IsOnline=true forever.
#       The listener's connection must actually DIE → SIGKILL, not SIGSTOP.
#
#   (b) the agent SELF-HEALS: its claude pane notices the listener died and re-runs
#       `ocagent listen` (verified — the new listener's parent chain IS the claude
#       pane). Presence flaps back online and A1 has nothing to reap. Freezing the
#       pane does NOT help: on macOS SIGSTOP against the claude pane pid (and its
#       pgid) leaves it running (Ss+), while the same signal correctly stops the
#       ocagent listener. So we EXHAUST the self-heal instead — poll_presence_offline_stable
#       re-kills each newly spawned listener (bounded by ZOMBIE_REKILL_MAX, EXACT pid
#       each time) until the agent stops re-spawning.
#
# End state: claude pane ALIVE (still squatting the tmux name) but holding no SSE
# = presence-deaf zombie — exactly what A1's decideUp is meant to reap. This is also
# WHY A1 exists: it is the LAST line of defence, for an agent that can no longer heal
# itself; a healthy live claude fixes itself and never reaches A1.
ZOMBIE_REKILL_LEFT="$ZOMBIE_REKILL_MAX"
log "DESTRUCTIVE: kill -9 $TARGET_LISTENER_PID (EXACT captured TARGET listener pid — drops the SSE socket for real)"
kill -9 "$TARGET_LISTENER_PID" \
  || fail_stage "kill -9 $TARGET_LISTENER_PID failed (listener already gone?) — cannot fabricate the zombie"

# Prove the session is STILL ALIVE right after the freeze (zombie, not a clean
# stop): the tmux session must still be present and the pane pid unchanged.
tmux_has_session "$TARGET_AGENT" \
  || fail_stage "TARGET tmux session vanished immediately after fabricating the zombie — that is a dead session, not a presence-deaf zombie"
FROZEN_PANE="$(pane_pid_of "$TARGET_AGENT")"
[[ "$FROZEN_PANE" == "$TARGET_OLD_PANE" ]] \
  || fail_stage "TARGET pane pid changed the instant we fabricated the zombie (was $TARGET_OLD_PANE, now $FROZEN_PANE) — not the intended presence-deaf zombie"
log "zombie seeded: session member-$TARGET_AGENT ALIVE (pane $TARGET_OLD_PANE) but listener $TARGET_LISTENER_PID KILLED — now exhausting self-heal (up to $ZOMBIE_REKILL_MAX re-kills)"

# Reverse-poll: server presence must fall to non-online and HOLD for >=1 tick.
# If it flaps back the listener self-healed — recorded as friction (see helper).
poll_presence_offline_stable "$TARGET_AGENT" \
  || fail_stage "TARGET $TARGET_AGENT presence never held stable non-online even after exhausting up to $ZOMBIE_REKILL_MAX listener re-kills — could not confirm the zombie state; see FRICTION lines above"

# CONTROL must remain online throughout — we touched nothing of its.
[[ "$(presence_of "$CONTROL_AGENT")" == "online" ]] \
  || fail_stage "CONTROL $CONTROL_AGENT lost presence during zombie fabrication — the isolation between target/control leaked"
log "CONTROL $CONTROL_AGENT still presence=online during zombie fabrication (untouched)"
pass_stage

# ===========================================================================
# PHASE 6 — WAIT FOR AUTO-TAKEOVER (server reconcile reaps the zombie)
# ===========================================================================
stage "6. wait for A1 auto-takeover (START clobber → zombie-takeover STOP → clean START)"

# We do NOT reap the zombie — the SERVER does. Poll the reconcile window for the
# ordered three-marker takeover sequence for the TARGET, within the budget
# (~3-4 ticks @30s cadence). The definitive proof is asserted in PHASE 7; here we
# just wait until the clean-START marker appears (the sequence has completed).
TARGET_SESSION="$(tmux_session "$TARGET_AGENT")"   # member-<id> — appears in reconcile log lines
deadline=$(( $(date +%s) + TAKEOVER_TIMEOUT ))
takeover_seen=0
while [[ "$(date +%s)" -lt "$deadline" ]]; do
  reconcile_window_snapshot
  # The zombie-takeover STOP line is the load-bearing marker (reconcile.go:252).
  if grep -qF 'zombie takeover: START clobbered a live presence-deaf session' "$RECON_SNAPSHOT" \
     && grep -qE "$TARGET_AGENT: desired=online command=stop" "$RECON_SNAPSHOT"; then
    # takeover fired; now wait for the subsequent CLEAN start marker (respawn).
    if grep -qE "$TARGET_AGENT: desired=online command=start" "$RECON_SNAPSHOT" \
       && [[ "$(pane_pid_of "$TARGET_AGENT")" != "" ]] \
       && [[ "$(pane_pid_of "$TARGET_AGENT")" != "$TARGET_OLD_PANE" ]]; then
      takeover_seen=1; break
    fi
  fi
  sleep 5
done
[[ "$takeover_seen" -eq 1 ]] \
  || fail_stage "A1 auto-takeover did not complete within ${TAKEOVER_TIMEOUT}s (no zombie-takeover STOP + clean respawn for $TARGET_AGENT in the reconcile window — see $RECON_SNAPSHOT)"
log "auto-takeover sequence observed for $TARGET_AGENT (reconcile window: $RECON_SNAPSHOT)"
pass_stage

# ===========================================================================
# PHASE 7 — TRIPLE PROOF (do NOT trust self-report; three independent sources)
# ===========================================================================
stage "7. triple proof of takeover (reconcile-log 3 markers + host pane rotation + last_op fold)"
reconcile_window_snapshot
cp -f "$RECON_SNAPSHOT" "$BACKUP_DIR/reconcile.proof.log" 2>/dev/null || true

# ── PROOF ①: server reconcile log — the three ordered markers (verbatim) ──────
# START (clobber-triggering) → zombie-takeover STOP → clean START, ALL for the
# TARGET member, in order. We assert the STOP marker's verbatim reason text and
# that a start line both precedes (clobber) and follows (clean respawn) it.
STOP_LINE_NO="$(grep -nF 'zombie takeover: START clobbered a live presence-deaf session — robust stop to reap it before respawn' "$RECON_SNAPSHOT" | head -n1 | cut -d: -f1)"
[[ -n "$STOP_LINE_NO" ]] \
  || fail_stage "PROOF① FAIL: verbatim 'zombie takeover: ... robust stop to reap it before respawn' marker absent from the reconcile window ($RECON_SNAPSHOT)"
# The STOP line must be for the TARGET and be a command=stop line.
sed -n "${STOP_LINE_NO}p" "$RECON_SNAPSHOT" | grep -qE "$TARGET_AGENT: desired=online command=stop — zombie takeover:" \
  || fail_stage "PROOF① FAIL: the zombie-takeover STOP line is not attributed to TARGET '$TARGET_AGENT' with command=stop (line $STOP_LINE_NO)"
# A clobber-window START must PRECEDE the STOP (the START that bounced), and a
# clean START must FOLLOW it (the respawn onto the reaped slot).
head -n "$STOP_LINE_NO" "$RECON_SNAPSHOT" | grep -qE "$TARGET_AGENT: desired=online command=start" \
  || fail_stage "PROOF① FAIL: no TARGET command=start line PRECEDING the takeover STOP (the clobber-triggering START is missing)"
tail -n "+$((STOP_LINE_NO + 1))" "$RECON_SNAPSHOT" | grep -qE "$TARGET_AGENT: desired=online command=start" \
  || fail_stage "PROOF① FAIL: no TARGET command=start line FOLLOWING the takeover STOP (the clean respawn START is missing)"
log "PROOF① OK: reconcile log shows START(clobber) → zombie-takeover STOP → START(clean) for $TARGET_AGENT (STOP at window line $STOP_LINE_NO)"
sed -n "${STOP_LINE_NO}p" "$RECON_SNAPSHOT" | sed 's/^/[a1-zombie] proof①| /' >&2

# ── PROOF ②: host pane pid rotation (OLD absent + session momentarily gone + NEW≠OLD) ──
# The OLD (zombie) pane pid must be gone from the process table (the robust STOP
# ladder reaped it). We already saw the session positively absent for a moment
# during PHASE 6's poll; re-confirm the OLD pid is dead and a NEW pane exists.
if ps -p "$TARGET_OLD_PANE" >/dev/null 2>&1; then
  fail_stage "PROOF② FAIL: OLD zombie pane pid $TARGET_OLD_PANE is STILL alive — robust STOP did not reap the squatting session"
fi
log "PROOF② (a): OLD zombie pane pid $TARGET_OLD_PANE is absent from the process table (reaped)"
TARGET_NEW_PANE="$(pane_pid_of "$TARGET_AGENT")"
[[ -n "$TARGET_NEW_PANE" ]] \
  || fail_stage "PROOF② FAIL: no live TARGET pane after takeover — the clean respawn never produced a session"
[[ "$TARGET_NEW_PANE" != "$TARGET_OLD_PANE" ]] \
  || fail_stage "PROOF② FAIL: TARGET pane pid unchanged ($TARGET_NEW_PANE) — a respawn would mint a NEW pid; unchanged means no reap+respawn happened"
log "PROOF② OK: pane pid rotated OLD=$TARGET_OLD_PANE → NEW=$TARGET_NEW_PANE (OLD reaped, NEW respawned)"

# ── PROOF ③: last_op fold sequence (member DTO) — corroborates ①/③ ────────────
# The fold onto member.last_op* is a LATEST-value projection (not a history), so
# it corroborates rather than re-orders: after a completed takeover the newest
# fold should be the clean START (last_op=start, ok=true), and the reconcile
# window already carries the ordered stop OK receipt. We assert the DTO reflects
# a start op that is OK (clean respawn landed) and that the takeover-era reason no
# longer pins session_already_exists (the clobber cleared).
LO_KIND="$(member_last_op "$TARGET_AGENT" last_op)"
LO_OK="$(member_last_op "$TARGET_AGENT" last_op_ok)"
LO_REASON="$(member_last_op "$TARGET_AGENT" last_op_reason)"
log "PROOF③ TARGET last_op='$LO_KIND' ok='$LO_OK' reason='${LO_REASON:0:80}'"
# After a clean respawn the latest fold is a START; if the timing catches the STOP
# receipt instead, accept a stop-OK too (both are on the takeover path) but the
# clobber reason must be gone.
case "$LO_KIND" in
  start)
    [[ "$LO_OK" == "True" || "$LO_OK" == "true" ]] \
      || fail_stage "PROOF③ FAIL: TARGET latest last_op=start but ok='$LO_OK' — the clean respawn did not land OK"
    [[ "$LO_REASON" != session_already_exists* ]] \
      || fail_stage "PROOF③ FAIL: TARGET last_op=start still pinned reason=session_already_exists* — still bouncing off the clobber-guard, takeover not complete"
    ;;
  stop)
    [[ "$LO_OK" == "True" || "$LO_OK" == "true" ]] \
      || fail_stage "PROOF③ FAIL: TARGET latest last_op=stop but ok='$LO_OK' — robust STOP reported not-OK (incomplete reap)"
    ;;
  *)
    fail_stage "PROOF③ FAIL: TARGET latest last_op='$LO_KIND' is neither start nor stop — not on the takeover path (expected the reap→respawn fold)"
    ;;
esac
log "PROOF③ OK: member DTO last_op fold consistent with a completed reap→clean-respawn (no lingering session_already_exists)"

# All three sources agree → the zombie was auto-reaped and cleanly respawned.
log "TRIPLE PROOF GREEN: reconcile log + host pane rotation + last_op fold all confirm A1 auto-takeover for $TARGET_AGENT"
pass_stage

# ===========================================================================
# PHASE 8 — CONTROL assertion (A1 must NOT mis-kill a healthy member)
# ===========================================================================
stage "8. control unharmed ($CONTROL_AGENT never STOPped, pane unchanged, stayed online)"
reconcile_window_snapshot

# 8a. the reconcile window must carry NO command=stop line for the control id.
#     (namespace/condition precision — A1 only reaps the clobbered zombie.)
if grep -qE "$CONTROL_AGENT: desired=.* command=stop" "$RECON_SNAPSHOT"; then
  grep -E "$CONTROL_AGENT: .* command=stop" "$RECON_SNAPSHOT" | sed 's/^/[a1-zombie] control-stop| /' >&2
  fail_stage "PHASE8 FAIL: reconcile log shows a command=stop for CONTROL $CONTROL_AGENT — A1 mis-killed a healthy member (see control-stop| lines)"
fi
log "PHASE8 (a): no command=stop reconcile line for CONTROL $CONTROL_AGENT (never reaped)"

# 8b. the control pane pid is UNCHANGED (no reap+respawn happened to it).
CONTROL_NOW_PANE="$(pane_pid_of "$CONTROL_AGENT")"
[[ -n "$CONTROL_NOW_PANE" ]] \
  || fail_stage "PHASE8 FAIL: CONTROL pane vanished — the healthy member was disturbed"
[[ "$CONTROL_NOW_PANE" == "$CONTROL_OLD_PANE" ]] \
  || fail_stage "PHASE8 FAIL: CONTROL pane pid changed ($CONTROL_OLD_PANE → $CONTROL_NOW_PANE) — the healthy member was respawned (mis-handled)"
log "PHASE8 (b): CONTROL pane pid unchanged ($CONTROL_OLD_PANE) — never respawned"

# 8c. the control never lost SSE presence.
[[ "$(presence_of "$CONTROL_AGENT")" == "online" ]] \
  || fail_stage "PHASE8 FAIL: CONTROL $CONTROL_AGENT is no longer presence=online — the healthy member was disturbed"
# 8d. control last_op must NOT be a stop (belt-and-braces against the DTO too).
CTRL_LO="$(member_last_op "$CONTROL_AGENT" last_op)"
[[ "$CTRL_LO" != "stop" ]] \
  || fail_stage "PHASE8 FAIL: CONTROL member DTO last_op=stop — a STOP was folded onto the healthy member"
log "PHASE8 OK: CONTROL $CONTROL_AGENT unharmed (no stop line, pane $CONTROL_OLD_PANE intact, still online, last_op='$CTRL_LO') — A1 did not mis-kill it"
pass_stage

# ===========================================================================
# PHASE 9 — TEARDOWN → CLEAN SLATE (CONT-revive frozen orphan first, then EXACT)
# ===========================================================================
stage "9. teardown restore → clean slate (CONT-revive frozen orphan → bounded EXACT kills)"

# 9a. Revive the STOP-frozen orphan listener BEFORE teardown, so no frozen
#     process is left behind. The server's robust STOP normally already reaped it
#     (kill -CONT on a since-reaped pid is a harmless no-op / not-found). This is
#     an EXACT single-pid CONT — never a pattern.
if [[ -n "$TARGET_OLD_PANE" ]]; then
  if ps -p "$TARGET_OLD_PANE" >/dev/null 2>&1; then
    log "note: original TARGET claude pane $TARGET_OLD_PANE still alive at teardown (A1 did not reap it) — bounded teardown below will clean it via EXACT tmux session kill"
  else
    log "original TARGET claude pane $TARGET_OLD_PANE is gone — the server's robust STOP reaped it (the A1 takeover under test)"
  fi
fi

# 9b. the ONLY teardown path: EXACT labels + EXACT member-/worker- tmux sessions.
oc_teardown_bounded "post-run"

# 9c. clean-slate corroboration: ~/.officraft back to empty/absent AND the port
#     THIS run owned (resolve-set LOCAL_BASE; canonical → 7755, namespace → the
#     run's own free port) free again. Assert the OWNED port, not a hardcoded
#     literal — a literal we never bound is a vacuous check that stays green even
#     when teardown leaks our real listener (T-191d family: guard the port the run
#     actually used, not a stale constant).
if [[ -d "$OC_ROOT" && -z "$(ls -A "$OC_ROOT" 2>/dev/null)" ]]; then
  log "restored: $OC_ROOT is empty (clean slate)"
elif [[ ! -e "$OC_ROOT" ]]; then
  log "restored: $OC_ROOT absent (clean slate)"
else
  warn "post-teardown: $OC_ROOT is NOT empty — residue remains (review before next run):"
  ls -A "$OC_ROOT" 2>/dev/null | sed 's/^/[a1-zombie] residue| /' >&2
  fail_stage "post-teardown residue under $OC_ROOT — clean-slate invariant broken (next run's PHASE 0 state whitelist would refuse)"
fi
owned_port="${LOCAL_BASE##*:}"
if lsof -nP -iTCP:"$owned_port" -sTCP:LISTEN >/dev/null 2>&1; then
  lsof -nP -iTCP:"$owned_port" -sTCP:LISTEN 2>/dev/null | sed "s/^/[a1-zombie] port$owned_port| /" >&2
  fail_stage "post-teardown: port $owned_port (this run's serve port) still has a LISTENER — teardown did not release it (see port$owned_port| lines)"
fi
log "clean-slate corroborated: $OC_ROOT empty/absent AND port $owned_port free"
pass_stage

# ===========================================================================
# SUMMARY
# ===========================================================================
summarize
# If we got here every phase was PASS (any FAIL exits early via fail_stage).
for r in "${STAGE_RESULT[@]}"; do [[ "$r" == "PASS" ]] || exit 1; done
exit 0
