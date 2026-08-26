#!/usr/bin/env bash
# e2e_test/tests_guard/run.sh — HERMETIC unit tests for the T-8aa1 isolation
# layer in e2e_test/lib/oc_lifecycle.sh: the live-fleet guard + the namespace
# allocator (oc_resolve_instance) + the derivation helpers.
#
# WHY bats-free: e2e_test/ has no shell-test harness. This is a tiny, dependency-
# free runner (assert helpers + a PATH shim that stubs EVERY external command the
# guard/allocator touches) so it can run inside bin/ci.sh on ANY host — including
# a LIVE fleet host — WITHOUT touching the real launchctl/tmux/lsof/fleet. The
# stubs return controlled output and NOTHING real is mutated.
# ⚠️ It used to say "NO teardown path is ever exercised", and that is no longer
# true: cases 20b/20e/20f drive the real setup.sh → run_all.sh → teardown.sh
# chain. The narrower property that does hold — and, more to the point, the one
# this file PINS rather than merely asserts — is that teardown reaches the disk
# only through the record-only seam, against a throwaway tree: case 20e pins the
# seam as teardown.sh's only way out, and 19c/20c/20f keep a sentinel in that
# tree and fail if anything deletes it. So it records what it would have removed
# instead of removing it. That is what makes this safe on a live fleet host;
# "no teardown code runs at all" is not, and has not been for a while.
#
# SCOPE — what decides which cases run
# NOTHING discovers anything here. This file IS the suite: every case is a
# literal block in this one script, run top to bottom, and there is no per-file
# collection step that would notice a block that stopped existing. So deleting or
# short-circuiting a case block does not fail — it silently runs less, and
# PASS/FAIL only ever count what was actually reached. `FAIL -eq 0` answers "did
# anything fail?", not "did anything run?", exactly like the rc of a test runner.
# That is why there is a PASS FLOOR at the bottom of this file. Read its comment
# for what it does and does NOT catch — it is a floor, so it catches the suite
# being gutted, not one case going missing.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$HERE/../lib/oc_lifecycle.sh"
[[ -f "$LIB" ]] || { echo "FATAL: lib not found at $LIB" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}

# ── PATH shim: stub every external command the guard/allocator invokes ───────
SHIMDIR="$(mktemp -d -t oc-guard-shim.XXXXXX)"
TRIPWIRE="$SHIMDIR/.tripwire"
: > "$TRIPWIRE"
trap 'rm -rf "$SHIMDIR"' EXIT

cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
# Only two verbs matter to the code under test: `print` (read-only detection) and
# `bootout` (MUST never be reached by a guard/allocator — tripwire if it is).
if [[ "$1" == "bootout" ]]; then
  if [[ "${SHIM_ALLOW_TEARDOWN:-0}" == "1" ]]; then
    echo "launchctl $*" >> "$SHIM_TEARDOWN_LOG"
  else
    echo "TRIPWIRE launchctl bootout $*" >> "$SHIM_TRIPWIRE"
  fi
  exit 0
fi
if [[ "$1" == "print" ]]; then
  case "$2" in
    */com.officraft.ocwarden) [[ "${SHIM_WARDEN:-0}" == "1" ]] && exit 0 || exit 1 ;;
    *) exit 1 ;;
  esac
fi
exit 0
SH

cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
# Answer LISTEN queries: exit 0 (occupied) iff the -iTCP:<port> is in SHIM_LISTEN_PORTS.
port=""
for a in "$@"; do case "$a" in -iTCP:*) port="${a#-iTCP:}";; esac; done
case " ${SHIM_LISTEN_PORTS:-} " in *" $port "*) exit 0 ;; *) exit 1 ;; esac
SH

cat > "$SHIMDIR/tmux" <<'SH'
#!/usr/bin/env bash
# forms used: `-L <sock> ls`  and  `-L <sock> ls -F '#S'`. Sessions in
# SHIM_SESSIONS (newline-sep) belong to the canonical socket 'officraft'.
sock="$2"
if [[ "${3:-}" == "ls" ]]; then
  if [[ "$sock" == "officraft" && -n "${SHIM_SESSIONS:-}" ]]; then
    [[ "${4:-}" == "-F" ]] && printf '%s\n' "$SHIM_SESSIONS"
    exit 0
  fi
  exit 1
fi
exit 0
SH

cat > "$SHIMDIR/ioreg" <<'SH'
#!/usr/bin/env bash
# The hardware-identity anchor (T-e1dd). Emits the one line the guard's awk reads.
# SHIM_HW_UUID drives it; empty = the "cannot read a UUID" case. This is a PATH
# stub on purpose and NOT an env override in the guard itself: production must
# have no way to be told what machine it is on.
printf '    "IOPlatformUUID" = "%s"\n' "${SHIM_HW_UUID-00000000-0000-0000-0000-FEEDFACE0000}"
SH

cat > "$SHIMDIR/ssh" <<'SH'
#!/usr/bin/env bash
# Stands in for the second machine. It EXECUTES the probe command the guard sent,
# in a real shell, against a fake remote $HOME — rather than printing canned
# answers. That difference matters: the probe nests `awk -F\"` and `\$4` inside a
# double-quoted command substitution inside a single-quoted remote command, and a
# canned-answer shim would keep every assertion green while a broken escaping
# silently returned an empty UUID from every real host — which is fail-OPEN.
#
# SHIM_SSH_FAIL   → unreachable host (must be fail-closed)
# SHIM_SSH_SILENT → an ssh that exits 0 having run nothing (ForceCommand, a
#                   restricted shell, an rc file that returns early). Also
#                   fail-closed: a probe that did not run is not a clean host.
[[ "${SHIM_SSH_FAIL:-0}" == "1" ]] && { echo "ssh: connect to host ${*: -2:1} port 22: Operation timed out" >&2; exit 255; }
[[ "${SHIM_SSH_SILENT:-0}" == "1" ]] && exit 0
# SHIM_SSH_NOISE → a line the remote emits before the probe's own output. Two
# flavours matter: ordinary stderr chatter (the ubiquitous known-hosts warning),
# which must be TOLERATED, and marker-shaped output, which must REFUSE.
[[ -n "${SHIM_SSH_NOISE:-}" ]] && printf '%s\n' "$SHIM_SSH_NOISE" >&2
cmd="${!#}"   # last arg = the remote command
[[ "$cmd" == *IOPlatformUUID* ]] || { echo "TRIPWIRE ssh shim got an unexpected remote command: $cmd" >> "$SHIM_TRIPWIRE"; exit 3; }
# A real remote HOME, so `[ -d "$HOME/.officraft/server" ]` is answered by the
# filesystem rather than by a canned string.
rhome="$SHIM_REMOTE_HOME"
mkdir -p "$rhome"
[[ "${SHIM_REMOTE_SERVER_TREE:-0}" == "1" ]] && mkdir -p "$rhome/.officraft/server"
# SHIM_REMOTE_TOOLS=none reproduces the real thing an ssh non-login shell does:
# Homebrew's bin dir is absent, so `tmux` is simply not found. Without this the
# harness guarantees every remote tool resolves and can never catch a probe that
# asks a question the far side cannot answer.
# The probe exports the remote Homebrew bin dir itself (gotcha #2). Rewrite that
# literal to OUR stub dir, or the command resolves the REAL tmux/launchctl of
# whatever machine this suite runs on — which both defeats the fake remote host
# and makes the result depend on the test machine's own fleet state.
rbin="$SHIM_REMOTE_BIN"
[[ "${SHIM_REMOTE_TOOLS:-all}" == "notmux" ]] && rbin="${SHIM_REMOTE_BIN}-notmux"
# Tripwire on the literal, same reason as the IOPlatformUUID one above: if
# OC_REMOTE_PATH_PREFIX ever names a different path this rewrite silently stops
# matching — and since the shim also puts $rbin on PATH unconditionally, every
# assertion would stay green while the real prefix was wrong.
[[ "$cmd" == *"/opt/homebrew/bin"* ]] || { echo "TRIPWIRE ssh shim: the probe command no longer contains the /opt/homebrew/bin literal this shim rewrites — OC_REMOTE_PATH_PREFIX changed: $cmd" >> "$SHIM_TRIPWIRE"; exit 3; }
cmd="${cmd//\/opt\/homebrew\/bin/$rbin}"
# PATH IS THE FIXTURE. Borrowing this host's /usr/bin here made "the tool is
# absent" an accident of the RUNNER rather than something the fixture builds:
# macOS has no /usr/bin/tmux, ubuntu-latest does, so the notmux case leaked the
# runner's real tmux, the probe answered live_agents=0, the guard passed, and
# the suite sailed into the teardown it plants on purpose — green on one OS,
# red on the other, testing nothing on either. $SHIM_REMOTE_BASE carries ONLY
# the generic tools the probe genuinely needs (awk/grep/id); every tool whose
# presence the test is ASSERTING ON lives in $rbin, under the fixture's control.
HOME="$rhome" PATH="$rbin:$SHIM_REMOTE_BASE" /bin/sh -c "$cmd"
SH

# The second machine's own tools, resolved on the far side of the ssh shim. They
# are SEPARATE from the local stubs on purpose: the guard must read the REMOTE
# host's identity, and a shim that answered with the local UUID would hide a guard
# that looks at the wrong machine — the exact bug this remote check exists to fix.
mkdir -p "$SHIMDIR/remote-bin"
cat > "$SHIMDIR/remote-bin/ioreg" <<'SH'
#!/usr/bin/env bash
printf '    "IOPlatformUUID" = "%s"\n' "${SHIM_REMOTE_HW-00000000-0000-0000-0000-FEEDFACE0001}"
SH
cat > "$SHIMDIR/remote-bin/launchctl" <<'SH'
#!/usr/bin/env bash
# Only `print` is reachable from the read-only probe. A bootout here would mean
# the guard is mutating the remote host, which it must never do.
if [[ "$1" == "print" ]]; then
  case "$2" in
    */com.officraft.ocwarden) [[ "${SHIM_REMOTE_WARDEN:-0}" == "1" ]] && exit 0 || exit 1 ;;
    *) exit 1 ;;
  esac
fi
echo "TRIPWIRE remote launchctl called with a non-print verb: $*" >> "$SHIM_TRIPWIRE"
exit 0
SH
cat > "$SHIMDIR/remote-bin/tmux" <<'SH'
#!/usr/bin/env bash
# The relocate target's agent sessions. Separate from the local tmux stub so a
# guard reading the LOCAL session list instead of the remote one is visible.
if [[ "${3:-}" == "ls" ]]; then
  [[ "${SHIM_REMOTE_AGENTS:-0}" == "1" ]] || exit 1
  [[ "${4:-}" == "-F" ]] && printf 'member-m-remote1\n'
  exit 0
fi
exit 0
SH
chmod +x "$SHIMDIR"/remote-bin/ioreg "$SHIMDIR"/remote-bin/launchctl "$SHIMDIR"/remote-bin/tmux
# The same stubs MINUS tmux. That is the real shape of gotcha #2: `ioreg`
# (/usr/sbin) and `launchctl` (/bin) are on an ssh non-login PATH, `tmux` is in
# Homebrew and is not — which is why a missing PATH export takes out exactly the
# liveness question and nothing else.
mkdir -p "$SHIMDIR/remote-bin-notmux"
cp "$SHIMDIR"/remote-bin/ioreg "$SHIMDIR"/remote-bin/launchctl "$SHIMDIR/remote-bin-notmux/"
export SHIM_REMOTE_BIN="$SHIMDIR/remote-bin"
# The generic half of the fake remote's PATH: the tools the probe legitimately
# needs that are NOT part of any assertion (awk, grep, id). Resolved through
# `command -v` so this works on both macOS and Linux, and kept deliberately
# small — anything not listed here is, from the probe's point of view, absent,
# which is what lets the fixture DECIDE that a tool cannot be found.
_rbase="$SHIMDIR/remote-base"
mkdir -p "$_rbase"
# `bash` is in that list although nothing in the probe calls it: every stub in
# $rbin carries a `/usr/bin/env bash` shebang, and `env` resolves its argument
# through PATH — a base dir without bash makes the fixture's own stubs
# unrunnable while leaving them perfectly present, which reads downstream as
# "the far side answered nothing" and takes out every remote case at once.
for _t in awk grep id bash; do
  _p="$(command -v "$_t" 2>/dev/null || true)"
  # An ABSOLUTE path, not merely a non-empty answer. `command -v` reports an
  # exported shell function as the bare word, and `ln -sf awk .../remote-base/awk`
  # then makes a symlink pointing at itself: ELOOP, which the far side cannot tell
  # apart from "that tool is not installed" — the exact disguise this whole commit
  # is about, rebuilt one level down in the thing meant to prevent it.
  [[ "$_p" == /* ]] || { echo "tests_guard: cannot build the fake remote PATH — '$_t' did not resolve to an absolute path on this machine (got: '${_p:-<nothing>}')" >&2; exit 2; }
  ln -sf "$_p" "$_rbase/$_t"
done
unset _t _p
# TRIPWIRE, checked before any case runs. If a tool the fixture withholds ever
# becomes reachable through this base dir again, say so loudly HERE — the
# alternative is what actually happened: the affected cases keep passing on the
# OS that happens to lack the tool, and on the OS that has it they fail as an
# unrelated-looking guard bug.
# The list is DERIVED from the stubs rather than written out, so a tool added to
# remote-bin/ later is protected the day it is added. Spelling it out meant the
# protection silently did not extend to anything new — and a guard that covers
# only what someone remembered to list is the shape this suite exists to catch.
# `ssh` is appended because the fake remote must never reach a real one.
# Array glob, not `$(… printf '%s\n' *)`. Unquoted command substitution word-splits
# and RE-GLOBS: with remote-bin/ empty the `*` survives literally and expands
# against the caller's cwd, so the loop iterates repo files, matches nothing, and
# the tripwire passes — checking nothing, silently. Whitespace or a glob character
# in a stub name skips that stub the same quiet way. That is the very failure this
# tripwire exists to catch, one level down, so the empty case is made loud too.
_stubs=("$SHIMDIR"/remote-bin/*)
[[ -e "${_stubs[0]}" ]] || { echo "tests_guard: remote-bin/ is empty — the base-dir tripwire would be checking nothing" >&2; exit 2; }
for _t in "${_stubs[@]##*/}" ssh; do
  if PATH="$_rbase" command -v "$_t" >/dev/null 2>&1; then
    echo "tests_guard: the fake remote base dir resolves '$_t' — the fixture no longer controls whether that tool exists on the far side" >&2
    exit 2
  fi
done
unset _t
export SHIM_REMOTE_BASE="$_rbase"
unset _rbase _stubs

chmod +x "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/tmux "$SHIMDIR"/ioreg "$SHIMDIR"/ssh

# These stubs are enabled ONLY by the hermetic teardown regression below.  They
# record every mutating surface instead of touching the host, which lets the
# test reject a canonical label/root/token target without ever exercising a
# real warden teardown.
cat > "$SHIMDIR/ocwarden" <<'SH'
#!/usr/bin/env bash
echo "ocwarden namespace=${OC_NAMESPACE:-<unset>} args=$*" >> "$SHIM_TEARDOWN_LOG"
exit 0
SH
cat > "$SHIMDIR/rm" <<'SH'
#!/usr/bin/env bash
if [[ "${SHIM_ALLOW_TEARDOWN:-0}" == "1" ]]; then
  # The BRACE GROUP is load-bearing. Written as three bare printfs with the
  # redirection on the last one only, the first two go to stdout and the log
  # receives a lone newline (0x0a) — no rm target is ever recorded, so every
  # tripwire that greps this log matches nothing and passes unconditionally.
  # Case (18c) is the permanent proof that this records what it claims.
  { printf 'rm'; printf ' <%s>' "$@"; printf '\n'; } >> "$SHIM_TEARDOWN_LOG"
  exit 0
fi
exec /bin/rm "$@"
SH
chmod +x "$SHIMDIR"/ocwarden "$SHIMDIR"/rm
export SHIM_TRIPWIRE="$TRIPWIRE"
export PATH="$SHIMDIR:$PATH"

# run_guard — source the lib + run a guard/allocator snippet in a SUBSHELL with a
# clean, controlled env. Echoes "<exit_code>". Stderr is captured to $GLOG.
GLOG="$SHIMDIR/.glog"
run_snippet() {
  local snippet="$1"; shift
  ( set +e
    # clean the isolation env so each case is deterministic.
    unset OC_NS OC_E2E_ALLOW_CANONICAL OC_E2E_NS OC_E2E_NS_PORT 2>/dev/null || true
    export HOME="${TEST_HOME:-/tmp/oc-guard-home}"
    # SNIPPET_LIB lets a NEGATIVE CONTROL source a deliberately mutated copy of
    # the lib (see 18c/18d) so the tripwires' discriminating power is pinned.
    source "${SNIPPET_LIB:-$LIB}" >/dev/null 2>&1
    eval "$snippet"
  ) >"$GLOG" 2>&1
  echo $?
}

# Discover the CURRENT canonical serve port from the single source of truth
# (same derivation the lib itself does from server/ocserverd/config.go) —
# NOT a hardcoded literal, so this test file doesn't become a drift site of
# its own the next time the port changes (T-b76b follow-up: Kyle's review
# note — hardcoding "7755" here would just be swapping one stale literal for
# another).
CANON_PORT="$(run_snippet 'printf "PORT=%s\n" "$OC_CANONICAL_SERVE_PORT"' >/dev/null; grep '^PORT=' "$GLOG" | cut -d= -f2)"
[[ -n "$CANON_PORT" ]] || { echo "FATAL: could not discover OC_CANONICAL_SERVE_PORT via $LIB" >&2; exit 2; }

echo "[tests_guard] hermetic isolation-layer unit tests"

# ── 1) live warden + CANONICAL mode → guard DIES ─────────────────────────────
rc="$(SHIM_WARDEN=1 run_snippet 'OC_NS=""; oc_live_fleet_guard')"
[[ "$rc" != "0" ]] && ok "live warden + canonical → guard dies (rc=$rc)" || bad "live warden + canonical → guard should die"
grep -q 'LIVE-FLEET GUARD' "$GLOG" && ok "die message names LIVE-FLEET GUARD" || bad "die message missing LIVE-FLEET GUARD marker"

# ── 2) no live fleet + CANONICAL → guard PASSES ──────────────────────────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" run_snippet 'OC_NS=""; oc_live_fleet_guard')"
check "no fleet + canonical → guard passes" "0" "$rc"

# ── 3) live warden + NAMESPACE mode → guard COEXISTS (passes) ─────────────────
rc="$(SHIM_WARDEN=1 run_snippet 'OC_NS="e2eabc123"; oc_live_fleet_guard')"
check "live warden + namespace → guard coexists (returns 0)" "0" "$rc"
grep -q 'coexist' "$GLOG" && ok "namespace-mode guard logs coexistence" || bad "namespace-mode guard should log coexistence"

# ── 4) detection fires on a member-* session on the canonical socket ──────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="member-m-abc123" \
      run_snippet 'oc_detect_live_canonical_fleet | grep -q "canonical tmux socket"')"
check "member-* on canonical socket is detected" "0" "$rc"

# ── 5) detection fires on a canonical-port listener (port from CANON_PORT) ───
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="$CANON_PORT" SHIM_SESSIONS="" \
      run_snippet "oc_detect_live_canonical_fleet | grep -q 'serve port $CANON_PORT'")"
check "canonical $CANON_PORT listener is detected" "0" "$rc"

# ── 6) detection is EMPTY on a clean host ────────────────────────────────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
      run_snippet 'out="$(oc_detect_live_canonical_fleet)"; [[ -z "$out" ]]')"
check "clean host → detection empty" "0" "$rc"

# ── 7) NAMESPACE allocation: every axis is non-canonical ─────────────────────
run_snippet 'oc_resolve_instance
  printf "NS=%s\n" "$OC_NS"
  printf "PORT=%s\n" "${LOCAL_BASE##*:}"
  printf "SERVE=%s\n" "$SERVE_LABEL"
  printf "WARDEN=%s\n" "$WARDEN_LABEL"
  printf "ROOT=%s\n" "$OC_ROOT"
  printf "SOCK=%s\n" "$TMUX_SOCKET_LOCAL"' >/dev/null
NS="$(grep '^NS=' "$GLOG" | cut -d= -f2)"
PORT="$(grep '^PORT=' "$GLOG" | cut -d= -f2)"
SERVE="$(grep '^SERVE=' "$GLOG" | cut -d= -f2)"
WARDEN="$(grep '^WARDEN=' "$GLOG" | cut -d= -f2)"
ROOT="$(grep '^ROOT=' "$GLOG" | cut -d= -f2)"
SOCK="$(grep '^SOCK=' "$GLOG" | cut -d= -f2)"
[[ "$NS" =~ ^[a-z0-9-]{1,16}$ ]] && ok "ns '$NS' matches product charset [a-z0-9-]{1,16}" || bad "ns '$NS' violates charset"
[[ "$PORT" != "$CANON_PORT" && "$PORT" != "8766" && "$PORT" != "8790" && "$PORT" != "8791" && "$PORT" != "8795" ]] \
  && ok "port $PORT is non-canonical/non-reserved" || bad "port $PORT collides with a reserved port"
[[ "$SERVE" == "com.officraft.serve.$NS" ]] && ok "serve label namespaced ($SERVE)" || bad "serve label wrong: $SERVE"
[[ "$WARDEN" == "com.officraft.ocwarden.$NS" && "$WARDEN" != "com.officraft.ocwarden" ]] \
  && ok "warden label namespaced ($WARDEN)" || bad "warden label wrong: $WARDEN"
[[ "$ROOT" == *"/.officraft-$NS" && "$ROOT" != *"/.officraft" ]] \
  && ok "root namespaced ($ROOT)" || bad "root wrong: $ROOT"
[[ "$SOCK" == "officraft-$NS" && "$SOCK" != "officraft" ]] \
  && ok "tmux socket namespaced ($SOCK)" || bad "socket wrong: $SOCK"

# ── 8) CANONICAL escape hatch: axes resolve to the canonical port ─────────────
# T-191d: the port the 0c guard verifies free (SINGLE_PROD_PORTS[0]) and the port
# this run actually OWNS (LOCAL_BASE/PUBLIC_HOST → oc_fresh_install pins serve to
# ${LOCAL_BASE##*:}) MUST be the SAME canonical port. The old bug: the canonical
# branch set SINGLE_PROD_PORTS to the dynamic CANON_PORT but left LOCAL_BASE at a
# hardcoded 8770, so the guard watched one port while the install bound another —
# and this test only checked SINGLE_PROD_PORTS, so it stayed green. Now assert the
# owned port too, AND that it equals the guarded port (the coupling invariant),
# against CANON_PORT (SSOT-derived, never a hardcoded literal of its own).
run_snippet 'export OC_E2E_ALLOW_CANONICAL=1; oc_resolve_instance
  printf "NS=[%s]\n" "$OC_NS"
  printf "PORTS=%s\n" "${SINGLE_PROD_PORTS[*]}"
  printf "GUARD0=%s\n" "${SINGLE_PROD_PORTS[0]}"
  printf "LB=%s\n" "${LOCAL_BASE##*:}"
  printf "PH=%s\n" "${PUBLIC_HOST##*:}"' >/dev/null
C8_GUARD0="$(grep '^GUARD0=' "$GLOG" | cut -d= -f2)"
C8_LB="$(grep '^LB=' "$GLOG" | cut -d= -f2)"
C8_PH="$(grep '^PH=' "$GLOG" | cut -d= -f2)"
[[ "$(grep '^NS=' "$GLOG")" == "NS=[]" ]] && ok "canonical escape hatch → OC_NS empty" || bad "canonical OC_NS not empty: $(grep '^NS=' "$GLOG")"
[[ "$(grep '^PORTS=' "$GLOG")" == "PORTS=$CANON_PORT 8766" ]] && ok "canonical guard ports = $CANON_PORT 8766" || bad "canonical ports wrong: $(grep '^PORTS=' "$GLOG")"
[[ "$C8_LB" == "$CANON_PORT" ]] && ok "canonical LOCAL_BASE port == $CANON_PORT (the port this run OWNS = current canonical)" || bad "canonical LOCAL_BASE port wrong: got '$C8_LB' want '$CANON_PORT' (T-191d: guard watches a port the run does not bind)"
[[ "$C8_PH" == "$CANON_PORT" ]] && ok "canonical PUBLIC_HOST port == $CANON_PORT" || bad "canonical PUBLIC_HOST port wrong: got '$C8_PH' want '$CANON_PORT'"
[[ -n "$C8_LB" && "$C8_LB" == "$C8_GUARD0" ]] && ok "canonical coupling: owned port (LOCAL_BASE $C8_LB) == guard port (SINGLE_PROD_PORTS[0] $C8_GUARD0)" || bad "canonical DECOUPLED: owned port '$C8_LB' != guard port '$C8_GUARD0' — the exact T-191d shape (guard verifies one port, install binds another)"

# ── 9) agent_workdir is namespace-aware (a1_zombie kill-anchor safety) ────────
rc="$(run_snippet 'OC_NS="e2ex"; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft-e2ex/agents/mira" ]]')"
check "agent_workdir namespaced under ns" "0" "$rc"
rc="$(run_snippet 'unset OC_NS; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft/agents/mira" ]]')"
check "agent_workdir canonical when ns unset (zero-diff)" "0" "$rc"

# ── 10) TRIPWIRE: no guard/allocator ever called launchctl bootout ───────────
if [[ -s "$TRIPWIRE" ]]; then bad "launchctl bootout was invoked: $(cat "$TRIPWIRE")"; else ok "no teardown/bootout invoked by any guard/allocator path"; fi

# ── 11) T-d41a: run_all.sh must still PRINT "[run_all] specs exit=<rc>" when a
#        spec fails. This is an OUTPUT assertion, on purpose: the bug it guards
#        is rc-blind. lib/common.sh used to `set -euo pipefail`, and because it
#        is SOURCED, the `-e` leaked into run_all.sh (which deliberately runs
#        `set -uo pipefail` so it can capture rc itself). Under the leaked `-e`
#        the failing playwright subshell killed the script BEFORE `RC=$?` and
#        the echo — the run still exited non-zero with the SAME code, so a
#        rc-only assertion stays green while the diagnostic line is gone.
#        "Failed for the wrong reason" and "correctly reported the failure"
#        share one exit code; only the output tells them apart.
#
#        Fidelity: the preamble (the `set -` line and the `source .../common.sh`
#        line) and the reporting tail (`RC=$?` + the echo) are lifted VERBATIM
#        from run_all.sh, so this reproduces the real interaction against the
#        real lib/common.sh. Only the playwright invocation is stood in for by a
#        subshell that exits 7 (hermetic: no browser, no server, no ports).
RUN_ALL="$HERE/../run_all.sh"
if [[ ! -f "$RUN_ALL" ]]; then
  bad "run_all.sh not found at $RUN_ALL"
else
  # Every one of these four locates a STATEMENT, so every pattern is anchored at
  # column 0 and shaped like the statement. An unanchored `-F` on the literal
  # would also match a COMMENT that merely mentions it, and then the fixture
  # below is reconstructed out of a comment: it echoes nothing and this case
  # fails naming lib/common.sh's `set -e`, which had nothing to do with it.
  # (Measured before this was anchored: one ordinary comment added to run_all.sh
  # mentioning the report line took tests_guard to PASS=152 FAIL=1 rc=1.)
  D41A_SET="$(grep -m1 -E '^set +-' "$RUN_ALL" || true)"
  D41A_SRC="$(grep -m1 -E '^source "\$HERE/lib/common\.sh"' "$RUN_ALL" || true)"
  D41A_RC="$(grep -m1 -E '^RC=\$\?' "$RUN_ALL" || true)"
  D41A_ECHO="$(grep -m1 -E '^echo "\[run_all\] specs exit=' "$RUN_ALL" || true)"
  if [[ -z "$D41A_SET" || -z "$D41A_SRC" || -z "$D41A_RC" || -z "$D41A_ECHO" ]]; then
    bad "run_all.sh no longer has the expected set/source/RC/echo shape — update guard (11)"
  else
    D41A_SH="$SHIMDIR/d41a_run_all_shape.sh"
    {
      echo '#!/usr/bin/env bash'
      echo "$D41A_SET"
      printf 'HERE=%q\n' "$(cd "$HERE/.." && pwd)"
      echo "$D41A_SRC"
      echo '( exit 7 )   # stand-in for the failing `npx playwright test` subshell'
      echo "$D41A_RC"
      echo "$D41A_ECHO"
      echo 'exit $RC'
    } > "$D41A_SH"
    D41A_OUT="$(bash "$D41A_SH" 2>&1)"; D41A_EXIT=$?
    if [[ "$D41A_OUT" == *"[run_all] specs exit=7"* ]]; then
      ok "spec failure still PRINTS '[run_all] specs exit=7' (sourcing common.sh leaks no -e)"
    else
      bad "spec-failure report line MISSING — got output '$D41A_OUT' (rc=$D41A_EXIT). \
lib/common.sh likely re-enabled 'set -e'; it is SOURCED, so -e leaks into run_all.sh \
and kills it before RC=\$? — same exit code, no diagnostic line."
    fi
    # Secondary (NOT the headline): the rc must still propagate. Deliberately
    # asserted after the output check so the output regression is what reddens.
    check "spec failure rc still propagates through run_all.sh" "7" "$D41A_EXIT"
    # And the sourced lib must not silently re-arm errexit in a non -e caller.
    rc="$(bash -c 'set -uo pipefail; source "$1" >/dev/null 2>&1; case $- in *e*) exit 1;; *) exit 0;; esac' _ "$HERE/../lib/common.sh"; echo $?)"
    check "sourcing lib/common.sh does not turn on errexit in a non-'-e' caller" "0" "$rc"
    # Converse: a caller that DID ask for -e must keep it (setup.sh et al).
    rc="$(bash -c 'set -euo pipefail; source "$1" >/dev/null 2>&1; case $- in *e*) exit 0;; *) exit 1;; esac' _ "$HERE/../lib/common.sh"; echo $?)"
    check "sourcing lib/common.sh preserves errexit for callers that set it" "0" "$rc"

    # ADJACENCY (static, complements the behavioural check above). The synthetic
    # script builds the tail adjacent BY CONSTRUCTION, so it is blind to someone
    # inserting a command between `npx playwright test` and `RC=$?` in the real
    # file. `$?` is clobbered by ANY intervening command, so a single line slipped
    # in there silently reports the WRONG rc — the line still prints, so the
    # behavioural assertion stays green. Hence a textual adjacency assertion on
    # the real run_all.sh. Comments/blank lines are NOT tolerated between them:
    # they are harmless to `$?` today, but permitting them is what makes room for
    # a command to be added later without anything reddening.
    D41A_PWLINE="$(grep -nE '^\(.*playwright test *\)' "$RUN_ALL" | head -1 | cut -d: -f1)"
    if [[ -z "$D41A_PWLINE" ]]; then
      bad "cannot locate the 'npx playwright test' line in run_all.sh — update guard (11)"
    else
      D41A_NEXT="$(sed -n "$((D41A_PWLINE+1))p" "$RUN_ALL")"
      D41A_NEXT2="$(sed -n "$((D41A_PWLINE+2))p" "$RUN_ALL")"
      [[ "$D41A_NEXT" =~ ^RC=\$\? ]] \
        && ok "RC=\$? is IMMEDIATELY after the playwright run (rc not clobbered)" \
        || bad "line after 'playwright test' is '$D41A_NEXT', expected 'RC=\$?' — anything in between clobbers \$? and run_all.sh reports the WRONG exit code while still printing the line"
      [[ "$D41A_NEXT2" == *'[run_all] specs exit=$RC'* ]] \
        && ok "the report echo immediately follows RC=\$?" \
        || bad "line after 'RC=\$?' is '$D41A_NEXT2', expected the '[run_all] specs exit=\$RC' echo"
    fi
  fi
