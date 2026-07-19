#!/usr/bin/env bash
# e2e_test/single_machine_e2e.sh — SINGLE-MACHINE full-reset E2E regression.
# ============================================================================
# PURPOSE
#   A DESTRUCTIVE, FULL-RESET, MANUAL end-to-end regression that exercises the
#   whole officraft server+warden+agent lifecycle on ONE real machine, in one
#   key, on the CANONICAL port 8770:
#
#     PHASE 0 preflight guards  →  PHASE 1 teardown old server  →
#     PHASE 2 fresh `ocserver install` (canonical 8770)  →
#     PHASE 3 bootstrap server-self warden  →  PHASE 4 spawn a test agent  →
#     PHASE 5 assert the agent booted with ZERO self-repair (two-track)  →
#     PHASE 6 teardown → clean slate.
#
#   Per-phase objective PASS/FAIL via stage/pass_stage/fail_stage; exits 0 only
#   if every phase is green.
#
# ⚠️  DESTRUCTIVE FULL-RESET. This TEARS DOWN + WIPES the local officraft
#     server on THIS machine and reinstalls it from zero. It is NOT the isolated
#     Playwright suite (setup.sh/run_all.sh, :8791) and NOT a read-only smoke
#     test. It is run BY HAND, by an operator who knows it wipes the local
#     server. It refuses to run without OC_SINGLE_MACHINE_YES=1.
#
# ⚠️  seth-m1 ONLY. Unlike cross_machine.sh (which uses a generic isolation ack),
#     this script is HARD-PINNED to seth-m1 (kyle route A) via a TRIPLE hardware
#     whitelist in PHASE 0 — hardware UUID (primary anchor) AND LocalHostName AND
#     the joey fleet-infra dir. On any other machine it dies in PHASE 0, before a
#     single destructive action. There is no second-machine / relocate leg.
#
# ⚠️  CANONICAL port 8770, NO --namespace (kyle route A). Isolation from prod
#     comes from "seth-m1 (no prod) + canonical 8770 + machine/state whitelist",
#     NOT from a namespace.
#
# TEARDOWN SAFETY (fleet zero-touch): teardown boots-out ONLY the four EXACT
#   launchd labels com.officraft.{serve,autodeploy,tunnel,ocwarden}. It NEVER
#   pkill/killall/pattern-kills — this box also runs other fleet jobs. It kills
#   only captured pids/labels and backs the DB up with `.dump` before destroying.
#
# USAGE:
#   OC_SINGLE_MACHINE_YES=1 bash e2e_test/single_machine_e2e.sh
#
# PARAMS (env, overridable):
#   TEST_AGENT       seeded agent member id to spawn (default mira)
#   OWNER_PASSWORD   deterministic owner password to seed (default: random uuid)
#   OC_SINGLE_MACHINE_YES=1   REQUIRED — acknowledges this run is destructive.
# ============================================================================
set -euo pipefail

# This script lives in e2e_test/; source the shared libs (do NOT re-define).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib/oc_lifecycle.sh"   # log/warn/die, stage bookkeeping, api_*, self-repair, ...
source "$HERE/lib/common.sh"         # oc_env(), PROD_PORTS, py(), REPO_ROOT (isolated-port guard is inert: OC_E2E_PORT defaults non-prod)

# ---------------------------------------------------------------------------
# 0. params + constants (canonical, single-machine)
# ---------------------------------------------------------------------------
TEST_AGENT="${TEST_AGENT:-mira}"
# Canonical loopback base — port 8770 (kyle route A), NO namespace.
LOCAL_BASE="http://127.0.0.1:8770"
# summarize() (from lib) reads these for its footer; single has no 2nd machine.
PUBLIC_HOST="127.0.0.1:8770"
SECOND_MACHINE="(none — single-machine)"
# Deterministic owner password: caller-provided, else a fresh uuid we seed + reuse.
OWNER_PASSWORD="${OWNER_PASSWORD:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Presence poll budget. A REAL claude spawn (PHASE 4) has no firm upper bound —
# observed ~1.6–2.6min for cold boot + SSE mount, so a 150s budget sits right on the
# upper edge and reds flakily on a slow-but-healthy spawn. 210s (3.5min) gives a
# comfortable margin above the observed ceiling; a genuine spawn failure still fails
# (bounded). Hub projection can also lag warden SSE connect a few seconds.
PRESENCE_TIMEOUT="${PRESENCE_TIMEOUT:-210}"

