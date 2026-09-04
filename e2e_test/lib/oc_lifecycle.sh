#!/usr/bin/env bash
# e2e_test/lib/oc_lifecycle.sh — SHARED officraft lifecycle helpers.
# Consumers: e2e_test/single_machine_e2e.sh, e2e_test/cross_machine.sh
# changing this affects BOTH — verify both.
# ============================================================================
# PURE-MOVE extraction from cross_machine.sh (origin/main=13719992). Helper
# bodies + their module-level state/config are byte-identical to the originals;
# no behavior change. Consumers set the runtime globals these read (OWNER_TOKEN,
# LOCAL_BASE, DB_PATH, BACKUP_DIR, and — for summarize() — PUBLIC_HOST /
# SECOND_MACHINE / TEST_AGENT) before calling. `remote()` is NOT here (it is
# cross-machine-only and stays in cross_machine.sh).
# ============================================================================

# ---------------------------------------------------------------------------
# shared config/state moved with the helpers (read by the helpers below)
# ---------------------------------------------------------------------------
# Friction-probe reply budget — SEPARATE from the presence budget: a just-
# relocated agent is presence=online the moment its SSE mounts, but a REAL
# claude still has its whole boot sequence (persona → resume → checkin) to run
# before it services a chat probe; cold boot has no firm upper bound (RUN#4/#9
# both saw the probe expire at 150s while the pane showed the agent mid-boot).
FRICTION_PROBE_TIMEOUT="${FRICTION_PROBE_TIMEOUT:-300}"

TMUX_SOCKET="officraft"                       # tmux -L <socket> (OC_TMUX_SOCKET)
tmux_session()  { printf 'member-%s' "$1"; }     # OC_SESSION for an agent id

# ── CANONICAL (main-instance) identity — the LIVE FLEET resources ────────────
# These are the resources a live ocwarden/ocagent fleet on this host actually
# uses. The e2e suites must NEVER bootout/kill/rm any of these unless the run is
# EXPLICITLY the canonical instance AND the host is provably free of a live fleet
# (the escape hatch). The default NAMESPACE mode (oc_resolve_instance below)
# derives suffixed siblings of every one of these so a run touches none of them.
OC_CANONICAL_WARDEN_LABEL="com.officraft.ocwarden"
OC_CANONICAL_TMUX_SOCKET="officraft"

# OC_CANONICAL_SERVE_PORT — the CURRENT officraft prod default, read from the
# single source of truth (server/ocserverd/config.go's `defaultPort` const)
# instead of a hand-maintained literal that silently goes stale (T-b76b,
# same pattern as T-a3ba's conformance/run.sh fix): this constant was still
# 8770, a RETIRED former default (config.go's own migration-history comment:
# 8770 → 8780 → 7755, the real current one). A stale value here is not just
# cosmetic — oc_detect_live_canonical_fleet() below uses it to detect "prod
# is running, refuse to touch it"; pointed at the wrong port, that guard
# silently covers NOTHING (a blind guard looks identical to "all clear").
# Computed via this file's own location, not the caller's REPO_ROOT, because
# single_machine_e2e.sh sources this file BEFORE it sources common.sh (which
# sets REPO_ROOT) — deriving from REPO_ROOT here would read an unset var.
_OC_LIFECYCLE_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OC_CANONICAL_SERVE_PORT="$(grep -E '^[[:space:]]*defaultPort[[:space:]]*=[[:space:]]*[0-9]+' \
  "$_OC_LIFECYCLE_REPO_ROOT/server/ocserverd/config.go" 2>/dev/null | grep -oE '[0-9]+' | head -1)"
if [[ -z "$OC_CANONICAL_SERVE_PORT" ]]; then
  echo "[oc_lifecycle] FATAL: could not parse server/ocserverd/config.go's defaultPort — refusing to load without a working canonical-port guard (T-b76b)." >&2
  exit 2
fi
unset _OC_LIFECYCLE_REPO_ROOT

OC_CANONICAL_TUNNEL_PORT=8766
# agent_workdir REMOTE_HOME_EXPR AGENT — the spawn workdir. The first arg is a
# shell expression that expands to $HOME on the target (local or remote). Reads
# OC_NS so a namespace-isolated run resolves ~/.officraft-<ns>/agents/<id> (the
# warden exports OC_AGENT_HOME=<ns-root>/agents on a namespaced spawn) — this is
# LOAD-BEARING for a1_zombie: it kills only the listener anchored to THIS run's
# namespaced workdir, never a canonical live agent that happens to share an id.
agent_workdir() {
  local ns_dash=""; [[ -n "${OC_NS:-}" ]] && ns_dash="-$OC_NS"
  printf '%s/.officraft%s/agents/%s' "$1" "$ns_dash" "$2"
}

# Self-repair keywords used ONLY as a PRE-SCREEN RED FLAG (NOT the final verdict).
# grep matching boot chatter is a cheap first pass — but real friction sometimes
# hides behind phrasing grep misses, and grep also false-positives on an agent
# merely QUOTING these words. So a grep hit RAISES A FLAG (logged, and treated as
# FAIL by the strict SQL pre-screen), but the AUTHORITATIVE zero-self-repair
# verdict is the TWO-TRACK judgment below (Track A dump→LLM, Track B ask-the-agent).
SELF_REPAIR_RE='找不到|stale|exit 127|我來修|patch|死路徑|self-repair'

# ── Track B probe wording (LOAD-BEARING — do not soften) ────────────────────
# EMPIRICAL: asking an agent "did you hit any PROBLEM?" makes it answer OK even
# when its boot was broken (it self-heals then reports success). Asking about
# FRICTION / self-applied WORKAROUNDS is what actually surfaces the FAIL. So the
# probe asks specifically about friction/workaround and pins the reply contract:
#   friction/workaround present → reply must start "FAIL:" + details
#   clean boot                  → reply must be exactly "OK"
FRICTION_PROBE_MSG='開機自檢(請誠實回報,這是回歸測試):你這次 boot / relocate 過程中，有沒有遇到任何 friction，或需要你自己動手做任何 workaround（例如修死路徑、重抓 binary、繞過缺檔、重試才成功）？有→請回「FAIL:」開頭並附上你做了什麼 workaround 的細節；完全順利、什麼都不用繞→請只回「OK」兩個字。'

# ---------------------------------------------------------------------------
# logging + stage bookkeeping
# ---------------------------------------------------------------------------
log()  { echo "[cross-machine] $*" >&2; }
warn() { echo "[cross-machine] WARN: $*" >&2; }
die()  { echo "[cross-machine] FATAL: $*" >&2; exit 1; }

STAGES=()      # human labels, in order
STAGE_RESULT=()  # "PASS" / "FAIL" per stage, index-aligned
CURRENT_STAGE=""

stage() {
  CURRENT_STAGE="$1"
  STAGES+=("$1")
  STAGE_RESULT+=("PENDING")
  log "──────────────────────────────────────────────────────────────"
  log "STAGE: $1"
}

