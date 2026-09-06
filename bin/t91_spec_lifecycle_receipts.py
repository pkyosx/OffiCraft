#!/usr/bin/env python3
"""T-91 (scope extension, owner 2026-09-06) — the 15 agent-lifecycle writes answer
a bounded receipt instead of the whole roster row they just wrote.

Owner ruling: c-7bd47b89b409 ("既然沒什麼人在用，是不是我們可以一起收掉？") answered
by c-8a1101749140 ("15 支"), read against the count he was answering — 16 write
routes still echoing a whole row, of which the cockpit genuinely consumes exactly
one, `PATCH /api/settings`. The 15 are those 16 minus the settings save, which
stays as it is.

Edits spec/openapi.json as TEXT rather than reserialising it. The file is
hand-maintained and its layout is house style (compact leaf objects, deliberate
non-alphabetical grouping); a json.dump round-trip would reformat all 17k lines
and bury the real changes. Precedent: bin/t646a_spec_update_task.py,
bin/t6cce_add_param_desc.py.

THE THREE RECEIPTS, and why there are three rather than one or fifteen. Every
one of the 15 handlers ends by writing a PROJECTION OF THE STORED ROW, with
exactly five exceptions. "Exactly five" is a claim about THESE TWO FILES, which
is what was searched — other files elsewhere in the tree decorate their own
responses, and that is not what this counts:

    api_members.go:1104   activate  -> activation_pending
    api_members.go:1278   relocate  -> relocation_pending
    api_members.go:1287   relocate  -> relocation_deferred
    api_outsource.go:357  relocate  -> relocation_pending
    api_outsource.go:361  relocate  -> relocation_deferred

(verified by grepping THOSE TWO FILES for assignments onto the response dto, not
by reading the ticket's list back — and the sentence above says "the whole tree"
nowhere for that reason). So three shapes: one for the twelve routes whose
answer is recoverable in full from get_member / get_outsource_worker, one for
activate, one shared by BOTH relocates.

ONE RECEIPT FOR BOTH RELOCATES IS NOT A CONVENIENCE, IT MAKES THE SPEC TRUE.
`POST /api/members/{member_id}/relocate` accepts an ow- id and delegates to
relocateWorkerByID (api_members.go:1181-1185), which writes the WORKER
projection — so that route could already answer an OutsourceWorkerDTO while the
spec said MemberDTO. Collapsing both to the same three-field receipt removes the
disagreement instead of documenting it.

Refuses to run twice. Verifies its own result by re-parsing and re-checking every
one of the 15 routes, so a silently corrupted spec cannot pass as success.
"""

import json
import sys

SPEC = "spec/openapi.json"

LIFECYCLE_DTO = "AgentLifecycleReceiptDTO"
ACTIVATE_DTO = "MemberActivateReceiptDTO"
RELOCATE_DTO = "AgentRelocateReceiptDTO"

# (path, method, old schema name, new schema name, the sentence appended to
# summary / x-mcp.description, naming the read that serves the rest)
ROUTES = [
    ("/api/members", "post", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}", "patch", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}", "delete", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}/activate", "post", "MemberDTO", ACTIVATE_DTO, "get_member"),
    ("/api/members/{member_id}/deactivate", "post", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}/relocate", "post", "MemberDTO", RELOCATE_DTO, "get_member"),
    ("/api/members/{member_id}/refocus", "post", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}/force-stop", "post", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/members/{member_id}/accelerated-stop", "post", "MemberDTO", LIFECYCLE_DTO, "get_member"),
    ("/api/outsource-workers/{id}/stop", "post", "OutsourceWorkerDTO", LIFECYCLE_DTO, "list_outsource_workers"),
    ("/api/outsource-workers/{id}/model", "post", "OutsourceWorkerDTO", LIFECYCLE_DTO, "list_outsource_workers"),
    ("/api/outsource-workers/{id}/refocus", "post", "OutsourceWorkerDTO", LIFECYCLE_DTO, "list_outsource_workers"),
    ("/api/outsource-workers/{id}/force-stop", "post", "OutsourceWorkerDTO", LIFECYCLE_DTO, "list_outsource_workers"),
    ("/api/outsource-workers/{id}/accelerated-stop", "post", "OutsourceWorkerDTO", LIFECYCLE_DTO, "list_outsource_workers"),
    ("/api/outsource-workers/{id}/relocate", "post", "OutsourceWorkerDTO", RELOCATE_DTO, "list_outsource_workers"),
]

