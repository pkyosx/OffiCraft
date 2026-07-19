"""Auth matrix — every served route × {none, owner, admin_agent, warden, agents}.

The first conformance batch: a TABLE-DRIVEN status assertion per (route,
identity) cell, mechanically tied to the committed ``routes_manifest.json``
snapshot of the server route table (``test_manifest_fully_covered`` fails the run
if a manifest row is neither in the matrix nor in the explicit SKIPPED table).

Expectations are MECHANICALLY DERIVED from each route's declared ``requires``
class (the RBAC route table that landed with service/authz.py):

  * ``requires="public"``   → anonymous 200 face (see PUBLIC_EXPECT below);
  * ``requires="machine"``  — the FLOOR: any valid principal passes the choke
    (status is then pure route semantics); no token → 401;
  * ``requires="admin_agent"`` → plain agents AND wardens are a flat 403
    (deny-first, before any target resolution); owner/admin get the route's
    semantic status;
  * ``requires="owner"``    → everything below owner (admin included) is 403.

The capability ladder (rank): machine/warden=0 < agent=1 < admin_agent=2 <
owner=3; enforcement is rank(principal) >= rank(requires). Below-floor cells
are NEVER hand-written in this table — ``Route`` derives them (403), so the
expected table cannot drift from the requires semantics it encodes. A second
tooth (``test_matrix_requires_match_manifest``) pins each row's ``requires``
label to the committed manifest, so a server requires change reddens the run.

Identity legend:
  * none        — no Authorization header at all;
  * owner       — the owner-scoped JWT from POST /api/login (rank 3);
  * admin_agent — an agent JWT whose member row carries role_key="assistant"
                  (rank 2; owner-hired — role_key is privilege-bearing);
  * warden      — an agent JWT whose member row carries kind="warden" (rank 0,
                  the machine floor; owner-hired — kind is privilege-bearing);
  * agent_self  — agent A's scope="agent" JWT (rank 1); {member_id}-target rows
                  aim at A (so A acts on ITSELF);
  * agent_other — agent B's JWT acting on A's resources (cross-identity).

DEGRADED rows (honest, not silent): listed in ``DEGRADED`` below with reasons —
mostly owner-positive faces of host-side-effect routes tested via an unknown
target id (404 proves the authz gate passed and the resolve ran; it never
executes the host action).
"""

from __future__ import annotations

import json
import pathlib
import uuid
from dataclasses import dataclass, field
from typing import Any, Callable

import httpx
import pytest

from conftest import AgentIdentity

IDENTITIES = ("none", "owner", "admin_agent", "warden", "agent_self", "agent_other")

# The linear capability ladder — the conformance-side mirror of the server's
# PRINCIPAL_RANK (re-declared, NOT imported: black-box iron rule).
_REQUIRES_RANK = {"machine": 0, "agent": 1, "admin_agent": 2, "owner": 3}
_IDENTITY_RANK = {
    "warden": 0,
    "agent_self": 1,
    "agent_other": 1,
    "admin_agent": 2,
    "owner": 3,
}

# Identities that pass an admin_agent choke — destructive path builders hand
# these FRESH scratch resources (they may mutate/destroy the target).
_ADMIN_FACES = ("owner", "admin_agent")


@dataclass
class Ctx:
    """Everything a cell needs to build its concrete request."""

    client: httpx.Client
    owner_token: str
    admin: AgentIdentity
    warden: AgentIdentity
    agent_a: AgentIdentity
    agent_b: AgentIdentity
    machine_id: str  # one session-scoped onboarded machine (offline warden)
    fresh_member: Callable[[], str]
    fresh_machine: Callable[[], str]
    fresh_role: Callable[[], str]

    def token(self, identity: str) -> str | None:
        return {
            "none": None,
            "owner": self.owner_token,
            "admin_agent": self.admin.token,
            "warden": self.warden.token,
            "agent_self": self.agent_a.token,
            "agent_other": self.agent_b.token,
        }[identity]


@pytest.fixture(scope="session")
def ctx(
    client,
    owner_token,
    admin_agent,
    warden_agent,
    agent_a,
    agent_b,
    fresh_member,
    fresh_machine,
    fresh_role,
) -> Ctx:
    return Ctx(
        client=client,
        owner_token=owner_token,
        admin=admin_agent,
        warden=warden_agent,
        agent_a=agent_a,
        agent_b=agent_b,
        machine_id=fresh_machine(),
        fresh_member=fresh_member,
        fresh_machine=fresh_machine,
        fresh_role=fresh_role,
    )


# ── The matrix table ─────────────────────────────────────────────────────────
# One Route per GATED manifest row. `path`/`body` may be a static value or a
# callable(ctx, identity) — callables let a destructive positive face aim at a
# fresh scratch resource while the deny faces aim at agent A.

PathLike = str | Callable[[Ctx, str], str]
BodyLike = Any


@dataclass
class Route:
    """One gated row. ``requires`` names the route's declared principal floor;
    ``overrides`` carries ONLY at-or-above-floor semantic statuses (default
    200). Below-floor cells are derived (403) and may never be hand-written."""

    requires: str
    path: PathLike | None = None  # default: the manifest path itself (no params)
    body: BodyLike | Callable[[Ctx, str], Any] | None = None
    notes: str = ""
    overrides: dict[str, int] = field(default_factory=dict)

    @property
    def expect(self) -> dict[str, int]:
        floor = _REQUIRES_RANK[self.requires]
        out: dict[str, int] = {"none": 401}
        for identity, rank in _IDENTITY_RANK.items():
            if rank < floor:
                assert identity not in self.overrides, (
                    f"below-floor cell {identity!r} must be derived, not written"
                )
                out[identity] = 403
            else:
                out[identity] = self.overrides.get(identity, 200)
        return out


def _member_path(template: str):
    """{member_id}-target rows: positive admin faces get a FRESH scratch member
    (the row may mutate/destroy it); everyone else aims at agent A."""

    def build(ctx: Ctx, identity: str) -> str:
        target = (
            ctx.fresh_member() if identity in _ADMIN_FACES else ctx.agent_a.member_id
        )
        return template.format(member_id=target)

    return build


def _matrix_webhook_requests_path(ctx: Ctx) -> str:
    """A fresh webhook endpoint on agent A (unique id per cell — the deny faces
    403 before resolve; only the owner face reads it)."""
    endpoint_id = f"conf-hook-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        f"/api/members/{ctx.agent_a.member_id}/webhooks",
        json={"endpoint_id": endpoint_id},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch webhook failed: {r.status_code} {r.text}"
    return f"/api/members/{ctx.agent_a.member_id}/webhooks/{endpoint_id}/requests"


