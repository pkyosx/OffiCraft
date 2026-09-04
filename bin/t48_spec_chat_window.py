#!/usr/bin/env python3
"""T-48 — GET /api/chat: window by message id, and stop writing read watermarks.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style (compact leaf objects, deliberate
non-alphabetical grouping); a json.dump round-trip would reformat all 17k lines
and bury the handful of real changes. Same shape as
bin/t646a_spec_update_task.py, which is the precedent.

The wire changes, all inside the GET operation of /api/chat:
  1. operation.description — the HISTORY PAGING paragraph loses the
     "a history page never advances the watermark" sentence (there is no
     watermark write left on this route to except it from) and gains a
     DEPRECATED note pointing at the new pair; the whole AUTO READ-RECEIPT
     paragraph is replaced by the start_id/end_id window, its guardrails, and
     the owner ruling that this route marks nothing read.
  2. parameters — ``peek`` is REMOVED (it existed only to opt out of the
     receipt this route no longer fires; kept-and-ignored, it would read to the
     next caller like a protection that is there); ``before_ts``/``before_id``
     are marked deprecated and say so; ``start_id``/``end_id`` are added after
     ``before_id``; the ``ids`` description drops its two references to the
     removed parameter and to the watermark.
  3. x-mcp.description + x-mcp.legacy.descriptor — the same, kept byte-consistent
     with each other (gen-mcp-catalog refuses a descriptor whose description
     disagrees with x-mcp.description).

The owner ruling behind the receipt removal (2026-09-02, verbatim):
「get_chat不應該可以標示已讀未讀，這應該要另一隻API明確表示有這個意圖」. Marking a
conversation read is now only POST /api/chat/mark-read.

Refuses to run twice. Verifies its own result by re-parsing the spec and
re-checking every change, so a silently corrupted spec cannot pass as success.

Kept in the tree so the diff it produced is reproducible and reviewable, not as
a build step.

🔴 THE PROSE BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. spec/openapi.json
is authoritative; this file holds a second copy of the wording as applied on
2026-09-02, and NO drift guard compares the two — so the moment anyone edits
those descriptions in the spec, this copy is stale and nothing says so. Read the
spec when you want the current wording; do not "sync" these strings.
"""

import json
import re
import sys

SPEC = "spec/openapi.json"

# ── 1. operation description ────────────────────────────────────────────────

OLD_HISTORY_TAIL = (
    " A HISTORY PAGE NEVER ADVANCES THE READ WATERMARK\n"
    "— reading old context is not reading the conversation's newest messages, so\n"
    "the auto read-receipt below fires only on a cursorless list."
)

NEW_HISTORY_TAIL = (
    "\nThese two cursors are DEPRECATED as of T-48 — see ``start_id``/``end_id``\n"
    "below, which supersede them."
)

OLD_RECEIPT_PARA = (
    "AUTO READ-RECEIPT: when a specific conversation is requested (``?with=<peer>``)\n"
    "WITHOUT a cursor, listing it IS reading it — the caller (``actor``, the\n"
    "verified JWT ``sub``) has\n"
    "now seen every returned message, so we advance its read watermark for that\n"
    "conversation to the newest returned message ts (monotonic upsert; a no-op when\n"
    "the conversation is empty or nothing newer landed). This is the agent-side\n"
    "automatic already-read core: an agent that lists a conversation is marked as\n"
    "having read it, with no extra call."
)