fi

# ── 12) T-c5d4 weakness-2: webdist restore must SURFACE a failed/partial delete,
#        not swallow it. teardown.sh used `find … -delete 2>/dev/null` with no rc
#        check — a silent failure leaves a dirty webdist that a later `go build`
#        bakes into the committed bin/ocserverd. oc_restore_webdist_pristine now
#        checks find's rc AND re-asserts only .gitkeep remains, printing a loud
#        WARN on trouble. OUTPUT+rc assertion on purpose: a fail-closed cleanup is
#        rc-blind to a half-delete, so we assert the reason/output, not only rc.
TEARDOWN="$HERE/../teardown.sh"
if ! grep -q 'oc_restore_webdist_pristine' "$TEARDOWN"; then
  bad "teardown.sh no longer calls oc_restore_webdist_pristine — update guard (12)"
elif grep -Eq 'find .*-delete.*2>/dev/null' "$TEARDOWN"; then
  bad "teardown.sh reintroduced 'find … -delete 2>/dev/null' — the stderr swallow that hid the failure (weakness-2)"
else
  ok "teardown.sh delegates webdist cleanup to oc_restore_webdist_pristine, no stderr swallow"
  # positive control: clean, fully-removable content restores quietly, rc 0.
  WT_POS="$(mktemp -d -t oc-webdist-pos.XXXXXX)"
  touch "$WT_POS/.gitkeep" "$WT_POS/index.html"; mkdir -p "$WT_POS/assets"; touch "$WT_POS/assets/app.js"
  POS_OUT="$( ( source "$HERE/../lib/common.sh" >/dev/null 2>&1; oc_restore_webdist_pristine "$WT_POS" ) 2>&1 )"; POS_RC=$?
  check "webdist restore: clean dir returns 0" "0" "$POS_RC"
  POS_LEFT="$(find "$WT_POS" -mindepth 1 -not -name '.gitkeep' | wc -l | tr -d ' ')"
  check "webdist restore: clean dir leaves only .gitkeep" "0" "$POS_LEFT"
  case "$POS_OUT" in
    *WARN*) bad "webdist restore: clean dir must NOT warn (got: $POS_OUT)" ;;
    *restored*) ok "webdist restore: clean dir prints 'restored', no WARN (positive control)" ;;
    *) bad "webdist restore: clean dir unexpected output: $POS_OUT" ;;
  esac
  rm -rf "$WT_POS"
  # negative control: an entry -delete CANNOT remove (dir chmod 000 → EACCES) —
  # the exact failure the old 2>/dev/null swallowed. NOTE: assumes a non-root
  # runner (ci.sh runs as the developer); as root -delete would succeed and this
  # case would REDDEN (fail-closed, never a false green).
  WT_NEG="$(mktemp -d -t oc-webdist-neg.XXXXXX)"
  touch "$WT_NEG/.gitkeep"; mkdir -p "$WT_NEG/locked"; touch "$WT_NEG/locked/app.js"; chmod 000 "$WT_NEG/locked"
  NEG_OUT="$( ( source "$HERE/../lib/common.sh" >/dev/null 2>&1; oc_restore_webdist_pristine "$WT_NEG" ) 2>&1 )"; NEG_RC=$?
  chmod 755 "$WT_NEG/locked" 2>/dev/null || true
  check "webdist restore: un-removable entry returns 1 (not swallowed)" "1" "$NEG_RC"
  case "$NEG_OUT" in
    *WARN*) ok "webdist restore: a FAILED delete emits a loud WARN (weakness-2 mutant reddens)" ;;
    *) bad "webdist restore: FAILED delete produced NO warn — the silent-failure bug (got: $NEG_OUT)" ;;
  esac
  rm -rf "$WT_NEG" 2>/dev/null || true
fi

# ── 13) T-191d: a1_zombie's post-teardown "port freed" corroboration must probe
#        the port THIS run OWNED (derived from LOCAL_BASE), not a hardcoded literal.
#        A literal we never bound (the retired 8770) makes the check vacuous — it
#        stays green even when teardown leaks our real listener (retired port is
#        always free). Static assertion on the real a1_zombie_e2e.sh: the owned
#        port must be derived from LOCAL_BASE AND the clean-slate lsof must probe
#        it — reverting to a hardcoded :<port> drops both and reddens here.
A1="$HERE/../a1_zombie_e2e.sh"
if [[ ! -f "$A1" ]]; then
  bad "a1_zombie_e2e.sh not found at $A1 — update guard (13)"
else
  if grep -Fq 'owned_port="${LOCAL_BASE##*:}"' "$A1"; then
    ok "a1_zombie derives owned_port from LOCAL_BASE (not a hardcoded literal)"
  else
    bad "a1_zombie no longer derives owned_port from LOCAL_BASE — post-teardown port check may have re-hardcoded a literal (T-191d regression)"
  fi
  if grep -Fq 'lsof -nP -iTCP:"$owned_port" -sTCP:LISTEN' "$A1"; then
    ok "a1_zombie post-teardown lsof probes the OWNED port (\${LOCAL_BASE##*:}), not a stale constant"
  else
    bad "a1_zombie post-teardown lsof no longer probes \"\$owned_port\" — a vacuous hardcoded-port check would stay green when teardown leaks the real listener (T-191d)"
  fi
fi

# ── 14) T-191d(E): cross_machine.sh's LOCAL_BASE default must be the CURRENT
#        canonical serve port (SSOT-derived), never a literal.
#        cross_machine.sh is CANONICAL BY CONSTRUCTION — it does NOT call
#        oc_resolve_instance (so case (8) structurally cannot see it; that is
#        exactly how this site survived the core package) — and it PINS the
#        seeded oc.toml's serve port to ${LOCAL_BASE##*:}. A stale literal there
#        therefore makes the run BIND one port while oc_lifecycle.sh's live-fleet
#        guard watches OC_CANONICAL_SERVE_PORT: the guard clears a port nobody
#        binds. BEHAVIOURAL, not a grep-for-a-string: the real assignment line is
#        lifted verbatim out of cross_machine.sh and EVALUATED with the real lib
#        sourced, so what is asserted is the value the script actually computes.
CM="$HERE/../cross_machine.sh"
if [[ ! -f "$CM" ]]; then
  bad "cross_machine.sh not found at $CM — update guard (14)"
else
  CM_LINE="$(grep -m1 -E '^LOCAL_BASE="\$\{LOCAL_BASE:-' "$CM" || true)"
  if [[ -z "$CM_LINE" ]]; then
    bad "cross_machine.sh no longer has a 'LOCAL_BASE=\"\${LOCAL_BASE:-…}\"' default line — update guard (14)"
  else
    run_snippet 'unset LOCAL_BASE
'"$CM_LINE"'
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_LB="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    [[ -n "$C14_LB" && "$C14_LB" == "$CANON_PORT" ]] \
      && ok "cross_machine LOCAL_BASE default port == $CANON_PORT (SSOT-derived, evaluated from the real line)" \
      || bad "cross_machine LOCAL_BASE default port is '$C14_LB', want '$CANON_PORT' — a hardcoded/retired literal makes the canonical run BIND a port the live-fleet guard is not watching (T-191d E)"
    # Discriminating control: prove the assertion above CAN fail. Push the
    # pre-fix shape through the identical evaluation path and require it to
    # disagree with CANON_PORT — otherwise case (14) would be vacuous.
    run_snippet 'unset LOCAL_BASE
      LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8770}"
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_CTL="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    [[ "$C14_CTL" == "8770" && "$C14_CTL" != "$CANON_PORT" ]] \
      && ok "control: pre-fix shape evaluates to 8770 != $CANON_PORT (case 14 can actually redden)" \
      || bad "control broken: pre-fix shape evaluated to '$C14_CTL' — case (14) may be vacuous"
    # SENTINEL: an explicit LOCAL_BASE= override must still win. fail-closed
    # must be ACCURATE, not merely wide — the legitimate override path is the
    # only way an operator points this at a namespaced/second instance.
    run_snippet 'LOCAL_BASE="http://127.0.0.1:8799"
'"$CM_LINE"'
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_OVR="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    check "sentinel: explicit LOCAL_BASE override still wins in cross_machine" "8799" "$C14_OVR"
  fi
fi

# ── 15/16) T-191d(D): the TWO prod-port REFUSAL LISTS must cover the CURRENT
#        prod port, derived from the SSOT (server/ocserverd/config.go's
#        defaultPort) — not only retired literals.
#
#        WHY THIS IS THE IMPORTANT ONE: prod is live on the canonical port right
#        now. These harnesses are DESTRUCTIVE (setup/teardown kill listeners and
#        wipe state). A refusal list that enumerates only RETIRED ports is GREEN
#        while protecting nothing: an operator who sets OC_E2E_PORT /
#        OC_CONF_PORT to the CURRENT prod port walks straight into the live
#        station and the guard never speaks. T-a3ba (56f47bc) fixed the code in
#        both files but shipped NO test — nothing anywhere reddened if the
#        SSOT-derived entry were dropped again. These cases are that test.
#
#        The two sites are asserted SEPARATELY on purpose: one shared assertion
#        would redden when EITHER drifts, which actively HIDES the other site
#        being uncovered.
#
#        BEHAVIOURAL: both files are really executed. Neither reaches a side
#        effect — common.sh is pure assignment, and conformance/run.sh's refusal
#        loop sits before the venv/build/bind steps and exits 2 there.
C15_COMMON="$HERE/../lib/common.sh"
C16_CONF="$HERE/../../conformance/run.sh"

# helper: source common.sh with a given OC_E2E_PORT, capture combined output.
c15_run() { OC_E2E_PORT="$1" bash -c 'source "$1" >/dev/null' _ "$C15_COMMON" 2>&1; }

if [[ ! -f "$C15_COMMON" ]]; then
  bad "lib/common.sh not found at $C15_COMMON — update guard (15)"
else
  # (15a) CURRENT prod port refused — reddens iff common.sh's list loses its
  #       SSOT-derived entry. This is the safety hole itself.
  case "$(c15_run "$CANON_PORT")" in
    *"is a PROD port"*) ok "common.sh REFUSES the CURRENT prod port ($CANON_PORT, SSOT-derived)" ;;
    *) bad "common.sh ACCEPTED OC_E2E_PORT=$CANON_PORT — the live prod port. The 'never touch prod' guard is blind to the only port prod actually uses (T-191d D)" ;;
  esac
  # (15b) retired ports stay refused — this is an ADD, not a REPLACE. Some
  #       install may still have 8770 pinned in its oc.toml.
  case "$(c15_run 8770)" in
    *"is a PROD port"*) ok "common.sh still refuses the RETIRED 8770 (added to, not swapped for, the SSOT entry)" ;;
    *) bad "common.sh no longer refuses 8770 — retired defaults must STAY in the list (an install may still pin one)" ;;
  esac
  # (15c) SENTINEL: the legitimate isolated port must still be allowed. A guard
  #       that refuses everything is not safer, it is just broken — this repo
  #       has already shipped an over-wide fail-closed once.
  C15_OK_OUT="$(c15_run 8791)"
  case "$C15_OK_OUT" in
    *"is a PROD port"*) bad "sentinel BROKEN: common.sh refused the legitimate isolated port 8791 — fail-closed must be accurate, not wide" ;;
    *) ok "sentinel: common.sh still ACCEPTS the legitimate isolated port 8791" ;;
  esac
  # (15d) no SILENT degradation: if the SSOT cannot be parsed, common.sh must
  #       FATAL. Degrading to an empty/partial list would delete the guard while
  #       looking exactly like a healthy run. Executed against a throwaway tree
  #       with no server/ocserverd/config.go.
  C15_T="$(mktemp -d -t oc-guard-nossot.XXXXXX)"
  mkdir -p "$C15_T/e2e_test/lib"
  cp "$C15_COMMON" "$C15_T/e2e_test/lib/common.sh"
  C15_NOSSOT="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null' _ "$C15_T/e2e_test/lib/common.sh" 2>&1)"
  case "$C15_NOSSOT" in
    *"could not parse"*) ok "common.sh FATALs when the SSOT (config.go defaultPort) is unparseable — no silent empty prod-port list" ;;
    *) bad "common.sh did NOT fatal with an unparseable SSOT (got: ${C15_NOSSOT:-<silence>}) — a silently empty PROD_PORTS is a guard that vanished while staying green (T-191d D)" ;;
  esac
  rm -rf "$C15_T" 2>/dev/null || true
fi

if [[ ! -f "$C16_CONF" ]]; then
  bad "conformance/run.sh not found at $C16_CONF — update guard (16)"
else
  # Stubs so the SENTINEL run below cannot proceed past the refusal gate into
  # venv creation / go build / port bind. EVERY stub emits the same marker, so
  # the sentinel proves "the gate was passed" no matter which post-gate step the
  # run happens to reach first.
  #
  # Why all three: which step comes first is ENVIRONMENT-DEPENDENT. With no
  # conformance/.venv the run tries `uv`/`python3` (line ~106); with a .venv
  # already present (e.g. a previous bin/ci.sh run in this same tree) it skips
  # straight to the `lsof` leftover-guard (line ~130). Stubbing only the venv
  # pair made this case flip to INCONCLUSIVE the moment CI had run here once.
  # All of these sit strictly AFTER the prod-port refusal loop, so reaching any
  # of them is proof the legitimate port was let through.
  C16_SHIM="$(mktemp -d -t oc-guard-conf.XXXXXX)"
  for _c in uv python3; do
    printf '#!/usr/bin/env bash\necho "SENTINEL_PAST_PROD_GATE" >&2\nexit 1\n' > "$C16_SHIM/$_c"
    chmod +x "$C16_SHIM/$_c"
  done
  # lsof: exit 0 = "port occupied" so run.sh stops at its leftover guard. This
  # stub CANNOT emit the marker — run.sh calls it as `lsof … >/dev/null 2>&1`,
  # which swallows both streams — so the evidence for that path is run.sh's own
  # post-gate "already in use" FATAL instead. Accepted below.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$C16_SHIM/lsof"
  chmod +x "$C16_SHIM/lsof"
  c16_run() { OC_CONF_PORT="$1" PATH="$C16_SHIM:$PATH" SHIM_LISTEN_PORTS="$1" \
                bash "$C16_CONF" --target go 2>&1; }
  # (16a) CURRENT prod port refused — the twin of (15a), asserted independently.
  case "$(c16_run "$CANON_PORT")" in
    *"is a PROD port"*) ok "conformance/run.sh REFUSES the CURRENT prod port ($CANON_PORT, SSOT-derived)" ;;
    *) bad "conformance/run.sh ACCEPTED OC_CONF_PORT=$CANON_PORT — the live prod port; its refusal list is blind to prod (T-191d D)" ;;
  esac
  # (16b) retired ports stay refused (ADD, not REPLACE).
  case "$(c16_run 8770)" in
    *"is a PROD port"*) ok "conformance/run.sh still refuses the RETIRED 8770" ;;
    *) bad "conformance/run.sh no longer refuses 8770 — retired defaults must STAY in the list" ;;
  esac
  # (16c) SENTINEL: the legitimate conformance port must still get THROUGH the
  #       gate (proved by reaching the stubbed venv step), not be refused.
  C16_OK_OUT="$(c16_run 8795)"
  case "$C16_OK_OUT" in
    *"is a PROD port"*) bad "sentinel BROKEN: conformance/run.sh refused explicit non-prod override 8795" ;;
    *"SENTINEL_PAST_PROD_GATE"*|*"already in use"*) ok "sentinel: conformance/run.sh lets explicit non-prod override 8795 through the prod gate" ;;
    *) bad "sentinel INCONCLUSIVE: 8795 was not refused, but the run never reached the post-gate step — this case may no longer be testing what it claims (got: ${C16_OK_OUT:-<silence>})" ;;
  esac
  # (16d) no SILENT degradation on an unparseable SSOT (twin of 15d). Also pins
  #       the `|| true` that keeps this from dying at the assignment under -e.
  C16_T="$(mktemp -d -t oc-guard-confnossot.XXXXXX)"
  mkdir -p "$C16_T/conformance"
  cp "$C16_CONF" "$C16_T/conformance/run.sh"
  C16_NOSSOT="$(OC_CONF_PORT=8795 bash "$C16_T/conformance/run.sh" --target go 2>&1)"
  case "$C16_NOSSOT" in
    *"could not parse"*) ok "conformance/run.sh FATALs (and SPEAKS) when the SSOT is unparseable — no silent empty refusal list" ;;
    *) bad "conformance/run.sh did NOT print its parse FATAL with an unparseable SSOT (got: ${C16_NOSSOT:-<silence>}) — the guard died silently at the assignment or degraded to an empty list (T-191d D / T-a3ba F2)" ;;
  esac
  # (16e) T-0e4b: the DEFAULT. With OC_CONF_PORT UNSET the port handed to the
  #       daemon must be 0, so the KERNEL allocates it at bind time — that is
  #       the whole reason two conformance runs can now go in parallel. Every
  #       other case here (16a-16d, 15*) pins an EXPLICIT OC_CONF_PORT, so all
  #       of them stay green no matter what the default is: before this case,
  #       reverting the default to a fixed port left the entire suite green and
  #       only a hand-run CONCURRENT pair could tell (CI never runs one).
  #
  #       Asserted on the port run.sh actually hands the daemon — the `port =`
  #       it writes into the throwaway oc.toml that ocserverd binds from
  #       (cfg.Server.Port → net.Listen, server.go's cmdServe) — NOT on run.sh's
  #       source text. A grep for ":-0}" would pin the SPELLING, and would go
  #       silently vacuous the first time someone rewrote the expression.
  #
  #       NOTHING is compiled and NOTHING is bound, so this costs about as much
  #       as 16a-16d: a throwaway tree gets a fake suite venv plus the three
  #       embed-staging sentinels (so build-seedsdist/docsdist/bindist all skip),
  #       and a `go` shim whose "build" emits a stub ocserverd instead of
  #       compiling one. run.sh's FIRST use of that binary is `migrate`, which is
  #       already after the oc.toml write — the stub records the port it was
  #       handed and dies there, so the run never reaches serve/pytest.
  #
  #       Reverting the default to a fixed literal turns this red two ways, and
  #       both are FAILs: normally the recorded port IS that literal; and if a
  #       real listener happens to hold it, run.sh's leftover guard exits first
  #       and NO port is recorded at all — a missing record is never a skip here.
  C16E_T="$(mktemp -d -t oc-guard-confdefault.XXXXXX)"
  C16E_SHIM="$(mktemp -d -t oc-guard-confdefshim.XXXXXX)"
  C16E_SEEN="$C16E_T/handed-port"
  mkdir -p "$C16E_T/conformance/.venv/bin" \
           "$C16E_T/server/ocserverd/seedsdist" \
           "$C16E_T/server/ocserverd/docsdist" \
           "$C16E_T/server/ocserverd/bindist"
  cp "$C16_CONF" "$C16E_T/conformance/run.sh"
  # The prod-port refusal list is parsed out of config.go (the SSOT) — give the
  # throwaway tree the REAL one so the gate behaves as it does in a checkout.
  cp "$HERE/../../server/ocserverd/config.go" "$C16E_T/server/ocserverd/config.go"
  : > "$C16E_T/server/ocserverd/seedsdist/stub.md"
  : > "$C16E_T/server/ocserverd/docsdist/stub.md"
  : > "$C16E_T/server/ocserverd/bindist/ocwarden"
  # Fake suite venv: satisfies both the `-x` test and the `import pytest, httpx`
  # probe, so run.sh neither creates a venv nor installs anything.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$C16E_T/conformance/.venv/bin/python"
  chmod +x "$C16E_T/conformance/.venv/bin/python"
  # The stub "ocserverd": report the port our caller wrote into $OC_CONFIG for us
  # to bind, then fail so run.sh stops at its first use of us (migrate).
  cat > "$C16E_SHIM/ocserverd-stub" <<'SH'
#!/usr/bin/env bash
grep -Eo '^[[:space:]]*port[[:space:]]*=[[:space:]]*[0-9]+' "${OC_CONFIG:-/dev/null}" \
  | grep -oE '[0-9]+' | head -1 > "$C16E_SEEN_PATH"
exit 1
SH
  chmod +x "$C16E_SHIM/ocserverd-stub"
  # `go` shim: only `build -o <path> .` matters — emit the stub, never compile.
  cat > "$C16E_SHIM/go" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == "build" ]]; then
  out=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "-o" ]]; then out="${2:-}"; shift; fi
    shift
  done
  [[ -n "$out" ]] || exit 1
  cp "$C16E_STUB_PATH" "$out"
  exit 0
fi
exit 0
SH
  chmod +x "$C16E_SHIM/go"
  : > "$C16E_SEEN"
  # `env -u OC_CONF_PORT` — the whole point is the UNSET default, so strip it
  # even if this host's environment happens to carry one.
  C16E_OUT="$(env -u OC_CONF_PORT PATH="$C16E_SHIM:$PATH" \
                C16E_SEEN_PATH="$C16E_SEEN" C16E_STUB_PATH="$C16E_SHIM/ocserverd-stub" \
                bash "$C16E_T/conformance/run.sh" --target go 2>&1)"
  C16E_PORT="$(tr -d '[:space:]' < "$C16E_SEEN" 2>/dev/null || true)"
  case "$C16E_PORT" in
    0) ok "conformance/run.sh's DEFAULT (OC_CONF_PORT unset) hands the daemon port 0 — the kernel allocates at bind, so concurrent runs cannot contend (T-0e4b)" ;;
    "") bad "conformance/run.sh never got as far as handing the daemon a port with OC_CONF_PORT unset, so this case cannot vouch for the default — treat as red, not skipped (run said: ${C16E_OUT:-<silence>})" ;;
    *) bad "conformance/run.sh's DEFAULT handed the daemon FIXED port $C16E_PORT, not 0 — a hardcoded default serialises the suite: two concurrent runs contend for that one port and the second dies on the leftover guard (T-0e4b)" ;;
  esac
  rm -rf "$C16_T" "$C16_SHIM" "$C16E_T" "$C16E_SHIM" 2>/dev/null || true
fi

# ── 17) T-191d: teardown.sh's closing "prod — untouched" reassurance must NAME
#        the port prod is actually on, derived from the SSOT.
#        This is the MESSAGE-level form of the (15)/(16) defect: the line used to
#        read "prod :8770/:8766 — not managed by this harness (untouched)", which
#        named a RETIRED officraft default and a foreign product's port while the
#        live one went unmentioned. An operator reading it was told the real
#        station had been spared by a sentence that had never heard of it — a
#        reassurance pointing at the wrong port is worse than no reassurance.
#        BEHAVIOURAL: the real echo line is lifted VERBATIM from teardown.sh and
#        EVALUATED with the real lib/common.sh sourced, so what is asserted is
#        the string the operator actually sees. Evaluating one echo has no side
#        effects — none of teardown.sh's kill/rm steps are reached.
TD="$HERE/../teardown.sh"
if [[ ! -f "$TD" ]]; then
  bad "teardown.sh not found at $TD — update guard (17)"
else
  TD_LINE="$(grep -m1 -E '^echo "\[teardown\] prod ' "$TD" || true)"
  if [[ -z "$TD_LINE" ]]; then
    bad "teardown.sh no longer has an 'echo \"[teardown] prod …\"' line — update guard (17) (or the operator lost the reassurance entirely)"
  else
    C17_OUT="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null 2>&1; '"$TD_LINE" _ "$C15_COMMON" 2>&1)"
    case "$C17_OUT" in
      *":$CANON_PORT"*|*" $CANON_PORT"*)
        ok "teardown.sh's 'prod untouched' line NAMES the current prod port ($CANON_PORT, SSOT-derived)" ;;
      *) bad "teardown.sh's 'prod untouched' line does NOT name the current prod port $CANON_PORT (got: ${C17_OUT:-<silence>}) — reassurance that names only retired ports tells the operator the live station was spared without ever mentioning it (T-191d)" ;;
    esac
    case "$C17_OUT" in
      *8770*) ok "teardown.sh's line still lists the RETIRED 8770 (added to, not swapped for, the current port)" ;;
      *) bad "teardown.sh's line dropped the retired 8770 — retired ports stay listed (got: $C17_OUT)" ;;
    esac
    # Discriminating control: the pre-fix literal, pushed through the identical
    # evaluation path, must NOT satisfy the assertion above — else (17) is vacuous.
    C17_CTL="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null 2>&1; echo "[teardown] prod :8770/:8766 — not managed by this harness (untouched)"' _ "$C15_COMMON" 2>&1)"
    case "$C17_CTL" in
      *"$CANON_PORT"*) bad "control broken: the pre-fix literal line already contains $CANON_PORT — case (17) may be vacuous" ;;
      *) ok "control: the pre-fix literal line never mentions $CANON_PORT (case 17 can actually redden)" ;;
    esac
  fi
fi

# ── 18) T-2257: namespaced teardown must propagate OC_NAMESPACE to ocwarden ─
#
# `oc_resolve_instance` correctly derived a namespaced label/root, but the
# lifecycle helper then ran bare `ocwarden teardown`.  A child process without
# OC_NAMESPACE silently resolves the canonical label and token, so a harmless
# E2E cleanup could unload the live fleet's warden.  This invokes the REAL
# oc_teardown_bounded call chain, but every mutation is shimmed above.  The
# recording shims are tripwires: canonical launchd label, canonical root, or
# canonical token in any recorded target turns the test red.  The direct child
# env assertion is deliberately what makes removing propagation red, rather
# than merely checking the parent's OC_NS variable.
TEARDOWN_LOG="$SHIMDIR/.teardown-log"
: > "$TEARDOWN_LOG"
TEST_HOME="$SHIMDIR/ns-teardown-home"
export SHIM_ALLOW_TEARDOWN=1 SHIM_TEARDOWN_LOG="$TEARDOWN_LOG" TEST_HOME
rc="$(run_snippet '
  OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
  oc_resolve_instance
  HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
  # SHIMDIR itself is intentionally not exported to the hermetic child; resolve
  # the fake through the exported PATH exactly as the harness resolves tools.
  OCWARDEN="$(command -v ocwarden)"
  mkdir -p "$HOME/.officraft/warden" "$OC_ROOT/warden"
  printf canonical > "$HOME/.officraft/warden/exec-warden.tok"
  printf isolated > "$OC_ROOT/warden/exec-warden.tok"
  oc_teardown_bounded "namespace-regression"
')"
check "namespaced teardown helper completes through hermetic shims" "0" "$rc"

if grep -Fqx 'ocwarden namespace=e2eproof args=teardown' "$TEARDOWN_LOG"; then
  ok "namespaced teardown passes OC_NAMESPACE=e2eproof to the ocwarden child"
else
  bad "namespaced teardown did NOT pass its namespace to ocwarden (log: $(tr '\n' '|' < "$TEARDOWN_LOG")) — bare teardown falls back to canonical warden"
fi

if grep -Eq 'com\.officraft\.ocwarden([[:space:]]|$)' "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical warden label (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical warden label"
fi
if grep -Fq "$TEST_HOME/.officraft/warden/exec-warden.tok" "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical warden token (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical warden token"
fi
if grep -Fq "$TEST_HOME/.officraft/" "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical officraft root (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical officraft root"
fi
# This one does NOT test the recording shims — it catches a SHIM BYPASS: code
# that deletes through an absolute /bin/rm (or any path that dodges $PATH) never
# reaches the recorder above, so the three log tripwires would stay silent while
# the file really vanished. The sentinel is the only assertion that survives
# that class of escape. It is NOT the tripwire for MUT-D — (18c) is.
if [[ -f "$TEST_HOME/.officraft/warden/exec-warden.tok" ]] \
   && [[ "$(cat "$TEST_HOME/.officraft/warden/exec-warden.tok")" == "canonical" ]]; then
  ok "canonical token sentinel remains intact after namespaced teardown (no shim bypass)"
else
  bad "canonical token sentinel was changed or removed by namespaced teardown"
fi

