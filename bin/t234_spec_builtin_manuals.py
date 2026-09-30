#!/usr/bin/env python3
"""T-234 — built-in task manuals: seed flags on the manual DTOs, a reset route,
and the delete refusal for a built-in manual, in spec/openapi.json.

Edits the spec as TEXT rather than reserialising it; a json.dump round-trip
would reformat the whole file and bury the real changes. Precedent:
bin/t228_spec_step_ops.py, bin/t92_spec_artifacts.py.

What it changes:
  1. TaskManualDTO / TaskManualListItemDTO   + optional is_seed, is_default
  2. TaskManualReceiptDTO                    + optional is_default, is_seed
  3. POST /api/task-manuals/{type_key}/reset — MCP `reset_task_manual`,
     modeled on POST /api/roles/{role}/reset, x-mcp.order APPENDED.
  4. DELETE /api/task-manuals/{type_key}     + "built-in cannot be deleted" bullet
  5. list / get task manual                  + "built-ins appear unwritten" bullet

Refuses to run twice, and re-parses its own result before reporting success.

🔴 THE WORDING BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. Once this
script has run, spec/openapi.json is authoritative and nothing compares the two
copies — do not reach for these strings when you want the current wording.
"""

import json
import sys

SPEC = "spec/openapi.json"

IS_SEED_PROP = {
    "default": False,
    "description": (
        "Whether this task type ships built-in with OffiCraft rather than being "
        "created on this station. Only a built-in manual can be reset "
        "(reset_task_manual), and a built-in manual cannot be deleted."
    ),
    "title": "Is Seed",
    "type": "boolean",
}

IS_DEFAULT_PROP = {
    "default": False,
    "description": (
        "True when the station is serving the shipped factory version of a "
        "built-in manual: no owner or agent edit overlays it. Any edit clears it "
        "(content or assignee); reset_task_manual sets it again. Always false "
        "for a manual created on this station."
    ),
    "title": "Is Default",
    "type": "boolean",
}

RECEIPT_IS_DEFAULT_PROP = {
    "default": False,
    "description": (
        "True when no edit overlays the shipped version after this write. "
        "reset_task_manual makes it true by dropping the edit; create and update "
        "leave it false. The flag tracks whether an edit EXISTS, not whether the "
        "text differs: writing text identical to the shipped version still "
        "clears it."
    ),
    "title": "Is Default",
    "type": "boolean",
}

RECEIPT_IS_SEED_PROP = {
    "default": False,
    "description": (
        "Whether this task type ships built-in. A registry fact the write does "
        "not decide; only a built-in manual has anything to reset to."
    ),
    "title": "Is Seed",
    "type": "boolean",
}

RESET_SUMMARY = (
    "Reset a built-in task manual to its shipped version (idempotent): drops any "
    "edit, content AND assignee alike. Only a built-in (seed) manual can be "
    "reset; anything else is 404. Owner or admin agent only. Answers with a "
    "bounded receipt (``type_key``, ``updated_ts``, ``is_default``, ``is_seed``, "
    "``sop_md_chars``, ``sop_md_cap_chars``, ``sop_md_sha256``), not the manual "
    "— call ``get_task_manual`` when you need the rest."
)

RESET_BULLETS = (
    "- Drops any edit (content AND assignee) and returns the manual to its shipped version; idempotent.\n"
    "- Only a built-in (seed) manual can be reset; anything else is a 404.\n"
    "- Owner or admin agent only; a plain agent is 403."
)

DELETE_BULLET = "\n- A built-in (seed) manual cannot be deleted: 403; reset it instead."

LIST_BULLET = (
    "\n- Built-in manuals are listed even when nothing was ever written for them; "
    "`is_seed` / `is_default` say which rows are built-in and unedited."
)

GET_BULLET = (
    "\n- A built-in manual is served even when nothing was ever written for it; "
    "`is_seed` / `is_default` say whether it is built-in and unedited."
)

