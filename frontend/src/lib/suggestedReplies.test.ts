import { describe, it, expect } from "vitest";
import { mergeSuggestedReplies } from "./suggestedReplies";

describe("mergeSuggestedReplies", () => {
  it("puts the member's own sentences first, each list in its own order", () => {
    expect(mergeSuggestedReplies(["b", "a"], ["d", "c"])).toEqual(["b", "a", "d", "c"]);
  });

  it("shows identical text once, at the member's own position", () => {
    expect(mergeSuggestedReplies(["繼續", "好"], ["先放著", "繼續"])).toEqual(["繼續", "好", "先放著"]);
  });

  it("works with either list empty and with both empty", () => {
    expect(mergeSuggestedReplies([], ["a"])).toEqual(["a"]);
    expect(mergeSuggestedReplies(["a"], [])).toEqual(["a"]);
    expect(mergeSuggestedReplies([], [])).toEqual([]);
  });

  it("keeps three lists in the order given and shows a sentence once, at its first list", () => {
    expect(mergeSuggestedReplies(["型", "共"], ["人", "型"], ["卡", "人", "共"])).toEqual(["型", "共", "人", "卡"]);
  });

  it("treats text that differs at all as two sentences", () => {
    expect(mergeSuggestedReplies(["OK"], ["ok"])).toEqual(["OK", "ok"]);
  });
});
