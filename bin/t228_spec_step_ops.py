#!/usr/bin/env python3
"""T-228 — add insert_step / delete_step / reorder_steps to spec/openapi.json.

Edits the spec as TEXT rather than reserialising it. The file is hand-maintained
and its layout is house style (a few blocks deliberately out of alphabetical
order from earlier appends); a json.dump round-trip would reformat 21k lines and
bury the real changes. Precedent: bin/t646a_spec_update_task.py.

What it adds:
  1. POST /api/tasks/{task_id}/steps                       — MCP `insert_step`
  2. POST /api/tasks/{task_id}/steps/{step_id}/delete      — MCP `delete_step`
  3. POST /api/tasks/{task_id}/steps/reorder               — MCP `reorder_steps`
  4. Four request/receipt schemas for the three operations.

The three tools take x-mcp.order 120/121/122 — APPENDED, because the order is
shared element-wise with the route table and conformance/routes_manifest.json,
and inserting in the middle renumbers every tool after it.

Refuses to run twice, and re-parses its own result before reporting success.

🔴 THE WORDING BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. Once this
script has run, spec/openapi.json is authoritative and nothing compares the two
copies — do not reach for these strings when you want the current wording.
"""

import json
import sys

SPEC = "spec/openapi.json"

NO_OVERWRITE_GUARD = (
    "⚠️ THERE IS NO OVERWRITE PROTECTION, and that is an owner ruling "
    "(rc-5160b97384c4, 2026-09-16): this call carries no version, compares "
    "nothing and retries nothing. When two writes to the same plan land at the "
    "same moment THE LATER WRITE WINS and the earlier one is simply gone — no "
    "error, no signal, and nothing to read afterwards that says it happened."
)

INSERT_DESC = (
    "Insert ONE step into this task's plan, in front of a step you name. WHY IT "
    "EXISTS: submit_plan is a WHOLESALE replace — it permanently deletes every "
    "unfinished step together with its working note — so adding one step "
    "mid-task used to mean destroying the steps already under way, the notes on "
    "them, and the gate step still waiting for the owner's answer. This call "
    "moves nothing else: every other step keeps its id, its status, its note and "
    "its bound reply card, so a step sitting in waiting_owner goes on waiting and "
    "the owner's answer still lands on it. WHERE IT LANDS: `before_step_id` names "
    "the step the new one goes IN FRONT OF; omit it (or send \"\") to append at "
    "the END of the timeline. An id that names no step on this task is a 404, and "
    "one that names a FINISHED step (done / superseded) is a 409 — finished work "
    "is history and nothing is inserted into it. `name` and `dod` must both be "
    "non-blank (400), the same quality gate a submitted plan carries. The "
    "resulting timeline must still satisfy the parallel-group shape rules (400): "
    "steps sharing a parallel_group sit consecutively and number at least two, "
    "and a gate step carries no parallel_group. WHO: the task's own executor, or "
    "an admin/owner — anyone else is a flat 403; a CLOSED task is a 409. "
    + NO_OVERWRITE_GUARD
    + " Answers with a bounded receipt (`task_id`, `step_id` — the new step's id "
    "— `steps_total`, `progress_done`, `progress_total`), not the plan; call "
    "get_task to read the step rows back."
)

DELETE_DESC = (
    "Delete ONE unfinished step from this task's plan, leaving every other step "
    "exactly as it is — id, status, working note and bound reply card all "
    "untouched. WHY IT EXISTS: the only way to drop a step used to be "
    "submit_plan, which replaces the whole plan and permanently deletes every "
    "unfinished step's note along the way. WHAT IT REFUSES: a step id that names "
    "no step on this task is a 404; a FINISHED step (done / superseded) is a 409, "
    "because terminal rows are immutable history; deleting the last remaining "
    "step is a 400, because a planned task cannot have zero steps. WHO: the "
    "task's own executor, or an admin/owner — anyone else is a flat 403; a CLOSED "
    "task is a 409. Removing the last UNFINISHED step leaves every remaining step "
    "done, which FINISHES the work — the task lands in ready_for_done, one "
    "mark_task_done away from a close that can never be undone. ⚠️ A step CAN be "
    "deleted while "
    "it is holding a reply card the owner has not answered — waiting_owner is not "
    "a finished state, and this call does not look at the card. The card stays in "
    "the owner's queue with no step behind it and the later answer lands as a safe "
    "no-op, which is exactly what submit_plan has always done to a replaced "
    "waiting-card step; this door just makes it cheaper to reach one step at a "
    "time. If the question no longer matters, expire the card yourself rather than "
    "leaving it sitting there. "
    + NO_OVERWRITE_GUARD
    + " Answers with a bounded receipt (`task_id`, `steps_total`, "
    "`progress_done`, `progress_total`), not the plan; call get_task to read the "
    "step rows back."
)

