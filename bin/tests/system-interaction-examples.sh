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

python3 - "$ROOT/seeds/system_interaction.md" "$ROOT/spec/mcp-catalog.json" "$HELP" <<'PY'
import json
import re
import sys
from pathlib import Path

seed_path, catalog_path, help_path = map(Path, sys.argv[1:])


def fail(message):
    raise SystemExit(f"[system-interaction-test] FAIL — {message}")


def read_fenced_blocks(text):
    blocks = []
    current = None
    for line_number, line in enumerate(text.splitlines(), 1):
        if not line.startswith("```"):
            if current is not None:
                current[3].append(line)
            continue

        if current is None:
            language = line[3:].strip()
            if not language:
                fail(f"line {line_number}: fenced block has no language")
            current = [line_number, None, language, []]
        else:
            current[1] = line_number
            blocks.append(tuple(current))
            current = None

    if current is not None:
        fail(f"line {current[0]}: fenced block is not closed")
    return blocks


blocks = read_fenced_blocks(seed_path.read_text(encoding="utf-8"))
mcp_annotation = re.compile(
    r"^// OffiCraft MCP tool:\s*([a-z][a-z0-9_]*)(?:（[^）]+）)?$"
)
command_annotation = re.compile(
    r"^// OffiCraft command:\s*(ocagent)\s+([a-z][a-z0-9-]*)$"
)

catalog = json.loads(catalog_path.read_text(encoding="utf-8"))
catalog_tools = catalog.get("tools")
if not isinstance(catalog_tools, list) or not catalog_tools:
    fail("MCP catalog has no tools list")
catalog_names = {
    tool.get("name")
    for tool in catalog_tools
    if isinstance(tool, dict) and isinstance(tool.get("name"), str)
}

mcp_names = []
command_names = []
command_lines = []

for start, end, language, content in blocks:
    mcp_annotations = [
        line.strip()
        for line in content
        if line.strip().startswith("// OffiCraft MCP tool:")
    ]
    if mcp_annotations:
        if len(mcp_annotations) != 1:
            fail(f"lines {start}-{end}: MCP block has multiple tool annotations")
        match = mcp_annotation.fullmatch(mcp_annotations[0])
        if not match:
            fail(f"lines {start}-{end}: malformed MCP tool annotation")
        if language != "jsonc":
            fail(f"lines {start}-{end}: MCP tool block must use jsonc, got {language!r}")

        tool_name = match.group(1)
        json_lines = [line for line in content if not line.lstrip().startswith("//")]
        json_text = re.sub(r",\s*([}\]])", r"\1", "\n".join(json_lines))
        try:
            payload = json.loads(json_text)
        except json.JSONDecodeError as exc:
            fail(f"lines {start}-{end}: MCP JSONC example is invalid after comments are removed: {exc}")
        if not isinstance(payload, dict) or payload.get("method") != "tools/call":
            fail(f"lines {start}-{end}: MCP example must call tools/call")
        params = payload.get("params")
        if not isinstance(params, dict) or params.get("name") != tool_name:
            actual = params.get("name") if isinstance(params, dict) else None
            fail(f"lines {start}-{end}: annotation names {tool_name!r}, JSON names {actual!r}")
        if tool_name not in catalog_names:
            fail(f"lines {start}-{end}: documented MCP tool {tool_name!r} is absent from the current catalog")
        mcp_names.append(tool_name)

    command_annotations = [
        line.strip()
        for line in content
        if line.strip().startswith("// OffiCraft command:")
    ]
    if command_annotations:
        if len(command_annotations) != 1:
            fail(f"lines {start}-{end}: CLI block has multiple command annotations")
        match = command_annotation.fullmatch(command_annotations[0])
        if not match:
            fail(f"lines {start}-{end}: malformed CLI command annotation")
        if language != "text":
            fail(f"lines {start}-{end}: CLI command block must use text, got {language!r}")

        visible = [
            line.strip()
            for line in content
            if line.strip() and not line.lstrip().startswith("//")
        ]
        if len(visible) != 1:
            fail(f"lines {start}-{end}: CLI command block must contain exactly one command line")
        tokens = visible[0].split()
        program, subcommand = match.groups()
        if len(tokens) < 2 or tokens[0] != program or tokens[1] != subcommand:
            fail(f"lines {start}-{end}: annotation {program} {subcommand!r} disagrees with {visible[0]!r}")
        command_names.append(subcommand)

    for line in content:
        line = line.strip()
        if not line or line.startswith("//") or not line.startswith("ocagent "):
            continue
        tokens = line.split()
        if tokens[1] == "--help":
            if len(tokens) != 2:
                fail(f"lines {start}-{end}: malformed top-level help command {line!r}")
        elif tokens[1] == "<subcommand>":
            if tokens[2:] != ["--help"]:
                fail(f"lines {start}-{end}: malformed generic subcommand help {line!r}")
        elif not re.fullmatch(r"[a-z][a-z0-9-]*", tokens[1]):
            fail(f"lines {start}-{end}: malformed ocagent subcommand {line!r}")
        else:
            command_lines.append(tokens[1])

if not mcp_names:
    fail("system_interaction.md contains no tagged MCP tool examples")
if not command_names:
    fail("system_interaction.md contains no tagged ocagent command examples")

help_text = help_path.read_text(encoding="utf-8")
for subcommand in sorted(set(command_names + command_lines)):
    if not re.search(rf"(?m)^\s*{re.escape(subcommand)}\s+", help_text):
        fail(f"documented ocagent command {subcommand!r} is absent from ocagent --help")

print(
    "[system-interaction-test] all green — "
    f"{len(mcp_names)} MCP examples match the current catalog; "
    f"{len(set(command_names))} tagged CLI commands and {len(command_lines)} help examples match ocagent --help"
)
PY
