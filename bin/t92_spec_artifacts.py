#!/usr/bin/env python3
"""T-92 — split a task artifact's single ``label`` into ``name`` + ``description``,
put every artifact behind one blob, and reduce a task response's artifacts to a COUNT.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style (compact leaf objects, deliberate
non-alphabetical grouping); a json.dump round-trip would reformat all 19k lines
and bury the real changes. Precedent: bin/t646a_spec_update_task.py,
bin/t6cce_add_param_desc.py — same shape, earlier tickets.

WHAT IT CHANGES (spec only — no Go, no generator run, no spec/mcp-catalog.json,
which is a bin/gen-mcp-catalog OUTPUT and is regenerated, never hand-edited):

  TaskArtifactDTO         label/attachment_id/filename/is_image OUT,
                          name/description IN  → 9 fields.
  TaskArtifactRefDTO      DELETED — nothing references it once a task response
                          carries no artifact rows.
  TaskArtifactListDTO     artifacts_detail_level KEPT and REDEFINED (ticket T-92
                          §4 risk ④ ruling: "從 task 那一側移除；list 的 full
                          留著但換掉定義"); its contrasting value is gone, which
                          is the same shape ``notes_included`` already has.
  TaskDTO                 artifacts + artifacts_detail_level OUT,
                          artifact_count IN (same field TaskListItemDTO carries).
  TaskArtifactInputDTO    label OUT; name IN and REQUIRED; description IN.
  TaskArtifactReplaceInputDTO   label OUT; name/description IN, omitted = carried
                          forward.
  TaskArtifactVersionDTO  label OUT, name/description IN — forced, because the
                          history table stores those two columns now. Deliberately
                          NOT narrowed further: that was outside what the owner
                          approved (card rc-210fc77beea1, option 0).
  Two NEW routes          task-scoped raw-body upload (add and replace), the
                          one-call main path the owner picked on rc-210fc77beea1.
                          x-mcp include:false — a binary ingest seam, like
                          POST /api/chat/attachments, not a tool. No MCP order
                          renumbering is needed for an excluded operation.
  Route prose             every sentence that described the old id+label index,
                          on get_task / add / replace / remove / list / history.

Refuses to run twice. Re-parses its own result and re-checks the invariants the
drift guards check, so a silently corrupted spec cannot pass as success.

🔴 THE PROSE BELOW IS APPLIED, NOT MIRRORED. spec/openapi.json is authoritative
the moment this script has run; nothing compares the two afterwards.
"""

import json
import sys

SPEC = "spec/openapi.json"


def fail(msg):
    print(f"[t92] FAIL: {msg}", file=sys.stderr)
    sys.exit(1)


def sub1(s, old, new, what):
    """Replace `old` exactly once. Anything else is a bug in this script."""
    n = s.count(old)
    if n != 1:
        fail(f"{what}: expected exactly 1 occurrence, found {n}")
    return s.replace(old, new)


def json_text(value):
    """The two ways this file can spell a JSON string value."""
    return (json.dumps(value, ensure_ascii=False), json.dumps(value, ensure_ascii=True))


def replace_string_value(s, old, new, what, expect):
    """Swap a JSON string VALUE wherever the file spells it, ascii-escaped or not."""
    hit = 0
    for o, n in zip(json_text(old), json_text(new)):
        c = s.count(o)
        if c:
            s = s.replace(o, n)
            hit += c
        if json_text(old)[0] == json_text(old)[1]:
            break
    if hit != expect:
        fail(f"{what}: replaced {hit} occurrences, expected {expect}")
    return s


def swap_descriptor_text(old_desc, old_text, new_text):
    """Swap the description token INSIDE a descriptor string, keeping its own escaping.

    Some descriptors were minted with ensure_ascii on and some with it off, and at least
    one is MIXED (an em-dash escaped, other characters not), so it cannot be re-dumped
    byte-identically. When only the prose changes, replacing the one token is exact.
    """
    for ensure in (False, True):
        tok = json.dumps(old_text, ensure_ascii=ensure)
        if old_desc.count(tok) == 1:
            return old_desc.replace(tok, json.dumps(new_text, ensure_ascii=ensure))
    # Mixed escaping: fall back to the LINE, which json.dumps(indent=2) always puts the
    # whole description on. Re-spelling it fully escaped changes no parsed value.
    lines = old_desc.split("\n")
    head = '      "description": '
    if len(lines) < 2 or not lines[1].startswith(head) or not lines[1].endswith(","):
        fail("descriptor's description is not the single line this fallback needs")
    if json.loads(lines[1][len(head):-1]) != old_text:
        fail("descriptor line 2 is not the description this script read")
    lines[1] = head + json.dumps(new_text, ensure_ascii=True) + ","
    return "\n".join(lines)


def rebuild_descriptor(old_desc, mutate):
    """Rebuild an x-mcp.legacy.descriptor string byte-identically apart from the edit."""
    obj = json.loads(old_desc)
    for ensure in (False, True):
        out = json.dumps(obj, indent=2, ensure_ascii=ensure)
        out = "\n".join((" " * 4 + l if i else l) for i, l in enumerate(out.split("\n")))
        if out == old_desc:
            mutate(obj)
            out = json.dumps(obj, indent=2, ensure_ascii=ensure)
            return "\n".join((" " * 4 + l if i else l) for i, l in enumerate(out.split("\n")))
    fail("legacy descriptor does not round-trip; refusing to rewrite it")


# ─────────────────────────────────────────────────────────────────────────────
# Canonical prose. Each route's text is stored in FOUR places in this file
# (description, summary, x-mcp.description, x-mcp.legacy.descriptor); they are
# byte-identical today and this script keeps them that way.
# ─────────────────────────────────────────────────────────────────────────────

