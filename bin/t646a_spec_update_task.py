#!/usr/bin/env python3
"""T-646a — fold update_task_title + update_task_description into one update_task.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style (compact leaf objects, deliberate
non-alphabetical grouping); a json.dump round-trip would reformat all 17k lines
and bury the four real changes.

The four changes:
  1. POST /api/tasks/{task_id} — the new operation, MCP tool `update_task`.
  2. TaskFieldsDTO — its request body, next to the two DTOs it replaces.
  3. update_task_title / update_task_description — taken OFF the MCP surface
     (x-mcp becomes exactly {"include": false}, which is what gen-mcp-catalog
     requires of an excluded operation) while their HTTP routes stay, so the
     frontend and every existing HTTP client are untouched.
  4. x-mcp.order renumbered back into the consecutive 0..N-1 range the
     generator enforces.

Refuses to run twice. Verifies its own result by re-parsing and re-checking the
order sequence, so a silently corrupted spec cannot pass as success.

Kept in the tree so the diff it produced is reproducible and reviewable, not as a
build step. Precedent: bin/t6cce_add_param_desc.py is the same shape from an
earlier ticket.

🔴 THE DESCRIPTION TEXT BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH.
spec/openapi.json is authoritative; this file holds a third verbatim copy of the
wording as applied on 2026-08-16, and NO drift guard compares the two — so the
moment anyone edits that description in the spec, this copy is stale and nothing
says so. Acceptable for a script that has already run and refuses to run again,
but do not reach for these strings when you want the current wording, and do not
"sync" them: read the spec. Flagged by the independent review of T-646a.
"""

import json
import re
import sys

SPEC = "spec/openapi.json"

TOOL_DESC = (
    "Correct THIS task's own TEXT — its title, its description, or both in one write "
    "(T-646a). Replaces `update_task_title` and `update_task_description`, which "
    "documented the same rules twice and could not be applied together: changing both "
    "meant two calls, two transactions and two SSE deltas, with room for someone else's "
    "write to land in between. WHO: the task's own executor, or an admin/owner; anyone "
    "else is a flat 403. Creating a task grants NO standing to keep rewriting it — if "
    "you handed the task over, it is the new executor's text now. PARTIAL: only the "
    "fields you NAME are touched, so omitting a field is a legal no-op for it that "
    "versions nothing and fans nothing. ⚠️ THE TWO FIELDS TREAT AN EXPLICIT "
    "BLANK DIFFERENTLY, and that is an owner ruling rather than an inconsistency "
    "(card rc-796541192519, 2026-08-11, option ①): a blank `title` (\"\" or "
    "whitespace-only) is REFUSED with 400 and does NOT clear the field, because "
    "create_task refuses a blank title too and an edit door looser than the create door "
    "would let a caller reach a task-list row with nothing in it; a blank `description` "
    "IS accepted and DOES clear the text, because plenty of cards legitimately have no "
    "prose. VALIDATION IS WHOLE-BODY AND HAPPENS FIRST: a request carrying a blank title "
    "alongside a perfectly good description writes NEITHER — a 400 leaves the task "
    "exactly as it was, never half-applied. Both values are trimmed of surrounding "
    "whitespace before they are stored AND before they are compared with what is there, "
    "so re-sending the same text with a stray trailing space is correctly seen as no "
    "change rather than spending one of the retained revisions saying nothing moved. The "
    "write is wholesale within each field: send the full corrected text, not a fragment. "
    "⚠️ Division of labour with update_step_note: the DESCRIPTION says what "
    "this task IS (stable); the step NOTE says where a step is RIGHT NOW (volatile, "
    "handover-facing) — do not put progress here. A CLOSED task (completed / terminated "
    "/ duplicated) is STILL editable, on the same terms — unlike its artifact set, which "
    "freezes at close: artifacts record what the task PRODUCED and must stop moving, "
    "while a ticket worded wrongly is usually found to be wrong after it closed, and "
    "freezing the text would preserve a known falsehood in the permanent record. Every "
    "change that actually alters a field retains the previous value as a document "
    "version — kind `task_title` / `task_description`, key = the task id — so a "
    "correction is recoverable through list_document_history and the older wording is "
    "never simply gone."
)

TITLE_PARAM = (
    "The new title, replacing the whole of it. OMITTING the field is a no-op for the "
    "title: nothing is written, nothing is versioned, nothing fans, and the call still "
    "answers 200 with the task — so the status code alone never tells you whether a "
    "rename happened; read the title back if you need to know. A blank or "
    "whitespace-only title is REFUSED (400, 'title must not be blank') and takes the "
    "WHOLE request down with it, `description` included. ⚠️ Reaching that "
    "guard requires the blank to actually arrive: at least one MCP client serializes an "
    "empty string as an ABSENT field, which lands in the no-op branch instead (observed "
    "four times over, 2026-08-11) — so a caller trying to clear a title may get silence "
    "rather than the 400. Surrounding whitespace is trimmed before the value is stored "
    "and before it is compared with the current title. Contrast `description` on this "
    "same tool, where a blank is a real write that clears the text."
)

