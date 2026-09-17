#!/usr/bin/env python3
"""T-236 — lore scope `everyone` and set_lore_entry_scope in spec/openapi.json.

Edits the spec as TEXT rather than reserialising it, for the reason
bin/t228_spec_step_ops.py gives: a json.dump round-trip would reformat the
whole hand-maintained file and bury the real changes.

What it does:
  1. POST /api/lore/{entry_id}/scope — MCP `set_lore_entry_scope`, x-mcp.order
     123 (APPENDED; the order is shared element-wise with the route table and
     conformance/routes_manifest.json).
  2. LoreEntryScopeDTO request schema and LoreEntryScopeReceiptDTO bounded
     receipt (owner's 2026-09-07 receipt ruling, as state/bump follow).
  3. LoreEntryDTO gains optional `scope_options` and `task_type_key`.
  4. `everyone` joins every description of the scope_kind vocabulary
     (LoreEntryDTO, the write DTO/receipt, the list op's scope_kinds /
     scope_kind filter docs), and write_lore_entry stops claiming staff lore
     files under a role.

A description that exists in several copies (operation description, summary,
x-mcp.description, and the JSON-in-a-string legacy descriptor) is replaced in
every copy with the escaping that copy uses; each descriptor line is checked to
re-encode byte-for-byte before it is touched.

Refuses to run twice, and re-parses its own result before reporting success.

🔴 THE WORDING BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. Once this
script has run, spec/openapi.json is authoritative and nothing compares the two
copies — do not reach for these strings when you want the current wording.
"""

import json
import sys

SPEC = "spec/openapi.json"
DESCRIPTOR_PREFIX = '            "descriptor": '

SCOPE_KIND_NOTE = (
    "The TARGET scope, one of ``agent``, ``manual``, ``everyone``. Anything else "
    "— the retired ``role`` included — is a 400 that names it. ``manual`` is also "
    "a 400 when the entry has no derivable task type; it is then absent from the "
    "entry's ``scope_options``. There is no ``scope_key`` field: the server "
    "derives the key itself (the author's member id / the task type_key / \"\")."
)

SCOPE_DESC = (
    "Move one 傳承 entry to a different scope — that is, change who receives it. "
    "ADMIN-ONLY: the owner or an admin agent; anyone else, the entry's own author "
    "included, is a 403. You send only the target ``scope_kind``, never a "
    "``scope_key`` — the server derives the key. ``agent`` keys the entry to its "
    "AUTHOR, so it rides that member's own boot document. ``manual`` keys it to "
    "the entry's TASK TYPE, so it rides ``get_task_manual``: the type_key of the "
    "entry's ``source_task_id`` when that task carries one; otherwise, when the "
    "author is an outsource member, the type_key of the task that member was "
    "bound to. A 臨時任務 with no type gives no type, and ``manual`` on an entry "
    "with no derivable type is a 400. ``everyone`` (所有人) keys it to \"\" and "
    "puts it in the boot document of EVERY member — staff, outsource and mira "
    "alike; this call is the only way an entry reaches ``everyone``, because "
    "write_lore_entry never files there. Any other ``scope_kind`` is a 400; an "
    "unknown entry is a 404. The entry's ``scope_options`` (list_lore_entries) "
    "names exactly the kinds this call accepts for it. Asking for the scope the "
    "entry already has is a 200 that changes nothing. A legacy ``role`` entry can "
    "be moved to any offered kind, never back to ``role``. Nothing else about the "
    "entry moves: ``state``, ``effective_ts``, title and body stay as they are. "
    "The author is NOT notified and NO change history is kept; the move takes "
    "effect the next time a member boots. Answers with a bounded receipt (``id``, "
    "``scope_kind``, ``scope_key``, ``state``, ``effective_ts``, ``updated_ts``), "
    "not the entry; call list_lore_entries for the rest of it."
    "\n\nPARAMETER NOTES. In the input schema the parameters below carry only a "
    "short summary; these are their full rules.\n- `scope_kind`: " + SCOPE_KIND_NOTE
)

SCOPE_KIND_SHORT = (
    "Target scope: ``agent``, ``manual`` or ``everyone``. The server derives the "
    "scope_key; see PARAMETER NOTES."
)


def fail(msg):
    print("[t236] FAIL — " + msg, file=sys.stderr)
    raise SystemExit(1)


def descriptor(name, desc, props, required):
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


def envelope(description):
    return {
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
            }
        },
        "description": description,
    }


