"""REST happy-path face — every served route, minimum-viable identity, spec shape.

Second conformance batch. The auth matrix (test_auth_matrix.py) pins WHO may
call each route; this file pins WHAT a permitted call returns: for every row of
``routes_manifest.json`` a concrete happy request is fired as the route's
lowest-friction passing identity (owner for admin/owner routes, a scratch agent
for the self-report rows, anonymous for public rows) and the response is
validated against the committed ``spec/openapi.json`` declaration:

  * the expected success status (200 for every JSON route today);
  * the response body against the spec's declared response schema
    (``schema_check.violations`` — $ref/required/type/anyOf subset);
  * per-row semantic ``check`` hooks for contract points the schema cannot
    express (echoes, token-null rules, catalog equality, binary round-trips).

Coverage has the same teeth as the matrix: ``test_happy_covers_manifest``
fails the run when a manifest row is neither in ``HAPPY`` nor in the explicit
``SKIPPED_HAPPY`` table (reason required), and
``test_openapi_covers_manifest`` pins the manifest row set to the frozen
``spec/openapi.json`` operations — a new server route reddens BOTH snapshots
before it can ship untested.

Rows that serve non-JSON bytes (binaries, install.sh, chat attachment blob) or
a non-OpenAPI protocol (MCP JSON-RPC) carry ``nonjson`` with a reason: status
is still asserted and a semantic ``check`` replaces schema validation.
"""

from __future__ import annotations

import base64
import json
import os
import pathlib
import uuid
from dataclasses import dataclass, field
from typing import Any, Callable

import httpx
import pytest

from conftest import AgentIdentity, hire_member, mint_member_token
from schema_check import violations

HERE = pathlib.Path(__file__).resolve().parent
SPEC = json.loads((HERE.parent / "spec" / "openapi.json").read_text(encoding="utf-8"))
MCP_CATALOG = json.loads(
    (HERE.parent / "spec" / "mcp-catalog.json").read_text(encoding="utf-8")
)

# A 1x1 transparent PNG — a REAL image payload so the attachment round-trip
# also exercises the is_image/gallery face (mime sniffing stays honest).
_PNG_B64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8"
    "z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)
_PNG_BYTES = base64.b64decode(_PNG_B64)


def _auth(token: str | None) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"} if token else {}


@dataclass
class HCtx:
    """Everything a happy row needs to build its concrete request."""

    client: httpx.Client
    owner_token: str
    agent: AgentIdentity  # this file's OWN scratch agent (self-report rows)
    machine_id: str  # this file's OWN scratch machine (mutating machine rows)
    fresh_member: Callable[[], str]
    fresh_machine: Callable[[], str]
    fresh_role: Callable[[], str]
    _attachment: tuple[str, bytes] | None = field(default=None, repr=False)
    _avatar_to_delete_url: str | None = field(default=None, repr=False)

    def token(self, identity: str) -> str | None:
        return {"owner": self.owner_token, "agent": self.agent.token, "none": None}[
            identity
        ]

    def attachment(self) -> tuple[str, bytes]:
        """Lazily seed ONE chat attachment (owner → happy agent); cached so the
        chat rows share a single fixture regardless of execution order."""
        if self._attachment is None:
            r = self.client.post(
                "/api/chat",
                json={
                    "to": self.agent.member_id,
                    "body": "conformance attachment seed",
                    "attachments": [
                        {
                            "data_b64": _PNG_B64,
                            "filename": "conf.png",
                            "mime": "image/png",
                        }
                    ],
                },
                headers=_auth(self.owner_token),
            )
            assert r.status_code == 200, f"attachment seed failed: {r.text}"
            atts = r.json()["attachments"]
            assert len(atts) == 1 and atts[0]["id"], f"bad attachment echo: {atts}"
            self._attachment = (atts[0]["id"], _PNG_BYTES)
        return self._attachment


@pytest.fixture(scope="session")
def hctx(
    client, owner_token, fresh_member, fresh_machine, fresh_role
) -> HCtx:
    member_id = hire_member(client, owner_token, "conf-happy-agent")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return HCtx(
        client=client,
        owner_token=owner_token,
        agent=AgentIdentity(member_id=member_id, token=token, role_key=""),
        machine_id=fresh_machine(),
        fresh_member=fresh_member,
        fresh_machine=fresh_machine,
        fresh_role=fresh_role,
    )


# ── The happy table ──────────────────────────────────────────────────────────

PathLike = str | Callable[[HCtx], str]
CheckFn = Callable[[HCtx, httpx.Response], None]


@dataclass
class Happy:
    """One happy row. ``identity`` is the LOWEST-friction identity that passes
    the route's authz gate; ``status`` the spec success status; ``nonjson`` a
    reason string for rows whose body is not spec-schema JSON."""

    identity: str = "owner"  # "owner" | "agent" | "none"
    path: PathLike | None = None  # default: the manifest template (no params)
    body: Any = None
    status: int = 200
    nonjson: str = ""
    check: CheckFn | None = None


def _check_version(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["version"] and data["catalog_hash"], data


def _check_login(_ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["token"] and data["token_type"] == "bearer", data


def _check_install_sh(_ctx: HCtx, r: httpx.Response) -> None:
    # lifecycle.md §5: templates the request base URL + token into text/plain.
    assert r.headers.get("content-type", "").startswith("text/plain"), r.headers
    assert "conf-happy-boot-token" in r.text, "token not templated into script"


def _check_binary(_ctx: HCtx, r: httpx.Response) -> None:
    assert len(r.content) > 0, "binary route served an empty body"


def _check_mcp_tools_list(_ctx: HCtx, r: httpx.Response) -> None:
    # mcp.md: JSON-RPC over HTTP 200; tools/list serves the committed catalog.
    payload = r.json()
    assert payload.get("jsonrpc") == "2.0" and payload.get("id") == 1, payload
    assert "error" not in payload, payload
    tools = payload["result"]["tools"]
    served = {t["name"] for t in tools}
    committed = {t["name"] for t in MCP_CATALOG["tools"]}
    assert served == committed, (
        f"tools/list drifted from spec/mcp-catalog.json: "
        f"served-only={sorted(served - committed)} "
        f"catalog-only={sorted(committed - served)}"
    )
    for tool in tools:
        assert isinstance(tool.get("inputSchema"), dict), f"tool without inputSchema: {tool}"


def _check_upload_ref(ctx: HCtx, r: httpx.Response) -> None:
    # The upload answers the light ref; the stored bytes serve back verbatim.
    ref = r.json()
    assert ref["id"].startswith("att-"), ref
    assert ref["mime"] == "image/png" and ref["filename"] == "conf-upload.png", ref
    served = ctx.client.get(
        f"/api/chat/attachment/{ref['id']}",
        headers=_auth(ctx.token("agent")),
    )
    assert served.status_code == 200 and served.content == _PNG_BYTES


def _check_attachment_roundtrip(ctx: HCtx, r: httpx.Response) -> None:
    _att_id, payload = ctx.attachment()
    assert r.content == payload, "attachment bytes did not round-trip"


def _seeded_avatar_delete_path(ctx: HCtx) -> str:
    """Give the DELETE happy row its own precondition.

    Parametrized route order is not a contract, so the adjacent PUT row cannot
    be the setup: without this seed a broken no-op DELETE still answers the
    same empty avatar_url and the conformance row stays green.
    """
    path = f"/api/members/{ctx.agent.member_id}/avatar"
    seeded = ctx.client.put(
        path + "?filename=conf-delete.png&mime=image/png",
        content=_PNG_BYTES,
        headers=_auth(ctx.owner_token),
    )
    assert seeded.status_code == 200, f"avatar delete seed failed: {seeded.text}"
    url = seeded.json()["avatar_url"]
    assert url.startswith("/api/chat/attachment/ava-"), seeded.json()
    ctx._avatar_to_delete_url = url
    return path


def _check_avatar_delete(ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    assert data["member_id"] == ctx.agent.member_id and data["avatar_url"] == "", data
    assert ctx._avatar_to_delete_url is not None, "DELETE row ran without its seed"
    old = ctx.client.get(
        ctx._avatar_to_delete_url,
        headers=_auth(ctx.owner_token),
    )
    assert old.status_code == 404, (
        f"DELETE left its previously referenced blob reachable: "
        f"{old.status_code} {old.text[:200]}"
    )
    ctx._avatar_to_delete_url = None


def _check_bootstrap_preview(_ctx: HCtx, r: httpx.Response) -> None:
    # lifecycle.md §2.3: a UI preview (no member_id) MUST get token: null.
    data = r.json()
    assert data["token"] is None, f"preview bootstrap minted a token: {data}"
    assert data["role"] and data["context"], data


def _check_share_link_shape(ctx: HCtx, r: httpx.Response) -> None:
    att_id, _payload = ctx.attachment()
    url = r.json()["url"]
    assert url.startswith(f"/api/chat/attachment/{att_id}?sig="), url
    sig = url.split("sig=", 1)[1]
    assert sig and "&" not in sig, f"malformed sig segment: {url}"


def _seeded_chat_path(template: str) -> Callable[[HCtx], str]:
    def build(ctx: HCtx) -> str:
        ctx.attachment()  # ensure at least one message/attachment exists
        return f"{template}?with={ctx.agent.member_id}"

    return build


def _nonempty_list(_ctx: HCtx, r: httpx.Response) -> None:
    assert isinstance(r.json(), list) and r.json(), "expected a non-empty list"


def test_list_answers_carry_sizes_but_never_the_documents(client, owner_token):
    """The three list endpoints answer a DIRECTORY, not the documents (T-1170).

    This is the ticket's core promise and the ONLY thing this test looks at: a
    list row must carry the SIZE of each long document and not the document.
    It lives in the black-box suite because the promise is about the WIRE, and
    the two halves that implement it — the Go handlers and the cockpit's
    generated client — were each green against their own fixtures while
    disagreeing with one another about a field name. A suite that only ever
    asks one side cannot see that.

    Every assertion is written to fail on the two DIFFERENT ways this regresses:
      * the prose comes back (the key is present at all), and
      * the size stops being real (0, or absent) — a size-shaped 0 is worse
        than an honest omission, because it reads as a measurement.

    The corpus is seeded here rather than assumed, so an empty station cannot
    make this pass by having nothing to leak.
    """
    h = _auth(owner_token)
    prose = "conformance directory probe — long enough to be worth omitting\n" * 4
    n = len(prose)

    # ── seed: one manual with both documents, one role definition, one
    # retained revision. Absence has to mean "omitted", never "empty".
    type_key = f"conf-dir-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals", json={"type_key": type_key}, headers=h)
    assert r.status_code == 200, r.text
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"sop_md": prose, "learnings": prose},
        headers=h,
    )
    assert r.status_code == 200, r.text

    r = client.post("/api/roles", json={"name": "conformance directory role"}, headers=h)
    assert r.status_code == 200, r.text
    role_key = r.json()["role"]["key"]
    r = client.post(
        f"/api/roles/{role_key}", json={"definition_md": "conf role prose"}, headers=h
    )
    assert r.status_code == 200, r.text

    for text in ("conf directory history v1", "conf directory history v2"):
        r = client.post("/api/global-context", json={"text": text}, headers=h)
        assert r.status_code == 200, r.text

    # ── list_task_manuals ───────────────────────────────────────────────────
    r = client.get("/api/task-manuals", headers=h)
    assert r.status_code == 200, r.text
    rows = {m["type_key"]: m for m in r.json()}
    assert type_key in rows, "the manual just created is not on its own listing"
    m = rows[type_key]
    for absent in ("sop_md", "learnings"):
        assert absent not in m, (
            f"list_task_manuals still carries {absent!r} — the default answer "
            f"is the directory, and the body is GET /api/task-manuals/{{type_key}}"
        )
    assert m["sop_md_chars"] == n, f"sop_md_chars={m['sop_md_chars']!r}, want {n}"
    assert m["learnings_chars"] == n, f"learnings_chars={m['learnings_chars']!r}, want {n}"

    # ── list_roles ──────────────────────────────────────────────────────────
    r = client.get("/api/roles", headers=h)
    assert r.status_code == 200, r.text
    roles = {x["key"]: x for x in r.json()}
    assert role_key in roles, "the role just created is not on the roster"
    role = roles[role_key]
    assert "definition_md" not in role, (
        "list_roles still carries definition_md — the document is GET /api/roles/{role}"
    )
    assert role["size_chars"] == len("conf role prose"), (
        f"size_chars={role['size_chars']!r}, want {len('conf role prose')}"
    )

    # ── list_document_history ───────────────────────────────────────────────
    r = client.get("/api/document-history/global_context/global", headers=h)
    assert r.status_code == 200, r.text
    versions = r.json()
    assert versions, "two writes retained no version — this check would be vacuous"
    v = versions[0]
    assert "content" not in v, (
        "list_document_history still carries content — the body is "
        "GET /api/document-history/{kind}/{key}/{id}"
    )
    # `field_chars` is a MAP keyed by the kind's own field names, and
    # `tombstoned` is its OWN boolean rather than an entry of that map: leaving
    # it in would report the length of the string "true" — a 4 that looks like
    # a measurement — where a reader looks for how long a field was.
    assert isinstance(v["field_chars"], dict) and v["field_chars"], (
        f"field_chars is not a populated map: {v.get('field_chars')!r}"
    )
    assert "tombstoned" not in v["field_chars"], (
        f"tombstoned leaked into field_chars: {v['field_chars']!r}"
    )
    assert isinstance(v["tombstoned"], bool), (
        f"tombstoned is not a boolean: {v['tombstoned']!r}"
    )
    want = len("conf directory history v1")
    assert v["field_chars"]["text"] == want, (
        f"field_chars.text={v['field_chars']['text']!r}, want {want} — a size "
        f"that is not the real length is worse than no size at all"
    )
    # …and the text IS reachable, one revision at a time. Without this the
    # assertions above would also pass against a server that lost the prose.
    r = client.get(
        f"/api/document-history/global_context/global/{v['id']}", headers=h
    )
    assert r.status_code == 200, r.text
    assert r.json()["content"]["text"] == "conf directory history v1", r.text


