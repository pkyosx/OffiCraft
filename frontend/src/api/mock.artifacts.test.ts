// Mock adapter parity for the task artifact set (T-3dc5 / T-66 / T-92). The
// mock is the FE's dev/test server, so it must reproduce the real handler's
// OBSERVABLE effects: listTasks strips the artifact rows but keeps the count
// (the light-list badge source), getTask carries THE COUNT AND NOTHING ELSE
// (T-92 took even the id+label index off the task read), listTaskArtifacts
// answers the full rows, and removeTaskArtifact un-pins one row (404 on an
// unknown id) leaving the count consistent.
//
// 🔴 THE BADGE AND THE PANEL READ TWO DIFFERENT ENDPOINTS, so the pair has to
// be checked as a pair. `listTaskArtifacts` used to `return []` outright, and
// nothing here noticed, because every case only ever read the task. The visible
// result in mock mode was a badge saying 「產物 N」 over a panel saying 「還沒有
// 產物」 — the one thing TaskArtifactsPopover's own comment says it must never
// do. The count/rows agreement case below is what makes that state red.

import { describe, it, expect, beforeEach } from "vitest";
import {
  mockApi,
  __resetMock,
  __injectMockTask,
  __injectMockArtifactVersions,
} from "./mock";
import type { MockTaskRow } from "./mock";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
} from "./adapter";
import { ApiError } from "./errors";

function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "link",
    url: "https://x/pr/1",
    name: "PR #1",
    description: "",
    mime: "",
    createdTs: 0,
    createdBy: "mira",
    versionCount: 1,
    ...over,
  };
}

function mkVersion(over: Partial<TaskArtifactVersionView>): TaskArtifactVersionView {
  return {
    id: 1,
    kind: "link",
    url: "https://x/pr/0",
    name: "PR #0",
    description: "",
    filename: "",
    mime: "",
    isImage: false,
    attachmentId: "",
    createdTs: 0,
    createdBy: "mira",
    ...over,
  };
}