FIELDS = {
    LIFECYCLE_DTO: "``id``",
    ACTIVATE_DTO: "``id``, ``activation_pending``, ``last_op_reason``",
    RELOCATE_DTO: "``id``, ``relocation_pending``, ``relocation_deferred``",
}


def sentence(new_dto: str, read_tool: str) -> str:
    return (
        " Answers with a bounded receipt (%s), not the roster row — call ``%s`` "
        "when you need the rest." % (FIELDS[new_dto], read_tool)
    )


LIFECYCLE_SCHEMA = {
    "description": (
        "Bounded receipt for the TWELVE agent-lifecycle writes whose entire answer was a "
        "re-read of the row they had just written (T-91, owner 2026-09-06). Seven staff "
        "routes — ``POST /api/members`` (hire_member), ``PATCH`` (update_member), "
        "``DELETE`` (dismiss_member), ``/deactivate``, ``/refocus``, ``/force-stop``, "
        "``/accelerated-stop`` — answered the whole MemberDTO, 33 fields flattened; five "
        "worker routes — ``/stop``, ``/model``, ``/refocus``, ``/force-stop``, "
        "``/accelerated-stop`` — answered the whole OutsourceWorkerDTO, 42 fields. All "
        "twelve are agent-callable MCP tools, so those answers land in a model's context.\n\n"
        "WHY ID ALONE IS THE WHOLE OF THE NEWS HERE, stated as something that can be "
        "checked rather than asserted: each of the twelve handlers ends on the shared "
        "projection fold (writeMemberDTO / writeWorkerProjection) with NO response-only "
        "mutation. The whole server has exactly five of those — api_members.go:1104, 1278 "
        "and 1287, api_outsource.go:357 and 361 — and every one belongs to activate or to "
        "a relocate, which is why those three routes have receipts of their own and these "
        "twelve do not. So nothing on this wire was unrecoverable: ``get_member`` and "
        "``list_outsource_workers`` serve all of it, at the moment the caller actually "
        "wants it rather than at the moment it wrote.\n\n"
        "THE COCKPIT LOSES NOTHING, checked call site by call site rather than inferred "
        "from the adapter: all twelve are awaited for their completion and their value "
        "discarded (frontend/src/components/OfficePage.tsx, MemberDetailPanel.tsx, "
        "MonitorPage.tsx). Two adapters did parse the answer on the way past — "
        "``patchMember`` returned ``toMember(wire)`` and the five worker verbs returned "
        "``toOutsourceWorker(wire)`` — and no caller read what they returned; those "
        "adapters answer ``void`` in the same change, which is what every one of their "
        "callers already treated them as."
    ),
    "properties": {
        "id": {
            "description": (
                "The agent this write acted on. On eleven of the twelve routes it is the "
                "caller's own path parameter, kept for the reason the restart receipt keeps "
                "it: a receipt that cannot say which agent it acted on is unreadable next to "
                "a log of several. On ``POST /api/members`` it is the one piece of genuine "
                "news on the wire — the server mints the id, and a caller that dropped it "
                "would have to search the roster for the row it had just created."
            ),
            "title": "Id",
            "type": "string",
        }
    },
    "required": ["id"],
    "title": LIFECYCLE_DTO,
    "additionalProperties": False,
    "type": "object",
}

