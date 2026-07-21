#!/usr/bin/env bash
# e2e_test/task_system_e2e.sh — M3 TASK-ENGINE full-reset E2E regression.
# ============================================================================
# PURPOSE
#   A DESTRUCTIVE, FULL-RESET, MANUAL end-to-end regression that exercises the
#   whole officraft M3 TASK ENGINE on ONE real machine, canonical serve port,
#   no namespace (kyle route A). It reuses single_machine_e2e.sh's PHASE 0-3
#   bring-up (all pure-moved into lib/oc_lifecycle.sh) to stand the server +
#   server-self warden up from zero, then drives the task engine end-to-end:
#
#     PHASE 0 preflight guards → PHASE 1 teardown old server →
#     PHASE 2 fresh `ocserver install` (canonical serve port) →
#     PHASE 3 bootstrap server-self warden (so the scheduler can spawn workers) →
#     STAGE A  task chain (matrix §2 A1-A8):
#       manual → outsource → create task → scheduler assigns → real worker
#       spawn → synthetic task done (disk-verified) → closeout → fire →
#     STAGE D  parallel_group fork-join (matrix §2.5 D1-D6):
#       3 illegal plan shapes → 400; legal 3-lane+join plan → 200; real
#       fork-join run (3/5/7 → sum 15, disk-verified) →
#     PHASE teardown → clean slate.
#
#   Per-stage objective PASS/FAIL via stage/pass_stage/fail_stage; exits 0 only
#   if every stage is green.
#
# ⚠️  DESTRUCTIVE FULL-RESET. This TEARS DOWN + WIPES the local officraft
#     server on THIS machine and reinstalls it from zero (same teardown path as
#     single_machine_e2e.sh). It is NOT the isolated Playwright suite
#     (setup.sh/run_all.sh, :8791) and NOT a read-only smoke test. It is run BY
#     HAND, by an operator who knows it wipes the local server. It refuses to run
#     without OC_TASK_SYSTEM_YES=1.
#
# ⚠️  seth-m1 ONLY. HARD-PINNED to seth-m1 (kyle route A) via the SAME triple
#     hardware whitelist as single (oc_preflight_guards): hardware UUID (primary
#     anchor) AND LocalHostName AND the joey fleet-infra dir. On any other machine
#     it dies in PHASE 0, before a single destructive action.
#
# ⚠️  CANONICAL serve port, NO --namespace (kyle route A). Isolation from prod
#     comes from "seth-m1 (no prod) + canonical serve port + machine/state whitelist",
#     NOT from a namespace.
#
# ⚠️  TOKEN COST. STAGE A and STAGE D each spawn a REAL claude worker session
#     (each burns tokens even in isolation). The synthetic tasks are deliberately
#     minimal (write a known integer to a file → 1-2 tool calls) and the worker
#     uses the cheapest model/effort (WORKER_MODEL/WORKER_EFFORT).
#
# TEARDOWN SAFETY (fleet zero-touch): teardown boots-out ONLY the four EXACT
#   launchd labels com.officraft.{serve,autodeploy,tunnel,ocwarden}. It NEVER
#   pkill/killall/pattern-kills — this box also runs other fleet jobs. It kills
#   only captured pids/labels and backs the DB up with `.dump` before destroying.
#
# USAGE:
#   OC_TASK_SYSTEM_YES=1 bash e2e_test/task_system_e2e.sh
#
# PARAMS (env, overridable):
#   TEST_AGENT          seeded agent member id used as the manual author + a seed
#                       member for the agent-token floor check (default mira)
#   OWNER_PASSWORD      deterministic owner password to seed (default: random uuid)
#   WORKER_MODEL        cheap model the outsource worker runs (default haiku)
#   WORKER_EFFORT       cheap effort for the outsource worker (default low)
#   TASK_WORKER_TIMEOUT per-task worker spawn/completion budget in s (default 300)
#   RUN_FORK            1 (default) run the STAGE D fork-join REAL worker spawn (D3/D5); 0
#                       skips only the real-spawn part — D1/D2 API-only checks always run
#   OC_TASK_SYSTEM_YES=1   REQUIRED — acknowledges this run is destructive.
# ============================================================================
set -euo pipefail

# This script lives in e2e_test/; source the shared libs (do NOT re-define).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib/oc_lifecycle.sh"   # log/warn/die, stage bookkeeping, api_*, bring-up, ...
source "$HERE/lib/common.sh"         # oc_env(), PROD_PORTS, py(), REPO_ROOT (isolated-port guard inert)

# ---------------------------------------------------------------------------
# 0. params + constants (canonical, single-machine) — MIRRORS single_machine_e2e.sh
# ---------------------------------------------------------------------------
TEST_AGENT="${TEST_AGENT:-mira}"
# LOCAL_BASE / PUBLIC_HOST (loopback base + summarize() footer) are set
# authoritatively by oc_resolve_instance below in BOTH modes — canonical → the
# current prod port (OC_CANONICAL_SERVE_PORT, from config.go); namespace → a
# per-run free port — so no stale port literal lives here.
SECOND_MACHINE="(none — task-system single-machine)"
# Deterministic owner password: caller-provided, else a fresh uuid we seed + reuse.
OWNER_PASSWORD="${OWNER_PASSWORD:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"

# Presence poll budget (reused from single). A REAL claude spawn has no firm upper
# bound — 210s (3.5min) gives a comfortable margin above the observed cold-boot ceiling.
PRESENCE_TIMEOUT="${PRESENCE_TIMEOUT:-210}"

