#!/usr/bin/env python3
"""T-48 — GET /api/chat: the response envelope, the unread backfill, and the
opaque continuation cursor.

Edits spec/openapi.json as TEXT rather than reserialising it, for the reason
bin/t48_spec_chat_window.py states: the file is hand-maintained, its layout is
house style, and a json.dump round-trip would reformat 20k lines and bury the
handful of real changes. Same shape as that script, which is the precedent.

The wire changes, all inside the GET operation of /api/chat plus one new
component schema:
  1. responses.200 — the bare ``ChatMessageDTO`` array becomes ``ChatListDTO``,
     an object ``{messages, next_cursor}``. The array had nowhere to say
     "there is more in this direction"; a caller could only guess from a short
     page, and a filtered page is short for reasons that have nothing to do
     with exhaustion.
  2. components.schemas.ChatListDTO — the new envelope.
  3. parameters — ``cursor``, ``unread``, ``sender``, ``recipient`` added after
     ``end_id``; the ``ids`` "not consulted" sentence names them.
  3b. ``caller_only`` REMOVED (owner ruling rc-09f6d801b2b8: 「定案，caller_only
     也一起廢掉」). It narrowed a listing to the caller, which ``sender`` /
     ``recipient`` now express by naming the id — and unlike this flag they say
     WHICH SIDE. Removed rather than deprecated-and-ignored: a caller still
     sending it now gets a 400 naming the parameter, because this operation
     refuses query parameters it does not declare.
  4. operation.description — the envelope + direction rules, the unread
     backfill (per ``(reader, sender)`` watermark, oldest first, still no
     watermark write), and the two participant-side filters.
  5. x-mcp.description + operation summary + x-mcp.legacy.descriptor — kept
     byte-consistent with each other (gen-mcp-catalog refuses a descriptor
     whose description disagrees with x-mcp.description) and with
     server/ocserverd/routes.go's Summary, which is edited by hand to match
     (spec_catalog_conformance_test.go pins the three together).

The owner ruling behind the envelope (rc-cb3f1b9b0528, verbatim):
「這應該是這隻API就要提供的，要改介面」.

Refuses to run twice. Verifies its own result by re-parsing the spec and
re-checking every change, so a silently corrupted spec cannot pass as success.

Kept in the tree so the diff it produced is reproducible and reviewable, not as
a build step.

🔴 THE PROSE BELOW IS A HISTORICAL RECORD, NOT A SOURCE OF TRUTH. spec/openapi.json
is authoritative; this file holds a second copy of the wording as applied, and
NO drift guard compares the two — so the moment anyone edits those descriptions
in the spec, this copy is stale and nothing says so. Read the spec when you want
the current wording; do not "sync" these strings.
"""

import json
import sys

SPEC = "spec/openapi.json"

# ── 1. operation description ────────────────────────────────────────────────

ENVELOPE_PARA = (
    "RESPONSE ENVELOPE (T-48): every path on this route answers ONE OBJECT —\n"
    "``{\"messages\": [...], \"next_cursor\": \"<opaque>\"}`` — never a bare array.\n"
    "``messages`` carries exactly the rows, in exactly the order, the bare array\n"
    "carried before, on every parameter combination. ``next_cursor`` is an OPAQUE\n"
    "continuation token: hand it back VERBATIM as ``?cursor=`` to get the next\n"
    "page. It is ABSENT (or empty) when there is nothing more IN THAT DIRECTION,\n"
    "and that absence is the ONLY end-of-walk signal — a page shorter than\n"
    "``limit`` is NOT one, because a filter can shorten a page while rows still\n"
    "wait behind it.\n"
    "\n"
    "WHICH DIRECTION a ``next_cursor`` continues in belongs to the path that\n"
    "minted it, because each path has exactly one direction that means anything:\n"
    "* the default newest-page listing — TOWARDS THE OLDER (the same walk\n"
    "  ``before_ts``/``before_id`` does, packed into one string).\n"
    "* ``?unread=true`` — TOWARDS THE NEWER, because that path serves the OLDEST\n"
    "  unread first.\n"
    "* ``?ids=``, ``?start_id=``, ``?end_id=`` — NO ``next_cursor`` AT ALL. Those\n"
    "  three answer a set the caller named and have no defined direction to\n"
    "  continue in; the field is omitted rather than guessed.\n"
    "🔴 THE TOKEN IS OPAQUE and encodes a ``(ts, id)`` POSITION — never an offset\n"
    "and never a row count, so a message landing mid-walk cannot shift a page\n"
    "boundary. Do not parse one, do not construct one, and do not carry one from\n"
    "one direction to the other: an unread cursor sent to a plain listing (or the\n"
    "reverse) is 422, not a silently wrong page. ``cursor`` alongside\n"
    "``before_ts``/``before_id`` or ``start_id``/``end_id`` is 422 for the same\n"
    "reason the two older cursor families already refuse each other.\n"
    "\n"
)

