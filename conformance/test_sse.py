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
  * §3  the CLOSED topic vocabulary — EVERY topic of the closed set is
        explicitly triggered and observed, and the trigger table is confronted
        with the product's own wire contract (spec/sse.md §3.1) at run time, so
        a topic added there without a trigger here reddens instead of silently
        losing its write-face coverage; op vocabulary patch/remove/signal;
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
  * §6.1 token-expiry directed band: covered by the ocserverd wire test because
        the public mint surface only accepts whole-day TTLs;
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
import pathlib
import re
import time
import uuid
from typing import Any

import pytest

from conftest import AgentIdentity, hire_member, mint_member_token
from sse_client import SSEConnection


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


HERE = pathlib.Path(__file__).resolve().parent

# The §3.1 topic table row: ``| `<topic>` | <trigger> | <op> |``.
#
# ⚠️ DUPLICATED PARSER (knowingly, this round): server/ocserverd's
# TestSSETopicsMatchSpec parses the SAME table to bind hub.go's `sseTopics` to
# it — the other edge of this guard (that test is what makes spec/sse.md a
# TRUSTWORTHY authority here; without it a topic added to hub.go alone would
# leave both guards green). Two parsers of one markdown table is a smell; the
# proper fix is a MACHINE-READABLE spec asset (a spec/sse-topics.json next to
# spec/openapi.json, consumed by both sides and by the frontend's
# SSE_RESYNC_TOPICS), which adds a frozen wire asset ⇒ owner's call under the
# wire freeze, not a tidy-up to do in passing.
_SPEC_TOPIC_ROW = re.compile(r"^\|\s*`([a-z_]+)`\s*\|", re.M)

# The §3.1 section delimiters. Located with str.find + an EXPLICIT not-found
# check, never with str.split: ``"x".split("nope", 1)`` returns ``["x"]``, so
# ``spec.split("### 3.1", 1)[-1].split("### 3.2", 1)[0]`` silently degrades to a
# DIFFERENT SLICE the moment either heading is renamed or moved — and the
# degraded slice still parses out a set that looks perfectly healthy, so the
# confrontation below guards nothing and nothing goes red.
#
# 🔴 WHY it stays "healthy" — the real mechanism, because the explanation that
# used to sit here was wrong and wrong explanations get quoted. It said,
# verbatim: the parse "degrades to THE WHOLE DOCUMENT" and "this document
# happens to contain exactly the same 12 topic rows elsewhere (§4.1's audience
# table)". **Both halves are false** (re-measured independently, pure-function,
# no server):
#   * losing "### 3.1" yields start-of-file → §3.2 — a ~9.9k-char SLICE, not the
#     34207-char document; losing "### 3.2" yields §3.1 → EOF. Neither is
#     "the whole document".
#   * §4.1 is irrelevant: it begins at char 11089, i.e. PAST the end of that
#     ~9.9k-char slice, so the degraded parse never reads it. It also lists only
#     7 topics, which cannot produce the full set. Deleting §4.1 outright leaves
#     the degraded answer BYTE-IDENTICAL.
# The actual mechanism is more general, and worse: **the end delimiter sits
# AFTER the target table, so when the start delimiter goes missing the degraded
# slice STILL FULLY CONTAINS the very table it was supposed to bound.** The
# answer is right because the table is still inside the slice — not because a
# second copy of it exists somewhere else. Generalise it: ANY split-style parser
# that bounds its target with a heading located AFTER that target will silently
# return the correct answer when its START delimiter disappears.
# (This matters operationally: anyone who believed the §4.1 story would try to
# reduce the risk by cleaning up §4.1. Measured — that changes nothing.)
#
# Fail-loud is the whole point: a parser for a machine-read contract must never
# have a "quietly parsed something else" branch.
_SPEC_TOPIC_SECTION_START = "### 3.1"
_SPEC_TOPIC_SECTION_END = "### 3.2"


class SpecTopicParseError(AssertionError):
    """spec/sse.md §3.1 could not be located/parsed — never a silent fallback."""


