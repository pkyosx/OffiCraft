#!/usr/bin/env python3
"""T-79 — 換版交代單: the owner's standing instructions to the assistant.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style; a json.dump round-trip would
reformat all 21k lines and bury the real changes. Precedent and shape:
bin/t646a_spec_update_task.py.

What it adds:
  1. Three DTOs — UpgradeInstructionDTO, UpgradeInstructionsDTO,
     UpgradeInstructionCreateDTO.
  2. Three path items / four operations, all four on the MCP surface:
     list_upgrade_instructions, create_upgrade_instruction,
     complete_upgrade_instruction, delete_upgrade_instruction.
  3. x-mcp.order values appended after the existing tools, keeping the
     0..N-1 consecutive range the generator enforces.

Refuses to run twice. Re-parses its own result and re-checks the order
sequence, so a silently corrupted spec cannot pass as success.

🔴 THE DESCRIPTION TEXT BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH.
spec/openapi.json is authoritative; this file holds a verbatim copy of the
wording as applied on 2026-09-06, and NO drift guard compares the two. Read the
spec for the current wording; do not "sync" these strings back.
"""

import json
import re
import sys

SPEC = "spec/openapi.json"


def fail(msg):
    print(f"[t79-spec] FAIL — {msg}", file=sys.stderr)
    sys.exit(1)


DTO_DESC = (
    "One 換版交代單 — an instruction the owner leaves for the assistant, handed "
    "to her at every station upgrade until somebody ticks it off.\n\n"
    "It is a ROW, not a message, and `done` is the whole difference: a message "
    "is consumed by whoever happens to read it and nothing afterwards records "
    "that the work behind it ever happened. An instruction that is still open "
    "is handed over again at the NEXT upgrade, and the one after that — which "
    "is exactly why nobody has to reason about WHICH upgrade will carry it, and "
    "why the owner may write one at any moment rather than just before a "
    "release.\n\n"
    "`done_by` and `done_ts` are zero-valued while the instruction is open. "
    "They are kept apart from `done` because \"it was ticked\" and \"who ticked "
    "it, when\" are different facts, and only the second one survives a "
    "disagreement about whether the work happened."
)

LIST_DTO_DESC = (
    "Response of `GET /api/upgrade-instructions`: every 換版交代單 this station "
    "holds, open ones first and each group oldest→newest (the hand-over order, "
    "so they read in the order they were asked for).\n\n"
    "`open_count` is NOT a convenience over the length of `instructions` — it "
    "counts the open ones only, and it is the number the cockpit shows. This "
    "design has exactly one failure mode, an instruction nobody ever acts on, "
    "and without a visible open count that failure is completely silent. That "
    "is why the count is part of the contract instead of something each client "
    "derives for itself."
)

CREATE_DTO_DESC = (
    "Create one 換版交代單. `body` is the whole of it — what the owner wants the "
    "assistant to do — and a blank one is refused: an instruction with no text "
    "would be handed over at every upgrade forever while telling its reader "
    "nothing.\n\n"
    "There is deliberately NO field saying WHEN it should be delivered. Every "
    "open instruction goes out at every upgrade, so one written now and one "
    "written seconds before a release behave identically. An earlier design "
    "bound each instruction to the commit it was written for; that was dropped "
    "once instructions became durable rows, because an instruction handed to an "
    "unrelated upgrade is not lost — it is still open, and it goes out again."
)

LIST_TOOL_DESC = (
    "List the 換版交代單 — the standing instructions the owner has left for the "
    "assistant, which the station hands over in a chat message every time it "
    "upgrades. Open ones come first, each group oldest→newest, and `open_count` "
    "counts the open ones only. admin_agent floor: the owner and the assistant; "
    "an ordinary agent gets 403. Read this to see what is still waiting — a "
    "finished instruction stays in the list, because it is the only evidence "
    "that the work was ever picked up."
)

CREATE_TOOL_DESC = (
    "Write one 換版交代單 — an instruction for the assistant that the station "
    "hands over at its next upgrade, and at every upgrade after that, until "
    "somebody ticks it off. OWNER ONLY, and that floor is the point rather than "
    "caution: the assistant authoring her own instructions would make the "
    "record meaningless. `body` is the whole instruction and a blank one is a "
    "422. There is no delivery-time field — write it whenever you like, the "
    "answer does not depend on when you typed it. ⚠️ Nothing here schedules "
    "anything: an instruction nobody ticks is handed over again indefinitely, "
    "so withdraw a mistake with delete_upgrade_instruction rather than leaving "
    "it open."
)