# ── 18c) PERMANENT NEGATIVE CONTROL for (18): the tripwires must actually fire ─
#
# The tripwires above are grep-for-absence assertions: they pass when the log is
# EMPTY, which is also what a broken recorder produces. That is not a theory —
# the shim's redirection was wrong on arrival (only the last of three printfs was
# redirected, so every entry was a bare 0x0a) and all three tripwires plus the
# sentinel were structurally incapable of failing. The suite reported 56/56 while
# testing nothing.
#
# So: replay the literal 2026-07-25 incident. A mutated copy of the lib gets the
# two incident deletions injected into oc_teardown_bounded —
#   rm -f  "$HOME_DIR/.officraft/warden/exec-warden.tok"
#   rm -rf "$HOME_DIR/.officraft/warden"
# — and the SAME tripwire greps are then required to MATCH. If (18)'s recorder
# ever regresses, this case reddens immediately.
# The mutant lives in a MIRROR TREE (e2e_test/lib/ under a scratch root whose
# server/ is symlinked to the real one) because the lib derives its repo root
# from BASH_SOURCE and FATALs when it cannot parse config.go's defaultPort.
MUTROOT="$SHIMDIR/mutd-tree"
mkdir -p "$MUTROOT/e2e_test/lib"
ln -sfn "$HERE/../../server" "$MUTROOT/server"
MUTLIB="$MUTROOT/e2e_test/lib/oc_lifecycle.sh"
awk '
  /^oc_teardown_bounded\(\)/ { inbounded = 1 }
  { print }
  inbounded && !injected && /^  oc_assert_teardown_instance$/ {
    print "  rm -f \"$HOME_DIR/.officraft/warden/exec-warden.tok\""
    print "  rm -rf \"$HOME_DIR/.officraft/warden\""
    injected = 1
  }
  END { if (!injected) exit 3 }
' "$LIB" > "$MUTLIB"
if [[ $? -ne 0 ]]; then
  bad "could not build the MUT-D mutant: the 'oc_assert_teardown_instance' anchor inside oc_teardown_bounded moved — update guard (18c)"
else
  MUT_LOG="$SHIMDIR/.teardown-log-mutd"
  : > "$MUT_LOG"
  MUT_HOME="$SHIMDIR/mutd-home"
  rc="$(SNIPPET_LIB="$MUTLIB" SHIM_TEARDOWN_LOG="$MUT_LOG" TEST_HOME="$MUT_HOME" run_snippet '
    OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
    oc_resolve_instance
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OCWARDEN="$(command -v ocwarden)"
    mkdir -p "$HOME/.officraft/warden" "$OC_ROOT/warden" "$HOME/backups"
    printf canonical > "$HOME/.officraft/warden/exec-warden.tok"
    oc_teardown_bounded "mutd-negative-control"
  ')"
  check "MUT-D control: the mutated teardown still completes (mutation is reachable)" "0" "$rc"
  [[ "$rc" == "0" ]] || { echo "  ---- MUT-D control GLOG ----"; cat "$GLOG"; }
  if grep -Fq "$MUT_HOME/.officraft/warden/exec-warden.tok" "$MUT_LOG"; then
    ok "MUT-D control: the canonical-token tripwire FIRES on the 2026-07-25 incident (grep is not vacuous)"
  else
    bad "MUT-D control: the canonical-token tripwire stayed SILENT while the incident deletion ran (log: $(tr '\n' '|' < "$MUT_LOG")) — the rm recorder is broken again and case (18) is testing nothing"
  fi
  if grep -Fq "$MUT_HOME/.officraft/" "$MUT_LOG"; then
    ok "MUT-D control: the canonical-root tripwire FIRES on the 2026-07-25 incident"
  else
    bad "MUT-D control: the canonical-root tripwire stayed SILENT while 'rm -rf ~/.officraft/warden' ran (log: $(tr '\n' '|' < "$MUT_LOG")) — case (18) is vacuous"
  fi
fi

# ── 18d/18e) oc_assert_teardown_instance must actually GATE both call sites ────
#
# The guard shipped with no failing-without-it coverage: replacing BOTH of its
# call sites with `:` left the suite fully green, so the one thing standing
# between a stale variable and the canonical warden was untested. These two cases
# drive a MIXED axis set (namespace selected, but WARDEN_LABEL/OC_ROOT still
# canonical — exactly the shape the 2026-07-25 incident had) through each entry
# point separately, and require it to DIE before any mutation. Asserted per call
# site on purpose: one combined case would stay green while either site lost its
# guard.
c18_mixed() { # c18_mixed NAME ENTRYPOINT
  local log="$SHIMDIR/.teardown-log-$1" entry="$2"
  : > "$log"
  SHIM_TEARDOWN_LOG="$log" TEST_HOME="$SHIMDIR/mixed-home-$1" run_snippet '
    OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
    oc_resolve_instance
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OCWARDEN="$(command -v ocwarden)"
    mkdir -p "$HOME/backups" "$OC_ROOT/warden"
    # STALE canonical axes left behind by a partial/aborted resolve.
    WARDEN_LABEL="com.officraft.ocwarden"
    OC_ROOT="$HOME/.officraft"
    '"$entry"
}
for _entry in 'oc_teardown_bounded "mixed-axes"' 'oc_teardown_warden'; do
  _name="${_entry%% *}"
  rc="$(c18_mixed "$_name" "$_entry")"
  if [[ "$rc" != "0" ]]; then
    ok "$_name DIES on a namespaced run whose WARDEN_LABEL/OC_ROOT are still canonical (rc=$rc)"
  else
    bad "$_name ACCEPTED a namespaced run with canonical WARDEN_LABEL/OC_ROOT — the teardown target guard is absent or bypassed at this call site; this is the exact 2026-07-25 shape"
  fi
  grep -q 'TEARDOWN TARGET GUARD' "$GLOG" \
    && ok "$_name refusal names TEARDOWN TARGET GUARD" \
    || bad "$_name died without the TEARDOWN TARGET GUARD marker (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
  # BEFORE ANY MUTATION, asserted as "the recorder saw NOTHING". Not just "no
  # canonical label": oc_teardown_bounded's own call site is what makes the
  # refusal precede the .dump backup and the serve/autodeploy/tunnel bootouts.
  # If only the nested oc_teardown_warden guard survives, the run still dies —
  # but only AFTER four bootouts, and only this assertion notices.
  _mut="$(grep -cE 'launchctl bootout|^rm <' "$SHIMDIR/.teardown-log-$_name" 2>/dev/null || true)"
  if [[ "${_mut:-0}" != "0" ]]; then
    bad "$_name mutated $_mut host resource(s) BEFORE refusing — the guard must run before the backup/bootout/delete sequence (log: $(tr '\n' '|' < "$SHIMDIR/.teardown-log-$_name"))"
  else
    ok "$_name refused before booting out or deleting anything"
  fi
done
unset SHIM_ALLOW_TEARDOWN SHIM_TEARDOWN_LOG TEST_HOME

# ── 19) T-e1dd: cross_machine's preflight — prod-host guard + gate ORDERING ───
#
# cross_machine.sh acked destructiveness before STAGE 1 and isolation before
# STAGE 3, with `rm -rf "$SERVER_ROOT"` in between — so the invocation printed in
# its own header deleted the server root and was refused 141 lines later. Nothing
# could catch it: the gates and the deletion were top-level code in a destructive
# script, so the only way to exercise them was to run it for real.
#
# Everything below runs against oc_cross_machine_preflight as a FUNCTION, on a
# throwaway $HOME (TEST_HOME) whose contents this file creates, with the recording
# shims installed. This file makes ZERO direct calls to rm/launchctl/tmux against
# any real resource — that is a stated requirement of the ticket, because the
# mutants below deliberately disable the guard being tested and a test that leaned
# on that guard for its own safety would destroy the host at exactly that moment.
E1DD_LOG="$SHIMDIR/.teardown-log-e1dd"
export SHIM_ALLOW_TEARDOWN=1 SHIM_TEARDOWN_LOG="$E1DD_LOG"

# The known production hardware UUID, read from the lib rather than duplicated —
# a second copy of this constant would be a drift site of exactly the kind the
# port literal already taught us about (T-b76b).
PROD_UUID="$(run_snippet 'printf "UUID=%s\n" "${OC_PROD_HOST_HW_UUIDS[0]}"' >/dev/null; grep '^UUID=' "$GLOG" | cut -d= -f2)"
[[ -n "$PROD_UUID" ]] || bad "could not read OC_PROD_HOST_HW_UUIDS from the lib — the prod-host identity guard has no pinned station"
DISPOSABLE_UUID="11111111-2222-3333-4444-555555555555"

# e1dd_home KIND — build a throwaway $HOME. KIND: clean | residue
e1dd_home() {
  local kind="$1" h="$SHIMDIR/e1dd-home-$1"
  rm -rf "$h" 2>/dev/null || true          # shim rm: recorded, never touches the host
  mkdir -p "$h/Library/LaunchAgents" "$h/backups" "$h/bin"
  printf '#!/bin/sh\nexit 0\n' > "$h/bin/ocserver"; chmod +x "$h/bin/ocserver"
  [[ "$kind" == "residue" ]] && mkdir -p "$h/.officraft/server/data"
  printf '%s' "$h"
}

# e1dd_pre HOME BODY — run BODY with the preflight's required globals wired to a
# throwaway home. Both acks and a disposable machine identity default ON, so each
# case turns exactly ONE thing off.
e1dd_pre() {
  local home="$1" body="$2"
  : > "$E1DD_LOG"
  # `-` not `:-` on the UUID vars: an EMPTY value is a case in its own right (the
  # "cannot read a hardware UUID, identity check has gone dark" path), and `:-`
  # would silently substitute the default and make that case inexpressible.
  TEST_HOME="$home" OC_CROSS_MACHINE_YES="${E1DD_YES:-1}" \
  REQUIRE_ISOLATION_CONFIRMED="${E1DD_ISO:-1}" SHIM_HW_UUID="${E1DD_UUID-$DISPOSABLE_UUID}" \
  SHIM_REMOTE_HW="${E1DD_REMOTE_HW-$DISPOSABLE_UUID}" \
  SHIM_REMOTE_SERVER_TREE="${E1DD_REMOTE_TREE:-0}" SHIM_SSH_FAIL="${E1DD_SSH_FAIL:-0}" \
  SHIM_SSH_SILENT="${E1DD_SSH_SILENT:-0}" SHIM_REMOTE_WARDEN="${E1DD_REMOTE_WARDEN:-0}" \
  SHIM_REMOTE_AGENTS="${E1DD_REMOTE_AGENTS:-0}" SHIM_SSH_NOISE="${E1DD_SSH_NOISE:-}" \
  SHIM_REMOTE_TOOLS="${E1DD_REMOTE_TOOLS:-all}" \
  SHIM_REMOTE_HOME="$SHIMDIR/remote-home-$$-${E1DD_REMOTE_TREE:-0}" \
  SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
  OC_CLAUDE_BIN="$home/bin/ocserver" run_snippet '
    SECOND_MACHINE="a-disposable-relocate-target"
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OC_ROOT="$HOME/.officraft"; SERVER_ROOT="${OC_SERVER_ROOT:-$OC_ROOT/server}"
    DB_PATH="$SERVER_ROOT/data/officraft.db"; OCSERVER="$HOME/bin/ocserver"
    OCWARDEN="$(command -v ocwarden)"; TMUX_SOCKET_LOCAL="$OC_CANONICAL_TMUX_SOCKET"
    SERVE_LABEL="com.officraft.serve"; AUTODEPLOY_LABEL="com.officraft.autodeploy"
    TUNNEL_LABEL="com.officraft.tunnel"; WARDEN_LABEL="com.officraft.ocwarden"
    '"$body"
}

H_CLEAN="$(e1dd_home clean)"; H_RESIDUE="$(e1dd_home residue)"

# 19a) DETECTION — the two questions, asked separately.
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$PROD_UUID" \
      run_snippet 'oc_detect_prod_host | grep -q "^identity:"')"
check "identity guard fires on a known production hardware UUID (even with a clean disk)" "0" "$rc"
rc="$(TEST_HOME="$H_RESIDUE" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'oc_detect_prod_host | grep -q "^residue:"')"
check "residue guard fires on an existing server tree (even on an unknown machine)" "0" "$rc"
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'out="$(oc_detect_prod_host)"; [[ -z "$out" ]]')"
check "detection is EMPTY on a disposable machine with no server tree" "0" "$rc"

# The case the pre-T-e1dd shape could not see, and the reason the identity guard
# exists at all: a production station whose server is STOPPED and whose disk is
# still bare (freshly provisioned) — every liveness signal silent.
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$PROD_UUID" SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
      run_snippet 'OC_NS=""; oc_live_fleet_guard && oc_detect_prod_host | grep -q "^identity:"')"
check "a production station with NOTHING running is still recognised as production" "0" "$rc"

# 19a') ADDRESSING — the guard must not be steerable by OC_SERVER_ROOT. That env
# is what the teardown deletes; if the detector derived its paths from it, the
# override would aim the guard at an empty directory while the deletion still
# landed on the real tree. This is the difference between a guard and a flag.
rc="$(TEST_HOME="$H_RESIDUE" OC_SERVER_ROOT="$SHIMDIR/decoy" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'SERVER_ROOT="$OC_SERVER_ROOT"; oc_detect_prod_host | grep -q "^residue:"')"
check "OC_SERVER_ROOT cannot steer the prod-host guard away from the real tree" "0" "$rc"

# 19b) THE GATE — refusals happen, and they happen BEFORE any mutation. Each case
#      runs the REAL preflight → teardown chain; the recorder proves how far
#      execution actually got.
e1dd_gate() { # e1dd_gate DESC HOME MARKER
  local desc="$1" home="$2" marker="$3"
  local rc; rc="$(e1dd_pre "$home" 'oc_cross_machine_preflight
    oc_teardown_bounded "e1dd-should-not-reach"')"
  [[ "$rc" != "0" ]] && ok "$desc → refuses (rc=$rc)" || bad "$desc → should refuse, returned 0"
  grep -q "$marker" "$GLOG" && ok "$desc → refusal names $marker" \
    || bad "$desc → refusal lacks $marker (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
  local mut; mut="$(grep -cE 'launchctl bootout|^rm <' "$E1DD_LOG" 2>/dev/null || true)"
  [[ "${mut:-0}" == "0" ]] && ok "$desc → nothing was booted out or deleted first" \
    || bad "$desc → mutated ${mut} resource(s) BEFORE refusing (log: $(tr '\n' '|' < "$E1DD_LOG"))"
}

E1DD_ISO=0 e1dd_gate "missing isolation ack" "$H_CLEAN" "REQUIRE_ISOLATION_CONFIRMED"
E1DD_YES=0 e1dd_gate "missing destructiveness ack" "$H_CLEAN" "DESTRUCTIVE"
E1DD_UUID="$PROD_UUID" e1dd_gate "a known production station" "$H_CLEAN" "PROD-HOST GUARD (identity)"
e1dd_gate "a host carrying a server tree" "$H_RESIDUE" "PROD-HOST GUARD (residue)"

# Both acks set is the maximum an operator can assert. It must not be enough on a
# production station — otherwise the guard is advisory and this ticket's failure
# mode is one env var away from coming back.
rc="$(E1DD_UUID="$PROD_UUID" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight')"
[[ "$rc" != "0" ]] && ok "both acks set still cannot run on a known production station (rc=$rc)" \
  || bad "both acks set BYPASSED the identity guard — the guard is only advisory"

# 19b') WHAT EACH REFUSAL MAY SAY. These two are opposites on purpose:
#   • the RESIDUE refusal MUST offer a way forward — its most likely reader is
#     someone re-running on their own throwaway VM, and a refusal that only says
#     "no" reads as a broken tool, which is how workarounds get invented.
#   • the IDENTITY refusal must NEVER name a way to clear the obstacle. Its
#     reader is standing on a production station; "delete this and retry" IS the
#     disaster this whole ticket is about.
e1dd_pre "$H_RESIDUE" 'oc_cross_machine_preflight' >/dev/null
if grep -Eq 'PROD-HOST GUARD \(residue\).*(rebuild|delete)' "$GLOG"; then
  ok "the residue refusal tells the operator how to proceed"
else
  bad "the residue refusal gives no way forward — it will read as a broken tool (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi
E1DD_UUID="$PROD_UUID" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
# The pattern hunts for an INSTRUCTION TO THE READER, not for the word "delete":
# the message is allowed — required, even — to say what the suite deletes. What it
# must never do is tell the person standing on that station what to remove or
# which flag to set in order to continue.
if grep -Eqi 'PROD-HOST GUARD \(identity\).*(rm -rf|delete (\$?HOME|~|the tree|it) |set [A-Z_]+=1|bypass|--force|override this|skip this)' "$GLOG"; then
  bad "the identity refusal tells someone standing on a production station how to clear the obstacle: $(grep -i 'identity' "$GLOG" | tail -c 300)"
else
  ok "the identity refusal offers no way to clear it — only 'run somewhere else'"
fi
# The REMOTE guard has its own three messages and the same rule applies, per
# branch. This case is deliberately the ambiguous one — a known station that is
# ALSO running its warden — because the danger is branch ORDER: if liveness were
# checked first, a machine known by hardware UUID to be production would be handed
# the liveness message's "retire it yourself" remedy.
E1DD_REMOTE_HW="$PROD_UUID" E1DD_REMOTE_WARDEN=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'PROD-HOST GUARD (remote): the second machine .* is a known production' "$GLOG"; then
  ok "a production SECOND_MACHINE that is also live gets the IDENTITY refusal, not the liveness one"
else
  bad "the liveness branch preempted identity on a known production station — that message tells its reader how to clear the obstacle (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi
if grep -Eqi 'is a known production officraft station.*(retire it yourself|launchctl bootout|clear ~/\.officraft)' "$GLOG"; then
  bad "the remote identity refusal names a way to clear the obstacle on a production station"
else
  ok "the remote identity refusal offers no way to clear it either"
fi

# 19b'') THE SECOND MACHINE gets three questions of its own. STAGE 5b deletes its
# ENTIRE ~/.officraft — more than this suite deletes locally — so guarding only
# the local host leaves the cheaper mistake available: from a genuinely clean
# throwaway VM, naming a production station as SECOND_MACHINE passes every local
# gate. The refusal must also happen HERE, in the preflight, not at STAGE 5b,
# which is after the local host has been torn down and reinstalled.
E1DD_REMOTE_HW="$PROD_UUID" e1dd_gate "a production SECOND_MACHINE" "$H_CLEAN" "PROD-HOST GUARD (remote)"
E1DD_REMOTE_TREE=1 e1dd_gate "a SECOND_MACHINE carrying a server tree" "$H_CLEAN" "PROD-HOST GUARD (remote)"

E1DD_REMOTE_WARDEN=1 e1dd_gate "a SECOND_MACHINE running a warden" "$H_CLEAN" "live fleet node"
# Agents outlive their warden (booted out for maintenance, crashed, launchd gave
# up, started by hand). STAGE 5b kill-sessions them explicitly, so warden
# registration alone is a guard that looks complete and is not.
E1DD_REMOTE_AGENTS=1 e1dd_gate "a SECOND_MACHINE with live agent sessions but no warden" "$H_CLEAN" "agents are running there right now"

# MARKER DILUTION. A remote rc file that prints a marker-shaped line lands BEFORE
# the probe's own output, so "take the first/any match" would read the wrong
# answer. Every marker is counted, and more than one answer to a question means
# the probe cannot be trusted — including for `hw=`, where a wrong-but-non-empty
# value would ALSO suppress the go-dark warning.
E1DD_REMOTE_HW="$PROD_UUID" E1DD_SSH_NOISE="hw=not-a-real-uuid" \
  e1dd_gate "a probe diluted by a stray hw= line" "$H_CLEAN" "did not come back with exactly one answer"
E1DD_REMOTE_TREE=1 E1DD_SSH_NOISE="server_tree=0" \
  e1dd_gate "a probe diluted by a stray server_tree= line" "$H_CLEAN" "did not come back with exactly one answer"

# …but ordinary ssh chatter is NOT marker-shaped and must be tolerated. A guard
# that refused every host emitting a known-hosts warning would be turned off
# within a day, which is a slower way of having no guard.
E1DD_SSH_NOISE="Warning: Permanently added 'tgt' (ED25519) to the list of known hosts." \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'prod-host guard OK (remote)' "$GLOG" \
  && ok "ordinary ssh chatter does not trip the marker parse" \
  || bad "a known-hosts warning made the remote probe unparseable — every real host emits that (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# Unreachable second machine → fail CLOSED. A host whose identity cannot be
# established is not thereby safe to wipe; "ssh failed, carry on" would be the
# same shape as the bug this ticket fixes.
E1DD_SSH_FAIL=1 e1dd_gate "an unreachable SECOND_MACHINE" "$H_CLEAN" "could not establish what machine"

# …and the quieter version of the same thing: ssh exits 0 having run nothing
# (ForceCommand, restricted shell, an rc file that returns early). An empty probe
# is NOT a clean host. This is the failure mode the first version of this guard
# got wrong, and it is the one that looks exactly like success.
E1DD_SSH_SILENT=1 e1dd_gate "a SECOND_MACHINE whose probe returned nothing" "$H_CLEAN" "could not establish what machine"

# The ssh failure message must carry ssh's own diagnosis — "fix ssh access" is
# useless standing next to "Permission denied (publickey)".
E1DD_SSH_FAIL=1 e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'ssh said:.*Operation timed out' "$GLOG" \
  && ok "the unreachable-host refusal repeats what ssh actually said" \
  || bad "the unreachable-host refusal swallowed ssh's diagnosis (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# GO-DARK WARNINGS. A check that cannot run must SAY so; the danger is the "guard
# OK" line continuing to claim "not a known production station" when the identity
# question was never actually asked.
E1DD_UUID="" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'IDENTITY check is INACTIVE' "$GLOG" \
  && ok "an unreadable local hardware UUID is announced, not silently skipped" \
  || bad "the local identity check went dark silently (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"
E1DD_REMOTE_HW="" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'remote IDENTITY check is INACTIVE' "$GLOG" \
  && ok "an unreadable remote hardware UUID is announced, not silently skipped" \
  || bad "the remote identity check went dark silently (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# The probe must read the REMOTE machine's identity, not this one's. A guard that
# looked at the local UUID would pass every case above and still be looking at the
# wrong machine — which is the entire bug this remote check was added for.
E1DD_UUID="$PROD_UUID" E1DD_REMOTE_HW="$DISPOSABLE_UUID" \
  e1dd_pre "$H_CLEAN" 'oc_prod_host_remote_guard "$SECOND_MACHINE"' >/dev/null
grep -q 'prod-host guard OK (remote)' "$GLOG" \
  && ok "the remote guard reads the REMOTE uuid (a production LOCAL uuid does not trip it)" \
  || bad "the remote guard tripped on the LOCAL machine's identity — it is probing the wrong host"

# A QUESTION THE FAR SIDE CANNOT ANSWER IS NOT A "NO". An ssh non-login shell has
# no Homebrew bin dir, so `tmux` is not found there — this script's own gotcha #2,
# and the reason every other remote command goes through the PATH-exporting
# wrapper. A not-found tool produces no output, which for a liveness check reads
# as "nothing running": fail-OPEN, on the default second machine.
E1DD_REMOTE_TOOLS=notmux E1DD_REMOTE_AGENTS=1 \
  e1dd_gate "a SECOND_MACHINE where the probe's tools are not on PATH" "$H_CLEAN" "did not come back with exactly one answer"
# …and pin WHICH question went unanswered — ALL FOUR COUNTS, not just the one
# that is meant to be zero. Matching `live_agents: 0` alone still passed when the
# probe answered NOTHING AT ALL (0,0,0,0): a shim that fell silent took the whole
# case with it and every assertion stayed green, because "refused" and "refused
# for the intended reason" are not the same claim. Requiring the other three to
# be 1 says the far side was alive and answering, and exactly one question could
# not be answered — which is the only state this case is about.
E1DD_REMOTE_TOOLS=notmux E1DD_REMOTE_AGENTS=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'hw: 1, server_tree: 1, live_warden: 1, live_agents: 0' "$GLOG"; then
  ok "the notmux refusal is the LIVENESS question going unanswered, with the other three answered"
else
  bad "the notmux case refused for the wrong reason — either tmux was answerable after all, or the probe answered nothing at all (expected 'hw: 1, server_tree: 1, live_warden: 1, live_agents: 0'; got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi

# BRANCH ORDER, second half. Only the liveness message names a remedy, so it must
# come LAST — a host with BOTH a server tree and a running warden is more
# incriminating than one with either alone, and must not be handed the more
# permissive message. This is the shape of an UNLISTED production server, which is
# exactly the case residue exists to catch.
E1DD_REMOTE_TREE=1 E1DD_REMOTE_WARDEN=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'PROD-HOST GUARD (remote): the second machine .* carries an officraft server tree' "$GLOG"; then
  ok "a SECOND_MACHINE with BOTH a server tree and a live warden gets the residue refusal, not the remedy-bearing one"
else
  bad "the liveness branch preempted residue — the more incriminating host got the more permissive message (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi

# 19c) SENTINEL — a genuinely disposable host must still be able to run. A guard
# that refuses everything passes every refusal test above and is useless.
rc="$(e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight')"
check "a disposable host with both acks PASSES the preflight" "0" "$rc"

# 19c') THE ORDERING IS A PROPERTY OF cross_machine.sh, and nothing above pins it
# there. Every assertion so far runs a preflight→teardown chain THIS FILE builds,
# so moving the preflight call BELOW the teardown call in cross_machine.sh would
# leave the whole section green — reinstating exactly this ticket's bug. Line
# numbers are weak evidence in general, but this is straight-line top-level script
# code, where source order IS execution order.
CM="$HERE/../cross_machine.sh"
_pre_ln="$(grep -n '^oc_cross_machine_preflight$' "$CM" | head -1 | cut -d: -f1)"
_td_ln="$(grep -n '^oc_teardown_bounded ' "$CM" | head -1 | cut -d: -f1)"
if [[ -z "$_pre_ln" || -z "$_td_ln" ]]; then
  bad "cross_machine.sh no longer has a top-level oc_cross_machine_preflight and/or oc_teardown_bounded call (pre=${_pre_ln:-none} td=${_td_ln:-none}) — this ordering pin has gone blind"
elif [[ "$_pre_ln" -lt "$_td_ln" ]]; then
  ok "cross_machine.sh calls the preflight (line $_pre_ln) BEFORE the teardown (line $_td_ln)"
else
  bad "cross_machine.sh calls the teardown (line $_td_ln) before the preflight (line $_pre_ln) — this is the T-e1dd bug"
fi
# …and no destructive top-level statement may sit ahead of the preflight call.
_e1dd_early_scan() { # _e1dd_early_scan FILE STOP_LINE
  awk -v stop="$2" 'NR<stop && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|launchctl bootout|kill-session|kill-server|pkill|killall|oc_teardown_)/' "$1" \
    | wc -l | tr -d ' '
}
_early="$(_e1dd_early_scan "$CM" "$_pre_ln")"
check "no destructive statement in cross_machine.sh runs before the preflight" "0" "${_early:-0}"
# CONTROL for the scan above. It is a grep-for-absence: a pattern broken by a
# future edit produces 0 hits forever and the assertion is permanently, silently
# green — indistinguishable from a clean file. Run the SAME function over a
# fixture that is deliberately dirty.
_fixture="$SHIMDIR/early-scan-fixture.sh"
{ printf '# rm -rf /commented-out — must not count\n'
  printf 'rm -rf /x\n'
  printf 'launchctl bootout gui/501/com.officraft.serve\n'
  printf 'oc_teardown_bounded "hoisted"\n'
  printf 'oc_cross_machine_preflight\n'; } > "$_fixture"
check "early-scan control: the scan reddens on a fixture with 3 destructive statements (and skips the comment)" \
  "3" "$(_e1dd_early_scan "$_fixture" 5)"

# 19d) MUTANTS — one edit each. Without them every assertion above could be
# vacuously true. The mutant lib needs a tree whose ../../server resolves, because
# the lib derives the canonical port from server/ocserverd/config.go and FATALs if
# it cannot (same construction as the MUT-D control above).
# e1dd_mutant NAME SED_EXPR HOME UUID [ISO_ACK]
#
# The ack defaults are passed EXPLICITLY, not inherited from ambient state. An
# earlier version relied on a `E1DD_ISO=0` prefix on the CALL reaching e1dd_pre
# through the function body — it does not, so the ack mutant ran fully acked and
# asserted rc==0 for a configuration that already returns rc==0 unmutated. It was
# green, and it would have stayed green with the ack check deleted outright: a
# mutant that proves nothing is worse than no mutant, because it reads as proof.
e1dd_mutant() {
  local name="$1" expr="$2" home="$3" uuid="$4" iso="${5:-1}" remote_hw="${6:-$DISPOSABLE_UUID}"
  local root="$SHIMDIR/mut-$name-tree" lib="$SHIMDIR/mut-$name-tree/e2e_test/lib/oc_lifecycle.sh"
  mkdir -p "$root/e2e_test/lib"; ln -sfn "$HERE/../../server" "$root/server"
  sed "$expr" "$LIB" > "$lib"
  if cmp -s "$lib" "$LIB"; then
    bad "MUT-$name did not change the lib — the mutation anchor moved; a vacuous mutant proves nothing"
    return
  fi
  # CONTROL: the same configuration against the UNMUTATED lib must REFUSE. Without
  # this, "the mutant proceeds" is not evidence — it is also what a configuration
  # that was always going to proceed looks like.
  local ctl; ctl="$(E1DD_ISO="$iso" E1DD_UUID="$uuid" E1DD_REMOTE_HW="$remote_hw" e1dd_pre "$home" 'oc_cross_machine_preflight')"
  if [[ "$ctl" == "0" ]]; then
    bad "MUT-$name control: the UNMUTATED lib already accepted this configuration — the mutant below proves nothing about that check"
    return
  fi
  local rc; rc="$(SNIPPET_LIB="$lib" E1DD_ISO="$iso" E1DD_UUID="$uuid" E1DD_REMOTE_HW="$remote_hw" e1dd_pre "$home" 'oc_cross_machine_preflight')"
  [[ "$rc" == "0" ]] \
    && ok "MUT-$name: unmutated refuses (rc=$ctl), with that check removed the run PROCEEDS — the live case is pinned to it" \
    || bad "MUT-$name: still refused (rc=$rc) — the live assertion is NOT pinned to this check (glog: $(tr '\n' '|' < "$GLOG" | tail -c 200))"
}
# 1. identity: drop the hardware-UUID comparison → the production-station case must go green-lit.
# Replaced with a no-op rather than deleted: that line is the entire body of the
# `for u in ...` loop, and deleting it leaves an empty loop body — a syntax error,
# which makes the lib fail to load and the mutant "refuse" for the wrong reason.
e1dd_mutant identity 's/.*identity: this machine.*/      : ;/' "$H_CLEAN" "$PROD_UUID"
# 2. residue: drop the server-tree check → the residue case must go green-lit.
e1dd_mutant residue 's|^  \[\[ -d "$server_root" \]\] .*$|  : ;|' "$H_RESIDUE" "$DISPOSABLE_UUID"
# 3. the isolation ack — run UNACKED (iso=0), so the control refuses and the
#    mutant proceeds. This is the check whose ORDER is the whole ticket.
e1dd_mutant ack '/^  \[\[ "${REQUIRE_ISOLATION_CONFIRMED:-0}" == "1" \]\] || die \\$/,+1d' "$H_CLEAN" "$DISPOSABLE_UUID" 0
# 4. the remote prod-host guard — a production SECOND_MACHINE must be refused.
e1dd_mutant remote 's/^  oc_prod_host_remote_guard .*$/  : ;/' "$H_CLEAN" "$DISPOSABLE_UUID" 1 "$PROD_UUID"