def _matrix_card(ctx: Ctx) -> str:
    """A fresh WAITING reply card (owner-opened scratch — the matrix probes
    authz faces, not initiator semantics)."""
    r = ctx.client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "conf matrix scratch card",
              "options": ["AI pick", "other"]},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch card failed: {r.status_code} {r.text}"
    return r.json()["id"]


def _matrix_answered_card(ctx: Ctx) -> str:
    """A fresh ANSWERED reply card (the PUT re-answer positive face's target)."""
    card_id = _matrix_card(ctx)
    r = ctx.client.post(
        f"/api/reply-cards/{card_id}/answer",
        json={"option_idx": 0},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch answer failed: {r.status_code} {r.text}"
    return card_id


def _matrix_task(ctx: Ctx) -> str:
    """A fresh ad-hoc task EXECUTED BY agent A (owner-created scratch — the
    matrix probes authz faces; agent_self rows then act as the executor)."""
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf matrix task",
              "executor_member_id": ctx.agent_a.member_id},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch task failed: {r.status_code} {r.text}"
    return r.json()["task"]["id"]


def _matrix_task_step(ctx: Ctx, gate: bool = False) -> tuple[str, str]:
    """A fresh task with one planned (pending) step (optionally a gate);
    returns (task_id, step_id). Task status is DERIVED from the steps now
    (T-9ca5) — no task-level 'start' report exists any more."""
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    task_id = _matrix_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "conf step", "dod": "done when asserted",
                         "is_gate": gate}]},
        headers=h,
    )
    assert r.status_code == 200, f"scratch plan failed: {r.status_code} {r.text}"
    step_id = r.json()["steps"][0]["id"]
    if gate:
        # A gate can arm only on an in_progress task — report the step
        # in_progress (owner drives it via admin capability) so the task derives
        # in_progress before the gate route fires. The step-status route case
        # (gate=False) leaves the step pending for its own pending→in_progress.
        r = ctx.client.post(
            f"/api/tasks/{task_id}/steps/{step_id}/status",
            json={"status": "in_progress"}, headers=h,
        )
        assert r.status_code == 200, f"scratch step start failed: {r.status_code} {r.text}"
    return task_id, step_id


def _matrix_closed_task(ctx: Ctx) -> str:
    """A fresh TERMINATED task executed by agent A (close-out targets are
    terminal-only; the owner's terminate closes it without touching the
    closeout stamp)."""
    task_id = _matrix_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/terminate",
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch terminate failed: {r.status_code} {r.text}"
    return task_id