def test_members_default_includes_outsource_workers_and_light_preserves_kind(
    client, owner_token, hctx, fresh_machine
):
    before = client.get("/api/members", headers=_auth(owner_token))
    assert before.status_code == 200, before.text
    before_ids = {member["id"] for member in before.json()}

    created = client.post(
        "/api/tasks",
        json={
            "title": f"member-list-guard-{uuid.uuid4().hex[:8]}",
            "executor_member_id": hctx.agent.member_id,
        },
        headers=_auth(hctx.agent.token),
    )
    assert created.status_code == 200, created.text
    task_id = created.json()["task"]["id"]
    reassigned = client.post(
        f"/api/tasks/{task_id}/reassign",
        json={"target": {"kind": "outsource", "machine": fresh_machine()}},
        headers=_auth(owner_token),
    )
    assert reassigned.status_code == 200, reassigned.text
    assert reassigned.json()["executor_id"] == ""

    members = client.get("/api/members", headers=_auth(owner_token))
    assert members.status_code == 200, members.text
    workers = [
        member
        for member in members.json()
        if member["id"].startswith("ow-") and member["id"] not in before_ids
    ]
    assert len(workers) == 1
    assert workers[0]["kind"] == "outsource"
    worker_id = workers[0]["id"]

    light = client.get("/api/members?fields=light", headers=_auth(owner_token))
    assert light.status_code == 200, light.text
    light_worker = next(member for member in light.json() if member["id"] == worker_id)
    assert light_worker["kind"] == "outsource"


def _happy_card(ctx: HCtx) -> str:
    """A fresh WAITING reply card opened by the happy agent (the real
    initiator identity: agents open cards, owners answer them)."""
    r = ctx.client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "conf happy card",
              "options": ["AI pick", "other"]},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy card failed: {r.status_code} {r.text}"
    return r.json()["id"]


def _happy_answered_card(ctx: HCtx) -> str:
    card_id = _happy_card(ctx)
    r = ctx.client.post(
        f"/api/reply-cards/{card_id}/answer",
        json={"option_idx": 0},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy answer failed: {r.status_code} {r.text}"
    return card_id


def _seeded_reply_cards_path(ctx: HCtx) -> str:
    _happy_card(ctx)  # ensure at least one waiting card exists
    return "/api/reply-cards"


def _onboard_claim(ctx: HCtx) -> dict:
    """Onboard a scratch machine and return the onboard body — the claim rows
    redeem its one-time claim_code."""
    r = ctx.client.post(
        "/api/machines",
        json={"display_name": f"conf-happy-claim-{uuid.uuid4().hex[:8]}"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"claim-seed onboard failed: {r.status_code} {r.text}"
    return r.json()


def _check_claim_token_authenticates(ctx: HCtx, r: httpx.Response) -> None:
    data = r.json()
    probe = ctx.client.get("/api/members", headers=_auth(data["token"]))
    assert probe.status_code == 200, "claimed machine token failed to authenticate"


def _happy_task(ctx: HCtx) -> str:
    """A fresh ad-hoc task the happy agent executes (the real initiator
    identity: agents create tasks)."""
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf happy task",
              "executor_member_id": ctx.agent.member_id},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy task failed: {r.status_code} {r.text}"
    return r.json()["task"]["id"]


def _happy_task_step(ctx: HCtx, gate: bool = False) -> tuple[str, str]:
    """A fresh task with one planned step; (task_id, step_id). Task status is
    DERIVED from the steps now (T-9ca5). For the gate case the step is reported
    in_progress (a gate arms only on an in_progress task); the step-status case
    leaves the step pending for its own pending→in_progress report."""
    h = _auth(ctx.agent.token)
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "conf happy step", "dod": "asserted",
                         "is_gate": gate}]},
        headers=h,
    )
    assert r.status_code == 200, f"happy plan failed: {r.status_code} {r.text}"
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = ctx.client.get(f"/api/tasks/{task_id}", headers=h).json()["steps"][0]["id"]
    if gate:
        r = ctx.client.post(
            f"/api/tasks/{task_id}/steps/{step_id}/status",
            json={"status": "in_progress"}, headers=h,
        )
        assert r.status_code == 200, f"happy step start failed: {r.status_code} {r.text}"
    return task_id, step_id


def _happy_closed_task(ctx: HCtx) -> str:
    """A fresh DONE task the happy agent executed (close-out targets are
    terminal-only). Task status is DERIVED (T-9ca5): a one-step plan reported
    done auto-derives the task to done and closes it."""
    h = _auth(ctx.agent.token)
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "conf happy step", "dod": "asserted"}]},
        headers=h,
    )
    assert r.status_code == 200, f"happy plan failed: {r.status_code} {r.text}"
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = ctx.client.get(f"/api/tasks/{task_id}", headers=h).json()["steps"][0]["id"]
    for status in ("in_progress", "done"):
        r = ctx.client.post(
            f"/api/tasks/{task_id}/steps/{step_id}/status",
            json={"status": status}, headers=h,
        )
        assert r.status_code == 200, f"happy step {status} failed: {r.status_code} {r.text}"
    return task_id