LIST_DESC_OLD = (
    "- Fetch one manual's text with get_task_manual."
)
GET_DESC_OLD = (
    "- One manual in full: purpose, fields, SOP and assignee.\\n"
    "- The SOP is judged against `sop_md_cap_chars`.\\n"
    "- Unknown type: 404."
)
DELETE_DESC_OLD = (
    "- Refused with 409 while any NON-terminal task of this type exists; closed "
    "tasks never block.\\n- Owner or admin agent only; a plain agent is 403."
)
RECEIPT_DESC_OLD = (
    "Bounded receipt returned after create_task_manual and update_task_manual (T-91)."
)
RECEIPT_DESC_NEW = (
    "Bounded receipt returned after create_task_manual, update_task_manual and "
    "reset_task_manual (T-91)."
)


def error_envelope(desc):
    return {
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
            }
        },
        "description": desc,
    }


def descriptor(name, desc, props, required):
    """The frozen MCP descriptor fragment, rendered exactly as the catalog
    carries it: indent 2, base indent 4, keys in the catalog's own order."""
    obj = {
        "description": desc,
        "inputSchema": {
            "properties": props,
            "required": required,
            "additionalProperties": False,
            "type": "object",
        },
        "name": name,
    }
    raw = json.dumps(obj, ensure_ascii=False, indent=2)
    return "\n".join(
        line if i == 0 else "    " + line for i, line in enumerate(raw.split("\n"))
    )


def reset_op(order):
    return {
        "description": RESET_BULLETS,
        "operationId": "handle_reset_task_manual_api_task_manuals__type_key__reset_post",
        "parameters": [
            {
                "in": "path",
                "name": "type_key",
                "required": True,
                "schema": {"title": "Type Key", "type": "string"},
            }
        ],
        "responses": {
            "200": {
                "content": {
                    "application/json": {
                        "schema": {"$ref": "#/components/schemas/TaskManualReceiptDTO"}
                    }
                },
                "description": "Successful Response",
            },
            "422": error_envelope("Validation error (unified error envelope)."),
            "4XX": error_envelope("Client error (unified error envelope)."),
            "5XX": error_envelope("Server error (unified error envelope)."),
        },
        "summary": RESET_SUMMARY,
        "x-mcp": {
            "description": RESET_SUMMARY,
            "include": True,
            "legacy": {
                "descriptor": descriptor(
                    "reset_task_manual",
                    RESET_SUMMARY,
                    {"type_key": {"type": "string"}},
                    ["type_key"],
                )
            },
            "name": "reset_task_manual",
            "order": order,
        },
    }


def fail(msg):
    print("[t234] FAIL — " + msg, file=sys.stderr)
    raise SystemExit(1)


def block(key, value, indent):
    """Render `"key": <value>,` as text whose key line sits at `indent` spaces."""
    raw = json.dumps({key: value}, ensure_ascii=False, indent=2, sort_keys=True)
    lines = raw.split("\n")[1:-1]
    pad = " " * (indent - 2)
    return "\n".join(pad + line for line in lines) + ",\n"


def replace_once(text, old, new, start=0, end=None):
    end = len(text) if end is None else end
    region = text[start:end]
    if region.count(old) != 1:
        fail("expected exactly one match for: %r (got %d)" % (old[:80], region.count(old)))
    idx = start + region.index(old)
    return text[:idx] + new + text[idx + len(old):]


def schema_span(text, name):
    start = text.find('      "%s": {\n' % name)
    end = text.find('        "title": "%s",\n' % name, start)
    if start < 0 or end < 0:
        fail("schema block not found: " + name)
    return start, end


def path_span(text, path):
    start = text.find('    "%s": {\n' % path)
    if start < 0:
        fail("path block not found: " + path)
    end = text.find('\n    },\n', start)
    return start, end


def insert_props(text, schema, before_prop, props):
    start, end = schema_span(text, schema)
    anchor = '          "%s": {\n' % before_prop
    payload = "".join(block(k, v, 10) for k, v in props)
    return replace_once(text, anchor, payload + anchor, start, end)


