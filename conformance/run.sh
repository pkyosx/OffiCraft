#!/usr/bin/env bash
# conformance/run.sh — one-shot runner for the language-agnostic black-box suite.
#
#   conformance/run.sh --target go   # isolated ocserverd (:8795, throwaway SQLite)
#
# go is the ONLY target since the Python retirement (rollback anchor: git tag
# py-final); the flag stays so the target vocabulary remains explicit. The run:
# temp oc.toml (via $OC_CONFIG) + temp SQLite → build ocserverd fresh → goose
# migrate → serve on an ISOLATED port → pytest with OC_TARGET_URL injected →
# teardown that kills ONLY the captured listener pid, plus a final reclaim of
# any listener still on the port whose command line proves it is this run's
# own throwaway binary (see reclaim_own_stray). Prod (the CURRENT
# default port, read from server/ocserverd/config.go — see PROD_PORT below —
# plus retired-but-still-possibly-pinned 8770/8766) and the e2e port (:8791)
# are never touched. Mirrors e2e_test/{setup,teardown}.sh discipline; kept
# self-contained because the lifecycles differ (temp config/db here vs repo
# oc.toml + var/data there).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"

# ── args ─────────────────────────────────────────────────────────────────────
TARGET=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --target) TARGET="${2:-}"; shift 2 ;;
    *) echo "[conformance] unknown arg: $1 (usage: run.sh --target go)" >&2; exit 64 ;;
  esac
done
if [[ "$TARGET" != "go" ]]; then
  echo "[conformance] usage: conformance/run.sh --target go" >&2
  echo "[conformance] (the py target retired with the Python backend — rollback anchor: git tag py-final)" >&2
  exit 64
fi

# ── black-box iron-rule gate (always, before anything runs) ──────────────────
# Conformance test code must NEVER import a server-implementation module —
# otherwise it stops being a language-agnostic behaviour definition. Same grep
# the ci.sh conformance gate runs (the forbidden names are the retired Python
# packages). *.py only (this script necessarily spells the forbidden names).
blackbox_hits="$(grep -RInE --include='*.py' \
  '^[[:space:]]*(import|from)[[:space:]]+(backend|service|dal|domain|plumbing)([.[:space:]]|$)' \
  "$HERE" || true)"
if [[ -n "$blackbox_hits" ]]; then
  echo "[conformance] FAIL — black-box violation: conformance/ imports backend modules:" >&2
  printf '  %s\n' "$blackbox_hits" >&2
  exit 1
fi
echo "[conformance] black-box lint OK (no backend imports in conformance/)"

# ── target scaffolding ────────────────────────────────────────────────────────
# Isolated, non-prod port: 8795 (e2e owns 8791).
CONF_PORT="${OC_CONF_PORT:-8795}"

# PROD_PORT — the CURRENT officraft prod default, read from the single source
# of truth (server/ocserverd/config.go's `defaultPort` const) instead of a
# hand-maintained number that silently goes stale (T-a3ba follow-up: this
# refusal list used to say "8770/8766" as if those WERE the current prod
# port; 8770 is actually a RETIRED former officraft default (config.go's own
# migration-history comment: 8770 → 8780 → 7755 — the real current one, which
# this enumeration never listed) — so the guard's NAME promised more than it
# enforced. It went unnoticed only because the separate "port already in use"
# leftover guard further below happens to cover a live prod on 7755 too — that
# is cover from a DIFFERENT guard, not this one actually working; do not read
# "nothing broke" as "the enumeration was fine"). A failed parse here is a
# HARD FAIL, not a silently-skipped guard — guessing "no known prod port"
# would be worse than the stale list it replaces.
#
# ⚠️ The trailing `|| true` is LOAD-BEARING, do not "clean it up". This file
# runs under `set -euo pipefail`. When config.go is unreadable or its
# `defaultPort` line stops matching, the grep pipeline exits non-zero, and
# under `set -e` a command substitution in an assignment kills the script AT
# THE ASSIGNMENT — the `if [[ -z ... ]]` below would never be reached, so the
# FATAL message would never be printed and the exit code would be 1, not 2.
# That is exactly the "silent hard fail" the comment above promises this is
# NOT. It was a live defect (T-a3ba round-2 review, finding F2): the guard
# shipped as dead code and the claim above shipped as a false statement.
# `|| true` makes the substitution succeed with an empty PROD_PORT so the
# check below actually runs and actually speaks. (e2e_test/lib/common.sh's
# twin of this guard never had the bug — that file deliberately uses
# `set -uo pipefail` with no `-e`.)
PROD_PORT="$(grep -E '^[[:space:]]*defaultPort[[:space:]]*=[[:space:]]*[0-9]+' \
  "$REPO_ROOT/server/ocserverd/config.go" 2>/dev/null | grep -oE '[0-9]+' | head -1 || true)"