DONE_TOOL_DESC = (
    "Tick one 換版交代單 off — record that the instruction has been carried out, "
    "so the station stops handing it over at every upgrade. The owner or the "
    "assistant may tick; an ordinary agent gets 403. THE FIRST TICK WINS: a "
    "second call answers 200 with the instruction unchanged and does NOT "
    "overwrite who did the work or when, which is what makes two sessions of "
    "the assistant racing on the same instruction safe. 404 if the instruction "
    "id names nothing. ⚠️ This verb means \"I did this\". To retract something "
    "that should never have been written, the owner uses "
    "delete_upgrade_instruction instead — ticking it would certify work that "
    "never happened."
)

DELETE_TOOL_DESC = (
    "Withdraw one 換版交代單 — permanent, not undoable, OWNER ONLY. This is the "
    "author retracting something he should not have written, and it exists "
    "because ticking is the assistant's verb for \"I did this\": without a "
    "withdraw path, an instruction written in error would be handed over at "
    "every single upgrade forever and the only way to stop it would be to ask "
    "the assistant to certify work that never happened. 404 if the instruction "
    "id names nothing. Answers with the row that was removed."
)

INSTRUCTION_ID_PARAM_DESC = (
    "Which instruction to act on. Take it from list_upgrade_instructions — an "
    "id that names nothing is a 404 rather than a silent no-op, so a typo "
    "announces itself here."
)


def indented(obj, pad):
    text = json.dumps(obj, ensure_ascii=False, indent=2)
    return "\n".join(
        ((pad + line) if i else line) for i, line in enumerate(text.split("\n"))
    )


def responses(ref):
    err = {
        "content": {
            "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}
            }
        },
    }
    return {
        "200": {
            "content": {
                "application/json": {"schema": {"$ref": ref}}
            },
            "description": "Successful Response",
        },
        "422": dict(err, description="Validation error (unified error envelope)."),
        "4XX": dict(err, description="Client error (unified error envelope)."),
        "5XX": dict(err, description="Server error (unified error envelope)."),
    }


ID_PARAM = {
    "in": "path",
    "name": "instruction_id",
    "required": True,
    "schema": {"title": "Instruction Id", "type": "string"},
}


def legacy_descriptor(name, desc, props, required):
    raw = json.dumps(
        {
            "description": desc,
            "inputSchema": {
                "properties": props,
                "required": required,
                "additionalProperties": False,
                "type": "object",
            },
            "name": name,
        },
        ensure_ascii=False,
        indent=2,
    )
    return "\n".join(
        (("      " + line) if i else line) for i, line in enumerate(raw.split("\n"))
    )


def operation(op_id, desc, name, order, ref, params=None, body_ref=None,
              legacy_props=None, legacy_required=None):
    op = {"description": desc, "operationId": op_id}
    if params:
        op["parameters"] = params
    if body_ref:
        op["requestBody"] = {
            "content": {"application/json": {"schema": {"$ref": body_ref}}},
            "required": True,
        }
    op["responses"] = responses(ref)
    op["summary"] = desc
    op["x-mcp"] = {
        "include": True,
        "order": order,
        "name": name,
        "description": desc,
        "legacy": {
            "descriptor": legacy_descriptor(
                name, desc, legacy_props or {}, legacy_required or []
            )
        },
    }
    return indented(op, "      ")


def build_paths(base_order):
    dto = "#/components/schemas/UpgradeInstructionDTO"
    lst = "#/components/schemas/UpgradeInstructionsDTO"
    body_desc = {
        "description": "The instruction itself — what the assistant is being "
        "asked to do. Blank is a 422.",
        "type": "string",
    }
    id_prop = {"description": INSTRUCTION_ID_PARAM_DESC, "type": "string"}

    collection = {
        "get": None,
        "post": None,
    }
    get_op = operation(
        "handle_list_upgrade_instructions_api_upgrade_instructions_get",
        LIST_TOOL_DESC, "list_upgrade_instructions", base_order, lst,
        legacy_props={}, legacy_required=[],
    )
    post_op = operation(
        "handle_create_upgrade_instruction_api_upgrade_instructions_post",
        CREATE_TOOL_DESC, "create_upgrade_instruction", base_order + 1, dto,
        body_ref="#/components/schemas/UpgradeInstructionCreateDTO",
        legacy_props={"body": body_desc}, legacy_required=["body"],
    )
    done_op = operation(
        "handle_complete_upgrade_instruction_api_upgrade_instructions__instruction_id__done_post",
        DONE_TOOL_DESC, "complete_upgrade_instruction", base_order + 2, dto,
        params=[ID_PARAM],
        legacy_props={"instruction_id": id_prop}, legacy_required=["instruction_id"],
    )
    del_op = operation(
        "handle_delete_upgrade_instruction_api_upgrade_instructions__instruction_id__delete",
        DELETE_TOOL_DESC, "delete_upgrade_instruction", base_order + 3, dto,
        params=[ID_PARAM],
        legacy_props={"instruction_id": id_prop}, legacy_required=["instruction_id"],
    )
    del collection
    return (
        '    "/api/upgrade-instructions": {\n'
        '      "get": ' + get_op + ',\n'
        '      "post": ' + post_op + '\n'
        '    },\n'
        '    "/api/upgrade-instructions/{instruction_id}": {\n'
        '      "delete": ' + del_op + '\n'
        '    },\n'
        '    "/api/upgrade-instructions/{instruction_id}/done": {\n'
        '      "post": ' + done_op + '\n'
        '    },\n'
    )


