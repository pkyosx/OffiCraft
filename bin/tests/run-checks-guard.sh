#!/usr/bin/env bash
# Contract tests for bin/run-checks.sh — the wrapper that proves each check it
# was asked for actually reached its own end.
#
# ── WHY THIS EXISTS (T-4d88) ─────────────────────────────────────────────────
# bin/run-checks.sh is the thing that stands between "make exited 0" and "the
# checks ran". Its behaviour was verified ONCE, BY HAND, on the day it was
# written: two mutants driven at a real Makefile target and eyeballed. That is
# evidence about that afternoon, not about tomorrow. Delete the marker loop, or
# let a refactor turn the missing-marker exit into a printed warning, and every
# cloud cell keeps reporting green while asserting nothing — the exact silent
# regression the wrapper exists to prevent, now inside the wrapper itself.
#
# So the mutants moved in here and run every round.
#
# ── HOW ──────────────────────────────────────────────────────────────────────
# Hermetic: a throwaway root ($WORK/root) with a COPY of the real script and a
# fixture Makefile whose targets are written to behave in the ways that matter —
# one prints its own marker, one prints output but no marker (an emptied or
# early-exited recipe), one fails outright, one prints somebody ELSE's marker.
# Nothing here runs a real check or touches the developer's tree.
#
# ── WHAT IT PINS ─────────────────────────────────────────────────────────────
#  C1  Zero arguments is REFUSED with a non-zero exit. An empty round asserts
#      nothing, and a wrapper that accepts one is a wrapper that can be defanged
#      by deleting the argument list.
#  C2  Positive control: every named target printing its marker exits 0. Without
#      this, C3's red could be coming from anything at all.
#  C3  A named target that does NOT print its marker ⇒ non-zero exit that NAMES
#      that target, and does not name the innocent one. The `exit 1` is asserted,
#      not the message alone: a printed warning is not a guard.
#  C4  A target whose marker is absent while some OTHER target's marker is
#      present is still red — the match is per-target and whole-line, so a log
#      that merely contains the words cannot buy a pass.
#  C5  make's own non-zero exit is propagated (the wrapper does not swallow a
#      real failure on its way to the marker check).
#  C6  Counterfactual: the SAME C3 scenario against a mutant copy whose marker
#      loop has been neutered must come out GREEN. This is what makes C3 mean
#      something — it proves the red comes from the marker check and not from
#      the fixture being broken in some other way.
#
# ── WHAT IT DOES NOT COVER ───────────────────────────────────────────────────
#  1. Whether the real Makefile's targets actually print their markers. That is
#     asserted every round by the real cells calling the real wrapper — this
#     file is about the wrapper's own logic.
#  2. Whether a caller uses the wrapper at all. That is
#     bin/tests/ci-run-checks-entrypoint-guard.sh.
#  3. A recipe gutted with its marker line LEFT BEHIND still prints it. The
#     marker proves the recipe reached its end, never that the end was worth
#     reaching — the wrapper's own header says so, and no test here pretends
#     otherwise.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
SRC="$ROOT/bin/run-checks.sh"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

[[ -f "$SRC" ]] || { echo "FATAL: $SRC not found — the wrapper this guard tests is gone" >&2; exit 2; }
command -v make >/dev/null 2>&1 || { echo "FATAL: no make on PATH; this guard drives a real make" >&2; exit 2; }

WORK="$(mktemp -d -t oc-run-checks-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# ── the fixture tree ─────────────────────────────────────────────────────────
mk_root() { # mk_root <dir> <script-to-install>
  local dir="$1" script="$2"
  mkdir -p "$dir/bin/lib"
  cp "$script" "$dir/bin/run-checks.sh"
  # T-127: the wrapper reads THE round list through this library, so the fixture
  # tree needs both. The library is copied from the real tree (it is the thing
  # under test on the --lane cases); the list is a fixture of its own, so these
  # cases never depend on which checks the repo happens to have today.
  cp "$ROOT/bin/lib/ci-round.sh" "$dir/bin/lib/ci-round.sh"
  {
    printf '# fixture round list\n'
    printf 'lane-one good-a\n'
    printf 'lane-one good-b\n'
    printf 'lane-two silent\n'
  } >"$dir/bin/lib/ci-round.txt"
  # Tabs are load-bearing in a Makefile; printf keeps them explicit.
  {
    printf 'good-a:\n\t@echo "[good-a] working"; echo "[oc-check-done] good-a"\n'
    printf 'good-b:\n\t@echo "[good-b] working"; echo "[oc-check-done] good-b"\n'
    # An emptied / early-exited recipe: it says something and succeeds, and
    # never reaches its own end. This is the shape a zero exit cannot tell apart
    # from a check that ran.
    printf 'silent:\n\t@echo "[silent] starting"\n'
    # A recipe that prints a marker belonging to a DIFFERENT target.
    printf 'impostor:\n\t@echo "[oc-check-done] some-other-target"\n'
    printf 'boom:\n\t@echo "[boom] about to fail"; exit 3\n'
  } >"$dir/Makefile"
}

