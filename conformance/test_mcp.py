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


def test_update_task_status_tool_is_unlisted(client, owner_token) -> None:
    """T-8449: the retired update_task_status tool is REMOVED — absent from a
    live tools/list (positive control: update_step_status, the lever that
    replaced it, IS listed)."""
    names = [t["name"] for t in _result(_rpc(client, owner_token, "tools/list"))["tools"]]
    assert "update_step_status" in names, names  # positive control
    assert "update_task_status" not in names, names


def test_update_task_status_call_is_unknown_tool(client, owner_token) -> None:
    """T-8449: calling the removed update_task_status is the STANDARD
    unknown-tool error (-32602, spec §3 — a params error, not a bespoke
    refusal or a live dispatch)."""
    _error(
        _rpc(client, owner_token, "tools/call",
             {"name": "update_task_status",
              "arguments": {"task_id": "t-x", "status": "in_progress"}}),
        -32602, id=1)


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


def test_call_missing_path_param_returns_precise_validation(
    client, owner_token
) -> None:
    """spec §3.1: a missing path value returns a named validation refusal."""
    result = _call(client, owner_token, "activate_member", {})
    assert result["isError"] is True, result
    body = json.loads(_text(result))
    assert body["error"]["code"] == "validation_error", body
    assert body["error"]["message"] == "field required: member_id", body
    assert result["structuredContent"] == body


@pytest.mark.parametrize("value", [".", "..", "../members", "../roles"])
def test_call_path_param_rejects_route_reinterpretation(
    client, owner_token, value
) -> None:
    """spec §3.1: a path separator or dot segment cannot reach another route."""
    result = _call(client, owner_token, "get_task_manual", {"type_key": value})
    assert result["isError"] is True, result
    body = json.loads(_text(result))
    assert body["error"]["code"] == "validation_error", body
    assert body["error"]["message"] == "invalid path: type_key", body
    assert result["structuredContent"] == body


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


# ── T-6020 governance opening: the two sentinels ─────────────────────────────
#
# The owner's 2026-07-26 ruling has TWO halves, and each half needs its own
# tooth. Nineteen operational routes moved from requires=owner + mcp_exclude to
# requires=admin_agent + an MCP tool; FIVE were deliberately left where they
# were. Neither half is self-enforcing: the first would rot into "the tool is
# named but nobody can call it", the second into "somebody tidily finished the
# job", and both would pass every other test in this file.

T6020_OPENED_TOOLS = {
    "get_settings": "GET /api/settings",
    "update_settings": "PATCH /api/settings",
    "check_release": "GET /api/release/check",
    "upgrade_station": "POST /api/update/upgrade",
    "list_webhook_requests":
        "GET /api/members/{member_id}/webhooks/{endpoint_id}/requests",
    "answer_reply_card": "POST /api/reply-cards/{card_id}/answer",
    "reanswer_reply_card": "PUT /api/reply-cards/{card_id}/answer",
    "install_warden_on_server_host": "POST /api/machines/{machine_id}/bootstrap-here",
    "uninstall_warden_on_server_host": "POST /api/machines/{machine_id}/teardown-here",
    "upgrade_warden": "POST /api/machines/{member_id}/upgrade",
    "post_task_message": "POST /api/tasks/{task_id}/message",
    "get_outsource_worker_boot_context": "GET /api/outsource-workers/{id}/boot-context",
    "refocus_outsource_worker": "POST /api/outsource-workers/{id}/refocus",
    "stop_outsource_worker": "POST /api/outsource-workers/{id}/stop",
    "restart_outsource_worker": "POST /api/outsource-workers/{id}/restart",
    "set_outsource_worker_model": "POST /api/outsource-workers/{id}/model",
    "delete_task_manual": "DELETE /api/task-manuals/{type_key}",
}