# mark the current stage PASS/FAIL by mutating the last STAGE_RESULT slot.
_mark() { STAGE_RESULT[$(( ${#STAGE_RESULT[@]} - 1 ))]="$1"; }

# fail_stage MSG — mark current stage FAIL, print summary, exit 1 immediately.
# (Every step failing aborts the whole run with a clear error, per spec.)
fail_stage() {
  _mark FAIL
  log "STAGE FAILED: $CURRENT_STAGE — $*"
  summarize
  exit 1
}

pass_stage() { _mark PASS; log "STAGE OK: $CURRENT_STAGE"; }

# summarize — print the per-stage ✓/✗ table + overall verdict.
summarize() {
  echo >&2
  log "================= CROSS-MACHINE E2E SUMMARY ================="
  local i overall="PASS"
  for i in "${!STAGES[@]}"; do
    local r="${STAGE_RESULT[$i]}" mark
    case "$r" in
      PASS)    mark="✓" ;;
      FAIL)    mark="✗"; overall="FAIL" ;;
      *)       mark="•"; overall="FAIL" ;;  # PENDING = never reached = fail
    esac
    printf '[cross-machine]   %s  %s\n' "$mark" "${STAGES[$i]}" >&2
  done
  log "------------------------------------------------------------"
  if [[ "$overall" == "PASS" ]]; then
    log "RESULT: PASS  (public=$PUBLIC_HOST  second=$SECOND_MACHINE  agent=$TEST_AGENT)"
    log "backups: $BACKUP_DIR"
    log "============================================================"
  else
    log "RESULT: FAIL  (see the ✗ stage above; backups: $BACKUP_DIR)"
    log "============================================================"
  fi
}

# ---------------------------------------------------------------------------
# small helpers
# ---------------------------------------------------------------------------
# py — system python3 as a text tool (tomllib on 3.11+).
py() {
  python3 "$@"
}

# json_field JSON KEY — read a top-level string/number field from a JSON blob.
json_field() { py -c 'import sys,json; print(json.load(sys.stdin).get(sys.argv[1],""))' "$1"; }

# api_get PATH — authenticated GET against LOCAL_BASE, prints body.
api_get() {
  curl -fsS --max-time 10 -H "Authorization: Bearer $OWNER_TOKEN" "$LOCAL_BASE$1"
}
# api_post PATH JSON — authenticated POST, prints body.
api_post() {
  curl -fsS --max-time 15 -X POST -H "Authorization: Bearer $OWNER_TOKEN" \
    -H 'content-type: application/json' -d "$2" "$LOCAL_BASE$1"
}
# api_post_logged PATH JSON [ATTEMPTS] [MAXTIME] — api_post that NEVER swallows evidence:
# bounded retry (absorbs transient refused/5xx right after install/bootstrap),
# and on every failed attempt logs the HTTP code + body head so a red stage is
# diagnosable from the log alone. Prints the successful body; rc=1 when all
# attempts fail (a REAL down still fails the stage — retries are bounded).
# MAXTIME (optional, default 15s) is the per-attempt curl --max-time. A SYNCHRONOUS
# long-running endpoint (e.g. bootstrap-here runs `ocwarden install --force` inline
# and the server budgets 60s) MUST pass a MAXTIME above the server's own ceiling —
# else the client cuts at 15s (code 000=timeout) BEFORE the handler returns, and the
# 000-retry loop re-fires the non-idempotent install every attempt. Default 15
# preserves prior behavior for every existing caller (cross_machine.sh unaffected).
api_post_logged() {
  local path="$1" json="$2" attempts="${3:-5}" maxtime="${4:-15}" out code body i
  for i in $(seq 1 "$attempts"); do
    out="$(curl -sS --max-time "$maxtime" -w $'\n%{http_code}' -X POST -H "Authorization: Bearer $OWNER_TOKEN" \
      -H 'content-type: application/json' -d "$json" "$LOCAL_BASE$path" 2>&1)"
    code="${out##*$'\n'}"
    body="${out%$'\n'*}"
    if [[ "$code" =~ ^2[0-9][0-9]$ ]]; then printf '%s' "$body"; return 0; fi
    warn "POST $path attempt $i/$attempts failed (HTTP $code): $(printf '%s' "$body" | head -c 300)"
    # Retry ONLY connection-level failures (code 000: refused / reset / timeout).
    # A 4xx/5xx means the server RECEIVED the request — for a non-idempotent RPC
    # (bootstrap-here, onboard) the work may have PARTIALLY executed, and a blind
    # re-send collides with its own leftovers (RUN#7: bootstrap-here was cut by a
    # serve restart mid-install → 500; the retry then hit the one-warden guard).
    [[ "$code" == "000" ]] || return 1
    sleep 2
  done
  return 1
}

# presence_of MEMBER_ID — echo the presence string for a member from /api/monitoring.
presence_of() {
  api_get /api/monitoring 2>/dev/null | py -c '
import sys, json
mid = sys.argv[1]
data = json.load(sys.stdin)
for s in data.get("sessions", []):
    if s.get("id") == mid:
        print(s.get("presence", "")); break
else:
    print("")
' "$1"
}

# machine_online MACHINE_ID — echo "true"/"false" for a machine from /api/machines.
machine_online() {
  api_get /api/machines 2>/dev/null | py -c '
import sys, json
mid = sys.argv[1]
for m in json.load(sys.stdin):
    if m.get("machine_id") == mid:
        print("true" if m.get("online") else "false"); break
else:
    print("false")
' "$1"
}

# poll_presence MEMBER_ID WANT TIMEOUT — poll until presence==WANT or timeout.
poll_presence() {
  local mid="$1" want="$2" budget="$3" deadline cur
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(presence_of "$mid")"
    [[ "$cur" == "$want" ]] && { log "presence[$mid]=$cur"; return 0; }
    sleep 3
  done
  warn "presence[$mid] never reached '$want' within ${budget}s (last='$cur')"
  return 1
}

# poll_machine_online MACHINE_ID TIMEOUT — poll /api/machines until online.
poll_machine_online() {
  local mid="$1" budget="$2" deadline cur
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(machine_online "$mid")"
    [[ "$cur" == "true" ]] && { log "machine[$mid] online (hub)"; return 0; }
    sleep 3
  done
  warn "machine[$mid] never went online within ${budget}s"
  return 1
}

# scan_self_repair MEMBER_ID — 0 self-repair chat messages from the agent = OK.
# Reads the canonical sqlite DB directly (chat_message table). This is the ONLY
# place we touch sqlite for assertions; presence truth still comes from the hub.
scan_self_repair() {
  local mid="$1"
  # sqlite ships no REGEXP by default, so we pull the agent's chat bodies and match
  # the self-repair pattern in python (portable, no sqlite extension needed).
  py -c '
import sys, sqlite3, re
db, mid, pat = sys.argv[1], sys.argv[2], sys.argv[3]
rx = re.compile(pat)
con = sqlite3.connect(db)
try:
    rows = con.execute("SELECT body FROM chat_message WHERE sender=?", (mid,)).fetchall()
except Exception as exc:
    print(f"ERR:{exc}"); sys.exit(0)
bad = [b for (b,) in rows if b and rx.search(b)]
print(len(bad))
for b in bad[:5]:
    sys.stderr.write("[cross-machine]   self-repair chatter: " + b[:160].replace(chr(10)," ") + "\n")
' "$DB_PATH" "$mid" "$SELF_REPAIR_RE"
}

# prescreen_self_repair MEMBER_ID WHERE — the CHEAP grep RED-FLAG pre-screen.
# Returns 0 = no red flag, 1 = red flag / inconclusive. This is NOT the terminal
# verdict — it only decides whether to shout a warning. The two-track judgment
# below is authoritative.
prescreen_self_repair() {
  local mid="$1" where="$2" n
  n="$(scan_self_repair "$mid")"
  if [[ "$n" == "0" ]]; then
    log "prescreen OK ($where): grep found 0 self-repair keywords from '$mid'"
    return 0
  fi
  if [[ "$n" == ERR:* || -z "$n" ]]; then
    warn "prescreen inconclusive ($where): '$n' — raising red flag (be strict)"
    return 1
  fi
  warn "prescreen RED FLAG ($where): grep matched $n message(s) from '$mid' (see chatter above) — two-track judgment decides"
  return 1
}

# ── TRACK A: manual dump → LLM verdict ──────────────────────────────────────
# Dump the agent's boot-window chat (raw, no keyword filter) to a file for a human
# or an LLM to read and judge "did this boot show friction / self-repair?". This
# is a MANUAL seam by design: this whole script is operator-driven, and the final
# zero-self-repair call for a regression is a judgment, not a regex. We DUMP and
# print the path; the operator (or a piped LLM) renders the A-track verdict.
#   dump_boot_window MEMBER_ID WHERE → prints the dump path.
dump_boot_window() {
  local mid="$1" where="$2" safe out
  safe="$(printf '%s' "$where" | tr -c 'A-Za-z0-9._-' '_')"
  out="$BACKUP_DIR/boot-dump.$mid.$safe.txt"
  {
    printf '# boot-window chat dump — member=%s where=%s\n' "$mid" "$where"
    printf '# TRACK A: read this and judge — any friction / self-repair / workaround? → FAIL\n\n'
    api_get "/api/chat?with=$mid&limit=200" 2>/dev/null | py -c '
import sys, json
try:
    # GET /api/chat answers {"messages": [...], "next_cursor": ...} (T-48).
    msgs = json.load(sys.stdin)["messages"]
except Exception as exc:
    print(f"(could not read chat: {exc})"); sys.exit(0)
for m in msgs:
    who = m.get("from", m.get("from_", "?"))
    print(f"[{who}] {m.get(\"body\",\"\")}")
'
  } > "$out" 2>/dev/null
  log "TRACK A: boot-window dump → $out (read/LLM-judge for friction; grep prescreen already ran)"
  printf '%s' "$out"
}

# ── TRACK B: ask the agent (friction/workaround wording) ────────────────────
# Post the FRICTION_PROBE_MSG to the agent, then poll the agent's replies for an
# "OK" (clean) or a "FAIL:" (friction/workaround admitted). We post AS OWNER (the
# server stamps sender from our JWT sub) addressed `to` the agent; we read back the
# agent's own reply via GET /api/chat?with=<agent>. Wire uses `from`/`body`.
#   probe_agent_friction MEMBER_ID WHERE → 0 = agent said OK, 1 = FAIL/timeout.
probe_agent_friction() {
  local mid="$1" where="$2" since verdict deadline reply
  since="$(date +%s.%N)"
  # Post the probe (owner → agent). Non-fatal if the post itself errors; we degrade
  # to Track A only and say so.
  if ! api_post /api/chat "$(py -c '
import json,sys
print(json.dumps({"to": sys.argv[1], "body": sys.argv[2]}))
' "$mid" "$FRICTION_PROBE_MSG")" >/dev/null 2>&1; then
    warn "TRACK B ($where): could not post friction probe to '$mid' — relying on Track A + prescreen"
    return 2
  fi
  log "TRACK B ($where): friction probe posted to '$mid' — awaiting self-report (OK / FAIL:)"
  # verdict MUST be pre-initialized: with zero replies the loop never assigns it,
  # and under set -u the closing `case "$verdict"` would crash the whole run
  # instead of taking the designed no-reply → strict-FAIL branch.
  verdict=""
  reply=""
  deadline=$(( $(date +%s) + FRICTION_PROBE_TIMEOUT ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    # newest reply FROM the agent AFTER we posted the probe.
    reply="$(api_get "/api/chat?with=$mid&limit=50" 2>/dev/null | py -c '
import sys, json
mid, since = sys.argv[1], float(sys.argv[2])
try:
    msgs = json.load(sys.stdin)["messages"]
except Exception:
    sys.exit(0)
cand = [m for m in msgs
        if m.get("from", m.get("from_")) == mid and float(m.get("ts",0)) >= since]
if cand:
    print(cand[-1].get("body","").strip().replace(chr(10)," "))
' "$mid" "$since")"
    if [[ -n "$reply" ]]; then
      log "TRACK B ($where): agent self-report: ${reply:0:200}"
      if [[ "$reply" == FAIL:* || "$reply" == "FAIL"* ]]; then
        verdict=FAIL; break
      fi
      # Accept a clean "OK" (exact, or leading token) as pass.
      if [[ "$reply" == "OK" || "$reply" == OK[[:space:]]* || "$reply" == OK[.。]* ]]; then
        verdict=OK; break
      fi
      # Any other non-empty reply = ambiguous → keep waiting briefly for a cleaner
      # follow-up, but remember it; treated as FAIL if it's all we get.
      verdict=AMBIGUOUS
    fi
    sleep 3
  done
  case "$verdict" in
    OK)   log "TRACK B ($where): agent reports clean boot (OK)"; return 0 ;;
    FAIL) warn "TRACK B ($where): agent ADMITTED friction/workaround (FAIL) — see self-report above"; return 1 ;;
    *)    warn "TRACK B ($where): no clean OK from '$mid' within ${FRICTION_PROBE_TIMEOUT}s (last='${reply:-<none>}') — strict FAIL"; return 1 ;;
  esac
}

# assert_no_self_repair MEMBER_ID WHERE — TWO-TRACK zero-self-repair judgment.
# grep is a pre-screen red flag only. The verdict combines:
#   Track A: dump boot window for human/LLM judgment (always produced), AND
#   Track B: ask the agent about friction/workaround (authoritative self-report).
# PASS requires: Track B says OK. A grep red flag alone does NOT auto-fail (it may
# be a quote), but a red flag WITH no clean Track B OK is a FAIL. If Track B can't
# run (post failed), we FALL BACK to strict prescreen + the Track-A dump and FAIL
# on any red flag (be strict — a regression must not slip through a dead probe).
assert_no_self_repair() {
  local mid="$1" where="$2" flag_rc probe_rc dump
  prescreen_self_repair "$mid" "$where"; flag_rc=$?
  dump="$(dump_boot_window "$mid" "$where")"
  probe_agent_friction "$mid" "$where"; probe_rc=$?

  if [[ "$probe_rc" -eq 0 ]]; then
    # Agent self-reports clean. If grep ALSO raised a flag, keep it visible but
    # trust the agent's explicit friction/workaround self-report (grep may quote).
    if [[ "$flag_rc" -ne 0 ]]; then
      warn "zero-self-repair ($where): Track B says OK but grep prescreen flagged — REVIEW the dump ($dump); accepting agent's self-report"
    fi
    log "zero-self-repair PASS ($where): agent self-reported OK (Track B); dump=$dump"
    return 0
  fi
  if [[ "$probe_rc" -eq 1 ]]; then
    warn "zero-self-repair FAIL ($where): Track B did not confirm clean (admitted friction OR no reply within budget — see TRACK B line above). dump=$dump"
    return 1
  fi
  # probe_rc==2: Track B unavailable → fall back to strict prescreen + Track-A dump.
  if [[ "$flag_rc" -eq 0 ]]; then
    warn "zero-self-repair ($where): Track B unavailable; grep prescreen clean; JUDGE Track-A dump ($dump). Treating as PASS on clean prescreen — REVIEW dump."
    return 0
  fi
  warn "zero-self-repair FAIL ($where): Track B unavailable AND grep prescreen flagged — strict FAIL. Judge Track-A dump: $dump"
  return 1
}

# ============================================================================
# SERVER BRING-UP (single-machine, canonical route A) — PURE-MOVE from
# single_machine_e2e.sh PHASE 0-3. These are the reusable "bring the server +
# server-self warden up from zero" steps a task-system e2e also needs. Bodies
# are byte-identical to the originals; they read the SAME module-level globals
# the caller already sets (see each function's "reads:" note). They are NOT
# called by cross_machine.sh (which keeps its own inline STAGE 1-3), so adding
# them here does not change cross_machine.sh behavior.
# ============================================================================

# ============================================================================
# INSTANCE RESOLUTION + LIVE-FLEET GUARD (T-8aa1) — the two-layer isolation that
# makes these DESTRUCTIVE suites safe to exist on a LIVE agent-fleet host.
#
#   Layer 1 (construction, root cure): oc_resolve_instance allocates a per-run
#     NAMESPACE by default and derives EVERY host-resource axis off it — serve
#     port, launchd labels, ~/.officraft-<ns> root, tmux -L socket. The whole
#     lifecycle (install/bootstrap/spawn/teardown) then operates ONLY on those
#     namespaced siblings and can never touch the canonical serve port /
#     com.officraft.ocwarden / officraft socket / ~/.officraft. Canonical
#     is reachable ONLY via the explicit OC_E2E_ALLOW_CANONICAL=1 escape hatch.
#
#   Layer 2 (cheap first line): oc_live_fleet_guard read-only detects a LIVE
#     canonical fleet and, when a run nonetheless targets canonical, dies BEFORE
#     any teardown. In namespace mode it is a no-op (coexistence is by design).
# ============================================================================

# oc_mint_namespace — a fresh run-scoped namespace matching the product charset
# [a-z0-9-]{1,16} (cli/ocwarden namespace.go / server config.go). Starts with a
# letter ('e2e') so it is a legal launchd-label component too.
oc_mint_namespace() {
  local raw
  raw="$( (uuidgen 2>/dev/null || date +%s%N) | tr 'A-Z' 'a-z' | tr -cd 'a-z0-9' | cut -c1-9)"
  [[ -n "$raw" ]] || raw="$$"
  printf 'e2e%s' "$raw"
}

# oc_pick_free_port — a free loopback TCP port that is NOT any canonical/known
# harness port (prod = OC_CANONICAL_SERVE_PORT/OC_CANONICAL_TUNNEL_PORT,
# currently 7755/8766, plus RETIRED former prod defaults 8770/8780 which
# nothing in this repo can derive — see OC_CANONICAL_SERVE_PORT's derivation
# above for why a hand-maintained literal here would silently go stale;
# 8790/8791 e2e-playwright; 8795 remains reserved for an explicitly pinned
# conformance reproduction, while normal conformance runs use a kernel-assigned port).
oc_pick_free_port() {
  local p reserved=" $OC_CANONICAL_SERVE_PORT $OC_CANONICAL_TUNNEL_PORT 8770 8780 8790 8791 8795 " _
  for _ in $(seq 1 60); do
    p=$(( 8800 + (RANDOM % 900) ))   # 8800..9699
    case "$reserved" in *" $p "*) continue ;; esac
    lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 && continue
    printf '%s' "$p"; return 0
  done
  die "could not find a free namespaced port in 8800-9699 after 60 tries"
}

