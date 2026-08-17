#!/usr/bin/env bash
# Positive controls for the MCP catalog generator and its byte-drift contract.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
GEN="$ROOT/bin/gen-mcp-catalog"
WORK="$(mktemp -d -t oc-mcp-catalog.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SPEC="$ROOT/spec/openapi.json"
CATALOG="$ROOT/spec/mcp-catalog.json"
BASELINE="$WORK/catalog.regen.json"

if ! "$GEN" "$BASELINE" --spec "$SPEC" >"$WORK/baseline.log" 2>&1; then
  echo "[mcp-catalog-test] FAIL — clean OpenAPI metadata did not generate"
  cat "$WORK/baseline.log"
  exit 1
fi
if ! cmp -s "$CATALOG" "$BASELINE"; then
  echo "[mcp-catalog-test] FAIL — clean generator output differs from committed catalog"
  diff -u "$CATALOG" "$BASELINE" | sed -n '1,80p' || true
  exit 1
fi

# The byte-diff gate itself is NOT in this file: this suite proves the GENERATOR
# is honest, and `make drift-mcp-catalog` proves the COMMITTED CATALOG has not
# drifted from it. Two different questions, deliberately two checks — so this
# block asserts the second one still EXISTS and is still REACHED.
#
# ⚠️ MOUNT POINTS, and why they are these two files. This block used to name
# bin/ci.sh and bin/ci-cloud.sh, because that is where every check was spelled
# out (twice) when this suite was written. T-4d88 collapsed all of it: each check
# is now ONE named Makefile target, and every caller — local and cloud — names
# targets instead of restating them. bin/ci-cloud.sh no longer exists, so the
# original loop could only ever be red. The intent is unchanged: pull the gate
# out and this suite must notice. Today that means (1) the target still defines
# the regenerate-and-diff, and (2) a cloud cell still asks for it by name — a
# target nobody calls is exactly as absent as a deleted one.
MAKEFILE="$ROOT/Makefile"
if ! grep -qE '^drift-mcp-catalog:' "$MAKEFILE" \
  || ! grep -qF 'bin/gen-mcp-catalog "$$fresh"' "$MAKEFILE" \
  || ! grep -qF 'diff -u spec/mcp-catalog.json "$$fresh"' "$MAKEFILE" \
  || ! grep -qF 'FAIL — gen-mcp-catalog drift' "$MAKEFILE"; then
  echo "[mcp-catalog-test] FAIL — Makefile is missing the drift-mcp-catalog byte-diff gate"
  exit 1
fi
if ! grep -qE '^[[:space:]]+drift-mcp-catalog[[:space:]]*(\\)?$' "$MAKEFILE"; then
  echo "[mcp-catalog-test] FAIL — drift-mcp-catalog is not on the .PHONY continuation list"
  exit 1
fi

WORKFLOW="$ROOT/.github/workflows/ci.yml"
if ! grep -qE '^[[:space:]]*- run: bash bin/run-checks\.sh .*[[:space:]]drift-mcp-catalog([[:space:]]|$)' "$WORKFLOW"; then
  echo "[mcp-catalog-test] FAIL — no cloud cell runs drift-mcp-catalog (.github/workflows/ci.yml)"
  exit 1
fi

MUTANT_SPEC="$WORK/openapi-mutant.json"
cp "$SPEC" "$MUTANT_SPEC"
python3 - "$MUTANT_SPEC" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
needle = '          "name": "get_version",\n'
if text.count(needle) != 1:
    raise SystemExit(f"expected one authoritative x-mcp.name line, found {text.count(needle)}")
path.write_text(text.replace(needle, '          "name": "get_version_mutant",\n'), encoding="utf-8")
PY

if "$GEN" "$WORK/mutant-output.json" --spec "$MUTANT_SPEC" >"$WORK/mutant.log" 2>&1; then
  echo "[mcp-catalog-test] FAIL — generator accepted a mutated x-mcp.name"
  exit 1
