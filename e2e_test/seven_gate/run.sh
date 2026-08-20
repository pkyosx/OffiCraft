#!/usr/bin/env bash
# e2e_test/seven_gate/run.sh — one seven-step run, end to end.
#
#   setup (isolated :8791) → hire the agent → PLANT the scene → start the
#   collector → run the actor → stop the collector → judge → emit the friction
#   questions → teardown
#
# Read seven_gate/CLAUDE.md before changing anything here.
#
#   bash e2e_test/seven_gate/run.sh                          # stub actor, current seeds
#   OC_SEEDS_SRC=/tmp/candidate-seeds bash …/run.sh          # candidate boot context
#   OC_SG_SKIP_STEP=reply_card bash …/run.sh                 # prove the gate can say no
#   OC_SG_ACTOR=actors/live.sh bash …/run.sh                 # 🔴 real agent — burns quota
#
# 🔴 DEFAULT-OFF ON THE ONE THING THAT COSTS MONEY: the default actor is the
# stub, which spawns nothing. Nothing in this file starts a claude process, and
# an actor that would must say so in its own header. This script is NOT wired
# into run_all.sh and NOT into bin/ci.sh — the JUDGE is what CI guards
# (tests_guard case 21), and the judge needs no server.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E="$HERE/.."
. "$E2E/lib/common.sh"
. "$HERE/lib/http.sh"
. "$HERE/lib/friction.sh"
. "$HERE/lib/window.sh"
. "$HERE/lib/carrier.sh"

