#!/usr/bin/env bash
# bin/tests/run.sh — HERMETIC unit tests for bin/codesign-artifact (T-33d5).
#
# Same bats-free pattern as e2e_test/tests_guard/run.sh: a tiny dependency-free
# runner plus a PATH shim that stubs EVERY external command the script under
# test touches (uname / security / codesign), so it runs inside bin/ci.sh on
# ANY host — no keychain, no real codesign, nothing mutated outside mktemp.
# The stubs are driven by SHIM_* env vars; a tripwire file records any codesign
# invocation that must never happen (e.g. when the identity is absent).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../codesign-artifact"
[[ -f "$SCRIPT" ]] || { echo "FATAL: script not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}

# ── PATH shim ────────────────────────────────────────────────────────────────
WORK="$(mktemp -d -t oc-codesign-tests.XXXXXX)"
SHIMDIR="$WORK/shim"
TRIPWIRE="$WORK/.tripwire"
# Second tripwire (T-588c): records every `security find-identity` call. The
# codesign tripwire alone cannot express the property the default-off switch is
# FOR — "no shared login keychain is touched" — because a gate below the identity
# probe leaves codesign uninvoked while still contending for the keychain.
SECWIRE="$WORK/.secwire"
mkdir -p "$SHIMDIR"
: > "$TRIPWIRE"
: > "$SECWIRE"

# ── process hygiene (T-1a54) ─────────────────────────────────────────────────
# Every guard is dispatched through run_bounded.py, which runs it in its own
# session/process group under a wall-clock ceiling and group-kills the whole
# subtree on timeout. A mutant a guard runs that busy-loops therefore dies at
# GUARD_TIMEOUT instead of burning a core forever, and the framework reaps its
# own children when it exits or is interrupted (EXIT/INT/TERM) — no orphans.
LIB_RUN_BOUNDED="$HERE/lib/run_bounded.py"
GUARD_TIMEOUT="${OC_GUARD_TIMEOUT:-300}"   # per-guard ceiling; normal runs finish in seconds
RB_PID=""
_framework_cleanup() {
  # Forward to the in-flight bounded guard, if any. run_bounded detached it into
  # its own session and group-kills the whole subtree on TERM, so this collects
  # everything the guard spawned rather than only the direct child.
  if [[ -n "$RB_PID" ]] && kill -0 "$RB_PID" 2>/dev/null; then
    kill -TERM "$RB_PID" 2>/dev/null
    for _ in 1 2 3 4 5 6 7 8 9 10; do kill -0 "$RB_PID" 2>/dev/null || break; sleep 0.2; done
    kill -KILL "$RB_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap '_framework_cleanup' EXIT
trap '_framework_cleanup; exit 130' INT
trap '_framework_cleanup; exit 143' TERM

run_guard() { # run_guard PATH-to-guard — returns the guard's rc (124 if it timed out)
  python3 "$LIB_RUN_BOUNDED" "$GUARD_TIMEOUT" bash "$1" &
  RB_PID=$!
  wait "$RB_PID"; local rc=$?
  RB_PID=""
  return "$rc"
}

cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
echo "${SHIM_UNAME:-Darwin}"
SH

cat > "$SHIMDIR/security" <<'SH'
#!/usr/bin/env bash
# only find-identity is consulted by the script under test
# SHIM_MALFORMED=1  → answer WITHOUT the "N valid identities found" trailer, i.e.
#                     the check could not be trusted (must never read as "absent")
# SHIM_SLOW_WRITE=1 → write the match line, then PAUSE before the trailer. This
#                     forces open the SIGPIPE window that T-da4b is about: a
#                     consumer that closes the pipe at the first match kills this
#                     process with SIGPIPE (141), which pipefail then promotes to
#                     the pipeline's rc. A collect-then-compare reader is immune.
[[ -n "${SHIM_SECWIRE:-}" ]] && echo "security $*" >> "$SHIM_SECWIRE"
if [[ "${SHIM_MALFORMED:-0}" == "1" ]]; then
  echo 'SecKeychainCopySearchList: authorization denied'
  exit 1
fi
if [[ "${SHIM_HAS_IDENTITY:-0}" == "1" ]]; then
  echo '  1) ABCDEF0123456789 "OffiCraft Code Signing"'
  [[ "${SHIM_SLOW_WRITE:-0}" == "1" ]] && sleep 0.2
  echo '     1 valid identities found'
else
  echo '     0 valid identities found'
fi
SH

cat > "$SHIMDIR/codesign" <<'SH'
#!/usr/bin/env bash
echo "codesign $*" >> "$SHIM_TRIPWIRE"
case "$1" in
  --force)
    [[ "${SHIM_SIGN_FAIL:-0}" == "1" ]] && { echo "errSecInternalComponent" >&2; exit 1; }
    # last argv is the binary path — append a marker so "signed" bytes differ
    eval "bin=\${$#}"
    printf 'SIGNED' >> "$bin"
    exit 0 ;;
  --verify)
    [[ "${SHIM_VERIFY_FAIL:-0}" == "1" ]] && { echo "invalid signature" >&2; exit 1; }
    exit 0 ;;
esac
exit 0
SH
chmod +x "$SHIMDIR"/uname "$SHIMDIR"/security "$SHIMDIR"/codesign

# run_case NAME — runs the script with the shim PATH; stdout+stderr and rc are
# captured into OUT/RC; the target binary is a fresh two-byte file each time.
#
# T-588c: signing is OFF unless requested, so every case below that is ABOUT the
# signing behaviour has to request it — run_case therefore sets
# OC_CODESIGN_ENABLE=1. The DEFAULT (nothing requested) shape is its own helper,
# run_case_default, used by the D-series cases; keeping them separate is what
# stops "the default no-ops" and "signing works when asked" from being one
# undifferentiated blob where a mutant to either reads as the other.
BIN="$WORK/target-binary"
run_case() {
  printf 'AB' > "$BIN"
  : > "$TRIPWIRE"
  : > "$SECWIRE"
  OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" SHIM_SECWIRE="$SECWIRE" \
         OC_CODESIGN_ENABLE=1 bash "$SCRIPT" "$BIN" com.officraft.test 2>&1)"
  RC=$?
}
# The DEFAULT shape: no OC_CODESIGN_ENABLE, no OC_CODESIGN_REQUIRE. This is what
# bin/build / bin/build-bindist / bin/ci.sh / bin/release publish all look like.
run_case_default() {
  printf 'AB' > "$BIN"
  : > "$TRIPWIRE"
  : > "$SECWIRE"
  OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" SHIM_SECWIRE="$SECWIRE" \
         bash "$SCRIPT" "$BIN" com.officraft.test 2>&1)"
  RC=$?
}