# oc_detect_live_canonical_fleet — READ-ONLY. Echo one line per reason a live
# canonical officraft fleet appears present on this host (empty = none). Uses
# only non-mutating probes: launchctl print / lsof / tmux ls. NEVER kills.
oc_detect_live_canonical_fleet() {
  local uid gui reasons=() s
  uid="$(id -u)"; gui="gui/$uid"
  # (1) canonical warden launchd job registered = a live warden.
  if launchctl print "$gui/$OC_CANONICAL_WARDEN_LABEL" >/dev/null 2>&1; then
    reasons+=("live warden launchd job registered: $gui/$OC_CANONICAL_WARDEN_LABEL")
  fi
  # (2) canonical serve port held by a listener we did not start.
  if lsof -nP -iTCP:"$OC_CANONICAL_SERVE_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    reasons+=("canonical serve port $OC_CANONICAL_SERVE_PORT has a live listener")
  fi
  # (3) member-*/worker-* agent sessions on the canonical tmux socket.
  if tmux -L "$OC_CANONICAL_TMUX_SOCKET" ls >/dev/null 2>&1; then
    while IFS= read -r s; do
      case "$s" in
        member-*|worker-*) reasons+=("live agent session on canonical tmux socket '$OC_CANONICAL_TMUX_SOCKET': $s") ;;
      esac
    done < <(tmux -L "$OC_CANONICAL_TMUX_SOCKET" ls -F '#S' 2>/dev/null)
  fi
  # bash 3.2-safe empty-array expansion.
  local r
  for r in ${reasons[@]+"${reasons[@]}"}; do printf '%s\n' "$r"; done
}

# oc_live_fleet_guard — the FIRST, cheapest safety line. Reads OC_NS to know the
# mode (set by oc_resolve_instance; "" = canonical). On a detected live canonical
# fleet: CANONICAL run → die BEFORE any teardown; NAMESPACE run → log + continue
# (the namespaced run touches none of the canonical resources — coexistence is
# the whole point of the isolation).
oc_live_fleet_guard() {
  local reasons; reasons="$(oc_detect_live_canonical_fleet)"
  if [[ -z "$reasons" ]]; then
    log "live-fleet guard OK: no live canonical officraft fleet detected on this host"
    return 0
  fi
  if [[ -z "${OC_NS:-}" ]]; then
    printf '%s\n' "$reasons" | sed 's/^/[oc-lifecycle] live-fleet| /' >&2
    die "LIVE-FLEET GUARD: a LIVE canonical officraft fleet is present on this host (see live-fleet| lines) AND this run targets the CANONICAL instance — refusing (teardown would bootout the real $OC_CANONICAL_WARDEN_LABEL / wipe ~/.officraft / kill live agents). Re-run in the DEFAULT namespace-isolated mode (do NOT set OC_E2E_ALLOW_CANONICAL), or run on a host with no live fleet."
  fi
  log "live-fleet guard: a live canonical fleet IS present, but this run is NAMESPACE-isolated (ns=$OC_NS) — it touches none of the canonical resources; continuing"
  printf '%s\n' "$reasons" | sed 's/^/[oc-lifecycle] live-fleet(coexist)| /' >&2
  return 0
}

# oc_resolve_instance — decide CANONICAL vs NAMESPACE and ALLOCATE every isolated
# resource axis (the construction-enforced isolation). DEFAULT = namespace: on
# ANY host, a fresh run-scoped ns → its own port, labels, ~/.officraft-<ns>
# root, and tmux socket, so the run is isolated BY CONSTRUCTION. CANONICAL is the
# explicit escape hatch (OC_E2E_ALLOW_CANONICAL=1) — still gated by the live-fleet
# guard + hardware/state whitelist in oc_preflight_guards.
#   overrides (globals the suites + oc_fresh_install/oc_teardown_bounded read):
#     OC_NS LOCAL_BASE PUBLIC_HOST SERVE_LABEL AUTODEPLOY_LABEL TUNNEL_LABEL
#     WARDEN_LABEL OC_ROOT SERVER_ROOT OC_TOML DB_PATH TMUX_SOCKET_LOCAL
#     TMUX_SOCKET SINGLE_PROD_PORTS
oc_resolve_instance() {
  local home="${HOME:?HOME must be set}" port
  if [[ "${OC_E2E_ALLOW_CANONICAL:-0}" == "1" ]]; then
    OC_NS=""
    # The port the 0c guard verifies free (SINGLE_PROD_PORTS[0]) and the port this
    # run actually OWNS (LOCAL_BASE/PUBLIC_HOST → oc_fresh_install pins serve to
    # ${LOCAL_BASE##*:}) MUST be the same canonical port. Derive BOTH from the one
    # SSOT (OC_CANONICAL_SERVE_PORT ← config.go defaultPort) on adjacent lines so
    # they can never drift apart again. T-191d: T-b76b moved the guard to the
    # dynamic 7755 but left LOCAL_BASE hardcoded at the retired 8770 → the guard
    # watched 7755 while the install bound 8770 (guard decoupled from what ran).
    SINGLE_PROD_PORTS=("$OC_CANONICAL_SERVE_PORT" "$OC_CANONICAL_TUNNEL_PORT")
    LOCAL_BASE="http://127.0.0.1:$OC_CANONICAL_SERVE_PORT"
    PUBLIC_HOST="127.0.0.1:$OC_CANONICAL_SERVE_PORT"
    log "instance: CANONICAL (OC_E2E_ALLOW_CANONICAL=1 escape hatch) — port $OC_CANONICAL_SERVE_PORT, labels com.officraft.*, root ~/.officraft, socket $OC_CANONICAL_TMUX_SOCKET. Still gated by live-fleet guard + hardware/state whitelist."
    return 0
  fi
  OC_NS="${OC_E2E_NS:-$(oc_mint_namespace)}"
  [[ "$OC_NS" =~ ^[a-z0-9-]{1,16}$ ]] \
    || die "internal: minted/overridden OC_NS='$OC_NS' violates the product charset [a-z0-9-]{1,16}"
  port="${OC_E2E_NS_PORT:-$(oc_pick_free_port)}"
  LOCAL_BASE="http://127.0.0.1:$port"
  PUBLIC_HOST="127.0.0.1:$port"
  SERVE_LABEL="com.officraft.serve.$OC_NS"
  AUTODEPLOY_LABEL="com.officraft.autodeploy.$OC_NS"
  TUNNEL_LABEL="com.officraft.tunnel.$OC_NS"
  WARDEN_LABEL="com.officraft.ocwarden.$OC_NS"
  OC_ROOT="$home/.officraft-$OC_NS"
  SERVER_ROOT="$OC_ROOT/server"
  OC_TOML="$SERVER_ROOT/oc.toml"
  DB_PATH="$SERVER_ROOT/data/officraft.db"
  TMUX_SOCKET_LOCAL="$OC_CANONICAL_TMUX_SOCKET-$OC_NS"   # officraft-<ns>
  TMUX_SOCKET="$TMUX_SOCKET_LOCAL"
  SINGLE_PROD_PORTS=("$port")   # 0c guard verifies OUR port is free (not canonical)
  log "instance: NAMESPACE-isolated ns='$OC_NS' → port=$port label=com.officraft.*.$OC_NS root=$OC_ROOT socket=$TMUX_SOCKET_LOCAL"
  log "  (never touches canonical $OC_CANONICAL_SERVE_PORT / $OC_CANONICAL_WARDEN_LABEL / socket $OC_CANONICAL_TMUX_SOCKET / ~/.officraft)"
}

