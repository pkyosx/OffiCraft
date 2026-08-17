// ChatGalleryPanel: which chat deltas are worth a `GET /api/chat/attachments`
// (T-3379).
//
// 🔴 THE JUDGE IS THE REQUEST, NOT THE SCREEN. "the gallery still shows the
// same rows" is true BEFORE the fix too — an unrelated line never changed a row,
// it just paid for a round trip to be told so. So every assertion here reads
// `listChatAttachments.mock.calls`: how many went out, and WHAT was asked for
// (the `with=` argument must be this member's id — a request that fetched some
// other member's gallery would be a different bug wearing the same count).
//
// 🔴 THE PREDICATE IS `from|to === member.id`, NOT the owner predicate. The
// server keeps a message when `m.Sender == with || m.Recipient == with`, so a
// member↔agent line — which moves NO owner unread number and is therefore
// skipped by `lib/ownerUnread.ts` — absolutely does change this gallery. The
// "inter-agent line WITH this member" case below is that guard: swapping in the
// owner predicate leaves the panel stale and reddens it.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatGalleryPanel } from "./ChatGalleryPanel";
import type { Member } from "../types";
import type { GalleryAttachment, SseDelta } from "../api/adapter";

const MEMBER_ID = "m1";

let galleryRows: GalleryAttachment[] = [];
const listChatAttachments = vi.fn(
  async (_withId: string): Promise<GalleryAttachment[]> => galleryRows,
);
let handlers: ((topic: string, delta?: SseDelta) => void)[] = [];

vi.mock("../api", () => ({
  api: {
    listChatAttachments: (withId: string) => listChatAttachments(withId),
    getChatAttachmentShareLink: async (id: string) => `/x/${id}`,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      handlers.push(cb);
      return () => {
        handlers = handlers.filter((x) => x !== cb);
      };
    },
  },
}));

