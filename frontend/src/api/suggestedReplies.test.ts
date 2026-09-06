// The 建議回覆 readers (T-122). They read two settings fields off a payload the
// frontend cannot verify — a server older than T-122 omits both, and a
// hand-edited row can hold anything — so every unusable shape has to answer the
// empty list rather than throw: the chips sit over a reply box that must keep
// working without them.
//
// The wire names are never written out here either. They come from the module
// under test through computed keys, which is what keeps the grep for either
// field name answering with ONE file.

import { describe, it, expect } from "vitest";
import {
  readSuggestedRepliesReplyCard,
  readSuggestedRepliesTaskMessage,
  suggestedRepliesPatchFields,
  withSuggestedRepliesReplyCard,
  withSuggestedRepliesTaskMessage,
  SUGGESTED_REPLIES_REPLY_CARD_FIELD,
  SUGGESTED_REPLIES_TASK_MESSAGE_FIELD,
} from "./suggestedReplies";

const replyCard = (v: unknown) => ({
  [SUGGESTED_REPLIES_REPLY_CARD_FIELD]: v,
});
const taskMessage = (v: unknown) => ({
  [SUGGESTED_REPLIES_TASK_MESSAGE_FIELD]: v,
});

describe("readSuggestedRepliesReplyCard", () => {
  it("answers the empty list when the settings payload has no such field", () => {
    expect(readSuggestedRepliesReplyCard({ owner_name: "伊娃" })).toEqual([]);
  });

  it("answers the empty list for a payload that is not an object at all", () => {
    expect(readSuggestedRepliesReplyCard(null)).toEqual([]);
    expect(readSuggestedRepliesReplyCard(undefined)).toEqual([]);
    expect(readSuggestedRepliesReplyCard("收到")).toEqual([]);
  });

  it("answers the empty list when the field is not an array", () => {
    expect(readSuggestedRepliesReplyCard(replyCard("收到"))).toEqual([]);
    expect(readSuggestedRepliesReplyCard(replyCard(null))).toEqual([]);
  });

  it("returns the configured sentences in the order the owner wrote them", () => {
    expect(
      readSuggestedRepliesReplyCard(replyCard(["收到", "先擱著", "照做"]))
    ).toEqual(["收到", "先擱著", "照做"]);
  });

  it("trims each sentence and drops the ones that are blank", () => {
    expect(
      readSuggestedRepliesReplyCard(replyCard(["  收到  ", "", "   ", "照做"]))
    ).toEqual(["收到", "照做"]);
  });

  it("drops entries that are not text rather than rendering them", () => {
    expect(
      readSuggestedRepliesReplyCard(replyCard(["收到", 42, null, "照做"]))
    ).toEqual(["收到", "照做"]);
  });
});

describe("readSuggestedRepliesTaskMessage", () => {
  it("reads its own field and never falls back to the reply-card one", () => {
    const settings = replyCard(["收到"]);
    expect(readSuggestedRepliesTaskMessage(settings)).toEqual([]);
    expect(readSuggestedRepliesReplyCard(settings)).toEqual(["收到"]);
  });

  it("reads the two lists independently off one payload", () => {
    const settings = {
      ...replyCard(["收到"]),
      ...taskMessage(["先擱著", "之後再說"]),
    };
    expect(readSuggestedRepliesReplyCard(settings)).toEqual(["收到"]);
    expect(readSuggestedRepliesTaskMessage(settings)).toEqual([
      "先擱著",
      "之後再說",
    ]);
  });
});

describe("suggestedRepliesPatchFields", () => {
  it("omits a list the patch does not name, so it stays unchanged", () => {
    expect(suggestedRepliesPatchFields({})).toEqual({});
    expect(
      suggestedRepliesPatchFields({ suggestedRepliesReplyCard: ["收到"] })
    ).toEqual(replyCard(["收到"]));
  });

  it("SENDS an empty list, because [] is a legal value that clears it", () => {
    expect(
      suggestedRepliesPatchFields({ suggestedRepliesTaskMessage: [] })
    ).toEqual(taskMessage([]));
  });
});

describe("withSuggestedRepliesReplyCard", () => {
  it("puts the list on a copy, leaving the payload it was given untouched", () => {
    const settings = { owner_name: "伊娃" };
    const next = withSuggestedRepliesReplyCard(settings, ["收到"]);
    expect(readSuggestedRepliesReplyCard(next)).toEqual(["收到"]);
    expect(readSuggestedRepliesReplyCard(settings)).toEqual([]);
  });

  it("does not disturb the task-message list already on the payload", () => {
    const settings = withSuggestedRepliesTaskMessage({}, ["先擱著"]);
    const next = withSuggestedRepliesReplyCard(settings, ["收到"]);
    expect(readSuggestedRepliesTaskMessage(next)).toEqual(["先擱著"]);
    expect(readSuggestedRepliesReplyCard(next)).toEqual(["收到"]);
  });
});