# ── TOKEN-MINIMIZING KNOBS (task-system specific) ──────────────────────────
# The outsource worker runs a REAL claude. Pin it to the cheapest model/effort so a
# full A+D run (2 worker spawns + D's fork sub-agents) burns the least token. These
# flow into manual.assignee {model,effort} (see STAGE A2 / §B of the wire contract).
WORKER_MODEL="${WORKER_MODEL:-claude-haiku-4-5-20251001}"
WORKER_EFFORT="${WORKER_EFFORT:-low}"
# STAGE D fork-join worker: a bigger model (kyle's ruling) — the fork-join e2e must prove
# the PRODUCT parallel_group mechanism works, so the worker EXECUTION layer must be reliable
# (haiku showed weak multi-step steering in A6 'partial'). Only the D fork-join worker
# upgrades; the A main-chain single-task worker stays on the cheap WORKER_MODEL.
FORK_WORKER_MODEL="${FORK_WORKER_MODEL:-claude-sonnet-4-6}"
FORK_WORKER_EFFORT="${FORK_WORKER_EFFORT:-medium}"
# Per-task worker budget: a worker that must ALSO do work (spawn → claim → write file
# → closeout) needs a more generous budget than a bare presence flip. 300s (5min).
TASK_WORKER_TIMEOUT="${TASK_WORKER_TIMEOUT:-300}"
# ── STAGE KNOBS (validate the cheap core chain first, then enable expensive stages) ──
# RUN_FORK — 1 (default) runs the STAGE D fork-join REAL worker spawn (D3/D5: 3/5/7 → 15,
#   disk-verified). Set 0 to skip only the real-spawn part; the cheap D1/D2 API-only
#   plan-shape negatives/roundtrip ALWAYS run regardless of this knob.
RUN_FORK="${RUN_FORK:-1}"

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
BACKUP_DIR="/tmp/oc-task-system-e2e-$TS"   # dump_boot_window (lib) writes here
mkdir -p "$BACKUP_DIR"

TMUX_SOCKET_LOCAL="officraft"   # tmux -L <socket>; TMUX_SOCKET (same value) is in lib

# The server-self warden member is auto-seeded by the DB.
SERVER_SELF_ID="m-server-self"

# SINGLE_PROD_PORTS (the prod ports the 0c guard verifies free) is set by
# oc_resolve_instance below in BOTH modes (canonical → serve+tunnel from the SSOT;
# namespace → this run's own port).

# ── INSTANCE RESOLUTION (T-8aa1) — construction-enforced isolation ───────────
# Default = a fresh run-scoped NAMESPACE (isolated port/labels/root/socket); the
# whole task-engine lifecycle (server + workers) then runs in ~/.officraft-<ns>
# and never touches the canonical fleet. Canonical only via OC_E2E_ALLOW_CANONICAL=1.
oc_resolve_instance

# ── Synthetic-task output dir (created early, torn down at the end) ──────────
# The synthetic tasks write known integers to files under here; STAGE A/D read
# these off DISK (do NOT trust the worker's self-report). rm -rf'd in teardown.
TSE_OUT="/tmp/oc-task-system-out-$TS"
mkdir -p "$TSE_OUT"

# Synthetic task type keys (dedupe/manual identity). Kept stable within a run.
SYNTH_TYPE="e2e-synth-write"          # STAGE A: write one integer to a file
FORK_TYPE="e2e-synth-forkjoin"        # STAGE D: 3-lane fork-join (3/5/7 → 15)

# ---------------------------------------------------------------------------
# preflight tooling — refuse to run unless the operator acknowledged destruction.
# ---------------------------------------------------------------------------
[[ "${OC_TASK_SYSTEM_YES:-}" == "1" ]] || die \
  "refusing: this is a DESTRUCTIVE full-reset TASK-ENGINE E2E (wipes the local server + spawns real claude workers on THIS machine). Re-run with OC_TASK_SYSTEM_YES=1 to acknowledge."

for tool in curl sqlite3 uuidgen launchctl ioreg scutil lsof tmux; do
  command -v "$tool" >/dev/null 2>&1 || die "required tool missing on PATH: $tool"
done
[[ -x "$OCSERVER" ]] || die "bin/ocserver not found/executable at $OCSERVER"

log "params: TEST_AGENT=$TEST_AGENT LOCAL_BASE=$LOCAL_BASE"
log "worker: model=$WORKER_MODEL effort=$WORKER_EFFORT task-budget=${TASK_WORKER_TIMEOUT}s"
log "layout: SERVER_ROOT=$SERVER_ROOT DB=$DB_PATH  backups→$BACKUP_DIR  synth-out→$TSE_OUT"

# ---------------------------------------------------------------------------
# task-system helpers (thin wrappers over lib api_* + jq-free json extraction)
# ---------------------------------------------------------------------------

# json_bool BODY KEY — echo True/true-normalized truthiness of a top-level bool.
json_bool() { printf '%s' "$1" | py -c 'import sys,json; v=json.load(sys.stdin).get(sys.argv[1]); print("true" if v is True or str(v).lower()=="true" else "false")' "$2"; }

# task_field TASK_JSON DOTKEY — read task.<key> from a {task:{...},deduped:...} DTO
# (or a bare TaskDTO). Returns "" if absent.
task_field() {
  printf '%s' "$1" | py -c '
import sys, json
d = json.load(sys.stdin)
t = d.get("task", d)   # accept {task:{...}} or a bare TaskDTO
print(t.get(sys.argv[1], ""))
' "$2"
}

# post_expect_code PATH JSON WANT LABEL — POST as OWNER, assert the HTTP status ==
# WANT (used for the parallel-shape 400 negatives). fail_stage on mismatch.
post_expect_code() {
  local path="$1" json="$2" want="$3" label="$4" out code body
  out="$(curl -sS --max-time 15 -w $'\n%{http_code}' -X POST -H "Authorization: Bearer $OWNER_TOKEN" \
    -H 'content-type: application/json' -d "$json" "$LOCAL_BASE$path" 2>&1)"
  code="${out##*$'\n'}"; body="${out%$'\n'*}"
  if [[ "$code" != "$want" ]]; then
    warn "$label: expected HTTP $want, got $code — body: $(printf '%s' "$body" | head -c 300)"
    return 1
  fi
  log "$label: HTTP $code (expected $want) ✓"
  return 0
}

# post_as_token TOKEN PATH JSON — POST with an ARBITRARY bearer token, echo
# "<code>\n<body>". Used for the agent-token manual-author floor (A1).
post_as_token() {
  local tok="$1" path="$2" json="$3"
  curl -sS --max-time 15 -w $'\n%{http_code}' -X POST -H "Authorization: Bearer $tok" \
    -H 'content-type: application/json' -d "$json" "$LOCAL_BASE$path" 2>&1
}

# read_int_file PATH — echo the trimmed contents of a file, or "" if absent.
read_int_file() { [[ -f "$1" ]] && tr -d ' \t\r\n' < "$1" || printf ''; }

