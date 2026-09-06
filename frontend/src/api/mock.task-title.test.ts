// The mock adapter's task-TITLE write (T-2ebe) and its retained-revision
// series, pinned against the SERVER's rules — not against whatever the mock
// happens to do, and not against its description twin either.
//
// Why this file exists alongside mock.task-description.test.ts: the two routes
// are deliberately near-identical, which is exactly what makes the ONE place
// they differ easy to lose. A blank description CLEARS the field; a blank title
// is a 400. Every case below names the server behaviour it mirrors, so a reader
// can check the pair rather than trust this file.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import { isHttpStatus } from "./errors";
import { ApiError } from "./errors";
import type { TaskView } from "./adapter";
import { documentRevisions } from "../test/documentHistory";

beforeEach(() => {
  __resetMock();
});

function seedTask(over: Partial<TaskView> = {}): string {
  const task: TaskView = {
    id: "t-title-fixture",
    taskNo: "T-2ebe",
    title: "原本的標題",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "high",
    executorKind: "member",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 1,
    steps: [],
    ...over,
  } as TaskView;
  __injectMockTask(task);
  return task.id;
}

describe("mockApi · updateTaskTitle", () => {
  it("writes the title, and a read serves it back", async () => {
    // T-91: the write answers a receipt, so there is no echo left to assert.
    // The read-back was ALWAYS the stronger half — an echo that did not persist
    // would have passed the echo-only assertion — and it is now the whole test.
    const id = seedTask();
    await mockApi.updateTaskTitle(id, "更正後的標題");
    expect((await mockApi.getTask(id)).title).toBe("更正後的標題");
  });

  it("404s an unknown task (server: resolveTask → writeResolveError)", async () => {
    await expect(
      mockApi.updateTaskTitle("t-nope", "into the void")
    ).rejects.toSatisfy((e: unknown) => isHttpStatus(e, 404));
  });

  // 🔴 THE difference from the description twin, and the reason this file is
  // separate from it. create_task has always refused a blank title, so an edit
  // door that cleared one would let a caller reach a task-list row with nothing
  // in it — the very surface this capability exists to keep true.
  it("400s a blank title instead of clearing the field", async () => {
    const id = seedTask();
    for (const blank of ["", "   ", "\n\t "]) {
      const err = await mockApi.updateTaskTitle(id, blank).catch((e) => e);
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).status).toBe(400);
      expect((err as ApiError).serverMessage).toBe("title must not be blank");
    }
    // The refusal is a refusal, not a partially applied write.
    expect((await mockApi.getTask(id)).title).toBe("原本的標題");
    expect(await documentRevisions(mockApi, "task_title", id)).toEqual([]);
  });

  it("stores the TRIMMED value, matching create_task", async () => {
    const id = seedTask();
    await mockApi.updateTaskTitle(id, "  兩邊都有空白  ");
    expect((await mockApi.getTask(id)).title).toBe("兩邊都有空白");
  });

  it("an unchanged title is a no-op that versions nothing, trimming included", async () => {
    // The server compares AFTER trimming, so re-sending a title with a stray
    // trailing space is correctly seen as no change — without that, a no-op
    // would spend one of the three retained slots saying nothing happened.
    const id = seedTask();
    await mockApi.updateTaskTitle(id, "原本的標題");
    await mockApi.updateTaskTitle(id, "  原本的標題 ");
    expect(await documentRevisions(mockApi, "task_title", id)).toEqual([]);
  });

  // 🔴 The asymmetry with every other task write, inherited from the twin.
  it("accepts a CLOSED task, unlike every other task write", async () => {
    const id = seedTask();
    await mockApi.terminateTask(id);
    expect((await mockApi.getTask(id)).closedTs).not.toBeNull();

    await mockApi.updateTaskTitle(id, "結案後的更正");
    expect((await mockApi.getTask(id)).title).toBe("結案後的更正");

    // The control that makes this a DIFFERENCE and not a missing check: the
    // same closed task still freezes its artifact set and its priority.
    await expect(
      mockApi.removeTaskArtifact(id, "ta-whatever")
    ).rejects.toSatisfy((e: unknown) => isHttpStatus(e, 409));
    await expect(mockApi.setTaskPriority(id, "low")).rejects.toSatisfy(
      (e: unknown) => isHttpStatus(e, 409)
    );
  });

  it("retains what each write replaced, newest first — the FIRST one included", async () => {
    // Unlike the description series, there is no empty-string branch to skip
    // over: a task cannot have a blank title, so the very first correction
    // already replaces something worth keeping. Server twin:
    // taskTitleHistorySnapshot has no "" arm, deliberately.
    const id = seedTask();
    for (const text of ["第二版", "第三版", "第四版"]) {
      await mockApi.updateTaskTitle(id, text);
    }
    const versions = await documentRevisions(mockApi, "task_title", id);
    expect(versions.map((v) => v.content.title)).toEqual([
      "第三版",
      "第二版",
      "原本的標題",
    ]);
  });

  it("restores an earlier wording over the live task", async () => {
    const id = seedTask();
    await mockApi.updateTaskTitle(id, "改壞的標題");
    const [version] = await documentRevisions(mockApi, "task_title", id);
    expect(version.content.title).toBe("原本的標題");

    await mockApi.restoreDocumentHistory("task_title", id, version.id);
    expect((await mockApi.getTask(id)).title).toBe("原本的標題");
    // The restore is itself a write, so what it overwrote is now retained.
    const after = await documentRevisions(mockApi, "task_title", id);
    expect(after[0].content.title).toBe("改壞的標題");
  });

  it("keeps its series separate from the description's over the SAME key", async () => {
    // Both kinds are keyed on the task id. If they shared a slot, correcting a
    // title would silently consume the description's retained revisions — and
    // restoring one would write back the other's field.
    const id = seedTask();
    await mockApi.updateTaskDescription(id, "第一版敘述");
    await mockApi.updateTaskDescription(id, "第二版敘述");
    await mockApi.updateTaskTitle(id, "新標題");

    expect(
      (await documentRevisions(mockApi, "task_title", id)).map(
        (v) => v.content.title
      )
    ).toEqual(["原本的標題"]);
    expect(
      (await documentRevisions(mockApi, "task_description", id)).map(
        (v) => v.content.description
      )
    ).toEqual(["第一版敘述"]);

    const [titleVersion] = await documentRevisions(mockApi, "task_title", id);
    await mockApi.restoreDocumentHistory("task_title", id, titleVersion.id);
    const task = await mockApi.getTask(id);
    expect(task.title).toBe("原本的標題");
    // Restoring the title touches nothing else on the task.
    expect(task.description).toBe("第二版敘述");
    expect(task.priority).toBe("high");
  });
});
