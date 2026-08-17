// The mock adapter's task-description write (T-e271) and its retained-revision
// series, pinned against the SERVER's rules — not against whatever the mock
// happens to do.
//
// Why this file exists at all: the cockpit's component tests run on `api/mock`,
// so every rule the mock gets wrong is a rule the UI is free to get wrong too,
// invisibly. The failure mode is asymmetric and both directions are bad — a
// mock more PERMISSIVE than the server lets a component ship a call the server
// will refuse, and a mock STRICTER than the server invents a refusal the owner
// will never actually meet.
//
// Each case below therefore names the server behaviour it mirrors, so a reader
// can check the pair rather than trust this file.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockTask } from "./mock";
import { isHttpStatus } from "./errors";
import type { TaskView } from "./adapter";
import { documentRevisions } from "../test/documentHistory";

beforeEach(() => {
  __resetMock();
});

/** Land one task the way an agent's create_task would. The mock UI never
 * fabricates tasks, so a fixture has to be injected — the same hook the rest of
 * the tasks-page tests use. */
async function seedTask(over: Partial<TaskView> = {}): Promise<string> {
  const task: TaskView = {
    id: "t-desc-fixture",
    taskNo: "T-de5c",
    title: "描述可編輯",
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

async function anyTaskId(): Promise<string> {
  return seedTask();
}

describe("mockApi · updateTaskDescription", () => {
  it("writes the description and echoes it back", async () => {
    const id = await anyTaskId();
    const after = await mockApi.updateTaskDescription(id, "更正後的敘述");
    expect(after.description).toBe("更正後的敘述");
    // And it is what a subsequent read serves — an echo that did not persist
    // would pass an echo-only assertion.
    expect((await mockApi.getTask(id)).description).toBe("更正後的敘述");
  });

  it("404s an unknown task (server: resolveTask → writeResolveError)", async () => {
    await expect(
      mockApi.updateTaskDescription("t-nope", "into the void")
    ).rejects.toSatisfy((e: unknown) => isHttpStatus(e, 404));
  });

  // 🔴 The asymmetry with every other task write. setTaskPriority and
  // removeTaskArtifact both throw 409 on a terminal task; this one must NOT,
  // because the server accepts it (owner ruling ②). A mock that "aligned" the
  // three would make the cockpit refuse a write the server takes.
  it("accepts a CLOSED task, unlike every other task write", async () => {
    const id = await anyTaskId();
    await mockApi.terminateTask(id);
    const closed = await mockApi.getTask(id);
    expect(closed.closedTs).not.toBeNull();

    const after = await mockApi.updateTaskDescription(id, "結案後的更正");
    expect(after.description).toBe("結案後的更正");

    // The control that makes this a DIFFERENCE and not just a missing check:
    // the same closed task still freezes its artifact set, and its priority.
    // Both are checked AFTER the task resolves and BEFORE anything else, so a
    // task with no artifacts still answers 409 on the freeze rather than 404.
    await expect(
      mockApi.removeTaskArtifact(id, "ta-whatever")
    ).rejects.toSatisfy((e: unknown) => isHttpStatus(e, 409));
    await expect(mockApi.setTaskPriority(id, "low")).rejects.toSatisfy(
      (e: unknown) => isHttpStatus(e, 409)
    );
  });

  it("an unchanged text is a no-op that versions nothing", async () => {
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "同一段文字");
    await mockApi.updateTaskDescription(id, "同一段文字");
    // One real change happened, and it replaced an EMPTY description — which
    // retains nothing (see below). So: zero revisions, not one recording that
    // nothing changed.
    expect(await documentRevisions(mockApi, "task_description", id)).toEqual(
      []
    );
  });

  it("retains nothing when it replaces an EMPTY description", async () => {
    // Server twin: taskDescriptionHistorySnapshot answers "{}" for "", and
    // retainDocumentVersion drops that. Most tasks are created with no
    // description, so without this rule the first correction of every task
    // would burn one of the three kept slots on emptiness.
    const id = await anyTaskId();
    expect((await mockApi.getTask(id)).description).toBe("");
    await mockApi.updateTaskDescription(id, "第一版");
    expect(await documentRevisions(mockApi, "task_description", id)).toEqual(
      []
    );
  });

  it("retains what each later write replaced, newest first", async () => {
    const id = await anyTaskId();
    for (const text of ["第一版", "第二版", "第三版"]) {
      await mockApi.updateTaskDescription(id, text);
    }
    const versions = await documentRevisions(mockApi, "task_description", id);
    // Two, not three: the first write replaced an empty description.
    expect(versions.map((v) => v.content.description)).toEqual([
      "第二版",
      "第一版",
    ]);
  });

  it("an explicit empty string CLEARS the text, and that is a versioned change", async () => {
    // Absent-vs-empty is decided on the wire (the optional `description`); by
    // the time it reaches this seam the argument is a concrete string, so ""
    // must clear rather than no-op.
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "會被清掉的文字");
    const after = await mockApi.updateTaskDescription(id, "");
    expect(after.description).toBe("");
    const versions = await documentRevisions(mockApi, "task_description", id);
    expect(versions[0].content.description).toBe("會被清掉的文字");
  });

  it("restores an earlier wording over the live task", async () => {
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "原本的說法");
    await mockApi.updateTaskDescription(id, "改壞的說法");
    const [version] = await documentRevisions(mockApi, "task_description", id);
    expect(version.content.description).toBe("原本的說法");

    await mockApi.restoreDocumentHistory("task_description", id, version.id);
    expect((await mockApi.getTask(id)).description).toBe("原本的說法");
    // The restore is itself a write, so what it overwrote is now retained —
    // server parity (SaveWithDocumentHistories wraps the restore too).
    const after = await documentRevisions(mockApi, "task_description", id);
    expect(after[0].content.description).toBe("改壞的說法");
  });

  // 🔴 T-646a (owner card rc-0fb94a25a8a8, option ①). Before that ticket this
  // seam stored the description raw while its title twin trimmed — the drift the
  // ticket removed. These three sit here, on the seam that changed, for the
  // reason the ticket's own review made concrete on the Go side: with every trim
  // assertion living somewhere else, the behaviour was silently revertible and
  // an entire green suite said nothing about it.
  it("stores the TRIMMED value, matching its title twin", async () => {
    const id = await anyTaskId();
    const after = await mockApi.updateTaskDescription(id, "  兩邊都有空白  ");
    expect(after.description).toBe("兩邊都有空白");
  });

  it("an unchanged description is a no-op that versions nothing, trimming included", async () => {
    // The server compares AFTER trimming, so re-sending a description with a
    // stray trailing space is correctly seen as no change — without that, a
    // no-op would spend one of the three retained slots saying nothing happened.
    // This is the claim a read-back cannot see: both implementations leave the
    // same text on the task, and only the revision list tells them apart.
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "原本的敘述");
    const before = await documentRevisions(mockApi, "task_description", id);
    await mockApi.updateTaskDescription(id, "  原本的敘述\n");
    expect(await documentRevisions(mockApi, "task_description", id)).toEqual(before);
  });

  it("a description of only whitespace trims to \"\" and therefore CLEARS", async () => {
    // Not a separate rule — the consequence of trimming a field whose empty
    // value is a real write. Named so it is a decision on the record rather
    // than something found in production. Its title twin REFUSES a
    // whitespace-only value instead; that asymmetry is owner card
    // rc-796541192519 ① and must not be "tidied up" on either seam.
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "先有內容");
    const after = await mockApi.updateTaskDescription(id, "   \t ");
    expect(after.description).toBe("");
  });

  it("restoring a description touches nothing else on the task", async () => {
    // Server twin: the restore branch calls writeTaskDescription, a
    // single-column write. A whole-row put would drag back the status and
    // priority the task had when that revision was written.
    const id = await anyTaskId();
    await mockApi.updateTaskDescription(id, "早期敘述");
    await mockApi.setTaskPriority(id, "low");
    await mockApi.updateTaskDescription(id, "後來的敘述");
    const [version] = await documentRevisions(mockApi, "task_description", id);

    await mockApi.restoreDocumentHistory("task_description", id, version.id);
    const task = await mockApi.getTask(id);
    expect(task.description).toBe("早期敘述");
    expect(task.priority).toBe("low");
  });
});
