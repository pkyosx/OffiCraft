#!/usr/bin/env bash
# e2e_test/cross_machine.sh — MULTI-MACHINE cross-machine full-reset E2E regression.
# ============================================================================
# WHAT THIS IS (and is NOT):
#   A DESTRUCTIVE, FULL-RESET, MANUAL end-to-end regression that exercises the
#   whole cross-machine lifecycle on REAL hardware in one key:
#
#     teardown old server  →  fresh `ocserver install`  →  bootstrap server-self
#     warden  →  spawn a test agent (mira) on server-self  →  onboard a REAL
#     second machine  →  relocate the agent to the 2nd machine  →  assert the
#     agent never "self-repaired" (booted clean) before AND after relocate.
#
# ############################################################################
# ## ⚠️⚠️⚠️  KNOWN BLOCKER — WARDEN NAMING COLLISION — READ BEFORE A REAL RUN ##
# ############################################################################
# ##  officraft's warden uses launchd label `com.officraft.ocwarden` and
# ##  root `~/.officraft/warden`. On THIS box a *vibe-clicking* fleet warden
# ##  is ALSO resident and is SUSPECTED to collide on the same label/root
# ##  namespace. If they share a label or a launchd domain slot, this script's
# ##  STAGE 1 `launchctl bootout com.officraft.ocwarden` + STAGE 3 fresh
# ##  bootstrap could MURDER or CORRUPT the live vibe-clicking warden — the
# ##  exact cross-fleet blast-radius the EXACT-label rule (gotcha #5) exists to
# ##  prevent, but which an OVERLAPPING label cannot save us from.
# ##
# ##  THIS SCRIPT DOES NOT RESOLVE THE COLLISION. Before a real run, kyle/Seth
# ##  MUST first establish isolation, EITHER by:
# ##    (a) giving officraft its own warden label + root (e.g. relabel to
# ##        `com.officraft.e2e.ocwarden` and root `~/.officraft/warden`
# ##        that provably does NOT overlap the vibe-clicking fleet), OR
# ##    (b) running this whole script inside a dedicated throwaway VM/host that
# ##        carries NO vibe-clicking fleet at all.
# ##  …then set REQUIRE_ISOLATION_CONFIRMED=1 to acknowledge isolation is done.
# ##  Until then this script HARD-STOPS *before* STAGE 3 (the first warden
# ##  bootstrap) — STAGES 1–2 already wiped the OC *server*, but no warden
# ##  daemon has been booted/torn beyond the EXACT-label OC ones yet at that
# ##  point, so the vibe-clicking fleet is untouched by the guard's exit.
# ############################################################################
#
#   It ends with a PASS/FAIL summary (per-stage ✓/✗) and exits 0 only if every
#   stage is green.
#
#   ⚠️ This is NOT a CI unit test and NOT the isolated Playwright suite in this
#      directory (setup.sh/run_all.sh, :8791). This one:
#        • runs against the CANONICAL server on the CURRENT canonical serve port
#          (OC_CANONICAL_SERVE_PORT, read from server/ocserverd/config.go's
#          defaultPort — :7755 today; prod-ish local layout),
#        • tears down and re-installs the server + wardens from zero every run,
#        • REQUIRES a real reachable second machine (default eva-m5),
#        • is run by hand, by an operator who KNOWS it wipes the local server.
#      Because it is destructive it is NOT wired into run_all.sh and must be
#      invoked explicitly. It refuses to run without OC_CROSS_MACHINE_YES=1.
#
# ----------------------------------------------------------------------------
# HARD-WON GOTCHAS baked into this script's logic (do not "simplify" these away):
#
#   1. install.sh host-derived base — GET /install.sh templates OC_BASE from the
#      INCOMING REQUEST HOST (request.base_url). So the second machine MUST fetch
#      the installer via the PUBLIC host (officraft.hardcoretech.link), NEVER
#      via 127.0.0.1 — else the remote warden is pinned to an unreachable base.
#
#   2. ssh non-login shell lacks homebrew PATH — the generated install.sh
#      pre-checks for `tmux`/`curl`, which live in /opt/homebrew/bin. A plain
#      `ssh host '…'` runs a non-login shell without that on PATH, so every
#      remote command here exports PATH=/opt/homebrew/bin:$PATH first.
#
#   3. plist PATH is written in stone — both ocserver + `ocwarden install` hardcode
#      PATH=/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin in the launchd plist.
#      We don't fight that; we only assert the jobs are loaded + healthy.
#
#   4. presence truth = HUB PROJECTION, not the DB — online/presence come from
#      live SSE listeners (hub.online_members), NOT a member.online column. So we
#      poll GET /api/monitoring (sessions[].presence) and GET /api/machines
#      (online) for truth, and NEVER read member.online from sqlite.
#
#   5. EXACT-label kill ONLY — teardown uses `launchctl bootout gui/<uid>/<EXACT
#      label>`. NEVER pkill/killall/pattern-kill: this same box also runs
#      com.vibeclicking.* (and other fleet) jobs that a broad kill would murder.
#
#   6. .dump backup BEFORE any destruction — the sqlite DB is sqlite3-.dump'd to
#      /tmp before we bootout/rm anything, so a botched run is recoverable.
#
#   7. clean the remote's ~/.officraft ONLY — on the second machine we tar a
#      backup then wipe ONLY ~/.officraft. We NEVER touch ~/.vibe-clicking
#      (another fleet agent, e.g. wade, lives there).
#
#   8. owner password lives in the DB (hash) since B2 — a fresh `ocserver
#      install` seeds none: the server mints a one-shot claim code and the
#      banner walks the owner through the browser first-run flow. To get a
#      DETERMINISTIC, known password we PRE-SEED the DB with OWNER_PASSWORD
#      via `ocserverd set-password` BEFORE install (oc.toml is pre-rendered
#      via the render-config seam; install keeps both, and with a password
#      set no claim code is ever minted). See seed_owner_password().
#
# ----------------------------------------------------------------------------
# USAGE:
#   OC_CROSS_MACHINE_YES=1 bash e2e_test/cross_machine.sh
#
# PARAMS (env, all overridable — defaults in the block below):
#   PUBLIC_HOST      public server host (install.sh host-derived base + remote reach)
#   SECOND_MACHINE   ssh target for the second machine
#   OWNER_PASSWORD   deterministic owner password to seed (default: random uuid)
#   TEST_AGENT       seeded agent member id to spawn/relocate (default mira)
#   LOCAL_BASE       loopback base for local health/API (default
#                    127.0.0.1:$OC_CANONICAL_SERVE_PORT — the CURRENT canonical
#                    serve port read from server/ocserverd/config.go, NOT a
#                    literal that goes stale)
#   OC_CROSS_MACHINE_YES=1   REQUIRED — acknowledges this run is destructive.
#   REQUIRE_ISOLATION_CONFIRMED=1  REQUIRED before STAGE 3 — acknowledges the
#                    warden naming-collision blocker above has been resolved
#                    (isolated label/root OR isolated VM). Default 0 → hard-stop
#                    before the first warden bootstrap.
# ============================================================================
set -uo pipefail   # NOT -e: we drive control flow via explicit stage() gating.

