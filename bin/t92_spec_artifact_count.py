#!/usr/bin/env python3
"""T-92 — get_task stops carrying artifact ROWS; it carries ``artifact_count``.

Edits spec/openapi.json as TEXT rather than reserialising it, for the reason
bin/t646a_spec_update_task.py gives: the file is hand-maintained, its layout is
house style, and a json.dump round-trip would reformat 17k lines and bury the
real changes. Same shape as that script — refuses to run twice, verifies its own
result by re-parsing.

WHY (owner rc-15016959ad4d, 2026-09-05, via Kyle m-f663f3c5de9a):
``get_task`` is the FIRST call every member makes on every ticket (the boot
manual requires it verbatim), so its artifact half is a cost every reader pays
on every read. On a long-lived ticket that half is the largest thing in the
response: T-33 measured 120 pinned artifacts whose labels alone serialised to
36,702 characters (O-231, 2026-09-05). Owner's ruling was a COUNT, not a
narrower row — verbatim 「只有 ID 好像也沒用」: an id on its own does not say
what a deliverable IS, so a caller holding one had to call
``list_task_artifacts`` anyway. The id-only index would have charged every
reader for a lookup that answered nobody.

🔴 HONEST ABOUT THE COST, because the ticket first got this backwards and the
owner decides with it: this does NOT make the total smaller. For T-33,
``list_task_artifacts`` measures 68,209 characters — 1.9x the 36,140 removed
here. What changes is WHO pays and WHEN: today every reader of every ticket
pays for rows most of them never open; after this, only the caller who actually
wants the deliverables pays, and they pay more. Fixed cost -> on demand.

The shape is NOT new. ``TaskListItemDTO`` has carried ``artifact_count`` since
T-3dc5 (server/ocserverd/wire.go:1985, fed by DAL.AllTaskArtifactCounts); this
gives the task VIEW the same shape its own list has had all along. The nearest
precedent for the move itself is T-66, which did exactly this to step notes:
rows out, an exact size in, contents behind a second call.

WHAT THIS SCRIPT CHANGES — spec/openapi.json ONLY. It does NOT touch Go, does
NOT run a generator, and does NOT edit spec/mcp-catalog.json (a generated
artifact of bin/gen-mcp-catalog, never a hand-edit entry). That ordering is
CLAUDE.md:45's spec-first rule, which also puts an owner review of the interface
between this file and any Go:

    design -> spec/openapi.json -> [owner reviews the interface] -> generators -> Go

  1. TaskDTO: drop ``artifacts`` and ``artifacts_detail_level``, add
     ``artifact_count``.
  2. TaskArtifactRefDTO: DELETED. It was referenced from exactly one place —
     TaskDTO.properties.artifacts.items — so removing (1) orphans it.
  3. TaskArtifactListDTO: keep ``artifacts_detail_level`` = "full" but stop
     DEFINING it by contrast. Today "full" is worded as "against the task
     view's index"; that counterpart is going away, so the wording, not the
     field, is what breaks. "full" still says something true and useful on its
     own — these rows are complete — and T-66's ``notes_included: false`` is
     the standing precedent for a self-description with no counterpart.
     (Proposed by O-231, approved by Kyle in c-f8197241b5e0.)
  4. Every other sentence in the spec that describes the old shape:
     get_task, list_task_artifacts, add_task_artifact, remove_task_artifact and
     TaskArtifactReceiptDTO. Found by sweeping the parsed spec for the phrases
     rather than by reading the diff — 19 string nodes, several of them the
     same text repeated across description / summary / x-mcp.description /
     x-mcp.legacy.descriptor.

OLD TEXT IS NEVER TYPED HERE. Every replacement reads its OLD string out of the
spec by JSON path and rewrites the file text; only the NEW strings are authored
in this file. That removes transcription error on the old side entirely — a
path that no longer resolves is a hard failure, not a silent no-op.
"""

from __future__ import annotations

