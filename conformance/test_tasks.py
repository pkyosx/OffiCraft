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
    the ASSIGNEE face stays owner-only governance (an agent supplying
    `assignee` on create or edit is a flat 403) and delete stays owner-only;
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


def _create_task(client, executor, title="conf task", **extra) -> dict:
    body = {"title": title, "executor_member_id": executor.member_id, **extra}
    r = client.post("/api/tasks", json=body, headers=_auth(executor.token))
    assert r.status_code == 200, f"create failed: {r.status_code} {r.text}"
    return r.json()


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
    r = _plan(client, token, task_id, [{"name": name, "dod": "asserted"}])
    assert r.status_code == 200, f"drive plan failed: {r.status_code} {r.text}"
    step_id = r.json()["steps"][0]["id"]
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


def _open_count(client, owner_token) -> int:
    r = client.get("/api/tasks/count", headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    return r.json()["open"]


def _new_manual(client, owner_token, **edits) -> str:
    type_key = f"conf-task-type-{uuid.uuid4().hex[:8]}"
    r = client.post(
        "/api/task-manuals", json={"type_key": type_key},
        headers=_auth(owner_token))
    assert r.status_code == 200, f"manual create failed: {r.status_code} {r.text}"
    if edits:
        r = client.post(
            f"/api/task-manuals/{type_key}", json=edits,
            headers=_auth(owner_token))
        assert r.status_code == 200, f"manual edit failed: {r.status_code} {r.text}"
    return type_key


# ── the full loop: create → plan → steps → gate → answer → resume → done ─────


def test_full_task_loop(client, owner_token, executor):
    created = _create_task(client, executor, title="release v2")
    assert created["deduped"] is False
    task = created["task"]
    assert task["status"] == "not_started"
    assert task["executor_kind"] == "member"
    assert task["task_no"].startswith("T-")
    before = _open_count(client, owner_token)

    # Plan: a plain step, a gate, a closing step. The task status is DERIVED —
    # it lifts off not_started when the first step is reported in_progress below.
    r = _plan(client, executor.token, task["id"], [
        {"name": "prep", "dod": "branch green"},
        {"name": "owner approves", "dod": "explicit go", "is_gate": True},
        {"name": "ship", "dod": "deployed"},
    ])
    assert r.status_code == 200, r.text
    view = r.json()
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
        f"/api/tasks/{task['id']}/steps/{gate['id']}/gate",
        json={"kind": "decision", "summary": "ship release v2?",
              "options": ["ship it", "hold"]},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    card = r.json()
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
        headers=_auth(owner_token)).json()
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
                    json={"option_idx": 0}, headers=_auth(owner_token))
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
    assert final["status"] == "done"
    assert final["closed_ts"]
    assert final["progress_done"] == 3 and final["progress_total"] == 3
    # The badge counts open tasks only — the finished loop dropped off.
    assert _open_count(client, owner_token) == before - 1


def test_open_gate_arms_a_plain_non_gate_step(client, owner_token, executor):
    """open_gate on a plain (is_gate=false) not-done step is a legitimate ad-hoc
    請示 — the explicit twin of create_reply_card's auto-bind. It arms the step
    (waiting_owner + bound card, task follows) WITHOUT flipping is_gate."""
    task = _create_task(client, executor, title="ad-hoc ask")["task"]
    r = _plan(client, executor.token, task["id"],
              [{"name": "build", "dod": "compiles"}])
    assert r.status_code == 200, r.text
    step = r.json()["steps"][0]
    assert step["is_gate"] is False
    # Report the step in_progress so the task derives in_progress — a gate can
    # only arm on an in_progress (or waiting_owner) task.
    assert _step_status(client, executor.token, task["id"], step["id"],
                        "in_progress").status_code == 200

    r = client.post(
        f"/api/tasks/{task['id']}/steps/{step['id']}/gate",
        json={"kind": "decision", "summary": "which cloud?",
              "options": ["aws", "gcp"]},
        headers=_auth(executor.token))
    assert r.status_code == 200, f"plain-step arm must 200: {r.status_code} {r.text}"
    card = r.json()
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
    r = _plan(client, executor.token, task["id"], [
        {"name": "寫規格", "dod": "spec.md 落檔"},
        {"name": "寫數字 A", "dod": "a.txt 落檔", "parallel_group": "pg-1"},
        {"name": "寫數字 B", "dod": "b.txt 落檔", "parallel_group": "pg-1"},
        {"name": "加總（匯合）", "dod": "sum.txt = A+B"},
    ])
    assert r.status_code == 200, r.text
    view = r.json()
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
    r = _plan(client, executor.token, task["id"], [
        {"name": "lane a", "dod": "d", "parallel_group": "pg"},
        {"name": "lane b", "dod": "d", "parallel_group": "pg"},
    ])
    assert r.status_code == 200, r.text
    lane_a = r.json()["steps"][0]
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
    r = _plan(client, executor.token, task["id"], [
        {"name": "lane b2", "dod": "d", "parallel_group": "pg"},
        {"name": "join", "dod": "d"},
    ])
    assert r.status_code == 200, r.text
    steps = r.json()["steps"]
    assert [s["parallel_group"] for s in steps] == ["pg", "pg", ""]
    assert steps[0]["status"] == "done"


