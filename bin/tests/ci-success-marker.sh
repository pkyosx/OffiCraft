#!/usr/bin/env bash
# Proves the CI completion marker cannot certify a partial log.
#
# ── WHAT THIS GUARD DOES *NOT* COVER (read this before trusting it) ──────────
# The previous version of this file listed exactly one residual (fragment
# assembly) while a whole vector — dispatched lane scripts — was wide open. A
# comment that is more optimistic than the protection it describes is the same
# 「核心不變量／context 跟碼共存」violation this ticket exists to punish, so the list below is meant to be
# exhaustive as of T-d3e3's rework. Add to it when you narrow it.
#
#  1. FRAGMENT ASSEMBLY. `A='[ci]'; B='all green'; echo "$A $B"` never puts the
#     literal adjacent in the source, so no source-level scan (here or in the
#     lane scan) can see it. Limit of static matching. Applies to ci.sh AND to
#     every lane. NOTE it is still not enough on its own — see 3.
#  2. NON-SHELL EMITTERS — DORMANT while 3 holds. The lane scan enumerates SHELL
#     scripts only (by .sh extension or sh/bash shebang). ci.sh also dispatches
#     python (conformance pytest, bin/tests/lib/run_bounded.py), node/npm (vitest,
#     playwright) and go test; a literal printed by any of those is unscanned.
#     But UNEXPLOITABLE BY ITSELF under the AND rule: a forged tail can only
#     survive if ci.sh stops before its own final line, and the only way it stops
#     is by dying — which means rc != 0. This vector is live only when combined
#     with 3, and 3 is now asserted.
#  3. A MID-RUN `exit 0` IN ci.sh — the one shape that defeated the AND rule.
#     Marker forged in a non-shell lane (2) + a bare `exit 0` in ci.sh after that
#     step ⇒ rc 0, forged marker on the last line, guard fully blind. It is now
#     PINNED by an assertion below (ci.sh must contain no bare `exit 0`), so what
#     is left is only the evasions of that static match: an obfuscated early
#     success such as `exit $((0))`, `exit "$RC"`, or a `trap … EXIT` that
#     rewrites the status. Lower severity than the vector this rework closed:
#     this one requires editing bin/ci.sh — the authority file itself, visible in
#     any diff review — whereas the lane forgery needed one line in a lane with
#     ci.sh left pristine.
#  4. UNTRACKED SCRIPTS. The scan set comes from `git ls-files`, so a script that
#     is dispatched but not tracked is invisible to it.
#  5. THIS FILE. It must be excluded from its own scan (its fixtures are made of
#     the literal), and it IS dispatched inside CI, so a forgery planted here is
#     caught only by the rc half of the authority, not by the scan.
#  6. DOUBLE MUTATION. This guard and bin/tests/run.sh guard each other's
#     enforcement line, so removing EITHER alone reddens — removing BOTH in one
#     change does not.
#  8. THE CONSUMER EXCLUSION IS SELF-GUARDED ONLY BY ITSELF (T-b65e). bin/release
#     is skipped by the lane scan and held to the stricter shape rule below
#     instead. Delete that rule AND leave the exclusion in place, and nothing
#     reddens — same double-mutation shape as 6. The rule is cheap to re-derive
#     from this note, which is why it is recorded rather than guarded again.
#  7. THE CONSUMER. Nothing mechanically forces a human or an agent reading a CI
#     log to apply the rule. accepts_run() below is the rule's executable
#     definition; a consumer who still runs a loose grep is simply wrong, and no
#     test in this repo can stop them.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CI="$ROOT/bin/ci.sh"
RUNNER="$ROOT/bin/tests/run.sh"
PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