def _matrix_manual(ctx: Ctx) -> str:
    """A fresh task manual (owner-created scratch); returns its type_key."""
    type_key = f"conf-type-{uuid.uuid4().hex[:8]}"
    r = ctx.client.post(
        "/api/task-manuals", json={"type_key": type_key},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch manual failed: {r.status_code} {r.text}"
    return type_key


def _matrix_task_artifact(ctx: Ctx) -> tuple[str, str]:
    """A fresh task (executed by agent A) with ONE link artifact pinned by the
    owner; returns (task_id, artifact_id) — the remove face's target. Only the
    at-or-above-floor faces (owner/admin) reach the handler; the deny faces 403
    before the artifact is ever read."""
    task_id = _matrix_task(ctx)
    r = ctx.client.post(
        f"/api/tasks/{task_id}/artifact",
        json={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
        headers={"Authorization": f"Bearer {ctx.owner_token}"},
    )
    assert r.status_code == 200, f"scratch artifact failed: {r.status_code} {r.text}"
    return task_id, r.json()["artifacts"][0]["id"]


def _matrix_reassigning_task(ctx: Ctx) -> str:
    """A fresh task under the `reassigning` LOCK whose NEW executor is agent A —
    so agent_self (A) may claim it (2xx, lock cleared) while agent_other (B) is
    403 (executor guard). Created executed by a fresh member, then the owner
    reassigns it (kind=member) to agent A → lock=reassigning, executor=A."""
    h = {"Authorization": f"Bearer {ctx.owner_token}"}
    r = ctx.client.post(
        "/api/tasks",
        json={"title": "conf matrix reassigning task",
              "executor_member_id": ctx.fresh_member()},
        headers=h,
    )
    assert r.status_code == 200, f"scratch reassign-seed failed: {r.status_code} {r.text}"
    task_id = r.json()["task"]["id"]
    r = ctx.client.post(
        f"/api/tasks/{task_id}/reassign",
        json={"target": {"kind": "member", "member_id": ctx.agent_a.member_id}},
        headers=h,
    )
    assert r.status_code == 200, f"scratch reassign failed: {r.status_code} {r.text}"
    assert r.json()["lock"] == "reassigning", r.text
    return task_id


MATRIX: dict[str, Route] = {
    # ── infra seams ──────────────────────────────────────────────────────────
    "GET /api/events": Route(
        requires="machine",
        notes="status-only stream probe (see _probe_sse)",
    ),
    "POST /api/mcp": Route(
        requires="machine",
        body={"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
    ),
    "POST /api/mint": Route(
        requires="owner",
        body=lambda ctx, _i: {"member_id": ctx.agent_a.member_id, "ttl_days": 1},
    ),
    # ── owner credential + settings (B3) ─────────────────────────────────────
    "POST /api/auth/change-password": Route(
        # positive faces send a WRONG current password (401): a real change
        # would rotate the shared owner credential and revoke the session
        # token fixture, poisoning every later test. The authz choke is what
        # this matrix pins; the full change semantics live in the server unit
        # tests (api_settings_test.go).
        requires="owner",
        overrides={"owner": 401},
        body={"current_password": "conf-wrong-current", "new_password": "conf-new-password"},
    ),
    "GET /api/settings": Route(requires="owner"),
    "GET /api/release/check": Route(
        # The harness pins $OC_RELEASE_API_BASE at an unroutable loopback
        # (run.sh), so the owner's positive authz face deterministically
        # answers the honest degraded 200 {"status":"unknown"} — never the
        # real GitHub. Full verdict semantics live in the server unit tests
        # (update_check_test.go).
        requires="owner",
    ),
    "POST /api/update/upgrade": Route(
        # With $OC_RELEASE_API_BASE unroutable (run.sh) no newer GitHub
        # release is ever known, so the owner's positive authz face
        # deterministically answers the "no newer release known" 409 — the
        # trigger's full precondition/execution semantics (the real
        # download+verify+swap+restart body) live in the server unit tests
        # (update_check_test.go / upgrade_test.go).
        requires="owner",
        overrides={"owner": 409},
    ),
    "PATCH /api/settings": Route(
        requires="owner",
        body={},  # empty patch = validated no-op read (mutating nothing)
    ),
    # ── members ──────────────────────────────────────────────────────────────
    "GET /api/members": Route(requires="machine"),
    "POST /api/members": Route(
        requires="machine",
        body={"name": "conf-hire-scratch"},
        notes=(
            "a PLAIN hire (name only) stays open at the machine floor; the "
            "privilege-bearing kind/role_key faces are pinned in "
            "test_hire_escalation_denied below"
        ),
    ),
    "GET /api/members/{member_id}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/members/{ctx.agent_a.member_id}",
    ),
    "PATCH /api/members/{member_id}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/members/{ctx.agent_a.member_id}",
        body={"name": "conf-agent-a"},
        notes="member PATCH (name/model/effort) carries no admin choke",
    ),
    "POST /api/members/{member_id}/activate": Route(
        requires="admin_agent",
        path=_member_path("/api/members/{member_id}/activate"),
        body={},
    ),
    "POST /api/members/{member_id}/relocate": Route(
        # placement-only 改機器 (admin floor). machine_id="auto" is validation-
        # exempt (no real machine to resolve in the black box), so the admin
        # positive faces land 200 on a fresh member; below-floor derive to 403.
        requires="admin_agent",
        path=_member_path("/api/members/{member_id}/relocate"),
        body={"machine_id": "auto"},
    ),
    "POST /api/members/{member_id}/deactivate": Route(
        requires="admin_agent",
        path=_member_path("/api/members/{member_id}/deactivate"),
    ),
    "POST /api/members/{member_id}/force-stop": Route(
        requires="admin_agent",
        path=_member_path("/api/members/{member_id}/force-stop"),
    ),
    "POST /api/members/{member_id}/refocus": Route(
        # positive faces: 409 — the fresh target is OFFLINE and refocus is
        # online-only; the choke face (past the admin guard) is what we pin.
        requires="admin_agent",
        overrides={"owner": 409, "admin_agent": 409},
        path=_member_path("/api/members/{member_id}/refocus"),
    ),
    "DELETE /api/members/{member_id}": Route(
        requires="admin_agent",
        path=_member_path("/api/members/{member_id}"),
    ),
    # ── webhooks (M4) — a member's 回呼端點 config CRUD; machine floor like the
    # members CRUD (no admin choke). GET/POST aim at agent A (list is safe; each
    # create uses a UNIQUE endpoint_id so the per-identity faces never collide on
    # the per-member uniqueness 409). PATCH/DELETE are DEGRADED — probed with a
    # missing endpoint id (404 across authenticated identities) — see DEGRADED.
    "GET /api/members/{member_id}/webhooks": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/members/{ctx.agent_a.member_id}/webhooks",
    ),
    "POST /api/members/{member_id}/webhooks": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/members/{ctx.agent_a.member_id}/webhooks",
        body=lambda _ctx, _i: {"endpoint_id": f"conf-hook-{uuid.uuid4().hex[:8]}"},
    ),
    "PATCH /api/members/{member_id}/webhooks/{endpoint_id}": Route(
        requires="machine",
        overrides={i: 404 for i in _IDENTITY_RANK},
        path=lambda ctx, _i: (
            f"/api/members/{ctx.agent_a.member_id}/webhooks/ep-conf-missing"
        ),
        body={"status": "disabled"},
    ),
    "DELETE /api/members/{member_id}/webhooks/{endpoint_id}": Route(
        requires="machine",
        overrides={i: 404 for i in _IDENTITY_RANK},
        path=lambda ctx, _i: (
            f"/api/members/{ctx.agent_a.member_id}/webhooks/ep-conf-missing"
        ),
    ),
    # The /in debug ring buffer is OWNER-ONLY (raw unverified external
    # payloads): every below-owner identity — admin agent included — is a
    # derived flat 403. The owner face reads a freshly seeded real endpoint.
    "GET /api/members/{member_id}/webhooks/{endpoint_id}/requests": Route(
        requires="owner",
        path=lambda ctx, _i: _matrix_webhook_requests_path(ctx),
    ),
    # ── self-report presence (identity from token, no target param) ─────────
    "POST /api/self/waking": Route(
        # owner: sub="owner" has no roster row → 404 (self-report is agent-only
        # by construction). Every roster-backed principal reports for ITSELF.
        requires="machine",
        overrides={"owner": 404},
        body={},
    ),
    "POST /api/self/stopping": Route(
        requires="machine", overrides={"owner": 404}, body={}
    ),
    "POST /api/self/stopped": Route(
        requires="machine", overrides={"owner": 404}, body={}
    ),
    "POST /api/self/refocus": Route(
        # restart_self: a self-op at the machine floor. owner (sub="owner") has
        # no roster row → 404 (self-op is agent-only by construction). Every
        # roster-backed identity resolves fine but is OFFLINE in the black-box
        # harness (no live SSE), so the online-only guard answers 409 — the
        # positive 200 stamp path (online + past the liveness floor) and the
        # 429 minimum-liveness refusal are pinned in the server unit tests
        # (api_members_restartself_test.go).
        requires="machine",
        overrides={"owner": 404, "agent_self": 409, "agent_other": 409,
                   "admin_agent": 409, "warden": 409},
        body={},
    ),
    # ── chat ─────────────────────────────────────────────────────────────────
    "POST /api/chat": Route(
        requires="machine",
        body={"to": "owner", "body": "conformance ping"},
    ),
    "GET /api/chat": Route(requires="machine"),
    "GET /api/chat/attachment/{attachment_id}": Route(
        # unknown blob id: authz face passes, lookup 404s — uniform across
        # authenticated identities (no attachment fixture in batch 1).
        requires="machine",
        overrides={i: 404 for i in _IDENTITY_RANK},
        path="/api/chat/attachment/att-conf-missing",
    ),
    "GET /api/chat/attachments": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/chat/attachments?with={ctx.agent_a.member_id}",
    ),
    "GET /api/chat/attachments/{attachment_id}/share-link": Route(
        # unknown blob id: authz face passes, lookup 404s — same probe shape as
        # the attachment blob row above; the sig SEMANTICS (single-file grant,
        # bad sig 401, sig scoped to the blob route) are pinned in
        # test_rest_happy.py where the attachment fixture lives.
        requires="machine",
        overrides={i: 404 for i in _IDENTITY_RANK},
        path="/api/chat/attachments/att-conf-missing/share-link",
    ),
    "POST /api/chat/attachments": Route(
        # the matrix harness only speaks JSON bodies; an EMPTY octet-stream
        # body probes the authz choke (fires before the handler), and every
        # at-floor face then hits the handler's "attachment is empty" 400.
        # The positive upload semantics live in test_rest_happy.py.
        requires="machine",
        overrides={i: 400 for i in _IDENTITY_RANK},
    ),
    "POST /api/chat/mark-read": Route(
        requires="machine",
        body={"peer": "owner", "last_read_ts": 1.0},
    ),
    "GET /api/chat/reads": Route(requires="machine"),
    "GET /api/chat/unread-count": Route(requires="machine"),
    # ── reply cards ──────────────────────────────────────────────────────────
    "POST /api/reply-cards": Route(
        requires="machine",
        body={"kind": "decision", "summary": "conf matrix card",
              "options": ["AI pick", "other"]},
    ),
    "GET /api/reply-cards": Route(requires="machine"),
    "GET /api/reply-cards/count": Route(requires="machine"),
    "GET /api/reply-cards/{card_id}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/reply-cards/{_matrix_card(ctx)}",
    ),
    "POST /api/reply-cards/{card_id}/answer": Route(
        # answering is the OWNER's act (requires=owner): the positive face
        # answers a fresh waiting card; below-owner identities are choked 403
        # before target resolution (a missing id never leaks a 404 oracle).
        requires="owner",
        path=lambda ctx, i: (
            f"/api/reply-cards/"
            f"{_matrix_card(ctx) if i == 'owner' else 'rc-conf-missing'}/answer"
        ),
        body={"option_idx": 0},
    ),
    "PUT /api/reply-cards/{card_id}/answer": Route(
        requires="owner",
        path=lambda ctx, i: (
            f"/api/reply-cards/"
            f"{_matrix_answered_card(ctx) if i == 'owner' else 'rc-conf-missing'}"
            "/answer"
        ),
        body={"text": "conf matrix revised answer"},
    ),
    "POST /api/reply-cards/{card_id}/expire": Route(
        # expiring is the OWNER's act too (requires=owner) and one-shot
        # terminal, so the positive face burns a FRESH waiting card per
        # invocation; below-owner identities are choked 403 before target
        # resolution (a missing id never leaks a 404 oracle).
        requires="owner",
        path=lambda ctx, i: (
            f"/api/reply-cards/"
            f"{_matrix_card(ctx) if i == 'owner' else 'rc-conf-missing'}/expire"
        ),
    ),
    # ── telemetry / monitoring ───────────────────────────────────────────────
    "POST /api/agent/context": Route(requires="machine", body={"context_pct": 5}),
    "POST /api/monitoring/telemetry": Route(
        requires="machine", body={"rate_limits": {}}
    ),
    "GET /api/monitoring": Route(requires="machine"),
    # ── display-name overlays ────────────────────────────────────────────────
    "PATCH /api/accounts/{account_id}": Route(
        requires="machine",
        path="/api/accounts/conf-account-tag",
        body={"display_name": "Conf Account"},
    ),
    "PATCH /api/machines/{machine_id}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/machines/{ctx.machine_id}",
        body={"display_name": "Conf Machine"},
    ),
    # ── machines ─────────────────────────────────────────────────────────────
    "GET /api/machines": Route(requires="machine"),
    "POST /api/machines": Route(
        requires="admin_agent",
        body={"display_name": "conf-machine-matrix"},
    ),
    "GET /api/machines/{machine_id}/boot-command": Route(
        requires="admin_agent",
        path=lambda ctx, _i: f"/api/machines/{ctx.machine_id}/boot-command",
    ),
    "POST /api/machines/{machine_id}/bootstrap-here": Route(
        # DEGRADED positive face: unknown machine id → 404 (resolve runs AFTER
        # the owner-only choke, BEFORE the host subprocess) — never installs.
        requires="owner",
        overrides={"owner": 404},
        path="/api/machines/m-conf-missing/bootstrap-here",
    ),
    "POST /api/machines/{machine_id}/teardown-here": Route(
        # DEGRADED positive face: same reasoning as bootstrap-here.
        requires="owner",
        overrides={"owner": 404},
        path="/api/machines/m-conf-missing/teardown-here",
    ),
    "POST /api/machines/{member_id}/uninstall": Route(
        # positive faces: the scratch machine's warden is OFFLINE → treated as
        # already uninstalled (200, dispatched=false) — no live command path.
        requires="admin_agent",
        path=lambda ctx, _i: f"/api/machines/{ctx.machine_id}/uninstall",
    ),
    "POST /api/machines/{member_id}/upgrade": Route(
        # positive face: the scratch machine's warden is OFFLINE → nothing to
        # command (200, dispatched=false) — fire-and-forget, no durable write.
        requires="owner",
        path=lambda ctx, _i: f"/api/machines/{ctx.machine_id}/upgrade",
    ),
    "DELETE /api/machines/{member_id}": Route(
        requires="admin_agent",
        path=lambda ctx, i: (
            f"/api/machines/"
            f"{ctx.fresh_machine() if i in _ADMIN_FACES else ctx.machine_id}"
        ),
    ),
    # ── global context / roles / lessons / bootstrap ─────────────────────────
    "GET /api/global-context": Route(requires="machine"),
    "POST /api/global-context": Route(
        requires="admin_agent",
        body={"text": "conformance user-custom block"},
    ),
    "POST /api/global-context/reset": Route(requires="admin_agent"),
    "GET /api/roles": Route(requires="machine"),
    "POST /api/roles": Route(
        requires="admin_agent",
        body={"name": "Conf Matrix Role"},
    ),
    "GET /api/roles/{role}": Route(requires="machine", path="/api/roles/assistant"),
    "POST /api/roles/{role}": Route(
        requires="admin_agent",
        path=lambda ctx, i: (
            f"/api/roles/{ctx.fresh_role() if i in _ADMIN_FACES else 'assistant'}"
        ),
        body={"name": "Conf Renamed Role"},
    ),
    "POST /api/roles/{role}/reset": Route(
        requires="admin_agent",
        path="/api/roles/assistant/reset",
    ),
    "DELETE /api/roles/{role}": Route(
        # positive faces: a FRESH custom role hard-deletes clean; below-floor
        # identities hit the governance choke (403) before any target logic.
        requires="admin_agent",
        path=lambda ctx, i: (
            f"/api/roles/{ctx.fresh_role() if i in _ADMIN_FACES else 'assistant'}"
        ),
    ),
    "GET /api/lessons/{role_key}/{task_type}": Route(
        requires="machine",
        path="/api/lessons/assistant/general",
    ),
    "POST /api/lessons/{role_key}/{task_type}": Route(
        # per-role write authz ABOVE the machine floor (handler-level): owner
        # writes ANY role; an agent-scoped caller writes ONLY its OWN member's
        # role_key. admin_agent's role IS assistant → 200; the warden carries
        # role_key="" → 403; agent B aims at assistant → 403 (cross-role
        # poison denied); agent A writes its own role → 200.
        requires="machine",
        overrides={"warden": 403, "agent_other": 403},
        path=lambda ctx, i: (
            f"/api/lessons/{ctx.agent_a.role_key}/general"
            if i == "agent_self"
            else "/api/lessons/assistant/general"
        ),
        body={"text": "conformance lessons doc"},
    ),
    "POST /api/lessons/{role_key}/{task_type}/patch": Route(
        # anchor-addressed patch (T-8327): SAME per-role write authz seam as
        # the whole-doc replace above (warden role_key="" → 403; agent B on
        # assistant → 403; owner/admin/agent-self → 200). Positive faces use an
        # always-valid APPEND edit (empty old) so cell order never matters.
        requires="machine",
        overrides={"warden": 403, "agent_other": 403},
        path=lambda ctx, i: (
            f"/api/lessons/{ctx.agent_a.role_key}/general/patch"
            if i == "agent_self"
            else "/api/lessons/assistant/general/patch"
        ),
        body={"edits": [{"old": "", "new": "conformance patch probe"}]},
    ),
    "GET /api/resume-summary": Route(requires="machine"),
    "GET /api/resume-summary-size": Route(requires="machine"),
    "POST /api/bootstrap": Route(
        # positive faces: {} = UI preview (default role, no token minted) → 200.
        requires="admin_agent",
        body={},
    ),
    # ── tasks (M3) — the FIRST requires="agent" rows: warden (rank 0) is
    # below the agent floor (rank 1) and derives to 403; the executor guard
    # (caller == executor unless admin capability) shows as agent_other=403.
    "GET /api/tasks": Route(requires="machine"),
    "POST /api/tasks": Route(
        requires="agent",
        body=lambda ctx, _i: {"title": "conf matrix create",
                              "executor_member_id": ctx.agent_a.member_id},
    ),
    "GET /api/tasks/count": Route(requires="machine"),
    "GET /api/tasks/{task_id}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}",
    ),
    "POST /api/tasks/{task_id}/terminate": Route(
        # owner-only: below-owner identities choke 403 BEFORE target
        # resolution (missing-id probe, the reply-card answer posture).
        requires="owner",
        path=lambda ctx, i: (
            f"/api/tasks/{_matrix_task(ctx) if i == 'owner' else 't-conf-missing'}"
            "/terminate"
        ),
    ),
    "POST /api/tasks/{task_id}/priority": Route(
        # T-0786: the executor may set high|mid|low on their OWN task; a
        # foreign agent is the executor-guard 403. frozen stays owner-only
        # (handler detail, pinned in test_tasks.py / api_tasks_test.go).
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/priority",
        body={"priority": "high"},
    ),
    "POST /api/tasks/{task_id}/message": Route(
        requires="owner",
        path=lambda ctx, i: (
            f"/api/tasks/{_matrix_task(ctx) if i == 'owner' else 't-conf-missing'}"
            "/message"
        ),
        body={"body": "conf matrix task message"},
    ),
    "POST /api/tasks/{task_id}/plan": Route(
        # executor guard: agent B pushing agent A's task is a flat 403;
        # admin capability (owner/admin_agent) passes (§14 convention).
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/plan",
        body={"steps": [{"name": "conf", "dod": "asserted"}]},
    ),
    "POST /api/tasks/{task_id}/status": Route(
        # update_task_status is RETIRED (T-9ca5): task status is DERIVED from the
        # steps, so the endpoint refuses every report AFTER the authz gate. The
        # executor guard still runs first (agent_other → 403); the at-floor faces
        # (owner/admin/agent_self) reach the 409 retirement refusal.
        requires="agent",
        overrides={"agent_other": 403, "agent_self": 409,
                   "admin_agent": 409, "owner": 409},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/status",
        body={"status": "in_progress"},
    ),
    "POST /api/tasks/{task_id}/duplicate": Route(
        # T-02c9: executor-guarded like every agent report row (agent B on
        # agent A's task → 403); admin capability (owner/admin_agent) passes.
        # Subject AND original are both FRESH scratch tasks per invocation, so
        # the at-floor 200 faces never collide on an already-terminal subject.
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/duplicate",
        body=lambda ctx, _i: {"duplicate_of": _matrix_task(ctx)},
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/status": Route(
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: "/api/tasks/{}/steps/{}/status".format(
            *_matrix_task_step(ctx)),
        body={"status": "in_progress"},
    ),
    "POST /api/tasks/{task_id}/steps/{step_id}/gate": Route(
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: "/api/tasks/{}/steps/{}/gate".format(
            *_matrix_task_step(ctx, gate=True)),
        body={"kind": "decision", "summary": "conf matrix gate",
              "options": ["go", "hold"]},
    ),
    "POST /api/tasks/{task_id}/deps": Route(
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/deps",
        body={"blocked_by": []},
    ),
    "POST /api/tasks/{task_id}/closeout": Route(
        # §6.3 close-out report: executor-guarded like every agent report
        # row (agent B on agent A's task → 403); idempotent, so the admin
        # faces re-reporting after agent_self is still a 200 no-op.
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_closed_task(ctx)}/closeout",
    ),
    "POST /api/tasks/{task_id}/reassign": Route(
        # ② owner G1: reassign is opened to `agent` (was admin_agent). The route
        # floor is now `agent`; the handler's executor guard then keeps a plain
        # agent to its OWN task — the scratch task is executed by agent A, so
        # agent_self (A) hands it over → 2xx, while agent_other (B, NOT the
        # executor) is 403 despite clearing the class floor (hence the explicit
        # override — the framework's requires-rank cannot model a task-level
        # executor guard). owner/admin drive any task; warden is below-floor 403.
        # Positive faces hand a fresh scratch task to a fresh scratch member
        # (admin) / to agent_b (agent_self — an active hired member, valid target).
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/reassign",
        body=lambda ctx, identity: {
            "target": {
                "kind": "member",
                "member_id": ctx.fresh_member()
                if identity in _ADMIN_FACES
                else ctx.agent_b.member_id,
            }
        },
    ),
    "POST /api/tasks/{task_id}/claim": Route(
        # T-9ca5 claim (takeover): the NEW executor takes over a reassigned task,
        # clearing the reassigning lock. Executor-guarded like reassign — the
        # task is reassigned TO agent A, so agent_self (A) claims it (2xx, lock
        # cleared) while agent_other (B, NOT the executor) is 403 despite
        # clearing the class floor. owner/admin drive any task; warden below-floor.
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_reassigning_task(ctx)}/claim",
    ),
    "POST /api/tasks/{task_id}/artifact": Route(
        # T-3dc5: the executing agent pins a deliverable — executor-guarded like
        # every agent write row (agent B on agent A's task → 403); admin
        # capability (owner/admin_agent) passes. A link artifact needs no upload,
        # so every at-floor face registers a valid one on a FRESH scratch task.
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: f"/api/tasks/{_matrix_task(ctx)}/artifact",
        body={"kind": "link", "url": "https://example.com/pr/1", "label": "conf PR"},
    ),
    "DELETE /api/tasks/{task_id}/artifact/{artifact_id}": Route(
        # T-3dc5 owner ruling 2026-07-18: un-pin has the SAME model as add — the
        # executing agent removes its own task's deliverables (agent B on agent
        # A's task → 403); admin capability (owner/admin_agent) removes on any
        # task. The scratch task is executed by agent A, so agent_self passes.
        requires="agent",
        overrides={"agent_other": 403},
        path=lambda ctx, _i: "/api/tasks/{}/artifact/{}".format(
            *_matrix_task_artifact(ctx)),
    ),
    "GET /api/self/task": Route(
        # identity-locked worker claim: NO black-box identity has a worker
        # row (the Phase 2 scheduler mints them), so every at-floor face is
        # an honest 404 — the warden's 403 (below the agent floor) is the
        # row's real teeth.
        requires="agent",
        overrides={"owner": 404, "admin_agent": 404,
                   "agent_self": 404, "agent_other": 404},
    ),
    # ── outsource panel (M3) ────────────────────────────────────────────────
    "GET /api/outsource-workers": Route(requires="machine"),
    "GET /api/outsource-workers/{id}": Route(
        # T-f190 detail-panel single read. NO black-box identity has a worker row
        # (the Phase 2 scheduler mints them), so every at-floor face is an honest
        # 404 — the anonymous 401 is the gate's teeth. Mirrors GET /api/self/task.
        requires="machine",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope",
        overrides={"warden": 404, "agent_self": 404, "agent_other": 404,
                   "admin_agent": 404, "owner": 404},
    ),
    "GET /api/outsource-workers/{id}/boot-context": Route(
        # T-ba6b initial-prompt preview — owner-only (the text embeds the full
        # task + manual, MCPExclude). Below-owner faces are a flat 403 (the
        # gate's teeth); the owner face is an honest 404 against the unknown
        # ow-nope row (no black-box worker exists — same as the worker ops).
        requires="owner",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/boot-context",
        overrides={"owner": 404},
    ),
    "POST /api/outsource-workers/{id}/relocate": Route(
        # T-f190 改機器; P7c drops the floor to admin_agent (外包對齊正職 — the
        # exact member relocate floor). Below-admin faces are a flat 403 (the
        # gate's teeth); the admin/owner faces are an honest 404 (no black-box
        # worker row). machine_id "auto" skips the machine resolve so the 404
        # is the worker's, not the machine's.
        requires="admin_agent",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/relocate",
        body=lambda _ctx, _i: {"machine_id": "auto"},
        overrides={"admin_agent": 404, "owner": 404},
    ),
    # T-32e1/T-f190 worker lifecycle ops — all owner-only (relocate above sits
    # at the admin floor since P7c; these stay owner-only until their own
    # alignment ruling). Below-owner faces are a flat 403 (the gate's teeth);
    # the owner face is an honest 404 against the unknown ow-nope row (no
    # black-box worker exists).
    "POST /api/outsource-workers/{id}/refocus": Route(
        requires="owner",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/refocus",
        overrides={"owner": 404},
    ),
    "POST /api/outsource-workers/{id}/stop": Route(
        requires="owner",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/stop",
        overrides={"owner": 404},
    ),
    "POST /api/outsource-workers/{id}/restart": Route(
        requires="owner",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/restart",
        overrides={"owner": 404},
    ),
    "POST /api/outsource-workers/{id}/model": Route(
        requires="owner",
        path=lambda _ctx, _i: "/api/outsource-workers/ow-nope/model",
        body=lambda _ctx, _i: {"model": "claude-opus-4-8"},
        overrides={"owner": 404},
    ),
    # ── task manuals (M3) ───────────────────────────────────────────────────
    "GET /api/task-manuals": Route(requires="machine"),
    "POST /api/task-manuals": Route(
        # agent floor (owner ruling 2026-07-13): any agent may author a new
        # task type; the assignee governance 403 is pinned in test_tasks.py.
        requires="agent",
        body=lambda _ctx, _i: {"type_key": f"conf-type-{uuid.uuid4().hex[:8]}"},
    ),
    "GET /api/task-manuals/{type_key}": Route(
        requires="machine",
        path=lambda ctx, _i: f"/api/task-manuals/{_matrix_manual(ctx)}",
    ),
    "POST /api/task-manuals/{type_key}": Route(
        # agent floor (owner ruling 2026-07-13): the CONTENT fields are
        # agent-editable; the assignee owner-only 403 is pinned in
        # test_tasks.py (this body is content-only on purpose).
        requires="agent",
        path=lambda ctx, i: (
            f"/api/task-manuals/{_matrix_manual(ctx)}"
            if i in ("owner", "admin_agent", "agent_self", "agent_other")
            else "/api/task-manuals/conf-missing-type"
        ),
        body={"purpose": "conf matrix manual edit"},
    ),
    "DELETE /api/task-manuals/{type_key}": Route(
        requires="owner",
        path=lambda ctx, i: (
            f"/api/task-manuals/"
            f"{_matrix_manual(ctx) if i == 'owner' else 'conf-missing-type'}"
        ),
    ),
    "POST /api/task-manuals/{type_key}/learnings": Route(
        # the agent write-back face: any agent may fold learnings in (floor
        # agent — the write is per-type, not per-executor).
        requires="agent",
        path=lambda ctx, _i: f"/api/task-manuals/{_matrix_manual(ctx)}/learnings",
        body={"text": "conf matrix learnings"},
    ),
}

