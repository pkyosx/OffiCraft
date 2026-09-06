// Mock adapter parity for the two 建議回覆 lists (settings; T-122): both empty
// out of the box, a PATCH trims + drops blanks + persists within the session,
// over either bound is a 422 that writes nothing (a REFUSAL, never a
// truncation), an explicit empty array is a legal value that clears the list,
// and — the assertion the ticket turns on — the two lists are independent.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { ApiError } from "./errors";
import {
  SUGGESTED_REPLIES_MAX_ENTRIES,
  SUGGESTED_REPLY_MAX_LEN,
} from "./suggestedReplies";

describe("mock settings — 建議回覆 (suggested_replies.*)", () => {
  beforeEach(() => __resetMock());

  it("defaults both lists to the empty list", async () => {
    const s = await mockApi.getServerSettings();
    expect(s.suggestedRepliesReplyCard).toEqual([]);
    expect(s.suggestedRepliesTaskMessage).toEqual([]);
  });

  it("PATCHes a list (trimmed, blanks dropped) and reads it back durably", async () => {
    const s = await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["  收到  ", "", "   ", "照做"],
    });
    expect(s.suggestedRepliesReplyCard).toEqual(["收到", "照做"]);
    const again = await mockApi.getServerSettings();
    expect(again.suggestedRepliesReplyCard).toEqual(["收到", "照做"]);
  });

  it("patches one list without touching the other", async () => {
    await mockApi.patchServerSettings({
      suggestedRepliesTaskMessage: ["任務用的"],
    });
    const s = await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["請示卡用的"],
    });
    expect(s.suggestedRepliesReplyCard).toEqual(["請示卡用的"]);
    expect(s.suggestedRepliesTaskMessage).toEqual(["任務用的"]);
  });

  it("422s an entry over the 120-rune cap, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({
        suggestedRepliesReplyCard: ["水".repeat(SUGGESTED_REPLY_MAX_LEN + 1)],
      })
    ).rejects.toBeInstanceOf(ApiError);
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesReplyCard
    ).toEqual([]);
  });

  it("422s a list over 20 entries, writing nothing", async () => {
    await expect(
      mockApi.patchServerSettings({
        suggestedRepliesTaskMessage: Array.from(
          { length: SUGGESTED_REPLIES_MAX_ENTRIES + 1 },
          (_, i) => `第 ${i} 句`
        ),
      })
    ).rejects.toBeInstanceOf(ApiError);
    expect(
      (await mockApi.getServerSettings()).suggestedRepliesTaskMessage
    ).toEqual([]);
  });

  it("accepts an EXPLICIT empty array and clears the list", async () => {
    // The opposite of the scheduled-message custom_* sets, where [] is a 422.
    // Here it is how the owner turns the chips off.
    await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: ["收到"],
    });
    const cleared = await mockApi.patchServerSettings({
      suggestedRepliesReplyCard: [],
    });
    expect(cleared.suggestedRepliesReplyCard).toEqual([]);
  });
});
