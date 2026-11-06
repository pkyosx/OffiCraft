import { describe, it, expect } from "vitest";
import { mergeChatSuggestedReplies } from "./chatSuggestedReplies";

describe("mergeChatSuggestedReplies", () => {
  it("puts the member's own sentences first, each list in its own order", () => {
    expect(mergeChatSuggestedReplies(["b", "a"], ["d", "c"])).toEqual(["b", "a", "d", "c"]);
  });

  it("shows identical text once, at the member's own position", () => {
    expect(mergeChatSuggestedReplies(["繼續", "好"], ["先放著", "繼續"])).toEqual(["繼續", "好", "先放著"]);
  });

  it("works with either list empty and with both empty", () => {
    expect(mergeChatSuggestedReplies([], ["a"])).toEqual(["a"]);
    expect(mergeChatSuggestedReplies(["a"], [])).toEqual(["a"]);
    expect(mergeChatSuggestedReplies([], [])).toEqual([]);
  });

  it("treats text that differs at all as two sentences", () => {
    expect(mergeChatSuggestedReplies(["OK"], ["ok"])).toEqual(["OK", "ok"]);
  });
});