def current_max_order(text):
    spec = json.loads(text)
    return max(
        op["x-mcp"]["order"]
        for ops in spec["paths"].values()
        for op in ops.values()
        if isinstance(op, dict) and (op.get("x-mcp") or {}).get("include")
    )


def main():
    text = open(SPEC, encoding="utf-8").read()
    if '"reset_task_manual"' in text:
        fail("spec already carries reset_task_manual — this script runs once")
    order = current_max_order(text) + 1

    seed_props = [("is_default", IS_DEFAULT_PROP), ("is_seed", IS_SEED_PROP)]
    text = insert_props(text, "TaskManualDTO", "purpose", seed_props)
    text = insert_props(text, "TaskManualListItemDTO", "purpose", seed_props)
    text = insert_props(
        text,
        "TaskManualReceiptDTO",
        "sop_md_cap_chars",
        [("is_default", RECEIPT_IS_DEFAULT_PROP), ("is_seed", RECEIPT_IS_SEED_PROP)],
    )
    start, end = schema_span(text, "TaskManualReceiptDTO")
    text = replace_once(text, RECEIPT_DESC_OLD, RECEIPT_DESC_NEW, start, end)

    start, end = path_span(text, "/api/task-manuals")
    text = replace_once(
        text, LIST_DESC_OLD + '",', LIST_DESC_OLD + json.dumps(LIST_BULLET)[1:-1] + '",',
        start, end,
    )
    start, end = path_span(text, "/api/task-manuals/{type_key}")
    text = replace_once(
        text, GET_DESC_OLD + '",', GET_DESC_OLD + json.dumps(GET_BULLET)[1:-1] + '",',
        start, end,
    )
    start, end = path_span(text, "/api/task-manuals/{type_key}")
    text = replace_once(
        text, DELETE_DESC_OLD + '",',
        DELETE_DESC_OLD + json.dumps(DELETE_BULLET, ensure_ascii=False)[1:-1] + '",',
        start, end,
    )

    anchor = '    "/api/task-manuals/{type_key}/sop/patch": {\n'
    text = replace_once(
        text, anchor,
        block("/api/task-manuals/{type_key}/reset", {"post": reset_op(order)}, 4) + anchor,
    )

    open(SPEC, "w", encoding="utf-8").write(text)
    verify(order)


def verify(order):
    spec = json.load(open(SPEC, encoding="utf-8"))
    orders = sorted(
        op["x-mcp"]["order"]
        for ops in spec["paths"].values()
        for op in ops.values()
        if isinstance(op, dict) and (op.get("x-mcp") or {}).get("include")
    )
    if orders != list(range(len(orders))):
        fail("order sequence is not 0..%d after the append" % (len(orders) - 1))
    schemas = spec["components"]["schemas"]
    for name in ("TaskManualDTO", "TaskManualListItemDTO", "TaskManualReceiptDTO"):
        props = schemas[name]["properties"]
        for field in ("is_default", "is_seed"):
            if field not in props or field in schemas[name]["required"]:
                fail("%s.%s missing or required" % (name, field))
    op = spec["paths"]["/api/task-manuals/{type_key}/reset"]["post"]
    mcp = op["x-mcp"]
    d = json.loads(mcp["legacy"]["descriptor"])
    if not (d["name"] == mcp["name"] == "reset_task_manual"):
        fail("reset_task_manual name disagrees")
    if not (d["description"] == mcp["description"] == op["summary"]):
        fail("reset_task_manual description disagrees")
    for path, method, needle in (
        ("/api/task-manuals/{type_key}", "delete", "cannot be deleted"),
        ("/api/task-manuals/{type_key}", "get", "even when nothing"),
        ("/api/task-manuals", "get", "even when nothing"),
    ):
        if needle not in spec["paths"][path][method]["description"]:
            fail("bullet missing on %s %s" % (method.upper(), path))
    print("[t234] ok — %d MCP tools; reset_task_manual at order %d" % (len(orders), order))


if __name__ == "__main__":
    main()
