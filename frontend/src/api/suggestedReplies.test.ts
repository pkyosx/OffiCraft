// The 建議回覆 reader (T-122). It reads a field the frozen wire does not
// declare yet, so it is the one place in the tree that has to survive a
// settings payload that simply does not contain it.

import { describe, it, expect } from "vitest";
import {
  readSuggestedReplies,
  withSuggestedReplies,
} from "./suggestedReplies";

describe("readSuggestedReplies", () => {
  it("answers the empty list when the settings payload has no such field", () => {
    expect(readSuggestedReplies({ owner_name: "伊娃" })).toEqual([]);
  });

  it("answers the empty list for a payload that is not an object at all", () => {
    expect(readSuggestedReplies(null)).toEqual([]);
    expect(readSuggestedReplies(undefined)).toEqual([]);
    expect(readSuggestedReplies("suggested_replies")).toEqual([]);
  });

  it("answers the empty list when the field is not an array", () => {
    expect(readSuggestedReplies({ suggested_replies: "收到" })).toEqual([]);
    expect(readSuggestedReplies({ suggested_replies: null })).toEqual([]);
  });

  it("returns the configured sentences in the order the owner wrote them", () => {
    expect(
      readSuggestedReplies({ suggested_replies: ["收到", "先擱著", "照做"] })
    ).toEqual(["收到", "先擱著", "照做"]);
  });

  it("trims each sentence and drops the ones that are blank", () => {
    expect(
      readSuggestedReplies({
        suggested_replies: ["  收到  ", "", "   ", "照做"],
      })
    ).toEqual(["收到", "照做"]);
  });

  it("drops entries that are not text rather than rendering them", () => {
    expect(
      readSuggestedReplies({ suggested_replies: ["收到", 42, null, "照做"] })
    ).toEqual(["收到", "照做"]);
  });
});

describe("withSuggestedReplies", () => {
  it("puts the list on a copy, leaving the payload it was given untouched", () => {
    const settings = { owner_name: "伊娃" };
    const next = withSuggestedReplies(settings, ["收到"]);
    expect(readSuggestedReplies(next)).toEqual(["收到"]);
    expect(readSuggestedReplies(settings)).toEqual([]);
  });
});
