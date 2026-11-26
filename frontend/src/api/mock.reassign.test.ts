// Mock adapter parity for 轉派 (POST /api/tasks/{id}/reassign, T-160e + the
// T-ba04 handover handshake). The mock is the FE's dev/test server, so it must
// reproduce the real handler's OBSERVABLE effects: the task takes the
// `reassigning` LOCK (T-9ca5 — orthogonal to its honest derived status, which
// stays in_progress; the NEW executor clears the lock on claim), waiting cards
// expire, non-terminal steps rewind to pending, the
// predecessor is STAMPED on the task (reassigned_from/_kind), the OLD outsource
// worker is KEPT LIVE (deferred handover dismiss — fired only on the takeover
// report, which the cockpit has no surface for), and an outsource target mints
// a fresh one with a server-shaped codename. BOTH sides — predecessor AND
// successor, member OR outsource — get a SYSTEM-authored (from="system", not
// the owner) pairing message wiring them into the handover dialogue.

import { describe, it, expect, beforeEach } from "vitest";
import {
  mockApi,
  __resetMock,
  __injectMockTask,
  __injectMockOutsourceWorker,
  __injectMockReplyCard,
} from "./mock";
import type {
  OutsourceWorkerView,
  ReplyCard,
  TaskStepView,
  TaskView,
} from "./adapter";
import { ApiError } from "./errors";

let seq = 0;