DESC_PARAM = (
    "The new description, replacing the whole of it. OMITTING the field is a no-op for "
    "the description: nothing is written, nothing is versioned, nothing fans. An EMPTY "
    "STRING is NOT refused — it is a genuine write that CLEARS the description, which is "
    "the opposite of what `title` does with the same input and is deliberate (a card "
    "with no prose is a true state; a card with no title is a blank row on the list). "
    "Surrounding whitespace is trimmed before the value is stored and before it is "
    "compared with the current description, so re-sending the same text with a stray "
    "trailing newline is correctly seen as no change. A description that is ONLY "
    "whitespace therefore trims to \"\" and CLEARS."
)

DTO_DESC = (
    "Partial update of one task's own text (MCP ``update_task``, T-646a). Both fields "
    "are nullable-with-no-default rather than defaulted, and that shape is the whole "
    "point: it keeps ABSENT and PRESENT-BUT-EMPTY distinguishable, so a body that never "
    "mentions the description cannot silently erase it. Unknown keys are refused "
    "(``additionalProperties: false``), so a caller reaching for ``name``, ``summary``, "
    "``text`` or ``desc`` is told rather than ignored. The two fields part company on an "
    "explicit blank — ``title`` refuses it with 400, ``description`` accepts it and "
    "clears — and that asymmetry is an owner ruling (card rc-796541192519, option "
    "①), not a leftover from the two DTOs this replaces. The body is validated as a "
    "WHOLE before anything is written: a blank title next to a valid description writes "
    "neither."
)


def fail(msg):
    print(f"[t646a] FAIL: {msg}", file=sys.stderr)
    sys.exit(1)


def build_operation():
    legacy = {
        "description": TOOL_DESC,
        "inputSchema": {
            "properties": {
                "task_id": {"type": "string"},
                "title": {
                    "anyOf": [{"type": "string"}, {"type": "null"}],
                    "default": None,
                    "title": "Title",
                    "description": TITLE_PARAM,
                },
                "description": {
                    "anyOf": [{"type": "string"}, {"type": "null"}],
                    "default": None,
                    "title": "Description",
                    "description": DESC_PARAM,
                },
            },
            "required": ["task_id"],
            "additionalProperties": False,
            "type": "object",
        },
        "name": "update_task",
    }
    raw = json.dumps(legacy, ensure_ascii=False, indent=2)
    raw = "\n".join(
        (("      " + line) if i else line) for i, line in enumerate(raw.split("\n"))
    )
    op = {
        "description": TOOL_DESC,
        "operationId": "handle_update_task_api_tasks__task_id__post",
        "parameters": [
            {
                "in": "path",
                "name": "task_id",
                "required": True,
                "schema": {"title": "Task Id", "type": "string"},
            }
        ],
        "requestBody": {
            "content": {
                "application/json": {
                    "schema": {"$ref": "#/components/schemas/TaskFieldsDTO"}
                }
            },
            "required": True,
        },
        "responses": {
            "200": {
                "content": {
                    "application/json": {
                        "schema": {"$ref": "#/components/schemas/TaskDTO"}
                    }
                },
                "description": "Successful Response",
            },
            "422": {
                "content": {
                    "application/json": {
                        "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
                    }
                },
                "description": "Validation error (unified error envelope).",
            },
            "4XX": {
                "content": {
                    "application/json": {
                        "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
                    }
                },
                "description": "Client error (unified error envelope).",
            },
            "5XX": {
                "content": {
                    "application/json": {
                        "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
                    }
                },
                "description": "Server error (unified error envelope).",
            },
        },
        "summary": TOOL_DESC,
        "x-mcp": {
            "include": True,
            "order": 0,
            "name": "update_task",
            "description": TOOL_DESC,
            "legacy": {"descriptor": raw},
        },
    }
    text = json.dumps(op, ensure_ascii=False, indent=2)
    return "\n".join(
        (("      " + line) if i else line) for i, line in enumerate(text.split("\n"))
    )