# 19e) This test file must not be able to destroy anything itself — the ticket's
# hardest constraint, because the mutants above deliberately disable the guard
# under test. Counted, not eyeballed: an absolute path or a kill-by-name dodges
# the PATH shim and would reach the real host. Matches in THIS block's own
# assertion strings are excluded by anchoring on a command position.
_e1dd_direct="$(grep -cE '^[[:space:]]*(/bin/rm|/usr/bin/killall|killall|pkill)[[:space:]]' "$HERE/run.sh" || true)"
check "this test file makes no direct call to a real destructive command" "0" "${_e1dd_direct:-0}"

unset SHIM_ALLOW_TEARDOWN SHIM_TEARDOWN_LOG TEST_HOME E1DD_ISO E1DD_YES

# ── 20) T-ff8a: a setup that REFUSED must not be torn down ────────────────────
#
# run_all.sh armed `trap cleanup EXIT` BEFORE running setup.sh, and cleanup ran
# teardown.sh unconditionally — while teardown.sh's step 4 is
# `rm -rf "$REPO_ROOT/var/data"`. setup.sh's three prod guards (oc.toml port,
# [storage].dsn, the leftover-listener check) all `exit 2` BEFORE setup has
# created anything, and each of those refusals then went out through the EXIT
# trap into that rm. The guards could stop the START and had no say over the
# FINISH: refusing to touch a DB was followed, one trap later, by deleting it —
# and the more suspicious the configuration, the more certain the deletion,
# because refusing is what fires the trap.
#
# Everything below drives the REAL run_all.sh → setup.sh → teardown.sh chain
# inside a THROWAWAY repo tree built by this file, with the deletion seam
# (oc_e2e_destroy) pointed at a recording impl. Nothing real is removed even when
# the guard is deliberately broken — which it is, twice, below. That is a
# requirement and not a nicety: a test whose own safety depends on the guard it
# is testing destroys the host at precisely the moment the guard regresses.
FF8A_ROOT="$SHIMDIR/ff8a-repo"
FF8A_E2E="$FF8A_ROOT/e2e_test"
FF8A_REC="$SHIMDIR/.ff8a-destroy-record"
FF8A_SENTINEL="$FF8A_ROOT/var/data/officraft.db"
mkdir -p "$FF8A_E2E/lib" "$FF8A_ROOT/server/ocserverd" "$FF8A_ROOT/var/data"
# common.sh derives the prod-port refusal set from config.go and FATALs if it
# cannot parse it, so the throwaway tree carries a copy (same construction as the
# mutant trees in 19d). A COPY, not a symlink: the mutants below rewrite it.
cp "$HERE/../../server/ocserverd/config.go" "$FF8A_ROOT/server/ocserverd/config.go"
cp "$HERE/../lib/common.sh" "$FF8A_E2E/lib/common.sh"
cp "$HERE/../setup.sh" "$HERE/../teardown.sh" "$HERE/../run_all.sh" "$FF8A_E2E/"
# An oc.toml on the WRONG port — the first of setup's three prod guards, chosen
# because it fires earliest and needs no ports, no npm and no go toolchain.
printf '[server]\nport = 19999\n\n[storage]\ndsn = "sqlite:///var/data/e2e.db"\n' > "$FF8A_ROOT/oc.toml"
printf 'PRETEND-THIS-IS-A-REAL-DB\n' > "$FF8A_SENTINEL"

ff8a_run() { # ff8a_run SCRIPT [LIB_OVERRIDE] — echoes "<rc>", output → $SHIMDIR/.ff8a-out
  local script="$1" lib="${2:-}"
  [[ -n "$lib" ]] && cp "$lib" "$FF8A_E2E/lib/common.sh"
  : > "$FF8A_REC"
  ( OC_E2E_DESTROY_RECORD="$FF8A_REC" OC_E2E_DESTROY_IMPL=oc_e2e_destroy_record_only \
    bash "$FF8A_E2E/$script" ) > "$SHIMDIR/.ff8a-out" 2>&1
  echo $?
}
# Line count, NOT `grep -c . || echo 0`: grep exits 1 on an empty file, so the
# `||` fired and the function echoed "0" TWICE — every numeric comparison then
# saw "0\n0", which is neither zero nor a number. The record is always truncated
# before a run, so a plain wc is both simpler and total.
ff8a_recorded() { wc -l < "$FF8A_REC" | tr -d ' '; }

# 20a) POSITIVE CONTROL FIRST. Every headline assertion below is an assertion of
# ABSENCE ("the deletion record is empty"), which is exactly the shape that
# passes when the recorder is broken, the script never ran, or the path moved.
# So: prove the recorder records, by running the REAL teardown.sh on purpose.
rc="$(ff8a_run teardown.sh)"
FF8A_POS="$(ff8a_recorded)"
[[ "$FF8A_POS" -gt 0 ]] \
  && ok "positive control: the real teardown.sh records $FF8A_POS deletion target(s) — the record can be non-empty"
[[ "$FF8A_POS" -gt 0 ]] \
  || bad "positive control FAILED: the real teardown.sh recorded NOTHING (rc=$rc). Every 'record is empty' assertion below would be vacuously green (out: $(tail -c 300 "$SHIMDIR/.ff8a-out"))"
grep -Fq "$FF8A_ROOT/var/data" "$FF8A_REC" \
  && ok "positive control: the recorded target set includes \$REPO_ROOT/var/data (the destructive one)" \
  || bad "positive control: teardown recorded deletions but NOT \$REPO_ROOT/var/data — the record is not watching the dangerous path (got: $(tr '\n' '|' < "$FF8A_REC"))"

# 20b) THE HEADLINE. setup refuses (prod guard, exit 2) → the EXIT trap must
# delete NOTHING.
rc="$(ff8a_run run_all.sh)"
[[ "$rc" != "0" ]] \
  && ok "run_all with a prod-guard-refusing setup exits non-zero (rc=$rc)" \
  || bad "run_all returned 0 despite setup refusing — the fixture is not reproducing the case"
grep -q 'oc.toml port' "$SHIMDIR/.ff8a-out" \
  && ok "…and it refused for the intended reason (setup's oc.toml port prod guard)" \
  || bad "run_all failed for some OTHER reason than the prod guard — this case is testing the wrong path (out: $(tail -c 400 "$SHIMDIR/.ff8a-out"))"
FF8A_N="$(ff8a_recorded)"
check "SETUP REFUSED → the teardown deletion record is EMPTY" "0" "$FF8A_N"
[[ "$FF8A_N" == "0" ]] || bad "  …recorded targets were: $(tr '\n' '|' < "$FF8A_REC")"
# Belt and braces on the filesystem itself: with a recording impl the sentinel
# survives either way, so this is NOT the headline — it is the check that the
# fixture never handed a real path to a real rm.
[[ -f "$FF8A_SENTINEL" ]] \
  && ok "the throwaway var/data sentinel is untouched (this test file deletes nothing real)" \
  || bad "the throwaway var/data sentinel was DELETED — the recording impl is not in force and this test is destructive"

# 20c) SENTINEL — an ARMED run must still be torn down. A gate that never lets
# the teardown run passes 20b and leaks a serve + a DB on every real run.
: > "$FF8A_REC"
FF8A_ARMED_OUT="$( ( OC_E2E_DESTROY_RECORD="$FF8A_REC" OC_E2E_DESTROY_IMPL=oc_e2e_destroy_record_only \
    bash -c 'source "$1/lib/common.sh" >/dev/null 2>&1
             oc_e2e_arm_teardown
             oc_e2e_teardown_on_exit "$1"' _ "$FF8A_E2E" ) 2>&1 )"
FF8A_ARMED_N="$(ff8a_recorded)"
[[ "$FF8A_ARMED_N" -gt 0 ]] \
  && ok "sentinel: an ARMED run DOES tear down ($FF8A_ARMED_N target(s) recorded) — the gate is not a permanent 'no'" \
  || bad "sentinel: an armed run tore down NOTHING — the gate refuses everything, which leaks a serve and a DB on every real run (out: $(tail -c 300 <<<"$FF8A_ARMED_OUT"))"

# 20d) THE ORDERING IN setup.sh. The arming must sit AFTER the last refusal gate
# and BEFORE the first mutation; nothing above can see that, because 20b drives
# the chain through whatever order the file happens to have. Straight-line
# top-level script code, so source order IS execution order.
FF8A_SETUP="$HERE/../setup.sh"
_arm_ln="$(grep -n '^oc_e2e_arm_teardown$' "$FF8A_SETUP" | head -1 | cut -d: -f1)"
if [[ -z "$_arm_ln" ]]; then
  bad "setup.sh has no top-level 'oc_e2e_arm_teardown' call — the teardown can never be armed, or the anchor moved"
else
  # the first mutation must be BELOW the arming…
  _early_mut="$(awk -v arm="$_arm_ln" 'NR<arm && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|nohup|go build)/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  check "no mutation in setup.sh happens before the arming" "0" "${_early_mut:-0}"
  _mut_ln="$(grep -nE '^[[:space:]]*(rm -rf|rm -f|nohup|go build)' "$FF8A_SETUP" | head -1 | cut -d: -f1)"
  # …and the PRE-CREATION prod guards must be above it. NOT "every exit 2 is
  # above the arming": setup's 2e TOCTOU re-check also exits 2 and it runs AFTER
  # the builds, when the run genuinely owns things and MUST be torn down. The
  # property is narrower and exact — the arming sits in the gap between the last
  # guard that precedes any mutation and the first mutation itself, so no refusal
  # is stranded on the armed side of a run that created nothing.
  _guards_above="$(awk -v arm="$_arm_ln" 'NR<arm && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  [[ "${_guards_above:-0}" -ge 3 ]] \
    && ok "setup.sh's ${_guards_above} pre-creation prod-guard refusals all sit ABOVE the arming (≥3: oc.toml port, storage.dsn, leftover listener)" \
    || bad "only ${_guards_above:-0} 'exit 2' refusals precede the arming — a prod guard has moved below it and its refusal would arm a teardown for a run that created nothing"
  _stranded="$(awk -v arm="$_arm_ln" -v mut="${_mut_ln:-0}" 'NR>arm && NR<mut && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  check "no refusal sits between the arming and the first mutation" "0" "${_stranded:-0}"
fi
# CONTROL for both scans above — they are greps for absence, so a pattern broken
# by a later edit yields 0 forever and both assertions go permanently, silently
# green. Run the SAME scans over a deliberately dirty fixture.
_ff8a_fix="$SHIMDIR/ff8a-order-fixture.sh"
{ printf '# exit 2 — a comment, must not count\n'
  printf 'rm -rf "$REPO_ROOT/var/data"\n'
  printf 'oc_e2e_arm_teardown\n'
  printf 'exit 2\n'
  printf 'nohup serve &\n'; } > "$_ff8a_fix"
check "ordering-scan control: the early-mutation scan reddens on a fixture whose rm is above the arming (and skips the comment)" \
  "1" "$(awk -v arm=3 'NR<arm && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|nohup|go build)/' "$_ff8a_fix" | wc -l | tr -d ' ')"
check "ordering-scan control: the stranded-refusal scan reddens on a fixture whose 'exit 2' sits between the arming and the mutation" \
  "1" "$(awk -v arm=3 -v mut=5 'NR>arm && NR<mut && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$_ff8a_fix" | wc -l | tr -d ' ')"
check "ordering-scan control: the guards-above scan counts 0 on a fixture with no refusal above the arming" \
  "0" "$(awk -v arm=3 'NR<arm && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$_ff8a_fix" | wc -l | tr -d ' ')"

# 20e) THE SEAM MUST BE THE ONLY WAY OUT of teardown.sh. A raw `rm` reintroduced
# there is invisible to every assertion above: the record stays empty and the
# deletion happens anyway — the exact "the record says nothing was deleted"
# false green this whole case is built on.
FF8A_TEARDOWN="$HERE/../teardown.sh"
_raw_rm="$(grep -cE '^[[:space:]]*rm[[:space:]]+-' "$FF8A_TEARDOWN" || true)"
check "teardown.sh has NO raw rm — every delete goes through the recorded seam" "0" "${_raw_rm:-0}"
check "raw-rm scan control: the same scan finds the 2 raw rms in a dirty fixture" "2" \
  "$(printf '# rm -rf /commented\nrm -rf /x\n  rm -f /y\noc_e2e_destroy /z\n' > "$_ff8a_fix"; grep -cE '^[[:space:]]*rm[[:space:]]+-' "$_ff8a_fix" || true)"
# …and run_all.sh's trap must reach teardown through the GATE, not directly.
FF8A_RUNALL="$HERE/../run_all.sh"
# NON-COMMENT lines only, on both scans. A plain `grep -F oc_e2e_teardown_on_exit`
# matched the COMMENT above the trap — so restoring the old ungated cleanup body
# left this assertion green while the name it was looking for survived only as
# prose. Verified against the real mutant: the loose form stayed green, this one
# does not. The comment is where the name is MOST likely to linger, which makes
# it the worst possible thing to accept as evidence.
_gated="$(grep -cE '^[^#]*oc_e2e_teardown_on_exit' "$FF8A_RUNALL" || true)"
[[ "${_gated:-0}" -gt 0 ]] \
  && ok "run_all.sh's EXIT trap goes through oc_e2e_teardown_on_exit (the gate), in CODE not a comment" \
  || bad "run_all.sh no longer calls oc_e2e_teardown_on_exit outside a comment — the trap has gone back to being ungated (T-ff8a regression)"
_direct_td="$(grep -cE '^[^#]*bash "\$HERE/teardown\.sh"' "$FF8A_RUNALL" || true)"
check "run_all.sh does not invoke teardown.sh directly (bypassing the gate)" "0" "${_direct_td:-0}"
# CONTROL for both: the SAME two scans over a fixture carrying the pre-T-ff8a
# cleanup body, with the gate's name present only as prose.
{ printf '# oc_e2e_teardown_on_exit — named in a comment only\n'
  printf 'cleanup() { bash "$HERE/teardown.sh" || true; }\n'; } > "$_ff8a_fix"
check "trap-scan control: the gate scan counts 0 when the name appears only in a comment" \
  "0" "$(grep -cE '^[^#]*oc_e2e_teardown_on_exit' "$_ff8a_fix" || true)"
check "trap-scan control: the direct-call scan counts 1 on the pre-T-ff8a cleanup body" \
  "1" "$(grep -cE '^[^#]*bash "\$HERE/teardown\.sh"' "$_ff8a_fix" || true)"

# 20f) MUTANTS — without them 20b is satisfied by a chain that was never going to
# delete anything. One edit each, against the THROWAWAY tree's copy of the lib.
ff8a_mutant() { # ff8a_mutant NAME SED_EXPR
  local name="$1" expr="$2" mut="$SHIMDIR/ff8a-mut-$1.sh"
  sed "$expr" "$HERE/../lib/common.sh" > "$mut"
  if cmp -s "$mut" "$HERE/../lib/common.sh"; then
    bad "MUT-$name did not change lib/common.sh — the mutation anchor moved; a vacuous mutant proves nothing"
    return
  fi
  local rc; rc="$(ff8a_run run_all.sh "$mut")"
  local n; n="$(ff8a_recorded)"
  [[ "$n" -gt 0 ]] \
    && ok "MUT-$name: with that check removed, a REFUSED setup deletes $n target(s) — 20b is pinned to it" \
    || bad "MUT-$name: a refused setup still deleted nothing (rc=$rc) — 20b is NOT pinned to this check, it would pass without it"
  cp "$HERE/../lib/common.sh" "$FF8A_E2E/lib/common.sh"
}
# 1. the gate itself: make the armed-check answer yes unconditionally — this is
#    literally the pre-T-ff8a behaviour ("the trap runs regardless").
ff8a_mutant gate 's|^oc_e2e_teardown_armed() .*$|oc_e2e_teardown_armed() { return 0; }|'
# 2. the gate's USE: have the trap helper run teardown without consulting it. The
#    check can survive as a function and still be wired to nothing.
ff8a_mutant use 's|^  if ! oc_e2e_teardown_armed; then$|  if false; then|'
[[ -f "$FF8A_SENTINEL" ]] \
  && ok "after both mutants, the throwaway sentinel is STILL there — breaking the guard cannot make this test destructive" \
  || bad "a mutant DELETED the throwaway sentinel — this test file relies on the guard it is testing for its own safety"

# ── 21) T-42bb: the seven_gate VERDICT — it must go red, and name the step ────
#
# seven_gate/judge.py decides whether a seven-step run happened, reading ONLY
# what the server was observed to hold. The thing that can silently rot in it is
# not "does a good run pass" — a judge that returns PASS unconditionally does
# that too. It is "does a run MISSING one step go red, and does it say WHICH".
# So the shape below is: one green fixture as the control, then SEVEN mutants,
# one per step, each removing exactly that step's fact from the bundle. Each must
# exit 1 AND name its own step on the last line. A mutant that reddens the wrong
# step is as bad as one that stays green — the caller acts on the name.
#
# HERMETIC: no server, no network. The bundle is a handful of JSON objects
# written here, which is the whole reason collect.py (I/O) and judge.py (pure)
# are separate files.
SG_DIR="$HERE/../seven_gate"
SG_WORK="$SHIMDIR/seven-gate"
mkdir -p "$SG_WORK"

# The full-green bundle, as a python emitter so a mutant is one deleted key.
# `python3` here is the same text-tool use lib/common.sh already makes of it.
cat > "$SG_WORK/mk.py" <<'PY'
import hashlib, json, os, sys
drop, out = sys.argv[1], sys.argv[2]
AG, NONCE = "m-sg", "sg-nonce-deadbeef"
PEER, PEER_NONCE = "m-sg-peer", "sg-peer-nonce-feedface"
IMG_ANSWER, IMG_SALT = "481902", "sg-salt-c0ffee"
REPLAN = os.environ.get("SG_REPLAN") == "1"

# ── the plan, and the TWO SHAPES OF IT THE JOURNAL SEES ─────────────────────
# ⑤ is a TIME fact now ("a step was done while the task was still open"), so the
# fixture has to be a time series and not one final snapshot. Two versions of
# the same task row are therefore emitted: a MID-FLIGHT one (step0 done, no
# close-out) and a FINAL one (everything done, close-out reported).
#
# The step_done mutant is the real exposure this fixture exists to pin, and it
# is now a state the SERVER CAN ACTUALLY PRODUCE: the mid-flight sample shows
# the task open with NOTHING done yet, and the final sample shows the whole plan
# done with the close-out already reported — i.e. the plan was back-filled in one
# go at the close. closeout_reported stays TRUE, so it is a bundle where ⑤ is red
# and ⑦ is green.
# ⚠️ THE PREVIOUS VERSION OF THIS FIXTURE WAS NOT REACHABLE and therefore proved
# nothing: it asserted step0.status="todo" together with task.status="done" and
# closeout_reported=true, and DeriveTaskStatus cannot derive `done` while a step
# is not done. It demonstrated that the predicate could be falsified, not that
# the world it described exists.
def step(i, name, status, fin):
    return {"id": "s%d" % i, "name": name, "order_idx": i - 1, "status": status,
            "started_ts": max(0.0, fin - 10.0) if fin else 0, "finished_ts": fin}

if REPLAN:
    # 21b-iii's world: the agent RE-PLANNED (submit_plan froze a node it did not
    # re-list into `superseded`, and ReplaceTaskPlan leaves it in place,
    # renumbered) and the two live nodes are a PARALLEL pair that finished
    # backwards. Both are ordinary server behaviour; both used to be red.
    mid_steps = [step(1, "被取代的舊節點", "superseded", 190.0),
                 step(2, "並行 A", "done", 175.0),
                 step(3, "並行 B", "in_progress", 0)]
    final_steps = [step(1, "被取代的舊節點", "superseded", 190.0),
                   step(2, "並行 A", "done", 175.0),
                   step(3, "並行 B", "done", 160.0)]
else:
    mid_steps = [step(1, "走完七步", "in_progress" if drop == "step_done" else "done",
                      0 if drop == "step_done" else 150.0),
                 step(2, "回報收尾", "in_progress", 0)]
    final_steps = [step(1, "走完七步", "done", 150.0),
                   step(2, "回報收尾", "done", 180.0)]

def task_row(steps, updated, status, closed):
    return {"id": "T-1", "creator_id": AG, "title": "probe", "created_ts": 100,
            "updated_ts": updated, "status": status,
            "steps": [] if drop == "submit_plan" else steps,
            "closeout_reported": closed}

mid = task_row(mid_steps, 150, "in_progress", False)
final = task_row(final_steps, 200, "done", drop != "closeout")
# The scratch ticket: same creator, EARLIER created_ts, no plan on it. Nothing
# on the server distinguishes it from the real one.
draft = {"id": "T-0-draft", "creator_id": AG, "title": "草稿", "created_ts": 50,
         "updated_ts": 60, "status": "not_started", "steps": [],
         "closeout_reported": False}
# 🔴 THE THIRD-PARTY ROW, AND IT IS NOW REALLY HERE. The comment that used to sit
# on this line said "A THIRD-PARTY TASK ROW is always present: ③ must key on
# creator_id, not on 'a task exists'" — and there was no such row in the fixture,
# so relaxing ③'s `creator_id == agent` to `True` left the whole suite at
# PASS=268 FAIL=0, silent (MEASURED). It is EARLIER than both of the agent's
# tickets and carries no plan, so a creator-blind judge picks it as "the
# earliest task" and ④ goes red — which is what makes the relaxation loud.
other = {"id": "T-other", "creator_id": "m-someone-else", "title": "別人的票",
         "created_ts": 10, "updated_ts": 20, "status": "in_progress",
         "steps": [], "closeout_reported": False}
def tasks_at(row):
    # The third-party row is present in EVERY sample, including the create_task
    # mutant's — that mutant must go red because no row carries the agent's id,
    # not because the server happened to hold no tasks at all.
    if drop == "create_task":
        return [other]
    return [other] + ([draft] if os.environ.get("SG_DRAFT") == "1" else []) + [row]

# 🔴 THE OWNER'S OWN CARD, AND IT IS NOW REALLY HERE — FIRST in the list, so a
# judge that stops keying on `from == agent` finds it instead. judge.py ⑥ says
# "a card the harness opened on its behalf proves nothing, which is why the
# initiator is checked"; with only the agent's card in the fixture, relaxing that
# check to `True` was silent (MEASURED: PASS=268 FAIL=0).
owner_card = {"id": "rc-owner", "from": "owner", "status": "waiting"}
cards_final = [owner_card] + ([] if drop == "reply_card" else
                              [{"id": "rc-1", "from": AG, "status": "waiting"}])

chat_final = (
    ([] if drop == "resume_scene" else
     [{"id": "c1", "from": AG, "to": "owner", "body": "接回現場：" + NONCE}])
    # 🔴 THE OWNER-ADDRESSED MESSAGE THAT CARRIES THE PEER'S NONCE. ⑧ is two
    # different claims — `to == peer` ("it talked to a colleague at all", which
    # judge.py calls the half the owner asked for) and the nonce ("it was a
    # reply"). Dropping `to == peer` while keeping the nonce check used to be
    # silent (MEASURED: PASS=268 FAIL=0) because nothing in the fixture quoted
    # the peer's nonce anywhere but in a message to the peer. This row is an
    # agent that reports UPWARD about the colleague without ever answering the
    # colleague, and ⑧ must not accept it.
    + [{"id": "c1b", "from": AG, "to": "owner",
        "body": "我看到隔壁那條線的記號了：" + PEER_NONCE + "，稍後處理。"}]
    # ⑧'s fact: agent → PEER, quoting what the peer said.
    + ([] if drop == "peer_message" else
       [{"id": "c2", "from": AG, "to": PEER, "body": "收到：" + PEER_NONCE}])
    # ⑨'s fact: the agent SAID the number that only the picture carries.
    + ([] if drop == "image_answer" else
       [{"id": "c3", "from": AG, "to": "owner",
         "body": "圖上的號碼是 " + IMG_ANSWER}]))

samples = [
    {"t": 1.0, "member": {"id": AG, "presence": "offline"},
     "chat": [], "tasks": [other], "reply_cards": [owner_card]},
    {"t": 2.0,
     "member": {"id": AG, "presence": "online" if drop == "report_waking" else "waking"},
     "chat": [], "tasks": [other], "reply_cards": [owner_card]},
    # MID-FLIGHT: the task is open and (unless the step_done mutant is on) one
    # step is already done. This sample is ⑤'s entire evidence.
    {"t": 5.0, "member": {"id": AG, "presence": "online"},
     "chat": [], "tasks": tasks_at(mid), "reply_cards": [owner_card]},
    {"t": 9.0, "member": {"id": AG, "presence": "online"},
     "chat": chat_final, "tasks": tasks_at(final), "reply_cards": cards_final},
]
os.makedirs(out, exist_ok=True)
# scene.json carries salt+sha256, never the number — see judge.py's header and
# seven_gate/run.sh 3c-bis.
json.dump({"agent_id": AG, "scene_nonce": NONCE,
           "peer_id": PEER, "peer_nonce": PEER_NONCE,
           "image_answer_salt": IMG_SALT,
           "image_answer_len": len(IMG_ANSWER),
           "image_answer_sha256":
               hashlib.sha256((IMG_SALT + IMG_ANSWER).encode("utf-8")).hexdigest()},
          open(out + "/scene.json", "w"))
with open(out + "/journal.ndjson", "w") as fh:
    for s in samples:
        fh.write(json.dumps(s, ensure_ascii=False) + "\n")
PY

sg_judge() { # sg_judge DROP -> prints "<rc>|<last line>"
  local drop="$1" dir="$SG_WORK/b-$1"
  rm -rf "$dir"
  python3 "$SG_WORK/mk.py" "$drop" "$dir" >/dev/null 2>&1 || { echo "9|fixture-build-failed"; return; }
  local outp rc
  outp="$(python3 "$SG_DIR/judge.py" "$dir" 2>&1)"; rc=$?
  printf '%s|%s\n' "$rc" "$(printf '%s\n' "$outp" | tail -n 1)"
}

# 21a) the control: nothing dropped → green, and the marker is EXACT. Without
# this the seven mutants below are satisfied by a judge that fails everything.
_sg="$(sg_judge none)"
check "seven_gate: a complete run exits 0" "0" "${_sg%%|*}"
check "seven_gate: a complete run's last line is the exact marker" \
  "[seven_gate] all green" "${_sg#*|}"

# 21b) ONE MUTANT PER STEP — that step's fact removed from the bundle each time. Both halves are
# asserted per mutant: rc must be 1 (green would mean the gate cannot say no)
# and the last line must name THAT step (a red pointing elsewhere sends the
# reader to the wrong place, which costs more than no red at all).
sg_mutant() { # sg_mutant KEY ZH
  local key="$1" zh="$2" res rc last
  res="$(sg_judge "$key")"; rc="${res%%|*}"; last="${res#*|}"
  check "seven_gate: with 「${zh}」 missing, the verdict is RED" "1" "$rc"
  case "$last" in
    *"failed at step"*"$key"*) ok "seven_gate: the RED names 「${zh}」 ($key) — $last" ;;
    *) bad "seven_gate: 「${zh}」 was missing but the verdict named something else: $last" ;;
  esac
}
# ⚠️ NO `sg_mutant report_waking` — ON PURPOSE, same as ⑤ below, and 21b-v is
# what replaces it. ① stopped being a gate (owner's ruling 2026-08-11, after ⑤)
# because the field it reads is written BY THIS HARNESS in every round:
# presence=="waking" is derived from `waking_since`, and reconcile stamps that on
# the landed START that run.sh's own owner-side `activate` causes, before the
# agent runs. A mutant here would assert the opposite of the contract.
sg_mutant resume_scene  接回現場
sg_mutant create_task   開票
sg_mutant submit_plan   提出計畫
# ⚠️ NO `sg_mutant step_done` — ON PURPOSE, and 21b-v below is what replaces it.
# ⑤ is no longer a gate (owner's ruling, 2026-08-11): it prints what it observed
# and cannot make a run red, because the thing it wanted to judge is not in the
# data — the server stamps WHEN EACH REPORT ARRIVED and never whether work
# happened between two reports. A mutant here would now assert the opposite of
# the contract. 21b-v pins the downgrade itself, in both directions.
sg_mutant reply_card    開一張等我回覆卡
sg_mutant closeout      回報收尾
sg_mutant peer_message  回覆另一個-agent
sg_mutant image_answer  看得到圖

# 21b-i) ⑤ HAS DISCRIMINATING POWER OF ITS OWN, AND IN A REACHABLE WORLD.
# ⑤ used to ask "does ANY step carry done", and ⑦ (closeout) is terminal-only
# while a task is terminal only when every non-superseded step is done
# (DeriveTaskStatus) — so ⑦ green IMPLIED ⑤ green and no bundle could exist where
# ⑤ was red and ⑦ was not. The prefix/ordering version that replaced it bought
# almost nothing back (with ⑦ green the prefix half is true by construction) and
# was RED ON REPLAN AND ON PARALLEL — see 21b-iii.
# ⑤ now reads a TIME fact: was the task ever OBSERVED carrying a done step while
# it had not yet reported its close-out. ⑦ reads the last state only and can say
# nothing about the states passed through, so the two are independent — and the
# bundle that separates them is a plain back-fill: nothing done mid-flight, the
# whole plan done and closed out in the final sample. Every row of it is a state
# the server can produce, which the previous fixture (step todo + task done) was
# not.
# ⚠️ 2026-08-11: THE CELL THIS PARAGRAPH DESCRIBES NO LONGER JUDGES ANYTHING.
# The time fact it reads separates a fast honest agent from a slow one, not an
# honest agent from a cheat — measured, see 21b-v and judge.py's ⑤ block. What
# survives here is the BUNDLE: the back-fill world is still the interesting one
# and it is still REACHABLE, which is the half the old fixture got wrong. What is
# asserted about it changed from "⑤ goes red on it" to "⑤ CANNOT redden it".
_sg_bf="$(sg_judge step_done)"
check "seven_gate: the back-fill bundle (nothing done mid-flight, whole plan done and closed out at the end) exits 0 — ⑤ is an observation and cannot redden a run on its own" \
  "0" "${_sg_bf%%|*}"