# oc_preflight_guards — PHASE 0 isolation guards (die on any failure, BEFORE any
# teardown). Triple hardware whitelist + existing-state whitelist + server-root
# containment + prod-port refusal + ambient OC_* strip + claude-resolvability
# detect/warn. Runs the exact guard sequence; the caller wraps it with
# stage/pass_stage.
#   reads: SETH_M1_HW_UUID, SETH_M1_LOCALHOSTNAME, JOEY_INFRA_DIR, OC_ROOT,
#          SERVER_ROOT, SINGLE_PROD_PORTS, HOME_DIR, OC_CLAUDE_BIN(env).
#   mutates env: unsets ambient OC_ID/OC_TOKEN/OC_BASE/OC_SESSION; sets CLAUDE_BIN.
oc_preflight_guards() {
  # 0·. LIVE-FLEET GUARD (T-8aa1) — the FIRST, cheapest line. Dies BEFORE any
  #     teardown if a live canonical fleet is present AND this run targets
  #     canonical; a no-op (log) in the default namespace-isolated mode.
  oc_live_fleet_guard

  # 0a. TRIPLE machine whitelist — CANONICAL MODE ONLY. A namespace-isolated run
  #     (OC_NS set) installs under ~/.officraft-<ns> with suffixed labels/port/
  #     socket and touches nothing canonical, so it is safe on ANY host and the
  #     seth-m1 hardware pin does not apply. In CANONICAL mode the whitelist still
  #     stands: ALL three must pass, else die. Whitelist (not blacklist): a generic
  #     "not seth-m5" would wrongly admit eva-m5 (a live warden-fleet node, not
  #     clean). Primary anchor = immutable hardware UUID.
  if [[ -z "${OC_NS:-}" ]]; then
    HW_UUID="$(ioreg -rd1 -c IOPlatformExpertDevice | awk -F'"' '/IOPlatformUUID/{print $4}')"
    [[ "$HW_UUID" == "$SETH_M1_HW_UUID" ]] \
      || die "MACHINE WHITELIST FAIL (primary anchor): hardware UUID '$HW_UUID' != pinned seth-m1 '$SETH_M1_HW_UUID' — refusing to full-reset a non-whitelisted machine (canonical mode). Drop OC_E2E_ALLOW_CANONICAL to run namespace-isolated on any host."
    LOCAL_HOSTNAME="$(scutil --get LocalHostName 2>/dev/null || echo '')"
    [[ "$LOCAL_HOSTNAME" == "$SETH_M1_LOCALHOSTNAME" ]] \
      || die "MACHINE WHITELIST FAIL: LocalHostName '$LOCAL_HOSTNAME' != pinned '$SETH_M1_LOCALHOSTNAME'."
    [[ -d "$JOEY_INFRA_DIR" ]] \
      || die "MACHINE WHITELIST FAIL: joey fleet-infra dir absent ($JOEY_INFRA_DIR) — not seth-m1."
    log "machine whitelist OK (canonical mode): HW_UUID + LocalHostName + joey-infra all match seth-m1"
  else
    log "machine whitelist SKIPPED: namespace-isolated run (ns=$OC_NS) is host-agnostic (installs under $OC_ROOT, never touches canonical)"
  fi

  # 0b. EXISTING-STATE whitelist — protect ANY pre-existing state from full-reset.
  #     ~/.officraft must NOT exist OR be empty; any content = refuse.
  if [[ -e "$OC_ROOT" ]]; then
    [[ -d "$OC_ROOT" ]] || die "STATE WHITELIST FAIL: $OC_ROOT exists but is not a directory — refusing."
    if [[ -n "$(ls -A "$OC_ROOT" 2>/dev/null)" ]]; then
      die "STATE WHITELIST FAIL: $OC_ROOT is non-empty — a full-reset here could clobber pre-existing state. Refusing (clear it deliberately first, then re-run)."
    fi
    log "state whitelist OK: $OC_ROOT exists but is empty"
  else
    log "state whitelist OK: $OC_ROOT does not exist"
  fi

  # 0b'. SERVER_ROOT CONTAINMENT — OC_SERVER_ROOT is an OVERRIDABLE env; if pointed
  #      outside ~/.officraft, PHASE 1's `rm -rf "$SERVER_ROOT"` would escape the
  #      0b state guard (the ambient strip does NOT cover OC_SERVER_ROOT). Assert it
  #      is strictly under $OC_ROOT and reject any `..` traversal — else die.
  case "$SERVER_ROOT" in
    *..*) die "SERVER_ROOT CONTAINMENT FAIL: '$SERVER_ROOT' contains '..' — refusing (traversal could escape $OC_ROOT)." ;;
  esac
  case "$SERVER_ROOT/" in
    "$OC_ROOT"/*) ;;  # OK: strictly under ~/.officraft
    *) die "SERVER_ROOT CONTAINMENT FAIL: '$SERVER_ROOT' is not under $OC_ROOT (OC_SERVER_ROOT override escapes the state whitelist) — refusing to rm -rf outside the guarded root." ;;
  esac
  log "server-root containment OK: $SERVER_ROOT under $OC_ROOT"

  # 0c. PROD-PORT hard refusal — SINGLE_PROD_PORTS (OC_CANONICAL_SERVE_PORT/
  #     OC_CANONICAL_TUNNEL_PORT, currently 7755/8766) must be free of a
  #     NON-owned listener. This script will OWN OC_CANONICAL_SERVE_PORT
  #     (canonical). Same spirit as common.sh's prod-port refusal: a prod
  #     port already held by something we did not start = refuse.
  for _p in "${SINGLE_PROD_PORTS[@]}"; do
    if lsof -nP -iTCP:"$_p" -sTCP:LISTEN >/dev/null 2>&1; then
      lsof -nP -iTCP:"$_p" -sTCP:LISTEN 2>/dev/null | sed 's/^/[single-machine] port| /' >&2
      die "PROD-PORT REFUSAL: port $_p already has a LISTENER we did not start — refusing to collide (see port| lines). Free it or stop the pre-existing server first."
    fi
  done
  log "port guard OK: ${SINGLE_PROD_PORTS[*]} free of non-owned listeners (this run will own port ${LOCAL_BASE##*:})"

  # 0d. AMBIENT OC_* STRIP — never let ambient OC_ID/OC_TOKEN/OC_BASE/OC_SESSION
  #     redirect auth/telemetry at the fleet/prod server. common.sh oc_env() strips
  #     them for any tool we spawn; also unset them in THIS process for our curls.
  if [[ -n "${OC_ID:-}${OC_TOKEN:-}${OC_BASE:-}${OC_SESSION:-}" ]]; then
    warn "ambient OC_* detected (OC_ID/OC_TOKEN/OC_BASE/OC_SESSION) — stripping so we never hit the fleet server"
  fi
  unset OC_ID OC_TOKEN OC_BASE OC_SESSION 2>/dev/null || true
  log "ambient OC_* stripped (this process + oc_env() for spawned tools)"

  # 0e. CLAUDE RESOLVABILITY — detect + warn only (no symlink; kyle's 2a2296b stamps
  #     OC_CLAUDE_BIN into the warden plist at install time). Confirm claude is
  #     resolvable to the installer so the stamp works; log how the warden will reach
  #     it. Never mutate the host's claude locations.
  CLAUDE_BIN="${OC_CLAUDE_BIN:-$(command -v claude 2>/dev/null || true)}"
  [[ -n "$CLAUDE_BIN" && -x "$CLAUDE_BIN" ]] \
    || die "claude not resolvable (set OC_CLAUDE_BIN or put claude on PATH) — install-time OC_CLAUDE_BIN stamp AND PHASE 4 agent spawn would both fail."
  # Will the warden reach claude WITHOUT the stamp? (resolveClaudeBin #2/#3 = common
  # locations / minimal launchd PATH). If not, PHASE 4 must rely on kyle's stamp (#1).
  _warden_reachable=0
  for _loc in "$HOME_DIR/.local/bin/claude" /opt/homebrew/bin/claude /usr/local/bin/claude; do
    [[ -x "$_loc" ]] && { _warden_reachable=1; break; }
  done
  if [[ "$_warden_reachable" == "1" ]]; then
    log "claude reachable at a resolveClaudeBin common location — warden resolves directly [src=$CLAUDE_BIN]"
  else
    log "WARN: claude ($CLAUDE_BIN) not at a common location / minimal launchd PATH — PHASE 4 relies on kyle's install-time OC_CLAUDE_BIN stamp (2a2296b). This is the asdf/nvm target scenario; the run validates the stamp end-to-end."
  fi
}

# ── IS THIS A PRODUCTION HOST? (T-e1dd) ─────────────────────────────────────
# The question oc_detect_live_canonical_fleet CANNOT answer. That one asks "is a
# fleet RUNNING right now?" — launchd registration, a port listener, live tmux
# sessions. A production machine whose server happens to be STOPPED (mid-deploy,
# just booted out, crashed) answers "nothing here" to all three, and the caller
# proceeds to delete its server root. A stopped server is also precisely when
# someone reaches for a reset script.
#
# TWO guards, deliberately overlapping. Keep both — their failure costs are not
# symmetric, which is the whole argument for the redundancy:
#
#   • IDENTITY (below): "is this machine one of the known production boxes?"
#     Anchored on the immutable hardware UUID. A BLACKLIST, unlike the seth-m1
#     whitelist in oc_preflight_guards 0a — this suite is specified to run on a
#     throwaway VM, so it cannot demand one specific machine. Known weakness of
#     any blacklist: a production host nobody added to the list is NOT covered.
#     That weakness is ACCEPTED, and it is accepted only because the residue
#     guard backs it up. Do not remove either one as "redundant".
#   • RESIDUE: "does this host carry an officraft server tree at all?" Deliberately
#     coarse — the mere existence of ~/.officraft/server is enough. It over-refuses
#     (a throwaway VM that already ran this suite once is refused until rebuilt)
#     and that is the accepted trade: the cost of this guard failing is one rebuilt
#     VM, the cost of the identity guard failing alone is a wiped production
#     database.
#
# ADDRESSING (load-bearing): paths derive from $HOME, never from $SERVER_ROOT /
# $OC_ROOT. Those are env-overridable ($OC_SERVER_ROOT), so deriving from them
# would let the very override this guard exists to catch aim the guard at an empty
# directory while the deletion still lands on the real tree.

# Hardware UUIDs of the machines that RUN A PRODUCTION OFFICRAFT STATION. Same
# anchor the existing suites pin on (ioreg IOPlatformUUID — immutable, survives
# renames, reinstalls and hostname changes; a name or a path prefix is not an
# identity). Append here when a station moves or a new one is stood up.
OC_PROD_HOST_HW_UUIDS=(
  "8523F496-3E4E-5CD3-B94D-54D1EB6BD137"   # seth-m5 — officraft.hardcoretech.link
)

# An ssh non-login shell does not get Homebrew's bin dir, so `tmux` (and `curl`)
# are simply NOT FOUND on an Apple-Silicon Mac unless PATH is exported first —
# cross_machine.sh's own gotcha #2. Every remote command in that script goes
# through its `remote()` wrapper for exactly this reason; the guard's probe must
# not be the one exception, because a tool that cannot be found answers "nothing
# here", which is fail-OPEN for a liveness check. Single source of truth: the
# script's REMOTE_PATH_PREFIX is derived from this.
OC_REMOTE_PATH_PREFIX='export PATH=/opt/homebrew/bin:$PATH;'

# oc_detect_prod_host — read-only. One line per piece of evidence, prefixed with
# which guard found it; empty output = this host looks disposable.
oc_detect_prod_host() {
  local home="${HOME:?HOME must be set}" reasons=() hw u server_root
  server_root="$home/.officraft/server"
  hw="$(ioreg -rd1 -c IOPlatformExpertDevice 2>/dev/null | awk -F'"' '/IOPlatformUUID/{print $4}')"
  if [[ -n "$hw" ]]; then
    for u in ${OC_PROD_HOST_HW_UUIDS[@]+"${OC_PROD_HOST_HW_UUIDS[@]}"}; do
      [[ "$hw" == "$u" ]] && reasons+=("identity: this machine's hardware UUID $hw is a known production station")
    done
  else
    # Say so. Silence here is the identity check going dark while the "guard OK"
    # line below still prints — a guard that stopped working must not look like a
    # guard that passed.
    warn "prod-host guard: could not read a hardware UUID from this host — the IDENTITY check is INACTIVE, only the residue check applies"
  fi
  [[ -d "$server_root" ]] && reasons+=("residue: an officraft server tree exists on this host: $server_root")
  local r
  for r in ${reasons[@]+"${reasons[@]}"}; do printf '%s\n' "$r"; done
}

# oc_detect_prod_host_remote SSH_TARGET — THREE questions about the SECOND
# machine: the two asked locally (identity, residue) plus LIVENESS, which the
# local host gets from oc_live_fleet_guard and this one would otherwise never be
# asked at all. Read-only; prints one reason line per hit.
#
# This is not symmetry for its own sake. cross_machine.sh's STAGE 5b deletes MORE
# on the remote host than the local teardown does locally: `rm -rf "$HOME/.officraft"`
# — the whole instance root, not just the server tree — after booting out the
# remote warden. Guarding only the local host leaves the easier attack: from a
# genuinely clean throwaway VM, `SECOND_MACHINE=<a production station>` passes
# every local gate and wipes that station's entire officraft tree. The local bug
# this ticket fixed at least required standing on production; this one needs only
# its name typed.
#
# Residue signal differs by role, deliberately: the second machine is EXPECTED to
# carry ~/.officraft (that is what being onboarded means, and STAGE 5b clearing it
# is the point), so only a SERVER tree — ~/.officraft/server, the DB-bearing part
# a warden node never has — counts as "this is a station, not a test node".
# NOTE — this function must NEVER call `die`. It is invoked inside a command
# substitution, where `exit` ends only the subshell: the caller would print the
# fatal message and then carry straight on into the teardown. That is not a
# hypothetical — it is how the first version of this guard behaved, and the
# unreachable-host case recorded 11 mutations after "refusing". Detectors report;
# only oc_prod_host_remote_guard decides.
oc_detect_prod_host_remote() {
  local target="$1" reasons=() hw u probe
  # One round trip, three facts. `ioreg` may be absent (non-mac) — that is handled
  # below, not here. ServerAlive* bounds a session that stalls AFTER connecting;
  # ConnectTimeout alone only bounds the TCP connect.
  #
  # stderr is folded into stdout rather than spilled to a temp file: this runs
  # BEFORE the guard has decided anything, and the whole point of the ordering
  # assertions is that nothing — not even a scratch file the detector creates and
  # cleans up — is written before a possible refusal. On failure the merged text
  # IS ssh's diagnosis. On success, ordinary stderr noise (the very common
  # "Warning: Permanently added … to the list of known hosts") is simply tolerated:
  # it matches no marker, so the exactly-once counts below are unaffected. Only
  # MARKER-SHAPED output — from either stream — can disturb the parse, and that
  # disturbs it into refusing.
  if ! probe="$(ssh -o BatchMode=yes -o ConnectTimeout=10 \
      -o ServerAliveInterval=5 -o ServerAliveCountMax=3 "$target" \
    "$OC_REMOTE_PATH_PREFIX"' printf "hw=%s\n" "$(ioreg -rd1 -c IOPlatformExpertDevice 2>/dev/null | awk -F\" "/IOPlatformUUID/{print \$4}")"; [ -d "$HOME/.officraft/server" ] && printf "server_tree=1\n" || printf "server_tree=0\n"; if ! command -v launchctl >/dev/null 2>&1; then printf "live_warden=err\n"; elif launchctl print "gui/$(id -u)/com.officraft.ocwarden" >/dev/null 2>&1; then printf "live_warden=1\n"; else printf "live_warden=0\n"; fi; if ! command -v tmux >/dev/null 2>&1; then printf "live_agents=err\n"; elif tmux -L officraft ls -F "#S" 2>/dev/null | grep -qE "^(member|worker)-"; then printf "live_agents=1\n"; else printf "live_agents=0\n"; fi' 2>&1)"; then
    # Unreachable is a REFUSAL, not a warning. A host whose identity cannot be
    # established is not thereby safe to wipe. Carry ssh's own diagnosis: "fix ssh
    # access" is useless next to "Permission denied (publickey)".
    printf '%s\n' "unreachable: could not reach '$target' over ssh to establish what machine it is — ssh said: $(printf '%s' "$probe" | tr '\n' ' ' | sed 's/  */ /g;s/ *$//')"
    return 0
  fi

  # PARSE FAIL-CLOSED. An ssh that exits 0 with no usable output is not a clean
  # host — it is a probe that did not run (ForceCommand, a restricted shell, an rc
  # file that returns early). Reading that as "nothing found" would reproduce, in
  # the new guard, exactly what this ticket is about: a guard that stopped working
  # looking identical to a guard that passed. Each marker is matched WHOLE-LINE and
  # must appear exactly once, so a stray `server_tree=0` echoed by a remote rc file
  # cannot dilute the real answer either.
  # EVERY marker, including hw=. Counting two out of three left the third one
  # dilutable: a stray `hw=` line from a remote rc file arrives BEFORE the probe's
  # own output, so `head -1` would take the wrong value — non-empty, matching no
  # blacklist entry, and suppressing the "identity check has gone dark" warning
  # because the value was not empty. One rule, all three markers.
  local n_hw n_tree n_live n_agents
  n_hw="$(printf '%s\n' "$probe" | grep -cE '^hw=' || true)"
  n_tree="$(printf '%s\n' "$probe" | grep -cx 'server_tree=[01]' || true)"
  n_live="$(printf '%s\n' "$probe" | grep -cx 'live_warden=[01]' || true)"
  n_agents="$(printf '%s\n' "$probe" | grep -cx 'live_agents=[01]' || true)"
  if [[ "$n_hw" != "1" || "$n_tree" != "1" || "$n_live" != "1" || "$n_agents" != "1" ]]; then
    printf '%s\n' "unparseable: the probe of '$target' did not come back with exactly one answer per question (hw: $n_hw, server_tree: $n_tree, live_warden: $n_live, live_agents: $n_agents) — cannot establish what that host is or carries"
    return 0
  fi

  hw="$(printf '%s\n' "$probe" | sed -n 's/^hw=//p')"
  if [[ -n "$hw" ]]; then
    for u in ${OC_PROD_HOST_HW_UUIDS[@]+"${OC_PROD_HOST_HW_UUIDS[@]}"}; do
      [[ "$hw" == "$u" ]] && reasons+=("identity: the second machine '$target' has hardware UUID $hw, a known production station")
    done
  else
    warn "prod-host guard: could not read a hardware UUID from '$target' — the remote IDENTITY check is INACTIVE, only the remote residue and liveness checks apply"
  fi
  printf '%s\n' "$probe" | grep -qx 'server_tree=1' \
    && reasons+=("residue: the second machine '$target' carries an officraft SERVER tree (~/.officraft/server) — a relocate target never has one")
  # The liveness half the local host gets from oc_live_fleet_guard. Without it a
  # live fleet NODE — someone's laptop with a registered warden, live agents and
  # their tokens, but no server tree — passes both questions above and STAGE 5b
  # deletes its entire ~/.officraft. Same blast radius the local live-fleet guard
  # exists to prevent, one host over.
  printf '%s\n' "$probe" | grep -qx 'live_warden=1' \
    && reasons+=("liveness: the second machine '$target' has a REGISTERED officraft warden — it is a live fleet node, not a disposable relocate target")
  # Agents can outlive their warden: booted out for maintenance, crashed, launchd
  # gave up after repeated failures, or started by hand. STAGE 5b sweeps and
  # kill-sessions remote member-*/worker-* sessions explicitly, so a host with live
  # agents and no registered warden would have those agents killed by a guard that
  # looked complete.
  printf '%s\n' "$probe" | grep -qx 'live_agents=1' \
    && reasons+=("liveness: the second machine '$target' has live member-*/worker-* sessions on tmux socket 'officraft' — agents are running there right now")
  local r
  for r in ${reasons[@]+"${reasons[@]}"}; do printf '%s\n' "$r"; done
}