def build_schema():
    dto = {
        "description": DTO_DESC,
        "properties": {
            "description": {
                "anyOf": [{"type": "string"}, {"type": "null"}],
                "title": "Description",
            },
            "title": {
                "anyOf": [{"type": "string"}, {"type": "null"}],
                "title": "Title",
            },
        },
        "title": "TaskFieldsDTO",
        "additionalProperties": False,
        "type": "object",
    }
    text = json.dumps(dto, ensure_ascii=False, indent=2)
    return "\n".join(
        (("      " + line) if i else line) for i, line in enumerate(text.split("\n"))
    )


def main():
    text = open(SPEC, encoding="utf-8").read()

    if "update_task_api_tasks__task_id__post" in text or "TaskFieldsDTO" in text:
        fail("spec already carries the T-646a change — refusing to apply twice")

    # ── 3. retire the two predecessors from the MCP surface ────────────────
    for name in ("update_task_title", "update_task_description"):
        pat = re.compile(
            r'        "x-mcp": \{\n'
            r'          "include": true,\n'
            r'          "order": \d+,\n'
            r'          "name": "' + name + r'",\n'
            r'.*?\n        \}',
            re.S,
        )
        text, n = pat.subn('        "x-mcp": {\n          "include": false\n        }', text)
        if n != 1:
            fail(f"expected exactly one x-mcp block for {name}, replaced {n}")

    # ── 1. the new operation, appended to the existing GET-only path item ──
    anchor = '    "/api/tasks/{task_id}": {\n      "get": {'
    if text.count(anchor) != 1:
        fail("could not find the GET-only /api/tasks/{task_id} path item")
    end = find_object_end(text, text.index(anchor) + len(anchor) - 1)
    text = text[:end] + ',\n      "post": ' + build_operation() + text[end:]

    # ── 2. the DTO, next to the two it replaces ───────────────────────────
    marker = '\n      "TaskLearningsPatchDTO": {'
    if text.count(marker) != 1:
        fail("could not find the TaskLearningsPatchDTO anchor")
    at = text.index(marker)
    text = text[:at] + '\n      "TaskFieldsDTO": ' + build_schema() + ',' + text[at:]

    # ── 4. renumber ───────────────────────────────────────────────────────
    text = renumber(text)

    open(SPEC, "w", encoding="utf-8").write(text)
    verify()


def find_object_end(text, start):
    """Index just past the closing brace of the JSON object starting at `start`."""
    if text[start] != "{":
        fail("find_object_end did not start on an object")
    depth, i, in_str, esc = 0, start, False, False
    while i < len(text):
        c = text[i]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
        elif c == '"':
            in_str = True
        elif c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return i + 1
        i += 1
    fail("unterminated object")


def renumber(text):
    """Rewrite every x-mcp.order so the included tools form 0..N-1 again.

    The new tool keeps the slot its predecessors held: it is placed immediately
    after submit_plan, which is where update_task_description used to sit, so the
    catalogue's element-wise order still mirrors routes.go.
    """
    spec = json.loads(text)
    rows = []
    for ops in spec["paths"].values():
        for op in ops.values():
            x = op.get("x-mcp") or {}
            if x.get("include"):
                rows.append((x["order"], x["name"]))
    others = sorted((o, n) for o, n in rows if n != "update_task")
    seq = []
    for _, name in others:
        seq.append(name)
        if name == "submit_plan":
            seq.append("update_task")
    if "update_task" not in seq:
        fail("submit_plan anchor missing — cannot place update_task")
    target = {name: i for i, name in enumerate(seq)}

    def one(match):
        name = match.group("name")
        if name not in target:
            fail(f"unplaced tool {name}")
        return f'"order": {target[name]},\n          "name": "{name}"'

    out, n = re.subn(
        r'"order": \d+,\n          "name": "(?P<name>[a-z_-]+)"', one, text
    )
    if n != len(target):
        fail(f"renumbered {n} tools, expected {len(target)}")
    return out


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    orders = sorted(
        (op["x-mcp"]["order"])
        for ops in spec["paths"].values()
        for op in ops.values()
        if (op.get("x-mcp") or {}).get("include")
    )
    if orders != list(range(len(orders))):
        fail(f"order sequence is not 0..{len(orders)-1} after renumber")
    for ops in spec["paths"].values():
        for op in ops.values():
            x = op.get("x-mcp")
            if x is not None and not x.get("include") and set(x) != {"include"}:
                fail("an excluded operation kept metadata beyond include:false")
    op = spec["paths"]["/api/tasks/{task_id}"]["post"]
    d = json.loads(op["x-mcp"]["legacy"]["descriptor"])
    if d["description"] != op["x-mcp"]["description"] or d["name"] != "update_task":
        fail("legacy descriptor disagrees with x-mcp")
    print(f"[t646a] ok — {len(orders)} MCP tools, update_task at order {op['x-mcp']['order']}")


if __name__ == "__main__":
    main()