def test_replan_relisting_done_steps_keeps_them_once(client, owner_token, executor):
    """Whole-replace-but-keep-done: re-listing an already-done node by name in a
    replan does NOT duplicate it (the 5→9 bug). The done node is preserved from
    the kept prefix; a fresh entry with the same name is that node, not a twin."""
    task = _create_task(client, executor, title="replan no-dup")["task"]
    r = _plan(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
    ])
    assert r.status_code == 200, r.text
    one = r.json()["steps"][0]
    assert _step_status(client, executor.token, task["id"], one["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], one["id"],
                        "done").status_code == 200

    # Re-submit the WHOLE plan back — done "one" re-listed — plus a new step.
    r = _plan(client, executor.token, task["id"], [
        {"name": "one", "dod": "d1"},
        {"name": "two", "dod": "d2"},
        {"name": "three", "dod": "d3"},
    ])
    assert r.status_code == 200, r.text
    steps = r.json()["steps"]
    names = [s["name"] for s in steps]
    assert names == ["one", "two", "three"], names
    assert names.count("one") == 1
    assert steps[0]["id"] == one["id"] and steps[0]["status"] == "done"
    assert r.json()["progress_done"] == 1 and r.json()["progress_total"] == 3


def test_replan_keeps_answered_card_step_as_superseded(
    client, owner_token, executor
):
    """T-1aea: a replan PRESERVES a step whose latest bound card was already
    answered — frozen to the `superseded` terminal state in its original slot
    ahead of the fresh plan (finished_ts stamped, card join intact) — while a
    step whose card still WAITS is replaced as before. Superseded counts
    toward neither progress side; re-arming it is a 409."""
    task = _create_task(client, executor, title="replan keeps answered")["task"]
    r = _plan(client, executor.token, task["id"], [
        {"name": "ask direction", "dod": "owner answered"},
        {"name": "pending ask", "dod": "owner answered"},
    ])
    assert r.status_code == 200, r.text
    answered_step, waiting_step = r.json()["steps"]
    # Lift the task to in_progress (derived) so the gates below can arm.
    assert _step_status(client, executor.token, task["id"],
                        answered_step["id"], "in_progress").status_code == 200

    # Arm both; answer only the first.
    r = client.post(
        f"/api/tasks/{task['id']}/steps/{answered_step['id']}/gate",
        json={"kind": "decision", "summary": "which way?",
              "options": ["a", "b"]},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    answered_card = r.json()
    r = client.post(
        f"/api/tasks/{task['id']}/steps/{waiting_step['id']}/gate",
        json={"kind": "decision", "summary": "later?", "options": ["a", "b"]},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    r = client.post(f"/api/reply-cards/{answered_card['id']}/answer",
                    json={"option_idx": 0}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text

    # Replan with entirely fresh names.
    r = _plan(client, executor.token, task["id"], [
        {"name": "build", "dod": "d"},
    ])
    assert r.status_code == 200, r.text
    body = r.json()
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
        f"/api/tasks/{task['id']}/steps/{frozen['id']}/gate",
        json={"kind": "decision", "summary": "again?", "options": ["a", "b"]},
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
    assert first["deduped"] is False

    # The same identity key against the OPEN task: the EXISTING task, 200.
    again = _create_task(client, executor, title="review 9 (again)",
                         type_key=type_key, inputs={"pr": "9"})
    assert again["deduped"] is True
    assert again["task"]["id"] == first["task"]["id"]

    # A different key never collides.
    other = _create_task(client, executor, title="review 10",
                         type_key=type_key, inputs={"pr": "10"})
    assert other["deduped"] is False
    assert other["task"]["id"] != first["task"]["id"]

    # Close the first; the same key then mints FRESH (periodic reopen, H2).
    r = client.post(f"/api/tasks/{first['task']['id']}/terminate",
                    headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    reopened = _create_task(client, executor, title="review 9 (reopen)",
                            type_key=type_key, inputs={"pr": "9"})
    assert reopened["deduped"] is False
    assert reopened["task"]["id"] != first["task"]["id"]

    # A missing required input is refused.
    r = client.post(
        "/api/tasks", json={"title": "no key", "type_key": type_key},
        headers=_auth(executor.token))
    assert r.status_code == 400, f"{r.status_code} {r.text}"


# ── terminal guards: a closed task is a wall ─────────────────────────────────


def test_terminated_task_refuses_every_agent_push(client, owner_token, executor):
    task = _create_task(client, executor)["task"]
    r = _plan(client, executor.token, task["id"],
              [{"name": "g", "dod": "d", "is_gate": True}])
    assert r.status_code == 200
    gate_id = r.json()["steps"][0]["id"]

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
        f"/api/tasks/{task['id']}/steps/{gate_id}/gate",
        json={"kind": "decision", "summary": "s", "options": ["a"]},
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
    assert r.json()["status"] == "waiting_external"
    assert r.json()["waiting_reason"] == "waiting for vendor credentials"
    r = _step_status(client, executor.token, task["id"], step_id, "in_progress")
    assert r.status_code == 200 and r.json()["status"] == "in_progress"
    assert r.json()["waiting_reason"] == ""


def test_waiting_owner_is_a_card_lifecycle_hold(client, owner_token, executor):
    """T-68b7: waiting_owner is bracketed entirely by the reply card. A manual
    STEP report of it is a 400 (the task-level report route is gone, T-8449);
    opening a card enters it; answering the card LEAVES it (the server restores
    in_progress) — and with two cards on one task, the task resumes only once
    the LAST is answered (SPEC §3.2)."""
    task = _create_task(client, executor, title="hold")["task"]
    r = _plan(client, executor.token, task["id"], [
        {"name": "q1", "dod": "d1"},
        {"name": "q2", "dod": "d2"},
    ])
    assert r.status_code == 200, r.text
    s1, s2 = r.json()["steps"]

    # A manual STEP report of waiting_owner is a 400, not the machine's 409.
    assert _step_status(client, executor.token, task["id"], s1["id"],
                        "waiting_owner").status_code == 400

    # Lift the task to in_progress (derived) so the gates below can arm.
    assert _step_status(client, executor.token, task["id"], s1["id"],
                        "in_progress").status_code == 200

    # Arm two cards (one per step); open_gate accepts a waiting_owner task.
    def _arm(step, summary):
        r = client.post(f"/api/tasks/{task['id']}/steps/{step['id']}/gate",
                        json={"kind": "decision", "summary": summary,
                              "options": ["a", "b"]},
                        headers=_auth(executor.token))
        assert r.status_code == 200, r.text
        return r.json()
    c1 = _arm(s1, "q1?")
    c2 = _arm(s2, "q2?")
    assert _get_task(client, owner_token, task["id"])["status"] == "waiting_owner"

    # Answer the first: its step resumes, the task keeps waiting on c2.
    assert client.post(f"/api/reply-cards/{c1['id']}/answer",
                       json={"option_idx": 0},
                       headers=_auth(owner_token)).status_code == 200
    view = _get_task(client, owner_token, task["id"])
    assert view["status"] == "waiting_owner", "still one card waiting"
    assert next(s for s in view["steps"]
                if s["id"] == s1["id"])["status"] == "in_progress"

    # Answer the last: the task resumes too.
    assert client.post(f"/api/reply-cards/{c2['id']}/answer",
                       json={"option_idx": 0},
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
    view = _plan(client, token, task["id"], [{"name": "build", "dod": "built"}]).json()
    step = view["steps"][0]
    assert _step_status(client, token, task["id"], step["id"],
                        "in_progress").status_code == 200

    def _plain_ask():
        r = client.post("/api/reply-cards",
                        json={"kind": "decision", "summary": "which?",
                              "options": ["a", "b"]}, headers=_auth(token))
        assert r.status_code == 200, r.text
        return r.json()

    first = _plain_ask()
    assert _get_task(client, owner_token, task["id"])["status"] == "waiting_owner"
    # Answer it → the hold releases, the step is back to in_progress.
    assert client.post(f"/api/reply-cards/{first['id']}/answer",
                       json={"option_idx": 0},
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


# ── owner's task-card message box ────────────────────────────────────────────


def test_task_message_rides_chat_with_task_context(client, owner_token, executor):
    task = _create_task(client, executor, title="msg target")["task"]
    r = client.post(f"/api/tasks/{task['id']}/message",
                    json={"body": "how is it going?"},
                    headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    msg = r.json()
    assert msg["from"] == "owner" and msg["to"] == executor.member_id
    assert msg["meta"]["task_id"] == task["id"]
    assert msg["meta"]["task_title"] == "msg target"
    # The visible body is prefixed with the task's display number so the
    # executor's message is self-identifying (owner 2026-07-14).
    assert msg["body"] == f"[{task['task_no']}] how is it going?"
    # It IS an ordinary chat message — the stream lists it.
    msgs = client.get(f"/api/chat?with={executor.member_id}&limit=-1",
                      headers=_auth(owner_token)).json()
    assert any(m["id"] == msg["id"] for m in msgs)
    # An empty message is refused.
    assert client.post(f"/api/tasks/{task['id']}/message", json={},
                       headers=_auth(owner_token)).status_code == 400


# ── manuals: CRUD + the delete guard ─────────────────────────────────────────


def test_manual_outsource_assignee_machine_and_unlimited_copies(
    client, owner_token
):
    """New assignee wire knobs (spec TaskManualDTO/TaskManualUpdateDTO):
    ``machine`` ("auto" | machine id; spawn placement preference) and
    ``copies`` >= 0 where 0 = 無限 (unlimited per-type copies) round-trip
    verbatim; illegal values are honest 400s."""
    type_key = _new_manual(client, owner_token)
    # machine + copies=0 (unlimited) round-trip through PATCH → GET.
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"assignee": {"kind": "outsource", "model": "claude-opus-4-6",
                           "effort": "high", "copies": 0,
                           "machine": "warden-mbp5"}},
        headers=_auth(owner_token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    a = client.get(f"/api/task-manuals/{type_key}",
                   headers=_auth(owner_token)).json()["assignee"]
    assert a["copies"] == 0 and a["machine"] == "warden-mbp5", a
    # "auto" is a legal machine value (the explicit default spelling).
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"assignee": {"kind": "outsource", "model": "m",
                           "copies": 2, "machine": "auto"}},
        headers=_auth(owner_token))
    assert r.status_code == 200
    assert r.json()["assignee"]["machine"] == "auto"
    # Illegal knobs are 400s: negative copies; blank / non-string machine.
    for bad in [{"kind": "outsource", "copies": -1},
                {"kind": "outsource", "machine": ""},
                {"kind": "outsource", "machine": 7}]:
        r = client.post(f"/api/task-manuals/{type_key}",
                        json={"assignee": bad}, headers=_auth(owner_token))
        assert r.status_code == 400, f"{bad}: {r.status_code} {r.text}"


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
    assert r.status_code == 200 and r.json()["learnings"] == "always check CI first"
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
# Agents author manual CONTENT; the assignee face + delete stay owner-only.


def test_agent_creates_manual_and_edits_content_fields(client, executor):
    type_key = f"conf-agent-type-{uuid.uuid4().hex[:8]}"
    # An agent creates a manual…
    r = client.post("/api/task-manuals", json={"type_key": type_key},
                    headers=_auth(executor.token))
    assert r.status_code == 200, f"agent create failed: {r.status_code} {r.text}"
    assert r.json()["assignee"] == {}
    # …and edits the content fields (purpose / fields / sop_md / learnings).
    r = client.post(
        f"/api/task-manuals/{type_key}",
        json={"purpose": "triage inbound bug reports",
              "fields": [{"name": "report", "required": True, "is_key": True}],
              "sop_md": "# SOP\n1. reproduce",
              "learnings": "check the version first"},
        headers=_auth(executor.token))
    assert r.status_code == 200, f"agent content edit failed: {r.status_code} {r.text}"
    manual = r.json()
    assert manual["purpose"] == "triage inbound bug reports"
    assert manual["fields"][0]["name"] == "report"
    assert manual["sop_md"].startswith("# SOP")
    assert manual["learnings"] == "check the version first"
    assert manual["assignee"] == {}, "content edit must not touch assignee"


def test_agent_supplied_assignee_is_403_on_create_and_edit(
    client, owner_token, executor
):
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
    assert r.status_code == 200 and r.json()["assignee"]["kind"] == "member"
    owner_type = f"conf-gov-type-{uuid.uuid4().hex[:8]}"
    r = client.post("/api/task-manuals",
                    json={"type_key": owner_type, "assignee": assignee},
                    headers=_auth(owner_token))
    assert r.status_code == 200 and r.json()["assignee"]["kind"] == "member"


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
    assert result["structuredContent"]["purpose"] == "mcp-authored purpose"
    # The governance boundary holds over MCP too: assignee → isError (403).
    r = _call("update_task_manual",
              {"type_key": type_key,
               "assignee": {"kind": "member", "member_id": executor.member_id}})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["result"]["isError"] is True, "assignee over MCP must 403"


# ── the worker-claim faces reachable black-box ───────────────────────────────


def test_get_my_task_member_404_and_warden_403(
    client, owner_token, executor, warden_agent
):
    # A plain member (no worker row) is an honest 404.
    r = client.get("/api/self/task", headers=_auth(executor.token))
    assert r.status_code == 404, f"{r.status_code} {r.text}"
    # A warden sits BELOW the agent floor: flat 403, deny-first.
    r = client.get("/api/self/task", headers=_auth(warden_agent.token))
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    assert "principal not permitted" in r.text


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


def test_executor_retunes_priority_and_frozen_stays_owner_only(
    client, owner_token, executor
):
    """T-0786: the executor sets high|mid|low on their OWN task; the frozen
    knob (set OR clear — leaving frozen is unfreezing) stays owner-only."""
    task = _create_task(client, executor)["task"]

    def _priority(token, value):
        return client.post(f"/api/tasks/{task['id']}/priority",
                           json={"priority": value}, headers=_auth(token))

    for value in ("high", "mid", "low"):
        r = _priority(executor.token, value)
        assert r.status_code == 200, f"{r.status_code} {r.text}"
        assert r.json()["priority"] == value
    # The executor freezing → 403.
    r = _priority(executor.token, "frozen")
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    # The owner freezes; the executor may not unfreeze; the owner may.
    assert _priority(owner_token, "frozen").status_code == 200
    r = _priority(executor.token, "high")
    assert r.status_code == 403, f"{r.status_code} {r.text}"
    assert _priority(owner_token, "mid").status_code == 200


def test_set_task_priority_via_mcp_loopback(client, executor):
    """The MCP face of the same capability: set_task_priority rides the
    loopback with the EXECUTOR's own token; get_task reads the change back;
    the frozen refusal surfaces as an isError result (403 envelope)."""
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

    r = _call(3, "set_task_priority", {"task_id": task["id"], "priority": "frozen"})
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    result = r.json()["result"]
    assert result["isError"] is True, "executor freezing over MCP must 403"
    assert '"forbidden"' in result["content"][0]["text"], result


# ── §6.3 close-out report ────────────────────────────────────────────────────


def _closeout(client, token, task_id):
    return client.post(f"/api/tasks/{task_id}/closeout", headers=_auth(token))


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

    # First report flips the flag.
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["closeout_reported"] is True

    # A repeat is a 200 no-op (idempotent — never a 409, never a re-flip).
    r = _closeout(client, executor.token, task["id"])
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    assert r.json()["closeout_reported"] is True
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
    assert r.json()["closeout_reported"] is True
    assert r.json()["status"] == "terminated"


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
    r = _plan(client, resumer, task["id"], plan)
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    steps = r.json()["steps"]
    assert _step_status(client, resumer, task["id"], steps[0]["id"],
                        "in_progress").status_code == 200
    assert _step_status(client, resumer, task["id"], steps[0]["id"],
                        "done").status_code == 200
    r = client.post(
        f"/api/tasks/{task['id']}/steps/{steps[2]['id']}/gate",
        json={"kind": "decision", "summary": "conf resume gate",
              "options": ["go", "hold"]},
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
                    json={"option_idx": 0}, headers=_auth(owner_token))
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


# ── auto card→step binding (owner design 2026-07-14) ─────────────────────────
# A plain POST /api/reply-cards by the executor of exactly one ACTIVE task
# binds the card to that task's CURRENT step and drives the same waiting
# machine as open_gate; the pointer persists after the step finishes.


def test_plain_card_auto_binds_the_current_step(client, owner_token):
    # A dedicated identity: auto-binding keys off the CALLER's active tasks,
    # so this test must not share an executor with the rest of the module.
    member_id = hire_member(client, owner_token, "conf-task-autobind")
    token = mint_member_token(client, owner_token, member_id, ttl_days=1)
    me = AgentIdentity(member_id=member_id, token=token, role_key="")

    task = _create_task(client, me, title="conf autobind")["task"]
    view = _plan(client, token, task["id"], [
        {"name": "recon", "dod": "understood"},
        {"name": "build", "dod": "built"},
    ]).json()
    build = view["steps"][1]
    assert _step_status(client, token, task["id"], build["id"],
                        "in_progress").status_code == 200

    # A PLAIN ask (no task/step in the body) binds to the running step.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "which flavour?",
              "options": ["AI pick", "other"]},
        headers=_auth(token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    card = r.json()
    assert card["task"] and card["task"]["id"] == task["id"]

    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "waiting_owner"
    bound = next(s for s in got["steps"] if s["id"] == build["id"])
    assert bound["status"] == "waiting_owner"
    assert bound["reply_card_id"] == card["id"]
    other = next(s for s in got["steps"] if s["id"] != build["id"])
    assert other["status"] == "pending" and other["reply_card_id"] == ""

    # The owner answers; the SERVER restores the held step + task to in_progress
    # (T-68b7 — no agent-reported resume), and the agent finishes the step — the
    # card pointer PERSISTS on the done step (the permanent approval mark).
    r = client.post(f"/api/reply-cards/{card['id']}/answer",
                    json={"option_idx": 0}, headers=_auth(owner_token))
    assert r.status_code == 200, r.text
    resumed = _get_task(client, owner_token, task["id"])
    assert resumed["status"] == "in_progress", "answering restores the task"
    assert next(s for s in resumed["steps"]
                if s["id"] == build["id"])["status"] == "in_progress", (
        "answering restores the held step")
    assert _step_status(client, token, task["id"], build["id"],
                        "done").status_code == 200
    got = _get_task(client, owner_token, task["id"])
    done_step = next(s for s in got["steps"] if s["id"] == build["id"])
    assert done_step["status"] == "done"
    assert done_step["reply_card_id"] == card["id"], (
        "the approval mark must persist after the step finishes")

    # With NO unambiguous current step (nothing running any more), a fresh
    # ask degrades to task-only binding and moves neither task nor steps.
    r = client.post(
        "/api/reply-cards",
        json={"kind": "decision", "summary": "one more thing?",
              "options": ["AI pick"]},
        headers=_auth(token))
    assert r.status_code == 200, f"{r.status_code} {r.text}"
    card2 = r.json()
    assert card2["task"] and card2["task"]["id"] == task["id"]
    got = _get_task(client, owner_token, task["id"])
    assert got["status"] == "in_progress"
    pending = next(s for s in got["steps"] if s["status"] == "pending")
    assert pending["reply_card_id"] == ""

    # Close out so this identity leaves no active task behind (other modules
    # open plain cards with their own agents — keep the pool clean).
    assert _step_status(client, token, task["id"], pending["id"],
                        "in_progress").status_code == 200
    # The last step done auto-derives the task to done and closes it (T-9ca5).
    assert _step_status(client, token, task["id"], pending["id"],
                        "done").status_code == 200
    assert _get_task(client, owner_token, task["id"])["status"] == "done"


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


def test_create_task_dedupes_across_field_name_case(client, owner_token, executor):
    # Manual key field is "PR Link"; callers send differently-cased/spaced keys.
    type_key = _new_manual(
        client, owner_token,
        fields=[{"name": "PR Link", "required": True, "is_key": True}],
        assignee={"kind": "member", "member_id": executor.member_id},
    )
    first = _create_task(client, executor, title="review",
                         type_key=type_key, inputs={"PR Link": "https://x/1"})
    assert first["deduped"] is False
    # Lower-cased + padded key, same value → dedupe onto the same task.
    again = _create_task(client, executor, title="review again",
                         type_key=type_key, inputs={"  pr link ": "https://x/1"})
    assert again["deduped"] is True
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
                        inputs={"PR Link": "https://x/9"})["deduped"] is False


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
    warnings = created.get("warnings") or []
    assert any("slack thread" in w for w in warnings), warnings
    # All-defined inputs → no warnings key (or empty).
    clean = _create_task(client, executor, title="w2", type_key=type_key,
                         inputs={"PR Link": "https://x/2"})
    assert not (clean.get("warnings") or [])


def test_create_ad_hoc_never_warns(client, executor):
    # An ad-hoc task has no manual, so arbitrary inputs never warn.
    created = _create_task(client, executor, title="adhoc",
                           inputs={"anything": "goes"})
    assert not (created.get("warnings") or [])


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
    plan = _plan(client, executor.token, task["id"], [
        {"name": "finished", "dod": "d"},
        {"name": "unfinished", "dod": "d"},
        {"name": "ask owner", "dod": "d", "is_gate": True},
    ]).json()
    steps = plan["steps"]
    assert _step_status(client, executor.token, task["id"], steps[0]["id"], "in_progress").status_code == 200
    assert _step_status(client, executor.token, task["id"], steps[0]["id"], "done").status_code == 200
    r = client.post(
        f"/api/tasks/{task['id']}/steps/{steps[2]['id']}/gate",
        json={"kind": "decision", "summary": "reassign gate",
              "options": ["go", "hold"]},
        headers=_auth(executor.token))
    assert r.status_code == 200, r.text
    card_id = r.json()["id"]

    r = _reassign(client, owner_token, task["id"],
                  {"kind": "member", "member_id": new_id}, note="接手備註")
    assert r.status_code == 200, r.text
    body = r.json()
    # reassigning is a LOCK now (T-9ca5), not a status; status stays DERIVED
    # (a done step + pending steps → in_progress).
    assert body["lock"] == "reassigning"
    assert body["status"] == "in_progress"
    assert body["executor_kind"] == "member"
    assert body["executor_id"] == new_id
    # identity untouched
    assert body["id"] == task["id"]
    assert body["dedupe_key"] == task["dedupe_key"]

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
                      headers=_auth(owner_token)).json()
    handover = [m for m in msgs if "你接手了任務" in m["body"]]
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


def test_reassign_to_outsource_lands_unassigned(client, owner_token, executor):
    """T-35e0 outsource target: the reassign no longer mints a worker on the
    spot — it lands the task UNASSIGNED (発包 → an unassigned outsource task)
    under the `reassigning` lock, carrying the dialog's model/effort on the
    row for the scheduler to pick up under the global cap. No worker is bound
    at reassign time."""
    task = _create_task(client, executor, title="outsource me")["task"]
    r = _reassign(client, owner_token, task["id"],
                  {"kind": "outsource", "model": "haiku", "effort": "high",
                   "machine": "auto"})
    assert r.status_code == 200, r.text
    body = r.json()
    # reassigning is a LOCK now (T-9ca5); the fresh task has no steps, so the
    # derived status is not_started alongside the reassigning lock.
    assert body["lock"] == "reassigning"
    assert body["status"] == "not_started"
    assert body["executor_kind"] == "outsource"
    # unassigned: the scheduler mints the successor later, none bound here.
    assert body["executor_id"] == ""


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
    # frozen task → 400 (unfreeze first).
    assert client.post(f"/api/tasks/{task['id']}/priority",
                       json={"priority": "frozen"},
                       headers=_auth(owner_token)).status_code == 200
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 400
    assert client.post(f"/api/tasks/{task['id']}/priority",
                       json={"priority": "mid"},
                       headers=_auth(owner_token)).status_code == 200
    # terminal task → 409.
    assert client.post(f"/api/tasks/{task['id']}/terminate",
                       headers=_auth(owner_token)).status_code == 200
    assert _reassign(client, owner_token, task["id"],
                     {"kind": "member", "member_id": fresh}).status_code == 409
