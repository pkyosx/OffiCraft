import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import { isHttpStatus } from "./errors";
import type { TaskView } from "./adapter";

const NOW = 1792807200;

let seq = 0;
function seedTask(over: Partial<TaskView> = {}): string {
  seq += 1;
  const task: TaskView = {
    id: `t-due-${seq}`,
    taskNo: `T-${seq}`,
    title: "到期",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: NOW - 3600,
    updatedTs: NOW - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
  __injectMockTask(task);
  return task.id;
}

beforeEach(() => {
  __resetMock();
  vi.useFakeTimers({ now: NOW * 1000 });
});
afterEach(() => {
  vi.useRealTimers();
});

describe("mockApi.getTaskCount", () => {
  it("counts open overdue and due-soon tasks and names the next change moment", async () => {
    seedTask({ dueTs: NOW - 60 });
    seedTask({ dueTs: NOW + 3600 });
    seedTask({ dueTs: NOW + 2 * 86_400 });
    seedTask({ status: "done", closedTs: NOW - 10, dueTs: NOW - 60 });
    seedTask();
    expect(await mockApi.getTaskCount()).toEqual({
      open: 4,
      total: 5,
      overdue: 1,
      dueSoon: 1,
      dueNextChangeTs: NOW + 3600,
    });
  });

  it("names the 24-hour window entry when it comes before any due moment", async () => {
    seedTask({ dueTs: NOW + 86_400 + 600 });
    expect((await mockApi.getTaskCount()).dueNextChangeTs).toBe(NOW + 600);
  });

  it("judges each task by its own lead, else the workspace lead", async () => {
    await mockApi.patchServerSettings({ taskDueSoonLeadDays: 3 });
    seedTask({ dueTs: NOW + 2 * 86_400 });
    seedTask({ dueTs: NOW + 5 * 86_400, dueSoonLeadDays: 7 });
    seedTask({ dueTs: NOW + 2 * 86_400, dueSoonLeadDays: 1 });
    expect(await mockApi.getTaskCount()).toEqual({
      open: 3,
      total: 3,
      overdue: 0,
      dueSoon: 2,
      dueNextChangeTs: NOW + 86_400,
    });
  });
});

describe("mockApi.patchServerSettings", () => {
  it("announces a changed workspace lead on the task topic", async () => {
    const topics: string[] = [];
    const off = mockApi.subscribeEvents((topic) => topics.push(topic));
    await mockApi.patchServerSettings({ taskDueSoonLeadDays: 5 });
    off();
    expect(topics).toEqual(["task"]);
  });

  it("refuses a workspace lead outside 1..30 with a 422", async () => {
    const err = await mockApi
      .patchServerSettings({ taskDueSoonLeadDays: 31 })
      .catch((e) => e);
    expect(isHttpStatus(err, 422)).toBe(true);
  });
});

describe("mockApi.setTaskDue", () => {
  it("stores and clears the due date", async () => {
    const id = seedTask();
    await mockApi.setTaskDue(id, 1793375999);
    expect((await mockApi.getTask(id)).dueTs).toBe(1793375999);
    await mockApi.setTaskDue(id, null);
    expect((await mockApi.getTask(id)).dueTs).toBeNull();
  });

  it("keeps the lead when only the date is sent and clears it with the date", async () => {
    const id = seedTask();
    await mockApi.setTaskDue(id, 1793375999, 7);
    await mockApi.setTaskDue(id, 1793462399);
    expect((await mockApi.getTask(id)).dueSoonLeadDays).toBe(7);
    await mockApi.setTaskDue(id, null);
    expect((await mockApi.getTask(id)).dueSoonLeadDays).toBeNull();
  });

  it("refuses a closed task with a 409", async () => {
    const id = seedTask({ status: "done", closedTs: NOW - 10 });
    const err = await mockApi.setTaskDue(id, 1793375999).catch((e) => e);
    expect(isHttpStatus(err, 409)).toBe(true);
  });
});