# Manifest rows deliberately NOT in the matrix (must carry a reason — the
# coverage test enforces the union). Batch 1 skips nothing outright.
SKIPPED: dict[str, str] = {}

# Honest degradations: covered in the matrix, but with a weaker probe than the
# route's full semantics. Reported here so nothing is silently soft.
DEGRADED: dict[str, str] = {
    "POST /api/machines/{machine_id}/bootstrap-here": (
        "owner face uses an UNKNOWN machine id (expects 404): a real target would "
        "run `ocwarden install` on the host under test — a host side effect the "
        "black-box harness must not trigger. The authz faces are fully asserted."
    ),
    "POST /api/machines/{machine_id}/teardown-here": (
        "owner face uses an UNKNOWN machine id (expects 404): a real target would "
        "run `ocwarden teardown` on the host. Authz faces fully asserted."
    ),
    "GET /api/events": (
        "status-code-only SSE probe (connection opened, first bytes not awaited); "
        "stream content/semantics are out of scope for the auth matrix."
    ),
    "GET /api/chat/attachment/{attachment_id}": (
        "probed with a missing attachment id (404 across authenticated "
        "identities); batch 1 posts no attachment fixture."
    ),
    "GET /api/chat/attachments/{attachment_id}/share-link": (
        "probed with a missing attachment id (404 across authenticated "
        "identities); the sig semantics are pinned in test_rest_happy.py."
    ),
    "PATCH /api/members/{member_id}/webhooks/{endpoint_id}": (
        "probed with a missing endpoint id (404 across authenticated identities): "
        "the machine-floor authz face passes, the webhook lookup 404s. The full "
        "status/purpose edit is pinned in test_rest_happy.py."
    ),
    "DELETE /api/members/{member_id}/webhooks/{endpoint_id}": (
        "probed with a missing endpoint id (404 across authenticated identities); "
        "the full revoke round-trip is pinned in test_rest_happy.py."
    ),
    "POST /api/members/{member_id}/refocus": (
        "owner face pinned at 409 (target offline; refocus is online-only) — the "
        "online-positive path needs a live SSE member, out of scope for batch 1."
    ),
    "POST /api/self/refocus": (
        "every roster-backed face pinned at 409 (caller offline; restart_self is "
        "online-only) — the online-positive 200 stamp AND the 429 minimum-liveness "
        "refusal both need a live SSE session with a stamped boot_ts, out of scope "
        "for the black-box harness; pinned in the server unit tests."
    ),
    "POST /api/machines/{member_id}/uninstall": (
        "owner face runs against an OFFLINE warden (200, no command dispatched); "
        "the live-dispatch path needs a connected warden, out of scope."
    ),
    "POST /api/auth/change-password": (
        "owner face pinned at 401 (wrong current password): a real change would "
        "rotate the shared owner credential and revoke the session token fixture. "
        "The full change/revocation semantics are pinned in the server unit tests."
    ),
    "GET /api/self/task": (
        "every at-floor face pinned at 404: an outsource worker identity is "
        "mintable only by the Phase 2 assignment scheduler (no black-box mint "
        "path). The positive claim (assigned → active + manual snapshot) is "
        "pinned in the server unit tests (api_tasks_test.go)."
    ),
}