# ── PINNED isolation values (PHASE 0 triple whitelist — seth-m1, kyle route A) ──
# Primary anchor: immutable hardware UUID (survives rename / re-image). Literal
# value captured on seth-m1 by joey; do NOT parameterize — it IS the lock.
SETH_M1_HW_UUID="E193559B-56B8-56E4-B84D-B624B6EB5956"
SETH_M1_LOCALHOSTNAME="MacBook-Pro-4"                       # soft anchor (readability/防呆)
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
DB_PATH="$SERVER_ROOT/data/officraft.db"   # scan_self_repair (lib) reads this
OC_ROOT="$HOME_DIR/.officraft"

OCSERVER="$REPO_ROOT/bin/ocserver"
OCWARDEN="$REPO_ROOT/bin/ocwarden"

TS="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="/tmp/oc-single-machine-e2e-$TS"   # dump_boot_window (lib) writes here
mkdir -p "$BACKUP_DIR"

TMUX_SOCKET_LOCAL="officraft"   # tmux -L <socket>; TMUX_SOCKET (same value) is in lib

# The server-self warden member is auto-seeded by the DB.
SERVER_SELF_ID="m-server-self"

# Canonical prod ports we must own/guard (canonical serve = 8770; tunnel-side 8766).
# NOTE: oc_resolve_instance (below) OVERRIDES these in the default namespace mode.
SINGLE_PROD_PORTS=(8770 8766)

# ── INSTANCE RESOLUTION (T-8aa1) — construction-enforced isolation ───────────
# By DEFAULT allocate a fresh run-scoped NAMESPACE and re-derive every resource
# axis (port, launchd labels, ~/.officraft-<ns> root, tmux socket) so this
# DESTRUCTIVE suite is isolated by construction and touches NOTHING canonical on
# a live fleet host. Canonical is the explicit OC_E2E_ALLOW_CANONICAL=1 escape
# hatch (still gated by the live-fleet guard + seth-m1 whitelist in PHASE 0).
# Overrides LOCAL_BASE / PUBLIC_HOST / *_LABEL / OC_ROOT / SERVER_ROOT / OC_TOML /
# DB_PATH / TMUX_SOCKET_LOCAL / TMUX_SOCKET / SINGLE_PROD_PORTS set above.
oc_resolve_instance

# ── claude resolvability (PHASE 0 preflight 0e) ─────────────────────────────
# The launchd warden (PHASE 3/4) spawns agents with claude. resolveClaudeBin tries:
# #1 OC_CLAUDE_BIN env, #2 LookPath on the warden's minimal launchd PATH
# (/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin), #3 common locations
# (~/.local/bin, /opt/homebrew/bin, /usr/local/bin). Kyle's product fix (2a2296b,
# ticket 4a3f132c) makes `ocserver/ocwarden install` resolve the installer's real
# claude path and STAMP it into the warden plist as OC_CLAUDE_BIN (#1) — so an
# asdf/nvm/volta claude the minimal PATH can't see is now handled at the source.
# This preflight therefore only DETECTS + WARNS (the symlink workaround retired now
# that the product fix landed): it confirms claude is resolvable to the installer
# (so the stamp has something to stamp) and logs whether the warden will hit it via
# a common location / minimal PATH or must rely on the stamp. It never mutates the
# host's claude locations.