SCOPE_OP = {
    "description": SCOPE_DESC,
    "operationId": "handle_set_lore_entry_scope_api_lore__entry_id__scope_post",
    "parameters": [
        {
            "in": "path",
            "name": "entry_id",
            "required": True,
            "schema": {"title": "Entry Id", "type": "string"},
        }
    ],
    "requestBody": {
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/LoreEntryScopeDTO"}
            }
        },
        "required": True,
    },
    "responses": {
        "200": {
            "content": {
                "application/json": {
                    "schema": {"$ref": "#/components/schemas/LoreEntryScopeReceiptDTO"}
                }
            },
            "description": "Successful Response",
        },
        "422": envelope("Validation error (unified error envelope)."),
        "4XX": envelope("Client error (unified error envelope)."),
        "5XX": envelope("Server error (unified error envelope)."),
    },
    "summary": SCOPE_DESC,
    "x-mcp": {
        "description": SCOPE_DESC,
        "include": True,
        "legacy": {
            "descriptor": descriptor(
                "set_lore_entry_scope",
                SCOPE_DESC,
                {
                    "entry_id": {"type": "string"},
                    "scope_kind": {
                        "description": SCOPE_KIND_SHORT,
                        "title": "Scope Kind",
                        "type": "string",
                    },
                },
                ["entry_id", "scope_kind"],
            )
        },
        "name": "set_lore_entry_scope",
        "order": 123,
    },
}

SCOPE_REQUEST = {
    "additionalProperties": False,
    "description": (
        "The set_lore_entry_scope request body (T-236): the TARGET scope kind and "
        "nothing else. The scope_key is derived by the server — the author's "
        "member id for ``agent``, the entry's derivable task type_key for "
        "``manual``, \"\" for ``everyone`` — so a caller cannot point an entry at "
        "another member's boot document or at an unrelated manual."
    ),
    "properties": {
        "scope_kind": {
            "description": SCOPE_KIND_NOTE,
            "title": "Scope Kind",
            "type": "string",
        }
    },
    "required": ["scope_kind"],
    "title": "LoreEntryScopeDTO",
    "type": "object",
}

TS = {"format": "double", "type": "number"}

SCOPE_RECEIPT = {
    "additionalProperties": False,
    "description": (
        "Bounded receipt for ``POST /api/lore/{entry_id}/scope`` "
        "(set_lore_entry_scope) (T-236), under the same owner ruling as "
        "``LoreEntryStateReceiptDTO`` (2026-09-07: 「不要回傳自己寫出去的 "
        "payload」): only the id and what the write itself decided ride home. "
        "``scope_key`` is the one value the caller could not send or predict — "
        "the server derived it. ``state``, ``effective_ts`` and ``updated_ts`` "
        "carry the same meaning they do on the state/bump receipt, so the three "
        "governance doors answer with one vocabulary; this call never changes "
        "``state`` or ``effective_ts``. ``title``, ``body``, ``author_id``, "
        "``source_task_id`` and the timestamps of creation are dropped; call "
        "``list_lore_entries`` (``GET /api/lore``) for the entry itself."
    ),
    "properties": {
        "id": {
            "description": "The entry that was moved, echoed from the path.",
            "title": "Id",
            "type": "string",
        },
        "scope_kind": {
            "description": (
                "``agent`` | ``manual`` | ``everyone`` — the scope the entry is in "
                "AFTER this write, read back from the stored row. Equal to the "
                "request when the call was a no-op."
            ),
            "title": "Scope Kind",
            "type": "string",
        },
        "scope_key": {
            "description": (
                "The key the SERVER derived: the author's member id for "
                "``agent``, the task type_key for ``manual``, \"\" for "
                "``everyone``."
            ),
            "title": "Scope Key",
            "type": "string",
        },
        "state": {
            "description": (
                "``active`` | ``pinned`` | ``retired`` — unchanged by this call, "
                "read back from the stored row."
            ),
            "title": "State",
            "type": "string",
        },
        "effective_ts": dict(TS, title="Effective Ts", description=(
            "The entry's ordering key, epoch seconds — untouched by a scope move."
        )),
        "updated_ts": dict(TS, title="Updated Ts", description=(
            "The stored row's last-write stamp, epoch seconds, as it stands after "
            "this call."
        )),
    },
    "required": ["effective_ts", "id", "scope_key", "scope_kind", "state", "updated_ts"],
    "title": "LoreEntryScopeReceiptDTO",
    "type": "object",
}

NEW_ENTRY_PROPS = {
    "scope_options": {
        "description": (
            "The scope kinds this entry may be switched to with "
            "``set_lore_entry_scope``, in display order: ``manual`` (only when "
            "``task_type_key`` is non-empty), ``agent``, ``everyone``. COMPUTED AT "
            "READ TIME from the entry's source task and its author's roster row, "
            "not stored. It includes the entry's current kind; switching to it is "
            "a no-op. additive-optional."
        ),
        "items": {"type": "string"},
        "title": "Scope Options",
        "type": "array",
    },
    "task_type_key": {
        "description": (
            "The task type a ``manual`` scope for this entry would key to, or \"\" "
            "when there is none. COMPUTED AT READ TIME: the type_key of "
            "``source_task_id`` when that task carries one; otherwise, when the "
            "author is an outsource member, the type_key of the task that member "
            "was bound to; a 臨時任務 with no type gives \"\". additive-optional."
        ),
        "title": "Task Type Key",
        "type": "string",
    },
}

