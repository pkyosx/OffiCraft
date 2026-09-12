#!/usr/bin/env python3
"""e2e_test/seven_gate/judge.py — the step-by-step verdict.

PURE. Reads ONE run directory (an evidence bundle written by collect.py) and
decides, per step, whether the SERVER held the fact that step is supposed to
leave behind. It opens no socket, mutates nothing, and never talks to the agent.
That is deliberate and it is the whole point of the split:

  * an agent asked "did you open the card?" answers yes — always, and for free;
  * a server row either carries the card or it does not.

So the judge's only input is what the server was observed to hold. If a step's
fact is absent, the step FAILED, no matter how the run narrated itself.

INPUT — one directory containing:
  scene.json      {"agent_id": "...", "scene_nonce": "...",
                   "peer_id": "...", "peer_nonce": "...",
                   "image_answer_salt": "...",
                   "image_answer_sha256": "..."}              (written before boot)

                  ⚠️ THE IMAGE ANSWER IS NOT IN THERE IN CLEAR. It used to be,
                  and this directory sits in the repo tree on the same machine
                  the live agent runs on — so ⑨'s own claim ("the number exists
                  in no file the agent can open") was false about the harness's
                  own bundle. Only salt+sha256 is stored now and this file
                  re-derives the match by hashing the digit runs it finds in the
                  agent's messages. That removes the plaintext from disk; it does
                  NOT make the answer unrecoverable to a determined process on
                  the same host (10^6 candidates against a salt that is right
                  there), and no claim here should be read as saying otherwise.
  journal.ndjson  one JSON object per poll:
                  {"t": <epoch float>, "member": <MemberDTO|null>,
                   "chat": [ChatMessageDTO...], "tasks": [TaskDTO...],
                   "reply_cards": [ReplyCardDTO...]}

WHY A JOURNAL AND NOT A FINAL SNAPSHOT: one of the facts is TRANSIENT.
`presence` returns to online/offline, so a run that only looks at the end cannot
tell "reported waking" from "never booted". The journal is a time series, so a
fact that existed for three seconds is still evidence. A step whose fact is
durable (a task row, a card row) is read from the journal all the same — one
reader, one shape.

EXIT — 0 iff EVERY GATE passed; 1 otherwise, with the FIRST failing gate named on
the last line. Every cell prints its own line regardless, because "which step"
is the answer the caller actually needs; a bare red tells them nothing.

⚠️ "EVERY GATE", not "every cell": some cells (see OBSERVATION_KEYS below) are
OBSERVATIONS — they print what they saw, are rendered OBSERVED rather than
PASS/FAIL, and cannot make this run red. `all green` therefore means "every gate
held", NOT "nine things were verified"; main() prints the gate count above the
cells so that distinction is on screen and not only in this docstring.
"""
import hashlib
import json
import os
import sys

# The steps, in the fixed order. `key` is the stable identifier a caller greps
# for; `zh` is what the owner calls it.
#
# The directory is still called seven_gate and the path now has NINE cells. The
# owner added two after the first baseline: 「跟其他 agent 溝通」 (the six steps
# before it exercise chat / reply card / task only ever TOWARDS THE OWNER) and
# 「看得到圖」 (whether it can read a picture at all). The name is historical;
# THIS LIST is the contract, and no other file may keep a copy of it.
STEPS = [
    ("report_waking", "報到"),
    ("resume_scene", "接回現場"),
    ("create_task", "開票"),
    ("submit_plan", "提出計畫"),
    ("step_done", "報一步完成"),
    ("reply_card", "開一張等我回覆卡"),
    # The KEY stays "closeout" while the label became 按下結案 (T-182): the key is
    # the id OC_SG_SKIP_STEP and every archived verdict.json is written in, and
    # renaming it would silently invalidate both. The label is what a reader acts on.
    ("closeout", "按下結案"),
    ("peer_message", "回覆另一個 agent"),
    ("image_answer", "看得到圖"),
]