if [[ -z "$PROD_PORT" ]]; then
  echo "[conformance] FATAL: could not parse server/ocserverd/config.go's defaultPort — refusing to run without a working prod-port guard (T-a3ba)." >&2
  exit 2
fi
# Additional refusals, NOT derived from config.go (nothing in this repo can
# derive them, so they stay a hand-maintained list and CAN drift again —
# named honestly as such, unlike the guard's old self-description):
#   - 8770, 8780: officraft's own RETIRED former defaults (config.go history)
#     — kept for any install that still has one explicitly pinned in oc.toml.
#   - 8766: a DIFFERENT product's live port ("vibe-clicking", see
#     conformance/CLAUDE.md) — not derivable from this repo at all.
for _p in "$PROD_PORT" 8770 8780 8766; do
  if [[ "$CONF_PORT" == "$_p" ]]; then
    echo "[conformance] FATAL: OC_CONF_PORT=$CONF_PORT is a PROD port (current officraft default=$PROD_PORT per server/ocserverd/config.go, or a retired officraft default / a different live product's port) — refuse." >&2
    exit 2
  fi
done
BASE="http://127.0.0.1:${CONF_PORT}"

# Suite venv: pytest+httpx ONLY (never a server-implementation stack — black-box).
CVENV="$HERE/.venv"
if [[ ! -x "$CVENV/bin/python" ]]; then
  echo "[conformance] creating suite venv (pytest+httpx only)"
  if command -v uv >/dev/null 2>&1; then
    uv venv --seed "$CVENV" >/dev/null
  else
    python3 -m venv "$CVENV"
  fi
fi
if ! "$CVENV/bin/python" -c "import pytest, httpx" >/dev/null 2>&1; then
  if command -v uv >/dev/null 2>&1; then
    uv pip install -q -r "$HERE/requirements.txt" --python "$CVENV/bin/python"
  else
    "$CVENV/bin/python" -m pip install -q -r "$HERE/requirements.txt"
  fi
fi

# routes_manifest.json is a FROZEN committed snapshot (it was mechanically
# extracted from the retired Python route table — tag py-final — and is now
# wire-freeze material alongside spec/*.json: change it spec-first, through the
# owner). The suite itself pins it against the live server: test_openapi_covers_
# manifest locks manifest ≡ spec operations, and the auth matrix locks every
# row's requires against live behaviour — a drifted manifest goes red in the run.

# Leftover guard — never stomp whatever already listens on the port.
if lsof -nP -iTCP:"$CONF_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[conformance] FATAL: :$CONF_PORT already in use — refuse to stomp it." >&2
  exit 2
fi

# Throwaway world: temp dir holds oc.toml + SQLite; nothing in the repo tree.
# The oc.toml is the post-B2 effective schema (port + dsn only — [auth] is
# retired); the owner password is seeded into the DB as a hash via the
# `ocserverd set-password` harness seam below (a real fresh install seeds
# none — the owner sets it through the first-run claim flow).
WORK="$(mktemp -d -t oc-conformance.XXXXXX)"
OWNER_PASSWORD="conf-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
DB_URL="sqlite:///$WORK/conformance.db"
cat >"$WORK/oc.toml" <<EOF
[server]
port = $CONF_PORT

[storage]
dsn = "$DB_URL"
EOF

SERVE_PID=""
LISTEN_PID=""

