import { describe, expect, it } from "vitest";
import { compareReplyCardRank } from "./replyCardRank";

describe("compareReplyCardRank", () => {
  it("orders by priority, then due date with dated cards first; a card naming no task ranks as mid with no due date", () => {
    const cards = [
      { id: "frozen", priority: "frozen" },
      { id: "low", priority: "low" },
      { id: "no-task", priority: null, dueTs: null },
      { id: "mid-late", priority: "mid", dueTs: 900 },
      { id: "high", priority: "high", dueTs: null },
      { id: "mid-soon", priority: "mid", dueTs: 500 },
    ];
    expect([...cards].sort(compareReplyCardRank).map((c) => c.id)).toEqual([
      "high", "mid-soon", "mid-late", "no-task", "low", "frozen",
    ]);
  });

  it("calls a mid card with no due date and a card naming no task a tie, leaving age to the caller", () => {
    expect(compareReplyCardRank({ priority: "mid" }, { priority: null })).toBe(0);
  });
});