def _parse_closed_topics(spec: str, source: str = "<spec/sse.md>") -> set[str]:
    """Extract the §3.1 topic set from the spec text, or RAISE.

    Split out from ``_closed_topic_set`` so the fail-loud behaviour itself is
    testable on constructed inputs (``test_spec_topic_parser_fails_loud``)
    without a server: a guard whose degradation mode is untested is a guard
    that has only ever been eyeballed.
    """
    start = spec.find(_SPEC_TOPIC_SECTION_START)
    if start == -1:
        raise SpecTopicParseError(
            f"{source}: heading {_SPEC_TOPIC_SECTION_START!r} not found — the "
            "closed topic set is READ from that section at run time, so a "
            "renamed/moved heading must fail here, not fall back to slicing "
            "from the start of the file (that slice still CONTAINS the §3.1 "
            "table, so it yields the right-looking set for the wrong reason "
            "and makes every topic guard vacuous)."
        )
    end = spec.find(_SPEC_TOPIC_SECTION_END, start + len(_SPEC_TOPIC_SECTION_START))
    if end == -1:
        raise SpecTopicParseError(
            f"{source}: heading {_SPEC_TOPIC_SECTION_END!r} not found after "
            f"{_SPEC_TOPIC_SECTION_START!r} — the section has no end delimiter, "
            "so the topic table can no longer be bounded. Fix the spec headings "
            "or this parser; do not let it swallow the rest of the document."
        )
    topics = set(_SPEC_TOPIC_ROW.findall(spec[start:end]))
    if not topics:
        raise SpecTopicParseError(
            f"{source}: §3.1 was located but ZERO topic rows parsed out of it — "
            "the table shape changed (or moved). An empty closed set would make "
            "the confrontation in test_every_closed_topic_emits pass trivially "
            "(nothing missing, nothing extra), so it is an error, not a result."
        )
    return topics


def _closed_topic_set() -> set[str]:
    """The closed topic vocabulary, READ FROM THE PRODUCT'S OWN WIRE CONTRACT at
    run time — ``spec/sse.md`` §3.1, the very table ``hub.go``'s ``sseTopics``
    cites as its source and that a topic addition MUST go through (spec-first,
    root CLAUDE.md §13).

    Deliberately NOT a list restated in this file: a hand-copied set would make
    the confrontation below vacuous (it would only ever confront one hand-copy
    with another, and the next person to add a topic would forget both). Same
    posture as ``test_rest_happy``/``test_auth_matrix``, which pin the live
    surface against the frozen ``spec/openapi.json`` + ``routes_manifest.json``
    rather than against a list typed here.

    ⚠️ SCOPE of this guard (and of ocserverd's TestSSETopicsMatchSpec, the other
    edge): it covers the ENTITY-DELTA topics only — the ones that ride
    ``hub.Publish``. The three DIRECTED bands (``context-high`` §6,
    ``token-expiry`` §6.1, ``warden-command`` §7) go out through ``PushDirected``,
    bypass ``Publish`` entirely, and are a separate envelope family by design
    (§3.1's own note: "a separate envelope family, not entity-delta topics").
    Their ABSENCE from this set is deliberate, not an oversight — do NOT "fix"
    it by adding them here or to ``sseTopics``; they are pinned by their own
    tests (``test_context_high_*``, ``test_warden_command_band_start_frame``,
    and ocserverd's token-expiry wire test).

    🔴 ``task-close`` §8 used to be the fourth. T-91 retired it as a wire band —
    the close-out nudge is a DURABLE CHAT ROW now, because an at-most-once push
    reached nobody who was offline when its task closed. It is absent here for a
    different reason from the other three: not "directed rather than entity",
    but "no longer sent on the wire at all".
    """
    path = HERE.parent / "spec" / "sse.md"
    return _parse_closed_topics(path.read_text(encoding="utf-8"), source=str(path))


