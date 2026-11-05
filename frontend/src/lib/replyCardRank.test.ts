import { describe, expect, it } from "vitest";
import { compareReplyCardRank } from "./replyCardRank";

describe("compareReplyCardRank", () => {
  it("orders by priority, then due date with dated cards first, and treats a missing priority as mid", () => {
    const cards = [
      { id: "low", priority: "low" },
      { id: "mid-none" },
      { id: "mid-late", priority: "mid", dueTs: 900 },
      { id: "high", priority: "high", dueTs: null },
      { id: "mid-soon", priority: "mid", dueTs: 500 },
    ];
    expect([...cards].sort(compareReplyCardRank).map((c) => c.id)).toEqual([
      "high", "mid-soon", "mid-late", "mid-none", "low",
    ]);
  });

  it("calls two cards of the same priority and no due date a tie, leaving age to the caller", () => {
    expect(compareReplyCardRank({ priority: "mid" }, {})).toBe(0);
  });
});