ARTIFACT_DTO_DESC = (
    "One pinned deliverable on a task's artifact set (T-3dc5, reshaped by T-92). ``kind`` is the closed set "
    "file|image|link and it is IMMUTABLE across versions. EVERY artifact — a link included — is backed by one "
    "chat_attachment blob (owner ruling c-59fc5834d967): a link's target is stored as a ``text/uri-list`` blob. That "
    "is why ``url`` here has exactly ONE meaning — WHERE TO GO FOR THIS DELIVERABLE'S CONTENT: the blob serve path "
    "(``/api/chat/attachment/{attachment_id}``) for a file/image, the external address for a link. The blob id is no "
    "longer echoed as a field of its own; it is the tail of ``url``, and one string said twice is the drift this DTO "
    "was reshaped to remove. ``name`` is the display name and is NEVER EMPTY ON THE WIRE — the stored name when the "
    "row has one, else the blob's own filename (file/image), else the link target (link), else ``#`` + the id without "
    "its ``ta-`` prefix. It is derived READ-TIME, so replacing the content changes the name with it instead of "
    "leaving a filename copied into a second place where it can go stale. ⚠️ A DERIVED ``name`` IS NOT BOUND BY THE "
    "48-RUNE WRITE CAP — that cap is a gate on what you may store, never a promise about what you will read back. "
    "``description`` is the prose the single old ``label`` used to carry alongside the title — what a reader reads to "
    "decide whether this is the artifact they want — and it MAY BE EMPTY and MAY EXCEED 256 runes: that cap binds new "
    "writes only, and the labels migrated into this field were written before any cap existed. ``mime`` is the blob's "
    "own content type, resolved read-time and honest-empty when the blob is gone; it is the ONLY field that separates "
    "a ``.md`` from a ``.pdf`` from a ``.zip``, which ``kind`` cannot do — a reader that drops it renders the other "
    "two wrongly and silently. ``created_by``/``created_ts`` are who last WROTE this artifact and when — the "
    "registrar and the moment of pinning until someone replaces it, the REPLACER and the moment of replacement "
    "afterwards (T-60 rewrites both in place; neither field is a record of the original pin). ``version_count`` "
    "(T-60) is how many versions of this deliverable exist, the live one INCLUDED — 1 for an artifact that has never "
    "been replaced, and bounded above because only the most recent few replaced versions are retained; list them with "
    "GET /api/tasks/{task_id}/artifact/{artifact_id}/history."
)

NAME_PROP_DESC = (
    "The deliverable's display name. NEVER EMPTY on the wire — and that is a read-time DERIVATION rather than a "
    "stored guarantee (T-92): the stored name when the row has one, else the blob's filename for a file/image, else "
    "the link target for a link, else ``#`` + the id without its ``ta-`` prefix. ⚠️ A derived name is NOT capped: the "
    "48-rune limit gates what may be STORED and says nothing about this value."
)

DESCRIPTION_PROP_DESC = (
    "Prose about this deliverable, for a reader deciding whether it is the one they want (T-92 — the half of the old "
    "``label`` that was not a title). MAY BE EMPTY, and MAY BE LONGER THAN THE 256-RUNE WRITE CAP: the cap binds new "
    "writes only and never touched the values migrated in from ``label``. Do not size a buffer or a column on 256."
)

URL_PROP_DESC = (
    "Where to go for this deliverable's content — ONE meaning, both kinds (T-92): the blob serve path "
    "(``/api/chat/attachment/{attachment_id}``) for a file/image, the external address for a link. Resolved "
    "read-time, and honest-empty when a file/image's blob is gone."
)

MIME_PROP_DESC = (
    "The blob's own content type, resolved read-time and empty when the blob is gone — never fabricated. It is the "
    "only field that distinguishes ``.md`` from ``.pdf`` from ``.zip``: ``kind`` = ``file`` covers all three, so a "
    "client that substitutes a default here displays the other two wrongly and says nothing. A link's blob is "
    "``text/uri-list``."
)

LIST_DTO_DESC = (
    "One task's pinned deliverables IN FULL (T-66, reshaped by T-92) — the answer of "
    "``GET /api/tasks/{task_id}/artifacts`` / MCP ``list_task_artifacts``, and since T-92 the ONLY call that returns "
    "an artifact ROW at all: a task response carries ``artifact_count`` and nothing else. ``artifacts`` holds EVERY "
    "artifact on the task, oldest→newest, each a complete ``TaskArtifactDTO``; an empty set is ``[]``, never a 404."
)

LIST_DETAIL_LEVEL_DESC = (
    "What this response IS, said by the response itself: always ``full`` — every artifact row here is complete, no "
    "field held back and no row abridged. ⚠️ SINCE T-92 IT HAS NO OPPOSITE: it used to stand against the ``index`` a "
    "task response declared, and a task response now carries a count and no rows at all. It is a self-description "
    "without a contrasting value, the same shape ``notes_included`` has, and it is kept so a reader holding this "
    "payload does not have to know which server version produced it to know the rows are whole."
)

TASK_ARTIFACT_COUNT_DESC = (
    "HOW MANY deliverables are pinned on this task — and, since T-92, ALL that a task response says about them. There "
    "is no ``artifacts`` array here any more, and no ids in it either: rows, ids and names all come from "
    "``list_task_artifacts(task_id)``, which answers the WHOLE ticket in one call. The count is EXACT, has no ceiling "
    "and is never truncated — 0 means the task genuinely has nothing pinned — the same promise ``note_size_chars`` "
    "makes for a step's note (T-66). WHY NOT EVEN THE IDS, when they are only a few characters each: the id list is a "
    "SET whose length grows with the age of the ticket and never shrinks, because deliverables are only ever added — "
    "putting it here would reintroduce, one migration later, the unbounded per-read cost this field exists to remove. "
    "And a caller holding an id is a caller about to act on that artifact, which needs the row anyway. Ask for the "
    "list when you want the list."
)

LIST_ITEM_ARTIFACT_COUNT_DESC = (
    "How many deliverables are pinned on this task — the same field, with the same exact-count promise, that the full "
    "task response carries (``TaskDTO.artifact_count``). It predates T-92 on this light projection; T-92 made the two "
    "responses agree by giving the full one a count as well, instead of an array."
)

VERSION_DTO_DESC = (
    "ONE retained PREVIOUS version of a pinned deliverable (T-60, fields tracked to T-92). Unlike a document revision "
    "this row carries the version WHOLE rather than a size summary: an artifact version is a pointer (a blob id or a "
    "url) plus its name and its prose, so there is nothing to hold back and the listing IS the content. ``id`` is the "
    "version's own row id, ascending with the age of the write; ``kind`` always equals the live artifact's kind, "
    "which cannot change across versions; ``created_ts``/``created_by`` are when THAT version was written and by "
    "whom. ``name``/``description`` are that version's own — T-92 split the single ``label`` this row used to carry "
    "into the two of them, here as well as on the live artifact, because the history table stores those two columns "
    "now. A file/image version's ``attachment_id`` still resolves — the blob is kept alive for as long as the version "
    "is retained, and collected when the version falls off the end — and ``url``/``mime``/``filename``/``is_image`` "
    "echo that blob, resolved read-time exactly like the live artifact's. ⚠️ THIS ROW IS DELIBERATELY WIDER THAN THE "
    "LIVE ``TaskArtifactDTO``, which T-92 narrowed: this is a cockpit-only read of a bounded handful of rows rather "
    "than a cost paid on every ticket read, and narrowing it was outside what the owner approved."
)

VERSION_NAME_DESC = (
    "This version's display name AS STORED (T-92). Unlike the live artifact's ``name`` this one is NOT derived — it "
    "is the column, so it is empty on a version written before names existed."
)

