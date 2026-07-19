"""Reply-card face — the 等我回覆卡 state machine and pane semantics.

M2 reply-card batch B1. The auth matrix pins WHO may call the six routes and
test_rest_happy pins the happy shapes; this file pins the BEHAVIOUR the SPEC
promises:

  * a card opens WAITING and simultaneously rides the chat stream (one chat
    message, meta.reply_card_id ↔ card.chat_message_id — the jump-to-origin
    link both ways);
  * an ANSWER is the only POSITIVE close: option pick, free text (a
    counter-question included), or an attachment all flip waiting→answered;
    there is NO close/skip surface (probed: no such routes exist). The
    owner-only EXPIRE (T-1aa4) is the sole other exit — waiting→expired,
    terminal, NOT an answer (its own test section below);
  * one-shot: a second POST answer is 409 (the agent asks again with a NEW
    card, never a reopen);
  * 重新決定 (PUT re-answer): only on an ANSWERED card (waiting → 409),
    replaces the answer, status STAYS answered;
  * creation limits: kind closed set, options 1..4 non-blank, blank summary
    refused;
  * panes: ?status=waiting sorts longest-waiting first; ?status=answered is
    the recently-answered window carrying the final answer; the badge count
    tracks waiting only;
  * the answer reaches the agent WITH the card context: the agent's own SSE
    connection receives the reply_card delta and a card refetch carries the
    original options + the owner's answer (+ attachment round-trip).

DEGRADED (honest): the 24h expiry of the recently-answered pane is time-based
and cannot be observed black-box without a 24h clock or time injection — the
window cutoff (boundary inclusive) is pinned by the server's unit tests
(api_replycards_test.go); this file pins the window's population + ordering.
"""

from __future__ import annotations

import base64

import pytest

from conftest import AgentIdentity, hire_member, mint_member_token
from sse_client import SSEConnection

_PNG_B64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8"
    "z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


@pytest.fixture(scope="module")
def asker(client, owner_token) -> AgentIdentity:
    """This module's OWN initiating agent (keeps SSE usage clear of other
    files' agent fixtures — single-session rule)."""
    member_id = hire_member(client, owner_token, "conf-replycard-asker")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="")


def _open_card(client, asker: AgentIdentity, summary="need a call",
               kind="decision", options=("AI pick", "other")) -> dict:
    r = client.post(
        "/api/reply-cards",
        json={"kind": kind, "summary": summary, "options": list(options)},
        headers=_auth(asker.token),
    )
    assert r.status_code == 200, f"open card failed: {r.status_code} {r.text}"
    return r.json()


def _answer(client, owner_token, card_id: str, body: dict, method="POST"):
    return client.request(
        method, f"/api/reply-cards/{card_id}/answer",
        json=body, headers=_auth(owner_token),
    )


def _get_card(client, token, card_id: str) -> dict:
    r = client.get(f"/api/reply-cards/{card_id}", headers=_auth(token))
    assert r.status_code == 200, r.text
    return r.json()