# oc_prod_host_remote_guard SSH_TARGET — refuse a second machine that is, or looks
# like, a production station. Same no-override rule as the local guard.
oc_prod_host_remote_guard() {
  local target="$1" reasons; reasons="$(oc_detect_prod_host_remote "$target")"
  if [[ -z "$reasons" ]]; then
    log "prod-host guard OK (remote): '$target' answered all four questions — not a known production station, no server tree, no registered warden, no live agent sessions"
    return 0
  fi
  printf '%s\n' "$reasons" | sed 's/^/[oc-lifecycle] prod-host(remote)| /' >&2
  # BRANCH ORDER IS THE MESSAGE'S SAFETY. Same rule as the local guard: identity
  # wins, because only the identity branch may not name a way forward. Ordered the
  # other way round, a machine we know by hardware UUID to be a production station
  # would be handed "retire that install and retry" merely because it also happens
  # to be running its warden — the instruction that IS the disaster.
  if printf '%s\n' "$reasons" | grep -qE '^(unreachable|unparseable):'; then
    die "PROD-HOST GUARD (remote): could not establish what machine '$target' is (see prod-host(remote)| lines for what ssh reported). This run boots out its warden and deletes its entire ~/.officraft, so it must not start while that host's identity is unknown — a probe that did not run is not the same answer as a host that came back clean. Fix ssh access to it, or point SECOND_MACHINE at the host you actually mean."
  fi
  if printf '%s\n' "$reasons" | grep -q '^identity:'; then
    die "PROD-HOST GUARD (remote): the second machine '$target' is a known production officraft station (see prod-host(remote)| lines). STAGE 5b boots out its warden and deletes its ENTIRE ~/.officraft — more than this suite deletes locally. Point SECOND_MACHINE at a disposable relocate target instead."
  fi
  if printf '%s\n' "$reasons" | grep -q '^residue:'; then
    die "PROD-HOST GUARD (remote): the second machine '$target' carries an officraft server tree (see prod-host(remote)| lines) — a relocate target never has one, so this host is, or looks like, a station. STAGE 5b boots out its warden and deletes its ENTIRE ~/.officraft. Point SECOND_MACHINE at a disposable relocate target instead."
  fi
  if printf '%s\n' "$reasons" | grep -q '^liveness:'; then
    # LAST on purpose. It is the only remote branch that names a remedy, so it may
    # only be reached once identity AND residue have both come back clean —
    # otherwise the more incriminating evidence set would get the more permissive
    # message. That matters most for the case residue exists to catch: an UNLISTED
    # production server has a server tree and a running warden essentially by
    # definition, and it is precisely where the blacklist's known weakness leaves
    # residue as the last line.
    die "PROD-HOST GUARD (remote): the second machine '$target' is running officraft right now (a registered warden and/or live agent sessions), so it is a live fleet node — STAGE 5b would boot out that warden, kill those sessions and delete its entire ~/.officraft, taking live agents and their credentials with it. Point SECOND_MACHINE at a quiet, disposable relocate target. ONLY IF YOU ARE CERTAIN this is leftover from your own previous run and nothing else is using that host: ssh there and retire it yourself — \`launchctl bootout gui/\$(id -u)/com.officraft.ocwarden\`, end any member-*/worker-* sessions on tmux socket 'officraft', then clear ~/.officraft. If you are not certain, you are pointing at the wrong host."
  fi
  # Unreachable in practice — the detector emits only the five kinds branched on
  # above and `reasons` is non-empty here by construction — but a new reason kind
  # must refuse rather than fall out of the function having printed nothing.
  die "PROD-HOST GUARD (remote): '$target' was refused for a reason this guard does not have a specific message for (see prod-host(remote)| lines)."
}

