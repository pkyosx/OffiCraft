"""Error-envelope face — the ONE wire shape every non-2xx response carries.

docs/design/api-error-envelope.md (owner-approved, referenced by spec/sse.md):
every error is ``{"error": {"code": <snake_case>, "message": <human text>}}``
— exactly those keys, nothing else — with ``code`` derived from the HTTP
status via the closed CODE_BY_STATUS vocabulary. This file fires one
representative request per error class (both handler-raised and
framework-raised sources) and pins the envelope:

  * 401 unauthorized   — no token / garbage token / wrong password / a bogus
    machine claim code;
  * 403 forbidden      — a below-floor identity at an owner route;
  * 404 not_found      — semantic (unknown member) AND framework (unknown path);
  * 405 method_not_allowed — framework-raised wrong method on a known path;
  * 409 conflict       — refocus on an offline member (online-only route);
  * 400 validation_error — handler-raised input rejections (bad base64
    attachment; non-numeric context_pct and empty telemetry, which
    lifecycle.md §3 pins as flat 400, NOT 422);
  * 422 validation_error — the request-validation source (framework-level).

The vocabulary table below is re-declared, NOT imported (black-box iron rule).
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Callable

import httpx
import pytest

from conftest import AgentIdentity

# The closed status→code vocabulary (docs/design/api-error-envelope.md).
CODE_BY_STATUS = {
    400: "validation_error",
    401: "unauthorized",
    403: "forbidden",
    404: "not_found",
    405: "method_not_allowed",
    409: "conflict",
    422: "validation_error",
    503: "service_unavailable",
}


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


@dataclass(frozen=True)
class ErrCase:
    """One representative error path: fire → expect (status, envelope code)."""

    status: int
    fire: Callable[[httpx.Client, str, AgentIdentity, Callable[[], str]], httpx.Response]


CASES: dict[str, ErrCase] = {
    "401-no-token": ErrCase(
        401, lambda c, _o, _a, _m: c.get("/api/members")
    ),
    "401-garbage-token": ErrCase(
        401,
        lambda c, _o, _a, _m: c.get(
            "/api/members", headers=_auth("not.a.jwt")
        ),
    ),
    "401-claim-bad-code": ErrCase(
        401,
        lambda c, _o, _a, _m: c.post(
            "/api/machines/claim", json={"code": "conf-envelope-bogus-code"}
        ),
    ),
    "401-wrong-password": ErrCase(
        401,
        lambda c, _o, _a, _m: c.post(
            "/api/login", json={"password": "definitely-wrong"}
        ),
    ),
    "403-below-floor": ErrCase(
        403,
        lambda c, _o, a, _m: c.post(
            "/api/mint",
            json={"member_id": a.member_id, "ttl_days": 1},
            headers=_auth(a.token),
        ),
    ),
    "404-unknown-member": ErrCase(
        404,
        lambda c, o, _a, _m: c.get(
            "/api/members/m-conf-envelope-missing", headers=_auth(o)
        ),
    ),
    "404-unknown-path": ErrCase(
        404, lambda c, _o, _a, _m: c.get("/api/definitely-not-a-route")
    ),
    "405-wrong-method": ErrCase(
        405, lambda c, _o, _a, _m: c.delete("/api/health")
    ),
    "409-refocus-offline": ErrCase(
        409,
        lambda c, o, _a, m: c.post(
            f"/api/members/{m()}/refocus", headers=_auth(o)
        ),
    ),
    "400-attachment-bad-base64": ErrCase(
        400,
        lambda c, o, a, _m: c.post(
            "/api/chat",
            json={
                "to": a.member_id,
                "body": "x",
                "attachments": [{"data_b64": "!!!not-base64!!!"}],
            },
            headers=_auth(o),
        ),
    ),
    # lifecycle.md §3: a non-numeric context_pct is a flat 400 (not 422).
    "400-context-pct-non-numeric": ErrCase(
        400,
        lambda c, _o, a, _m: c.post(
            "/api/agent/context",
            json={"context_pct": "high"},
            headers=_auth(a.token),
        ),
    ),
    # lifecycle.md §3: an all-absent telemetry body is 400.
    "400-telemetry-empty": ErrCase(
        400,
        lambda c, _o, a, _m: c.post(
            "/api/monitoring/telemetry", json={}, headers=_auth(a.token)
        ),
    ),
    "422-login-missing-field": ErrCase(
        422, lambda c, _o, _a, _m: c.post("/api/login", json={})
    ),
    "422-install-missing-token": ErrCase(
        422, lambda c, _o, _a, _m: c.get("/install.sh")
    ),
}


def _assert_envelope(r: httpx.Response, status: int, case: str) -> None:
    assert r.status_code == status, (
        f"{case}: expected {status}, got {r.status_code} {r.text[:300]}"
    )
    assert r.headers.get("content-type", "").startswith("application/json"), (
        f"{case}: error body must be JSON, got {r.headers.get('content-type')}"
    )
    body = r.json()
    assert isinstance(body, dict) and set(body) == {"error"}, (
        f"{case}: envelope must be exactly {{'error': …}}, got {json.dumps(body)[:300]}"
    )
    err = body["error"]
    assert isinstance(err, dict) and set(err) == {"code", "message"}, (
        f"{case}: error body must be exactly {{code, message}}, got "
        f"{json.dumps(err)[:300]}"
    )
    assert err["code"] == CODE_BY_STATUS[status], (
        f"{case}: status {status} must carry code "
        f"{CODE_BY_STATUS[status]!r}, got {err['code']!r}"
    )
    assert isinstance(err["message"], str) and err["message"].strip(), (
        f"{case}: message must be a non-empty human string, got {err['message']!r}"
    )


@pytest.mark.parametrize("case", sorted(CASES), ids=sorted(CASES))
def test_error_envelope(
    client: httpx.Client,
    owner_token: str,
    agent_a: AgentIdentity,
    fresh_member,
    case: str,
) -> None:
    spec = CASES[case]
    r = spec.fire(client, owner_token, agent_a, fresh_member)
    _assert_envelope(r, spec.status, case)


def test_envelope_message_is_human_not_detail(client: httpx.Client) -> None:
    """No legacy ``{"detail": …}`` face (the retired implementation's framework
    default) may survive anywhere — spot
    check both a handler-raised and a framework-raised error."""
    for r in (
        client.get("/api/members"),  # handler-gated 401
        client.get("/api/definitely-not-a-route"),  # framework 404
        client.post("/api/login", json={}),  # RequestValidationError 422
    ):
        body = r.json()
        assert "detail" not in body, (
            f"legacy detail shape leaked: {json.dumps(body)[:200]}"
        )