# ---------------------------------------------------------------------------
# preflight tooling — refuse to run unless the operator acknowledged destruction.
# ---------------------------------------------------------------------------
[[ "${OC_SINGLE_MACHINE_YES:-}" == "1" ]] || die \
  "refusing: this is a DESTRUCTIVE full-reset E2E (wipes the local server + agent on THIS machine). Re-run with OC_SINGLE_MACHINE_YES=1 to acknowledge."

for tool in curl sqlite3 uuidgen launchctl ioreg scutil lsof tmux; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool missing on PATH: $tool"
done
[[ -x "$OCSERVER" ]] || die "bin/ocserver not found/executable at $OCSERVER"

log "params: TEST_AGENT=$TEST_AGENT LOCAL_BASE=$LOCAL_BASE (canonical 8770, no namespace)"
log "layout: SERVER_ROOT=$SERVER_ROOT DB=$DB_PATH  backups→$BACKUP_DIR"

# ===========================================================================
# PHASE 0 — PREFLIGHT ISOLATION GUARDS (die on any failure, BEFORE any teardown)
# ===========================================================================
stage "0. preflight isolation guards (seth-m1 triple whitelist + state + prod ports + env strip)"

# PHASE 0 guard sequence (triple hardware whitelist + state/containment + prod
# ports + ambient OC_* strip + claude-resolvability detect/warn) is a PURE-MOVE
# into the shared lib so a sibling task-system e2e reuses the SAME isolation.
# Reads the PINNED globals defined above; dies on any guard failure BEFORE any
# teardown. Behavior is byte-identical to the former inline 0a–0e block.
oc_preflight_guards
pass_stage

# ===========================================================================
# PHASE 1 — TEARDOWN OLD SERVER (destructive, but backup FIRST; bounded kills)
# ===========================================================================
stage "1. teardown old server (backup → EXACT-label bootout → warden teardown → rm dirs)"

# teardown is a PURE-MOVE into the shared lib as oc_teardown_bounded WHERE — the
# ONLY teardown path. EXACT labels only; NEVER pkill/killall/glob; .dump backup
# before any destruction; poll-until-gone. It reads the same DB_PATH/BACKUP_DIR/
# GUI/*_LABEL/OCWARDEN/HOME_DIR/SERVER_ROOT/TMUX_SOCKET_LOCAL globals defined
# above. Behavior is byte-identical to the former inline teardown_bounded().
oc_teardown_bounded "pre-install"
pass_stage

# ===========================================================================
# PHASE 2 — FRESH INSTALL SERVER (canonical 8770) + seed KNOWN owner password
# ===========================================================================
stage "2. fresh install server (ocserver install --force, canonical 8770) + seed owner password"

# The full PHASE 2 sequence (seed owner password via render-config + set-password,
# `ocserver install --force` under oc_env, /health + /api/version sanity, owner
# login → OWNER_TOKEN, 15s serve stability window) is a PURE-MOVE into the shared
# lib as oc_fresh_install. It reads SERVER_ROOT/DB_PATH/OCSERVER/REPO_ROOT/OC_TOML/
# LOCAL_BASE/OWNER_PASSWORD and sets OWNER_TOKEN + GIT_SHA. Byte-identical behavior.
oc_fresh_install
pass_stage

# ===========================================================================
# PHASE 3 — BOOTSTRAP SERVER-SELF WARDEN (this machine)
# ===========================================================================
stage "3. bootstrap server-self warden (local) + verify launchd loaded + SSE connected"

# The full PHASE 3 sequence (POST /api/machines/{m-server-self}/bootstrap-here via
# the real product flow → server runs `ocwarden install --force` inline; verify
# launchd loaded + SSE connected with the hub-online fallback) is a PURE-MOVE into
# the shared lib as oc_bootstrap_warden. It reads SERVER_SELF_ID/GUI/WARDEN_LABEL/
# HOME_DIR/REPO_ROOT and sets BOOT_JSON/BOOT_OK/BOOT_EXIT. Byte-identical behavior.
oc_bootstrap_warden
pass_stage