def _happy_reassigning_task(ctx: HCtx) -> str:
    """A fresh task under the `reassigning` LOCK whose NEW executor is the happy
    agent — so the happy agent may CLAIM it (the claim endpoint is
    executor-guarded). Created executed by a fresh member, then the owner
    reassigns it (kind=member) to the happy agent → lock=reassigning."""
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf happy claim task",
              "executor_member_id": ctx.fresh_member()},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy claim-seed failed: {r.status_code} {r.text}"
    task_id = r.json()["task"]["id"]
    r = ctx.client.post(
        f"/api/tasks/{task_id}/reassign",
        json={"target": {"kind": "member", "member_id": ctx.agent.member_id}},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy reassign failed: {r.status_code} {r.text}"
    assert r.json()["lock"] == "reassigning", r.text
    return task_id


def _happy_task_artifact(ctx: HCtx) -> tuple[str, str]:
    """A fresh task (the happy agent executes it) with one link artifact pinned;
    (task_id, artifact_id) — the un-pin (DELETE) target."""
    task_id = _happy_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact",
        json={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        headers=_auth(ctx.agent.token),
    )
    assert r.status_code == 200, f"happy artifact failed: {r.status_code} {r.text}"
    return task_id, r.json()["artifact_id"]


def _happy_manual(ctx: HCtx) -> str:
    """A fresh task manual (owner-created); returns its type_key.

    Deliberately exercises the LEGACY explicit-type_key create path (T-fa76:
    deprecated but kept for old MCP callers) — the display_name backfill to
    the key is asserted here; the new display_name→minted-tm- flow is the
    happy POST row below."""
    type_key = f"conf-happy-type-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        "/api/task-manuals", json={"type_key": type_key},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy manual failed: {r.status_code} {r.text}"
    assert r.json()["display_name"] == type_key, (
        f"legacy create must backfill display_name=type_key: {r.text}"
    )
    return type_key


def _seeded_tasks_path(ctx: HCtx) -> str:
    _happy_task(ctx)  # ensure at least one task exists
    return "/api/tasks"


def _seeded_task_count_path(ctx: HCtx) -> str:
    _happy_task(ctx)  # ensure at least one OPEN task exists
    return "/api/tasks/count"


def _happy_webhook(ctx: HCtx) -> tuple[str, str]:
    """A fresh webhook endpoint on the happy agent; (member_id, endpoint_id).
    The GET/PATCH/DELETE rows seed one so their faces act on a real endpoint."""
    endpoint_id = f"conf-hook-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/webhooks",
        json={"endpoint_id": endpoint_id, "purpose": "conf happy hook"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy webhook seed failed: {r.status_code} {r.text}"
    return ctx.agent.member_id, endpoint_id


def _happy_webhook_requests_path(ctx: HCtx) -> str:
    """Seed a webhook AND one delivered /in call so the requests ring buffer
    has a row to serve; returns the debug-log path."""
    endpoint_id = f"conf-hook-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/webhooks",
        json={"endpoint_id": endpoint_id, "purpose": "conf requests hook"},
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"requests-hook seed failed: {r.status_code} {r.text}"
    token = r.json()["token"]
    r = ctx.client.post(f"/in?t={token}", content=b"conf request-log seed")
    assert r.status_code == 200, f"/in seed failed: {r.status_code} {r.text}"
    return f"/api/members/{ctx.agent.member_id}/webhooks/{endpoint_id}/requests"


def _happy_scheduled_message(ctx: HCtx) -> tuple[str, str]:
    """A fresh scheduled message on the happy agent; (member_id, schedule_id).
    The GET/PATCH/DELETE rows seed one so their faces act on a real schedule."""
    r = ctx.client.post(
        f"/api/members/{ctx.agent.member_id}/scheduled-messages",
        json={
            "label": "conf happy schedule",
            "body": "conformance scheduled body",
            "cadence": "daily",
            "hour": 9,
            "minute": 0,
            "timezone": "Asia/Taipei",
        },
        headers=_auth(ctx.owner_token),
    )
    assert r.status_code == 200, f"happy schedule seed failed: {r.status_code} {r.text}"
    return ctx.agent.member_id, r.json()["id"]


def _happy_restorable_revision(ctx: HCtx) -> str:
    """A restore path pointing at a revision that REALLY EXISTS.

    Two writes to the global context leave the first one retained, so the row
    exercises the real success path (200 + the restored version's DTO) instead
    of a 404 dressed up as coverage. Owner identity throughout: replacing the
    global context is governance, and so is restoring it.
    """
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    for text in ("conformance happy history v1", "conformance happy history v2"):
        r = ctx.client.post("/api/global-context", json={"text": text}, headers=h)
        assert r.status_code == 200, f"history seed write failed: {r.status_code} {r.text}"
    r = ctx.client.get("/api/document-history/global_context/global", headers=h)
    assert r.status_code == 200, f"history read failed: {r.status_code} {r.text}"
    versions = r.json()
    assert versions, "two writes retained no version — nothing to restore"
    return f"/api/document-history/global_context/global/{versions[0]['id']}/restore"


def _happy_document_version(ctx) -> str:
    """The same retained revision, addressed for a READ of its body.

    It reuses _happy_restorable_revision's seeding so both rows face a version
    that really exists; the listing itself carries no text since T-1170, so the
    id has to come from the listing and the prose from this route.
    """
    restore_path = _happy_restorable_revision(ctx)
    return restore_path.removesuffix("/restore")


_RESET_INSIGHT_OVERLAY = "conformance happy insight overlay to be discarded"


def _reset_insight_path(ctx: HCtx) -> str:
    """Make the reset row SELF-SUFFICIENT: write a distinctive overlay first, so
    the assertions afterwards measure a MOVE rather than a no-op.

    Without this the row would silently depend on whether an earlier row had
    left `assistant` non-default — and a reset that did nothing at all would
    still answer 200 with a spec-shaped body. Same posture as
    _document_history_restore_path above, which seeds its own precondition
    instead of trusting row order.
    """
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    r = ctx.client.post(
        "/api/insight/assistant", json={"text": _RESET_INSIGHT_OVERLAY}, headers=h
    )
    assert r.status_code == 200, f"reset seed write failed: {r.status_code} {r.text}"
    assert r.json()["is_default"] is False, "seed write did not leave a custom doc"
    return "/api/insight/assistant/reset"


def _check_reset_insight(ctx: HCtx, r: httpx.Response) -> None:
    """The factory seed is back, and the custom doc is provably gone.

    🔴 Deliberately does NOT compare against the seed's TEXT. The suite is
    black-box and must not read seeds/insight_assistant.md; what it can state
    order-independently is that the response is no longer the overlay
    _reset_insight_path just wrote, that is_default flipped back to True, and
    that the doc is non-empty — the assistant is the one role that ships a seed,
    so an empty answer here would mean the fold stopped finding it.
    """
    d = r.json()
    assert d["is_default"] is True, f"reset did not flip is_default: {d}"
    assert d["text"] != _RESET_INSIGHT_OVERLAY, "reset left the custom doc in place"
    assert d["text"].strip(), "reset served an EMPTY doc — the factory seed was not restored"
    assert d["size_chars"] == len(d["text"]), d
    assert d["cap_chars"] >= d["size_chars"], d
    assert d["role_key"] == "assistant", d
    # The precondition for this very route, still true after it ran (T-6501):
    # has_seed is about what SHIPS, so a reset can never consume it.
    assert d["has_seed"] is True, d
    # The READ face agrees — the response is not a one-off projection.
    g = ctx.client.get(
        "/api/insight/assistant",
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert g.status_code == 200, f"{g.status_code} {g.text}"
    assert g.json()["text"] == d["text"], "GET after reset disagrees with the reset response"
    assert g.json()["is_default"] is True, g.text


# ── the two editable boot-context blocks (T-791e) ────────────────────────────

_BOOT_DOC_EDIT = "# conformance edit — 系統互動 / 啟動程序\n\nnot the factory text\n"


def _boot_doc_written(ctx: HCtx, r: httpx.Response) -> None:
    """The edit came back verbatim and the block stopped reading as default."""
    d = r.json()
    assert d["text"] == _BOOT_DOC_EDIT, d
    assert d["is_default"] is False, d
    assert d["size_chars"] == len(d["text"]), d
    assert d["cap_chars"] >= d["size_chars"], d
    assert d["has_seed"] is True, d


def _boot_doc_reset(path: str):
    """Reset check: the factory text is back and the edit is provably gone.

    🔴 Deliberately does NOT compare against the seed's TEXT — the suite is
    black-box and must not read seeds/*.md. What it can state order-independently
    is that the answer is no longer the edit, that is_default flipped, and that
    the document is NOT EMPTY: these blocks are the ones every agent boots from,
    so an empty answer would mean the fold stopped finding the shipped seed.
    """

    def check(ctx: HCtx, r: httpx.Response) -> None:
        d = r.json()
        assert d["is_default"] is True, d
        assert d["text"] != _BOOT_DOC_EDIT, "reset left the edit in place"
        assert d["text"].strip(), "reset served an EMPTY block — the shipped seed was not restored"
        assert d["has_seed"] is True, d
        g = ctx.client.get(path, headers={"Authorization": f"Bearer {ctx.owner_token}"})
        assert g.status_code == 200, f"{g.status_code} {g.text}"
        assert g.json()["text"] == d["text"], "GET after reset disagrees with the reset response"

    return check


def _boot_doc_read(kind: str, key: str):
    def check(_ctx: HCtx, r: httpx.Response) -> None:
        d = r.json()
        assert d["kind"] == kind and d["key"] == key, d
        assert d["text"].strip(), "the block is empty — the shipped seed was not folded in"
        assert d["size_chars"] == len(d["text"]), d
        assert d["cap_chars"] >= d["size_chars"], d
        assert isinstance(d["is_default"], bool), d
        assert d["has_seed"] is True, d

    return check


HAPPY: dict[str, Happy] = {
    # ── public ───────────────────────────────────────────────────────────────
    "GET /api/health": Happy(identity="none"),
    "GET /api/version": Happy(identity="none", check=_check_version),
    "GET /health": Happy(identity="none"),
    "GET /version": Happy(identity="none", check=_check_version),
    "POST /api/login": Happy(
        identity="none",
        body=lambda _ctx: {"password": os.environ["OC_OWNER_PASSWORD"]},
        check=_check_login,
    ),
    "GET /install.sh": Happy(
        identity="none",
        path="/install.sh?token=conf-happy-boot-token",
        nonjson="text/plain bootstrap script (lifecycle.md §5), not spec JSON",
        check=_check_install_sh,
    ),
    "GET /api/warden/binary": Happy(
        identity="none",
        nonjson="binary artifact download, not spec JSON",
        check=_check_binary,
    ),
    "GET /api/agent/binary": Happy(
        identity="none",
        nonjson="binary artifact download, not spec JSON",
        check=_check_binary,
    ),
    # ── owner credential + settings (B3) ─────────────────────────────────────
    "GET /api/auth/status": Happy(
        identity="none",
        check=lambda _c, r: _expect(r, lambda d: d["password_set"] is True),
    ),
    "GET /api/settings": Happy(
        check=lambda _c, r: _expect(
            r, lambda d: d["owner_token_ttl"] > 0 and d["agent_token_ttl"] > 0 and 40 <= d["handover_pct"] <= 90
        ),
    ),
    "GET /api/push/public-key": Happy(
        check=lambda _c, r: _expect(r, lambda d: bool(d["public_key"])),
    ),
    "POST /api/push/subscription": Happy(
        body={
            "endpoint": "https://push.example.test/conformance",
            "keys": {"p256dh": "conformance-p256dh", "auth": "conformance-auth"},
        },
        status=204,
        nonjson="204 subscription save has no response body",
        check=lambda _c, _r: None,
    ),
    "DELETE /api/push/subscription": Happy(
        body={"endpoint": "https://push.example.test/conformance"},
        status=204,
        nonjson="204 subscription deletion has no response body",
        check=lambda _c, _r: None,
    ),
    "PATCH /api/settings": Happy(
        # Patch to the defaults: exercises the write path without steering the
        # shared instance away from its expected knobs.
        body={"owner_token_ttl": 86400, "agent_token_ttl": 604800, "handover_pct": 50},
        check=lambda _c, r: _expect(
            r, lambda d: d["owner_token_ttl"] == 86400 and d["agent_token_ttl"] == 604800 and d["handover_pct"] == 50
        ),
    ),
    "GET /api/release/check": Happy(
        # $OC_RELEASE_API_BASE is pinned unroutable (run.sh), so the fresh
        # check deterministically answers the honest degraded verdict: 200
        # {"status":"unknown"} with current_version mirroring /api/version and
        # no fabricated latest tag/link. The reachable-GitHub verdicts are
        # pinned in the server unit tests (update_check_test.go).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "unknown"
            and d["current_version"]
            and d["latest_tag"] is None
            and d["release_url"] is None,
        ),
    ),
    # ── infra seams ──────────────────────────────────────────────────────────
    "POST /api/mint": Happy(
        body=lambda ctx: {"member_id": ctx.agent.member_id, "ttl_days": 1},
        check=_check_login,
    ),
    "POST /api/mcp": Happy(
        body={"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
        nonjson="JSON-RPC face (spec/mcp.md), not an OpenAPI response schema",
        check=_check_mcp_tools_list,
    ),
    # ── members ──────────────────────────────────────────────────────────────
    "GET /api/members": Happy(check=_nonempty_list),
    "POST /api/members": Happy(
        body=lambda _ctx: {"name": f"conf-happy-hire-{uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(r, lambda d: d["id"]),
    ),
    "GET /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}",
        check=lambda ctx, r: _expect(
            r, lambda d: d["id"] == ctx.agent.member_id
        ),
    ),
    "PATCH /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}",
        body={"name": "conf-happy-renamed"},
        check=lambda _c, r: _expect(r, lambda d: d["name"] == "conf-happy-renamed"),
    ),
    "PUT /api/members/{member_id}/avatar": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/avatar"
        "?filename=conf-avatar.png&mime=image/png",
        body=_PNG_BYTES,
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["member_id"] == ctx.agent.member_id
            and d["avatar_url"].startswith("/api/chat/attachment/ava-")
            and d["mime"] == "image/png"
            and d["filename"] == "conf-avatar.png",
        ),
    ),
    "DELETE /api/members/{member_id}/avatar": Happy(
        path=_seeded_avatar_delete_path,
        check=_check_avatar_delete,
    ),
    "POST /api/members/{member_id}/activate": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/activate",
        body={},
        check=lambda _c, r: _expect(r, lambda d: d["desired_state"] == "online"),
    ),
    "POST /api/members/{member_id}/relocate": Happy(
        # placement-only 改機器: writes desired_machine_id, NEVER touches
        # desired_state (the activate contrast). The pin must name a REAL
        # machine — this file's own onboarded one. The check pins BOTH: the pin
        # landed AND desired_state was NOT flipped online.
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/relocate",
        body=lambda ctx: {"machine_id": ctx.machine_id},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["desired_machine_id"] == ctx.machine_id
            and d.get("desired_state") != "online",
        ),
    ),
    "POST /api/members/{member_id}/deactivate": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/deactivate",
        check=lambda _c, r: _expect(r, lambda d: d["desired_state"] == "offline"),
    ),
    "POST /api/members/{member_id}/force-stop": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/force-stop",
    ),
    "DELETE /api/members/{member_id}": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}",
        check=lambda _c, r: _expect(r, lambda d: d["roster_status"] == "removed"),
    ),
    # ── webhooks (M4) — a member's 回呼端點 config CRUD (admin_agent floor since
    # T-5336; the DTO carries the endpoint's plaintext inlet token) ───────────
    "GET /api/members/{member_id}/webhooks": Happy(
        path=lambda ctx: f"/api/members/{_happy_webhook(ctx)[0]}/webhooks",
        check=_nonempty_list,
    ),
    "POST /api/members/{member_id}/webhooks": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/webhooks",
        body=lambda _ctx: {"endpoint_id": f"conf-hook-{uuid.uuid4().hex[:8]}",
                           "purpose": "conf happy create"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "enabled"
            and d["token"]
            and d["endpoint_id"].startswith("conf-hook-"),
        ),
    ),
    "PATCH /api/members/{member_id}/webhooks/{endpoint_id}": Happy(
        path=lambda ctx: "/api/members/{}/webhooks/{}".format(*_happy_webhook(ctx)),
        body={"status": "disabled", "purpose": "conf happy patched"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "disabled"
            and d["purpose"] == "conf happy patched",
        ),
    ),
    "DELETE /api/members/{member_id}/webhooks/{endpoint_id}": Happy(
        path=lambda ctx: "/api/members/{}/webhooks/{}".format(*_happy_webhook(ctx)),
        check=lambda _c, r: _expect(
            r, lambda d: d["endpoint_id"].startswith("conf-hook-")
        ),
    ),
    "GET /api/members/{member_id}/webhooks/{endpoint_id}/requests": Happy(
        path=_happy_webhook_requests_path,
        check=lambda _c, r: _expect(
            r,
            lambda d: len(d) == 1
            and d[0]["outcome"] == "delivered"
            and d[0]["body"] == "conf request-log seed"
            and d[0]["truncated"] is False,
        ),
    ),
    # ── scheduled messages (T-f059) — the clock-driven twin of the webhook
    # CRUD above; same admin_agent floor, no credential on the DTO ────────────
    "GET /api/members/{member_id}/scheduled-messages": Happy(
        path=lambda ctx: (
            f"/api/members/{_happy_scheduled_message(ctx)[0]}/scheduled-messages"
        ),
        check=_nonempty_list,
    ),
    "POST /api/members/{member_id}/scheduled-messages": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/scheduled-messages",
        body=lambda _ctx: {
            "label": "conf happy create",
            "body": "conformance scheduled create",
            "cadence": "daily",
            "hour": 9,
            "minute": 0,
            "timezone": "Asia/Taipei",
        },
        # last_fired_slot is seeded at creation, which is the wire-visible form
        # of "a new schedule does not fire the slot it was born after". An empty
        # cursor here would mean the next tick delivers immediately.
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "enabled"
            and d["id"].startswith("sch-")
            and d["timezone"] == "Asia/Taipei"
            and d["last_fired_slot"].endswith("T09:00+08:00"),
        ),
    ),
    "PATCH /api/members/{member_id}/scheduled-messages/{schedule_id}": Happy(
        path=lambda ctx: "/api/members/{}/scheduled-messages/{}".format(
            *_happy_scheduled_message(ctx)
        ),
        body={"status": "disabled", "label": "conf happy patched"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "disabled"
            and d["label"] == "conf happy patched",
        ),
    ),
    "DELETE /api/members/{member_id}/scheduled-messages/{schedule_id}": Happy(
        path=lambda ctx: "/api/members/{}/scheduled-messages/{}".format(
            *_happy_scheduled_message(ctx)
        ),
        check=lambda _c, r: _expect(r, lambda d: d["id"].startswith("sch-")),
    ),
    # T-8b0d: the SAME bounded wake snapshot as /api/resume-summary, for a
    # TARGET member (this file's own scratch agent) instead of the caller.
    # The check pins the control-others contract: identity in the body is
    # the TARGET's id, never the owner caller's — that is the whole point of
    # this row over the caller-locked "GET /api/resume-summary" one above.
    "GET /api/members/{member_id}/resume-summary": Happy(
        path=lambda ctx: f"/api/members/{ctx.agent.member_id}/resume-summary",
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["identity"] == ctx.agent.member_id
            and isinstance(d.get("tasks"), list)
            and isinstance(d.get("overview"), dict),
        ),
    ),
    # ── webhook inlet (M4 §2) — PUBLIC, token-only (?t=); silent 200 for every
    # case so it never leaks endpoint existence. The anonymous face (no token)
    # is the lowest-friction happy probe; the accept/ignore delivery semantics
    # are pinned in the server unit tests (api_webhooks_test.go).
    "POST /in": Happy(
        identity="none",
        check=lambda _c, r: _expect(r, lambda d: d["status"] == "ok"),
    ),
    # ── self-report presence (identity from token — agent reports for ITSELF) ─
    "POST /api/self/waking": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    "POST /api/self/stopping": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    "POST /api/self/stopped": Happy(
        identity="agent",
        body={},
        check=lambda ctx, r: _expect(r, lambda d: d["id"] == ctx.agent.member_id),
    ),
    # ── chat ─────────────────────────────────────────────────────────────────
    "POST /api/chat": Happy(
        body=lambda ctx: {"to": ctx.agent.member_id, "body": "happy ping"},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["from"] == "owner"
            and d["to"] == ctx.agent.member_id
            and d["body"] == "happy ping",
        ),
    ),
    "GET /api/chat": Happy(
        path=_seeded_chat_path("/api/chat"), check=_nonempty_list
    ),
    "GET /api/chat/attachment/{attachment_id}": Happy(
        path=lambda ctx: f"/api/chat/attachment/{ctx.attachment()[0]}",
        nonjson="raw attachment bytes, not spec JSON",
        check=_check_attachment_roundtrip,
    ),
    "GET /api/chat/attachments/{attachment_id}/share-link": Happy(
        path=lambda ctx: f"/api/chat/attachments/{ctx.attachment()[0]}/share-link",
        check=_check_share_link_shape,
    ),
    "GET /api/chat/attachments": Happy(
        path=_seeded_chat_path("/api/chat/attachments"), check=_nonempty_list
    ),
    "POST /api/chat/attachments": Happy(
        identity="agent",
        path="/api/chat/attachments?filename=conf-upload.png",
        body=_PNG_BYTES,
        check=_check_upload_ref,
    ),
    "POST /api/chat/mark-read": Happy(
        body=lambda ctx: {"peer": ctx.agent.member_id, "last_read_ts": 1.0},
    ),
    "GET /api/chat/reads": Happy(),
    "GET /api/chat/unread-count": Happy(
        check=lambda _c, r: _expect(r, lambda d: isinstance(d["unread"], int)),
    ),
    # ── reply cards ──────────────────────────────────────────────────────────
    "POST /api/reply-cards": Happy(
        identity="agent",
        body={"kind": "action", "summary": "conf happy open card",
              "options": ["done, continue"]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "waiting"
            and d["answer"] is None
            and d["answered_ts"] is None
            and d["chat_message_id"],
        ),
    ),
    "GET /api/reply-cards": Happy(
        path=_seeded_reply_cards_path, check=_nonempty_list
    ),
    "GET /api/reply-cards/count": Happy(
        check=lambda _c, r: _expect(r, lambda d: d["waiting"] >= 1),
    ),
    "GET /api/reply-cards/{card_id}": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}",
        check=lambda ctx, r: _expect(
            r, lambda d: d["from"] == ctx.agent.member_id
        ),
    ),
    "POST /api/reply-cards/{card_id}/answer": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}/answer",
        body={"option_idx": 0},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "answered"
            and d["answer"]["option_idx"] == 0
            and d["answered_ts"],
        ),
    ),
    "PUT /api/reply-cards/{card_id}/answer": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_answered_card(ctx)}/answer",
        body={"text": "conf happy revised"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "answered"
            and d["answer"]["text"] == "conf happy revised"
            and d["answer"]["option_idx"] is None,
        ),
    ),
    "POST /api/reply-cards/{card_id}/expire": Happy(
        path=lambda ctx: f"/api/reply-cards/{_happy_card(ctx)}/expire",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "expired"
            and d["expired_ts"]
            and d["answer"] is None
            and d["answered_ts"] is None,
        ),
    ),
    # ── telemetry / monitoring ───────────────────────────────────────────────
    "POST /api/agent/context": Happy(
        identity="agent",
        body={"context_pct": 42},
        check=lambda _c, r: _expect(r, lambda d: d["context_pct"] == 42),
    ),
    "POST /api/monitoring/telemetry": Happy(
        identity="agent", body={"rate_limits": {"primary_used_pct": 1}}
    ),
    "GET /api/monitoring": Happy(),
    # T-da06. The conformance server is a fresh workdir: no SCHEDULED backup has
    # landed, so the honest answer is `unknown`. Asserting membership of the
    # closed vocabulary would be tautological (the server can emit nothing else)
    # — the assertion that carries weight is that a studio with no retreat point
    # does NOT read healthy, which is the entire defect this ticket removed.
    "GET /api/backup-health": Happy(
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "unknown"
            and d["stale_after_secs"] == 43200,
        ),
    ),
    # ── display-name overlays ────────────────────────────────────────────────
    "PATCH /api/accounts/{account_id}": Happy(
        path="/api/accounts/conf-happy-account",
        body={"display_name": "Conf Happy Account"},
        check=lambda _c, r: _expect(
            r, lambda d: d["display_name"] == "Conf Happy Account"
        ),
    ),
    "PATCH /api/machines/{machine_id}": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}",
        body={"display_name": "Conf Happy Machine"},
        check=lambda _c, r: _expect(
            r, lambda d: d["display_name"] == "Conf Happy Machine"
        ),
    ),
    # ── machines ─────────────────────────────────────────────────────────────
    "GET /api/machines": Happy(check=_nonempty_list),
    "POST /api/machines": Happy(
        body=lambda _ctx: {
            "display_name": f"conf-happy-machine-{uuid.uuid4().hex[:8]}"
        },
        check=lambda _c, r: _expect(
            r,
            lambda d: d["machine_id"]
            and d["claim_code"]
            and d["claim_expires_in"] == 600
            and f"/install.sh?code={d['claim_code']}" in d["boot_command"]
            and d["token"] not in d["boot_command"],
        ),
    ),
    "GET /api/machines/{machine_id}/boot-command": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/boot-command",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["claim_code"]
            and d["claim_expires_in"] == 600
            and f"/install.sh?code={d['claim_code']}" in d["boot_command"]
            and d["token"] not in d["boot_command"],
        ),
    ),
    "POST /api/machines/claim": Happy(
        identity="none",
        body=lambda ctx: {"code": _onboard_claim(ctx)["claim_code"]},
        check=_check_claim_token_authenticates,
    ),
    "POST /api/machines/{member_id}/uninstall": Happy(
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/uninstall",
    ),
    "POST /api/machines/{member_id}/upgrade": Happy(
        # The scratch machine's warden is OFFLINE → nothing to command:
        # honest dispatched=false, no durable write (fire-and-forget verb).
        path=lambda ctx: f"/api/machines/{ctx.machine_id}/upgrade",
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["member_id"] == ctx.machine_id
            and d["machine_id"] == ctx.machine_id
            and d["dispatched"] is False,
        ),
    ),
    "DELETE /api/machines/{member_id}": Happy(
        path=lambda ctx: f"/api/machines/{ctx.fresh_machine()}",
    ),
    # ── global context / roles / lessons / bootstrap ─────────────────────────
    # ── document history (T-7d33) ───────────────────────────────────────────
    "GET /api/document-history/{kind}/{key}": Happy(
        path="/api/document-history/global_context/global",
    ),
    "GET /api/document-history/{kind}/{key}/seed": Happy(
        path="/api/document-history/global_context/global/seed",
        # The user-custom block's default IS the empty document, and the reader
        # that compares against it needs the field NAME present (an absent key
        # and an empty string are different documents to a diff).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["kind"] == "global_context"
            and d["key"] == "global"
            and d["content"]["text"] == ""
            and d["content"]["tombstoned"] == "true",
        ),
    ),
    # The BODY of one named revision (T-1170). It reuses the same seeded
    # revision the restore row aims at, so the row exercises a version that
    # REALLY EXISTS rather than a 404 dressed up as coverage — and it asserts
    # the text, which is the whole point of the route: the listing above no
    # longer carries any.
    "GET /api/document-history/{kind}/{key}/{id}": Happy(
        path=_happy_document_version,
        check=lambda _c, r: _expect(
            r,
            lambda d: d["kind"] == "global_context"
            and d["key"] == "global"
            and d["content"]["text"] == "conformance happy history v1",
        ),
    ),
    "POST /api/document-history/{kind}/{key}/{id}/restore": Happy(
        path=_happy_restorable_revision,
        check=lambda _c, r: _expect(r, lambda d: d["id"] and d["content"]),
    ),
    "GET /api/global-context": Happy(),
    "POST /api/global-context": Happy(
        body={"text": "conformance happy user-custom block"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["text"] == "conformance happy user-custom block"
            and d["is_default"] is False,
        ),
    ),
    "POST /api/global-context/reset": Happy(
        check=lambda _c, r: _expect(r, lambda d: d["is_default"] is True),
    ),
    # ── the boot context's other two blocks, editable since T-791e ───────────
    # The reset rows run against a block the replace rows may or may not have
    # edited first: reset is idempotent and replace is a whole document, so
    # neither row depends on the other's order.
    "GET /api/system-interaction": Happy(
        check=lambda _c, r: _boot_doc_read("system_interaction", "global")(_c, r)
    ),
    "POST /api/system-interaction": Happy(
        body={"text": _BOOT_DOC_EDIT}, check=_boot_doc_written
    ),
    "POST /api/system-interaction/reset": Happy(
        check=_boot_doc_reset("/api/system-interaction")
    ),
    "GET /api/boot-sequence/{runtime_key}": Happy(
        path="/api/boot-sequence/claude",
        check=lambda _c, r: _boot_doc_read("boot_sequence", "claude")(_c, r),
    ),
    "POST /api/boot-sequence/{runtime_key}": Happy(
        path="/api/boot-sequence/codex",
        body={"text": _BOOT_DOC_EDIT},
        check=_boot_doc_written,
    ),
    "POST /api/boot-sequence/{runtime_key}/reset": Happy(
        path="/api/boot-sequence/codex/reset",
        check=_boot_doc_reset("/api/boot-sequence/codex"),
    ),
    # ── 下線程序, the fourth owner-editable global document (T-c9c0) ──────────
    # A singleton keyed `global`, so it takes the system-interaction shape
    # rather than the boot-sequence one: no runtime on the path.
    "GET /api/offboard": Happy(
        check=lambda _c, r: _boot_doc_read("offboard", "global")(_c, r)
    ),
    "POST /api/offboard": Happy(body={"text": _BOOT_DOC_EDIT}, check=_boot_doc_written),
    "POST /api/offboard/reset": Happy(check=_boot_doc_reset("/api/offboard")),
    "GET /api/roles": Happy(check=_nonempty_list),
    "GET /api/doc-sizes": Happy(
        # Size-only overview: every capped document reports its own size and
        # its OWN segment's cap, and NO document text rides along (no
        # definition_md / text / sop_md / learnings anywhere in the payload).
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d.get("roles"), list)
            and isinstance(d.get("task_manuals"), list)
            and d["roles"]
            and all(
                isinstance(row.get(seg), dict)
                and isinstance(row[seg].get("size_chars"), int)
                and isinstance(row[seg].get("cap_chars"), int)
                for row in d["roles"]
                for seg in ("duty", "insight", "lessons")
            )
            and all(
                isinstance(row.get(seg), dict)
                and isinstance(row[seg].get("size_chars"), int)
                and isinstance(row[seg].get("cap_chars"), int)
                for row in d["task_manuals"]
                for seg in ("sop", "learnings")
            )
            # Exact key sets, so a document body cannot ride along under any
            # name — an absence check naming the fields we happen to know about
            # would go green on a payload that renamed them.
            and set(d) == {"roles", "task_manuals"}
            and all(
                set(row) == {"role_key", "duty", "insight", "lessons"}
                and set(row[seg]) == {"size_chars", "cap_chars"}
                for row in d["roles"]
                for seg in ("duty", "insight", "lessons")
            )
            and all(
                set(row) == {"type_key", "sop", "learnings"}
                and set(row[seg]) == {"size_chars", "cap_chars"}
                for row in d["task_manuals"]
                for seg in ("sop", "learnings")
            ),
        ),
    ),
    "POST /api/roles": Happy(
        body=lambda _ctx: {"name": f"Conf Happy Role {uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(r, lambda d: d["role"]["key"]),
    ),
    "GET /api/roles/{role}": Happy(
        path="/api/roles/assistant",
        check=lambda _c, r: _expect(r, lambda d: d["key"] == "assistant"),
    ),
    "POST /api/roles/{role}": Happy(
        path=lambda ctx: f"/api/roles/{ctx.fresh_role()}",
        body={"name": "Conf Happy Renamed"},
        check=lambda _c, r: _expect(r, lambda d: d["name"] == "Conf Happy Renamed"),
    ),
    "POST /api/roles/{role}/reset": Happy(
        path="/api/roles/assistant/reset",
        check=lambda _c, r: _expect(r, lambda d: d["key"] == "assistant"),
    ),
    "DELETE /api/roles/{role}": Happy(
        path=lambda ctx: f"/api/roles/{ctx.fresh_role()}",
    ),
    "GET /api/lessons/{role_key}/{task_type}": Happy(
        path="/api/lessons/assistant/general",
    ),
    "POST /api/lessons/{role_key}/{task_type}": Happy(
        path="/api/lessons/assistant/general",
        body={"text": "conformance happy lessons doc"},
        check=lambda _c, r: _expect(
            r, lambda d: d["text"] == "conformance happy lessons doc"
        ),
    ),
    "POST /api/lessons/{role_key}/{task_type}/patch": Happy(
        # Anchor-addressed patch (T-8327): an APPEND edit (empty old) always
        # lands regardless of the doc's current content; the receipt carries
        # size_chars/cap_chars/sha256 verification anchors instead of the full
        # text. T-3aeb renamed `size` -> `size_chars` (a size field must carry
        # its unit) and added the cap the write was judged against, so a caller
        # can compute its remaining budget without a second request.
        path="/api/lessons/assistant/general/patch",
        body={"edits": [{"old": "", "new": "conformance happy patch line"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["size_chars"] > 0
            and d["cap_chars"] >= d["size_chars"]
            and "size" not in d
            and len(d["sha256"]) == 64
            and d["is_default"] is False,
        ),
    ),
    # ── insight (T-3809) ─────────────────────────────────────────────────────
    # The role journal's third block: the lessons trio with the task_type axis
    # removed, so the key is the BARE role_key and the paths carry one segment
    # rather than two.
    "GET /api/insight/{role_key}": Happy(
        path="/api/insight/assistant",
        # ⚠️ This row deliberately does NOT assert the empty doc, and the reason
        # is worth keeping. The first version asserted text == "" and
        # is_default is True — "an untouched insight doc reads as genuinely
        # empty", which is the one observable this ticket ships. It failed on
        # the first live run: test_auth_matrix drives the SAME server earlier in
        # the session and its write cells had already put content in
        # assistant's doc. Any "this doc has never been written" assertion is
        # unsound in a suite that shares one server across files — the property
        # is real, but this is not the layer that can see it. It belongs to a
        # server unit test and to the cockpit's mock.
        #
        # What IS order-independent is the size/cap contract, and it is worth
        # more than the lessons GET row (which asserts nothing at all):
        # size_chars must be the CHARACTER count of the served text — Python's
        # len() over a str counts code points, exactly as the server counts
        # runes — and the cap must be at or above it. A server that reported
        # UTF-16 units, or bytes, or a stale cap, would still return 200 here.
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d["text"], str)
            and d["size_chars"] == len(d["text"])
            and d["cap_chars"] >= d["size_chars"]
            and d["role_key"] == "assistant"
            # T-6501. has_seed is order-independent in a way `text` is not: it
            # asks whether seeds/insight_assistant.md SHIPS, which no write in
            # this suite can change. It is also the field the cockpit gates the
            # reset row on, so a server that never set it (false) would silently
            # remove the only path back to the factory doc.
            and d["has_seed"] is True,
        ),
    ),
    "POST /api/insight/{role_key}": Happy(
        path="/api/insight/assistant",
        body={"text": "conformance happy insight doc"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["text"] == "conformance happy insight doc"
            # The write flips is_default off. Since T-e1e3 a role MAY have a
            # factory seed behind it, so this flip is what distinguishes
            # authored text from shipped text — it is no longer interchangeable
            # with "the doc is non-empty".
            and d["is_default"] is False
            and d["size_chars"] == len("conformance happy insight doc")
            and d["cap_chars"] >= d["size_chars"],
        ),
    ),
    "POST /api/insight/{role_key}/patch": Happy(
        # An APPEND edit (empty `old`) always lands regardless of the doc's
        # current content, so this row does not depend on what the replace row
        # above left behind. The receipt carries size_chars / cap_chars / sha256
        # verification anchors instead of the full text — same shape as the
        # lessons patch receipt, and `size` (unitless) must stay absent.
        path="/api/insight/assistant/patch",
        body={"edits": [{"old": "", "new": "conformance happy insight patch"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["size_chars"] > 0
            and d["cap_chars"] >= d["size_chars"]
            and "size" not in d
            and len(d["sha256"]) == 64
            and d["is_default"] is False,
        ),
    ),
    "POST /api/insight/{role_key}/reset": Happy(
        # T-6501. The way back to the PER-ROLE factory seed — reset_role's
        # counterpart on the Insight block. The path callable writes its own
        # precondition (see _reset_insight_path): a reset that restored nothing
        # would answer 200 with a spec-shaped body, so the row has to know what
        # the document looked like BEFORE it ran.
        path=_reset_insight_path,
        check=_check_reset_insight,
    ),
    "GET /api/resume-summary": Happy(
        check=lambda _c, r: _expect(
            r, lambda d: isinstance(d.get("tasks"), list)
        ),
    ),
    "GET /api/resume-summary-size": Happy(
        # Size-only PEEK: the overview counts/sizes + a derived
        # estimated_total_chars, and NO content keys (no chat/tasks bodies).
        check=lambda _c, r: _expect(
            r,
            lambda d: isinstance(d.get("overview"), dict)
            and isinstance(d["overview"].get("chat_chars"), int)
            and isinstance(d.get("estimated_total_chars"), int)
            and "chat" not in d
            and "tasks" not in d,
        ),
    ),
    "POST /api/bootstrap": Happy(body={}, check=_check_bootstrap_preview),
    # ── tasks (M3) ───────────────────────────────────────────────────────────
    "GET /api/tasks": Happy(path=_seeded_tasks_path, check=_nonempty_list),
    "POST /api/tasks": Happy(
        identity="agent",
        body=lambda ctx: {"title": "conf happy create",
                          "executor_member_id": ctx.agent.member_id},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["deduped"] is False
            and d["task"]["status"] == "not_started"
            and d["task"]["executor_id"] == ctx.agent.member_id
            and d["task"]["task_no"].startswith("T-"),
        ),
    ),
    "GET /api/tasks/count": Happy(
        path=_seeded_task_count_path,
        check=lambda _c, r: _expect(r, lambda d: d["open"] >= 1),
    ),
    "GET /api/tasks/{task_id}": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}",
        check=lambda _c, r: _expect(r, lambda d: d["closed_ts"] is None),
    ),
    "POST /api/tasks/{task_id}/terminate": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/terminate",
        check=lambda _c, r: _expect(
            r, lambda d: d["status"] == "terminated" and d["closed_ts"]
        ),
    ),
    "POST /api/tasks/{task_id}/priority": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/priority",
        body={"priority": "frozen"},
        check=lambda _c, r: _expect(
            r, lambda d: d["priority"] == "frozen" and d["frozen_by"] == "owner"
        ),
    ),
    "POST /api/tasks/{task_id}/message": Happy(
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/message",
        body={"body": "conf happy task message"},
        check=lambda ctx, r: _expect(
            r,
            lambda d: d["from"] == "owner"
            and d["to"] == ctx.agent.member_id
            and d["meta"]["task_id"],
        ),
    ),
    "POST /api/tasks/{task_id}/reassign": Happy(
        # T-35e0: reassign to outsource lands the task UNASSIGNED (発包 → an
        # unassigned outsource task); the scheduler mints the successor later
        # under the global cap, so no worker is bound at reassign time. The
        # task enters the reassigning handover hold with executor_id="".
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/reassign",
        body={"target": {"kind": "outsource", "model": "sonnet",
                         "effort": "low"}},
        # reassigning is a LOCK now (T-9ca5), not a status; status stays DERIVED
        # (the fresh task has no steps → not_started).
        check=lambda _c, r: _expect(
            r,
            lambda d: d["lock"] == "reassigning"
            and d["executor_kind"] == "outsource"
            and d["executor_id"] == "",
        ),
    ),
    "POST /api/tasks/{task_id}/plan": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/plan",
        body={"steps": [{"name": "one", "dod": "d1"},
                        {"name": "two", "dod": "d2", "is_gate": True}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["steps_total"] == 2
            and d["progress_total"] == 2
            and d["progress_done"] == 0,
        ),
    ),
    "POST /api/tasks/{task_id}/claim": Happy(
        # T-9ca5 claim (takeover): the NEW executor takes over a reassigned task,
        # clearing the reassigning lock. The task is reassigned TO the happy
        # agent (executor-guarded), so the happy agent claims it → lock cleared.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_reassigning_task(ctx)}/claim",
        check=lambda _c, r: _expect(r, lambda d: d["lock"] == ""),
    ),
    "POST /api/tasks/{task_id}": Happy(
        # T-646a: the executor corrects its own task's title AND description in
        # ONE call — the case its two predecessors could not express, and the
        # reason this route exists. Aimed at the same CLOSED task as the two
        # rows below and for the same reason: a terminal task stays correctable,
        # and a 200 echoing both new values on a card whose artifact set is
        # frozen is the wire statement of that.
        #
        # The check reads BOTH fields back rather than only asserting 200 — a
        # route that accepted the body and wrote nothing, or wrote one field and
        # dropped the other, would pass a status check. It also re-asserts the
        # task is still done and still closed, so a text write that quietly
        # disturbed the terminal state could not pass either.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}",
        body={"title": "one call", "description": "both fields"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["title"] == "one call"
            and d["description"] == "both fields"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/description": Happy(
        # T-e271: the executor corrects its own task's wording. Aimed at a
        # CLOSED (done) task deliberately — owner ruling 2 says a terminal task
        # stays correctable, and the response echoing the new text on a task
        # whose artifact set is frozen is the wire statement of that. The check
        # reads the description back rather than only asserting 200: a route
        # that accepted the body and wrote nothing would pass a status check.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/description",
        body={"description": "corrected wording"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["description"] == "corrected wording"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/title": Happy(
        # T-2ebe: the executor corrects its own task's title. Aimed at the same
        # CLOSED task as its description twin above and for the same reason — a
        # terminal task stays correctable, and a 200 that echoes the new title on
        # a card whose artifact set is frozen is the wire statement of that. The
        # check reads the title back rather than only asserting 200: a route that
        # accepted the body and wrote nothing would pass a status check. It also
        # re-asserts the task is still done and still closed, so a title write
        # that quietly disturbed the task's terminal state could not pass either.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/title",
        body={"title": "corrected title"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["title"] == "corrected title"
            and d["status"] == "done"
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/duplicate": Happy(
        # T-02c9: mark a fresh task a duplicate of a fresh original — the
        # subject is executed by the happy agent, so the executor guard passes.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/duplicate",
        body=lambda ctx: {"duplicate_of": _happy_task(ctx)},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "duplicated"
            and bool(d["duplicate_of"])
            and d["closed_ts"] is not None,
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/status": Happy(
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/status".format(
            *_happy_task_step(ctx)),
        body={"status": "in_progress"},
        check=lambda _c, r: _expect(
            r, lambda d: d["step_status"] == "in_progress"
            and d["task_status"] == "in_progress"
            and d["progress_done"] == 0 and d["progress_total"] == 1
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/note": Happy(
        # T-cc3e. Written against a PENDING step on purpose: _happy_task_step
        # leaves the step pending unless gate=True, and the note being writable
        # with no status report first is the ticket's whole claim (waiting_reason
        # is the one bound to a status; this one is not). The check reads the
        # receipt's echoed note, so a handler that 200s without storing anything
        # cannot pass.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/note".format(
            *_happy_task_step(ctx)),
        body={"note": "conf happy note — 做到哪、下一步接什麼"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["note"] == "conf happy note — 做到哪、下一步接什麼"
            and d["step_status"] == "pending"
            and bool(d["task_id"]) and bool(d["step_id"]),
        ),
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/gate": Happy(
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/steps/{}/gate".format(
            *_happy_task_step(ctx, gate=True)),
        body={"kind": "decision", "summary": "conf happy gate",
              "options": ["go", "hold"]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["status"] == "waiting"
            and d["task"] is not None
            and d["task"]["id"],
        ),
    ),
    "POST /api/tasks/{task_id}/deps": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/deps",
        body=lambda ctx: {"blocked_by": [_happy_task(ctx)]},
        check=lambda _c, r: _expect(r, lambda d: len(d["deps"]) == 1),
    ),
    "POST /api/tasks/{task_id}/closeout": Happy(
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_closed_task(ctx)}/closeout",
        # T-bb70: the close-out answers a BOUNDED receipt, not the whole task.
        # The key-set equality is the point — asserting only that the fields are
        # present would stay green if the route went back to serving the task,
        # because a whole task carries closeout_reported too.
        check=lambda _c, r: _expect(
            r,
            lambda d: set(d) == {
                "task_id", "task_status", "closeout_reported", "closeout_ts"
            }
            and d["closeout_reported"] is True
            and d["task_status"] == "done"
            and d["closeout_ts"] > 0,
        ),
    ),
    "POST /api/tasks/{task_id}/artifact": Happy(
        # T-3dc5: the executing agent pins a deliverable. A link artifact needs
        # no upload, so it is the lowest-friction happy body; the response is a
        # bounded receipt naming the pinned artifact and the resulting count.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/artifact",
        body={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        check=lambda _c, r: _expect(
            r, lambda d: d["artifact_id"] != "" and d["artifact_count"] == 1
        ),
    ),
    "DELETE /api/tasks/{task_id}/artifact/{artifact_id}": Happy(
        # T-3dc5 (owner ruling 2026-07-18): the executing agent un-pins its own
        # task's artifact — the lowest-friction identity now that remove shares
        # add's agent+executor model. The response is a bounded receipt naming
        # the artifact that went and the count that is left.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifact/{}".format(
            *_happy_task_artifact(ctx)),
        check=lambda _c, r: _expect(r, lambda d: d["artifact_count"] == 0),
    ),
    # ── outsource panel (M3) ─────────────────────────────────────────────────
    "GET /api/outsource-workers": Happy(
        check=lambda _c, r: _expect(r, lambda d: isinstance(d, list)),
    ),
    # ── task manuals (M3) ────────────────────────────────────────────────────
    "GET /api/task-manuals": Happy(),
    "POST /api/task-manuals": Happy(
        # agent floor (owner ruling 2026-07-13): agents author task types.
        # T-fa76 system-key flow: the caller passes display_name only; the
        # server mints the tm- type_key and returns it (the caller addresses
        # later calls by it). The legacy explicit-type_key path stays pinned
        # via _happy_manual above.
        identity="agent",
        body=lambda _ctx: {"display_name": f"conf 顯示名 {uuid.uuid4().hex[:8]}"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["type_key"].startswith("tm-")
            and len(d["type_key"]) == len("tm-") + 12
            and d["display_name"].startswith("conf 顯示名 ")
            and d["fields"] == []
            and d["assignee"] == {},
        ),
    ),
    "GET /api/task-manuals/{type_key}": Happy(
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
    ),
    "POST /api/task-manuals/{type_key}": Happy(
        # agent floor (owner ruling 2026-07-13): content fields are
        # agent-editable (assignee is governance: owner/admin agent since
        # T-6020 — see test_tasks.py).
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
        body={"purpose": "conf happy purpose",
              "fields": [{"name": "pr", "required": True, "is_key": True}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["purpose"] == "conf happy purpose"
            and d["fields"][0]["is_key"] is True,
        ),
    ),
    "DELETE /api/task-manuals/{type_key}": Happy(
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}",
        check=lambda _c, r: _expect(r, lambda d: d["deleted"] is True),
    ),
    "POST /api/task-manuals/{type_key}/learnings": Happy(
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}/learnings",
        body={"text": "conf happy learnings"},
        check=lambda _c, r: _expect(
            r, lambda d: d["learnings"] == "conf happy learnings"
        ),
    ),
    "POST /api/task-manuals/{type_key}/learnings/patch": Happy(
        identity="agent",
        path=lambda ctx: f"/api/task-manuals/{_happy_manual(ctx)}/learnings/patch",
        body={"edits": [{"old": "", "new": "conf happy patch"}]},
        check=lambda _c, r: _expect(r, lambda d: d["applied_edits"] == 1),
    ),
    # ── product guide (docs/guide embed) ────────────────────────────────────
    "GET /api/docs": Happy(
        # machine floor — docs/guide/why.md always lists (T-68f1: the repo
        # README no longer ships as slug "readme").
        check=lambda _c, r: _expect(
            r, lambda d: any(row["slug"] == "why" for row in d)
        ),
    ),
    "GET /api/docs/{slug}": Happy(
        path="/api/docs/why",
        check=lambda _c, r: _expect(
            r,
            lambda d: d["slug"] == "why" and len(d["markdown_md"]) > 0,
        ),
    ),
}