def _waiting_count(client, owner_token) -> int:
    r = client.get("/api/reply-cards/count", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    return r.json()["waiting"]


# ── create: waiting + rides the chat stream ──────────────────────────────────


def test_card_opens_waiting_and_rides_the_chat_stream(client, owner_token, asker):
    card = _open_card(client, asker, summary="ship the release?")
    assert card["status"] == "waiting"
    assert card["from"] == asker.member_id
    assert card["options"][0] == "AI pick"  # [0] = the AI recommendation slot
    assert card["answer"] is None and card["answered_ts"] is None

    # The companion chat message exists, is FROM the asker TO the owner, and
    # both links hold: card.chat_message_id ↔ message.meta.reply_card_id.
    r = client.get(
        f"/api/chat?with={asker.member_id}&limit=-1", headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    msgs = {m["id"]: m for m in r.json()}
    msg = msgs.get(card["chat_message_id"])
    assert msg, f"card's chat message missing: {card['chat_message_id']}"
    assert msg["from"] == asker.member_id and msg["to"] == "owner"
    assert msg["meta"].get("reply_card_id") == card["id"]
    assert msg["body"] == "ship the release?"


def test_create_validation_rules(client, asker):
    def post(body):
        return client.post(
            "/api/reply-cards", json=body, headers=_auth(asker.token))

    base = {"kind": "decision", "summary": "s", "options": ["a", "b"]}
    assert post({**base, "kind": "poll"}).status_code == 400
    assert post({**base, "summary": "   "}).status_code == 400
    assert post({**base, "options": []}).status_code == 400
    assert post({**base, "options": ["a", "b", "c", "d", "e"]}).status_code == 400
    assert post({**base, "options": ["a", "  "]}).status_code == 400
    # four options is the inclusive cap
    assert post({**base, "options": ["a", "b", "c", "d"]}).status_code == 200
    # missing required keys are the 422 (decode-layer) face
    assert post({"summary": "s", "options": ["a"]}).status_code == 422
    assert post({"kind": "action", "options": ["a"]}).status_code == 422
    assert post({"kind": "action", "summary": "s"}).status_code == 422


# ── create: question-side attachments (T-5e8a 開卡帶附件) ────────────────────


def test_card_opens_with_question_attachments(client, owner_token, asker):
    # One blob pre-uploaded through the streaming seam (the {id} ref form)…
    png = base64.b64decode(_PNG_B64)
    up = client.post(
        "/api/chat/attachments?filename=card-shot.png",
        content=png, headers=_auth(asker.token),
    )
    assert up.status_code == 200, up.text
    ref = up.json()
    # …plus one inline data_b64 item, on the SAME create.
    r = client.post(
        "/api/reply-cards",
        json={
            "kind": "decision", "summary": "see the screenshots?",
            "options": ["go", "hold"],
            "attachments": [
                {"id": ref["id"]},
                {"data_b64": _PNG_B64, "filename": "inline.png",
                 "mime": "image/png"},
            ],
        },
        headers=_auth(asker.token),
    )
    assert r.status_code == 200, r.text
    card = r.json()
    atts = card["attachments"]
    assert len(atts) == 2, atts
    by_name = {a["filename"]: a for a in atts}
    assert set(by_name) == {"card-shot.png", "inline.png"}, atts
    assert by_name["card-shot.png"]["id"] == ref["id"], atts
    for a in atts:
        assert a["url"] == f"/api/chat/attachment/{a['id']}", a
        assert a["mime"] == "image/png" and a["is_image"] is True, a
        # The owner cockpit downloads through the shared chat blob route.
        served = client.get(a["url"], headers=_auth(owner_token))
        assert served.status_code == 200 and served.content == png

    # The full-card read serves the same refs back.
    got = _get_card(client, owner_token, card["id"])
    assert got["attachments"] == atts, got["attachments"]

    # The LIGHT list row stays light: no attachment refs ride the list.
    r = client.get("/api/reply-cards?status=waiting", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    row = next(x for x in r.json() if x["id"] == card["id"])
    assert "attachments" not in row, row

    # The member gallery (meta-stamped seam) surfaces the card's attachments.
    r = client.get(
        f"/api/chat/attachments?with={asker.member_id}",
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text
    gallery_ids = {e["id"] for e in r.json()}
    assert {a["id"] for a in atts} <= gallery_ids, gallery_ids


def test_card_without_attachments_serves_an_empty_array(client, owner_token, asker):
    card = _open_card(client, asker, summary="no attachments here")
    assert card["attachments"] == []


def test_card_attachment_input_validation(client, asker):
    base = {"kind": "decision", "summary": "s", "options": ["a"]}

    def post(atts):
        return client.post(
            "/api/reply-cards", json={**base, "attachments": atts},
            headers=_auth(asker.token))

    # unknown ref / both id+data_b64 / over the 10-item cap — all 400.
    assert post([{"id": "att-does-not-exist"}]).status_code == 400
    assert post([{"id": "att-x", "data_b64": _PNG_B64}]).status_code == 400
    assert post([{"data_b64": _PNG_B64}] * 11).status_code == 400


# ── answer: the only close ───────────────────────────────────────────────────


def test_option_answer_closes_the_card_and_decrements_the_badge(
    client, owner_token, asker
):
    card = _open_card(client, asker)
    before = _waiting_count(client, owner_token)
    r = _answer(client, owner_token, card["id"], {"option_idx": 0})
    assert r.status_code == 200, r.text
    answered = r.json()
    assert answered["status"] == "answered"
    assert answered["answer"]["option_idx"] == 0
    assert answered["answered_ts"]
    assert _waiting_count(client, owner_token) == before - 1


def test_free_text_counter_question_also_closes_the_card(
    client, owner_token, asker
):
    """SPEC: a typed counter-question (「收件人是誰?」) IS an answer — the card
    closes; the agent asks again with a NEW card."""
    card = _open_card(client, asker, summary="send this mail?")
    r = _answer(client, owner_token, card["id"], {"text": "who is the recipient?"})
    assert r.status_code == 200, r.text
    assert r.json()["status"] == "answered"
    assert r.json()["answer"]["option_idx"] is None
    assert r.json()["answer"]["text"] == "who is the recipient?"


def test_answer_validation_rules(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {}).status_code == 400
    assert _answer(
        client, owner_token, card["id"], {"option_idx": 2}
    ).status_code == 400  # two options → idx 2 out of range
    assert _answer(
        client, owner_token, card["id"], {"option_idx": -1}
    ).status_code == 400
    assert _answer(
        client, owner_token, "rc-conf-missing", {"option_idx": 0}
    ).status_code == 404
    # the card is untouched by the refused answers
    assert _get_card(client, owner_token, card["id"])["status"] == "waiting"


def test_second_answer_is_refused_one_card_one_shot(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {"option_idx": 0}).status_code == 200
    r = _answer(client, owner_token, card["id"], {"option_idx": 1})
    assert r.status_code == 409, f"second POST answer must 409, got {r.status_code}"
    # the stored answer did not change
    assert _get_card(client, owner_token, card["id"])["answer"]["option_idx"] == 0


def test_no_close_or_skip_surface_exists(client, owner_token, asker):
    """SPEC: 沒有純關閉/略過 — probed by construction: no DELETE, no close/skip
    route; the card keeps waiting."""
    card = _open_card(client, asker)
    h = _auth(owner_token)
    assert client.delete(f"/api/reply-cards/{card['id']}", headers=h).status_code == 405
    assert client.post(
        f"/api/reply-cards/{card['id']}/close", headers=h).status_code == 404
    assert client.post(
        f"/api/reply-cards/{card['id']}/skip", headers=h).status_code == 404
    assert _get_card(client, owner_token, card["id"])["status"] == "waiting"


# ── 重新決定 (re-answer) ─────────────────────────────────────────────────────


def test_reanswer_replaces_answer_and_stays_answered(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {"option_idx": 0}).status_code == 200
    first = _get_card(client, owner_token, card["id"])
    r = _answer(client, owner_token, card["id"],
                {"option_idx": 1, "text": "changed my mind"}, method="PUT")
    assert r.status_code == 200, r.text
    revised = r.json()
    assert revised["status"] == "answered"  # never reopens
    assert revised["answer"]["option_idx"] == 1
    assert revised["answer"]["text"] == "changed my mind"
    assert revised["answered_ts"] >= first["answered_ts"]  # re-enters the 24h window
    # the badge never re-counts a revised card
    waiting_ids = {
        c["id"] for c in client.get(
            "/api/reply-cards?status=waiting", headers=_auth(owner_token)
        ).json()
    }
    assert card["id"] not in waiting_ids


def test_reanswer_requires_an_answered_card(client, owner_token, asker):
    card = _open_card(client, asker)
    r = _answer(client, owner_token, card["id"], {"option_idx": 0}, method="PUT")
    assert r.status_code == 409, f"PUT on a waiting card must 409, got {r.status_code}"
    assert _get_card(client, owner_token, card["id"])["status"] == "waiting"


# ── panes + badge ────────────────────────────────────────────────────────────


def test_waiting_pane_sorts_longest_waiting_first(client, owner_token, asker):
    first = _open_card(client, asker, summary="older ask")
    second = _open_card(client, asker, summary="newer ask")
    cards = client.get(
        "/api/reply-cards?status=waiting", headers=_auth(owner_token)
    ).json()
    ids = [c["id"] for c in cards]
    assert ids.index(first["id"]) < ids.index(second["id"]), (
        "waiting pane must order longest-waiting first"
    )
    assert all(c["status"] == "waiting" for c in cards)


def test_answered_pane_carries_the_decision_digest_and_skips_waiting(
    client, owner_token, asker
):
    waiting = _open_card(client, asker, summary="still waiting")
    answered = _open_card(client, asker, summary="answered ask")
    assert _answer(
        client, owner_token, answered["id"], {"option_idx": 1}
    ).status_code == 200
    pane = client.get(
        "/api/reply-cards?status=answered", headers=_auth(owner_token)
    ).json()
    by_id = {c["id"]: c for c in pane}
    assert answered["id"] in by_id
    row = by_id[answered["id"]]
    # The decision DIGEST (T-3f31): the picked index AND its original wording
    # ride the light row; the full option list does NOT (查看當初選項 is a
    # get_reply_card pull now).
    assert row["answer"]["option_idx"] == 1
    assert row["answer"]["option"] == "other"
    assert "options" not in row and "body" not in row
    assert waiting["id"] not in by_id
    # newest answer first
    ts = [c["answered_ts"] for c in pane]
    assert ts == sorted(ts, reverse=True)
    # unknown pane value is refused
    r = client.get("/api/reply-cards?status=closed", headers=_auth(owner_token))
    assert r.status_code == 400


def test_list_rows_are_light_title_plus_decision_only(client, owner_token, asker):
    """T-3f31 owner ruling (卡只需要 title+決策): the list wire carries the
    summary + the decision digest, NEVER the body / options full text — the
    boot-context hog was every card's full interior riding the 24h pane. The
    digest truncates a long answer text to a preview and folds attachments to
    a COUNT; the full card stays one get_reply_card away."""
    card = _open_card(client, asker, summary="light row ask")
    # Give the card a heavy interior via the create body.
    heavy = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "heavy ask",
              "body": "細" * 3000, "options": ["A" * 400, "B" * 400]},
        headers=_auth(asker.token),
    ).json()
    long_text = "答" * 400
    assert _answer(client, owner_token, heavy["id"], {
        "option_idx": 0, "text": long_text,
        "attachments": [{"data_b64": _PNG_B64, "filename": "p.png",
                         "mime": "image/png"}],
    }).status_code == 200

    waiting_pane = client.get(
        "/api/reply-cards?status=waiting", headers=_auth(owner_token)).json()
    row = {c["id"]: c for c in waiting_pane}[card["id"]]
    assert row["summary"] == "light row ask" and row["status"] == "waiting"
    assert row["answer"] is None and row["answered_ts"] is None
    for gone in ("body", "options", "chat_message_id"):
        assert gone not in row, row

    answered_pane = client.get(
        "/api/reply-cards?status=answered", headers=_auth(owner_token)).json()
    row = {c["id"]: c for c in answered_pane}[heavy["id"]]
    # body/options never ride; the answer digest is bounded.
    assert "body" not in row and "options" not in row
    assert row["answer"]["option_idx"] == 0
    assert row["answer"]["option"] == "A" * 400  # the original wording
    assert row["answer"]["text"].endswith("…")
    assert len(row["answer"]["text"]) < len(long_text)
    assert row["answer"]["attachments"] == 1  # a COUNT, not refs
    # The full interior still rides the single-card read.
    full = _get_card(client, owner_token, heavy["id"])
    assert full["body"] == "細" * 3000
    assert full["options"] == ["A" * 400, "B" * 400]
    assert full["answer"]["text"] == long_text
    assert full["answer"]["attachments"][0]["filename"] == "p.png"


def test_list_limit_caps_rows_after_pane_ordering(client, owner_token, asker):
    """?limit=N keeps the pane's FIRST N rows (waiting: longest-waiting first;
    answered: newest answer first); absent / non-positive = the whole pane."""
    first = _open_card(client, asker, summary="limit older")
    second = _open_card(client, asker, summary="limit newer")

    def waiting(query=""):
        r = client.get(f"/api/reply-cards?status=waiting{query}",
                       headers=_auth(owner_token))
        assert r.status_code == 200, r.text
        return [c["id"] for c in r.json()]

    everything = waiting()
    assert first["id"] in everything and second["id"] in everything
    capped = waiting("&limit=1")
    assert capped == everything[:1]
    # Non-positive = uncapped (the whole pane).
    assert waiting("&limit=0") == everything
    assert waiting("&limit=-1") == everything

    # The answered pane caps too, after its newest-answer-first order.
    assert _answer(client, owner_token, first["id"],
                   {"text": "ok"}).status_code == 200
    assert _answer(client, owner_token, second["id"],
                   {"text": "ok"}).status_code == 200
    r = client.get("/api/reply-cards?status=answered&limit=1",
                   headers=_auth(owner_token))
    assert r.status_code == 200
    pane = r.json()
    assert len(pane) == 1 and pane[0]["id"] == second["id"]


def test_badge_counts_waiting_only(client, owner_token, asker):
    before = _waiting_count(client, owner_token)
    card = _open_card(client, asker)
    assert _waiting_count(client, owner_token) == before + 1
    _answer(client, owner_token, card["id"], {"text": "ok"})
    assert _waiting_count(client, owner_token) == before


# ── the answer reaches the agent, with context ───────────────────────────────


def test_answer_reaches_the_agent_with_card_context(
    base_url, client, owner_token, asker
):
    """The agent-side loop: the asker holds its own SSE connection; the owner's
    answer fans a reply_card delta onto it; the agent refetches the card and
    gets the FULL context back — summary, the original option wording, and the
    owner's answer with attachment."""
    with SSEConnection(base_url, asker.token) as agent_sse:
        assert agent_sse.status_code == 200, agent_sse.error_body
        card = _open_card(client, asker, summary="context ride-back")
        agent_sse.wait_for_frame("reply_card")  # the create delta

        r = _answer(client, owner_token, card["id"], {
            "option_idx": 1,
            "text": "see the screenshot",
            "attachments": [{"data_b64": _PNG_B64, "filename": "proof.png",
                             "mime": "image/png"}],
        })
        assert r.status_code == 200, r.text

        frame = agent_sse.wait_for_frame("reply_card")["frame"]
        assert frame["op"] == "patch"
        payload = frame["data"]["payload"]
        assert payload["id"] == card["id"]
        assert payload["from"] == asker.member_id
        assert payload["status"] == "answered"

        # the pull path: the AGENT's own token reads the full card back.
        full = _get_card(client, asker.token, card["id"])
        assert full["summary"] == "context ride-back"
        assert full["options"] == ["AI pick", "other"]
        assert full["answer"]["option_idx"] == 1
        assert full["answer"]["text"] == "see the screenshot"
        atts = full["answer"]["attachments"]
        assert len(atts) == 1 and atts[0]["filename"] == "proof.png"
        blob = client.get(atts[0]["url"], headers=_auth(asker.token))
        assert blob.status_code == 200
        assert blob.content == base64.b64decode(_PNG_B64)


# ── expired (T-1aa4): the owner-only terminal that is NOT an answer ─────────


def _expire(client, owner_token, card_id: str):
    return client.post(
        f"/api/reply-cards/{card_id}/expire", headers=_auth(owner_token))


def test_expire_closes_a_waiting_card_without_an_answer(
    client, owner_token, asker
):
    card = _open_card(client, asker, summary="stale ask?")
    before = _waiting_count(client, owner_token)
    r = _expire(client, owner_token, card["id"])
    assert r.status_code == 200, r.text
    expired = r.json()
    assert expired["status"] == "expired"
    assert expired["expired_ts"]
    assert expired["answer"] is None and expired["answered_ts"] is None
    assert _waiting_count(client, owner_token) == before - 1

    # The card left the waiting pane and shows on the expired pane (24h,
    # expired_ts-keyed) with NO decision digest; the count carries `expired`.
    h = _auth(owner_token)
    waiting_ids = {
        c["id"] for c in client.get(
            "/api/reply-cards?status=waiting", headers=h).json()
    }
    assert card["id"] not in waiting_ids
    expired_rows = client.get(
        "/api/reply-cards?status=expired", headers=h).json()
    row = next(c for c in expired_rows if c["id"] == card["id"])
    assert row["status"] == "expired" and row["expired_ts"]
    assert row["answer"] is None
    counts = client.get("/api/reply-cards/count", headers=h).json()
    assert counts["expired"] >= 1


def test_expire_is_terminal_no_reopen_no_answer(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _expire(client, owner_token, card["id"]).status_code == 200
    # one-shot terminal: a second expire, an answer, and a re-answer all 409.
    assert _expire(client, owner_token, card["id"]).status_code == 409
    assert _answer(
        client, owner_token, card["id"], {"option_idx": 0}
    ).status_code == 409
    assert _answer(
        client, owner_token, card["id"], {"text": "late"}, method="PUT"
    ).status_code == 409
    got = _get_card(client, owner_token, card["id"])
    assert got["status"] == "expired" and got["answer"] is None


def test_expire_refused_on_answered_or_missing_cards(
    client, owner_token, asker
):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {"option_idx": 0}).status_code == 200
    assert _expire(client, owner_token, card["id"]).status_code == 409
    assert _get_card(client, owner_token, card["id"])["status"] == "answered"
    assert _expire(client, owner_token, "rc-conf-missing").status_code == 404


def test_expiring_a_gate_card_resumes_the_task_and_step(
    client, owner_token, asker
):
    """The expire twin of the answer's server-driven 答卡→回前態: the bound
    step and task return to in_progress; the agent then advances itself."""
    h_agent = _auth(asker.token)
    r = client.post(
        "/api/tasks",
        json={"title": "conf expire gate", "executor_member_id": asker.member_id},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    task_id = r.json()["task"]["id"]
    r = client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "approve", "dod": "go", "is_gate": True}]},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    step_id = r.json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): report the step in_progress so the task
    # derives in_progress — a gate can only arm on an in_progress task.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h_agent,
    ).status_code == 200
    r = client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/gate",
        json={"kind": "decision", "summary": "go?", "options": ["go", "hold"]},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]

    task = client.get(f"/api/tasks/{task_id}", headers=_auth(owner_token)).json()
    assert task["status"] == "waiting_owner"

    assert _expire(client, owner_token, card_id).status_code == 200
    task = client.get(f"/api/tasks/{task_id}", headers=_auth(owner_token)).json()
    assert task["status"] == "in_progress"
    assert task["steps"][0]["status"] == "in_progress"


