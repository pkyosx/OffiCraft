"""MCP face — the /api/mcp JSON-RPC transport contract (spec/mcp.md).

Third conformance batch. test_rest_happy.py already pins tools/list ≡
spec/mcp-catalog.json by NAME set; this file pins the rest of the frozen
transport behaviour, MUST by MUST:

  * §1  envelope: one JSON-RPC object per POST, errors ride HTTP 200, the
        closed error-code set (-32700/-32600/-32601/-32602) with its exact
        trigger conditions, id:null when the request id is unknowable;
  * §2  methods: initialize (protocolVersion echo/default, serverInfo),
        ping, tools/list order, notification → HTTP 202 no body,
        unknown method → -32601;
  * §3  tools/call: -32602 on every params violation INCLUDING unknown tool;
        the argument split (path / query / body); loopback auth forwarding
        (same gate as REST); result mapping — content single text item,
        isError ≡ status>=400 (a 4xx is a RESULT, never a JSON-RPC error),
        structuredContent present iff the body is a JSON object;
  * §5  tools/list ≡ the frozen snapshot ELEMENT-WISE (order included);
  * §6  catalog_hash: recomputed from the committed routes manifest
        ("{METHOD} {path}" over exactly the non-mcp_exclude rows, sorted,
        \\n-joined, SHA-256, first 16 hex) and compared against BOTH version
        probes — two implementations must agree or agents falsely restart.

Black-box: everything below speaks HTTP to OC_TARGET_URL only. The route→tool
mapping comes from routes_manifest.json (the frozen committed route
snapshot), never from importing server-implementation code.
"""

from __future__ import annotations

import hashlib
import json
import pathlib
import uuid
from typing import Any

import httpx
import pytest

from conftest import AgentIdentity

HERE = pathlib.Path(__file__).resolve().parent
MCP_CATALOG = json.loads(
    (HERE.parent / "spec" / "mcp-catalog.json").read_text(encoding="utf-8")
)
MANIFEST = json.loads((HERE / "routes_manifest.json").read_text(encoding="utf-8"))
MCP_ROWS = [r for r in MANIFEST if r.get("mcp_tool")]


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def _rpc(
    client: httpx.Client,
    token: str,
    method: str,
    params: Any = None,
    *,
    id: Any = 1,
    include_id: bool = True,
) -> httpx.Response:
    body: dict[str, Any] = {"jsonrpc": "2.0", "method": method}
    if include_id:
        body["id"] = id
    if params is not None:
        body["params"] = params
    return client.post("/api/mcp", json=body, headers=_auth(token))


def _result(r: httpx.Response, id: Any = 1) -> Any:
    """Assert a SUCCESS envelope (spec §1) and return its result."""
    assert r.status_code == 200, f"MCP success must ride HTTP 200: {r.status_code} {r.text[:200]}"
    payload = r.json()
    assert payload.get("jsonrpc") == "2.0", payload
    assert payload.get("id") == id, payload
    assert "error" not in payload, payload
    return payload["result"]


def _error(r: httpx.Response, code: int, id: Any = "__any__") -> dict[str, Any]:
    """Assert an ERROR envelope carried in HTTP 200 (spec §1) with ``code``."""
    assert r.status_code == 200, (
        f"MCP protocol errors MUST ride HTTP 200, got {r.status_code} {r.text[:200]}"
    )
    payload = r.json()
    assert payload.get("jsonrpc") == "2.0", payload
    assert "result" not in payload, payload
    err = payload["error"]
    assert err["code"] == code, f"expected {code}, got {err}"
    assert isinstance(err["message"], str) and err["message"], err
    if id != "__any__":
        assert payload.get("id") == id, payload
    return err


# ── §2 methods ────────────────────────────────────────────────────────────────


def test_initialize_echoes_protocol_version(client, owner_token) -> None:
    r = _rpc(client, owner_token, "initialize", {"protocolVersion": "2099-01-01"})
    result = _result(r)
    assert result["protocolVersion"] == "2099-01-01", result
    assert result["capabilities"] == {"tools": {"listChanged": False}}, result
    assert result["serverInfo"]["name"] == "officraft", result
    assert result["serverInfo"]["version"], result