# Manifest rows deliberately NOT happy-tested (reason required — the coverage
# tooth enforces the union).
SKIPPED_HAPPY: dict[str, str] = {
    "POST /api/auth/set-password": (
        "the positive face needs an UNSET password + the serve-log claim token; "
        "the harness seeds the password before serve, so no claim token exists. "
        "The already-set 409 is pinned in test_set_password_after_set_conflicts "
        "below; the full first-run flow is pinned in the server unit tests "
        "(api_settings_test.go)."
    ),
    "POST /api/auth/change-password": (
        "the positive face rotates the shared owner credential AND revokes the "
        "session-scoped owner token fixture (password_changed_at iat cut) — it "
        "would poison every later test. The wrong-current 401 face is pinned in "
        "the auth matrix; the full change/revocation semantics in the server "
        "unit tests (api_settings_test.go)."
    ),
    "GET /api/events": (
        "SSE stream, not a JSON response — behaviour contract lives in "
        "spec/sse.md; the auth matrix probes its status face."
    ),
    "POST /api/members/{member_id}/refocus": (
        "online-only: the happy face needs a live SSE member (out of black-box "
        "batch scope); its offline 409 is pinned in test_error_envelope.py."
    ),
    "POST /api/self/refocus": (
        "restart_self: the happy face needs a live SSE session with a stamped "
        "boot_ts (online-only + minimum-liveness floor, out of black-box batch "
        "scope); its offline 409 is pinned in the auth matrix, and the online "
        "200 stamp + 429 liveness refusal in the server unit tests "
        "(api_members_restartself_test.go)."
    ),
    "POST /api/machines/{machine_id}/bootstrap-here": (
        "positive face runs `ocwarden install` on the HOST under test — a side "
        "effect the black-box harness must not trigger (matrix DEGRADED row)."
    ),
    "POST /api/machines/{machine_id}/teardown-here": (
        "no positive face EXISTS any more (T-42a0): the route refuses every "
        "target — the server-local machine because retiring it revokes "
        "credentials fleet-wide, any other machine because the verb carries no "
        "machine selector and cannot reach it. Both 409s and the ordering "
        "(unknown id still resolves to 404 first) are pinned in the server unit "
        "tests (api_machines_teardown_target_t42a0_test.go); the authz faces are "
        "fully asserted in the matrix."
    ),
    "POST /api/theme/fetch": (
        "the positive face needs an EXTERNAL http origin serving a valid theme "
        "bundle — the black-box harness is deliberately hermetic (same reason "
        "$OC_RELEASE_API_BASE is pinned unroutable), and standing a second "
        "server up for one row would trade that away. The format 422 is pinned "
        "in the auth matrix; the fetch-and-import path end to end, the timeout "
        "and size ceilings, and the theme-shape validation are pinned in the "
        "server unit tests (api_theme_fetch_t29c7_test.go) against a real "
        "httptest origin."
    ),
    "POST /api/update/upgrade": (
        "the positive face needs a reachable GitHub Releases repo holding a "
        "newer published release — the harness pins $OC_RELEASE_API_BASE "
        "unroutable on purpose (hermeticity). The no-newer-known 409 is pinned "
        "in the auth matrix owner cell and test_upgrade_no_newer_conflicts "
        "below; the precondition and execution semantics (pin → download → "
        "sha256 verify via checksums.txt → swap → restart, failures 502 with "
        "the old binary untouched) in the server unit tests "
        "(update_check_test.go / upgrade_test.go)."
    ),
    "GET /api/outsource-workers/{id}": (
        "the positive face needs a LIVE worker row, mintable only by the Phase 2 "
        "assignment scheduler (no black-box mint path). The unknown-404 / "
        "anonymous-401 faces are pinned in the "
        "auth matrix; the projection fold (machine/account/context/cost/"
        "delegated_by) in the server unit tests (api_outsource_test.go, "
        "TestListOutsourceWorkers_RuntimeFold)."
    ),
    "GET /api/outsource-workers/{id}/boot-context": (
        "T-ba6b initial-prompt preview: the positive face needs a LIVE worker "
        "row + its bound task, mintable only by the Phase 2 assignment scheduler "
        "(no black-box mint path — same reasoning as GET /api/outsource-workers/"
        "{id}). The below-owner-403 / owner-404 faces are pinned in the auth "
        "matrix; the re-assembled boot-context fold (codename/task/identity, "
        "never a token, unknown-worker 404) in the server unit tests "
        "(api_outsource_test.go, TestGetWorkerBootContext / "
        "TestGetWorkerBootContext_UnknownWorker404)."
    ),
    "POST /api/outsource-workers/{id}/relocate": (
        "T-f190 owner 改機器: the positive face needs a LIVE worker row + an "
        "online target warden, neither of which the black-box harness can mint. "
        "The below-owner-403 / owner-404 / unknown-machine faces are pinned in "
        "the auth matrix; the full relocate semantics (pin write + old-session "
        "stop + pinned-host start re-spawn — the P5b member verbs, no lifecycle "
        "change) in "
        "the server unit tests (api_outsource_test.go, TestRelocateOutsourceWorker)."
    ),
    "POST /api/outsource-workers/{id}/refocus": (
        "T-32e1 owner 換手: the positive face needs a LIVE, online worker row, "
        "mintable only by the Phase 2 scheduler (no black-box mint path — same "
        "reasoning as relocate). The below-owner-403 / owner-404 faces are pinned "
        "in the auth matrix; the online-only 409, refocus_since stamp, and "
        "kill+respawn in the server unit tests (worker_lifecycle_test.go, "
        "TestRefocusWorker_*)."
    ),
    "POST /api/outsource-workers/{id}/stop": (
        "T-f190 owner 停止: the positive face needs a LIVE worker row (no black-box "
        "mint path). The below-owner-403 / owner-404 faces are pinned in the auth "
        "matrix; the desired_state=offline set + refocus clear + session kill + no-revive "
        "in the server unit tests (worker_lifecycle_test.go, TestStopWorker_* / "
        "TestStoppedWorker_TickNeverRevives)."
    ),
    "POST /api/outsource-workers/{id}/restart": (
        "T-f190 owner 重啟: the positive face needs a STOPPED worker row (no "
        "black-box mint path). The below-owner-403 / owner-404 faces are pinned in "
        "the auth matrix; the still-alive-409 (T-7526: the guard is LIVENESS, not "
        "'did anyone press stop' — a worker whose session died on its own IS "
        "restartable) + desired_state=online set + re-dispatch in the server unit "
        "tests (worker_lifecycle_test.go, TestRestartWorker_ClearsAndRedispatches / "
        "TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn)."
    ),
    "POST /api/outsource-workers/{id}/model": (
        "T-f190 owner 換 model: the positive face needs a LIVE worker row (no "
        "black-box mint path). The below-owner-403 / owner-404 faces are pinned in "
        "the auth matrix; the model/effort persist + active-respawn / "
        "assigned-persist-only in the server unit tests (worker_lifecycle_test.go, "
        "TestSetWorkerModel_*)."
    ),
    "GET /api/docs/assets/{name}": (
        "probed with a missing asset name (404 across identities) in the auth "
        "matrix — a positive face would couple this suite to O-46's exact image "
        "filenames under docs/guide/assets. The serve + content-type round-trip "
        "is pinned decoupled in the server unit tests (api_docs_test.go, "
        "TestReadDocAssetFromServesBytesWithContentType)."
    ),
}