echo "codesign-artifact hermetic tests"

# ── D-series (T-588c): SIGNING IS OFF BY DEFAULT ────────────────────────────
# The property under test is NOT "the artifact is unsigned" — it is "the SHARED
# LOGIN KEYCHAIN is never consulted". That is the whole reason two full bin/ci.sh
# runs can now proceed at once, so it is asserted against its own tripwire on the
# `security` shim, not inferred from the artifact's bytes. A gate placed BELOW the
# `security find-identity` call would leave the binary unsigned and still
# serialise concurrent CI; D1 is the only assertion that can tell those apart.
SHIM_HAS_IDENTITY=1 run_case_default
check "DEFAULT (nothing requested) exits 0" "0" "$RC"
check "DEFAULT leaves the binary untouched even with the identity PRESENT" "AB" "$(cat "$BIN")"
check "DEFAULT never invokes codesign" "" "$(cat "$TRIPWIRE")"
check "D1: DEFAULT never even consults the keychain (no security find-identity)" "" "$(cat "$SECWIRE")"
case "$OUT" in *"signing NOT REQUESTED"*) ok "DEFAULT says so out loud (signing NOT REQUESTED)";; *) bad "DEFAULT says so out loud (signing NOT REQUESTED) ($OUT)";; esac

# D2. …and a BROKEN keychain is irrelevant on the default path: the FAIL-CHECK-BROKEN
# hard-stop (exit 3) must not be reachable when nobody asked to sign, or every
# dev/CI machine with a wedged `security` would be blocked by a feature it does
# not use.
SHIM_MALFORMED=1 run_case_default
check "D2: DEFAULT with a BROKEN keychain check still exits 0 (never reached)" "0" "$RC"
check "D2: DEFAULT with a BROKEN keychain check never consults it" "" "$(cat "$SECWIRE")"
unset SHIM_MALFORMED

# D3. the opt-in is what re-arms everything: same fixture, ENABLE=1 → the keychain
# IS consulted and the artifact IS signed. Without this, D1 could pass because the
# script is simply broken.
SHIM_HAS_IDENTITY=1 run_case
check "D3: OC_CODESIGN_ENABLE=1 consults the keychain" "yes" "$([[ -s "$SECWIRE" ]] && echo yes || echo no)"
check "D3: OC_CODESIGN_ENABLE=1 signs the artifact" "ABSIGNED" "$(cat "$BIN")"

# D4. OC_CODESIGN_REQUIRE=1 IMPLIES the opt-in — bin/build-release sets only that
# one knob (owner ruling rc-e43a3aae0912), so if REQUIRE did not imply ENABLE the
# release-signing entry point would silently become a no-op: it would exit 0 with
# an unsigned artifact and print "signing NOT REQUESTED", the exact silent
# downgrade that ruling forbids.
printf 'AB' > "$BIN"; : > "$TRIPWIRE"; : > "$SECWIRE"
OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" SHIM_SECWIRE="$SECWIRE" \
       SHIM_HAS_IDENTITY=1 OC_CODESIGN_REQUIRE=1 bash "$SCRIPT" "$BIN" com.officraft.test 2>&1)"
RC=$?
check "D4: REQUIRE=1 alone (no ENABLE) still signs — REQUIRE implies ENABLE" "ABSIGNED" "$(cat "$BIN")"
check "D4: REQUIRE=1 alone exits 0 on a provisioned host" "0" "$RC"
case "$OUT" in *"signing NOT REQUESTED"*) bad "D4: REQUIRE=1 must NOT be swallowed by the default-off gate";; *) ok "D4: REQUIRE=1 is not swallowed by the default-off gate";; esac

# D5. OC_CODESIGN_DISABLE=1 still wins over an explicit opt-in (the hard override
# outranks the request; order of the two gates is a real decision, so pin it).
printf 'AB' > "$BIN"; : > "$TRIPWIRE"; : > "$SECWIRE"
OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" SHIM_SECWIRE="$SECWIRE" \
       SHIM_HAS_IDENTITY=1 OC_CODESIGN_ENABLE=1 OC_CODESIGN_DISABLE=1 bash "$SCRIPT" "$BIN" com.officraft.test 2>&1)"
check "D5: DISABLE=1 beats ENABLE=1" "AB" "$(cat "$BIN")"
case "$OUT" in *"OC_CODESIGN_DISABLE=1"*) ok "D5: DISABLE=1 says which knob stopped it";; *) bad "D5: DISABLE=1 says which knob stopped it ($OUT)";; esac

# D6. STATIC drift-guard, in the same spirit as R5/R6 below: none of the shared
# default-path scripts may re-arm signing behind the owner's back. The obvious
# undo of this whole change is one `export OC_CODESIGN_ENABLE=1` in bin/build (or
# in ci.sh), which would put every concurrent CI run back on the shared keychain
# while every behavioural assertion above stayed green. Matched on a SETTING of
# the var (anchored to an assignment), so explanatory comments stay legal.
for shared in build build-bindist ci.sh; do
  if grep -qE '^[^#]*OC_CODESIGN_(ENABLE|REQUIRE)=' "$HERE/../$shared"; then
    bad "bin/$shared must NOT set OC_CODESIGN_ENABLE/REQUIRE (it runs on dev Macs + in CI; signing is opt-in per T-588c)"
  else
    ok "bin/$shared leaves signing unrequested (default off — no shared keychain in CI)"
  fi
done

# 1. non-darwin host → no-op, exit 0, codesign never invoked
SHIM_UNAME=Linux SHIM_HAS_IDENTITY=1 run_case
check "non-darwin exits 0" "0" "$RC"
check "non-darwin leaves the binary untouched" "AB" "$(cat "$BIN")"
check "non-darwin never invokes codesign" "" "$(cat "$TRIPWIRE")"
unset SHIM_UNAME

