"""Task face — the M3 任務系統 state machine, gate↔reply-card weld, dedupe,
terminal guards, and the manual lifecycle.

The auth matrix pins WHO may call the twenty routes and test_rest_happy pins
the happy shapes; this file pins the BEHAVIOUR the M3 contract promises:

  * the full loop: create (ad-hoc) → plan → drive steps → arm a gate (the
    step's reply card opens, rides chat, links back) → the owner answers via
    the EXISTING reply-card route → the SERVER restores the step/task to
    in_progress (T-68b7: waiting_owner is a card-lifecycle hold, entered on
    open and left on answer — the server never advances the WORK forward, only
    releases the hold; this supersedes ruling H4) → the agent finishes steps,
    reports done → closed_ts stamps, badge drops;
  * dedupe is a 200, never an error: a same-key create against an OPEN typed
    task answers the EXISTING task + deduped:true; a DIFFERENT key mints
    fresh; a TERMINAL twin never blocks a reopen (rulings H1/H2);
  * terminal states are walls: every later agent push (status / plan / deps /
    step / gate) and a second terminate are flat 409s;
  * manuals: create / partial edit / agent learnings write-back round-trip;
    delete is refused (409) while the type has open tasks and passes once
    they close; the deleted manual reads 404;
  * manual authorship split (owner ruling 2026-07-13): agents CREATE manuals
    and edit the CONTENT fields (purpose / fields / sop_md / learnings) —
    also via the MCP tools create_task_manual / update_task_manual — while
    the ASSIGNEE face stays GOVERNANCE — floor admin_agent since T-6020, so a
    PLAIN agent supplying `assignee` on create or edit is a flat 403 — and
    delete is likewise admin_agent;
  * the worker claim faces reachable black-box: a plain member 404s, a warden
    (below the agent floor) 403s — the positive claim needs the Phase 2
    scheduler and is pinned in the server unit tests (api_tasks_test.go).

DEGRADED (honest): no black-box path mints an outsource worker (the Phase 2
assignment scheduler is the only minting seam), so worker claim/release and
the outsource panel's populated face are pinned server-side only
(api_tasks_test.go); this file pins the panel's empty-list shape implicitly
through test_rest_happy.
"""

from __future__ import annotations

import hashlib
import uuid

import pytest

from conftest import AgentIdentity, hire_member, mint_member_token


def _auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


@pytest.fixture(scope="module")
def executor(client, owner_token) -> AgentIdentity:
    """This module's OWN executing agent (single-session rule)."""
    member_id = hire_member(client, owner_token, "conf-task-executor")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    return AgentIdentity(member_id=member_id, token=token, role_key="")


@pytest.fixture(scope="module")
def machine(fresh_machine) -> str:
    """This module's OWN onboarded machine. Every placement face — the manual's
    outsource assignee and the 發包 target alike — now demands a machine id that
    RESOLVES, so a placement fixture is no longer an arbitrary string."""
    return fresh_machine()


def _create_task(client, executor, title="conf task", **extra) -> dict:
    """Create a task; answer ``{"task": <the task, read back>}``.

    T-91: the create route no longer hands the whole task back beside
    ``deduped`` — it answers ``taskCreateResultDTO``, a receipt naming the
    ticket the call landed on. Two separate things happen here, and they are
    separate on purpose:

      * the RECEIPT's shape is pinned once, for every caller in this file, by
        KEY-SET EQUALITY. Asserting only that ``task_id`` is present would stay
        green if the route ever went back to serving the task, because a task
        envelope would carry a task_id too;
      * the task itself is then fetched from ``GET /api/tasks/{id}``, so every
        behavioural assertion downstream still reads a real task — from the
        read face, which is where a task has always been readable, and which
        this create never claimed to be.
    """
    body = {"title": title, "executor_member_id": executor.member_id, **extra}
    r = client.post("/api/tasks", json=body, headers=_auth(executor.token))
    assert r.status_code == 200, f"create failed: {r.status_code} {r.text}"
    receipt = r.json()
    # A fresh create carries exactly these three. A dedupe HIT adds the title
    # and status of the ticket the caller landed on but never opened; a typed
    # create may add non-blocking `warnings`. Nothing else may ride along.
    assert set(receipt) <= {
        "task_id", "task_no", "deduped", "title", "status", "warnings"
    } and {"task_id", "task_no", "deduped"} <= set(receipt), receipt
    if not receipt["deduped"]:
        # Owner ruling 2026-09-05: a fresh create does not echo the caller's
        # own sentence, nor the constant status this handler stamps.
        assert "title" not in receipt and "status" not in receipt, receipt
    g = client.get(
        f"/api/tasks/{receipt['task_id']}", headers=_auth(executor.token)
    )
    assert g.status_code == 200, f"create read-back failed: {g.status_code} {g.text}"
    return {"task": g.json(), "receipt": receipt}


# T-91: POST /api/reply-cards answers replyCardCreateReceiptDTO — the ids the
# caller cannot compute plus the attachment list the server resolved — not the
# card it opened.
_CARD_CREATE_RECEIPT_KEYS = {"id", "chat_message_id", "created_ts", "attachments"}


def _card_opened(client, token, r) -> dict:
    """Pin the create receipt's SHAPE, then answer the card from the read face.

    Key-set equality, not key presence: asserting only that ``id`` is on the
    response would stay green if the route went back to serving the whole
    card, because a card carries an id too. The card the callers below then
    assert against comes from ``GET /api/reply-cards/{id}`` — the face that
    has always been the way to read a card.
    """
    assert r.status_code == 200, f"open card failed: {r.status_code} {r.text}"
    receipt = r.json()
    assert set(receipt) == _CARD_CREATE_RECEIPT_KEYS, receipt
    g = client.get(f"/api/reply-cards/{receipt['id']}", headers=_auth(token))
    assert g.status_code == 200, f"card read-back failed: {g.status_code} {g.text}"
    card = g.json()
    assert card["id"] == receipt["id"], (card, receipt)
    assert card["chat_message_id"] == receipt["chat_message_id"], (card, receipt)
    assert card["created_ts"] == receipt["created_ts"], (card, receipt)
    return card


def _plan(client, token, task_id, steps):
    return client.post(
        f"/api/tasks/{task_id}/plan", json={"steps": steps},
        headers=_auth(token))


def _step_status(client, token, task_id, step_id, status, reason=None):
    body = {"status": status}
    if reason is not None:
        body["waiting_reason"] = reason
    return client.post(
        f"/api/tasks/{task_id}/steps/{step_id}/status",
        json=body, headers=_auth(token))


def _drive_in_progress(client, token, task_id, name="conf drive"):
    """Task status is DERIVED from steps (T-9ca5): plan one step and report it
    in_progress so the task derives to in_progress. Returns the step id."""
    planned = _plan_view(client, token, task_id, [{"name": name, "dod": "asserted"}])
    step_id = planned["steps"][0]["id"]
    assert _step_status(client, token, task_id, step_id,
                        "in_progress").status_code == 200
    return step_id


def _drive_done(client, token, task_id):
    """Derive a task to done (auto-closes): plan one step, report it done."""
    step_id = _drive_in_progress(client, token, task_id)
    assert _step_status(client, token, task_id, step_id,
                        "done").status_code == 200


def _get_task(client, token, task_id) -> dict:
    r = client.get(f"/api/tasks/{task_id}", headers=_auth(token))
    assert r.status_code == 200, r.text
    return r.json()


def _plan_view(client, token, task_id, steps) -> dict:
    """Submit a plan and return the STORED task view. submit_plan answers with a
    BOUNDED receipt (T-a98d), not the plan, so the step rows come back through
    get_task — the same path an agent takes. The receipt itself is pinned by
    test_submit_plan_answers_with_a_bounded_receipt."""
    r = _plan(client, token, task_id, steps)
    assert r.status_code == 200, f"plan failed: {r.status_code} {r.text}"
    return _get_task(client, token, task_id)


def _open_count(client, owner_token) -> int:
    r = client.get("/api/tasks/count", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    return r.json()["open"]


# T-91: create_task_manual and update_task_manual answer taskManualReceiptDTO.
# The two document triples are POINTERS: learnings_* is on the response only
# when this call wrote the learnings, sop_md_* only when it wrote the SOP, so
# "not written" is expressible and is not spelled 0.
_MANUAL_RECEIPT_ALWAYS = {"type_key", "updated_ts"}
_MANUAL_RECEIPT_OPTIONAL = {
    "learnings_chars", "learnings_cap_chars", "learnings_sha256",
    "sop_md_chars", "sop_md_cap_chars", "sop_md_sha256",
}


def _manual_written(client, token, r) -> dict:
    """Pin the write receipt's shape; answer the STORED manual.

    The manual no longer rides home, so what the callers below assert about
    purpose / fields / assignee is read off GET /api/task-manuals/{type_key}.
    That is strictly stronger than the echo was: an echo assembled from what
    the handler was about to write would look right even if nothing landed.
    """
    assert r.status_code == 200, f"manual write failed: {r.status_code} {r.text}"
    receipt = r.json()
    assert _MANUAL_RECEIPT_ALWAYS <= set(receipt), receipt
    assert set(receipt) <= _MANUAL_RECEIPT_ALWAYS | _MANUAL_RECEIPT_OPTIONAL, receipt
    g = client.get(f"/api/task-manuals/{receipt['type_key']}",
                   headers=_auth(token))
    assert g.status_code == 200, f"manual read-back: {g.status_code} {g.text}"
    return g.json()


def _new_manual(client, owner_token, **edits) -> str:
    type_key = f"conf-task-type-{uuid.uuid4().hex[:8]}"
    r = client.post(
        "/api/task-manuals", json={"type_key": type_key},
        headers=_auth(owner_token))
    assert r.status_code == 200, f"manual create failed: {r.status_code} {r.text}"
    # T-91: the create answers taskManualReceiptDTO — the manual it wrote plus
    # the documents THIS call wrote, and it wrote neither, so both optional
    # triples are absent. Key-set equality: asserting only that type_key is
    # present would stay green if the whole manual came back.
    assert set(r.json()) == {"type_key", "updated_ts"}, r.text
    assert r.json()["type_key"] == type_key, r.text
    if edits:
        r = client.post(
            f"/api/task-manuals/{type_key}", json=edits,
            headers=_auth(owner_token))
        assert r.status_code == 200, f"manual edit failed: {r.status_code} {r.text}"
    return type_key


# ── the full loop: create → plan → steps → gate → answer → resume → done ─────


def test_full_task_loop(client, owner_token, executor):
    created = _create_task(client, executor, title="release v2")
    assert created["receipt"]["deduped"] is False
    task = created["task"]
    assert task["status"] == "not_started"
    assert task["executor_kind"] == "member"
    # task_no IS the id (T-5291, owner 2026-08-25) — before that it was a
    # SEPARATELY DERIVED display value, not the id. The old shape is
    # deliberately not named: it changed more than once across this ticket's
    # rounds, and test_rest_happy.py named a different one, which is two
    # accounts of the same history in one directory.
    # Asserting equality rather than a prefix: the shape a client can rely on
    # is "the number names the task", and a prefix check would pass for
    # anything that merely kept the first two characters.
    assert task["task_no"] == task["id"]
    before = _open_count(client, owner_token)

    # Plan: a plain step, a gate, a closing step. The task status is DERIVED —
    # it lifts off not_started when the first step is reported in_progress below.
    view = _plan_view(client, executor.token, task["id"], [
        {"name": "prep", "dod": "branch green"},
        {"name": "owner approves", "dod": "explicit go", "is_gate": True},
        {"name": "ship", "dod": "deployed"},
    ])
    assert view["progress_total"] == 3 and view["progress_done"] == 0
    prep, gate, ship = view["steps"]
    # The announced (dashed) gate: is_gate with NO card yet.
    assert gate["is_gate"] is True and gate["reply_card_id"] == ""

    # Drive the first step to done.
    assert _step_status(client, executor.token, task["id"], prep["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], prep["id"],
                        "done").status_code == 200

    # Arm the gate: a real M2 reply card opens, bound both ways.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "ship release v2?",
              "options": [{"text": "ship it"}, {"text": "hold"}],
              "linked_task": {"task_id": task["id"], "step_id": gate["id"]}},
        headers=_auth(executor.token))
    card = _card_opened(client, owner_token, r)
    assert card["status"] == "waiting"
    assert card["task"]["id"] == task["id"]  # 請示 → 任務 jump data
    view = _get_task(client, owner_token, task["id"])
    assert view["status"] == "waiting_owner"
    gate_view = next(s for s in view["steps"] if s["id"] == gate["id"])
    assert gate_view["status"] == "waiting_owner"
    assert gate_view["reply_card_id"] == card["id"]
    # The card rides the chat stream (the M2 companion message).
    msgs = client.get(
        f"/api/chat?with={executor.member_id}&limit=-1",
        headers=_auth(owner_token)).json()["messages"]
    companion = {m["id"]: m for m in msgs}.get(card["chat_message_id"])
    assert companion and companion["meta"].get("reply_card_id") == card["id"]
    # And the waiting pane lists it WITH the task reference.
    pane = client.get("/api/reply-cards?status=waiting",
                      headers=_auth(owner_token)).json()
    mine = {c["id"]: c for c in pane}[card["id"]]
    assert mine["task"]["id"] == task["id"]

    # Before the answer, the agent CANNOT report the held step out of
    # waiting_owner — the card lifecycle owns the exit (T-68b7).
    assert _step_status(client, executor.token, task["id"], gate["id"],
                        "in_progress").status_code == 409, (
        "an agent may not bail the held step out of waiting_owner before the "
        "card is answered")

    # The owner answers through the EXISTING reply-card route…
    r = client.post(f"/api/reply-cards/{card['id']}/answer",
                    json={"option_idxs": [0]}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    # …and the SERVER restores the held step and the task to in_progress
    # (T-68b7 "答卡→回前態" — supersedes H4's "answering moves nothing").
    view = _get_task(client, owner_token, task["id"])
    assert view["status"] == "in_progress", (
        "answering a gate card must restore the task to in_progress")
    gate_view = next(s for s in view["steps"] if s["id"] == gate["id"])
    assert gate_view["status"] == "in_progress", (
        "the answered gate step must be restored to in_progress")
    # The surviving half of H4 (explicit): answering releases the HOLD but never
    # advances the WORK — the gate step is NOT auto-completed; the agent reports
    # done itself below.
    assert gate_view["status"] != "done", (
        "answering must never auto-complete the gate step (H4's surviving half)")

    # The agent finishes the work itself (the surviving half of H4): the gate
    # step advances to done, then the closing step, then the task.
    assert _step_status(client, executor.token, task["id"], gate["id"],
                        "done").status_code == 200
    assert _step_status(client, executor.token, task["id"], ship["id"],
                        "in_progress").status_code == 200
    # Reporting the LAST step done auto-derives the task to done and closes it
    # (T-9ca5 — done is no longer an agent task-status report).
    r = _step_status(client, executor.token, task["id"], ship["id"], "done")
    assert r.status_code == 200, r.text
    final = r.json()
    assert final["step_status"] == "done"
    assert final["task_status"] == "done"
    assert final["closed_ts"]
    assert final["progress_done"] == 3 and final["progress_total"] == 3
    # The badge counts open tasks only — the finished loop dropped off.
    assert _open_count(client, owner_token) == before - 1


def test_linked_task_arms_a_plain_non_gate_step(client, owner_token, executor):
    """A linked_task naming a plain (is_gate=false) not-done step is a legitimate
    ad-hoc 請示. It arms the step (waiting_owner + bound card, task follows)
    WITHOUT flipping is_gate."""
    task = _create_task(client, executor, title="ad-hoc ask")["task"]
    planned = _plan_view(client, executor.token, task["id"],
                         [{"name": "build", "dod": "compiles"}])
    step = planned["steps"][0]
    assert step["is_gate"] is False
    # Report the step in_progress so the task derives in_progress — a gate can
    # only arm on an in_progress (or waiting_owner) task.
    assert _step_status(client, executor.token, task["id"], step["id"],
                        "in_progress").status_code == 200

    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "which cloud?",
              "linked_task": {"task_id": task["id"], "step_id": step["id"]},
              "options": [{"text": "aws"}, {"text": "gcp"}]},
        headers=_auth(executor.token))
    card = _card_opened(client, owner_token, r)
    assert card["status"] == "waiting"
    assert card["task"]["id"] == task["id"]

    view = _get_task(client, owner_token, task["id"])
    assert view["status"] == "waiting_owner"
    armed = next(s for s in view["steps"] if s["id"] == step["id"])
    assert armed["status"] == "waiting_owner"
    assert armed["reply_card_id"] == card["id"]
    # is_gate is a plan-declared property — arming does not rewrite it.
    assert armed["is_gate"] is False