# How many times can this source emit the authoritative string, by ANY construct?
#
# Matching only the literal `echo "[ci] all green"` form made the marker
# FORGEABLE: printf, a heredoc body, a variable assignment or a nested script
# could emit the same bytes mid-run and be invisible to the guard. A forged
# marker emitted just before a silent early exit passes the exit code, the
# tail -n 1 log rule AND the old guard at once.
#
# So count the literal STRING in every non-comment line, emitter-agnostic, after
# deleting quote characters (\042 " and \047 ') so a split-argument emitter —
# echo "[ci]" "all green" — normalises onto the same literal. Occurrences, not
# lines, so two forgeries on one line still count as two. That dimension is
# PINNED by the one-line-double fixture below: swap this for `grep -cF` (lines)
# and that assertion reddens.
#
# BOTH greps are `|| true`-guarded on purpose. grep exits 1 on "no match", and
# under `set -euo pipefail` that rc propagates out of the command substitution
# and KILLS THE WHOLE GUARD — so the single most interesting mutation, deleting
# the marker outright, used to take the guard down with it: no FAIL line, no
# remaining assertions, just rc=1. "No match" is a legitimate answer here (it is
# exactly what every lane script must return), so it has to read as 0, not death.
marker_occurrences() {
  { grep -vE '^[[:space:]]*#' "$1" || true; } \
    | tr -d '\042\047' \
    | { grep -oF '[ci] all green' || true; } \
    | wc -l | tr -d '[:space:]'
}

# Exactly one emittable marker, and it is ci.sh's final nonempty source line.
validate_source() {
  local source="$1" count last
  count="$(marker_occurrences "$source")"
  [[ "$count" == "1" ]] || return 1
  last="$(awk 'NF { line=$0 } END { print line }' "$source")"
  [[ "$last" == 'echo "[ci] all green"' ]]
}

# Every OTHER shell script in the repo must be able to emit the authority ZERO
# times. bin/ci.sh is not the only writer of the CI log: it DISPATCHES lane
# scripts (e2e_test/tests_guard/run.sh, bin/tests/run.sh, bin/build-*,
# conformance/run.sh …) whose stdout lands in the same stream a consumer reads.
# validate_source only ever looked at ci.sh, so a forged marker planted in a
# dispatched lane was invisible to this guard while being perfectly capable of
# becoming the log's final line.
lane_clean() { [[ "$(marker_occurrences "$1")" == "0" ]]; }

# The scan set: tracked shell scripts (by extension or by sh/bash shebang) minus
# the FOUR files that legitimately carry the literal — bin/ci.sh (the authority
# itself), THIS guard (its fixtures are made of the literal), and the two
# CONSUMERS: bin/release (T-b65e: its pre-build CI gate has to know what the
# verdict looks like in order to refuse anything else) and bin/local-ci.sh
# (T-4d88: the wider pre-GA round runs bin/ci.sh as its first phase and applies
# the same two-part rule to it).
#
# Neither consumer is waved through: skipping them here only moves them to a
# TIGHTER rule below (see "the consumer's shape"), which requires each one's
# single occurrence to be a comparison and nothing else. The distinction that
# matters to a false green is emit vs compare, and a file that only compares
# cannot forge anything. Note also that neither is a dispatched lane — their
# stdout never becomes a CI log — so even a forged marker there could not be
# mistaken for a verdict; the rule below exists so that stays true by
# construction rather than by luck.
shell_sources() {
  local f shebang
  ( cd "$ROOT" && git ls-files 2>/dev/null || find . -type f | sed 's|^\./||' ) \
  | while IFS= read -r f; do
      [[ -f "$ROOT/$f" ]] || continue
      case "$f" in
        bin/ci.sh|bin/tests/ci-success-marker.sh|bin/release|bin/local-ci.sh) continue ;;
        *.sh) printf '%s\n' "$f"; continue ;;
      esac
      IFS= read -r shebang < "$ROOT/$f" 2>/dev/null || true
      case "$shebang" in
        '#!'*bash*|'#!'*/sh|'#!'*/sh\ *) printf '%s\n' "$f" ;;
      esac
    done
}