# 2. identity absent → warn-and-return, binary untouched, codesign never invoked
SHIM_HAS_IDENTITY=0 run_case
check "missing identity exits 0 (never blocks a build)" "0" "$RC"
check "missing identity leaves the binary untouched" "AB" "$(cat "$BIN")"
check "missing identity never invokes codesign" "" "$(cat "$TRIPWIRE")"
case "$OUT" in *WARNING*setup-codesign-cert*) ok "missing identity warns with the provisioning pointer";; *) bad "missing identity warns with the provisioning pointer ($OUT)";; esac

# 3. identity present → signs (bytes replaced by the signed copy) + verifies
SHIM_HAS_IDENTITY=1 run_case
check "signing exits 0" "0" "$RC"
check "binary is replaced by the signed copy" "ABSIGNED" "$(cat "$BIN")"
case "$(cat "$TRIPWIRE")" in
  *"--force --sign OffiCraft Code Signing --identifier com.officraft.test"*) ok "codesign invoked with the stable identity + identifier";;
  *) bad "codesign invoked with the stable identity + identifier ($(cat "$TRIPWIRE"))";;
esac
case "$(cat "$TRIPWIRE")" in *"--verify --strict"*) ok "signature is verified after signing";; *) bad "signature is verified after signing";; esac
if ls "$WORK"/.target-binary.codesign.* >/dev/null 2>&1; then bad "no temp copy left behind"; else ok "no temp copy left behind"; fi

# 4. signing fails (e.g. locked keychain) → keep the original bytes, exit 0
SHIM_HAS_IDENTITY=1 SHIM_SIGN_FAIL=1 run_case
check "sign failure exits 0 (never blocks a deploy)" "0" "$RC"
check "sign failure keeps the original binary" "AB" "$(cat "$BIN")"
case "$OUT" in *WARNING*) ok "sign failure warns loudly";; *) bad "sign failure warns loudly ($OUT)";; esac
if ls "$WORK"/.target-binary.codesign.* >/dev/null 2>&1; then bad "sign failure leaves no temp copy"; else ok "sign failure leaves no temp copy"; fi

# 5. verify fails after signing → keep the original bytes, exit 0
SHIM_HAS_IDENTITY=1 SHIM_VERIFY_FAIL=1 run_case
check "verify failure exits 0" "0" "$RC"
check "verify failure keeps the original binary" "AB" "$(cat "$BIN")"

# 6. explicit disable knob
SHIM_HAS_IDENTITY=1 OC_CODESIGN_DISABLE=1 run_case
check "OC_CODESIGN_DISABLE=1 skips signing" "AB" "$(cat "$BIN")"
check "OC_CODESIGN_DISABLE=1 never invokes codesign" "" "$(cat "$TRIPWIRE")"
unset OC_CODESIGN_DISABLE

# 7. missing target file is the one HARD error (a build bug, not a keychain state)
: > "$TRIPWIRE"
OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" SHIM_HAS_IDENTITY=1 bash "$SCRIPT" "$WORK/does-not-exist" com.officraft.test 2>&1)"
check "missing target file exits 1" "1" "$?"

# ── T-da4b: the SIGPIPE misjudge, the sentinel, and its POSITIVE signal ───────

# 8. RED/GREEN for the actual bug: with the identity PRESENT but `security`
#    still writing when the reader could close the pipe, the old
#    `security ... | grep -Fq` form took SIGPIPE(141) → pipefail → "absent" →
#    SILENT adhoc ship. Collect-then-compare must sign anyway, every time.
SHIM_HAS_IDENTITY=1 SHIM_SLOW_WRITE=1 run_case
check "slow-writing security still signs (no SIGPIPE misjudge)" "ABSIGNED" "$(cat "$BIN")"
check "slow-writing security exits 0" "0" "$RC"
case "$OUT" in *"identity CONFIRMED present"*) ok "slow-writing security still reports the identity as present";; *) bad "slow-writing security still reports the identity as present ($OUT)";; esac

# 8b. red control: the ORIGINAL construct must still misjudge under that window,
#     proving the shim really does open a SIGPIPE window (else 8 proves nothing).
ORIG_RC=0
( set -o pipefail
  PATH="$SHIMDIR:$PATH" SHIM_HAS_IDENTITY=1 SHIM_SLOW_WRITE=1 \
    security find-identity -v -p codesigning 2>/dev/null | /usr/bin/grep -Fq '"OffiCraft Code Signing"' ) || ORIG_RC=$?
if [[ "$ORIG_RC" == "141" ]]; then
  ok "red control: the original 'security | grep -Fq' form takes SIGPIPE (rc=141) in this window"
else
  bad "red control: expected the original form to take SIGPIPE rc=141, got rc=$ORIG_RC"
fi

# 9. POSITIVE marker on the good path (lesson #44): a sentinel that only speaks
#    when it fails cannot be distinguished from one that is itself broken.
SHIM_HAS_IDENTITY=1 run_case
case "$OUT" in *"identity CONFIRMED present in keychain"*) ok "identity present prints the POSITIVE confirmation marker";; *) bad "identity present prints the POSITIVE confirmation marker ($OUT)";; esac

# 10. sentinel: identity absent + OC_CODESIGN_REQUIRE=1 → HARD block, never adhoc
SHIM_HAS_IDENTITY=0 OC_CODESIGN_REQUIRE=1 run_case
check "REQUIRE=1 + missing identity exits 4 (blocks the build)" "4" "$RC"
check "REQUIRE=1 + missing identity never invokes codesign" "" "$(cat "$TRIPWIRE")"
check "REQUIRE=1 + missing identity leaves the binary untouched" "AB" "$(cat "$BIN")"
case "$OUT" in *FAIL-IDENTITY-MISSING*) ok "REQUIRE=1 + missing identity prints the FAIL-IDENTITY-MISSING marker";; *) bad "REQUIRE=1 + missing identity prints the FAIL-IDENTITY-MISSING marker ($OUT)";; esac
unset OC_CODESIGN_REQUIRE