import json
import pathlib
import sys

SPEC = pathlib.Path(__file__).resolve().parent.parent / "spec" / "openapi.json"


def fail(msg: str) -> "None":
    print(f"t92_spec: {msg}", file=sys.stderr)
    raise SystemExit(1)


# ── the new wording ──────────────────────────────────────────────────────────

ARTIFACT_COUNT_DESC = (
    "How many deliverables are pinned on this task — and nothing else about them "
    "(T-92, owner rc-15016959ad4d). It is EXACT, it has no ceiling and it is never "
    "trimmed to fit a response, so 0 means the task genuinely has nothing pinned "
    "rather than 'not loaded'. THE ROWS THEMSELVES ARE NOT HERE AT ALL: not their "
    "``label``, and NOT EVEN THEIR ``id``. Leaving the ids out is the owner's own "
    "call and not an oversight in it (verbatim: 「只有 ID 好像也沒用」) — an id says "
    "nothing about what the deliverable IS, so a caller holding one had to call "
    "``list_task_artifacts`` anyway, which made the old id+label index a toll every "
    "reader of every task paid for an answer it could not give. Do not put the ids "
    "back on the grounds that they are only a few characters: that is the change, "
    "not an omission in it. ``GET /api/tasks/{task_id}/artifacts`` (MCP "
    "``list_task_artifacts``) answers the WHOLE ticket in one call, every field "
    "present. This is the same field ``TaskListItemDTO`` has carried since T-3dc5; "
    "the task view now shapes its artifact half the way its own list already did."
)

GET_TASK_DESC = (
    "Read one task — and read it knowing it is a SUMMARY, not the whole of it: the "
    "response says so itself (``detail_level`` = ``summary``, ``notes_included`` = "
    "false). WHAT IS COMPLETE HERE: the task's own fields, its deps, its progress "
    "counts, its gate cards, and EVERY ONE of its steps. The step list has no cap, "
    "no paging and no truncation of any kind — the rows you get back are all the "
    "rows there are, so a step that is not here does not exist on this task. WHAT IS "
    "OMITTED, AND EXACTLY HOW MUCH OF IT: each step's working-note TEXT (T-66). In "
    "its place every step carries ``note_size_chars`` — the EXACT number of "
    "characters of note sitting on the server for that step, where 0 means that step "
    "genuinely has no note — and ``note_cap_chars``, the ceiling. A positive "
    "``note_size_chars`` is a precise promise that that many characters are waiting "
    "for you, and ``get_task_step(task_id, step_id)`` is the one call that returns "
    "them, one step at a time. Read the sizes first, then fetch only the notes you "
    "actually need. ALSO OMITTED, AND EXACTLY WHAT IS LEFT IN ITS PLACE: the task's "
    "pinned deliverables are NOT here — there are no artifact rows at all (T-92, "
    "owner rc-15016959ad4d). What stands in their place is ``artifact_count``, and it "
    "is the same kind of precise promise ``note_size_chars`` is: the EXACT number "
    "pinned, uncapped, never trimmed, and 0 meaning the task genuinely has nothing "
    "pinned. NOT EVEN THE IDS ARE HERE, which is the owner's call rather than an "
    "oversight (verbatim: 「只有 ID 好像也沒用」) — an id alone does not say what a "
    "deliverable IS, so whoever held one had to call ``list_task_artifacts`` anyway, "
    "and the old id+label index therefore charged EVERY reader of EVERY task for rows "
    "most of them never opened, while on a long-lived ticket those labels were the "
    "single largest thing in this response. Do not put the ids back on the grounds "
    "that they are only a few characters: that is the change, not an omission in it. "
    "``list_task_artifacts(task_id)`` returns the deliverables themselves — ``kind``, "
    "``url``, ``label``, ``filename``, ``mime``, ``is_image``, ``attachment_id``, "
    "``created_ts``, ``created_by`` and ``version_count`` — for EVERY artifact on the "
    "ticket in ONE call; there is deliberately no per-artifact read. Be warned that "
    "on a ticket with many deliverables that call is LARGER than everything removed "
    "here (T-33, 2026-09-05: 120 artifacts, 68,209 characters): what this shape buys "
    "is that only the caller who wants them pays for them. Unknown id → 404."
)