ACTOR="${OC_SG_ACTOR:-$HERE/actors/stub.sh}"
[[ "$ACTOR" = /* ]] || ACTOR="$HERE/$ACTOR"
[[ -f "$ACTOR" ]] || { echo "[seven_gate] FATAL: no actor at $ACTOR" >&2; exit 2; }

STAMP="$(date -u '+%Y%m%dT%H%M%SZ')"
RUN_DIR="${OC_SG_RUN_DIR:-$HERE/runs/$STAMP}"
mkdir -p "$RUN_DIR"

# 0. THE RUN MUST OUTLIVE WHOEVER STARTED IT, AND IT MUST NEVER DIE SILENTLY.
#    A paid run died exactly here: the agent session that had launched this
#    script in the background was collected, the carrier was killed WITH it
#    mid-poll, and — because the rc a waiter watches was the rc of the SHELL THAT
#    DIED — nothing was ever written. Nobody judged, nobody tore down, and the
#    real agent it had spawned (which lives in tmux and did NOT die) kept
#    spending. See lib/carrier.sh for the full account and the three layers.
#    The run dir is pinned BEFORE the re-exec so the detached copy writes to the
#    same place, and announced BEFORE it so a caller that dies one second later
#    still knows which file to look at.
export OC_SG_RUN_DIR="$RUN_DIR"
echo "[seven_gate] run $STAMP → $RUN_DIR (terminal signal: $RUN_DIR/outer.rc — it is written NO MATTER HOW this run ends)"
sg_carrier_detach "$0" "$@"

LOG="$RUN_DIR/run.log"
exec > >(tee -a "$LOG") 2>&1
echo "[seven_gate] run $STAMP → $RUN_DIR  (actor=$(basename "$ACTOR")  OC_SEEDS_SRC=${OC_SEEDS_SRC:-<repo seeds/>})"

COLLECTOR_PID=""
RESPONDER_PID=""
# EXACT PIDs only, never a name pattern: this harness's serve is the same binary
# with the same argv as the live one, so `pkill -f` here would take the fleet
# down with it (root CLAUDE.md「驗證、CI 與出貨／程序安全」).
cleanup() {
  local rc=$?
  [[ -n "$RESPONDER_PID" ]] && kill "$RESPONDER_PID" 2>/dev/null
  [[ -n "$COLLECTOR_PID" ]] && kill "$COLLECTOR_PID" 2>/dev/null
  bash "$E2E/teardown.sh" || true
  # LAST, and after teardown: the terminal signal means "this run is over and
  # here is what happened", so it must not appear while the run is still
  # dismantling itself. Every exit path arrives here — the ordinary end, every
  # `exit 2` refusal above, and the signal handlers armed just below, which
  # deliberately exit THROUGH this trap rather than around it.
  sg_carrier_write "$rc"
  sg_carrier_watchdog_stop
}
trap cleanup EXIT
sg_carrier_arm "$RUN_DIR/outer.rc"
sg_carrier_watchdog

# 1. isolated server. setup.sh re-stages seedsdist through bin/build-seedsdist on
#    every run, and that script honours OC_SEEDS_SRC — so a candidate boot
#    context is one env var away and the tracked seeds/ is never touched.
bash "$E2E/setup.sh"
. "$E2E/.state/env"
BASE="${OC_E2E_BASE}"
OWNER_TOK="$(cat "$E2E/.state/owner.tok")"
# Every owner-side call goes through sg_http, which writes the status code and
# the body to run.log AND to http.log. Nothing here is allowed to end in
# `>/dev/null` — that is precisely what made the first baseline unreadable.
export SG_BASE="$BASE" SG_TOKEN="$OWNER_TOK" SG_HTTP_TAG="owner" \
       SG_HTTP_LOG="$RUN_DIR/http.log"

# 2. a fresh agent for this run. Fresh on purpose: the whole question is what a
#    NEW agent does with the boot context, and a reused member arrives already
#    knowing things the boot context never taught it.
AGENT_NAME="sg-$STAMP"
AGENT="$(sg_http POST /api/members "{\"name\":\"$AGENT_NAME\",\"role_key\":\"assistant\"}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or d.get("member",{}).get("id",""))')"
[[ -n "$AGENT" ]] || { echo "[seven_gate] FATAL: hire failed — cannot judge a run with no agent." >&2; exit 2; }
AGENT_TOK="$(sg_http POST /api/mint "{\"member_id\":\"$AGENT\",\"ttl_days\":1}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$AGENT_TOK" ]] || { echo "[seven_gate] FATAL: mint failed for $AGENT." >&2; exit 2; }
echo "[seven_gate] agent=$AGENT ($AGENT_NAME)"

# 2b0. PIN THE RUNTIME / MODEL / EFFORT the agent will be launched with. The
#      whole ticket is "does a NEW agent, reading only the boot context, walk
#      this path" — and the answer is allowed to differ per runtime and per
#      effort, so a regression that cannot name the configuration cannot compare
#      two runs. These are the owner's launch settings on the member row
#      (PATCH /api/members/{id}); the server hands them to the warden inside the
#      START frame (reconcile.go buildStartFrame: Runtime/Model/Effort), which is
#      why nothing in the spawn chain needs touching.
#
#      🔴 SET, THEN READ BACK, THEN REFUSE ON MISMATCH. A PATCH that answers 200
#      having stored something else would give a run that claims one
#      configuration and measures another — the exact class of lie this harness
#      exists to remove. The read-back is what makes the claim evidence, so it
#      is also written into scene.json and printed.
if [[ -n "${OC_SG_RUNTIME:-}${OC_SG_MODEL:-}${OC_SG_EFFORT:-}" ]]; then
  PATCH_BODY="$(python3 -c '
import json, os, sys
out = {}
for env, key in (("OC_SG_RUNTIME", "runtime"), ("OC_SG_MODEL", "model"), ("OC_SG_EFFORT", "effort")):
    v = os.environ.get(env, "")
    if v:
        out[key] = v
print(json.dumps(out))')"
  sg_http PATCH "/api/members/$AGENT" "$PATCH_BODY" >/dev/null \
    || { echo "[seven_gate] FATAL: pinning runtime/model/effort was refused — see the [http] line above. A run that cannot set its own configuration must not claim one." >&2; exit 2; }
fi
# Read back UNCONDITIONALLY — including the no-pin case, so every run records
# what it actually ran as instead of what it meant to.
MEMBER_NOW="$(sg_http GET "/api/members/$AGENT")"
read -r GOT_RUNTIME GOT_MODEL GOT_EFFORT <<<"$(printf '%s' "$MEMBER_NOW" | python3 -c '
import sys, json
d = json.load(sys.stdin)
print(d.get("runtime", "") or "-", d.get("model", "") or "-", d.get("effort", "") or "-")')"
echo "[seven_gate] member config (read back from the server): runtime=$GOT_RUNTIME model=$GOT_MODEL effort=$GOT_EFFORT"
for _want in "${OC_SG_RUNTIME:-}:$GOT_RUNTIME:runtime" "${OC_SG_MODEL:-}:$GOT_MODEL:model" "${OC_SG_EFFORT:-}:$GOT_EFFORT:effort"; do
  _asked="${_want%%:*}"; _rest="${_want#*:}"; _got="${_rest%%:*}"; _field="${_rest#*:}"
  if [[ -n "$_asked" && "$_asked" != "$_got" ]]; then
    echo "[seven_gate] FATAL: asked for $_field='$_asked' but the server stored '$_got'. Refusing to run a regression that would report a configuration it is not using." >&2
    exit 2
  fi
done

# 2b. THE OWNER'S INTENT, and ① cannot be observed without it. presence=waking is
#     derived (server/ocserverd/domain.go PresenceState) from desired_state ==
#     online AND a fresh waking_since — BOTH, not either. A freshly hired member
#     is desired_state=offline, so its report_waking stamps waking_since and the
#     projection still reads `offline`: the first baseline's ① went red here,
#     with the report itself answering a clean 200. This is the owner switching
#     the member on, which is the owner's act and no part of the seven steps —
#     it belongs on this side of the actor boundary, not in the actor.
#     Order matters: activate ZEROES waking_since (api_members.go), so it must
#     precede the agent's boot report, never follow it.
#     Pinning above happens FIRST: activate is what triggers the reconcile that
#     builds the START frame, so the launch settings must already be on the row.
#     (the body is captured rather than discarded — see lib/http.sh's header:
#     no server call in this harness may end in >/dev/null.)
ACTIVATED="$(sg_http POST "/api/members/$AGENT/activate" '{}')"
[[ -n "$ACTIVATED" ]] || echo "[seven_gate] WARNING: activate returned an empty body — check the [http] line above."
echo "[seven_gate] owner intent: desired_state=online (waking is derivable from here on)"

# 3. PLANT THE SCENE. ②'s fact cannot be read directly — resume_summary is a GET
#    and stamps nothing — so the scene carries a nonce that ONLY the resume
#    snapshot surfaces, and ② passes iff the agent quotes it back. Planted as an
#    owner→agent chat message BEFORE boot, which is exactly what "接回現場"
#    means: something was already here.
NONCE="sg-nonce-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
PLANTED="$(sg_http POST /api/chat "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"【上一班留下的現場】本現場標記 "+sys.argv[2]+" — 接回現場後請把它原樣帶回來。"}))' "$AGENT" "$NONCE")")"
[[ -n "$PLANTED" ]] || { echo "[seven_gate] FATAL: planting the scene message produced no response — ② could only ever be red, and red for a HARNESS reason. Read the [http] line above." >&2; exit 2; }

# 3b. SEAT A COLLEAGUE. The six steps above all point at the OWNER — chat to the
#     owner, a card for the owner, a task the owner watches. Talking to another
#     AGENT is a different act with a different recipient and nobody patient on
#     the other end, and the boot context has to teach it too (owner, after the
#     first baseline: 「包含 chat / reply card / task」「他要知道怎麼透過這三個元件
#     跟 owner 溝通」「或是跟其他 agent 溝通」).
#     The peer SPEAKS FIRST, carrying its own nonce, so the fact the gate reads
#     is a REPLY and not a broadcast: something addressed to the colleague that
#     shows the colleague's message was actually read.
PEER_NAME="sg-peer-$STAMP"
PEER="$(sg_http POST /api/members "{\"name\":\"$PEER_NAME\",\"role_key\":\"assistant\"}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or d.get("member",{}).get("id",""))')"
[[ -n "$PEER" ]] || { echo "[seven_gate] FATAL: could not hire the peer agent — ⑦ could only ever be red, and red for a HARNESS reason." >&2; exit 2; }
PEER_TOK="$(sg_http POST /api/mint "{\"member_id\":\"$PEER\",\"ttl_days\":1}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$PEER_TOK" ]] || { echo "[seven_gate] FATAL: mint failed for the peer $PEER." >&2; exit 2; }
PEER_NONCE="sg-peer-nonce-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
# Sent AS THE PEER, from the peer's own token — an owner-sent message would be
# the owner talking, which is the half ② and ⑥ already cover.
PEER_SAID="$(SG_TOKEN="$PEER_TOK" SG_HTTP_TAG="peer" sg_http POST /api/chat \
  "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"我是隔壁的 "+sys.argv[2]+"。我這邊在等你那條線的結果——你看完現場後直接回我一句，把這個記號帶上："+sys.argv[3]}))' "$AGENT" "$PEER_NAME" "$PEER_NONCE")")"
[[ -n "$PEER_SAID" ]] || { echo "[seven_gate] FATAL: the peer's opening message did not land — see the [http] line above." >&2; exit 2; }
echo "[seven_gate] peer=$PEER ($PEER_NAME) spoke first: $PEER_NONCE"


# 3c. PLANT THE PICTURE. ⑨ asks one thing: can it SEE. So a number is drawn into
#     a PNG and that number exists NOWHERE ELSE — not in the body, not in the
#     filename, not in the mime, not in a task, not in any file the agent can
#     open. If it ever leaks into text, an agent that cannot see passes, and
#     THAT PASS IS INDISTINGUISHABLE FROM THE REAL ONE. Hence the scan below.
#     Regenerated per run and never 42: a hard-coded answer is one a model can
#     have memorised, and a cell that can be passed from memory measures nothing.
IMG_ANSWER="$(python3 -c 'import random;print("".join(random.choice("0123456789") for _ in range(6)))')"
IMG_PATH="$RUN_DIR/scene-image.png"
python3 "$HERE/make_answer_png.py" "$IMG_ANSWER" "$IMG_PATH" >/dev/null \
  || { echo "[seven_gate] FATAL: could not draw the scene image." >&2; exit 2; }
# The body names the picture and asks for the number; it must NOT contain it.
# Same for the filename — a filename is text, and text is exactly what this cell
# is trying to rule out as a source.
IMG_POSTED="$(python3 - "$AGENT" "$IMG_PATH" <<'PY'
import base64, json, sys
agent, path = sys.argv[1], sys.argv[2]
print(json.dumps({
    "to": agent,
    "body": "【交接圖】上一班把要用的號碼寫在這張圖上了，沒有留文字版。看一下圖，把上面那個號碼原樣回我。",
    "attachments": [{
        "filename": "handover-note.png",
        "mime": "image/png",
        "data_b64": base64.b64encode(open(path, "rb").read()).decode("ascii"),
    }],
}, ensure_ascii=False))
PY
)"
IMG_RESP="$(SG_HTTP_TAG="owner:image" sg_http POST /api/chat "$IMG_POSTED")"
[[ -n "$IMG_RESP" ]] || { echo "[seven_gate] FATAL: the picture did not land — ⑨ could only ever be red, and red for a HARNESS reason." >&2; exit 2; }
echo "[seven_gate] picture planted (the number is in the pixels only)"

# 3c-bis. THE EVIDENCE BUNDLE'S OWN HEADER — written HERE, before the leak scan,
#     so the scan below walks it like any other file. It carries salt+sha256 of
#     the answer, NEVER the answer: this directory lives in the repo tree on the
#     same host as a live agent, so a plaintext answer here made ⑨'s own claim
#     ("in no file the agent can open") false about the harness's own bundle.
#     judge.py re-derives the match by hashing the digit runs in the agent's
#     messages, so nothing is lost. ⚠️ This raises the bar from `grep` to
#     brute-forcing 10^6 candidates against a salt sitting in the same file — it
#     is not secrecy, and no line here may claim it is.
IMG_SALT="$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')"
python3 - "$AGENT" "$NONCE" "$STAMP" "$RUN_DIR/scene.json" "$PEER" "$PEER_NONCE" \
          "$IMG_ANSWER" "$IMG_SALT" "$GOT_RUNTIME" "$GOT_MODEL" "$GOT_EFFORT" <<'PY'
import hashlib, json, sys
(agent, nonce, stamp, out, peer, peer_nonce, answer, salt,
 runtime, model, effort) = sys.argv[1:12]
json.dump({"agent_id": agent, "scene_nonce": nonce, "stamp": stamp,
           "peer_id": peer, "peer_nonce": peer_nonce,
           "image_answer_salt": salt,
           "image_answer_len": len(answer),
           "image_answer_sha256":
               hashlib.sha256((salt + answer).encode("utf-8")).hexdigest(),
           "agent_runtime": runtime, "agent_model": model, "agent_effort": effort},
          open(out, "w"), ensure_ascii=False, indent=2)
PY
echo "[seven_gate] scene planted: $NONCE (scene.json carries the picture's answer as salt+sha256, never in clear)"

# 3d. THE LEAK SCAN — the cell's whole validity in one check. Everything the
#     agent can READ AS TEXT is pulled back off the server and searched for the
#     answer; a single hit means a text-only agent could pass, so the run
#     REFUSES rather than producing a green nobody can trust.
#     A POSITIVE CONTROL runs first: the same scanner looks for the scene nonce,
#     which we KNOW is in the text. If the control finds nothing, the scanner is
#     broken and "zero hits" would be meaningless — so that is a refusal too.
#
#     🔴 THE SCANNER'S REACH IS PART OF THE CLAIM, AND IT USED TO BE SHORTER THAN
#     THE CLAIM. Until this round it read SERVER TEXT ONLY, while ⑨ said the
#     number was "in no task, plan or FILE the agent can open" — and the harness
#     was writing it, in clear, into $RUN_DIR/scene.json, inside the repo tree,
#     on the machine the live agent runs on. Two things changed: scene.json now
#     carries salt+sha256 instead of the number (judge.py re-derives the match),
#     and the scan below walks the run dir as well as the server.
#     The picture itself is EXCLUDED by name and that is not a loophole: the
#     answer is supposed to be in those pixels, and a compressed PNG matching six
#     ASCII digits by chance would be a refusal nobody could act on.
#     The THIRD surface — the environment handed down to a real agent — is not
#     scannable from here, because the stub legitimately receives the answer that
#     way. It is enforced at the only hop that matters, actor → warden → tmux, by
#     actors/live.sh via lib/scrub.sh (which carries its own positive control).
scan_scene_text() { # scan_scene_text NEEDLE -> prints hit count
  local needle="$1" hits=0 hay
  for p in "/api/chat?limit=500" "/api/tasks" "/api/reply-cards?status=waiting" \
           "/api/reply-cards?status=answered" "/api/members"; do
    hay="$(SG_HTTP_TAG="owner:leakscan" sg_http GET "$p" 2>/dev/null)"
    hits=$(( hits + $(printf '%s' "$hay" | grep -o -F "$needle" | wc -l | tr -d ' ') ))
  done
  # The agent's own wake snapshot — the one surface assembled FOR it.
  hay="$(SG_TOKEN="$AGENT_TOK" SG_HTTP_TAG="owner:leakscan" sg_http GET /api/resume-summary 2>/dev/null)"
  hits=$(( hits + $(printf '%s' "$hay" | grep -o -F "$needle" | wc -l | tr -d ' ') ))
  # …and every file this run has written so far, the picture excepted.
  hits=$(( hits + $(scan_scene_files "$needle") ))
  printf '%s' "$hits"
}
# ⚠️ `find -L`: plain `find` does not descend into a SYMLINKED directory, so a
# file behind one was outside "every file this run has written so far"
# (measured on the tests_guard walks that share this shape). At this point the
# run dir holds only what the harness itself has written, so following links
# cannot wander somewhere large.
scan_scene_files() { # scan_scene_files NEEDLE -> prints hit count over $RUN_DIR
  local needle="$1" hits=0 f
  while IFS= read -r f; do
    [[ "$(basename "$f")" == "scene-image.png" ]] && continue
    hits=$(( hits + $(grep -o -a -F "$needle" "$f" 2>/dev/null | wc -l | tr -d ' ') ))
  done < <(find -L "$RUN_DIR" -type f 2>/dev/null | sort)
  printf '%s' "$hits"
}
CONTROL_HITS="$(scan_scene_text "$NONCE")"
# The file half needs its own control: the server half alone can satisfy the
# combined count above while the directory walk finds nothing at all (an empty
# walk and a clean walk look identical), which is how the pkill ban lost its
# reach the first time. scene.json is written BEFORE this point and carries the
# scene nonce in clear, so a working walk must see it.
FILE_CONTROL_HITS="$(scan_scene_files "$NONCE")"
if [[ "${FILE_CONTROL_HITS:-0}" -lt 1 ]]; then
  echo "[seven_gate] FATAL: the leak scanner's FILE walk found 0 hits for the scene nonce under $RUN_DIR, and scene.json carries it in clear. The walk is reaching nothing, so a clean file scan would mean nothing. Refusing to run." >&2
  exit 2
fi
if [[ "${CONTROL_HITS:-0}" -lt 1 ]]; then
  echo "[seven_gate] FATAL: the leak scanner's POSITIVE CONTROL found 0 hits for the scene nonce, which IS in the text. The scanner is broken, so a clean answer-scan would mean nothing. Refusing to run." >&2
  exit 2
fi
LEAK_HITS="$(scan_scene_text "$IMG_ANSWER")"
if [[ "${LEAK_HITS:-0}" -ne 0 ]]; then
  echo "[seven_gate] FATAL: the image's number appears $LEAK_HITS time(s) in TEXT the agent can read. ⑨ would be passable without ever opening the picture, and that pass would look exactly like a real one. Refusing to run." >&2
  # WHERE, because this refusal has TWO possible causes and they need different
  # actions. (a) the harness really leaked the answer — fix that. (b) COINCIDENCE:
  # the answer is six digits and the scanned corpus is full of epoch stamps and
  # ids, so a run-dir walk gives a small-but-real chance that a random six digits
  # already occur somewhere. The action for (b) is simply to run again (the answer
  # is redrawn per run). Printing the hits is what lets the reader tell which.
  grep -rn -a -F "$IMG_ANSWER" "$RUN_DIR" 2>/dev/null \
    | grep -v 'scene-image\.png' | head -5 | sed 's/^/[seven_gate]   hit: /' >&2
  echo "[seven_gate] (six random digits can also collide with a timestamp by chance — if the hits above are not the harness handing the answer over, just run again: the number is redrawn every run.)" >&2
  exit 2
fi
echo "[seven_gate] leak scan: answer 0 hits in readable text AND in $RUN_DIR (positive controls: scene nonce $CONTROL_HITS hit(s) overall, $FILE_CONTROL_HITS of them in files — both halves of the scanner work)"

# 4. collector FIRST — ①'s presence=waking is gone within seconds of the agent
#    mounting SSE, so a collector started after the actor reads a green run red.
#    Its window is DERIVED from the actor budget (lib/window.sh), never set on
#    its own: a collector that stops before the actor does turns every later
#    fact into a red that names the AGENT for the harness's own gap. The
#    assertion is here as well as in CI because a violated window makes the
#    whole verdict untrustworthy, and that is a refusal, not a warning.
sg_assert_collection_window || exit 2
COLLECT_SECONDS="$(sg_collect_seconds)"
echo "[seven_gate] collector window ${COLLECT_SECONDS}s ≥ actor budget $(sg_actor_budget_secs)s"
python3 "$HERE/collect.py" --base "$BASE" --token-file "$E2E/.state/owner.tok" \
  --agent "$AGENT" --run-dir "$RUN_DIR" --interval "$OC_SG_INTERVAL" \
  --seconds "$COLLECT_SECONDS" >>"$RUN_DIR/collect.log" 2>&1 &
COLLECTOR_PID=$!
sleep 2

# 4b. THE OWNER ON THE OTHER END OF ⑥'s CARD. A card opened by the executor of an
#     active task AUTO-BINDS to that task's current step and parks it in
#     waiting_owner (api_replycards.go inferCardTaskStep → armStepWithCard), and
#     waiting_owner has exactly ONE exit: the owner answers. Without someone on
#     that end, ⑥ succeeding is what makes ⑦ impossible — the step can never
#     move again, the task can never close, and closeout is terminal-only. That
#     is not a harness convenience; it is who the counterparty is. It answers
#     only cards this run's agent opened, on this run's isolated server.
#     It answers CARDS. It never answers the friction questions — those are the
#     agent's own words or they are nothing (see 〈friction〉 in CLAUDE.md).
(
  SG_HTTP_TAG="owner:cards"
  while :; do
    _cards="$(sg_http GET '/api/reply-cards?status=waiting')" || _cards=""
    for _cid in $(printf '%s' "$_cards" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
rows = d.get("cards", d) if isinstance(d, dict) else d
for c in rows if isinstance(rows, list) else []:
    if c.get("from") == sys.argv[1] and c.get("id"):
        print(c["id"])
' "$AGENT" 2>/dev/null); do
      sg_http POST "/api/reply-cards/$_cid/answer" \
        '{"option_idx":0,"text":"（七步關卡的 owner 端）就照你列的第一個選項辦，做完照常回報收尾。"}' \
        || true
    done
    sleep "${OC_SG_ANSWER_INTERVAL:-2}"
  done
) &
RESPONDER_PID=$!
echo "[seven_gate] owner card-responder pid=$RESPONDER_PID (answers ONLY $AGENT's cards)"

# 5. the actor. Its rc is recorded and deliberately not acted on.
#    OC_SG_OWNER_TOKEN is the COUNTERPARTY's token, and it is in the contract
#    because a real agent needs an owner on the other end: someone has to ask it
#    to do something, and someone has to put the friction questions to it
#    afterwards. It CANNOT forge a single judged fact — ① is a self-report keyed
#    to the caller's own token, ②'s message and ⑥'s card are matched on
#    from==agent, ③'s task on creator_id==agent, and ④⑤⑦ hang off THAT task —
#    so an actor holding it still cannot make a red run look green.
OC_SG_BASE="$BASE" OC_SG_AGENT="$AGENT" OC_SG_AGENT_TOKEN="$AGENT_TOK" \
OC_SG_SCENE_NONCE="$NONCE" OC_SG_RUN_DIR="$RUN_DIR" OC_SG_OWNER="owner" \
OC_SG_OWNER_TOKEN="$OWNER_TOK" OC_SG_PEER="$PEER" OC_SG_PEER_NONCE="$PEER_NONCE" \
OC_SG_IMAGE_ANSWER="$IMG_ANSWER" OC_SG_RUNTIME="$GOT_RUNTIME" \
  bash "$ACTOR" 2>&1 | tee "$RUN_DIR/actor.log"
echo "[seven_gate] actor rc=${PIPESTATUS[0]} (recorded, not judged)"

# 6. one last settle, then stop collecting and judge what the server held.
sleep "$OC_SG_SETTLE"
kill "$RESPONDER_PID" 2>/dev/null; wait "$RESPONDER_PID" 2>/dev/null
RESPONDER_PID=""
kill "$COLLECTOR_PID" 2>/dev/null; wait "$COLLECTOR_PID" 2>/dev/null
COLLECTOR_PID=""
python3 "$HERE/judge.py" "$RUN_DIR"; RC=$?
printf '%s\n' "$RC" > "$RUN_DIR/rc"

# 7. the friction questions, verbatim from the one file that holds them. Asked on
#    green runs too — the gate knows whether the fact landed, never whether the
#    agent got there by guessing.
echo
sg_friction_questions "$HERE/friction.md"
echo "[seven_gate] ↑ ask these two, verbatim; paste the answers into $RUN_DIR/friction.txt"
echo "[seven_gate] artifacts: $RUN_DIR (run.log actor.log collect.log http.log journal.ndjson scene.json verdict.json rc)"
echo "[seven_gate] every server call this run made, with its HTTP status and body: $RUN_DIR/http.log"
exit "$RC"
