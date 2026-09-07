#!/usr/bin/env bash
# Run named Makefile check targets AND prove each of them reached its own end.
#
# ── WHY THIS EXISTS (T-4d88) ─────────────────────────────────────────────────
# A ZERO EXIT SAYS "NOTHING FAILED", NOT "SOMETHING RAN". A make target whose
# recipe is emptied — deleted in an edit, commented out, cut short by an early
# `exit 0` — succeeds instantly and silently. rc cannot tell that apart from a
# check that ran and passed, and neither can a human reading a log for a line
# that is simply absent.
#
# This protection is not new here. Before T-4d88 the cloud macOS cell piped its
# script through `tee` and then grepped for that script's final marker, for
# exactly this reason. T-4d88 deleted the script; the marker went with it and the
# protection lost its home. This file is where it lives now.
#
# ── HOW ──────────────────────────────────────────────────────────────────────
# Every check target in the Makefile ends by printing its OWN end marker:
#
#     [oc-check-done] <target>
#
# printed as the LAST clause of the recipe's single shell command, so anything
# that stops the recipe early — a failure, an early exit, a deleted body — takes
# the marker with it. This wrapper runs `make <targets>`, tees the output, and
# then requires the marker of EVERY target it was asked for. A missing one is a
# non-zero exit naming the target, not a warning.
#
# ── WHERE THE TARGETS COME FROM ─────────────────────────────────────────────
# The expected markers are DERIVED from what this invocation was asked to run,
# and are asserted one per target no matter which of the two doors was used:
#
#   * NAMED TARGETS — `bin/run-checks.sh lint-ts test-frontend-unit`. What a
#     developer reaches for while working on their own area.
#   * `--lane <lane>` — expand that lane out of bin/lib/ci-round.txt and assert
#     every target it expands to. This is the door the cloud gate cells use.
#
# T-4d88 killed three copies of HOW each check runs (they moved into one Makefile
# recipe each). What it did NOT kill was the second copy of WHICH checks run:
# bin/ci.sh listed them and .github/workflows/ci.yml listed them again, by hand,
# and nothing compared the two. T-127 removed the workflow's copy — the cells now
# ask for a lane, and bin/lib/ci-round.txt is the only place a target is written
# down. `--lane` is that door; it is not a second enumeration, it is the end of
# the second enumeration.
#
# ⚠️ EXPANSION DOES NOT SOFTEN THE ASSERTION, and that matters more here than
# anywhere: this wrapper's marker requirement is the ONLY thing standing between
# "every check passed" and "a recipe was emptied and succeeded instantly". A lane
# is expanded FIRST and then held to every target it produced, exactly as if a
# caller had typed them out. An unknown or empty lane is a hard failure rather
# than an empty round, because a round of zero checks exits 0 and looks green.
#
# ⚠️ WHAT THIS IS STILL NOT: it does not assert that the lanes COLLECTIVELY cover
# every target in the Makefile. That assertion exists now, but it lives in
# `lint-ci-round` (bin/ci-round-guard.py), not here.
#
# ── WHAT IT DOES NOT COVER ───────────────────────────────────────────────────
#  1. A recipe gutted with the marker line LEFT BEHIND still prints it. The
#     marker proves the recipe reached its end, never that the end was worth
#     reaching. It is the same class of statement as the suite markers this repo
#     already relies on.
#  2. Prerequisite targets pulled in by make are not asserted — only the targets
#     named on the command line. A caller asserts what it asked for.
#  3. Nothing here forces a caller to use this wrapper instead of bare `make`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/bin/lib/ci-round.sh"

# `--lane <lane>` expands to that lane's targets and is then indistinguishable
# from having typed them: TARGETS is what the marker assertion below reads, and
# there is only one of it. A lane that names nothing, or names a lane the round
# list does not carry, exits non-zero from ci_round_lane rather than yielding an
# empty TARGETS — an empty round asserts nothing and exits 0, which is the one
# outcome this whole file exists to refuse.
TARGETS=()
if [[ "${1:-}" == "--lane" ]]; then
  if [[ $# -ne 2 ]]; then
    echo "FAIL — bin/run-checks.sh --lane takes exactly one lane name." >&2
    exit 2
  fi
  while IFS= read -r t; do TARGETS+=("$t"); done < <(ci_round_lane "$ROOT" "$2")
  if [[ ${#TARGETS[@]} -eq 0 ]]; then
    echo "FAIL — lane '$2' expanded to no checks." >&2
    exit 2
  fi
  echo "[run-checks] lane '$2' expands to ${#TARGETS[@]} check(s): ${TARGETS[*]}"
else
  TARGETS=("$@")
fi

if [[ ${#TARGETS[@]} -eq 0 ]]; then
  echo "FAIL — bin/run-checks.sh needs at least one Makefile target: an empty round would assert nothing." >&2
  exit 2
fi

LOG="$(mktemp -t oc-run-checks.XXXXXX)"
trap 'rm -f "$LOG"' EXIT

set +e
make -C "$ROOT" "${TARGETS[@]}" 2>&1 | tee "$LOG"
rc="${PIPESTATUS[0]}"
set -e
[[ "$rc" == "0" ]] || exit "$rc"

missing=()
for target in "${TARGETS[@]}"; do
  grep -qFx "[oc-check-done] $target" "$LOG" || missing+=("$target")
done

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "FAIL — make exited 0 but these checks never reached their own end marker: ${missing[*]}" >&2
  echo "A target whose recipe was emptied, commented out or cut short by an early exit succeeds" >&2
  echo "instantly and silently. Each check prints '[oc-check-done] <target>' as the last clause of" >&2
  echo "its recipe; a missing one means that check did NOT run to completion, whatever rc says." >&2
  exit 1
fi

echo "[run-checks] all ${#TARGETS[@]} check(s) reported their own end marker: ${TARGETS[*]}"