# ── plumbing ─────────────────────────────────────────────────────────────────


def _resolve(value, ctx: Ctx, identity: str):
    return value(ctx, identity) if callable(value) else value


def _headers(ctx: Ctx, identity: str) -> dict[str, str]:
    token = ctx.token(identity)
    return {"Authorization": f"Bearer {token}"} if token else {}


def _probe_sse(ctx: Ctx, path: str, identity: str) -> int:
    """Open the SSE stream, capture the status line, close immediately."""
    with ctx.client.stream(
        "GET", path, headers=_headers(ctx, identity), timeout=10.0
    ) as r:
        return r.status_code


def _gated_rows(manifest: list[dict[str, str]]) -> list[str]:
    return [
        f"{r['method']} {r['path']}" for r in manifest if r["auth"] != "public"
    ]


_MANIFEST: list[dict[str, str]] = json.loads(
    (pathlib.Path(__file__).parent / "routes_manifest.json").read_text()
)

_PARAMS = [
    pytest.param(key, identity, id=f"{key.replace(' ', ':')}[{identity}]")
    for key in _gated_rows(_MANIFEST)
    if key in MATRIX
    for identity in IDENTITIES
]


@pytest.mark.parametrize(("route_key", "identity"), _PARAMS)
def test_auth_matrix(ctx: Ctx, route_key: str, identity: str) -> None:
    method, template = route_key.split(" ", 1)
    route = MATRIX[route_key]
    path = _resolve(route.path, ctx, identity) or template
    assert "{" not in path, f"unresolved path template for {route_key}: {path}"

    expected = route.expect[identity]

    if route_key == "GET /api/events":
        status = _probe_sse(ctx, path, identity)
    else:
        body = _resolve(route.body, ctx, identity)
        kwargs: dict[str, Any] = {"headers": _headers(ctx, identity)}
        if body is not None:
            kwargs["json"] = body
        status = ctx.client.request(method, path, **kwargs).status_code

    assert status == expected, (
        f"{route_key} as {identity}: expected {expected}, got {status}"
    )