REORDER_DESC = (
    "Reorder this task's UNFINISHED steps. Nothing is rebuilt: every step keeps "
    "its id, its status, its working note and its bound reply card — only the "
    "positions change. That is the whole difference from submit_plan, where "
    "re-listing a step under the same name mints a NEW step with a new id and the "
    "old note is gone. `step_ids` is the COMPLETE ordered list of this task's "
    "unfinished step ids: all of them, in the order you want them, and nothing "
    "else. FINISHED steps (done / superseded) do not appear in it and do not "
    "move — they keep the timeline positions they already hold, and the "
    "unfinished steps fill the positions that are left, in the order given. A "
    "`step_ids` that is not exactly the set of this task's unfinished steps is a "
    "400 — one missing, one repeated, one that names no step on this task, or one "
    "that names a finished step, each refuses the whole call and nothing is "
    "written. The resulting timeline must still satisfy the parallel-group shape "
    "rules (400). WHO: the task's own executor, or an admin/owner — anyone else "
    "is a flat 403; a CLOSED task is a 409. "
    + NO_OVERWRITE_GUARD
    + " Answers with a bounded receipt (`task_id`, `steps_total`, "
    "`progress_done`, `progress_total`), not the plan; call get_task to read the "
    "step rows back."
)

INSERT_BULLETS = (
    "- Adds ONE step; every other step keeps its id, status, note and bound card.\n"
    "- `before_step_id` is the step it goes in front of; omit it to append at the end.\n"
    "- 404 unknown `before_step_id`; 409 when it names a done/superseded step.\n"
    "- 400 blank `name`/`dod` or an illegal parallel-group shape.\n"
    "- 403 unless you are the executor (admin/owner excepted); 409 closed task.\n"
    "- No overwrite protection: concurrent writes are last-writer-wins, silently."
)

DELETE_BULLETS = (
    "- Removes ONE unfinished step; every other step is untouched.\n"
    "- 404 unknown step; 409 done/superseded step; 400 when it would leave zero steps.\n"
    "- 422 when removing it would finish a creator≠executor task with no handover.\n"
    "- A step holding an UNANSWERED reply card is deletable; the card is orphaned.\n"
    "- 403 unless you are the executor (admin/owner excepted); 409 closed task.\n"
    "- No overwrite protection: concurrent writes are last-writer-wins, silently."
)

REORDER_BULLETS = (
    "- Reorders unfinished steps in place; no row is rebuilt, ids and notes survive.\n"
    "- `step_ids` is the complete ordered list of the unfinished steps, nothing else.\n"
    "- Done/superseded steps keep their timeline positions and must not be listed.\n"
    "- 400 on a mismatched set or an illegal parallel-group shape.\n"
    "- 403 unless you are the executor (admin/owner excepted); 409 closed task.\n"
    "- No overwrite protection: concurrent writes are last-writer-wins, silently."
)

BEFORE_STEP_ID_DESC = (
    "The step the new one goes IN FRONT OF. Omitted or \"\" appends at the end of "
    "the timeline. An id that names no step on this task is a 404; one that names "
    "a done or superseded step is a 409 — nothing is inserted into finished work."
)

STEP_IDS_DESC = (
    "The complete ordered list of this task's UNFINISHED step ids — all of them, "
    "in the order you want them, and nothing else. Done and superseded steps keep "
    "the positions they already hold and must not appear here."
)


def error_responses():
    return {
        "200": None,  # filled by caller
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
    }


