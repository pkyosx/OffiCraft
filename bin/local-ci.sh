#!/usr/bin/env bash
# officraft WIDE local round — bin/ci.sh PLUS the e2e specs it does not run.
#
# ── WHAT THIS IS, AND WHAT IT IS NOT ─────────────────────────────────────────
# This is NOT a replacement for bin/ci.sh and NOT a rename of it. bin/ci.sh is
# unchanged, still self-contained, still the thing you run before a push, and
# nothing about which round decides a land moved here. This script CALLS it and
# then does the one thing it deliberately does not do: run the playwright specs
# in e2e_test/ (bin/ci.sh only exercises their wiring, via tests_guard — a green
# there means zero specs ran).
#
# It is also not a `make ci`-style aggregate: it names no check. The round comes
# from bin/lib/ci-round.txt, which since T-127 is the only place any check is
# written down — bin/ci.sh reads all of it and each cloud cell reads one lane, so
# there is no second list of what CI runs anywhere.
#
# ── WHEN TO RUN IT (not every time) ──────────────────────────────────────────
# Two occasions, both rare and both deliberate:
#   * before cutting a GA release;
#   * when you changed something in the LIVE-AGENT path and want to see that
#     class actually exercised.
# The everyday round stays `bash bin/ci.sh`. This one stands a whole station up
# and drives a browser, so it costs minutes; with --live-agent it costs money.
#
# ── THE LIVE-AGENT CLASS: DEFAULT OFF, AND A TYPO MUST NOT SPEND ─────────────
# Specs that need a live agent process spawn a real `claude` and BURN REAL API
# QUOTA. The mechanism that keeps them out is NOT reinvented here: membership is
# declared by filename (*.live-agent.spec.js), playwright.config.js ignores that
# class unless OC_E2E_LIVE_AGENT is EXACTLY '1', and e2e_test/assert-specs-ran.sh
# audits afterwards that the class really stayed out when it was not asked for.
# All three are pre-existing (T-c329, owner ruled at rc-d51e755d3207 /
# rc-4e3ae0ec146d) and this file only decides what to pass them.
#
# 🔴 Two consequences of that ruling, implemented literally below:
#   * DEFAULT IS OFF. Running with no arguments never spends.
#   * A NEAR MISS FALLS TOWARD NOT SPENDING. `--live-agent` is matched as a WHOLE
#     ARGUMENT. `--live-agent=1`, `--live-agents`, `-live-agent`, `--live_agent`
#     are not it — every unrecognised argument is REFUSED before anything runs
#     (exit 2), so a mistyped opt-in cannot become an opt-in, and — worse and the
#     real reason for exact matching — a mistyped opt-OUT such as
#     `--live-agent=0` cannot become one either. A prefix-matching parser would
#     read that last one as consent to spend, which is the exact inversion this
#     shape exists to make impossible.
#
# The flag is also the ONLY input. An OC_E2E_LIVE_AGENT already sitting in the
# caller's environment is overridden by this script's own decision and the
# override is announced, because "did that shell still have the variable
# exported?" must never be part of the answer to "is this run going to spend?".
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LIVE_AGENT=0
DRY_RUN=0

usage() {
  cat <<'USAGE'
usage: bash bin/local-ci.sh [--live-agent | --no-live-agent] [--dry-run]

  (no arguments)     bin/ci.sh, then the e2e specs. The live-agent class does
                     NOT run and no API quota is spent.
  --live-agent       ALSO run the live-agent specs. 🔴 This spawns a real agent
                     and SPENDS REAL MONEY, every time.
  --no-live-agent    The default, stated explicitly.
  --dry-run          Print what this round WOULD do, run nothing, spend nothing.

Run this before a GA release, or when you changed live-agent behaviour.
The everyday round is `bash bin/ci.sh`.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --live-agent)    LIVE_AGENT=1 ;;
    --no-live-agent) LIVE_AGENT=0 ;;
    --dry-run)       DRY_RUN=1 ;;
    -h|--help)       usage; exit 0 ;;
    *)
      echo "[local-ci] REFUSED — unrecognised argument: '$1'" >&2
      echo "[local-ci] Nothing was executed and no API quota was spent." >&2
      echo "[local-ci] '--live-agent' is matched as a WHOLE argument on purpose: a near miss" >&2
      echo "[local-ci] ('--live-agent=1', '--live-agents', '--live_agent') must not be read as" >&2
      echo "[local-ci] consent to spend, and '--live-agent=0' must not be read as consent either." >&2
      echo >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

