// LINE/FB-style unread jump (M2 batch 19) — black-box pins on ChatArea:
//
//   ① NEW-MESSAGE FLOATING CHIP: the owner has scrolled UP (latest below the
//      fold) and a new message addressed to them lands → a floating
//      "有新訊息" chip appears; clicking it scrolls to the FIRST unseen
//      message (the anchor stays the first one even as more accumulate);
//      reaching the bottom dismisses it. At the bottom the existing
//      auto-follow stays: new message → follow, NO chip.
//   ② ENTRY POSITIONING: entering a conversation whose roster badge carried
//      unreadCount > 0 lands on the FIRST unread message (an "以下是未讀訊息"
//      divider pinned to the top of the viewport), derived from the
//      unreadCount SNAPSHOT taken at entry — race-free against the server's
//      list-即讀 watermark clear. No unread → the existing land-at-bottom.
//
//   ③ B3 跳到原訊息 (jumpToMsgId): entering with a message target locates it
//      (center scroll + transient highlight) and OWNS the entry positioning
//      (no divider/bottom scroll fights it); one-shot — a later refetch never
//      re-scrolls; a target outside the loaded window falls back to bottom.
//
// jsdom cannot really scroll, so scrollIntoView is stubbed to record its
// element + args, and the viewport geometry (scrollHeight/clientHeight/
// scrollTop) is defined per test to drive the near-bottom detection.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

// Window peer = agent "b" (Beto). Owner id is "owner". `messagesPeer` mirrors
// useChat's contract: the peer the CURRENT messages array belongs to (set
// together with the messages; lags one commit behind a peer switch).
let messages: ChatMessage[] = [];
let messagesPeer = "b";
const markRead = vi.fn(() => Promise.resolve());
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer,
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead,
  }),
}));

function mkMember(unreadCount: number, id = "b", name = "Beto"): Member {
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
    tmuxSession: "member-b",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount,
  };
}

function mkMsg(
  id: string,
  from: string,
  to: string,
  ts: number,
): ChatMessage {
  return { id, from, to, body: `msg ${id}`, ts, attachments: [], replyCardId: null };
}

function renderChat(unreadCount: number, jumpToMsgId?: string) {
  return render(
    <I18nProvider>
      <ChatArea
        member={mkMember(unreadCount)}
        members={[mkMember(0)]}
        jumpToMsgId={jumpToMsgId}
      />
    </I18nProvider>,
  );
}

// scrollIntoView recorder: jsdom has no layout, so we pin the CALLS — which
// element was asked to scroll into view, with what options.
let scrollCalls: { el: Element; args: unknown }[] = [];

/** Define the scroll viewport's geometry so onScroll's near-bottom math is
 * driven honestly: distance = scrollHeight - scrollTop - clientHeight. */
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

beforeEach(() => {
  localStorage.clear();
  markRead.mockClear();
  scrollCalls = [];
  Element.prototype.scrollIntoView = function (
    this: Element,
    args?: unknown,
  ) {
    scrollCalls.push({ el: this, args });
  } as typeof Element.prototype.scrollIntoView;
  messages = [];
  messagesPeer = "b";
});