UNREAD_PARA = (
    "UNREAD BACKFILL (T-48): ``?unread=true`` answers YOUR UNREAD ONLY —\n"
    "messages whose ``recipient`` is the verified caller, whose ``sender`` is\n"
    "somebody else, and whose ``ts`` is newer than the caller's read watermark\n"
    "FOR THAT SENDER. OLDEST FIRST, and ``limit`` takes the OLDEST batch — the\n"
    "opposite end from the default listing, because a backfill has to be re-read\n"
    "in the order it was said.\n"
    "🔴 THE WATERMARK IS PER ``(reader, sender)``. ``chat_read`` holds one row per\n"
    "pair, so a message is unread against ITS OWN sender's row and never against\n"
    "a single global high-water mark. A caller who has read everything from A and\n"
    "nothing from B must still be shown B's older messages; folding the two into\n"
    "one number hides exactly those, and hides them SILENTLY — the answer is a\n"
    "short page, which is indistinguishable from having nothing unread.\n"
    "🔴 YOUR OWN MESSAGES ARE NEVER UNREAD (owner ruling), the ones you addressed\n"
    "to yourself included: ``sender == caller`` is excluded outright rather than\n"
    "compared against a watermark.\n"
    "🔴 AND STILL NO WATERMARK WRITE. Reading your unread does not clear it: page\n"
    "through the whole backlog and every line of it is still unread afterwards.\n"
    "``POST /api/chat/mark-read`` is the only thing that clears it.\n"
    "``limit`` keeps this route's LEGACY semantics here, not the window path's\n"
    "1..200 bound: 0 is an empty page and a NEGATIVE limit is uncapped (and then\n"
    "carries no ``next_cursor``, because everything is already in the answer).\n"
    "Any value other than the string ``true`` reads as NOT SENT, matching the\n"
    "string-flag convention this route already used. Sent together with\n"
    "``before_ts``/``before_id`` or ``start_id``/``end_id`` it is 422: those name a\n"
    "position in the whole stream, this names a set defined by your watermarks,\n"
    "and there is no honest way to serve both at once.\n"
    "\n"
    "PARTICIPANT-SIDE FILTERS (T-48): ``?sender=<id>`` keeps only what that id\n"
    "SENT and ``?recipient=<id>`` only what was addressed to it; given together\n"
    "they AND, which pins ONE DIRECTION of one line — something ``with`` cannot\n"
    "express, because ``with`` matches EITHER side. They compose with ``with``,\n"
    "and like every filter here they answer an empty page rather than an error\n"
    "when nothing matches.\n"
    "They also REPLACE the removed ``caller_only`` flag: \"only my own\" is\n"
    "``?sender=<me>`` or ``?recipient=<me>``, which says WHICH SIDE — something\n"
    "the boolean could not, since it matched either.\n"
    "\n"
)