# ── coverage teeth: the matrix must track the manifest, and vice versa ──────


def test_manifest_fully_covered(routes_manifest: list[dict[str, str]]) -> None:
    gated = set(_gated_rows(routes_manifest))
    covered = set(MATRIX) | set(SKIPPED)
    missing = gated - covered
    stale = (set(MATRIX) | set(SKIPPED)) - gated
    assert not missing, f"gated routes with NO matrix row and NO skip reason: {sorted(missing)}"
    assert not stale, f"matrix/skip rows no longer in ROUTE_SPECS: {sorted(stale)}"
    for key, reason in SKIPPED.items():
        assert reason.strip(), f"SKIPPED[{key}] carries no reason (silent skip)"


def test_matrix_requires_match_manifest(
    routes_manifest: list[dict[str, str]],
) -> None:
    """Mechanical tie to the RBAC table: each matrix row's ``requires`` label —
    the value the whole expectation row is DERIVED from — must equal the
    server's committed route-table requires. A server requires change reddens
    this before any cell can silently pass on stale semantics."""
    declared = {
        f"{r['method']} {r['path']}": r["requires"] for r in routes_manifest
    }
    for key, route in MATRIX.items():
        assert route.requires == declared[key], (
            f"{key}: matrix derives from requires={route.requires!r} but the "
            f"manifest declares {declared[key]!r}"
        )
        assert route.expect["none"] == 401, f"{key}: gated must 401 without a token"