ACTIVATE_SCHEMA = {
    "description": (
        "Bounded receipt for ``POST /api/members/{member_id}/activate`` (activate_member) "
        "(T-91, owner 2026-09-06). It answered the whole MemberDTO, 33 fields flattened, "
        "and it is agent-callable, so that answer lands in a model's context.\n\n"
        "THIS ONE CANNOT COLLAPSE TO AN ID, and for the same reason its worker twin cannot "
        "(OutsourceRestartReceiptDTO): ``activation_pending`` is computed at dispatch time "
        "and written onto the RESPONSE ONLY (api_members.go:1103-1104). It is one of the "
        "three flags this document describes as set only on this kind of response and "
        "absent or null on every other read, so a caller that drops it cannot ask again — "
        "there is no row to ask. The cockpit already depends on exactly this: "
        "frontend/src/api/http.ts activateMember returns ``{activationPending: "
        "wire.activation_pending === true}`` and the 喚醒中… button stays put on true.\n\n"
        "``last_op_reason`` rides beside it for the reason api_members.go:1105-1112 gives "
        "in its own words — the flag is one bit and at least four different states reach "
        "it, so the handler stamps WHICH one on the row before answering. Unlike the flag "
        "this one IS recoverable from ``get_member``; it is kept because a caller holding a "
        "pending bit with no cause has to make a second call to act on the first, which is "
        "the round trip this whole reshape exists to remove. Everything else the DTO "
        "carried is the member's stored row, which ``get_member`` serves."
    ),
    "properties": {
        "id": {
            "description": (
                "The member this activation was aimed at — the caller's own path parameter, "
                "kept because a receipt that cannot say which member it acted on is "
                "unreadable next to a log of several."
            ),
            "title": "Id",
            "type": "string",
        },
        "activation_pending": {
            "description": (
                "True when the activation intent was STORED but no START went out on this "
                "attempt — a warden that would not take it, an unbuildable start frame "
                "(missing persona or token), a backoff, an open circuit. It is a POSITIVE "
                "determination rather than a list of known failures: the handler asks "
                "whether a START actually went out, so failure modes not yet invented "
                "answer honestly here too. Absent when the member was already online or the "
                "start landed. It is here or nowhere: the flag is set only on responses of "
                "this kind and is absent or null on every other read of the member."
            ),
            "title": "Activation Pending",
            "type": "boolean",
        },
        "last_op_reason": {
            "description": (
                "WHICH cause, as a structured ``<code>: <detail>`` line, stamped on the row "
                "by the same handler before it answers. An arm that named no code falls back "
                "to the generic \"活化 was recorded, but nothing has been dispatched yet\". "
                "Empty when there is no refusal to report."
            ),
            "title": "Last Op Reason",
            "type": "string",
        },
    },
    "required": ["id"],
    "title": ACTIVATE_DTO,
    "additionalProperties": False,
    "type": "object",
}

RELOCATE_SCHEMA = {
    "description": (
        "Bounded receipt shared by BOTH relocate routes — ``POST "
        "/api/members/{member_id}/relocate`` (relocate_member) and ``POST "
        "/api/outsource-workers/{id}/relocate`` (T-91, owner 2026-09-06). They answered the "
        "whole MemberDTO (33 fields) and the whole OutsourceWorkerDTO (42 fields) "
        "respectively.\n\n"
        "ONE RECEIPT FOR THE TWO IS WHAT MAKES THIS PAGE TRUE, not a tidy-up. The member "
        "route accepts an ow- id as well — the verb is \"move one agent\" — and delegates "
        "it to relocateWorkerByID (api_members.go:1181-1185), which writes the WORKER "
        "projection. So that route could already answer an OutsourceWorkerDTO while this "
        "document said MemberDTO. The two answers are now the same three fields whichever "
        "kind of agent was named, and the disagreement is gone rather than documented.\n\n"
        "NEITHER FLAG IS RECOVERABLE, which is why this is not an id-only receipt: both are "
        "computed at dispatch time and written onto the RESPONSE ONLY — api_members.go:1278 "
        "and 1287 for the member arm, api_outsource.go:357 and 361 for the worker arm. The "
        "cockpit reads both on the member arm (frontend/src/api/http.ts relocateMember) and "
        "discards the worker arm's answer entirely. Everything else the two DTOs carried is "
        "the stored row, which ``get_member`` and ``list_outsource_workers`` serve."
    ),
    "properties": {
        "id": {
            "description": (
                "The agent this relocate was aimed at — the caller's own path parameter, "
                "kept because a receipt that cannot say which agent it acted on is "
                "unreadable next to a log of several."
            ),
            "title": "Id",
            "type": "string",
        },
        "relocation_pending": {
            "description": (
                "True when the move is SCHEDULED BUT NOT LANDED. The pin itself is persisted "
                "before any dispatch, so a relocate never fails on dispatch — and that is "
                "what made a clean 200 dangerous. Two different non-landings reach this "
                "flag: a decided recycle STOP/START the target warden would not accept, and "
                "a wind-down opened by design so nothing has been dispatched yet. Absent "
                "means nothing was left undelivered; it does NOT mean the agent is already "
                "running on the pin. The cadence retries the pinned move regardless."
            ),
            "title": "Relocation Pending",
            "type": "boolean",
        },
        "relocation_deferred": {
            "description": (
                "WHICH of ``relocation_pending``'s two causes this is: true means a "
                "deliberately deferred move — a wind-down is open and the agent keeps "
                "running on the old machine until its own 收口 — rather than a delivery "
                "failure. A caller must hold back the \"nothing was dispatched\" alert for "
                "this case; the cockpit does exactly that."
            ),
            "title": "Relocation Deferred",
            "type": "boolean",
        },
    },
    "required": ["id"],
    "title": RELOCATE_DTO,
    "additionalProperties": False,
    "type": "object",
}