# 🔴 NOT EVERY CELL IS A GATE, AND THE READER MUST NOT HAVE TO GUESS WHICH.
#
# A cell listed here DOES NOT DECIDE ANYTHING: it prints what it observed and it
# cannot make a run red. Everything else is a gate — its fact is absent, the run
# is red, and the first red is named on the last line.
#
# ⑤ is here because the thing it wants to know ("were steps reported AS THE WORK
# WENT, or all at once at the close?") IS NOT IN THE DATA: every progress fact
# the server holds is stamped when the agent pressed the button, and nothing
# records whether work happened between two reports. Two designs tried to judge
# it anyway and both failed — see the block at ⑤ below for the measurements.
#
# ① is here (owner's ruling, 2026-08-11, after ⑤) because ON THE LIVE PATH — the
# one this harness exists for — IT IS TRUE OF EVERY RUN whatever the agent does.
# It reads presence == "waking", which PresenceState derives from `waking_since`
# — and `waking_since` HAS A SECOND WRITER: reconcile stamps it on a LANDED START
# (reconcile.go, `m.WakingSince = now`). actors/live.sh onboards a machine, runs a
# real ocwarden, WAITS FOR IT TO COME ONLINE, and only then activates with a
# machine_id ⇒ the START lands ⇒ reconcile stamps, BEFORE claude is spawned. A
# gate that cannot say no about the population it is aimed at is not a gate; it is
# a sentence that reads like evidence.
#   ⚠️ SCOPE — THE STUB PATH IS THE EXCEPTION, AND SAYING SO MAKES THE ARGUMENT
#   STRONGER, NOT WEAKER. run.sh's own activate carries `{}` (no machine_id) and
#   there is no warden anywhere on that path, so the START never lands
#   (DispatchUnlanded ⇒ no stamp — that is what
#   TestReconcile_UnlandedStartDoesNotStampWakingSince pins). ①'s green under the
#   stub comes from actors/stub.sh calling report_waking itself. So the ONLY path
#   on which this cell can still be falsified is the one NOBODY RUNS, while the
#   path that costs money is the one where it is unfalsifiable.
#   🔴 EVIDENCE STRENGTH, SAID PLAINLY: established by READING THE CODE plus
#   RUNNING the server's own two tests on this tree — TestReconcile_
#   LandedStartStampsWakingSince (stamps, and PresenceState then reads "waking")
#   and TestReconcile_UnlandedStartDoesNotStampWakingSince (does not stamp), BOTH
#   PASS. NOBODY STARTED A SERVER, ONBOARDED A MACHINE AND SAMPLED `presence`.
#   The live/stub split above is derived from reading run.sh, live.sh and stub.sh
#   in that order — it is not a live measurement either. Do not cite it as one.
#   HOW IT BECOMES A GATE AGAIN: when the server holds a fact only the agent's
#   own report_waking can write, and that fact is on a DTO the collector reads.
#   Today waking_since is on neither (not on MemberDTO; GET /api/members/{id} is
#   the only member read collect.py makes), and the one member field written
#   solely by report_waking — actual_model — is optional in the body and is also
#   written by the telemetry ingest. Re-arming needs a wire change, not an edit
#   here.
#
# Downgrading is the honest answer in both cases, but a downgrade that nobody can
# see is how a gate disappears quietly, so:
#   * the per-step line says OBSERVED, never PASS/FAIL;
#   * verdict.json records `passed: null` (not true — "true" would read as
#     "verified");
#   * main() prints, above the verdict, HOW MANY cells are gates and names the
#     ones that are not;
#   * and tests_guard case 21b-v pins this membership in BOTH directions: a
#     mutant that re-arms a downgraded cell as a gate goes red, and so does one
#     that quietly downgrades any other cell into this set.
# Read a green run as "the N gates below held", never as "nine things were
# verified".
OBSERVATION_KEYS = ("report_waking", "step_done")


def load_bundle(run_dir):
    with open(os.path.join(run_dir, "scene.json"), encoding="utf-8") as fh:
        scene = json.load(fh)
    samples = []
    path = os.path.join(run_dir, "journal.ndjson")
    with open(path, encoding="utf-8") as fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                samples.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise SystemExit(
                    "[seven_gate] FATAL: journal.ndjson line %d is not JSON (%s). "
                    "A truncated journal must not be judged — it would read as "
                    "'the agent did nothing'." % (lineno, exc)
                )
    return scene, samples


def _says_answer(body, salt, digest, width=6):
    """Does `body` contain the planted number, without this file ever holding it?

    The number is `width` digits. Every window of that length in the body is
    hashed with the run's salt and compared — so the check is exactly as strong
    as the old `answer in body` was, with the plaintext gone from disk.
    Overlapping windows are deliberate: 「號碼是481902」 and 「4819023」 must both
    be found, and a non-overlapping scan would miss the second."""
    if not digest:
        return False
    for i in range(0, max(0, len(body) - width + 1)):
        chunk = body[i:i + width]
        if not chunk.isdigit():
            continue
        if hashlib.sha256((salt + chunk).encode("utf-8")).hexdigest() == digest:
            return True
    return False