VERSION_DESCRIPTION_DESC = (
    "This version's prose (T-92 — the half of the old ``label`` that was not a title). May be empty, and may exceed "
    "the 256-rune write cap for the same reason the live artifact's may: the cap never touched migrated values."
)


GET_TASK_TEXT = (
    "Read one task — and read it knowing it is a SUMMARY, not the whole of it: the response says so itself "
    "(``detail_level`` = ``summary``, ``notes_included`` = false). WHAT IS COMPLETE HERE: the task's own fields, its "
    "deps, its progress counts, its gate cards, and EVERY ONE of its steps. The step list has no cap, no paging and "
    "no truncation of any kind — the rows you get back are all the rows there are, so a step that is not here does "
    "not exist on this task. WHAT IS OMITTED, AND EXACTLY HOW MUCH OF IT: each step's working-note TEXT (T-66). In "
    "its place every step carries ``note_size_chars`` — the EXACT number of characters of note sitting on the server "
    "for that step, where 0 means that step genuinely has no note — and ``note_cap_chars``, the ceiling. A positive "
    "``note_size_chars`` is a precise promise that that many characters are waiting for you, and "
    "``get_task_step(task_id, step_id)`` is the one call that returns them, one step at a time. Read the sizes first, "
    "then fetch only the notes you actually need. THE PINNED DELIVERABLES ARE OMITTED THE SAME WAY, AND SINCE T-92 "
    "THERE IS NOT EVEN AN INDEX OF THEM: ``artifact_count`` is the only thing said about them here — an EXACT, "
    "un-truncated, un-capped count, 0 meaning the task genuinely has nothing pinned. No array, no ids, no names: "
    "``list_task_artifacts(task_id)`` returns every artifact on the ticket, complete, in ONE call, and there is "
    "deliberately no per-artifact read. Ask for that list when you are going to USE an artifact; a count is what you "
    "need to know one exists. Unknown id → 404."
)

ADD_ARTIFACT_TEXT = (
    "Register a deliverable (file, image, or link) onto the task's artifact set — the pinned deliverables shown on "
    "the task card. This verb only ADDS, and is repeatable: call it again to pin one more. To change what an "
    "ALREADY-PINNED deliverable points at, use replace_task_artifact instead of remove+add: it keeps the artifact id. "
    "THIS JSON DOOR IS NOT THE MAIN PATH FOR A LOCAL FILE. Pinning bytes you have on disk is one call — the "
    "task-scoped upload (POST /api/tasks/{task_id}/artifacts/upload, raw body) — which stores and pins in the same "
    "breath and hands back the artifact id. Upload-then-bind is two steps, and a caller who does the first and not "
    "the second leaves a blob nothing points at, which nothing goes looking for either. Use THIS call for a link "
    "(kind=link + url), or to pin a blob that is ALREADY in the store — an attachment someone sent you in chat, a "
    "file you pinned elsewhere — with kind=file|image + attachment_id, which is what that field is for now: reusing "
    "an existing blob rather than uploading a second copy of the same bytes. name is REQUIRED and is the display name "
    "(a link title such as \"PR #123\", a report's title), capped at 48 characters — Unicode runes, so 48 CJK "
    "characters fit — and a blank one is refused. description is optional prose about what this deliverable IS and "
    "why it is worth opening, capped at 256 runes; it is what the next reader has to go on, because a task response "
    "carries only a COUNT of artifacts. Both caps refuse rather than truncate, and both bind NEW writes only — "
    "artifacts pinned before they existed keep whatever they have. Answers with a bounded receipt (task_id, "
    "artifact_id, artifact_count), not the whole task."
)

REPLACE_ARTIFACT_TEXT = (
    "Replace the CONTENT of one already-pinned deliverable while its artifact id stays exactly the same — the card "
    "keeps pointing at the same artifact and what sits behind it changes. Use this instead of remove+add whenever you "
    "are shipping a corrected version of something you already pinned: remove+add mints a NEW id, so anyone holding "
    "the old one is left pointing at nothing. For a file/image whose new bytes are on disk the one-call door is the "
    "task-scoped upload (POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload, raw body); use THIS call to "
    "point a file/image at a blob already in the store (attachment_id), or to change a link's target (url). THE KIND "
    "CANNOT CHANGE ACROSS VERSIONS: a file artifact stays a file artifact, so sending a url for one (or an "
    "attachment_id for a link, or an explicit kind that differs from what is pinned) is a 400 — un-pin it and "
    "register a new artifact if the kind is what you meant to change. name and description are optional here and an "
    "omitted one is CARRIED FORWARD: a replacement is a corrected version of the same deliverable, so you never "
    "re-type either just to swap the content. Sending one replaces it, and the length caps (48 runes for name, 256 "
    "for description) are checked ONLY against a value you actually send — omit the field and whatever is stored "
    "stands, however long it is. A blank name is refused, because every deliverable has a name; a blank description "
    "clears it. ⚠️ Some clients serialise an empty string as an omitted field, so \"omit to keep\" is reliable and "
    "\"send blank to clear\" is not — do not build on the latter. The version you replaced is KEPT and readable, but "
    "only the most recent few are retained: the oldest falls off the end for good when a newer one arrives, and the "
    "file it pointed at is deleted with it, so a version that has scrolled off is not recoverable from anywhere. ONLY "
    "WHILE THE TASK IS STILL OPEN: once a task closes (done / terminated / duplicated) its deliverable set is frozen "
    "in every direction — replace is refused with the same 409 as add and remove, and admin/owner are not exempt. "
    "Answers with a bounded receipt (task_id, artifact_id, artifact_count, version_count), not the whole task."
)

LIST_ARTIFACTS_TEXT = (
    "Read one task's pinned deliverables IN FULL — and since T-92 the ONLY call that returns an artifact row at all: "
    "``get_task`` answers ``artifact_count`` and nothing else, no ids and no names. Answers ``{task_id, "
    "artifacts_detail_level, artifacts}`` where every artifact on the task is present, oldest→newest, complete: "
    "``id``, ``kind`` (file|image|link), ``name`` (never empty — derived read-time from the blob's filename or the "
    "link target when the row has no stored name), ``description`` (the prose, possibly empty and possibly longer "
    "than the 256-rune write cap), ``url`` (where to go for the content — the blob serve path for a file/image, the "
    "external address for a link), ``mime`` (the blob's own content type, the only field that separates .md from .pdf "
    "from .zip), ``created_ts``, ``created_by`` and ``version_count``. ONE call answers the WHOLE ticket, and that is "
    "deliberate — there is no per-artifact read, because whoever opens a task's deliverables wants the set (a "
    "32-artifact ticket would otherwise cost 32 calls), whereas a step note is read one at a time and "
    "``get_task_step`` is per-step for exactly that reason. Blob metadata is resolved read-time and is honest-empty "
    "when the underlying blob is gone — never fabricated. A task with nothing pinned answers ``artifacts: []``, not a "
    "404; an unknown task id is a 404. Same read floor as ``get_task``: any authenticated principal may read any "
    "task's artifacts, and no field here was behind a stricter door before."
)