def test_list_is_light_detail_is_full(client, owner_token, executor):
    """GET /api/tasks (list_tasks) is the LIGHT projection — the collapsed
    card's fields, WITHOUT the heavy steps/description/inputs; GET
    /api/tasks/{id} (get_task) stays the FULL DTO. progress_done/total still
    ride the light list (counted, not derived from a steps payload)."""
    task = _create_task(
        client, executor, title="light list probe",
        description="a description the list must NOT carry")["task"]
    assert _plan(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
    ]).status_code == 200
    # Drive the first step to done (pending → in_progress → done) so the light
    # list's progress must read 1/2 — counted, never derived from a steps blob.
    first_step = _get_task(client, owner_token, task["id"])["steps"][0]["id"]
    assert _step_status(client, executor.token, task["id"], first_step,
                        "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], first_step,
                        "done").status_code == 200

    r = client.get("/api/tasks", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    item = next(t for t in r.json() if t["id"] == task["id"])
    # The light card fields are present …
    for k in ("id", "task_no", "title", "type_key", "status", "priority",
              "executor_kind", "executor_id", "creator_id", "waiting_reason",
              "dedupe_key", "created_ts", "updated_ts", "closed_ts", "deps",
              "progress_done", "progress_total"):
        assert k in item, f"light list item missing {k!r}: {item}"
    # … the heavy detail fields are NOT.
    for k in ("steps", "description", "inputs"):
        assert k not in item, f"light list item must not carry {k!r}: {item}"
    # Progress is still counted on the light list.
    assert item["progress_total"] == 2 and item["progress_done"] == 1

    # The detail endpoint stays FULL — steps/description/inputs all present.
    full = _get_task(client, owner_token, task["id"])
    assert len(full["steps"]) == 2
    assert full["description"] == "a description the list must NOT carry"
    assert "inputs" in full


# ── creator attribution (T-e987) ─────────────────────────────────────────────


def test_creator_id_is_caller_sub_and_rides_list_and_get(
        client, owner_token, executor):
    """creator_id is stamped from the verified token sub (§14 — never a request
    parameter) and rides both the light list (list_tasks) and the full DTO
    (get_task)."""
    task = _create_task(client, executor, title="who made me")["task"]
    # Stamped from the CALLER's sub — this module's executing agent.
    assert task["creator_id"] == executor.member_id

    # Rides the light list projection …
    r = client.get("/api/tasks", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    item = next(t for t in r.json() if t["id"] == task["id"])
    assert item["creator_id"] == executor.member_id

    # … and the full detail DTO.
    full = _get_task(client, owner_token, task["id"])
    assert full["creator_id"] == executor.member_id


# ── parallel (fork-join) plan shape: the submit_plan write gate ──────────────


def test_parallel_plan_roundtrip_and_flat_progress(client, owner_token, executor):
    """A legal fork-join plan lands whole: parallel_group round-trips, both
    lanes may run in_progress AT THE SAME TIME (the server never guards step
    order), and progress counts every lane as one flattened leaf."""
    task = _create_task(client, executor, title="parallel roundtrip")["task"]
    view = _plan_view(client, executor.token, task["id"], [
        {"name": "寫規格", "dod": "spec.md 落檔"},
        {"name": "寫數字 A", "dod": "a.txt 落檔", "parallel_group": "pg-1"},
        {"name": "寫數字 B", "dod": "b.txt 落檔", "parallel_group": "pg-1"},
        {"name": "加總（匯合）", "dod": "sum.txt = A+B"},
    ])
    assert [s["parallel_group"] for s in view["steps"]] == \
        ["", "pg-1", "pg-1", ""]
    assert view["progress_total"] == 4  # flattened: every lane is one leaf

    spec, lane_a, lane_b, _join = view["steps"]
    for sid in (spec["id"],):
        assert _step_status(client, executor.token, task["id"], sid,
                            "in_progress").status_code == 200
        assert _step_status(client, executor.token, task["id"], sid,
                            "done").status_code == 200
    # Fork: BOTH lanes in_progress simultaneously — no order guard.
    for lane in (lane_a, lane_b):
        assert _step_status(client, executor.token, task["id"], lane["id"],
                            "in_progress").status_code == 200
    view = _get_task(client, owner_token, task["id"])
    running = [s for s in view["steps"] if s["status"] == "in_progress"]
    assert len(running) == 2
    # Join precondition is the agent's discipline, not a server guard: lanes
    # report done independently and progress counts each one.
    for lane in (lane_a, lane_b):
        assert _step_status(client, executor.token, task["id"], lane["id"],
                            "done").status_code == 200
    view = _get_task(client, owner_token, task["id"])
    assert view["progress_done"] == 3 and view["progress_total"] == 4


def test_parallel_plan_shape_violations_are_400(client, owner_token, executor):
    """The three write-gate refusals: a gate inside a group, a split group,
    and a one-lane group. A refused plan writes NOTHING."""
    task = _create_task(client, executor, title="parallel guards")["task"]

    # 1. gate-in-group
    r = _plan(client, executor.token, task["id"], [
        {"name": "lane a", "dod": "d", "parallel_group": "pg"},
        {"name": "approve", "dod": "d", "parallel_group": "pg",
         "is_gate": True},
    ])
    assert r.status_code == 400, f"{r.status_code} {r.text}"

    # 2. split group (same key, not consecutive)
    r = _plan(client, executor.token, task["id"], [
        {"name": "lane a", "dod": "d", "parallel_group": "pg"},
        {"name": "solo", "dod": "d"},
        {"name": "lane b", "dod": "d", "parallel_group": "pg"},
    ])
    assert r.status_code == 400, f"{r.status_code} {r.text}"

    # 3. one-lane group (parallel means at least two)
    r = _plan(client, executor.token, task["id"], [
        {"name": "lonely", "dod": "d", "parallel_group": "pg"},
        {"name": "join", "dod": "d"},
    ])
    assert r.status_code == 400, f"{r.status_code} {r.text}"

    # Nothing half-landed; a plain sequential plan is untouched by the gate.
    assert _get_task(client, owner_token, task["id"])["steps"] == []
    r = _plan(client, executor.token, task["id"], [
        {"name": "one", "dod": "d"},
        {"name": "two", "dod": "d"},
    ])
    assert r.status_code == 200, r.text


def test_parallel_replan_checks_the_combined_timeline(
    client, owner_token, executor
):
    """Contiguity spans the kept done prefix on a replan: rewriting the
    still-pending lane right after the kept done lane is legal; re-using the
    group key further down (a split stage in the stored timeline) is 400."""
    task = _create_task(client, executor, title="parallel replan")["task"]
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "lane a", "dod": "d", "parallel_group": "pg"},
        {"name": "lane b", "dod": "d", "parallel_group": "pg"},
    ])
    lane_a = planned["steps"][0]
    assert _step_status(client, executor.token, task["id"], lane_a["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], lane_a["id"],
                        "done").status_code == 200

    # Refused: the fresh "pg" lanes sit apart from the kept done "pg" lane.
    r = _plan(client, executor.token, task["id"], [
        {"name": "solo", "dod": "d"},
        {"name": "lane b2", "dod": "d", "parallel_group": "pg"},
        {"name": "lane b3", "dod": "d", "parallel_group": "pg"},
    ])
    assert r.status_code == 400, f"{r.status_code} {r.text}"

    # Legal: the rewritten lane butts against the kept done lane.
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "lane b2", "dod": "d", "parallel_group": "pg"},
        {"name": "join", "dod": "d"},
    ])
    steps = planned["steps"]
    assert [s["parallel_group"] for s in steps] == ["pg", "pg", ""]
    assert steps[0]["status"] == "done"


def test_replan_relisting_done_steps_keeps_them_once(client, owner_token, executor):
    """Whole-replace-but-keep-done: re-listing an already-done node by name in a
    replan does NOT duplicate it (the 5→9 bug). The done node is preserved from
    the kept prefix; a fresh entry with the same name is that node, not a twin."""
    task = _create_task(client, executor, title="replan no-dup")["task"]
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
    ])
    one = planned["steps"][0]
    assert _step_status(client, executor.token, task["id"], one["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], one["id"],
                        "done").status_code == 200

    # Re-submit the WHOLE plan back — done "one" re-listed — plus a new step.
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
        {"name": "three", "dod": "d3"},
    ])
    steps = planned["steps"]
    names = [s["name"] for s in steps]
    assert names == ["one", "two", "three"], names
    assert names.count("one") == 1
    assert steps[0]["id"] == one["id"] and steps[0]["status"] == "done"
    assert planned["progress_done"] == 1 and planned["progress_total"] == 3