def _expect(r: httpx.Response, predicate: Callable[[Any], Any]) -> None:
    data = r.json()
    assert predicate(data), f"semantic check failed on: {json.dumps(data)[:500]}"


# ── plumbing ─────────────────────────────────────────────────────────────────

_MANIFEST: list[dict[str, str]] = json.loads(
    (HERE / "routes_manifest.json").read_text(encoding="utf-8")
)
_MANIFEST_KEYS = [f"{r['method']} {r['path']}" for r in _MANIFEST]


def _response_schema(method: str, template: str, status: int) -> Any:
    op = SPEC["paths"][template][method.lower()]
    resp = op["responses"][str(status)]
    return resp.get("content", {}).get("application/json", {}).get("schema")


_PARAMS = [
    pytest.param(key, id=key.replace(" ", ":"))
    for key in _MANIFEST_KEYS
    if key in HAPPY
]


@pytest.mark.parametrize("route_key", _PARAMS)
def test_rest_happy(hctx: HCtx, route_key: str) -> None:
    method, template = route_key.split(" ", 1)
    row = HAPPY[route_key]
    path = row.path(hctx) if callable(row.path) else (row.path or template)
    assert "{" not in path, f"unresolved path template for {route_key}: {path}"

    body = row.body(hctx) if callable(row.body) else row.body
    kwargs: dict[str, Any] = {"headers": _auth(hctx.token(row.identity))}
    if isinstance(body, (bytes, bytearray)):
        kwargs["content"] = bytes(body)  # raw octet-stream rows (upload)
    elif body is not None:
        kwargs["json"] = body
    r = hctx.client.request(method, path, **kwargs)

    assert r.status_code == row.status, (
        f"{route_key} happy face: expected {row.status}, "
        f"got {r.status_code} {r.text[:300]}"
    )

    if row.nonjson:
        assert row.check is not None, f"{route_key}: nonjson row needs a check"
    else:
        schema = _response_schema(method, template, row.status)
        assert schema is not None and schema != {}, (
            f"{route_key}: spec declares no JSON schema — mark the row nonjson "
            "with a reason instead of silently skipping shape validation"
        )
        problems = violations(r.json(), schema, SPEC)
        assert not problems, (
            f"{route_key}: response does not conform to spec/openapi.json:\n  "
            + "\n  ".join(problems)
        )

    if row.check is not None:
        row.check(hctx, r)