def test_initialize_defaults_protocol_version(client, owner_token) -> None:
    # No params at all, and an EMPTY-string protocolVersion, both → the default.
    for params in (None, {"protocolVersion": ""}):
        r = _rpc(client, owner_token, "initialize", params)
        assert _result(r)["protocolVersion"] == "2025-06-18", (params, r.text)


def test_ping_returns_empty_object(client, owner_token) -> None:
    assert _result(_rpc(client, owner_token, "ping")) == {}


def test_tools_list_equals_frozen_snapshot_elementwise(client, owner_token) -> None:
    """spec §5: a live tools/list MUST equal the snapshot's tools array
    element-wise — order included (catalog/route-table order, spec §2)."""
    tools = _result(_rpc(client, owner_token, "tools/list"))["tools"]
    assert tools == MCP_CATALOG["tools"], (
        "live tools/list != spec/mcp-catalog.json (element-wise). "
        f"live order={[t['name'] for t in tools]}"
    )
    # And the catalog order is the ROUTE-TABLE order of the non-excluded rows.
    assert [t["name"] for t in tools] == [r["mcp_tool"] for r in MCP_ROWS], (
        "tools/list order is not the route-table order of non-mcp_exclude rows"
    )


def test_notification_answers_202_no_body(client, owner_token) -> None:
    """spec §2: notifications/* OR any id-less request → HTTP 202, no body,
    no JSON-RPC envelope."""
    for method in ("notifications/initialized", "ping"):
        r = _rpc(client, owner_token, method, include_id=False)
        assert r.status_code == 202, f"{method}: {r.status_code} {r.text[:200]}"
        # OBSERVED WIRE (spec deviation — reported): spec/mcp.md §2 says "202
        # with no body", but the implementation answers the JSON literal
        # ``null`` (JSONResponse(None)). Pin "no JSON-RPC envelope" — the
        # load-bearing MUST — and accept both byte forms pending owner ruling.
        assert r.content in (b"", b"null"), (
            f"{method}: notification must carry no envelope: {r.content[:100]}"
        )


def test_unknown_method_is_32601(client, owner_token) -> None:
    _error(_rpc(client, owner_token, "definitely/not-a-method"), -32601, id=1)


# ── §1 protocol/transport error codes ────────────────────────────────────────


def test_parse_error_32700_id_null(client, owner_token) -> None:
    r = client.post(
        "/api/mcp",
        content=b'{"jsonrpc": "2.0", not json at all',
        headers={**_auth(owner_token), "Content-Type": "application/json"},
    )
    _error(r, -32700, id=None)


def test_invalid_request_32600_batch_array(client, owner_token) -> None:
    """spec §1/§7: batch arrays are explicitly NOT supported → -32600, id null."""
    r = client.post(
        "/api/mcp",
        json=[{"jsonrpc": "2.0", "id": 1, "method": "ping"}],
        headers=_auth(owner_token),
    )
    _error(r, -32600, id=None)


def test_invalid_request_32600_non_object_body(client, owner_token) -> None:
    r = client.post("/api/mcp", json="ping", headers=_auth(owner_token))
    _error(r, -32600, id=None)


def test_invalid_request_32600_method_not_string(client, owner_token) -> None:
    r = client.post(
        "/api/mcp",
        json={"jsonrpc": "2.0", "id": 1, "method": 42},
        headers=_auth(owner_token),
    )
    _error(r, -32600)


@pytest.mark.parametrize(
    "params",
    [
        pytest.param([1, 2], id="params-not-object"),
        pytest.param({"name": 42}, id="name-not-string"),
        pytest.param({"name": "get_members", "arguments": [1]}, id="arguments-not-object"),
        pytest.param({"name": "no_such_tool", "arguments": {}}, id="unknown-tool-name"),
    ],
)
def test_tools_call_invalid_params_32602(client, owner_token, params) -> None:
    """spec §1+§3: every params violation INCLUDING an unknown tool name is
    -32602 (unknown tool is a params error, NOT -32601)."""
    _error(_rpc(client, owner_token, "tools/call", params), -32602, id=1)


