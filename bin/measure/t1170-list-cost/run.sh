#!/usr/bin/env bash
# Both arms + the comparison, in one command.
#
#   run.sh BEFORE_WORKTREE AFTER_WORKTREE [OUTDIR]
#
# The comparison is the point: compare.py REFUSES (non-zero) when the two arms
# did not fill the same corpus against the same caps, which is the only thing
# that makes a delta between them mean anything. Running the arms without it
# produces two numbers and no evidence.
set -uo pipefail

BEFORE_WT="${1:-}"; AFTER_WT="${2:-}"; OUTDIR="${3:-}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "$BEFORE_WT" || -z "$AFTER_WT" ]]; then
  echo "usage: run.sh BEFORE_WORKTREE AFTER_WORKTREE [OUTDIR]" >&2; exit 64
fi
[[ -n "$OUTDIR" ]] || OUTDIR="$(mktemp -d -t oc-t1170-out.XXXXXX)"
mkdir -p "$OUTDIR"

bash "$HERE/arm.sh" "$BEFORE_WT" "$OUTDIR/before.json" || { echo "[run] before arm failed" >&2; exit 1; }
bash "$HERE/arm.sh" "$AFTER_WT"  "$OUTDIR/after.json"  || { echo "[run] after arm failed"  >&2; exit 1; }

echo
python3 "$HERE/compare.py" "$OUTDIR/before.json" "$OUTDIR/after.json"
rc=$?
echo
echo "[run] arm outputs: $OUTDIR/before.json $OUTDIR/after.json"
exit $rc