# poll_file_eq PATH WANT BUDGET — poll until PATH exists on disk AND its trimmed
# content == WANT (or budget expires). 0 on match, 1 on timeout. Disk truth only.
poll_file_eq() {
  local path="$1" want="$2" budget="$3" deadline cur
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(read_int_file "$path")"
    [[ "$cur" == "$want" ]] && { log "disk[$path]=$cur (== $want)"; return 0; }
    sleep 3
  done
  warn "disk[$path] never became '$want' within ${budget}s (last='${cur:-<absent>}')"
  return 1
}

# ow_for_task TASK_ID FIELD — from GET /api/outsource-workers, echo FIELD of the
# (first) worker bound to TASK_ID. FIELD in {id,status,created_ts,...}. "" if none.
ow_for_task() {
  api_get /api/outsource-workers 2>/dev/null | py -c '
import sys, json
tid, field = sys.argv[1], sys.argv[2]
for w in json.load(sys.stdin):
    if w.get("task_id") == tid:
        print(w.get(field, "")); break
else:
    print("")
' "$1" "$2"
}

# poll_ow_status TASK_ID WANT BUDGET — poll /api/outsource-workers until the worker
# bound to TASK_ID reaches status WANT. Echoes nothing; 0 on match, 1 on timeout.
poll_ow_status() {
  local tid="$1" want="$2" budget="$3" deadline cur
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(ow_for_task "$tid" status)"
    [[ "$cur" == "$want" ]] && { log "outsource-worker[task=$tid] status=$cur"; return 0; }
    sleep 3
  done
  warn "outsource-worker[task=$tid] never reached status '$want' within ${budget}s (last='${cur:-<none>}')"
  return 1
}

# poll_ow_gone TASK_ID BUDGET — poll until NO worker row is bound to TASK_ID (row
# dropped after release, per §B: status==released is not shown). 0 gone, 1 timeout.
poll_ow_gone() {
  local tid="$1" budget="$2" deadline cur
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    cur="$(ow_for_task "$tid" id)"
    [[ -z "$cur" ]] && { log "outsource-worker for task=$tid dropped from panel (released)"; return 0; }
    sleep 3
  done
  warn "outsource-worker for task=$tid still present after ${budget}s (last id='$cur')"
  return 1
}

# worker_session OW_ID — the warden tmux session name for an outsource worker
# (member-<ow-id> since the A案 P5b naming convergence).
worker_session() { printf 'member-%s' "$1"; }

# tmux_has_worker OW_ID — 0 if the warden tmux session member-<ow-id> exists.
tmux_has_worker() {
  tmux -L "$TMUX_SOCKET_LOCAL" has-session -t "=$(worker_session "$1")" 2>/dev/null
}

# poll_tmux_worker_gone OW_ID BUDGET — poll until member-<ow-id> session is gone.
poll_tmux_worker_gone() {
  local ow="$1" budget="$2" deadline
  deadline=$(( $(date +%s) + budget ))
  while [[ "$(date +%s)" -lt "$deadline" ]]; do
    tmux_has_worker "$ow" || { log "tmux session $(worker_session "$ow") gone"; return 0; }
    sleep 3
  done
  warn "tmux session $(worker_session "$ow") still present after ${budget}s"
  return 1
}

# ===========================================================================
# PHASE 0 — PREFLIGHT ISOLATION GUARDS (die on any failure, BEFORE any teardown)
# ===========================================================================
stage "0. preflight isolation guards (seth-m1 triple whitelist + state + prod ports + env strip)"
# SAME guard sequence as single_machine_e2e.sh — reads the PINNED globals above.
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
# sets OWNER_TOKEN (used by all api_* helpers) + GIT_SHA.
oc_fresh_install
pass_stage

# ===========================================================================
# PHASE 3 — BOOTSTRAP SERVER-SELF WARDEN (so the scheduler can spawn workers)
# ===========================================================================
stage "3. bootstrap server-self warden (local) + verify launchd loaded + SSE connected"
# The scheduler needs an ONLINE warden to dispatch worker spawns to (§D pickWorkerWarden).
oc_bootstrap_warden
pass_stage

# ===========================================================================
# STAGE A — M3 TASK CHAIN (matrix §2 A1-A8; wire contract §A-§D)
# ===========================================================================

# ── A1: create the synthetic manual + agent-author floor (9111cef) ──────────
stage "A1. create task-manual '$SYNTH_TYPE' (owner) + assert 9111cef agent-author floor"

# A1.owner — create a blank manual type (owner token). POST /api/task-manuals.
#   body {type_key}; type_key empty→400, dup→409 (wire §A). Owner may set assignee.
A1_CREATE="$(api_post_logged /api/task-manuals "{\"type_key\":\"$SYNTH_TYPE\"}" || echo '{}')"
[[ -n "$(printf '%s' "$A1_CREATE" | json_field type_key)" ]] \
  || fail_stage "POST /api/task-manuals ($SYNTH_TYPE) returned no type_key — create rejected"
log "manual created: type_key=$SYNTH_TYPE"

# A1.content — PATCH the manual's CONTENT fields (owner). POST /api/task-manuals/{type_key}.
#   fields: output_file (required, is_key → the dedupe key) + number (required).
#   sop_md instructs the worker to write the integer `number` to the path `output_file`.
A1_SOP='SYNTHETIC E2E TASK. Read your two inputs. Write the integer given in input `number` to the absolute path given in input `output_file` (create parent dirs if needed; write ONLY the integer, no trailing text). Then mark the step done and closeout. Do NOT do anything else.'
A1_CONTENT="$(py -c '
import json, sys
print(json.dumps({
  "purpose": "E2E synthetic: write a known integer to a file (1-2 tool calls).",
  "fields": [
    {"name": "output_file", "required": True, "is_key": True},
    {"name": "number",      "required": True, "is_key": False},
  ],
  "sop_md": sys.argv[1],
  "learnings": "",
}))
' "$A1_SOP")"
A1_PATCH="$(api_post_logged "/api/task-manuals/$SYNTH_TYPE" "$A1_CONTENT" || echo '{}')"
[[ -n "$(printf '%s' "$A1_PATCH" | json_field type_key)" ]] \
  || fail_stage "PATCH content for $SYNTH_TYPE (owner) returned no DTO — content write rejected"
log "manual content patched (owner): fields[output_file(key),number] + sop_md"