# Shared lifecycle helpers (log/warn/die, stage bookkeeping, py/json_field,
# api_*, presence/online polling, two-track self-repair, tmux_session/agent_workdir
# + their SELF_REPAIR_RE / FRICTION_PROBE_* / STAGES state) live in a sourceable
# lib shared with single_machine_e2e.sh. PURE-MOVE — behavior unchanged.
source "$(dirname "${BASH_SOURCE[0]}")/lib/oc_lifecycle.sh"

# ---------------------------------------------------------------------------
# 0. params + constants
# ---------------------------------------------------------------------------
PUBLIC_HOST="${PUBLIC_HOST:-officraft.hardcoretech.link}"
SECOND_MACHINE="${SECOND_MACHINE:-eva-m5}"
TEST_AGENT="${TEST_AGENT:-mira}"
# LOCAL_BASE default — T-191d(E). This was a hardcoded `http://127.0.0.1:8770`,
# and 8770 is a RETIRED former officraft default (config.go's own migration
# history: 8770 → 8780 → 7755). That is not a cosmetic address bug here: this
# script is CANONICAL BY CONSTRUCTION (it installs and drives the REAL local
# server; seed_owner_password() below PINS the seeded oc.toml's serve port to
# `${LOCAL_BASE##*:}`), so a stale literal made the run BIND a port that
# oc_lifecycle.sh's live-fleet guard — which watches OC_CANONICAL_SERVE_PORT —
# is not watching. The guard then clears a port nobody binds while the run owns
# a different one: a blind guard that looks exactly like "all clear".
# Derive from the same single source of truth the lib uses
# (server/ocserverd/config.go's `defaultPort`, surfaced as
# OC_CANONICAL_SERVE_PORT by lib/oc_lifecycle.sh, sourced above — which FATALs
# if that parse fails, so there is no silent empty/degraded value). An explicit
# LOCAL_BASE= override still wins, unchanged.
# Guarded by e2e_test/tests_guard/run.sh case (14).
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:$OC_CANONICAL_SERVE_PORT}"
LOCAL_BASE="${LOCAL_BASE%/}"
PUBLIC_BASE="https://${PUBLIC_HOST}"
# Deterministic owner password: caller-provided, else a fresh uuid we seed + reuse.
OWNER_PASSWORD="${OWNER_PASSWORD:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Isolation ack for the warden naming-collision blocker (see the big ⚠️ header).
# 0 = not confirmed → the script HARD-STOPS before STAGE 3 (first warden boot).
REQUIRE_ISOLATION_CONFIRMED="${REQUIRE_ISOLATION_CONFIRMED:-0}"

# Presence poll budget (hub projection can lag warden SSE connect a few seconds).
PRESENCE_TIMEOUT="${PRESENCE_TIMEOUT:-150}"

# TIGHTENED CONSTANTS (verified against origin/main; do not loosen):
#   • agent workdir      = ~/.officraft/agents/<agent>/  (lowercase id; cli/ocwarden
#                          spawn cd's here — see testdata/golden_launch.txt).
#   • tmux session name  = member-<agent>  (OC_SESSION), on socket `officraft`
#                          (OC_TMUX_SOCKET) → `tmux -L officraft`.
#   • lsof               = present on eva-m5 (the default SECOND_MACHINE); the
#                          ESTABLISHED-socket check in STAGE 7 relies on it there.

# EXACT launchd labels (server + wardens). EXACT-label kill only (gotcha #5).
SERVE_LABEL="com.officraft.serve"
AUTODEPLOY_LABEL="com.officraft.autodeploy"
TUNNEL_LABEL="com.officraft.tunnel"
WARDEN_LABEL="com.officraft.ocwarden"

UID_NUM="$(id -u)"
GUI="gui/$UID_NUM"

# Canonical server layout (matches bin/ocserver).
HOME_DIR="${HOME:?HOME must be set}"
SERVER_ROOT="${OC_SERVER_ROOT:-$HOME_DIR/.officraft/server}"
OC_TOML="$SERVER_ROOT/oc.toml"
DB_PATH="$SERVER_ROOT/data/officraft.db"

# ── INSTANCE (T-8aa1): cross_machine is CANONICAL by construction ────────────
# Unlike the single/task/a1 suites (which default to a namespace-isolated
# instance via oc_resolve_instance), cross_machine CANNOT be namespaced: its
# relocate leg installs the 2nd machine through the PUBLIC host's install.sh
# (host-derived OC_BASE — gotcha #1), and that public tunnel only ever exposes
# the CANONICAL instance (a namespaced install is serve-ONLY, no tunnel). So it
# stays canonical; its construction-enforced protection is the LIVE-FLEET GUARD
# below, which upgrades the old operator assert into a hard, self-checking gate.
OC_NS=""                              # canonical → live-fleet guard runs in DIE mode
OC_ROOT="$HOME_DIR/.officraft"     # canonical instance root