def build_schemas():
    item = {
        "description": DTO_DESC,
        "properties": {
            "body": {"title": "Body", "type": "string"},
            "created_by": {"title": "Created By", "type": "string"},
            "created_ts": {"title": "Created Ts", "type": "number"},
            "done": {"default": False, "title": "Done", "type": "boolean"},
            "done_by": {"title": "Done By", "type": "string"},
            "done_ts": {"default": 0, "title": "Done Ts", "type": "number"},
            "id": {"title": "Id", "type": "string"},
        },
        "required": [
            "id", "body", "created_ts", "created_by", "done", "done_ts", "done_by",
        ],
        "title": "UpgradeInstructionDTO",
        "additionalProperties": False,
        "type": "object",
    }
    listing = {
        "description": LIST_DTO_DESC,
        "properties": {
            "instructions": {
                "items": {"$ref": "#/components/schemas/UpgradeInstructionDTO"},
                "title": "Instructions",
                "type": "array",
            },
            "open_count": {"default": 0, "title": "Open Count", "type": "integer"},
        },
        "required": ["instructions", "open_count"],
        "title": "UpgradeInstructionsDTO",
        "additionalProperties": False,
        "type": "object",
    }
    create = {
        "description": CREATE_DTO_DESC,
        "properties": {"body": {"title": "Body", "type": "string"}},
        "required": ["body"],
        "title": "UpgradeInstructionCreateDTO",
        "additionalProperties": False,
        "type": "object",
    }
    return (
        '      "UpgradeInstructionCreateDTO": ' + indented(create, "      ") + ',\n'
        '      "UpgradeInstructionDTO": ' + indented(item, "      ") + ',\n'
        '      "UpgradeInstructionsDTO": ' + indented(listing, "      ") + ',\n'
    )


def main():
    text = open(SPEC, encoding="utf-8").read()
    if "UpgradeInstructionDTO" in text or "/api/upgrade-instructions" in text:
        fail("spec already carries the T-79 change — refusing to apply twice")

    spec = json.loads(text)
    orders = [
        op["x-mcp"]["order"]
        for ops in spec["paths"].values()
        for op in ops.values()
        if (op.get("x-mcp") or {}).get("include")
    ]
    base = max(orders) + 1
    if sorted(orders) != list(range(len(orders))):
        fail("existing x-mcp.order values are not the consecutive 0..N-1 range")

    schema_anchor = '\n      "VersionDTO": {'
    if text.count(schema_anchor) != 1:
        fail("could not find the VersionDTO schema anchor")
    at = text.index(schema_anchor)
    text = text[:at] + "\n" + build_schemas().rstrip("\n").rstrip(",") + "," + text[at:]

    path_anchor = '\n    "/api/version": {'
    if text.count(path_anchor) != 1:
        fail("could not find the /api/version path anchor")
    at = text.index(path_anchor)
    text = text[:at] + "\n" + build_paths(base).rstrip("\n").rstrip(",") + "," + text[at:]

    open(SPEC, "w", encoding="utf-8").write(text)

    after = json.loads(open(SPEC, encoding="utf-8").read())
    got = sorted(
        op["x-mcp"]["order"]
        for ops in after["paths"].values()
        for op in ops.values()
        if (op.get("x-mcp") or {}).get("include")
    )
    if got != list(range(len(got))):
        fail("x-mcp.order is no longer the consecutive 0..N-1 range after the edit")
    for need in (
        "UpgradeInstructionDTO",
        "UpgradeInstructionsDTO",
        "UpgradeInstructionCreateDTO",
    ):
        if need not in after["components"]["schemas"]:
            fail(f"schema {need} missing after the edit")
    for need in (
        "/api/upgrade-instructions",
        "/api/upgrade-instructions/{instruction_id}",
        "/api/upgrade-instructions/{instruction_id}/done",
    ):
        if need not in after["paths"]:
            fail(f"path {need} missing after the edit")
    print(f"[t79-spec] OK — 3 schemas, 3 path items, 4 tools at order {base}..{base + 3}")


if __name__ == "__main__":
    main()