# The five the owner explicitly declined to open (routes.go carries the reasons
# row by row). They stay requires=owner AND mcp_exclude.
# T6020_REVISED_TOOLS: rows a LATER owner ruling moved OFF the admin floor. The
# 2026-07-26 ruling opened 19; this table is the difference between that history
# and today, so `len(OPENED) + len(REVISED)` must stay 19. That count locks the
# CARDINALITY, not the IDENTITY of the rows: dropping a row is caught (18 != 19),
# but SWAPPING one out for another admin-floor row keeps the count at 19 and
# every per-row assertion still holds for the newcomer, so the row that left is
# no longer under the admin-floor assertion. NOT a guard of the authorization
# boundary. What actually stops a swap today is a human reading the diff. (The
# frozen manifest and the auth matrix may or may not catch it as well; that has
# NOT been verified, so it is not claimed here.) The Go
# twin is `t6020Revised` in
# server/ocserverd/routes_t6020_governance_test.go.
#
# ADDING A ROW NEEDS ITS OWN OWNER RULING, AND THE len(...) == 2 GUARD
# BELOW MUST BE EDITED IN THE SAME COMMIT: this table exempts a row from the
# admin-floor assertion, so growing it has to be a deliberate, visible act rather
# than a side effect of some other change.
T6020_REVISED_TOOLS = {
    # owner 2026-08-07, card rc-3ff94b116970 (T-1b88): the same verb, two kinds of
    # caller — the owner (or an admin agent), or the card's own author. The floor
    # dropped to `agent` and the author check moved in-handler, because "is this
    # MY card" is a per-card fact no principal class can express. The two ANSWER
    # rows were NOT revised: closing someone else's ask with an answer is still
    # governance.
    "expire_reply_card": ("POST /api/reply-cards/{card_id}/expire", "agent"),
    # owner 2026-08-20, card rc-b896e3f641e7 (T-b56e), option 0: 「開給執行者
    # （可終止自己名下的票）」 — again a per-task fact no principal class can
    # express ("is this MY task"). The floor dropped to `agent` and the decision
    # moved in-handler (callerMayTerminateTask), which also carries the one
    # subtraction the ladder cannot state: an OUTSOURCE worker is refused even on
    # its own task, because a 正職 and a contractor both rank principalAgent.
    "terminate_task": ("POST /api/tasks/{task_id}/terminate", "agent"),
}

T6020_WITHHELD_ROUTES = (
    ("POST", "/api/mint"),
    ("POST", "/api/auth/change-password"),
    ("GET", "/api/push/public-key"),
    ("POST", "/api/push/subscription"),
    ("DELETE", "/api/push/subscription"),
)


def test_t6020_opened_routes_are_admin_floor_tools(client, owner_token) -> None:
    """Half one: each of the 19 is a LISTED tool at the admin_agent floor. The
    manifest half catches a Requires that quietly went back to owner; the
    tools/list half catches a route that kept its tool name but fell out of the
    catalog (an agent's only view of a tool is tools/list, so that is invisible
    unreachability, not a cosmetic gap)."""
    listed = {t["name"] for t in _result(_rpc(client, owner_token, "tools/list"))["tools"]}
    by_op = {f"{r['method']} {r['path']}": r for r in MANIFEST}
    # 17 still at the admin floor + 2 later revised = the 19 the ruling opened.
    # Split so a revision MOVES a row (visible in the diff) instead of deleting one.
    assert len(T6020_OPENED_TOOLS) + len(T6020_REVISED_TOOLS) == 19, (
        "the 2026-07-26 ruling opened 19 routes; these tables account for "
        f"{len(T6020_OPENED_TOOLS)} + {len(T6020_REVISED_TOOLS)} — a row was "
        "dropped rather than moved"
    )
    for tool, op in T6020_OPENED_TOOLS.items():
        row = by_op.get(op)
        assert row is not None, f"{op} vanished from the route manifest"
        assert row["requires"] == "admin_agent", (
            f"{op} declares requires={row['requires']!r} — T-6020 put it at the "
            "admin_agent floor; owner would re-lock Mira out, agent/machine "
            "would over-open it"
        )
        assert row["mcp_tool"] == tool, (
            f"{op} exposes tool {row['mcp_tool']!r}, expected {tool!r}"
        )
        assert tool in listed, (
            f"{tool} is on the route table but absent from a live tools/list — "
            "an AI caller cannot discover, and the client cannot resolve, a tool "
            "that tools/list does not carry"
        )


def test_t6020_revised_routes_sit_at_their_revised_floor(client, owner_token) -> None:
    """Half one, revised half: a row a later owner ruling moved off the admin
    floor must declare its NEW floor on the live manifest and still be a listed
    tool. This is the only place in this file that proves the floor actually
    moved — the handler-level Go tests drive the handler function directly and
    never pass through the RBAC choke, so their green says nothing about it."""
    assert len(T6020_REVISED_TOOLS) == 2, (
        f"T6020_REVISED_TOOLS lists {len(T6020_REVISED_TOOLS)} rows, expected 2 — a "
        "further revision needs its OWN owner ruling, and this guard must be edited "
        "in the same commit"
    )
    listed = {t["name"] for t in _result(_rpc(client, owner_token, "tools/list"))["tools"]}
    by_op = {f"{r['method']} {r['path']}": r for r in MANIFEST}
    for tool, (op, want_floor) in T6020_REVISED_TOOLS.items():
        row = by_op.get(op)
        assert row is not None, f"{op} vanished from the route manifest"
        assert row["requires"] == want_floor, (
            f"{op} declares requires={row['requires']!r} — the revising owner ruling "
            f"put it at {want_floor!r}; a higher floor re-locks out the very caller "
            "the revision was for, a lower one hands it to warden/machine tokens"
        )
        assert row["mcp_tool"] == tool, (
            f"{op} exposes tool {row['mcp_tool']!r}, expected {tool!r}"
        )
        assert tool in listed, (
            f"{tool} is on the route table but absent from a live tools/list"
        )