# A1.floor — the 9111cef agent-author floor: with an AGENT token,
#   PATCH content OK, but a body carrying `assignee` → 403 (callerMaySetAssignee).
# Mint an agent-scope token for the seeded TEST_AGENT via POST /api/mint (owner-authed).
MINT_JSON="$(api_post_logged /api/mint "{\"member_id\":\"$TEST_AGENT\",\"ttl_days\":1}" || echo '{}')"
AGENT_TOKEN="$(printf '%s' "$MINT_JSON" | json_field token)"
if [[ -n "$AGENT_TOKEN" ]]; then
  # (a) agent PATCHing a CONTENT-ONLY body must succeed (agent floor).
  AF_OK="$(post_as_token "$AGENT_TOKEN" "/api/task-manuals/$SYNTH_TYPE" '{"learnings":"agent-authored note"}')"
  AF_OK_CODE="${AF_OK##*$'\n'}"
  [[ "$AF_OK_CODE" =~ ^2[0-9][0-9]$ ]] \
    || fail_stage "agent-token content PATCH expected 2xx, got $AF_OK_CODE — 9111cef agent-author floor regressed"
  log "agent-token content PATCH OK (HTTP $AF_OK_CODE) — content-field author floor holds"
  # (b) agent PATCHing a body that carries `assignee` must be 403.
  AF_DENY="$(post_as_token "$AGENT_TOKEN" "/api/task-manuals/$SYNTH_TYPE" \
    "{\"assignee\":{\"kind\":\"outsource\",\"model\":\"$WORKER_MODEL\"}}")"
  AF_DENY_CODE="${AF_DENY##*$'\n'}"
  [[ "$AF_DENY_CODE" == "403" ]] \
    || fail_stage "agent-token PATCH with assignee expected HTTP 403 (callerMaySetAssignee), got $AF_DENY_CODE — owner-only governance floor regressed"
  log "agent-token PATCH with assignee → HTTP 403 ✓ (owner-only assignee governance holds)"
else
  # mint is owner-gated and works for a seed member (mira) on a fresh install
  # (POST /api/mint {member_id,ttl_days} → {token,...}). No token here is a real failure.
  fail_stage "POST /api/mint {member_id:$TEST_AGENT,ttl_days:1} returned no .token — mint route/seed-member floor regressed (mint is owner-gated + works for seed member mira on fresh install @9111cef)"
fi
pass_stage

# ── A2: owner sets outsourcing on the manual ────────────────────────────────
stage "A2. owner sets manual.assignee = outsource ($WORKER_MODEL/$WORKER_EFFORT, copies=1, machine=auto)"
# wire §B: owner sets manual.assignee {kind:outsource, model(req), effort, copies, machine}.
A2_BODY="$(py -c '
import json, sys
print(json.dumps({"assignee": {
  "kind": "outsource", "model": sys.argv[1], "effort": sys.argv[2],
  "copies": 1, "machine": "auto",
}}))
' "$WORKER_MODEL" "$WORKER_EFFORT")"
A2_PATCH="$(api_post_logged "/api/task-manuals/$SYNTH_TYPE" "$A2_BODY" || echo '{}')"
[[ -n "$(printf '%s' "$A2_PATCH" | json_field type_key)" ]] \
  || fail_stage "owner PATCH assignee=outsource for $SYNTH_TYPE returned no DTO — outsource set rejected"
# assert GET manual reflects the outsource assignee.
A2_GET="$(api_get "/api/task-manuals/$SYNTH_TYPE" 2>/dev/null || echo '{}')"
A2_KIND="$(printf '%s' "$A2_GET" | py -c 'import sys,json; a=json.load(sys.stdin).get("assignee") or {}; print(a.get("kind",""))' 2>/dev/null || echo '')"
[[ "$A2_KIND" == "outsource" ]] \
  || fail_stage "GET /api/task-manuals/$SYNTH_TYPE assignee.kind='$A2_KIND' (expected 'outsource') — outsource assignee not persisted"
log "manual outsourcing set + verified: assignee.kind=outsource"
pass_stage

# ── A3: create the task (+ dedupe roundtrip) ────────────────────────────────
stage "A3. create task (type=$SYNTH_TYPE, inputs {output_file,number:42}) + dedupe roundtrip"
SYNTH_OUT="$TSE_OUT/single.txt"
A3_BODY="$(py -c '
import json, sys
print(json.dumps({
  "title": "E2E synth: write 42",
  "type_key": sys.argv[1],
  "inputs": {"output_file": sys.argv[2], "number": 42},
}))
' "$SYNTH_TYPE" "$SYNTH_OUT")"
A3_CREATE="$(api_post_logged /api/tasks "$A3_BODY" || echo '{}')"
TASK_ID="$(task_field "$A3_CREATE" id)"
[[ -n "$TASK_ID" ]] || fail_stage "POST /api/tasks ($SYNTH_TYPE) returned no task id — create rejected"
# outsource path → executor left blank until the scheduler binds a worker.
A3_EXEC="$(task_field "$A3_CREATE" executor_id)"
[[ -z "$A3_EXEC" ]] \
  || warn "task $TASK_ID created with executor_id='$A3_EXEC' — expected unassigned on the outsource path"
log "task created: id=$TASK_ID (executor unassigned — outsource path)"

# dedupe: create the SAME task again (same is_key output_file) → deduped:true, same id.
A3_DUP="$(api_post_logged /api/tasks "$A3_BODY" || echo '{}')"
A3_DUP_ID="$(task_field "$A3_DUP" id)"
A3_DEDUPED="$(json_bool "$A3_DUP" deduped)"
[[ "$A3_DEDUPED" == "true" ]] \
  || fail_stage "re-create with same dedupe key did not return deduped:true (got '$A3_DEDUPED') — non-terminal dedupe regressed"
[[ "$A3_DUP_ID" == "$TASK_ID" ]] \
  || fail_stage "dedupe returned a DIFFERENT task id ($A3_DUP_ID != $TASK_ID) — dedupe must return the existing non-terminal task"
log "dedupe OK: same (type,output_file) → deduped:true, same id=$TASK_ID"
pass_stage