# reclaim_own_stray — the third option between "always kill" and "never kill".
#
# Both of the previous designs were wrong in one direction. Killing $LISTEN_PID
# unconditionally killed processes the identity checks had just argued were NOT
# ours (round-1 finding). Refusing to kill anything the identity checks had not
# blessed left OUR OWN ocserverd holding :$CONF_PORT whenever one of those
# checks fired on a FALSE POSITIVE — and then `rm -rf "$WORK"` deleted its
# executable out from under it, so the next human saw a process pointing at a
# path that no longer exists, and the next `bin/ci.sh` died on the unrelated
# ":$CONF_PORT already in use" guard (round-2 finding F1, reproduced 3/3).
#
# The question "is this listener ours?" is answerable without trusting either
# identity check: ask the OS what the process is running. $WORK is a fresh
# mktemp -d per invocation, so a listener whose argv[0] is "$WORK/ocserverd"
# CANNOT be anyone else's — not another checkout, not another concurrent
# conformance run, not a leftover from an earlier run (different $WORK), not
# prod (canonical install path). This is the same discipline as
# e2e_test/teardown.sh:30-45 ("This is not a blind pkill"), which run.sh's
# header already claims to mirror but did not.
#
# Every candidate on the port is examined (not `head -1`): the ambiguous-pid
# exit can leave 2+ listeners, and ours may not be first.
#
# Residual gap, stated plainly rather than papered over: if `ps` cannot read
# the command line at all (empty output), we do NOT know whose the process is,
# and this function leaves it alone — so a `ps` failure can still strand our
# own listener. That is the deliberate direction to fail in (never kill an
# unknown), and it is a fresh `ps` call independent of the one the identity
# check made, so a transient hiccup there is usually recovered here. It is not
# a case this ticket can close; the NOTE printed below is what tells a human
# it happened. (Its sibling — `lsof` itself failing — is NOT silent: that path
# WARNs explicitly rather than returning as if the port were empty.)
# _pid_alive PID — true only if the pid exists AND is not a reaped-pending
# zombie. `kill -0` alone is not a death test: a just-killed child stays
# signalable as a zombie until reaped, which would make the death confirmation
# below report a false failure.
_pid_alive() {
  local _st
  _st="$(ps -p "$1" -o state= 2>/dev/null || true)"
  [[ -n "$_st" && "$_st" != Z* ]]
}

reclaim_own_stray() {
  local _stray _cmd _left="" _out _rc

  # Look ONCE and keep the exit status. `lsof` exits 1 for "no matches", which
  # is the normal happy answer; ANY other non-zero (127 = not installed, etc.)
  # means we could not look at all — which is NOT the same as "nothing is
  # there". Reporting that as silence would be the exact class of bug this
  # ticket exists to kill, so it gets a voice.
  _out="$(lsof -nP -tiTCP:"$CONF_PORT" -sTCP:LISTEN 2>/dev/null)"; _rc=$?
  if [[ $_rc -ne 0 && $_rc -ne 1 ]]; then
    echo "[conformance] WARN: could not inspect :$CONF_PORT for leftovers — lsof exited $_rc (not the 'no matches' 1). Teardown therefore CANNOT say whether this run left its own listener behind. Check :$CONF_PORT by hand before the next run." >&2
    return
  fi

  while IFS= read -r _stray; do
    [[ -n "$_stray" ]] || continue
    _cmd="$(ps -p "$_stray" -o command= 2>/dev/null || true)"
    case "$_cmd" in
      "$WORK/ocserverd"*)
        # TERM, grace, then KILL — the same escalation cleanup() applies to its
        # captured pids. Nothing is printed until the outcome is KNOWN: the
        # whole point of this ticket is that a log line must not claim more
        # than actually happened.
        kill "$_stray" 2>/dev/null || true
        for _ in 1 2 3 4 5; do
          _pid_alive "$_stray" || break
          sleep 1
        done
        if _pid_alive "$_stray"; then
          kill -9 "$_stray" 2>/dev/null || true
          sleep 1
        fi
        if _pid_alive "$_stray"; then
          echo "[conformance] WARN: :$CONF_PORT is held by OUR OWN stray listener pid=$_stray ($WORK/ocserverd) and it SURVIVED both TERM and KILL — teardown did NOT reclaim it. The next run's port guard will refuse to start until this pid is gone; stop it by hand." >&2
        else
          echo "[conformance] reclaimed OUR OWN stray listener pid=$_stray on :$CONF_PORT (confirmed dead) — its command line was this run's throwaway binary ($WORK/ocserverd), so it was provably ours. (A guard above exited before the pid was blessed; leaving it would have wedged the next run's port guard.)" >&2
        fi
        ;;
      *)
        _left="$_left $_stray"
        ;;
    esac
  done <<EOF