REMOVE_ARTIFACT_DESC = (
    "Un-pin one artifact from a task's set (MCP ``remove_task_artifact``). SAME permission model as add (owner ruling "
    "2026-07-18 — the executing agent removes its OWN task's deliverables): requires the executing agent — caller "
    "must be the task's executor, admin capability (owner/admin agent) excepted. Returns a BOUNDED receipt "
    "(``TaskArtifactReceiptDTO``: the removed artifact's id plus the resulting count) — not the task; pull GET "
    "/api/tasks/{task_id}/artifacts (MCP ``list_task_artifacts``) for the artifact list, which since T-92 is the only "
    "call that carries one — GET /api/tasks/{task_id} answers a COUNT. The LIVE row's chat_attachment blob is left "
    "intact (it may be shared with a chat message), but the delete does not stop at the live row: every retained "
    "version of this artifact (``task_artifact_history``) is deleted in the SAME transaction and the blobs that only "
    "those versions referenced are collected, so un-pinning a replaced artifact destroys its version history and "
    "those versions' files for good. SYMMETRIC with add and, since T-60, with replace (owner ruling 2026-07-25): a "
    "closed task's deliverable set is frozen in EVERY direction — an add-only freeze made un-pin an unrecoverable "
    "loss, since the deliverable could be taken off a closed card and never put back. Like add's, the freeze sits "
    "AFTER the permission check, so admin/owner are not exempt. Guards: 404 unknown task → 403 not the executor → 409 "
    "terminal task (a closed task's deliverables are frozen) → 404 unknown artifact → 400 the artifact belongs to a "
    "different task."
)

REMOVE_ARTIFACT_TOOL_TEXT = (
    "Un-pin (remove) one artifact from a task's artifact set — the counterpart to add_task_artifact. You may remove "
    "artifacts from a task you are the executor of (the owner/assistant may remove on any task). Give the task id and "
    "the artifact id — the id returned when it was added, or from list_task_artifacts, which since T-92 is where "
    "artifact ids come from: get_task answers a count and carries none. The LIVE file blob is left intact, and on an "
    "artifact that was never replaced only the pin on the card is removed. BUT IF YOU HAD REPLACED IT, un-pinning "
    "also destroys its past: every retained version of this artifact is deleted in the same breath, and the files "
    "only those versions pointed at go with them, unrecoverably. ONLY WHILE THE TASK IS STILL OPEN: once a task "
    "closes (done / terminated / duplicated) its deliverable set is frozen in every direction — remove is refused "
    "with the same 409 as add and replace. So swap a deliverable BEFORE you close the task, not after; after the "
    "close it can neither be removed nor put back. Answers with a bounded receipt (task_id, artifact_id, "
    "artifact_count), not the whole task."
)

HISTORY_OLD_SENTENCE = (
    "The plain task read (GET /api/tasks/{task_id}) makes no caller distinction at all and its response already "
    "carries the artifact set, so gating the version history on being the executor would leave one door refusing what "
    "the other hands over."
)
HISTORY_NEW_SENTENCE = (
    "The artifact set itself is readable by anyone authenticated (GET /api/tasks/{task_id}/artifacts makes no caller "
    "distinction at all), so gating the version history on being the executor would leave one door refusing what the "
    "other hands over."
)

UPLOAD_ADD_TEXT = (
    "Pin a LOCAL file or image onto this task as a deliverable in ONE call (T-92, owner card rc-210fc77beea1): the "
    "raw request body IS the bytes (``application/octet-stream``; NOT base64, NOT multipart), the server stores the "
    "blob AND registers the artifact in the same transaction, and the answer is the ordinary add receipt — the new "
    "artifact's id plus the resulting count. THIS IS THE MAIN PATH for bytes on disk, and the reason is not "
    "convenience: upload-then-bind is TWO steps with a gap in the middle, and a caller who takes the first and not "
    "the second leaves a blob that nothing references and that nothing goes looking for — the collector runs when a "
    "retained version falls off the end, not as a sweep. One call has no such gap. ``?name=`` is REQUIRED (48 runes, "
    "refused not truncated, blank refused) and ``?description=`` optional (256 runes); ``?filename=`` and ``?mime=`` "
    "describe the BLOB exactly as they do on the chat-attachment upload, with an omitted mime falling back to a "
    "magic-byte image sniff and then ``application/octet-stream``. The request ``Content-Type`` header is "
    "deliberately IGNORED — clients default it to ``application/octet-stream``, indistinguishable from a real "
    "declaration; ``?mime=`` is the explicit channel. ``kind`` is not a parameter: an image mime pins ``image``, "
    "anything else pins ``file``. Size caps are the chat upload's exactly (one mechanism, not two): 20 MB for an "
    "``image/*`` blob, 100 MB otherwise, with an over-cap or empty body a flat 400. Permission and freeze are add's "
    "exactly: the task's executor (admin excepted), 409 on a terminal task. Excluded from the MCP tool surface — a "
    "binary ingest seam like the chat-attachment upload, not a tool; ``add_task_artifact`` remains the JSON door for "
    "a link, or for reusing a blob already in the store."
)

UPLOAD_REPLACE_TEXT = (
    "Replace a pinned file/image deliverable's content from a LOCAL file in ONE call (T-92) — the raw-body twin of "
    "``replace_task_artifact``, keeping the artifact id exactly as that verb does. The request body IS the new bytes "
    "(``application/octet-stream``), the server stores the blob and swaps the live row in the same transaction, and "
    "the answer is the ordinary replace receipt (task_id, artifact_id, artifact_count, version_count). It exists for "
    "the same reason the add-side upload does: upload-then-replace leaves an unreferenced blob behind whenever the "
    "second step does not happen. ``?name=`` and ``?description=`` are OPTIONAL and an omitted one is CARRIED "
    "FORWARD, exactly as on the JSON replace; ``?filename=``/``?mime=`` describe the new blob. THE KIND CANNOT "
    "CHANGE: this route refuses a LINK artifact with a 400 rather than converting it, and the sniffed image/file "
    "distinction must match what is pinned. Permission, freeze, retention and blob collection are the JSON replace's "
    "exactly. Excluded from the MCP tool surface — a binary ingest seam, not a tool."
)