def test_t6020_withheld_routes_stay_owner_only_and_unlisted(client, owner_token) -> None:
    """Half two: the five the owner declined are still requires=owner AND still
    absent from the MCP surface — name-wise unlistable and, per the manifest,
    mcp_exclude. Without this the natural next edit is "finish the job"."""
    listed = {t["name"] for t in _result(_rpc(client, owner_token, "tools/list"))["tools"]}
    by_op = {f"{r['method']} {r['path']}": r for r in MANIFEST}
    for method, path in T6020_WITHHELD_ROUTES:
        row = by_op.get(f"{method} {path}")
        assert row is not None, f"{method} {path} vanished from the route manifest"
        assert row["requires"] == "owner", (
            f"{method} {path} declares requires={row['requires']!r} — the owner "
            "explicitly declined to open this one on 2026-07-26 (minting an "
            "identity is self-escalation; the password and Web Push are the "
            "owner's personal account/browser). Re-read routes.go before changing it."
        )
        assert row["mcp_tool"] is None, (
            f"{method} {path} grew an MCP tool ({row['mcp_tool']!r}) — it must "
            "stay off the AI-callable surface"
        )
    # And no tool anywhere in the live catalog routes to one of those five.
    for tool in listed:
        row = next((r for r in MCP_ROWS if r["mcp_tool"] == tool), None)
        assert row is not None, f"live tool {tool!r} has no manifest row"
        assert (row["method"], row["path"]) not in T6020_WITHHELD_ROUTES, (
            f"live tool {tool!r} resolves to withheld route {row['method']} {row['path']}"
        )


def test_t6020_withheld_routes_refuse_the_admin_agent_live(client, admin_agent) -> None:
    """The same five, asserted against the RUNNING server rather than against a
    committed file. The manifest half above can only catch a snapshot that was
    edited; this half catches a route table that was edited without the snapshot
    — and it is the half that speaks to what an admin agent can actually reach.

    Deliberately a REST probe, not tools/call: these routes have no tool name,
    so tools/call could only ever answer "unknown tool", which would be the
    right answer for the wrong reason (it would keep passing even if the choke
    were opened)."""
    probes = (
        ("POST", "/api/mint", {"member_id": admin_agent.member_id, "ttl_days": 1}),
        ("POST", "/api/auth/change-password",
         {"current_password": "conf-wrong", "new_password": "conf-new-password"}),
        ("GET", "/api/push/public-key", None),
        ("POST", "/api/push/subscription",
         {"endpoint": "https://push.example.test/t6020",
          "keys": {"p256dh": "t6020-p256dh", "auth": "t6020-auth"}}),
        ("DELETE", "/api/push/subscription",
         {"endpoint": "https://push.example.test/t6020"}),
    )
    for method, path, body in probes:
        r = client.request(method, path, json=body, headers=_auth(admin_agent.token))
        assert r.status_code == 403, (
            f"{method} {path}: an admin agent got {r.status_code} — the owner "
            f"explicitly declined to open this route on 2026-07-26. {r.text}"
        )
        assert "principal not permitted" in r.text, (
            f"{method} {path}: refused, but not by the RBAC choke: {r.text}"
        )


def test_t6020_opened_tool_is_callable_by_the_admin_agent(client, admin_agent) -> None:
    """The 19 are not merely NAMED for the admin agent: get_settings is driven
    end-to-end over tools/call with Mira's own token, and the result is the real
    settings object rather than an unknown-tool error or an isError 403. One
    concrete call, because a route table and a catalog can both agree while the
    RBAC choke still refuses."""
    r = _rpc(client, admin_agent.token, "tools/call",
             {"name": "get_settings", "arguments": {}})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    body = r.json()
    assert "error" not in body, (
        f"get_settings must resolve for an admin agent, not error: {r.text}"
    )
    result = body["result"]
    assert result.get("isError") is not True, (
        f"the RBAC choke still refuses the admin agent: {r.text}"
    )
    assert "owner_token_ttl" in result["structuredContent"], r.text
    assert "agent_token_ttl" in result["structuredContent"], r.text


def test_t6020_opened_tool_is_refused_for_a_plain_agent(client, agent_a) -> None:
    """Sentinel for the SAME call: a plain agent gets the 403 envelope as an
    isError result. This is what proves the 19 were lowered to admin_agent and
    not to the agent floor — and it is deliberately the identical tool + token
    plumbing as the positive case above, so only the identity differs."""
    r = _rpc(client, agent_a.token, "tools/call",
             {"name": "get_settings", "arguments": {}})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    result = _result(r)
    assert result["isError"] is True, (
        f"a plain agent must NOT reach get_settings: {r.text}"
    )
    assert '"forbidden"' in result["content"][0]["text"], result