NEW_RECEIPT_PARA = (
    "WINDOW BY MESSAGE ID (T-48): ``?start_id=<id>`` returns the ``limit`` messages\n"
    "running from that message TOWARDS THE NEWEST, and ``?end_id=<id>`` the ``limit``\n"
    "messages running from it TOWARDS THE OLDEST; BOTH ENDPOINTS ARE INCLUSIVE, and\n"
    "both still answer oldest→newest. They exist because ``before_ts``/``before_id``\n"
    "can only walk BACKWARDS: a caller told to jump to one specific message could\n"
    "reach it but could not then load what came AFTER it.\n"
    "\n"
    "GUARDRAILS on that path, each with the code it answers:\n"
    "* NEITHER given — this route behaves EXACTLY as it does today, byte for byte.\n"
    "  Every rule below applies only when at least one of them is sent.\n"
    "* ``start_id`` AND ``end_id`` given together and inconsistent (``start_id``\n"
    "  strictly newer than ``end_id`` in the stream's ``(ts, id)`` order) — 422.\n"
    "  Deliberately NOT an empty array: an empty page is what a real but empty\n"
    "  window returns, so a contradictory request would be indistinguishable from\n"
    "  a legitimate one that found nothing.\n"
    "* an id no message carries — 404, naming it. Same reason as ``ids``.\n"
    "* sent together with the deprecated ``before_ts``/``before_id`` — 422. The two\n"
    "  cursor families disagree about direction; honouring one and dropping the\n"
    "  other silently is how a caller ends up reading the wrong end of the stream.\n"
    "* ``limit`` outside 1..200 — 422, ON THIS PATH ONLY. The legacy paths keep\n"
    "  today's semantics unchanged (a NEGATIVE limit still disables the cap, 0\n"
    "  still returns an empty list); the committed callers that pass ``limit=-1``\n"
    "  are not asked to pay for a window they do not open.\n"
    "  ⚠️ THE CAP BOUNDS ROWS, NOT BYTES. 200 rows has been measured at 687 KB.\n"
    "  Nothing here bounds the payload size.\n"
    "\n"
    "THIS ROUTE NEVER WRITES A READ WATERMARK (owner ruling, 2026-09-02: 「get_chat\n"
    "不應該可以標示已讀未讀，這應該要另一隻 API 明確表示有這個意圖」). It used to:\n"
    "a cursorless ``?with=<peer>`` list advanced the caller's watermark for that\n"
    "conversation, on the theory that listing a conversation IS reading it. It is\n"
    "not. Measured in an isolated station (T-48 report ``ta-ab9c8e1ba74e``): a\n"
    "member whose ONLY action was holding the SSE downlink open — never woken, no\n"
    "task, transcript one line long — had ``chat_read`` written with\n"
    "``last_read_ts`` equal to a message nobody had looked at, and the owner's read\n"
    "tick went 0→1. The receipt measured that the listener process was alive, not\n"
    "that anyone had read anything.\n"
    "\n"
    "Marking a conversation read is now ONLY ``POST /api/chat/mark-read`` — an\n"
    "existing route, stating the intent explicitly. The ``peek`` parameter, which\n"
    "existed solely to OPT OUT of the receipt this route no longer fires, is\n"
    "REMOVED rather than kept and ignored: a parameter with no effect reads to the\n"
    "next caller like a protection that is there."
)

# ── 2. parameters ───────────────────────────────────────────────────────────

DEPRECATED_CURSOR_DESC = (
    "DEPRECATED (T-48) — superseded by ``start_id``/``end_id``, which take a message "
    "id instead of a composite keyset cursor and can walk FORWARDS as well as back. "
    "Still honoured, still a matched pair (one without the other is 422), still never "
    "combined with the new parameters (422). Kept because the agent-side ``get_chat`` "
    "and the cockpit scrollback both still send it."
)

START_ID_DESC = (
    "Window anchor, INCLUSIVE, walking TOWARDS THE NEWEST: return this message and the "
    "``limit``-1 that follow it, oldest→newest. This is the direction "
    "``before_ts``/``before_id`` cannot express — those only ever walk back, so a "
    "caller that jumped to one specific message could not load what came after it. An "
    "id no message carries is 404, not an empty page: an empty page is what a real "
    "window at the end of the stream returns, and the two must not be "
    "indistinguishable. Sending it alongside ``before_ts``/``before_id`` is 422. "
    "Sending it with an ``end_id`` that is strictly OLDER than it is 422. On this path "
    "``limit`` must be 1..200 or the call is 422 — the legacy paths keep their own "
    "semantics."
)

END_ID_DESC = (
    "Window anchor, INCLUSIVE, walking TOWARDS THE OLDEST: return this message and the "
    "``limit``-1 that precede it, still answered oldest→newest. Same guardrails as "
    "``start_id`` (404 on an unknown id, 422 when combined with "
    "``before_ts``/``before_id``, 422 when the pair contradicts, ``limit`` bounded to "
    "1..200 on this path). Given TOGETHER with ``start_id`` the two bound one window "
    "and ``limit`` still caps it, so a window wider than 200 rows is truncated from "
    "the ``start_id`` end rather than silently returning everything."
)

OLD_IDS_NOT_CONSULTED = (
    "``with``, ``limit``, ``before_ts``/``before_id`` and ``peek`` are NOT consulted"
)
NEW_IDS_NOT_CONSULTED = (
    "``with``, ``limit``, ``before_ts``/``before_id`` and ``start_id``/``end_id`` "
    "are NOT consulted"
)

OLD_IDS_WATERMARK = (
    "and a by-ids read NEVER advances a read watermark — re-reading a message you "
    "were already shown is not reading the conversation."
)
NEW_IDS_WATERMARK = (
    "and a by-ids read returns them untouched — as of T-48 NO read on this route "
    "advances a watermark at all."
)