# This script lives in e2e_test/; the checkout root is one level up.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
OCSERVER="$REPO_ROOT/bin/ocserver"
OCWARDEN="$REPO_ROOT/bin/ocwarden"
# NOTE: STAGE 3 uses the real product flow POST /api/machines/{id}/bootstrap-here
# (server runs `ocwarden install --force` in-process). The flip-era bash
# bin/warden-install has been retired/deleted — nothing here references it.

TS="$(date +%Y%m%d-%H%M%S)"
BACKUP_DIR="/tmp/oc-cross-machine-e2e-$TS"
mkdir -p "$BACKUP_DIR"

# Remote command prefix — ALWAYS export homebrew PATH (gotcha #2).
REMOTE_PATH_PREFIX='export PATH=/opt/homebrew/bin:$PATH;'

# remote SSH... — run a command on the second machine with homebrew PATH exported.
remote() {
  ssh "$SECOND_MACHINE" "$REMOTE_PATH_PREFIX $*"
}

# ---------------------------------------------------------------------------
# preflight — refuse to run unless the operator acknowledged destruction.
# ---------------------------------------------------------------------------
[[ "${OC_CROSS_MACHINE_YES:-}" == "1" ]] || die \
  "refusing: this is a DESTRUCTIVE full-reset E2E (wipes the local server + agent, needs a real second machine). Re-run with OC_CROSS_MACHINE_YES=1 to acknowledge."

for tool in curl ssh sqlite3 uuidgen launchctl; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool missing on PATH: $tool"
done
[[ -x "$OCSERVER" ]] || die "bin/ocserver not found/executable at $OCSERVER"

log "params: PUBLIC_HOST=$PUBLIC_HOST SECOND_MACHINE=$SECOND_MACHINE TEST_AGENT=$TEST_AGENT LOCAL_BASE=$LOCAL_BASE"
log "layout: SERVER_ROOT=$SERVER_ROOT DB=$DB_PATH  backups→$BACKUP_DIR"

# ── LIVE-FLEET GUARD (T-8aa1) — construction-enforced, BEFORE any teardown ────
# STAGE 1 below boots out the EXACT canonical labels (incl. com.officraft.
# ocwarden) and rm -rf's the server root with ONLY OC_CROSS_MACHINE_YES=1 acked —
# on a host with a LIVE fleet that would murder the real warden/agents. This
# read-only guard detects a live canonical fleet and DIES HERE (before STAGE 1),
# so cross_machine can only proceed on a host with no live fleet — exactly
# condition (b) in this script's header, now ENFORCED, not merely asserted.
oc_live_fleet_guard

# ===========================================================================
# STAGE 1 — TEARDOWN OLD SERVER (destructive, but backup FIRST)
# ===========================================================================
stage "1. teardown old server (backup → EXACT-label bootout → warden teardown → rm dirs)"

# 1a. .dump backup BEFORE any destruction (gotcha #6). Best-effort if DB absent.
if [[ -f "$DB_PATH" ]]; then
  if sqlite3 "$DB_PATH" ".dump" > "$BACKUP_DIR/officraft.dump.sql" 2>/dev/null; then
    log "backed up DB → $BACKUP_DIR/officraft.dump.sql ($(wc -l < "$BACKUP_DIR/officraft.dump.sql") lines)"
  else
    warn "sqlite3 .dump failed (DB may be locked/corrupt) — continuing (backup best-effort)"
  fi
else
  log "no existing DB at $DB_PATH — nothing to back up (first-ever install?)"
fi

# 1b. bootout the server + warden jobs by EXACT label ONLY (gotcha #5).
#     NEVER pkill/killall — com.vibeclicking.* etc. share this box.
for label in "$SERVE_LABEL" "$AUTODEPLOY_LABEL" "$TUNNEL_LABEL" "$WARDEN_LABEL"; do
  log "launchctl bootout $GUI/$label (EXACT label; tolerate not-loaded)"
  launchctl bootout "$GUI/$label" 2>/dev/null || true
done

# 1c. best-effort ocwarden teardown (removes its plist/tokfile cleanly if present).
if [[ -x "$OCWARDEN" ]]; then
  log "bin/ocwarden teardown (best-effort clean warden removal)"
  "$OCWARDEN" teardown 2>&1 | sed 's/^/[cross-machine] warden-td| /' >&2 || \
    warn "ocwarden teardown returned non-zero (may be already-gone) — continuing"
fi

# 1d. remove the server run dir + the local ~/.officraft server layout.
#     We only remove the officraft server tree — never ~/.vibe-clicking (gotcha #7).
log "rm -rf $SERVER_ROOT (server checkout/db/config/logs — backed up above)"
rm -rf "$SERVER_ROOT"
# Also clear a stale local exec-warden tokfile so the fresh warden re-mints cleanly.
rm -f "$HOME_DIR/.officraft/exec-warden.tok" 2>/dev/null || true

# 1e. retire STALE AGENT RUNTIMES + workdirs from the previous install. A surviving
#     member-<id> session holds a token signed by the WIPED server (permanently
#     deauthed) AND squats the tmux session name, so the fresh warden's
#     clobber-guard correctly refuses the new spawn ("already-running") and the
#     agent can never reach presence=online — a full reset must reset agents too.
#     EXACT targeting only: officraft agents live on the dedicated tmux socket
#     `officraft` as `member-<id>`; the vibe-clicking fleet lives on OTHER
#     sockets and is never touched (gotcha #5/#7).
if tmux -L officraft ls >/dev/null 2>&1; then
  while IFS= read -r sess; do
    [[ "$sess" == member-* ]] || continue
    log "tmux -L officraft kill-session -t =$sess (EXACT stale agent session from previous install)"
    tmux -L officraft kill-session -t "=$sess" 2>/dev/null || true
  done < <(tmux -L officraft ls -F '#S' 2>/dev/null)
