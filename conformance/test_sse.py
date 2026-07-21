"""SSE face — the /api/events stream contract (spec/sse.md).

Third conformance batch: a LIVE black-box SSE client (sse_client.SSEConnection,
httpx stream + stdlib parser) drives the stream surface end-to-end. Timeout
discipline: every wait is bounded and every awaited event is EXPLICITLY
TRIGGERED by an HTTP write first (a delta fans within one 0.25 s poll), so the
suite never sits out the 15 s heartbeat; negative (MUST-NOT-emit) waits are
1–1.5 s each and used sparingly.

Coverage, MUST by MUST:

  * §1  gate (401 before stream), headers, ``: connected`` preamble;
  * §2  delta frame shape: id==seq==epoch, {seq,topic,op,data,ts,trigger}
        envelope, {entity,key,epoch,deleted,payload} inner, remove ⇒
        deleted+payload:null;
  * §2.3 trigger attribution: an owner write rides trigger:"owner", an agent
        write rides the verified member id (never a client-supplied field);
  * §2.1 seq strictly monotonic within a connection (== per-connection publish
        order, §4);
  * §2.2 partial payload convenience shapes (chat {id,from,to}; signals null);
  * §3  the CLOSED 9-topic vocabulary — every topic explicitly triggered and
        observed, incl. ``monitoring`` (spec froze the wire over SSE_TOPICS)
        and the M2 ``reply_card`` addition; op vocabulary patch/remove/signal;
  * §4  per-recipient routing (T-30d7): an AGENT connection receives a delta
        iff addressed (chat→from/to, member→self); an unrelated agent's stream
        stays quiet; the owner/dashboard connection is全量;
  * §5  presence = pure connection projection (offline→online→offline via a
        live agent connection); first-connect clears waking; last-disconnect
        banks live telemetry cost exactly once;
  * §5.1 dual-SSE takeover: a second listener for the same member TAKES OVER
        (new connection 200 + streaming, displaced stream terminated by the
        server, presence online throughout); the anti-flap throttle refuses
        an over-budget connect with the {"error":{code:"conflict"}} envelope
        BEFORE stream bytes; owner/dashboard connections exempt;
  * §6  context-high directed band: warn emit on the agent's own connection
        (bare data:, no id line), never on the owner connection; stale-pct
        (boot_ts) guard suppresses a predecessor's leftover pct;
  * §7  warden-command band: a real onboarded warden token drains a START
        frame produced by the event-driven reconcile dispatch; args shape and
        member_token viability asserted; never delivered to the owner fan-out.

What this file deliberately does NOT verify (black-box limits — see the
DEGRADED table in test_lifecycle.py): the 15 s heartbeat period (a positive
observation costs 15 s of wall clock per run), cross-restart seq rollback
(cannot restart an injected OC_TARGET_URL), and multi-owner scoping (the
target is single-tenant; no second owner exists to receive/miss a frame).
"""

from __future__ import annotations

import json
import time
import uuid
from typing import Any

import pytest

from conftest import AgentIdentity, hire_member, mint_member_token
from sse_client import SSEConnection


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def _fresh_agent(client, owner_token, tag: str) -> AgentIdentity:
    member_id = hire_member(client, owner_token, f"conf-sse-{tag}")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="")