run_wrapper() { # run_wrapper <root> <args...> -> rc, stdout+stderr in $OUT
  local dir="$1"; shift
  OUT="$(bash "$dir/bin/run-checks.sh" "$@" 2>&1)"
  return $?
}

REAL="$WORK/real"
mk_root "$REAL" "$SRC"

# ── C1: zero arguments is refused ────────────────────────────────────────────
run_wrapper "$REAL"; rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "C1 — zero arguments is refused with a non-zero exit (rc=$rc)"
else
  bad "C1 — zero arguments exited 0; an empty round asserts nothing, so accepting one lets any caller defang the wrapper by passing no targets"
fi
if printf '%s' "$OUT" >/dev/null && grep -qi 'target' <<<"$OUT"; then
  ok "C1 — the refusal says what is missing (mentions the targets it needed)"
else
  bad "C1 — the refusal printed nothing about targets: $OUT"
fi

# ── C2: positive control ─────────────────────────────────────────────────────
run_wrapper "$REAL" good-a good-b; rc=$?
if [[ "$rc" -eq 0 ]]; then
  ok "C2 — targets that print their own end marker pass (rc=0)"
else
  bad "C2 — targets that DO print their markers were rejected (rc=$rc); every red below would be meaningless: $OUT"
fi
if grep -q 'all 2 check(s) reported their own end marker' <<<"$OUT"; then
  ok "C2 — and it says so, naming how many it held itself to"
else
  bad "C2 — the passing run printed no summary of what it verified: $OUT"
fi

# ── C3: a named target that never reaches its end ────────────────────────────
run_wrapper "$REAL" good-a silent; rc=$?
c3_out="$OUT"
if [[ "$rc" -ne 0 ]]; then
  ok "C3 — a target that never printed its end marker EXITS NON-ZERO (rc=$rc), not merely warns"
else
  bad "C3 — a silent target passed (rc=0). make exited 0 and nothing noticed the check never ran: $c3_out"
fi
if grep -q 'silent' <<<"$c3_out"; then
  ok "C3 — the failure NAMES the target that went silent"
else
  bad "C3 — the failure does not name 'silent', so a red would not say which check vanished: $c3_out"
fi
# Anchored on the ACCUSATION line specifically. An unanchored match also hits
# the all-clear summary, which lists every target it was asked for — that made
# this case report an innocent-naming bug that was not there.
if grep -qE '^FAIL — make exited 0 .*end marker:.*\bgood-a\b' <<<"$c3_out"; then
  bad "C3 — the failure also accuses good-a, which did print its marker; a guard that names innocents gets ignored: $c3_out"
else
  ok "C3 — and it does not accuse the target that did print its marker"
fi

# ── C4: the match is per-target and whole-line ───────────────────────────────
run_wrapper "$REAL" silent impostor; rc=$?
if [[ "$rc" -ne 0 ]] && grep -q 'silent' <<<"$OUT"; then
  ok "C4 — a marker naming SOME OTHER target does not satisfy the one that was asked for (rc=$rc)"
else
  bad "C4 — a log containing an unrelated [oc-check-done] line bought a pass (rc=$rc): $OUT"
fi

# ── C5: a real failure is propagated, not swallowed ──────────────────────────
run_wrapper "$REAL" boom; rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "C5 — make's own failure is propagated (rc=$rc)"
else
  bad "C5 — a failing target came out as rc=0: $OUT"
fi
if grep -q 'all 1 check(s) reported their own end marker' <<<"$OUT"; then
  bad "C5 — a failing run still printed the all-clear summary: $OUT"
else
  ok "C5 — and a failing run does not print the all-clear summary"
fi

# ── C6: counterfactual — neuter the marker loop, C3 must go green ────────────
# Built by editing the ONE line that does the work. If that line is not found,
# this guard says so rather than reporting a mutant it never made: a mutant that
# silently failed to apply produces a green that means nothing.
MUTANT_SRC="$WORK/run-checks-mutant.sh"
NEEDLE='grep -qFx "[oc-check-done] $target" "$LOG" || missing+=("$target")'
if ! grep -qF "$NEEDLE" "$SRC"; then
  bad "C6 — could not find the per-target marker assertion in bin/run-checks.sh, so the counterfactual was never built. Either that line moved (update this guard in the same commit) or it is gone (which is the regression this file exists to catch)."