# 11. REQUIRE=1 with the identity PRESENT still signs (the sentinel is not a wall)
SHIM_HAS_IDENTITY=1 OC_CODESIGN_REQUIRE=1 run_case
check "REQUIRE=1 + identity present still signs" "ABSIGNED" "$(cat "$BIN")"
check "REQUIRE=1 + identity present exits 0" "0" "$RC"
unset OC_CODESIGN_REQUIRE

# 12. a check that MALFUNCTIONED is never read as "identity absent" — it hard-fails
#     even with REQUIRE off, because a broken check must be loud, not a downgrade.
SHIM_MALFORMED=1 run_case
check "unreadable identity list exits 3 (never silently adhoc)" "3" "$RC"
check "unreadable identity list never invokes codesign" "" "$(cat "$TRIPWIRE")"
case "$OUT" in *FAIL-CHECK-BROKEN*) ok "unreadable identity list prints the FAIL-CHECK-BROKEN marker";; *) bad "unreadable identity list prints the FAIL-CHECK-BROKEN marker ($OUT)";; esac
unset SHIM_MALFORMED

# 13. T-da4b REVIEW — THE CELL NOBODY COVERED: REQUIRE=1 x sign/verify FAILURE.
#     Cases 4/5 pinned sign-failure with REQUIRE OFF; cases 10/11 pinned REQUIRE=1
#     with the identity absent/present. Nothing crossed them, and the crossing is
#     where the ticket's own defect survived: the identity IS present (so case 10's
#     exit 4 never fires and the log even prints "CONFIRMED present"), signing then
#     fails on a locked keychain, and the ORIGINAL ADHOC BYTES SHIP with rc=0.
#     bin/build-release's header explicitly promises "login keychain locked" stops
#     a release; before this it did not. A downgrade needs a sentinel too.
SHIM_HAS_IDENTITY=1 SHIM_SIGN_FAIL=1 OC_CODESIGN_REQUIRE=1 run_case
check "REQUIRE=1 + sign failure exits 5 (never ships adhoc)" "5" "$RC"
check "REQUIRE=1 + sign failure leaves the adhoc binary unpublished" "AB" "$(cat "$BIN")"
case "$OUT" in *FAIL-SIGN-FAILED*) ok "REQUIRE=1 + sign failure prints the FAIL-SIGN-FAILED marker";; *) bad "REQUIRE=1 + sign failure prints the FAIL-SIGN-FAILED marker ($OUT)";; esac
unset OC_CODESIGN_REQUIRE

SHIM_HAS_IDENTITY=1 SHIM_VERIFY_FAIL=1 OC_CODESIGN_REQUIRE=1 run_case
check "REQUIRE=1 + verify failure exits 5 (never ships adhoc)" "5" "$RC"
check "REQUIRE=1 + verify failure leaves the adhoc binary unpublished" "AB" "$(cat "$BIN")"
unset OC_CODESIGN_REQUIRE

# 13b. POSITIVE CONTROL for 13 — the SAME fixtures with REQUIRE off must still
#      exit 0. Without this, "exits 5" could pass simply because the shim broke
#      the script outright, and cases 4/5 would not tell us apart: this pins that
#      the new exit is caused by REQUIRE and nothing else. DoD 2 (dev Macs are
#      never blocked) is the thing being protected here.
SHIM_HAS_IDENTITY=1 SHIM_SIGN_FAIL=1 run_case
check "positive control: sign failure WITHOUT REQUIRE still exits 0 (dev unblocked)" "0" "$RC"
case "$OUT" in *FAIL-SIGN-FAILED*) bad "sign failure without REQUIRE must NOT print FAIL-SIGN-FAILED";; *) ok "sign failure without REQUIRE stays a warning, not a block";; esac

echo "codesign-artifact tests: $PASS ok, $FAIL failed"

# ── T-da4b / owner ruling rc-e43a3aae0912: the RELEASE path hard-blocks ───────
# "發版時硬擋(憑證不在就不出貨)" — and dev Macs explicitly stay unblocked.
#
# Cases 10/11 above prove codesign-artifact OBEYS OC_CODESIGN_REQUIRE when the
# caller sets it. They prove NOTHING about whether anyone ever sets it — the env
# var is supplied by the test itself. These cases guard the thing that actually
# ships adhoc or not: WHICH ENTRY POINT turns the requirement on.
#
# The input space has TWO shapes and both are covered on purpose (a mutant only
# mutates code, never inputs — a guard that sees only one shape is blind on the
# other):
#   RELEASE shape → bin/build-release  → REQUIRE on  → missing identity = exit 4
#   DEV shape     → bin/build (direct) → REQUIRE off → missing identity = warn, 0
RELEASE="$HERE/../build-release"
echo "release-path enforcement tests (T-da4b owner ruling)"
if [[ ! -f "$RELEASE" ]]; then
  bad "bin/build-release exists at $RELEASE"
else
  # A stand-in for bin/build: no npm, no go — just the one thing that decides
  # whether a release ships adhoc, the real codesign-artifact call on a real
  # file. It inherits PATH (the shim) and the env from bin/build-release, so
  # this exercises the REAL propagation chain, not a re-implementation of it.
  FAKEBUILD="$WORK/fake-build"
  cat > "$FAKEBUILD" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "fake-build: OC_CODESIGN_REQUIRE=${OC_CODESIGN_REQUIRE:-<unset>}"