# oc_prod_host_guard — refuse a host that is, or looks like, a production station.
# There is deliberately NO env override on either check: a flag nobody sets
# correctly is indistinguishable from a guard that was never there, which is the
# failure this whole guard exists to prevent. The two refusals carry DIFFERENT
# ways forward, and that is why they are separate messages — the residue case has
# a legitimate next step (clear the tree, or rebuild the VM), and the identity
# case must never suggest one, because on a production station that instruction
# IS the disaster.
oc_prod_host_guard() {
  local reasons; reasons="$(oc_detect_prod_host)"
  if [[ -z "$reasons" ]]; then
    log "prod-host guard OK: not a known production station, and no officraft server tree on this host"
    return 0
  fi
  printf '%s\n' "$reasons" | sed 's/^/[oc-lifecycle] prod-host| /' >&2
  if printf '%s\n' "$reasons" | grep -q '^identity:'; then
    die "PROD-HOST GUARD (identity): this machine is a known production officraft station (see prod-host| lines). This suite deletes the server root, its database and the agent workdirs of whatever host it runs on. Run it on a dedicated throwaway VM instead. Whether the server is currently running makes no difference to what the deletion costs."
  fi
  die "PROD-HOST GUARD (residue): this host already carries an officraft server tree (see prod-host| lines), so it is not a clean throwaway host. THIS IS EXPECTED IF YOU ALREADY RAN THIS SUITE HERE: the run installs a server, and from then on the host looks indistinguishable from a production one to this guard — which is intended, because telling those two apart from the outside is exactly what this guard cannot do safely. To run again: rebuild the throwaway VM, or, ONLY IF YOU ARE CERTAIN this tree is leftover from your own previous run and nothing else, delete \$HOME/.officraft/server yourself first. If you are not certain, you are on the wrong host."
}