else
  # Whole-line replacement, matched with index() — NOT sub(), whose first
  # argument is a regex and would blow up on the brackets and parentheses in
  # that line (it did, the first time this was written).
  awk -v needle="$NEEDLE" '
    index($0, needle) { print "  :"; next } { print }
  ' "$SRC" >"$MUTANT_SRC"
  MUT="$WORK/mutant"
  mk_root "$MUT" "$MUTANT_SRC"
  run_wrapper "$MUT" good-a silent; rc=$?
  if [[ "$rc" -eq 0 ]]; then
    ok "C6 — with the marker assertion neutered the SAME scenario passes, so C3's red is caused by that assertion and nothing else"
  else
    bad "C6 — the mutant still failed (rc=$rc), so C3's red is not attributable to the marker check; something else in this fixture is red: $OUT"
  fi
fi

# ═════════════════════════════════════════════════════════════════════════════
# THE LANE DOOR (T-127)
# ═════════════════════════════════════════════════════════════════════════════
# `--lane` is how every cloud gate cell now asks for its checks, and it exists so
# that .github/workflows/ci.yml contains no target name to fall behind. The whole
# value of that depends on ONE property: expansion must not soften the marker
# assertion. A lane that expanded and then asserted nothing would be a silent
# hole in the exact place the old hand-typed lists were.

# ── C7: a lane expands and every expanded target is still asserted ───────────
run_wrapper "$REAL" --lane lane-one; rc=$?
if [[ "$rc" -eq 0 ]] && [[ "$OUT" == *"good-a"* ]] && [[ "$OUT" == *"good-b"* ]]; then
  ok "C7 — --lane expands to that lane's targets and runs them (both named in the summary)"
else
  bad "C7 — --lane lane-one should run good-a and good-b and pass (rc=$rc): $OUT"
fi

# ── C8: THE ONE THAT MATTERS — expansion does not soften the assertion ───────
# lane-two holds `silent`, whose recipe succeeds without reaching its own end.
# Named directly, C3 already proves that is refused. Through the lane door it
# must be refused identically, and the message must NAME the target — otherwise
# a reader cannot tell which check in the lane never ran.
run_wrapper "$REAL" --lane lane-two; rc=$?
if [[ "$rc" -ne 0 ]] && [[ "$OUT" == *"silent"* ]]; then
  ok "C8 — a target reached through --lane is held to its own end marker exactly as a named one is, and the failure names it"
else
  bad "C8 — --lane lane-two must fail naming 'silent' (rc=$rc): $OUT"
fi

# ── C9: an unknown lane FAILS; it must never be an empty, green round ────────
# "Nothing ran" and "nothing failed" are the same picture, and an empty round
# exits 0. A cell asking for a lane nobody assigned is a wiring mistake, and a
# wiring mistake that reports success is how a check disappears without a trace.
run_wrapper "$REAL" --lane lane-nobody-assigned; rc=$?
if [[ "$rc" -ne 0 ]] && [[ "$OUT" == *"lane-nobody-assigned"* ]]; then
  ok "C9 — an unknown lane is refused and named, rather than expanding to an empty (and therefore green) round"
else
  bad "C9 — an unknown lane must fail and name itself (rc=$rc): $OUT"
fi

# ── C10: --lane takes exactly one lane ───────────────────────────────────────
run_wrapper "$REAL" --lane lane-one lane-two; rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "C10 — --lane with more than one argument is refused (rc=$rc), so a second lane cannot be silently dropped"
else
  bad "C10 — --lane lane-one lane-two should be refused: $OUT"
fi

# ── C11: a malformed round row must not yield a SHORTER, GREEN lane ──────────
# Found by an independent review, in bin/ci.sh: the round was collected with a
# process substitution, which DISCARDS the reader's exit status. A malformed row
# was skipped, the well-formed targets ran, and the round printed its all-clear
# with rc 0. The same shape sat one layer down in ci_round_targets, which piped
# through awk and reported AWK's success. Both are command substitution now, and
# this case is what keeps them that way.
printf 'lane-one good-a\nlane-one this row has too many fields\n' >"$REAL/bin/lib/ci-round.txt"
run_wrapper "$REAL" --lane lane-one; rc=$?
if [[ "$rc" -ne 0 ]]; then
  ok "C11 — a malformed round row REFUSES the lane (rc=$rc) instead of silently running the rows that happened to parse"
else
  bad "C11 — a malformed row must not produce a short, green lane: $OUT"
fi
mk_root "$REAL" "$SRC"   # restore the fixture's round list for anything after this

printf 'run-checks wrapper contract tests: %d ok, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