LIST_ARTIFACTS_DESC = (
    "Read one task's pinned deliverables IN FULL — and since T-92 this is the ONLY "
    "read that carries them at all: ``get_task`` answers with ``artifact_count`` and "
    "no artifact rows, so anything that needs to NAME, DRAW or OPEN a deliverable "
    "comes here. Answers ``{task_id, artifacts_detail_level, artifacts}`` where "
    "``artifacts_detail_level`` is ``full`` — these rows are complete, no field of an "
    "artifact is omitted here — and every artifact on the task is present, "
    "oldest→newest: ``kind`` (file|image|link), ``url`` (the blob serve path for a "
    "file/image, the external link for a link), ``label``, ``filename``, ``mime``, "
    "``is_image``, ``attachment_id``, ``created_ts``, ``created_by`` and "
    "``version_count``. ONE call answers the WHOLE ticket, and that is deliberate — "
    "there is no per-artifact read, because whoever opens a task's deliverables wants "
    "the set (a 32-artifact ticket would otherwise cost 32 calls), whereas a step note "
    "is read one at a time and ``get_task_step`` is per-step for exactly that reason. "
    "SIZE: this answer grows with the ticket and there is no paging — T-33 measured "
    "68,209 characters across 120 artifacts on 2026-09-05 — so call it when you "
    "actually want the deliverables, not as a reflex after ``get_task``. File/image "
    "metadata is resolved read-time and is honest-empty when the underlying blob is "
    "gone — never fabricated. A task with nothing pinned answers ``artifacts: []``, "
    "not a 404; an unknown task id is a 404. Same read floor as ``get_task``: any "
    "authenticated principal may read any task's artifacts, and no field here was "
    "behind a stricter door before."
)

LIST_DTO_DESC = (
    "One task's pinned deliverables IN FULL (T-66) — the answer of ``GET "
    "/api/tasks/{task_id}/artifacts`` / MCP ``list_task_artifacts``, and since T-92 "
    "the ONLY response that carries artifact rows at all (a task response carries "
    "``artifact_count`` and no rows). ``artifacts`` holds EVERY artifact on the task, "
    "oldest→newest, each a complete ``TaskArtifactDTO``; an empty set is ``[]``, "
    "never a 404. It is a wrapped list rather than a bare array so the response can "
    "say what it is: ``artifacts_detail_level`` is ``full`` — these rows are "
    "complete, no field of an artifact is omitted here."
)

LIST_DTO_LEVEL_DESC = (
    "What this response IS, said by the response itself (T-66): always ``full`` — "
    "every artifact row here is COMPLETE, no field of an artifact is omitted. Read it "
    "as a promise about THESE rows, standing on its own, not as one half of a pair: "
    "until T-92 a task response declared ``index`` and this value was worded against "
    "it, but the task view no longer carries artifact rows to contrast with (it "
    "carries ``artifact_count``). The promise is unchanged by that; only what it used "
    "to be measured against is gone. ``TaskStepDetailDTO``'s ``notes_included`` is "
    "the same kind of field — a response describing its own completeness with nothing "
    "to be compared to."
)

RECEIPT_DESC = (
    "Bounded receipt returned after pinning or un-pinning ONE deliverable (T-a98d). "
    "It names the artifact the write touched and the resulting size of the set — the "
    "whole task used to ride back on a one-line pin, which no agent client could "
    "read. Fetch GET /api/tasks/{task_id} when full task detail is needed, and GET "
    "/api/tasks/{task_id}/artifacts (MCP ``list_task_artifacts``) for the artifact "
    "set itself — since T-92 the task response carries only ``artifact_count`` and no "
    "artifact rows, so this receipt's ``artifact_id`` is the one place the id of the "
    "artifact you just pinned is handed to you."
)