# (old, new, expected occurrences across all copies). Plain strings; each copy
# gets the escaping it is stored with.
PROSE_EDITS = [
    (
        "your role if you are staff, yourself if you are an outsource member "
        "(who has no role for a role scope to name)",
        "an ``agent`` scope keyed to you, staff and outsource alike. A write never "
        "files to ``everyone`` (所有人): only an admin's set_lore_entry_scope moves "
        "an entry there",
        4,
    ),
    (
        "``role`` for staff, ``agent`` for an outsource member",
        "an ``agent`` scope keyed to the writer, staff and outsource alike (the "
        "staff-to-``role`` arm is retired; see ``LoreEntryDTO.scope_kind``)",
        1,
    ),
    (
        "Staff and outsource members take the same arm.",
        "Staff and outsource members take the same arm. Neither arm produces "
        "``everyone`` (所有人): a write never files there, and only "
        "``set_lore_entry_scope`` (admin) moves an existing entry to it.",
        1,
    ),
    (
        "A write can never produce the retired ``role`` value.",
        "A write can never produce the retired ``role`` value, nor ``everyone``, "
        "which only ``set_lore_entry_scope`` sets.",
        1,
    ),
    (
        "``agent`` or ``manual``, and the two are not interchangeable. An "
        "``agent`` entry rides ONE member's own boot document — staff and "
        "outsource alike; a ``manual`` entry rides ``get_task_manual``.\n\n"
        "Which one a write lands in",
        "``agent``, ``manual`` or ``everyone``, and they are not interchangeable. "
        "An ``agent`` entry rides ONE member's own boot document — staff and "
        "outsource alike; a ``manual`` entry rides ``get_task_manual``; an "
        "``everyone`` entry (所有人) rides the boot document of EVERY member — "
        "staff, outsource and mira alike.\n\n``everyone`` is never produced by a "
        "write. Only ``set_lore_entry_scope`` (admin) moves an existing entry into "
        "or out of it, and the move takes effect the next time a member boots.\n\n"
        "Which of ``agent`` / ``manual`` a write lands in",
        1,
    ),
    (
        "switches exhaustively on the two live values",
        "switches exhaustively on the three live values",
        1,
    ),
    (
        "the task manual's ``type_key`` when it is ``manual``. A surviving legacy",
        "the task manual's ``type_key`` when it is ``manual``; \"\" when it is "
        "``everyone``. A surviving legacy",
        1,
    ),
    (
        "Accepted values: ``agent``, ``manual``; ANY other element",
        "Accepted values: ``agent``, ``manual``, ``everyone`` (所有人 — its entries "
        "carry an empty scope_key, so filter it without a scope_key); ANY other "
        "element",
        5,
    ),
    (
        "REPEATABLE scope-kind set: ``agent``, ``manual``. Any other value",
        "REPEATABLE scope-kind set: ``agent``, ``manual``, ``everyone``. Any other "
        "value",
        1,
    ),
]

# Raw edits to the descriptor's inner JSON text (not to a string inside it).
DESCRIPTOR_RAW_EDITS = [
    (
        '"scope_kind": {\n              "type": "string"\n            },',
        '"scope_kind": {\n              "description": "Single-value twin of '
        '``scope_kinds``: ``agent``, ``manual`` or ``everyone``; anything else is '
        'a 400.",\n              "type": "string"\n            },',
        1,
    ),
]

SINGULAR_PARAM_OLD = (
    '          {\n            "in": "query",\n            "name": "scope_kind",\n'
)
SINGULAR_PARAM_NEW = (
    "          {\n            \"description\": "
    + json.dumps(
        "Single-value twin of ``scope_kinds``. Accepted values: ``agent``, "
        "``manual``, ``everyone``; any other value — the retired ``role`` "
        "included — is a 400 that names it. Ignored when ``scope_kinds`` is sent "
        "(PLURAL WINS).",
        ensure_ascii=False,
    )
    + ',\n            "in": "query",\n            "name": "scope_kind",\n'
)


def enc(s, ascii_only=False):
    return json.dumps(s, ensure_ascii=ascii_only)[1:-1]