check "seven_gate: …and its last line is the exact green marker (a downgraded cell must not leave a half-red verdict behind either)" \
  "[seven_gate] all green" "${_sg_bf#*|}"
# …and the bundle it says that about must be REACHABLE. The old one asserted a
# `todo` step on a `done` task, which DeriveTaskStatus cannot produce: it proved
# the predicate was falsifiable, not that the world existed.
_sg_reach="$(python3 -c '
import json, sys
last = None
for line in open(sys.argv[1]):
    line = line.strip()
    if line:
        last = json.loads(line)
for t in last.get("tasks") or []:
    if t.get("id") != "T-1":
        continue
    bad = [s for s in t.get("steps") or []
           if t.get("status") == "done" and s.get("status") not in ("done", "superseded")]
    print("unreachable" if bad else "reachable")
    break
else:
    print("no-task")' "$SG_WORK/b-step_done/journal.ndjson" 2>/dev/null)"
check "seven_gate: …and the back-fill bundle is a state the server can actually reach (no un-done step on a done task)" \
  "reachable" "$_sg_reach"

# 21b-v) 🔴 THE GATE/OBSERVATION SPLIT ITSELF — pinned in BOTH directions.
#
# WHY THIS CASE EXISTS. TWO cells have been downgraded from gates to
# observations, for two different reasons, and both downgrades are the right
# answer AND exactly the shape this repo keeps getting hurt by: a check that
# stops checking while everything still prints green.
#   ⑤ — what it wanted to judge is NOT IN THE DATA (judge.py's ⑤ block has the
#        measurements): the server stamps when each report arrived, never whether
#        work happened between two reports.
#   ① — the field it reads IS WRITTEN BY THIS HARNESS in every round: presence
#        =="waking" derives from `waking_since`, and reconcile stamps that on the
#        landed START that run.sh's own owner-side `activate` causes, before the
#        agent runs. It was a gate that could not say no about its own
#        population — an unfalsifiable green, which is worse than a false red.
# So the membership of OBSERVATION_KEYS is not a comment, it is an assertion —
# someone re-arming a downgraded cell goes red HERE, and so does someone quietly
# moving a real gate into the observation set. Both directions matter: the first
# resurrects a verdict nobody can stand behind, the second is how a gate
# disappears without anyone noticing.
#
# 🔴 THE BOUNDARY OF THIS CASE, SAID BEFORE ANYONE HAS TO DISCOVER IT:
#
#   VERBATIM COMPARISON ONLY STOPS THE LINES WE THOUGHT OF. "THE SCREEN DOES NOT
#   LIE" IS NOT AN ENUMERABLE PROPERTY, AND THIS GUARD IS BOUNDED.
#
# TWO rounds of independent review have each found three new ways to make this
# harness print something false while the whole suite stayed all green — the
# gate count (a wrong number), the position (a line saying "below" printed under
# the cells), ⑤'s numbers (a constant), a SECOND NOTE line printed next to the
# true one, ⑤'s PREFIX rewritten into a claim that the agent was verified, and
# ①'s content pinned to a fixed sighting. Every one of those is now pinned. The
# next reviewer will find a seventh. THAT IS NOT THIS GUARD FAILING — it never
# promised the universal. What it holds, stated no wider than the mechanism:
# THESE lines, byte for byte, on TWO fixtures whose values differ, and EXACTLY
# ONE GATE-COUNT line — a line matching `NOTE: <n> of the …`, which is the only
# shape the count and the position checks look at.
# ⚠️ OTHER `NOTE:` LINES ARE NOT PINNED, and that is not hypothetical: judge.py
# already prints an unpinned `NOTE: the journal is EMPTY …`, and a review mutant
# printing `NOTE: every cell below is a GATE — this run verified all nine.` was
# green. An earlier draft of this paragraph claimed "no unaccounted-for NOTE
# line", which promised the whole NOTE population while the mechanism only ever
# covered the gate-count subset — a boundary written wider than what it guards is
# the same defect as a guard that stopped guarding.
#
# What that buys, precisely: an edit to any pinned sentence must be a DELIBERATE
# edit here too. What it does not buy: safety from a sentence nobody pinned.
# ⇒ If you are adding output to judge.py that a reader could act on, pin it here
#   in the same commit. If you are reviewing and you find number seven, that is
#   the guard working as designed — add it, do not conclude the guard was broken.
_sg_labels() { # _sg_labels JUDGE BUNDLE -> "<gates>|<observations>|<obs keys>"
  python3 - "$1" "$2" <<'PY'
import json, os, subprocess, sys
judge, bundle = sys.argv[1], sys.argv[2]
subprocess.run([sys.executable, judge, bundle], capture_output=True)
rows = json.load(open(os.path.join(bundle, "verdict.json")))
# 🔴 THE TYPE IS PART OF THE CONTRACT, AND IT WAS NOT PINNED. This function used
# to ask only `passed is None`, so a cell that returned a truthy non-boolean
# sentinel (measured: a review mutant made ⑦ return the string 'not-checked')
# printed PASS on screen, wrote 'not-checked' into verdict.json, AND was still
# counted here as one of the eight gates. The machine-readable contract is
# true/false/null — anything else is a shape nobody downstream can read, so it
# is named rather than silently bucketed.
bad = [r for r in rows if r["passed"] is not None and not isinstance(r["passed"], bool)]
if bad:
    print("BAD-TYPE:" + ",".join("%s=%r" % (r["key"], r["passed"]) for r in bad))
    raise SystemExit(0)
obs = [r for r in rows if r["passed"] is None]
print("%d|%d|%s" % (len(rows) - len(obs), len(obs), ",".join(r["key"] for r in obs)))
PY
}
check "seven_gate: the verdict declares 7 GATES and exactly 2 OBSERVATIONS, and they are ① and ⑤ (a cell that stopped deciding must say so in the machine-readable output, not only in a comment)" \
  "7|2|report_waking,step_done" "$(_sg_labels "$SG_DIR/judge.py" "$SG_WORK/b-none")"
# …and on screen. `passed: null` in a file nobody opens is not a label. BOTH
# downgraded cells are checked, and BOTH ARE PINNED THE SAME WAY: whole line,
# byte for byte, on two fixtures whose values differ. The asymmetry this replaces
# was measured — ⑤ had a verbatim suffix plus a counter-fixture while ① had two
# substrings, so a mutant that made ① print a FIXED sighting ("seen at t=0.0") on
# a bundle where the agent was NEVER seen waking left the suite at 319/0. The
# weakly-pinned cell was the newly-downgraded one, whose entire remaining job is
# to say whether and when it saw anything.
_sg_line() { # _sg_line BUNDLE STEPPREFIX -> that one output line
  python3 "$SG_DIR/judge.py" "$1" 2>&1 | grep -E "^\[seven_gate\] $2 "
}
_sg_obs_line="$(_sg_line "$SG_WORK/b-none" 'step5 step_done')"
_sg_obs1_line="$(_sg_line "$SG_WORK/b-none" 'step1 report_waking')"
case "$_sg_obs_line" in
  *OBSERVED*) ok "seven_gate: …and ⑤'s line reads OBSERVED, not PASS — $(printf '%s' "$_sg_obs_line" | cut -c1-120)…" ;;
  *) bad "seven_gate: ⑤'s line does not say OBSERVED, so a reader counts it as a step that was verified: $_sg_obs_line" ;;
esac
case "$_sg_obs1_line" in
  *OBSERVED*) ok "seven_gate: …and ①'s line reads OBSERVED, not PASS — $(printf '%s' "$_sg_obs1_line" | cut -c1-120)…" ;;
  *) bad "seven_gate: ①'s line does not say OBSERVED, so a reader counts it as a step that was verified: $_sg_obs1_line" ;;
esac
# ⑤ AND ① — WHOLE LINE, VERBATIM, INCLUDING THE PART BEFORE THE COLON. The old
# form cut at a marker and compared only the SUFFIX, so the whole "why this cell
# cannot decide" preamble was free text: a mutant rewrote ⑤'s prefix into
# "⑤ CONFIRMS THIS AGENT REPORTED EACH STEP AS THE WORK WENT" and the suite
# stayed at 319/0. The preamble is the half a reader forms their conclusion from.
check "seven_gate: …and ⑤'s line is EXACTLY this, preamble included (comparing only the suffix let a mutant rewrite the preamble into a claim that the agent was verified — measured)" \
  '[seven_gate] step5 step_done      報一步完成 OBSERVED — OBSERVED, NOT JUDGED (this cell cannot make the run red — the server records when each report ARRIVED, never whether work happened between two reports, so nothing here separates '"'"'reported as the work went'"'"' from '"'"'reported all at once'"'"'): task T-1: 2 of 2 plan step(s) at done; 2 distinct server-stamped finished_ts; first→last completion 30.000s (server-stamped, not sampled); first completion→close-out sighting not comparable in this bundle (the sample clock reads before the server'"'"'s finished_ts)' \
  "$_sg_obs_line"
check "seven_gate: …and ①'s line is EXACTLY this, preamble included (it says WHY it cannot decide — without that the downgrade is a null in a file plus one English word)" \
  '[seven_gate] step1 report_waking  報到 OBSERVED — OBSERVED, NOT JUDGED (this cell cannot make the run red — on the live path, where a warden is online before the harness activates the member, reconcile stamps the `waking_since` this projection derives from when the START lands, before the agent runs; so a sighting here is not evidence the agent reported anything): member m-sg was seen at presence=waking at t=2.0' \
  "$_sg_obs1_line"
# …and ①'s COUNTER-FIXTURE, the half ⑤ already had and ① did not: on a bundle
# where the agent was never seen waking, that line must CHANGE. Without this the
# assertion above is satisfied by a cell that ignores the bundle entirely.
rm -rf "$SG_WORK/b-report_waking"
python3 "$SG_WORK/mk.py" report_waking "$SG_WORK/b-report_waking" >/dev/null 2>&1 \
  || bad "seven_gate: could not build the ① counter-fixture — the assertion under it would be testing nothing"
_sg_obs1_neg="$(_sg_line "$SG_WORK/b-report_waking" 'step1 report_waking')"
check "seven_gate: …and on a bundle where the agent was NEVER seen waking, ①'s line follows the bundle (this is what separates 'it reports what it saw' from 'it prints a sentence')" \
  '[seven_gate] step1 report_waking  報到 OBSERVED — OBSERVED, NOT JUDGED (this cell cannot make the run red — on the live path, where a warden is online before the harness activates the member, reconcile stamps the `waking_since` this projection derives from when the START lands, before the agent runs; so a sighting here is not evidence the agent reported anything): no sample ever showed member m-sg at presence=waking' \
  "$_sg_obs1_neg"
# …and THAT bundle still exits 0 — the cost of the downgrade, asserted so nobody
# rediscovers it by accident. On the control tree (f29f63c2) this same bundle was
# rc=1 with `RED — failed at step1 report_waking`. "This agent never reported in"
# is now something NO cell in this harness will say no to.
python3 "$SG_DIR/judge.py" "$SG_WORK/b-report_waking" >/dev/null 2>&1
check "seven_gate: …and that bundle is GREEN (rc=0) — ① cannot redden a run, which is exactly the capability the downgrade gave up; if this ever goes to 1 again, the ruling changed" \
  "0" "$?"
# 🔴 THE GATE-COUNT LINE IS PINNED VERBATIM, NOT COUNTED, AND ITS POSITION IS
# PINNED SEPARATELY. This used to be `grep -c 'cells below are GATES'` == 1,
# i.e. "that substring appears once" — nothing about the NUMBER, nothing about
# WHERE. Independent review 2026-08-11 planted three mutants in the real
# judge.py and the whole suite stayed at PASS=303 FAIL=0:
#   * `len(gates)` → `len(verdicts)` ⇒ the screen said "9 of the 9 cells below
#     are GATES … read a green run as 'the 9 gates held'" — the exact misreading
#     the downgrade exists to prevent, printed BY THE HARNESS, while
#     verdict.json still recorded 8|1|step_done. Screen and file contradicted
#     each other and nothing spoke.
#   * the whole gates/obs/NOTE block moved BELOW the per-cell loop ⇒ a line
#     saying "the cells BELOW" printed under step9.
#   * a constant `return "OBSERVED: 0 distinct …"` in _observe_step_shape ⇒ ⑤'s
#     only remaining function (reporting the shape truthfully) replaced by a
#     fixed lie.
# So: the sentence is compared whole, the position is compared to step1's, and
# ⑤'s numbers are pinned to THIS fixture's known values with a counter-fixture
# that must change them. ⚠️ EDITING THE SENTENCE IN judge.py MEANS EDITING THE
# LINE BELOW — that is the cost of pinning it, and it is the point.
# …AND THERE MUST BE EXACTLY ONE OF THEM. This used to be `| head -n 1`, and the
# position check took the FIRST match too — so nothing required the line to be
# unique. A mutant printing a SECOND NOTE line right beside the true one ("9 of
# the 9 cells below are GATES — this run verified all nine.") left the suite at
# 319/0: the first line was still correct, and the second could say anything.
_sg_head_n="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-none" 2>&1 | grep -cE '^\[seven_gate\] NOTE: [0-9]+ of the')"
check "seven_gate: …and there is EXACTLY ONE gate-count line (a second one printed next to the true one said the opposite and was silent — measured)" \
  "1" "$_sg_head_n"
_sg_head_line="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-none" 2>&1 | grep -E '^\[seven_gate\] NOTE: [0-9]+ of the')"
check "seven_gate: …and the GATE-COUNT line above the cells is EXACTLY this sentence (counting the substring let a mutant print '9 of the 9' and stay green — measured)" \
  '[seven_gate] NOTE: 7 of the 9 cells below are GATES (their fact is absent ⇒ the run is red). THE REST ARE OBSERVATIONS — step1 report_waking (報到), step5 step_done (報一步完成) — they print what they saw and CANNOT make this run red. Read a green run as "the 7 gates held", never as "9 things were verified".' \
  "$_sg_head_line"
_sg_head_pos="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-none" 2>&1 | awk '
  /^\[seven_gate\] NOTE: [0-9]+ of the/ { c++; if (!n) n = NR }
  /^\[seven_gate\] step1 / { if (!s) s = NR }
  END { if (!n) print "no-note-line"; else if (c > 1) print "note-printed-" c "-times";
        else if (!s) print "no-step1-line";
        else print (n < s ? "note-above-cells" : "note-below-cells") }')"
check "seven_gate: …and that line is printed ABOVE the cells it talks about (it says 'the cells below'; a mutant that moved it under step9 was silent — same shape as case 23d's 'preflight line < spend line')" \
  "note-above-cells" "$_sg_head_pos"
# ⑤'s COUNTER-FIXTURE (its whole line is already pinned above, on b-none, whose
# plan is two done steps stamped 150.0 and 180.0 — 2 distinct stamps, 30.000s
# apart). Collapse the two stamps into one and BOTH numbers
# must follow. Without this, the line above is still only "⑤ prints a string
# somebody wrote down once".
python3 - "$SG_WORK/b-none" "$SG_WORK/b-onestamp" <<'PY'
import json, os, shutil, sys
src, dst = sys.argv[1], sys.argv[2]
if not os.path.isdir(dst):
    os.makedirs(dst)
shutil.copy(os.path.join(src, "scene.json"), os.path.join(dst, "scene.json"))
rows = []
for line in open(os.path.join(src, "journal.ndjson"), encoding="utf-8"):
    if not line.strip():
        continue
    s = json.loads(line)
    for t in s.get("tasks") or []:
        for st in t.get("steps") or []:
            if st.get("status") == "done" and st.get("finished_ts"):
                st["finished_ts"] = 150.0
    rows.append(json.dumps(s, ensure_ascii=False))
with open(os.path.join(dst, "journal.ndjson"), "w", encoding="utf-8") as fh:
    fh.write("\n".join(rows) + "\n")
PY
_sg_one_shape="$(python3 -c '
import json, sys
last = [json.loads(l) for l in open(sys.argv[1]) if l.strip()][-1]
t = [x for x in last["tasks"] if x["id"] == "T-1"][0]
done = [s for s in t["steps"] if s["status"] == "done"]
print("%d done|%d distinct" % (len(done), len({s["finished_ts"] for s in done})))
' "$SG_WORK/b-onestamp/journal.ndjson" 2>/dev/null)"
check "seven_gate: the one-stamp counter-fixture really carries two done steps sharing a single finished_ts (otherwise the assertion under it proves nothing)" \
  "2 done|1 distinct" "$_sg_one_shape"
_sg_one_line="$(_sg_line "$SG_WORK/b-onestamp" 'step5 step_done')"
check "seven_gate: …and on that bundle ⑤'s line CHANGES with it, whole line (this is what separates 'it reports the shape' from 'it prints a sentence')" \
  '[seven_gate] step5 step_done      報一步完成 OBSERVED — OBSERVED, NOT JUDGED (this cell cannot make the run red — the server records when each report ARRIVED, never whether work happened between two reports, so nothing here separates '"'"'reported as the work went'"'"' from '"'"'reported all at once'"'"'): task T-1: 2 of 2 plan step(s) at done; 1 distinct server-stamped finished_ts; first→last completion n/a (2 done steps share ONE server stamp); first completion→close-out sighting not comparable in this bundle (the sample clock reads before the server'"'"'s finished_ts)' \
  "$_sg_one_line"
# ⚠️ AND IT MUST NOT SAY "a one-step plan is a legitimate way to get here" ON A
# LINE THAT ALSO SAYS "2 of 2". That sentence was the ONLY else-branch until
# 2026-08-11, so every multi-step plan that shared a stamp (or carried none) got
# a line that contradicted itself half-way through.
_sg_one_contra="$(printf '%s' "$_sg_one_line" | grep -cF 'a one-step plan is a legitimate way to get here')"
check "seven_gate: …and it does NOT call a 2-step plan a one-step plan (the self-contradicting line the else-branch used to print)" \
  "0" "$_sg_one_contra"
# MUT-regate / MUT-degrade — the declaration moved, on a COPY of judge.py.
# Each mutant judges its OWN bundle (judge.py writes verdict.json into whatever
# directory it is given, and a shared one would let the last writer decide).
SG_JMUT="$SG_WORK/judge-mut.py"
python3 "$SG_WORK/mk.py" none "$SG_WORK/b-regate"  >/dev/null 2>&1 \
  && python3 "$SG_WORK/mk.py" none "$SG_WORK/b-degrade" >/dev/null 2>&1 \
  || bad "seven_gate: could not build the 21b-v mutant bundles — the two cells below would be testing nothing"
sed 's/^OBSERVATION_KEYS = ("report_waking", "step_done")$/OBSERVATION_KEYS = ()/' \
    "$SG_DIR/judge.py" > "$SG_JMUT"
if ! grep -q '^OBSERVATION_KEYS = ()$' "$SG_JMUT"; then
  bad "seven_gate: MUT-regate did not apply — the OBSERVATION_KEYS declaration moved, so 21b-v is testing nothing (fix the sed)"
else
  check "MUT-regate: with ① and ⑤ put back in the gate set, the split is visibly different (a re-armed observation cannot slip past this case)" \
    "9|0|" "$(_sg_labels "$SG_JMUT" "$SG_WORK/b-regate")"
  # …and ON SCREEN too: with nothing downgraded there is no gate-count line at
  # all. Only verdict.json was checked here before, so the screen half of the
  # label had no mutant of its own in either direction.
  _sg_regate_note="$(python3 "$SG_JMUT" "$SG_WORK/b-regate" 2>&1 | grep -cE '^\[seven_gate\] NOTE: [0-9]+ of the')"
  check "MUT-regate: …and the gate-count line disappears from the screen with it (nothing is downgraded, so there is nothing to warn about)" \
    "0" "$_sg_regate_note"
fi
sed 's/^OBSERVATION_KEYS = ("report_waking", "step_done")$/OBSERVATION_KEYS = ("report_waking", "step_done", "closeout")/' \
    "$SG_DIR/judge.py" > "$SG_JMUT"
if ! grep -q '"report_waking", "step_done", "closeout"' "$SG_JMUT"; then
  bad "seven_gate: MUT-degrade did not apply — the OBSERVATION_KEYS declaration moved, so the other direction is testing nothing (fix the sed)"
else
  check "MUT-degrade: quietly moving a REAL gate (⑦) into the observation set is caught and named — this is the direction in which a gate disappears silently" \
    "6|3|report_waking,step_done,closeout" "$(_sg_labels "$SG_JMUT" "$SG_WORK/b-degrade")"
fi
# …and a HALF re-arm is caught too. Both directions above move the whole set; a
# third shape moves ONE member, which is what someone "tidying up" actually does
# — and it is the shape that would quietly put ① back to deciding a run on a
# field the harness writes itself.
sed 's/^OBSERVATION_KEYS = ("report_waking", "step_done")$/OBSERVATION_KEYS = ("step_done",)/' \
    "$SG_DIR/judge.py" > "$SG_JMUT"
if ! grep -q '^OBSERVATION_KEYS = ("step_done",)$' "$SG_JMUT"; then
  bad "seven_gate: MUT-rearm-one did not apply — the OBSERVATION_KEYS declaration moved (fix the sed)"
else
  check "MUT-rearm-one: re-arming JUST ① (the shape a tidy-up produces) is caught and named, not averaged away by the other observation staying put" \
    "8|1|step_done" "$(_sg_labels "$SG_JMUT" "$SG_WORK/b-degrade")"
fi
# 21b-vi) FAIL-CLOSED, AND THE SENTENCE UNDERNEATH IT. A gate that produced no
# verdict at all is red — that half was already true and review confirmed it by
# hand. What was NOT true is the evidence line: the cell's else-branch text goes
# out unchanged, so a ⑦ that decided nothing printed "task T-1 has
# closeout_reported=false" over a bundle where it is TRUE. The red is right and
# the sentence under it is wrong, which is the exact failure mode this repo
# keeps paying for. _seal() is the one place the conversion happens, so it is
# asserted directly rather than through a judge.py mutant.
_sg_seal="$(cd "$SG_DIR" && python3 -c '
import judge
rows = judge._seal([("closeout", "回報收尾", None, "EVIDENCE-FROM-THE-ELSE-BRANCH"),
                    ("step_done", "報一步完成", None, "obs"),
                    ("create_task", "開票", True, "ok")])
print("|".join("%s=%r,%s" % (k, p, "MARKED" if judge.NOTHING_DECIDED in w else "bare")
               for k, _z, p, w in rows))' 2>&1)"
check "seven_gate: a GATE that decided nothing is red AND its evidence says so, while an OBSERVATION keeps its null and a real verdict is untouched (fail-closed, and the red points at judge.py instead of the agent)" \
  "closeout=False,MARKED|step_done=None,bare|create_task=True,bare" "$_sg_seal"

# 21b-vii) 🔴 A BROKEN PLANT MUST NOT BE WORDED AS AN AGENT FAILURE. ⑧ and ⑨
# each already had one branch that says "This is a HARNESS red, not an agent
# red" — for a missing peer_id and a missing image digest. Neither covered the
# EMPTY-VALUE case, and the fall-through accused the agent verbatim (review
# 2026-08-11, measured on the real judge.py):
#   step8 … chat message c2 runs m-sg → m-sg-peer but does NOT carry the peer's
#   nonce '' — the agent spoke to the colleague without showing it read what the
#   colleague said
# An empty nonce can never be matched (`peer_nonce and peer_nonce in body`), so
# that sentence was structurally unearnable — the harness blaming the agent for
# its own missing plant, which is the shape ⑤ was downgraded for.
_sg_plant() { # _sg_plant KEY VALUE STEPGREP -> the evidence line for that cell
  python3 - "$SG_WORK/b-none" "$SG_WORK/b-plant" "$1" "$2" <<'PY'
import json, os, shutil, sys
src, dst, key, value = sys.argv[1:5]
if os.path.isdir(dst):
    shutil.rmtree(dst)
shutil.copytree(src, dst)
path = os.path.join(dst, "scene.json")
scene = json.load(open(path, encoding="utf-8"))
scene[key] = value
json.dump(scene, open(path, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
PY
  python3 "$SG_DIR/judge.py" "$SG_WORK/b-plant" 2>&1 | grep -E "^\[seven_gate\] $3 "
}
#
# 🔴 ② IS THE SAME BUG POINTING THE OTHER WAY, AND IT IS THE WORSE HALF: it does
# `nonce in body`, and `"" in body` is TRUE for every message — so an unplanted
# scene_nonce made ② PASS on the agent's first word. MEASURED 2026-08-11 on the
# green fixture: scene_nonce="" ⇒ step2 PASS, `all green`, rc=0, nothing said.
# A gate that stops gating while everything prints green is the whole subject of
# this round, so it is asserted here next to its two siblings.
for _sg_pl in "peer_nonce|step8|step8 peer_message" \
              "image_answer_salt|step9|step9 image_answer" \
              "scene_nonce|step2|step2 resume_scene"; do
  _sg_pl_key="${_sg_pl%%|*}"; _sg_pl_rest="${_sg_pl#*|}"
  _sg_pl_step="${_sg_pl_rest%%|*}"; _sg_pl_grep="${_sg_pl_rest#*|}"
  _sg_pl_line="$(_sg_plant "$_sg_pl_key" "" "$_sg_pl_grep")"
  case "$_sg_pl_line" in
    *"This is a HARNESS red, not an agent red"*)
      ok "seven_gate: an EMPTY $_sg_pl_key in scene.json makes $_sg_pl_step say the plant is broken, not that the agent failed" ;;
    *) bad "seven_gate: an EMPTY $_sg_pl_key in scene.json does not name the harness: $_sg_pl_line" ;;
  esac
  case "$_sg_pl_line" in
    *"the agent spoke to the colleague without showing"*|*"nothing shows the picture was opened"*|*"nothing shows the prior scene was read back"*)
      bad "seven_gate: …and it still carries the sentence that accuses the agent: $_sg_pl_line" ;;
    *) ok "seven_gate: …and it does NOT carry the sentence that accuses the agent ($_sg_pl_step)" ;;
  esac
  # …and it is FAIL. Two different ways this matters: a harness red must stay a
  # red (naming the culprit is not excusing the run), and ②'s empty needle must
  # not be the PASS it used to be.
  case "$_sg_pl_line" in
    *" FAIL — "*) ok "seven_gate: …and $_sg_pl_step is still FAIL, not PASS (empty $_sg_pl_key)" ;;
    *) bad "seven_gate: …and $_sg_pl_step is not FAIL on an empty $_sg_pl_key: $_sg_pl_line" ;;
  esac
done
# …and WHITESPACE counts as empty. `peer_nonce` and `salt` were .strip()ed from
# the start; `scene_nonce` was not, so a nonce of one space walked straight past
# the empty check and `" " in body` matched an UNRELATED message — ② PASS, rc=0
# (measured on the fix before this one). Three fields, one rule.
_sg_ws_line="$(_sg_plant scene_nonce " " 'step2 resume_scene')"
case "$_sg_ws_line" in
  *"This is a HARNESS red, not an agent red"*)
    ok "seven_gate: …and a scene_nonce of pure WHITESPACE is treated as the broken plant it is, not matched against every message" ;;
  *) bad "seven_gate: a whitespace-only scene_nonce does not name the harness: $_sg_ws_line" ;;
esac

# 21b-iii) ⑤ MUST NOT BE RED ON THE TWO THINGS THE SERVER DOES ON PURPOSE.
# This is the other half of 21b-i and it is the half that was broken: a bundle
# with (a) a `superseded` replan record sitting BEFORE later work — exactly where
# ReplaceTaskPlan leaves it, renumbered, while DeriveTaskStatus/TaskProgress skip
# it — and (b) two parallel nodes whose finished_ts run BACKWARDS along
# order_idx, which SPEC §3.1 permits by construction. Both are what a correct
# agent produces; both were a deterministic RED that named the agent.
rm -rf "$SG_WORK/b-replan"
SG_REPLAN=1 python3 "$SG_WORK/mk.py" none "$SG_WORK/b-replan" >/dev/null 2>&1
# The fixture has to really contain both shapes, or this case is a green that
# proves nothing.
_sg_rp_shape="$(python3 -c '
import json, sys
last = [json.loads(l) for l in open(sys.argv[1]) if l.strip()][-1]
t = [x for x in last["tasks"] if x["id"] == "T-1"][0]
sup = [s for s in t["steps"] if s["status"] == "superseded"]
done = [s for s in t["steps"] if s["status"] == "done"]
fts = [s["finished_ts"] for s in sorted(done, key=lambda s: s["order_idx"])]
back = any(a > b for a, b in zip(fts, fts[1:]))
first_is_sup = t["steps"][0]["status"] == "superseded"
print("%s|%s|%s" % (bool(sup) and first_is_sup, back, t.get("closeout_reported")))
' "$SG_WORK/b-replan/journal.ndjson" 2>/dev/null)"
check "seven_gate: the replan/parallel fixture really carries a leading superseded row AND backwards finished_ts" \
  "True|True|True" "$_sg_rp_shape"
_sg_rp_out="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-replan" 2>&1)"; _sg_rp_rc=$?
check "seven_gate: a replanned plan with a parallel group finishing out of order is GREEN (⑤ is not red on correct agent behaviour)" \
  "0" "$_sg_rp_rc"
case "$(printf '%s\n' "$_sg_rp_out" | tail -n 1)" in
  "[seven_gate] all green") ok "seven_gate: …and it is green on the marker, not by accident" ;;
  *) bad "seven_gate: the replan/parallel bundle did not end green: $(printf '%s\n' "$_sg_rp_out" | tail -n 3 | tr '\n' '|')" ;;
esac

