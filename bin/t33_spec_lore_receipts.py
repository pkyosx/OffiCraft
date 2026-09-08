#!/usr/bin/env python3
"""T-33 (owner 2026-09-07) — the THREE 傳承 writes answer a bounded receipt
instead of the whole entry they just wrote.

Owner ruling, verbatim: 「別這樣 浪費 context 我們才修一輪不要回傳自己寫出去的
payload」. ``POST /api/lore``, ``POST /api/lore/{entry_id}/state`` and
``POST /api/lore/{entry_id}/bump`` all answered ``LoreEntryDTO`` — so the
``title`` and the ``body``, the two fields capped by ``lore_cap_chars_title``
and ``lore_cap_chars_body``, rode home to the agent that had just typed them,
and on the two governance verbs rode home to a caller that had never sent them
at all. Same package and same rule as T-91's receipts (2026-09-05: 「自己發送出
去的內容，除了像是 ID 這類的，或是真的需要從回覆得知的，其他都不應該再回傳回
來。」).

Two receipts, not one and not three:

  * ``LoreEntryWriteReceiptDTO`` — the write face. It keeps ``scope_kind`` /
    ``scope_key`` because WHERE an entry landed is decided server-side off the
    effective related task, so it is news rather than an echo.
  * ``LoreEntryStateReceiptDTO`` — shared by the state door and the bump door.
    Both move the same row's mutable surface and neither can report anything
    the other cannot; one shape is what keeps them from drifting into two
    answers to the same question.

``filed_note`` is renamed ``scope_note`` and MOVES to the write receipt — it was
mounted on ``LoreEntryDTO``, where it is empty on every row ``GET /api/lore``
ever serves, because only a write can produce it. The sentence it carries is
unchanged.

``GET /api/lore`` is untouched: a list face is a read, and ``LoreEntryListDTO``
is the right answer for it. It only loses the always-empty column.

Edits spec/openapi.json as TEXT rather than reserialising it — the file is
hand-maintained and its layout is house style; a json.dump round-trip would
reformat 17k lines and bury the change. Precedent: bin/t91_spec_lifecycle_receipts.py,
bin/t646a_spec_update_task.py.

Refuses to run twice. Verifies its own result by re-parsing.
"""

import json
import sys

SPEC = "spec/openapi.json"

WRITE_DTO = "LoreEntryWriteReceiptDTO"
STATE_DTO = "LoreEntryStateReceiptDTO"

ROUTES = [
    ("/api/lore", "post", WRITE_DTO),
    ("/api/lore/{entry_id}/state", "post", STATE_DTO),
    ("/api/lore/{entry_id}/bump", "post", STATE_DTO),
]

SCOPE_NOTE_DESC = (
    "Empty on every ordinary write. It carries one sentence in exactly one "
    "case: the write named a ``task_id`` whose task carries NO type, so the "
    "effective related task was NULL and the entry was filed under the "
    "writer's own boot document rather than under a manual.\n\nIt exists "
    "because the writer has no other way to learn that. Whether the task it "
    "named happens to carry a type is not something the writer holds in mind "
    "at the moment of the write, and both outcomes answer 200 — so without "
    "this sentence the two are indistinguishable from the caller's side. It "
    "REPORTS where the entry went; it is not a warning that a request was "
    "re-routed, because a task with no type was never a place an entry could "
    "hang.\n\nIt is on the RECEIPT and not on ``LoreEntryDTO``, where it used "
    "to live as ``filed_note``: only a write can produce it, so on every row "
    "``GET /api/lore`` serves it was an always-empty column riding every "
    "entry of every page."
)

