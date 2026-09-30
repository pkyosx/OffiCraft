#!/usr/bin/env python3
"""T-234 follow-up — owner ruling on the spec draft: resetting something that
has no factory version is an explicit "not applicable" refusal (409), not a 404,
for all three reset routes (task manual, role definition, insight). A key that
names nothing at all stays 404. Also: the receipt's type_key now has four
faces, the create/update receipts list is_default / is_seed, and the delete
summary (the MCP description) says a built-in manual cannot be deleted.

Edits spec/openapi.json as TEXT, like bin/t234_spec_builtin_manuals.py. Every
fragment is replaced in each encoding the file carries it in (plain property
string, and the escaped legacy descriptor with or without \\u escapes), with the
expected number of hits asserted. Refuses to run twice, then re-parses the
result and checks summary == x-mcp.description == descriptor description.

🔴 THE WORDING BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. Once this
script has run, spec/openapi.json is authoritative.
"""

import json
import sys

SPEC = "spec/openapi.json"

RECEIPT_FIELDS_OLD = "(``type_key``, ``updated_ts``, ``sop_md_chars``"
RECEIPT_FIELDS_NEW = (
    "(``type_key``, ``updated_ts``, ``is_default``, ``is_seed``, ``sop_md_chars``"
)

# (old, new, expected hits across all encodings)
EDITS = [
    # reset_task_manual
    (
        "- Only a built-in (seed) manual can be reset; anything else is a 404.",
        "- Only a built-in (seed) manual can be reset: a manual created on this "
        "station is refused with 409 (not applicable); an unknown type is 404.",
        1,
    ),
    (
        "Only a built-in (seed) manual can be reset; anything else is 404.",
        "Only a built-in (seed) manual can be reset: a manual created on this "
        "station is refused with 409 (not applicable), an unknown type_key is 404.",
        3,
    ),
    # reset_role
    (
        "- Only a SEED role can be reset; anything else is a 404.",
        "- Only a SEED role can be reset: a role created on this station is "
        "refused with 409 (not applicable); an unknown role is 404.",
        1,
    ),
    (
        "Reset a role definition to seed (idempotent tombstone overlay).",
        "Reset a role definition to seed (idempotent tombstone overlay). Only a "
        "shipped (seed) role can be reset: a role created on this station is "
        "refused with 409 (not applicable), an unknown role is 404.",
        3,
    ),
    # reset_insight
    (
        "- A role with no seed file is a 404 — there must be a factory version to return to.",
        "- A role with no seed file is refused with 409 (not applicable) — there "
        "must be a factory version to return to; a role that does not exist is 404.",
        1,
    ),
    (
        "A role with NO seed file (seeds/insight_<role_key>.md) returns 404: "
        "there must be a factory version to reset TO.",
        "A role with NO seed file (seeds/insight_<role_key>.md) is refused with "
        "409 (not applicable): there must be a factory version to reset TO; a "
        "role that does not exist is 404.",
        3,
    ),
    (
        "a role with has_seed=false gets a 404 from that route",
        "a role with has_seed=false gets a 409 (not applicable) from that route",
        1,
    ),
    # get_document_seed / DocumentSeedDTO
    (
        "404 means it has none at all — a role the owner created, a task manual "
        "— which is the same set whose reset the server also 404s,",
        "404 means it has none at all — a role the owner created, a task manual "
        "created on this station — which is the same set whose reset the server "
        "refuses as not applicable (409),",
        3,
    ),
    (
        "404 when the document has no shipped default (a custom role, a task "
        "manual) — exactly the documents whose reset the server also 404s.",
        "404 when the document has no shipped default (a custom role, a task "
        "manual created on this station) — exactly the documents whose reset "
        "the server refuses as not applicable (409).",
        1,
    ),
    # TaskManualReceiptDTO.type_key
    (
        "It is only NEWS on one of the three faces: the display_name create path "
        "mints it server-side. On the legacy create path the caller's own "
        "type_key is taken verbatim, and on update_task_manual it is the caller's "
        "own URL path parameter - on those two it is an echo,",
        "It is only NEWS on one of the four faces: the display_name create path "
        "mints it server-side. On the legacy create path the caller's own "
        "type_key is taken verbatim, and on update_task_manual and "
        "reset_task_manual it is the caller's own URL path parameter - on those "
        "three it is an echo,",
        1,
    ),
    # delete_task_manual
    (
        "Delete a task type (open tasks of the type → 409).",
        "Delete a task type (open tasks of the type → 409). A built-in manual "
        "cannot be deleted (403) — reset it with reset_task_manual instead.",
        3,
    ),
]


def fail(msg):
    print("[t234-na] FAIL — " + msg, file=sys.stderr)
    raise SystemExit(1)


def encodings(s):
    plain = json.dumps(s, ensure_ascii=False)[1:-1]
    out = [plain]
    for inner_ascii in (False, True):
        inner = json.dumps(s, ensure_ascii=inner_ascii)[1:-1]
        outer = json.dumps(inner, ensure_ascii=False)[1:-1]
        if outer not in out:
            out.append(outer)
    return out


def replace_all_encodings(text, old, new, expected):
    hits = 0
    olds, news = encodings(old), encodings(new)
    for o, n in zip(olds, news):
        c = text.count(o)
        hits += c
        text = text.replace(o, n)
    if hits != expected:
        fail("expected %d hits for %r, got %d" % (expected, old[:70], hits))
    return text


def receipt_listing(text):
    spec = json.loads(text)
    for path in ("/api/task-manuals", "/api/task-manuals/{type_key}"):
        summary = spec["paths"][path]["post"]["summary"]
        if RECEIPT_FIELDS_OLD not in summary:
            fail("receipt field listing not found on POST " + path)
    return replace_all_encodings(text, RECEIPT_FIELDS_OLD, RECEIPT_FIELDS_NEW, 6)


def main():
    text = open(SPEC, encoding="utf-8").read()
    if "409 (not applicable)" in text:
        fail("spec already carries the not-applicable wording — this script runs once")
    for old, new, expected in EDITS:
        text = replace_all_encodings(text, old, new, expected)
    text = receipt_listing(text)
    json.loads(text)
    open(SPEC, "w", encoding="utf-8").write(text)
    verify()


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    touched = (
        ("/api/task-manuals", "post"),
        ("/api/task-manuals/{type_key}", "post"),
        ("/api/task-manuals/{type_key}", "delete"),
        ("/api/task-manuals/{type_key}/reset", "post"),
        ("/api/roles/{role}/reset", "post"),
        ("/api/insight/{role_key}/reset", "post"),
        ("/api/document-history/{kind}/{key}/seed", "get"),
    )
    for path, method in touched:
        op = spec["paths"][path][method]
        mcp = op["x-mcp"]
        d = json.loads(mcp["legacy"]["descriptor"])
        if not (d["description"] == mcp["description"] == op["summary"]):
            fail("description disagrees on " + mcp["name"])
    for path, needle in (
        ("/api/task-manuals/{type_key}/reset", "409 (not applicable)"),
        ("/api/roles/{role}/reset", "409 (not applicable)"),
        ("/api/insight/{role_key}/reset", "409 (not applicable)"),
    ):
        op = spec["paths"][path]["post"]
        if needle not in op["summary"] or needle not in op["description"]:
            fail("not-applicable wording missing on " + path)
    print("[t234-na] ok")


if __name__ == "__main__":
    main()