# The single source of truth for this round. Exported unconditionally — including
# the 0 — so the strict `=== '1'` test downstream sees THIS decision and never an
# inherited one.
if [[ -n "${OC_E2E_LIVE_AGENT:-}" && "${OC_E2E_LIVE_AGENT}" != "$LIVE_AGENT" ]]; then
  echo "[local-ci] note — ignoring inherited OC_E2E_LIVE_AGENT='${OC_E2E_LIVE_AGENT}'; this round's flag decides."
  echo "[local-ci]        pass --live-agent on the command line to actually ask for the live-agent class."
fi
export OC_E2E_LIVE_AGENT="$LIVE_AGENT"

if [[ "$LIVE_AGENT" == "1" ]]; then
  echo "[local-ci] 🔴 live-agent class REQUESTED (OC_E2E_LIVE_AGENT=1) — this run spawns a real agent and SPENDS REAL API QUOTA."
else
  echo "[local-ci] live-agent class NOT requested (OC_E2E_LIVE_AGENT=0) — no agent will be spawned and no API quota spent."
fi

if [[ "$DRY_RUN" == "1" ]]; then
  echo "[local-ci] DRY RUN — the two phases below were NOT executed:"
  echo "[local-ci]   phase 1: bash bin/ci.sh"
  echo "[local-ci]   phase 2: bash e2e_test/run_all.sh  +  bash e2e_test/assert-specs-ran.sh <log>"
  echo "[local-ci] dry run complete — nothing ran, nothing was spent. This is NOT a verdict."
  exit 0
fi

LOGDIR="$(mktemp -d -t oc-local-ci.XXXXXX)"
echo "[local-ci] logs: $LOGDIR"

# ── phase 1: the ordinary local round, unchanged ─────────────────────────────
# Its verdict is read by the repo's own rule and not by rc alone: rc == 0 AND the
# log's final line is exactly the authority marker. Both halves are required —
# rc has lied here before (bin/common.sh's `set -e` once swallowed run_all.sh's
# deliberate rc capture), and the last line alone is forgeable by a dispatched
# lane that prints the marker and then dies. bin/tests/ci-success-marker.sh is
# that rule's executable form and holds THIS file to it: the marker appears here
# exactly once and only as a comparison, never as something this script emits.
echo "[local-ci] === phase 1/2: bin/ci.sh ==="
set +e
bash "$ROOT/bin/ci.sh" 2>&1 | tee "$LOGDIR/ci.log"
ci_rc="${PIPESTATUS[0]}"
set -e
if [[ "$ci_rc" != "0" ]]; then
  echo "[local-ci] FAIL — bin/ci.sh exited $ci_rc. Log: $LOGDIR/ci.log" >&2
  exit "$ci_rc"
fi
ci_tail="$(tail -n 1 "$LOGDIR/ci.log")"
if [[ "$ci_tail" == '[ci] all green' ]]; then
  echo "[local-ci] phase 1 accepted (rc 0 AND the final line is the authority marker)."
else
  echo "[local-ci] FAIL — bin/ci.sh exited 0 but its log does not END with the authority marker." >&2
  echo "[local-ci] rc alone does not certify a round; a truncated or forged tail is exactly what" >&2
  echo "[local-ci] the two-part rule exists to reject. Log: $LOGDIR/ci.log" >&2
  exit 1
fi

# ── phase 2: the specs bin/ci.sh never runs ──────────────────────────────────
# run_all.sh owns the whole lifecycle (setup -> playwright -> teardown, teardown
# on an EXIT trap) including its own refusals, so it is invoked as a unit. Then
# the EXISTING after-the-fact audit is handed the log: a zero exit says "nothing
# failed", not "something ran", and that same script separately re-checks that
# the live-agent class stayed out when this round did not ask for it. It reads
# OC_E2E_LIVE_AGENT from the environment, which is why the export above is the
# one decision both phases see.
echo "[local-ci] === phase 2/2: e2e specs (isolated station on :8791) ==="
set +e
bash "$ROOT/e2e_test/run_all.sh" 2>&1 | tee "$LOGDIR/e2e.log"
e2e_rc="${PIPESTATUS[0]}"
set -e
if [[ "$e2e_rc" != "0" ]]; then
  echo "[local-ci] FAIL — e2e_test/run_all.sh exited $e2e_rc. Log: $LOGDIR/e2e.log" >&2
  exit "$e2e_rc"
fi
bash "$ROOT/e2e_test/assert-specs-ran.sh" "$LOGDIR/e2e.log"

echo "[local-ci] logs kept at: $LOGDIR"
echo "[local-ci] all green (bin/ci.sh + e2e specs; live-agent=$OC_E2E_LIVE_AGENT)"
