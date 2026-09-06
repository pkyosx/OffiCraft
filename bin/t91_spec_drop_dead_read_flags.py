#!/usr/bin/env python3
"""T-91 follow-through (owner 2026-09-06, rc-9fa492c6b7b5 [0]) — drop the six
read-face flags that can no longer have a value.

WHAT AND WHY. The lifecycle-receipt reshape moved `activation_pending`,
`relocation_pending` and `relocation_deferred` off the two READ structures and
onto the three receipts that actually compute them (MemberActivateReceiptDTO,
AgentRelocateReceiptDTO, OutsourceRestartReceiptDTO). Since that landed, no
handler anywhere writes those three names onto a MemberDTO or an
OutsourceWorkerDTO, so on those two schemas the fields are permanently absent —
and their own descriptions still said "set ONLY on the activate/relocate
response", a sentence about a response that no longer exists. A field that can
never be present, documented by a sentence that is no longer true, is worse than
no field: a client that reads the doc writes a branch that never runs.

WHAT IS DELIBERATELY NOT TOUCHED, because it is the whole point of the change:
the same three names on MemberActivateReceiptDTO, AgentRelocateReceiptDTO and
OutsourceRestartReceiptDTO, and every route summary / x-mcp description that
names them. Those are where the signals live now. This script asserts they are
still there afterwards rather than trusting itself.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style (compact leaf objects, deliberate
non-alphabetical grouping, and — inside OutsourceWorkerDTO — two different
indentation runs); a json.dump round-trip would reformat all 21k lines and bury
the real change. Precedent: bin/t91_spec_lifecycle_receipts.py.

Refuses to run twice, and re-parses + re-checks its own result.
"""

import json
import re
import sys

SPEC = "spec/openapi.json"

DEAD = ("activation_pending", "relocation_pending", "relocation_deferred")

# schema name -> the properties to delete from it
TARGETS = {
    "MemberDTO": DEAD,
    "OutsourceWorkerDTO": DEAD,
}

# schema name -> the properties that MUST survive (the receipts)
KEEP = {
    "MemberActivateReceiptDTO": ("id", "activation_pending", "last_op_reason"),
    "AgentRelocateReceiptDTO": ("id", "relocation_pending", "relocation_deferred"),
    "OutsourceRestartReceiptDTO": ("id", "activation_pending", "last_op_reason"),
}


def schema_span(lines: list[str], name: str) -> tuple[int, int]:
    """[start, end) line indices of the top-level component schema `name`."""
    head = f'      "{name}": {{\n'
    try:
        start = lines.index(head)
    except ValueError:
        sys.exit(f"schema {name!r} not found (looked for exactly {head!r})")
    for i in range(start + 1, len(lines)):
        if lines[i] in ('      },\n', '      }\n'):
            return start, i + 1
    sys.exit(f"schema {name!r} has no closing brace at its own indent")


def drop_property(lines: list[str], span: tuple[int, int], prop: str) -> list[str]:
    """Delete the `prop` entry from the properties object inside `span`.

    Brace-counts from the property's own key line, so a nested anyOf/description
    cannot end the block early. Indentation is read off the key line rather than
    assumed, because OutsourceWorkerDTO's three dead entries sit at a different
    indent from their neighbours.
    """
    start, end = span
    key = re.compile(r'^(\s*)"' + re.escape(prop) + r'": \{$')
    hits = [i for i in range(start, end) if key.match(lines[i].rstrip("\n"))]
    if len(hits) != 1:
        sys.exit(f"expected exactly one {prop!r} key line in span, got {len(hits)}")
    i = hits[0]
    depth = 0
    j = i
    while j < end:
        depth += lines[j].count("{") - lines[j].count("}")
        if depth == 0:
            break
        j += 1
    else:
        sys.exit(f"unbalanced braces while deleting {prop!r}")
    # j is the closing line; it ends with "}," (never last — every schema here
    # keeps properties after the dead ones) or "}".
    tail = lines[j].rstrip("\n").rstrip()
    if not tail.endswith("},"):
        sys.exit(f"{prop!r} is the LAST property in its object; "
                 "removing it would leave a trailing comma — handle by hand")
    return lines[:i] + lines[j + 1:]


def props_of(spec: dict, name: str) -> set[str]:
    return set(spec["components"]["schemas"][name].get("properties", {}))


def main() -> None:
    text = open(SPEC, encoding="utf-8").read()
    before = json.loads(text)

    already = [n for n, ps in TARGETS.items()
               if not (props_of(before, n) & set(ps))]
    if already:
        sys.exit(f"already applied — no dead flags left on {already}")

    lines = text.splitlines(keepends=True)
    removed = 0
    for name, props in TARGETS.items():
        for prop in props:
            span = schema_span(lines, name)
            n0 = len(lines)
            lines = drop_property(lines, span, prop)
            removed += n0 - len(lines)
            print(f"  - {name}.{prop}: {n0 - len(lines)} lines")

    out = "".join(lines)
    after = json.loads(out)  # fail before writing if we broke the JSON

    for name, props in TARGETS.items():
        left = props_of(after, name) & set(props)
        assert not left, f"{name} still carries {left}"
    for name, must in KEEP.items():
        have = props_of(after, name)
        assert have == set(must), f"{name} properties changed: {have} != {set(must)}"

    # Nothing outside the two target schemas may have moved.
    b = json.dumps(before, sort_keys=True)
    a = json.dumps(after, sort_keys=True)
    for name in TARGETS:
        before["components"]["schemas"][name]["properties"] = \
            after["components"]["schemas"][name]["properties"]
    assert json.dumps(before, sort_keys=True) == a, \
        "the edit changed something outside MemberDTO/OutsourceWorkerDTO properties"
    del b

    open(SPEC, "w", encoding="utf-8").write(out)
    print(f"ok — {removed} lines removed from {SPEC}")


if __name__ == "__main__":
    main()