def _presence(client, owner_token, member_id: str) -> str:
    r = client.get(f"/api/members/{member_id}", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    return r.json()["presence"]


def _poll_presence(
    client, owner_token, member_id: str, want: str, timeout: float = 5.0
) -> str:
    """Disconnect detection is server-side asynchronous — poll briefly."""
    deadline = time.monotonic() + timeout
    got = _presence(client, owner_token, member_id)
    while got != want and time.monotonic() < deadline:
        time.sleep(0.2)
        got = _presence(client, owner_token, member_id)
    return got


@pytest.fixture()
def owner_sse(base_url, owner_token):
    conn = SSEConnection(base_url, owner_token)
    assert conn.status_code == 200, conn.error_body
    yield conn
    conn.close()


# ── §1 endpoint basics ────────────────────────────────────────────────────────


def test_events_requires_auth_before_stream(client) -> None:
    r = client.get("/api/events")
    assert r.status_code == 401, r.text
    body = r.json()
    assert body["error"]["code"] == "unauthorized", body


def test_stream_headers_and_connected_preamble(owner_sse: SSEConnection) -> None:
    assert owner_sse.headers is not None
    assert owner_sse.headers.get("content-type", "").startswith("text/event-stream")
    assert owner_sse.headers.get("cache-control") == "no-cache"
    assert owner_sse.headers.get("x-accel-buffering") == "no"
    first = owner_sse.next_event(timeout=5.0)
    assert first["comment"] == "connected" and first["data"] is None, (
        f"stream MUST begin with ': connected', got {first}"
    )


# ── §2 delta frame shape ─────────────────────────────────────────────────────


def test_delta_frame_shape(client, owner_token, agent_a, owner_sse) -> None:
    r = client.post(
        "/api/chat",
        json={"to": agent_a.member_id, "body": "conf-sse frame shape"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    hit = owner_sse.wait_for_frame("chat")
    event, frame = hit["event"], hit["frame"]
    # envelope: exactly {seq, topic, op, data, ts, trigger}; id line == seq.
    assert set(frame) == {"seq", "topic", "op", "data", "ts", "trigger"}, frame
    assert event["id"] == str(frame["seq"]), (event, frame)
    assert isinstance(frame["seq"], int) and frame["seq"] >= 1
    assert isinstance(frame["ts"], float), frame
    assert frame["op"] == "patch"
    inner = frame["data"]
    assert set(inner) == {"entity", "key", "epoch", "deleted", "payload"}, inner
    assert inner["epoch"] == frame["seq"], "epoch MUST equal seq"
    assert inner["deleted"] is False
    assert isinstance(inner["key"], str) and inner["key"], "key is an opaque hint"
    # §2.2: the chat convenience payload is exactly {id, from, to}.
    payload = inner["payload"]
    assert set(payload) == {"id", "from", "to"}, payload
    assert payload["from"] == "owner" and payload["to"] == agent_a.member_id
    # §2.3: an owner-scope write attributes trigger:"owner".
    assert frame["trigger"] == "owner", frame


def test_frame_trigger_names_the_verified_actor(
    client, owner_token, agent_a, owner_sse
) -> None:
    """§2.3: the trigger is the verified token sub of the writer — an
    AGENT-scope write rides the member id (the client-side echo key the
    ocagent listener suppresses its own frames on), never "owner"/"server"."""
    r = client.post(
        "/api/chat",
        json={"to": "owner", "body": "conf-sse trigger attribution"},
        headers=_auth(agent_a.token),
    )
    assert r.status_code == 200, r.text
    frame = owner_sse.wait_for_frame("chat")["frame"]
    assert frame["trigger"] == agent_a.member_id, frame


def test_seq_strictly_monotonic_in_publish_order(
    client, owner_token, agent_a, owner_sse
) -> None:
    for i in range(3):
        r = client.post(
            "/api/chat",
            json={"to": agent_a.member_id, "body": f"conf-sse seq {i}"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text
    seqs = [owner_sse.wait_for_frame("chat")["frame"]["seq"] for _ in range(3)]
    assert seqs == sorted(seqs) and len(set(seqs)) == 3, (
        f"seq MUST be strictly monotonic in per-connection publish order: {seqs}"
    )


def test_member_remove_frame(client, owner_token, fresh_member, owner_sse) -> None:
    """§2+§3.2: a member dismissal fans op=remove with deleted:true and
    payload:null."""
    victim = fresh_member()
    r = client.delete(f"/api/members/{victim}", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    frame = owner_sse.wait_for(
        lambda ev: ev.get("data") is not None
        and json.loads(ev["data"]).get("topic") == "member"
        and json.loads(ev["data"])["op"] == "remove"
    )
    inner = json.loads(frame["data"])["data"]
    assert inner["deleted"] is True and inner["payload"] is None, inner


# ── §3 the closed topic/op vocabulary — all 9 topics observed ─────────────────


def test_all_nine_topics_emit(client, owner_token, agent_a, fresh_member, owner_sse) -> None:
    """Trigger every topic of the closed set (spec §3.1 — the M1 freeze was 8
    topics, monitoring included despite the 7-topic SSE_TOPICS constant;
    reply_card joined in M2) and pin its op + payload semantics."""
    tag = uuid.uuid4().hex[:8]
    member = fresh_member()
    # T-4166: a plain reply-card create now REFUSES (409) when the asker is
    # executing active work it cannot bind the ask to — a card that holds
    # nothing is what orphaned the production cards. The session-scoped agent_a
    # accumulates tasks across this suite, so the reply_card TRIGGER needs an
    # identity with no live work; the topic being pinned here is the SSE fan,
    # not the binding rule (that has its own tests in test_reply_cards.py).
    asker_id = hire_member(client, owner_token, f"conf-sse-asker-{tag}",
                           f"conf-role-sse-{tag}")
    asker_token = mint_member_token(client, owner_token, asker_id, ttl_days=1)
    triggers: list[tuple[str, Any]] = [
        ("member", lambda: client.patch(
            f"/api/members/{member}", json={"name": f"conf-topic-{tag}"},
            headers=_auth(owner_token))),
        ("chat", lambda: client.post(
            "/api/chat", json={"to": agent_a.member_id, "body": "topic probe"},
            headers=_auth(owner_token))),
        ("chat_read", lambda: client.post(
            "/api/chat/mark-read",
            json={"peer": agent_a.member_id, "last_read_ts": time.time()},
            headers=_auth(owner_token))),
        ("reply_card", lambda: client.post(
            "/api/reply-cards",
            json={"kind": "decision", "summary": f"topic probe {tag}",
                  "options": ["AI pick", "other"]},
            headers=_auth(asker_token))),
        ("global_context", lambda: client.post(
            "/api/global-context", json={"text": f"topic probe {tag}"},
            headers=_auth(owner_token))),
        ("role_def", lambda: client.post(
            "/api/roles", json={"name": f"Conf Topic Role {tag}"},
            headers=_auth(owner_token))),
        ("lessons", lambda: client.post(
            "/api/lessons/assistant/general", json={"text": f"topic probe {tag}"},
            headers=_auth(owner_token))),
        ("context", lambda: client.post(
            "/api/agent/context", json={"context_pct": 7},
            headers=_auth(agent_a.token))),
        ("monitoring", lambda: client.post(
            "/api/monitoring/telemetry",
            json={"rate_limits": {"primary_used_pct": 2}},
            headers=_auth(agent_a.token))),
    ]
    expected_op = {
        "member": "patch", "chat": "patch", "chat_read": "patch",
        "reply_card": "patch",
        "global_context": "patch", "role_def": "patch", "lessons": "patch",
        "context": "signal", "monitoring": "signal",
    }
    for topic, fire in triggers:
        r = fire()
        assert r.status_code == 200, f"{topic} trigger failed: {r.status_code} {r.text[:200]}"
        frame = owner_sse.wait_for_frame(topic)["frame"]
        assert frame["op"] == expected_op[topic], (topic, frame)
        assert frame["op"] in {"patch", "remove", "signal"}, frame
        if frame["op"] == "signal":
            # §3.2: volatile in-memory store change — payload always null.
            assert frame["data"]["payload"] is None, (topic, frame)
    # §2.2: global_context / role_def / lessons deltas carry payload null.
    # (Their frames were consumed above; re-fire one to pin it explicitly.)
    r = client.post(
        "/api/global-context", json={"text": f"payload-null probe {tag}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200
    frame = owner_sse.wait_for_frame("global_context")["frame"]
    assert frame["data"]["payload"] is None, frame


# ── §4 per-recipient routing (T-30d7) ────────────────────────────────────────
#
# The fan-out is per-recipient: an AGENT connection receives a delta iff it is
# addressed (member→self, chat→participants, reply_card→initiator, task→
# executor+creator); every other agent's stream stays quiet. This replaced the
# old全域廣播 where every online agent burned a wake on every unrelated delta
# (owner report: a zero-task agent woken by every task delta in the system).
# The owner/dashboard connection stays全量 — it is the global cockpit view, and
# every existing topic-coverage test above observes through it for that reason.
# These two tests pin the AGENT side both ways: addressed agent + owner receive,
# unrelated agent receives nothing.


def test_chat_delta_only_to_participants_and_owner(
    base_url, client, owner_token, owner_sse
) -> None:
    """A chat owner→A reaches A and the owner cockpit, never an unrelated agent
    B (the Slack-Seth waste: zero involvement ⇒ zero wake)."""
    a = _fresh_agent(client, owner_token, f"recipa-{uuid.uuid4().hex[:6]}")
    b = _fresh_agent(client, owner_token, f"recipb-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, a.token) as ca, SSEConnection(base_url, b.token) as cb:
        assert ca.status_code == 200 and cb.status_code == 200, (ca.error_body, cb.error_body)
        ca.wait_for(lambda ev: ev["comment"] == "connected")
        cb.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            "/api/chat", json={"to": a.member_id, "body": "for A only"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text
        # addressed recipient A receives it …
        hit = ca.wait_for_frame("chat")
        assert hit["frame"]["data"]["payload"]["to"] == a.member_id, hit
        # … the owner/dashboard connection is全量 …
        owner_sse.wait_for_frame("chat")
        # … and the unrelated agent B receives nothing (bounded negative wait).
        cb.assert_quiet(timeout=1.5)
    _poll_presence(client, owner_token, a.member_id, "offline")
    _poll_presence(client, owner_token, b.member_id, "offline")


def test_member_delta_only_to_subject_and_owner(
    base_url, client, owner_token, owner_sse
) -> None:
    """A member delta reaches the member's OWN connection (the wind-down /
    recycle hooks key on a member delta naming self — correctness, not just
    efficiency) and the owner cockpit, never an unrelated agent."""
    a = _fresh_agent(client, owner_token, f"selfa-{uuid.uuid4().hex[:6]}")
    b = _fresh_agent(client, owner_token, f"selfb-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, a.token) as ca, SSEConnection(base_url, b.token) as cb:
        assert ca.status_code == 200 and cb.status_code == 200, (ca.error_body, cb.error_body)
        ca.wait_for(lambda ev: ev["comment"] == "connected")
        cb.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.patch(
            f"/api/members/{a.member_id}",
            json={"name": f"renamed-{uuid.uuid4().hex[:4]}"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text
        ca.wait_for_frame("member")          # the subject gets its own delta
        owner_sse.wait_for_frame("member")   # owner全量
        cb.assert_quiet(timeout=1.5)         # unrelated agent: nothing
    _poll_presence(client, owner_token, a.member_id, "offline")
    _poll_presence(client, owner_token, b.member_id, "offline")


# ── §5 presence projection + edge hooks ───────────────────────────────────────


def test_presence_is_pure_connection_projection(
    base_url, client, owner_token
) -> None:
    agent = _fresh_agent(client, owner_token, f"presence-{uuid.uuid4().hex[:6]}")
    assert _presence(client, owner_token, agent.member_id) == "offline"
    conn = SSEConnection(base_url, agent.token)
    try:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        assert _presence(client, owner_token, agent.member_id) == "online", (
            "first connect MUST project the member online"
        )
    finally:
        conn.close()
    got = _poll_presence(client, owner_token, agent.member_id, "offline")
    assert got == "offline", f"last disconnect MUST project offline, got {got!r}"


def test_owner_connection_projects_no_member_and_may_dualize(
    base_url, owner_token
) -> None:
    """§5 + §5.1: an owner/dashboard connection projects no member online and
    is EXEMPT from the single-session rule — two may be open concurrently."""
    with SSEConnection(base_url, owner_token) as first:
        assert first.status_code == 200
        with SSEConnection(base_url, owner_token) as second:
            assert second.status_code == 200, (
                f"second owner connection refused: {second.status_code} "
                f"{second.error_body[:200]}"
            )
            second.wait_for(lambda ev: ev["comment"] == "connected")


def test_dual_sse_second_listener_takes_over(
    base_url, client, owner_token
) -> None:
    """§5.1 (T-b315): a second live listener for the same member TAKES OVER —
    the new connection is admitted (200 + streaming), the displaced stream is
    terminated by the server, presence stays online across the handover (no
    flicker), and deltas land on the new connection."""
    agent = _fresh_agent(client, owner_token, f"dual-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as first:
        assert first.status_code == 200
        first.wait_for(lambda ev: ev["comment"] == "connected")
        with SSEConnection(base_url, agent.token) as second:
            assert second.status_code == 200, (
                f"a takeover must admit the new connection, got "
                f"{second.status_code} {second.error_body[:200]}"
            )
            second.wait_for(lambda ev: ev["comment"] == "connected")
            # The displaced FIRST stream is terminated promptly server-side.
            assert first.wait_closed(10.0), (
                "the displaced listener's stream must be terminated by the server"
            )
            # Presence never flickered: the member is still online under the
            # NEW connection the instant the old stream is gone.
            assert _presence(client, owner_token, agent.member_id) == "online", (
                "the online projection must not flicker across the handover"
            )
            # Deltas now land on the new connection.
            r = client.post(
                "/api/chat",
                json={"to": agent.member_id, "body": "post-takeover"},
                headers=_auth(owner_token),
            )
            assert r.status_code == 200
            second.wait_for_frame("chat")
    _poll_presence(client, owner_token, agent.member_id, "offline")


def test_takeover_throttle_over_budget_409_conflict_envelope(
    base_url, client, owner_token
) -> None:
    """§5.1 anti-flap throttle: past the takeover burst (3 per 60 s window)
    an excess connect is refused with the conflict envelope as a proper HTTP
    status BEFORE stream bytes, and the incumbent connection keeps streaming
    (never kicked by a refused attempt)."""
    agent = _fresh_agent(client, owner_token, f"thr-{uuid.uuid4().hex[:6]}")
    conns = [SSEConnection(base_url, agent.token)]  # first connect: no takeover
    try:
        assert conns[0].status_code == 200, conns[0].error_body
        conns[0].wait_for(lambda ev: ev["comment"] == "connected")
        for _ in range(3):  # takeovers 1..3 — inside the burst, all admitted
            c = SSEConnection(base_url, agent.token)
            conns.append(c)
            assert c.status_code == 200, (
                f"in-burst takeover refused: {c.status_code} {c.error_body[:200]}"
            )
            c.wait_for(lambda ev: ev["comment"] == "connected")
        # Takeover 4 within the window: throttled → pre-stream 409.
        refused = SSEConnection(base_url, agent.token)
        try:
            assert refused.status_code == 409, (
                f"an over-budget takeover must be refused 409, got "
                f"{refused.status_code}"
            )
            ctype = (refused.headers or {}).get("content-type", "")
            assert not ctype.startswith("text/event-stream"), (
                "the 409 must be a plain HTTP response, not a stream"
            )
            body = json.loads(refused.error_body)
            assert set(body) == {"error"} and set(body["error"]) == {
                "code", "message"}, body
            assert body["error"]["code"] == "conflict", body
        finally:
            refused.close()
        # The INCUMBENT (latest admitted) connection survives the refusal.
        assert _presence(client, owner_token, agent.member_id) == "online"
        r = client.post(
            "/api/chat",
            json={"to": agent.member_id, "body": "still alive"},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200
        conns[-1].wait_for_frame("chat")
    finally:
        for c in conns:
            c.close()
    _poll_presence(client, owner_token, agent.member_id, "offline")


def test_first_connect_clears_waking(base_url, client, owner_token) -> None:
    """§5.2 first-connect edge: the wake completes the instant the agent holds
    /api/events — waking_since MUST be cleared (observable: presence falls to
    OFFLINE after disconnect, not back to 'waking', although desired_state is
    still online and the 90 s waking TTL has not lapsed)."""
    agent = _fresh_agent(client, owner_token, f"waking-{uuid.uuid4().hex[:6]}")
    r = client.post(
        f"/api/members/{agent.member_id}/activate", json={},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    r = client.post("/api/self/waking", json={}, headers=_auth(agent.token))
    assert r.status_code == 200, r.text
    assert _presence(client, owner_token, agent.member_id) == "waking"
    conn = SSEConnection(base_url, agent.token)
    try:
        assert conn.status_code == 200
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        assert _presence(client, owner_token, agent.member_id) == "online"
    finally:
        conn.close()
    got = _poll_presence(client, owner_token, agent.member_id, "offline")
    assert got == "offline", (
        f"waking_since survived the connect edge: presence {got!r} after "
        "disconnect (expected offline — the marker must be spent exactly once)"
    )
    client.post(
        f"/api/members/{agent.member_id}/deactivate", headers=_auth(owner_token)
    )


# ── zombie stop gate (pre-stream 409 while a stop is in effect) ──────────────
#
# Defence line B of the zombie-agent work: a listener that survived its kill
# must never RE-project a stopped member online by reconnecting. While a stop
# is in effect (desired_state=offline ∧ a stop anchor stamped — deactivate /
# force-stop always stamp stopping_since) or the member is dismissed, a fresh
# /api/events connection is refused PRE-stream with the conflict envelope
# (the dual-SSE guard's envelope family). The gate is deliberately narrower
# than desired_state=offline alone: a freshly hired member (desired offline,
# NO anchors) still connects — pinned below — and activate lifts the gate in
# the same write it flips desired_state in.


def test_zombie_reconnect_refused_while_stop_in_effect(
    base_url, client, owner_token
) -> None:
    agent = _fresh_agent(client, owner_token, f"zombie-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/members/{agent.member_id}/deactivate", headers=_auth(owner_token)
        )
        assert r.status_code == 200, r.text
        # The LIVE connection survives the deactivate — the wind-down nudge
        # (the member delta) must still reach the agent's own stream.
        conn.wait_for_frame("member")
        assert _presence(client, owner_token, agent.member_id) == "stopping"
    # Wait for the disconnect to land server-side (rules out a dual-SSE 409
    # explaining the refusal below: presence "stopped" ⇒ no live listener).
    got = _poll_presence(client, owner_token, agent.member_id, "stopped")
    assert got == "stopped", f"expected the graceful-stop projection, got {got!r}"

    # The zombie reconnect: refused pre-stream, conflict envelope, no stream.
    zombie = SSEConnection(base_url, agent.token)
    assert zombie.status_code == 409, (
        f"a stop-in-effect reconnect must be refused 409, got {zombie.status_code}"
    )
    ctype = (zombie.headers or {}).get("content-type", "")
    assert not ctype.startswith("text/event-stream"), (
        "the refusal must be a plain HTTP response, not a stream"
    )
    body = json.loads(zombie.error_body)
    assert set(body) == {"error"} and set(body["error"]) == {"code", "message"}, body
    assert body["error"]["code"] == "conflict", body
    # …and it never projected online (the whole point of the gate).
    assert _presence(client, owner_token, agent.member_id) == "stopped"

    # stop→start: activate clears the anchors + flips desired_state in ONE
    # write — the gate lifts atomically and the next connect streams again.
    r = client.post(
        f"/api/members/{agent.member_id}/activate", json={},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    with SSEConnection(base_url, agent.token) as revived:
        assert revived.status_code == 200, revived.error_body
        revived.wait_for(lambda ev: ev["comment"] == "connected")
        assert _presence(client, owner_token, agent.member_id) == "online"
    _poll_presence(client, owner_token, agent.member_id, "offline")
    client.post(
        f"/api/members/{agent.member_id}/deactivate", headers=_auth(owner_token)
    )


def test_fresh_hire_desired_offline_still_connects(
    base_url, client, owner_token
) -> None:
    """The gate's lower boundary: a freshly HIRED member is desired-offline
    with NO stop anchors — that is 'not started yet', never 'stop in effect',
    and MUST still be admitted (scratch agents, pre-activate flows)."""
    agent = _fresh_agent(client, owner_token, f"hireok-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
    _poll_presence(client, owner_token, agent.member_id, "offline")


def test_dismissed_member_reconnect_refused(base_url, client, owner_token) -> None:
    """A dismissed (roster removed) member must never re-project online."""
    agent = _fresh_agent(client, owner_token, f"dismiss-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
    r = client.delete(
        f"/api/members/{agent.member_id}", headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    deadline = time.monotonic() + 5.0
    zombie = SSEConnection(base_url, agent.token)
    while zombie.status_code != 409 and time.monotonic() < deadline:
        zombie.close()  # defensive retry: the roster gate answers 409 pre-Connect
        time.sleep(0.2)
        zombie = SSEConnection(base_url, agent.token)
    assert zombie.status_code == 409, (
        f"a removed member's reconnect must be refused 409, got {zombie.status_code}"
    )
    assert json.loads(zombie.error_body)["error"]["code"] == "conflict"
    zombie.close()


def test_warden_exempt_from_desired_offline_gate(
    base_url, client, owner_token
) -> None:
    """Wardens sit at desired_state=offline BY DEFAULT (onboarding/seed) and
    their removal lifecycle is the one-shot uninstall intent — the
    desired-offline arm of the gate MUST NOT refuse a warden, even with a
    stop anchor stamped."""
    r = client.post(
        "/api/machines",
        json={"display_name": f"conf-sse-gate-warden-{uuid.uuid4().hex[:6]}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    onboard = r.json()
    machine_id, warden_token = onboard["machine_id"], onboard["token"]
    # Stamp a stop anchor on the warden member (deactivate writes desired
    # offline + stopping_since) — the gate must STILL admit the warden.
    r = client.post(
        f"/api/members/{machine_id}/deactivate", headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    with SSEConnection(base_url, warden_token) as conn:
        assert conn.status_code == 200, (
            f"warden refused by the stop gate: {conn.status_code} "
            f"{conn.error_body[:200]}"
        )
        conn.wait_for(lambda ev: ev["comment"] == "connected")
    _poll_presence(client, owner_token, machine_id, "stopped")


def _session_row(client, owner_token, member_id: str) -> dict[str, Any]:
    r = client.get("/api/monitoring", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    rows = [s for s in r.json()["sessions"] if s["id"] == member_id]
    assert rows, f"member {member_id} missing from the monitoring fold"
    return rows[0]


def test_last_disconnect_banks_cost_exactly_once(
    base_url, client, owner_token
) -> None:
    """§5.2 last-disconnect edge: live telemetry cost folds into the durable
    banked_cost then is POPPED — a second disconnect edge cannot double-bank."""
    agent = _fresh_agent(client, owner_token, f"bank-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            "/api/monitoring/telemetry", json={"cost": 1.25},
            headers=_auth(agent.token),
        )
        assert r.status_code == 200, r.text
        row = _session_row(client, owner_token, agent.member_id)
        assert row["cost"] == 1.25 and row["banked_cost"] is None, row
    _poll_presence(client, owner_token, agent.member_id, "offline")
    deadline = time.monotonic() + 5.0
    row = _session_row(client, owner_token, agent.member_id)
    while row["banked_cost"] != 1.25 and time.monotonic() < deadline:
        time.sleep(0.2)
        row = _session_row(client, owner_token, agent.member_id)
    assert row["banked_cost"] == 1.25, f"cost was not banked on disconnect: {row}"
    assert row["cost"] is None, f"live cost MUST be popped after banking: {row}"
    # Second edge with no fresh report: MUST NOT double-bank.
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200
        conn.wait_for(lambda ev: ev["comment"] == "connected")
    _poll_presence(client, owner_token, agent.member_id, "offline")
    time.sleep(0.5)  # give a (wrong) second fold the chance to happen
    row = _session_row(client, owner_token, agent.member_id)
    assert row["banked_cost"] == 1.25, f"double-banked on a re-fired edge: {row}"


# ── §6 context-high directed band ─────────────────────────────────────────────


def _is_context_high(ev: dict[str, Any]) -> bool:
    if ev.get("data") is None:
        return False
    try:
        return json.loads(ev["data"]).get("topic") == "context-high"
    except ValueError:
        return False


def test_context_high_warn_band_directed_to_agent_only(
    base_url, client, owner_token, owner_sse
) -> None:
    """§6: an ACTIONABLE pct in the warn band (fresh report AFTER boot_ts) emits
    ONE directed frame on the agent's own connection — bare data:, no id line,
    level 'warn' — and never on the owner connection."""
    agent = _fresh_agent(client, owner_token, f"ctxhigh-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        # 45: >= warn(40), < handover(50) — the warn band, the ONLY wire band.
        r = client.post(
            "/api/agent/context", json={"context_pct": 45},
            headers=_auth(agent.token),
        )
        assert r.status_code == 200, r.text
        ev = conn.wait_for(_is_context_high)
        assert ev["id"] is None, (
            f"directed band frames carry NO id: line (not replayable): {ev}"
        )
        frame = json.loads(ev["data"])
        assert set(frame) == {"topic", "data"}, frame
        inner = frame["data"]
        assert set(inner) == {"topic", "to", "level", "pct", "reason"}, inner
        assert inner["topic"] == "context-high"
        assert inner["to"] == agent.member_id
        assert inner["level"] == "warn", (
            "only level:'warn' is ever emitted on the wire (handover is the "
            f"producer auto-recycle, not an SSE emit): {inner}"
        )
        assert inner["pct"] == 45 and isinstance(inner["reason"], str), inner
        # The owner (dashboard) connection sees the `context` entity signal but
        # MUST NOT receive the directed reminder.
        owner_sse.wait_for_frame("context")
        leaked = None
        try:
            leaked = owner_sse.wait_for(_is_context_high, timeout=1.0)
        except TimeoutError:
            pass
        assert leaked is None, f"owner connection received context-high: {leaked}"
    _poll_presence(client, owner_token, agent.member_id, "offline")


def test_context_high_stale_pct_guard(base_url, client, owner_token) -> None:
    """§6 stale-pct guard: a pct reported BEFORE the connection's boot_ts (a
    predecessor session's leftover) MUST NOT trigger the band."""
    agent = _fresh_agent(client, owner_token, f"stale-{uuid.uuid4().hex[:6]}")
    r = client.post(
        "/api/agent/context", json={"context_pct": 45}, headers=_auth(agent.token)
    )
    assert r.status_code == 200, r.text
    time.sleep(0.05)  # pct ts strictly < boot_ts
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        ev = None
        try:
            ev = conn.wait_for(_is_context_high, timeout=1.5)
        except TimeoutError:
            pass
        assert ev is None, f"stale pct triggered the band: {ev}"
    _poll_presence(client, owner_token, agent.member_id, "offline")


# ── §7 warden-command band ────────────────────────────────────────────────────


def test_warden_command_band_start_frame(
    base_url, client, owner_token, owner_sse
) -> None:
    """§7 + lifecycle §4.3/§4.6: onboard a machine (mints the warden member +
    its agent-scope exec-token), hold its SSE downstream, then activate an
    agent bound to that machine — the event-driven reconcile dispatch MUST
    enqueue a START command that drains onto the WARDEN's connection only:
    bare data: frame, rpc vocabulary, the full args shape, and a member_token
    that actually authenticates."""
    r = client.post(
        "/api/machines",
        json={"display_name": f"conf-sse-warden-{uuid.uuid4().hex[:6]}"},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    onboard = r.json()
    machine_id, warden_token = onboard["machine_id"], onboard["token"]

    member_id = hire_member(client, owner_token, f"conf-sse-spawnee-{uuid.uuid4().hex[:6]}")
    warden = SSEConnection(base_url, warden_token)
    try:
        assert warden.status_code == 200, warden.error_body
        warden.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/members/{member_id}/activate",
            json={"machine_id": machine_id},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text

        def _is_cmd(ev: dict[str, Any]) -> bool:
            if ev.get("data") is None:
                return False
            try:
                return json.loads(ev["data"]).get("topic") == "warden-command"
            except ValueError:
                return False

        ev = warden.wait_for(_is_cmd, timeout=8.0)
        assert ev["id"] is None, f"command frames carry NO id: line: {ev}"
        frame = json.loads(ev["data"])
        assert set(frame) == {"topic", "data"}, frame
        cmd = frame["data"]
        assert cmd["rpc"] == "start", cmd
        args = cmd["args"]
        assert set(args) == {
            "member_id", "persona_context", "member_token", "role",
            "task_type", "model", "effort", "session_name",
        }, args
        assert args["member_id"] == member_id, args
        assert args["persona_context"].strip(), "START must carry the folded persona"
        assert args["role"], args
        # Confidentiality payoff: the riding member_token is a REAL credential.
        probe = client.get("/api/members", headers=_auth(args["member_token"]))
        assert probe.status_code == 200, (
            "START member_token failed to authenticate"
        )
        # The owner-scope entity fan-out MUST NEVER carry a command frame
        # (member_token would leak to the dashboard).
        leaked = None
        try:
            leaked = owner_sse.wait_for(_is_cmd, timeout=1.0)
        except TimeoutError:
            pass
        assert leaked is None, f"warden-command leaked to the owner fan-out: {leaked}"
    finally:
        client.post(
            f"/api/members/{member_id}/deactivate", headers=_auth(owner_token)
        )
        warden.close()
    _poll_presence(client, owner_token, machine_id, "offline")


# ── T-db62 diagnostic: gate-card arm must fan reply_card to owner cockpit ─────
#
# The 請示 nav badge (useReplyCardCount) refetches on every reply_card delta;
# owner reported the badge staying blank for an open_gate gate card until a
# manual reload. conformance historically triggered reply_card ONLY via the
# standalone POST /api/reply-cards path (test_all_nine_topics_emit). This pins
# the OTHER open path — open_gate arming — actually fans a reply_card delta to
# the owner connection, byte-for-byte the same live-update signal the badge
# rides. If the badge bug is a missing frame, this goes red.
def test_gate_arm_emits_reply_card_frame_to_owner(
    client, owner_token, agent_a, owner_sse
) -> None:
    h = _auth(agent_a.token)
    r = client.post(
        "/api/tasks",
        json={"title": "conf gate sse probe", "executor_member_id": agent_a.member_id},
        headers=h,
    )
    assert r.status_code == 200, r.text
    task_id = r.json()["task"]["id"]
    r = client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "approve", "dod": "go", "is_gate": True}]},
        headers=h,
    )
    assert r.status_code == 200, r.text
    step_id = r.json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): report the step in_progress so the task
    # derives in_progress — a gate can only arm on an in_progress task.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h,
    ).status_code == 200
    r = client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/gate",
        json={"kind": "decision", "summary": "gate probe", "options": ["go", "hold"]},
        headers=h,
    )
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]
    frame = owner_sse.wait_for_frame("reply_card")["frame"]
    assert frame["op"] == "patch", frame
    assert frame["data"]["payload"]["id"] == card_id, frame
    assert frame["data"]["payload"]["status"] == "waiting", frame
