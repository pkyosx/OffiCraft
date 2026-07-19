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
    options: ["A", "B"],
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
      executorKind: "member",
      executorId: "someone-else",
      title: "交接目標",
      waitingReason: "等供應商回信",
      status: "waiting_external",
    });
    __injectMockTask(task);

    const after = await mockApi.reassignTask(task.id, {
      target: { kind: "member", memberId: "mira" },
      note: "先看 PR #12",
    });

    // T-9ca5: reassigning is an ORTHOGONAL lock now, not a status — the task
    // keeps an honest derived status (in flight through the handover) and the
    // handover rides task.lock.
    expect(after.status).toBe("in_progress");
    expect(after.lock).toBe("reassigning");
    expect(after.executorKind).toBe("member");
    expect(after.executorId).toBe("mira");
    // Leaving waiting_external clears its reason (the server does the same).
    expect(after.waitingReason).toBe("");

    // T-ba04: the predecessor is stamped on the task.
    expect(after.reassignedFrom).toBe("someone-else");
    expect(after.reassignedFromKind).toBe("member");

    // The NEW executor is told who its predecessor is, to confirm the handover
    // WITH them, then flip the status back themselves — the note rides along. A
    // system message, never an owner DM (T-ba04).
    const inbox = await mockApi.peekChat("mira");
    expect(inbox.some((m) => m.from === "system")).toBe(true);
    const notice = inbox.map((m) => m.body).join("\n");
    expect(notice).toContain(task.taskNo);
    expect(notice).toContain("你的前任是");
    expect(notice).toContain("reassigning→in_progress");
    expect(notice).toContain("先看 PR #12");

    // The OLD executor is told to go hand over to the successor (also system).
    const old = await mockApi.peekChat("someone-else");
    expect(old.some((m) => m.from === "system")).toBe(true);
    expect(old.map((m) => m.body).join("\n")).toContain("已轉派給 Mira");
  });

  it("expires the task's waiting cards and rewinds non-terminal steps", async () => {
    const card = mkCard({});
    const task = mkTask({
      executorKind: "member",
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
      target: { kind: "member", memberId: "mira" },
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
    const task = mkTask({ executorKind: "member", executorId: "mira" });
    __injectMockTask(task);

    // Machines never execute tasks.
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "member", memberId: "warden-mbp5" },
      })
    ).rejects.toMatchObject({ status: 400 });
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "member", memberId: "nobody" },
      })
    ).rejects.toMatchObject({ status: 400 });
    // A no-op reassign is a conflict, not a silent success.
    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "member", memberId: "mira" },
      })
    ).rejects.toMatchObject({ status: 409 });
  });
});

describe("mock reassign — outsource target", () => {
  it("keeps the old worker live (deferred handover dismiss), stamps the predecessor, mints a fresh one, and pairs both", async () => {
    const task = mkTask({ title: "轉外包" });
    __injectMockTask(task);
    __injectMockOutsourceWorker(mkWorker({ taskId: task.id }));

    const after = await mockApi.reassignTask(task.id, {
      target: { kind: "outsource", model: "opus", effort: "high", machine: "auto" },
    });

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
    const old = await mockApi.peekChat("ow-old");
    expect(old.some((m) => m.from === "system")).toBe(true);
    expect(old.map((m) => m.body).join("\n")).toContain("此任務已轉派給");
    // The freshly-minted worker gets its OWN pairing message (it used to get
    // none) naming its predecessor + the self-flip protocol.
    const mintedInbox = await mockApi.peekChat(minted.id);
    const mintedNotice = mintedInbox.map((m) => m.body).join("\n");
    expect(mintedInbox.some((m) => m.from === "system")).toBe(true);
    expect(mintedNotice).toContain("你的前任是");
    expect(mintedNotice).toContain("reassigning→in_progress");
  });

  it("defaults a blank effort to medium and rejects an out-of-vocabulary one", async () => {
    const task = mkTask({});
    __injectMockTask(task);

    const after = await mockApi.reassignTask(task.id, {
      target: { kind: "outsource", model: "", effort: "", machine: "auto" },
    });
    const workers = await mockApi.listOutsourceWorkers();
    expect(workers.find((w) => w.id === after.executorId)?.effort).toBe("medium");
    // A blank model keeps the honest X family (no model → no family letter).
    expect(workers.find((w) => w.id === after.executorId)?.codename).toBe("X-1");

    await expect(
      mockApi.reassignTask(task.id, {
        target: { kind: "outsource", model: "opus", effort: "extreme", machine: "auto" },
      })
    ).rejects.toMatchObject({ status: 400 });
  });
});

describe("mock reassign — task-level guards", () => {
  it("refuses a closed task (409) and a frozen one (400)", async () => {
    const closed = mkTask({ status: "done", closedTs: Date.now() / 1000 });
    const frozen = mkTask({ priority: "frozen" });
    __injectMockTask(closed);
    __injectMockTask(frozen);

    await expect(
      mockApi.reassignTask(closed.id, {
        target: { kind: "member", memberId: "mira" },
      })
    ).rejects.toMatchObject({ status: 409 });
    await expect(
      mockApi.reassignTask(frozen.id, {
        target: { kind: "member", memberId: "mira" },
      })
    ).rejects.toMatchObject({ status: 400 });
  });

  it("404s an unknown task", async () => {
    await expect(
      mockApi.reassignTask("task-ghost", {
        target: { kind: "member", memberId: "mira" },
      })
    ).rejects.toBeInstanceOf(ApiError);
  });
});