function mkStep(over: Partial<TaskStepView>): TaskStepView {
  seq += 1;
  return {
    id: `step-${seq}`,
    name: `step-${seq}`,
    dod: "",
    status: "pending",
    isGate: false,
    replyCardId: "",
    parallelGroup: "",
    orderIdx: seq,
    startedTs: 0,
    finishedTs: 0,
    ...over,
  };
}

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${2000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "outsource",
    executorId: "ow-old",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return {
    id: "ow-old",
    codename: "O-3",
    model: "opus",
    effort: "high",
    status: "active",
    taskId: "task-x",
    createdTs: Date.now() / 1000 - 1800,
    ...over,
  };
}

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-reassign",
    from: "mira",
    kind: "decision",
    summary: "要往哪個方向做？",
    body: "",
    options: [{ text: "A", aiPick: true }, { text: "B", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

beforeEach(() => {
  __resetMock();
  seq = 0;
});

describe("mock reassign — member target", () => {
  it("re-points the executor, enters reassigning, and notifies BOTH sides", async () => {
    const task = mkTask({
      executorKind: "staff",
      executorId: "someone-else",
      title: "交接目標",
      waitingReason: "等供應商回信",
      status: "waiting_external",
    });
    __injectMockTask(task);

    // T-91: reassign answers a receipt, so the task it left behind is READ
    // BACK. Every property below is about the STORED task, which is where the
    // cockpit reads it too.
    await mockApi.reassignTask(task.id, {
      target: { kind: "staff", memberId: "mira" },
      note: "先看 PR #12",
    });
    const after = await mockApi.getTask(task.id);

    // T-9ca5: reassigning is an ORTHOGONAL lock now, not a status — the task
    // keeps an honest derived status (in flight through the handover) and the
    // handover rides task.lock.
    expect(after.status).toBe("in_progress");
    expect(after.lock).toBe("reassigning");
    expect(after.executorKind).toBe("staff");
    expect(after.executorId).toBe("mira");
    // Leaving waiting_external clears its reason (the server does the same).
    expect(after.waitingReason).toBe("");

    // T-ba04: the predecessor is stamped on the task.
    expect(after.reassignedFrom).toBe("someone-else");
    expect(after.reassignedFromKind).toBe("staff");

    // The NEW executor is told who its predecessor is and to claim the task
    // itself. A system message, never an owner DM (T-ba04).
    const inbox = await mockApi.listChat("mira");
    expect(inbox.map((m) => ({ from: m.from, to: m.to, body: m.body }))).toEqual([
      {
        from: "system",
        to: "mira",
        body:
          "[T-2001] You are taking over this task; your predecessor is someone-else.\n\n" +
          "Receiving these instructions means there is a task you need to take over. Complete the following preparation, then claim it and start executing:\n\n" +
          "* **Read the task**: use `get_task` to read the task; if it already has steps, also check the DoD of the current step, and use `get_task_step` to read the full note of every step whose `note_size_chars` is not 0. If you have not read the corresponding task manual yet, read it with `get_task_manual`.\n" +
          "* **Confirm the handover**: if there is a `reassigned_from`, first read the `handover_note`, then use `post_chat` to confirm the current progress and in-flight items with your predecessor. Wait at most 5 minutes; if the predecessor is offline, unreachable, or has not replied in time, continue the takeover directly from the handover information on the task — do not stop here and wait.\n" +
          "* **Read-only before claiming**: before you call `claim_task`, you may only read this task; your predecessor can still write handover information until you claim it.\n" +
          "* **Claim and execute**: once preparation is done, call `claim_task` to claim the task, then continue planning or executing according to the task's current state. Reply Cards opened before the reassignment have expired automatically; for items in the handover information that still need an Owner decision, open a new card yourself.",
      },
    ]);
    const notice = inbox.map((m) => m.body).join("\n");
    // 🔴 THE NOTE NO LONGER RIDES ALONG (rc-0c36d8739b8f: 「拿掉 —— 交接備註只留
    // 在任務上」). This used to assert the opposite; the assertion was inverted
    // rather than deleted, because a stapled copy AFTER the instructions is what
    // made this document unsplittable in the first place.
    // ⚠️ Honest limit: this only pins that the NOTICE has no copy. The cockpit
    // has no handoverNote field at all today, so there is nothing on this side
    // to assert the note landed on the TASK — the server side owns that.
    expect(notice).not.toContain("先看 PR #12");

    // The OLD executor is told to stop and write the handover ONTO THE TASK.
    // 🔴 IT DOES NOT NAME THE SUCCESSOR (owner 2026-08-24: 「讓他自己去查」「不管
    // 是不是 outsource」) — an outsource successor is minted later, so naming one
    // forced a fabricated placeholder into a person's grammatical slot.
    const old = await mockApi.listChat("someone-else");
    expect(old.some((m) => m.from === "system")).toBe(true);
    const oldNotice = old.map((m) => m.body).join("\n");
    expect(oldNotice).toContain("This task has been reassigned to a new assignee");
    expect(oldNotice).toContain("write the current progress, in-flight items, risks to watch, and next steps into the task's step notes");
    expect(oldNotice).not.toContain("Mira");
  });

  it("tells a member taking over a task nobody executed that it has no predecessor", async () => {
    const task = mkTask({ executorKind: "outsource", executorId: "" });
    __injectMockTask(task);

    await mockApi.reassignTask(task.id, { target: { kind: "staff", memberId: "mira" } });

    const inbox = await mockApi.listChat("mira");
    expect(inbox.map((m) => ({ from: m.from, to: m.to, body: m.body }))).toEqual([
      {
        from: "system",
        to: "mira",
        body:
          "[T-2001] 你接手了這張任務，這張任務沒有前任。\n\n" +
          "Receiving these instructions means there is a task you need to take over. Complete the following preparation, then claim it and start executing:\n\n" +
          "* **Read the task**: use `get_task` to read the task; if it already has steps, also check the DoD of the current step, and use `get_task_step` to read the full note of every step whose `note_size_chars` is not 0. If you have not read the corresponding task manual yet, read it with `get_task_manual`.\n" +
          "* **Confirm the handover**: if there is a `reassigned_from`, first read the `handover_note`, then use `post_chat` to confirm the current progress and in-flight items with your predecessor. Wait at most 5 minutes; if the predecessor is offline, unreachable, or has not replied in time, continue the takeover directly from the handover information on the task — do not stop here and wait.\n" +
          "* **Read-only before claiming**: before you call `claim_task`, you may only read this task; your predecessor can still write handover information until you claim it.\n" +
          "* **Claim and execute**: once preparation is done, call `claim_task` to claim the task, then continue planning or executing according to the task's current state. Reply Cards opened before the reassignment have expired automatically; for items in the handover information that still need an Owner decision, open a new card yourself.",
      },
    ]);
  });

  it("expires the task's waiting cards and rewinds non-terminal steps", async () => {
    const card = mkCard({});
    const task = mkTask({
      executorKind: "staff",
      executorId: "someone-else",
      steps: [
        mkStep({ name: "done-one", status: "done" }),
        mkStep({ name: "history", status: "superseded" }),
        mkStep({ name: "live", status: "in_progress" }),
        mkStep({ name: "gate", status: "waiting_owner", replyCardId: card.id }),
      ],
    });
    __injectMockTask(task);
    __injectMockReplyCard(card);

    await mockApi.reassignTask(task.id, {
      target: { kind: "staff", memberId: "mira" },
    });

    // Terminal step rows are history and survive; everything else rewinds.
    const after = await mockApi.getTask(task.id);
    expect(after.steps.map((s) => s.status)).toEqual([
      "done",
      "superseded",
      "pending",
      "pending",
    ]);
    // The question was addressed to the OLD executor → its card expires.
    const expired = await mockApi.listReplyCards("expired");
    expect(expired.map((c) => c.id)).toContain(card.id);
  });

  it("rejects a warden / unknown / already-executor member target", async () => {
    const task = mkTask({ executorKind: "staff", executorId: "mira" });
    __injectMockTask(task);

    // Machines never execute tasks.
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "staff", memberId: "warden-mbp5" },
      })
    ).rejects.toMatchObject({ status: 400 });
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "staff", memberId: "nobody" },
      })
    ).rejects.toMatchObject({ status: 400 });
    // A no-op reassign is a conflict, not a silent success.
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "staff", memberId: "mira" },
      })
    ).rejects.toMatchObject({ status: 409 });
  });
});