def _observe_step_shape(task, samples, multi=""):
    """⑤'s OBSERVATION — the shape of the progress reports, judged by nobody.

    Two numbers, and the difference between them is the point:

      * how many DISTINCT server-stamped `finished_ts` the done steps carry, and
        how far apart the first and the last are. Both are SERVER FACTS
        (api_tasks.go stamps `step.FinishedTS = nowSecs()`, i.e.
        time.Now().UnixNano()/1e9 — about 2e-7 s of resolution at today's epoch),
        so they do not depend on how often the collector polled;
      * when the CLOSE was FIRST SEEN IN THE JOURNAL, which is a SAMPLED
        number. Since T-182 the task also carries a server-stamped `closed_ts`
        for the same moment, but the sighting is kept as the sampled figure it
        has always been and labelled as such every time it is printed, because
        the previous two versions of this cell drew a verdict from exactly this
        kind of number.
    """
    if not task:
        return ("no task to observe — ③ did not find one" + multi)
    tid = task.get("id")
    steps = task.get("steps") or []
    done = [s for s in steps if s.get("status") == "done"]
    stamps = sorted({float(s.get("finished_ts") or 0.0) for s in done
                     if (s.get("finished_ts") or 0)})
    bits = ["task %s: %d of %d plan step(s) at done" % (tid, len(done), len(steps)),
            "%d distinct server-stamped finished_ts" % len(stamps)]
    # ⚠️ THE "one-step plan" SENTENCE USED TO BE THE ONLY ELSE-BRANCH, so it was
    # printed on the SAME LINE as "3 of 3 plan step(s) at done" whenever three
    # steps shared one stamp or carried none at all — a line that contradicted
    # itself. The four cases below are told apart because the reader acts on this
    # text; none of them is a verdict (this cell judges nothing).
    if len(stamps) >= 2:
        bits.append("first→last completion %.3fs (server-stamped, not sampled)"
                    % (stamps[-1] - stamps[0]))
    elif not done:
        bits.append("first→last completion n/a (no step is at done)")
    elif not stamps:
        bits.append("first→last completion n/a (%d done step(s) and NOT ONE carries "
                    "a server stamp — this server stamps finished_ts on every step "
                    "it moves to done, so suspect the bundle, not the agent)"
                    % len(done))
    elif len(done) == 1:
        bits.append("first→last completion n/a (one done step, one stamp — a "
                    "one-step plan is a legitimate way to get here)")
    else:
        bits.append("first→last completion n/a (%d done steps share ONE server "
                    "stamp)" % len(done))
    closed_ts = task.get("closed_ts")
    if stamps and closed_ts:
        bits.append("first completion→task closed_ts %.3fs (both server-stamped)"
                    % (float(closed_ts) - stamps[0]))
    first_co = None
    for s in samples:
        for t in s.get("tasks") or []:
            if t.get("id") == tid and t.get("status") == "done":
                first_co = s.get("t")
                break
        if first_co is not None:
            break
    # CLAUDE.md promises this second number is printed EVERY ROUND. It used to be
    # skipped in silence when there were no stamps but the close-out HAD been
    # seen (neither `stamps and first_co` nor `not first_co` held), so the branch
    # order below is: say why it is missing before saying nothing.
    if not first_co:
        bits.append("the close was never seen in any sample")
    elif not stamps:
        bits.append("first completion→close sighting n/a (no done step "
                    "carries a server stamp to measure from)")
    else:
        gap = float(first_co) - stamps[0]
        if gap < 0:
            # The journal's own clock and the server's stamps are not comparable
            # in this bundle (a synthetic fixture, or a clock that moved). Say so
            # rather than print a negative duration somebody would try to explain.
            bits.append("first completion→close sighting not comparable in "
                        "this bundle (the sample clock reads before the server's "
                        "finished_ts)")
        else:
            bits.append("first completion→close FIRST SEEN in the journal "
                        "%.2fs (⚠️ SAMPLED at the collector's poll interval; the "
                        "task's own closed_ts is the server-stamped twin)" % gap)
    return ("OBSERVED, NOT JUDGED (this cell cannot make the run red — the server "
            "records when each report ARRIVED, never whether work happened "
            "between two reports, so nothing here separates 'reported as the work "
            "went' from 'reported all at once'): " + "; ".join(bits) + multi)


def _iter(samples, key):
    for s in samples:
        for item in s.get(key) or []:
            yield s, item