OLD_IDS_NOT_CONSULTED = (
    "``with``, ``limit``, ``before_ts``/``before_id`` and ``start_id``/``end_id`` "
    "are NOT consulted"
)
NEW_IDS_NOT_CONSULTED = (
    "``with``, ``limit``, ``before_ts``/``before_id``, ``start_id``/``end_id``, "
    "``cursor``, ``unread``, ``sender`` and ``recipient`` are NOT consulted, the "
    "answer carries no ``next_cursor``"
)

# ── 2. new parameters ───────────────────────────────────────────────────────

CURSOR_DESC = (
    "Opaque continuation token — copy the previous response's ``next_cursor`` back "
    "VERBATIM. It encodes a ``(ts, id)`` POSITION in the stream, not an offset and "
    "not a row count, so a message posted while you are paging can neither displace "
    "a row you have not read yet nor hide one. The DIRECTION it continues in belongs "
    "to the path that minted it — TOWARDS THE OLDER for the default listing, TOWARDS "
    "THE NEWER for ``unread=true`` — and sending one to the other path is 422 rather "
    "than a silently wrong page. Sending it with ``before_ts``/``before_id`` is 422 "
    "(one keyset walk per request), and so is sending it with "
    "``start_id``/``end_id``, which mint no cursor at all. Never construct or edit "
    "one; an unreadable token is 422, naming the parameter."
)

UNREAD_DESC = (
    "``true`` (the STRING) selects the unread backfill: only messages addressed to "
    "the verified caller, from somebody else, newer than the caller's read watermark "
    "FOR THAT SENDER. ``chat_read`` is per ``(reader, peer)``, and this path compares "
    "each message against ITS OWN sender's row — never against one global watermark, "
    "which would silently drop everything older from a peer you have never opened. "
    "Answered OLDEST FIRST and ``limit`` takes the OLDEST batch, because a backfill is "
    "re-read in the order it was said; ``next_cursor`` therefore continues TOWARDS THE "
    "NEWER. Your own messages are never unread, including ones you addressed to "
    "yourself. Reading this CLEARS NOTHING — no path on this route writes a watermark; "
    "``POST /api/chat/mark-read`` does. Refused with 422 alongside "
    "``before_ts``/``before_id`` or ``start_id``/``end_id``. Any other value reads as "
    "not sent. ``limit`` keeps this route's legacy semantics on this path — 0 is an "
    "empty page, a NEGATIVE limit is uncapped and mints no cursor — not the window "
    "path's 1..200 bound."
)

SENDER_DESC = (
    "Keep only messages this id SENT. Narrower than ``with``, which matches EITHER "
    "side of a message; the two compose (``?with=a&sender=b`` is b's half of the "
    "a-line). It is also how a caller narrows a listing to itself now that the "
    "``caller_only`` flag is gone — and it says WHICH SIDE, which that flag could "
    "not. Not consulted on the ``ids`` path. An id "
    "nobody sent under is not an error — it answers 200 with an empty page, like any "
    "filter that matches nothing."
)

RECIPIENT_DESC = (
    "Keep only messages ADDRESSED TO this id. Given together with ``sender`` the two "
    "AND, which pins one DIRECTION of one line — ``with`` alone cannot express that, "
    "because it matches either side. Not consulted on the ``ids`` path; an id nothing "
    "was addressed to answers 200 with an empty page."
)

# ── 2b. caller_only, removed ────────────────────────────────────────────────

CALLER_ONLY_PARAM = (
    '          {\n'
    '            "description": "When true, return only messages involving both the '
    'verified caller and the optional `with` participant. Omitted or false preserves '
    'the existing participant-wide result.",\n'
    '            "in": "query",\n'
    '            "name": "caller_only",\n'
    '            "required": false,\n'
    '            "schema": {\n'
    '              "default": false,\n'
    '              "title": "Caller Only",\n'
    '              "type": "boolean"\n'
    '            }\n'
    '          },\n'
)