# ===========================================================================
# PHASE 4 — SPAWN TEST AGENT on server-self
# ===========================================================================
stage "4. spawn test agent ($TEST_AGENT) on server-self → presence online"

# Activate the seeded test agent on the server-self machine. activate sets
# desired=online + binds to machine_id; the warden then spawns the runtime and
# the agent's SSE listen flips presence→online (hub projection, NOT the DB).
ACT_JSON="$(api_post_logged "/api/members/$TEST_AGENT/activate" "{\"machine_id\":\"$SERVER_SELF_ID\"}" || echo '{}')"
[[ -n "$(printf '%s' "$ACT_JSON" | json_field id)" ]] \
  || fail_stage "activate $TEST_AGENT on $SERVER_SELF_ID returned no member DTO — activation rejected"
log "activated $TEST_AGENT on $SERVER_SELF_ID"

# poll presence→online via the HUB (not DB).
poll_presence "$TEST_AGENT" online "$PRESENCE_TIMEOUT" \
  || fail_stage "$TEST_AGENT never reached presence=online on server-self within ${PRESENCE_TIMEOUT}s"
pass_stage

# ===========================================================================
# PHASE 5 — ASSERT ZERO SELF-REPAIR (two-track: SQL/dump + ask-the-agent)
# ===========================================================================
stage "5. zero self-repair verify ($TEST_AGENT on server-self — two-track)"

# TWO-TRACK zero-self-repair judgment (from lib): grep prescreen (red flag) +
# Track A dump for human/LLM + Track B ask-the-agent-about-friction (authoritative).
assert_no_self_repair "$TEST_AGENT" "server-self (single-machine)" \
  || fail_stage "$TEST_AGENT failed zero-self-repair on server-self boot (agent admitted friction/workaround, or probe unavailable + red flag)"

# eyeball the local tmux pane too — RED-FLAG pre-screen only; the two-track
# verdict above is authoritative. Agent runs on socket `$TMUX_SOCKET_LOCAL`,
# session `$(tmux_session $TEST_AGENT)` (= member-<agent>).
LOCAL_PANE="$(tmux -L "$TMUX_SOCKET_LOCAL" capture-pane -t "$(tmux_session "$TEST_AGENT")" -p 2>/dev/null || echo '')"
if [[ -n "$LOCAL_PANE" ]]; then
  echo "$LOCAL_PANE" | tail -n 40 | sed 's/^/[single-machine] pane| /' >&2
  if echo "$LOCAL_PANE" | grep -qE "$SELF_REPAIR_RE"; then
    warn "$TEST_AGENT tmux pane matched self-repair keywords (RED FLAG — review pane| above; two-track verdict already passed this stage)"
  else
    log "local tmux pane clean (no self-repair pattern)"
  fi
else
  warn "could not capture local tmux pane for $(tmux_session "$TEST_AGENT") (two-track judgment already ran) — continuing"
fi
pass_stage

# ===========================================================================
# PHASE 6 — TEARDOWN → CLEAN SLATE (restore)
# ===========================================================================
stage "6. teardown restore → clean slate (bounded, EXACT labels only)"

oc_teardown_bounded "post-run"
# Restore the state whitelist invariant: ~/.officraft back to empty/absent.
if [[ -d "$OC_ROOT" && -z "$(ls -A "$OC_ROOT" 2>/dev/null)" ]]; then
  log "restored: $OC_ROOT is empty (clean slate)"
elif [[ ! -e "$OC_ROOT" ]]; then
  log "restored: $OC_ROOT absent (clean slate)"
else
  warn "post-teardown: $OC_ROOT is NOT empty — residue remains (review before next run):"
  ls -A "$OC_ROOT" 2>/dev/null | sed 's/^/[single-machine] residue| /' >&2
fi
pass_stage

# ===========================================================================
# SUMMARY
# ===========================================================================
summarize
# If we got here every phase was PASS (any FAIL exits early via fail_stage).
for r in "${STAGE_RESULT[@]}"; do [[ "$r" == "PASS" ]] || exit 1; done
exit 0
