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

# ── 5) detection fires on a canonical 8770 listener ──────────────────────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="8770" SHIM_SESSIONS="" \
      run_snippet 'oc_detect_live_canonical_fleet | grep -q "serve port 8770"')"
check "canonical 8770 listener is detected" "0" "$rc"

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
[[ "$PORT" != "8770" && "$PORT" != "8766" && "$PORT" != "8790" && "$PORT" != "8791" && "$PORT" != "8795" ]] \
  && ok "port $PORT is non-canonical/non-reserved" || bad "port $PORT collides with a reserved port"
[[ "$SERVE" == "com.officraft.serve.$NS" ]] && ok "serve label namespaced ($SERVE)" || bad "serve label wrong: $SERVE"
[[ "$WARDEN" == "com.officraft.ocwarden.$NS" && "$WARDEN" != "com.officraft.ocwarden" ]] \
  && ok "warden label namespaced ($WARDEN)" || bad "warden label wrong: $WARDEN"
[[ "$ROOT" == *"/.officraft-$NS" && "$ROOT" != *"/.officraft" ]] \
  && ok "root namespaced ($ROOT)" || bad "root wrong: $ROOT"
[[ "$SOCK" == "officraft-$NS" && "$SOCK" != "officraft" ]] \
  && ok "tmux socket namespaced ($SOCK)" || bad "socket wrong: $SOCK"

# ── 8) CANONICAL escape hatch: axes resolve to the canonical literals ─────────
run_snippet 'export OC_E2E_ALLOW_CANONICAL=1; oc_resolve_instance
  printf "NS=[%s]\n" "$OC_NS"
  printf "PORTS=%s\n" "${SINGLE_PROD_PORTS[*]}"' >/dev/null
[[ "$(grep '^NS=' "$GLOG")" == "NS=[]" ]] && ok "canonical escape hatch → OC_NS empty" || bad "canonical OC_NS not empty: $(grep '^NS=' "$GLOG")"
[[ "$(grep '^PORTS=' "$GLOG")" == "PORTS=8770 8766" ]] && ok "canonical guard ports = 8770 8766" || bad "canonical ports wrong: $(grep '^PORTS=' "$GLOG")"

# ── 9) agent_workdir is namespace-aware (a1_zombie kill-anchor safety) ────────
rc="$(run_snippet 'OC_NS="e2ex"; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft-e2ex/agents/mira" ]]')"
check "agent_workdir namespaced under ns" "0" "$rc"
rc="$(run_snippet 'unset OC_NS; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft/agents/mira" ]]')"
check "agent_workdir canonical when ns unset (zero-diff)" "0" "$rc"

# ── 10) TRIPWIRE: no guard/allocator ever called launchctl bootout ───────────
if [[ -s "$TRIPWIRE" ]]; then bad "launchctl bootout was invoked: $(cat "$TRIPWIRE")"; else ok "no teardown/bootout invoked by any guard/allocator path"; fi

echo "[tests_guard] PASS=$PASS FAIL=$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1
echo "[tests_guard] all green"
