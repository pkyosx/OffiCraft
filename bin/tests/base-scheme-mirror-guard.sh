#!/usr/bin/env bash
# bin/tests/base-scheme-mirror-guard.sh — the http/https rule is hand-copied
# across THREE Go modules; this fails the build the moment they disagree (T-78).
#
# WHY THERE ARE THREE COPIES. server/ocserverd, cli/ocwarden and cli/ocagent are
# three separate Go modules with no module in common, so there is nowhere to put
# a shared package without adding one and rewiring three go.mod files. That is
# the right fix; it was deliberately not done under an incident clock. Until it
# is, the copies are kept honest here rather than by hoping.
#
# WHY DRIFT HERE CAN BE SILENT — MEASURED, not assumed (2026-09-04, three
# mutants, each planted and reverted with the file sha256 checked both ways):
#
#   D1  ocagent's copy: 127.0.0.1 → 127.0.0.2
#       guard FAIL ✅  — but ocagent's OWN test also failed. So this class of
#       drift is already covered without this guard; do not cite it as the
#       reason the guard exists.
#   D2  ocagent's copy: END marker deleted
#       guard FAIL ✅  — the marker check is load-bearing. Without it the awk
#       extraction returns empty, and two empty strings compare EQUAL: the guard
#       would pass while the block was gone.
#   D3  ocagent's copy: one word changed inside a COMMENT in the block
#       ocagent  go test → ok
#       ocserverd go test → ok
#       guard FAIL ✅  — THIS is the case that justifies the guard: every
#       module's own suite is green and the copies have still diverged.
#
# D3 is a comment today, but the same silence covers any behaviour these two
# suites do not happen to assert. The failure mode it protects against is a rule
# changed in two places out of three: every suite green, while the server hands
# out one answer and the agent believes another — and the disagreement first
# shows up as a machine that cannot call home, hours later, on someone else's
# computer.
#
# WHAT IS COMPARED: the text between the BEGIN and END markers, byte for byte.
# The package clause, imports and file header are deliberately NOT compared —
# they legitimately differ per module.
set -euo pipefail

BEGIN='// ── T-78 CANONICAL BLOCK — DO NOT EDIT ONE COPY'
END='// ── END T-78 CANONICAL BLOCK'

FILES=(
  server/ocserverd/base_scheme_t78.go
  cli/ocwarden/base_scheme_t78.go
  cli/ocagent/base_scheme_t78.go
)

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

for f in "${FILES[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "FAIL — $f is missing. The rule lives in three copies; a deleted copy is not a" >&2
    echo "       simplification, it is a module that silently kept the old behaviour." >&2
    exit 1
  fi
  # Extract the block. A missing marker is a failure, not an empty extraction:
  # an empty file would otherwise compare equal to another empty file.
  awk -v b="$BEGIN" -v e="$END" '
    index($0, b) { on = 1 }
    on { print }
    index($0, e) { on = 0 }
  ' "$f" > "$tmpdir/$(echo "$f" | tr / _)"
  if ! grep -qF -- "$BEGIN" "$f" || ! grep -qF -- "$END" "$f"; then
    echo "FAIL — $f has no T-78 canonical block markers. Someone removed or renamed them;" >&2
    echo "       without markers this guard would compare two empty strings and pass." >&2
    exit 1
  fi
  lines=$(wc -l < "$tmpdir/$(echo "$f" | tr / _)")
  if [[ "$lines" -lt 20 ]]; then
    echo "FAIL — the block extracted from $f is only $lines lines. That is too short to be" >&2
    echo "       the real rule; the markers are probably adjacent or nested wrongly." >&2
    exit 1
  fi
done

ref="$tmpdir/$(echo "${FILES[0]}" | tr / _)"
for f in "${FILES[@]:1}"; do
  cur="$tmpdir/$(echo "$f" | tr / _)"
  if ! diff -u "$ref" "$cur" > "$tmpdir/diff.txt"; then
    echo "FAIL — the T-78 rule has DRIFTED between ${FILES[0]} and $f." >&2
    echo "       Both modules' own tests still pass; that is exactly why this guard exists." >&2
    echo "       Copy the block verbatim into every file listed at the top of this script." >&2
    sed -n '1,60p' "$tmpdir/diff.txt" >&2
    fail=1
  fi
done

[[ "$fail" -eq 0 ]] || exit 1
echo "ok — T-78 canonical block identical across ${#FILES[@]} modules ($(wc -l < "$ref") lines each)"