# A captured run is successful only if BOTH hold: rc == 0 AND the final line is
# the exact authority.
#
# Why the rc half exists at all — CLAUDE.md deliberately used to say the
# authority "is the literal marker, NOT exit 0", and that wording was earned:
# bin/common.sh's `set -e` once beat run_all.sh's intentional rc capture and made
# a failure signal vanish, i.e. rc has historically been UNTRUSTWORTHY. The rule
# that follows from that is "rc alone is NOT SUFFICIENT to call a run green" —
# NOT "rc must never be looked at". Requiring both is strictly stronger than
# either half and is compatible with the original intent, because the last-line
# half alone is not sufficient either: a lane that prints a forged marker and
# dies (`echo "[ci] all green"; exit 1`) leaves the authority sitting on the
# log's LAST line while ci.sh's `set -e` aborts the run. Two independent signals,
# both required, neither trusted alone.
accepts_run() { # accepts_run RC LOGFILE
  [[ "$1" == "0" ]] || return 1
  [[ "$(tail -n 1 "$2")" == "[ci] all green" ]]
}

echo "ci success-marker contract tests"
if validate_source "$CI"; then ok "ci.sh has one final exact success marker"; else bad "ci.sh has one final exact success marker"; fi

WORK="$(mktemp -d -t oc-ci-marker-tests.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# ── the marker-deleted case, placed EARLY on purpose ────────────────────────
# marker_occurrences must be TOTAL: "the marker appears zero times" is a normal
# answer (every lane script returns it), not an error. It is deliberately called
# here as a BARE ASSIGNMENT — no `if`, no `||` — because that is the one context
# where `set -e` is NOT suppressed, and therefore the only shape that can tell a
# total function from one that dies on `grep`'s no-match rc=1. Drop the
# `|| true`s and this line is the last thing you ever see from this file: no
# FAIL message, no tally, no remaining coverage, just rc=1.
# It sits early so the ~20 assertions below double as the proof of survival.
grep -vF '[ci] all green' "$CI" > "$WORK/no-marker.sh"
NO_MARKER_COUNT="$(marker_occurrences "$WORK/no-marker.sh")"
if [[ "$NO_MARKER_COUNT" == "0" ]]; then
  ok "a source with the marker deleted entirely counts 0 (guard survives, no pipefail death)"
else
  bad "a source with the marker deleted entirely counts 0 (guard survives, no pipefail death) — got $NO_MARKER_COUNT"
fi
if validate_source "$WORK/no-marker.sh"; then bad "ci.sh with NO success marker is rejected"; else ok "ci.sh with NO success marker is rejected"; fi

# ── occurrences, not lines ──────────────────────────────────────────────────
# The one shape that tells the two counting rules apart: a SINGLE line carrying
# TWO emitters. `grep -cF` (lines) answers 1 and waves it through as if it were
# the lone legitimate marker; `grep -oF | wc -l` (occurrences) answers 2. Without
# this fixture the occurrences-vs-lines claim above is an unpinned dead
# dimension — the whole guard stayed 11/11 green when it was swapped out.
printf '%s\n' '[[ -n "${OC_X:-}" ]] && echo "[ci] all green" || echo "[ci] all green"' \
  > "$WORK/one-line-double.sh"
DOUBLE_COUNT="$(marker_occurrences "$WORK/one-line-double.sh")"
if [[ "$DOUBLE_COUNT" == "2" ]]; then
  ok "two emitters on ONE line count as 2 (occurrences, not lines)"
else
  bad "two emitters on ONE line count as 2 (occurrences, not lines) — got $DOUBLE_COUNT"
fi

printf '%s\n' '[ci] (5/5) conformance suite' '[ci] all green' > "$WORK/good.log"
if accepts_run 0 "$WORK/good.log"; then ok "completed log with rc=0 is accepted"; else bad "completed log with rc=0 is accepted"; fi

# Broad grep would accept this nested marker; final-line matching must not.
printf '%s\n' '[tests_guard] all green' '[ci] (1/5) golang' '[ci] FAIL — go test' > "$WORK/partial.log"
if accepts_run 0 "$WORK/partial.log"; then bad "partial log after nested green is rejected"; else ok "partial log after nested green is rejected"; fi