OLD_IDS_DESIGNED = (
    "(designed behaviour; ``caller_only`` is what narrows a listing to the caller)"
)
NEW_IDS_DESIGNED = (
    "(designed behaviour, not a leak)"
)

OLD_IDS_NARROW = (
    "This door now states the listing's rule rather than a stricter one of its own; "
    "``caller_only`` remains the way to narrow a read to yourself."
)
NEW_IDS_NARROW = (
    "This door now states the listing's rule rather than a stricter one of its own. "
    "Narrowing a read to yourself is ``sender``/``recipient`` naming your own id — "
    "the ``caller_only`` flag that used to do it is gone, and unlike the flag those "
    "two say WHICH SIDE."
)

# ── 3. the response envelope ────────────────────────────────────────────────

CHAT_LIST_DESC = (
    "One page of the chat stream plus its continuation token — what EVERY path of "
    "``GET /api/chat`` answers since T-48.\n\nIt replaced a bare ``ChatMessageDTO`` "
    "array, which had nowhere to say \"there is more in this direction\". A caller "
    "could only infer exhaustion from a short page, and a page is short for reasons "
    "that have nothing to do with exhaustion — a participant filter, a "
    "``caller_only`` narrowing, an unread set spread across senders. The envelope "
    "moves that answer from inference to statement."
)

MESSAGES_DESC = (
    "The page, oldest→newest on every path — the SAME rows in the SAME order the bare "
    "array carried before T-48. An empty array is a real answer (a filter that matched "
    "nothing, or the end of a walk), never an error."
)

NEXT_CURSOR_DESC = (
    "Opaque continuation token for the next page IN THIS PATH'S DIRECTION, ABSENT (or "
    "empty) when there is none. Feed it back verbatim as ``?cursor=``. Its absence is "
    "the ONLY end-of-walk signal — a page shorter than ``limit`` is not one. It encodes "
    "a ``(ts, id)`` position and never an offset, so pages cannot shift under a "
    "concurrent post; do not parse or construct one. The default listing continues "
    "TOWARDS THE OLDER and ``unread=true`` TOWARDS THE NEWER; ``ids``, ``start_id`` and "
    "``end_id`` never carry one."
)

CHAT_LIST_SCHEMA = (
    '      "ChatListDTO": {\n'
    '        "description": %s,\n'
    '        "properties": {\n'
    '          "messages": {\n'
    '            "description": %s,\n'
    '            "items": {\n'
    '              "$ref": "#/components/schemas/ChatMessageDTO"\n'
    '            },\n'
    '            "title": "Messages",\n'
    '            "type": "array"\n'
    '          },\n'
    '          "next_cursor": {\n'
    '            "description": %s,\n'
    '            "title": "Next Cursor",\n'
    '            "type": "string"\n'
    '          }\n'
    '        },\n'
    '        "required": [\n'
    '          "messages"\n'
    '        ],\n'
    '        "title": "ChatListDTO",\n'
    '        "additionalProperties": false,\n'
    '        "type": "object"\n'
    '      },\n'
)

OLD_RESPONSE_SCHEMA = (
    '                "schema": {\n'
    '                  "items": {\n'
    '                    "$ref": "#/components/schemas/ChatMessageDTO"\n'
    '                  },\n'
    '                  "title": "Response Handle List Chat Api Chat Get",\n'
    '                  "type": "array"\n'
    '                }\n'
)
NEW_RESPONSE_SCHEMA = (
    '                "schema": {\n'
    '                  "$ref": "#/components/schemas/ChatListDTO"\n'
    '                }\n'
)

# ── 4. x-mcp ────────────────────────────────────────────────────────────────

OLD_TOOL_DESC = (
    "List the chat stream (?with=<id>&limit=<n>; oldest→newest). Window by message id: "
    "start_id walks TOWARDS THE NEWEST from that message, end_id TOWARDS THE OLDEST, "
    "both endpoints inclusive. The older before_ts + before_id keyset cursor still "
    "works but is deprecated. Re-read specific messages by id: ids=<id>&ids=<id> "
    "returns those messages in full. THIS ROUTE NEVER MARKS ANYTHING READ (T-48) — to "
    "mark a conversation read, call mark_read explicitly."
)

