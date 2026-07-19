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
mkdir -p "$SHIMDIR"
: > "$TRIPWIRE"
trap 'rm -rf "$WORK"' EXIT

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
BIN="$WORK/target-binary"
run_case() {
  printf 'AB' > "$BIN"
  : > "$TRIPWIRE"
  OUT="$(PATH="$SHIMDIR:$PATH" SHIM_TRIPWIRE="$TRIPWIRE" bash "$SCRIPT" "$BIN" com.officraft.test 2>&1)"
  RC=$?
}

echo "codesign-artifact hermetic tests"

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
  #     with no keychain still builds and still ships (adhoc, with a warning).
  SHIM_HAS_IDENTITY=0 run_dev_case
  check "DEV + missing identity exits 0 (dev Macs are NOT blocked)" "0" "$RC"
  case "$OUT" in
    *"fake-build: OC_CODESIGN_REQUIRE=<unset>"*) ok "DEV shape leaves OC_CODESIGN_REQUIRE unset (default off)";;
    *) bad "DEV shape leaves OC_CODESIGN_REQUIRE unset (default off) ($OUT)";;
  esac
  case "$OUT" in
    *WARNING*ADHOC-signed*) ok "DEV + missing identity warns and ships as built";;
    *) bad "DEV + missing identity warns and ships as built ($OUT)";;
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
[[ "$FAIL" == "0" ]] || exit 1
exit 0