WRITE_SCHEMA = {
    "description": (
        "Bounded receipt for ``POST /api/lore`` (write_lore_entry) (T-33). It "
        "used to answer with the whole LoreEntryDTO, so the ``title`` and "
        "``body`` the agent had just written came straight back into its "
        "context window — a body sized by ``lore_cap_chars_body`` paid for "
        "twice on one call. Owner ruling 2026-09-07, verbatim: 「別這樣 浪費 "
        "context 我們才修一輪不要回傳自己寫出去的 payload」; the rule it applies "
        "is the one T-91's receipts already follow (2026-09-05: 「自己發送出去的"
        "內容，除了像是 ID 這類的，或是真的需要從回覆得知的，其他都不應該再回傳"
        "回來。」).\n\nEVERY FIELD HERE IS MINTED OR DECIDED BY THE HANDLER, "
        "none is an echo. What is dropped: ``title`` and ``body`` (just sent), "
        "``author_id`` (the verified caller, which is the caller), "
        "``source_task_id`` (the ``task_id`` just sent), plus ``state``, "
        "``retire_reason``, ``effective_ts`` and ``updated_ts``, which on a "
        "fresh write are constants — a new entry is always ``active`` with no "
        "reason, and all three of its timestamps equal ``created_ts``. Call "
        "``list_lore_entries`` (``GET /api/lore``) for the entry itself.\n\n"
        "``scope_kind`` and ``scope_key`` STAY, and they are why this receipt "
        "is more than an id. WHICH of the three boot documents an entry landed "
        "in is decided SERVER-SIDE, off the effective related task and off the "
        "caller's own roster row — a caller that named a task cannot predict "
        "it, and it is the machine-readable half of what ``scope_note`` says "
        "in a sentence."
    ),
    "properties": {
        "id": {
            "description": (
                "``L-<n>``, MINTED HERE, ``n`` ascending globally. The handle "
                "``set_lore_entry_state`` and ``bump_lore_entry`` take as "
                "``entry_id``, and the one thing the caller cannot compute."
            ),
            "title": "Id",
            "type": "string",
        },
        "seq": {
            "description": (
                "The number behind the id, assigned here — also the stable "
                "tie-break when two entries carry the same ``effective_ts``."
            ),
            "title": "Seq",
            "type": "integer",
        },
        "scope_kind": {
            "description": (
                "``role``, ``agent`` or ``manual`` — WHERE THIS ENTRY WAS "
                "FILED, which the server decided and the caller did not ask "
                "for. The deciding question is the EFFECTIVE RELATED TASK: the "
                "named task when it carries a type, and NULL otherwise, where "
                "\"otherwise\" covers BOTH naming no task and naming a 臨時任務 "
                "that carries no type. An effective task gives ``manual``; NULL "
                "gives ``role`` for staff and ``agent`` for an outsource "
                "member, who holds no role for ``role`` to name."
            ),
            "title": "Scope Kind",
            "type": "string",
        },
        "scope_key": {
            "description": (
                "The role_key when ``scope_kind`` is ``role``; the writer's own "
                "member id when it is ``agent``; the task manual's "
                "``type_key`` when it is ``manual``. Together with "
                "``scope_kind`` it is the filter that reads this entry back "
                "out of ``list_lore_entries``."
            ),
            "title": "Scope Key",
            "type": "string",
        },
        "created_ts": {
            "format": "double",
            "description": (
                "The SERVER's stamp for the entry, epoch seconds. The caller "
                "does not send it and cannot backdate it. ``effective_ts`` and "
                "``updated_ts`` are not on this receipt because on a fresh "
                "write both equal this one — they can only come apart later, "
                "and the receipt for that move reports them."
            ),
            "title": "Created Ts",
            "type": "number",
        },
        "scope_note": {
            "description": SCOPE_NOTE_DESC,
            "title": "Scope Note",
            "type": "string",
        },
    },
    "required": [
        "id",
        "seq",
        "scope_kind",
        "scope_key",
        "created_ts",
        "scope_note",
    ],
    "title": WRITE_DTO,
    "additionalProperties": False,
    "type": "object",
}

STATE_SCHEMA = {
    "description": (
        "Bounded receipt for the two 傳承 GOVERNANCE writes — "
        "``POST /api/lore/{entry_id}/state`` (set_lore_entry_state) and "
        "``POST /api/lore/{entry_id}/bump`` (bump_lore_entry) (T-33). Both used "
        "to answer with the whole LoreEntryDTO, so an entry's ``title`` and "
        "``body`` came home on every 置頂 / 失效 / 提到最新 — a payload the "
        "caller had never sent, on a call whose entire content is one state "
        "word or nothing at all. Owner ruling 2026-09-07: 「不要回傳自己寫出去的 "
        "payload」, read together with the 2026-09-05 rule that only ids and "
        "what the write itself decides ride home.\n\nONE SHAPE FOR BOTH DOORS. "
        "They move the same row's mutable surface and neither can report "
        "anything the other cannot: the state door writes ``state`` (and "
        "``retire_reason``, which it stores only with ``retired`` and clears "
        "otherwise), the bump door writes ``effective_ts``, and both stamp "
        "``updated_ts``. Two shapes would be two answers to one question, free "
        "to drift. What is dropped is the read-only half — ``seq``, "
        "``scope_kind``, ``scope_key``, ``title``, ``body``, ``author_id``, "
        "``source_task_id``, ``created_ts`` — none of which either verb can "
        "change; call ``list_lore_entries`` (``GET /api/lore``) for the entry "
        "itself."
    ),
    "properties": {
        "id": {
            "description": (
                "The entry that was moved, echoed from the path. An id, which "
                "the owner's rule exempts in as many words (「除了像是 ID 這類"
                "的」): it is what lets a caller match this answer to the "
                "request it made."
            ),
            "title": "Id",
            "type": "string",
        },
        "state": {
            "description": (
                "``active`` | ``pinned`` | ``retired`` — the state the entry is "
                "in AFTER this write, read back from the stored row. On the "
                "state door it confirms the asked-for move landed. On the bump "
                "door the caller sent no state at all, and this is where it "
                "learns 提到最新 did NOT change one — a bump reorders, it does "
                "not revive a retired entry."
            ),
            "title": "State",
            "type": "string",
        },
        "effective_ts": {
            "format": "double",
            "description": (
                "The entry's ordering key as it now stands, epoch seconds. On "
                "the bump door this is the SERVER's new now-stamp, which is the "
                "whole point of the call and the one value the caller cannot "
                "compute — two entries bumped from two machines still order by "
                "one clock. On the state door it is untouched, which is how a "
                "caller sees that retiring or reviving did not reorder "
                "anything."
            ),
            "title": "Effective Ts",
            "type": "number",
        },
        "updated_ts": {
            "format": "double",
            "description": (
                "The SERVER's stamp for THIS write, epoch seconds. It moves on "
                "both doors and on every call, so it — not ``effective_ts`` — "
                "is what says the write happened at all."
            ),
            "title": "Updated Ts",
            "type": "number",
        },
    },
    "required": ["id", "state", "effective_ts", "updated_ts"],
    "title": STATE_DTO,
    "additionalProperties": False,
    "type": "object",
}

