// Mock adapter parity for the task artifact set (T-3dc5). The mock is the FE's
// dev/test server, so it must reproduce the real handler's OBSERVABLE effects:
// listTasks strips the artifact rows but keeps the count (the light-list badge
// source), getTask folds the full set, and removeTaskArtifact un-pins one row
// (404 on an unknown id) leaving the count consistent.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import type { TaskArtifactView, TaskView } from "./adapter";
import { ApiError } from "./errors";

function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "link",
    url: "https://x/pr/1",
    label: "PR #1",
    filename: "",
    mime: "",
    isImage: false,
    attachmentId: "",
    createdTs: 0,
    createdBy: "mira",
    ...over,
  };
}

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-art",
    taskNo: "T-9001",
    title: "artifact task",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "owner",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    artifacts: [],
    artifactCount: 0,
    ...over,
  };
}

beforeEach(() => __resetMock());

describe("mock task artifacts", () => {
  it("listTasks strips the artifact rows but keeps the count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    const rows = await mockApi.listTasks();
    const row = rows.find((t) => t.id === "task-art")!;
    expect(row.artifacts).toEqual([]);
    expect(row.artifactCount).toBe(2);
  });

  it("getTask folds the full artifact set with count == length", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    const full = await mockApi.getTask("task-art");
    expect(full.artifacts?.length).toBe(1);
    expect(full.artifactCount).toBe(1);
  });

  it("removeTaskArtifact un-pins one row and reports the fresh count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    const after = await mockApi.removeTaskArtifact("task-art", "ta-1");
    expect(after.artifacts?.map((a) => a.id)).toEqual(["ta-2"]);
    expect(after.artifactCount).toBe(1);
  });

  it("removeTaskArtifact on an unknown artifact is a 404", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    await expect(
      mockApi.removeTaskArtifact("task-art", "ta-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });
});
