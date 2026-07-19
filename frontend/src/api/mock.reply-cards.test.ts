// Reply-card seam of the mock adapter (M2 回覆卡 B2). Locked here — mirroring
// the server contract the http adapter rides:
//   1. waiting list = longest-waiting first; answered list = 24h window,
//      newest answer first; the badge count = WAITING only.
//   2. Answering is the ONLY close: a real answer (option / text / attachments
//      — a typed counter-question included) flips waiting→answered; an empty
//      answer and an out-of-range option are 400; double-answer is 409.
//   3. 重新決定 (reanswer) revises an ANSWERED card wholesale — status STAYS
//      answered (never reopens, never re-counts) — and a waiting card is 409.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockReplyCard } from "./mock";
import { ApiError } from "./errors";
import type { ReplyCard } from "./adapter";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: ["寄出", "先不要", "改寄給別人"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

async function statusOf(e: Promise<unknown>): Promise<number> {
  try {
    await e;
    return 0;
  } catch (err) {
    expect(err).toBeInstanceOf(ApiError);
    return (err as ApiError).status;
  }
}

beforeEach(() => {
  __resetMock();
});

describe("mock reply-card api", () => {
  it("lists waiting cards longest-waiting first and counts only them", async () => {
    const now = Date.now() / 1000;
    __injectMockReplyCard(mkCard({ id: "young", createdTs: now - 60 }));
    __injectMockReplyCard(mkCard({ id: "old", createdTs: now - 7200 }));
    __injectMockReplyCard(
      mkCard({
        id: "done",
        status: "answered",
        answeredTs: now - 30,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );

    const waiting = await mockApi.listReplyCards("waiting");
    expect(waiting.map((c) => c.id)).toEqual(["old", "young"]);
    expect((await mockApi.getReplyCardCount()).waiting).toBe(2);
  });

  it("windows the answered list to 24h, newest answer first", async () => {
    const now = Date.now() / 1000;
    const ans = { optionIdx: null, text: "ok", attachments: [] };
    __injectMockReplyCard(
      mkCard({ id: "recent", status: "answered", answeredTs: now - 60, answer: ans })
    );
    __injectMockReplyCard(
      mkCard({ id: "older", status: "answered", answeredTs: now - 3600, answer: ans })
    );
    __injectMockReplyCard(
      mkCard({
        id: "expired",
        status: "answered",
        answeredTs: now - 25 * 3600,
        answer: ans,
      })
    );

    const answered = await mockApi.listReplyCards("answered");
    expect(answered.map((c) => c.id)).toEqual(["recent", "older"]);
    // The count endpoint's `answered` mirrors the same 24h window (the collapsed
    // 近期已回覆 · N header reads it without pulling this list).
    expect((await mockApi.getReplyCardCount()).answered).toBe(2);
  });

  it("answering a waiting card closes it and decrements the count", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));

    const card = await mockApi.answerReplyCard("rc-1", { optionIdx: 1 });
    expect(card.status).toBe("answered");
    expect(card.answer?.optionIdx).toBe(1);
    expect(card.answeredTs).not.toBeNull();
    expect((await mockApi.getReplyCardCount()).waiting).toBe(0);
    expect(await mockApi.listReplyCards("waiting")).toEqual([]);
    expect((await mockApi.listReplyCards("answered")).map((c) => c.id)).toEqual(
      ["rc-1"]
    );
  });

  it("a typed counter-question is a real answer and closes the card too", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const card = await mockApi.answerReplyCard("rc-1", {
      text: "收件人是誰？",
    });
    expect(card.status).toBe("answered");
    expect(card.answer?.optionIdx).toBeNull();
    expect(card.answer?.text).toBe("收件人是誰？");
  });

  it("rejects an empty answer and an out-of-range option with 400", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    expect(await statusOf(mockApi.answerReplyCard("rc-1", {}))).toBe(400);
    expect(
      await statusOf(mockApi.answerReplyCard("rc-1", { optionIdx: 9 }))
    ).toBe(400);
    // Both rejections left the card untouched.
    expect((await mockApi.getReplyCardCount()).waiting).toBe(1);
  });

  it("answering an already-answered card is a 409 (revise via reanswer)", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    await mockApi.answerReplyCard("rc-1", { optionIdx: 0 });
    expect(
      await statusOf(mockApi.answerReplyCard("rc-1", { optionIdx: 1 }))
    ).toBe(409);
  });

  it("reanswer replaces the answer wholesale and stays answered", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const first = await mockApi.answerReplyCard("rc-1", { optionIdx: 0 });

    const revised = await mockApi.reanswerReplyCard("rc-1", {
      text: "改成手動寄",
    });
    expect(revised.status).toBe("answered");
    expect(revised.answer?.optionIdx).toBeNull();
    expect(revised.answer?.text).toBe("改成手動寄");
    expect(revised.answeredTs).toBeGreaterThanOrEqual(first.answeredTs ?? 0);
    // A revision never re-counts the badge.
    expect((await mockApi.getReplyCardCount()).waiting).toBe(0);
  });

  it("reanswering a still-waiting card is a 409", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    expect(
      await statusOf(mockApi.reanswerReplyCard("rc-1", { optionIdx: 0 }))
    ).toBe(409);
  });

  it("an unknown card id is a 404", async () => {
    expect(
      await statusOf(mockApi.answerReplyCard("nope", { optionIdx: 0 }))
    ).toBe(404);
    expect(await statusOf(mockApi.getReplyCard("nope"))).toBe(404);
  });

  it("expire flips a waiting card to terminal expired without an answer", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const expired = await mockApi.expireReplyCard("rc-1");
    expect(expired.status).toBe("expired");
    expect(expired.expiredTs).toBeGreaterThan(0);
    expect(expired.answer).toBeNull();

    // Off the waiting pane, onto the expired pane; counts follow.
    expect(await mockApi.listReplyCards("waiting")).toEqual([]);
    expect(
      (await mockApi.listReplyCards("expired")).map((c) => c.id)
    ).toEqual(["rc-1"]);
    const counts = await mockApi.getReplyCardCount();
    expect(counts.waiting).toBe(0);
    expect(counts.expired).toBe(1);

    // Terminal: expire again / answer / reanswer are all 409; unknown id 404.
    expect(await statusOf(mockApi.expireReplyCard("rc-1"))).toBe(409);
    expect(
      await statusOf(mockApi.answerReplyCard("rc-1", { optionIdx: 0 }))
    ).toBe(409);
    expect(
      await statusOf(mockApi.reanswerReplyCard("rc-1", { optionIdx: 0 }))
    ).toBe(409);
    expect(await statusOf(mockApi.expireReplyCard("nope"))).toBe(404);
  });

  it("expire refuses an answered card", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    await mockApi.answerReplyCard("rc-1", { optionIdx: 0 });
    expect(await statusOf(mockApi.expireReplyCard("rc-1"))).toBe(409);
    expect((await mockApi.getReplyCard("rc-1")).status).toBe("answered");
  });

  it("reads one card in full by id (the inline chat card's pull path)", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1" }));
    const card = await mockApi.getReplyCard("rc-1");
    expect(card.id).toBe("rc-1");
    expect(card.options).toEqual(["寄出", "先不要", "改寄給別人"]);
    expect(card.status).toBe("waiting");

    await mockApi.answerReplyCard("rc-1", { optionIdx: 1 });
    const answered = await mockApi.getReplyCard("rc-1");
    expect(answered.status).toBe("answered");
    expect(answered.answer?.optionIdx).toBe(1);
  });
});
