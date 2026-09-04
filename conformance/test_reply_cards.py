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
    EXPIRE is the sole other exit — waiting→expired, terminal, NOT an answer
    (its own test section below). WHO may press it moved twice and the auth
    matrix owns that question: owner-only at T-1aa4, owner/admin agent from
    T-6020, and from T-1b88 (owner 2026-08-07, card rc-3ff94b116970) also the
    card's OWN AUTHOR — a route floor of `agent` plus an in-handler authorship
    check, so an agent may retire the unanswered card IT opened while a
    stranger's card is 403. The cases below drive it as the owner, which is
    still permitted; answering (answer/reanswer) stayed governance-only, and an
    ALREADY-ANSWERED card is 409 for everyone including the owner;
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


DEFAULT_OPTIONS = ({"text": "AI pick", "ai_pick": True}, {"text": "other"})


def _open_card(client, asker: AgentIdentity, summary="need a call",
               kind="decision", options=DEFAULT_OPTIONS,
               select_mode=None) -> dict:
    body = {"kind": kind, "summary": summary, "options": list(options),
            "linked_task": None}
    if select_mode is not None:
        body["select_mode"] = select_mode
    r = client.post(
        "/api/reply-cards",
        json=body,
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
    # ai_pick — not position — is what marks the AI's recommendation.
    assert card["options"] == [{"text": "AI pick", "ai_pick": True},
                               {"text": "other", "ai_pick": False}]
    assert card["select_mode"] == "single"
    assert card["answer"] is None and card["answered_ts"] is None

    # The companion chat message exists, is FROM the asker TO the owner, and
    # both links hold: card.chat_message_id ↔ message.meta.reply_card_id.
    r = client.get(
        f"/api/chat?with={asker.member_id}&limit=-1", headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    msgs = {m["id"]: m for m in r.json()["messages"]}
    msg = msgs.get(card["chat_message_id"])
    assert msg, f"card's chat message missing: {card['chat_message_id']}"
    assert msg["from"] == asker.member_id and msg["to"] == "owner"
    assert msg["meta"].get("reply_card_id") == card["id"]
    assert msg["body"] == "ship the release?"


def test_create_validation_rules(client, asker):
    def post(body):
        return client.post(
            "/api/reply-cards", json=body, headers=_auth(asker.token))

    base = {"kind": "decision", "summary": "s", "options": [{"text": "a"}, {"text": "b"}],
            "linked_task": None}

    def refused(body, message):
        """400 FOR THE STATED REASON, compared IN FULL.

        linked_task is required since T-18 and its refusal is also a 400, so a
        status-only assertion here would stay green while every rule below went
        untested — drop linked_task from `base` and nothing would notice. A
        substring probe has the same hole one level down: the linked_task
        sentence names task_id and step_id, so a fragment like "options" could
        in principle match the wrong refusal. Pin the whole sentence."""
        r = post(body)
        assert r.status_code == 400, f"{r.status_code} {r.text}"
        assert r.json()["error"]["message"] == message, r.text

    refused({**base, "kind": "poll"}, "kind must be 'decision' or 'action'")
    refused({**base, "summary": "   "}, "summary must not be blank")
    refused({**base, "options": []}, "options must carry at least one choice")
    refused({**base, "options": [{"text": "a"}, {"text": "b"}, {"text": "c"},
                                 {"text": "d"}, {"text": "e"}]},
            "a single-select card may carry at most 4 options")
    # T-43: the cap is per select_mode, so the multi card refuses at ITS OWN
    # number — asserting only the single one would leave 20 unpinned.
    refused({**base, "select_mode": "multi",
             "options": [{"text": str(i)} for i in range(21)]},
            "a multi-select card may carry at most 20 options")
    refused({**base, "options": [{"text": "a"}, {"text": "  "}]},
            "options must not be blank")
    refused({**base, "select_mode": "many"},
            "select_mode must be 'single' or 'multi'")
    # ai_pick replaces the old positional convention, and a single-select card
    # may answer "which does the AI suggest" at most once.
    refused({**base, "options": [{"text": "a", "ai_pick": True},
                                 {"text": "b", "ai_pick": True}]},
            "a single-select card may mark at most one option ai_pick")
    # ...which is a select_mode rule, not a blanket ban: multi takes both.
    assert post({**base, "select_mode": "multi",
                 "options": [{"text": "a", "ai_pick": True},
                             {"text": "b", "ai_pick": True}]}).status_code == 200
    # four options is the inclusive cap on a single card…
    assert post({**base, "options": [{"text": "a"}, {"text": "b"}, {"text": "c"}, {"text": "d"}]}).status_code == 200
    # …and twenty is the inclusive cap on a multi one (T-43).
    assert post({**base, "select_mode": "multi",
                 "options": [{"text": str(i)} for i in range(20)]}).status_code == 200
    # missing required keys are the 422 (decode-layer) face
    assert post({"summary": "s", "options": [{"text": "a"}]}).status_code == 422
    assert post({"kind": "action", "options": [{"text": "a"}]}).status_code == 422
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
            "options": [{"text": "go"}, {"text": "hold"}], "linked_task": None,
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
    # 🔴 linked_task is REQUIRED (T-18) and its refusal is ALSO a 400. Without it
    # in `base` every assertion below still passed while proving nothing: the
    # create was rejected at the linked_task gate and never reached a single
    # attachment rule. So each case now asserts the REASON, not just the status —
    # a 400 for the wrong reason is indistinguishable from a 400 for the right
    # one, which is the exact failure mode this whole ticket is about.
    base = {"kind": "decision", "summary": "s", "options": [{"text": "a"}],
            "linked_task": None}

    def post(atts):
        return client.post(
            "/api/reply-cards", json={**base, "attachments": atts},
            headers=_auth(asker.token))

    def refused_because(atts, needle):
        r = post(atts)
        assert r.status_code == 400, f"{r.status_code} {r.text}"
        msg = r.json()["error"]["message"]
        assert needle in msg, (
            f"refused for the wrong reason: wanted {needle!r}, got {msg!r}")
        assert "linked_task" not in msg, (
            "this case never reached the attachment rules — it was rejected at "
            f"the linked_task gate: {msg!r}")

    # unknown ref / both id+data_b64 / over the 10-item cap — all 400, each for
    # its own reason (T-5e8a).
    refused_because([{"id": "att-does-not-exist"}], "att-does-not-exist")
    refused_because([{"id": "att-x", "data_b64": _PNG_B64}],
                    "carries both id and data_b64")
    refused_because([{"data_b64": _PNG_B64}] * 11,
                    "at most 10 attachments")


# ── answer: the only close ───────────────────────────────────────────────────


def test_option_answer_closes_the_card_and_decrements_the_badge(
    client, owner_token, asker
):
    card = _open_card(client, asker)
    before = _waiting_count(client, owner_token)
    r = _answer(client, owner_token, card["id"], {"option_idxs": [0]})
    assert r.status_code == 200, r.text
    answered = r.json()
    assert answered["status"] == "answered"
    assert answered["answer"]["option_idxs"] == [0]
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
    assert r.json()["answer"]["option_idxs"] is None
    assert r.json()["answer"]["text"] == "who is the recipient?"


def test_answer_validation_rules(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {}).status_code == 400
    assert _answer(
        client, owner_token, card["id"], {"option_idxs": [2]}
    ).status_code == 400  # two options → idx 2 out of range
    assert _answer(
        client, owner_token, card["id"], {"option_idxs": [-1]}
    ).status_code == 400
    assert _answer(
        client, owner_token, "rc-conf-missing", {"option_idxs": [0]}
    ).status_code == 404
    # the card is untouched by the refused answers
    assert _get_card(client, owner_token, card["id"])["status"] == "waiting"


MULTI_OPTIONS = ({"text": "甲"}, {"text": "乙"}, {"text": "丙"})


def test_empty_option_idxs_list_is_not_an_answer(client, owner_token, asker):
    """``option_idxs: []`` carries no decision and must be refused exactly like
    an empty body.

    Its own test because the guard changed SHAPE: the field used to be one
    nullable integer, where "absent" was the only way to carry no option. As a
    LIST, ``[]`` is present-but-empty — and a server that tested presence rather
    than length would close the card and release its task hold on a decision the
    owner never made, answering 200 with nothing wrong on the wire to see."""
    card = _open_card(client, asker, select_mode="multi", options=MULTI_OPTIONS)

    r = _answer(client, owner_token, card["id"], {"option_idxs": []})

    assert r.status_code == 400, f"{r.status_code} {r.text}"
    assert r.json()["error"]["message"] == (
        "answer must carry an option, text, or an attachment"), r.text
    after = _get_card(client, owner_token, card["id"])
    assert after["status"] == "waiting"
    assert after["answer"] is None and after["answered_ts"] is None


def test_answer_option_idxs_are_stored_deduped_and_ascending(
    client, owner_token, asker
):
    """The owner's CLICK ORDER is not part of the decision: [2,0] and [0,2] say
    the same thing and must come back off the wire identical.

    A reader that could tell them apart once read a re-ordered re-answer as a
    CHANGED one and swallowed a delivery."""
    descending = _open_card(client, asker, summary="order A",
                            select_mode="multi", options=MULTI_OPTIONS)
    ascending = _open_card(client, asker, summary="order B",
                           select_mode="multi", options=MULTI_OPTIONS)

    assert _answer(client, owner_token, descending["id"],
                   {"option_idxs": [2, 0]}).status_code == 200
    assert _answer(client, owner_token, ascending["id"],
                   {"option_idxs": [0, 2]}).status_code == 200

    a = _get_card(client, owner_token, descending["id"])["answer"]
    b = _get_card(client, owner_token, ascending["id"])["answer"]
    assert a["option_idxs"] == [0, 2], a
    assert a["option_idxs"] == b["option_idxs"]

    dup = _open_card(client, asker, summary="dupes",
                     select_mode="multi", options=MULTI_OPTIONS)
    assert _answer(client, owner_token, dup["id"],
                   {"option_idxs": [1, 1, 0]}).status_code == 200
    assert _get_card(client, owner_token, dup["id"])["answer"]["option_idxs"] == [0, 1]


def test_single_select_card_refuses_two_indices(client, owner_token, asker):
    """A single-select card takes ONE circled option. Quietly keeping the first
    of two would record a decision the owner did not make, on a card that looks
    perfectly well-formed afterwards."""
    card = _open_card(client, asker, options=MULTI_OPTIONS)  # select_mode defaults to single
    assert card["select_mode"] == "single"

    r = _answer(client, owner_token, card["id"], {"option_idxs": [0, 2]})

    assert r.status_code == 400, f"{r.status_code} {r.text}"
    assert r.json()["error"]["message"] == (
        "this card is single-select: option_idxs may carry at most one index"), r.text
    assert _get_card(client, owner_token, card["id"])["status"] == "waiting"

    # One index is fine on the same card...
    assert _answer(client, owner_token, card["id"],
                   {"option_idxs": [2]}).status_code == 200
    assert _get_card(client, owner_token, card["id"])["answer"]["option_idxs"] == [2]

    # ...and a MULTI card of the same shape takes both, so the refusal above is
    # the select_mode gate rather than a blanket ban on two indices.
    multi = _open_card(client, asker, summary="multi twin",
                       select_mode="multi", options=MULTI_OPTIONS)
    assert _answer(client, owner_token, multi["id"],
                   {"option_idxs": [0, 2]}).status_code == 200
    answered = _get_card(client, owner_token, multi["id"])
    assert answered["answer"]["option_idxs"] == [0, 2]


def test_multi_select_digest_carries_every_circled_option(
    client, owner_token, asker
):
    """The LIGHT list row is the agent-facing contract, so a multi-select answer
    must show EVERY circled option there — reporting only the first would tell
    the asker the owner chose less than it did, with nothing malformed to
    notice."""
    card = _open_card(client, asker, summary="multi digest",
                      select_mode="multi", options=MULTI_OPTIONS)
    assert _answer(client, owner_token, card["id"],
                   {"option_idxs": [2, 0]}).status_code == 200

    pane = client.get("/api/reply-cards?status=answered",
                      headers=_auth(owner_token)).json()
    row = {c["id"]: c for c in pane}[card["id"]]
    assert row["answer"]["option_idxs"] == [0, 2]
    assert row["answer"]["options"] == ["甲", "丙"]


def test_second_answer_is_refused_one_card_one_shot(client, owner_token, asker):
    card = _open_card(client, asker)
    assert _answer(client, owner_token, card["id"], {"option_idxs": [0]}).status_code == 200
    r = _answer(client, owner_token, card["id"], {"option_idxs": [1]})
    assert r.status_code == 409, f"second POST answer must 409, got {r.status_code}"
    # the stored answer did not change
    assert _get_card(client, owner_token, card["id"])["answer"]["option_idxs"] == [0]


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
    assert _answer(client, owner_token, card["id"], {"option_idxs": [0]}).status_code == 200
    first = _get_card(client, owner_token, card["id"])
    r = _answer(client, owner_token, card["id"],
                {"option_idxs": [1], "text": "changed my mind"}, method="PUT")
    assert r.status_code == 200, r.text
    revised = r.json()
    assert revised["status"] == "answered"  # never reopens
    assert revised["answer"]["option_idxs"] == [1]
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
    r = _answer(client, owner_token, card["id"], {"option_idxs": [0]}, method="PUT")
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
        client, owner_token, answered["id"], {"option_idxs": [1]}
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
    assert row["answer"]["option_idxs"] == [1]
    assert row["answer"]["options"] == ["other"]
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
        json={"kind": "decision", "summary": "heavy ask", "linked_task": None,
              "body": "細" * 3000, "options": [{"text": "A" * 400}, {"text": "B" * 400}]},
        headers=_auth(asker.token),
    ).json()
    long_text = "答" * 400
    assert _answer(client, owner_token, heavy["id"], {
        "option_idxs": [0], "text": long_text,
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
    assert row["answer"]["option_idxs"] == [0]
    assert row["answer"]["options"] == ["A" * 400]  # the original wording
    assert row["answer"]["text"].endswith("…")
    assert len(row["answer"]["text"]) < len(long_text)
    assert row["answer"]["attachments"] == 1  # a COUNT, not refs
    # The full interior still rides the single-card read.
    full = _get_card(client, owner_token, heavy["id"])
    assert full["body"] == "細" * 3000
    assert full["options"] == [{"text": "A" * 400, "ai_pick": False},
                               {"text": "B" * 400, "ai_pick": False}]
    assert full["answer"]["text"] == long_text
    assert full["answer"]["attachments"][0]["filename"] == "p.png"


def test_view_full_serves_the_same_pane_as_whole_cards(client, owner_token, asker):
    """T-a3e4: ?view=full serves the SAME pane, same rows, same order, as FULL
    cards — each row byte-identical to that card's own get_reply_card.

    Why the endpoint exists: a renderer that draws the whole card (the cockpit's
    panes and its inline chat cards) had to follow the light list with one
    GET /api/reply-cards/{id} PER ROW, so opening one pane cost one ROUND TRIP
    per waiting card. The win is round trips, NOT bytes — a full pane is very
    nearly the same size either way, so nothing here asserts a size."""
    card = _open_card(client, asker, summary="full view ask")
    heavy = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "heavy full ask", "linked_task": None,
              "body": "細" * 3000, "options": [{"text": "A" * 400}, {"text": "B" * 400}]},
        headers=_auth(asker.token),
    ).json()
    long_text = "答" * 400
    assert _answer(client, owner_token, heavy["id"], {
        "option_idxs": [0], "text": long_text,
        "attachments": [{"data_b64": _PNG_B64, "filename": "p.png",
                         "mime": "image/png"}],
    }).status_code == 200

    for status, wanted in (("waiting", card["id"]), ("answered", heavy["id"])):
        r = client.get(f"/api/reply-cards?status={status}&view=full",
                       headers=_auth(owner_token))
        assert r.status_code == 200, r.text
        pane = r.json()
        assert pane, f"{status} pane empty — nothing is being compared"
        rows = {c["id"]: c for c in pane}
        assert wanted in rows, (wanted, list(rows))
        # Byte-identity with the per-row GET this replaces: the client is not
        # being handed a THIRD shape it has to special-case.
        for cid, row in rows.items():
            assert row == _get_card(client, owner_token, cid), cid

    full_row = {c["id"]: c for c in client.get(
        "/api/reply-cards?status=answered&view=full",
        headers=_auth(owner_token)).json()}[heavy["id"]]
    # Everything T-3f31 took OUT of the light row is present again...
    assert full_row["body"] == "細" * 3000
    assert full_row["options"] == [{"text": "A" * 400, "ai_pick": False},
                                   {"text": "B" * 400, "ai_pick": False}]
    assert full_row["chat_message_id"]
    # ...including the answer in full: untruncated text and real attachment
    # REFS, where the light digest carried a preview and a COUNT (an int). The
    # two shapes disagree on a type, so this is not merely longer/shorter.
    assert full_row["answer"]["text"] == long_text
    assert full_row["answer"]["attachments"][0]["filename"] == "p.png"

    light_row = {c["id"]: c for c in client.get(
        "/api/reply-cards?status=answered",
        headers=_auth(owner_token)).json()}[heavy["id"]]
    assert light_row["answer"]["attachments"] == 1
    assert "body" not in light_row and "options" not in light_row

    # Same pane means same ORDER, and ?limit= still caps AFTER that ordering.
    # A projection that reordered or re-selected rows would be a different
    # endpoint wearing the same name.
    def ids(query):
        r = client.get(f"/api/reply-cards?status=waiting{query}",
                       headers=_auth(owner_token))
        assert r.status_code == 200, r.text
        return [c["id"] for c in r.json()]

    light_order = ids("")
    assert len(light_order) > 1, "need 2+ waiting rows for order to mean anything"
    assert ids("&view=full") == light_order
    assert ids("&view=full&limit=1") == light_order[:1]


def test_view_defaults_to_light_and_rejects_anything_else(client, owner_token, asker):
    """`view` is OPTIONAL and the default is UNCHANGED (owner red line): every
    client that predates T-a3e4 — including the list_reply_cards MCP tool, which
    cannot send the parameter at all — keeps getting the light rows.

    An unrecognised value is a 400 naming both accepted values: silently serving
    light on a typo would restore the per-row fan-out with no signal, which is
    the exact cost this parameter removes."""
    _open_card(client, asker, summary="default projection ask")

    absent = client.get("/api/reply-cards?status=waiting",
                        headers=_auth(owner_token))
    explicit = client.get("/api/reply-cards?status=waiting&view=light",
                          headers=_auth(owner_token))
    assert absent.status_code == explicit.status_code == 200
    assert absent.json() == explicit.json()
    for row in absent.json():
        for gone in ("body", "options", "chat_message_id", "attachments"):
            assert gone not in row, row

    for bad in ("Full", "FULL", "list", "complete", "1"):
        r = client.get(f"/api/reply-cards?status=waiting&view={bad}",
                       headers=_auth(owner_token))
        assert r.status_code == 400, (bad, r.status_code, r.text)
        assert "light" in r.text and "full" in r.text, r.text
    # Positive control: the accepted values are not refused, so the loop above
    # cannot be satisfied by a handler that rejects everything.
    for ok in ("", "&view=light", "&view=full"):
        r = client.get(f"/api/reply-cards?status=waiting{ok}",
                       headers=_auth(owner_token))
        assert r.status_code == 200, (ok, r.text)


def test_view_is_not_advertised_to_agents(client, owner_token):
    """`view` is DELIBERATELY absent from the list_reply_cards MCP tool. The
    LIGHT row is the agent-facing contract by owner ruling (T-3f31, 卡只需要
    title+決策); advertising a one-call way to pull whole panes of full cards
    into an agent's context would undo exactly what that ticket shrank. The
    agent path to a full card stays get_reply_card, one card at a time.

    Asserted against the LIVE tools/list, not the frozen file: what agents can
    discover is what the server actually serves."""
    r = client.post("/api/mcp", json={
        "jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {},
    }, headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    tools = {t["name"]: t for t in r.json()["result"]["tools"]}
    assert "list_reply_cards" in tools, sorted(tools)
    props = tools["list_reply_cards"]["inputSchema"].get("properties", {})
    assert "view" not in props, props
    # Anti-tautology: the tool DOES advertise its other query parameters, so a
    # missing `view` means withheld — not "this tool advertises nothing".
    assert "status" in props and "limit" in props, props


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
            "option_idxs": [1],
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
        assert full["options"] == [{"text": "AI pick", "ai_pick": True},
                                   {"text": "other", "ai_pick": False}]
        assert full["answer"]["option_idxs"] == [1]
        assert full["answer"]["text"] == "see the screenshot"
        atts = full["answer"]["attachments"]
        assert len(atts) == 1 and atts[0]["filename"] == "proof.png"
        blob = client.get(atts[0]["url"], headers=_auth(asker.token))
        assert blob.status_code == 200
        assert blob.content == base64.b64decode(_PNG_B64)


# ── expired (T-1aa4): the terminal exit that is NOT an answer ──────────────
# Not a governance-only verb any more: T-1b88 (owner 2026-08-07, card
# rc-3ff94b116970) opened it to the card's own author as well. These cases press
# it as the owner (still allowed); the per-caller matrix lives in
# test_auth_matrix.py.


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
        client, owner_token, card["id"], {"option_idxs": [0]}
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
    assert _answer(client, owner_token, card["id"], {"option_idxs": [0]}).status_code == 200
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
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = client.get(f"/api/tasks/{task_id}", headers=h_agent).json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): report the step in_progress so the task
    # derives in_progress — a card can only bind an in_progress task.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h_agent,
    ).status_code == 200
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "go?", "options": [{"text": "go"}, {"text": "hold"}],
              "linked_task": {"task_id": task_id, "step_id": step_id}},
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