ADD_NAME_DESC = (
    "REQUIRED (T-92, owner ruling rc-85b07ab98651 \u300c\u73fe\u5728\u958b\u59cb\u4efb\u52d9\u7522\u7269\u90fd\u9700\u8981\u6709\u500b\u540d\u5b57\uff0c\u820a\u7684\u4e0d\u7ba1\u300d). The name this deliverable is "
    "LISTED under - short enough to read in a row, e.g. \"PR #428\" or \"migration rollback plan\". At most 48 "
    "characters, counted in Unicode runes so 48 CJK characters fit; a longer one is REFUSED with a 400 rather than "
    "truncated, and so is a blank or whitespace-only one. THE REQUIREMENT BINDS NEW WRITES ONLY: artifacts pinned "
    "before it existed keep whatever they have, including nothing, so a reader must NOT assume a stored name is "
    "present - what makes the name non-empty on the way out is the read-time derivation, not this rule."
)

ADD_DESCRIPTION_DESC = (
    "OPTIONAL prose about what this deliverable IS and why it is worth opening - the half of the old ``label`` that "
    "was not a title (T-92). It is what the next reader has to go on, because a task response carries only a COUNT of "
    "artifacts: an unexplained row costs whoever finds it a download to learn what it was. At most 256 runes, refused "
    "rather than truncated. Omitting it is not an error and the artifact is pinned with an empty description. The cap "
    "binds NEW writes only, so values already stored may be far longer."
)

ADD_ATTACHMENT_ID_DESC = (
    "A blob ALREADY in the store, to pin without uploading a second copy of the same bytes - an attachment someone "
    "sent you in chat, a file already pinned elsewhere. Read ONLY when kind is 'file' or 'image', where one of this "
    "and the raw-body upload route must have supplied the content; an id that resolves to no stored blob is a 400. "
    "\u26a0\ufe0f SINCE T-92 THIS IS NO LONGER THE MAIN WAY TO PIN A LOCAL FILE: uploading to the chat store and then "
    "binding here is two steps, and a caller that takes the first and not the second leaves a blob nothing "
    "references and nothing goes looking for. Send the bytes to POST /api/tasks/{task_id}/artifacts/upload instead "
    "and the store-and-pin happens in one transaction."
)

ADD_URL_DESC = (
    "The link target, read ONLY when kind is 'link' - where it IS required, a blank one being a 400. It is never "
    "parsed and never fetched: any non-empty string is accepted as sent, so a typo is pinned as a deliverable that "
    "leads nowhere. Since T-92 the server stores it as a ``text/uri-list`` blob like any other artifact's content, "
    "which the caller never sees and never needs to. With kind 'file' or 'image' this field is not merely optional "
    "but never looked at at all, so a file artifact sent with a url and no content is refused for the MISSING "
    "content, not for the field you actually filled in."
)

REPLACE_NAME_DESC = (
    "The NEW version's display name. OPTIONAL, and an omitted one CARRIES THE PINNED NAME FORWARD - a replacement is "
    "a corrected version of the same deliverable, so you never re-type the display name just to swap the content - "
    "and JSON null is read as omitted rather than as a value. Sending one replaces it, and the 48-rune cap is "
    "checked ONLY against a value you actually send: omit the field and whatever is stored stands, however long it "
    "is. A blank one is REFUSED, because every deliverable has a name. \u26a0\ufe0f Some clients serialise an empty "
    "string as an omitted field, so \"omit to keep\" is reliable in a way \"send blank\" is not."
)

REPLACE_DESCRIPTION_DESC = (
    "The NEW version's prose. OPTIONAL, omitted = carried forward, JSON null read as omitted, 256-rune cap checked "
    "only against a value actually sent (T-92). Unlike ``name`` a blank one is ACCEPTED and CLEARS it - plenty of "
    "deliverables need no explanation - but see the warning on ``name``: a client that turns an empty string into an "
    "omitted field will silently keep the old prose instead, so do not build on clearing."
)

ADD_INPUT_DTO_DESC = (
    "Register one artifact onto a task (MCP ``add_task_artifact``). ``kind`` is required: file|image|link. For a "
    "LINK, ``url`` is required - a bare http(s) URL. For a FILE/IMAGE, ``attachment_id`` names a blob ALREADY in the "
    "store; bytes that are still on disk go to POST /api/tasks/{task_id}/artifacts/upload instead, which stores and "
    "pins in one call and is the main path since T-92 - two-step upload-then-bind is what leaves unreferenced blobs "
    "behind. ``name`` is REQUIRED (48 runes, blank refused) and ``description`` is optional (256 runes); both caps "
    "refuse rather than truncate and both bind NEW writes only."
)

REPLACE_INPUT_DTO_DESC = (
    "Replace one pinned artifact's content in place (MCP ``replace_task_artifact``). The id does not move; the "
    "content does. Send ``attachment_id`` for a file/image artifact (a blob already in the store - new bytes on disk "
    "go to the raw-body replace/upload route instead) or ``url`` for a link artifact - whichever the artifact's "
    "EXISTING kind calls for, since the kind cannot change across versions. ``kind`` is optional and is an ASSERTION "
    "rather than an instruction: when present it must equal the pinned kind, so a caller that believes it is "
    "replacing a link is told it is wrong instead of being handed a 400 about some other field. ``name`` and "
    "``description`` are optional and an OMITTED one CARRIES THE PINNED VALUE FORWARD; an explicit blank ``name`` is "
    "refused and an explicit blank ``description`` clears it, and JSON null counts as absent in both."
)

ADD_ROUTE_DESC = (
    "Register a deliverable onto the task's artifact set (MCP ``add_task_artifact``; requires the executing agent - "
    "caller must be the task's executor, admin capability excepted). This verb only ADDS, and is repeatable: each "
    "call pins one more artifact; to change what an already-pinned artifact points at, use ``replace_task_artifact`` "
    "(``POST /api/tasks/{task_id}/artifact/{artifact_id}/replace``), which keeps the id. SINCE T-92 THIS JSON DOOR IS "
    "NOT THE MAIN PATH FOR LOCAL BYTES: ``POST /api/tasks/{task_id}/artifacts/upload`` stores the blob and pins the "
    "artifact in ONE transaction, which upload-then-bind cannot do - the gap between the two steps is where "
    "unreferenced blobs come from. Use this route for a LINK (``kind=link`` + ``url``) or to pin a blob ALREADY in "
    "the store (``kind=file|image`` + ``attachment_id``, now documented as blob REUSE rather than the default way to "
    "pin a file). ``name`` is required (48 runes, blank refused) and ``description`` optional (256 runes); both "
    "refuse rather than truncate. Returns a BOUNDED receipt (``TaskArtifactReceiptDTO``: the new artifact's id plus "
    "the resulting count) - not the task; pull GET /api/tasks/{task_id}/artifacts (MCP ``list_task_artifacts``) for "
    "the artifact list, which since T-92 is the only call that carries one. Guards: 404 unknown task; 409 terminal "
    "task (a closed task's deliverables are frozen); 400 an invalid kind, a missing/blank or over-long ``name``, an "
    "over-long ``description``, a missing/blank ``attachment_id`` for file/image, a missing/blank ``url`` for link, "
    "or an ``attachment_id`` that resolves to no stored blob."
)