NEW_TOOL_DESC = (
    "List the chat stream (?with=<id>&limit=<n>; oldest→newest). Answers an OBJECT "
    "{messages, next_cursor}, never a bare array: next_cursor is opaque, send it back "
    "as cursor= for the next page, and its ABSENCE — not a short page — is the only "
    "'nothing more' signal. Your unread backfill: unread=true returns the OLDEST "
    "unread addressed to you, judged against the per-sender watermark, and still marks "
    "nothing read. Narrow either side with sender= / recipient=. Window by message id: "
    "start_id walks TOWARDS THE NEWEST, end_id TOWARDS THE OLDEST, both inclusive. The "
    "older before_ts + before_id cursor still works but is deprecated. Re-read specific "
    "messages by id: ids=<id>&ids=<id>. THIS ROUTE NEVER MARKS ANYTHING READ (T-48) — "
    "to mark a conversation read, call mark_read explicitly."
)


def fail(msg):
    print(f"[t48-envelope] FAIL: {msg}", file=sys.stderr)
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
    """Render one nullable-string query parameter at the spec's indentation."""
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
    return "\n".join("          " + line for line in raw.split("\n"))


def patch_descriptor(doc):
    """Text surgery on the legacy descriptor JSON document (a string in the spec)."""
    doc = sub1(
        doc,
        esc_ascii(OLD_TOOL_DESC),
        esc_ascii(NEW_TOOL_DESC),
        "descriptor tool description",
    )
    doc = sub1(doc, esc(OLD_IDS_NOT_CONSULTED), esc(NEW_IDS_NOT_CONSULTED),
               "descriptor ids not-consulted sentence")
    doc = sub1(doc, esc(OLD_IDS_DESIGNED), esc(NEW_IDS_DESIGNED),
               "descriptor ids designed-behaviour clause")
    doc = sub1(doc, esc(OLD_IDS_NARROW), esc(NEW_IDS_NARROW),
               "descriptor ids narrow-to-yourself sentence")

    # caller_only — delete the property whole.
    at = doc.find('          "caller_only": {')
    if at < 0 or doc.count('          "caller_only": {') != 1:
        fail("descriptor: expected exactly one caller_only property")
    end = find_object_end(doc, doc.index("{", at))
    if doc[end : end + 2] != ",\n":
        fail("descriptor: caller_only property is not followed by a comma")
    doc = doc[:at] + doc[end + 2 :]

    def prop(name, description):
        body = json.dumps(
            {"description": description, "type": "string"}, ensure_ascii=True, indent=2
        )
        body = "\n".join(
            (("          " + line) if i else line) for i, line in enumerate(body.split("\n"))
        )
        return f'          "{name}": {body},\n'

    # Inserted so the property map stays alphabetical:
    #   ... caller_only, cursor, end_id, ids, limit, recipient, sender, start_id,
    #   unread, with
    doc = sub1(doc, '          "end_id": {',
               prop("cursor", CURSOR_DESC) + '          "end_id": {',
               "descriptor end_id anchor for cursor")
    doc = sub1(doc, '          "start_id": {',
               prop("sender", SENDER_DESC) + '          "start_id": {',
               "descriptor start_id anchor for sender")
    doc = sub1(doc, '          "sender": {',
               prop("recipient", RECIPIENT_DESC) + '          "sender": {',
               "descriptor sender anchor for recipient")
    doc = sub1(doc, '          "with": {',
               prop("unread", UNREAD_DESC) + '          "with": {',
               "descriptor with anchor for unread")
    return doc