def judge(scene, samples):
    """-> list of (key, zh, passed, evidence_or_reason). No I/O, no ordering
    assumptions beyond 'the task the agent created is the task steps 4/5/7 are
    read from' — which is what binds those three to THIS run instead of to any
    task that happens to exist on the server."""
    agent = scene["agent_id"]
    # .strip() for the same reason peer_nonce and salt have it: a needle that is
    # only whitespace is a broken plant, and " " in body is true of almost every
    # message — the empty-string case below would not catch it. run.sh's nonce is
    # urandom hex so it cannot produce one today; the asymmetry was the defect.
    nonce = (scene["scene_nonce"] or "").strip()
    out = []

    # ① 報到 — OBSERVATION ONLY: this cell judges nothing (see OBSERVATION_KEYS
    # at the top of this file for the ruling and the evidence behind it). It
    # reports whether, and when, the member was ever seen projected as "waking".
    #
    # WHY IT STOPPED DECIDING, in one line: `presence == "waking"` is derived
    # from `waking_since`, which reconcile stamps on a LANDED START — and on the
    # live path live.sh has a warden online before it activates, so the START
    # lands and the stamp happens before claude is spawned. On that path the cell
    # could not say no about its own population. (The stub path has no warden, so
    # the START does not land and ①'s green there comes from stub.sh's own
    # report_waking — i.e. the only path that can still falsify this cell is the
    # one nobody runs. Full scope note at OBSERVATION_KEYS.)
    #
    # ⚠️ THE OTHER DIRECTION IS ALSO REAL AND IS NOT WHY IT WAS DOWNGRADED: the
    # value is transient (mounting SSE clears the anchor; the TTL lapses), so a
    # missed sample used to redden this AND NAME THE AGENT. Both halves are gone
    # now for the same reason — nothing here decides anything.
    #
    # DO NOT "FIX" THIS BY RELAXING IT to "presence was ever non-offline". That
    # is strictly worse: it would pass on an agent that never reported and was
    # merely listening, and it would still be an observation.
    hit = next((s for s in samples
                if (s.get("member") or {}).get("presence") == "waking"), None)
    out.append(("report_waking", "報到", None,
                "OBSERVED, NOT JUDGED (this cell cannot make the run red — on the "
                "live path, where a warden is online before the harness activates "
                "the member, reconcile stamps the `waking_since` this projection "
                "derives from when the START lands, before the agent runs; so a "
                "sighting here is not evidence the agent reported anything): "
                + ("member %s was seen at presence=waking at t=%s"
                   % (agent, hit["t"]) if hit
                   else "no sample ever showed member %s at presence=waking"
                        % agent)))

    # ② 接回現場 — resume_summary is a GET and stamps NOTHING server-side, so
    # there is no row to read. What IS readable is the consequence: the scene
    # nonce was planted, before boot, where only the resume snapshot surfaces
    # it, and the agent has to quote it back into a server-stored message. An
    # agent that skipped the resume cannot produce the nonce.
    #
    # 🔴 AND THE EMPTY NONCE IS THE OTHER DIRECTION OF THE SAME BROKEN-PLANT BUG
    # ⑧⑨ have: `"" in body` is TRUE for every message, so an unplanted nonce made
    # this GATE PASS on the agent's first word — a gate silently ceasing to gate,
    # which is the exact shape this whole round is about. MEASURED 2026-08-11:
    # scene_nonce="" on the green fixture ⇒ ② PASS, run all green, nothing said.
    if not nonce:
        out.append(("resume_scene", "接回現場", False,
                    "scene.json carries an EMPTY scene_nonce — the harness never "
                    "planted anything for the agent to read back, so this step "
                    "could not be observed at all (and an empty needle matches "
                    "every message, which would have made this gate PASS "
                    "unconditionally). This is a HARNESS red, not an agent red: "
                    "fix the plant."))
    else:
        hit = next((item for _, item in _iter(samples, "chat")
                    if item.get("from") == agent and nonce in (item.get("body") or "")), None)
        out.append(("resume_scene", "接回現場", hit is not None,
                    "chat message %s from %s quotes the scene nonce" % (hit.get("id"), agent) if hit
                    else "no server-stored message from %s ever quoted the scene nonce "
                         "%r — nothing shows the prior scene was read back" % (agent, nonce)))

    # ③ 開票 — a task row whose creator_id is the agent. Earliest wins, and it
    # is THE task ④⑤⑦ are judged on.
    #
    # ⚠️ EARLIEST IS A GUESS, AND IT IS THE ONE PLACE THIS FILE GUESSES. There is
    # no server fact that says "this ticket is the one this round is about": the
    # assignment (assignment.md) deliberately never mentions tickets, so the
    # harness cannot plant a marker the agent would carry into the task without
    # destroying what ③ measures. An agent that opens a scratch/draft ticket
    # first therefore gets ④⑤⑦ judged against the WRONG row — three reds that
    # are the harness's, not the agent's. The alternative (pick whichever of the
    # agent's tasks satisfies ④⑤⑦) is worse: it makes those cells unfalsifiable
    # by construction. So the exposure is kept, named here, written verbatim in
    # CLAUDE.md, and — since it cannot be removed — SAID OUT LOUD in the evidence
    # of every cell it can poison (`_multi` below).
    mine = {}
    for _, t in _iter(samples, "tasks"):
        if t.get("creator_id") == agent:
            prev = mine.get(t.get("id"))
            if prev is None or (t.get("updated_ts") or 0) >= (prev.get("updated_ts") or 0):
                mine[t["id"]] = t
    ordered = sorted(mine.values(), key=lambda t: (t.get("created_ts") or 0, t.get("id")))
    task = ordered[0] if ordered else None
    _multi = ""
    if len(ordered) > 1:
        _multi = (" ⚠️ THIS AGENT OPENED %d TASKS (%s) AND THE GATE JUDGES THE "
                  "EARLIEST — ④⑤⑦ are read from %s only. If those cells look "
                  "wrong, suspect a draft/scratch ticket opened before the real "
                  "one rather than the agent: the gate has no server fact that "
                  "says which ticket this round is about (see CLAUDE.md 〈③ 取最早…〉)."
                  % (len(ordered), ", ".join(t.get("id") or "<no id>" for t in ordered),
                     task.get("id")))
    out.append(("create_task", "開票", task is not None,
                ("task %s (%r) has creator_id=%s" % (task.get("id"), task.get("title"), agent)) + _multi if task
                else "no task on the server carries creator_id=%s — this agent "
                     "never opened a ticket" % agent))

    # ④ 提出計畫 — submit_plan is the only writer of steps[] on a task.
    steps = (task or {}).get("steps") or []
    out.append(("submit_plan", "提出計畫", bool(task) and len(steps) > 0,
                "task %s carries %d plan step(s)" % (task.get("id"), len(steps)) if steps
                else ("task %s has an empty steps[] — no plan was ever submitted"
                      % (task.get("id") if task else "<none: ③ failed>")) + _multi))

    # ⑤ 報一步完成 — OBSERVATION ONLY: this cell judges nothing (see
    # OBSERVATION_KEYS at the top of this file). It prints the shape of the
    # progress reports and cannot make the run red.
    #
    # 🔴 THE TWO THINGS THIS CELL HAS ALREADY BEEN, AND WHY BOTH FAILED.
    #
    # (1) "does ANY step of this task carry status=done". ⑦ (the close) could
    #     only be reached once every non-superseded step derived the task there
    #     (domain.go DeriveTaskStatus: allDone → done), so ⑦ GREEN IMPLIED ⑤
    #     GREEN: no world existed in which this cell was red while ⑦ was green.
    #     Zero discriminating power. MEASURED: OC_SG_SKIP_STEP=step_done left ⑤
    #     PASS and put the red on ⑦. (T-182 loosened the implication one notch —
    #     allDone now reaches `ready_for_done`, and ⑦ needs a further button
    #     press — but the implication still runs the same direction, so this cell
    #     gains no discriminating power from the change.)
    #
    # (2) "the done steps are a PREFIX of the plan, finished in plan order". That
    #     one is worse than zero, because it is red on TWO behaviours the server
    #     produces on purpose:
    #       * REPLAN. submit_plan freezes the nodes it did not re-list into
    #         `superseded` and LEAVES THEM IN PLACE, renumbered 0..n-1
    #         (dal_tasks.go ReplaceTaskPlan), while DeriveTaskStatus and
    #         TaskProgress both SKIP them. A superseded row therefore sits BEFORE
    #         later done rows, and a prefix test fails after any replan — and the
    #         boot context teaches agents to replan (seeds/system_interaction.md:
    #         「重新規劃——用 submit_plan 重交 plan」).
    #       * PARALLEL. SPEC §3.1: every step row is one leaf and parallel items
    #         are separate rows, so nodes in a parallel group finish in whatever
    #         order they finish. An order test calls that "back-filled".
    #     Both reds land on the AGENT for something the model does by design —
    #     the exact disease lib/window.sh's header names.
    #     And it bought almost nothing: with ⑦ green every non-superseded step is
    #     done, so the prefix half is TRUE BY CONSTRUCTION and only the ordering
    #     half was still saying anything.
    #
    # (3) "was the task ever OBSERVED carrying a done step while still open" — a
    #     TIME fact read from the journal's sampling. MEASURED FALSE RED
    #     (independent review, 2026-08-11, real collect.py --interval 1 + real
    #     judge.py against a stand-in server): an agent that reports every step,
    #     in order, honestly, is GREEN at a 3.0s gap between "first step done"
    #     and the close-out and RED at 0.05s — same behaviour, only faster. The
    #     same review measured that the journal of that fast-but-honest agent is
    #     BYTE-FOR-BYTE IDENTICAL to the journal of one that touches nothing
    #     until the close and then flips the whole plan to done. So the red could
    #     not tell the two apart, and a cheat only had to be SLOW to pass.
    #
    # 🔴 SO THIS CELL IS NO LONGER A GATE. It is an OBSERVATION: it prints the
    # shape it sees and it CANNOT MAKE A RUN RED. `passed` is None, which is what
    # main() renders as OBSERVED and what verdict.json records as null.
    #
    # WHY, IN ONE SENTENCE, AND IT IS NOT A LIMITATION OF THIS FILE: every
    # progress fact the server holds is stamped AT THE MOMENT THE AGENT PRESSED
    # THE BUTTON. Nothing on the server records whether any work happened BETWEEN
    # two reports. "Worked incrementally" and "did nothing and reported at the
    # end" therefore differ only in the SIZE OF THE GAPS — and a gap is not
    # evidence of work, it is evidence of waiting. Judging it would need a
    # threshold, and no threshold is derivable: the harness guarantees no minimum
    # interval (the ⑥ card round-trip disappears on the OC_SG_SKIP_STEP=reply_card
    # run and the agent chooses when to open the card anyway), a localhost round
    # trip bounds honest and dishonest agents alike, and a plan step can legally
    # be one line of text. Worse, any threshold N is satisfied by `sleep N`, which
    # costs a cheater nothing. A guessed constant here is the same species of
    # mistake this harness already paid for twice (`--seconds 900`, PASS_FLOOR 100).
    #
    # WHAT IT WOULD TAKE TO JUDGE IT:
    #   * the CLOSE MOMENT as a server fact the judge can read. T-182 SUPPLIED
    #     THIS and it changed nothing here: `closed_ts` is on TaskDTO, stamped by
    #     closeTask, so the sighting no longer HAS to be sampled;
    #   * a threshold that is DERIVED from something, not picked. See above: no
    #     such derivation is known, and "another guessed number" is not a fix.
    # The second one is why the cell is still an observation. Getting the
    # timestamp was never the missing half.
    #
    # WHAT IT DOES GIVE YOU, and it needs no constant and no sampling luck: the
    # two numbers below, printed on every run, green or not.
    out.append(("step_done", "報一步完成", None, _observe_step_shape(task, samples, _multi)))

    # ⑥ 開一張等我回覆卡 — a reply card whose `from` is the agent. The agent has
    # to DECIDE to open it; a card the harness opened on its behalf proves
    # nothing, which is why the initiator is checked and not merely the count.
    card = next((item for _, item in _iter(samples, "reply_cards")
                 if item.get("from") == agent), None)
    out.append(("reply_card", "開一張等我回覆卡", card is not None,
                "reply card %s was opened by %s (status=%s)"
                % (card.get("id"), agent, card.get("status")) if card
                else "no reply card on the server carries from=%s — the agent "
                     "never asked the owner anything" % agent))

    # ⑦ 按下結案 — T-182. `status == done` is the server's own record that the
    # agent CALLED mark_task_done, and on this run nothing else can produce it:
    # a task whose every step is reported settles in `ready_for_done` and stays
    # there until somebody presses the button. Before T-182 the steps closed the
    # task by themselves, so "done" proved nothing about the agent and a separate
    # `closeout_reported` flag had to carry that fact; the flag and the tool
    # behind it are gone, and `done` now means what the flag meant.
    closed = bool(task) and task.get("status") == "done"
    out.append(("closeout", "按下結案", closed,
                "task %s reached status=done (closed_ts=%s)"
                % (task.get("id"), task.get("closed_ts")) if closed
                else ("task %s is status=%s — the work may have stopped but the "
                      "agent never closed the task"
                      % (task.get("id"), task.get("status")) if task
                      else "task <none: ③ failed>") + _multi))

    # ⑧ 回覆另一個 agent — the three channels an agent has are chat, the reply
    # card and the task, and every step above exercises them only TOWARDS THE
    # OWNER. Talking to a COLLEAGUE is a different act: a different recipient,
    # and nobody with an owner's patience on the other end. So the harness seats
    # a second member, has that member speak FIRST (carrying its own nonce), and
    # reads one fact: a server-stored message SENT BY the agent and ADDRESSED TO
    # that member.
    #
    # Two conditions, and they are different claims:
    #   * to == peer     — it talked to a colleague at all (never the owner,
    #                      never itself). This is the half the owner asked for.
    #   * nonce quoted   — it was a REPLY: the colleague's message was read and
    #                      answered, not merely broadcast past. Same read-back
    #                      trick as ②, and the same honest limit: it proves the
    #                      content came back, not which tool fetched it.
    peer = (scene.get("peer_id") or "").strip()
    peer_nonce = (scene.get("peer_nonce") or "").strip()
    if not peer:
        out.append(("peer_message", "回覆另一個 agent", False,
                    "scene.json carries no peer_id — the harness never seated a "
                    "second agent, so this step could not be observed at all. "
                    "This is a HARNESS red, not an agent red: fix the plant."))
    elif not peer_nonce:
        # 🔴 WITHOUT THIS BRANCH THE EMPTY NONCE ACCUSED THE AGENT. `replied`
        # tests `peer_nonce and peer_nonce in body`, so an empty nonce can never
        # match — and the run fell through to the "spoke to the colleague without
        # showing it read what the colleague said" sentence, which names the
        # agent for a plant the harness never made. Same class as the missing
        # peer_id above; it just did not have its own branch.
        out.append(("peer_message", "回覆另一個 agent", False,
                    "scene.json carries an EMPTY peer_nonce — the peer never "
                    "spoke (or its message carried no nonce), so there is nothing "
                    "for the agent to quote back and this step could not be "
                    "observed at all. This is a HARNESS red, not an agent red: "
                    "fix the plant."))
    else:
        addressed = [item for _, item in _iter(samples, "chat")
                     if item.get("from") == agent and item.get("to") == peer]
        replied = next((m for m in addressed
                        if peer_nonce and peer_nonce in (m.get("body") or "")), None)
        hit = replied or (addressed[0] if addressed else None)
        if replied is not None:
            why = ("chat message %s runs %s → %s and quotes the peer's nonce — "
                   "the colleague was read AND answered"
                   % (replied.get("id"), agent, peer))
        elif hit is not None:
            why = ("chat message %s runs %s → %s but does NOT carry the peer's "
                   "nonce %r — the agent spoke to the colleague without showing "
                   "it read what the colleague said"
                   % (hit.get("id"), agent, peer, peer_nonce))
        else:
            why = ("no server-stored message runs %s → %s — the agent never "
                   "said anything to the other agent (talking to the owner does "
                   "not count: that is what steps ②⑥ already read)"
                   % (agent, peer))
        out.append(("peer_message", "回覆另一個 agent", replied is not None, why))

    # ⑨ 看得到圖 — can it SEE? An image was planted before boot whose pixels
    # carry a number. The fact is simply: did a message the agent SENT ever
    # contain that number. A text-only agent has no path to those digits but the
    # pixels — PROVIDED the answer is not reachable as text anywhere else, which
    # is the entire validity of this cell and is NOT something a comment can
    # assert. What actually holds it up, and exactly how far each part reaches:
    #
    #   * run.sh 3d scans the planted scene as TEXT ON THE SERVER (/api/chat,
    #     /api/tasks, both reply-card panes, /api/members, the agent's own resume
    #     snapshot) AND every file this run writes under the run dir except the
    #     picture itself, and refuses to start on a single hit. A positive
    #     control runs first so "zero hits" can never quietly mean "the scanner
    #     is broken".
    #   * actors/live.sh scrubs the harness's whole OC_SG_*/SG_* namespace out of
    #     the environment it hands the warden, and PROVES it (lib/scrub.sh
    #     sg_scrub_assert, with its own positive control) before spawning. Before
    #     that existed the answer travelled run.sh → actor → warden → tmux → the
    #     agent's shell, where one `env` would have handed a blind agent a green
    #     indistinguishable from a real one.
    #   * scene.json holds salt+sha256, not the number (see this file's header).
    #
    # WHAT IS STILL NOT COVERED, said plainly: the live agent runs as the same
    # user on the same host, so nothing here stops a process that goes LOOKING —
    # reading the repo tree, or brute-forcing 10^6 candidates against the salt.
    # The construction removes the answer from everything the agent is HANDED; it
    # does not make it unreachable.
    #
    # The answer is REGENERATED PER RUN and never the famous 42: a hard-coded
    # answer is one a model can have memorised, and a cell a model can pass from
    # memory measures nothing.
    salt = (scene.get("image_answer_salt") or "").strip()
    digest = (scene.get("image_answer_sha256") or "").strip().lower()
    width = int(scene.get("image_answer_len") or 6)
    if not digest:
        out.append(("image_answer", "看得到圖", False,
                    "scene.json carries no image_answer_sha256 — the harness "
                    "never planted the picture, so this step could not be "
                    "observed at all. This is a HARNESS red, not an agent red."))
    elif not salt:
        # The twin of the empty peer_nonce above. run.sh always writes a 32-hex
        # salt and hashes salt+answer, so an empty salt means the plant broke:
        # _says_answer would hash the bare digits and match nothing, and the
        # else-branch below would tell the reader the agent never opened the
        # picture.
        out.append(("image_answer", "看得到圖", False,
                    "scene.json carries a digest but an EMPTY image_answer_salt "
                    "— the digest was computed over salt+answer, so nothing can "
                    "ever match it and this step could not be observed at all. "
                    "This is a HARNESS red, not an agent red."))
    else:
        seen = next((item for _, item in _iter(samples, "chat")
                     if item.get("from") == agent
                     and _says_answer(item.get("body") or "", salt, digest, width)), None)
        out.append(("image_answer", "看得到圖", seen is not None,
                    "chat message %s from %s carries the number that exists only "
                    "in the planted image's pixels" % (seen.get("id"), agent) if seen
                    else "no message from %s ever carried the number drawn in the "
                         "planted image — nothing shows the picture was opened "
                         "and read (the number appears in no text the agent can "
                         "reach, so it cannot be produced any other way)" % agent))

    # THE GATE/OBSERVATION SPLIT IS APPLIED IN _seal() BELOW — see it for why.
    return _seal(out)