bash "$OC_TEST_CODESIGN" "$OC_TEST_BIN" com.officraft.ocserverd
SH
  chmod +x "$FAKEBUILD"

  # run_release_case — bin/build-release → fake bin/build → real codesign-artifact
  run_release_case() {
    printf 'AB' > "$BIN"
    : > "$TRIPWIRE"
    OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" \
           OC_BUILD_CMD="$FAKEBUILD" OC_TEST_CODESIGN="$SCRIPT" OC_TEST_BIN="$BIN" \
           bash "$RELEASE" 2>&1)"
    RC=$?
  }
  # run_dev_case — the SAME fake build invoked directly (the dev/autodeploy
  # shape: nobody sets OC_CODESIGN_REQUIRE). Same fixture, different shape.
  run_dev_case() {
    printf 'AB' > "$BIN"
    : > "$TRIPWIRE"
    OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" \
           OC_TEST_CODESIGN="$SCRIPT" OC_TEST_BIN="$BIN" \
           bash "$FAKEBUILD" 2>&1)"
    RC=$?
  }

  # R1. the release entry point actually turns the requirement ON, and it
  #     survives into the delegated child process (export, not a local).
  SHIM_HAS_IDENTITY=1 run_release_case
  case "$OUT" in
    *"fake-build: OC_CODESIGN_REQUIRE=1"*) ok "build-release exports OC_CODESIGN_REQUIRE=1 into the build";;
    *) bad "build-release exports OC_CODESIGN_REQUIRE=1 into the build ($OUT)";;
  esac

  # R2. THE RULING, end to end: release build + identity genuinely absent →
  #     hard block. Non-zero exit, the identifiable marker, nothing signed,
  #     no artifact to ship.
  SHIM_HAS_IDENTITY=0 run_release_case
  check "RELEASE + missing identity exits 4 (does not ship)" "4" "$RC"
  case "$OUT" in
    *FAIL-IDENTITY-MISSING*) ok "RELEASE + missing identity prints the FAIL-IDENTITY-MISSING marker";;
    *) bad "RELEASE + missing identity prints the FAIL-IDENTITY-MISSING marker ($OUT)";;
  esac
  check "RELEASE + missing identity never invokes codesign" "" "$(cat "$TRIPWIRE")"
  check "RELEASE + missing identity leaves the artifact unsigned/untouched" "AB" "$(cat "$BIN")"

  # R3. the gate is not a wall: a provisioned release machine still builds+signs.
  SHIM_HAS_IDENTITY=1 run_release_case
  check "RELEASE + identity present exits 0" "0" "$RC"
  check "RELEASE + identity present signs the artifact" "ABSIGNED" "$(cat "$BIN")"
  case "$OUT" in
    *"identity CONFIRMED present in keychain"*) ok "RELEASE + identity present prints the POSITIVE marker";;
    *) bad "RELEASE + identity present prints the POSITIVE marker ($OUT)";;
  esac

  # R4. THE OPTION THE OWNER DID NOT CHOOSE — the dev/autodeploy shape must stay
  #     unblocked. Identical fixture, minus the release entry point: a dev Mac
  #     with no keychain still builds and still ships.
  #     ⚠️ The REASON it is unblocked changed in T-588c and the assertion changed
  #     with it. This case used to expect `WARNING … ADHOC-signed`, i.e. "we tried
  #     to sign, found no identity, and shipped anyway". Signing is now off by
  #     default, so the dev shape never gets as far as looking: it stops at
  #     "signing NOT REQUESTED", above the keychain probe. Asserting the old
  #     warning here would demand the very keychain access the default-off switch
  #     exists to avoid — the assertion and the feature would be at odds.
  SHIM_HAS_IDENTITY=0 run_dev_case
  check "DEV + missing identity exits 0 (dev Macs are NOT blocked)" "0" "$RC"
  case "$OUT" in
    *"fake-build: OC_CODESIGN_REQUIRE=<unset>"*) ok "DEV shape leaves OC_CODESIGN_REQUIRE unset (default off)";;
    *) bad "DEV shape leaves OC_CODESIGN_REQUIRE unset (default off) ($OUT)";;
  esac
  case "$OUT" in
    *"signing NOT REQUESTED"*) ok "DEV + missing identity ships as built without asking to sign";;
    *) bad "DEV + missing identity ships as built without asking to sign ($OUT)";;
  esac

  # R5/R6. STATIC drift-guards on the two shared seams. R2/R4 prove today's
  #     behaviour; these stop the fix from being undone the obvious way — by
  #     "helpfully" hoisting the requirement into the shared build scripts,
  #     which would hard-block every dev Mac and every CI run (bin/build-bindist
  #     runs in bin/ci.sh). Matched on a SETTING of the var, not a mention, so
  #     the explanatory comments in those files stay legal.
  for shared in build build-bindist; do
    if grep -qE '^[^#]*OC_CODESIGN_REQUIRE=' "$HERE/../$shared"; then
      bad "bin/$shared must NOT set OC_CODESIGN_REQUIRE (it runs on dev Macs + CI; release-only knob belongs in bin/build-release)"
    else
      ok "bin/$shared leaves OC_CODESIGN_REQUIRE unset (dev/CI stay unblocked)"
    fi
  done

  # R7. and the release entry point must actually keep setting it (the mutant
  #     this whole section exists to catch: drop the export → R1/R2 go red).
  if grep -qE '^[^#]*export OC_CODESIGN_REQUIRE=1' "$RELEASE"; then
    ok "bin/build-release sets the release requirement"
  else
    bad "bin/build-release sets the release requirement"
  fi
fi

echo "release-path tests: $PASS ok, $FAIL failed"

# ── bin/setup-codesign-cert hermetic tests (T-33d5 follow-up) ────────────────
# No sudo, never touches the login keychain: p12 import goes into a throwaway
# keychain created inside $WORK and deleted below (never added to the search
# list). The real-keychain cases only run on macOS; elsewhere they are skipped.
SETUP="$HERE/../setup-codesign-cert"
echo "setup-codesign-cert hermetic tests"
if [[ ! -f "$SETUP" ]]; then
  bad "setup-codesign-cert exists at $SETUP"
else
  # 8. openssl req failure is surfaced, not swallowed (shimmed openssl; the
  #    script exits at step 1, long before any sudo/keychain call)
  cat > "$SHIMDIR/openssl" <<'SH'
#!/usr/bin/env bash
if [[ "$1" == "req" ]]; then
  echo "shim-openssl: req exploded (unable to load provider)" >&2
  exit 1