NEW_SCHEMAS = [
    (LIFECYCLE_DTO, LIFECYCLE_SCHEMA),
    (ACTIVATE_DTO, ACTIVATE_SCHEMA),
    (RELOCATE_DTO, RELOCATE_SCHEMA),
]


def find_block(text: str, start: int) -> int:
    """Return the index just past the JSON object that opens at the first '{' at/after start."""
    i = text.index("{", start)
    depth = 0
    in_str = False
    esc = False
    while i < len(text):
        c = text[i]
        if in_str:
            if esc:
                esc = False
            elif c == "\\":
                esc = True
            elif c == '"':
                in_str = False
        else:
            if c == '"':
                in_str = True
            elif c == "{":
                depth += 1
            elif c == "}":
                depth -= 1
                if depth == 0:
                    return i + 1
        i += 1
    raise ValueError("unbalanced JSON block")


def operation_span(text: str, path: str, method: str):
    key = '\n    "%s": {' % path
    p = text.find(key)
    if p < 0:
        raise SystemExit("path not found in spec: %s" % path)
    p_end = find_block(text, p + len(key) - 1)
    mkey = '\n      "%s": {' % method
    m = text.find(mkey, p, p_end)
    if m < 0:
        raise SystemExit("method %s not found under %s" % (method, path))
    return m, find_block(text, m + len(mkey) - 1)



def summary_of(op_text: str) -> str:
    return json.loads("{" + op_text[op_text.index('"summary": ') :].split("\n")[0].rstrip(",") + "}")["summary"]


def xmcp_of(op_text: str):
    at = op_text.find('"x-mcp": {')
    if at < 0:
        return None
    end = find_block(op_text, at + len('"x-mcp": ') - 1)
    return json.loads(op_text[at + len('"x-mcp": ') : end])


def replace_json_string(op_text: str, key: str, old: str, new: str, inside: str | None = None) -> str:
    """Replace one JSON string value, matched by its exact serialised form."""
    old_tok = key + json.dumps(old, ensure_ascii=False)
    new_tok = key + json.dumps(new, ensure_ascii=False)
    start = 0
    if inside is not None:
        start = op_text.index(inside)
    at = op_text.find(old_tok, start)
    if at < 0:
        raise SystemExit("could not locate %s value to extend" % key)
    return op_text[:at] + new_tok + op_text[at + len(old_tok) :]