def main():
    text = open(SPEC, encoding="utf-8").read()

    if '"name": "unread"' in text or "ChatListDTO" in text:
        fail("spec already carries the T-48 envelope change — refusing to apply twice")

    # ── the new component schema, ahead of ChatMessageDTO (alphabetical) ────
    anchor = '      "ChatMessageDTO": {'
    if text.count(anchor) != 1:
        fail("could not find the ChatMessageDTO component")
    schema = CHAT_LIST_SCHEMA % (
        json.dumps(CHAT_LIST_DESC, ensure_ascii=False),
        json.dumps(MESSAGES_DESC, ensure_ascii=False),
        json.dumps(NEXT_CURSOR_DESC, ensure_ascii=False),
    )
    text = text.replace(anchor, schema + anchor, 1)

    # ── the GET /api/chat operation, isolated so nothing else can be hit ────
    anchor = '    "/api/chat": {\n      "get": '
    if text.count(anchor) != 1:
        fail("could not find the GET /api/chat operation")
    ostart = text.index(anchor) + len(anchor)
    oend = find_object_end(text, ostart)
    op = text[ostart:oend]

    # ── 1. operation description ───────────────────────────────────────────
    op = sub1(op, esc("HISTORY PAGING (scrollback):"),
              esc(ENVELOPE_PARA) + esc("HISTORY PAGING (scrollback):"),
              "history-paging paragraph head")
    op = sub1(op, esc("THIS ROUTE NEVER WRITES A READ WATERMARK"),
              esc(UNREAD_PARA) + esc("THIS ROUTE NEVER WRITES A READ WATERMARK"),
              "watermark paragraph head")

    # ── 2. parameters, inserted after end_id ───────────────────────────────
    marker = '            "name": "end_id",'
    if op.count(marker) != 1:
        fail("could not locate the end_id parameter for insertion")
    obj_start = op.rindex("{", 0, op.index(marker))
    obj_end = find_object_end(op, obj_start)
    if op[obj_end : obj_end + 2] != ",\n":
        fail("end_id parameter is not followed by a comma")
    addition = ",\n".join(
        param_block(*spec) for spec in (
            ("cursor", "Cursor", CURSOR_DESC),
            ("unread", "Unread", UNREAD_DESC),
            ("sender", "Sender", SENDER_DESC),
            ("recipient", "Recipient", RECIPIENT_DESC),
        )
    ) + ",\n"
    op = op[: obj_end + 2] + addition + op[obj_end + 2 :]

    # ── 2b. caller_only, removed ───────────────────────────────────────────
    # Only the parameter object here; the three `ids` sentences that name
    # caller_only are edited in section 5, AFTER the descriptor has been
    # patched — the descriptor carries its own copy of each, so editing the
    # outer text first would find two matches and refuse.
    op = sub1(op, CALLER_ONLY_PARAM, "", "caller_only parameter object")

    # ── 3. the response envelope ───────────────────────────────────────────
    op = sub1(op, OLD_RESPONSE_SCHEMA, NEW_RESPONSE_SCHEMA, "200 response schema")

    # ── 4. x-mcp — BEFORE the outer `ids` edit, so each match stays unique ──
    op = sub1(
        op,
        f'          "name": "get_chat",\n          "description": "{esc(OLD_TOOL_DESC)}"',
        f'          "name": "get_chat",\n          "description": "{esc(NEW_TOOL_DESC)}"',
        "x-mcp.description",
    )
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

    # ── 5. the `ids` not-consulted sentence on the operation parameter ─────
    op = sub1(op, esc(OLD_IDS_NOT_CONSULTED), esc(NEW_IDS_NOT_CONSULTED),
              "ids not-consulted sentence")
    op = sub1(op, esc(OLD_IDS_DESIGNED), esc(NEW_IDS_DESIGNED),
              "ids designed-behaviour clause")
    op = sub1(op, esc(OLD_IDS_NARROW), esc(NEW_IDS_NARROW),
              "ids narrow-to-yourself sentence")

    open(SPEC, "w", encoding="utf-8").write(text[:ostart] + op + text[oend:])
    verify()


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    op = spec["paths"]["/api/chat"]["get"]

    env = spec["components"]["schemas"].get("ChatListDTO")
    if env is None:
        fail("ChatListDTO component missing")
    if env["properties"]["messages"]["items"]["$ref"] != "#/components/schemas/ChatMessageDTO":
        fail("ChatListDTO.messages does not carry ChatMessageDTO rows")
    nc = env["properties"]["next_cursor"]
    if nc["type"] != "string":
        fail("ChatListDTO.next_cursor is not a string")
    if "default" in nc:
        fail("next_cursor must carry NO default: a default makes the generated "
             "TypeScript field REQUIRED, and the field is absent on the wire "
             "whenever a walk has ended")
    if env["required"] != ["messages"]:
        fail("ChatListDTO must require exactly messages")

    ref = op["responses"]["200"]["content"]["application/json"]["schema"]
    if ref != {"$ref": "#/components/schemas/ChatListDTO"}:
        fail(f"200 response is not the envelope: {ref}")

    params = {p["name"]: p for p in op["parameters"]}
    order = [p["name"] for p in op["parameters"]]
    want = ["with", "limit", "before_ts", "before_id", "start_id", "end_id",
            "cursor", "unread", "sender", "recipient", "ids"]
    if order != want:
        fail(f"parameter order is not the expected sequence: {order}")
    for name, desc in (("cursor", CURSOR_DESC), ("unread", UNREAD_DESC),
                       ("sender", SENDER_DESC), ("recipient", RECIPIENT_DESC)):
        if params[name]["description"] != desc:
            fail(f"{name} description is not the agreed text")
        if params[name]["schema"]["anyOf"] != [{"type": "string"}, {"type": "null"}]:
            fail(f"{name} schema is not the nullable-string shape")

    desc = op["description"]
    for needle in ("RESPONSE ENVELOPE (T-48)", "UNREAD BACKFILL (T-48)",
                   "PARTICIPANT-SIDE FILTERS (T-48)",
                   "THE WATERMARK IS PER ``(reader, sender)``"):
        if needle not in desc:
            fail(f"operation description is missing: {needle}")
    if OLD_IDS_NOT_CONSULTED in params["ids"]["description"]:
        fail("ids description still carries the old not-consulted sentence")
    if NEW_IDS_NOT_CONSULTED not in params["ids"]["description"]:
        fail("ids description did not get the new not-consulted sentence")
    if "caller_only" in params:
        fail("the caller_only parameter survived")

    if op["summary"] != NEW_TOOL_DESC:
        fail("operation summary disagrees with x-mcp.description")
    xmcp = op["x-mcp"]
    if xmcp["description"] != NEW_TOOL_DESC:
        fail("x-mcp.description is not the agreed text")
    d = json.loads(xmcp["legacy"]["descriptor"])
    if d["description"] != xmcp["description"] or d["name"] != "get_chat":
        fail("legacy descriptor disagrees with x-mcp")
    props = d["inputSchema"]["properties"]
    if "caller_only" in props:
        fail("the descriptor kept the caller_only property")
    if list(props) != sorted(props):
        fail(f"descriptor properties are no longer alphabetical: {list(props)}")
    for name, want_desc in (("cursor", CURSOR_DESC), ("unread", UNREAD_DESC),
                            ("sender", SENDER_DESC), ("recipient", RECIPIENT_DESC)):
        if name not in props:
            fail(f"descriptor is missing {name}")
        if props[name]["description"] != want_desc:
            fail(f"descriptor {name} description is not the agreed text")
    if NEW_IDS_NOT_CONSULTED not in props["ids"]["description"]:
        fail("descriptor ids description did not get the new not-consulted sentence")

    print(
        "[t48-envelope] ok — parameters: "
        + ", ".join(order)
        + f"; descriptor props: {', '.join(props)}"
    )


if __name__ == "__main__":
    main()