$_out
EOF

  if [[ -n "$_left" ]]; then
    echo "[conformance] NOTE: :$CONF_PORT still has listener(s)$_left that are NOT ours (command line is not $WORK/ocserverd) — deliberately left alone. Inspect manually; this is not a blind pkill." >&2
  fi
}

cleanup() {
  # Kill ONLY captured pids (launch pid + actual listener) — never a pattern kill.
  for pid in "$LISTEN_PID" "$SERVE_PID"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  # Grace, then hard-kill a survivor.
  for _ in 1 2 3 4 5; do
    { [[ -z "$SERVE_PID" ]] || ! kill -0 "$SERVE_PID" 2>/dev/null; } && break
    sleep 1
  done
  for pid in "$LISTEN_PID" "$SERVE_PID"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
  # Poll for release, then reclaim anything still on the port that is provably
  # ours. Runs BEFORE `rm -rf "$WORK"` on purpose: the identity evidence we use
  # is the live process's argv, and deleting the workdir first is what made the
  # orphan hard to diagnose in the first place.
  for _ in 1 2 3 4 5 6 7 8; do
    lsof -nP -iTCP:"$CONF_PORT" -sTCP:LISTEN >/dev/null 2>&1 || break
    sleep 0.5
  done
  reclaim_own_stray
  rm -rf "$WORK"
  echo "[conformance] teardown done (workdir removed)"
}
trap cleanup EXIT

# Strip ambient fleet env (same hazard e2e_test/lib/common.sh guards): OC_ID /
# OC_TOKEN / OC_BASE must never leak the isolated serve toward the prod server.
#
# OC_RELEASE_API_BASE (t-dc68): re-point the GitHub Releases update check at an
# unroutable loopback address — the black-box run must never reach the real
# api.github.com (hermeticity + the anonymous 60/hour rate limit); every check
# fails FAST and the wire answers its honest degraded faces deterministically.
# OC_NO_ONBOARDING=1 (T-ba62) is a HOST-SAFETY switch, not a test convenience.
# The automatic first-run onboarding installs THIS host's warden, and a launchd
# label is a singleton in the user's GUI domain keyed on uid — it does not follow
# $HOME, a temp dir, or this throwaway database. So a suite that reached
# set-password on its fresh DB would re-point the operator's REAL warden at a
# server on :8795 that is torn down seconds later. The server carries its own
# interlock (it refuses to install over an existing warden), but this suite runs
# on machines that may have none at all, where that interlock passes.
oc_env() { env -u OC_ID -u OC_TOKEN -u OC_BASE OC_CONFIG="$WORK/oc.toml" \
             OC_NO_ONBOARDING=1 \
             OC_DATABASE_URL="$DB_URL" OC_RELEASE_API_BASE="http://127.0.0.1:1" "$@"; }