def edits(spec: dict) -> list[tuple[str, str]]:
    """(old, new) pairs, with every OLD read out of the spec by path."""
    sch = spec["components"]["schemas"]

    def at(*keys):
        node = spec
        for k in keys:
            if not isinstance(node, dict) or k not in node:
                fail(f"path {'/'.join(map(str, keys))} does not resolve — spec moved")
            node = node[k]
        if not isinstance(node, str):
            fail(f"path {'/'.join(map(str, keys))} is not a string")
        return node

    get_task = ("paths", "/api/tasks/{task_id}", "get")
    list_art = ("paths", "/api/tasks/{task_id}/artifacts", "get")
    add_art = ("paths", "/api/tasks/{task_id}/artifact", "post")
    del_art = ("paths", "/api/tasks/{task_id}/artifact/{artifact_id}", "delete")

    old_get = at(*get_task, "description")
    old_list = at(*list_art, "description")

    pairs: list[tuple[str, str]] = [
        (old_get, GET_TASK_DESC),
        (old_list, LIST_ARTIFACTS_DESC),
        (sch["TaskArtifactListDTO"]["description"], LIST_DTO_DESC),
        (
            sch["TaskArtifactListDTO"]["properties"]["artifacts_detail_level"][
                "description"
            ],
            LIST_DTO_LEVEL_DESC,
        ),
        (sch["TaskArtifactReceiptDTO"]["description"], RECEIPT_DESC),
    ]

    # add_task_artifact / remove_task_artifact: one clause each, not the whole
    # description — the rest of those texts is about permissions and guards and
    # is untouched by this ticket. Replace the clause, leave the sentence's
    # neighbours exactly as they are.
    add_desc = at(*add_art, "description")
    old_clause = "since T-66 GET /api/tasks/{task_id} carries only an id+label INDEX of it"
    if old_clause not in add_desc:
        fail("add_task_artifact: the id+label clause is not where it was")
    pairs.append(
        (
            old_clause,
            "since T-92 GET /api/tasks/{task_id} carries only ``artifact_count`` and "
            "no artifact rows",
        )
    )

    del_desc = at(*del_art, "description")
    if old_clause in del_desc:
        pass  # same clause, already covered by the replacement above
    del_summary = at(*del_art, "summary")
    old_from = "(the id returned when it was added, or from get_task's artifacts)"
    if old_from not in del_summary:
        fail("remove_task_artifact: the 'from get_task's artifacts' clause moved")
    pairs.append(
        (
            old_from,
            "(the id returned when it was added, or from ``list_task_artifacts`` — "
            "since T-92 ``get_task`` no longer carries artifact rows)",
        )
    )

    return pairs


def apply_text_edits(raw: str, pairs: list[tuple[str, str]]) -> str:
    """Replace each pair in the RAW file text.

    Every one of these strings appears in the file JSON-escaped, and the ones on
    an MCP operation appear a SECOND time double-escaped inside
    ``x-mcp.legacy.descriptor`` (a JSON document stored as a JSON string).
    Double-escaped forms are replaced FIRST: doing it the other way round would
    corrupt the inner document, because the single-escaped pass would match
    inside it.
    """
    for old, new in pairs:
        enc_old = json.dumps(old, ensure_ascii=False)[1:-1]
        enc_new = json.dumps(new, ensure_ascii=False)[1:-1]
        enc2_old = json.dumps(enc_old, ensure_ascii=False)[1:-1]
        enc2_new = json.dumps(enc_new, ensure_ascii=False)[1:-1]

        n2 = raw.count(enc2_old)
        if n2:
            raw = raw.replace(enc2_old, enc2_new)
        n1 = raw.count(enc_old)
        if n1 == 0 and n2 == 0:
            fail(f"neither form found for: {old[:70]!r}")
        raw = raw.replace(enc_old, enc_new)
        print(f"  replaced {n2} escaped + {n1} plain :: {old[:60]}...")
    return raw