# oc_cross_machine_preflight — the SINGLE gate cross_machine.sh passes before ANY
# destructive action. It exists as a function (rather than top-level script code)
# so tests_guard can execute it against shimmed evidence and assert that a refusal
# happens with ZERO mutations — top-level code could only ever be tested by running
# the destructive script itself.
#
# Ordering is the substance of T-e1dd, not a detail: the isolation ack used to sit
# 141 lines AFTER the `rm -rf "$SERVER_ROOT"` it reads as protection, and it was
# never about that deletion at all (it guards the STAGE 3 warden naming collision).
# The documented invocation acked only the destructiveness flag, so the documented
# way to run this script wiped the server root and was refused afterwards.
#
# Deliberately NOT reused: oc_preflight_guards(). Three of its clauses transplant
# cleanly and are inlined below; two cannot and are excluded on purpose —
#   • 0a TRIPLE MACHINE WHITELIST pins the run to seth-m1 via hardware UUID. This
#     suite is specified to run on a throwaway VM, and the whitelist's third
#     anchor requires a vibe-clicking fleet to be PRESENT — the exact opposite of
#     this script's stated precondition. (The pinned variables are also undefined
#     here, so under `set -u` it would abort with `unbound variable` rather than a
#     guard message.)
#   • 0b EXISTING-STATE WHITELIST demands ~/.officraft be absent or empty. This
#     suite's STAGE 3 necessarily creates ~/.officraft/warden, so importing 0b
#     would refuse every run after the first. The prod-host guard above covers the
#     danger 0b was aimed at, addressed from $HOME instead of the overridable root.
#   reads: OC_CROSS_MACHINE_YES, REQUIRE_ISOLATION_CONFIRMED, OC_ROOT, SERVER_ROOT,
#          SECOND_MACHINE, HOME_DIR, OCSERVER, OC_CLAUDE_BIN(env).
#   mutates env: unsets ambient OC_ID/OC_TOKEN/OC_BASE/OC_SESSION.
oc_cross_machine_preflight() {
  # (a) DESTRUCTIVENESS ack.
  [[ "${OC_CROSS_MACHINE_YES:-}" == "1" ]] || die \
    "refusing: this is a DESTRUCTIVE full-reset E2E (wipes the local server + agent, needs a real second machine). Re-run with OC_CROSS_MACHINE_YES=1 to acknowledge."

  # (b) ISOLATION ack — MOVED HERE (T-e1dd). It reads as "the operator has
  #     established isolation", and every destructive action below depends on
  #     that being true, so it has to be answered before the first one, not
  #     before STAGE 3's warden bootstrap.
  [[ "${REQUIRE_ISOLATION_CONFIRMED:-0}" == "1" ]] || die \
    "refusing: REQUIRE_ISOLATION_CONFIRMED is not set. This run tears down and reinstalls the CANONICAL officraft instance on this host — the same launchd labels, the same ~/.officraft root and the same tmux socket a real fleet would use — so it is only safe on a host isolated for it. Set REQUIRE_ISOLATION_CONFIRMED=1 only to state that the isolation is real (dedicated throwaway VM/host); it does not create any."

  # (c) required tooling.
  local tool
  for tool in curl ssh sqlite3 uuidgen launchctl; do
    command -v "$tool" >/dev/null 2>&1 || die "required tool missing on PATH: $tool"
  done
  [[ -x "${OCSERVER:?OCSERVER must be set}" ]] || die "bin/ocserver not found/executable at $OCSERVER"

  # (d) LIVE-FLEET GUARD — is a fleet RUNNING here?
  oc_live_fleet_guard

  # (e) PROD-HOST GUARD — is THIS machine a production station? (the stopped-prod
  #     case (d) structurally cannot see; see oc_detect_prod_host).
  oc_prod_host_guard

  # (f) …and the same question about the SECOND machine, asked HERE rather than at
  #     STAGE 5b where the remote wipe happens. A refusal at this point costs
  #     nothing; a refusal at STAGE 5b would arrive after the local host has
  #     already been torn down and reinstalled — the same "destroy first, refuse
  #     second" shape this whole change exists to remove, just one host over.
  oc_prod_host_remote_guard "${SECOND_MACHINE:?SECOND_MACHINE must be set}"

  # (g) SERVER_ROOT CONTAINMENT — transplanted verbatim from oc_preflight_guards
  #     0b'. OC_SERVER_ROOT is an overridable env and the teardown rm -rf's it; if
  #     it is pointed outside the instance root, the deletion escapes every guard
  #     that reasons about that root.
  case "$SERVER_ROOT" in
    *..*) die "SERVER_ROOT CONTAINMENT FAIL: '$SERVER_ROOT' contains '..' — refusing (traversal could escape $OC_ROOT)." ;;
  esac
  case "$SERVER_ROOT/" in
    "$OC_ROOT"/*) ;;
    *) die "SERVER_ROOT CONTAINMENT FAIL: '$SERVER_ROOT' is not under $OC_ROOT (OC_SERVER_ROOT override escapes the guarded root) — refusing to rm -rf outside it." ;;
  esac
  log "server-root containment OK: $SERVER_ROOT under $OC_ROOT"

  # (h) AMBIENT OC_* STRIP — transplanted from 0d. Never let an ambient token or
  #     base URL point this run's API calls at the fleet/prod server.
  if [[ -n "${OC_ID:-}${OC_TOKEN:-}${OC_BASE:-}${OC_SESSION:-}" ]]; then
    warn "ambient OC_* detected (OC_ID/OC_TOKEN/OC_BASE/OC_SESSION) — stripping so we never hit the fleet server"
  fi
  unset OC_ID OC_TOKEN OC_BASE OC_SESSION 2>/dev/null || true
  log "ambient OC_* stripped (this process)"

  # (i) CLAUDE RESOLVABILITY — transplanted from 0e. STAGE 4 spawns a real agent,
  #     so an unresolvable claude fails this run either way; failing here costs a
  #     second instead of four stages. Deliberately NOT exported as CLAUDE_BIN:
  #     nothing downstream in this suite reads such a variable, and leaving one
  #     around would imply a plumbing that does not exist.
  local claude_bin="${OC_CLAUDE_BIN:-$(command -v claude 2>/dev/null || true)}"
  [[ -n "$claude_bin" && -x "$claude_bin" ]] \
    || die "claude not resolvable (set OC_CLAUDE_BIN or put claude on PATH) — the install-time OC_CLAUDE_BIN stamp AND the STAGE 4 agent spawn would both fail."
  log "claude resolvable [src=$claude_bin]"
}

# oc_assert_teardown_instance — fail closed unless every resource axis agrees
# with the selected instance.  This is deliberately checked BEFORE the first
# teardown mutation: a namespaced E2E must never silently fall back to the
# canonical warden label/root/socket when one derived variable is missing or
# stale.
oc_assert_teardown_instance() {
  local ns="${OC_NS:-}" expected_root expected_suffix
  if [[ -n "$ns" ]]; then
    expected_root="$HOME_DIR/.officraft-$ns"
    expected_suffix=".$ns"
    [[ "${OC_ROOT:-}" == "$expected_root" ]] \
      || die "TEARDOWN TARGET GUARD: namespace '$ns' requires OC_ROOT '$expected_root' (got '${OC_ROOT:-<unset>}')"
    [[ "${SERVE_LABEL:-}" == "com.officraft.serve$expected_suffix" ]] \
      || die "TEARDOWN TARGET GUARD: namespace '$ns' requires namespaced serve label (got '${SERVE_LABEL:-<unset>}')"
    [[ "${WARDEN_LABEL:-}" == "com.officraft.ocwarden$expected_suffix" ]] \
      || die "TEARDOWN TARGET GUARD: namespace '$ns' requires namespaced warden label (got '${WARDEN_LABEL:-<unset>}')"
    [[ "${TMUX_SOCKET_LOCAL:-}" == "$OC_CANONICAL_TMUX_SOCKET-$ns" ]] \
      || die "TEARDOWN TARGET GUARD: namespace '$ns' requires namespaced tmux socket (got '${TMUX_SOCKET_LOCAL:-<unset>}')"
    return 0
  fi

  # Canonical is an explicit mode too: refuse a mixed set of canonical and
  # namespaced axes rather than guessing which target the caller meant.
  [[ "${OC_ROOT:-}" == "$HOME_DIR/.officraft" ]] \
    || die "TEARDOWN TARGET GUARD: canonical mode requires OC_ROOT '$HOME_DIR/.officraft' (got '${OC_ROOT:-<unset>}')"
  [[ "${SERVE_LABEL:-}" == "com.officraft.serve" ]] \
    || die "TEARDOWN TARGET GUARD: canonical mode requires canonical serve label (got '${SERVE_LABEL:-<unset>}')"
  [[ "${WARDEN_LABEL:-}" == "$OC_CANONICAL_WARDEN_LABEL" ]] \
    || die "TEARDOWN TARGET GUARD: canonical mode requires canonical warden label (got '${WARDEN_LABEL:-<unset>}')"
  [[ "${TMUX_SOCKET_LOCAL:-}" == "$OC_CANONICAL_TMUX_SOCKET" ]] \
    || die "TEARDOWN TARGET GUARD: canonical mode requires canonical tmux socket (got '${TMUX_SOCKET_LOCAL:-<unset>}')"
}

# oc_teardown_warden — remove precisely the selected instance's warden.  Do
# not rely on the caller's ambient environment: `ocwarden teardown` defaults
# to canonical for an absent OC_NAMESPACE, so a namespaced harness MUST pass
# its namespace on this exact subprocess invocation.
oc_teardown_warden() {
  oc_assert_teardown_instance
  if [[ -x "$OCWARDEN" ]]; then
    log "bin/ocwarden teardown (instance=${OC_NS:-canonical}, explicit OC_NAMESPACE)"
    local -a args=(teardown)
    [[ -z "${OC_NS:-}" ]] && args+=(--canonical)
    env OC_NAMESPACE="${OC_NS:-}" "$OCWARDEN" "${args[@]}" 2>&1 | sed 's/^/[single-machine] warden-td| /' >&2 \
      || warn "ocwarden teardown returned non-zero (may be already-gone) — continuing"
  fi
}

# oc_teardown_bounded WHERE — the ONLY teardown path (PHASE 1 / PHASE 6). EXACT
# labels only; NEVER pkill/killall/glob; .dump backup before any destruction;
# poll-until-gone. Calls fail_stage on a stuck label / surviving root.
#   reads: DB_PATH, BACKUP_DIR, GUI, SERVE_LABEL, AUTODEPLOY_LABEL, TUNNEL_LABEL,
#          WARDEN_LABEL, OCWARDEN, HOME_DIR, OC_ROOT, SERVER_ROOT, TMUX_SOCKET_LOCAL.
oc_teardown_bounded() {
  local where="$1"
  # OC_ROOT is the (possibly namespaced) instance root — ~/.officraft for the
  # canonical instance, ~/.officraft-<ns> for a namespace-isolated run. EVERY
  # destructive path below is derived from it so a namespaced teardown can NEVER
  # touch the canonical ~/.officraft tree. Fallback preserves old behavior for
  # any caller that predates oc_resolve_instance.
  local oc_root="${OC_ROOT:-$HOME_DIR/.officraft}"

  # Fail before backing up, booting out, or deleting anything if the instance
  # axes no longer identify one unambiguous target.
  oc_assert_teardown_instance

  # 1a. .dump backup BEFORE any destruction. Best-effort if DB absent.
  if [[ -f "$DB_PATH" ]]; then
    if sqlite3 "$DB_PATH" ".dump" > "$BACKUP_DIR/officraft.$where.dump.sql" 2>/dev/null; then
      log "backed up DB → $BACKUP_DIR/officraft.$where.dump.sql ($(wc -l < "$BACKUP_DIR/officraft.$where.dump.sql") lines)"
    else
      warn "sqlite3 .dump failed (DB may be locked/corrupt) — continuing (backup best-effort)"
    fi
  else
    log "no existing DB at $DB_PATH — nothing to back up"
  fi

  # 1b. bootout the server + warden jobs by EXACT label ONLY. NEVER pkill/killall
  #     — this box also runs other fleet jobs a broad kill would murder.
  local label
  for label in "$SERVE_LABEL" "$AUTODEPLOY_LABEL" "$TUNNEL_LABEL" "$WARDEN_LABEL"; do
    log "launchctl bootout $GUI/$label (EXACT label; tolerate not-loaded)"
    launchctl bootout "$GUI/$label" 2>/dev/null || true
  done

  # 1c. best-effort selected-instance warden teardown (removes only that
  # instance's plist/tokfile; namespace propagation is load-bearing).
  oc_teardown_warden

  # 1d. EXACT stale-agent sessions on the dedicated `officraft` tmux socket
  #     ONLY (member-<id> persistent agents + worker-<ow-id> ephemeral outsource
  #     workers); other sockets (fleet) are never touched. A surviving member-/
  #     worker- session holds a token signed by the wiped server and squats the
  #     session name, so the fresh warden's clobber-guard would refuse the spawn.
  #     (worker-* matters for task_system_e2e; single/cross spawn no workers, so
  #     the extra prefix is a harmless no-op there.)
  if tmux -L "$TMUX_SOCKET_LOCAL" ls >/dev/null 2>&1; then
    while IFS= read -r sess; do
      case "$sess" in member-*|worker-*) ;; *) continue ;; esac
      log "tmux -L $TMUX_SOCKET_LOCAL kill-session -t =$sess (EXACT stale agent/worker session)"
      tmux -L "$TMUX_SOCKET_LOCAL" kill-session -t "=$sess" 2>/dev/null || true
    done < <(tmux -L "$TMUX_SOCKET_LOCAL" ls -F '#S' 2>/dev/null)
  fi

  # 1e. remove the server root + stale agent workdirs + stale exec-warden tokfile.
  #     ONLY the officraft server/agent trees — NEVER ~/.vibe-clicking.
  if [[ -d "$oc_root/agents" ]]; then
    tar -czf "$BACKUP_DIR/agents-workdirs.$where.tgz" -C "$oc_root" agents 2>/dev/null \
      || warn "agents/ backup tar failed — continuing (workdirs are disposable caches)"
    log "rm -rf $oc_root/agents (stale agent workdirs/creds — backed up)"
    rm -rf "$oc_root/agents"
  fi
  log "rm -rf $SERVER_ROOT (server checkout/db/config/logs — backed up above)"
  rm -rf "$SERVER_ROOT"
  rm -f "$oc_root/exec-warden.tok" 2>/dev/null || true
  # bootstrap-here installs the warden into $oc_root/warden (ocwarden + ocagent
  # binaries + log/). `ocwarden teardown` above removes only its tokfile +
  # launchd plist, LEAVING that dir — so without this the post-run root stays
  # non-empty and the NEXT run's PHASE 0 state whitelist (0b) would refuse
  # (script becomes non-repeatable). Remove the whole warden tree — EXACT path
  # under $oc_root, never a glob.
  log "rm -rf $oc_root/warden (warden install tree — bootstrap-here leftover; ocwarden teardown leaves it)"
  rm -rf "$oc_root/warden"
  # Outsource-worker workdirs (task_system_e2e): the scheduler creates
  # $oc_root/workers/<ow-id> per ephemeral worker; a crash mid-run leaves them,
  # so the NEXT PHASE 0 state whitelist (0b) would refuse (non-empty root).
  # EXACT path under $oc_root, never a glob. No-op for single/cross (no workers).
  log "rm -rf $oc_root/workers (ephemeral outsource-worker workdirs — task_system leftover)"
  rm -rf "$oc_root/workers"
  # NAMESPACE mode: the ENTIRE ~/.officraft-<ns> root is disposable and unique
  # to this run — remove it wholesale so no residue survives (leaves the canonical
  # ~/.officraft untouched). EXACT namespaced path, guarded on OC_NS being set
  # so this can NEVER rm the canonical root.
  if [[ -n "${OC_NS:-}" && "$oc_root" == "$HOME_DIR/.officraft-$OC_NS" ]]; then
    log "rm -rf $oc_root (namespace-isolated instance root — disposable, unique to ns=$OC_NS)"
    rm -rf "$oc_root"
  fi
  # Server launchd plist FILES (serve/autodeploy/tunnel): bootout unloads the jobs
  # but leaves the .plist files in ~/Library/LaunchAgents. Remove the EXACT files
  # (NOT a glob — never `rm com.officraft.*`) so LaunchAgents is clean; ocwarden
  # teardown already removes its own plist.
  local _lbl
  for _lbl in "$SERVE_LABEL" "$AUTODEPLOY_LABEL" "$TUNNEL_LABEL"; do
    rm -f "$HOME_DIR/Library/LaunchAgents/$_lbl.plist" 2>/dev/null || true
  done

  # verify the labels are truly gone. bootout is ASYNCHRONOUS — poll-until-gone
  # (bounded) rather than a single immediate check.
  for label in "$SERVE_LABEL" "$WARDEN_LABEL"; do
    local gone=0 _
    for _ in $(seq 1 20); do
      if ! launchctl print "$GUI/$label" >/dev/null 2>&1; then gone=1; break; fi
      sleep 1
    done
    if [[ "$gone" != 1 ]]; then
      fail_stage "$label still registered 20s after bootout — refusing to proceed on a dirty box"
    fi
  done
  [[ -d "$SERVER_ROOT" ]] && fail_stage "server root still present after rm: $SERVER_ROOT"
  return 0
}

# oc_fresh_install — PHASE 2 fresh install (canonical serve port): seed KNOWN owner
# password (render-config + set-password seam), run `ocserver install --force`
# under oc_env, /health + /api/version sanity, owner login → OWNER_TOKEN, then
# the 15s serve stability window. Calls fail_stage on any failure.
#   reads: SERVER_ROOT, DB_PATH, OCSERVER, REPO_ROOT, OC_TOML, LOCAL_BASE,
#          OWNER_PASSWORD.
#   sets: OWNER_TOKEN (used by all api_* helpers), GIT_SHA.
oc_fresh_install() {
  # 2a. PRE-SEED the KNOWN OWNER_PASSWORD via the render-config + set-password seam.
  #     `ocserver install` NEVER clobbers a pre-existing oc.toml, so the installer's
  #     own namespace/port injection does NOT run for our seeded file — we inject
  #     BOTH the port pin AND (in namespace mode) the [server].namespace line here,
  #     matching render_oc_toml exactly. Without the namespace line the server would
  #     boot as the MAIN instance and bootstrap-here would install a CANONICAL
  #     warden — defeating the isolation.
  seed_owner_password() {
    mkdir -p "$SERVER_ROOT/data"
    local dsn="sqlite:///$DB_PATH"
    "$OCSERVER" render-config "$REPO_ROOT/oc.toml.example" "$OC_TOML" "$dsn" \
      || die "ocserver render-config failed — cannot seed the e2e oc.toml"
    # Pin serve port to LOCAL_BASE's port + inject namespace (ns mode) into [server].
    local port="${LOCAL_BASE##*:}"
    OC_TOML="$OC_TOML" OC_PIN_PORT="$port" OC_PIN_NS="${OC_NS:-}" py -c '
import os, re
p   = os.environ["OC_TOML"]
port= os.environ.get("OC_PIN_PORT", "")
ns  = os.environ.get("OC_PIN_NS", "")
txt = open(p, encoding="utf-8").read()
if re.match(r"^[0-9]+$", port):
    txt = re.sub(r"(?m)^port\s*=\s*\d+", f"port = {port}", txt, count=1)
if ns:
    if re.search(r"(?m)^namespace\s*=", txt):
        txt = re.sub(r"(?m)^namespace\s*=.*$", f"namespace = \"{ns}\"", txt, count=1)
    else:  # add right under the (pinned) port line, inside the ONE [server] table
        txt = re.sub(r"(?m)^(port\s*=\s*\d+)$", r"\1" + f"\nnamespace = \"{ns}\"", txt, count=1)
open(p, "w", encoding="utf-8").write(txt)
'
    OC_CONFIG="$OC_TOML" OC_NEW_PASSWORD="$OWNER_PASSWORD" "$REPO_ROOT/bin/ocserverd" set-password >/dev/null \
      || die "ocserverd set-password failed — cannot seed a known owner password"
    log "seeded oc.toml (port=${port} ns='${OC_NS:-<canonical>}') + known OWNER_PASSWORD hash → DB ($DB_PATH)"
  }
  seed_owner_password

  # 2b. run the installer. It sees our pre-seeded oc.toml and KEEPS it; builds,
  #     migrates, loads launchd. --force re-runs every step. oc_env() strips ambient
  #     OC_*. In NAMESPACE mode pass --namespace/--port so the launchd labels + root
  #     it derives match our seeded oc.toml (namespaced install = serve ONLY: no
  #     autodeploy/tunnel — the stability window below simply passes through).
  local -a install_args=(install --force)
  if [[ -n "${OC_NS:-}" ]]; then
    install_args=(install --force --namespace "$OC_NS" --port "${LOCAL_BASE##*:}")
  fi
  log "bin/ocserver ${install_args[*]} (keeps seeded oc.toml; builds + migrates + loads launchd)"
  if ! oc_env "$OCSERVER" "${install_args[@]}" 2>&1 | sed 's/^/[single-machine] install| /' >&2; then
    fail_stage "ocserver ${install_args[*]} failed (see install| lines above)"
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
  # autodeploy's FIRST tick restarts serve moments after install's own health check
  # passed; a long RPC fired inside that window gets cut mid-install. Only leave
  # PHASE 2 after /health is continuously green for 15 consecutive seconds.
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
}

# oc_bootstrap_warden — PHASE 3 bootstrap the server-self warden on THIS host via
# the real product flow POST /api/machines/{m-server-self}/bootstrap-here (server
# runs `ocwarden install --force` inline), then verify launchd loaded + SSE
# connected (log line or hub-online fallback). Calls fail_stage on any failure.
#   reads: SERVER_SELF_ID, GUI, WARDEN_LABEL, HOME_DIR, REPO_ROOT.
#   sets: BOOT_JSON, BOOT_OK, BOOT_EXIT.
oc_bootstrap_warden() {
  # The server-self warden member ('m-server-self') is auto-seeded by the DB. We
  # install a real local warden via the REAL PRODUCT FLOW (OWNER-ONLY):
  #   POST /api/machines/{m-server-self}/bootstrap-here
  # The server resolves the ocwarden binary (503 if absent), re-mints a fresh
  # exec-token, and runs `<ocwarden> install --force` in the server user's own
  # ~/.officraft — i.e. it installs the warden ON THIS HOST.
  log "POST /api/machines/$SERVER_SELF_ID/bootstrap-here (server installs its own warden via ocwarden install --force)"
  # bootstrap-here is a SYNCHRONOUS install RPC: the handler runs `ocwarden install
  # --force` inline and budgets 60s server-side before returning ok=false. The
  # default 15s client cap cuts BEFORE the handler returns (code 000=timeout), and
  # the 000-retry loop then re-fires --force every attempt (bounces the warden). Give
  # it a 90s per-attempt cap (> the server's own 60s ceiling) so a real handler
  # timeout surfaces as a returned ok=false, not a client-side early cut; 2 attempts
  # only (a genuine conn-refused still gets one bounded retry).
  BOOT_JSON="$(api_post_logged "/api/machines/$SERVER_SELF_ID/bootstrap-here" '{}' 2 90 || echo '{}')"
  BOOT_OK="$(printf '%s' "$BOOT_JSON" | json_field ok)"
  BOOT_EXIT="$(printf '%s' "$BOOT_JSON" | json_field exit_code)"
  printf '%s' "$BOOT_JSON" | py -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for line in str(d.get("log","")).splitlines():
    sys.stderr.write("[single-machine] bootstrap-here| " + line + "\n")
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

  # verify the warden actually connected its SSE command reader. In namespace mode
  # bootstrap-here (server oc.toml carries [server].namespace) stamps OC_NAMESPACE
  # into the ocwarden install, so the warden installs under the namespaced root.
  # ocwarden.out.log ONLY: logf -> realMain's `out` -> os.Stdout -> the plist's
  # StandardOutPath. Greping err.log too would make this probe pass on either
  # file and therefore prove nothing about which one carries the line.
  WARDEN_ROOT="${OC_ROOT:-$HOME_DIR/.officraft}/warden"
  sse_ok=""
  for _ in $(seq 1 20); do
    if grep -qE 'command reader: enabled \(SSE' \
         "$WARDEN_ROOT/log/ocwarden.out.log" \
         "$REPO_ROOT/var/log/ocwarden.out.log" 2>/dev/null; then
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
}