fi
exit 0
SH
  chmod +x "$SHIMDIR/openssl"
  OUT="$(PATH="$SHIMDIR:$PATH" bash "$SETUP" 2>&1)"
  RC=$?
  rm -f "$SHIMDIR/openssl"
  check "openssl req failure exits 1" "1" "$RC"
  case "$OUT" in
    *"shim-openssl: req exploded"*) ok "openssl req stderr is surfaced on failure";;
    *) bad "openssl req stderr is surfaced on failure ($OUT)";;
  esac

  # 9. the pkcs12 export must pin the keychain-compatible legacy algorithms —
  #    OpenSSL 3.x defaults (AES-256/PBKDF2/SHA-256 MAC) make SecKeychainItemImport
  #    fail with "MAC verification failed". Static drift-guard on the script text,
  #    so the live cases below provably exercise the same flags the script uses.
  P12FLAGS='-keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1'
  if grep -Fq -- "$P12FLAGS" "$SETUP"; then
    ok "pkcs12 export pins SHA1-3DES PBE + SHA1 MAC (keychain-compatible)"
  else
    bad "pkcs12 export pins SHA1-3DES PBE + SHA1 MAC (keychain-compatible)"
  fi

  # 10. live red/green: on macOS, export a p12 with the pinned flags and import
  #     it into a throwaway keychain; with a real OpenSSL 3.x also assert the
  #     old default-params export is rejected (the original bug).
  if [[ "$(/usr/bin/uname -s)" == "Darwin" ]] && command -v security >/dev/null; then
    P12DIR="$WORK/p12"; mkdir -p "$P12DIR"
    TESTKC="$P12DIR/t.keychain"
    openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 1 \
      -keyout "$P12DIR/key.pem" -out "$P12DIR/cert.pem" -subj "/CN=oc-p12-test" \
      >/dev/null 2>&1
    security create-keychain -p test "$TESTKC" 2>/dev/null
    # green: pinned flags import cleanly (any openssl flavor)
    # shellcheck disable=SC2086
    openssl pkcs12 -export $P12FLAGS -inkey "$P12DIR/key.pem" -in "$P12DIR/cert.pem" \
      -out "$P12DIR/good.p12" -passout pass:t -name oc-p12-test 2>/dev/null
    if security import "$P12DIR/good.p12" -k "$TESTKC" -P t -A >/dev/null 2>&1; then
      ok "pinned-flags p12 imports into a keychain ($(openssl version 2>/dev/null | cut -d' ' -f1-2))"
    else
      bad "pinned-flags p12 imports into a keychain"
    fi
    # red control: OpenSSL 3.x defaults must still reproduce the bug
    if openssl version 2>/dev/null | grep -q '^OpenSSL 3'; then
      openssl pkcs12 -export -inkey "$P12DIR/key.pem" -in "$P12DIR/cert.pem" \
        -out "$P12DIR/bad.p12" -passout pass:t -name oc-p12-test 2>/dev/null
      IMP_ERR="$(security import "$P12DIR/bad.p12" -k "$TESTKC" -P t -A 2>&1)"
      IMP_RC=$?
      if [[ "$IMP_RC" != "0" ]] && [[ "$IMP_ERR" == *"MAC verification failed"* ]]; then
        ok "OpenSSL 3 default-params p12 still fails MAC verification (red control)"
      else
        bad "OpenSSL 3 default-params p12 still fails MAC verification (rc=$IMP_RC: $IMP_ERR)"
      fi
    else
      echo "  skip — red control needs OpenSSL 3.x on PATH ($(openssl version 2>/dev/null))"
    fi
    security delete-keychain "$TESTKC" 2>/dev/null
  else
    echo "  skip — live p12/keychain cases need macOS security(1)"
  fi
fi

echo "bin tests: $PASS ok, $FAIL failed"

# ── bin/install.sh live-service gate (T-eefc) ────────────────────────────────
# Own file, own PATH shim (launchctl/lsof/uname) and own temp HOME, so it cannot
# share or disturb the fixtures above. Its exit code is folded in here rather
# than left to a human to run — the thing it guards is an OUTAGE of the live
# station, which is precisely the class of regression nobody re-tests by hand.
GUARD="$HERE/install-guard.sh"
echo
if [[ -f "$GUARD" ]]; then
  if run_guard "$GUARD"; then
    ok "install.sh live-service gate suite passed"
  else
    bad "install.sh live-service gate suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-guard.sh is missing"
fi

# ── default-port contract (oc.toml.example ↔ bin/ocserver render guard) ─────
# Own file, own tempdir. The template and render_oc_toml's literal guard are a
# contract with no compiler behind it: drift in either direction is invisible
# until an install detonates at render time. Folded in here so it reddens CI.
PORTS="$HERE/port-default.sh"
echo
if [[ -f "$PORTS" ]]; then
  if run_guard "$PORTS"; then
    ok "default-port contract suite passed"
  else
    bad "default-port contract suite FAILED (see output above)"
  fi
else
  bad "bin/tests/port-default.sh is missing"
fi

# ── install.sh serve-plist claude stamp (T-ba62) ────────────────────────────
# Own file, own tempdir, same PATH-shim discipline. The stamp is what carries
# PATH/OC_CLAUDE_BIN from the operator's interactive shell into the serve plist,
# and from there (via bootstrap-here's env passthrough) into `ocwarden install`.
# Losing it means every one-click host installs a warden that cannot resolve
# claude — the silent failure this ticket closed.
CLAUDESTAMP="$HERE/install-claude-stamp.sh"
echo
if [[ -f "$CLAUDESTAMP" ]]; then
  if run_guard "$CLAUDESTAMP"; then
    ok "install.sh serve-plist claude stamp suite passed"
  else
    bad "install.sh serve-plist claude stamp suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-claude-stamp.sh is missing"
fi

# ── install.sh tool preflight (T-7f38) ──────────────────────────────────────
# Own file, own PATH shim, own temp HOME. It guards the fail-closed gate that
# stops an install on a machine missing tmux or claude — the state where the
# install goes green and every member then sits at 「waking」 with nothing naming
# the cause. Its own counterfactual is inside the suite: the same run against a
# mutant install.sh with the preflight call deleted must SUCCEED, so a gate that
# stops firing reddens here instead of quietly passing.
PREFLIGHT="$HERE/install-preflight-guard.sh"
echo
if [[ -f "$PREFLIGHT" ]]; then
  if run_guard "$PREFLIGHT"; then
    ok "install.sh tool preflight suite passed"
  else
    bad "install.sh tool preflight suite FAILED (see output above)"
  fi
else
  bad "bin/tests/install-preflight-guard.sh is missing"
fi

