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
});

describe("mockApi.setTaskDue", () => {
  it("stores and clears the due date", async () => {
    const id = seedTask();
    await mockApi.setTaskDue(id, 1793375999);
    expect((await mockApi.getTask(id)).dueTs).toBe(1793375999);
    await mockApi.setTaskDue(id, null);
    expect((await mockApi.getTask(id)).dueTs).toBeNull();
  });

  it("refuses a closed task with a 409", async () => {
    const id = seedTask({ status: "done", closedTs: NOW - 10 });
    const err = await mockApi.setTaskDue(id, 1793375999).catch((e) => e);
    expect(isHttpStatus(err, 409)).toBe(true);
  });
});