# ── the rc half of the authority (T-d3e3 rework) ────────────────────────────
# The live false-green a reviewer built by hand: plant `echo "[ci] all green";
# exit 1` inside a DISPATCHED lane and ci.sh aborts on set -e with the forged
# authority sitting on the last line. The last-line rule ALONE calls that GREEN.
printf '%s\n' '[ci] (0) e2e_test isolation-guard unit tests (hermetic)' \
              '[ci] FAIL — pretend lane blew up' '[ci] all green' > "$WORK/forged-tail.log"
if accepts_run 1 "$WORK/forged-tail.log"; then
  bad "a run whose last line IS the marker but whose rc is non-zero is REJECTED"
else
  ok "a run whose last line IS the marker but whose rc is non-zero is REJECTED"
fi
if accepts_run 1 "$WORK/good.log"; then bad "rc!=0 rejects even an otherwise perfect log"; else ok "rc!=0 rejects even an otherwise perfect log"; fi
# …and the converse half is still load-bearing: rc=0 alone must NOT be enough.
if accepts_run 0 "$WORK/partial.log"; then bad "rc=0 alone does NOT certify a run (last line still decides)"; else ok "rc=0 alone does NOT certify a run (last line still decides)"; fi

# Mutant: adding later work after the marker is the original false-green bug.
cp "$CI" "$WORK/marker-midway.sh"
printf '%s\n' '' false >> "$WORK/marker-midway.sh"
if validate_source "$WORK/marker-midway.sh"; then bad "mutant with marker before later work is rejected"; else ok "mutant with marker before later work is rejected"; fi

# Mutant: duplicate authorities must also be rejected.
cp "$CI" "$WORK/duplicate-marker.sh"
printf '%s\n' 'echo "[ci] all green"' >> "$WORK/duplicate-marker.sh"
if validate_source "$WORK/duplicate-marker.sh"; then bad "mutant with duplicate success marker is rejected"; else ok "mutant with duplicate success marker is rejected"; fi

# ── forgeability fixtures (T-d3e3 D2) ───────────────────────────────────────
# Each forgery is spliced in BEFORE ci.sh's final line, so the legitimate echo
# stays last and the tail -n 1 rule cannot be what rejects it — only the
# emitter-agnostic count can. This is the dangerous shape: a second emitter
# fires mid-run, a later step exits 0 early, and the log's last line is a
# forged authority.
forge() { # forge NAME LINE... → path to a ci.sh mutant with LINEs spliced in
  local out="$WORK/$1.sh"; shift
  sed '$d' "$CI" > "$out"
  printf '%s\n' "$@" >> "$out"
  tail -n 1 "$CI" >> "$out"
  printf '%s' "$out"
}
reject() { # reject DESC FIXTURE
  if validate_source "$2"; then bad "$1"; else ok "$1"; fi
}

reject "forged printf marker is rejected" \
  "$(forge forge-printf "printf '[ci] all green\\n'")"
reject "forged split-argument echo marker is rejected" \
  "$(forge forge-split 'echo "[ci]" "all green"')"
reject "forged variable-built marker is rejected" \
  "$(forge forge-var 'OK_MARKER="[ci] all green"' 'echo "$OK_MARKER"')"
reject "forged heredoc marker is rejected" \
  "$(forge forge-heredoc "cat <<'FORGED'" '[ci] all green' 'FORGED')"

# ── dispatched-lane scan (T-d3e3 rework) ────────────────────────────────────
# The hole none of the fixtures above could see, because they all mutate ci.sh:
# ci.sh is not the only writer of the CI log. A forged marker in a LANE is worth
# exactly as much to a false-green as one in ci.sh, and validate_source never
# looked at a single lane. Rule: nothing but ci.sh may be ABLE to emit it.
SCAN_SET="$(shell_sources)"
SCANNED="$(printf '%s\n' "$SCAN_SET" | grep -c . || true)"
lane_offenders="$(while IFS= read -r f; do
  [[ -n "$f" ]] || continue
  lane_clean "$ROOT/$f" || printf '%s\n' "$f"
