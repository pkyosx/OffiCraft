"""T-3809 — the role journal's three blocks, seen from ANOTHER role's identity.

Black-box, like everything else in this directory: every fact below is a real
HTTP status code and a real response body from the target under test, never a
config value and never a reading of the server source.

The behaviour pinned here (T-3809 node 5, `ts-89bc8e76657a`):
  * from ANOTHER role's member identity, read Duty / Insight / Learning once
    each — all three are READABLE (owner ruling 2026-08-02, `rc-dc171587220c`,
    option 1, verbatim 「包含 Insight：這一輪不關任何讀取」);
  * the SAME identity writes Insight once — refused 403, and the message is
    verbatim `an agent may only write its own role's insight`. The wording is
    the assertion, not decoration: `lessonsWriteAuthz` hard-codes the word
    `lessons` into its own 403, so an implementation that reuses it answers a
    refused insight write by sending the reader to the wrong document;
  * then the role's OWN agent writes once — succeeds.

🔴 READ WHAT THESE ASSERTIONS ARE WORTH, because two of them are worth less
than they look. **Cross-role READ of Duty and of Learning already held before
T-3809** — those two cases pin the owner's ruling in place, they are NOT an
achievement of this ticket, and an implementation that deleted every line of
insight code would still pass them. The discrimination in this file lives in
the Insight cases: the cross-role 200 (a narrower read floor goes red), the two
verbatim 403s (the lessons wording goes red), and the no-trace check (a 403
that nevertheless wrote goes red). If a future change makes this file's Duty or
Learning rows fail, the thing that broke is the ruling, not this ticket.

`OC_T3809_INSIGHT_EVIDENCE=<path>` optionally appends one JSON line per request
(status + body) for a run that has to hand over its raw wire evidence. Unset —
the normal case, including CI — nothing is written anywhere.
"""

from __future__ import annotations

import json
import os
import pathlib
import uuid

import httpx
import pytest

from conftest import _auth, hire_member, mint_member_token

FORBIDDEN_MSG = "an agent may only write its own role's insight"


def _log(step: str, method: str, path: str, identity: str, r: httpx.Response):
    """Opt-in raw-wire journal. No env var → no file, no directory, no side effect."""
    dest = os.environ.get("OC_T3809_INSIGHT_EVIDENCE", "")
    if not dest:
        return
    body = r.text
    if len(body) > 2000:
        body = body[:2000] + "…<truncated>"
    path_out = pathlib.Path(dest)
    path_out.parent.mkdir(parents=True, exist_ok=True)
    with path_out.open("a", encoding="utf-8") as fh:
        fh.write(
            json.dumps(
                {
                    "step": step,
                    "identity": identity,
                    "request": f"{method} {path}",
                    "status": r.status_code,
                    "body": body,
                },
                ensure_ascii=False,
            )
            + "\n"
        )


