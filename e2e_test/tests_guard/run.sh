#!/usr/bin/env bash
# e2e_test/tests_guard/run.sh — HERMETIC unit tests for the T-8aa1 isolation
# layer in e2e_test/lib/oc_lifecycle.sh: the live-fleet guard + the namespace
# allocator (oc_resolve_instance) + the derivation helpers.
#
# WHY bats-free: e2e_test/ has no shell-test harness. This is a tiny, dependency-
# free runner (assert helpers + a PATH shim that stubs EVERY external command the
# guard/allocator touches) so it can run inside bin/ci.sh on ANY host — including
# a LIVE fleet host — WITHOUT touching the real launchctl/tmux/lsof/fleet. The
# stubs return controlled output; NOTHING real is mutated and NO teardown path is
# ever exercised (we only ever call the read-only detector / the guard / the pure
# allocator).
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
if [[ "$1" == "bootout" ]]; then echo "TRIPWIRE launchctl bootout $*" >> "$SHIM_TRIPWIRE"; exit 0; fi
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

chmod +x "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/tmux
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
    source "$LIB" >/dev/null 2>&1
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
  D41A_SET="$(grep -m1 -E '^set +-' "$RUN_ALL" || true)"
  D41A_SRC="$(grep -m1 -F 'source "$HERE/lib/common.sh"' "$RUN_ALL" || true)"
  D41A_RC="$(grep -m1 -E '^RC=\$\?' "$RUN_ALL" || true)"
  D41A_ECHO="$(grep -m1 -F '[run_all] specs exit=' "$RUN_ALL" || true)"
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

echo "[tests_guard] PASS=$PASS FAIL=$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
echo "[tests_guard] all green"