done <<< "$SCAN_SET")"
if [[ -z "$lane_offenders" ]]; then
  ok "no shell script other than bin/ci.sh can emit the CI authority"
else
  bad "shell scripts other than bin/ci.sh can emit the CI authority: $(printf '%s ' $lane_offenders)"
fi
# ── the consumers' shape (T-b65e; second consumer added by T-4d88) ───────────
# Two files are excluded from the scan above because they must state the verdict
# they REQUIRE: bin/release (its pre-build CI gate) and bin/local-ci.sh (the
# wider pre-GA round, whose first phase is bin/ci.sh). An exclusion with no
# replacement rule is just a hole, so the replacement is stricter than the scan:
# EXACTLY ONE occurrence each, and it must sit in a comparison. An `echo`/
# `printf` of the marker appearing in there — or a second occurrence quietly
# accumulating — reddens here.
#
# The shape test binds the MARKER to the comparison, and rejects emission
# constructs outright. An earlier version only asked whether a `[[ … == … ]]`
# appeared somewhere on the line, which two emissions walk straight through:
#   [[ 1 == 1 ]] && echo '<marker>'      and      echo '<marker>'; [[ 1 == 1 ]]
# Both were measured passing it. An exclusion is only as legitimate as the rule
# that replaces it, so the replacement has to actually be stricter than the scan.
#
# The list is written out rather than derived: the ONLY way to add a third
# consumer is to edit this line, and that edit is the review it should get.
for consumer in bin/release bin/local-ci.sh; do
  if [[ ! -f "$ROOT/$consumer" ]]; then
    bad "consumer $consumer is missing — its exclusion from the lane scan is then a hole with no replacement rule"
    continue
  fi
  CONS_OCC="$(marker_occurrences "$ROOT/$consumer")"
  if [[ "$CONS_OCC" == "1" ]]; then
    ok "$consumer carries the CI authority exactly once"
  else
    bad "$consumer carries the CI authority $CONS_OCC times (expected exactly 1: its gate's comparison)"
  fi
  CONS_LINE="$({ grep -vE '^[[:space:]]*#' "$ROOT/$consumer" || true; } | { grep -F '[ci] all green' || true; })"
  case "$CONS_LINE" in
    *echo*|*printf*|*'>'*)
      bad "$consumer's occurrence must be a COMPARISON, not an emission (line: $CONS_LINE)" ;;
    *'=='*'[ci] all green'*)
      ok "$consumer's occurrence is a COMPARISON, not an emission" ;;
    *)
      bad "$consumer's occurrence is not a comparison against the marker (line: ${CONS_LINE:-<none>})" ;;
  esac
done

# A scan is only worth what it actually looks at: pin that the real dispatched
# lanes are inside the set (a silently-empty enumeration would otherwise pass
# the assertion above forever).
for lane in e2e_test/tests_guard/run.sh bin/tests/run.sh conformance/run.sh bin/build-bindist; do
  if printf '%s\n' "$SCAN_SET" | grep -qFx "$lane"; then
    ok "lane scan covers $lane"
  else
    bad "lane scan covers $lane (scanned $SCANNED files)"
  fi
done
# The reviewer's live false-green, reproduced hermetically: the exact forgery he
# planted in the step-0 lane must be caught by the lane scan.
cp "$ROOT/e2e_test/tests_guard/run.sh" "$WORK/forged-lane.sh"
printf '%s\n' 'echo "[ci] FAIL — pretend lane blew up" >&2; echo "[ci] all green"; exit 1' \
  >> "$WORK/forged-lane.sh"
if lane_clean "$WORK/forged-lane.sh"; then
  bad "a lane script carrying a forged marker is caught by the lane scan"
else
  ok "a lane script carrying a forged marker is caught by the lane scan"
fi
# Sentinel: the untouched lane itself must PASS (the scan is not a wall).
if lane_clean "$ROOT/e2e_test/tests_guard/run.sh"; then
  ok "sentinel — the real step-0 lane script passes the lane scan"