@pytest.fixture(scope="module")
def subject(client: httpx.Client, owner_token: str):
    """A real role R with real Duty + real Learning, plus R's own agent."""
    tag = uuid.uuid4().hex[:8]
    r = client.post(
        "/api/roles", json={"name": f"T3809 Subject {tag}"}, headers=_auth(owner_token)
    )
    assert r.status_code == 200, r.text
    role_key = r.json()["role"]["key"]

    duty = f"DUTY-{tag}: this role ships the widget."
    r = client.post(
        f"/api/roles/{role_key}",
        json={"definition_md": duty},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text

    learning = f"LEARNING-{tag}: the widget build needs node 20."
    r = client.post(
        f"/api/lessons/{role_key}/general",
        json={"text": learning},
        headers=_auth(owner_token),
    )
    assert r.status_code == 200, r.text

    member_id = hire_member(client, owner_token, f"t3809-own-{tag}", role_key)
    own_token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return {
        "role_key": role_key,
        "duty": duty,
        "learning": learning,
        "own_token": own_token,
        "member_id": member_id,
    }


# ── 1. another role's member identity READS all three ────────────────────────


def test_other_role_agent_reads_duty(client, agent_b, subject):
    """PRE-EXISTING behaviour, pinned deliberately — see the module docstring."""
    r = client.get(f"/api/roles/{subject['role_key']}", headers=_auth(agent_b.token))
    _log("read-duty", "GET", f"/api/roles/{subject['role_key']}", "agent_other", r)
    assert r.status_code == 200, r.text
    assert subject["duty"] in r.text


def test_other_role_agent_reads_learning(client, agent_b, subject):
    """PRE-EXISTING behaviour, pinned deliberately — see the module docstring."""
    path = f"/api/lessons/{subject['role_key']}/general"
    r = client.get(path, headers=_auth(agent_b.token))
    _log("read-learning", "GET", path, "agent_other", r)
    assert r.status_code == 200, r.text
    assert subject["learning"] in r.json()["text"]


def test_other_role_agent_reads_insight(client, agent_b, subject):
    path = f"/api/insight/{subject['role_key']}"
    r = client.get(path, headers=_auth(agent_b.token))
    _log("read-insight", "GET", path, "agent_other", r)
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["role_key"] == subject["role_key"]
    # Ruling 3 (zero automatic split): a freshly migrated role's insight is
    # empty, and it is readable while empty — "readable" is the assertion, the
    # emptiness is node 6's.
    assert body["text"] == ""


# ── 2. the SAME identity WRITES insight → 403, verbatim message ──────────────


def test_other_role_agent_write_insight_is_403_verbatim(client, agent_b, subject):
    path = f"/api/insight/{subject['role_key']}"
    r = client.post(
        path, json={"text": "poison from another role"}, headers=_auth(agent_b.token)
    )
    _log("write-insight-cross-role", "POST", path, "agent_other", r)
    assert r.status_code == 403, r.text
    assert r.json()["error"]["message"] == FORBIDDEN_MSG


def test_other_role_agent_patch_insight_is_403_verbatim(client, agent_b, subject):
    path = f"/api/insight/{subject['role_key']}/patch"
    r = client.post(
        path,
        json={"edits": [{"old": "", "new": "poison from another role"}]},
        headers=_auth(agent_b.token),
    )
    _log("patch-insight-cross-role", "POST", path, "agent_other", r)
    assert r.status_code == 403, r.text
    assert r.json()["error"]["message"] == FORBIDDEN_MSG


def test_refused_write_left_no_trace(client, agent_b, subject):
    """A 403 that still wrote would satisfy the status assertion above."""
    path = f"/api/insight/{subject['role_key']}"
    r = client.get(path, headers=_auth(agent_b.token))
    _log("read-insight-after-403", "GET", path, "agent_other", r)
    assert r.status_code == 200, r.text
    assert r.json()["text"] == ""


# ── 3. the role's OWN agent writes → succeeds ────────────────────────────────


def test_own_agent_write_insight_succeeds(client, subject):
    path = f"/api/insight/{subject['role_key']}"
    text = "INSIGHT: prefer a slow correct split to a fast wrong one."
    r = client.post(path, json={"text": text}, headers=_auth(subject["own_token"]))
    _log("write-insight-own-role", "POST", path, "agent_self", r)
    assert r.status_code == 200, r.text
    assert r.json()["text"] == text

    r2 = client.get(path, headers=_auth(subject["own_token"]))
    _log("read-back-insight-own-role", "GET", path, "agent_self", r2)
    assert r2.status_code == 200, r2.text
    assert r2.json()["text"] == text


def test_duty_and_learning_untouched_by_insight_write(client, agent_b, subject):
    r = client.get(f"/api/roles/{subject['role_key']}", headers=_auth(agent_b.token))
    _log("read-duty-after", "GET", f"/api/roles/{subject['role_key']}", "agent_other", r)
    assert r.status_code == 200 and subject["duty"] in r.text
    path = f"/api/lessons/{subject['role_key']}/general"
    r = client.get(path, headers=_auth(agent_b.token))
    _log("read-learning-after", "GET", path, "agent_other", r)
    assert r.status_code == 200 and subject["learning"] in r.json()["text"]


# ── 4. the THIRD write face: RESTORE from a retained revision ────────────────
#
# The two 403 cases above cover replace and patch. There is a third way to put
# text into a role's insight doc, and it lives in a DIFFERENT file behind a
# DIFFERENT switch: POST /api/document-history/insight/{role}/{id}/restore ends
# up in putInsightOn just as surely as replace_insight does. Its guard is one
# `write && !insightWriteAuthz(...)` cell in documentHistoryAllowed, and a
# post-land review measured what happens when that cell is deleted: the whole go
# suite and the whole conformance suite stayed GREEN while another role's agent
# rewrote the victim's Insight from V2 back to V1 (403 → 200). Nothing in the
# build spoke for it. This is that missing voice.
#
# The refusal alone would not be worth much — an endpoint that 403s at everyone
# satisfies it. The positive control (the role's OWN agent restores the same
# revision, by the same id, and the text really moves) is what makes the refusal
# mean "this caller was refused" rather than "this path is broken".


def test_other_role_agent_cannot_restore_victim_insight(client, agent_b, subject):
    doc = f"/api/insight/{subject['role_key']}"
    history = f"/api/document-history/insight/{subject['role_key']}"
    own = _auth(subject["own_token"])
    v1 = "INSIGHT V1: retired judgement — the version an attacker would put back."
    v2 = "INSIGHT V2: current judgement."

    for text in (v1, v2):
        r = client.post(doc, json={"text": text}, headers=own)
        _log("write-insight-own-role", "POST", doc, "agent_self", r)
        assert r.status_code == 200, r.text

    # Reading the retained versions is open to every authenticated identity
    # (ruling rc-dc171587220c — `write &&` short-circuits the guard), so the
    # attacker gets the revision id for free. That is not the hole; the hole
    # would be being allowed to USE it.
    r = client.get(history, headers=_auth(agent_b.token))
    _log("list-insight-history", "GET", history, "agent_other", r)
    assert r.status_code == 200, r.text
    # The listing names the revisions but carries no prose (T-1170), so finding
    # the one holding V1 means fetching each body by id — which is itself the
    # read the ruling leaves open, on the same identity.
    v1_ids = []
    for row in r.json():
        body = client.get(f"{history}/{row['id']}", headers=_auth(agent_b.token))
        _log("get-insight-version", "GET", f"{history}/{row['id']}", "agent_other", body)
        assert body.status_code == 200, body.text
        if body.json()["content"].get("text") == v1:
            v1_ids.append(row["id"])
    assert v1_ids, f"no retained revision holding V1 — nothing to try to restore: {r.text}"
    version_id = v1_ids[0]

    restore = f"{history}/{version_id}/restore"
    r = client.post(restore, headers=_auth(agent_b.token))
    _log("restore-insight-cross-role", "POST", restore, "agent_other", r)
    assert r.status_code == 403, (
        f"cross-role restore was NOT refused: {r.status_code} {r.text} — another "
        "role's agent just rewrote this role's Insight through the history face"
    )
    assert r.json()["error"]["message"] == FORBIDDEN_MSG

    # A 403 that nevertheless wrote would satisfy the status assertion above.
    r = client.get(doc, headers=_auth(agent_b.token))
    _log("read-insight-after-refused-restore", "GET", doc, "agent_other", r)
    assert r.status_code == 200, r.text
    assert r.json()["text"] == v2

    # Positive control: same revision, same URL, the role's OWN agent — 200, and
    # the document really moves. Without this the refusal above could be a broken
    # route rather than an enforced boundary.
    r = client.post(restore, headers=own)
    _log("restore-insight-own-role", "POST", restore, "agent_self", r)
    assert r.status_code == 200, r.text
    r = client.get(doc, headers=own)
    assert r.status_code == 200, r.text
    assert r.json()["text"] == v1
