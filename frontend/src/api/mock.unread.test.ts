// Mock adapter unread parity (M2-1, count badge): the mock computes
// `member.unreadCount` with the SAME watermark-inverse rule the backend applies
// (server-side unread fold on the roster read), so the mock and http
// adapters agree by construction:
//
//   1. only messages ADDRESSED TO the owner count — an agent↔agent message
//      never counts (AC #1);
//   2. entering the conversation (listChat, the FE's open-thread call) advances
//      the owner watermark past EVERY message → the count clears to 0 (AC #2);
//   3. a new message while the owner is in the thread is covered by the same
//      auto-mark on the refetch → never reported unread (AC #3, adapter side);
//   4. the count is presence-independent — the mock's members are all OFFLINE
//      and still carry a count (AC #4);
//   5. the count is PER MESSAGE — three waiting messages read as 3, not "some".
//
// Inbound member→owner messages are injected via the test-only __injectMockChat
// hook (the honest mock never fabricates a member reply on its own).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockChat } from "./mock";
import { MOCK_OWNER_ID } from "./seeds";

async function miraUnread(): Promise<{
  unreadCount: number;
  lifecycle: string;
}> {
  const members = await mockApi.listMembers();
  const mira = members.find((m) => m.id === "mira");
  if (!mira) throw new Error("mock roster lost mira");
  return { unreadCount: mira.unreadCount, lifecycle: mira.lifecycle };
}

function inbound(from: string, to: string, ts: number) {
  __injectMockChat({
    id: `t-${from}-${ts}`,
    from,
    to,
    body: "hi",
    ts,
    attachments: [],
    replyCardId: null,
  });
}

describe("mock adapter unread parity", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("an OFFLINE member with a message to the owner is unread (AC #4)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    const mira = await miraUnread();
    expect(mira.lifecycle).toBe("offline"); // presence untouched — separate axis
    expect(mira.unreadCount).toBe(1);
  });

  it("an agent↔agent message never counts for the owner (AC #1)", async () => {
    inbound("mira", "joey", 1000); // coordination between agents, not to owner
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("counts per message and clears ALL on entering the conversation (AC #2/#5)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    inbound("mira", MOCK_OWNER_ID, 1002);
    expect((await miraUnread()).unreadCount).toBe(3);
    await mockApi.listChat("mira"); // the FE's open-thread call (auto-mark)
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("a new message while in the thread reads on the refetch (AC #3)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    await mockApi.listChat("mira"); // owner is in the room
    inbound("mira", MOCK_OWNER_ID, 2000); // new message lands
    await mockApi.listChat("mira"); // the SSE-driven refetch
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("the explicit mark-read choke clears identically", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1000 });
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("peekChat returns the SAME thread but never clears the count (read-only)", async () => {
    // Badge-flash fix seam: the background-window path fetches through
    // peekChat, which must deliver the identical thread WITHOUT the
    // "list 即讀" watermark side effect — the unread count survives.
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    const peeked = await mockApi.peekChat("mira");
    expect(peeked).toHaveLength(2);
    expect((await miraUnread()).unreadCount).toBe(2); // still unread
    // The marking list then clears — same messages, different contract.
    const listed = await mockApi.listChat("mira");
    expect(listed.map((m) => m.id)).toEqual(peeked.map((m) => m.id));
    expect((await miraUnread()).unreadCount).toBe(0);
  });
});

describe("mock adapter getChatUnreadCount (the office red-dot signal)", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("is 0 when nothing is addressed to the owner (no dot)", async () => {
    inbound("mira", "joey", 1000); // agent to agent, never counts
    expect(await mockApi.getChatUnreadCount()).toBe(0);
  });

  it("sums unread across every peer (not per-member)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    inbound("joey", MOCK_OWNER_ID, 1002);
    expect(await mockApi.getChatUnreadCount()).toBe(3);
  });

  it("clears a peer's share on entering that conversation", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("joey", MOCK_OWNER_ID, 1001);
    await mockApi.listChat("mira"); // owner reads mira's thread
    expect(await mockApi.getChatUnreadCount()).toBe(1); // joey still unread
  });
});

describe("mock adapter scrollback cursor parity (T-bf82)", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("a before-cursor page returns strictly older messages (id tie-break) oldest→newest", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000); // t-mira-1000
    // Two equal-ts messages — the id must tie-break exactly like the BE.
    __injectMockChat({
      id: "t-a",
      from: "mira",
      to: MOCK_OWNER_ID,
      body: "hi",
      ts: 2000,
      attachments: [],
      replyCardId: null,
    });
    __injectMockChat({
      id: "t-b",
      from: MOCK_OWNER_ID,
      to: "mira",
      body: "hi",
      ts: 2000,
      attachments: [],
      replyCardId: null,
    });
    inbound("mira", MOCK_OWNER_ID, 3000); // t-mira-3000

    // Page back from the newest: everything strictly older, ascending (ts, id).
    const older = await mockApi.listChat("mira", 30, {
      beforeTs: 3000,
      beforeId: "t-mira-3000",
    });
    expect(older.map((m) => m.id)).toEqual(["t-mira-1000", "t-a", "t-b"]);

    // Tie-break: the cursor (2000, "t-b") keeps the equal-ts smaller id "t-a"
    // and never re-serves "t-b".
    const tie = await mockApi.listChat("mira", 30, {
      beforeTs: 2000,
      beforeId: "t-b",
    });
    expect(tie.map((m) => m.id)).toEqual(["t-mira-1000", "t-a"]);

    // The limit keeps the NEWEST slice of the older page.
    const capped = await mockApi.listChat("mira", 2, {
      beforeTs: 3000,
      beforeId: "t-mira-3000",
    });
    expect(capped.map((m) => m.id)).toEqual(["t-a", "t-b"]);
  });

  it("a HISTORY page never advances the read watermark (unread keeps counting)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 2000);
    expect((await miraUnread()).unreadCount).toBe(2);

    // Reading old context with a cursor must NOT consume the unread state…
    await mockApi.listChat("mira", 30, { beforeTs: 2000, beforeId: "zzz" });
    expect((await miraUnread()).unreadCount).toBe(2);

    // …while the cursorless open-thread list still auto-marks (unchanged).
    await mockApi.listChat("mira");
    expect((await miraUnread()).unreadCount).toBe(0);
  });
});