# ── 3. x-mcp ────────────────────────────────────────────────────────────────

OLD_TOOL_DESC = (
    "List the chat stream (?with=<id>&limit=<n>; oldest→newest). History paging: "
    "before_ts + before_id (both together) return the limit messages strictly OLDER "
    "than that keyset cursor — a history page NEVER advances the read watermark. "
    "Re-read specific messages by id: ids=<id>&ids=<id> returns those messages in full "
    "without a peer and without a cursor; the ids schema states who may read what, the "
    "per-call limit, and what an unknown id does."
)

NEW_TOOL_DESC = (
    "List the chat stream (?with=<id>&limit=<n>; oldest→newest). Window by message id: "
    "start_id walks TOWARDS THE NEWEST from that message, end_id TOWARDS THE OLDEST, "
    "both endpoints inclusive. The older before_ts + before_id keyset cursor still "
    "works but is deprecated. Re-read specific messages by id: ids=<id>&ids=<id> "
    "returns those messages in full. THIS ROUTE NEVER MARKS ANYTHING READ (T-48) — to "
    "mark a conversation read, call mark_read explicitly."
)

DESCRIPTOR_CURSOR_PREFIX = "DEPRECATED (T-48) — prefer start_id/end_id. "

DESCRIPTOR_LIMIT_SUFFIX = (
    " ⚠️ On the start_id/end_id path this is DIFFERENT: there the limit MUST be 1..200 "
    "and anything outside that is a 422 rather than a silent wrong answer. The "
    "forgiving behaviour described above is the LEGACY paths only."
)


def fail(msg):
    print(f"[t48] FAIL: {msg}", file=sys.stderr)
    sys.exit(1)


def esc(s):
    """Escape as the spec file escapes: non-ASCII stays literal."""
    return json.dumps(s, ensure_ascii=False)[1:-1]


def esc_ascii(s):
    """Escape the way the legacy descriptor document escapes its own strings."""
    return json.dumps(s, ensure_ascii=True)[1:-1]


def sub1(text, old, new, what):
    if text.count(old) != 1:
        fail(f"expected exactly one occurrence of {what}, found {text.count(old)}")
    return text.replace(old, new)


def find_object_end(text, start):
    """Index just past the closing brace of the JSON object starting at `start`."""
    if text[start] != "{":
        fail("find_object_end did not start on an object")
    depth, i, in_str, esc_next = 0, start, False, False
    while i < len(text):
        c = text[i]
        if in_str:
            if esc_next:
                esc_next = False
            elif c == "\\":
                esc_next = True
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


def find_string_end(text, start):
    """Index just past the closing quote of the JSON string starting at `start`."""
    if text[start] != '"':
        fail("find_string_end did not start on a string")
    i, esc_next = start + 1, False
    while i < len(text):
        c = text[i]
        if esc_next:
            esc_next = False
        elif c == "\\":
            esc_next = True
        elif c == '"':
            return i + 1
        i += 1
    fail("unterminated string")


def param_block(name, title, description):
    """Render one query parameter object at the spec's indentation."""
    obj = {
        "description": description,
        "in": "query",
        "name": name,
        "required": False,
        "schema": {
            "anyOf": [{"type": "string"}, {"type": "null"}],
            "title": title,
        },
    }
    raw = json.dumps(obj, ensure_ascii=False, indent=2)
    return "\n".join(
        (("          " + line) if i else "          " + line)
        for i, line in enumerate(raw.split("\n"))
    )


