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
    step_id = r.json()["steps"][0]["id"]
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
    step_id = r.json()["steps"][0]["id"]
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
    return task_id, r.json()["artifacts"][0]["id"]


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
            r, lambda d: d["token_ttl"] > 0 and 40 <= d["handover_pct"] <= 90
        ),
    ),
    "PATCH /api/settings": Happy(
        # Patch to the defaults: exercises the write path without steering the
        # shared instance away from its expected knobs.
        body={"token_ttl": 86400, "handover_pct": 50},
        check=lambda _c, r: _expect(
            r, lambda d: d["token_ttl"] == 86400 and d["handover_pct"] == 50
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
    "POST /api/members/{member_id}/activate": Happy(
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/activate",
        body={},
        check=lambda _c, r: _expect(r, lambda d: d["desired_state"] == "online"),
    ),
    "POST /api/members/{member_id}/relocate": Happy(
        # placement-only 改機器: writes desired_machine_id, NEVER touches
        # desired_state (the activate contrast). machine_id="auto" is validation-
        # exempt so a fresh (offline) member relocates without a real target. The
        # check pins BOTH: the pin landed AND desired_state was NOT flipped online.
        path=lambda ctx: f"/api/members/{ctx.fresh_member()}/relocate",
        body={"machine_id": "auto"},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["desired_machine_id"] == "auto"
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
    # ── webhooks (M4) — a member's 回呼端點 config CRUD (machine floor) ────────
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
    "GET /api/roles": Happy(check=_nonempty_list),
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
        # size/sha256 verification anchors instead of the full text.
        path="/api/lessons/assistant/general/patch",
        body={"edits": [{"old": "", "new": "conformance happy patch line"}]},
        check=lambda _c, r: _expect(
            r,
            lambda d: d["applied_edits"] == 1
            and d["size"] > 0
            and len(d["sha256"]) == 64
            and d["is_default"] is False,
        ),
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
        check=lambda _c, r: _expect(r, lambda d: d["priority"] == "frozen"),
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
            lambda d: len(d["steps"]) == 2
            and d["progress_total"] == 2
            and d["steps"][1]["is_gate"] is True
            and d["steps"][1]["reply_card_id"] == "",
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
            r, lambda d: d["steps"][0]["status"] == "in_progress"
            and d["steps"][0]["started_ts"]
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
        check=lambda _c, r: _expect(
            r, lambda d: d["closeout_reported"] is True and d["status"] == "done"
        ),
    ),
    "POST /api/tasks/{task_id}/artifact": Happy(
        # T-3dc5: the executing agent pins a deliverable. A link artifact needs
        # no upload, so it is the lowest-friction happy body; the response folds
        # it into the task's artifact set.
        identity="agent",
        path=lambda ctx: f"/api/tasks/{_happy_task(ctx)}/artifact",
        body={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        check=lambda _c, r: _expect(
            r,
            lambda d: len(d["artifacts"]) == 1
            and d["artifacts"][0]["kind"] == "link"
            and d["artifacts"][0]["url"] == "https://example.com/pr/1",
        ),
    ),
    "DELETE /api/tasks/{task_id}/artifact/{artifact_id}": Happy(
        # T-3dc5 (owner ruling 2026-07-18): the executing agent un-pins its own
        # task's artifact — the lowest-friction identity now that remove shares
        # add's agent+executor model. The response is the task, artifact removed.
        identity="agent",
        path=lambda ctx: "/api/tasks/{}/artifact/{}".format(
            *_happy_task_artifact(ctx)),
        check=lambda _c, r: _expect(r, lambda d: d["artifacts"] == []),
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
        # agent-editable (assignee stays owner-only — see test_tasks.py).
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
        "positive face runs `ocwarden teardown` on the host — same reasoning."
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
    "GET /api/self/task": (
        "the positive face needs a BOUND outsource worker identity, mintable "
        "only by the Phase 2 assignment scheduler (no black-box mint path). "
        "The memberless-404 / below-floor-403 faces are pinned in the auth "
        "matrix and test_tasks.py; the positive claim (assigned → active + "
        "manual snapshot) in the server unit tests (api_tasks_test.go)."
    ),
    "GET /api/outsource-workers/{id}": (
        "the positive face needs a LIVE worker row, mintable only by the Phase 2 "
        "assignment scheduler (no black-box mint path — same reasoning as GET "
        "/api/self/task). The unknown-404 / anonymous-401 faces are pinned in the "
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
        "the auth matrix; the not-stopped-409 + desired_state=online set + re-dispatch "
        "in the server unit tests (worker_lifecycle_test.go, "
        "TestRestartWorker_ClearsAndRedispatches)."
    ),
    "POST /api/outsource-workers/{id}/model": (
        "T-f190 owner 換 model: the positive face needs a LIVE worker row (no "
        "black-box mint path). The below-owner-403 / owner-404 faces are pinned in "
        "the auth matrix; the model/effort persist + active-respawn / "
        "assigned-persist-only in the server unit tests (worker_lifecycle_test.go, "
        "TestSetWorkerModel_*)."
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
    Releases now): reads carry NEITHER field, and a PATCH still writing the
    retired keys is simply ignored (unknown-key JSON semantics), never an
    error and never a resurrected setting."""
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
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert "updater_url" not in body, body
    assert "updater_invite_code_set" not in body, body
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