# 21b-iv) THE FIXTURE'S OWN INTEGRITY — three rows whose absence used to make
# three cells unguarded while the comments claimed otherwise. Each was MEASURED
# silent: relaxing ③'s creator_id, ⑧'s `to == peer` or ⑥'s `from == agent` to a
# constant left the whole suite at PASS=268 FAIL=0. The mutants are caught by the
# per-step cases above ONLY because these rows exist, so they are asserted here
# by name: deleting one must not be a quiet loss of reach.
_sg_bundle="$SG_WORK/b-none/journal.ndjson"
check "seven_gate: the bundle carries a task row from SOMEBODY ELSE (③ must key on creator_id, not on 'a task exists')" \
  "1" "$(grep -qF 'm-someone-else' "$_sg_bundle" && echo 1 || echo 0)"
check "seven_gate: the bundle carries a reply card opened by the OWNER (⑥ must key on the initiator, not on the count)" \
  "1" "$(grep -qF '"rc-owner"' "$_sg_bundle" && echo 1 || echo 0)"
_sg_upward="$(python3 -c '
import json, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
hit = [m for s in rows for m in (s.get("chat") or [])
       if m.get("from") == "m-sg" and m.get("to") == "owner"
       and "sg-peer-nonce-feedface" in (m.get("body") or "")]
print(1 if hit else 0)' "$_sg_bundle" 2>/dev/null)"
check "seven_gate: the bundle carries an OWNER-addressed message quoting the peer's nonce (⑧'s 'to == peer' half must be load-bearing)" \
  "1" "$_sg_upward"

# 21b-ii) ③ TAKES THE EARLIEST TASK, AND THAT IS A GUESS — the one guess in that
# file. An agent that opens a scratch ticket before the real one gets ④⑤⑦ judged
# against the wrong row (MEASURED: ③ PASS pointing at the draft, ④⑤⑦ all FAIL,
# first red 「提出計畫 FAIL — task … has an empty steps[]」). It cannot be fixed by
# picking whichever task satisfies ④⑤⑦ — that would make those cells
# unfalsifiable — and it cannot be fixed with a planted marker, because
# assignment.md deliberately never tells the agent to open a ticket at all. So
# the requirement is that the verdict SAYS SO instead of silently accusing the
# agent.
rm -rf "$SG_WORK/b-draft"
SG_DRAFT=1 python3 "$SG_WORK/mk.py" none "$SG_WORK/b-draft" >/dev/null 2>&1
_sg_draft_out="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-draft" 2>&1)"
case "$_sg_draft_out" in
  *"OPENED 2 TASKS"*"T-0-draft"*)
    ok "seven_gate: with a draft ticket opened first, the verdict names BOTH tasks and says it judged the earliest" ;;
  *) bad "seven_gate: two tasks from the same agent produced no warning — ④⑤⑦'s reds would silently accuse the agent of the harness's own guess: $_sg_draft_out" ;;
esac
_sg_draft_last="$(printf '%s\n' "$_sg_draft_out" | tail -n 1)"
case "$_sg_draft_last" in
  *"suspect a draft"*) ok "seven_gate: …and the FIRST RED itself carries the hint — $_sg_draft_last" ;;
  *) bad "seven_gate: the last line (the one a caller acts on) does not mention the extra ticket: $_sg_draft_last" ;;
esac
# …and it must stay a WARNING that fires only when it applies: a hint printed on
# every run is one nobody reads by the time it matters.
_sg_single_out="$(python3 "$SG_DIR/judge.py" "$SG_WORK/b-none" 2>&1)"
case "$_sg_single_out" in
  *"OPENED"*) bad "seven_gate: a run with ONE task still prints the multi-task warning — a warning that is always on is noise" ;;
  *) ok "seven_gate: a run with one task prints no multi-task warning (the hint fires only when it applies)" ;;
esac

# 21c) an EMPTY journal must not read as a pass. This is the failure mode a
# collector crash produces, and "no evidence" answering green is the one bug
# that would make every future run meaningless.
rm -rf "$SG_WORK/b-empty"; mkdir -p "$SG_WORK/b-empty"
printf '{"agent_id":"m-sg","scene_nonce":"n"}' > "$SG_WORK/b-empty/scene.json"
: > "$SG_WORK/b-empty/journal.ndjson"
python3 "$SG_DIR/judge.py" "$SG_WORK/b-empty" >/dev/null 2>&1
check "seven_gate: an EMPTY journal is RED, not green" "1" "$?"

# 21d) the friction wording is the load-bearing part of the follow-up and it
# lives in exactly ONE file. Pinned verbatim, because the way this stops working
# is someone "tidying" it into 「順不順」 — which returns a pleasantry every time
# and therefore returns nothing.
SG_FRICTION="$SG_DIR/friction.md"
check "seven_gate: friction Q1 is verbatim" "1" \
  "$(grep -cF '哪一步你猶豫了／翻回去重讀了／用猜的？' "$SG_FRICTION" || true)"
check "seven_gate: friction Q2 is verbatim" "1" \
  "$(grep -cF '你有沒有做出後來才發現做錯的事？' "$SG_FRICTION" || true)"
# The banned phrasings may be NAMED in the prose that explains why they are
# banned, so the scan is for a QUESTION — the phrase followed by a question mark.
_sg_bad="$(grep -cE '(順不順|順利嗎|有沒有問題|還可以嗎)[？?]' "$SG_FRICTION" || true)"
check "seven_gate: friction asks none of the pleasantry questions" "0" "${_sg_bad:-0}"
# run.sh must READ that file rather than carry its own copy of the questions —
# two copies drift, and the one that drifts is the one that gets asked.
_sg_reads="$(grep -cF 'friction.md' "$SG_DIR/run.sh" || true)"
[[ "${_sg_reads:-0}" -gt 0 ]] \
  && ok "seven_gate: run.sh sources the questions from friction.md (no second copy)" \
  || bad "seven_gate: run.sh no longer reads friction.md — the questions have been copied, and copies drift"

# 21e) the default actor spawns nothing. This file cannot prove what a live
# actor costs, but it CAN pin that the default is not one: run.sh's fallback
# actor must be the stub, and the stub must not reach for a claude binary.
_sg_def="$(grep -cE 'OC_SG_ACTOR:-\$HERE/actors/stub\.sh' "$SG_DIR/run.sh" || true)"
[[ "${_sg_def:-0}" -gt 0 ]] \
  && ok "seven_gate: run.sh's default actor is the stub (no agent spawned unless asked)" \
  || bad "seven_gate: run.sh's default actor is no longer the stub — a bare run may now burn API quota"
_sg_claude="$(grep -cE '(^|[^a-z])claude([^a-z]|$)' "$SG_DIR/actors/stub.sh" || true)"
check "seven_gate: the stub actor never invokes claude" "0" "${_sg_claude:-0}"

# 21f) NO SERVER CALL MAY BE MADE OUTSIDE THE ONE LOGGING HELPER. This is the
# bug that cost the first baseline: every call was `curl … >/dev/null`, so the
# three that the server REFUSED (a 409 each) looked exactly like the ones it
# accepted, and "the call failed" was indistinguishable from "the call worked
# and the fact still is not there" — which is the shape a wrong API contract
# takes. lib/http.sh writes the method, path, HTTP STATUS and BODY of every
# call; the invariant that keeps it true is that nothing else reaches curl.
# A reminder in a comment would not survive; this will.
# Comment lines are stripped first: these files EXPLAIN the banned shape in
# their headers, and a scan that cannot tell the rule from its own description
# reddens on the documentation — the fastest way to get a guard deleted.
_sg_code_only() { grep -v '^[[:space:]]*#' "$1"; }
for _sg_caller in "$SG_DIR/run.sh" "$SG_DIR"/actors/*.sh; do
  _sg_curl="$(_sg_code_only "$_sg_caller" | grep -cE '(^|[^[:alnum:]_])curl([^[:alnum:]_]|$)' || true)"
  check "seven_gate: $(basename "$_sg_caller") makes no raw curl call (every call goes through lib/http.sh, which logs status + body)" \
    "0" "${_sg_curl:-0}"
done
# …and the helper itself must not throw a response away. `-o <file>` + `-w
# %{http_code}` is the shape that keeps both halves; a curl in here piped to
# /dev/null would restore the blindness at its source.
_sg_helper_null="$(_sg_code_only "$SG_DIR/lib/http.sh" | grep -cE 'curl[^#]*>[[:space:]]*/dev/null' || true)"
check "seven_gate: lib/http.sh never sends a curl response to /dev/null" "0" "${_sg_helper_null:-0}"
# "log the whole body" and "never write a credential to disk" are both true only
# because the helper redacts. /api/mint and /api/machines answer with live
# bearer JWTs; the first run of this harness wrote three of them into run.log and
# http.log and bin/ci.sh's gitleaks gate caught it. Pinned as a behaviour, not a
# grep for the sed: a fixture body is pushed through the real function.
_sg_redact="$(SG_HTTP_LOG="" bash -c '. "$1"/lib/http.sh; _sg_http_oneline "{\"token\":\"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJtLTEifQ.c2lnbmF0dXJl\",\"id\":\"m-1\"}"' _ "$SG_DIR" 2>/dev/null)"
case "$_sg_redact" in
  *eyJ*) bad "seven_gate: lib/http.sh logs a bearer JWT verbatim — the harness would write live credentials to run.log/http.log (got: $_sg_redact)" ;;
  *REDACTED*id*m-1*) ok "seven_gate: lib/http.sh redacts credentials but keeps the rest of the body (got: $_sg_redact)" ;;
  *) bad "seven_gate: lib/http.sh's redaction ate the body — a redacted log that shows nothing else is as blind as no log (got: $_sg_redact)" ;;
esac
_sg_helper_code="$(grep -cF '%{http_code}' "$SG_DIR/lib/http.sh" || true)"
[[ "${_sg_helper_code:-0}" -ge 1 ]] \
  && ok "seven_gate: lib/http.sh captures the HTTP status code (a body without a status cannot separate a refusal from a no-op)" \
  || bad "seven_gate: lib/http.sh no longer captures %{http_code} — a refused call and an accepted one are indistinguishable again"

# 21g) the LIVE actor is default-off, and its opt-in is STRICT. e2e_test/CLAUDE.md
# records what the loose version cost: an EXCLUDE-shaped flag set in only one
# place meant every laptop spawned real agents and paid for them. The switch
# must be an INCLUDE flag compared exactly, so every typo lands on "did not run,
# did not spend".
if [[ -f "$SG_DIR/actors/live.sh" ]]; then
  _sg_live_optin="$(grep -cE 'OC_SG_LIVE_AGENT.*!=[[:space:]]*"1"' "$SG_DIR/actors/live.sh" || true)"
  [[ "${_sg_live_optin:-0}" -ge 1 ]] \
    && ok "seven_gate: live.sh refuses unless OC_SG_LIVE_AGENT is EXACTLY \"1\" (strict include-flag, not an exclude-flag)" \
    || bad "seven_gate: live.sh's spend opt-in is not a strict '!= \"1\"' refusal — a typo could now spawn a real agent and spend real quota"
  # It must ask the questions from the ONE file, never carry its own copy —
  # same reason 21d pins run.sh.
  _sg_live_fr="$(grep -cE 'sg_friction_questions|friction\.md' "$SG_DIR/actors/live.sh" || true)"
  [[ "${_sg_live_fr:-0}" -ge 1 ]] \
    && ok "seven_gate: live.sh takes the friction questions from friction.md (no second copy)" \
    || bad "seven_gate: live.sh no longer reads friction.md — the questions have been copied, and copies drift"
  # And it must not author the answers. The banned shape is a friction.txt
  # written from a string this file made up; the allowed one is the agent's own
  # messages. Pinned as: live.sh never claims an answer the agent did not send.
  _sg_live_verbatim="$(grep -cE '載體不代寫' "$SG_DIR/actors/live.sh" "$SG_DIR/friction.md" 2>/dev/null | awk -F: '{s+=$2} END {print s+0}')"
  [[ "${_sg_live_verbatim:-0}" -ge 1 ]] \
    && ok "seven_gate: the no-ghostwriting rule for friction answers is stated where the writer of friction.txt will read it" \
    || bad "seven_gate: nothing states that the harness must not write the friction answers itself"
fi

# ── 22) T-42bb: the collection window must outlast the actor ──────────────────
#
# THE BUG. collect.py used to be started with `--seconds 900` while
# actors/live.sh would wait 30 + 120 + 1800 + 300 ≈ 2250s. On DEFAULTS the
# collector stopped sampling ~22 minutes before the actor stopped working, so
# every fact that landed after that instant was invisible to judge.py — and the
# verdict it produced was 「回報收尾 FAIL」: A RED NAMING THE AGENT FOR THE
# HARNESS'S OWN GAP. The person who hit it worked around it by knowing to raise
# OC_SG_MAX_SECONDS; the next person would not have known that flag existed.
#
# So what is pinned here is the RELATION, not a number: whatever the knobs are
# set to, the collector's window must be >= the actor's budget. A future knob
# that lengthens the actor lengthens the budget, and this keeps holding with
# nobody remembering it.
SG_WINDOW="$SG_DIR/lib/window.sh"
sg_window_probe() { # sg_window_probe <file> [env assignments…] -> "budget|window|rc"
  local f="$1"; shift
  env "$@" bash -c '
    . "$1" || exit 9
    b="$(sg_actor_budget_secs)"; w="$(sg_collect_seconds)"
    sg_assert_collection_window >/dev/null 2>&1; rc=$?
    printf "%s|%s|%s\n" "$b" "$w" "$rc"' _ "$f"
}

# 22a) the shipped defaults hold — and the window is genuinely bigger, not equal
# by accident of both being zero.
_w="$(sg_window_probe "$SG_WINDOW")"
_wb="${_w%%|*}"; _rest="${_w#*|}"; _ww="${_rest%%|*}"; _wrc="${_rest##*|}"
check "seven_gate: the shipped defaults satisfy the collection-window invariant" "0" "$_wrc"
[[ "${_wb:-0}" -gt 0 && "${_ww:-0}" -gt "${_wb:-0}" ]] \
  && ok "seven_gate: collector window ${_ww}s strictly exceeds the actor budget ${_wb}s (not equal-by-accident)" \
  || bad "seven_gate: window=${_ww:-?} budget=${_wb:-?} — the window must strictly exceed a non-zero budget"

# 22b) it must track the ACTOR, not a constant: stretch the longest actor wait
# and the window has to grow with it. This is the half a hardcoded number fails.
_w2="$(sg_window_probe "$SG_WINDOW" OC_SG_LIVE_WAIT=99999)"
_w2b="${_w2%%|*}"; _r2="${_w2#*|}"; _w2w="${_r2%%|*}"; _w2rc="${_r2##*|}"
check "seven_gate: a much longer live wait still satisfies the invariant" "0" "$_w2rc"
[[ "${_w2w:-0}" -gt "${_ww:-0}" ]] \
  && ok "seven_gate: stretching OC_SG_LIVE_WAIT grew the collector window (${_ww}s → ${_w2w}s) — it is derived, not fixed" \
  || bad "seven_gate: OC_SG_LIVE_WAIT grew but the collector window did not (${_ww}s → ${_w2w}s) — the derivation is severed"

# 22c) THE MUTANT. Sever the derivation — put the old independent constant back —
# and the invariant must go RED. Without this, a sg_collect_seconds that returned
# a huge constant would satisfy 22a/22b's rc check and this case would guard
# nothing. The mutant is the exact historical bug, not an invented one.
SG_WMUT="$SHIMDIR/window-mutant.sh"
sed -e 's|^  echo \$(( \$(sg_actor_budget_secs) + OC_SG_SETTLE + OC_SG_COLLECT_MARGIN ))$|  echo 900|' \
    "$SG_WINDOW" > "$SG_WMUT"
if ! grep -qE '^  echo 900$' "$SG_WMUT"; then
  bad "seven_gate: the window mutant did not apply — the derivation line moved, so case 22c is testing nothing (fix the sed)"
else
  _wm="$(sg_window_probe "$SG_WMUT")"
  _wmrc="${_wm##*|}"
  check "seven_gate: with the derivation severed (collector window back to a constant 900), the invariant goes RED" "1" "$_wmrc"
fi

# 22d) the defaults have ONE home. A second `:-<default>` for any of these knobs
# in run.sh or the actors is a second constant, and two constants a human keeps
# in sync is the shape that produced the bug.
for _knob in OC_SG_LIVE_WAIT OC_SG_MACHINE_WAIT OC_SG_SPAWN_WAIT OC_SG_FRICTION_WAIT OC_SG_CARD_WAIT OC_SG_SETTLE; do
  _dupes="$(grep -h -oE "\\\$\{$_knob:-[^}]*\}" "$SG_DIR/run.sh" "$SG_DIR"/actors/*.sh 2>/dev/null | wc -l | tr -d ' ')"
  check "seven_gate: $_knob has no second default outside lib/window.sh" "0" "${_dupes:-0}"
done

# 22e) THE LAST COPY OF 900, and the one 22a–22d could not see. 22d only greps
# the SHELL files for a second `${KNOB:-…}`; the collector's own
# `--seconds` default lived in collect.py's argparse and was still literally
# 900. That is not a dead letter: it is what a caller that omits the flag gets,
# SILENTLY, which is the original bug exactly — and it was reachable while every
# assertion above stayed green. MEASURED on the pre-change tree: deleting
# `--seconds "$COLLECT_SECONDS"` from run.sh left tests_guard at PASS=251 FAIL=0
# rc=0, i.e. the whole of case 22 was blind to it.
#
# Pinned as a RELATION again, not a number: the collector must have NO window of
# its own, so it must REFUSE to start when nobody hands it one. Behavioural, and
# hermetic — argparse rejects before any socket or file is touched.
SG_COLLECT="$SG_DIR/collect.py"
#
# ⚠️ The claim is NOT "rc != 0". A collect.py that still carries a default gets
# past parsing and then dies on the unreadable token file — ALSO rc != 0, and
# indistinguishable. MEASURED: mutant C (default=900.0 put back) exits 1 on
# FileNotFoundError. So the assertion is that the refusal NAMES the window, and
# it is paired with the control below.
_c_noflag="$(python3 "$SG_COLLECT" --token-file /nonexistent --agent m-x --run-dir /nonexistent 2>&1)"; _c_noflag_rc=$?
if [[ "$_c_noflag_rc" -ne 0 && "$_c_noflag" == *"--seconds"* ]]; then
  ok "seven_gate: collect.py refuses to start without a window and names --seconds (rc=$_c_noflag_rc) — it carries no window of its own"
else
  bad "seven_gate: collect.py did not refuse for want of a window (rc=$_c_noflag_rc, said: $(echo "$_c_noflag" | tr '\n' '|')) — it is carrying its own default again, which is the 900 this case exists to kill"
fi
# POSITIVE CONTROL. Without it, "names --seconds" could be satisfied by a script
# that mentions the flag in every failure. With the window supplied, the SAME
# invocation must get PAST argument parsing and fail on the missing token file
# instead — a different failure, and one that never mentions --seconds.
_c_flag="$(python3 "$SG_COLLECT" --token-file /nonexistent --agent m-x --run-dir /nonexistent --seconds 1 2>&1)"
case "$_c_flag" in
  *"--seconds"*) bad "control broken: collect.py still complains about --seconds when it was given one — 22e cannot tell a missing window from a broken script" ;;
  *) ok "seven_gate: control — given a window, collect.py gets past parsing and dies on the token file instead" ;;
esac

# 22f) …and the caller must hand it the DERIVED value. 22e closes "a forgotten
# flag is silent"; it cannot see a caller that passes a literal. So the token
# after --seconds in run.sh's collector invocation must be a variable expansion,
# and that variable must be assigned from sg_collect_seconds. A number there —
# any number, not just 900 — fails.
_sec_tok="$(grep -oE -- '--seconds[[:space:]]+[^[:space:]]+' "$SG_DIR/run.sh" | head -1 | awk '{print $2}')"
# Pure bash on purpose: BSD and GNU sed disagree about \{n,m\}, and a guard that
# dies on the developer's sed is the a749470 shape all over again.
_sec_bare="$(printf '%s' "${_sec_tok:-}" | tr -d '"'"'"'{}')"
_sec_var=""
[[ "$_sec_bare" =~ ^\$([A-Za-z_][A-Za-z_0-9]*)$ ]] && _sec_var="${BASH_REMATCH[1]}"
if [[ -z "$_sec_var" ]]; then
  bad "seven_gate: run.sh passes --seconds ${_sec_tok:-<nothing>} — that is not a variable, so the collector's window is a constant again"
elif grep -qE "^[[:space:]]*$_sec_var=\"?\\\$\(sg_collect_seconds\)\"?[[:space:]]*$" "$SG_DIR/run.sh"; then
  ok "seven_gate: run.sh passes --seconds \$$_sec_var and $_sec_var comes from sg_collect_seconds — derived end to end"
else
  bad "seven_gate: run.sh passes --seconds \$$_sec_var but $_sec_var is not assigned from sg_collect_seconds — the derivation does not reach the collector"
fi

# ── 23) T-42bb: an unbound-variable typo in live.sh must go red WITHOUT a run ──
#
# WHAT HAPPENED. The first real-agent run died on actors/live.sh line 213:
#
#     actors/live.sh: line 213: OC_SG_LIVE_WAITs: unbound variable
#
# One letter — the `s` of "seconds" glued onto the NAME when the window.sh
# refactor rewrote `${OC_SG_LIVE_WAIT:-1800}s` as `$OC_SG_LIVE_WAIT`. The agent
# had ALREADY BEEN SPAWNED and ① had passed; the actor then died, its trap killed
# the tmux session, and ②..⑨ all went red. The verdict said 「the agent did
# nothing」; the truth was 「the harness killed it」. A paid run, zero information.
#
# WHY CI DID NOT CATCH IT. CI never executes live.sh — that file only runs on a
# real run, which is the one thing CI must never do. So the guard cannot be "run
# it": it has to walk every variable REFERENCE while spawning nothing. That is
# lib/varcheck.py, and this case is what makes it load-bearing rather than a
# script nobody calls.
#
# 🔴 SCOPE IS DRAWN BY CONSEQUENCE, NOT BY FILENAME — the same lesson case 24
# paid an outage for, seven lines away, and this list did not learn it. It used
# to be a hand-written roll-call of eight paths, and the exposure it guards
# belongs to a PROPERTY, not to those eight: any .sh under seven_gate/ is a file
# CI never executes and only a real, paid run does. MEASURED: lib/scrub.sh was
# added to this harness — it runs at the PRE-SPEND hop, the most expensive
# possible place to die — and a `$VARs` typo in it was completely silent, because
# nobody thought to add a ninth line here. So the scope is now the same QUERY
# case 24 uses (it picks up the next file somebody adds) plus a floor, because a
# walk that finds nothing passes every assertion by having none.
#
# ⚠️ `find -L`, NOT plain `find`: a plain walk does not DESCEND INTO A SYMLINKED
# DIRECTORY, so the query's "every .sh under seven_gate/" was not true of a file
# behind one. MEASURED (2026-08-11, both walks): a .sh carrying a `$VARs` typo
# AND a banned `pgrep`, planted under `lib/zzsymdir -> <tmpdir>`, left this case
# and case 24 completely green. `-L` also costs nothing here: with a symlink
# LOOP planted, /usr/bin/find -L still terminated and listed each real file once
# (measured; GNU-style finds print a "filesystem loop" error to stderr and exit
# 1, but the pipelines below read stdout and never look at find's rc).
SG_VARCHECK="$SG_DIR/lib/varcheck.py"
SG_VARFILES=()
_sg_varscanned=0
while IFS= read -r _sg_varf; do
  SG_VARFILES+=("$_sg_varf")
  _sg_varscanned=$(( _sg_varscanned + 1 ))
done < <(find -L "$SG_DIR" -name '*.sh' -type f | sort)
[[ "$_sg_varscanned" -ge 6 ]] \
  && ok "seven_gate: varcheck's scope is the directory — it walked $_sg_varscanned .sh files under seven_gate/ (not a roll-call somebody has to remember to extend)" \
  || bad "seven_gate: varcheck only found $_sg_varscanned .sh file(s) under $SG_DIR — a walk that finds nothing passes silently, which is exactly how this check lost its reach for lib/scrub.sh"
# …and the files that ONLY a paid run executes are named, so a future narrowing
# of the walk cannot quietly drop the expensive ones.
for _sg_varmust in actors/live.sh lib/ownedkill.sh lib/scrub.sh; do
  printf '%s\n' "${SG_VARFILES[@]}" | grep -Fqx "$SG_DIR/$_sg_varmust" \
    && ok "seven_gate: …including $_sg_varmust (CI never runs it; a typo there only ever surfaces on a paid run)" \
    || bad "seven_gate: $_sg_varmust is not in varcheck's reach — a one-letter typo there dies mid-run, after the money is spent"
done

# 23a) the shipped harness is clean.
# The output is KEPT, not sent to /dev/null: varcheck already prints file, line
# and variable name (23b/23c assert on exactly that), and this cell used to throw
# all of it away — so a real typo in a real file produced "want '0' got '1'" and
# nothing else, while the two mutant cells right below it named their variable.
# A red that does not say WHERE is a red somebody has to reproduce by hand.
_vc_ship_out="$(python3 "$SG_VARCHECK" "${SG_VARFILES[@]}" 2>&1)"; _vc_ship_rc=$?
if [[ "$_vc_ship_rc" -eq 0 ]]; then
  ok "seven_gate: every variable reference in the harness scripts is bound (no unbound-variable landmine)"
else
  bad "seven_gate: every variable reference in the harness scripts is bound (no unbound-variable landmine) — varcheck rc=$_vc_ship_rc: $(printf '%s' "$_vc_ship_out" | head -5 | tr '\n' ' ')"
fi