fi
if [[ -d "$HOME_DIR/.officraft/agents" ]]; then
  tar -czf "$BACKUP_DIR/agents-workdirs.tgz" -C "$HOME_DIR/.officraft" agents 2>/dev/null \
    || warn "agents/ backup tar failed — continuing (workdirs are disposable caches)"
  log "rm -rf $HOME_DIR/.officraft/agents (stale agent workdirs/creds — backed up)"
  rm -rf "$HOME_DIR/.officraft/agents"
fi

# verify the labels are truly gone. bootout is asynchronous — the job can stay
# registered for a few seconds while its process exits, so poll-until-gone
# (bounded) instead of a single immediate check (a real re-registration, e.g.
# KeepAlive re-bootstrap, will still be caught after the deadline).
for label in "$SERVE_LABEL" "$WARDEN_LABEL"; do
  gone=0
  for _ in $(seq 1 20); do
    if ! launchctl print "$GUI/$label" >/dev/null 2>&1; then gone=1; break; fi
    sleep 1
  done
  if [[ "$gone" != 1 ]]; then
    fail_stage "$label still registered 20s after bootout — refusing to proceed on a dirty box"
  fi
done
[[ -d "$SERVER_ROOT" ]] && fail_stage "server root still present after rm: $SERVER_ROOT"
pass_stage

# ===========================================================================
# STAGE 2 — FRESH INSTALL SERVER  + seed KNOWN owner password
# ===========================================================================
stage "2. fresh install server (ocserver install --force) + seed owner password"

# 2a. PRE-SEED the KNOWN OWNER_PASSWORD (gotcha #8). Since B2 no credential
#     lives in oc.toml: render the (port+dsn) oc.toml via the render-config
#     seam, then write OWNER_PASSWORD's hash straight into the fresh DB via
#     the committed `bin/ocserverd set-password`. install keeps the oc.toml,
#     and with the password already set the server mints no claim code — we
#     log in deterministically, no first-run flow to drive.
seed_owner_password() {
  mkdir -p "$SERVER_ROOT/data"
  local dsn="sqlite:///$DB_PATH"
  # render-config EXAMPLE OUT DSN — same renderer install step 3 uses.
  "$OCSERVER" render-config "$REPO_ROOT/oc.toml.example" "$OC_TOML" "$dsn" \
    || die "ocserver render-config failed — cannot seed the e2e oc.toml"
  # Pin the serve port to LOCAL_BASE's port in the seeded oc.toml (defaults to
  # the SSOT-derived OC_CANONICAL_SERVE_PORT, but honor an override so
  # LOCAL_BASE and the config never drift). THIS is why the LOCAL_BASE default
  # above must not be a stale literal: this line decides what the server binds.
  local port="${LOCAL_BASE##*:}"
  if [[ "$port" =~ ^[0-9]+$ ]]; then
    py -c '
import sys, re
p, port = sys.argv[1], sys.argv[2]
txt = open(p, encoding="utf-8").read()
txt = re.sub(r"(?m)^port\s*=\s*\d+", f"port = {port}", txt, count=1)
open(p, "w", encoding="utf-8").write(txt)
' "$OC_TOML" "$port"
  fi
  # Migrate + store the argon2id hash in the DB (password rides env, not argv).
  OC_CONFIG="$OC_TOML" OC_NEW_PASSWORD="$OWNER_PASSWORD" "$REPO_ROOT/bin/ocserverd" set-password >/dev/null \
    || die "ocserverd set-password failed — cannot seed a known owner password"
  log "seeded oc.toml (port=${port:-$OC_CANONICAL_SERVE_PORT}) + known OWNER_PASSWORD hash → DB ($DB_PATH)"
}
seed_owner_password

# 2b. run the installer. It sees our pre-seeded oc.toml and KEEPS it; it builds
#     the venv, migrates, and loads serve+autodeploy under launchd. --force makes
#     it re-run every step (fresh box).
log "bin/ocserver install --force (keeps our seeded oc.toml; builds venv + migrates + loads launchd)"
if ! "$OCSERVER" install --force 2>&1 | sed 's/^/[cross-machine] install| /' >&2; then
  fail_stage "ocserver install --force failed (see install| lines above)"
fi

# 2c. health: /health must be 200.
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$LOCAL_BASE/health" 2>/dev/null || echo 000)"
[[ "$code" == "200" ]] || fail_stage "server /health not 200 after install (got $code)"
log "/health = 200"

# 2d. /api/version must carry a git_sha (proves the real server booted).
VER_JSON="$(curl -fsS --max-time 5 "$LOCAL_BASE/api/version" 2>/dev/null || echo '{}')"
GIT_SHA="$(printf '%s' "$VER_JSON" | json_field git_sha)"
[[ -n "$GIT_SHA" && "$GIT_SHA" != "unknown" ]] || fail_stage "/api/version returned no usable git_sha (got '$GIT_SHA')"
log "/api/version git_sha=$GIT_SHA"

# 2e. login with the KNOWN owner password → owner token (used by all api_* helpers).
LOGIN_JSON="$(curl -fsS --max-time 10 -X POST "$LOCAL_BASE/api/login" \
  -H 'content-type: application/json' -d "{\"password\":\"$OWNER_PASSWORD\"}" 2>/dev/null || echo '{}')"
OWNER_TOKEN="$(printf '%s' "$LOGIN_JSON" | json_field token)"
[[ -n "$OWNER_TOKEN" ]] || fail_stage "owner login failed with seeded password — cannot obtain owner token"
log "owner login OK — token acquired"

# ── SERVE STABILITY WINDOW ──────────────────────────────────────────────────
# Fresh install loads autodeploy, whose FIRST tick restarts serve moments after
# the install's own health check passed. A long RPC (stage 3's bootstrap-here)
# fired inside that restart window gets cut mid-install by the graceful
# shutdown (500 CancelledError) and leaves half-installed warden state (RUN#7).
# So: only leave stage 2 after /health has been continuously green for 15
# consecutive seconds (any miss resets the streak; bounded at 120s).
log "waiting for serve stability (15s of consecutive /health greens; autodeploy first tick passes through here)"
stable=0
stability_deadline=$(( $(date +%s) + 120 ))
while [[ "$(date +%s)" -lt "$stability_deadline" ]]; do
  if curl -fsS --max-time 2 "$LOCAL_BASE/health" >/dev/null 2>&1; then
    stable=$((stable + 1))
    [[ "$stable" -ge 15 ]] && break
  else
    stable=0
  fi
  sleep 1