# ── A4a: scheduler assigns → worker minted (binding precursor for A5) ────────
# NOTE: a priority / band-FIFO ordering block used to live here (labelled "A4"). It
#   has been REMOVED from the e2e (kyle ruling 2026-07-13) — scheduler ordering is an
#   internal outsourceDecide decision, best asserted deterministically at the
#   conformance/unit layer (see the "STAGE A4 … REMOVED FROM E2E" note after A8).
#   The binding assertion below stays in place (A5 needs OW_ID) and folds into A5.
stage "A5. scheduler binds worker (assigned + executor_id) + BEST-EFFORT liveness (active/session are TRANSIENT — outcome verified A6-A8)"
# wire §D: 30s tick + create_task event tick; binding mints ow-<hex12> {status:assigned}
#   and sets task.executor_id = worker.id. Poll the panel until a worker binds to the task.
poll_ow_status "$TASK_ID" assigned "$TASK_WORKER_TIMEOUT" \
  || fail_stage "no outsource worker reached status=assigned for task=$TASK_ID within ${TASK_WORKER_TIMEOUT}s — scheduler never bound a worker"
OW_ID="$(ow_for_task "$TASK_ID" id)"
[[ -n "$OW_ID" ]] || fail_stage "worker bound to task=$TASK_ID but its id was empty on GET /api/outsource-workers"
log "scheduler minted+bound worker: ow-id=$OW_ID (status=assigned)"
# assert task.executor_id == the bound ow-id.
A4_TASK="$(api_get "/api/tasks/$TASK_ID" 2>/dev/null || echo '{}')"
A4_EXEC="$(task_field "$A4_TASK" executor_id)"
[[ "$A4_EXEC" == "$OW_ID" ]] \
  || fail_stage "task.executor_id='$A4_EXEC' != bound ow-id '$OW_ID' — outsource binding did not set task.executor_id"

# ── A5 (cont.): BEST-EFFORT liveness observation (active + tmux are TRANSIENT) ──
# wire §D: the worker's first get_my_task claim flips assigned→active; the warden runs a
#   tmux session member-<ow-id> with claude. BUT a fast worker (small model + tiny task)
#   can blow through assigned→active→done→closeout→released in well under a minute —
#   PROVEN: run1+run2 both wrote the '42' product yet the worker had already left the
#   'active' projection before a coarse poll caught it. So BOTH 'active' and the
#   member-<ow-id> tmux session are TRANSIENT and may already be gone when we look. The
#   AUTHORITATIVE proof the worker really ran is the OUTCOME — A6 disk product (42) +
#   A7 task done/closeout + A8 released. Observe liveness BEST-EFFORT here; NEVER fail on
#   a missed transient snapshot (that was the original false-negative).
_saw=""
_deadline=$(( $(date +%s) + 20 ))
while [[ "$(date +%s)" -lt "$_deadline" ]]; do
  if [[ "$(ow_for_task "$TASK_ID" status 2>/dev/null)" == "active" ]]; then
    _saw="active"; log "observed worker $OW_ID status=active (claimed via get_my_task)"; break
  fi
  if tmux_has_worker "$OW_ID"; then
    _saw="session"; log "observed worker $OW_ID tmux session $(worker_session "$OW_ID") live"; break
  fi
  if [[ -z "$(ow_for_task "$TASK_ID" id 2>/dev/null)" ]]; then
    _saw="gone"; log "worker $OW_ID already left the active projection (fast completion) — outcome verified in A6-A8"; break
  fi
  sleep 2
done
[[ -n "$_saw" ]] || log "no live active/session snapshot for $OW_ID within 20s — transient (fast worker); outcome verified in A6-A8"
pass_stage

# ── A6: synthetic task done — DISK-verified ─────────────────────────────────
stage "A6. synthetic task done — disk product $SYNTH_OUT == '42' (do NOT trust self-report)"
# Read DISK, not the worker's self-report: the file must exist AND contain exactly 42.
poll_file_eq "$SYNTH_OUT" "42" "$TASK_WORKER_TIMEOUT" \
  || fail_stage "worker never wrote '42' to $SYNTH_OUT within ${TASK_WORKER_TIMEOUT}s — synthetic task did not complete on disk"
log "disk product verified: $SYNTH_OUT contains 42"
# best-effort: assert the task's step(s) reached done (server truth).
A6_TASK="$(api_get "/api/tasks/$TASK_ID" 2>/dev/null || echo '{}')"
A6_STEPS_DONE="$(printf '%s' "$A6_TASK" | py -c '
import sys, json
try: d = json.load(sys.stdin)
except Exception: print("?"); sys.exit(0)
t = d.get("task", d)
steps = t.get("steps") or []   # steps live at task.steps @9111cef
if not steps: print("nosteps"); sys.exit(0)
print("all" if all(s.get("status")=="done" for s in steps) else "partial")
' 2>/dev/null || echo '?')"
# Disk product is the authoritative A6 gate; this task.steps[].status read is a soft cross-check.
[[ "$A6_STEPS_DONE" == "all" ]] \
  && log "server: all task.steps[].status=done" \
  || warn "server step-status cross-check inconclusive (got '$A6_STEPS_DONE') — disk product already authoritative"
pass_stage

# ── A7: closeout → task terminal (done) + closed_ts stamped ──────────────────
stage "A7. closeout → task status=done + closed_ts stamped + closeout_reported==true (worker did its closeout duty)"
# @9111cef: closeTask flips workers to `released` WITHOUT reclaiming the session; the WORKER
#   itself must POST /api/tasks/{id}/closeout to be reclaimed immediately (else a 120s backstop
#   grace reclaims it). closeout is a BOOLEAN `closeout_reported` (there is NO closeout_ts field).
#   So we assert: task reached status=done + closed_ts set + closeout_reported==true — proving
#   the worker performed its own closeout (immediate fire). Generous TASK_WORKER_TIMEOUT budget.
_a7_ok=""; A7_STATUS=""; A7_CLOSED=""; A7_REPORTED=""
_deadline=$(( $(date +%s) + TASK_WORKER_TIMEOUT ))
while [[ "$(date +%s)" -lt "$_deadline" ]]; do
  A7_TASK="$(api_get "/api/tasks/$TASK_ID" 2>/dev/null || echo '{}')"
  A7_STATUS="$(task_field "$A7_TASK" status)"
  A7_CLOSED="$(task_field "$A7_TASK" closed_ts)"
  A7_REPORTED="$(json_bool "$A7_TASK" closeout_reported)"
  if [[ "$A7_STATUS" == "done" && -n "$A7_CLOSED" && "$A7_CLOSED" != "0" && "$A7_REPORTED" == "true" ]]; then
    _a7_ok=1; break
  fi
  sleep 3