# ⚠️ THE EVIDENCE OF A FAIL-CLOSED RED DESCRIBES NOTHING THAT WAS MEASURED. A
# gate that produced no verdict at all still carries whatever text its cell
# happened to build — which is the cell's else-branch, and that branch asserts a
# fact ("the agent never closed the task") that may be the opposite of what is in the
# bundle. Reviewed 2026-08-11: the red is correct, the sentence under it is not,
# so the conversion says so in the line the reader acts on.
NOTHING_DECIDED = (" ⚠️ FAIL-CLOSED: this GATE produced no verdict at all, so it "
                   "is red because NOTHING WAS DECIDED, not because the sentence "
                   "above was measured — that text is the cell's else-branch and "
                   "may not describe this bundle. Fix judge.py, not the agent.")


def _seal(rows):
    """Apply the gate/observation split from the single declaration at the top of
    this file — not sprinkled through the cells — so that there is exactly one
    place to read (and exactly one place a mutant can move). Fail-closed both
    ways: a cell in OBSERVATION_KEYS never decides anything no matter what it
    computed, and a GATE that somehow produced no verdict is a red, never a pass,
    because "nothing was decided" must not print as PASS."""
    out = []
    for k, z, p, w in rows:
        if k in OBSERVATION_KEYS:
            out.append((k, z, None, w))
        elif p is None:
            out.append((k, z, False, w + NOTHING_DECIDED))
        else:
            out.append((k, z, p, w))
    return out