fi
if ! grep -q 'x-mcp.name disagrees' "$WORK/mutant.log"; then
  echo "[mcp-catalog-test] FAIL — x-mcp.name mutant failed for an unexpected reason"
  cat "$WORK/mutant.log"
  exit 1
fi

AUTHORITY_MUTANT="$WORK/openapi-authority-mutant.json"
cp "$SPEC" "$AUTHORITY_MUTANT"
python3 - "$AUTHORITY_MUTANT" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
needle = "Read the build identity this station is RUNNING:"
if text.count(needle) != 3:
    raise SystemExit(f"expected summary, x-mcp description and legacy descriptor, found {text.count(needle)}")
text = text.replace(needle, "MUTANT Read the build identity this station is RUNNING:")
path.write_text(text, encoding="utf-8")
PY

AUTHORITY_OUTPUT="$WORK/authority-mutant.json"
if ! "$GEN" "$AUTHORITY_OUTPUT" --spec "$AUTHORITY_MUTANT" >"$WORK/authority.log" 2>&1; then
  echo "[mcp-catalog-test] FAIL — valid legacy-bootstrap input did not generate"
  cat "$WORK/authority.log"
  exit 1
fi
if cmp -s "$CATALOG" "$AUTHORITY_OUTPUT"; then
  echo "[mcp-catalog-test] FAIL — legacy-bootstrap mutation did not reach generated output"
  exit 1
fi
if ! diff -u "$CATALOG" "$AUTHORITY_OUTPUT" >"$WORK/authority.diff"; then
  :
fi
if ! grep -q 'MUTANT Read the build identity this station is RUNNING:' "$WORK/authority.diff"; then
  echo "[mcp-catalog-test] FAIL — legacy-bootstrap diff did not name the mutated descriptor"
  cat "$WORK/authority.diff"
  exit 1
fi

MUTANT_CATALOG="$WORK/catalog-mutant.json"
cp "$CATALOG" "$MUTANT_CATALOG"
python3 - "$MUTANT_CATALOG" <<'PY'
from pathlib import Path
import sys

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
needle = "Read the build identity this station is RUNNING:"
if text.count(needle) != 1:
    raise SystemExit(f"expected one catalog description to mutate, found {text.count(needle)}")
path.write_text(text.replace(needle, "MUTANT Read the build identity this station is RUNNING:"), encoding="utf-8")
PY

MUTANT_GATE_OUTPUT="$WORK/catalog-gate-output.json"
if ! "$GEN" "$MUTANT_GATE_OUTPUT" --spec "$SPEC" >"$WORK/catalog-gate.log" 2>&1; then
  echo "[mcp-catalog-test] FAIL — clean source could not regenerate for catalog gate mutant"
  cat "$WORK/catalog-gate.log"
  exit 1
fi
if cmp -s "$MUTANT_CATALOG" "$MUTANT_GATE_OUTPUT"; then
  echo "[mcp-catalog-test] FAIL — committed catalog gate mutant was not detected"
  exit 1
fi
if ! diff -u "$MUTANT_CATALOG" "$MUTANT_GATE_OUTPUT" >"$WORK/drift.diff"; then
  :
fi
if ! grep -q 'MUTANT Read the build identity this station is RUNNING:' "$WORK/drift.diff"; then
  echo "[mcp-catalog-test] FAIL — catalog gate diff did not name the mutated descriptor"
  cat "$WORK/drift.diff"
  exit 1
fi

RESTORED="$WORK/catalog.restored.json"
"$GEN" "$RESTORED" --spec "$SPEC" >/dev/null
if ! cmp -s "$CATALOG" "$RESTORED"; then
  echo "[mcp-catalog-test] FAIL — clean generation did not restore byte-exact output"
  exit 1
fi

echo "[mcp-catalog-test] all green — clean exactness, metadata validation, bootstrap mutation, and catalog gate mutant"