def rewrite_descriptor_line(line, counts):
    raw_value = line[len(DESCRIPTOR_PREFIX):]
    inner = json.loads(raw_value)
    if json.dumps(inner, ensure_ascii=False) != raw_value:
        fail("descriptor line does not re-encode byte-for-byte")
    ascii_only = inner.isascii()
    new_inner = inner
    for i, (old, new, _) in enumerate(PROSE_EDITS):
        o = enc(old, ascii_only)
        counts[i] += new_inner.count(o)
        new_inner = new_inner.replace(o, enc(new, ascii_only))
    for i, (old, new, _) in enumerate(DESCRIPTOR_RAW_EDITS):
        counts[len(PROSE_EDITS) + i] += new_inner.count(old)
        new_inner = new_inner.replace(old, new)
    if new_inner == inner:
        return line
    return DESCRIPTOR_PREFIX + json.dumps(new_inner, ensure_ascii=False)


def rewrite_plain_line(line, counts):
    for i, (old, new, _) in enumerate(PROSE_EDITS):
        o = enc(old)
        counts[i] += line.count(o)
        line = line.replace(o, enc(new))
    return line


def block(key, value, indent):
    raw = json.dumps({key: value}, ensure_ascii=False, indent=2, sort_keys=True)
    lines = raw.split("\n")[1:-1]
    pad = " " * (indent - 2)
    return "\n".join(pad + line for line in lines) + ",\n"


def insert_before(text, anchor, payload, after=None):
    start = 0
    if after is not None:
        start = text.find(after)
        if start < 0 or text.count(after) != 1:
            fail("start anchor missing or not unique: " + after.strip())
    idx = text.find(anchor, start)
    if idx < 0:
        fail("anchor not found: " + anchor.strip())
    if after is None and text.count(anchor) != 1:
        fail("anchor is not unique: " + anchor.strip())
    return text[:idx] + payload + text[idx:]


def main():
    text = open(SPEC, encoding="utf-8").read()
    if '"set_lore_entry_scope"' in text:
        fail("spec already carries set_lore_entry_scope — this script runs once")

    counts = [0] * (len(PROSE_EDITS) + len(DESCRIPTOR_RAW_EDITS))
    out = []
    for line in text.split("\n"):
        if line.startswith(DESCRIPTOR_PREFIX):
            out.append(rewrite_descriptor_line(line, counts))
        else:
            out.append(rewrite_plain_line(line, counts))
    text = "\n".join(out)
    expected = [e[2] for e in PROSE_EDITS] + [e[2] for e in DESCRIPTOR_RAW_EDITS]
    if counts != expected:
        fail("replacement counts %r, expected %r" % (counts, expected))

    if text.count(SINGULAR_PARAM_OLD) != 1:
        fail("singular scope_kind query parameter is not unique")
    text = text.replace(SINGULAR_PARAM_OLD, SINGULAR_PARAM_NEW)

    text = insert_before(
        text,
        '          "title": {\n',
        block("scope_options", NEW_ENTRY_PROPS["scope_options"], 10)
        + block("task_type_key", NEW_ENTRY_PROPS["task_type_key"], 10),
        after='      "LoreEntryDTO": {\n',
    )
    text = insert_before(
        text,
        '      "LoreEntryListDTO": {\n',
        block("LoreEntryScopeDTO", SCOPE_REQUEST, 8)
        + block("LoreEntryScopeReceiptDTO", SCOPE_RECEIPT, 8),
    )
    text = insert_before(
        text,
        '    "/api/lore/{entry_id}/bump": {\n',
        block("/api/lore/{entry_id}/scope", {"post": SCOPE_OP}, 6),
    )

    open(SPEC, "w", encoding="utf-8").write(text)
    verify()


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    orders = sorted(
        op["x-mcp"]["order"]
        for ops in spec["paths"].values()
        for op in ops.values()
        if (op.get("x-mcp") or {}).get("include")
    )
    if orders != list(range(len(orders))):
        fail("order sequence is not 0..%d after the append" % (len(orders) - 1))
    for path, method in (
        ("/api/lore", "get"),
        ("/api/lore", "post"),
        ("/api/lore/{entry_id}/scope", "post"),
    ):
        op = spec["paths"][path][method]
        d = json.loads(op["x-mcp"]["legacy"]["descriptor"])
        if not (op["description"] == op["summary"] == op["x-mcp"]["description"] == d["description"]):
            fail("description copies disagree for %s %s" % (method, path))
        if d["name"] != op["x-mcp"]["name"]:
            fail("descriptor name disagrees for %s %s" % (method, path))
    entry = spec["components"]["schemas"]["LoreEntryDTO"]
    if {"scope_options", "task_type_key"} & set(entry["required"]):
        fail("new LoreEntryDTO fields must stay optional")
    print("[t236] ok — %d MCP tools; set_lore_entry_scope at %d"
          % (len(orders),
             spec["paths"]["/api/lore/{entry_id}/scope"]["post"]["x-mcp"]["order"]))


if __name__ == "__main__":
    main()