describe("mock reassign — outsource target", () => {
  it("keeps the old worker live (deferred handover dismiss), stamps the predecessor, mints a fresh one, and notifies only the predecessor", async () => {
    const task = mkTask({ title: "轉外包" });
    __injectMockTask(task);
    __injectMockOutsourceWorker(mkWorker({ taskId: task.id }));

    await mockApi.reassignTask(task.id, {
      target: { kind: "outsource", model: "opus", effort: "high", machine: "mach-a" },
    });
    const after = await mockApi.getTask(task.id);

    expect(after.status).toBe("in_progress");
    expect(after.lock).toBe("reassigning");
    expect(after.executorKind).toBe("outsource");
    // T-ba04: the predecessor is stamped so the successor / cockpit can name it.
    expect(after.reassignedFrom).toBe("ow-old");
    expect(after.reassignedFromKind).toBe("outsource");

    const workers = await mockApi.listOutsourceWorkers();
    // BOTH workers are on the task now — the old one is NO LONGER dismissed at
    // reassign time (it stays live through the reassigning hold to hand over
    // with the successor); the real server fires it on the takeover report.
    expect(workers.filter((w) => w.taskId === task.id)).toHaveLength(2);
    const minted = workers.find((w) => w.id === after.executorId)!;
    expect(minted.model).toBe("opus");
    expect(minted.effort).toBe("high");
    expect(minted.status).toBe("assigned");
    // Codename: the O family's MAX+1 over every codename ISSUED, the still-live
    // O-3 included — a codename is never reused.
    expect(minted.codename).toBe("O-4");

    // The OLD outsource worker (now kept live) is told to hand over — a system
    // message, not an owner DM.
    const old = await mockApi.listChat("ow-old");
    expect(old.map((m) => ({ from: m.from, to: m.to, body: m.body }))).toEqual([
      {
        from: "system",
        to: "ow-old",
        body:
          "[T-2001] This task has been reassigned to a new assignee.\n\nReceiving these instructions means the current task needs to be handed over to another executor. Stop moving it forward and complete the necessary wrap-up, so the new assignee can get the current results and the full context remotely:\n" +
          "\n" +
          "* Preserve results: push the git commits that need to be kept to the remote; upload the files that need to be kept with `ocagent upload` and write the attachment ids into the step notes. Do not leave results that only this machine can reach.\n" +
          "* Write the handover: write the current progress, in-flight items, risks to watch, and next steps into the task's step notes; you can keep adding to them until the new assignee claims the task, after which they can no longer be written. Reply Cards opened before the reassignment have expired automatically; if anything is still waiting on an Owner decision or action, write it into the step notes, and the new assignee will open a new card after claiming. During the handover, do not open any more Reply Cards bound to this task.\n" +
          "* Handle sub-agents: if any sub-agent is running, ask it to wrap up and write its results back to the corresponding task step.\n" +
          "\n" +
          "Once the above is done, the handover is complete. If the new assignee is online and reaches out, confirm any details then; otherwise there is no need to wait for or go looking for the new assignee.",
      },
    ]);
    // The minted worker is told nothing: it finds the task through its boot
    // sequence's reassigning-lock check.
    expect(await mockApi.listChat(minted.id)).toEqual([]);
  });

  it("defaults a blank effort to medium and rejects an out-of-vocabulary one", async () => {
    const task = mkTask({});
    __injectMockTask(task);

    await mockApi.reassignTask(task.id, {
      target: { kind: "outsource", model: "", effort: "", machine: "mach-a" },
    });
    const after = await mockApi.getTask(task.id);
    const workers = await mockApi.listOutsourceWorkers();
    expect(workers.find((w) => w.id === after.executorId)?.effort).toBe("medium");
    expect(workers.find((w) => w.id === after.executorId)?.codename).toBe("O-1");

    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "outsource", model: "opus", effort: "extreme", machine: "mach-a" },
      })
    ).rejects.toMatchObject({ status: 400 });
  });
});

describe("mock reassign — task-level guards", () => {
  it("refuses a closed task (409) but reassigns a frozen one", async () => {
    const closed = mkTask({ status: "done", closedTs: Date.now() / 1000 });
    const frozen = mkTask({ priority: "frozen" });
    __injectMockTask(closed);
    __injectMockTask(frozen);

    await expect(
      mockApi.reassignTask(closed.id, {
        target: { kind: "staff", memberId: "mira" },
      })
    ).rejects.toMatchObject({ status: 409 });

    // 🔴 This used to assert 400 on the frozen one — the OLD behaviour, ruled
    // away by owner on 2026-08-11 (T-b9f6). Inverted rather than deleted so the
    // next reader sees the protection was removed on purpose, not lost. Frozen
    // still means "do not advance this": the reassign only ARRANGES the
    // handover, and the scheduler still refuses to wake anybody.
    await mockApi.reassignTask(frozen.id, {
      target: { kind: "staff", memberId: "mira" },
    });
  });

  it("404s an unknown task", async () => {
    await expect(
      mockApi.reassignTask("task-ghost", {
        target: { kind: "staff", memberId: "mira" },
      })
    ).rejects.toBeInstanceOf(ApiError);
  });
});