# ── install.sh --uninstall/--purge/--dry-run ownership + safety (T-3ef9) ────
# Own file, own PATH shim (launchctl only), own temp HOME. This is a NEW
# destructive capability (stop a launchd job, move-or-delete files) — folded
# in here so a regression in its ownership check reddens CI instead of
# waiting for someone to hit it on a real machine.
UNINSTALLGUARD="$HERE/uninstall-guard.sh"
echo
if [[ -f "$UNINSTALLGUARD" ]]; then
  if run_guard "$UNINSTALLGUARD"; then
    ok "install.sh --uninstall ownership/safety suite passed"
  else
    bad "install.sh --uninstall ownership/safety suite FAILED (see output above)"
  fi
else
  bad "bin/tests/uninstall-guard.sh is missing"
fi

# ── namespace mirror across the hand-transcribed copies (T-5047) ───────────
# The namespace→(root, launchd label) derivation exists at ELEVEN SITES in SIX
# FILES, in three languages, across three Go modules that cannot import each
# other. Do NOT restate a smaller number here: this comment said FOUR, and an
# out-of-date count in a dispatcher comment is exactly how the missing sites went
# unnoticed three times. The FILE count has itself been wrong four times (FOUR,
# FIVE, SIX, SEVEN) while the SITE count was right — which is why the shared
# table's header says to count sites. The authoritative, maintained list is the header of
# namespace-mirror-guard.sh. The Go copies are guarded by their own module tests
# (cli/ocwarden/namespace_mirror_test.go, cli/ocagent/namespace_mirror_test.go,
# server/ocserverd/onboarding_mirror_test.go) against the same shared table; this
# guard covers the two shell copies and the charset regex. The
# consequence of a one-character drift is not a wrong string — the server asks
# launchd about a label the warden never registered, concludes "no warden here",
# and installs a second one over the live job.
NSMIRROR="$HERE/namespace-mirror-guard.sh"
echo
if [[ -f "$NSMIRROR" ]]; then
  if run_guard "$NSMIRROR"; then
    ok "namespace mirror suite passed"
  else
    bad "namespace mirror suite FAILED (see output above)"
  fi
else
  bad "bin/tests/namespace-mirror-guard.sh is missing"
fi

# ── install.sh EXIT-time stdin drain (T-fa39) ──────────────────────────────
# Own file, own temp HOME + fake label + launchctl shim. Guards the cosmetic
# half of the same defect the --uninstall rewrite fixed: `curl … | bash` exits
# early, the pipe's reading end closes, and curl signs off with
# "curl: (23|56) Failure writing output to destination" — a red line at the end
# of a SUCCESSFUL run. Kept separate from uninstall-guard.sh because it tests a
# property of how the script is FED, not of what --uninstall decides.
DRAINGUARD="$HERE/stdin-drain-guard.sh"
echo
if [[ -f "$DRAINGUARD" ]]; then
  if run_guard "$DRAINGUARD"; then
    ok "install.sh stdin-drain suite passed"
  else
    bad "install.sh stdin-drain suite FAILED (see output above)"
  fi
else
  bad "bin/tests/stdin-drain-guard.sh is missing"
fi

# ── install.sh prints and exits nothing until fully read (T-4358) ──────────
# The other half of the same defect. The drain above shortens the window in
# which curl gets EPIPE; it cannot close it, because a piped bash acts on the
# first chunk it parses while the rest of the file is still on the wire. The fix
# is structural — one oc_main() the last line calls — so it needs its own real
# HTTP probe rather than another assertion inside the drain suite.
# NOT "fully read before it executes": install.sh's top-level prologue really
# does run as it is parsed. What holds is that none of it prints or exits, so
# nothing is observable until delivery completes. The guard asserts both halves.
READBEFOREEXEC="$HERE/curl-bash-read-before-execute-guard.sh"
echo
if [[ -f "$READBEFOREEXEC" ]]; then
  if run_guard "$READBEFOREEXEC"; then
    ok "install.sh read-before-execute suite passed"
  else
    bad "install.sh read-before-execute suite FAILED (see output above)"
  fi
else
  bad "bin/tests/curl-bash-read-before-execute-guard.sh is missing"
fi

# ── harness process hygiene (T-1a54) ────────────────────────────────────────
# Pins run_bounded.py: the ceiling that stops a busy-loop mutant from running
# forever, and the group-reap that stops anything a guard spawned from leaking
# as an orphan (the seth-m5 46h core-burn).
PROCHYGIENE="$HERE/proc-hygiene-guard.sh"
echo
if [[ -f "$PROCHYGIENE" ]]; then
  if run_guard "$PROCHYGIENE"; then
    ok "harness process-hygiene suite passed"
  else
    bad "harness process-hygiene suite FAILED (see output above)"
  fi
else
  bad "bin/tests/proc-hygiene-guard.sh is missing"
fi

# ── go test cache-defeat (T-bedc) ───────────────────────────────────────────
# bin/ci.sh's step 1e used to run a bare `go test ./...`, so go served green from
# its TEST RESULT CACHE — a real CI log contained `ok  ocwarden  (cached)`, i.e. a
# grid cell that certified a run which never executed (and, worse, structurally
# hid flakes: a suite only runs on the first commit that changes its inputs).
# `-count=1` defeats that cache; this guard pins the flag on every go test call
# site in the repo's shell scripts, by COMMAND-POSITION parsing rather than a
# substring grep (which would match the prose in ci.sh and in the guard itself).
echo
NOCACHE="$HERE/go-test-nocache-guard.sh"
if [[ -f "$NOCACHE" ]]; then
  if run_guard "$NOCACHE"; then
    ok "go test cache-defeat suite passed"
  else
    bad "go test cache-defeat suite FAILED (see output above)"
  fi
else
  bad "bin/tests/go-test-nocache-guard.sh is missing"
fi

# ── bin/release publish/promote read-back (T-588c) ───────────────────────────
# Own file because it needs its own shim set (`gh`, `curl`) and its own fixture
# git repo, and because what it guards is a different KIND of property: not "does
# this script decide correctly" but "after the irreversible step, does it check
# what actually happened". Every case there is a negative — one violated
# requirement per case — so deleting any single read-back rule in bin/release
# turns exactly one of them red instead of silently widening what ships.
# `gh release create` is NEVER reached: the shim records and creates nothing.
RELEASEGUARD="$HERE/release-guard.sh"
echo
if [[ -f "$RELEASEGUARD" ]]; then
  if run_guard "$RELEASEGUARD"; then
    ok "bin/release publish/promote read-back suite passed"
  else
    bad "bin/release publish/promote read-back suite FAILED (see output above)"
  fi