# T-e731: the seed .md files, the prebuilt ocwarden/ocagent, and the frozen MCP
# catalog are served EMBED-ONLY (server/ocserverd/assets.go + api_machines.go —
# no disk fallback). The fresh ocserverd below therefore boots off its embed
# ALONE (running with CWD = repo root no longer feeds it disk assets), so it
# MUST be built with seedsdist/bindist STAGED or it boots with an empty embed
# (no seeds → boot 500s, no catalog → tools/list fails). Idempotent + guarded:
# skip when already staged (bin/ci.sh stages before invoking this step) so a
# standalone run.sh self-stages without a redundant ocwarden/ocagent rebuild.
if ! ls "$REPO_ROOT"/server/ocserverd/seedsdist/*.md >/dev/null 2>&1; then
  echo "[conformance] staging seedsdist (embed-only seeds)"
  bash "$REPO_ROOT/bin/build-seedsdist"
fi
if [[ ! -f "$REPO_ROOT/server/ocserverd/bindist/ocwarden" ]]; then
  echo "[conformance] staging bindist (embed-only binaries + frozen catalog)"
  bash "$REPO_ROOT/bin/build-bindist"
fi
# Build ocserverd fresh from source into the throwaway dir, then migrate +
# serve against the temp oc.toml/SQLite.
echo "[conformance] building ocserverd (go build from server/ocserverd)"
(cd "$REPO_ROOT/server/ocserverd" && go build -o "$WORK/ocserverd" .)

echo "[conformance] migrate (ocserverd migrate → $DB_URL)"
(cd "$REPO_ROOT" && oc_env "$WORK/ocserverd" migrate >/dev/null)

echo "[conformance] seeding owner password (ocserverd set-password, hash → DB settings)"
(cd "$REPO_ROOT" && oc_env env OC_NEW_PASSWORD="$OWNER_PASSWORD" "$WORK/ocserverd" set-password >/dev/null)

# Leftover guard, re-checked (TOCTOU close, T-a3ba): the FIRST guard (line ~85)
# ran before the venv/go-build/migrate/set-password steps above — several
# seconds to low-tens-of-seconds of window in which nothing re-checked the
# port. A listener that grabbed :$CONF_PORT during that window would go
# undetected by the first guard, and (before this change) would have been
# indistinguishable from our own serve by the health-check loop below — its
# 200 would satisfy `ok=1` just as well as ours. Re-check immediately before
# we actually bind.
if lsof -nP -iTCP:"$CONF_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[conformance] FATAL: :$CONF_PORT became occupied during build/migrate/seed (TOCTOU window) — refuse to stomp it. Find and stop that listener, then re-run." >&2
  exit 2
fi

echo "[conformance] starting isolated ocserverd on $BASE"
(cd "$REPO_ROOT" && oc_env nohup "$WORK/ocserverd" serve >"$WORK/serve.log" 2>&1) &
SERVE_PID=$!

# Expected build identity: gitSHA() (server/ocserverd/server.go) is unstamped
# here (plain `go build`, no -ldflags) so its boot-time fallback runs
# `git rev-parse --short HEAD` in CWD — and serve's CWD is $REPO_ROOT (line
# above). Compute the same probe from the shell so we have something to
# compare the responder's self-report against.
EXPECTED_SHA="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"

ok=""
GOT_SHA=""
for _ in $(seq 1 30); do
  if RESP="$(curl -sf "$BASE/api/version" 2>/dev/null)"; then
    GOT_SHA="$(printf '%s' "$RESP" | "$CVENV/bin/python" -c \
      'import sys,json
try:
    print(json.load(sys.stdin).get("git_sha",""))
except Exception:
    print("")' 2>/dev/null || true)"
    ok=1
    break
  fi
  if ! kill -0 "$SERVE_PID" 2>/dev/null; then break; fi
  sleep 1
done
if [[ -z "$ok" ]]; then
  echo "[conformance] FATAL: serve did not become healthy in 30s. serve.log tail:" >&2
  tail -20 "$WORK/serve.log" >&2 || true
  exit 1
fi

# The ACTUAL listener pid can differ from the launch pid — capture the socket
# holder. AMBIGUOUS (0 or >1 candidates) is a hard failure, never a silent
# `head -1` pick — mirrors e2e_test/a1_zombie_e2e.sh's listener_pid_of, which
# treats "none or >1" as empty/refuse rather than guessing.
LISTEN_CANDIDATES=()
while IFS= read -r _cand; do
  [[ -n "$_cand" ]] && LISTEN_CANDIDATES+=("$_cand")
done < <(lsof -nP -tiTCP:"$CONF_PORT" -sTCP:LISTEN 2>/dev/null || true)
if [[ "${#LISTEN_CANDIDATES[@]}" -ne 1 ]]; then
  # bash-3.2-safe empty-array expansion (same hazard as oc_lifecycle.sh's
  # `reasons` array) — never bare "${LISTEN_CANDIDATES[*]}" under set -u.
  _cand_list=""
  for _c in ${LISTEN_CANDIDATES[@]+"${LISTEN_CANDIDATES[@]}"}; do
    _cand_list="$_cand_list $_c"
  done
  echo "[conformance] FATAL: health check got HTTP 200 on :$CONF_PORT but the listener pid is AMBIGUOUS (${#LISTEN_CANDIDATES[@]} candidates:${_cand_list:- none}) — refusing to guess which one answered us (launch pid=$SERVE_PID). Teardown will reclaim any of those pids whose command line proves it is this run's own throwaway binary, and will say so; anything it leaves behind is NOT ours — investigate and stop it, then re-run." >&2
  exit 1
fi
# NOT the global LISTEN_PID yet — cleanup() reads the global, and this pid is
# still UNVERIFIED at this point. Promoting it here (T-a3ba review finding)
# meant a mismatch branch below could exit 1, hit `trap cleanup EXIT`, and
# cleanup() would kill a process we had JUST finished arguing is not ours —
# the exact "can't tell self from other" bug this whole ticket is about,
# just relocated into teardown. _CANDIDATE_PID is local-in-spirit (read-only
# below); LISTEN_PID stays "" until BOTH identity checks pass.
#
# Holding LISTEN_PID back is NOT by itself a safe teardown, and the first
# version of this fix wrongly treated it as one: it means the mismatch exits
# leave the port holder untouched, which is right when the holder is someone
# else's and WRONG when the check misfired on our own server. cleanup()'s
# reclaim_own_stray covers that second case with independent evidence (argv
# path under $WORK), so "never kill someone else's" and "never abandon our
# own" both hold instead of trading off.
_CANDIDATE_PID="${LISTEN_CANDIDATES[0]}"

# Identity check #1 (content-level): the responder must self-report the
# git_sha we expect. e2e_test/setup.sh already fetched this field before
# T-a3ba but never compared it — wired up on both sides now.
if [[ -z "$GOT_SHA" || "$GOT_SHA" != "$EXPECTED_SHA" ]]; then
  echo "[conformance] FATAL: health 200 but identity mismatch — /api/version reported git_sha='${GOT_SHA:-<empty>}', expected '$EXPECTED_SHA' (this checkout's HEAD). launch pid=$SERVE_PID listener pid=$_CANDIDATE_PID. Either the 200 came from a DIFFERENT process (a leftover listener from an earlier run, or someone else's server), or THIS CHECK IS WRONG (e.g. the server's gitSHA() probe timed out and reported 'unknown'). We do not know which, so teardown decides by evidence, not by this check: it kills pid=$_CANDIDATE_PID only if its command line is this run's own throwaway binary ($WORK/ocserverd), and prints which way it went. If teardown reports leaving it alone, it is not ours — find and stop that listener yourself, then re-run." >&2
  exit 1
fi

# Identity check #2 (process-level): the listener's own command line must be
# the exact throwaway binary built for THIS run ($WORK is a fresh mktemp per
# invocation) — the check that actually distinguishes us from another
# conformance/e2e instance running the SAME commit concurrently, which
# check #1 (git_sha) alone cannot tell apart.
LISTEN_CMD="$(ps -p "$_CANDIDATE_PID" -o command= 2>/dev/null || true)"
case "$LISTEN_CMD" in
  "$WORK/ocserverd"*) : ;;
  *)
    echo "[conformance] FATAL: health 200 but identity mismatch — listener pid=$_CANDIDATE_PID's command ('${LISTEN_CMD:-<unknown>}') is not our throwaway binary ($WORK/ocserverd), even though git_sha matched. This looks like a concurrent conformance/e2e run on the same commit racing us for :$CONF_PORT (launch pid=$SERVE_PID). We will NOT kill pid=$_CANDIDATE_PID — this check IS the ownership evidence and it says no; teardown applies the same test and will leave it alone. Find and stop the other run yourself, then re-run." >&2
    exit 1
    ;;
esac

# Both checks passed — ONLY NOW is it safe to hand this pid to cleanup().
LISTEN_PID="$_CANDIDATE_PID"
echo "[conformance] serve healthy AND identity-verified (launch pid=$SERVE_PID listener pid=$LISTEN_PID sha=$GOT_SHA)"

echo "[conformance] pytest (OC_TARGET_URL=$BASE)"
set +e
env -u OC_ID -u OC_TOKEN -u OC_BASE \
  OC_TARGET_URL="$BASE" OC_OWNER_PASSWORD="$OWNER_PASSWORD" \
  "$CVENV/bin/python" -m pytest "$HERE" -q
RC=$?
set -e

if [[ $RC -eq 0 ]]; then
  echo "[conformance] all green (target=$TARGET base=$BASE)"
else
  echo "[conformance] FAILED (target=$TARGET exit=$RC)" >&2
fi
exit $RC
