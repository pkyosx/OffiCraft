import { describe, expect, it } from "vitest";
import type { ReplyCardRow } from "../api/adapter";
import { sortWaitingRows } from "./replyCardSort";

function row(id: string, createdTs: number, task?: { priority?: string; dueTs?: number | null }): ReplyCardRow {
  return {
    id,
    from: "mira",
    kind: "decision",
    summary: id,
    status: "waiting",
    createdTs,
    answeredTs: null,
    task: task ? { id: `t-${id}`, typeKey: "", title: id, ...task } : null,
  };
}

const rows = [
  row("noTaskNew", 900),
  row("lowOld", 100, { priority: "low", dueTs: 5000 }),
  row("frozen", 500, { priority: "frozen", dueTs: 1000 }),
  row("highOld", 200, { priority: "high" }),
  row("midDue", 300, { priority: "mid", dueTs: 3000 }),
  row("highNew", 800, { priority: "high", dueTs: 3000 }),
  row("noTaskOld", 50),
];
const order = (sort: Parameters<typeof sortWaitingRows>[1]) => sortWaitingRows(rows, sort).map((r) => r.id);

describe("sortWaitingRows", () => {
  it("createdDesc → newest opened first; createdAsc → oldest first", () => {
    expect(order("createdDesc")).toEqual(["noTaskNew", "highNew", "frozen", "midDue", "highOld", "lowOld", "noTaskOld"]);
    expect(order("createdAsc")).toEqual(["noTaskOld", "lowOld", "highOld", "midDue", "frozen", "highNew", "noTaskNew"]);
  });

  it("priority → high, mid, low, frozen, then cards with no task; newest first within a rank", () => {
    expect(order("priority")).toEqual(["highNew", "highOld", "midDue", "lowOld", "frozen", "noTaskNew", "noTaskOld"]);
  });

  it("due → earliest due first, undated (and task-less) last; newest first on a tie", () => {
    expect(order("due")).toEqual(["frozen", "highNew", "midDue", "lowOld", "noTaskNew", "highOld", "noTaskOld"]);
  });

  it("does not reorder its input", () => {
    const before = rows.map((r) => r.id);
    sortWaitingRows(rows, "priority");
    expect(rows.map((r) => r.id)).toEqual(before);
  });
});