# 23b) THE MUTANT — put the exact typo back and the guard must go red, naming it.
# Without this, a varcheck.py that returned 0 unconditionally would satisfy 23a
# and this case would be guarding nothing. The mutant is the historical bug
# verbatim, not an invented one.
# The fixture tree MIRRORS the real layout (actors/ beside lib/): varcheck
# resolves `. "$SG/lib/window.sh"` by walking up from the file, so a copy dropped
# into a bare temp dir would report every knob as unbound — the control would
# fail and the mutant would "pass" for the wrong reason.
SG_VARTREE="$SHIMDIR/sg-vartree"
rm -rf "$SG_VARTREE"; mkdir -p "$SG_VARTREE/actors" "$SG_VARTREE/lib"
cp "$SG_DIR"/lib/*.sh "$SG_VARTREE/lib/"
cp "$SG_DIR/friction.md" "$SG_VARTREE/" 2>/dev/null || true
SG_VARMUT="$SG_VARTREE/actors/live.sh"
sed 's|deadline in ${OC_SG_LIVE_WAIT}s;|deadline in $OC_SG_LIVE_WAITs;|' \
    "$SG_DIR/actors/live.sh" > "$SG_VARMUT"
if ! grep -q 'OC_SG_LIVE_WAITs' "$SG_VARMUT"; then
  bad "seven_gate: the varcheck mutant did not apply — the line moved, so case 23b is testing nothing (fix the sed)"
else
  _vc_out="$(python3 "$SG_VARCHECK" "$SG_VARMUT" 2>&1)"; _vc_rc=$?
  check "seven_gate: with the typo put back, varcheck goes RED" "1" "$_vc_rc"
  case "$_vc_out" in
    *"OC_SG_LIVE_WAITs"*) ok "seven_gate: varcheck NAMES the typo'd variable — $(printf '%s' "$_vc_out" | head -1)" ;;
    *) bad "seven_gate: varcheck reddened but never named OC_SG_LIVE_WAITs, so it would not tell anyone what to fix: $_vc_out" ;;
  esac
  # …and the SAME tree without the typo must pass, or 23b's red proves nothing
  # about the typo (it could be the copy, the path, or the tool being broken).
  cp "$SG_DIR/actors/live.sh" "$SG_VARTREE/actors/live-control.sh"
  python3 "$SG_VARCHECK" "$SG_VARTREE/actors/live-control.sh" >/dev/null 2>&1
  check "seven_gate: the SAME copy without the typo passes (23b's red is the typo, not the fixture)" "0" "$?"
fi

# 23c) THE SAME TYPO IN THE NEWEST FILE — which is the half a roll-call could not
# do. 23b proves varcheck catches the historical bug in the file somebody
# remembered to list; this proves the SCOPE reaches a file nobody listed, by
# planting the identical one-letter shape in lib/scrub.sh. That file is worse
# than live.sh to die in: it runs at the PRE-SPEND hop, so a crash there is the
# harness refusing to spawn — silent, and indistinguishable from a machine that
# never came online.
SG_VARMUT2="$SG_VARTREE/lib/scrub-typo.sh"
sed 's|for p in \$SG_SCRUB_PREFIXES; do|for p in $SG_SCRUB_PREFIXESs; do|' \
    "$SG_DIR/lib/scrub.sh" > "$SG_VARMUT2"
if ! grep -q 'SG_SCRUB_PREFIXESs' "$SG_VARMUT2"; then
  bad "seven_gate: the scrub varcheck mutant did not apply — the loop moved, so case 23c is testing nothing (fix the sed)"
else
  _vc2_out="$(python3 "$SG_VARCHECK" "$SG_VARMUT2" 2>&1)"; _vc2_rc=$?
  check "seven_gate: a \$VARs typo in lib/scrub.sh goes RED (the scope is the directory, so a new file is covered the day it lands)" \
    "1" "$_vc2_rc"
  case "$_vc2_out" in
    *"SG_SCRUB_PREFIXESs"*) ok "seven_gate: …and it NAMES the typo'd variable — $(printf '%s' "$_vc2_out" | head -1)" ;;
    *) bad "seven_gate: varcheck reddened on lib/scrub.sh but never named SG_SCRUB_PREFIXESs: $_vc2_out" ;;
  esac
  cp "$SG_DIR/lib/scrub.sh" "$SG_VARTREE/lib/scrub-control.sh"
  python3 "$SG_VARCHECK" "$SG_VARTREE/lib/scrub-control.sh" >/dev/null 2>&1
  check "seven_gate: the SAME copy of scrub.sh without the typo passes (23c's red is the typo, not the file)" "0" "$?"
fi

# 23d) the expensive-path preflight must come BEFORE the spend. live.sh spawns a
# real agent at the activate/tmux step; anything that can kill the actor AFTER
# that point burns money to produce nothing. The variables the late phases use
# are therefore forced to expand in a preflight ahead of it.
_pf_line="$(grep -n 'PRE-SPEND PREFLIGHT' "$SG_DIR/actors/live.sh" | head -1 | cut -d: -f1)"
_spend_line="$(grep -n 'activate' "$SG_DIR/actors/live.sh" | grep -v '^.*#' | head -1 | cut -d: -f1)"
if [[ -n "$_pf_line" && -n "$_spend_line" && "$_pf_line" -lt "$_spend_line" ]]; then
  ok "seven_gate: live.sh's pre-spend preflight (line $_pf_line) runs before the spend point (line $_spend_line)"
else
  bad "seven_gate: live.sh has no PRE-SPEND PREFLIGHT ahead of the activate that starts the spending (preflight=${_pf_line:-none} spend=${_spend_line:-none})"
fi

# 23e) …AND IT MUST PARSE UNDER THE BASH THAT ACTUALLY RUNS IT.
#
# 🔴 THE HOLE THIS CLOSES. varcheck answers "is every name bound"; it says
# nothing about SYNTAX. And the syntax that matters is not this developer's
# bash: every one of these scripts is `#!/usr/bin/env bash`, so WHICH bash runs
# them is decided by PATH — a Mac without Homebrew bash, or any trimmed
# launchd/cron PATH, resolves to the stock /bin/bash, which on macOS is still
# 3.2.57 (this repo already has a launchd-empty-PATH boot-death in its history).
# MEASURED at 68e3bfd1: lib/scrub.sh — the file that runs at the PRE-SPEND hop —
# could not run under 3.2 at all, while the whole suite was green here.
#
# ⚠️ SAID NARROWLY, BECAUSE THIS CHECK IS WEAKER THAN IT LOOKS: `-n` parses the
# file, and a command substitution's CONTENTS are parsed at EXPANSION time, not
# at file-parse time. So `-n` did NOT see the 68e3bfd1 bug (measured, and pinned
# as an assertion in case 26). This cell catches the class `-n` can see — a
# construct that is valid in bash 4+/5 and rejected outright by 3.2. The
# behavioural half of the same exposure lives in case 26, which runs the scrub
# under the stock interpreter for real.
SG_STOCK_BASH="/bin/bash"
if [[ ! -x "$SG_STOCK_BASH" ]]; then
  bad "seven_gate: there is no executable $SG_STOCK_BASH on this host, so the stock-interpreter parse check cannot run (23e is testing nothing)"
  bad "seven_gate: (23e mutant skipped — no stock bash)"
  bad "seven_gate: (23e control skipped — no stock bash)"
else
  _sg_stockver="$("$SG_STOCK_BASH" -c 'echo "${BASH_VERSION%%(*}"')"
  _sg_synbad=""
  for _sg_synf in "${SG_VARFILES[@]}"; do
    "$SG_STOCK_BASH" -n "$_sg_synf" 2>/dev/null || _sg_synbad="$_sg_synbad ${_sg_synf#$SG_DIR/}"
  done
  check "seven_gate: every .sh under seven_gate/ parses under the STOCK $SG_STOCK_BASH ($_sg_stockver), not just this shell's bash ($BASH_VERSION) — nothing here is green because of what is early on somebody's PATH" \
    "" "$_sg_synbad"
  # THE MUTANT, and it is the whole point of pinning the interpreter: a shape
  # that the developer's bash accepts and the stock one rejects. `|&` is bash 4.
  SG_SYNMUT="$SHIMDIR/sg-syntax-mut.sh"
  printf '#!/usr/bin/env bash\necho hi |%s cat\n' '&' > "$SG_SYNMUT"
  "$SG_STOCK_BASH" -n "$SG_SYNMUT" 2>/dev/null
  check "MUT-bash4syntax: a bash-4-only construct is REJECTED by the stock $SG_STOCK_BASH (so the cell above is a live check, not a walk over files nothing parses)" \
    "2" "$?"
  # …and the negative control, because "rejects everything" and "rejects this"
  # look identical: a trivially valid file must be ACCEPTED by the same binary.
  # (The other half — that THIS shell's bash accepts `|&` and 3.2 does not — is
  # deliberately NOT an assertion: tests_guard itself gets run under whatever
  # bash is first on PATH, and this suite's assertion COUNT must not depend on
  # the host. MEASURED by hand 2026-08-11 on this machine: `/bin/bash -n` rc=2
  # vs `bash -n` (5.3.9) rc=0 on the identical file.)
  printf '#!/usr/bin/env bash\necho hi\n' > "$SHIMDIR/sg-syntax-ok.sh"
  "$SG_STOCK_BASH" -n "$SHIMDIR/sg-syntax-ok.sh" 2>/dev/null
  check "MUT-bash4syntax: …and the same stock binary ACCEPTS an ordinary script (it is not rejecting everything, which would make the cell above vacuous)" \
    "0" "$?"
fi

# ── 24) T-42bb: the harness may only kill what it can PROVE it created ───────
#
# WHAT THIS IS ABOUT. actors/live.sh spawns a real agent into tmux and then has
# to clean up after itself. It used to do that on the socket named `officraft` —
# THE LIVE FLEET'S SOCKET, where serving members' sessions live. It killed only
# exact names, so nothing bad happened; but "nothing bad happened" was a property
# of the harness being CORRECT, and case (23) is the record of this same harness
# shipping a one-letter typo whose trap killed the agent it had just spawned. On
# the fleet socket that class of bug is not a red run, it is an irreversible kill
# of someone else's live agent.
#
# So lib/ownedkill.sh puts TWO INDEPENDENT layers between the harness and that
# outcome, and this case is what makes each of them load-bearing rather than a
# comment:
#
#   ① PHYSICAL — the run gets its OWN tmux socket (`officraft-<ns>`, via the
#      warden's OC_NAMESPACE), and sg_own_socket_assert refuses the fleet's
#      `officraft` outright. MUTANT: point the derivation back at the fleet.
#   ② OWNERSHIP — only the session names / pids this run WROTE DOWN at creation
#      time may be killed. MUTANT (twice): relax the kill to `pgrep -f` pattern
#      matching, and to "list the sessions and pick the ones that look like ours".
#
# AND A POSITIVE CONTROL, because both layers can be satisfied by a teardown that
# quietly kills NOTHING — and that green looks exactly like the real one. So a
# REAL process is spawned, recorded, and must actually die; a second identical
# process is NOT recorded, and must survive.
SG_OWNED="$SG_DIR/lib/ownedkill.sh"
SG_LIVE="$SG_DIR/actors/live.sh"
SG24="$SHIMDIR/sg24"; rm -rf "$SG24"; mkdir -p "$SG24/bin"
SG24_TMUX_LOG="$SG24/tmux.log"; : > "$SG24_TMUX_LOG"

# A RECORDING tmux, ahead of the file-wide stub on PATH. Every tmux command the
# ownership layer issues lands in one file, so "it killed nothing" and "it killed
# somebody else's session" stop being the same silence. It also answers
# list-sessions, which is what a "list and pick" mutant reaches for.
cat > "$SG24/bin/tmux" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SG24_TMUX_LOG"
[[ "${3:-}" == "list-sessions" || "${3:-}" == "ls" ]] && printf '%s\n' "${SG24_SESSIONS:-}"
exit 0
SH
chmod +x "$SG24/bin/tmux"
# The socket the fixture calls "ours", and the sessions the recording tmux will
# report as living on it: the one this run owns, plus a FOREIGN one. The foreign
# name is deliberately the same shape (`member-*`) as ours — that is the whole
# point, because on the real fleet socket it always will be.
SG24_SOCK="officraft-sg24"
SG24_OWNED_SESSION="member-m-sg24"
SG24_FOREIGN_SESSION="member-someone-else"
SG24_SESSIONS="$SG24_OWNED_SESSION
$SG24_FOREIGN_SESSION"
SG24_SLEDGER="$SG24/session-ledger"

sg24_sessions() { # sg24_sessions LIB SOCKET LEDGER -> "<rc>"; tmux calls → $SG24_TMUX_LOG
  : > "$SG24_TMUX_LOG"
  PATH="$SG24/bin:$PATH" SG24_TMUX_LOG="$SG24_TMUX_LOG" SG24_SESSIONS="$SG24_SESSIONS" \
    bash -c '. "$1" || exit 9; sg_own_kill_sessions "$2" "$3"' _ "$1" "$2" "$3" \
    >"$SG24/sess.out" 2>&1
  echo $?
}
sg24_killed() { grep -c 'kill-session' "$SG24_TMUX_LOG" 2>/dev/null || true; }

# 24a) THE REFUSAL ITSELF, on the shipped lib. This is the line that turns "we
# are careful" into "it cannot happen", so it is exercised, not grepped.
_sg24_assert() { # _sg24_assert SOCKET -> "<rc>"; message → $SG24/assert.err
  bash -c '. "$1" || exit 9; sg_own_socket_assert "$2"' _ "$SG_OWNED" "$1" \
    2>"$SG24/assert.err" >/dev/null
  echo $?
}
check "ownedkill: sg_own_socket_assert REFUSES the live fleet socket 'officraft'" "2" "$(_sg24_assert officraft)"
grep -q 'LIVE FLEET' "$SG24/assert.err" \
  && ok "ownedkill: …and says so by name — $(head -1 "$SG24/assert.err")" \
  || bad "ownedkill: the fleet socket was refused but the message never named it, so nobody reading a log would know why (got: $(head -c 200 "$SG24/assert.err"))"
# The EMPTY socket is the other way onto a shared socket: `tmux` with no -L is
# tmux's own default, shared with every tmux on the host.
check "ownedkill: an EMPTY socket is refused too (that is tmux's shared default socket)" "2" "$(_sg24_assert '')"
check "ownedkill: this run's own namespaced socket is ACCEPTED (the assert is not a blanket 'no')" "0" "$(_sg24_assert "$SG24_SOCK")"

# 24b) THE DERIVATION IN live.sh IS REALLY NAMESPACED — evaluated, not grepped.
# A grep for "officraft-" would pass on a line that can still resolve to the bare
# fleet socket, which is exactly the historical shape
# (`${OC_SG_TMUX_SOCKET:-officraft}`). So the assignment lines are pulled out of
# the file and RUN, and we look at what comes out.
sg24_socket_of() { # sg24_socket_of FILE -> the socket that file would use
  bash -c 'set -u
           eval "$(grep -E "^OC_SG_NAMESPACE=|^TMUX_SOCKET=" "$1")"
           printf "%s\n" "${TMUX_SOCKET:-}"' _ "$1" 2>/dev/null
}
_sg24_live_sock="$(sg24_socket_of "$SG_LIVE")"
[[ -n "$_sg24_live_sock" ]] \
  && ok "seven_gate: live.sh's socket derivation still evaluates (got '$_sg24_live_sock')" \
  || bad "seven_gate: live.sh's socket derivation produced NOTHING — the TMUX_SOCKET/OC_SG_NAMESPACE assignments moved and 24b/24c are testing nothing"
[[ "$_sg24_live_sock" != "officraft" ]] \
  && ok "seven_gate: live.sh does NOT resolve to the fleet socket (got '$_sg24_live_sock')" \
  || bad "seven_gate: live.sh's tmux socket resolves to the LIVE FLEET socket 'officraft' — a teardown here can reach serving members' sessions"
check "seven_gate: and that derived socket passes sg_own_socket_assert" "0" "$(_sg24_assert "$_sg24_live_sock")"
# PER-RUN, not a fixed namespace: two evaluations must differ. A constant
# namespace is off the fleet socket but shared between concurrent runs, so run A
# would be killing run B's sessions — the same bug one blast radius smaller.
[[ "$(sg24_socket_of "$SG_LIVE")" != "$_sg24_live_sock" ]] \
  && ok "seven_gate: the namespace is minted PER RUN (two evaluations differ) — two concurrent runs cannot share a socket" \
  || bad "seven_gate: two evaluations of live.sh's namespace produced the SAME socket '$_sg24_live_sock' — concurrent runs would share it and kill each other's sessions"
# …and the assert must be WIRED, in code. It can survive as a function in the lib
# and be called from nowhere (case 20f's second mutant is the same lesson).
_sg24_wired="$(_sg_code_only "$SG_LIVE" | grep -cE 'sg_own_socket_assert' || true)"
[[ "${_sg24_wired:-0}" -ge 1 ]] \
  && ok "seven_gate: live.sh runs its socket through sg_own_socket_assert in CODE (not just in a comment)" \
  || bad "seven_gate: live.sh no longer calls sg_own_socket_assert outside a comment — the refusal is wired to nothing"
# …and its teardown must go through the ledger helpers, not straight at tmux.
_sg24_rawkill="$(_sg_code_only "$SG_LIVE" | grep -cE 'tmux .*kill-session' || true)"
check "seven_gate: live.sh issues no raw tmux kill-session of its own (every kill goes through the ledger helpers)" "0" "${_sg24_rawkill:-0}"

# 24c) MUTANT ① — REMOVE THE SOCKET ISOLATION. The historical line goes back in,
# on a COPY (nothing under $SG_DIR is touched), and both halves must redden: the
# derivation resolves to the fleet socket, and the assert refuses it BY NAME
# before a single tmux command is issued.
SG24_LIVEMUT="$SG24/live-nosocket.sh"
sed 's|^TMUX_SOCKET="officraft-\$OC_SG_NAMESPACE"$|TMUX_SOCKET="${OC_SG_TMUX_SOCKET:-officraft}"|' \
    "$SG_LIVE" > "$SG24_LIVEMUT"
if cmp -s "$SG24_LIVEMUT" "$SG_LIVE"; then
  bad "seven_gate: MUT-nosocket did not change live.sh — the TMUX_SOCKET line moved, so case 24c is testing nothing (fix the sed)"
else
  _sg24_mut_sock="$(sg24_socket_of "$SG24_LIVEMUT")"
  check "MUT-nosocket: with the namespacing removed, live.sh resolves back to the FLEET socket" "officraft" "$_sg24_mut_sock"
  check "MUT-nosocket: …and sg_own_socket_assert REFUSES it (rc=2), so 24b is pinned to the assert" "2" "$(_sg24_assert "$_sg24_mut_sock")"
  case "$(cat "$SG24/assert.err")" in
    *officraft*LIVE\ FLEET*|*LIVE\ FLEET*officraft*)
      ok "MUT-nosocket: the refusal NAMES the fleet socket — $(head -1 "$SG24/assert.err")" ;;
    *) bad "MUT-nosocket: the refusal never named 'officraft' as the LIVE FLEET socket, so the red would not tell anyone what it just stopped (got: $(head -c 200 "$SG24/assert.err"))" ;;
  esac
  # END TO END: a cleanup aimed at that socket must issue ZERO tmux commands.
  printf '%s\n' "$SG24_OWNED_SESSION" > "$SG24_SLEDGER"
  check "MUT-nosocket: a cleanup aimed at the fleet socket exits 2" "2" "$(sg24_sessions "$SG_OWNED" "$_sg24_mut_sock" "$SG24_SLEDGER")"
  check "MUT-nosocket: …and issued NO tmux command at all — the refusal is before the kill, not after it" "0" "$(wc -l < "$SG24_TMUX_LOG" | tr -d ' ')"
fi

# 24d) POSITIVE CONTROL, sessions: ON ITS OWN SOCKET, WITH A LEDGER, IT REALLY
# KILLS. Without this the whole case is satisfied by a teardown that does nothing
# — and that green is indistinguishable from the real one.
printf '%s\n' "$SG24_OWNED_SESSION" > "$SG24_SLEDGER"
check "ownedkill: on its own socket, a ledgered session IS killed (rc)" "0" "$(sg24_sessions "$SG_OWNED" "$SG24_SOCK" "$SG24_SLEDGER")"
check "ownedkill: …exactly one kill-session was issued" "1" "$(sg24_killed)"
grep -Fq -- "-L $SG24_SOCK kill-session -t $SG24_OWNED_SESSION" "$SG24_TMUX_LOG" \
  && ok "ownedkill: …on OUR socket, naming the ledgered session exactly ($SG24_OWNED_SESSION)" \
  || bad "ownedkill: the kill did not name the ledgered session on our socket (recorded: $(tr '\n' '|' < "$SG24_TMUX_LOG"))"
grep -Fq "$SG24_FOREIGN_SESSION" "$SG24_TMUX_LOG" \
  && bad "ownedkill: a session that is NOT in the ledger ($SG24_FOREIGN_SESSION) was named — the ledger is not the authority" \
  || ok "ownedkill: the un-ledgered session $SG24_FOREIGN_SESSION was never touched"
# FAIL-CLOSED, both shapes. A missing record must leak (recoverable, visible),
# never kill (irreversible).
: > "$SG24_SLEDGER"
check "ownedkill: an EMPTY ledger kills nothing, and does not fail the run" "0" "$(sg24_sessions "$SG_OWNED" "$SG24_SOCK" "$SG24_SLEDGER")"
check "ownedkill: …zero tmux commands issued for an empty ledger" "0" "$(wc -l < "$SG24_TMUX_LOG" | tr -d ' ')"
check "ownedkill: an ABSENT ledger kills nothing either" "0" "$(sg24_sessions "$SG_OWNED" "$SG24_SOCK" "$SG24/no-such-ledger")"
check "ownedkill: …zero tmux commands issued for an absent ledger" "0" "$(wc -l < "$SG24_TMUX_LOG" | tr -d ' ')"

# 24e) POSITIVE CONTROL + OWNERSHIP, on REAL PROCESSES. No stub can prove this
# half: the claim is that a recorded pid dies and an identical un-recorded one
# lives, and "identical" is the load-bearing word — on the fleet the same binary
# runs with the same argv, which is why `pkill -f` is banned. So both sleepers
# are literally the same executable with the same command line.
SG24_MARK="sg24-$$-${RANDOM}"
SG24_SLEEPER="$SG24/$SG24_MARK"
# `exec -a` keeps the marker in the sleeper's OWN argv (so a pattern matcher can
# find it) while leaving exactly ONE process to kill and reap.
#
# 🔴 THE SLEEPER MUST OUTLIVE EVERY WATCH BELOW, and by a wide margin. If a decoy
# can reach the end of its own `sleep` while we are still watching it, "dead"
# stops meaning "somebody killed it" and starts meaning "time passed" — and the
# assertion that a mutant reached an un-recorded process would pass on a mutant
# that did nothing. That is not hypothetical: it was MEASURED here on 2026-08-27
# while fixing this very function. Every sleeper is killed and reaped explicitly,
# so the only cost of a long one is what happens if the suite is itself killed.
SG24_SLEEP_SECS=300
SG24_WATCH_SURVIVES=1   # seconds; the window that IS the detector for 24e
SG24_WATCH_DIES=15      # seconds; the ceiling on an event that must happen (24f)
printf '#!/usr/bin/env bash\nexec -a "$0" sleep %s\n' "$SG24_SLEEP_SECS" > "$SG24_SLEEPER"
chmod +x "$SG24_SLEEPER"
# Asserted, not assumed: this is the invariant that keeps "dead" honest, and the
# next person to tune a number here is the one it is written for.
[[ "$SG24_SLEEP_SECS" -gt $(( SG24_WATCH_DIES * 4 )) && "$SG24_SLEEP_SECS" -gt $(( SG24_WATCH_SURVIVES * 4 )) ]] \
  && ok "ownedkill: the sleeper lives ${SG24_SLEEP_SECS}s, far longer than the longest watch (${SG24_WATCH_DIES}s) — a decoy cannot reach its own end while we are looking at it, so 'dead' can only mean 'killed'" \
  || bad "ownedkill: the sleeper (${SG24_SLEEP_SECS}s) is not comfortably longer than the ${SG24_WATCH_DIES}s watch — a decoy that simply finishes would be read as KILLED, and 24f would pass on a mutant that touched nothing"
SG24_PLEDGER="$SG24/pid-ledger"
sg24_alive() { local s; s="$(ps -p "$1" -o state= 2>/dev/null | tr -d ' ')"; [[ -n "$s" && "$s" != Z* ]]; }
sg24_pids() { # sg24_pids LIB EXPECT_DECOY(survives|dies) -> "<owned alive|dead>|<decoy alive|dead>"
  local lib="$1" expect="$2" owned decoy o=dead d=alive watch t0
  # 🔴 THE DEADLINE IS NOT THE SAME QUESTION IN BOTH DIRECTIONS (T-1a7d, 2026-08-27).
  # This helper feeds two OPPOSITE assertions and used to give both the same
  # ten-round window. For one of them that window IS the detector; for the other
  # it silently rewrote the claim, and that is where the false red came from:
  #
  #  survives (24e, the shipped lib) — the claim is "nothing ever signals it".
  #    A bounded window is the only honest way to read that: watch for a while,
  #    report what you saw. The watch always runs to the end here (the decoy
  #    never dies), so it is a FIXED COST, never a race. Keep it.
  #
  #  dies (24f, the pgrep mutant) — the claim is "the pattern matcher reaches a
  #    process the ledger never named". That is a LIVENESS property: it says the
  #    kill HAPPENS, not that it happens within half a second. Bounding it the
  #    same way re-reads it as "…and the kernel reaps it within the window", a
  #    sentence nobody wrote and this machine breaks. MEASURED, with the loop
  #    reproduced in isolation and the decoy's death deliberately delayed by a
  #    `sleep` (no load applied — the lateness is manufactured, not borrowed):
  #    0 s / 0.3 s / 0.7 s late → both windows read `dead`; 1.5 s late → the old
  #    window reads `alive` and case 24f goes RED with nothing wrong. On a box
  #    carrying a dozen agents that lateness is ordinary. A guard that reddens
  #    for no reason is a guard somebody switches off — and switching THIS one
  #    off costs the live fleet (see 24f's header: 2026-08-11, `pkill -f
  #    "ocserverd serve"` planted in lib/carrier.sh, live ocserverd down 27 s).
  #    So this side waits up to ${SG24_WATCH_DIES}s and stops the instant the
  #    decoy dies: FREE when the mutant works, still red — just later — when it
  #    does not. A deadline on an event that must happen is not a loosened
  #    threshold on one that must not.
  #
  # ⚠️ WALL CLOCK, NOT A ROUND COUNT. The first draft of this fix said "200
  # rounds ≈ 10 s" and was WRONG BY 3×: the loop body forks `ps`, so a nominally
  # 50 ms round costs ~145 ms here — MEASURED 2026-08-27: 10 rounds = 1 s, 200
  # rounds = 29 s, on a box at load ~3.4. A count of rounds is a duration only if
  # you already know what the machine costs, which is precisely the thing that
  # varies. `date +%s` is portable back to the stock /bin/bash this suite is
  # careful to keep working.
  case "$expect" in
    survives) watch="$SG24_WATCH_SURVIVES" ;;
    dies)     watch="$SG24_WATCH_DIES" ;;
    # Not `bad` here: this runs inside a command substitution, where `bad`'s line
    # would be swallowed into the caller's variable and its FAIL++ lost with the
    # subshell. Emit a value no assertion can accept, so the caller's own checks
    # go red and print it.
    *) printf 'sg24_pids: unknown decoy expectation %s\n' "$expect" >&2
       printf 'UNKNOWN-EXPECTATION|UNKNOWN-EXPECTATION\n'; return 1 ;;
  esac
  "$SG24_SLEEPER" & owned=$!
  "$SG24_SLEEPER" & decoy=$!
  printf '%s\n' "$owned" > "$SG24_PLEDGER"
  SG24_MARK="$SG24_MARK" bash -c '. "$1" || exit 9; sg_own_kill_pids "$2"' _ "$lib" "$SG24_PLEDGER" >/dev/null 2>&1
  # `wait` is the synchronisation point: it returns only once the signal has
  # actually landed on the pid we expected to die.
  wait "$owned" 2>/dev/null
  sg24_alive "$owned" && o=alive
  t0="$(date +%s)"
  while :; do
    sg24_alive "$decoy" || { d=dead; break; }
    [[ $(( $(date +%s) - t0 )) -ge "$watch" ]] && break
    sleep 0.05
  done
  kill "$decoy" 2>/dev/null; wait "$decoy" 2>/dev/null
  printf '%s|%s\n' "$o" "$d"
}
_sg24_p="$(sg24_pids "$SG_OWNED" survives)"
check "ownedkill: POSITIVE CONTROL — the pid written to the ledger is really killed" "dead" "${_sg24_p%%|*}"
check "ownedkill: OWNERSHIP — an IDENTICAL process that was never recorded survives" "alive" "${_sg24_p#*|}"
# fail-closed on this side too: no ledger ⇒ the recorded-nowhere process lives.
"$SG24_SLEEPER" & _sg24_orphan=$!
bash -c '. "$1" || exit 9; sg_own_kill_pids "$2"' _ "$SG_OWNED" "$SG24/no-such-ledger" >/dev/null 2>&1
sg24_alive "$_sg24_orphan" \
  && ok "ownedkill: with NO pid ledger, nothing is killed (a leaked process is recoverable; a wrong kill is not)" \
  || bad "ownedkill: with no pid ledger something still died — the missing-record case is not fail-closed"
kill "$_sg24_orphan" 2>/dev/null; wait "$_sg24_orphan" 2>/dev/null

# 24f) MUTANT ②a — RELAX THE PID KILL TO PATTERN MATCHING. This is the exact
# shape root CLAUDE.md §13 bans and the exact shape that took the cockpit and
# every agent offline for two minutes: `pkill -f` / `pgrep -f` on a name, when
# the fleet runs the same binaries with the same argv. Name is not identity.
SG24_PIDMUT="$SG24/ownedkill-pgrep.sh"
sed 's|kill "$pid" 2>/dev/null|kill $(pgrep -f "$SG24_MARK") 2>/dev/null|' "$SG_OWNED" > "$SG24_PIDMUT"
if ! grep -q 'pgrep -f' "$SG24_PIDMUT"; then
  bad "seven_gate: MUT-pgrep did not apply to lib/ownedkill.sh — the exact-kill line moved, so case 24f is testing nothing (fix the sed)"
else
  _sg24_pm="$(sg24_pids "$SG24_PIDMUT" dies)"
  check "MUT-pgrep: the mutant still kills the process it owns (so the difference below is the PATTERN, not a broken mutant)" "dead" "${_sg24_pm%%|*}"
  check "MUT-pgrep: …and it ALSO kills the un-recorded look-alike — 24e is pinned to the ledger, not to luck" "dead" "${_sg24_pm#*|}"
fi
# The text scan that would have caught this one before it ran.
#
# 🔴 SCOPE IS DRAWN BY CONSEQUENCE, NOT BY FILENAME — this cost a real outage.
# The scan used to name two files (lib/ownedkill.sh, actors/live.sh), and the
# ban's blast radius has nothing to do with which two files somebody listed:
# ANY of these scripts runs on a machine where the live fleet's ocserverd,
# ocwarden and agents are running the same binaries with the same argv. MEASURED
# on the old scan, one mutant, three targets: planted in actors/live.sh → rc=1,
# named; planted in run.sh's cleanup() → rc=0, SILENT; planted in lib/carrier.sh
# → rc=0, SILENT. That last file is the one tests_guard case 25 EXECUTES as a
# fixture, and on 2026-08-11 somebody put `pkill -f "ocserverd serve"` into it to
# build a positive control, ran this suite, and took the live ocserverd down for
# 27 seconds. So the scope is now "every .sh under seven_gate/", which is a
# QUERY (it picks up the next file somebody adds) rather than a roll-call.
#
# Comments are stripped first: several of these files NAME the banned shape in
# their headers to explain why it is banned, and a scan that reddens on its own
# documentation is a scan somebody deletes. Those comments must stay green.
#
# ⚠️ `find -L` for the same reason case 23 spells out: plain `find` does not
# descend into a symlinked directory, so "every .sh under seven_gate/" was false
# of anything behind one — measured green with a banned shape planted there.
_sg24_scanned=0
while IFS= read -r _sg24_f; do
  _sg24_scanned=$(( _sg24_scanned + 1 ))
  _sg24_bans="$(_sg_code_only "$_sg24_f" | grep -cE '(^|[^[:alnum:]_])(pkill|killall|pgrep)([^[:alnum:]_]|$)' || true)"
  check "seven_gate: ${_sg24_f#$SG_DIR/} uses no pkill/killall/pgrep in code (name is not identity)" "0" "${_sg24_bans:-0}"
done < <(find -L "$SG_DIR" -name '*.sh' -type f | sort)
# A roll-call of zero files would pass every assertion above by having none. The
# floor is a floor, not a count: it must not need editing when a script is added,
# only when the walk itself breaks.
[[ "$_sg24_scanned" -ge 6 ]] \
  && ok "seven_gate: the banned-shape scan walked $_sg24_scanned .sh files under seven_gate/ (scope is the directory, not a list of names)" \
  || bad "seven_gate: the banned-shape scan only found $_sg24_scanned .sh file(s) under $SG_DIR — a walk that finds nothing passes silently, which is how this ban lost its reach the first time"
# …and the three files the old two-name scan could not see are each named, so a
# future narrowing of the walk cannot quietly drop them.
for _sg24_must in run.sh lib/carrier.sh actors/live.sh; do
  find -L "$SG_DIR" -name '*.sh' -type f | grep -Fqx "$SG_DIR/$_sg24_must" \
    && ok "seven_gate: …including $_sg24_must (a mutant here used to be SILENT)" \
    || bad "seven_gate: $_sg24_must is not in the banned-shape scan's reach — that is the file that took the live server down"
done
check "banned-shape scan control: the SAME scan finds the shape in the pgrep mutant" "1" \
  "$(_sg_code_only "$SG24_PIDMUT" | grep -cE '(^|[^[:alnum:]_])(pkill|killall|pgrep)([^[:alnum:]_]|$)' || true)"

# 24g) MUTANT ②b — "LIST THE SESSIONS AND PICK THE ONES THAT LOOK LIKE OURS".
# The one the text scan above CANNOT see: it contains no pkill, no pgrep, no
# glob — just tmux telling the truth about what is running and the harness
# choosing. That is why 24d is behavioural and recorded rather than a grep.
SG24_SESSMUT="$SG24/ownedkill-listpick.sh"
sed 's@tmux -L "$socket" kill-session -t "$name" 2>/dev/null@tmux -L "$socket" list-sessions -F "#{session_name}" 2>/dev/null | grep "^member-" | while IFS= read -r n; do tmux -L "$socket" kill-session -t "$n"; done@' \
    "$SG_OWNED" > "$SG24_SESSMUT"
if ! grep -q 'list-sessions' "$SG24_SESSMUT"; then
  bad "seven_gate: MUT-listpick did not apply to lib/ownedkill.sh — the kill-session line moved, so case 24g is testing nothing (fix the sed)"
else
  printf '%s\n' "$SG24_OWNED_SESSION" > "$SG24_SLEDGER"
  sg24_sessions "$SG24_SESSMUT" "$SG24_SOCK" "$SG24_SLEDGER" >/dev/null
  grep -Fq "$SG24_FOREIGN_SESSION" "$SG24_TMUX_LOG" \
    && ok "MUT-listpick: with the kill relaxed to list-and-pick, the FOREIGN session $SG24_FOREIGN_SESSION is killed — 24d's silence is the ledger, not an empty socket"
  grep -Fq "$SG24_FOREIGN_SESSION" "$SG24_TMUX_LOG" \
    || bad "MUT-listpick: the list-and-pick mutant killed nobody else's session (recorded: $(tr '\n' '|' < "$SG24_TMUX_LOG")) — 24d would pass without the ledger and this case proves nothing"
fi
: > "$SG24_TMUX_LOG"

# ── 25) T-42bb: the carrier must outlive its caller, and never die silently ──
#
# WHAT HAPPENED (2026-08-10, on a run that had already spent money). run.sh was
# started as a background command by an agent session. The session was collected;
# the carrier was killed WITH it, mid-poll. The agent it had spawned did NOT die
# (agents live in tmux). So: nobody judged, nobody tore down, a real agent kept
# burning quota — and the waiter never learned, because the rc it was watching
# was the rc of the shell that died, and that rc was never written. A dead run
# and a running run were the same silence.
#
# TWO PROPERTIES, and the second is the one that must not be traded away:
#   ① the carrier puts ITSELF in a new session, so a group kill aimed at the
#     caller cannot reach it (it must not depend on the caller remembering
#     `nohup` — that is the class of bug this whole gate exists to delete);
#   ② however it dies, a terminal signal file appears. Because if ① ever fails
#     for a reason nobody predicted, the failure must be a VISIBLE death.
#
# Hermetic: no server, no agent, no tmux, no money. The fixture below is the
# skeleton of run.sh's carrier wiring around a `sleep`, sourcing the REAL
# lib/carrier.sh; 25f then pins that run.sh is wired the same way, since a
# perfect lib called from nowhere protects nothing (case 20f's lesson).
SG_CARRIER="$SG_DIR/lib/carrier.sh"
SG25="$SHIMDIR/sg25"; rm -rf "$SG25"; mkdir -p "$SG25"
SG25_FIX="$SG25/carrier-fixture.sh"
cat > "$SG25_FIX" <<'SH'
#!/usr/bin/env bash
set -uo pipefail
. "$SG25_LIB"
RUN_DIR="$1"
export OC_SG_RUN_DIR="$RUN_DIR"
sg_carrier_detach "$0" "$@"
cleanup() {
  local rc=$?
  sg_carrier_write "$rc"
  sg_carrier_watchdog_stop
}
trap cleanup EXIT
sg_carrier_arm "$RUN_DIR/outer.rc"
sg_carrier_watchdog
printf '%s\n' "$$" > "$RUN_DIR/carrier.pid"
sleep "$SG25_SLEEP"
printf 'finished\n' > "$RUN_DIR/finished"
exit "$SG25_RC"
SH

# Start the fixture as the leader of its OWN session, so the test can kill that
# whole process group the way a supervisor collecting a session does — WITHOUT
# taking this test suite (which shares a process group with everything it spawns)
# down as collateral. Prints the leader's pid; its pgid equals that pid.
_sg25_spawn() { # _sg25_spawn RUN_DIR [env assignments…] -> leader pid
  local run_dir="$1"; shift
  python3 -c '
import os, sys
pid = os.fork()
if pid:
    print(pid)
    sys.exit(0)
# The child must NOT keep this command substitution`s pipe open, or the caller
# would block until the fixture exits — which is the opposite of the point.
fd = os.open(os.devnull, os.O_RDWR)
os.dup2(fd, 1)
os.dup2(fd, 2)
os.setsid()
os.execvp("env", ["env"] + sys.argv[1:])
' "$@" bash "$SG25_FIX" "$run_dir"
}
_sg25_wait_file() { # _sg25_wait_file PATH DEADLINE_TENTHS -> 0 if it appeared
  local p="$1" n="${2:-100}" i
  for ((i = 0; i < n; i++)); do [[ -e "$p" ]] && return 0; sleep 0.1; done
  return 1
}
_sg25_rc() { cat "$1/outer.rc" 2>/dev/null | tr -d ' \n'; }

# 25a) THE INCIDENT ITSELF: kill the caller's whole process group, hard (SIGKILL
# — no trap can soften it, which is exactly what a collected session looks like)
# while the run is mid-flight. Both halves are asserted, because either alone is
# a false comfort: the work must FINISH, and the terminal signal must EXIST.
SG25_A="$SG25/a"; mkdir -p "$SG25_A"
_sg25_leader="$(_sg25_spawn "$SG25_A" "SG25_LIB=$SG_CARRIER" SG25_SLEEP=4 SG25_RC=0 OC_SG_WATCHDOG_INTERVAL=1)"
if ! _sg25_wait_file "$SG25_A/carrier.pid" 100; then
  bad "carrier: the fixture never started (no carrier.pid) — case 25a is testing nothing"
else
  kill -KILL "-$_sg25_leader" 2>/dev/null
  _sg25_wait_file "$SG25_A/finished" 120 || true
  [[ -f "$SG25_A/finished" ]] \
    && ok "carrier: the caller's process group was SIGKILLed mid-run and the carrier RAN TO COMPLETION anyway" \
    || bad "carrier: a group SIGKILL aimed at the caller killed the carrier too — the run does not outlive the session that started it"
  _sg25_wait_file "$SG25_A/outer.rc" 60 || true
  check "carrier: …and the terminal signal exists, with the run's own rc" "0" "$(_sg25_rc "$SG25_A")"
fi

# 25b) THE CONTROL, and it is what makes 25a mean anything: the SAME kill against
# a carrier that did not detach (OC_SG_NO_DETACH=1) must reproduce the incident —
# the work stops, and there is no terminal signal. Without this, 25a would also
# pass on a machine where that kill happened not to reach anything.
SG25_B="$SG25/b"; mkdir -p "$SG25_B"
_sg25_leader_b="$(_sg25_spawn "$SG25_B" "SG25_LIB=$SG_CARRIER" SG25_SLEEP=4 SG25_RC=0 OC_SG_NO_DETACH=1 OC_SG_WATCHDOG_INTERVAL=1)"
if ! _sg25_wait_file "$SG25_B/carrier.pid" 100; then
  bad "carrier: the no-detach control never started — case 25b is testing nothing"
else
  kill -KILL "-$_sg25_leader_b" 2>/dev/null
  sleep 6
  [[ -f "$SG25_B/finished" ]] \
    && bad "carrier: CONTROL BROKEN — the un-detached carrier survived a group SIGKILL, so 25a's survival proves nothing about the detach" \
    || ok "carrier: control — WITHOUT the detach, the same kill stops the run dead (this is the incident, reproduced)"
  # The watchdog is a child of the carrier and shares its process group here, so
  # it dies in the same volley: without the detach there is nobody left to
  # write anything. That is the silence this whole case exists to remove.
  [[ -f "$SG25_B/outer.rc" ]] \
    && bad "carrier: CONTROL BROKEN — the un-detached carrier still produced a terminal signal, so 25a's signal proves nothing" \
    || ok "carrier: control — and WITHOUT the detach there is no terminal signal at all: a dead run and a running run look identical (rc file absent)"
fi

# 25c) DEATH BY SIGNAL. TERM aimed at the carrier itself must still leave a
# signal, with the reason recorded — a death that says what killed it.
# ⚠️ TIMING, stated because the assertion below would otherwise be read as
# stronger than it is: bash runs a caught signal's trap only once the FOREGROUND
# COMMAND IT IS WAITING ON returns. So the signal file appears when the carrier
# next regains control (here: when the fixture's `sleep` ends), not at the
# instant of the TERM. The guarantee is that it appears AT ALL and carries the
# right rc and reason — not that it is instantaneous. The instant-death case is
# 25d's SIGKILL, which the watchdog answers without waiting for bash.
SG25_C="$SG25/c"; mkdir -p "$SG25_C"
_sg25_spawn "$SG25_C" "SG25_LIB=$SG_CARRIER" SG25_SLEEP=4 SG25_RC=0 OC_SG_WATCHDOG_INTERVAL=1 >/dev/null
if ! _sg25_wait_file "$SG25_C/carrier.pid" 100; then
  bad "carrier: the TERM fixture never started — case 25c is testing nothing"
else
  kill -TERM "$(cat "$SG25_C/carrier.pid")" 2>/dev/null
  _sg25_wait_file "$SG25_C/outer.rc" 150 || true
  check "carrier: a TERM to the carrier still writes the terminal signal (128+15)" "143" "$(_sg25_rc "$SG25_C")"
  [[ -f "$SG25_C/finished" ]] \
    && bad "carrier: the TERM never actually stopped the run (it reached its end), so the 143 above says nothing about a signalled death" \
    || ok "carrier: …and the run really was cut short by it (its completion marker was never written)"
  grep -q 'signal:TERM' "$SG25_C/outer.status" 2>/dev/null \
    && ok "carrier: …and outer.status records WHY it ended — $(tail -1 "$SG25_C/outer.status")" \
    || bad "carrier: the rc was written but nothing recorded that a signal caused it (outer.status: $(cat "$SG25_C/outer.status" 2>/dev/null))"
fi

# 25d) DEATH NO TRAP CAN SEE. SIGKILL straight at the carrier: every trap above
# is a promise bash can only keep while bash is alive, and this is the death that
# produced the incident's silence. The watchdog is the layer that answers it.
SG25_D="$SG25/d"; mkdir -p "$SG25_D"
_sg25_spawn "$SG25_D" "SG25_LIB=$SG_CARRIER" SG25_SLEEP=20 SG25_RC=0 OC_SG_WATCHDOG_INTERVAL=1 >/dev/null
if ! _sg25_wait_file "$SG25_D/carrier.pid" 100; then
  bad "carrier: the SIGKILL fixture never started — case 25d is testing nothing"
else
  kill -KILL "$(cat "$SG25_D/carrier.pid")" 2>/dev/null
  _sg25_wait_file "$SG25_D/outer.rc" 100 || true
  check "carrier: even an untrappable SIGKILL leaves a terminal signal (the watchdog writes it)" "137" "$(_sg25_rc "$SG25_D")"
  grep -q 'vanished' "$SG25_D/outer.status" 2>/dev/null \
    && ok "carrier: …and it says the carrier VANISHED rather than ended — $(tail -1 "$SG25_D/outer.status")" \
    || bad "carrier: a signal appeared for the SIGKILL case but never said the carrier vanished (outer.status: $(cat "$SG25_D/outer.status" 2>/dev/null))"
fi

# 25e) THE ORDINARY ENDS still carry the run's own rc — including a REFUSAL,
# which is the shape of every `exit 2` guard in run.sh. A terminal signal that
# flattened every ending to 0 would be worse than none.
SG25_E="$SG25/e"; mkdir -p "$SG25_E"
_sg25_spawn "$SG25_E" "SG25_LIB=$SG_CARRIER" SG25_SLEEP=1 SG25_RC=2 OC_SG_WATCHDOG_INTERVAL=1 >/dev/null
_sg25_wait_file "$SG25_E/outer.rc" 100 || true
check "carrier: an ordinary exit writes ITS OWN rc, not a flattened 0 (a refusal stays a refusal)" "2" "$(_sg25_rc "$SG25_E")"

# 25f) THE WIRING IN run.sh, in CODE. The lib can be perfect and called from
# nowhere. Three things are pinned: the detach happens, the traps are armed, and
# the terminal signal is written from the EXIT path (the same trap that runs
# teardown — so a run that refuses at a guard still reports).
_sg25_code() { grep -v '^[[:space:]]*#' "$1"; }
for _sg25_fn in sg_carrier_detach sg_carrier_arm sg_carrier_write; do
  _sg25_hits="$(_sg25_code "$SG_DIR/run.sh" | grep -cF "$_sg25_fn" || true)"
  [[ "${_sg25_hits:-0}" -ge 1 ]] \
    && ok "carrier: run.sh calls $_sg25_fn in code" \
    || bad "carrier: run.sh no longer calls $_sg25_fn — the carrier protection is wired to nothing"
done
# ORDER: the detach must come before the run does anything expensive or
# stateful. Detaching after setup.sh would leave the window in which the
# incident actually happened wide open.
_sg25_detach_line="$(_sg25_code "$SG_DIR/run.sh" | grep -n 'sg_carrier_detach' | head -1 | cut -d: -f1)"
_sg25_setup_line="$(_sg25_code "$SG_DIR/run.sh" | grep -n 'setup\.sh' | head -1 | cut -d: -f1)"
if [[ -n "$_sg25_detach_line" && -n "$_sg25_setup_line" && "$_sg25_detach_line" -lt "$_sg25_setup_line" ]]; then
  ok "carrier: run.sh detaches (code line $_sg25_detach_line) before it starts the isolated server (line $_sg25_setup_line)"
else
  bad "carrier: run.sh's detach does not precede setup.sh (detach=${_sg25_detach_line:-none} setup=${_sg25_setup_line:-none}) — the run would spend part of its life killable by its caller"
fi

# ── 26) T-42bb: the answers to ②⑧⑨ must not reach the agent's own shell ──────
#
# 🔴 WHAT WAS MEASURED, statically end to end plus the last hop live: run.sh 5
# exports OC_SG_SCENE_NONCE / OC_SG_PEER_NONCE / OC_SG_IMAGE_ANSWER to the actor;
# actors/live.sh started the warden with `env -u OC_WARDEN_TOKFILE …` (ONE
# variable unset); cli/ocwarden's execRunner.Run never sets cmd.Env, so the child
# inherits os.Environ(); and the tmux `new-session` it issues inherits that in
# turn — verified on a throwaway socket: a value exported before
# `tmux -L … new-session -d 'env > f'` is in f. So ⑨'s answer sat one `env` away
# from the real agent, and ⑨'s own comment says a leak there makes a blind
# agent's green indistinguishable from a real one. The leak scan could not see
# it: it read server TEXT only.
#
# The fix is in the harness, not in cli/ocwarden (that spawn path is the whole
# fleet's). lib/scrub.sh removes the harness's entire OC_SG_*/SG_* namespace from
# the child environment and proves it before the spawn. This case is hermetic: it
# starts no server, spawns no agent, and works on a COPY of the lib so the mutant
# can never touch the real one.
SG26="$SHIMDIR/sg-scrub"; mkdir -p "$SG26"
SG_SCRUB="$SG_DIR/lib/scrub.sh"
if [[ ! -f "$SG_SCRUB" ]]; then
  bad "seven_gate: lib/scrub.sh is gone — nothing is keeping ②⑧⑨'s answers out of the agent's environment"