done
if [[ -z "$_a7_ok" ]]; then
  # If done+closed_ts landed but closeout_reported never flipped true, that is a REAL finding:
  # the worker completed the task but did not report its own closeout (fired only via 120s grace,
  # not immediately) — a worker-seed discipline gap, not a flaky e2e.
  if [[ "$A7_STATUS" == "done" && -n "$A7_CLOSED" && "$A7_CLOSED" != "0" ]]; then
    fail_stage "task=$TASK_ID reached done+closed_ts but closeout_reported never became true within ${TASK_WORKER_TIMEOUT}s — the worker COMPLETED the task but did NOT report closeout (fired only via the 120s grace backstop, not immediately). Worker-seed closeout-discipline gap."
  else
    fail_stage "task=$TASK_ID never reached status=done with a closed_ts + closeout_reported within ${TASK_WORKER_TIMEOUT}s (last status='${A7_STATUS:-?}' closed_ts='${A7_CLOSED:-?}' closeout_reported='${A7_REPORTED:-?}')"
  fi
fi
log "task done + closed_ts stamped + closeout_reported=true (status=$A7_STATUS, closed_ts=$A7_CLOSED) — worker self-reported closeout ✓"
pass_stage

# ── A8: fire — worker released + tmux session gone ──────────────────────────
stage "A8. fire → worker $OW_ID released (panel row drops) + tmux member-$OW_ID gone"
# @9111cef: on done/closeout the bound worker flips to status "released" — the active
#   projection (GET /api/outsource-workers) DROPS released rows, so the row disappearing
#   IS the released signal — and its tmux session member-<ow-id> is killed (bounded poll).
poll_ow_gone "$TASK_ID" "$TASK_WORKER_TIMEOUT" \
  || fail_stage "worker for task=$TASK_ID never dropped from the /api/outsource-workers active projection (never released) within ${TASK_WORKER_TIMEOUT}s"
poll_tmux_worker_gone "$OW_ID" "$TASK_WORKER_TIMEOUT" \
  || fail_stage "tmux session $(worker_session "$OW_ID") never disappeared after release — worker not fired cleanly"
log "worker fired: panel row dropped + tmux session gone"
pass_stage

# ===========================================================================
# STAGE A4 (priority / band-FIFO assignment ORDER) — REMOVED FROM E2E.
#   kyle ruling (2026-07-13): scheduler priority-band + within-band FIFO is an
#   INTERNAL outsourceDecide decision. Asserting its ORDER through a live e2e is
#   inherently flaky — fast tiny workers turn over (bind → finish → release) within
#   ~1min, so a "LOW still unassigned while HIGH bound" snapshot races the turnover,
#   and released workers drop out of the active projection so created_ts can't be
#   re-read. Same "result layer > observation layer" lesson as A5/A6.
#   Coverage relocated to the conformance/unit layer (kyle's lane): given a fixed
#   candidate set, assert outsourceDecide's admit ORDER deterministically (zero race,
#   zero real worker). Seth's "priority tiers + within-band FIFO" requirement stays
#   covered — just at a layer that can prove it reliably.
# ===========================================================================

# ===========================================================================
# STAGE D — parallel_group fork-join (matrix §2.5 D1-D6; wire §C.2 + §D fork-join)
# ===========================================================================

# ── D2 first (API-only negatives, cheap): 3 illegal plan shapes → 400 ───────
stage "D2. parallel_group illegal plan shapes → HTTP 400 (gate-in-group / split-group / one-lane) via EXECUTOR token"
# @9111cef: the plan endpoint checks the EXECUTOR guard BEFORE shape validation — an OWNER
#   token that is NOT the task's executor gets 403 and never reaches the 400 shape check.
#   So create an AD-HOC task whose executor_member_id = a seed member (mira), mint THAT
#   member's agent token, and POST the 3 illegal plans as MIRA's token → assert 400 each.
#   (Zero worker spawn — API only.)
D2_TASK_BODY="$(py -c '
import json, sys
print(json.dumps({"title": "E2E plan-shape negative harness", "executor_member_id": sys.argv[1]}))
' "$TEST_AGENT")"
D2_TASK="$(api_post_logged /api/tasks "$D2_TASK_BODY" || echo '{}')"
D2_TID="$(task_field "$D2_TASK" id)"
[[ -n "$D2_TID" ]] || fail_stage "could not create the throwaway task for D2 plan-shape negatives"
# Mint the EXECUTOR (mira) token so the plan POSTs pass the executor guard and reach shape validation.
D2_MINT="$(api_post_logged /api/mint "{\"member_id\":\"$TEST_AGENT\",\"ttl_days\":1}" || echo '{}')"
D2_EXEC_TOKEN="$(printf '%s' "$D2_MINT" | json_field token)"
[[ -n "$D2_EXEC_TOKEN" ]] \
  || fail_stage "could not mint executor ($TEST_AGENT) token for D2 — plan POSTs would hit the 403 executor guard before the 400 shape check"

# post_plan_as_exec JSON WANT LABEL — POST a plan as the EXECUTOR (mira) token; assert HTTP == WANT.
post_plan_as_exec() {
  local json="$1" want="$2" label="$3" resp code
  resp="$(post_as_token "$D2_EXEC_TOKEN" "/api/tasks/$D2_TID/plan" "$json")"
  code="${resp##*$'\n'}"
  if [[ "$code" != "$want" ]]; then
    warn "$label: expected HTTP $want, got $code — body: $(printf '%s' "${resp%$'\n'*}" | head -c 300)"
    return 1
  fi
  log "$label: HTTP $code (expected $want) ✓"
  return 0
}

# illegal #1: a gate step INSIDE a parallel_group (is_gate=true & parallel_group!="").
D2_GATE_IN_GROUP="$(py -c '
import json
print(json.dumps({"steps": [
  {"name": "laneA", "parallel_group": "pg-1"},
  {"name": "gate-inside", "parallel_group": "pg-1", "is_gate": True},
]}))
')"
post_plan_as_exec "$D2_GATE_IN_GROUP" 400 "D2.gate-in-group" \
  || fail_stage "gate-in-group plan was NOT rejected 400 (ValidatePlanParallelShape rule 1)"