def test_closing_a_task_retires_its_waiting_card(client, owner_token, asker):
    """T-4166: a card must not outlive the task it waits on. The owner
    terminates a task under a still-waiting card; the SERVER retires the card
    (expired) in the same breath, so it leaves the 等我回覆 pane instead of
    sitting there unanswerable — answering it is 409 either way (T-f571), which
    is exactly why it must not still be listed. The closed task stays closed."""
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
    # submit_plan answers with a bounded receipt (T-a98d); read the rows back.
    step_id = client.get(f"/api/tasks/{task_id}", headers=h_agent).json()["steps"][0]["id"]
    # Task status is DERIVED (T-9ca5): lift the task to in_progress via the step
    # report so the card can bind.
    assert client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json={"status": "in_progress"}, headers=h_agent,
    ).status_code == 200
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "go?", "options": [{"text": "go"}],
              "linked_task": {"task_id": task_id, "step_id": step_id}},
        headers=h_agent,
    )
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]
    # The owner terminates the task under the still-waiting card → orphan.
    assert client.post(
        f"/api/tasks/{task_id}/terminate", headers=_auth(owner_token)
    ).status_code == 200

    # The close swept it: expired, off the waiting pane, unanswerable.
    assert _get_card(client, owner_token, card_id)["status"] == "expired"
    waiting = client.get("/api/reply-cards?status=waiting",
                         headers=_auth(owner_token)).json()
    assert card_id not in [c["id"] for c in waiting]
    r = _answer(client, owner_token, card_id, {"option_idxs": [0]})
    assert r.status_code == 409, r.text
    assert "expired" in r.json()["error"]["message"], r.text
    # A second expire is refused (terminal, no re-close) …
    assert _expire(client, owner_token, card_id).status_code == 409
    # … and the terminated task was never re-touched by the sweep.
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