REPLACE_ROUTE_DESC = (
    "Replace ONE pinned artifact's content in place, keeping its id (MCP ``replace_task_artifact``; requires the "
    "executing agent - caller must be the task's executor, admin capability excepted). The live row is overwritten "
    "and the version it replaced is retained in an append-only journal keyed by that same artifact id; only the most "
    "recent few versions are kept, and the blob of a version that falls off the end is collected with it. New bytes "
    "on disk go to ``POST /api/tasks/{task_id}/artifact/{artifact_id}/replace/upload`` instead, which stores and "
    "swaps in one transaction (T-92). THE KIND IS IMMUTABLE ACROSS VERSIONS: a ``kind`` that disagrees with the "
    "pinned one, a ``url`` sent for a file/image artifact, or an ``attachment_id`` sent for a link artifact are each "
    "a 400. ``name`` and ``description`` are optional and omitted = carried forward; their caps (48 / 256 runes) are "
    "checked ONLY against a value actually sent, so a stored value longer than its cap survives a content swap "
    "untouched. Returns a BOUNDED receipt (``TaskArtifactReplaceReceiptDTO``) - not the task; pull GET "
    "/api/tasks/{task_id}/artifacts for the artifact list. Guards: 404 unknown task -> 403 not the executor -> 409 "
    "terminal task (a closed task's deliverables are frozen, admin/owner included) -> 404 unknown artifact -> 400 "
    "the artifact belongs to a different task -> 400 a cross-kind replacement, a missing/blank replacement for the "
    "pinned kind, a blank or over-long ``name``, an over-long ``description``, or an ``attachment_id`` that resolves "
    "to no stored blob."
)

ERR = {
    "422": {"desc": "Validation error (unified error envelope)."},
    "4XX": {"desc": "Client error (unified error envelope)."},
    "5XX": {"desc": "Server error (unified error envelope)."},
}


def render(obj, base):
    """House style for an expanded block: json.dumps(indent=2) re-indented."""
    out = json.dumps(obj, indent=2, ensure_ascii=False)
    return "\n".join((" " * base + l if i else l) for i, l in enumerate(out.split("\n")))


def entries(obj, base):
    """Render dict ENTRIES only (no enclosing braces) as they sit inside a properties map."""
    body = render(obj, base).split("\n")
    return "\n".join(body[1:-1])


def swap_entries(s, obj_old, obj_new, base, what):
    old = entries(obj_old, base)
    if s.count(old) != 1:
        fail(f"{what}: rendered old entries do not appear exactly once ({s.count(old)})")
    return s.replace(old, entries(obj_new, base))


def swap_block(s, obj_old, obj_new, base, what):
    old = render(obj_old, base)
    if s.count(old) != 1:
        fail(f"{what}: rendered old block does not appear exactly once ({s.count(old)})")
    return s.replace(old, render(obj_new, base))


def responses(ok_ref):
    r = {"200": {"content": {"application/json": {"schema": {"$ref": ok_ref}}},
                 "description": "Successful Response"}}
    for code, meta in ERR.items():
        r[code] = {"content": {"application/json": {"schema":
                   {"$ref": "#/components/schemas/ErrorEnvelopeDTO"}}},
                   "description": meta["desc"]}
    return r


def qparam(name, required=False):
    # Key order matches the existing query params on POST /api/chat/attachments.
    if required:
        sch = {"title": name.replace("_", " ").title(), "type": "string"}
    else:
        sch = {"anyOf": [{"type": "string"}, {"type": "null"}],
               "title": name.replace("_", " ").title()}
    return {"in": "query", "name": name, "required": required, "schema": sch}


def pparam(name):
    return {"in": "path", "name": name, "required": True,
            "schema": {"title": name.replace("_", " ").title(), "type": "string"}}


def upload_route(op_id, path_params, required_name, text, ok_ref):
    params = [pparam(p) for p in path_params]
    params += [qparam("name", required=required_name), qparam("description"),
               qparam("filename"), qparam("mime")]
    return {"post": {
        "description": text,
        "operationId": op_id,
        "parameters": params,
        "requestBody": {"content": {"application/octet-stream": {
            "schema": {"format": "binary", "title": "Body", "type": "string"}}}, "required": True},
        "responses": responses(ok_ref),
        "summary": text,
        "x-mcp": {"include": False},
    }}


