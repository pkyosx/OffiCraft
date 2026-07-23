// T-3451: the member→current-task join logic (deriveMemberCurrentTasks). A 正職
// member carries no task on its own DTO, so the office page derives it from the
// task list. Locked here: only a member's OPEN task counts, and the FRESHEST
// (max updated_ts) wins when a member holds several (owner 2026-07-23: a member
// may hold multiple open tasks; show the freshest).
import { describe, it, expect } from "vitest";
import { deriveMemberCurrentTasks } from "./useMemberCurrentTasks";
import type { TaskView } from "../api/adapter";

// Minimal builder — deriveMemberCurrentTasks reads only executorKind /
// executorId / closedTs / updatedTs / id / taskNo / title, so the rest is
// filled with honest zero-values to satisfy the type without noise.
function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "t-x",
    taskNo: "T-x",
    title: "some title",
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
    createdTs: 0,
    updatedTs: 0,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

describe("deriveMemberCurrentTasks", () => {
  it("maps a member.id → its open task's title/no/id", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ id: "t-1", taskNo: "T-1", title: "改 UI", executorId: "mira" }),
    ]);
    expect(map.get("mira")).toEqual({
      taskId: "t-1",
      taskNo: "T-1",
      title: "改 UI",
    });
  });

  it("among several OPEN tasks for one member, the freshest updated_ts wins", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ id: "t-old", title: "舊的", executorId: "kyle", updatedTs: 100 }),
      mkTask({ id: "t-new", title: "新的", executorId: "kyle", updatedTs: 200 }),
      mkTask({ id: "t-mid", title: "中間", executorId: "kyle", updatedTs: 150 }),
    ]);
    expect(map.get("kyle")?.title).toBe("新的");
    expect(map.get("kyle")?.taskId).toBe("t-new");
  });

  it("ignores CLOSED tasks (closedTs set) — done/terminated/duplicated are not current", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ id: "t-done", executorId: "mira", closedTs: 999, updatedTs: 500 }),
    ]);
    expect(map.has("mira")).toBe(false);
  });

  it("a member with an open task shadows its own older CLOSED one", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ id: "t-done", title: "已結", executorId: "mira", closedTs: 999, updatedTs: 900 }),
      mkTask({ id: "t-open", title: "在辦", executorId: "mira", closedTs: null, updatedTs: 100 }),
    ]);
    // The open one wins even though the closed one has a newer updated_ts.
    expect(map.get("mira")?.title).toBe("在辦");
  });

  it("ignores OUTSOURCE-kind tasks and unassigned tasks (empty executorId)", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ id: "t-out", executorKind: "outsource", executorId: "ow-9" }),
      mkTask({ id: "t-none", executorKind: "member", executorId: "" }),
    ]);
    expect(map.size).toBe(0);
  });

  it("a member with no open task is simply absent from the map", () => {
    const map = deriveMemberCurrentTasks([
      mkTask({ executorId: "mira", closedTs: 1 }),
    ]);
    expect(map.get("mira")).toBeUndefined();
  });
});