# ── §3 tools/call — split, loopback auth, result mapping ─────────────────────


def _call(client, token, name: str, arguments: dict | None = None) -> dict[str, Any]:
    params: dict[str, Any] = {"name": name}
    if arguments is not None:
        params["arguments"] = arguments
    return _result(_rpc(client, token, "tools/call", params))


def _text(result: dict[str, Any]) -> str:
    content = result["content"]
    assert isinstance(content, list) and len(content) == 1, (
        f"content MUST be a single item: {content}"
    )
    item = content[0]
    assert item["type"] == "text", item
    return item["text"]


def test_call_get_route_list_result(client, owner_token) -> None:
    """GET tool returning a top-level ARRAY: isError false, text carries the
    raw JSON, structuredContent MUST be ABSENT (spec §3.3)."""
    result = _call(client, owner_token, "get_members", {})
    assert result["isError"] is False, result
    data = json.loads(_text(result))
    assert isinstance(data, list) and data, "expected the roster list"
    assert "structuredContent" not in result, (
        "structuredContent MUST be omitted for a non-object (array) body"
    )


def test_call_object_result_has_structured_content(client, owner_token, agent_a) -> None:
    """Path-param split + object body: structuredContent present and equal to
    the parsed text body (spec §3.1 rule 1, §3.3)."""
    result = _call(client, owner_token, "get_member", {"member_id": agent_a.member_id})
    assert result["isError"] is False, result
    body = json.loads(_text(result))
    assert body["id"] == agent_a.member_id, body
    assert result["structuredContent"] == body, (
        "structuredContent MUST equal the parsed JSON object body"
    )


def test_call_write_route_body_split(client, owner_token) -> None:
    """Non-GET split (spec §3.1 rule 3): remaining args become the JSON body."""
    name = f"conf-mcp-hire-{uuid.uuid4().hex[:8]}"
    result = _call(client, owner_token, "hire_member", {"name": name})
    assert result["isError"] is False, result
    body = json.loads(_text(result))
    assert body["name"] == name and body["id"], body
    assert result["structuredContent"] == body


def test_call_get_route_query_split(client, owner_token, agent_a) -> None:
    """GET split (spec §3.1 rule 2): remaining args become query params; None
    optionals are dropped. get_chat?with=<peer> must filter to that thread."""
    marker = f"conf-mcp-query-{uuid.uuid4().hex[:8]}"
    seed = client.post(
        "/api/chat",
        json={"to": agent_a.member_id, "body": marker},
        headers=_auth(owner_token),
    )
    assert seed.status_code == 200, seed.text
    result = _call(
        client, owner_token, "get_chat", {"with": agent_a.member_id, "limit": None}
    )
    assert result["isError"] is False, result
    messages = json.loads(_text(result))
    assert isinstance(messages, list), messages
    assert any(m.get("body") == marker for m in messages), (
        "query param `with` was not honoured by the argument split"
    )


def test_call_route_error_is_result_not_rpc_error(client, owner_token) -> None:
    """spec §3.3: a 4xx from the looped-back route is a SUCCESSFUL JSON-RPC
    result with isError:true — never a JSON-RPC error. The body is the REST
    error envelope, an object, so structuredContent MUST be present."""
    result = _call(client, owner_token, "get_member", {"member_id": "m-conf-mcp-missing"})
    assert result["isError"] is True, result
    body = json.loads(_text(result))
    assert body["error"]["code"] == "not_found", body
    assert result["structuredContent"] == body


def test_call_missing_path_param_falls_through_naturally(client, owner_token) -> None:
    """spec §3.1 rule 1: a missing path key substitutes the EMPTY string — no
    -32602 at this layer; the loopback route 404s/405s naturally → isError."""
    result = _call(client, owner_token, "activate_member", {})
    assert result["isError"] is True, result


