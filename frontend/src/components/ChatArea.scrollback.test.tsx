// ChatArea scrollback (T-bf82) — the component-side pins:
//
//   1. TOP TRIGGER: scrolling near the top (<120px) with hasMore pulls one
//      older page; hasMore=false renders the "已到最早訊息" marker instead
//      and never fires a load.
//   2. PREPEND ≠ FRESH: loaded history must never arm the "有新訊息" chip
//      nor re-anchor the unread divider (the prevIdsRef diff would otherwise
//      misread a prepend as fresh inbound).
//   3. SCROLL COMPENSATION: the viewport keeps the row the owner was reading
//      — scrollTop grows by exactly the height the prepend added.
//   4. 收折 × 分頁 boundary: an EXPANDED inter-agent block stays expanded
//      when a history prepend merges an older run into it and thereby
//      changes the group's first-message id (the collapse key).
//
// jsdom has no layout: scroll geometry is defined per test and
// scrollIntoView is stubbed (same technique as ChatArea.unread-jump.test).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { useState } from "react";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

// Stateful useChat stand-in: loadOlder() PREPENDS `olderPage` (like the real
// hook) so the component's anchoring/bookkeeping sees a real state commit.
let initialMessages: ChatMessage[] = [];
let olderPage: ChatMessage[] = [];
let hasMore = true;
let loadOlderCalls = 0;
// Optional test hook run inside loadOlder BEFORE the prepend commits (used to
// grow the fake scrollHeight so the compensation math is observable).
let onLoadOlder: (() => void) | null = null;

vi.mock("../hooks/useChat", () => ({
  useChat: () => {
    const [msgs, setMsgs] = useState<ChatMessage[]>(() => initialMessages);
    return {
      messages: msgs,
      messagesPeer: "b",
      peerLastReadTs: 0,
      send: vi.fn(() => Promise.resolve()),
      markRead: vi.fn(() => Promise.resolve()),
      hasMore,
      loadOlder: async () => {
        loadOlderCalls += 1;
        onLoadOlder?.();
        setMsgs((prev) => [...olderPage, ...prev]);
      },
    };
  },
}));

function mkMember(id = "b", name = "Beto"): Member {
  return {
    id,
    memberId: id,
    name,
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
    tmuxSession: `member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function mkMsg(id: string, from: string, to: string, ts: number): ChatMessage {
  return {
    id,
    from,
    to,
    body: `msg ${id}`,
    ts,
    attachments: [],
    replyCardId: null,
  };
}

function setScrollGeometry(
  el: Element,
  geo: { scrollHeight: number; clientHeight: number; scrollTop: number },
) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    value: geo.scrollHeight,
  });
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    value: geo.clientHeight,
  });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    writable: true,
    value: geo.scrollTop,
  });
}

function renderChat() {
  const beto = mkMember();
  const alma = mkMember("a", "Alma");
  return render(
    <I18nProvider>
      <ChatArea member={beto} members={[beto, alma]} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
  initialMessages = [];
  olderPage = [];
  hasMore = true;
  loadOlderCalls = 0;
  onLoadOlder = null;
});

describe("scroll-top history loading", () => {
  it("near the top pulls one older page; the prepend keeps the viewport row (scrollTop compensation)", async () => {
    initialMessages = [
      mkMsg("c2", "b", "owner", 2000),
      mkMsg("c3", "b", "owner", 2001),
    ];
    olderPage = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat();
    const list = container.querySelector(".chat__messages")!;

    // Scrolled near the top (scrollTop 100 < 120), far from the bottom.
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    // The prepend adds 300px of history above the fold.
    onLoadOlder = () =>
      Object.defineProperty(list, "scrollHeight", {
        configurable: true,
        value: 1300,
      });
    await act(async () => {
      fireEvent.scroll(list);
    });

    expect(loadOlderCalls).toBe(1);
    // The older message rendered above the previous first row.
    const ids = Array.from(list.querySelectorAll("[data-msg-id]")).map((el) =>
      el.getAttribute("data-msg-id"),
    );
    expect(ids).toEqual(["c1", "c2", "c3"]);
    // Compensation: scrollTop grew by exactly the added height (100 + 300).
    expect((list as HTMLElement).scrollTop).toBe(400);
  });

  it("prepended HISTORY never arms the new-message chip nor re-anchors the divider", async () => {
    initialMessages = [
      mkMsg("c2", "b", "owner", 2000),
      mkMsg("c3", "b", "owner", 2001),
    ];
    olderPage = [mkMsg("c1", "b", "owner", 1000)]; // inbound-to-owner history
    const { container } = renderChat();
    const list = container.querySelector(".chat__messages")!;

    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100, // near the top AND far from the bottom (scrolled up)
    });
    await act(async () => {
      fireEvent.scroll(list);
    });

    expect(loadOlderCalls).toBe(1);
    // History is not fresh: no chip, no unread divider.
    expect(container.querySelector(".chat__new-msg-chip")).toBeNull();
    expect(container.querySelector(".chat__unread-divider")).toBeNull();
  });

  it("hasMore=false shows the 已到最早訊息 marker and never loads", async () => {
    hasMore = false;
    initialMessages = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat();
    const list = container.querySelector(".chat__messages")!;

    expect(container.querySelector(".chat__history-start")).not.toBeNull();

    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 0,
    });
    await act(async () => {
      fireEvent.scroll(list);
    });
    expect(loadOlderCalls).toBe(0);
  });

  it("hasMore=true renders no marker", () => {
    initialMessages = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat();
    expect(container.querySelector(".chat__history-start")).toBeNull();
  });
});

describe("收折 × 分頁 — expanded inter-agent block survives a prepend", () => {
  it("keeps the block expanded when history merges into the run (collapse key moves)", async () => {
    // Same local day throughout (groupMessages runs per day-group): the run
    // starts as [i2, i3]; the older page prepends i1, merging into ONE run
    // whose first-message id (the collapse key) becomes i1.
    const day = Math.floor(Date.now() / 1000) - 60;
    initialMessages = [
      mkMsg("i2", "a", "b", day + 1),
      mkMsg("i3", "b", "a", day + 2),
    ];
    olderPage = [mkMsg("i1", "a", "b", day)];
    const { container } = renderChat();
    const list = container.querySelector(".chat__messages")!;

    // Expand the block (stores the CURRENT group id "i2").
    const toggle = container.querySelector(
      ".chat__inter-toggle",
    ) as HTMLButtonElement;
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(2);

    // History lands and the run's first id becomes i1.
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 0,
    });
    await act(async () => {
      fireEvent.scroll(list);
    });

    // Still ONE run, still EXPANDED (membership judgement, not the raw key):
    // all three bubbles visible.
    const toggles = container.querySelectorAll(".chat__inter-toggle");
    expect(toggles.length).toBe(1);
    expect(toggles[0].getAttribute("aria-expanded")).toBe("true");
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(3);

    // Collapse still works on the merged block (every member key clears).
    fireEvent.click(toggles[0]);
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(0);
    expect(
      container
        .querySelector(".chat__inter-toggle")!
        .getAttribute("aria-expanded"),
    ).toBe("false");
  });
});
