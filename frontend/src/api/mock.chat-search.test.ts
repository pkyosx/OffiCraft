import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockChat, __injectMockReplyCard } from "./mock";
import type { ChatMessage, ReplyCard } from "./adapter";

function card(over: Partial<ReplyCard> & { id: string }): ReplyCard {
  return {
    from: "mira",
    kind: "decision",
    summary: "要寄出嗎?",
    body: "",
    options: [{ text: "deploy now", aiPick: false }],
    selectMode: "single",
    status: "waiting",
    createdTs: 1,
    attachments: [],
    answeredTs: null,
    expiredTs: null,
    chatMessageId: "",
    answer: null,
    task: null,
    ...over,
  };
}

function inject(over: Partial<ChatMessage> & { id: string; ts: number }) {
  __injectMockChat({
    from: "owner",
    to: "mira",
    body: "x",
    attachments: [],
    replyCardId: null,
    ...over,
  });
}

describe("mock searchChat", () => {
  beforeEach(() => __resetMock());

  it("keeps only this conversation's undeleted messages the owner sent or received whose body contains the trimmed query in any ASCII case, and counts the same set", async () => {
    inject({ id: "c-1", ts: 1, body: "Deploy 完成" });
    inject({ id: "c-2", ts: 2, body: "午餐吃什麼" });
    inject({ id: "c-3", ts: 3, body: "deploy again", from: "mira", to: "owner" });
    inject({ id: "c-4", ts: 4, body: "deploy elsewhere", to: "kye" });
    inject({ id: "c-5", ts: 5, body: "deploy deleted", deletedTs: 6 });
    inject({ id: "c-6", ts: 7, body: "deploy between members", from: "mira", to: "kye" });
    inject({ id: "c-7", ts: 8, body: "deploy between members", from: "kye", to: "mira" });

    const page = await mockApi.searchChat("mira", "  DEPLOY ");
    expect(page).toEqual({
      messages: [
        expect.objectContaining({ id: "c-1", body: "Deploy 完成" }),
        expect.objectContaining({ id: "c-3", body: "deploy again" }),
      ],
      matchCount: 2,
    });
  });

  it("matches a reply-card message by the card's question body or free-text answer, not by its option labels", async () => {
    __injectMockReplyCard(card({ id: "rc-1", body: "先 Deploy 到 staging" }));
    __injectMockReplyCard(
      card({ id: "rc-2", status: "answered", answer: { optionIdxs: null, text: "明天再 deploy", attachments: [] } }),
    );
    __injectMockReplyCard(card({ id: "rc-3" }));
    inject({ id: "c-1", ts: 1, from: "mira", to: "owner", body: "要寄出嗎?", replyCardId: "rc-1" });
    inject({ id: "c-2", ts: 2, from: "mira", to: "owner", body: "要寄出嗎?", replyCardId: "rc-2" });
    inject({ id: "c-3", ts: 3, from: "mira", to: "owner", body: "要寄出嗎?", replyCardId: "rc-3" });

    const page = await mockApi.searchChat("mira", "deploy");
    expect(page.messages.map((m) => [m.id, m.body])).toEqual([
      ["c-1", "要寄出嗎?"],
      ["c-2", "要寄出嗎?"],
    ]);
    expect(page.matchCount).toBe(2);
  });

  it("pages 30 matches at a time from the newest, oldest→newest within a page, the cursor reaches the older ones, and every page counts all matches", async () => {
    for (let i = 0; i < 35; i++) inject({ id: `c-${String(i).padStart(2, "0")}`, ts: 100 + i, body: `deploy ${i}` });

    const first = await mockApi.searchChat("mira", "deploy");
    expect(first.messages.map((m) => m.body)).toEqual(
      Array.from({ length: 30 }, (_, k) => `deploy ${k + 5}`),
    );
    expect(first.nextCursor).toBeDefined();
    expect(first.matchCount).toBe(35);

    const older = await mockApi.searchChat("mira", "deploy", first.nextCursor);
    expect(older.messages.map((m) => m.body)).toEqual(["deploy 0", "deploy 1", "deploy 2", "deploy 3", "deploy 4"]);
    expect(older.nextCursor).toBeUndefined();
    expect(older.matchCount).toBe(35);
  });
});