done
[[ "$stable" -ge 15 ]] \
  || fail_stage "serve never held 15s of consecutive /health greens within 120s (still restart-cycling)"
log "serve stable (15s green streak) — safe to fire long RPCs"
pass_stage

# ===========================================================================
# STAGE 3 — BOOTSTRAP SERVER-SELF WARDEN (this machine)
# ===========================================================================
stage "3. bootstrap server-self warden (local) + verify launchd loaded + SSE connected"

# ── ISOLATION GATE — the warden naming-collision blocker (see big ⚠️ header) ──
# STAGE 3 is the FIRST place we BOOT a warden under label $WARDEN_LABEL. If the
# officraft warden label/root is NOT isolated from the vibe-clicking fleet, a
# real run here risks murdering/corrupting the live fleet warden. HARD-STOP unless
# the operator has established isolation and set REQUIRE_ISOLATION_CONFIRMED=1.
if [[ "$REQUIRE_ISOLATION_CONFIRMED" != "1" ]]; then
  log "──────────────────────────────────────────────────────────────"
  warn "HARD-STOP before STAGE 3 warden bootstrap: warden naming-collision UNCONFIRMED."
  warn "officraft warden label='$WARDEN_LABEL' root='$HOME_DIR/.officraft/warden'"
  warn "is SUSPECTED to collide with a resident vibe-clicking fleet warden on this box."
  warn "Booting/tearing a warden now could take out the live fleet warden (cross-fleet"
  warn "blast radius). RESOLVE isolation first — EITHER:"
  warn "  (a) give officraft its own non-overlapping warden label + root, OR"
  warn "  (b) run this whole script in a throwaway VM/host with NO vibe-clicking fleet."
  warn "Then re-run with REQUIRE_ISOLATION_CONFIRMED=1 to acknowledge."
  warn "STAGES 1–2 wiped the OC *server* only; no non-OC warden was touched by this stop."
  fail_stage "REQUIRE_ISOLATION_CONFIRMED=1 not set — refusing to bootstrap a warden on a box with an unresolved fleet-warden collision"
fi
log "isolation confirmed (REQUIRE_ISOLATION_CONFIRMED=1) — proceeding to warden bootstrap"

# The server-self warden member ('m-server-self') is auto-seeded by the DB. We
# install a real local warden bound to it via the REAL PRODUCT FLOW:
#   POST /api/machines/{m-server-self}/bootstrap-here
# The server resolves the ocwarden binary (503 if absent), re-mints a fresh
# exec-token internally, and runs `<ocwarden> install --force` as a subprocess in
# the SERVER USER's own ~/.officraft (HOME is inherited) — i.e. it installs the
# warden ON THIS HOST, which is exactly what "bootstrap server-self" means. We use
# this (rather than a side installer) because it is the flow the owner actually clicks in
# the UI, so the regression exercises the real code path (handle_bootstrap_here),
# not a side installer. bootstrap-here is OWNER-ONLY — our OWNER_TOKEN qualifies.
SERVER_SELF_ID="m-server-self"

log "POST /api/machines/$SERVER_SELF_ID/bootstrap-here (server installs its own warden via ocwarden install --force)"
# bootstrap-here is a SYNCHRONOUS install RPC: the handler runs `ocwarden install
# --force` inline and budgets 60s server-side before returning. The default 15s
# client cap cuts BEFORE the handler returns (~16-20s install → code 000=timeout),
# and the 000-retry loop then re-fires --force every attempt (bounces the warden).
# Pass a 90s per-attempt cap (> the server's 60s ceiling) so a real handler timeout
# surfaces as a returned ok=false, not a client-side early cut; 2 attempts (a
# genuine conn-refused still gets one bounded retry).
BOOT_JSON="$(api_post_logged "/api/machines/$SERVER_SELF_ID/bootstrap-here" '{}' 2 90 || echo '{}')"
BOOT_OK="$(printf '%s' "$BOOT_JSON" | json_field ok)"
BOOT_EXIT="$(printf '%s' "$BOOT_JSON" | json_field exit_code)"
# Surface the merged stdout+stderr `log` field verbatim (the guard/error reason).
printf '%s' "$BOOT_JSON" | py -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for line in str(d.get("log","")).splitlines():
    sys.stderr.write("[cross-machine] bootstrap-here| " + line + "\n")
' 2>/dev/null || true
# ok is a JSON bool → json_field prints "True"/"False" (python truthiness).
if [[ "$BOOT_OK" != "True" && "$BOOT_OK" != "true" ]]; then
  fail_stage "bootstrap-here for $SERVER_SELF_ID returned ok=$BOOT_OK exit_code=$BOOT_EXIT (see bootstrap-here| lines — 503=ocwarden binary absent, exit 1=one-warden guard refused)"
fi
log "bootstrap-here ok (exit_code=$BOOT_EXIT)"

# verify the warden launchd job is loaded.
launchctl print "$GUI/$WARDEN_LABEL" >/dev/null 2>&1 \
  || fail_stage "$WARDEN_LABEL not loaded after bootstrap-here"
log "launchd $WARDEN_LABEL loaded"

# verify the warden actually connected its SSE command reader (log line:
# "[ocwarden] command reader: enabled (SSE …)"). bootstrap-here installs into the
# server user's ~/.officraft/warden, so logs land under that warden root's
# log/ dir. Poll the err/out log briefly (repo var/log kept as a legacy fallback).
WARDEN_ROOT="$HOME_DIR/.officraft/warden"
sse_ok=""
for _ in $(seq 1 20); do
  if grep -qE 'command reader: enabled \(SSE' \
       "$WARDEN_ROOT/log/ocwarden.err.log" "$WARDEN_ROOT/log/ocwarden.out.log" \
       "$REPO_ROOT/var/log/ocwarden.err.log" "$REPO_ROOT/var/log/ocwarden.out.log" 2>/dev/null; then
    sse_ok=1; break
  fi
  sleep 1
