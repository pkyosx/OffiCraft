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
            current = [line_number, None, line[3:].strip(), []]
        else:
            current[1] = line_number
            blocks.append(tuple(current))
            current = None

    if current is not None:
        fail(f"line {current[0]}: fenced block is not closed")
    return blocks


seed_text = seed_path.read_text(encoding="utf-8")
blocks = read_fenced_blocks(seed_text)
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
catalog_by_name = {
    tool["name"]: tool
    for tool in catalog_tools
    if isinstance(tool, dict) and isinstance(tool.get("name"), str)
}
catalog_names = set(catalog_by_name)

mcp_names = []
# 🔴 Counts how many examples were actually confronted with a NON-EMPTY required
# list. Without it the argument check below is satisfiable by a catalog where
# nothing is required, and would report green while comparing nothing.
schema_checked = 0
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

        # ── arguments vs the tool's inputSchema ───────────────────────────────
        # 🔴 ADDED AFTER THIS GUARD SHIPPED A BROKEN EXAMPLE. T-18 made
        # create_reply_card's linked_task required; the example here was not
        # updated, and this file stayed GREEN through it — it only ever checked
        # that the JSONC parses, that the annotation matches params.name, and
        # that the tool exists. Nothing compared the ARGUMENTS against the
        # schema, so an example that 400s on every call read as fine.
        #
        # That is worse than a red CI lane: this document is what every member
        # reads at boot (get_system_interaction), so a stale example is not a
        # test failure, it is agents in the field copying a call that cannot
        # succeed. The catalog was already loaded three lines up; comparing is
        # cheap, and it closes BOTH directions — a newly required parameter the
        # example lacks, and a retired parameter the example still passes.
        schema = catalog_by_name[tool_name].get("inputSchema") or {}
        required = schema.get("required") or []
        properties = schema.get("properties") or {}
        arguments = params.get("arguments")
        if not isinstance(arguments, dict):
            fail(f"lines {start}-{end}: {tool_name!r} example has no arguments object")
        missing = [key for key in required if key not in arguments]
        if missing:
            fail(f"lines {start}-{end}: {tool_name!r} example omits required "
                 f"argument(s) {missing} — an agent copying it gets a 400. "
                 f"Update the example (and the prose around it) to match the tool.")
        unknown = [key for key in arguments if properties and key not in properties]
        if unknown:
            fail(f"lines {start}-{end}: {tool_name!r} example passes argument(s) "
                 f"{unknown} the tool does not accept — retired or misspelled. "
                 f"An agent copying it gets a 422.")
        if required:
            schema_checked += 1
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

if mcp_names and not schema_checked:
    fail("no documented MCP example was compared against a non-empty required "
         "list, so the argument/schema check above measured nothing — either the "
         "catalog stopped declaring required parameters or the examples stopped "
         "using tools that have any")

# The seed may also name tools inline (`post_chat`, `report_waking()`) instead
# of in tagged examples. Every backticked snake_case name must be a catalog tool
# unless it is listed here as a field or argument name.
NON_TOOL_IDENTIFIERS = {"linked_task", "task_id"}
inline_tools = set()
for name in re.findall(r"`([a-z][a-z0-9]*(?:_[a-z0-9]+)+)(?:\(\))?`", seed_text):
    if name in catalog_names:
        inline_tools.add(name)
    elif name not in NON_TOOL_IDENTIFIERS:
        fail(f"`{name}` reads as an MCP tool but is absent from the current catalog "
             f"— retired or misspelled. If it is a field name, add it to NON_TOOL_IDENTIFIERS.")
for subcommand in re.findall(r"\bocagent\s+([a-z][a-z0-9-]*)", seed_text):
    command_lines.append(subcommand)

if not mcp_names and not inline_tools:
    fail("system_interaction.md names no MCP tool at all, so the catalog check measured nothing")
if not command_names and not command_lines:
    fail("system_interaction.md names no ocagent subcommand, so the help check measured nothing")

help_text = help_path.read_text(encoding="utf-8")
for subcommand in sorted(set(command_names + command_lines)):
    if not re.search(rf"(?m)^\s*{re.escape(subcommand)}\s+", help_text):
        fail(f"documented ocagent command {subcommand!r} is absent from ocagent --help")

print(
    "[system-interaction-test] all green — "
    f"{len(mcp_names)} MCP examples match the current catalog "
    f"({schema_checked} confronted with required arguments); "
    f"{len(inline_tools)} inline MCP tool names are in the catalog; "
    f"{len(set(command_names))} tagged CLI commands and {len(command_lines)} help examples match ocagent --help"
)
PY