def test_expire_is_the_only_exit_for_an_orphaned_card(
    client, owner_token, asker
):
    """T-f571 orphans (a waiting card on an already-terminal task) cannot be
    answered — expire is their one exit, and the closed task stays closed."""
    h_agent = _auth(asker.token)
    r = client.post(
        "/api/tasks",
        json={"title": "conf orphan expire", "executor_member_id": asker.member_id},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    task_id = r.json()["task"]["id"]
    r = client.post(
        f"/api/tasks/{task_id}/plan",
        json={"steps": [{"name": "approve", "dod": "go", "is_gate": True}]},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    step_id = r.json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): lift the task to in_progress via the step
    # report so the gate can arm.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h_agent,
    ).status_code == 200
    r = client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/gate",
        json={"kind": "decision", "summary": "go?", "options": ["go"]},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]
    # The owner terminates the task under the still-waiting card → orphan.
    assert client.post(
        f"/api/tasks/{task_id}/terminate", headers=_auth(owner_token)
    ).status_code == 200

    assert _answer(
        client, owner_token, card_id, {"option_idx": 0}
    ).status_code == 409
    assert _expire(client, owner_token, card_id).status_code == 200
    assert _get_card(client, owner_token, card_id)["status"] == "expired"
    task = client.get(f"/api/tasks/{task_id}", headers=_auth(owner_token)).json()
    assert task["status"] == "terminated"


def test_expired_delta_reaches_the_initiating_agent(
    base_url, client, owner_token, asker
):
    """The expiry rides the same reply_card downlink as an answer: op patch,
    payload {id, from, status:"expired"} — the agent refetches and reads the
    terminal state (no answer to read)."""
    with SSEConnection(base_url, asker.token) as agent_sse:
        assert agent_sse.status_code == 200, agent_sse.error_body
        card = _open_card(client, asker, summary="expiry ride-back")
        agent_sse.wait_for_frame("reply_card")  # the create delta

        assert _expire(client, owner_token, card["id"]).status_code == 200

        frame = agent_sse.wait_for_frame("reply_card")["frame"]
        assert frame["op"] == "patch"
        payload = frame["data"]["payload"]
        assert payload["id"] == card["id"]
        assert payload["from"] == asker.member_id
        assert payload["status"] == "expired"

        full = _get_card(client, asker.token, card["id"])
        assert full["status"] == "expired" and full["expired_ts"]
        assert full["answer"] is None