# illegal #2: a SPLIT group — same key non-consecutive (separated by another step).
D2_SPLIT_GROUP="$(py -c '
import json
print(json.dumps({"steps": [
  {"name": "laneA", "parallel_group": "pg-1"},
  {"name": "interloper", "parallel_group": ""},
  {"name": "laneB", "parallel_group": "pg-1"},
]}))
')"
post_plan_as_exec "$D2_SPLIT_GROUP" 400 "D2.split-group" \
  || fail_stage "split-group plan was NOT rejected 400 (ValidatePlanParallelShape rule 2)"

# illegal #3: a ONE-LANE group — same key appears only once (<2 lanes).
D2_ONE_LANE="$(py -c '
import json
print(json.dumps({"steps": [
  {"name": "loneLane", "parallel_group": "pg-1"},
  {"name": "seqAfter", "parallel_group": ""},
]}))
')"
post_plan_as_exec "$D2_ONE_LANE" 400 "D2.one-lane-group" \
  || fail_stage "one-lane-group plan was NOT rejected 400 (ValidatePlanParallelShape rule 3)"
log "all 3 illegal parallel_group shapes rejected 400 ✓"
pass_stage

# ── D1: legal plan (3 consecutive lanes + a join step) → 200, roundtrips ─────
stage "D1. legal plan: 3 consecutive parallel_group lanes + join step → 200 + roundtrip"
D1_PLAN="$(py -c '
import json
print(json.dumps({"steps": [
  {"name": "lane-3", "parallel_group": "pg-1", "dod": "write 3 to file_3"},
  {"name": "lane-5", "parallel_group": "pg-1", "dod": "write 5 to file_5"},
  {"name": "lane-7", "parallel_group": "pg-1", "dod": "write 7 to file_7"},
  {"name": "join-sum", "parallel_group": "", "dod": "read all lanes + write sum (15)"},
]}))
')"
# D1 also posts to the mira-executor task ($D2_TID), so it MUST go through the executor token.
post_plan_as_exec "$D1_PLAN" 200 "D1.legal-plan" \
  || fail_stage "legal 3-lane+join plan was NOT accepted 200 — ValidatePlanParallelShape false-rejected the happy shape"
# roundtrip: GET the task and assert task.steps carries the 3 same-group lanes + the join.
D1_GET="$(api_get "/api/tasks/$D2_TID" 2>/dev/null || echo '{}')"
D1_SHAPE="$(printf '%s' "$D1_GET" | py -c '
import sys, json
t = json.load(sys.stdin); t = t.get("task", t)
steps = t.get("steps") or []   # steps at task.steps; per-step parallel_group @9111cef
lanes = [s for s in steps if s.get("parallel_group")]
groups = {s.get("parallel_group") for s in lanes}
print("ok" if len(lanes) >= 3 and len(groups) == 1 else "bad(lanes=%d groups=%d)" % (len(lanes), len(groups)))
' 2>/dev/null || echo '?')"
[[ "$D1_SHAPE" == "ok" ]] \
  && log "plan roundtrip OK: 3 lanes in one parallel_group + join" \
  || fail_stage "plan roundtrip read failed ('$D1_SHAPE') — task.steps did not carry the 3 same-parallel_group lanes @9111cef"
pass_stage

# ── D3-D6: real fork-join run (3/5/7 → sum 15), DISK-verified ───────────────
# Gated behind RUN_FORK — this is the only STAGE D part that spawns a real worker +
#   sub-agents and polls disk. D1/D2 above (API-only, zero worker spawn) always ran.
if [[ "$RUN_FORK" == "1" ]]; then
stage "D3-D6. real fork-join run (lanes 3/5/7 → join sum 15) — disk-verified"
# Build a fork-join manual + outsource it, then create ONE task whose worker forks 3
# sub-agents (each writes its number to its own file) + a join (reads all → writes 15).
FORK_L3="$TSE_OUT/lane_3.txt"; FORK_L5="$TSE_OUT/lane_5.txt"; FORK_L7="$TSE_OUT/lane_7.txt"
FORK_SUM="$TSE_OUT/join_sum.txt"

# D.manual — create + content-patch the fork-join type (owner).
api_post_logged /api/task-manuals "{\"type_key\":\"$FORK_TYPE\"}" >/dev/null \
  || fail_stage "could not create fork-join manual $FORK_TYPE"
D_SOP='SYNTHETIC E2E FORK-JOIN. Submit a plan with THREE consecutive parallel_group lanes (one parallel_group id) followed by ONE sequential join step. For each lane, spawn a sub-agent that writes ONLY its integer to its lane file: write 3→'"$FORK_L3"', 5→'"$FORK_L5"', 7→'"$FORK_L7"'. After ALL three lanes are done, the JOIN step (you, the worker, NOT a sub-agent) reads the three lane files, sums them (=15), and writes ONLY 15 to '"$FORK_SUM"'. Report every step yourself (the worker body owns all MCP status reports; sub-agents never report). Then closeout.'
D_CONTENT="$(py -c '
import json, sys
print(json.dumps({
  "purpose": "E2E synthetic fork-join: 3 lanes (3/5/7) → join sum 15.",
  "fields": [
    {"name": "l3", "required": True, "is_key": True},
    {"name": "l5", "required": True, "is_key": False},
    {"name": "l7", "required": True, "is_key": False},
    {"name": "sum_file", "required": True, "is_key": False},
  ],
  "sop_md": sys.argv[1],
  "learnings": "",
}))
' "$D_SOP")"
api_post_logged "/api/task-manuals/$FORK_TYPE" "$D_CONTENT" >/dev/null \
  || fail_stage "could not patch content for fork-join manual $FORK_TYPE"
# outsource it — fork-join worker uses the BIGGER FORK_WORKER_MODEL (kyle's ruling: the
# execution layer must be reliable to prove the parallel_group mechanism), copies=1.
D_ASSIGNEE="$(py -c '
import json, sys
print(json.dumps({"assignee": {"kind":"outsource","model":sys.argv[1],"effort":sys.argv[2],"copies":1,"machine":"auto"}}))
' "$FORK_WORKER_MODEL" "$FORK_WORKER_EFFORT")"
api_post_logged "/api/task-manuals/$FORK_TYPE" "$D_ASSIGNEE" >/dev/null \
  || fail_stage "could not set outsource assignee on fork-join manual $FORK_TYPE"
log "fork-join manual $FORK_TYPE created + outsourced"