def test_replan_keeps_answered_card_step_as_superseded(
    client, owner_token, executor
):
    """T-1aea: a replan PRESERVES a step whose latest bound card was already
    answered — frozen to the `superseded` terminal state in its original slot
    ahead of the fresh plan (finished_ts stamped, card join intact) — while a
    step whose card still WAITS is replaced as before. Superseded counts
    toward neither progress side; re-arming it is a 409."""
    task = _create_task(client, executor, title="replan keeps answered")["task"]
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "ask direction", "dod": "owner answered"},
        {"name": "pending ask", "dod": "owner answered"},
    ])
    answered_step, waiting_step = planned["steps"]
    # Lift the task to in_progress (derived) so the gates below can arm.
    assert _step_status(client, executor.token, task["id"],
                        answered_step["id"], "in_progress").status_code == 200

    # Arm both; answer only the first.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "which way?",
              "options": [{"text": "a"}, {"text": "b"}],
              "linked_task": {"task_id": task["id"],
                              "step_id": answered_step["id"]}},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    answered_card = r.json()
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "later?", "options": [{"text": "a"}, {"text": "b"}],
              "linked_task": {"task_id": task["id"],
                              "step_id": waiting_step["id"]}},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    r = client.post(f"/api/reply-cards/{answered_card['id']}/answer",
                    json={"option_idxs": [0]}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text

    # Replan with entirely fresh names.
    body = _plan_view(client, executor.token, task["id"], [
        {"name": "build", "dod": "d"},
    ])
    steps = body["steps"]
    assert [s["name"] for s in steps] == ["ask direction", "build"], steps
    frozen = steps[0]
    assert frozen["id"] == answered_step["id"]
    assert frozen["status"] == "superseded"
    assert frozen["finished_ts"] > 0
    assert frozen["reply_card_id"] == answered_card["id"]
    assert frozen["reply_card_status"] == "answered"
    # The waiting-card step was replaced wholesale, and superseded history
    # counts toward neither progress side: 0/1 (just "build").
    assert body["progress_done"] == 0 and body["progress_total"] == 1

    # The frozen row is a wall: agent report out of it 409, re-arming 409.
    assert _step_status(client, executor.token, task["id"], frozen["id"],
                        "in_progress").status_code == 409
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "again?", "options": [{"text": "a"}, {"text": "b"}],
              "linked_task": {"task_id": task["id"], "step_id": frozen["id"]}},
        headers=_auth(executor.token))
    assert r.status_code == 409, f"{r.status_code} {r.text}"


# ── dedupe: 200 + deduped, terminal twins reopen ─────────────────────────────