def test_spec_topic_parser_fails_loud() -> None:
    """The §3.1 reader must ERROR — never return a healthy-looking set — when
    the section it reads is not where it expects it.

    This is the anti-vacuity guard for the guard: the previous ``str.split``
    form produced the RIGHT ANSWER FOR THE WRONG REASON on a missing heading.

    🔴 The reason stated here before was wrong, and is quoted so it is not
    re-used: it claimed the split "returned the whole document on a missing
    heading, and because §4.1's audience table lists the same 12 topics, the
    degraded parse produced the right answer". Re-measured: the degraded slice
    is ~9.9k chars (the file is 34207), §4.1 starts at char 11089 and is never
    reached, §4.1 lists only 7 topics, and deleting §4.1 leaves the degraded
    answer identical. See the note on the delimiters above for the real
    mechanism — the end delimiter sits AFTER the table, so a slice that loses
    its start delimiter still contains the whole table.

    Every case below is asserted to be a genuine mutation (the mutated text no
    longer contains the delimiter it is supposed to have lost — a `### 3.1` →
    `### 3.1bis` rename would still CONTAIN `### 3.1` and make these cases pass
    without testing anything).
    """
    real = (HERE.parent / "spec" / "sse.md").read_text(encoding="utf-8")
    # Deliberately a floor, not the exact count: the closed set grows (it was 12
    # when this was written, 13 today), and a hard-coded size here would be one
    # more stale number to chase — the EQUALITY that pins the set lives in
    # test_every_closed_topic_emits, this is only a positive control.
    assert len(_parse_closed_topics(real)) >= 12, "positive control: the real spec parses"

    no_start = real.replace(_SPEC_TOPIC_SECTION_START, "### 3.9 (heading moved)")
    assert _SPEC_TOPIC_SECTION_START not in no_start, "mutation must really remove it"
    with pytest.raises(SpecTopicParseError):
        _parse_closed_topics(no_start)

    no_end = real.replace(_SPEC_TOPIC_SECTION_END, "### 3.8 (heading moved)")
    assert _SPEC_TOPIC_SECTION_END not in no_end, "mutation must really remove it"
    with pytest.raises(SpecTopicParseError):
        _parse_closed_topics(no_end)

    empty_section = (
        f"{_SPEC_TOPIC_SECTION_START} Topics\n\nthe table moved elsewhere\n\n"
        f"{_SPEC_TOPIC_SECTION_END} Ops\n\n| `member` | x |\n"
    )
    with pytest.raises(SpecTopicParseError):
        _parse_closed_topics(empty_section)


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


# ── §3 the closed topic/op vocabulary — EVERY topic of the set observed ───────