else
  cp "$SG_SCRUB" "$SG26/scrub.sh"
  # 26a) the child environment is really clean, and the check can really see.
  _sg26() { # _sg26 <lib> [interpreter] [PATH] -> "<assert-rc>|<secret hits>|<harness names>"
    env -i PATH="${3:-$PATH}" HOME="${HOME:-/tmp}" \
        OC_SG_IMAGE_ANSWER=481902 OC_SG_SCENE_NONCE=sg-nonce-deadbeef \
        SG_TOKEN=owner-token-abc \
        "${2:-bash}" -c '
set -uo pipefail
. "$1" || exit 9
sg_scrub_assert "481902" "sg-nonce-deadbeef" "owner-token-abc" >/dev/null 2>&1; a=$?
h=$(sg_scrub_env printenv | grep -cE "481902|sg-nonce-deadbeef|owner-token-abc" || true)
n=$(sg_scrub_env printenv | grep -cE "^(OC_SG_|SG_)" || true)
printf "%s|%s|%s\n" "$a" "$h" "$n"' _ "$1"
  }
  check "seven_gate: with the scrub in place, the environment the warden (and therefore the agent) would inherit carries none of ②⑧⑨'s answers" \
    "0|0|0" "$(_sg26 "$SG26/scrub.sh")"
  # 26a-bis) THE SAME THING UNDER THE INTERPRETER THIS ACTUALLY RUNS ON.
  #
  # 🔴 THE CELL ABOVE WAS BLIND FOR A REASON WORTH KEEPING WRITTEN DOWN: it says
  # `env -i PATH="$PATH"`, i.e. it inherits the DEVELOPER'S PATH, so it only ever
  # exercised whatever bash is early on it (5.3.9 here). actors/live.sh is
  # `#!/usr/bin/env bash`, so on a Mac without Homebrew bash — or under any
  # trimmed launchd/cron PATH — the interpreter is the stock /bin/bash, 3.2.57.
  # MEASURED at 68e3bfd1: this file's names_left= filter was written as a
  # one-line `case` inside `$( )`, which 3.2 cannot parse; sg_scrub_assert died
  # mid-function and live.sh refused to spawn ONE HOP BEFORE THE SPEND, looking
  # exactly like a machine that never came online — and this suite was green.
  # So the PATH is pinned to /usr/bin:/bin and the interpreter is named.
  if [[ -x "$SG_STOCK_BASH" ]]; then
    check "seven_gate: …and the same holds under the stock $SG_STOCK_BASH with a pinned PATH (the hop that refuses to spawn must work on a stock macOS, not just where Homebrew bash is first on PATH)" \
      "0|0|0" "$(_sg26 "$SG26/scrub.sh" "$SG_STOCK_BASH" /usr/bin:/bin)"
    # MUT-inlinecase — the 68e3bfd1 shape put back, verbatim. Three cells,
    # because the interesting fact is the DISAGREEMENT between interpreters.
    awk '{ if ($0 ~ /\| sg_scrub_filter\)"$/) {
             print "                | while IFS= read -r n; do";
             print "                    for p in $SG_SCRUB_PREFIXES; do";
             print "                      case \"$n\" in \"$p\"*) printf '"'"'%s '"'"' \"$n\"; break ;; esac";
             print "                    done";
             print "                  done)\"" } else print }' \
        "$SG26/scrub.sh" > "$SG26/scrub-inline.sh"
    if ! grep -q 'in "\$p"\*) printf' "$SG26/scrub-inline.sh"; then
      bad "seven_gate: MUT-inlinecase did not apply to lib/scrub.sh — the names_left= pipeline moved, so 26a-bis is testing nothing (fix the awk)"
      bad "seven_gate: (MUT-inlinecase second cell skipped — mutant did not apply)"
      bad "seven_gate: (MUT-inlinecase third cell skipped — mutant did not apply)"
    else
      _sg26_inl32="$(_sg26 "$SG26/scrub-inline.sh" "$SG_STOCK_BASH" /usr/bin:/bin)"
      _sg26_inl32_clean=no; [[ "$_sg26_inl32" == "0|0|0" ]] && _sg26_inl32_clean=yes
      check "MUT-inlinecase: with the filter written back as a one-line case inside \$( ), the stock $SG_STOCK_BASH never reaches a clean spawn (got '${_sg26_inl32:-<the assert died mid-function, there is not even an rc>}')" \
        "no" "$_sg26_inl32_clean"
      # …and the disagreement itself. The UNPINNED form of the cell above (the
      # one that says `PATH="$PATH"` and plain `bash`) reports this same file
      # perfectly clean on bash 4+ — that blindness is the whole reason the
      # pinned cell exists. The expectation is written per major version because
      # tests_guard is itself run by whatever bash is first on PATH, and this
      # suite's assertion COUNT and RESULT must not depend on the host: run this
      # file under 3.2 and the unpinned interpreter IS the stock one, so it
      # agrees with the pinned cell instead of disagreeing with it.
      _sg26_unpinned_exp="0|0|0"
      [[ "${BASH_VERSINFO[0]}" -lt 4 ]] && _sg26_unpinned_exp=""
      check "MUT-inlinecase: …and the UNPINNED interpreter (this shell's bash $BASH_VERSION) reports what its major version predicts on the identical file — on 4+ that is a spotless '0|0|0', which is exactly the blindness the pinned cell was added to remove" \
        "$_sg26_unpinned_exp" "$(_sg26 "$SG26/scrub-inline.sh")"
      "$SG_STOCK_BASH" -n "$SG26/scrub-inline.sh" 2>/dev/null
      check "MUT-inlinecase: …and even the stock binary's own \`-n\` passes it (a command substitution is parsed when it is EXPANDED) — which is why 23e's static parse is not enough on its own" \
        "0" "$?"
    fi
  else
    bad "seven_gate: no executable $SG_STOCK_BASH — the scrub is only ever exercised under whatever bash is first on PATH"
    bad "seven_gate: (MUT-inlinecase skipped — no stock bash)"
    bad "seven_gate: (MUT-inlinecase skipped — no stock bash)"
    bad "seven_gate: (MUT-inlinecase skipped — no stock bash)"
  fi
  # 26b) MUTANT — the derivation severed, the assertion must REFUSE and NAME it.
  # Without this, 26a is satisfied by an environment that never had the secrets.
  sed 's/^sg_scrub_names() {$/sg_scrub_names() { return 0;/' \
      "$SG26/scrub.sh" > "$SG26/scrub-mut.sh"
  if ! grep -q 'sg_scrub_names() { return 0;' "$SG26/scrub-mut.sh"; then
    bad "seven_gate: MUT-noscrub did not apply to lib/scrub.sh — the function signature moved, so case 26b is testing nothing (fix the sed)"
  else
    _sg26_mut="$(_sg26 "$SG26/scrub-mut.sh")"
    check "MUT-noscrub: with the scrub reduced to a no-op, the child environment carries all three answers again" \
      "3" "$(printf '%s' "$_sg26_mut" | cut -d'|' -f2)"
    check "MUT-noscrub: …and sg_scrub_assert REFUSES rather than letting the spawn happen" \
      "1" "$(printf '%s' "$_sg26_mut" | cut -d'|' -f1)"
    _sg26_msg="$(env -i PATH="$PATH" HOME="${HOME:-/tmp}" OC_SG_IMAGE_ANSWER=481902 \
      bash -c '. "$1"; sg_scrub_assert "481902" 2>&1' _ "$SG26/scrub-mut.sh")"
    case "$_sg26_msg" in
      *OC_SG_IMAGE_ANSWER*) ok "MUT-noscrub: …and it NAMES the variable that survived — $(printf '%s' "$_sg26_msg" | tail -1)" ;;
      *) bad "MUT-noscrub: the refusal never named what leaked, so it would not tell anyone what to fix: $_sg26_msg" ;;
    esac
  fi
  # 26c) THE POSITIVE CONTROL OF THE POSITIVE CONTROL. A scrub that is asked
  # about secrets that were never in the environment must refuse too — otherwise
  # "clean" is reachable by asking the wrong question.
  _sg26_vac="$(env -i PATH="$PATH" HOME="${HOME:-/tmp}" \
    bash -c '. "$1"; sg_scrub_assert "not-in-this-environment" >/dev/null 2>&1; echo $?' _ "$SG26/scrub.sh")"
  check "seven_gate: the scrub refuses when its own control finds nothing (a vacuous 'clean' is not a pass)" \
    "1" "$_sg26_vac"
  # 26d) THE WIRING. The lib can be perfect and called from nowhere — which is
  # exactly what happened to the `env -u OC_WARDEN_TOKFILE` line it replaces.
  _sg26_code() { grep -v '^[[:space:]]*#' "$1"; }
  _sg26_live="$SG_DIR/actors/live.sh"
  _sg26_code "$_sg26_live" | grep -qF 'sg_scrub_assert' \
    && ok "seven_gate: live.sh proves the scrub in CODE before it starts the warden" \
    || bad "seven_gate: live.sh no longer calls sg_scrub_assert — the proof is wired to nothing and ⑨'s answer rides into the agent's shell again"
  _sg26_assert_line="$(_sg26_code "$_sg26_live" | grep -n 'sg_scrub_assert' | head -1 | cut -d: -f1)"
  _sg26_warden_line="$(_sg26_code "$_sg26_live" | grep -n 'OCWARDEN" run' | head -1 | cut -d: -f1)"
  if [[ -n "$_sg26_assert_line" && -n "$_sg26_warden_line" && "$_sg26_assert_line" -lt "$_sg26_warden_line" ]]; then
    ok "seven_gate: …and it proves it BEFORE the warden starts (code line $_sg26_assert_line < $_sg26_warden_line)"
  else
    bad "seven_gate: live.sh's scrub proof does not precede the warden start (assert=${_sg26_assert_line:-none} warden=${_sg26_warden_line:-none}) — a refusal after the spawn is not a refusal"
  fi
  _sg26_code "$_sg26_live" | grep -qE 'sg_scrub_env[[:space:]]+env' \
    && ok "seven_gate: …and the warden is actually launched THROUGH the scrub" \
    || bad "seven_gate: live.sh launches the warden without sg_scrub_env — the assertion above would be proving something about an environment the warden never gets"
fi

echo "[tests_guard] PASS=$PASS FAIL=$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1

# ── PASS FLOOR ──────────────────────────────────────────────────────────────
# See SCOPE at the top: there is no discovery here, so a case block that stops
# existing takes its assertions with it and everything still reports green. This
# floor is what makes that loud ONCE ENOUGH OF IT IS GONE — see the measured
# cells below for where that threshold actually sits.
#
# A FLOOR, not the exact count, on purpose — the same reasoning that
# e2e_test/assert-specs-ran.sh writes out for the spec tally: an exact number
# reddens the first time someone legitimately adds a case, and a check that
# reddens on correct work is a check somebody deletes.
#
# 🔴 BUT A FLOOR THAT DRIFTS FAR ENOUGH BELOW THE COUNT IS NOT A FLOOR EITHER,
# and this one had. It sat at 100 while the suite grew to 291: 191 assertions —
# two thirds of the file — could evaporate and this still printed `all green`.
# "It never needs updating when cases are added" is exactly what made it useless,
# because what it measures is not how many assertions exist but how big a hole is
# tolerated, and that hole grew with every case anyone wrote. Measured history of
# the same 100: PASS=153 (2026-08-08, hole 53) → 237 (2026-08-10, hole 137) → 291
# (2026-08-11, hole 191).
#
# SO IT IS NOW SET NEAR THE COUNT, WITH DELIBERATE SLACK, AND IT IS EXPECTED TO
# BE EDITED. 303 today, floor 300: three assertions of room. (291/288 → 298/295
# when 2026-08-11's bash-3.2 round added 23e's three cells and case 26's four →
# 303/300 when ⑤'s downgrade traded two cells away — `sg_mutant step_done` and
# the ⑤-red/⑦-green pair — for seven in 21b-i/21b-v. Each move edited the floor
# in the same commit, which is the edit this block asks for.) The slack is measured, not guessed — deleting the whole of case 26 (then
# 8 assertions) gave PASS=283, which was FATAL and named at 288 and GREEN at
# 280. Read the
# guarantee narrowly: a change that removes FOUR OR MORE assertions is loud; one
# that removes three or fewer is not, and nothing here knows which case blocks
# are that small.
# ⚠️ THIS IS A FLOOR, NOT AN EXPECTATION — being ABOVE it means nothing, going
# BELOW it means the suite collapsed. If a legitimate change genuinely removes
# assertions, MOVE THIS NUMBER IN THE SAME COMMIT and say why; that edit is the
# review this file wants, not an obstacle to route around. Do NOT "fix" a red
# floor by lowering it to whatever today's run printed without knowing what left.
#
# WHAT IT STILL DOES NOT CATCH, said plainly: it is VOLUME-SHAPED, not
# importance-shaped. It counts assertions and does not care WHICH ones went, so
# a three-assertion deletion is invisible no matter how load-bearing those three
# were — case 11 (the rc-propagation shape of run_all.sh) and 20e (teardown's
# only way out) are among the highest-value and the smallest blocks in this file,
# and whether either fits inside three assertions has NOT been measured. Nothing
# here watches at case-name or block granularity.
# Mutants (the first three measured when the suite stood at 153 assertions and
# the floor at 100; each restored from a scratchpad copy with the sha256
# re-checked):
#   * floor raised to an unreachable 9999            → PASS=153 FAIL=0, rc=1, named.
#   * the whole 19x/20x half of the file deleted     → PASS=66  FAIL=0, rc=1, named.
#   * ONE case block (19a, five assertions) deleted  → PASS=148 FAIL=0, rc=0 — GREEN.
#   * (2026-08-11, floor 288) case 26 deleted, 8     → PASS=283 FAIL=0, rc=1, named.
# The third one is what a floor of 100 permitted; at 288 a five-assertion block
# would no longer fit, and the fourth line is that same shape actually measured
# against the new floor.
#
# THE SUCCESS MARKER IS PRINTED FROM INSIDE THIS BLOCK, from the floor's passing
# branch and nowhere else — that is the only reason bin/ci.sh's `tail -n 1`
# check says anything about the floor. It used to sit on its own line after the
# `fi`, and then deleting this whole block while leaving that last line behind
# printed the marker with no floor evaluated at all: MEASURED, floor block
# deleted and the trailing echo kept → PASS=153 FAIL=0 rc=0, last line
# `[tests_guard] all green`, `bin/ci.sh` all green. Keep it in the branch.
#
# 2026-08-27 (T-1a7d): 323 → 324. Case 24e gained ONE assertion — the one that
# pins the decoy sleeper's lifetime above the longest watch, without which a
# decoy that simply finished would read as "killed" and 24f would pass on a
# mutant that touched nothing. Floor moved 320 → 321 in the same commit, keeping
# the same three assertions of slack, as this block asks.
PASS_FLOOR=321
if [[ "$PASS" -lt "$PASS_FLOOR" ]]; then
  echo "[tests_guard] FATAL: only $PASS assertion(s) ran, floor is $PASS_FLOOR." >&2
  echo "[tests_guard] FAIL=0 with a collapsed PASS count means cases went missing, not that they passed." >&2
  exit 1
else
  echo "[tests_guard] all green"
fi