def test_create_dedupes_open_tasks_and_reopens_after_terminal(
    client, owner_token, executor
):
    type_key = _new_manual(
        client, owner_token,
        fields=[{"name": "pr", "required": True, "is_key": True}],
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    first = _create_task(client, executor, title="review 9",
                         type_key=type_key, inputs={"pr": "9"})
    assert first["receipt"]["deduped"] is False

    # The same identity key against the OPEN task: the EXISTING task, 200.
    again = _create_task(client, executor, title="review 9 (again)",
                         type_key=type_key, inputs={"pr": "9"})
    assert again["receipt"]["deduped"] is True
    assert again["task"]["id"] == first["task"]["id"]
    # T-91: on a HIT — and only on a hit — the receipt also names the ticket
    # the caller landed on but never opened, because the caller cannot know
    # it. Key-set equality, not presence: a receipt that carried these two on
    # a FRESH create would be echoing the caller's own sentence back, which is
    # the thing this reshape removed.
    assert set(again["receipt"]) == {
        "task_id", "task_no", "deduped", "title", "status"
    }, again["receipt"]
    assert again["receipt"]["title"] == "review 9", again["receipt"]
    assert again["receipt"]["status"] == first["task"]["status"], again["receipt"]

    # A different key never collides.
    other = _create_task(client, executor, title="review 10",
                         type_key=type_key, inputs={"pr": "10"})
    assert other["receipt"]["deduped"] is False
    assert other["task"]["id"] != first["task"]["id"]

    # Close the first; the same key then mints FRESH (periodic reopen, H2).
    r = client.post(f"/api/tasks/{first['task']['id']}/terminate",
                    headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    reopened = _create_task(client, executor, title="review 9 (reopen)",
                            type_key=type_key, inputs={"pr": "9"})
    assert reopened["receipt"]["deduped"] is False
    assert reopened["task"]["id"] != first["task"]["id"]

    # A missing required input is refused.
    r = client.post(
        "/api/tasks", json={"title": "no key", "type_key": type_key},
        headers=_auth(executor.token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"


# ── terminal guards: a closed task is a wall ─────────────────────────────────


def test_terminated_task_refuses_every_agent_push(client, owner_token, executor):
    task = _create_task(client, executor)["task"]
    planned = _plan_view(client, executor.token, task["id"],
                         [{"name": "g", "dod": "d", "is_gate": True}])
    gate_id = planned["steps"][0]["id"]

    r = client.post(f"/api/tasks/{task['id']}/terminate",
                    headers=_auth(owner_token))
    assert r.status_code == 200 and r.json()["status"] == "terminated"

    h = _auth(executor.token)
    # (The task-level status report route is gone, T-8449 — priority stands in
    # as the plain agent push here.)
    assert client.post(f"/api/tasks/{task['id']}/priority",
                       json={"priority": "high"}, headers=h).status_code == 409
    assert _plan(client, executor.token, task["id"],
                 [{"name": "x", "dod": "y"}]).status_code == 409
    assert client.post(f"/api/tasks/{task['id']}/deps",
                       json={"blocked_by": []}, headers=h).status_code == 409
    assert _step_status(client, executor.token, task["id"], gate_id,
                        "in_progress").status_code == 409
    assert client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "s", "options": [{"text": "a"}],
              "linked_task": {"task_id": task["id"], "step_id": gate_id}},
        headers=h).status_code == 409
    # A second terminate is a 409 too (already closed).
    assert client.post(f"/api/tasks/{task['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 409
    # The steps froze as they stood (audit trail).
    view = _get_task(client, owner_token, task["id"])
    assert view["steps"][0]["status"] == "pending"


# ── the state machine's wire guards ──────────────────────────────────────────


def test_status_machine_wire_guards(client, owner_token, executor):
    task = _create_task(client, executor)["task"]
    # Task status is DERIVED from the steps (T-9ca5) and the task-level status
    # report route is GONE from the wire (T-8449) — its absence is pinned in
    # test_rest_happy (404) and the MCP catalog. The remaining wire guards live
    # on the STEP report below.

    # waiting_external moved DOWN to the STEP (T-9ca5): the task DERIVES it.
    # Entering it requires a one-line reason (422 without), and it clears on exit.
    step_id = _drive_in_progress(client, executor.token, task["id"], name="vendor")
    assert _step_status(client, executor.token, task["id"], step_id,
                        "waiting_external").status_code == 422
    r = _step_status(client, executor.token, task["id"], step_id,
                     "waiting_external", reason="waiting for vendor credentials")
    assert r.status_code == 200
    assert r.json()["step_status"] == "waiting_external"
    assert r.json()["task_status"] == "waiting_external"
    assert r.json()["waiting_reason"] == "waiting for vendor credentials"
    r = _step_status(client, executor.token, task["id"], step_id, "in_progress")
    assert r.status_code == 200 and r.json()["step_status"] == "in_progress"
    assert r.json()["task_status"] == "in_progress"
    assert r.json()["waiting_reason"] == ""


def test_waiting_owner_is_a_card_lifecycle_hold(client, owner_token, executor):
    """T-68b7: waiting_owner is bracketed entirely by the reply card. A manual
    STEP report of it is a 400 (the task-level report route is gone, T-8449);
    opening a card enters it; answering the card LEAVES it (the server restores
    in_progress) — and with two cards on one task, the task resumes only once
    the LAST is answered (SPEC §3.2)."""
    task = _create_task(client, executor, title="hold")["task"]
    planned = _plan_view(client, executor.token, task["id"], [
        {"name": "q1", "dod": "d1"},
        {"name": "q2", "dod": "d2"},
    ])
    s1, s2 = planned["steps"]

    # A manual STEP report of waiting_owner is a 400, not the machine's 409.
    assert _step_status(client, executor.token, task["id"], s1["id"],
                        "waiting_owner").status_code == 400

    # Lift the task to in_progress (derived) so the gates below can arm.
    assert _step_status(client, executor.token, task["id"], s1["id"],
                        "in_progress").status_code == 200

    # Arm two cards (one per step); a linked_task binds onto a waiting_owner task.
    def _arm(step, summary):
        r = client.post("/api/reply-cards",
                        json={"kind": "decision", "summary": summary,
                              "options": [{"text": "a"}, {"text": "b"}],
                              "linked_task": {"task_id": task["id"],
                                              "step_id": step["id"]}},
                        headers=_auth(executor.token))
        assert r.status_code == 200, r.text
        return r.json()
    c1 = _arm(s1, "q1?")
    c2 = _arm(s2, "q2?")
    assert _get_task(client, owner_token, task["id"])["status"] == "waiting_owner"

    # Answer the first: its step resumes, the task keeps waiting on c2.
    assert client.post(f"/api/reply-cards/{c1['id']}/answer",
                       json={"option_idxs": [0]},
                       headers=_auth(owner_token)).status_code == 200
    view = _get_task(client, owner_token, task["id"])
    assert view["status"] == "waiting_owner", "still one card waiting"
    assert next(s for s in view["steps"]
                if s["id"] == s1["id"])["status"] == "in_progress"

    # Answer the last: the task resumes too.
    assert client.post(f"/api/reply-cards/{c2['id']}/answer",
                       json={"option_idxs": [0]},
                       headers=_auth(owner_token)).status_code == 200
    assert _get_task(client, owner_token, task["id"])["status"] == "in_progress"


def test_reask_after_answer_re_enters_waiting(client, owner_token):
    """T-68b7: the answer released the hold (step→in_progress), but if it did
    NOT settle the question the agent opens a NEW card — that re-binds the same
    current step and the step/task re-enter waiting_owner. The card-lifecycle
    exit and re-entry compose cleanly."""
    member_id = hire_member(client, owner_token, "conf-task-reask")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf reask")["task"]
    view = _plan_view(client, token, task["id"], [{"name": "build", "dod": "built"}])
    step = view["steps"][0]
    assert _step_status(client, token, task["id"], step["id"],
                        "in_progress").status_code == 200

    def _plain_ask():
        r = client.post("/api/reply-cards",
                        json={"kind": "decision", "summary": "which?",
                              "options": [{"text": "a"}, {"text": "b"}],
                              "linked_task": {"task_id": task["id"],
                                              "step_id": step["id"]}},
                        headers=_auth(token))
        assert r.status_code == 200, r.text
        return r.json()

    first = _plain_ask()
    assert _get_task(client, owner_token, task["id"])["status"] == "waiting_owner"
    # Answer it → the hold releases, the step is back to in_progress.
    assert client.post(f"/api/reply-cards/{first['id']}/answer",
                       json={"option_idxs": [0]},
                       headers=_auth(owner_token)).status_code == 200
    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "in_progress"
    assert next(s for s in got["steps"]
                if s["id"] == step["id"])["status"] == "in_progress"

    # The answer did not settle it → a fresh ask re-binds the current step and
    # the step + task re-enter waiting_owner (the new card is bound, not the old).
    second = _plain_ask()
    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "waiting_owner"
    rebound = next(s for s in got["steps"] if s["id"] == step["id"])
    assert rebound["status"] == "waiting_owner"
    assert rebound["reply_card_id"] == second["id"]


def test_deps_are_markers_with_validation(client, owner_token, executor):
    a = _create_task(client, executor, title="blocked")["task"]
    b = _create_task(client, executor, title="blocker")["task"]
    h = _auth(executor.token)
    # Self-reference and unknown ids are 422.
    assert client.post(f"/api/tasks/{a['id']}/deps",
                       json={"blocked_by": [a["id"]]}, headers=h).status_code == 422
    assert client.post(f"/api/tasks/{a['id']}/deps",
                       json={"blocked_by": ["t-conf-missing"]},
                       headers=h).status_code == 422
    # A real dep lands — and the status NEVER moves (deps are display markers).
    # The task is in_progress by derivation (a reported step), never by a dep.
    _drive_in_progress(client, executor.token, a["id"])
    r = client.post(f"/api/tasks/{a['id']}/deps",
                    json={"blocked_by": [b["id"]]}, headers=h)
    assert r.status_code == 200
    assert r.json()["deps"] == [b["id"]]
    assert r.json()["status"] == "in_progress"
    # Wholesale replace: an empty list clears.
    r = client.post(f"/api/tasks/{a['id']}/deps",
                    json={"blocked_by": []}, headers=h)
    assert r.status_code == 200 and r.json()["deps"] == []


# ── T-a3e4: ask for the statuses you render, and let the server name the deps ─


def test_status_set_returns_exactly_those_states(client, owner_token, executor):
    """?statuses= (repeatable) answers the SET the cockpit ticked. The cockpit
    used to download every task and hide most of them in the browser (measured
    408,482 B vs 17,295 B on the live workshop), because ?open=true only removes
    the archive — it still ships every live task whatever the 狀態 filter says.

    Absolute counts are meaningless in this shared DB, so every assertion is
    about ids this test created."""
    live = _create_task(client, executor, title="a3e4 live")["task"]
    _drive_in_progress(client, executor.token, live["id"])
    closed = _create_task(client, executor, title="a3e4 closed")["task"]
    assert client.post(f"/api/tasks/{closed['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 200
    fresh = _create_task(client, executor, title="a3e4 fresh")["task"]

    def ids(**params) -> set[str]:
        r = client.get("/api/tasks", params=params, headers=_auth(owner_token))
        assert r.status_code == 200, r.text
        return {t["id"] for t in r.json()}

    got = ids(statuses=["in_progress", "terminated"])
    assert live["id"] in got and closed["id"] in got
    # The load-bearing half: a state that was NOT asked for is absent — a filter
    # that merely returned "fewer" rows would pass a count-only assertion.
    assert fresh["id"] not in got, "not_started leaked into an in_progress+terminated ask"

    only_fresh = ids(statuses=["not_started"])
    assert fresh["id"] in only_fresh
    assert live["id"] not in only_fresh and closed["id"] not in only_fresh

    # An unknown value is a 400 that NAMES it (dropping it silently would narrow
    # the answer without telling the caller).
    r = client.get("/api/tasks", params={"statuses": ["done", "nonsense"]},
                   headers=_auth(owner_token))
    assert r.status_code == 400, r.text
    assert "nonsense" in r.text

    # The FROZEN half: ?status= / ?open= / no params behave exactly as before,
    # including ?status=reassigning still being a 400 (the SET accepts that value,
    # the single param never did).
    assert live["id"] in ids(status="in_progress")
    assert closed["id"] not in ids(status="in_progress")
    assert client.get("/api/tasks", params={"status": "reassigning"},
                      headers=_auth(owner_token)).status_code == 400
    assert closed["id"] not in ids(open="true")
    assert closed["id"] in ids()
    # Filters compose (AND), so an impossible intersection is empty rather than
    # one param silently winning.
    both = ids(status="in_progress", statuses=["not_started"])
    assert live["id"] not in both and fresh["id"] not in both


def test_dep_tasks_names_a_blocker_the_status_filter_excluded(
        client, owner_token, executor):
    """Every light row carries dep_tasks: each dep resolved SERVER-SIDE to the
    task_no/title/status the 「等 <task_no> <標題>」 row prints. The point is the
    combination — the blocker is DONE and the request asked only for in_progress,
    so the blocker is not in the response, yet it is still named. Before this the
    client had to pull the whole closed population to name it (and did, on every
    task SSE delta)."""
    blocked = _create_task(client, executor, title="a3e4 blocked")["task"]
    blocker = _create_task(client, executor, title="a3e4 前置作業")["task"]
    plain = _create_task(client, executor, title="a3e4 沒有 dep")["task"]
    _drive_in_progress(client, executor.token, blocked["id"])
    _drive_in_progress(client, executor.token, plain["id"])
    _drive_done(client, executor.token, blocker["id"])
    assert client.post(f"/api/tasks/{blocked['id']}/deps",
                       json={"blocked_by": [blocker["id"]]},
                       headers=_auth(executor.token)).status_code == 200

    r = client.get("/api/tasks", params={"statuses": ["in_progress"]},
                   headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    rows = r.json()
    ids = {t["id"] for t in rows}
    assert blocked["id"] in ids
    assert blocker["id"] not in ids, "the done blocker must NOT be in the response"

    item = next(t for t in rows if t["id"] == blocked["id"])
    assert item["deps"] == [blocker["id"]]
    refs = item["dep_tasks"]
    assert len(refs) == 1, refs
    assert refs[0]["id"] == blocker["id"]
    assert refs[0]["task_no"] == blocker["task_no"]
    assert refs[0]["title"] == "a3e4 前置作業"
    assert refs[0]["status"] == "done"

    # A dep-less task serves an empty list, never null.
    plain_row = next(t for t in rows if t["id"] == plain["id"])
    assert plain_row["deps"] == [] and plain_row["dep_tasks"] == []


def test_task_count_carries_the_unfiltered_total(client, owner_token, executor):
    """count serves `total` (every task, terminal included) beside `open`. With
    the list answering a status SET, an empty list no longer distinguishes 「什麼
    都沒有」 from 「這幾個狀態裡沒有」, and 目前沒有任務 is a claim about the whole
    workshop — this is the cheap basis for it, instead of a widened list fetch."""
    task = _create_task(client, executor, title="a3e4 count")["task"]
    r = client.get("/api/tasks/count", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    before = r.json()
    assert before["total"] >= before["open"] >= 1
    assert client.post(f"/api/tasks/{task['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 200
    after = client.get("/api/tasks/count", headers=_auth(owner_token)).json()
    # Closing a task drops `open` but NOT `total` — the two are different numbers
    # on purpose, so a `total` aliased to `open` reddens here.
    assert after["open"] == before["open"] - 1
    assert after["total"] == before["total"]


def test_outsource_worker_carries_its_bound_task_facts(client, owner_token):
    """The 外包 panel's row facts (task_no / task_created_ts / task_type_key /
    task_type_name) ride the worker DTO. They used to be a CLIENT-side join
    against the unfiltered task list + the manuals list, re-pulled on every
    worker/task/chat delta just to order and label a handful of rows."""
    r = client.get("/api/outsource-workers", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    for w in r.json():
        # Shape, for whatever workers this run happens to have: the fields are
        # always present (never null), so a client can rely on them.
        for k in ("task_no", "task_created_ts", "task_type_key", "task_type_name"):
            assert k in w, f"worker DTO missing {k!r}: {w}"
        assert isinstance(w["task_no"], str)
        assert isinstance(w["task_created_ts"], (int, float))
        assert isinstance(w["task_type_key"], str)
        assert isinstance(w["task_type_name"], str)


# ── owner's task-card message box ────────────────────────────────────────────


def test_task_message_rides_chat_with_task_context(client, owner_token, executor):
    task = _create_task(client, executor, title="msg target")["task"]
    r = client.post(f"/api/tasks/{task['id']}/message",
                    json={"body": "how is it going?"},
                    headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    # T-91: the post answers chatPostReceiptDTO — the minted id and ts plus the
    # resolved attachments. The message it made is asserted on the chat stream,
    # which this test read anyway; the read is now the ANCHOR rather than a
    # corroboration, and `attachments` is pinned present-and-empty because a
    # key that comes and goes makes "no files" and "no such concept" the same
    # answer.
    receipt = r.json()
    assert set(receipt) == {"id", "ts", "attachments"}, receipt
    assert receipt["attachments"] == [], receipt
    # It IS an ordinary chat message — the stream lists it.
    msgs = client.get(f"/api/chat?with={executor.member_id}&limit=-1",
                      headers=_auth(owner_token)).json()["messages"]
    msg = next((m for m in msgs if m["id"] == receipt["id"]), None)
    assert msg is not None, "the task message is not on the chat stream"
    assert msg["from"] == "owner" and msg["to"] == executor.member_id
    assert msg["meta"]["task_id"] == task["id"]
    assert msg["meta"]["task_title"] == "msg target"
    # The visible body is prefixed with the task's display number so the
    # executor's message is self-identifying (owner 2026-07-14).
    assert msg["body"] == f"[{task['task_no']}] how is it going?"
    # An empty message is refused.
    assert client.post(f"/api/tasks/{task['id']}/message", json={},
                       headers=_auth(owner_token)).status_code == 400


# ── manuals: CRUD + the delete guard ─────────────────────────────────────────


def test_manual_outsource_assignee_machine_and_unlimited_copies(
    client, owner_token, machine
):
    """Assignee wire knobs (spec TaskManualDTO/TaskManualUpdateDTO): ``machine``
    is the machine id the type's workers boot on and ``copies`` >= 0 where 0 =
    無限 (unlimited per-type copies); both round-trip verbatim. ``machine`` must
    name a machine that EXISTS — the shape check refuses the retired "auto"
    spelling (400, it names no machine) and the resolve refuses a stale id (404),
    so a type can no longer be configured onto a placement no worker can reach.
    The other illegal knobs stay honest 400s."""
    type_key = _new_manual(client, owner_token)
    # machine + copies=0 (unlimited) round-trip through PATCH → GET.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"assignee": {"kind": "outsource", "model": "claude-opus-4-6",
                           "effort": "high", "copies": 0,
                           "machine": machine}},
        headers=_auth(owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    a = client.get(f"/api/task-manuals/{type_key}",
                   headers=_auth(owner_token)).json()["assignee"]
    assert a["copies"] == 0 and a["machine"] == machine, a
    # "auto" is not a machine — the shape check refuses it outright.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"assignee": {"kind": "outsource", "model": "m",
                           "copies": 2, "machine": "auto"}},
        headers=_auth(owner_token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    # A shaped-fine id that names no machine is the resolve's 404.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"assignee": {"kind": "outsource", "machine": "warden-mbp5"}},
        headers=_auth(owner_token))
    assert r.status_code == 404, f"{r.status_code} {r.text}"
    # Neither refusal wrote anything — the earlier placement still stands.
    a = client.get(f"/api/task-manuals/{type_key}",
                   headers=_auth(owner_token)).json()["assignee"]
    assert a["machine"] == machine and a["copies"] == 0, a
    # Illegal knobs are 400s: negative copies; blank / non-string machine.
    for bad in [{"kind": "outsource", "copies": -1},
                {"kind": "outsource", "machine": ""},
                {"kind": "outsource", "machine": 7}]:
        r = client.post(f"/api/task-manuals/{type_key}",
                        json={"assignee": bad}, headers=_auth(owner_token))
        assert r.status_code == 400, f"{bad}: {r.status_code} {r.text}"


def test_manual_create_assignee_machine_must_resolve(client, owner_token, machine):
    """The create face carries the SAME assignee rule as the edit face — a manual
    may not be born configured for a machine that is not installed."""
    def create(assignee):
        return client.post(
            "/api/task-manuals",
            json={"type_key": f"conf-task-type-{uuid.uuid4().hex[:8]}",
                  "assignee": assignee},
            headers=_auth(owner_token))

    assert create({"kind": "outsource", "machine": "auto"}).status_code == 400
    assert create({"kind": "outsource", "machine": "warden-mbp5"}).status_code == 404
    r = create({"kind": "outsource", "machine": machine})
    assert _manual_written(client, owner_token, r)["assignee"]["machine"] == machine


def test_settings_outsource_cap_unlimited(client, owner_token):
    """outsource_max_parallel now spans -1..20: -1 = 無限 (unlimited — no
    global cap) round-trips; below -1 stays a 422."""
    orig = client.get("/api/settings", headers=_auth(owner_token)).json()
    try:
        r = client.patch("/api/settings", json={"outsource_max_parallel": -1},
                         headers=_auth(owner_token))
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        assert r.json()["outsource_max_parallel"] == -1
        assert client.get("/api/settings", headers=_auth(owner_token)
                          ).json()["outsource_max_parallel"] == -1
        r = client.patch("/api/settings", json={"outsource_max_parallel": -2},
                         headers=_auth(owner_token))
        assert r.status_code == 422, f"{r.status_code} {r.text}"
    finally:
        # Restore the pre-test cap so this test never leaks state.
        client.patch(
            "/api/settings",
            json={"outsource_max_parallel": orig["outsource_max_parallel"]},
            headers=_auth(owner_token))


def test_manual_crud_and_delete_guard(client, owner_token, executor):
    type_key = _new_manual(
        client, owner_token,
        purpose="review incoming PRs",
        fields=[{"name": "pr", "required": True, "is_key": True},
                {"name": "repo", "required": False, "is_key": False}],
        sop_md="# SOP\n1. read the diff",
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    # The read face folds it all back.
    r = client.get(f"/api/task-manuals/{type_key}", headers=_auth(owner_token))
    assert r.status_code == 200
    manual = r.json()
    assert manual["purpose"] == "review incoming PRs"
    assert [f["name"] for f in manual["fields"]] == ["pr", "repo"]
    assert manual["assignee"]["kind"] == "member"
    # The list face carries it.
    listed = client.get("/api/task-manuals", headers=_auth(owner_token)).json()
    assert type_key in {m["type_key"] for m in listed}
    # The agent learnings write-back is whole-doc replace.
    r = client.post(f"/api/task-manuals/{type_key}/learnings",
                    json={"text": "always check CI first"},
                    headers=_auth(executor.token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    # T-91: taskLearningsWriteReceiptDTO — the doc does not ride home; its size
    # and digest do. Key-set equality: asserting only that type_key is present
    # would stay green if the whole manual came back.
    got = r.json()
    assert set(got) == {"type_key", "size_chars", "cap_chars", "sha256"}, got
    assert got["size_chars"] == len("always check CI first"), got
    assert got["sha256"] == hashlib.sha256(
        "always check CI first".encode("utf-8")).hexdigest(), got
    # …and it LANDED.
    assert client.get(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token)
                      ).json()["learnings"] == "always check CI first"
    # A duplicate create is a 409.
    assert client.post("/api/task-manuals", json={"type_key": type_key},
                       headers=_auth(owner_token)).status_code == 409

    # Delete guard: an OPEN task of the type blocks the delete…
    task = _create_task(client, executor, title="review 55",
                        type_key=type_key, inputs={"pr": "55"})["task"]
    r = client.delete(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token))
    assert r.status_code == 409, f"open task must block delete: {r.status_code}"
    # …and a CLOSED one does not.
    assert client.post(f"/api/tasks/{task['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 200
    r = client.delete(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token))
    assert r.status_code == 200 and r.json()["deleted"] is True
    assert client.get(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token)).status_code == 404


# ── manual authorship split (owner ruling 2026-07-13) ────────────────────────
# Agents author manual CONTENT; the assignee face + delete are governance
# (owner / admin_agent — T-6020), so a plain agent is a flat 403 on both.


def test_agent_creates_manual_and_edits_content_fields(client, executor):
    type_key = f"conf-agent-type-{uuid.uuid4().hex[:8]}"
    # An agent creates a manual…
    r = client.post("/api/task-manuals", json={"type_key": type_key},
                    headers=_auth(executor.token))
    assert _manual_written(client, executor.token, r)["assignee"] == {}
    # …and edits the content fields (purpose / fields / sop_md / learnings).
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"purpose": "triage inbound bug reports",
              "fields": [{"name": "report", "required": True, "is_key": True}],
              "sop_md": "# SOP\n1. reproduce",
              "learnings": "check the version first"},
        headers=_auth(executor.token))
    manual = _manual_written(client, executor.token, r)
    assert manual["purpose"] == "triage inbound bug reports"
    assert manual["fields"][0]["name"] == "report"
    assert manual["sop_md"].startswith("# SOP")
    assert manual["learnings"] == "check the version first"
    assert manual["assignee"] == {}, "content edit must not touch assignee"


def test_agent_supplied_assignee_is_403_on_create_and_edit(
    client, owner_token, executor
):
    """Sentinel: the T-6020 opening of the assignee gate stops at admin_agent —
    a PLAIN agent is still a flat 403 on both faces, and the refused call writes
    nothing."""
    assignee = {"kind": "member", "member_id": executor.member_id}
    # Create carrying assignee → 403, and the manual is NOT created.
    type_key = f"conf-gov-type-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals",
                    json={"type_key": type_key, "assignee": assignee},
                    headers=_auth(executor.token))
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    assert client.get(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token)).status_code == 404
    # Edit carrying assignee → 403, and the stored assignee is untouched.
    existing = _new_manual(client, owner_token)
    r = client.post(f"/api/task-manuals/{existing}",
                    json={"assignee": assignee},
                    headers=_auth(executor.token))
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    stored = client.get(f"/api/task-manuals/{existing}",
                        headers=_auth(owner_token)).json()
    assert stored["assignee"] == {}, "refused edit must write nothing"
    # The owner's assignee writes keep working on BOTH faces.
    r = client.post(f"/api/task-manuals/{existing}",
                    json={"assignee": assignee}, headers=_auth(owner_token))
    assert _manual_written(client, owner_token, r)["assignee"]["kind"] == "member"
    owner_type = f"conf-gov-type-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals",
                    json={"type_key": owner_type, "assignee": assignee},
                    headers=_auth(owner_token))
    assert _manual_written(client, owner_token, r)["assignee"]["kind"] == "member"


def test_admin_agent_may_set_a_manual_assignee(
    client, owner_token, executor, admin_agent
):
    """T-6020 (owner ruling 2026-07-26): the assignee gate's floor moved from
    owner to admin_agent, so the admin 助理 sets who executes a task type on
    BOTH faces — create and edit."""
    assignee = {"kind": "member", "member_id": executor.member_id}
    type_key = f"conf-adm-type-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals",
                    json={"type_key": type_key, "assignee": assignee},
                    headers=_auth(admin_agent.token))
    assert _manual_written(client, owner_token, r)["assignee"]["member_id"] == (
        executor.member_id), r.text
    existing = _new_manual(client, owner_token)
    r = client.post(f"/api/task-manuals/{existing}",
                    json={"assignee": assignee}, headers=_auth(admin_agent.token))
    assert _manual_written(client, owner_token, r)["assignee"]["member_id"] == (
        executor.member_id), r.text


