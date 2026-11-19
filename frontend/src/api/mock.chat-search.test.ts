import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockChat } from "./mock";
import type { ChatMessage } from "./adapter";

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

  it("keeps only this conversation's undeleted messages whose body contains the trimmed query in any ASCII case", async () => {
    inject({ id: "c-1", ts: 1, body: "Deploy 完成" });
    inject({ id: "c-2", ts: 2, body: "午餐吃什麼" });
    inject({ id: "c-3", ts: 3, body: "deploy again", from: "mira", to: "owner" });
    inject({ id: "c-4", ts: 4, body: "deploy elsewhere", to: "kye" });
    inject({ id: "c-5", ts: 5, body: "deploy deleted", deletedTs: 6 });

    const page = await mockApi.searchChat("mira", "  DEPLOY ");
    expect(page).toEqual({
      messages: [
        expect.objectContaining({ id: "c-1", body: "Deploy 完成" }),
        expect.objectContaining({ id: "c-3", body: "deploy again" }),
      ],
      matchCount: 2,
    });
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