NEW_SCHEMAS = [(WRITE_DTO, WRITE_SCHEMA), (STATE_DTO, STATE_SCHEMA)]


def main() -> int:
    text = open(SPEC, encoding="utf-8").read()

    if WRITE_DTO in text or STATE_DTO in text:
        raise SystemExit("refusing to run twice: receipt schemas already present")

    # 1. Drop `filed_note` from LoreEntryDTO (property block + required entry).
    prop_start = text.index('            "filed_note": {')
    prop_end = text.index('            "title": {', prop_start)
    text = text[:prop_start] + text[prop_end:]

    req = '            "scope_key",\n            "filed_note",\n'
    if text.count(req) != 1:
        raise SystemExit("required-list anchor not unique")
    text = text.replace(req, '            "scope_key",\n')

    # 2. Rename the one prose reference on LoreEntryWriteDTO.
    old_ref = "When that happens ``filed_note`` on the response says so"
    if text.count(old_ref) != 1:
        raise SystemExit("LoreEntryWriteDTO prose anchor not unique")
    text = text.replace(
        old_ref, "When that happens ``scope_note`` on the write receipt says so"
    )

    if "filed_note" in text or "Filed Note" in text:
        raise SystemExit("filed_note survived the removal")

    # 3. Insert the two receipt schemas after LoreEntryListDTO.
    anchor = '        "LoreEntryListDTO": {'
    at = text.index(anchor)
    end = text.index('\n        },\n', at) + len('\n        },\n')
    block = ""
    for name, schema in NEW_SCHEMAS:
        body = json.dumps(schema, ensure_ascii=False, indent=2)
        body = "\n".join(("        " + ln) if ln else ln for ln in body.splitlines())
        block += '        "%s": %s,\n' % (name, body.lstrip())
    text = text[:end] + block + text[end:]

    # 4. Rewire the three 200 responses.
    spec_for_offsets = json.loads(text)  # parse guard before the ref surgery
    del spec_for_offsets
    for path, method, dto in ROUTES:
        # Each of the three routes has exactly one LoreEntryDTO $ref, in its
        # 200 response; located by walking forward from the path key so the
        # three cannot be confused with one another.
        pkey = '    "%s": {' % path
        p_at = text.index(pkey)
        m_at = text.index('      "%s": {' % method, p_at)
        ref = '"$ref": "#/components/schemas/LoreEntryDTO"'
        r_at = text.index(ref, m_at)
        text = (
            text[:r_at]
            + '"$ref": "#/components/schemas/%s"' % dto
            + text[r_at + len(ref) :]
        )

    open(SPEC, "w", encoding="utf-8").write(text)

    # 5. Verify by re-parsing.
    spec = json.loads(open(SPEC, encoding="utf-8").read())
    schemas = spec["components"]["schemas"]
    for name, _ in NEW_SCHEMAS:
        if name not in schemas:
            raise SystemExit("post-check: %s missing" % name)
    if "filed_note" in schemas["LoreEntryDTO"]["properties"]:
        raise SystemExit("post-check: filed_note still on LoreEntryDTO")
    if "filed_note" in schemas["LoreEntryDTO"]["required"]:
        raise SystemExit("post-check: filed_note still required on LoreEntryDTO")
    for path, method, dto in ROUTES:
        got = spec["paths"][path][method]["responses"]["200"]["content"][
            "application/json"
        ]["schema"]["$ref"]
        want = "#/components/schemas/%s" % dto
        if got != want:
            raise SystemExit(
                "post-check: %s %s -> %s, wanted %s" % (method, path, got, want)
            )
    if (
        spec["paths"]["/api/lore"]["get"]["responses"]["200"]["content"][
            "application/json"
        ]["schema"]["$ref"]
        != "#/components/schemas/LoreEntryListDTO"
    ):
        raise SystemExit("post-check: GET /api/lore was not left alone")
    print("ok: 3 lore writes rewired, 2 receipt schemas added, filed_note dropped")
    return 0


if __name__ == "__main__":
    sys.exit(main())