else
  bad "sentinel — the real step-0 lane script passes the lane scan"
fi

# ── guard-of-the-guard, second half (T-d3e3 rework) ─────────────────────────
# bin/tests/run.sh dispatches THIS file as `run_guard "$MARKER_GUARD" || exit 1`
# and asserts — through its own accounted bad() — that the `|| exit 1` is still
# there. That assertion is enforced by run.sh's single enforcement point,
# `[[ "$FAIL" == "0" ]] || exit 1`; delete THAT and every accounted assertion in
# run.sh, including the one protecting this guard, silently goes decorative.
# So the two files guard each other and removing either enforcement ALONE
# reddens: drop run.sh's `|| exit 1` → run.sh's own check fails → FAIL>0 → exit 1;
# drop run.sh's FAIL enforcement → this check fails → guard rc=1 → `|| exit 1`.
# Anchored to a whole line: the same literal also appears inside run.sh's
# explanatory comments, and an unanchored -F match would be satisfied by those
# alone — a check with no discriminating power.
if grep -qE '^\[\[ "\$FAIL" == "0" \]\] \|\| exit 1[[:space:]]*$' "$RUNNER"; then
  ok "bin/tests/run.sh still enforces its accounted-failure count (|| exit 1)"
else
  bad "bin/tests/run.sh still enforces its accounted-failure count — without it every ok/bad in that file is decorative"
fi

# ── no mid-run `exit 0` in ci.sh ────────────────────────────────────────────
# The ONE shape that still beat the AND rule, built by hand during review: plant
# a marker in a NON-SHELL lane (bin/tests/lib/run_bounded.py — dispatched, and
# outside the shell scan by construction), then add a bare `exit 0` to ci.sh
# after step 0b. Result: rc 0, last line the forged marker, guard 26 ok/0 failed
# — a clean false green through BOTH halves of the authority.
#
# What makes it work is not the python forgery, it is the early `exit 0`. Without
# it a forged tail can only survive by ci.sh ABORTING, and aborting means rc != 0.
# So this assertion is what keeps residual vector 2 dormant: ci.sh must run to its
# final line or die, never quietly succeed early.
EARLY_EXITS="$(grep -nE '^[[:space:]]*exit 0([[:space:]]|$)' "$CI" || true)"
if [[ -z "$EARLY_EXITS" ]]; then
  ok "ci.sh contains no bare 'exit 0' (it reaches the marker or dies — never exits green early)"
else
  bad "ci.sh contains a bare 'exit 0' — a mid-run early success lets ANY earlier forged marker become the last line with rc 0: $(printf '%s ' $EARLY_EXITS)"
fi

# The marker only means anything because ci.sh ABORTS on the first failing step;
# without fail-fast every step could go red and the legitimate final echo would
# still run with rc 0, satisfying BOTH halves of the authority at once.
if grep -qFx 'set -euo pipefail' "$CI"; then
  ok "ci.sh still fails fast (set -euo pipefail) — the precondition of the marker"
else
  bad "ci.sh still fails fast (set -euo pipefail) — without it a red step never stops the run and the REAL marker prints with rc 0"
fi

# Sentinel: the hardened rule must not be so strict that the REAL ci.sh fails —
# a single legitimate `echo "[ci] all green"` as the final line still passes.
if validate_source "$CI"; then ok "sentinel — the legitimate single echo form still PASSES"; else bad "sentinel — the legitimate single echo form still PASSES"; fi

# Nested suites' own markers are DIFFERENT authorities, not forgeries: a source
# that also emits them must still validate (regression twin of the log-side
# partial.log case above).
NESTED="$(forge nested-markers 'echo "[tests_guard] all green"' 'echo "[conformance] all green"')"
if validate_source "$NESTED"; then ok "nested [tests_guard]/[conformance] markers are not mistaken for the CI marker"; else bad "nested [tests_guard]/[conformance] markers are not mistaken for the CI marker"; fi

echo "ci success-marker contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