# ── RBAC semantic pins (beyond the per-cell status table) ────────────────────


def test_hire_escalation_denied(ctx: Ctx) -> None:
    """Regression pin for the hire privilege-escalation hole: kind / role_key
    are privilege-bearing (kind="warden" → machine principal; role_key=
    "assistant" → admin principal). A below-admin caller carrying either gets a
    flat 403; an admin/owner caller may set them (the fixtures already prove
    the owner-positive face — they hire exactly these rows)."""
    for body in (
        {"name": "conf-escalate-warden", "kind": "warden"},
        {"name": "conf-escalate-admin", "role_key": "assistant"},
    ):
        for identity in ("agent_self", "agent_other", "warden"):
            r = ctx.client.post(
                "/api/members", json=body, headers=_headers(ctx, identity)
            )
            assert r.status_code == 403, (
                f"privileged hire {body} as {identity}: "
                f"expected 403, got {r.status_code}"
            )
    # admin-positive face: an admin agent may hire with privilege-bearing fields.
    r = ctx.client.post(
        "/api/members",
        json={"name": "conf-admin-hires-role", "role_key": "assistant"},
        headers=_headers(ctx, "admin_agent"),
    )
    assert r.status_code == 200, f"admin privileged hire: {r.status_code} {r.text}"


def test_admin_choke_denies_before_resolve(ctx: Ctx) -> None:
    """Deny-first pin: the requires choke runs BEFORE target resolution — a
    plain agent aiming an admin route at a NONEXISTENT member gets 403, never
    404 (no existence oracle), with the unified denial message."""
    r = ctx.client.post(
        "/api/members/m-conf-missing/activate",
        json={},
        headers=_headers(ctx, "agent_self"),
    )
    assert r.status_code == 403, f"expected deny-first 403, got {r.status_code}"
    assert "principal not permitted" in r.text