done
# Belt-and-braces: the hub itself should show server-self online once SSE is up.
if [[ -z "$sse_ok" ]]; then
  poll_machine_online "$SERVER_SELF_ID" 30 \
    && sse_ok=1 \
    && log "server-self online via hub (SSE connected, log line not matched but hub confirms)"
fi
[[ -n "$sse_ok" ]] || fail_stage "server-self warden never reported SSE 'command reader: enabled' nor went online in the hub"
log "server-self warden SSE connected"
pass_stage

# ===========================================================================
# STAGE 4 — SPAWN TEST AGENT on server-self + assert ZERO self-repair
# ===========================================================================
stage "4. spawn test agent ($TEST_AGENT) on server-self → presence online → zero self-repair"

# Activate the seeded test agent (default 'mira') on the server-self machine.
# activate sets desired=online + binds to machine_id; the warden then spawns the
# runtime and the agent's SSE listen flips presence→online (hub projection).
ACT_JSON="$(api_post_logged "/api/members/$TEST_AGENT/activate" "{\"machine_id\":\"$SERVER_SELF_ID\"}" || echo '{}')"
[[ -n "$(printf '%s' "$ACT_JSON" | json_field id)" ]] \
  || fail_stage "activate $TEST_AGENT on $SERVER_SELF_ID returned no member DTO — activation rejected"
log "activated $TEST_AGENT on $SERVER_SELF_ID"

# poll presence→online via the HUB (not DB) — gotcha #4.
poll_presence "$TEST_AGENT" online "$PRESENCE_TIMEOUT" \
  || fail_stage "$TEST_AGENT never reached presence=online on server-self within ${PRESENCE_TIMEOUT}s"

# zero-self-repair gate #1 (pre-relocate): SQL scan of the agent's chat messages.
assert_no_self_repair "$TEST_AGENT" "server-self (pre-relocate)" \
  || fail_stage "$TEST_AGENT emitted self-repair chatter on server-self boot"

# eyeball the local tmux pane too — the agent runs on socket `$TMUX_SOCKET`,
# session `$(tmux_session $TEST_AGENT)` (= member-<agent>). This is a RED-FLAG
# pre-screen only (a grep hit is logged for review); the authoritative
# zero-self-repair verdict already ran via assert_no_self_repair (two-track) above.
LOCAL_PANE="$(tmux -L "$TMUX_SOCKET" capture-pane -t "$(tmux_session "$TEST_AGENT")" -p 2>/dev/null || echo '')"
if [[ -n "$LOCAL_PANE" ]]; then
  echo "$LOCAL_PANE" | tail -n 40 | sed 's/^/[cross-machine] pane| /' >&2
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
# STAGE 5 — ONBOARD SECOND MACHINE (real remote host)
# ===========================================================================
stage "5. onboard second machine ($SECOND_MACHINE) via install.sh (public host) + verify"

# 5a. reachability.
ssh -o ConnectTimeout=10 -o BatchMode=yes "$SECOND_MACHINE" true 2>/dev/null \
  || fail_stage "cannot ssh $SECOND_MACHINE (need key-based access, BatchMode)"

# 5b. backup then wipe ONLY the remote ~/.officraft (gotcha #7 — never
#     ~/.vibe-clicking, another fleet agent lives there). Same stale-agent rule as
#     local 1e: a member-<id> session surviving from a previous run squats the
#     session name (clobber-guard refuses the relocate spawn) while holding a
#     token the wiped server no longer honors — kill EXACT member-* sessions on
#     the dedicated `officraft` tmux socket first (other sockets untouched).
log "backing up + wiping remote ~/.officraft on $SECOND_MACHINE (NOT ~/.vibe-clicking)"
# Bootout the remote warden job FIRST (EXACT label, tolerate not-loaded), exactly
# like local stage 1: wiping the files while the launchd job stays registered
# leaves a crash-looping registration that makes the fresh install's bootstrap
# fail with "Bootstrap failed: 5: Input/output error" (ocwarden install renders
# the plist but does not bootout a pre-registered label — RUN#5 hit this live).
remote 'launchctl bootout "gui/$(id -u)/'"$WARDEN_LABEL"'" 2>/dev/null; for i in 1 2 3 4 5 6 7 8 9 10; do launchctl print "gui/$(id -u)/'"$WARDEN_LABEL"'" >/dev/null 2>&1 || break; sleep 1; done; true' \
  || warn "remote warden bootout returned non-zero — continuing"
remote 'if tmux -L officraft ls >/dev/null 2>&1; then tmux -L officraft ls -F "#S" 2>/dev/null | while IFS= read -r s; do case "$s" in member-*) echo "[remote] kill stale agent session: $s"; tmux -L officraft kill-session -t "=$s" 2>/dev/null || true;; esac; done; fi' \
  || warn "remote stale-session sweep returned non-zero — continuing"
remote 'if [ -d "$HOME/.officraft" ]; then tar -czf /tmp/officraft-backup-'"$TS"'.tgz -C "$HOME" .officraft 2>/dev/null || true; fi; rm -rf "$HOME/.officraft"' \
  || warn "remote backup/wipe returned non-zero — inspect (continuing; wipe is best-effort)"
# hard guard: prove we did NOT touch the sibling fleet dir.
remote '[ -e "$HOME/.vibe-clicking" ] && echo VIBE_PRESENT || echo VIBE_ABSENT' \
  | grep -q 'VIBE_' || warn "could not confirm ~/.vibe-clicking status on remote"

# 5c. create the machine member on the server (owner-gated) → machine_id + token.
ONBOARD_JSON="$(api_post_logged /api/machines "{\"display_name\":\"e2e-$SECOND_MACHINE-$TS\"}" || echo '{}')"
MACHINE_ID="$(printf '%s' "$ONBOARD_JSON" | json_field machine_id)"
MACHINE_TOKEN="$(printf '%s' "$ONBOARD_JSON" | json_field token)"
[[ -n "$MACHINE_ID" && -n "$MACHINE_TOKEN" ]] \
  || fail_stage "POST /api/machines returned no machine_id/token"