def main() -> None:
    raw = SPEC.read_text()

    if '"artifact_count"' in raw and '"TaskArtifactRefDTO"' not in raw:
        fail("already applied (TaskArtifactRefDTO gone) — refusing to run twice")

    spec = json.loads(raw)
    if "artifact_count" in spec["components"]["schemas"]["TaskDTO"]["properties"]:
        fail("already applied (TaskDTO.artifact_count exists) — refusing to run twice")

    # 1 + 2 must happen together: TaskArtifactRefDTO is referenced from exactly
    # one place, so this asserts that before deleting either.
    refs = raw.count("#/components/schemas/TaskArtifactRefDTO")
    if refs != 1:
        fail(
            f"TaskArtifactRefDTO has {refs} $refs, expected exactly 1 "
            "(TaskDTO.properties.artifacts.items) — it is not an orphan after this "
            "change and must not be deleted"
        )

    print("t92_spec: rewriting descriptions")
    raw = apply_text_edits(raw, edits(spec))

    # 1. TaskDTO: artifacts + artifacts_detail_level -> artifact_count.
    start_anchor = '          "artifacts": {\n            "description": "The task\'s pinned deliverables as an INDEX'
    end_anchor = '          "blocking": {'
    i = raw.find(start_anchor)
    if i < 0:
        fail("TaskDTO.artifacts block not found")
    j = raw.find(end_anchor, i)
    if j < 0:
        fail("TaskDTO.blocking (the block after) not found")
    new_prop = (
        '          "artifact_count": {\n'
        '            "default": 0,\n'
        f"            \"description\": {json.dumps(ARTIFACT_COUNT_DESC, ensure_ascii=False)},\n"
        '            "title": "Artifact Count",\n'
        '            "type": "integer"\n'
        "          },\n"
    )
    raw = raw[:i] + new_prop + raw[j:]
    print("t92_spec: TaskDTO artifacts/artifacts_detail_level -> artifact_count")

    # 2. TaskArtifactRefDTO: now an orphan, delete the schema.
    s = raw.find('      "TaskArtifactRefDTO": {')
    if s < 0:
        fail("TaskArtifactRefDTO schema not found")
    e = raw.find('      "TaskArtifactListDTO": {', s)
    if e < 0:
        fail("the schema after TaskArtifactRefDTO moved — refusing to guess the span")
    raw = raw[:s] + raw[e:]
    print("t92_spec: TaskArtifactRefDTO deleted (orphaned by the change above)")

    # verify before writing: it must still parse, and say what we think it says.
    out = json.loads(raw)
    td = out["components"]["schemas"]["TaskDTO"]["properties"]
    if "artifacts" in td or "artifacts_detail_level" in td:
        fail("verify: TaskDTO still carries an artifact row field")
    if td.get("artifact_count", {}).get("type") != "integer":
        fail("verify: TaskDTO.artifact_count is not an integer property")
    if "TaskArtifactRefDTO" in out["components"]["schemas"]:
        fail("verify: TaskArtifactRefDTO survived")
    if "TaskArtifactRefDTO" in json.dumps(out, ensure_ascii=False):
        fail("verify: something still names TaskArtifactRefDTO")
    lvl = out["components"]["schemas"]["TaskArtifactListDTO"]["properties"][
        "artifacts_detail_level"
    ]
    if lvl.get("default") != "full":
        fail("verify: list_task_artifacts stopped declaring full — not this ticket")

    SPEC.write_text(raw)
    print(f"t92_spec: wrote {SPEC} ({len(raw)} bytes)")
    print("t92_spec: NOT run — bin/gen-ocapi, bin/gen-mcp-catalog. spec-first: the")
    print("t92_spec: owner reviews this interface before any generator or Go.")


if __name__ == "__main__":
    main()