# ── public surface (the 11 auth="public" rows) ───────────────────────────────

PUBLIC_EXPECT: dict[str, tuple[str, int]] = {
    # route key → (concrete path, expected status with NO credentials)
    "GET /api/health": ("/api/health", 200),
    "GET /api/version": ("/api/version", 200),
    "GET /health": ("/health", 200),
    "GET /version": ("/version", 200),
    "GET /install.sh": ("/install.sh?token=conf-boot-token", 200),
    "GET /api/warden/binary": ("/api/warden/binary", 200),
    "GET /api/agent/binary": ("/api/agent/binary", 200),
    "GET /api/auth/status": ("/api/auth/status", 200),
    # login is public but validated: exercised in test_login_semantics below;
    # here the anonymous face posts a wrong password and must get 401, not 403.
    "POST /api/login": ("/api/login", 401),
    # set-password is public but claim-token gated: the harness sets the
    # password before serve, so the anonymous face is the already-set 409 (the
    # claim token is never consulted — no guessing oracle). The 401 wrong-token
    # and positive faces live in the server unit tests (api_settings_test.go).
    "POST /api/auth/set-password": ("/api/auth/set-password", 409),
    # machine claim is public but code-gated: the anonymous face posts a bogus
    # code and must get the flat 401 (never 403). The positive redeem + the
    # single-use 409-free semantics live in test_rest_happy / test_lifecycle.
    "POST /api/machines/claim": ("/api/machines/claim", 401),
    # the public webhook inlet is token-only (?t=) and SILENT: every case —
    # unknown/absent token, disabled endpoint — answers the same 200 so it never
    # leaks endpoint existence. The anonymous face (no ?t=) is that silent 200.
    "POST /in": ("/in", 200),
}


def test_public_coverage(routes_manifest: list[dict[str, str]]) -> None:
    public = {
        f"{r['method']} {r['path']}" for r in routes_manifest if r["auth"] == "public"
    }
    assert public == set(PUBLIC_EXPECT), (
        f"public rows drifted: manifest={sorted(public)} table={sorted(PUBLIC_EXPECT)}"
    )
    # auth="public" ⟺ requires="public" (the boot assertion's invariant, pinned
    # here on the black-box side of the seam too).
    for r in routes_manifest:
        assert (r["auth"] == "public") == (r["requires"] == "public"), (
            f"{r['method']} {r['path']}: auth={r['auth']!r} vs "
            f"requires={r['requires']!r}"
        )


@pytest.mark.parametrize(
    ("route_key",), [(k,) for k in sorted(PUBLIC_EXPECT)], ids=sorted(PUBLIC_EXPECT)
)
def test_public_routes_serve_without_token(client: httpx.Client, route_key: str) -> None:
    method, _ = route_key.split(" ", 1)
    path, expected = PUBLIC_EXPECT[route_key]
    kwargs: dict[str, Any] = {}
    if route_key == "POST /api/login":
        kwargs["json"] = {"password": "definitely-wrong-password"}
    if route_key == "POST /api/auth/set-password":
        kwargs["json"] = {
            "password": "conf-anonymous-claim",
            "claim_token": "conf-any-token",
        }
    if route_key == "POST /api/machines/claim":
        kwargs["json"] = {"code": "conf-bogus-claim-code"}
    r = client.request(method, path, **kwargs)
    assert r.status_code == expected, f"{route_key}: {r.status_code} != {expected}"
    assert r.status_code not in (403,), "public route must never 403 anonymously"


def test_login_semantics(client: httpx.Client, owner_token: str) -> None:
    """Right password → 200 token (exercised by the owner_token fixture);
    wrong password → 401; missing field → 422."""
    assert owner_token
    assert client.post("/api/login", json={"password": "nope"}).status_code == 401
    assert client.post("/api/login", json={}).status_code == 422


def test_install_script_requires_exactly_one_credential_param(client: httpx.Client) -> None:
    """The installer requires EXACTLY ONE of ?code= / ?token= — neither, or
    both, is a 422."""
    assert client.get("/install.sh").status_code == 422
    assert client.get("/install.sh?token=conf-tok&code=conf-code").status_code == 422


def test_seed_role_delete_is_refused(client: httpx.Client, owner_token: str) -> None:
    """Semantic pin: even the owner cannot hard-delete a SEED role (403)."""
    r = client.delete(
        "/api/roles/assistant", headers={"Authorization": f"Bearer {owner_token}"}
    )
    assert r.status_code == 403