def path_param(name, title):
    return {
        "in": "path",
        "name": name,
        "required": True,
        "schema": {"title": title, "type": "string"},
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


def operation(op_id, summary, bullets, body_ref, receipt_ref, params, mcp):
    """body_ref None = the operation takes NO request body, the shape /claim and
    the other option-less task verbs already use."""
    responses = error_responses()
    responses["200"] = {
        "content": {
            "application/json": {"schema": {"$ref": "#/components/schemas/" + receipt_ref}}
        },
        "description": "Successful Response",
    }
    op = {
        "description": bullets,
        "operationId": op_id,
        "parameters": params,
        "responses": responses,
        "summary": summary,
        "x-mcp": mcp,
    }
    if body_ref is not None:
        op["requestBody"] = {
            "content": {
                "application/json": {
                    "schema": {"$ref": "#/components/schemas/" + body_ref}
                }
            },
            "required": True,
        }
    return op


TASK_ID_PROP = {"type": "string"}
STEP_ID_PROP = {"type": "string"}

INSERT_OP = operation(
    "handle_insert_task_step_api_tasks__task_id__steps_post",
    INSERT_DESC,
    INSERT_BULLETS,
    "TaskStepInsertDTO",
    "TaskStepInsertReceiptDTO",
    [path_param("task_id", "Task Id")],
    {
        "description": INSERT_DESC,
        "include": True,
        "legacy": {
            "descriptor": descriptor(
                "insert_step",
                INSERT_DESC,
                {
                    "before_step_id": {
                        "default": "",
                        "description": BEFORE_STEP_ID_DESC,
                        "title": "Before Step Id",
                        "type": "string",
                    },
                    "dod": {"title": "Dod", "type": "string"},
                    "is_gate": {
                        "anyOf": [{"type": "boolean"}, {"type": "null"}],
                        "default": None,
                        "title": "Is Gate",
                    },
                    "name": {"title": "Name", "type": "string"},
                    "parallel_group": {
                        "anyOf": [{"type": "string"}, {"type": "null"}],
                        "default": None,
                        "title": "Parallel Group",
                    },
                    "task_id": TASK_ID_PROP,
                },
                ["task_id", "name", "dod"],
            )
        },
        "name": "insert_step",
        "order": 120,
    },
)

DELETE_OP = operation(
    "handle_delete_task_step_api_tasks__task_id__steps__step_id__delete_post",
    DELETE_DESC,
    DELETE_BULLETS,
    None,
    "TaskStepMutationReceiptDTO",
    [path_param("task_id", "Task Id"), path_param("step_id", "Step Id")],
    {
        "description": DELETE_DESC,
        "include": True,
        "legacy": {
            "descriptor": descriptor(
                "delete_step",
                DELETE_DESC,
                {"step_id": STEP_ID_PROP, "task_id": TASK_ID_PROP},
                ["task_id", "step_id"],
            )
        },
        "name": "delete_step",
        "order": 121,
    },
)

REORDER_OP = operation(
    "handle_reorder_task_steps_api_tasks__task_id__steps_reorder_post",
    REORDER_DESC,
    REORDER_BULLETS,
    "TaskStepReorderDTO",
    "TaskStepMutationReceiptDTO",
    [path_param("task_id", "Task Id")],
    {
        "description": REORDER_DESC,
        "include": True,
        "legacy": {
            "descriptor": descriptor(
                "reorder_steps",
                REORDER_DESC,
                {
                    "step_ids": {
                        "description": STEP_IDS_DESC,
                        "items": {"type": "string"},
                        "title": "Step Ids",
                        "type": "array",
                    },
                    "task_id": TASK_ID_PROP,
                },
                ["task_id", "step_ids"],
            )
        },
        "name": "reorder_steps",
        "order": 122,
    },
)

SCHEMAS = {
    "TaskStepInsertDTO": {
        "additionalProperties": False,
        "description": (
            "One step to insert into an existing plan (MCP `insert_step`). The "
            "step's own fields are the same four a submit_plan node carries — "
            "`name` and `dod` are required and must be non-blank — plus "
            "`before_step_id`, which says WHERE it lands. Parallel (fork-join) "
            "shape is validated over the resulting timeline (400 otherwise), the "
            "same rules submit_plan applies: steps sharing a non-empty "
            "`parallel_group` must sit consecutively and number at least two, and "
            "a gate step must not carry a `parallel_group`."
        ),
        "properties": {
            "before_step_id": {
                "default": "",
                "description": BEFORE_STEP_ID_DESC,
                "title": "Before Step Id",
                "type": "string",
            },
            "dod": {"title": "Dod", "type": "string"},
            "is_gate": {
                "anyOf": [{"type": "boolean"}, {"type": "null"}],
                "default": None,
                "title": "Is Gate",
            },
            "name": {"title": "Name", "type": "string"},
            "parallel_group": {
                "anyOf": [{"type": "string"}, {"type": "null"}],
                "default": None,
                "title": "Parallel Group",
            },
        },
        "required": ["dod", "name"],
        "title": "TaskStepInsertDTO",
        "type": "object",
    },
    "TaskStepInsertReceiptDTO": {
        "additionalProperties": False,
        "description": (
            "Bounded receipt returned after `insert_step`. `step_id` is the id "
            "the server minted for the new step — the caller could not know it, "
            "and it is the handle every later note, status report or delete takes. "
            "The counters are the STORED timeline's, kept done/superseded history "
            "included. Fetch GET /api/tasks/{task_id} for the step rows themselves."
        ),
        "properties": {
            "progress_done": {"title": "Progress Done", "type": "integer"},
            "progress_total": {"title": "Progress Total", "type": "integer"},
            "step_id": {"title": "Step Id", "type": "string"},
            "steps_total": {"title": "Steps Total", "type": "integer"},
            "task_id": {"title": "Task Id", "type": "string"},
        },
        "required": [
            "progress_done",
            "progress_total",
            "step_id",
            "steps_total",
            "task_id",
        ],
        "title": "TaskStepInsertReceiptDTO",
        "type": "object",
    },
    "TaskStepMutationReceiptDTO": {
        "additionalProperties": False,
        "description": (
            "Bounded receipt returned after `delete_step` and `reorder_steps`. "
            "Neither call mints anything, so the receipt carries only what the "
            "caller could not know: how many steps the STORED timeline now holds "
            "(kept done/superseded history included) and where the leaf progress "
            "landed. Fetch GET /api/tasks/{task_id} for the step rows themselves."
        ),
        "properties": {
            "progress_done": {"title": "Progress Done", "type": "integer"},
            "progress_total": {"title": "Progress Total", "type": "integer"},
            "steps_total": {"title": "Steps Total", "type": "integer"},
            "task_id": {"title": "Task Id", "type": "string"},
        },
        "required": ["progress_done", "progress_total", "steps_total", "task_id"],
        "title": "TaskStepMutationReceiptDTO",
        "type": "object",
    },
    "TaskStepReorderDTO": {
        "additionalProperties": False,
        "description": (
            "The reorder_steps request body. `step_ids` is the complete ordered "
            "list of the task's UNFINISHED step ids; done and superseded steps "
            "keep the timeline positions they already hold and must not appear. A "
            "list that is not exactly that set is a 400 and nothing is written."
        ),
        "properties": {
            "step_ids": {
                "description": STEP_IDS_DESC,
                "items": {"type": "string"},
                "title": "Step Ids",
                "type": "array",
            }
        },
        "required": ["step_ids"],
        "title": "TaskStepReorderDTO",
        "type": "object",
    },
}


def fail(msg):
    print("[t228] FAIL — " + msg, file=sys.stderr)
    raise SystemExit(1)


def block(key, value, indent):
    """Render `"key": <value>,` as text at the given indent, house style."""
    raw = json.dumps({key: value}, ensure_ascii=False, indent=2, sort_keys=True)
    lines = raw.split("\n")[1:-1]  # drop the wrapping braces
    pad = " " * (indent - 2)
    return "\n".join(pad + line for line in lines) + ",\n"


def insert_before(text, anchor, payload):
    idx = text.find(anchor)
    if idx < 0:
        fail("anchor not found: " + anchor.strip())
    if text.count(anchor) != 1:
        fail("anchor is not unique: " + anchor.strip())
    return text[:idx] + payload + text[idx:]


def main():
    text = open(SPEC, encoding="utf-8").read()
    if '"insert_step"' in text:
        fail("spec already carries insert_step — this script runs once")

    text = insert_before(
        text,
        '      "TaskStepNotePatchDTO": {\n',
        block("TaskStepInsertDTO", SCHEMAS["TaskStepInsertDTO"], 8)
        + block("TaskStepInsertReceiptDTO", SCHEMAS["TaskStepInsertReceiptDTO"], 8)
        + block("TaskStepMutationReceiptDTO", SCHEMAS["TaskStepMutationReceiptDTO"], 8),
    )
    text = insert_before(
        text,
        '      "TaskStepStatusReceiptDTO": {\n',
        block("TaskStepReorderDTO", SCHEMAS["TaskStepReorderDTO"], 8),
    )

    text = insert_before(
        text,
        '    "/api/tasks/{task_id}/steps/{step_id}": {\n',
        block("/api/tasks/{task_id}/steps", {"post": INSERT_OP}, 6)
        + block("/api/tasks/{task_id}/steps/reorder", {"post": REORDER_OP}, 6),
    )
    text = insert_before(
        text,
        '    "/api/tasks/{task_id}/steps/{step_id}/note": {\n',
        block("/api/tasks/{task_id}/steps/{step_id}/delete", {"post": DELETE_OP}, 6),
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
    for path, tool in (
        ("/api/tasks/{task_id}/steps", "insert_step"),
        ("/api/tasks/{task_id}/steps/{step_id}/delete", "delete_step"),
        ("/api/tasks/{task_id}/steps/reorder", "reorder_steps"),
    ):
        op = spec["paths"][path]["post"]
        d = json.loads(op["x-mcp"]["legacy"]["descriptor"])
        if d["name"] != tool or d["description"] != op["x-mcp"]["description"]:
            fail("legacy descriptor disagrees with x-mcp for " + tool)
        if op["x-mcp"]["name"] != tool:
            fail("x-mcp.name disagrees with the tool name for " + tool)
    print("[t228] ok — %d MCP tools; insert_step/delete_step/reorder_steps at %d/%d/%d"
          % (len(orders),
             spec["paths"]["/api/tasks/{task_id}/steps"]["post"]["x-mcp"]["order"],
             spec["paths"]["/api/tasks/{task_id}/steps/{step_id}/delete"]["post"]["x-mcp"]["order"],
             spec["paths"]["/api/tasks/{task_id}/steps/reorder"]["post"]["x-mcp"]["order"]))


if __name__ == "__main__":
    main()