# D.create — one fork-join task.
D_TASK_BODY="$(py -c '
import json, sys
print(json.dumps({
  "title": "E2E fork-join: 3/5/7 → 15",
  "type_key": sys.argv[1],
  "inputs": {"l3": sys.argv[2], "l5": sys.argv[3], "l7": sys.argv[4], "sum_file": sys.argv[5]},
}))
' "$FORK_TYPE" "$FORK_L3" "$FORK_L5" "$FORK_L7" "$FORK_SUM")"
D_TASK="$(api_post_logged /api/tasks "$D_TASK_BODY" || echo '{}')"
FORK_TID="$(task_field "$D_TASK" id)"
[[ -n "$FORK_TID" ]] || fail_stage "POST /api/tasks ($FORK_TYPE) returned no task id"
log "fork-join task created: id=$FORK_TID"

# scheduler assigns + spawns the worker. Assert BINDING (durable); 'active' is TRANSIENT
# (same lesson as A5) — never REQUIRE the active snapshot. The authoritative proof the
# worker really ran the fork-join is the disk products (D3 lanes + D5 sum) below.
poll_ow_status "$FORK_TID" assigned "$TASK_WORKER_TIMEOUT" \
  || fail_stage "fork-join scheduler never bound a worker to task=$FORK_TID within ${TASK_WORKER_TIMEOUT}s"
FORK_OW="$(ow_for_task "$FORK_TID" id)"
[[ -n "$FORK_OW" ]] || fail_stage "fork-join worker bound but its id was empty on GET /api/outsource-workers"
if [[ "$(ow_for_task "$FORK_TID" status 2>/dev/null)" == "active" ]]; then
  log "fork-join worker active: ow-id=$FORK_OW"
else
  log "fork-join worker bound: ow-id=$FORK_OW (active transient — outcome verified by disk products D3/D5)"
fi

# D3 — each lane file lands with its own number (DISK truth, generous budget: worker
#   spawns sub-agents; each does 1-2 tool calls). Give fork+join extra headroom.
FORK_BUDGET=$(( TASK_WORKER_TIMEOUT * 2 ))
poll_file_eq "$FORK_L3" "3" "$FORK_BUDGET" || fail_stage "lane file $FORK_L3 never became '3' — D3 fork lane failed"
poll_file_eq "$FORK_L5" "5" "$FORK_BUDGET" || fail_stage "lane file $FORK_L5 never became '5' — D3 fork lane failed"
poll_file_eq "$FORK_L7" "7" "$FORK_BUDGET" || fail_stage "lane file $FORK_L7 never became '7' — D3 fork lane failed"
log "D3 OK: all 3 lane files landed (3/5/7) on disk"

# D5 — join: sum file == 15 (read after all lanes; disk truth).
poll_file_eq "$FORK_SUM" "15" "$FORK_BUDGET" || fail_stage "join file $FORK_SUM never became '15' — D5 join/sum failed"
log "D5 OK: join sum file == 15 on disk"

# D5 ordering — join step started only AFTER all lanes finished (finished_ts ordering).
D5_TASK="$(api_get "/api/tasks/$FORK_TID" 2>/dev/null || echo '{}')"
D5_ORDER="$(printf '%s' "$D5_TASK" | py -c '
import sys, json
t = json.load(sys.stdin); t = t.get("task", t)
steps = t.get("steps") or []   # task.steps; per-step started_ts/finished_ts @9111cef
lanes = [s for s in steps if s.get("parallel_group")]
joins = [s for s in steps if not s.get("parallel_group")]
def num(s, k):
    v = s.get(k) or 0
    try: return float(v)
    except Exception: return 0.0
if not lanes or not joins: print("nodata"); sys.exit(0)
last_lane_fin = max(num(s, "finished_ts") for s in lanes)
starts = [num(s, "started_ts") for s in joins if num(s, "started_ts") > 0]
join_start = min(starts) if starts else 0
if last_lane_fin > 0 and join_start > 0:
    print("ok" if join_start >= last_lane_fin else "bad")
else:
    print("notimestamps")
' 2>/dev/null || echo '?')"
# D5 ordering via step timestamps is a SOFT cross-check: the disk sum==15 above is the
# AUTHORITATIVE proof the join ran AFTER all lanes (you cannot sum three files that don't
# exist yet). Step-timestamp ordering additionally needs the worker to have stamped every
# step's started/finished_ts — which a small model may not do reliably (see A6 'partial'
# step-status). So log-and-continue, never fail on a partial/missing step-timestamp read.
if [[ "$D5_ORDER" == "ok" ]]; then
  log "D5 ordering OK (server): join step started_ts >= all lane finished_ts"
else
  warn "D5 ordering step-timestamp cross-check inconclusive (got '$D5_ORDER') — disk sum==15 already proves join-after-lanes (can't sum unwritten files); worker step-timestamp discipline may be partial on a small model"
fi

# D4/D6 not server-observable (no actor field on task/step DTO @9111cef) — covered by conformance/教材, not this e2e.
pass_stage
else
  log "fork-join real run skipped (RUN_FORK=0)"
fi

# ===========================================================================
# PHASE teardown — TEARDOWN → CLEAN SLATE (restore) + drop synthetic output
# ===========================================================================
stage "teardown restore → clean slate (bounded, EXACT labels only) + rm synthetic out"
oc_teardown_bounded "post-run"
# Restore the state whitelist invariant: ~/.officraft back to empty/absent.
if [[ -d "$OC_ROOT" && -z "$(ls -A "$OC_ROOT" 2>/dev/null)" ]]; then
  log "restored: $OC_ROOT is empty (clean slate)"
elif [[ ! -e "$OC_ROOT" ]]; then
  log "restored: $OC_ROOT absent (clean slate)"
else
  warn "post-teardown: $OC_ROOT is NOT empty — residue remains (review before next run):"
  ls -A "$OC_ROOT" 2>/dev/null | sed 's/^/[task-system] residue| /' >&2
fi
# Drop the synthetic output dir (the known-integer product files).
log "rm -rf $TSE_OUT (synthetic task output — disposable)"
rm -rf "$TSE_OUT"
pass_stage

# ===========================================================================
# SUMMARY
# ===========================================================================
summarize
# If we got here every stage was PASS (any FAIL exits early via fail_stage).
for r in "${STAGE_RESULT[@]}"; do [[ "$r" == "PASS" ]] || exit 1; done
exit 0