describe("② entry positioning (first unread)", () => {
  it("entering with unread lands on the first unread message with a divider", () => {
    // 4 inbound (b→owner) + 1 outgoing; unreadCount=2 → first unread is the
    // 2nd-from-last INBOUND message (c4) — the outgoing c2 never counts.
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "owner", "b", 1001),
      mkMsg("c3", "b", "owner", 1002),
      mkMsg("c4", "b", "owner", 1003),
      mkMsg("c5", "b", "owner", 1004),
    ];
    const { container } = renderChat(2);

    // The divider renders immediately BEFORE the first unread message (c4).
    const divider = container.querySelector(".chat__unread-divider");
    expect(divider).not.toBeNull();
    expect(
      divider!.nextElementSibling?.getAttribute("data-msg-id"),
    ).toBe("c4");

    // Initial scroll anchors TWO rows above the divider (Seth batch-19 LINE
    // spec: 1–2 already-read messages stay visible as context) — c3 is the
    // divider's previous sibling, c2 the one before it — NOT the bottom
    // sentinel.
    const targets = scrollCalls.map((c) => c.el);
    expect(targets).toContain(
      container.querySelector('[data-msg-id="c2"]'),
    );
    expect(
      targets.some((el) => el.classList.contains("chat__scroll-anchor")),
    ).toBe(false);

    // No floating chip on entry — the chip is for NEW arrivals only.
    expect(container.querySelector(".chat__new-msg-chip")).toBeNull();
  });

  it("switching from a NON-EMPTY thread still renders the divider (stale-peer latch regression)", () => {
    // Bug: ChatArea is NOT remounted on a peer switch, and useChat clears its
    // messages one commit AFTER the switch — so the entry-positioning effect
    // used to fire with member.id = NEW peer but messages = the PREVIOUS
    // peer's thread, latching the one-shot against stale data. Entering an
    // unread room FROM a non-empty thread then never rendered the divider.
    //
    // The commit sequence below mirrors the real hook exactly (messagesPeer
    // lags with messages):
    //   1. viewing peer "a" with a settled non-empty thread;
    //   2. switch to peer "b": member flips first, messages/messagesPeer are
    //      still a's for that commit;
    //   3. useChat's reset lands (empty thread, peer "b");
    //   4. b's thread loads (3 inbound, unreadCount snapshot 2).
    const memberA = mkMember(0, "a", "Alma");
    const memberB = mkMember(2, "b", "Beto");

    // ① settled on peer "a" (non-empty thread → positioning latched for a).
    messages = [
      mkMsg("a1", "a", "owner", 900),
      mkMsg("a2", "owner", "a", 901),
    ];
    messagesPeer = "a";
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={memberA} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // ② the stale commit: member is already "b", messages still a's.
    rerender(
      <I18nProvider>
        <ChatArea member={memberB} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // ③ useChat's reset commit: empty thread now owned by "b".
    messages = [];
    messagesPeer = "b";
    rerender(
      <I18nProvider>
        <ChatArea member={memberB} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // ④ b's thread loads: 3 inbound, entry snapshot said 2 unread.
    messages = [
      mkMsg("b1", "b", "owner", 1000),
      mkMsg("b2", "b", "owner", 1001),
      mkMsg("b3", "b", "owner", 1002),
    ];
    rerender(
      <I18nProvider>
        <ChatArea member={memberB} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // The divider MUST render, anchored at the first unread (b2 — the
    // 2nd-from-last inbound). Pre-fix the stale commit ② consumed the
    // one-shot and no divider ever appeared.
    const divider = container.querySelector(".chat__unread-divider");
    expect(divider).not.toBeNull();
    expect(divider!.nextElementSibling?.getAttribute("data-msg-id")).toBe(
      "b2",
    );
  });

  it("entering with no unread lands at the bottom, no divider", () => {
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "owner", "b", 1001),
    ];
    const { container } = renderChat(0);

    expect(container.querySelector(".chat__unread-divider")).toBeNull();
    // The bottom sentinel was scrolled into view (the existing behavior).
    expect(
      scrollCalls.some((c) =>
        c.el.classList.contains("chat__scroll-anchor"),
      ),
    ).toBe(true);
  });
});

