"""conformance fixtures — pure HTTP, zero server-implementation imports (black-box iron rule).

Everything the matrix needs is manufactured THROUGH the public API of the
target under test (`OC_TARGET_URL`):

  * ``owner_token``  — POST /api/login with the injected OC_OWNER_PASSWORD;
  * agent identities — POST /api/members (hire) + POST /api/mint (ttl_days);
  * scratch machines / roles — POST /api/machines / POST /api/roles factories.

No fixture ever reaches around the HTTP surface (no DB poke, no import of the
server code), so the same fixtures drive a Python or a Go target unchanged.
"""

from __future__ import annotations

import json
import os
import pathlib
import uuid
from dataclasses import dataclass

import httpx
import pytest

HERE = pathlib.Path(__file__).resolve().parent


def _require_env(name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        raise RuntimeError(
            f"{name} is not set — run this suite via conformance/run.sh "
            "(or export it when pointing at an external target)."
        )
    return value


@pytest.fixture(scope="session")
def base_url() -> str:
    """The target under test. Injected by run.sh; never a hardcoded server."""
    return _require_env("OC_TARGET_URL").rstrip("/")


@pytest.fixture(scope="session")
def routes_manifest() -> list[dict[str, str]]:
    """The committed ROUTE_SPECS snapshot (method/path/auth) the matrix covers."""
    rows = json.loads((HERE / "routes_manifest.json").read_text(encoding="utf-8"))
    assert rows, "routes_manifest.json is empty — it is a frozen committed snapshot (wire freeze)"
    return rows


@pytest.fixture(scope="session")
def client(base_url: str) -> httpx.Client:
    with httpx.Client(base_url=base_url, timeout=10.0) as c:
        yield c


@pytest.fixture(scope="session")
def owner_token(client: httpx.Client) -> str:
    """Owner JWT via the real login flow (password injected via env)."""
    password = _require_env("OC_OWNER_PASSWORD")
    r = client.post("/api/login", json={"password": password})
    assert r.status_code == 200, f"owner login failed: {r.status_code} {r.text}"
    token = r.json()["token"]
    assert token
    return token


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


@dataclass(frozen=True)
class AgentIdentity:
    """One minted agent: roster member id + its scope="agent" JWT."""

    member_id: str
    token: str
    role_key: str


def hire_member(
    client: httpx.Client,
    owner_token: str,
    name: str,
    role_key: str | None = None,
    kind: str | None = None,
) -> str:
    """HTTP hire — returns the server-minted member id. ``role_key``/``kind`` are
    privilege-bearing (RBAC hire guard); the OWNER token carries them here."""
    body: dict[str, object] = {"name": name}
    if role_key is not None:
        body["role_key"] = role_key
    if kind is not None:
        body["kind"] = kind
    r = client.post("/api/members", json=body, headers=_auth(owner_token))
    assert r.status_code == 200, f"hire failed: {r.status_code} {r.text}"
    return r.json()["id"]


def mint_member_token(
    client: httpx.Client, owner_token: str, member_id: str, ttl_days: int = 1
) -> str:
    """HTTP mint — owner-gated long-lived agent JWT (POST /api/mint, ttl_days)."""
    r = client.post(
        "/api/mint",
        json={"member_id": member_id, "ttl_days": ttl_days},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, f"mint failed: {r.status_code} {r.text}"
    return r.json()["token"]


def _make_agent(client: httpx.Client, owner_token: str, tag: str) -> AgentIdentity:
    # A throwaway NON-admin role_key: is_admin keys on role_key == "assistant",
    # so a fresh string keeps these identities ordinary agents (the deny face).
    role_key = f"conf-role-{tag}"
    member_id = hire_member(client, owner_token, f"conf-agent-{tag}", role_key)
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key=role_key)


@pytest.fixture(scope="session")
def agent_a(client: httpx.Client, owner_token: str) -> AgentIdentity:
    """The matrix's SELF agent: {member_id}-target rows aim at this member."""
    return _make_agent(client, owner_token, "a")


@pytest.fixture(scope="session")
def agent_b(client: httpx.Client, owner_token: str) -> AgentIdentity:
    """The matrix's OTHER agent: acts on agent_a's resources cross-identity."""
    return _make_agent(client, owner_token, "b")


@pytest.fixture(scope="session")
def admin_agent(client: httpx.Client, owner_token: str) -> AgentIdentity:
    """An ADMIN-class agent: role_key="assistant" (the admin role the resolver
    keys on). The OWNER hires it — role_key is privilege-bearing, so only an
    owner/admin caller may set it (the RBAC hire guard)."""
    member_id = hire_member(
        client, owner_token, "conf-admin-agent", role_key="assistant"
    )
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="assistant")


@pytest.fixture(scope="session")
def warden_agent(client: httpx.Client, owner_token: str) -> AgentIdentity:
    """A MACHINE-class principal: kind="warden" member (the per-machine executor,
    the capability FLOOR). Owner-hired — kind is privilege-bearing, same guard."""
    member_id = hire_member(client, owner_token, "conf-warden", kind="warden")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="")


@pytest.fixture(scope="session")
def fresh_member(client: httpx.Client, owner_token: str):
    """Factory: a disposable roster member (for destructive owner-face rows)."""

    def make() -> str:
        return hire_member(
            client, owner_token, f"conf-scratch-{uuid.uuid4().hex[:8]}"
        )

    return make


@pytest.fixture(scope="session")
def fresh_machine(client: httpx.Client, owner_token: str):
    """Factory: a disposable onboarded machine (warden member) — via HTTP only."""

    def make() -> str:
        r = client.post(
            "/api/machines",
            json={"display_name": f"conf-machine-{uuid.uuid4().hex[:8]}"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, f"onboard failed: {r.status_code} {r.text}"
        return r.json()["machine_id"]

    return make


@pytest.fixture(scope="session")
def fresh_role(client: httpx.Client, owner_token: str):
    """Factory: a disposable custom role — returns its server-minted role key."""

    def make() -> str:
        r = client.post(
            "/api/roles",
            json={"name": f"Conf Role {uuid.uuid4().hex[:8]}"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, f"create role failed: {r.status_code} {r.text}"
        return r.json()["role"]["key"]

    return make