def main():
    s = open(SPEC, encoding="utf-8").read()
    if '"artifact_count"' in s and '"TaskArtifactRefDTO"' not in s:
        fail("already applied (TaskArtifactRefDTO is gone) — refusing to run twice")
    spec = json.loads(s)
    sc = spec["components"]["schemas"]
    paths = spec["paths"]

    # ── 1. TaskArtifactDTO: label/attachment_id/filename/is_image out, name/description in.
    old = sc["TaskArtifactDTO"]["properties"]
    new = {k: v for k, v in old.items()
           if k not in ("attachment_id", "filename", "is_image", "label")}
    new["description"] = {"default": "", "description": DESCRIPTION_PROP_DESC,
                          "title": "Description", "type": "string"}
    new["mime"] = {"default": "", "description": MIME_PROP_DESC, "title": "Mime", "type": "string"}
    new["name"] = {"default": "", "description": NAME_PROP_DESC, "title": "Name", "type": "string"}
    new["url"] = {"default": "", "description": URL_PROP_DESC, "title": "Url", "type": "string"}
    new = {k: new[k] for k in sorted(new)}
    s = swap_block(s, old, new, 8, "TaskArtifactDTO.properties")
    s = replace_string_value(s, sc["TaskArtifactDTO"]["description"], ARTIFACT_DTO_DESC,
                             "TaskArtifactDTO.description", 1)

    # ── 2. TaskArtifactRefDTO: delete the schema outright — nothing points at it any more.
    ref_block = (
        '      "TaskArtifactRefDTO": {\n'
        '        "description": ' + json.dumps(sc["TaskArtifactRefDTO"]["description"], ensure_ascii=False) + ',\n'
        '        "properties": {\n'
        '          "id": {"title": "Id", "type": "string"},\n'
        '          "label": {"default": "", "title": "Label", "type": "string"}\n'
        '        },\n'
        '        "required": ["id"],\n'
        '        "title": "TaskArtifactRefDTO",\n'
        '        "additionalProperties": false,\n'
        '        "type": "object"\n'
        '      },\n'
    )
    s = sub1(s, ref_block, "", "TaskArtifactRefDTO removal")

    # ── 3. TaskArtifactListDTO: keep artifacts_detail_level, redefine it (T-92 risk ④ ruling).
    s = replace_string_value(s, sc["TaskArtifactListDTO"]["description"], LIST_DTO_DESC,
                             "TaskArtifactListDTO.description", 1)
    s = replace_string_value(
        s, sc["TaskArtifactListDTO"]["properties"]["artifacts_detail_level"]["description"],
        LIST_DETAIL_LEVEL_DESC, "TaskArtifactListDTO.artifacts_detail_level.description", 1)

    # ── 4. Receipt DTO: it advertised the vanished index.
    s = replace_string_value(
        s, sc["TaskArtifactReceiptDTO"]["description"],
        sc["TaskArtifactReceiptDTO"]["description"].replace(
            "for the artifact set itself — since T-66 the task response carries only an id+label INDEX of the artifacts.",
            "for the artifact set itself — since T-92 the task response carries only ``artifact_count``."),
        "TaskArtifactReceiptDTO.description", 1)

    # ── 5. Write inputs.
    def optstr(title, desc=None):
        o = {"anyOf": [{"type": "string"}, {"type": "null"}], "default": None}
        if desc:
            o["description"] = desc
        o["title"] = title
        return o

    old = sc["TaskArtifactInputDTO"]["properties"]
    new = {
        "attachment_id": optstr("Attachment Id", ADD_ATTACHMENT_ID_DESC),
        "description": optstr("Description", ADD_DESCRIPTION_DESC),
        "kind": {"title": "Kind", "type": "string"},
        "name": {"description": ADD_NAME_DESC, "title": "Name", "type": "string"},
        "url": optstr("Url", ADD_URL_DESC),
    }
    s = swap_block(s, old, new, 8, "TaskArtifactInputDTO.properties")
    s = sub1(s, '        "required": [\n          "kind"\n        ],\n        "title": "TaskArtifactInputDTO",',
             '        "required": [\n          "kind",\n          "name"\n        ],\n        "title": "TaskArtifactInputDTO",',
             "TaskArtifactInputDTO.required")
    s = replace_string_value(s, sc["TaskArtifactInputDTO"]["description"], ADD_INPUT_DTO_DESC,
                             "TaskArtifactInputDTO.description", 1)

    old = sc["TaskArtifactReplaceInputDTO"]["properties"]
    new = {
        "attachment_id": optstr("Attachment Id"),
        "description": optstr("Description", REPLACE_DESCRIPTION_DESC),
        "kind": optstr("Kind"),
        "name": optstr("Name", REPLACE_NAME_DESC),
        "url": optstr("Url"),
    }
    s = swap_block(s, old, new, 8, "TaskArtifactReplaceInputDTO.properties")
    s = replace_string_value(s, sc["TaskArtifactReplaceInputDTO"]["description"],
                             REPLACE_INPUT_DTO_DESC, "TaskArtifactReplaceInputDTO.description", 1)

    # ── 6. Version rows: forced by the history table's new columns; nothing else narrowed.
    old = sc["TaskArtifactVersionDTO"]["properties"]
    new = {k: v for k, v in old.items() if k != "label"}
    new["description"] = {"default": "", "description": VERSION_DESCRIPTION_DESC,
                          "title": "Description", "type": "string"}
    new["name"] = {"default": "", "description": VERSION_NAME_DESC, "title": "Name", "type": "string"}
    new = {k: new[k] for k in sorted(new)}
    s = swap_block(s, old, new, 8, "TaskArtifactVersionDTO.properties")
    s = replace_string_value(s, sc["TaskArtifactVersionDTO"]["description"], VERSION_DTO_DESC,
                             "TaskArtifactVersionDTO.description", 1)
    s = replace_string_value(
        s, old["filename"]["description"],
        old["filename"]["description"].replace(
            "so a version whose ``label`` is empty is not left mute",
            "so a version whose ``name`` is empty is not left mute"),
        "TaskArtifactVersionDTO.filename.description", 1)

    # ── 7. TaskDTO: two fields out, one count in.
    td = sc["TaskDTO"]["properties"]
    s = swap_entries(s, {"artifacts": td["artifacts"], "artifacts_detail_level": td["artifacts_detail_level"]},
                     {"artifact_count": {"default": 0, "description": TASK_ARTIFACT_COUNT_DESC,
                                         "title": "Artifact Count", "type": "integer"}},
                     8, "TaskDTO artifact fields")

    # ── 8. TaskListItemDTO: same field, say so.
    s = sub1(s,
             '          "artifact_count": {\n            "default": 0,\n            "title": "Artifact Count",\n            "type": "integer"\n          },',
             '          "artifact_count": {\n            "default": 0,\n            "description": '
             + json.dumps(LIST_ITEM_ARTIFACT_COUNT_DESC, ensure_ascii=False)
             + ',\n            "title": "Artifact Count",\n            "type": "integer"\n          },',
             "TaskListItemDTO.artifact_count")

    # ── 9. Route prose: every sentence that described the old id+label index.
    s = replace_string_value(s, paths["/api/tasks/{task_id}"]["get"]["description"],
                             GET_TASK_TEXT, "get_task text", 3)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact"]["post"]["summary"],
                             ADD_ARTIFACT_TEXT, "add tool text", 2)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact"]["post"]["description"],
                             ADD_ROUTE_DESC, "add route description", 1)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact/{artifact_id}/replace"]["post"]["summary"],
                             REPLACE_ARTIFACT_TEXT, "replace tool text", 2)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact/{artifact_id}/replace"]["post"]["description"],
                             REPLACE_ROUTE_DESC, "replace route description", 1)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifacts"]["get"]["description"],
                             LIST_ARTIFACTS_TEXT, "list artifacts text", 3)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact/{artifact_id}"]["delete"]["description"],
                             REMOVE_ARTIFACT_DESC, "remove route description", 1)
    s = replace_string_value(s, paths["/api/tasks/{task_id}/artifact/{artifact_id}"]["delete"]["summary"],
                             REMOVE_ARTIFACT_TOOL_TEXT, "remove tool text", 2)
    hist = paths["/api/tasks/{task_id}/artifact/{artifact_id}/history"]["get"]["description"]
    if HISTORY_OLD_SENTENCE not in hist:
        fail("history description no longer carries the sentence T-92 falsifies")
    s = replace_string_value(s, hist, hist.replace(HISTORY_OLD_SENTENCE, HISTORY_NEW_SENTENCE),
                             "history route description", 1)

    # ── 10. The four legacy descriptors that carry a copy of the text (and, for the two
    #        write verbs, a copy of the input schema).
    def patch_descriptor(path, method, new_desc, mutate_schema=None):
        old = paths[path][method]["x-mcp"]["legacy"]["descriptor"]
        old_text = json.loads(old)["description"]
        if mutate_schema is None:
            built = swap_descriptor_text(old, old_text, new_desc)
        else:
            def mutate(obj):
                obj["description"] = new_desc
                mutate_schema(obj["inputSchema"])
            built = rebuild_descriptor(old, mutate)
        return sub1(s, json.dumps(old, ensure_ascii=False),
                    json.dumps(built, ensure_ascii=False),
                    f"{method.upper()} {path} legacy descriptor")

    s = patch_descriptor("/api/tasks/{task_id}", "get", GET_TASK_TEXT)
    s = patch_descriptor("/api/tasks/{task_id}/artifacts", "get", LIST_ARTIFACTS_TEXT)
    s = patch_descriptor("/api/tasks/{task_id}/artifact/{artifact_id}", "delete",
                         REMOVE_ARTIFACT_TOOL_TEXT)

    def add_schema(sch):
        p = sch["properties"]
        del p["label"]
        p["name"] = {"description": ADD_NAME_DESC, "title": "Name", "type": "string"}
        p["description"] = optstr("Description", ADD_DESCRIPTION_DESC)
        p["attachment_id"] = optstr("Attachment Id", ADD_ATTACHMENT_ID_DESC)
        p["url"] = optstr("Url", ADD_URL_DESC)
        sch["required"] = ["task_id", "kind", "name"]

    def replace_schema(sch):
        p = sch["properties"]
        del p["label"]
        p["name"] = optstr("Name", REPLACE_NAME_DESC)
        p["description"] = optstr("Description", REPLACE_DESCRIPTION_DESC)

    s = patch_descriptor("/api/tasks/{task_id}/artifact", "post", ADD_ARTIFACT_TEXT, add_schema)
    s = patch_descriptor("/api/tasks/{task_id}/artifact/{artifact_id}/replace", "post",
                         REPLACE_ARTIFACT_TEXT, replace_schema)

    # ── 11. The two new raw-body routes. Excluded from MCP, so no order renumbering.
    up_add = upload_route("handle_upload_task_artifact_api_tasks__task_id__artifacts_upload_post",
                          ["task_id"], True, UPLOAD_ADD_TEXT,
                          "#/components/schemas/TaskArtifactReceiptDTO")
    up_rep = upload_route(
        "handle_upload_replace_task_artifact_api_tasks__task_id__artifact__artifact_id__replace_upload_post",
        ["task_id", "artifact_id"], False, UPLOAD_REPLACE_TEXT,
        "#/components/schemas/TaskArtifactReplaceReceiptDTO")

    def path_block(path, obj):
        return "    " + json.dumps(path, ensure_ascii=False) + ": " + render(obj, 4) + ",\n"

    s = sub1(s, '    "/api/tasks/{task_id}/artifacts": {',
             path_block("/api/tasks/{task_id}/artifacts/upload", up_add).rstrip(",\n") + ",\n"
             + '    "/api/tasks/{task_id}/artifacts": {',
             "insert artifacts/upload")
    s = sub1(s, '    "/api/tasks/{task_id}/artifacts/upload": {',
             path_block("/api/tasks/{task_id}/artifact/{artifact_id}/replace/upload", up_rep).rstrip(",\n") + ",\n"
             + '    "/api/tasks/{task_id}/artifacts/upload": {',
             "insert replace/upload")

    open(SPEC, "w", encoding="utf-8").write(s)
    verify()