else
  bad "bin/tests/release-guard.sh is missing"
fi

# ── retired image overlay stays retired (T-f014) ────────────────────────────
# The cockpit used to carry two full-size overlays for the same click: the
# shared preview shell (filename, share link, download, close) and a bare
# `Lightbox` backdrop. Which one a user got depended on the call site, and the
# split rotted invisibly — AttachmentStrip stopped reading its `onOpenImage`
# prop, so five call sites passed a handler into a component that ignored it and
# mounted a second overlay that could never open, with nothing red. The
# component and its stylesheet block are gone; this keeps them gone. A green
# does NOT mean there is only one image surface — see the guard's header.
LIGHTBOX="$HERE/lightbox-retired-guard.sh"
echo
if [[ -f "$LIGHTBOX" ]]; then
  if run_guard "$LIGHTBOX"; then
    ok "retired-Lightbox suite passed"
  else
    bad "retired-Lightbox suite FAILED (see output above)"
  fi
else
  bad "bin/tests/lightbox-retired-guard.sh is missing"
fi


# ── single-source rule defer markers (T-c19c) ───────────────────────────────
# The "兩份權威打架" rule (stop and open a reply card when two authorities
# contradict each other) has ONE body, in seeds/system_interaction.md §4.1, and
# three sites that defer to it (CLAUDE.md §8, §9(d), docs/guide/best-practices.md).
# Before T-c19c CLAUDE.md carried its own full copy of the instruction — two
# copies of the same rule, free to drift apart until every reader obeyed whichever
# one they opened. Each deferring site now pins the seed block's content hash, so
# editing the rule turns all of them red at once, and CLAUDE.md /
# docs/guide/best-practices.md are each required BY PATH to carry a current
# marker — a count alone was paddable (three markers in a junk file passed) and
# its minimum was a knob (MIN_DEFER_SITES=0 asserted the empty set). A green here
# does NOT prove there is no unregistered fourth copy, and a red does NOT prove
# the rule's meaning changed — see the guard's header for the honest boundary
# before treating it as either.
RULEDEFER="$HERE/rule-defer-guard.sh"
echo
if [[ -f "$RULEDEFER" ]]; then
  if run_guard "$RULEDEFER"; then
    ok "single-source rule defer-marker suite passed"
  else
    bad "single-source rule defer-marker suite FAILED (see output above)"
  fi
else
  bad "bin/tests/rule-defer-guard.sh is missing"
fi

# ── boot-pack roster freshness (T-792e) ─────────────────────────────────────
# §11 of seeds/system_interaction.md used to state a headcount ("你是這個工作室
# 唯一的 AI member"). It was true when written, false a few months later, and
# nothing anywhere went red — a stale boot pack throws no error, it just teaches
# every agent on every boot that it has no teammates. The seed now names the
# ROLES and points at the roster instead of freezing a snapshot. This guard pins
# both halves: the removed phrasings cannot come back, and the guidance that
# replaced them (plus the sentence explaining why the snapshot is omitted)
# cannot be deleted. A green does NOT prove the boot pack is free of stale
# claims — the guard matches fixed strings; see its header for the boundary.
SEEDROSTER="$HERE/seed-roster-guard.sh"
echo
if [[ -f "$SEEDROSTER" ]]; then
  if run_guard "$SEEDROSTER"; then
    ok "boot-pack roster freshness suite passed"
  else
    bad "boot-pack roster freshness suite FAILED (see output above)"
  fi
else
  bad "bin/tests/seed-roster-guard.sh is missing"
fi

# ── guard-of-the-guard (T-d3e3 rework) ──────────────────────────────────────
# The ci success-marker guard is dispatched at the very BOTTOM of this file,
# AFTER the `[[ "$FAIL" == "0" ]] || exit 1` enforcement below, so its exit code
# is carried by nothing but its own `|| exit 1`. That `|| exit 1` is exactly the
# regression that caused this ticket's rework: without it the guard prints
# "8 ok, 3 failed" and run.sh still exits 0, i.e. the guard is decorative and CI
# step 0b stays green on a forged marker. Nothing reddened when it was removed.
# It does now: this assertion is accounted through bad(), so it is enforced by
# the FAIL count below — and the marker guard, symmetrically, asserts that THIS
# enforcement line still exists. Neither can be deleted alone without a red.
echo
SELF="$HERE/run.sh"
# Anchored, because this file greps ITSELF: an unanchored -F pattern matches the
# very line that carries it, which is a check that can never fail.
if grep -qE '^[[:space:]]*run_guard "\$MARKER_GUARD" \|\| exit 1[[:space:]]*$' "$SELF"; then
  ok "ci success-marker guard dispatch is exit-code-enforced (|| exit 1)"
else
  bad "ci success-marker guard dispatch is exit-code-enforced (|| exit 1) — without it the guard prints FAIL and run.sh still exits 0"
fi

echo "bin tests (incl. install guard): $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1

# T-d3e3: the top-level marker is a final exact log authority, not a broad grep.
# This file runs under `set -uo pipefail` with NO -e, so the guard's exit code
# must be enforced EXPLICITLY (same convention as the `[[ "$FAIL" == "0" ]] ||
# exit 1` above). Without the `|| exit 1` the guard is decorative: it prints
# FAIL and run.sh still exits 0, so CI step 0b stays green on a forged marker.
# Dispatched through run_guard (T-1a54) like every other guard in this file, so
# it inherits the wall-clock ceiling and the process-group reap rather than
# being the one guard that can hang the harness forever.
MARKER_GUARD="$HERE/ci-success-marker.sh"
if [[ -x "$MARKER_GUARD" ]]; then
  run_guard "$MARKER_GUARD" || exit 1
else
  echo "FATAL: ci success-marker guard missing/not executable: $MARKER_GUARD" >&2
  exit 1
fi
exit 0