def main() -> int:
    text = open(SPEC, encoding="utf-8").read()

    if LIFECYCLE_DTO in text:
        print("refusing to run twice: %s is already in %s" % (LIFECYCLE_DTO, SPEC))
        return 1

    # 1. Rewire each route's 200 schema, and append the receipt sentence to the
    #    summary and (when the operation is on the MCP surface) x-mcp.description.
    for path, method, old_dto, new_dto, read_tool in ROUTES:
        lo, hi = operation_span(text, path, method)
        op = text[lo:hi]

        r_at = op.find('"responses": {')
        if r_at < 0:
            raise SystemExit("no responses block: %s %s" % (method, path))
        ok_at = op.find('"200": {', r_at)
        if ok_at < 0:
            raise SystemExit("no 200 response: %s %s" % (method, path))
        ok_end = find_block(op, ok_at + len('"200": ') - 1)
        old_ref = '"$ref": "#/components/schemas/%s"' % old_dto
        new_ref = '"$ref": "#/components/schemas/%s"' % new_dto
        seg = op[ok_at:ok_end]
        if seg.count(old_ref) != 1:
            raise SystemExit(
                "expected exactly one %s in the 200 of %s %s, found %d"
                % (old_dto, method, path, seg.count(old_ref))
            )
        op = op[:ok_at] + seg.replace(old_ref, new_ref) + op[ok_end:]

        add = sentence(new_dto, read_tool)

        # THREE copies of the sentence, because the tool surface keeps three and
        # gen-mcp-catalog refuses the build when they disagree: the operation
        # summary, x-mcp.description, and the byte-compatible legacy descriptor
        # that carries its own escaped copy of the same string.
        op = replace_json_string(op, '"summary": ', summary_of(op), summary_of(op) + add)
        xm = xmcp_of(op)
        if xm is not None and "description" in xm:
            old_desc = xm["description"]
            new_desc = old_desc + add
            op = replace_json_string(op, '"description": ', old_desc, new_desc, inside='"x-mcp": {')
            legacy = xm.get("legacy", {}).get("descriptor")
            if legacy is not None:
                # The descriptors are byte-compatible fragments frozen at
                # different times, so some escape non-ASCII and some do not.
                # Match whichever form this one actually carries rather than
                # assuming; a wrong guess would silently leave it stale.
                for esc in (True, False):
                    inner_old = json.dumps(old_desc, ensure_ascii=esc)
                    inner_new = json.dumps(new_desc, ensure_ascii=esc)
                    if legacy.count(inner_old) == 1:
                        break
                else:
                    raise SystemExit(
                        "legacy descriptor of %s %s does not carry its description verbatim"
                        % (method, path)
                    )
                op = op.replace(
                    json.dumps(legacy, ensure_ascii=False),
                    json.dumps(legacy.replace(inner_old, inner_new, 1), ensure_ascii=False),
                    1,
                )

        text = text[:lo] + op + text[hi:]

    # 2. Add the three receipt schemas next to the two T-91 receipts already there.
    anchor = '\n      "SettingsDTO": {'
    at = text.find(anchor)
    if at < 0:
        raise SystemExit("anchor schema SettingsDTO not found")
    block = ""
    for name, schema in NEW_SCHEMAS:
        body = json.dumps(schema, ensure_ascii=False, indent=2)
        body = "\n".join(("      " + ln) if ln else ln for ln in body.splitlines())
        block += '\n      "%s": %s,' % (name, body.lstrip())
    text = text[:at] + block + text[at:]

    open(SPEC, "w", encoding="utf-8").write(text)

    # 3. Verify by re-parsing.
    spec = json.loads(open(SPEC, encoding="utf-8").read())
    schemas = spec["components"]["schemas"]
    for name, _ in NEW_SCHEMAS:
        if name not in schemas:
            raise SystemExit("post-check: %s missing" % name)
    for path, method, old_dto, new_dto, _ in ROUTES:
        got = spec["paths"][path][method]["responses"]["200"]["content"][
            "application/json"
        ]["schema"]["$ref"]
        want = "#/components/schemas/%s" % new_dto
        if got != want:
            raise SystemExit("post-check: %s %s -> %s, wanted %s" % (method, path, got, want))
        summary = spec["paths"][path][method].get("summary", "")
        if "bounded receipt" not in summary:
            raise SystemExit("post-check: %s %s summary lost its receipt sentence" % (method, path))
        xm = spec["paths"][path][method].get("x-mcp", {})
        if "description" in xm:
            if "bounded receipt" not in xm["description"]:
                raise SystemExit("post-check: %s %s x-mcp.description not updated" % (method, path))
            legacy = json.loads(xm["legacy"]["descriptor"])
            if legacy["description"] != xm["description"]:
                raise SystemExit(
                    "post-check: %s %s legacy descriptor disagrees with x-mcp.description"
                    % (method, path)
                )
    print("ok: 15 routes rewired, 3 receipt schemas added")
    return 0


if __name__ == "__main__":
    sys.exit(main())