def verify():
    spec = json.load(open(SPEC, encoding="utf-8"))
    sc, paths = spec["components"]["schemas"], spec["paths"]
    if "TaskArtifactRefDTO" in sc:
        fail("TaskArtifactRefDTO survived")
    if "TaskArtifactRefDTO" in json.dumps(spec):
        fail("a dangling $ref to TaskArtifactRefDTO remains")
    t = sc["TaskDTO"]["properties"]
    if "artifacts" in t or "artifacts_detail_level" in t or "artifact_count" not in t:
        fail("TaskDTO artifact fields are not in their T-92 shape")
    a = set(sc["TaskArtifactDTO"]["properties"])
    want = {"id", "kind", "name", "description", "url", "mime", "created_ts", "created_by", "version_count"}
    if a != want:
        fail(f"TaskArtifactDTO fields are {sorted(a)}, expected {sorted(want)}")
    if sc["TaskArtifactInputDTO"]["required"] != ["kind", "name"]:
        fail("add input does not require name")
    for p in ("/api/tasks/{task_id}/artifacts/upload",
              "/api/tasks/{task_id}/artifact/{artifact_id}/replace/upload"):
        if p not in paths:
            fail(f"{p} was not inserted")
        if paths[p]["post"]["x-mcp"] != {"include": False}:
            fail(f"{p} must be MCP-excluded with exactly include:false")
    orders = sorted(op["x-mcp"]["order"] for ops in paths.values() for op in ops.values()
                    if (op.get("x-mcp") or {}).get("include"))
    if orders != list(range(len(orders))):
        fail(f"x-mcp order sequence is not 0..{len(orders) - 1}")
    # Only the routes this script rewrote. Three OTHER routes on origin/main already
    # carry four copies that disagree (/api/outsource-workers/{id}/refocus and the two
    # reply-card verbs) — pre-existing, out of this ticket's scope, reported separately;
    # a spec-wide assertion here would fail on somebody else's drift.
    for path, m in (("/api/tasks/{task_id}", "get"),
                    ("/api/tasks/{task_id}/artifact", "post"),
                    ("/api/tasks/{task_id}/artifact/{artifact_id}", "delete"),
                    ("/api/tasks/{task_id}/artifact/{artifact_id}/replace", "post"),
                    ("/api/tasks/{task_id}/artifacts", "get")):
        op = paths[path][m]
        x = op["x-mcp"]
        d = json.loads(x["legacy"]["descriptor"])
        if d["description"] != x["description"] or x["description"] != op["summary"]:
            fail(f"{m.upper()} {path}: the four copies of the tool text disagree")
        props = (d.get("inputSchema") or {}).get("properties") or {}
        if "label" in props or "label" in ((d.get("inputSchema") or {}).get("required") or []):
            fail(f"{m.upper()} {path}: legacy descriptor's input schema still carries label")
    for stale in ("artifacts_detail_level`` = ``index", "id+label", "``id`` and ``label``"):
        if stale in json.dumps(spec, ensure_ascii=False):
            fail(f"stale phrase still in the spec: {stale}")
    print(f"[t92] ok — {len(orders)} MCP tools, TaskArtifactDTO at {len(want)} fields, "
          f"2 raw-body routes added, 0 stale index phrases")


if __name__ == "__main__":
    main()
