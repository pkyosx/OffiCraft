// Mock adapter parity for the task-card message box (owner → executor, POST
// /api/tasks/{id}/message). The server stores the TRIMMED body prefixed with
// the task's display number ([T-xxxx]); the mock must produce the identical
// string so FE dev/tests reflect real behavior. This pins the trim + prefix
// against a body that carries leading/trailing whitespace.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import type { TaskView } from "./adapter";

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-msg-parity",
    taskNo: "T-abcd",
    title: "parity target",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
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

describe("mock task message box — server parity", () => {
  beforeEach(() => __resetMock());

  it("stores the TRIMMED body prefixed with the task number (matches the server)", async () => {
    const task = mkTask({});
    __injectMockTask(task);

    await mockApi.postTaskMessage(task.id, { body: "  先做 P0 的部分  " });

    const thread = await mockApi.peekChat("mira");
    expect(thread.map((m) => m.body)).toContain("[T-abcd] 先做 P0 的部分");
  });

  it("keeps an attachment-only message body empty (no prefix)", async () => {
    const task = mkTask({});
    __injectMockTask(task);

    await mockApi.postTaskMessage(task.id, {
      body: "   ",
      attachments: [{ dataB64: "Zm9v", mime: "text/plain", filename: "f.txt" }],
    });

    const thread = await mockApi.peekChat("mira");
    expect(thread.map((m) => m.body)).toContain("");
  });
});
