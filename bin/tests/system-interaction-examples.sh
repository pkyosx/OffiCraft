#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
GO_BIN="${OC_GO:-go}"
WORK="$(mktemp -d -t oc-system-interaction.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

HELP="$WORK/ocagent-help.txt"
set +e
(
  cd "$ROOT/cli/ocagent"
  "$GO_BIN" run . --help
) >"$HELP" 2>&1
HELP_RC=$?
set -e
if [[ "$HELP_RC" != 0 ]]; then
  echo "[system-interaction-test] FAIL — ocagent --help exited $HELP_RC"
  cat "$HELP"
  exit 1
fi

python3 - "$ROOT/seeds/system_interaction.md" "$HELP" <<'PY'
import re
import sys
from pathlib import Path

seed_path, help_path = map(Path, sys.argv[1:])


def fail(message):
    raise SystemExit(f"[system-interaction-test] FAIL — {message}")


seed_text = seed_path.read_text(encoding="utf-8")
subcommands = sorted(set(re.findall(r"\bocagent\s+([a-z][a-z0-9-]*)", seed_text)))
if not subcommands:
    fail("system_interaction.md names no ocagent subcommand, so the help check measured nothing")

help_text = help_path.read_text(encoding="utf-8")
for subcommand in subcommands:
    if not re.search(rf"(?m)^\s*{re.escape(subcommand)}\s+", help_text):
        fail(f"documented ocagent command {subcommand!r} is absent from ocagent --help")

print(
    "[system-interaction-test] all green — "
    f"{len(subcommands)} ocagent subcommands named in system_interaction.md match ocagent --help"
)
PY