def test_admin_agent_may_delete_a_manual(client, owner_token, admin_agent):
    """T-6020: DELETE /api/task-manuals/{type_key} dropped to the admin floor."""
    type_key = _new_manual(client, owner_token)
    r = client.delete(f"/api/task-manuals/{type_key}",
                      headers=_auth(admin_agent.token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert client.get(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token)).status_code == 404


def test_agent_delete_manual_is_403(client, owner_token, executor):
    type_key = _new_manual(client, owner_token)
    r = client.delete(f"/api/task-manuals/{type_key}",
                      headers=_auth(executor.token))
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    assert "principal not permitted" in r.text
    # Still there for the owner.
    assert client.get(f"/api/task-manuals/{type_key}",
                      headers=_auth(owner_token)).status_code == 200


def test_agent_manual_authorship_via_mcp_tools(client, executor):
    """The MCP face of the same capability: create_task_manual +
    update_task_manual ride the loopback with the AGENT's own token."""
    type_key = f"conf-mcp-type-{uuid.uuid4().hex[:8]}"

    def _call(tool, arguments):
        return client.post(
            "/api/mcp",
            json={"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                  "params": {"name": tool, "arguments": arguments}},
            headers=_auth(executor.token))

    r = _call("create_task_manual", {"type_key": type_key})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["result"].get("isError") is not True, r.text
    r = _call("update_task_manual",
              {"type_key": type_key, "purpose": "mcp-authored purpose"})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    result = r.json()["result"]
    assert result.get("isError") is not True, r.text
    # T-91: update_task_manual answers a receipt naming the manual it wrote —
    # the purpose the caller just sent does not ride home. Key-set equality
    # over the structured content, then the stored value read back off the
    # read face, so the claim "the edit LANDED" is not merely dropped.
    assert set(result["structuredContent"]) == {"type_key", "updated_ts"}, result
    assert result["structuredContent"]["type_key"] == type_key, result
    stored = client.get(f"/api/task-manuals/{type_key}",
                        headers=_auth(executor.token))
    assert stored.status_code == 200, f"{stored.status_code} {stored.text}"
    assert stored.json()["purpose"] == "mcp-authored purpose", stored.text
    # The governance boundary holds over MCP too: assignee → isError (403).
    r = _call("update_task_manual",
              {"type_key": type_key,
               "assignee": {"kind": "member", "member_id": executor.member_id}})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["result"]["isError"] is True, "assignee over MCP must 403"


# ── the worker-claim face is retired (T-4595) ────────────────────────────────


def test_get_my_task_route_is_gone(client, owner_token, executor, warden_agent):
    """GET /api/self/task is retired — no handler, no tool, no route.

    This used to assert the route's two refusal faces (a plain member with no
    worker row → 404, a warden below the agent floor → 403). Both are now the
    404 of a path that does not exist, so what is left to check is that the
    surface itself is gone — including for the identities that used to get a
    *different* answer here, which is what would betray a half-removal.

    Not vacuous, and the two controls below are what make the 404 mean
    something. The FIRST proves the client and the token still get a live answer
    out of a sibling /api/self/* route. The SECOND is the one that matters: the
    router answers 405 — not 404 — for a wrong METHOD on a path it still knows,
    so a 404 above is the router saying it does not know the PATH at all. Without
    that second probe a 404 would be consistent with "the route survived and only
    GET was dropped", which is exactly the half-removal this test exists to catch.
    """
    for who in (executor, warden_agent):
        r = client.get("/api/self/task", headers=_auth(who.token))
        assert r.status_code == 404, f"{r.status_code} {r.text}"
    # Control 1 — the fixture is live: a sibling /api/self/* route still answers.
    r = client.post("/api/self/waking", headers=_auth(executor.token), json={})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    # Control 2 — path-absence vs method-mismatch really are distinguishable here:
    # /api/self/waking is POST-only, and asking for it by GET is a 405, not a 404.
    r = client.get("/api/self/waking", headers=_auth(executor.token))
    assert r.status_code == 405, (
        f"{r.status_code} {r.text} — a wrong method on a KNOWN path must 405; "
        "if it 404s, the 404s asserted above no longer prove the path is gone"
    )
    # …and /api/self/task 404s under that other method too — a surviving route
    # with only its GET arm removed would answer 405 here.
    r = client.post("/api/self/task", headers=_auth(executor.token), json={})
    assert r.status_code == 404, f"{r.status_code} {r.text}"


def test_get_my_task_is_not_advertised_as_a_tool(client, executor):
    """The LIVE tools/list must not carry the retired tool (T-4595).

    Asserted against the running server's catalog rather than the frozen file:
    a stale committed catalog is exactly the drift the two generators exist to
    prevent, so checking the file would check the wrong authority.
    """
    r = client.post(
        "/api/mcp",
        headers=_auth(executor.token),
        json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"},
    )
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    names = {t["name"] for t in r.json()["result"]["tools"]}
    assert names, "tools/list came back empty — this check would pass vacuously"
    assert "get_my_task" not in names, "retired tool still advertised"
    # Positive control: the two verbs a worker now uses instead ARE on the list,
    # so a missing name means retired, not a broken lookup.
    assert {"get_task", "report_waking"} <= names, sorted(names)


# ── executor guard ───────────────────────────────────────────────────────────


def test_foreign_agent_cannot_drive_anothers_task(client, owner_token, executor):
    task = _create_task(client, executor)["task"]
    intruder_id = hire_member(client, owner_token, "conf-task-intruder")
    intruder = mint_member_token(client, owner_token, intruder_id, ttl_days=1)
    # (The task-level status report route is gone, T-8449 — the plan submit is
    # the executor-guarded agent push probed here.)
    r = _plan(client, intruder, task["id"], [{"name": "x", "dod": "y"}])
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    # The executor itself still passes the guard — it drives its task via the
    # step report (the task status is derived from there, T-9ca5).
    _drive_in_progress(client, executor.token, task["id"])


# ── set_task_priority (T-0786) ───────────────────────────────────────────────


def test_executor_freezes_and_unfreezes_symmetrically(client, owner_token, executor):
    """T-0786 + T-6020 (owner ruling 2026-07-26): the executor retunes their OWN
    task, AND may freeze it and take it back out again. The symmetry is the
    assertion — the pre-T-6020 wire refused both to everyone but the owner, and
    a one-sided opening would strand the executor holding a task only the owner
    could restart. The freezer is recorded in `frozen_by` so a frozen ticket
    still says whose 喊停 it was, and the field clears on the way out."""
    task = _create_task(client, executor)["task"]

    def _priority(token, value):
        return client.post(f"/api/tasks/{task['id']}/priority",
                           json={"priority": value}, headers=_auth(token))

    for value in ("high", "mid", "low"):
        r = _priority(executor.token, value)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        assert r.json()["priority"] == value
        assert r.json()["frozen_by"] == "", r.text
    # The executor freezes its own task — and is named as the freezer.
    r = _priority(executor.token, "frozen")
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["priority"] == "frozen", r.text
    assert r.json()["frozen_by"] == executor.member_id, (
        f"frozen_by must name the executor that froze it: {r.text}"
    )
    # … and unfreezes it itself (the symmetry), which clears the attribution.
    r = _priority(executor.token, "high")
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["frozen_by"] == "", r.text
    # The owner's own freeze is attributed to the owner, and the executor may
    # cross it — one shared knob, not two per-actor knobs.
    r = _priority(owner_token, "frozen")
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["frozen_by"] == "owner", r.text
    assert _priority(executor.token, "mid").status_code == 200


def test_admin_agent_freezes_a_task_it_does_not_execute(
    client, owner_token, executor, admin_agent
):
    """T-6020: the admin 助理 may freeze/unfreeze ANY task (it passes the
    executor guard by capability), and is named in `frozen_by` so the owner can
    tell an agent's 喊停 from their own."""
    task = _create_task(client, executor)["task"]

    def _priority(token, value):
        return client.post(f"/api/tasks/{task['id']}/priority",
                           json={"priority": value}, headers=_auth(token))

    r = _priority(admin_agent.token, "frozen")
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["frozen_by"] == admin_agent.member_id, r.text
    r = _priority(admin_agent.token, "low")
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["frozen_by"] == "", r.text


def test_foreign_agent_still_cannot_freeze(client, owner_token, executor):
    """T-6020 sentinel: opening `frozen` to {owner, admin_agent, executor} must
    NOT have opened it to every agent. A plain agent that executes nothing is
    refused on the freeze AND on the unfreeze of somebody else's freeze — and
    the refused calls change nothing."""
    task = _create_task(client, executor)["task"]
    intruder_id = hire_member(client, owner_token, "conf-freeze-intruder")
    intruder = mint_member_token(client, owner_token, intruder_id, ttl_days=1)

    def _priority(token, value):
        return client.post(f"/api/tasks/{task['id']}/priority",
                           json={"priority": value}, headers=_auth(token))

    r = _priority(intruder, "frozen")
    assert r.status_code == 403, f"foreign agent freeze: {r.status_code} {r.text}"
    assert _priority(executor.token, "frozen").status_code == 200
    r = _priority(intruder, "high")
    assert r.status_code == 403, f"foreign agent unfreeze: {r.status_code} {r.text}"
    r = client.get(f"/api/tasks/{task['id']}", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    assert r.json()["priority"] == "frozen", r.text
    assert r.json()["frozen_by"] == executor.member_id, (
        f"a refused call must not rewrite the attribution: {r.text}"
    )


def test_set_task_priority_via_mcp_loopback(client, executor):
    """The MCP face of the same capability: set_task_priority rides the
    loopback with the EXECUTOR's own token; get_task reads the change back;
    the frozen set succeeds and carries its attribution (T-6020)."""
    task = _create_task(client, executor)["task"]

    def _call(id_, tool, arguments):
        return client.post(
            "/api/mcp",
            json={"jsonrpc": "2.0", "id": id_, "method": "tools/call",
                  "params": {"name": tool, "arguments": arguments}},
            headers=_auth(executor.token))

    r = _call(1, "set_task_priority", {"task_id": task["id"], "priority": "high"})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    result = r.json()["result"]
    assert result.get("isError") is not True, r.text
    assert result["structuredContent"]["priority"] == "high"

    r = _call(2, "get_task", {"task_id": task["id"]})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["result"]["structuredContent"]["priority"] == "high"

    # T-6020: the executor freezing over MCP is now a SUCCESS, attributed.
    r = _call(3, "set_task_priority", {"task_id": task["id"], "priority": "frozen"})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    result = r.json()["result"]
    assert result.get("isError") is not True, r.text
    assert result["structuredContent"]["priority"] == "frozen", r.text
    assert result["structuredContent"]["frozen_by"] == executor.member_id, r.text


# ── §6.3 close-out report ────────────────────────────────────────────────────


def _closeout(client, token, task_id):
    return client.post(f"/api/tasks/{task_id}/closeout", headers=_auth(token))


# T-bb70: BOTH close-out exits (first report and idempotent repeat) answer with
# a bounded receipt, never the whole task. The key SET is pinned on purpose: a
# whole task carries closeout_reported too, so a field-presence assertion would
# stay green through exactly the regression this replaces.
CLOSEOUT_RECEIPT_KEYS = {
    "task_id", "task_status", "closeout_reported", "closeout_ts"}


def _assert_closeout_receipt(r, task_id, status):
    body = r.json()
    assert set(body) == CLOSEOUT_RECEIPT_KEYS, (
        f"close-out must answer a bounded receipt, got keys "
        f"{sorted(set(body) - CLOSEOUT_RECEIPT_KEYS)} extra / "
        f"{sorted(CLOSEOUT_RECEIPT_KEYS - set(body))} missing ({len(r.text)} chars)")
    assert body["task_id"] == task_id, r.text
    assert body["task_status"] == status, r.text
    assert body["closeout_reported"] is True, r.text
    assert body["closeout_ts"] > 0, r.text
    return body


def test_closeout_reports_after_terminal_and_is_idempotent(
    client, owner_token, executor
):
    task = _create_task(client, executor, title="conf closeout done")["task"]
    # An OPEN task has nothing to close out — flat 409.
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 409, f"{r.status_code} {r.text}"

    _drive_done(client, executor.token, task["id"])
    assert _get_task(client, executor.token, task["id"])[
        "closeout_reported"] is False

    # First report flips the flag and answers a bounded receipt.
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    first = _assert_closeout_receipt(r, task["id"], "done")

    # A repeat is a 200 no-op (idempotent — never a 409, never a re-flip), and
    # it answers the SAME bounded receipt, stamp included.
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert _assert_closeout_receipt(r, task["id"], "done") == first, r.text
    assert _get_task(client, executor.token, task["id"])[
        "closeout_reported"] is True

    # Unknown task → 404.
    r = _closeout(client, executor.token, "t-conf-missing")
    assert r.status_code == 404, f"{r.status_code} {r.text}"


def test_closeout_covers_terminated_tasks_too(client, owner_token, executor):
    task = _create_task(client, executor, title="conf closeout terminated")["task"]
    r = client.post(f"/api/tasks/{task['id']}/terminate",
                    headers=_auth(owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    # The executor of a TERMINATED task still owes (and can file) a close-out.
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    _assert_closeout_receipt(r, task["id"], "terminated")


def test_closeout_enforces_the_executor_guard(client, owner_token, executor):
    task = _create_task(client, executor, title="conf closeout guard")["task"]
    _drive_done(client, executor.token, task["id"])
    stranger_id = hire_member(client, owner_token, "conf-closeout-stranger")
    stranger = mint_member_token(client, owner_token, stranger_id, ttl_days=1)
    r = _closeout(client, stranger, task["id"])
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    # Admin capability (owner) passes — the §14 caller-identity convention.
    r = _closeout(client, owner_token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"


# ── §6.2 resume-summary task block ───────────────────────────────────────────


def _resume(client, token) -> dict:
    r = client.get("/api/resume-summary", headers=_auth(token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    return r.json()


def test_resume_summary_carries_the_callers_open_tasks_as_light_rows(
    client, owner_token
):
    # A DEDICATED agent — the module executor's tasks must stay out of the
    # caller-locked block (and vice versa).
    resumer_id = hire_member(client, owner_token, "conf-task-resumer")
    resumer = mint_member_token(client, owner_token, resumer_id, ttl_days=1)
    me = AgentIdentity(member_id=resumer_id, token=resumer, role_key="")

    # A CLOSED own task must not list.
    closed = _create_task(client, me, title="conf resume closed")["task"]
    _drive_done(client, resumer, closed["id"])

    # The live task: an executed step, the current step with a LONG DoD and an
    # armed gate. T-3f31 owner ruling (任務不該包含細節): NONE of that plan
    # detail may ride the wake snapshot — the row carries the current node's
    # id + NAME plus detail_chars (the size of the omitted plan text) instead.
    task = _create_task(client, me, title="conf resume live")["task"]
    long_dod = "驗" * 400
    plan = [
        {"name": "prep", "dod": "ready"},
        {"name": "build", "dod": long_dod},
        {"name": "approve", "dod": "owner said go", "is_gate": True},
        {"name": "ship", "dod": "deployed", "is_gate": True},
    ]
    planned = _plan_view(client, resumer, task["id"], plan)
    steps = planned["steps"]
    assert _step_status(client, resumer, task["id"], steps[0]["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, resumer, task["id"], steps[0]["id"],
                        "done").status_code == 200
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "conf resume gate",
              "options": [{"text": "go"}, {"text": "hold"}],
              "linked_task": {"task_id": task["id"], "step_id": steps[2]["id"]}},
        headers=_auth(resumer))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    card_id = r.json()["id"]

    snapshot = _resume(client, resumer)
    block = snapshot["tasks"]
    assert [t["id"] for t in block] == [task["id"]], block
    got = block[0]
    # Identity surface (light).
    assert got["task_no"] == task["task_no"]
    assert got["type_key"] == "" and got["title"] == "conf resume live"
    assert got["status"] == "waiting_owner"
    assert got["priority"] == task["priority"]
    # Executed-vs-pending boundary: prep done, build is the current node —
    # carried as id + NAME (the light row's 當前節點).
    assert got["progress_done"] == 1 and got["progress_total"] == 4
    assert got["current_step_id"] == steps[1]["id"]
    assert got["current_step_name"] == "build"
    # NO plan detail rides the row: no steps key, and the DoD text is absent
    # from the entire snapshot body.
    assert "steps" not in got, got
    assert long_dod[:10] not in str(snapshot)
    # detail_chars sizes the omitted plan text (Σ step name + DoD runes).
    want_chars = sum(len(s["name"]) + len(s["dod"]) for s in plan)
    assert got["detail_chars"] == want_chars, got

    # The overview folds the peek-then-decide sizes.
    ov = snapshot["overview"]
    assert ov["tasks_returned"] == 1 and ov["tasks_open_total"] == 1
    assert ov["tasks_detail_chars"] == want_chars
    assert ov["chat_count"] == len(snapshot["chat"])
    assert ov["cards_waiting"] == 1 and ov["cards_answered_recent"] == 0

    # The owner answers the gate card → the caller's card counts fold over.
    r = client.post(f"/api/reply-cards/{card_id}/answer",
                    json={"option_idxs": [0]}, headers=_auth(owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    ov = _resume(client, resumer)["overview"]
    assert ov["cards_waiting"] == 0 and ov["cards_answered_recent"] == 1

    # Identity lock: the OWNER's snapshot never carries this agent's tasks.
    owner_block = _resume(client, owner_token)["tasks"]
    assert task["id"] not in [t["id"] for t in owner_block]


def test_resume_summary_task_block_is_bounded(client, owner_token):
    resumer_id = hire_member(client, owner_token, "conf-task-resumer-cap")
    resumer = mint_member_token(client, owner_token, resumer_id, ttl_days=1)
    me = AgentIdentity(member_id=resumer_id, token=resumer, role_key="")
    ids = [
        _create_task(client, me, title=f"conf resume cap {i}")["task"]["id"]
        for i in range(7)
    ]
    # Touch the OLDEST task last — recency is by update, not creation. A
    # priority retune bumps updated_ts while keeping the task open.
    assert client.post(f"/api/tasks/{ids[0]}/priority",
                       json={"priority": "high"},
                       headers=_auth(resumer)).status_code == 200
    snapshot = _resume(client, resumer)
    block = snapshot["tasks"]
    assert len(block) == 5, [t["id"] for t in block]
    assert block[0]["id"] == ids[0], (ids, [t["id"] for t in block])
    # The overview reports the TRUE open total past the cap (peek signal for
    # list_tasks paging).
    ov = snapshot["overview"]
    assert ov["tasks_returned"] == 5 and ov["tasks_open_total"] == 7


# ── the ONE card-open entrance: linked_task (T-18) ───────────────────────────
# POST /api/reply-cards is the only way a card opens, and linked_task is
# REQUIRED: null (not about a task) or {task_id, step_id} (about this step).
# Nothing is inferred. These pin the wire behaviour, INCLUDING the sentences the
# refusals carry — on this ticket the message is the feature.


def test_create_reply_card_without_linked_task_names_both_legal_shapes(
    client, owner_token
):
    """An omitted linked_task is a 400 that spells out BOTH legal shapes.

    The old auto-binding failed SILENTLY: an asker who never thought about
    binding got a 200 and a card with no 等我回覆 hold. The replacement is only
    stronger than a written rule if the refusal tells you what to write, so the
    message is asserted, not just the status."""
    member_id = hire_member(client, owner_token, "conf-linked-required")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf linked required")["task"]
    view = _plan_view(client, token, task["id"], [
        {"name": "recon", "dod": "understood"},
    ])
    step = view["steps"][0]
    assert _step_status(client, token, task["id"], step["id"],
                        "in_progress").status_code == 200

    # A body that would have auto-bound perfectly before T-18 — one active task,
    # one running step. Still refused, because the caller never SAID anything.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "which way?", "options": [{"text": "AI pick"}]},
        headers=_auth(token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    msg = r.json()["error"]["message"]
    for want in ("linked_task", "linked_task=null", "task_id", "step_id", "等我回覆"):
        assert want in msg, (want, msg)

    # Nothing minted, nothing moved.
    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "in_progress"
    assert next(s for s in got["steps"]
                if s["id"] == step["id"])["reply_card_id"] == ""

    # Close out so this identity leaves no active task behind.
    assert _step_status(client, token, task["id"], step["id"],
                        "done").status_code == 200


def test_create_reply_card_with_task_id_but_no_step_id_is_refused(
    client, owner_token
):
    """The ORPHAN SHAPE stays unreachable. A card bound to a task but to no step
    places no waiting_owner hold, so the task marches to done underneath the ask
    and the owner's answer is refused 409 forever — the shape T-4166 spent a
    whole ticket removing from the old entrance. The new entrance must not hand
    it back, so this is a 400 and the message names what it costs."""
    member_id = hire_member(client, owner_token, "conf-linked-orphan")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf linked orphan")["task"]
    view = _plan_view(client, token, task["id"], [
        {"name": "recon", "dod": "understood"},
    ])
    step = view["steps"][0]
    assert _step_status(client, token, task["id"], step["id"],
                        "in_progress").status_code == 200

    base = {"kind": "decision", "summary": "which way?", "options": [{"text": "AI pick"}]}
    r = client.post("/api/reply-cards",
                    json={**base, "linked_task": {"task_id": task["id"]}},
                    headers=_auth(token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    msg = r.json()["error"]["message"]
    for want in ("step_id", "等我回覆", "linked_task=null"):
        assert want in msg, (want, msg)

    # An explicitly blank step_id is the same offence, not a way round it.
    r = client.post(
        "/api/reply-cards",
        json={**base, "linked_task": {"task_id": task["id"], "step_id": "  "}},
        headers=_auth(token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"

    # A step with no task is the mirror refusal.
    r = client.post("/api/reply-cards",
                    json={**base, "linked_task": {"step_id": step["id"]}},
                    headers=_auth(token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    assert "task_id" in r.json()["error"]["message"]

    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "in_progress"
    assert next(s for s in got["steps"]
                if s["id"] == step["id"])["reply_card_id"] == ""
    assert _step_status(client, token, task["id"], step["id"],
                        "done").status_code == 200


def test_create_reply_card_with_linked_task_arms_the_named_step(
    client, owner_token
):
    """{task_id, step_id} drives the waiting machine the retired open_gate route
    used to, and the pointer persists after the step finishes."""
    member_id = hire_member(client, owner_token, "conf-linked-bound")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf linked bound")["task"]
    view = _plan_view(client, token, task["id"], [
        {"name": "recon", "dod": "understood"},
        {"name": "build", "dod": "built"},
    ])
    build = view["steps"][1]
    assert _step_status(client, token, task["id"], build["id"],
                        "in_progress").status_code == 200

    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "which way?", "options": [{"text": "AI pick"}],
              "linked_task": {"task_id": task["id"], "step_id": build["id"]}},
        headers=_auth(token))
    card = _card_opened(client, owner_token, r)
    assert card["task"] and card["task"]["id"] == task["id"]

    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "waiting_owner"
    bound = next(s for s in got["steps"] if s["id"] == build["id"])
    assert bound["status"] == "waiting_owner"
    assert bound["reply_card_id"] == card["id"]
    # The untouched sibling never moves.
    other = next(s for s in got["steps"] if s["id"] == view["steps"][0]["id"])
    assert other["status"] == "pending" and other["reply_card_id"] == ""

    r = client.post(f"/api/reply-cards/{card['id']}/answer",
                    json={"option_idxs": [0]}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    resumed = _get_task(client, owner_token, task["id"])
    assert resumed["status"] == "in_progress", "answering restores the task"
    assert next(s for s in resumed["steps"]
                if s["id"] == build["id"])["status"] == "in_progress"
    assert _step_status(client, token, task["id"], build["id"],
                        "done").status_code == 200
    done_step = next(s for s in _get_task(client, owner_token, task["id"])["steps"]
                     if s["id"] == build["id"])
    assert done_step["reply_card_id"] == card["id"], (
        "the approval mark must persist after the step finishes")

    # Close out so this identity leaves no active task behind.
    recon = view["steps"][0]
    for status in ("in_progress", "done"):
        assert _step_status(client, token, task["id"], recon["id"],
                            status).status_code == 200
    assert _get_task(client, owner_token, task["id"])["status"] == "done"


def test_create_reply_card_with_null_linked_task_opens_a_plain_card(
    client, owner_token
):
    """null is a legal ANSWER, not a fallback — and it must work for an agent
    holding live work, otherwise "this ask is not about my task" would be
    unsayable for exactly the people who need to say it."""
    member_id = hire_member(client, owner_token, "conf-linked-null")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf linked null")["task"]
    view = _plan_view(client, token, task["id"], [
        {"name": "early", "dod": "d1"},
    ])
    early = view["steps"][0]
    assert _step_status(client, token, task["id"], early["id"],
                        "in_progress").status_code == 200

    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "unrelated", "options": [{"text": "AI pick"}],
              "linked_task": None},
        headers=_auth(token))
    card = _card_opened(client, owner_token, r)
    assert card["task"] is None, "linked_task=null must open a PLAIN card"
    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "in_progress", "an unbound card places no hold"
    assert next(s for s in got["steps"]
                if s["id"] == early["id"])["reply_card_id"] == ""
    assert client.post(f"/api/reply-cards/{card['id']}/answer",
                       json={"option_idxs": [0]},
                       headers=_auth(owner_token)).status_code == 200

    # The retired `bind` lever is refused outright (unknown field), not silently
    # dropped — it must not look like it still works.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "unrelated", "options": [{"text": "AI pick"}],
              "linked_task": None, "bind": "none"},
        headers=_auth(token))
    assert r.status_code == 422, f"{r.status_code} {r.text}"

    # Close out so this identity leaves no active task behind.
    assert _step_status(client, token, task["id"], early["id"],
                        "done").status_code == 200



# ── T-f3ae task quality gate ─────────────────────────────────────────────────
# submit_plan's DoD / non-empty-plan refusals; create_task's identity-key
# normalization + K1 mandatory-key check + undefined-field warnings; the
# manual-side K1 rule (is_key ⟹ required).


def test_submit_plan_rejects_empty_dod_and_empty_plan(client, owner_token, executor):
    task = _create_task(client, executor, title="quality gate")["task"]

    # A step with a blank DoD is refused.
    assert _plan(client, executor.token, task["id"], [
        {"name": "a", "dod": "real"},
        {"name": "b", "dod": ""},
    ]).status_code == 400
    # A step with a missing DoD key is refused.
    assert _plan(client, executor.token, task["id"], [
        {"name": "a", "dod": "real"},
        {"name": "b"},
    ]).status_code == 400
    # A zero-step plan (the 空殼 case) is refused.
    assert _plan(client, executor.token, task["id"], []).status_code == 400
    # A refused plan writes nothing.
    assert _get_task(client, owner_token, task["id"])["steps"] == []
    # A plan where every step has a DoD lands.
    r = _plan(client, executor.token, task["id"], [
        {"name": "a", "dod": "d1"},
        {"name": "b", "dod": "d2"},
    ])
    assert r.status_code == 200, r.text


def test_submit_plan_answers_with_a_bounded_receipt(client, owner_token, executor):
    """T-a98d: submit_plan no longer echoes the whole task (~80k characters for
    a plan the caller just wrote). It answers with the counters the caller could
    NOT know — how many rows the STORED timeline holds (kept history included)
    and where leaf progress landed — and nothing else."""
    task = _create_task(client, executor, title="plan receipt")["task"]
    view = _plan_view(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
    ])
    assert _step_status(client, executor.token, task["id"],
                        view["steps"][0]["id"], "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"],
                        view["steps"][0]["id"], "done").status_code == 200

    # The replan keeps the done step ahead of the fresh one, so the receipt
    # counts 2 rows for a one-step body — it describes the store, not the send.
    r = _plan(client, executor.token, task["id"], [{"name": "three", "dod": "d3"}])
    assert r.status_code == 200, r.text
    body = r.json()
    assert body == {"task_id": task["id"], "steps_total": 2,
                    "progress_done": 1, "progress_total": 2}, r.text


def test_create_task_dedupes_across_field_name_case(client, owner_token, executor):
    # Manual key field is "PR Link"; callers send differently-cased/spaced keys.
    type_key = _new_manual(
        client, owner_token,
        fields=[{"name": "PR Link", "required": True, "is_key": True}],
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    first = _create_task(client, executor, title="review",
                         type_key=type_key, inputs={"PR Link": "https://x/1"})
    assert first["receipt"]["deduped"] is False
    # Lower-cased + padded key, same value → dedupe onto the same task.
    again = _create_task(client, executor, title="review again",
                         type_key=type_key, inputs={"  pr link ": "https://x/1"})
    assert again["receipt"]["deduped"] is True
    assert again["task"]["id"] == first["task"]["id"]


def test_create_task_k1_rejects_empty_identity_key(client, owner_token, executor):
    # A required is_key field (K1-compliant manual). Omitting its value → 400.
    type_key = _new_manual(
        client, owner_token,
        fields=[{"name": "PR Link", "required": True, "is_key": True}],
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    r = client.post("/api/tasks",
                    json={"title": "no key", "type_key": type_key,
                          "inputs": {"PR Link": ""}},
                    headers=_auth(executor.token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    # A real value passes.
    assert _create_task(client, executor, title="has key",
                        type_key=type_key,
                        inputs={"PR Link": "https://x/9"})["receipt"]["deduped"] is False


def test_create_task_warns_on_undefined_fields(client, owner_token, executor):
    type_key = _new_manual(
        client, owner_token,
        fields=[{"name": "PR Link", "required": True, "is_key": True}],
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    # A typed create carrying a field the manual does not define → 200 + warning.
    created = _create_task(client, executor, title="w1", type_key=type_key,
                           inputs={"pr link": "https://x/1",
                                   "slack thread": "https://s/1"})
    # T-91: `warnings` is optional on taskCreateResultDTO and present only when
    # a typed create had something to advise about — so it is read off the
    # receipt, which is where the advisory about THIS call belongs.
    warnings = created["receipt"].get("warnings") or []
    assert any("slack thread" in w for w in warnings), warnings
    # All-defined inputs → no warnings key (or empty).
    clean = _create_task(client, executor, title="w2", type_key=type_key,
                         inputs={"PR Link": "https://x/2"})
    assert not (clean["receipt"].get("warnings") or [])


def test_create_ad_hoc_never_warns(client, executor):
    # An ad-hoc task has no manual, so arbitrary inputs never warn.
    created = _create_task(client, executor, title="adhoc",
                           inputs={"anything": "goes"})
    assert not (created["receipt"].get("warnings") or [])


def test_manual_rejects_is_key_without_required(client, owner_token):
    type_key = _new_manual(client, owner_token)
    # is_key without required → 400.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"fields": [{"name": "PR Link", "is_key": True, "required": False}]},
        headers=_auth(owner_token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"
    # is_key AND required → 200.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"fields": [{"name": "PR Link", "is_key": True, "required": True}]},
        headers=_auth(owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"


# ── mark_duplicate (T-02c9) ──────────────────────────────────────────────────


def _mark_duplicate(client, token, task_id, duplicate_of):
    return client.post(
        f"/api/tasks/{task_id}/duplicate",
        json={"duplicate_of": duplicate_of}, headers=_auth(token))


def test_mark_duplicate_closes_and_guards_depth1(client, owner_token, executor):
    """T-02c9: mark_duplicate is a DEDICATED terminal action. It closes the task
    with status=duplicated + duplicate_of set (closed_ts stamps), the graph is
    kept depth-1 (no self, no pointing at a duplicate, no marking an original,
    no re-marking a closed task); 'duplicated' is reachable ONLY through this
    dedicated action (task status is derived, never agent-reported)."""
    original = _create_task(client, executor, title="original")["task"]
    dup = _create_task(client, executor, title="dup shell")["task"]

    # validation: self → 409, unknown original → 404, blank → 422.
    assert _mark_duplicate(client, executor.token, dup["id"], dup["id"]).status_code == 409
    assert _mark_duplicate(client, executor.token, dup["id"], "t-nope").status_code == 404
    assert _mark_duplicate(client, executor.token, dup["id"], "").status_code == 422

    # happy path: dup becomes duplicated, points at the original, is closed.
    r = _mark_duplicate(client, executor.token, dup["id"], original["id"])
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["status"] == "duplicated"
    assert body["duplicate_of"] == original["id"]
    assert body["closed_ts"] is not None

    # the light list projection carries duplicate_of too (both DTO paths).
    listed = client.get(
        "/api/tasks", params={"status": "duplicated"}, headers=_auth(owner_token)
    ).json()
    row = next(x for x in listed if x["id"] == dup["id"])
    assert row["duplicate_of"] == original["id"], row

    # depth-1: cannot point AT a duplicate, cannot mark an existing original.
    other = _create_task(client, executor, title="other")["task"]
    assert _mark_duplicate(client, executor.token, other["id"], dup["id"]).status_code == 409
    assert _mark_duplicate(client, executor.token, original["id"], other["id"]).status_code == 409

    # re-marking a closed task → 409.
    assert _mark_duplicate(client, executor.token, dup["id"], original["id"]).status_code == 409


def test_mark_duplicate_owner_may_mark_any_task(client, owner_token, executor):
    """T-02c9 point 5: the owner (admin) may mark any task, not just its
    executor — the same lever that lets the finder converge a duplicate."""
    original = _create_task(client, executor, title="orig-owner")["task"]
    dup = _create_task(client, executor, title="dup-owner")["task"]
    r = _mark_duplicate(client, owner_token, dup["id"], original["id"])
    assert r.status_code == 200, r.text
    assert r.json()["status"] == "duplicated"


# ── reassign (T-160e) ────────────────────────────────────────────────────────


def _reassign(client, token, task_id, target, note=None):
    body = {"target": target}
    if note is not None:
        body["note"] = note
    return client.post(
        f"/api/tasks/{task_id}/reassign", json=body, headers=_auth(token))


def test_reassign_hands_over_to_a_member_and_only_they_take_over(
    client, owner_token, executor
):
    """T-160e + T-9ca5: the owner reassigns a running task to another member —
    waiting gate cards expire, unfinished steps fall back to pending (done rows
    stay), the task enters the `reassigning` LOCK (status stays DERIVED), and
    ONLY the new executor may CLAIM it (the old executor is a 403 — no longer
    the executor); the claim clears the lock."""
    new_id = hire_member(client, owner_token, "conf-reassign-target")
    new_token = mint_member_token(client, owner_token, new_id, ttl_days=1)

    task = _create_task(client, executor, title="handover me")["task"]
    plan = _plan_view(client, executor.token, task["id"], [
        {"name": "finished", "dod": "d"},
        {"name": "unfinished", "dod": "d"},
        {"name": "ask owner", "dod": "d", "is_gate": True},
    ])
    steps = plan["steps"]
    assert _step_status(client, executor.token, task["id"], steps[0]["id"], "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], steps[0]["id"], "done").status_code == 200
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "reassign gate",
              "options": [{"text": "go"}, {"text": "hold"}],
              "linked_task": {"task_id": task["id"], "step_id": steps[2]["id"]}},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]

    r = _reassign(client, owner_token, task["id"],
                  {"kind": "member", "member_id": new_id}, note="接手備註")
    assert r.status_code == 200, r.text
    body = r.json()
    # T-91: reassign answers taskWriteReceiptDTO — the task's CARD, not the
    # task. Key-set equality: asserting only that `lock` is present would stay
    # green if the whole task came back, because a task carries a lock too.
    assert set(body) == {
        "task_id", "title", "status", "executor_id", "executor_kind", "lock",
        "closed_ts", "duplicate_of", "deps", "progress_done", "progress_total",
        "artifact_count", "description_size_chars", "description_sha256",
    }, body
    # reassigning is a LOCK now (T-9ca5), not a status; status stays DERIVED
    # (a done step + pending steps → in_progress).
    assert body["lock"] == "reassigning"
    assert body["status"] == "in_progress"
    assert body["executor_kind"] == "member"
    assert body["executor_id"] == new_id
    # identity untouched — `id` is spelled task_id on the receipt, and
    # dedupe_key is not a card field, so it is checked where it lives.
    assert body["task_id"] == task["id"]
    assert _get_task(client, owner_token, task["id"])["dedupe_key"] == (
        task["dedupe_key"])

    # the waiting gate card expired (server-side — the ask was the old
    # executor's); expired is terminal, the owner cannot answer it any more.
    card = client.get(f"/api/reply-cards/{card_id}", headers=_auth(owner_token)).json()
    assert card["status"] == "expired", card

    # steps: done kept; the unfinished + released gate rows fall pending.
    view = _get_task(client, owner_token, task["id"])
    by_name = {s["name"]: s["status"] for s in view["steps"]}
    assert by_name == {"finished": "done", "unfinished": "pending",
                       "ask owner": "pending"}, by_name

    # The server-authored handover message teaches the NEW executor the claim
    # action — never the removed task-status report (T-8449).
    msgs = client.get(f"/api/chat?with={new_id}&limit=-1",
                      headers=_auth(owner_token)).json()["messages"]
    # 🔴 SELECTED STRUCTURALLY, NOT BY ITS WORDING (T-6f44). This used to look
    # for the Chinese substring 「你接手了任務」, which made a suite about BEHAVIOUR
    # fail the day the owner edited the sentence — the notice was posted, the
    # filter simply stopped matching, and the failure said nothing about that.
    # The three facts that are actually contractual are the sender, the
    # recipient (the query already pins it) and the task the row is about; all
    # three ride the row itself and none of them is prose an owner may reword.
    handover = [m for m in msgs
                if m["from"] == "system" and m["meta"].get("task_id") == task["id"]]
    assert handover, "reassign must post a handover chat message to the new executor"
    for m in handover:
        assert "claim_task" in m["body"], m["body"]
        assert "update_task_status" not in m["body"], m["body"]

    # the OLD executor is out: it is no longer the executor, so it cannot claim.
    assert client.post(f"/api/tasks/{task['id']}/claim",
                       headers=_auth(executor.token)).status_code == 403
    # the NEW executor takes over via the claim action — the lock clears
    # (T-9ca5: claim replaced the reassigning→in_progress status report).
    r = client.post(f"/api/tasks/{task['id']}/claim", headers=_auth(new_token))
    assert r.status_code == 200, r.text
    assert r.json()["lock"] == ""


def test_reassign_to_outsource_lands_unassigned(
    client, owner_token, executor, machine
):
    """T-35e0 outsource target: the reassign no longer mints a worker on the
    spot — it lands the task UNASSIGNED (発包 → an unassigned outsource task)
    under the `reassigning` lock, carrying the dialog's model/effort/machine on
    the row for the scheduler to pick up under the global cap. No worker is
    bound at reassign time."""
    task = _create_task(client, executor, title="outsource me")["task"]
    r = _reassign(client, owner_token, task["id"],
                  {"kind": "outsource", "model": "haiku", "effort": "high",
                   "machine": machine})
    assert r.status_code == 200, r.text
    body = r.json()
    # reassigning is a LOCK now (T-9ca5); the fresh task has no steps, so the
    # derived status is not_started alongside the reassigning lock.
    assert body["lock"] == "reassigning"
    assert body["status"] == "not_started"
    assert body["executor_kind"] == "outsource"
    # unassigned: the scheduler mints the successor later, none bound here.
    assert body["executor_id"] == ""


def test_dispatch_target_machine_must_resolve(
    client, owner_token, executor, machine
):
    """發包 placement is an EXPLICIT machine on both dispatch faces (create with a
    target, reassign to one): any non-blank ``target.machine`` must name a real
    machine, and "auto" is simply an id that names none (404). OMITTING it stays
    legal — the field is inherited (the type manual for a typed task, else the
    dispatching member) and has no server-invented fallback."""
    def create(machine_id):
        target = {"kind": "outsource"}
        if machine_id is not None:
            target["machine"] = machine_id
        return client.post("/api/tasks",
                           json={"title": "發包 placement", "target": target},
                           headers=_auth(owner_token))

    for bad in ("auto", "warden-mbp5"):
        r = create(bad)
        assert r.status_code == 404, f"create {bad!r}: {r.status_code} {r.text}"
    r = create(machine)
    assert r.status_code == 200, r.text
    # T-91: create answers taskCreateResultDTO — the placement it produced is
    # read off the task itself.
    assert set(r.json()) == {"task_id", "task_no", "deduped"}, r.text
    placed = _get_task(client, owner_token, r.json()["task_id"])
    assert placed["executor_kind"] == "outsource"
    assert create(None).status_code == 200, "an omitted machine inherits, never 404s"

    for bad in ("auto", "warden-mbp5"):
        task = _create_task(client, executor, title=f"發包 {bad}")["task"]
        r = _reassign(client, owner_token, task["id"],
                      {"kind": "outsource", "machine": bad})
        assert r.status_code == 404, f"reassign {bad!r}: {r.status_code} {r.text}"
        # The refusal changed nothing — the task is still the member's.
        after = _get_task(client, owner_token, task["id"])
        assert after["executor_kind"] == "member", after
        assert after["executor_id"] == executor.member_id, after
    task = _create_task(client, executor, title="發包 real machine")["task"]
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "outsource", "machine": machine}).status_code == 200


def test_reassign_guards(client, owner_token, executor):
    """T-160e guards: frozen 400, terminal 409, warden/unknown target 400,
    same-executor 409. ② the route is opened to `agent` + an executor guard —
    a NON-executor agent is 403. 正職授權矩陣 (T-23cf) rule 7: the OWN executor (a
    一般正職) may 發包 its task (outsource → 2xx) but may NOT hand it to another
    member (member target → 403 — owner/Mira's channel only)."""
    task = _create_task(client, executor, title="guard me")["task"]
    member_target = {"kind": "member", "member_id": executor.member_id}

    fresh = hire_member(client, owner_token, "conf-reassign-guard-tgt")
    # ② a NON-executor agent may not reassign someone else's task — executor
    # guard 403 (leaves the guard task untouched for the checks below).
    intruder_id = hire_member(client, owner_token, "conf-reassign-intruder")
    intruder = mint_member_token(client, owner_token, intruder_id, ttl_days=1)
    assert _reassign(client, intruder, task["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 403
    # rule 7: the OWN executor (一般正職) reassigning to another MEMBER is 403 —
    # a member handover is owner/Mira's alone.
    own = _create_task(client, executor, title="my own to hand over")["task"]
    assert _reassign(client, executor.token, own["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 403
    # rule 7 positive: the OWN executor MAY turn it into a 發包 (outsource → 2xx),
    # on a SEPARATE fresh task so the mutation never disturbs the checks below.
    outsourced = _create_task(client, executor, title="my own to 發包")["task"]
    assert _reassign(client, executor.token, outsourced["id"],
                     {"kind": "outsource", "model": "sonnet",
                      "effort": "low"}).status_code == 200
    # target == current executor → 409.
    assert _reassign(client, owner_token, task["id"], member_target).status_code == 409
    # warden target / unknown member → 400.
    warden_id = hire_member(client, owner_token, "conf-reassign-warden", kind="warden")
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": warden_id}).status_code == 400
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": "m-nobody"}).status_code == 400
    # A FROZEN task IS reassignable (owner ruling 2026-08-11, T-b9f6 —
    # 「我不覺得凍結的東西應該不能轉派 我覺得應該移除凍結不能轉派的限制」).
    # This block used to assert 400 「unfreeze it before reassigning」; it is
    # INVERTED rather than deleted, so the next reader sees the refusal was
    # ruled away on purpose instead of guessing it was lost. Freezing still
    # means "do not advance this" — the reassign only ARRANGES the successor,
    # and the priority must survive it (a reassign that silently thawed the
    # task would defeat the freeze while looking like success).
    assert client.post(f"/api/tasks/{task['id']}/priority",
                       json={"priority": "frozen"},
                       headers=_auth(owner_token)).status_code == 200
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 200
    assert client.get(f"/api/tasks/{task['id']}",
                      headers=_auth(owner_token)).json()["priority"] == "frozen"
    # Hand it back so the rest of this test keeps its original fixture, and
    # unfreeze (the terminal-task case below must not be measuring a frozen one).
    assert _reassign(client, owner_token, task["id"], member_target).status_code == 200
    assert client.post(f"/api/tasks/{task['id']}/priority",
                       json={"priority": "mid"},
                       headers=_auth(owner_token)).status_code == 200
    # terminal task → 409.
    assert client.post(f"/api/tasks/{task['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 200
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 409