def test_every_closed_topic_emits(client, owner_token, agent_a, fresh_member, owner_sse) -> None:
    """Trigger every topic of the closed set (spec §3.1 — the M1 freeze was 8
    topics, monitoring included despite the 7-topic SSE_TOPICS constant;
    reply_card joined in M2, the task batch added three more) and pin its op +
    payload semantics.

    The trigger table below is CONFRONTED with the closed set read from
    ``spec/sse.md`` §3.1 at run time (``_closed_topic_set``): covering fewer
    topics than the wire contract declares means the missing topics' publish
    seam could be deleted wholesale with this suite still green, which is
    exactly what happened while this table was a hand-written list of 9.
    """
    tag = uuid.uuid4().hex[:8]
    member = fresh_member()
    # A kind='outsource' roster row IS an outsource worker (the P7d fold — the
    # worker table lives in `member`), so the ordinary worker write face below
    # has a subject without needing the scheduler's spawn seam.
    worker = hire_member(client, owner_token, f"conf-topic-worker-{tag}", kind="outsource")
    # The member row's PATCH body is a NAMED VALUE, not an inline literal, so
    # the assertion below can bind to the very field this write sets instead of
    # to a value re-typed next to it. See the identity check in the loop: if
    # this body ever stops writing `name`, that check FAILS LOUDLY instead of
    # silently degrading into something a stale frame satisfies.
    member_patch_body: dict[str, Any] = {"name": f"conf-topic-{tag}"}
    triggers: list[tuple[str, Any]] = [
        ("member", lambda: client.patch(
            f"/api/members/{member}", json=member_patch_body,
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
                  "options": [{"text": "AI pick"}, {"text": "other"}], "linked_task": None},
            headers=_auth(agent_a.token))),
        # The three M3 task-batch topics, each through an ORDINARY write face
        # (task creation / a worker field edit / manual creation) — not a
        # side-door: these are the same seams the cockpit and the MCP tools use.
        ("task", lambda: client.post(
            "/api/tasks",
            json={"title": f"topic probe {tag}",
                  "executor_member_id": agent_a.member_id},
            headers=_auth(agent_a.token))),
        ("outsource_worker", lambda: client.post(
            f"/api/outsource-workers/{worker}/model",
            json={"effort": "high"},
            headers=_auth(owner_token))),
        ("task_manual", lambda: client.post(
            "/api/task-manuals", json={"type_key": f"conf-topic-{tag}"},
            headers=_auth(owner_token))),
        ("global_context", lambda: client.post(
            "/api/global-context", json={"text": f"topic probe {tag}"},
            headers=_auth(owner_token))),
        ("role_def", lambda: client.post(
            "/api/roles", json={"name": f"Conf Topic Role {tag}"},
            headers=_auth(owner_token))),
        ("lessons", lambda: client.post(
            "/api/lessons/assistant", json={"text": f"topic probe {tag}"},
            headers=_auth(owner_token))),
        # insight — the ORDINARY write face (replace_insight), not the restore
        # path. Pinned here because a doc write that reaches the DB but never
        # publishes is invisible from HTTP alone (200 + row changed + cockpit
        # stuck on the old value), exactly the shape lessons is pinned against.
        ("insight", lambda: client.post(
            "/api/insight/assistant", json={"text": f"topic probe {tag}"},
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
        "task": "patch", "outsource_worker": "patch", "task_manual": "patch",
        "global_context": "patch", "role_def": "patch", "lessons": "patch",
        "insight": "patch",
        "context": "signal", "monitoring": "signal",
    }
    # ── the self-confrontation: this table IS the closed set, not a subset ────
    closed = _closed_topic_set()
    covered = {topic for topic, _ in triggers}
    missing, extra = sorted(closed - covered), sorted(covered - closed)
    assert not missing and not extra, (
        "the trigger table MUST equal the closed topic set declared by the "
        "product's wire contract (spec/sse.md §3.1).\n"
        f"  never triggered here (their publish seam could be deleted and this "
        f"suite would stay green): {missing}\n"
        f"  triggered here but NOT in the closed set (a phantom topic, or the "
        f"contract lost one): {extra}"
    )
    assert sorted(expected_op) == sorted(covered), (
        "every triggered topic needs its expected frame kind pinned; "
        f"missing: {sorted(covered - set(expected_op))}, "
        f"stale: {sorted(set(expected_op) - covered)}"
    )
    # ── BARRIER: the loop below may only ever see frames IT triggered ─────────
    #
    # ``wait_for_frame`` drains from the FRONT of the connection's queue, so it
    # will happily hand back a delta that was already sitting there before the
    # trigger ran. This test's setup writes to the roster (``fresh_member()``,
    # ``hire_member(...)``) while ``owner_sse`` is ALREADY OPEN, so without this
    # barrier the first row (``member``) consumed one of those setup frames and
    # its assertion was VACUOUSLY TRUE: deleting putMember's publish seam
    # outright (write straight to the store, HTTP still 200, wire silent)
    # left this row — and the whole suite — green. Reviewed and reproduced;
    # that is the exact failure this test exists to catch.
    #
    # This must NOT be "downgraded" to moving those two setup writes above the
    # connection. That fixes today's two writes and nothing else: the next
    # person to add a third setup write re-poisons every row here SILENTLY,
    # because a stale frame produces a PASS, and nothing in the suite would
    # object. The barrier swallows whatever backlog exists (any count) and
    # returns only once the stream has gone quiet, so the property is
    # count-independent instead of resting on "setup only writes twice".
    #
    # ⚠️ The barrier is the SECONDARY guard. It is blind, by construction, to
    # any write that happens AFTER it returns (see sse_client.drain_backlog's
    # note — that is true of every absorbing barrier, so a "setup write inserted
    # below the barrier" experiment can only ever produce an uninformative
    # green). The PRIMARY guard is the value binding on the `member` row below.
    #
    # 🔴 WHAT IS *NOT* PROVEN HERE — read this before trusting the other rows.
    # Only the `member` row binds the frame to the write that triggered it. The
    # other ELEVEN rows still assert no more than "a frame with this topic
    # arrived", so their non-vacuity is BORROWED from this barrier having
    # emptied the backlog — it is not proven. Two measured facts make that a
    # live risk rather than a theoretical one:
    #   * a single trigger in this table can fan MORE THAN ONE topic. Measured
    #     (review round 2, full-frame trace): creating a reply_card also fans
    #     `chat`; creating a role also fans `member`.
    #   * today no row is poisoned by that cross-talk ONLY because the
    #     cross-talking topics happen to sit EARLIER in this table, so their
    #     frames are already consumed by the time the later row waits.
    # That is an ORDERING ACCIDENT, not a property: REORDERING THIS TABLE CAN
    # SILENTLY MAKE A ROW VACUOUS AGAIN, and nothing here would object. If you
    # reorder, or add a trigger with cross-talk, bind that row to its own write
    # the way the `member` row does — do not assume the barrier covers you.
    owner_sse.drain_backlog(quiet_for=1.0, timeout=5.0, label="before the closed-topic loop")

    for topic, fire in triggers:
        r = fire()
        assert r.status_code == 200, f"{topic} trigger failed: {r.status_code} {r.text[:200]}"
        # NAME THE TOPIC on the miss: the bare TimeoutError from wait_for_frame
        # says only "no matching SSE event", so a red CI run left the reader to
        # infer WHICH topic from this table's order. That inference is exactly
        # the hand-reasoning this test exists to abolish — the whole point of
        # the confrontation above is that the failure names names.
        try:
            frame = owner_sse.wait_for_frame(topic)["frame"]
        except TimeoutError as exc:
            raise AssertionError(
                f"topic {topic!r} was triggered (HTTP 200) but NO delta arrived "
                f"within 5s — its publish seam is missing (the write happened, "
                f"the wire stayed silent)"
            ) from exc
        assert frame["op"] == expected_op[topic], (topic, frame)
        assert frame["op"] in {"patch", "remove", "signal"}, frame
        if topic == "member":
            # VALUE BINDING — the row's real guard, and the reason this row does
            # not need a mutant to prove it is not vacuous.
            #
            # "a member frame arrived" was satisfiable by ANY member frame,
            # including one this test's own setup produced seconds earlier; that
            # is how the row stayed green with putMember's publish seam bypassed
            # entirely.
            #
            # 🔴 CORRECTION (independent review round 2, MEASURED — the previous
            # version of this comment claimed, verbatim:
            #     "a stale frame is inherently about a DIFFERENT member (a
            #      scratch hire, some other test's roster write), so it can
            #      never satisfy this"
            # and that the check "holds no matter WHERE a future stray write is
            # added". **The first claim is false and is quoted here so nobody
            # trusts it again.** The polluting frame comes from `fresh_member()`
            # — and `member` IS that member, so `payload["id"] == member` is
            # TRUE for the stale frame (measured: `'m-ca96…' == 'm-ca96…'`).
            # The SUBJECT does not discriminate at all.
            #
            # What actually discriminates is the VALUE this PATCH just wrote:
            # the payload is an eager snapshot taken inside hub.Publish, so a
            # frame published BEFORE this write cannot carry the name this write
            # sets. The id check below is kept only as a sanity check (right
            # entity), NOT as the guard — do not lean on it.
            #
            # ⚠️ DEGRADATION CONDITION, stated so it cannot be re-discovered the
            # hard way: this guard is only as strong as "the row PATCHes a field
            # whose value the frame echoes back". If the row is ever changed to
            # PATCH something else (desired_state, role, …), value binding is
            # gone and the row falls back to "some member delta arrived" — the
            # vacuous state this whole ticket exists to remove. That is why the
            # body is a named dict and why the first assertion below is about
            # the TEST ITSELF: change the body without re-binding this check and
            # the row goes RED with instructions, instead of going quietly
            # green.
            assert "name" in member_patch_body, (
                "the member row no longer PATCHes `name`, so the value binding "
                "below has nothing to bind to. Do NOT delete the binding: pick "
                "a field this write actually sets AND that the member payload "
                "echoes back, and assert that instead. Dropping it silently "
                "returns this row to 'any member frame will do', which a stale "
                f"setup frame satisfies. Current body: {member_patch_body}"
            )
            payload = frame["data"]["payload"]
            assert payload["id"] == member, (
                f"member row: delta for the wrong entity (expected {member!r}). "
                f"Got payload: {payload}"
            )
            assert payload["name"] == member_patch_body["name"], (
                f"the member row observed a delta that does NOT carry the value "
                f"the PATCH it just issued wrote (expected name "
                f"{member_patch_body['name']!r}) — this is the stale-frame "
                f"failure mode: without this check the row passes while the "
                f"write's publish seam is missing. Note the subject alone would "
                f"NOT have caught it: the polluting frame is about this very "
                f"member. Got payload: {payload}"
            )
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
# executor only — the creator was dropped by T-0eb5); every other agent's
# stream stays quiet. This replaced the
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
    still online and the configured waking TTL has not lapsed)."""
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


# ── zombie stop gate (pre-stream 409 once the stop has COLLECTED) ────────────
#
# Defence line B of the zombie-agent work: a listener that survived its kill
# must never RE-project a stopped member online by reconnecting. A dismissed
# member's reconnect, a force-stopped member's reconnect and a member that has
# REPORTED stopped are refused PRE-stream with the conflict envelope (the
# dual-SSE guard's envelope family).
#
# What the gate deliberately does NOT refuse is the window in between: 下線 has
# no clock, so a session legitimately sits at desired_state=offline ∧
# stopping_since for as long as its close-out takes, and the listener treats a
# run of authoritative refusals as "I have been retired" and kills its own
# session — refusing there is how a station upgrade or a network blip takes a
# hand-off down half-written. The invariant the refusal used to carry in that
# window is carried instead by the §5 projection: stopping_since DOMINATES, so
# a readmitted connection reads `stopping`, never `online` — pinned below, and
# it is the assertion that keeps this widening honest.
#
# The gate is also narrower than desired_state=offline alone: a freshly hired
# member (desired offline, NO anchors) still connects — pinned below — and
# activate lifts the gate in the same write it flips desired_state in.


def test_close_out_in_flight_reconnect_admitted_then_refused_once_stopped(
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
    # Wait for the disconnect to land server-side (so the reconnect below is a
    # genuine re-admission, not a dual-SSE takeover of a live slot).
    got = _poll_presence(client, owner_token, agent.member_id, "stopped")
    assert got == "stopped", f"expected the graceful-stop projection, got {got!r}"

    # Close-out still in flight: the reconnect is ADMITTED and streams…
    with SSEConnection(base_url, agent.token) as resumed:
        assert resumed.status_code == 200, (
            "a reconnect during the close-out must be admitted (refusing it "
            f"self-terminates the agent mid-hand-off), got {resumed.status_code}"
        )
        resumed.wait_for(lambda ev: ev["comment"] == "connected")
        # …and it does NOT buy the connection a green light: the stop anchor
        # dominates the projection, which is what makes admitting it safe.
        assert _presence(client, owner_token, agent.member_id) == "stopping"
        # The agent finishes its sequence on that resumed stream.
        r = client.post("/api/self/stopped", json={}, headers=_auth(agent.token))
        assert r.status_code == 200, r.text
    got = _poll_presence(client, owner_token, agent.member_id, "stopped")
    assert got == "stopped", f"expected the graceful-stop projection, got {got!r}"

    # Reported stopped ⇒ the close-out is over ⇒ the zombie reconnect is
    # refused pre-stream, conflict envelope, no stream.
    zombie = SSEConnection(base_url, agent.token)
    assert zombie.status_code == 409, (
        f"a reconnect after report_stopped must be refused 409, got {zombie.status_code}"
    )
    ctype = (zombie.headers or {}).get("content-type", "")
    assert not ctype.startswith("text/event-stream"), (
        "the refusal must be a plain HTTP response, not a stream"
    )
    body = json.loads(zombie.error_body)
    assert set(body) == {"error"} and set(body["error"]) == {"code", "message"}, body
    assert body["error"]["code"] == "conflict", body
    # Pin WHICH arm refused: the roster arm answers a different message, and a
    # 409 that came from there would make this test blind to the stop arm.
    assert "stop in effect" in body["error"]["message"], body
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


def test_force_stopped_member_reconnect_refused(
    base_url, client, owner_token
) -> None:
    """The other arm that stays shut: a member the owner FORCE-stopped was cut
    off deliberately, so it must not come back on its own — unlike the graceful
    close-out above, which is admitted precisely because it is still working."""
    agent = _fresh_agent(client, owner_token, f"forced-{uuid.uuid4().hex[:6]}")
    with SSEConnection(base_url, agent.token) as conn:
        assert conn.status_code == 200, conn.error_body
        conn.wait_for(lambda ev: ev["comment"] == "connected")
        r = client.post(
            f"/api/members/{agent.member_id}/force-stop", json={},
            headers=_auth(owner_token),
        )
        assert r.status_code == 200, r.text
    _poll_presence(client, owner_token, agent.member_id, "stopped")

    zombie = SSEConnection(base_url, agent.token)
    assert zombie.status_code == 409, (
        f"a force-stopped member's reconnect must be refused 409, got {zombie.status_code}"
    )
    body = json.loads(zombie.error_body)
    assert body["error"]["code"] == "conflict", body
    assert "stop in effect" in body["error"]["message"], body
    zombie.close()

    # …and a LATER deactivate must not downgrade that verdict. The two arms are
    # told apart by comparing the anchors, so a deactivate that re-stamped
    # stopping_since to now would move a force-stopped member onto the admitted
    # side — and the 下線 arm runs no clock, so nothing would collect it after.
    # The cockpit offers no 下線 button in this state, but the API does.
    r = client.post(
        f"/api/members/{agent.member_id}/deactivate", headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    still = SSEConnection(base_url, agent.token)
    assert still.status_code == 409, (
        "a deactivate after a force-stop must not re-open the gate, got "
        f"{still.status_code}"
    )
    assert "stop in effect" in json.loads(still.error_body)["error"]["message"]
    still.close()


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
        # EXACT set, not a subset: an extra key here is a field the warden was
        # never told about. `task_type` was in this set until T-2 — it was
        # sourced from the lessons bucket and carried for parity only — and its
        # removal is what this equality now pins.
        assert set(args) == {
            "member_id", "persona_context", "member_token", "role",
            "runtime", "model", "effort", "session_name",
        }, args
        assert args["member_id"] == member_id, args
        assert args["runtime"] == "claude", args
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
# owner reported the badge staying blank for a gate card until a manual reload.
# test_every_closed_topic_emits triggers reply_card through an UNBOUND create;
# this pins the BOUND create — a linked_task that arms a step (T-18 folded
# open_gate into it) — actually fanning a reply_card delta to the owner
# connection, byte-for-byte the same live-update signal the badge rides. If the
# badge bug is a missing frame, this goes red.
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
    # T-91: create answers taskCreateResultDTO — the minted id, not the task.
    task_id = r.json()["task_id"]
    r = client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "approve", "dod": "go", "is_gate": True}]},
        headers=h,
    )
    assert r.status_code == 200, r.text
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = client.get(f"/api/tasks/{task_id}", headers=h).json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): report the step in_progress so the task
    # derives in_progress — a card can only bind an in_progress task.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h,
    ).status_code == 200
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "gate probe", "options": [{"text": "go"}, {"text": "hold"}],
              "linked_task": {"task_id": task_id, "step_id": step_id}},
        headers=h,
    )
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]
    frame = owner_sse.wait_for_frame("reply_card")["frame"]
    assert frame["op"] == "patch", frame
    assert frame["data"]["payload"]["id"] == card_id, frame
    assert frame["data"]["payload"]["status"] == "waiting", frame