def main(argv):
    if len(argv) != 2:
        print("usage: judge.py <run-dir>", file=sys.stderr)
        return 2
    run_dir = argv[1]
    scene, samples = load_bundle(run_dir)
    verdicts = judge(scene, samples)
    print("[seven_gate] judging %s — %d server sample(s), agent=%s"
          % (run_dir, len(samples), scene["agent_id"]))
    if not samples:
        print("[seven_gate] NOTE: the journal is EMPTY. Every step below fails "
              "for want of evidence, which is not the same as the agent having "
              "done nothing — check the collector first.")
    # WHICH CELLS CAN ACTUALLY SAY NO — printed BEFORE the cells, because the
    # reader forms "nine things were checked" from the list, not from a footnote.
    gates = [k for k, _z, p, _w in verdicts if p is not None]
    obs = [(i, k, z) for i, (k, z, p, _w) in enumerate(verdicts, 1) if p is None]
    if obs:
        # ⚠️ THE WORDING MUST SURVIVE MORE THAN ONE OBSERVATION. It used to be
        # built as "<cell> is an OBSERVATION", which read as
        # "step1 … (報到) is, step5 … (報一步完成) is an OBSERVATION" the moment a
        # second cell was downgraded — a sentence the reader has to parse twice
        # on the one line that exists to stop a misreading.
        print("[seven_gate] NOTE: %d of the %d cells below are GATES (their fact "
              "is absent ⇒ the run is red). THE REST ARE OBSERVATIONS — %s — they "
              "print what they saw and CANNOT make this run red. Read a green run "
              "as \"the %d gates held\", never as \"%d things were verified\"."
              % (len(gates), len(verdicts),
                 ", ".join("step%d %s (%s)" % (i, k, z) for i, k, z in obs),
                 len(gates), len(verdicts)))
    failed = None
    for i, (key, zh, passed, why) in enumerate(verdicts, 1):
        mark = "OBSERVED" if passed is None else ("PASS" if passed else "FAIL")
        print("[seven_gate] step%d %-14s %s %s — %s" % (i, key, zh, mark, why))
        if passed is False and failed is None:
            failed = (i, key, zh, why)
    with open(os.path.join(run_dir, "verdict.json"), "w", encoding="utf-8") as fh:
        json.dump([{"step": i, "key": k, "zh": z, "passed": p, "evidence": w}
                   for i, (k, z, p, w) in enumerate(verdicts, 1)], fh,
                  ensure_ascii=False, indent=2)
    if failed:
        print("[seven_gate] RED — failed at step%d %s (%s): %s"
              % (failed[0], failed[1], failed[2], failed[3]))
        return 1
    print("[seven_gate] all green")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