function mkTask(over: Partial<MockTaskRow>): MockTaskRow {
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

/** The artifact rows a task response carries — which since T-92 is NONE, on
 * either projection. Read through a widened view because the field is off the
 * view model entirely: the assertion is precisely that nothing answers here. */
function artifactRowsOn(task: object): unknown {
  return (task as Record<string, unknown>).artifacts;
}

beforeEach(() => __resetMock());

describe("mock task artifacts", () => {
  it("listTasks strips the artifact rows but keeps the count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    const rows = await mockApi.listTasks();
    const row = rows.find((t) => t.id === "task-art")!;
    // The store row holds each artifact WHOLE; the light projection carries a
    // count and no rows (server parity, and the same narrowing `getTask` does
    // by destructuring). A light row that hands the stored artifacts back makes
    // mock mode the one place a task LIST carries url/mime — a cockpit could
    // render from it here and find nothing against a real server.
    expect(artifactRowsOn(row)).toBeUndefined();
    expect(row.artifactCount).toBe(2);
  });

  it("getTask carries the artifact COUNT and no rows at all", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1", name: "PR #1" })] }),
    );
    const full = await mockApi.getTask("task-art");
    expect(full.artifactCount).toBe(1);
    // T-92: not even the ids. The store row is whole, so a pass-through here
    // would type-check and make mock mode the one place a task read carries
    // url/mime/name — and a cockpit that read them would 404 for real.
    expect(artifactRowsOn(full)).toBeUndefined();
  });

  // 🔴 The case that reddens on `return []`. MEASURED (mutant planted in place,
  // `findTask(taskId); return []`): 4 failed / 8 passed — this one, the
  // count/rows agreement, the clone case and the post-remove read. Before those
  // four existed the same mutant was the SHIPPED code and this file was green,
  // which is precisely how the empty stub survived review.
  it("listTaskArtifacts answers the task's rows IN FULL, not an empty set", async () => {
    __injectMockTask(
      mkTask({
        artifacts: [
          mkArtifact({ id: "ta-1", name: "PR #1" }),
          mkArtifact({
            id: "ta-2",
            kind: "file",
            url: "/api/chat/attachment/att-2",
            name: "design.md",
            description: "評審用的設計稿",
            mime: "text/markdown",
          }),
        ],
      }),
    );
    const rows = await mockApi.listTaskArtifacts("task-art");
    expect(rows.map((a) => a.id)).toEqual(["ta-1", "ta-2"]);
    // Each row is the WHOLE deliverable — the fields the panel draws from and
    // the task read does not carry at all.
    expect(rows[1]).toMatchObject({
      kind: "file",
      url: "/api/chat/attachment/att-2",
      name: "design.md",
      description: "評審用的設計稿",
      mime: "text/markdown",
    });
  });

  it("the badge count and the panel's rows agree on the same task", async () => {
    __injectMockTask(
      mkTask({
        artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })],
      }),
    );
    // The badge reads the count off the task; the panel fetches its own rows.
    // 「產物 N」 over 「還沒有產物」 is exactly the disagreement this forbids — and
    // since T-92 the count is the ONLY thing the two reads have in common, so
    // this is the whole of what keeps them honest.
    const full = await mockApi.getTask("task-art");
    const rows = await mockApi.listTaskArtifacts("task-art");
    expect(rows.length).toBe(full.artifactCount);
    expect(rows.map((a) => a.id)).toEqual(["ta-1", "ta-2"]);
  });

  it("listTaskArtifacts on an unknown task is a 404, never a silent []", async () => {
    await expect(
      mockApi.listTaskArtifacts("task-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });

  it("listTaskArtifacts hands back a clone the caller cannot write through", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    const rows = await mockApi.listTaskArtifacts("task-art");
    rows[0].name = "mutated";
    const again = await mockApi.listTaskArtifacts("task-art");
    expect(again[0].name).toBe("PR #1");
  });

  it("removeTaskArtifact drops the row from the panel read too", async () => {
    __injectMockTask(
      mkTask({
        artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })],
      }),
    );
    await mockApi.removeTaskArtifact("task-art", "ta-1");
    const rows = await mockApi.listTaskArtifacts("task-art");
    expect(rows.map((a) => a.id)).toEqual(["ta-2"]);
  });

  it("removeTaskArtifact un-pins one row and reports the fresh count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    await mockApi.removeTaskArtifact("task-art", "ta-1");
    // The write itself is a bounded receipt (T-a98d), so the fresh set is read
    // back the way the cockpit reads it — the count off the task, the rows off
    // the artifact endpoint, which since T-92 is the only place they live.
    const after = await mockApi.getTask("task-art");
    expect(after.artifactCount).toBe(1);
    expect((await mockApi.listTaskArtifacts("task-art")).map((a) => a.id)).toEqual(["ta-2"]);
  });

  it("removeTaskArtifact on an unknown artifact is a 404", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    await expect(
      mockApi.removeTaskArtifact("task-art", "ta-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });

  // T-2654 — the mock must REFUSE where production refuses. It used to delete
  // on a closed task while the server 409s, and the mock cockpit is how UI
  // changes get checked, so the parity gap made a broken flow look correct.
  // The 409 comes before the artifact lookup, same order as the server.
  it.each(["done", "terminated", "duplicated"] as const)(
    "removeTaskArtifact on a %s task is a 409 and leaves the artifact pinned",
    async (status) => {
      __injectMockTask(
        mkTask({ status, closedTs: 3000, artifacts: [mkArtifact({ id: "ta-1" })] }),
      );
      await expect(
        mockApi.removeTaskArtifact("task-art", "ta-1"),
      ).rejects.toMatchObject({ status: 409 } as Partial<ApiError>);
      const still = await mockApi.listTaskArtifacts("task-art");
      expect(still.map((a) => a.id)).toEqual(["ta-1"]);
      expect((await mockApi.getTask("task-art")).artifactCount).toBe(1);
    },
  );
});

describe("mock task artifact versions (T-60)", () => {
  it("lists the retained versions of a replaced deliverable, newest first", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1", versionCount: 3 })] }));
    __injectMockArtifactVersions("ta-1", [
      mkVersion({ id: 2, url: "https://x/pr/2" }),
      mkVersion({ id: 1, url: "https://x/pr/1" }),
    ]);
    const versions = await mockApi.listTaskArtifactVersions("task-art", "ta-1");
    expect(versions.map((v) => v.id)).toEqual([2, 1]);
    expect(versions.map((v) => v.url)).toEqual(["https://x/pr/2", "https://x/pr/1"]);
  });

  it("answers an empty list for an artifact that was never replaced", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    expect(await mockApi.listTaskArtifactVersions("task-art", "ta-1")).toEqual([]);
  });

  it("is a 404 for an artifact that is not pinned on the task", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    await expect(
      mockApi.listTaskArtifactVersions("task-art", "ta-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });

  // Server parity: un-pinning deletes the versions in the same transaction, so
  // a version list can never outlive the artifact it belongs to.
  it("drops the retained versions when the artifact is un-pinned", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }));
    __injectMockArtifactVersions("ta-1", [mkVersion({ id: 1 })]);
    await mockApi.removeTaskArtifact("task-art", "ta-1");
    __injectMockTask(mkTask({ id: "task-art-2", artifacts: [mkArtifact({ id: "ta-1" })] }));
    expect(await mockApi.listTaskArtifactVersions("task-art-2", "ta-1")).toEqual([]);
  });
});