log "onboarded machine member: machine_id=$MACHINE_ID"

# 5d. remote install via the PUBLIC install.sh (gotcha #1: host-derived base — the
#     generated script's OC_BASE comes from the request host, so we MUST hit the
#     public host, not 127.0.0.1). ssh non-login shell → export homebrew PATH so
#     the install.sh tmux/curl precheck passes (gotcha #2).
log "remote install: curl '$PUBLIC_BASE/install.sh?token=<redacted>' | bash  (on $SECOND_MACHINE)"
if ! ssh "$SECOND_MACHINE" \
     "$REMOTE_PATH_PREFIX cd /tmp && curl -fsSL '$PUBLIC_BASE/install.sh?token=$MACHINE_TOKEN' | bash" \
     2>&1 | sed 's/^/[cross-machine] remote-install| /' >&2; then
  fail_stage "remote install.sh pipeline failed on $SECOND_MACHINE (see remote-install| lines)"
fi

# 5e. verify the remote install artifacts: real Mach-O ocagent + ocwarden binaries,
#     the warden launchd job loaded, and the exec-warden tokfile present.
log "verifying remote warden artifacts on $SECOND_MACHINE"
REMOTE_AGENT_FILE="$(remote 'file "$HOME/.officraft/warden/ocagent" 2>/dev/null' || echo '')"
echo "$REMOTE_AGENT_FILE" | grep -qE 'Mach-O' \
  || fail_stage "remote ~/.officraft/warden/ocagent is not a Mach-O binary (got: ${REMOTE_AGENT_FILE:-<missing>})"
remote 'test -x "$HOME/.officraft/warden/ocwarden"' \
  || fail_stage "remote ~/.officraft/warden/ocwarden missing/not executable"
remote 'test -f "$HOME/.officraft/warden/exec-warden.tok"' \
  || fail_stage "remote exec-warden tokfile missing (~/.officraft/warden/exec-warden.tok)"
log "remote ocagent is Mach-O; ocwarden + tokfile present"

# 5f. remote warden launchd job loaded + SSE connected.
remote "launchctl print $GUI/$WARDEN_LABEL >/dev/null 2>&1" \
  || fail_stage "remote $WARDEN_LABEL not loaded on $SECOND_MACHINE"
sse_ok=""
for _ in $(seq 1 20); do
  if remote 'grep -qE "command reader: enabled \(SSE" "$HOME/.officraft/warden/log/ocwarden.err.log" "$HOME/.officraft/warden/log/ocwarden.out.log" 2>/dev/null'; then
    sse_ok=1; break
  fi
  sleep 1
done
[[ -n "$sse_ok" ]] || log "remote SSE log line not matched yet — falling back to hub online check"

# 5g. server-side truth: the machine member must go online in the HUB (gotcha #4).
poll_machine_online "$MACHINE_ID" "$PRESENCE_TIMEOUT" \
  || fail_stage "second machine ($MACHINE_ID) never went online in the hub within ${PRESENCE_TIMEOUT}s"
pass_stage

# ===========================================================================
# STAGE 6 — RELOCATE TEST AGENT → SECOND MACHINE
# ===========================================================================
stage "6. COLD relocate $TEST_AGENT → $SECOND_MACHINE (stop → activate {machine_id=2nd} → observed there)"

# COLD MOVE — the owner's REAL relocation flow (Seth 2026-07-10): "我需要移動的
# 時候會先將它停止，然後再重新在另外一台電腦上喚醒". Stop first, then wake on
# the target machine. A HOT relocate (re-pointing a still-running agent) has NO
# mechanism since auto-relocate was removed (ffe5e01) and is NOT the supported
# path — RUN#6 proved re-pointing a live agent silently does nothing, so this
# stage deliberately exercises the cold flow instead.
log "cold relocate step 1/2: deactivate $TEST_AGENT (stop on current machine)"
DEACT_JSON="$(api_post_logged "/api/members/$TEST_AGENT/deactivate" '{}' || echo '{}')"
[[ -n "$(printf '%s' "$DEACT_JSON" | json_field id)" ]] \
  || fail_stage "deactivate $TEST_AGENT returned no member DTO"
# Wait for the stop to take via the member DTO (NOT poll_presence/monitoring
# sessions — a stopped member may drop out of the sessions list entirely, which
# reads as "" there). The settled post-deactivate state is "stopped", NOT
# "offline": deactivate stamps stopping_since, and presence_state projects a
# stamped member as stopping (SSE live) → stopped (SSE dropped); the stamp is
# only cleared on the NEXT spawn (into waking). "offline" is the never-stamped
# case, so accept either as "the stop took".
deadline=$(( $(date +%s) + PRESENCE_TIMEOUT ))
cur_presence=""
while [[ "$(date +%s)" -lt "$deadline" ]]; do
  cur_presence="$(api_get "/api/members/$TEST_AGENT" 2>/dev/null | json_field presence)"
  [[ "$cur_presence" == "offline" || "$cur_presence" == "stopped" ]] && break
  sleep 3
done
[[ "$cur_presence" == "offline" || "$cur_presence" == "stopped" ]] \
  || fail_stage "$TEST_AGENT never settled (offline|stopped) after deactivate within ${PRESENCE_TIMEOUT}s (last='$cur_presence' — stop did not take)"
log "presence[$TEST_AGENT]=$cur_presence (stop took on origin machine)"

log "cold relocate step 2/2: activate $TEST_AGENT on $MACHINE_ID (wake on the 2nd machine)"
REL_JSON="$(api_post_logged "/api/members/$TEST_AGENT/activate" "{\"machine_id\":\"$MACHINE_ID\"}" || echo '{}')"
[[ -n "$(printf '%s' "$REL_JSON" | json_field id)" ]] \
  || fail_stage "relocate activate $TEST_AGENT → $MACHINE_ID returned no member DTO"
log "relocate requested: $TEST_AGENT → $MACHINE_ID"