describe("① new-message floating chip", () => {
  it("scrolled up + new inbound → chip appears, click jumps to the FIRST unseen, bottom dismisses", () => {
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
      mkMsg("c3", "b", "owner", 1002),
    ];
    const { container, rerender } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;

    // The owner scrolls UP: far from the bottom (distance = 700 > 80).
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    scrollCalls = [];

    // A new inbound message lands.
    messages = [...messages, mkMsg("c4", "b", "owner", 1003)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // Chip appears; the viewport was NOT yanked to the bottom.
    expect(container.querySelector(".chat__new-msg-chip")).not.toBeNull();
    expect(
      scrollCalls.some((c) =>
        c.el.classList.contains("chat__scroll-anchor"),
      ),
    ).toBe(false);

    // MORE messages accumulate — the anchor stays the FIRST unseen (c4).
    messages = [...messages, mkMsg("c5", "b", "owner", 1004)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    expect(
      container.querySelectorAll(".chat__new-msg-chip").length,
    ).toBe(1);

    // Click → smooth-scroll to the first unseen message (c4).
    fireEvent.click(container.querySelector(".chat__new-msg-chip")!);
    const jump = scrollCalls[scrollCalls.length - 1];
    expect(jump.el.getAttribute("data-msg-id")).toBe("c4");
    expect(jump.args).toEqual({ behavior: "smooth", block: "start" });

    // Reaching the bottom dismisses the chip (and marks the newest read —
    // the existing bottom-crossing behavior).
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 800,
    });
    fireEvent.scroll(list);
    expect(container.querySelector(".chat__new-msg-chip")).toBeNull();
    expect(markRead).toHaveBeenCalledWith(1004);
  });

  it("at the bottom + new inbound → auto-follows, NO chip", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    scrollCalls = [];

    messages = [...messages, mkMsg("c2", "b", "owner", 1001)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // Followed the bottom sentinel; no chip surfaced.
    expect(
      scrollCalls.some((c) =>
        c.el.classList.contains("chat__scroll-anchor"),
      ),
    ).toBe(true);
    expect(container.querySelector(".chat__new-msg-chip")).toBeNull();
  });

  it("scrolled up + new inbound → the unread divider anchors at the SAME first unseen message as the chip (owner bug: chip without divider)", () => {
    // Owner report: staying IN the conversation (window foreground), two new
    // messages land → the chip appears, but clicking it showed NO
    // "以下是未讀訊息" divider — the divider only ever anchored at
    // conversation ENTRY and had no path for in-conversation arrivals.
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
    ];
    const { container, rerender } = renderChat(0);
    // Entered with no unread → no divider yet.
    expect(container.querySelector(".chat__unread-divider")).toBeNull();
    const list = container.querySelector(".chat__messages")!;

    // The owner scrolls UP, then TWO new inbound messages land.
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    scrollCalls = [];
    messages = [
      ...messages,
      mkMsg("c3", "b", "owner", 1002),
      mkMsg("c4", "b", "owner", 1003),
    ];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // Chip AND divider — both anchored at the FIRST unseen message (c3).
    expect(container.querySelector(".chat__new-msg-chip")).not.toBeNull();
    const divider = container.querySelector(".chat__unread-divider");
    expect(divider).not.toBeNull();
    expect(divider!.nextElementSibling?.getAttribute("data-msg-id")).toBe(
      "c3",
    );
    // The re-anchor must NOT yank the viewport (no scrollIntoView at all —
    // the entry-positioning scroll is entry-only; the chip is the opt-in jump).
    expect(scrollCalls).toEqual([]);

    // Reading down to the bottom CLOSES the run; the next unseen inbound
    // starts a NEW run → the divider re-anchors there (LINE keeps ONE divider,
    // at the start of the latest unread block).
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 800,
    });
    fireEvent.scroll(list);
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    messages = [...messages, mkMsg("c5", "b", "owner", 1004)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    const moved = container.querySelector(".chat__unread-divider");
    expect(moved).not.toBeNull();
    expect(moved!.nextElementSibling?.getAttribute("data-msg-id")).toBe("c5");
    expect(
      container.querySelectorAll(".chat__unread-divider").length,
    ).toBe(1);
  });

  it("an arrival while the ENTRY unread run is still open keeps the divider at the entry anchor", () => {
    // Entered with unreadCount=2 → divider at c2. The owner never reads down
    // to the bottom; a further inbound EXTENDS the same unread run — the
    // divider must stay at the run's start, not jump to the newest arrival.
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
      mkMsg("c3", "b", "owner", 1002),
    ];
    const { container, rerender } = renderChat(2);
    const divider = container.querySelector(".chat__unread-divider");
    expect(divider).not.toBeNull();
    expect(divider!.nextElementSibling?.getAttribute("data-msg-id")).toBe(
      "c2",
    );

    // Still scrolled up (entry landed at the divider, not the bottom) — a new
    // inbound lands.
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    messages = [...messages, mkMsg("c4", "b", "owner", 1003)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(2)} members={[mkMember(2)]} />
      </I18nProvider>,
    );

    // Chip arms, but the divider stays at the ENTRY anchor (c2): one run.
    expect(container.querySelector(".chat__new-msg-chip")).not.toBeNull();
    const after = container.querySelector(".chat__unread-divider");
    expect(after!.nextElementSibling?.getAttribute("data-msg-id")).toBe("c2");
    expect(
      container.querySelectorAll(".chat__unread-divider").length,
    ).toBe(1);
  });

  it("scrolled up + a new INTER-AGENT message (not addressed to the owner) → no chip", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);

    messages = [...messages, mkMsg("c2", "b", "a", 1001)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    expect(container.querySelector(".chat__new-msg-chip")).toBeNull();
  });
});

describe("③ jump-to-origin (跳到原訊息, B3)", () => {
  it("locates + highlights the target message and suppresses entry positioning", () => {
    // unreadCount=2 would normally anchor the divider — the explicit jump
    // target must own the entry viewport instead.
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
      mkMsg("c3", "b", "owner", 1002),
    ];
    const { container } = renderChat(2, "c2");

    // The target row was center-scrolled and carries the highlight flash.
    const jump = scrollCalls.find(
      (c) => c.el.getAttribute("data-msg-id") === "c2",
    );
    expect(jump).toBeTruthy();
    expect(jump!.args).toEqual({ block: "center" });
    expect(
      container
        .querySelector('[data-msg-id="c2"]')!
        .classList.contains("chat__msg--located"),
    ).toBe(true);
    // Entry positioning was consumed: no divider scroll, no bottom scroll.
    expect(container.querySelector(".chat__unread-divider")).toBeNull();
    expect(
      scrollCalls.some((c) => c.el.classList.contains("chat__scroll-anchor")),
    ).toBe(false);
  });

  it("is one-shot: a later refetch of the same thread never re-scrolls", () => {
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
    ];
    const { rerender } = renderChat(0, "c1");
    expect(
      scrollCalls.some((c) => c.el.getAttribute("data-msg-id") === "c1"),
    ).toBe(true);
    scrollCalls = [];

    // An SSE refetch replaces the array (same content, new identity).
    messages = [...messages];
    rerender(
      <I18nProvider>
        <ChatArea
          member={mkMember(0)}
          members={[mkMember(0)]}
          jumpToMsgId="c1"
        />
      </I18nProvider>,
    );
    expect(
      scrollCalls.some((c) => c.el.getAttribute("data-msg-id") === "c1"),
    ).toBe(false);
  });

  it("a target outside the loaded window falls back to the bottom (honest miss)", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat(0, "c-ancient");

    expect(
      scrollCalls.some((c) => c.el.classList.contains("chat__scroll-anchor")),
    ).toBe(true);
    expect(container.querySelector(".chat__msg--located")).toBeNull();
  });
});