def patch_descriptor(doc):
    """Text surgery on the legacy descriptor JSON document (a string in the spec)."""
    doc = sub1(
        doc,
        esc_ascii(OLD_TOOL_DESC),
        esc_ascii(NEW_TOOL_DESC),
        "descriptor tool description",
    )

    # peek property — delete it whole.
    at = doc.find('          "peek": {')
    if at < 0 or doc.count('          "peek": {') != 1:
        fail("descriptor: expected exactly one peek property")
    end = find_object_end(doc, doc.index("{", at))
    if doc[end : end + 2] != ",\n":
        fail("descriptor: peek property is not followed by a comma")
    doc = doc[:at] + doc[end + 2 :]

    # before_ts / before_id — prefix the deprecation note.
    for name in ("before_id", "before_ts"):
        marker = f'          "{name}": {{\n            "description": "'
        doc = sub1(
            doc,
            marker,
            marker + esc_ascii(DESCRIPTOR_CURSOR_PREFIX),
            f"descriptor {name} description",
        )

    # limit — append the start_id/end_id caveat.
    at = doc.find('          "limit": {\n            "description": "')
    if at < 0:
        fail("descriptor: limit property not found")
    sstart = doc.index('"', at + len('          "limit": {\n            "description":'))
    send = find_string_end(doc, sstart)
    doc = doc[: send - 1] + esc_ascii(DESCRIPTOR_LIMIT_SUFFIX) + doc[send - 1 :]

    # ids — the same two edits as the operation parameter. This one block of the
    # descriptor keeps its non-ASCII LITERAL (the rest of the document escapes
    # it), so match and write it the same way its neighbours are written.
    doc = sub1(doc, esc(OLD_IDS_NOT_CONSULTED), esc(NEW_IDS_NOT_CONSULTED),
               "descriptor ids not-consulted sentence")
    doc = sub1(doc, esc(OLD_IDS_WATERMARK), esc(NEW_IDS_WATERMARK),
               "descriptor ids watermark sentence")

    # start_id / end_id — inserted so the property map stays alphabetical.
    def prop(name, description):
        body = json.dumps(
            {"description": description, "type": "string"}, ensure_ascii=True, indent=2
        )
        body = "\n".join(
            (("          " + line) if i else line) for i, line in enumerate(body.split("\n"))
        )
        return f'          "{name}": {body},\n'

    doc = sub1(doc, '          "ids": {', prop("end_id", END_ID_DESC) + '          "ids": {',
               "descriptor ids anchor for end_id")
    doc = sub1(doc, '          "with": {', prop("start_id", START_ID_DESC) + '          "with": {',
               "descriptor with anchor for start_id")
    return doc


