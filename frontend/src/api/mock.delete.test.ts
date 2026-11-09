// Mock twin of POST /api/chat/{id}/delete: the same refusals the server makes,
// the owner's listing keeps the deleted message (body intact) flagged by
// `deletedTs`, a quote of it carries no text, and the recipient gets one
// system notice replying to it.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockChat } from "./mock";
import type { ChatMessage } from "./adapter";
import { ApiError } from "./errors";

const NOW = () => Date.now() / 1000;

async function post(body: string, replyTo?: string, replyExcerpt?: string): Promise<ChatMessage> {
  await mockApi.postChat({ to: "mira", body, replyTo, replyExcerpt });
  const thread = await mockApi.listChat("mira");
  return thread[thread.length - 1];
}

function inject(over: Partial<ChatMessage> & { id: string }) {
  __injectMockChat({
    from: "owner",
    to: "mira",
    body: "x",
    ts: NOW() - 60,
    attachments: [],
    replyCardId: null,
    ...over,
  });
}

async function refusal(id: string): Promise<[number, string]> {
  try {
    await mockApi.deleteChatMessage(id);
  } catch (e) {
    if (e instanceof ApiError) return [e.status, e.serverMessage];
    throw e;
  }
  throw new Error("expected a refusal");
}

describe("mock deleteChatMessage", () => {
  beforeEach(() => __resetMock());

  it("deletes the owner's fresh message, keeps it in the owner's listing and notifies the recipient once", async () => {
    const sent = await post("打錯了");
    const before = Date.now() / 1000;
    const answered = await mockApi.deleteChatMessage(sent.id);
    expect(answered.id).toBe(sent.id);
    expect(answered.body).toBe("打錯了");
    expect(answered.deletedTs).toBeGreaterThanOrEqual(before);

    const thread = await mockApi.listChat("mira");
    const kept = thread.find((m) => m.id === sent.id)!;
    expect(kept.body).toBe("打錯了");
    expect(kept.deletedTs).toBe(answered.deletedTs);
    const notices = thread.filter((m) => m.from === "system");
    expect(notices.map((m) => [m.to, m.replyTo, m.body])).toEqual([
      [
        "mira",
        sent.id,
        `Owner 刪除了訊息 #${sent.id}，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=${sent.id} 讀回。`,
      ],
    ]);

    // Again: unchanged, and no second notice.
    const again = await mockApi.deleteChatMessage(sent.id);
    expect(again.deletedTs).toBe(answered.deletedTs);
    expect((await mockApi.listChat("mira")).filter((m) => m.from === "system")).toHaveLength(1);
  });

  it("an ordinary message reads deletedTs 0", async () => {
    const sent = await post("普通");
    expect(sent.deletedTs).toBe(0);
  });

  it("a quote of a deleted message carries no text and no excerpt", async () => {
    const original = await post("原句內容");
    const reply = await post("回這段", original.id, "原句");
    expect(reply.replyToChat).toEqual({
      id: original.id,
      from: "owner",
      fromName: "",
      to: "mira",
      toName: "",
      content: "原句內容",
      replyExcerpt: "原句",
      deleted: false,
    });

    await mockApi.deleteChatMessage(original.id);
    const reread = (await mockApi.listChat("mira")).find((m) => m.id === reply.id)!;
    expect(reread.replyToChat).toEqual({
      id: original.id,
      from: "owner",
      fromName: "",
      to: "mira",
      toName: "",
      content: "",
      deleted: true,
    });
  });

  it("refuses what the server refuses, with the server's words", async () => {
    inject({ id: "theirs", from: "mira", to: "owner" });
    inject({ id: "old", ts: NOW() - 25 * 60 * 60 });
    inject({ id: "card", replyCardId: "rc-1" });
    inject({ id: "task", taskId: "T-7" });
    expect(await refusal("theirs")).toEqual([
      409,
      "chat message 'theirs' was sent by 'mira' — only the owner's own messages can be deleted",
    ]);
    expect(await refusal("old")).toEqual([
      409,
      "chat message 'old' was sent more than 24 hours ago — only messages younger than 24 hours can be deleted",
    ]);
    expect(await refusal("card")).toEqual([
      409,
      "chat message 'card' carries a reply card — expire the card instead; a card message cannot be deleted",
    ]);
    expect(await refusal("task")).toEqual([
      409,
      "chat message 'task' is a message on task T-7 — task messages cannot be deleted",
    ]);
    expect(await refusal("nope")).toEqual([404, "chat message 'nope' not found"]);
    // None of them changed.
    const thread = await mockApi.listChat("mira");
    expect(
      thread.filter((m) => ["theirs", "old", "card", "task"].includes(m.id)).map((m) => m.deletedTs),
    ).toEqual([0, 0, 0, 0]);
  });

  it("the attachment gallery leaves a deleted message's files out", async () => {
    const att = { url: "/x", mime: "application/pdf", isImage: false };
    inject({ id: "kept", attachments: [{ id: "a-kept", filename: "k.pdf", ...att }] });
    inject({ id: "gone", attachments: [{ id: "a-gone", filename: "g.pdf", ...att }] });
    await mockApi.deleteChatMessage("gone");
    expect((await mockApi.listChatAttachments("mira")).map((a) => a.id)).toEqual(["a-kept"]);
  });
});