# ── extra semantic pin the table cannot express (two-faced bootstrap) ────────


def test_bootstrap_with_member_mints_token(hctx: HCtx) -> None:
    """lifecycle.md §2.3: bootstrap WITH member_id (a warden spawn) returns a
    freshly minted member JWT — non-null, and it must actually authenticate."""
    member_id = hctx.fresh_member()
    r = hctx.client.post(
        "/api/bootstrap",
        json={"member_id": member_id},
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    token = r.json()["token"]
    assert isinstance(token, str) and token, "spawn bootstrap must mint a token"
    probe = hctx.client.get("/api/members", headers=_auth(token))
    assert probe.status_code == 200, "minted bootstrap token failed to authenticate"


def test_update_task_status_route_is_gone(hctx: HCtx) -> None:
    """T-8449: the retired task-level status report route is REMOVED from the
    wire — the executor's report lands on no handler at all (404), and the task
    never mutates. (The step-level report route stays — the derivation input.)"""
    task_id = _happy_task(hctx)
    h = _auth(hctx.agent.token)
    r = hctx.client.post(
        f"/api/tasks/{task_id}/status", json={"status": "in_progress"}, headers=h)
    assert r.status_code == 404, (
        f"POST /api/tasks/{{id}}/status must be gone (404), got "
        f"{r.status_code} {r.text[:200]}")
    # Nothing mutated — the task is still not_started.
    got = hctx.client.get(f"/api/tasks/{task_id}", headers=_auth(hctx.owner_token))
    assert got.status_code == 200 and got.json()["status"] == "not_started", got.text


def test_claim_code_is_single_use(hctx: HCtx) -> None:
    """A claim code redeems exactly once: onboard → claim 200 (token bound to
    the onboarded machine) → the SAME code again is a flat 401."""
    ob = _onboard_claim(hctx)
    r = hctx.client.post("/api/machines/claim", json={"code": ob["claim_code"]})
    assert r.status_code == 200, r.text
    data = r.json()
    assert data["machine_id"] == ob["machine_id"], data
    again = hctx.client.post("/api/machines/claim", json={"code": ob["claim_code"]})
    assert again.status_code == 401, (
        f"a spent claim code must 401, got {again.status_code} {again.text[:200]}"
    )


def test_install_sh_code_variant_claims_before_download(hctx: HCtx) -> None:
    """install.sh?code= serves the claim-code variant: a HEAD probe of the
    warden binary route runs FIRST (a 503-ing server must not burn the
    one-time code), then the code is templated into a POST /api/machines/claim
    exchange that runs BEFORE the binary download, and sed joins the tool
    precheck. The legacy ?token= variant is pinned untouched by the
    GET /install.sh happy row."""
    r = hctx.client.get("/install.sh?code=conf-happy-claim-code")
    assert r.status_code == 200, r.text
    assert r.headers.get("content-type", "").startswith("text/plain"), r.headers
    body = r.text
    assert '"code":"conf-happy-claim-code"' in body, "code not templated into the claim body"
    assert "for tool in tmux curl sed; do" in body, "sed missing from the precheck"
    probe_at = body.find("curl -fsI ")
    claim_at = body.find("/api/machines/claim")
    # the double quote skips the '# Usage: curl -fsSL ...' comment line
    download_at = body.find('curl -fsSL "')
    assert 0 <= probe_at < claim_at, "the binary probe must precede the claim exchange"
    assert claim_at < download_at, "the claim exchange must precede the binary download"
    assert "/api/warden/binary" in body[probe_at:claim_at], "the probe must hit the binary route"


# ── machine placement: an explicit machine id, or nothing ────────────────────


def _desired_machine(hctx: HCtx, member_id: str) -> str:
    r = hctx.client.get(f"/api/members/{member_id}", headers=_auth(hctx.owner_token))
    assert r.status_code == 200, r.text
    return r.json()["desired_machine_id"]


def test_relocate_requires_a_machine_that_resolves(hctx: HCtx) -> None:
    """Placement is an EXPLICIT decision: relocate takes a real machine id, and
    since the owner ruling of 2026-07-27 it takes NOTHING ELSE. Every other
    non-blank value — the retired "auto" spelling included — is the same honest
    404 a typo'd id has always been, so a member can never be pinned to a
    destination dispatch cannot reach; and "" is no longer the unpin it used to
    be (that made the pin-destroying form the one you got by forgetting a
    field), it is a 400 that leaves the pin exactly where it was."""
    member_id = hctx.fresh_member()
    h = _auth(hctx.owner_token)
    before = _desired_machine(hctx, member_id)
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            f"/api/members/{member_id}/relocate", json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        assert _desired_machine(hctx, member_id) == before, (
            f"a refused relocate must leave the pin untouched ({bad!r})")
    # Sentinel: a REAL machine still lands, so the refusals above are the rule
    # and not a broken fixture.
    r = hctx.client.post(
        f"/api/members/{member_id}/relocate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 200, r.text
    assert r.json()["desired_machine_id"] == hctx.machine_id
    # "" is a semantic refusal (400) and the pin survives; an ABSENT key is the
    # missing-required-field face (422). Both leave the machine just pinned.
    for body, want in (({"machine_id": ""}, 400), ({}, 422)):
        r = hctx.client.post(
            f"/api/members/{member_id}/relocate", json=body, headers=h)
        assert r.status_code == want, f"{body!r}: {r.status_code} {r.text[:200]}"
        assert _desired_machine(hctx, member_id) == hctx.machine_id, (
            f"a refused relocate must leave the pin untouched ({body!r})")


def test_activate_requires_a_machine_that_resolves(hctx: HCtx) -> None:
    """activate's machine bind is held to the SAME rule as relocate: a non-blank
    machine_id must resolve. It matters most here because activate flips
    desired_state online in the same call — a refused bind must leave BOTH the
    pin and the desired_state untouched, never a member that wants to be online
    on a machine that does not exist."""
    member_id = hctx.fresh_member()
    h = _auth(hctx.owner_token)
    before = hctx.client.get(f"/api/members/{member_id}", headers=h).json()
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            f"/api/members/{member_id}/activate", json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        got = hctx.client.get(f"/api/members/{member_id}", headers=h).json()
        assert got["desired_machine_id"] == before["desired_machine_id"], (
            f"a refused activate must leave the pin untouched ({bad!r})")
        assert got["desired_state"] != "online", (
            f"a refused activate must not wake the member ({bad!r})")
    # Sentinel: a REAL machine activates and pins in one call.
    r = hctx.client.post(
        f"/api/members/{member_id}/activate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["desired_machine_id"] == hctx.machine_id, body
    assert body["desired_state"] == "online", body


def test_outsource_worker_relocate_requires_a_machine_that_resolves(
    hctx: HCtx,
) -> None:
    """The worker twin of the member rule. No black-box path mints a worker, so
    the positive face is DEGRADED to "the machine resolve passed and the WORKER
    is what 404s" — distinguishable because a refused machine names the machine
    in the error message, while a resolved one names the worker."""
    h = _auth(hctx.owner_token)
    for bad in ("auto", "warden-nope"):
        r = hctx.client.post(
            "/api/outsource-workers/ow-nope/relocate",
            json={"machine_id": bad}, headers=h)
        assert r.status_code == 404, f"{bad!r}: {r.status_code} {r.text[:200]}"
        assert f"machine '{bad}' not found" in r.text, r.text
    r = hctx.client.post(
        "/api/outsource-workers/ow-nope/relocate",
        json={"machine_id": hctx.machine_id}, headers=h)
    assert r.status_code == 404, r.text
    assert "machine" not in r.json()["error"]["message"], (
        "a resolvable machine must let the request reach the worker resolve")


# ── share-sig semantics (the ?sig= third auth path on the blob GET) ─────────


def _share_url(hctx: HCtx, att_id: str) -> str:
    r = hctx.client.get(
        f"/api/chat/attachments/{att_id}/share-link",
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    return r.json()["url"]


def _second_attachment(hctx: HCtx) -> str:
    """Seed a SECOND attachment (distinct from ctx.attachment()) so the
    single-file-grant face has a foreign blob to aim the sig at."""
    r = hctx.client.post(
        "/api/chat",
        json={
            "to": hctx.agent.member_id,
            "body": "share-sig foreign blob seed",
            "attachments": [
                {"data_b64": _PNG_B64, "filename": "conf2.png", "mime": "image/png"}
            ],
        },
        headers=_auth(hctx.owner_token),
    )
    assert r.status_code == 200, r.text
    return r.json()["attachments"][0]["id"]


def test_share_sig_serves_blob_without_credentials(hctx: HCtx) -> None:
    """A share link works BARE — no Authorization header, no ?token= — and
    returns the exact stored bytes."""
    att_id, payload = hctx.attachment()
    r = hctx.client.get(_share_url(hctx, att_id))
    assert r.status_code == 200, r.text
    assert r.content == payload, "share-sig fetch did not round-trip the bytes"


def test_share_sig_rejects_tampered_sig(hctx: HCtx) -> None:
    att_id, _ = hctx.attachment()
    url = _share_url(hctx, att_id)
    good_sig = url.split("sig=", 1)[1]
    bad_sig = good_sig[:-1] + ("A" if good_sig[-1] != "A" else "B")
    r = hctx.client.get(f"/api/chat/attachment/{att_id}?sig={bad_sig}")
    assert r.status_code == 401, f"tampered sig must 401, got {r.status_code}"
    assert hctx.client.get(f"/api/chat/attachment/{att_id}?sig=").status_code == 401


def test_share_sig_grants_exactly_one_attachment(hctx: HCtx) -> None:
    """The sig is an HMAC over ONE attachment id: replaying it against any
    other blob id is a 401 — a leaked link never widens into a second file."""
    att_id, _ = hctx.attachment()
    other_id = _second_attachment(hctx)
    assert other_id != att_id
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    r = hctx.client.get(f"/api/chat/attachment/{other_id}?sig={sig}")
    assert r.status_code == 401, f"foreign-blob sig must 401, got {r.status_code}"


def test_share_sig_ignored_on_other_routes(hctx: HCtx) -> None:
    """?sig= is a credential ONLY on the blob GET: every other gated route
    stays 401 deny-by-default (the sig never becomes a general token)."""
    att_id, _ = hctx.attachment()
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    for path in (
        f"/api/chat/attachments/{att_id}/share-link?sig={sig}",
        f"/api/chat?sig={sig}",
        f"/api/members?sig={sig}",
    ):
        r = hctx.client.get(path)
        assert r.status_code == 401, f"{path}: expected 401, got {r.status_code}"


def test_share_sig_never_shadows_a_bad_bearer(hctx: HCtx) -> None:
    """Precedence pin: a PRESENT bearer credential (header or ?token=) is
    verified as a token and a bad one stays 401 — it never falls through to a
    valid ?sig= riding the same request."""
    att_id, _ = hctx.attachment()
    sig = _share_url(hctx, att_id).split("sig=", 1)[1]
    r = hctx.client.get(
        f"/api/chat/attachment/{att_id}?sig={sig}",
        headers={"Authorization": "Bearer not-a-jwt"},
    )
    assert r.status_code == 401, f"bad bearer + good sig must 401, got {r.status_code}"
    r = hctx.client.get(f"/api/chat/attachment/{att_id}?token=not-a-jwt&sig={sig}")
    assert r.status_code == 401, f"bad ?token= + good sig must 401, got {r.status_code}"


# ── coverage teeth ───────────────────────────────────────────────────────────


def test_set_password_after_set_conflicts(hctx: HCtx) -> None:
    """Once a password is set, set-password is a flat 409 — the claim token is
    never consulted (no oracle for guessing it) and the credential is
    untouched."""
    r = hctx.client.post(
        "/api/auth/set-password",
        json={"password": "conf-stomp-password", "claim_token": "conf-any-token"},
    )
    assert r.status_code == 409, f"{r.status_code} {r.text}"
    login = hctx.client.post(
        "/api/login", json={"password": os.environ["OC_OWNER_PASSWORD"]}
    )
    assert login.status_code == 200, "the credential must be untouched"


def test_upgrade_no_newer_conflicts(hctx: HCtx) -> None:
    """With GitHub unreachable (the harness pins $OC_RELEASE_API_BASE at an
    unroutable loopback) no newer release is ever known, so the owner's
    explicit upgrade trigger is an honest 409 — never a fabricated upgrade."""
    r = hctx.client.post(
        "/api/update/upgrade", headers=_auth(hctx.owner_token)
    )
    assert r.status_code == 409, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["error"]["code"] == "conflict", body


def test_settings_updater_server_fields_retired(hctx: HCtx) -> None:
    """The updater-server pair (updater_url + updater_invite_code) left the
    wire with the ocupdaterd teardown (t-dc68 — updates come from GitHub
    Releases now): reads carry NEITHER field, and a PATCH writing the retired
    keys is rejected as an unknown-field validation error."""
    h = _auth(hctx.owner_token)
    r = hctx.client.get("/api/settings", headers=h)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert "updater_url" not in body, body
    assert "updater_invite_code" not in body, body
    assert "updater_invite_code_set" not in body, body

    r = hctx.client.patch(
        "/api/settings",
        json={
            "updater_url": "http://127.0.0.1:59999/",
            "updater_invite_code": "conf-retired-invite-code",
        },
        headers=h,
    )
    assert r.status_code == 422, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["error"]["code"] == "validation_error", body
    assert "conf-retired-invite-code" not in r.text, "retired secret echoed"


def test_settings_updater_channel_toggles_roundtrip(hctx: HCtx) -> None:
    """The two software-update toggles (updater_receive_beta + updater_auto_update):
    both default false, PATCH flips each independently (partial semantics),
    reads reflect the live value. The test restores both OFF so the shared
    instance never runs with auto-update armed."""
    h = _auth(hctx.owner_token)
    r = hctx.client.get("/api/settings", headers=h)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert body["updater_receive_beta"] is False, body
    assert body["updater_auto_update"] is False, body
    try:
        r = hctx.client.patch(
            "/api/settings", json={"updater_receive_beta": True}, headers=h
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is False, body  # untouched (PATCH)

        r = hctx.client.patch(
            "/api/settings", json={"updater_auto_update": True}, headers=h
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is True, body

        r = hctx.client.get("/api/settings", headers=h)
        assert r.status_code == 200
        body = r.json()
        assert body["updater_receive_beta"] is True, body
        assert body["updater_auto_update"] is True, body
    finally:
        r = hctx.client.patch(
            "/api/settings",
            json={"updater_receive_beta": False, "updater_auto_update": False},
            headers=h,
        )
        assert r.status_code == 200, f"restore failed: {r.status_code} {r.text}"
        body = r.json()
        assert body["updater_receive_beta"] is False, body
        assert body["updater_auto_update"] is False, body


def test_settings_custom_theme_background_and_mode_round_trip(hctx: HCtx) -> None:
    """The outer-canvas image and its lay-down mode are wire fields (T-081b), so
    the server — not just the client validator — decides what is admissible and
    what comes back. A legal image + mode round-trips durably; the mode
    vocabulary is closed; and a mode naming a zone that carries no image is a
    422 that writes nothing (a mode alone paints nothing, so it is a mistake
    worth naming rather than ignoring)."""
    h = _auth(hctx.owner_token)
    png = (
        "data:image/png;base64,"
        "iVBORw0KGgoAAAABAAAAAQ=="  # PNG magic + filler; the gate checks magic+size
    )
    try:
        r = hctx.client.patch(
            "/api/settings",
            json={
                "custom_themes": [
                    {
                        "id": "conf-canvas",
                        "name": "Conformance canvas",
                        "colors": {"--color-bg": "#101018"},
                        "backgrounds": {"canvas": png},
                        "backgroundModes": {"canvas": "cover"},
                    }
                ]
            },
            headers=h,
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        b = r.json()["custom_themes"][0]
        assert b["backgrounds"] == {"canvas": png}, b
        assert b["backgroundModes"] == {"canvas": "cover"}, b

        # Durable across a re-read — an omitted passthrough would empty it here.
        r = hctx.client.get("/api/settings", headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        b = r.json()["custom_themes"][0]
        assert b["backgrounds"] == {"canvas": png}, b
        assert b["backgroundModes"] == {"canvas": "cover"}, b

        # The mode vocabulary is CLOSED — and unlike an unknown wording code,
        # this one is not lenient: the whole bundle is refused.
        for bad in ({"canvas": "contain"}, {"topbar": "tile"}):
            r = hctx.client.patch(
                "/api/settings",
                json={
                    "custom_themes": [
                        {
                            "id": "conf-bad-mode",
                            "name": "Bad",
                            "colors": {"--color-bg": "#101018"},
                            "backgrounds": {"canvas": png},
                            "backgroundModes": bad,
                        }
                    ]
                },
                headers=h,
            )
            assert r.status_code == 422, f"{bad}: {r.status_code} {r.text}"
            assert r.json()["error"]["code"] == "validation_error", r.text

        # A mode with no image behind it is refused too.
        r = hctx.client.patch(
            "/api/settings",
            json={
                "custom_themes": [
                    {
                        "id": "conf-lone-mode",
                        "name": "Lone",
                        "colors": {"--color-bg": "#101018"},
                        "backgroundModes": {"canvas": "sides"},
                    }
                ]
            },
            headers=h,
        )
        assert r.status_code == 422, f"{r.status_code} {r.text}"

        # …and none of the refusals disturbed what was already stored.
        r = hctx.client.get("/api/settings", headers=h)
        assert [b["id"] for b in r.json()["custom_themes"]] == ["conf-canvas"], r.text
    finally:
        r = hctx.client.patch("/api/settings", json={"custom_themes": []}, headers=h)
        assert r.status_code == 200, f"restore failed: {r.status_code} {r.text}"


def test_settings_custom_theme_unknown_wording_code_is_dropped_not_rejected(
    hctx: HCtx,
) -> None:
    """custom_themes has ONE lenient rule inside an otherwise all-or-nothing
    validator: a `wording` code outside the message-key whitelist does not 422
    the bundle — it is dropped and the request succeeds, so an already-imported
    theme pack stays usable when the whitelist shrinks (T-081b). The echo
    therefore carries a wording map the client did not send. Everything else in
    the overlay stays strict, and the surviving codes are durable."""
    h = _auth(hctx.owner_token)
    try:
        r = hctx.client.patch(
            "/api/settings",
            json={
                "custom_themes": [
                    {
                        "id": "conf-wording",
                        "name": "Conformance wording",
                        "colors": {"--color-bg": "#101018"},
                        "wording": {
                            "zh": {
                                "nav.tasks": "待辦",
                                "profile.themeOffice": "精靈村",
                                "not.a.real.key": "x",
                            }
                        },
                    }
                ]
            },
            headers=h,
        )
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        zh = r.json()["custom_themes"][0]["wording"]["zh"]
        assert zh == {"nav.tasks": "待辦"}, zh

        # Durable: a re-read carries only the surviving code.
        r = hctx.client.get("/api/settings", headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        zh = r.json()["custom_themes"][0]["wording"]["zh"]
        assert zh == {"nav.tasks": "待辦"}, zh

        # The leniency is scoped to the CODE. A language outside {zh,en} is
        # still a 422 and still writes nothing.
        r = hctx.client.patch(
            "/api/settings",
            json={
                "custom_themes": [
                    {
                        "id": "conf-bad-lang",
                        "name": "Bad",
                        "colors": {"--color-bg": "#101018"},
                        "wording": {"xian": {"nav.tasks": "仙"}},
                    }
                ]
            },
            headers=h,
        )
        assert r.status_code == 422, f"{r.status_code} {r.text}"
        assert r.json()["error"]["code"] == "validation_error", r.text
        r = hctx.client.get("/api/settings", headers=h)
        assert [b["id"] for b in r.json()["custom_themes"]] == ["conf-wording"], r.text
    finally:
        r = hctx.client.patch("/api/settings", json={"custom_themes": []}, headers=h)
        assert r.status_code == 200, f"restore failed: {r.status_code} {r.text}"


def test_upload_then_ref_post_roundtrip(hctx: HCtx) -> None:
    """The send-side seam end to end: upload raw bytes → post_chat with the
    light {id} ref → the message stamps the STORED blob's mime/filename (a
    filename/mime alongside the ref is ignored) → the blob serves back
    byte-exact. No base64 ever rides the message body."""
    payload = b"PK\x03\x04 conformance zip payload " * 64
    up = hctx.client.post(
        "/api/chat/attachments?filename=conf-ref.zip&mime=application/zip",
        content=payload,
        headers=_auth(hctx.agent.token),
    )
    assert up.status_code == 200, up.text
    ref = up.json()
    assert ref["mime"] == "application/zip" and ref["filename"] == "conf-ref.zip"

    posted = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [
            {"id": ref["id"], "filename": "spoof.txt", "mime": "text/plain"},
        ]},
        headers=_auth(hctx.agent.token),
    )
    assert posted.status_code == 200, posted.text
    atts = posted.json()["attachments"]
    assert len(atts) == 1 and atts[0]["id"] == ref["id"], atts
    assert atts[0]["mime"] == "application/zip", "stored blob must beat the ref's mime"
    assert atts[0]["filename"] == "conf-ref.zip", "stored blob must beat the ref's filename"

    served = hctx.client.get(
        f"/api/chat/attachment/{ref['id']}", headers=_auth(hctx.agent.token)
    )
    assert served.status_code == 200 and served.content == payload

    # Multi-reference: the same blob rides a second message untouched.
    again = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": ref["id"]}]},
        headers=_auth(hctx.agent.token),
    )
    assert again.status_code == 200, again.text


def test_chat_recipient_validation_preserves_offline_mailbox(hctx: HCtx) -> None:
    """A valid member remains a durable mailbox while disconnected, but an
    invented recipient is rejected instead of becoming an orphaned chat row."""
    delivered = hctx.client.post(
        "/api/chat",
        json={"to": hctx.agent.member_id, "body": "offline-mailbox-probe"},
        headers=_auth(hctx.owner_token),
    )
    assert delivered.status_code == 200, delivered.text

    # The conformance agent has no SSE listener: this reads the persisted
    # mailbox, not a success that depended on live fan-out.
    mailbox = hctx.client.get(
        "/api/chat?with=owner&limit=-1", headers=_auth(hctx.agent.token)
    )
    assert mailbox.status_code == 200, mailbox.text
    assert any(m["body"] == "offline-mailbox-probe" for m in mailbox.json())

    rejected = hctx.client.post(
        "/api/chat",
        json={
            "to": "m-conformance-typo",
            "body": "must-not-land",
            "attachments": [{"data_b64": "bm8tb3JwaGFu", "mime": "text/plain"}],
        },
        headers=_auth(hctx.owner_token),
    )
    assert rejected.status_code == 404, rejected.text
    assert "chat recipient 'm-conformance-typo' not found" in rejected.text

    missing_mailbox = hctx.client.get(
        "/api/chat?with=m-conformance-typo&limit=-1", headers=_auth(hctx.owner_token)
    )
    assert missing_mailbox.status_code == 200, missing_mailbox.text
    assert all(m["body"] != "must-not-land" for m in missing_mailbox.json())


def test_upload_ref_rejections(hctx: HCtx) -> None:
    """The ref form's 400 faces: unknown id, id together with data_b64, and
    an over-cap / empty upload body."""
    headers = _auth(hctx.agent.token)
    r = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": "att-conf-missing"}]},
        headers=headers,
    )
    assert r.status_code == 400 and "not found" in r.text, f"{r.status_code} {r.text}"

    att_id, _ = hctx.attachment()
    r = hctx.client.post(
        "/api/chat",
        json={"to": "owner", "attachments": [{"id": att_id, "data_b64": "aGk="}]},
        headers=headers,
    )
    assert r.status_code == 400 and "both id and data_b64" in r.text, (
        f"{r.status_code} {r.text}"
    )

    r = hctx.client.post("/api/chat/attachments", content=b"", headers=headers)
    assert r.status_code == 400 and "empty" in r.text, f"{r.status_code} {r.text}"

    r = hctx.client.post(
        "/api/chat/attachments?mime=image/png",
        content=b"x" * (20 * 1024 * 1024 + 1),
        headers=headers,
    )
    assert r.status_code == 400 and "20 MB" in r.text, f"{r.status_code} {r.text}"


def test_chat_scrollback_cursor_page_never_marks_read(hctx: HCtx) -> None:
    """T-bf82 scrollback: ``GET /api/chat?with=&before_ts=&before_id=`` serves
    the strictly-older history page (oldest→newest) and NEVER advances the
    caller's read watermark; the cursorless list still auto-marks (unchanged);
    a partial cursor is a 422."""
    peer = hctx.agent.member_id
    sent = []
    for i in range(3):
        r = hctx.client.post(
            "/api/chat",
            json={"to": peer, "body": f"scrollback seed {i}"},
            headers=_auth(hctx.owner_token),
        )
        assert r.status_code == 200, r.text
        sent.append(r.json())
    newest = sent[-1]

    def agent_watermark() -> float:
        r = hctx.client.get(
            "/api/chat/reads",
            params={"with": "owner"},
            headers=_auth(hctx.agent.token),
        )
        assert r.status_code == 200, r.text
        rows = [x for x in r.json() if x["reader_id"] == peer]
        return rows[0]["last_read_ts"] if rows else 0.0

    marked_before = agent_watermark()
    assert marked_before < newest["ts"], (
        "fixture broken: the agent already read past the fresh seed"
    )

    # The AGENT pages history back from the newest message: the page is
    # strictly older (newest excluded), ascending, ending on the second seed.
    r = hctx.client.get(
        "/api/chat",
        params={
            "with": "owner",
            "before_ts": newest["ts"],
            "before_id": newest["id"],
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, r.text
    page = r.json()
    ids = [m["id"] for m in page]
    assert newest["id"] not in ids, "a history page must exclude the cursor row"
    assert ids[-2:] == [sent[0]["id"], sent[1]["id"]], (
        f"history page must end on the strictly-older seeds: {ids[-4:]}"
    )
    ts_list = [m["ts"] for m in page]
    assert ts_list == sorted(ts_list), "history page must stay oldest→newest"

    # THE watermark pin: reading old context is not reading the conversation.
    assert agent_watermark() == marked_before, (
        "a before_ts/before_id history page must never advance the watermark"
    )

    # A partial cursor is a 422 — the params travel together.
    for partial in ({"before_ts": newest["ts"]}, {"before_id": newest["id"]}):
        r = hctx.client.get(
            "/api/chat",
            params={"with": "owner", **partial},
            headers=_auth(hctx.agent.token),
        )
        assert r.status_code == 422, f"partial cursor: {r.status_code} {r.text}"

    # The cursorless list is untouched: it still auto-marks to the newest ts.
    r = hctx.client.get(
        "/api/chat",
        params={"with": "owner"},
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, r.text
    assert agent_watermark() >= newest["ts"], (
        "the cursorless list must keep the auto read-receipt behavior"
    )


def test_happy_covers_manifest(routes_manifest: list[dict[str, str]]) -> None:
    rows = {f"{r['method']} {r['path']}" for r in routes_manifest}
    covered = set(HAPPY) | set(SKIPPED_HAPPY)
    overlap = set(HAPPY) & set(SKIPPED_HAPPY)
    missing = rows - covered
    stale = covered - rows
    assert not overlap, f"rows both happy-tested and skipped: {sorted(overlap)}"
    assert not missing, (
        f"manifest rows with NO happy row and NO skip reason: {sorted(missing)}"
    )
    assert not stale, f"happy/skip rows no longer in ROUTE_SPECS: {sorted(stale)}"
    for key, reason in SKIPPED_HAPPY.items():
        assert reason.strip(), f"SKIPPED_HAPPY[{key}] carries no reason"


def test_openapi_covers_manifest(routes_manifest: list[dict[str, str]]) -> None:
    """The frozen spec/openapi.json and the served route table must describe
    the SAME operation set — a route added to the server without a spec
    freeze update (or vice versa) reddens the run here."""
    manifest_ops = {f"{r['method']} {r['path']}" for r in routes_manifest}
    spec_ops = {
        f"{m.upper()} {p}"
        for p, ops in SPEC["paths"].items()
        for m in ops
        if m in ("get", "post", "put", "patch", "delete")
    }
    assert spec_ops == manifest_ops, (
        f"spec-only={sorted(spec_ops - manifest_ops)} "
        f"manifest-only={sorted(manifest_ops - spec_ops)}"
    )


# ---------------------------------------------------------------------------
# command_result → member.last_op_reason fold (成員啟動失敗原因全鏈可見).
# The warden's telemetry receipt carries a STRUCTURED refusal cause
# ("<code>: <detail>", e.g. "session_already_exists: ..."); the server must
# persist it on the member SEPARATELY from the free-form log and expose it as
# MemberDTO.last_op_reason. A reason-less receipt (older warden) folds "".
# ---------------------------------------------------------------------------


def _member_row(hctx: HCtx, member_id: str) -> dict:
    r = hctx.client.get("/api/members", headers=_auth(hctx.owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    rows = [m for m in r.json() if m["id"] == member_id]
    assert rows, f"member {member_id} missing from roster"
    return rows[0]


def test_command_result_reason_folds_onto_member(hctx: HCtx) -> None:
    target = hctx.fresh_member()
    reason = (
        'session_already_exists: tmux session "member-x" is already live '
        "(clobber-guard refused to stomp it)"
    )
    r = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "start",
                "ok": False,
                "reason": reason,
                "log": reason,
                "at": "2026-07-13T08:00:00Z",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    row = _member_row(hctx, target)
    assert row["last_op"] == "start" and row["last_op_ok"] is False, row
    assert row["last_op_reason"] == reason, row


def test_command_result_without_reason_folds_empty(hctx: HCtx) -> None:
    # Old-warden compat: no reason key ⇒ last_op_reason is "" (status-only for
    # the FE), and it OVERWRITES a stale prior reason (the reason always
    # describes THIS op) while the log still folds.
    target = hctx.fresh_member()
    seed = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "start",
                "ok": False,
                "reason": "spawn_exec_failed: stale prior cause",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert seed.status_code == 200, f"{seed.status_code} {seed.text}"
    r = hctx.client.post(
        "/api/monitoring/telemetry",
        json={
            "command_result": {
                "member_id": target,
                "rpc": "stop",
                "ok": True,
                "log": "session=member-x: stopped",
            }
        },
        headers=_auth(hctx.agent.token),
    )
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    row = _member_row(hctx, target)
    assert row["last_op"] == "stop" and row["last_op_reason"] == "", row
    assert row["last_op_log"] == "session=member-x: stopped", row