# The agent must come online ON THE SECOND MACHINE. Presence alone is a FALSE
# GREEN (the hub projects online for ANY live SSE listener, wherever it runs),
# so the load-bearing assertion is the OBSERVED machine claim: poll the member
# DTO until machine == the TARGET machine id (the SSE-claim `machine_of`, which
# an agent cannot fake — it rides the connection token).
deadline=$(( $(date +%s) + PRESENCE_TIMEOUT ))
observed=""
while [[ "$(date +%s)" -lt "$deadline" ]]; do
  observed="$(api_get "/api/members/$TEST_AGENT" 2>/dev/null | json_field machine)"
  if [[ "$observed" == "$MACHINE_ID" ]]; then
    break
  fi
  sleep 3
done
[[ "$observed" == "$MACHINE_ID" ]] \
  || fail_stage "$TEST_AGENT never OBSERVED on $MACHINE_ID within ${PRESENCE_TIMEOUT}s (observed machine='$observed' — presence-online alone is not relocation)"
log "observed machine[$TEST_AGENT]=$observed (== target $MACHINE_ID)"
# ...and it must also be presence=online there (it may blip offline→waking→online).
poll_presence "$TEST_AGENT" online "$PRESENCE_TIMEOUT" \
  || fail_stage "$TEST_AGENT did not reach presence=online after relocate within ${PRESENCE_TIMEOUT}s"
pass_stage

# ===========================================================================
# STAGE 7 — POST-RELOCATE ZERO SELF-REPAIR (on the second machine)
# ===========================================================================
stage "7. post-relocate zero self-repair on $SECOND_MACHINE (tmux + SQL + ESTABLISHED + symlink)"

# 7a. eyeball the remote tmux pane — RED-FLAG pre-screen only. The agent runs on
#     socket `$TMUX_SOCKET`, session member-<agent>. Homebrew PATH exported so tmux
#     resolves (gotcha #2). A grep hit is logged for review; the authoritative
#     verdict is the two-track judgment in 7b.
REMOTE_SESSION="$(tmux_session "$TEST_AGENT")"
REMOTE_PANE="$(remote "tmux -L $TMUX_SOCKET capture-pane -t $REMOTE_SESSION -p 2>/dev/null" || echo '')"
if [[ -n "$REMOTE_PANE" ]]; then
  echo "$REMOTE_PANE" | tail -n 40 | sed 's/^/[cross-machine] remote-pane| /' >&2
  if echo "$REMOTE_PANE" | grep -qE "$SELF_REPAIR_RE"; then
    warn "$TEST_AGENT remote tmux pane matched self-repair keywords (RED FLAG — review remote-pane|; two-track verdict in 7b decides)"
  else
    log "remote tmux pane clean (no self-repair pattern)"
  fi
else
  warn "could not capture remote tmux pane for $REMOTE_SESSION — relying on two-track judgment + link checks"
fi

# 7b. TWO-TRACK zero-self-repair judgment again, post-relocate (gate #2): grep
#     prescreen (red flag) + Track A dump + Track B ask-the-agent-about-friction.
assert_no_self_repair "$TEST_AGENT" "$SECOND_MACHINE (post-relocate)" \
  || fail_stage "$TEST_AGENT failed zero-self-repair after relocate (agent admitted friction/workaround, or probe unavailable + red flag)"

# 7c. the remote ocagent must actually be listening on the server SSE (an
#     ESTABLISHED connection to the server) — proves the runtime is live on the
#     2nd host, not a ghost. lsof on the ocagent process' ESTABLISHED sockets.
EST="$(remote "lsof -nP -iTCP -sTCP:ESTABLISHED 2>/dev/null | grep -i ocagent || true")"
if [[ -n "$EST" ]]; then
  echo "$EST" | sed 's/^/[cross-machine] estab| /' >&2
  log "remote ocagent has an ESTABLISHED SSE connection"
else
  # Fallback: at minimum the process must exist and presence is already online.
  remote "pgrep -fl ocagent >/dev/null 2>&1" \
    || fail_stage "no remote ocagent process/ESTABLISHED SSE connection found after relocate"
  warn "no ESTABLISHED socket matched but an ocagent process is running (presence online) — accepting"
fi

# 7d. workdir ocagent must be a SYMLINK → warden/ocagent (per HEAD commit: spawn
#     publishes the workdir ocagent as a symlink to the resolved warden binary).
#     The CANONICAL workdir is ~/.officraft/agents/<agent>/ (verified against
#     origin/main cli/ocwarden spawn — golden_launch.txt cd's exactly here). We
#     check that path first; a maxdepth find stays only as a diagnostic fallback.
LINK_CHECK="$(remote '
wd="$(printf "%s/.officraft/agents/%s" "$HOME" '"'$TEST_AGENT'"')"
if [ -L "$wd/ocagent" ]; then
  printf "%s -> %s\n" "$wd/ocagent" "$(readlink "$wd/ocagent")"; exit 0
fi
# Fallback (diagnostic): any ocagent symlink under ~/.officraft/agents.
found=$(find "$HOME/.officraft/agents" -maxdepth 3 -type l -name ocagent 2>/dev/null | head -1)
[ -n "$found" ] && printf "%s -> %s\n" "$found" "$(readlink "$found")"
' || echo '')"
if [[ -n "$LINK_CHECK" ]]; then
  log "workdir ocagent symlink: $LINK_CHECK"
  echo "$LINK_CHECK" | grep -q 'warden/ocagent' \
    || warn "workdir ocagent symlink does not resolve to warden/ocagent (got: $LINK_CHECK) — review"
else
  warn "could not locate a workdir ocagent symlink on $SECOND_MACHINE (agent workdir path may differ) — review manually"
fi
pass_stage

# ===========================================================================
# STAGE 8 — SUMMARY
# ===========================================================================
summarize
# If we got here every stage was PASS (any FAIL exits early via fail_stage).
# summarize recomputes overall; exit accordingly for the caller.
for r in "${STAGE_RESULT[@]}"; do [[ "$r" == "PASS" ]] || exit 1; done
exit 0