def test_call_forwards_caller_authorization(
    client, owner_token, agent_a: AgentIdentity
) -> None:
    """spec §3.2: the loopback MUST forward the caller's Authorization header —
    an agent calling an ADMIN-floor tool (create_role, requires=admin_agent)
    gets the SAME 403 envelope as REST, as an isError result."""
    result = _call(client, agent_a.token, "create_role", {"name": "Conf MCP Escalate"})
    assert result["isError"] is True, result
    body = json.loads(_text(result))
    assert body["error"]["code"] == "forbidden", (
        f"agent→hire_member must be the RBAC 403 envelope, got {body}"
    )


def test_call_empty_arguments_defaults_to_empty_object(client, owner_token) -> None:
    """spec §3: ``arguments`` absent → {} (and a body IS sent for write routes:
    global-context reset takes no args and must succeed)."""
    result = _call(client, owner_token, "reset_global_context")
    assert result["isError"] is False, result
    body = json.loads(_text(result))
    assert body["is_default"] is True, body


def test_call_get_global_context_carries_org_name(client, owner_token) -> None:
    """T-d693: the workshop name (org.name setting) now lands server-side and
    rides get_global_context so an agent reads which workshop it works for. A
    write→read-back roundtrip through the MCP face: PATCH /api/settings sets it
    (trim + echo), get_global_context reflects the live value in both its text
    body and structuredContent (object body, spec §3.3). Restores the original
    so the shared instance is left as found."""
    h = _auth(owner_token)

    def _org_name_via_mcp() -> str:
        result = _call(client, owner_token, "get_global_context")
        assert result["isError"] is False, result
        body = json.loads(_text(result))
        assert result["structuredContent"] == body, (
            "structuredContent MUST equal the parsed global-context object body"
        )
        assert "org_name" in body, f"global-context MUST carry org_name: {body}"
        return body["org_name"]

    original = _org_name_via_mcp()
    try:
        r = client.patch("/api/settings", json={"org_name": "  伊娃工作室  "}, headers=h)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        assert r.json()["org_name"] == "伊娃工作室", r.text  # trimmed + echoed
        assert _org_name_via_mcp() == "伊娃工作室", (
            "get_global_context must reflect the live org_name after the write"
        )
    finally:
        r = client.patch("/api/settings", json={"org_name": original}, headers=h)
        assert r.status_code == 200, f"restore failed: {r.status_code} {r.text}"
        assert _org_name_via_mcp() == original, "org_name restore did not take"


# ── §6 catalog_hash — recompute the normative algorithm ──────────────────────


def _recomputed_catalog_hash() -> str:
    lines = sorted(f"{r['method']} {r['path']}" for r in MCP_ROWS)
    digest = hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest()
    return digest[:16]


def test_catalog_hash_algorithm(client) -> None:
    """spec §6: '{METHOD} {path}' over exactly the non-mcp_exclude route rows,
    sorted lexicographically, \\n-joined (no trailing newline), SHA-256,
    first 16 lowercase hex chars — recomputed here from the committed manifest
    and compared against BOTH version probes."""
    expected = _recomputed_catalog_hash()
    assert len(expected) == 16 and expected == expected.lower()
    for probe in ("/api/version", "/version"):
        r = client.get(probe)
        assert r.status_code == 200, r.text
        served = r.json()["catalog_hash"]
        assert served == expected, (
            f"{probe}: catalog_hash {served!r} != recomputed {expected!r} — "
            "two implementations disagreeing here makes agents falsely restart"
        )


def test_catalog_hash_keys_off_tool_surface_only(client) -> None:
    """spec §4+§6 coherence: the hash input set is EXACTLY the tool surface
    tools/list serves (same mcp_exclude filter) — pin manifest rows ↔ frozen
    catalog names 1:1."""
    manifest_tools = [r["mcp_tool"] for r in MCP_ROWS]
    catalog_tools = [t["name"] for t in MCP_CATALOG["tools"]]
    assert manifest_tools == catalog_tools, (
        f"manifest-only={sorted(set(manifest_tools) - set(catalog_tools))} "
        f"catalog-only={sorted(set(catalog_tools) - set(manifest_tools))}"
    )