def main():
    text = open(SPEC, encoding="utf-8").read()

    if '"name": "start_id"' in text or "T-48" in text:
        fail("spec already carries the T-48 change — refusing to apply twice")

    # ── the GET /api/chat operation, isolated so nothing else can be hit ────
    anchor = '    "/api/chat": {\n      "get": '
    if text.count(anchor) != 1:
        fail("could not find the GET /api/chat operation")
    ostart = text.index(anchor) + len(anchor)
    oend = find_object_end(text, ostart)
    op = text[ostart:oend]

    # ── 1. operation description ───────────────────────────────────────────
    op = sub1(op, esc(OLD_HISTORY_TAIL), esc(NEW_HISTORY_TAIL), "history-paging tail")
    op = sub1(op, esc(OLD_RECEIPT_PARA), esc(NEW_RECEIPT_PARA), "auto read-receipt paragraph")

    # ── 2. parameters ──────────────────────────────────────────────────────
    peek_param = (
        '          {\n'
        '            "in": "query",\n'
        '            "name": "peek",\n'
        '            "required": false,\n'
        '            "schema": {\n'
        '              "anyOf": [\n'
        '                {\n'
        '                  "type": "string"\n'
        '                },\n'
        '                {\n'
        '                  "type": "null"\n'
        '                }\n'
        '              ],\n'
        '              "title": "Peek"\n'
        '            }\n'
        '          },\n'
    )
    op = sub1(op, peek_param, "", "peek parameter object")

    for name in ("before_ts", "before_id"):
        old = f'          {{\n            "in": "query",\n            "name": "{name}",\n'
        new = (
            "          {\n"
            f'            "description": "{esc(DEPRECATED_CURSOR_DESC)}",\n'
            '            "deprecated": true,\n'
            '            "in": "query",\n'
            f'            "name": "{name}",\n'
        )
        op = sub1(op, old, new, f"{name} parameter head")

    marker = '            "name": "before_id",'
    if op.count(marker) != 1:
        fail("could not locate the before_id parameter for insertion")
    obj_start = op.rindex("{", 0, op.index(marker))
    obj_end = find_object_end(op, obj_start)
    if op[obj_end : obj_end + 2] != ",\n":
        fail("before_id parameter is not followed by a comma")
    insert_at = obj_end + 2
    addition = (
        param_block("start_id", "Start Id", START_ID_DESC)
        + ",\n"
        + param_block("end_id", "End Id", END_ID_DESC)
        + ",\n"
    )
    op = op[:insert_at] + addition + op[insert_at:]

    # ── 3. x-mcp ───────────────────────────────────────────────────────────
    # Done BEFORE the two `ids` edits below: the descriptor carries its own copy
    # of those sentences, so patching it first keeps each outer match unique.
    op = sub1(
        op,
        f'          "name": "get_chat",\n          "description": "{esc(OLD_TOOL_DESC)}"',
        f'          "name": "get_chat",\n          "description": "{esc(NEW_TOOL_DESC)}"',
        "x-mcp.description",
    )
    # The operation SUMMARY carries the same one-liner, and that is mechanical
    # rather than cosmetic: spec_catalog_conformance_test.go requires the
    # OpenAPI summary, the routes.go route summary and x-mcp.description to be
    # the same string (server/ocserverd/routes.go is edited to match by hand).
    op = sub1(
        op,
        f'        "summary": "{esc(OLD_TOOL_DESC)}",',
        f'        "summary": "{esc(NEW_TOOL_DESC)}",',
        "operation summary",
    )

    key = '"descriptor": '
    if op.count(key) != 1:
        fail("expected exactly one legacy descriptor")
    sstart = op.index(key) + len(key)
    send = find_string_end(op, sstart)
    doc = json.loads(op[sstart:send])
    op = op[:sstart] + json.dumps(patch_descriptor(doc), ensure_ascii=False) + op[send:]

    # ── 2b. the two `ids` sentences on the operation parameter ─────────────
    op = sub1(op, esc(OLD_IDS_NOT_CONSULTED), esc(NEW_IDS_NOT_CONSULTED),
              "ids not-consulted sentence")
    op = sub1(op, esc(OLD_IDS_WATERMARK), esc(NEW_IDS_WATERMARK),
              "ids watermark sentence")

    open(SPEC, "w", encoding="utf-8").write(text[:ostart] + op + text[oend:])
    verify()


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    op = spec["paths"]["/api/chat"]["get"]

    params = {p["name"]: p for p in op["parameters"]}
    if "peek" in params:
        fail("peek parameter survived")
    for name in ("start_id", "end_id"):
        if name not in params:
            fail(f"{name} parameter missing")
        if params[name]["schema"]["anyOf"] != [{"type": "string"}, {"type": "null"}]:
            fail(f"{name} schema is not the nullable-string shape")
    order = [p["name"] for p in op["parameters"]]
    if order != ["with", "limit", "before_ts", "before_id", "start_id", "end_id",
                 "caller_only", "ids"]:
        fail(f"parameter order is not the expected sequence: {order}")
    for name in ("before_ts", "before_id"):
        if params[name].get("deprecated") is not True:
            fail(f"{name} is not marked deprecated")
        if params[name]["description"] != DEPRECATED_CURSOR_DESC:
            fail(f"{name} description is not the agreed text")
    if params["start_id"]["description"] != START_ID_DESC:
        fail("start_id description is not the agreed text")
    if params["end_id"]["description"] != END_ID_DESC:
        fail("end_id description is not the agreed text")

    desc = op["description"]
    if "AUTO READ-RECEIPT" in desc or "peek" not in desc:
        fail("operation description was not rewritten as expected")
    if "WINDOW BY MESSAGE ID (T-48)" not in desc:
        fail("operation description lost the new window paragraph")
    if "A HISTORY PAGE NEVER ADVANCES" in desc:
        fail("operation description kept the history-page watermark sentence")

    if OLD_IDS_NOT_CONSULTED in params["ids"]["description"]:
        fail("ids description still names peek")
    if NEW_IDS_WATERMARK not in params["ids"]["description"]:
        fail("ids description did not get the new watermark sentence")

    if op["summary"] != NEW_TOOL_DESC:
        fail("operation summary disagrees with x-mcp.description")
    xmcp = op["x-mcp"]
    if xmcp["description"] != NEW_TOOL_DESC:
        fail("x-mcp.description is not the agreed text")
    d = json.loads(xmcp["legacy"]["descriptor"])
    if d["description"] != xmcp["description"] or d["name"] != "get_chat":
        fail("legacy descriptor disagrees with x-mcp")
    props = d["inputSchema"]["properties"]
    if "peek" in props:
        fail("descriptor kept the peek property")
    for name in ("start_id", "end_id"):
        if name not in props:
            fail(f"descriptor is missing {name}")
    if props["start_id"]["description"] != START_ID_DESC:
        fail("descriptor start_id description is not the agreed text")
    if props["end_id"]["description"] != END_ID_DESC:
        fail("descriptor end_id description is not the agreed text")
    for name in ("before_ts", "before_id"):
        if not props[name]["description"].startswith(DESCRIPTOR_CURSOR_PREFIX):
            fail(f"descriptor {name} is missing the deprecation prefix")
    if not props["limit"]["description"].endswith(DESCRIPTOR_LIMIT_SUFFIX):
        fail("descriptor limit is missing the start_id/end_id caveat")
    if "peek" in props["ids"]["description"]:
        fail("descriptor ids description still names peek")

    print(
        "[t48] ok — parameters: "
        + ", ".join(order)
        + f"; descriptor props: {', '.join(sorted(props))}"
    )


if __name__ == "__main__":
    main()