function mkMember(id: string = MEMBER_ID): Member {
  return {
    id,
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function row(id: string, filename: string): GalleryAttachment {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime: "image/png",
    isImage: true,
    messageId: `msg-${id}`,
    from: "owner",
    fromName: "",
    to: MEMBER_ID,
    ts: 100,
  };
}

function chat(id: string, from: string, to: string): SseDelta {
  return { topic: "chat", names: { id, from, to }, ids: [id, from, to] };
}

/** Fire a burst: every delta lands in the SAME microtask, like the wire's fan. */
async function burst(...deltas: SseDelta[]) {
  await act(async () => {
    for (const d of deltas) for (const cb of [...handlers]) cb(d.topic, d);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** A topic with NO delta at all — the older transport / a null payload. */
async function bareTopic(topic: string) {
  await act(async () => {
    for (const cb of [...handlers]) cb(topic);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Mount, let the initial fetch settle, then forget it: we measure the burst. */
async function mountSettled() {
  const view = render(
    <I18nProvider>
      <ChatGalleryPanel member={mkMember()} onClose={() => {}} />
    </I18nProvider>,
  );
  await waitFor(() =>
    expect(listChatAttachments).toHaveBeenCalledWith(MEMBER_ID),
  );
  listChatAttachments.mockClear();
  return view;
}

describe("ChatGalleryPanel — chat deltas that cannot change this gallery", () => {
  beforeEach(() => {
    galleryRows = [row("a1", "before.png")];
    handlers = [];
    listChatAttachments.mockClear();
    localStorage.clear();
  });

  it("costs ZERO requests for a line between two OTHER agents", async () => {
    await mountSettled();
    await burst(chat("c-1", "m-other", "m-third"));
    // Not "fewer" — none. The panel's answer cannot move, so nothing is asked.
    expect(listChatAttachments.mock.calls).toEqual([]);
  });

  it("costs ZERO requests for owner → some OTHER member", async () => {
    await mountSettled();
    await burst(chat("c-2", "owner", "m-other"));
    expect(listChatAttachments.mock.calls).toEqual([]);
  });

  it("costs ZERO requests for a whole burst of unrelated lines", async () => {
    await mountSettled();
    await burst(
      chat("c-3", "m-other", "m-third"),
      chat("c-4", "ow-9", "m-other"),
    );
    expect(listChatAttachments.mock.calls).toEqual([]);
  });
});

describe("ChatGalleryPanel — chat deltas that DO change this gallery", () => {
  beforeEach(() => {
    galleryRows = [row("a1", "before.png")];
    handlers = [];
    listChatAttachments.mockClear();
    localStorage.clear();
  });

  it("refetches THIS member's gallery when the message is addressed to them", async () => {
    const { container } = await mountSettled();
    galleryRows = [row("a2", "after.png"), row("a1", "before.png")];
    await burst(chat("c-5", "owner", MEMBER_ID));
    // The argument is the assertion: it asked for THIS member's gallery, once.
    expect(listChatAttachments.mock.calls).toEqual([[MEMBER_ID]]);
    // And the answer was adopted — a refetch that threw its result away would
    // pass the count assertion above.
    await waitFor(() => expect(container.textContent).toContain("after.png"));
  });

  it("refetches when THIS member is the SENDER of an inter-agent line", async () => {
    // 🔴 The owner predicate (`to === "owner"`) answers FALSE here, and so does
    // "is the owner at either end" — both would skip it and leave the gallery
    // stale. The server keeps this message (Sender == with), so we must not.
    const { container } = await mountSettled();
    galleryRows = [row("a3", "sent-to-worker.png"), row("a1", "before.png")];
    await burst(chat("c-6", MEMBER_ID, "ow-1"));
    expect(listChatAttachments.mock.calls).toEqual([[MEMBER_ID]]);
    await waitFor(() =>
      expect(container.textContent).toContain("sent-to-worker.png"),
    );
  });

  it("refetches EXACTLY ONCE for a burst mixing an unrelated line with ours", async () => {
    const { container } = await mountSettled();
    galleryRows = [row("a4", "mixed.png"), row("a1", "before.png")];
    await burst(
      chat("c-7", "m-other", "m-third"),
      chat("c-8", MEMBER_ID, "owner"),
    );
    // One burst, one question ("what is this gallery now?"), one request — and
    // the relevant half is never swallowed by the unrelated half.
    expect(listChatAttachments.mock.calls).toEqual([[MEMBER_ID]]);
    await waitFor(() => expect(container.textContent).toContain("mixed.png"));
  });

  it("refetches on a resync, whose deltas name NOTHING", async () => {
    const { container } = await mountSettled();
    galleryRows = [row("a5", "resynced.png"), row("a1", "before.png")];
    await burst({ topic: "chat", names: {}, ids: [] });
    expect(listChatAttachments.mock.calls).toEqual([[MEMBER_ID]]);
    await waitFor(() => expect(container.textContent).toContain("resynced.png"));
  });

  it("refetches when the transport supplies no delta object at all", async () => {
    const { container } = await mountSettled();
    galleryRows = [row("a6", "bare.png"), row("a1", "before.png")];
    await bareTopic("chat");
    expect(listChatAttachments.mock.calls).toEqual([[MEMBER_ID]]);
    await waitFor(() => expect(container.textContent).toContain("bare.png"));
  });

  it("follows the member the panel is switched to, not the one it mounted on", async () => {
    // 🔴 The predicate closes over `member.id`, and the effect's ONLY dependency
    // is that id. Switching peer is a RE-RENDER, not a remount (OfficePage
    // keys nothing), so a stale dependency list would leave this panel deciding
    // with the previous member's id: the new member's traffic would be skipped
    // as "unrelated" while the old member's would still cost a request. Both
    // halves are asserted below because different mutants redden different ones
    // (dropping the dependency short-circuits at (a); the pre-fix code, which
    // has no predicate at all, reddens (b)) — not because one run shows both.
    const view = render(
      <I18nProvider>
        <ChatGalleryPanel member={mkMember("m1")} onClose={() => {}} />
      </I18nProvider>,
    );
    await waitFor(() => expect(listChatAttachments).toHaveBeenCalledWith("m1"));

    view.rerender(
      <I18nProvider>
        <ChatGalleryPanel member={mkMember("m2")} onClose={() => {}} />
      </I18nProvider>,
    );
    // Switching member re-pulls for the NEW member (the panel must not keep
    // showing m1's files).
    await waitFor(() =>
      expect(listChatAttachments).toHaveBeenCalledWith("m2"),
    );
    listChatAttachments.mockClear();

    // m1's traffic is now somebody else's business.
    await burst(chat("c-9", "owner", "m1"));
    expect(listChatAttachments.mock.calls).toEqual([]);

    // m2's traffic is ours.
    await burst(chat("c-10", "owner", "m2"));
    expect(listChatAttachments.mock.calls).toEqual([["m2"]]);
  });
});
