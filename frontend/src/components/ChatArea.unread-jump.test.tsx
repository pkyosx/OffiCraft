// LINE/FB-style unread jump (M2 batch 19) — black-box pins on ChatArea:
//
//   ① THE ROUND 回到最新訊息 ARROW: shows whenever the NEWEST message is not in
//      the viewport — scrolling up alone is enough, no arrival required — and
//      hides again at the bottom. Clicking it goes to the LATEST message.
//   ②' THE NEW-MESSAGE PREVIEW STRIP (T-48, replaces the 「有新訊息」 pill): a
//      new inbound message while scrolled up puts ONE strip above the composer
//      carrying the sender and the line. A further arrival REPLACES it — never
//      a second strip. It and the arrow are MUTUALLY EXCLUSIVE. Clicking it
//      goes to the LATEST message; its x drops it (after which the arrow takes
//      over, because dismissing a preview is not reading it). At the bottom the
//      existing auto-follow stays: new message → follow, no strip, no arrow.
//   ② ENTRY POSITIONING: entering a conversation whose roster badge carried
//      unreadCount > 0 lands on the FIRST unread message (an "以下是未讀訊息"
//      divider pinned to the top of the viewport), derived from the
//      unreadCount SNAPSHOT taken at entry — race-free against ChatArea's own
//      mark-read, which fires as soon as the first page lands on a focused
//      window and drives the badge to 0. No unread → the existing
//      land-at-bottom.
//
//   ③ B3 跳到原訊息 (jumpToMsgId): entering with a message target locates it
//      (center scroll + transient highlight) and OWNS the entry positioning
//      (no divider/bottom scroll fights it); one-shot — a later refetch never
//      re-scrolls; a target outside the loaded window is FETCHED as an anchor
//      window (T-48 ③) and only a target that genuinely cannot be located
//      falls back to the bottom.
//
// jsdom cannot really scroll, so scrollIntoView is stubbed to record its
// element + args, and the viewport geometry (scrollHeight/clientHeight/
// scrollTop) is defined per test to drive the near-bottom detection.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import type { JumpOutcome } from "../hooks/useChat";

// Window peer = agent "b" (Beto). Owner id is "owner".
let messages: ChatMessage[] = [];
const markRead = vi.fn(() => Promise.resolve());
// ③ T-48: the anchor-window fetch behind 跳到原訊息. `loadAroundResult` is what
// the fetch reports back — see useChat's JumpOutcome: the target is in the
// thread ("found"), the id names nothing here ("missing"), or a later load
// committed on top of ours and the message is still perfectly present
// ("superseded").
const loadAround = vi.fn(async (_id: string) => loadAroundResult);
let loadAroundResult: JumpOutcome = "found";
// ③ T-48: the thread is an ANCHOR WINDOW with live messages below it (useChat's
// `hasNewer`), and the way back to the tail.
let hasNewer = false;
const resetToLatest = vi.fn(async () => {});
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead,
    loadAround,
    hasNewer,
    resetToLatest,
  }),
}));

function mkMember(unreadCount: number, id = "b", name = "Beto"): Member {
  return {
    id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
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

/** jsdom has no layout, so the ARROW's question — is the newest ROW inside the
 * viewport — has to be modelled as well as the box's scroll numbers. This
 * models the simplest box there is: nothing in it but rows, the last row's
 * bottom edge sitting exactly at the bottom of the scrollable content. So a
 * viewport `distance` px from the bottom of the box is also `distance` px from
 * the bottom of the newest row, which is the arithmetic every case below was
 * written against.
 *
 * 🔴 THE SHIPPED BOX IS NOT THAT BOX — it has a 12px flex gap and a sentinel
 * under the last row, so the two distances differ by 12 — and that difference
 * is the bug this models the fix for. The case that pins it does not use this
 * helper's assumption; it sets the row bottom itself
 * (見「捲到底部但最後一列下面還有 12px 版面空隙」). */
function stubLayout() {
  const rect = (bottom: number) =>
    ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      width: 0,
      height: bottom,
      bottom,
      toJSON: () => ({}),
    }) as DOMRect;
  Element.prototype.getBoundingClientRect = function (this: Element) {
    if (this.classList.contains("chat__messages")) {
      return rect((this as HTMLElement).clientHeight);
    }
    const box = this.closest(".chat__messages") as HTMLElement | null;
    if (!box || !this.hasAttribute("data-msg-id")) return rect(0);
    const rows = box.querySelectorAll("[data-msg-id]");
    if (rows[rows.length - 1] !== this) return rect(0);
    const trailing = rowTrailingGap;
    const distance = box.scrollHeight - box.scrollTop - box.clientHeight;
    return rect(box.clientHeight + distance - trailing);
  };
}
/** How much of the box's bottom `distance` is layout BELOW the newest row —
 * `.chat__messages`' flex gap plus the zero-height `endRef` sentinel — rather
 * than the newest row being cut off. The exact number is not the point and
 * production reads nothing like it: any non-zero value must give the same
 * answers, because the box's bottom and the newest row's bottom are simply not
 * the same edge. It is 12 here because that is what the shipped stylesheet
 * measures, so these cases run the geometry the owner actually sees. */
const TRAILING_LAYOUT_PX = 12;
let rowTrailingGap = TRAILING_LAYOUT_PX;

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
  // jsdom reports the window as UNFOCUSED, and ChatArea's read receipt is gated
  // on `useWindowActive()` — leaving the default in place would make every
  // "did not mark read" assertion below true for the wrong reason. The cockpit
  // under test is the one the owner is looking at.
  document.hasFocus = () => true;
  markRead.mockClear();
  loadAround.mockReset();
  loadAround.mockImplementation(async () => loadAroundResult);
  loadAroundResult = "found";
  resetToLatest.mockClear();
  hasNewer = false;
  scrollCalls = [];
  rowTrailingGap = TRAILING_LAYOUT_PX;
  stubLayout();
  Element.prototype.scrollIntoView = function (
    this: Element,
    args?: unknown,
  ) {
    scrollCalls.push({ el: this, args });
    // …and MOVE THE VIEWPORT, because where it lands is half of what the arrow
    // is asking about. jsdom's own scrollIntoView does nothing at all, so a
    // component that scrolls to the newest row and then measures whether the
    // newest row is visible would be measuring the position it started from —
    // and a test asserting "the arrow is gone" would only be able to pass on an
    // optimistic guess, which is exactly the shape T-48 removed. `block: "end"`
    // puts the target's bottom edge flush with the viewport's, leaving whatever
    // layout follows it (the flex gap, the sentinel) still below the fold.
    const box = this.closest?.(".chat__messages") as HTMLElement | null;
    if (!box) return;
    const bottom = box.scrollHeight - box.clientHeight;
    box.scrollTop = this.hasAttribute("data-msg-id")
      ? Math.max(0, bottom - rowTrailingGap)
      : bottom;
  } as typeof Element.prototype.scrollIntoView;
  messages = [];
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

    // Entry scroll anchors the actual unread boundary.  A compact phone or
    // desktop pane cannot afford an older-row offset: it hides c4 below fold.
    const targets = scrollCalls.map((c) => c.el);
    expect(targets).toContain(
      divider,
    );
    expect(
      targets.some((el) => el.classList.contains("chat__scroll-anchor")),
    ).toBe(false);

    // No preview strip on entry — the strip is for NEW arrivals only.
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
  });

  it("entering an unread room FROM a non-empty thread still renders the divider", () => {
    // 🔴 THE ROOM IS ENTERED BY MOUNTING (T-48, R13-5). `OfficePage` renders
    // `ChatArea` under `key={peerId}`, so leaving a settled non-empty thread for
    // an unread room builds a NEW component: the entry-positioning one-shot is
    // fresh, because it belongs to the mount.
    //
    // The bug this replaces was reachable only while one instance was reused
    // across rooms — the effect fired for a commit whose `member.id` was already
    // the new peer while `messages` was still the previous peer's thread, and
    // latched the one-shot against it, so the divider never rendered. What is
    // asserted here is the OUTCOME that regression was about, driven the way the
    // app drives it.
    const memberA = mkMember(0, "a", "Alma");
    const memberB = mkMember(2, "b", "Beto");

    // ① settled on peer "a" (non-empty thread → positioning latched for a).
    messages = [
      mkMsg("a1", "a", "owner", 900),
      mkMsg("a2", "owner", "a", 901),
    ];
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={memberA.id} member={memberA} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // ② switch to "b": the key changes, so this unmounts A and mounts B. Its
    // thread starts empty, exactly as useChat's own state does on a fresh mount.
    messages = [];
    rerender(
      <I18nProvider>
        <ChatArea key={memberB.id} member={memberB} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // ③ b's thread loads: 3 inbound, entry snapshot said 2 unread.
    messages = [
      mkMsg("b1", "b", "owner", 1000),
      mkMsg("b2", "b", "owner", 1001),
      mkMsg("b3", "b", "owner", 1002),
    ];
    rerender(
      <I18nProvider>
        <ChatArea key={memberB.id} member={memberB} members={[memberA, memberB]} />
      </I18nProvider>,
    );

    // The divider MUST render, anchored at the first unread (b2 — the
    // 2nd-from-last inbound).
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

describe("① 回到最新箭頭 + ② 新訊息預覽列", () => {
  it("scrolling up alone raises the arrow — no arrival required — and the bottom hides it again", () => {
    // 🔴 THE CONDITION IS 「最新那一則不在視窗內」 (owner rc-72054864ff88), not
    // "a new message arrived" and not "scrolled more than a screen". The pill
    // this replaces had the arrival condition, so a reader who had scrolled up
    // to read history had NO way back to the bottom until somebody wrote.
    messages = [
      mkMsg("c1", "b", "owner", 1000),
      mkMsg("c2", "b", "owner", 1001),
    ];
    const { container } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();

    // Only 200px of content below the fold — well under one screen.
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 800,
      scrollTop: 0,
    });
    fireEvent.scroll(list);
    expect(
      container.querySelector("[data-testid='chat-jump-latest']"),
    ).not.toBeNull();
    // Nothing arrived, so there is no strip to show.
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();

    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 800,
      scrollTop: 200,
    });
    fireEvent.scroll(list);
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();

    // 🔴 AND THE ONE THAT WAS SHIPPED BROKEN (T-48): the box does not end where
    // the newest row ends. `.chat__messages` puts a flex gap and a zero-height
    // sentinel under the last row, so a viewport with the newest message
    // FLUSH AGAINST ITS BOTTOM still reports `TRAILING_LAYOUT_PX` of box left
    // below — and the arrow used to come back for it, permanently, every time
    // 回到最新 landed. Measured in the browser 12/12 runs at both widths.
    // Nothing here says what that layout is worth: raise TRAILING_LAYOUT_PX to
    // 40 and this case must still pass, because the question is about the row.
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 800,
      scrollTop: 200 - TRAILING_LAYOUT_PX,
    });
    fireEvent.scroll(list);
    expect(
      container.querySelector("[data-testid='chat-jump-latest']"),
      "最新那一則整列都在視窗裡 ⇒ 沒有「回到最新」這回事",
    ).toBeNull();
  });

  it("scrolled up + new inbound → ONE preview strip that REPLACES its content, never a second one, and the arrow gives way to it", () => {
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
    // Scrolled up with nothing new: the arrow, alone.
    expect(
      container.querySelector("[data-testid='chat-jump-latest']"),
    ).not.toBeNull();
    scrollCalls = [];

    // A new inbound message lands.
    messages = [...messages, mkMsg("c4", "b", "owner", 1003)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // 🔴 MUTUAL EXCLUSION. The strip took the arrow's place; both on screen at
    // once is the owner's 「兩者互斥」 ruling broken, and it is the mutant the
    // obvious `!latestInView` arrow condition produces.
    const strip = container.querySelector("[data-testid='chat-new-msg-preview']");
    expect(strip).not.toBeNull();
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();
    // Sender + the line itself — the whole reason the pill was replaced.
    expect(strip!.textContent).toContain("Beto");
    expect(strip!.textContent).toContain("msg c4");
    // The viewport was NOT yanked to the bottom.
    expect(
      scrollCalls.some((c) => c.el.classList.contains("chat__scroll-anchor")),
    ).toBe(false);

    // MORE messages accumulate → still exactly ONE strip, now showing the
    // LATEST arrival. (The pill had a constant label and so could not stack
    // visibly; a strip with content can, and must not.)
    messages = [...messages, mkMsg("c5", "b", "owner", 1004)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    expect(
      container.querySelectorAll("[data-testid='chat-new-msg-preview']").length,
    ).toBe(1);
    const restrip = container.querySelector(
      "[data-testid='chat-new-msg-preview']",
    )!;
    expect(restrip.textContent).toContain("msg c5");
    expect(restrip.textContent).not.toContain("msg c4");

    // Click → land on the LATEST message (c5), not the first unseen (c4).
    // 🔴 THIS IS ③. The old chip jumped to the first unseen one, so a burst of
    // arrivals left the reader mid-block with the rest still below the fold —
    // reproduced in the isolated environment with ten messages. The first-unseen
    // position is still marked, by the divider, which does not move.
    fireEvent.click(
      container.querySelector("[data-testid='chat-new-msg-jump']")!,
    );
    const jump = scrollCalls[scrollCalls.length - 1];
    expect(jump.el.getAttribute("data-msg-id")).toBe("c5");
    expect(jump.args).toEqual({ block: "end" });
    // The strip is consumed by the jump; the arrow does not come back either,
    // because we are now looking at the newest message.
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();
  });

  it("the strip's x drops it and hands the place back to the arrow — dismissing is not reading", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    messages = [...messages, mkMsg("c2", "b", "owner", 1001)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).not.toBeNull();
    scrollCalls = [];

    fireEvent.click(
      container.querySelector("[data-testid='chat-new-msg-dismiss']")!,
    );
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
    // The newest message is still off screen ⇒ the arrow, and no scrolling.
    expect(
      container.querySelector("[data-testid='chat-jump-latest']"),
    ).not.toBeNull();
    expect(scrollCalls).toEqual([]);
  });

  it("正在回覆某則時，預覽列排在回覆橫幅上面", () => {
    // owner 指定的順序。版面上「誰在上面」由兩件事決定：DOM 順序（這裡）與 CSS
    // 有沒有把它翻回去（visual-guards/chat-bottom-affordance.ct.spec.tsx 量的）。
    // 只驗其中一件都會漏 —— jsdom 看得到順序但看不到版面，真瀏覽器量得到版面但
    // 那個順序是 story 自己寫的。
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    messages = [...messages, mkMsg("c2", "b", "owner", 1001)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // 對第一則按「回覆這則」，把橫幅叫出來。
    fireEvent.click(container.querySelectorAll(".chat__msg-reply")[0]);
    const strip = container.querySelector(
      "[data-testid='chat-new-msg-preview']",
    )!;
    const banner = container.querySelector(
      "[data-testid='chat-reply-banner']",
    )!;
    expect(strip).not.toBeNull();
    expect(banner).not.toBeNull();
    expect(
      strip.compareDocumentPosition(banner) &
        Node.DOCUMENT_POSITION_FOLLOWING,
      "預覽列必須排在回覆橫幅前面",
    ).toBeTruthy();
  });

  it("reaching the bottom drops the strip and marks the newest read", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    messages = [...messages, mkMsg("c2", "b", "owner", 1004)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).not.toBeNull();

    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 200,
      scrollTop: 800,
    });
    fireEvent.scroll(list);
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();
    expect(markRead).toHaveBeenCalledWith(1004);
  });

  it("at the bottom + new inbound → auto-follows, NO strip and NO arrow", () => {
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0);
    scrollCalls = [];

    messages = [...messages, mkMsg("c2", "b", "owner", 1001)];
    rerender(
      <I18nProvider>
        <ChatArea member={mkMember(0)} members={[mkMember(0)]} />
      </I18nProvider>,
    );

    // Followed the bottom sentinel; neither affordance surfaced.
    expect(
      scrollCalls.some((c) =>
        c.el.classList.contains("chat__scroll-anchor"),
      ),
    ).toBe(true);
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
    expect(container.querySelector("[data-testid='chat-jump-latest']")).toBeNull();
  });

  it("scrolled up + new inbound → the unread divider anchors at the FIRST unseen message while the strip shows the LAST", () => {
    // Owner report: staying IN the conversation (window foreground), two new
    // messages land → the affordance appears, but jumping showed NO
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

    // The divider marks where the unread block STARTS (c3); the strip previews
    // what most recently arrived (c4). Two ends of the same batch, on purpose.
    const strip = container.querySelector(
      "[data-testid='chat-new-msg-preview']",
    );
    expect(strip).not.toBeNull();
    expect(strip!.textContent).toContain("msg c4");
    const divider = container.querySelector(".chat__unread-divider");
    expect(divider).not.toBeNull();
    expect(divider!.nextElementSibling?.getAttribute("data-msg-id")).toBe(
      "c3",
    );
    // The re-anchor must NOT yank the viewport (no scrollIntoView at all —
    // the entry-positioning scroll is entry-only; the jump is opt-in).
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

    // The strip is up, but the divider stays at the ENTRY anchor (c2): one run.
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).not.toBeNull();
    const after = container.querySelector(".chat__unread-divider");
    expect(after!.nextElementSibling?.getAttribute("data-msg-id")).toBe("c2");
    expect(
      container.querySelectorAll(".chat__unread-divider").length,
    ).toBe(1);
  });

  it("scrolled up + a new INTER-AGENT message (not addressed to the owner) → no strip", () => {
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
    expect(
      container.querySelector("[data-testid='chat-new-msg-preview']"),
    ).toBeNull();
    // The arrow stays: the newest message is still off screen, whoever it is
    // addressed to.
    expect(
      container.querySelector("[data-testid='chat-jump-latest']"),
    ).not.toBeNull();
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

  it("捲到底不再往新撈一頁 —— 走訪拿掉之後,捲動只是捲動", async () => {
    // 🔴 刪除的分母(T-48 fix12)。以前捲到錨點窗底部會買一頁往新的;`loadAround`
    // 現在自己撈到活尾巴,所以那一支整個不存在。這條釘的是**它真的不見了**:
    // 「一次手勢一頁」那一整套(400ms 節流、被吞手勢的補送、視覺閘、served
    // anchor)全部是為它養的,任何一塊活著回來都會先在這裡顯形。
    //
    // 走訪停在半路(hasNewer 仍為 true)是最會誘人把它加回來的地形,所以就用它。
    hasNewer = true;
    messages = [mkMsg("a1", "b", "owner", 100), mkMsg("a2", "b", "owner", 101)];
    const { container } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 300,
      scrollTop: 700,
    });
    fireEvent.scroll(list);

    // 出口是箭頭,不是手勢 —— 而箭頭在(下一條釘它按下去做什麼)。
    expect(
      container.querySelector('[data-testid="chat-jump-latest"]'),
      "走訪停在半路時,唯一的出口是箭頭",
    ).not.toBeNull();
  });

  it("the arrow is still there at the BOTTOM of an anchor window, and clicking it FETCHES the live tail", () => {
    // 🔴 The half of the jump nobody sees coming. After landing on an old
    // message the thread is a window from the middle of the history, so
    // "the last row on screen" and "the newest message" stop being the same
    // row. An arrow that only scrolls would move the viewport and leave the
    // owner still in the past — the same lie in a new place.
    hasNewer = true;
    messages = [mkMsg("a1", "b", "owner", 100), mkMsg("a2", "b", "owner", 101)];
    const { container } = renderChat(0);
    const list = container.querySelector(".chat__messages")!;
    // Geometry says the viewport IS at the bottom — of the loaded window.
    setScrollGeometry(list, {
      scrollHeight: 200,
      clientHeight: 200,
      scrollTop: 0,
    });
    fireEvent.scroll(list);

    const arrow = container.querySelector(
      "[data-testid='chat-jump-latest']",
    ) as HTMLButtonElement;
    expect(arrow).not.toBeNull();
    scrollCalls = [];
    fireEvent.click(arrow);

    expect(resetToLatest).toHaveBeenCalledTimes(1);
    // …and it does NOT settle on the last loaded row, which is not the newest.
    expect(
      scrollCalls.some((c) => c.el.getAttribute("data-msg-id") === "a2"),
    ).toBe(false);
  });

  it("a target outside the loaded window is FETCHED, and the jump lands on it once the window arrives", async () => {
    // 🔴 THE DEFECT THIS TICKET EXISTS FOR. The thread opens on the newest 30
    // rows; a target older than that was never in the DOM, so the jump scrolled
    // to the bottom — pixel-identical to a jump that succeeded on a recent
    // message. Now the window around the id is fetched (useChat.loadAround,
    // `?end_id=` + `?start_id=`), and the landing happens on the row itself.
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0, "c-ancient");

    expect(loadAround).toHaveBeenCalledWith("c-ancient");
    // NOT the bottom, and NOT a fake highlight: while the fetch is in flight
    // the jump has simply not happened yet.
    expect(
      scrollCalls.some((c) => c.el.classList.contains("chat__scroll-anchor")),
    ).toBe(false);
    // 🔴 …and NOT read either. Arriving through a jump link mounts the thread on
    // the NEWEST window for the moment the anchor fetch is in flight; marking
    // that read would consume the whole unread run before the reader has been
    // taken anywhere near it.
    expect(markRead).not.toHaveBeenCalled();

    // The anchor window lands — this is what useChat's loadAround commit does.
    messages = [
      mkMsg("c-ancient", "b", "owner", 10),
      mkMsg("c-after", "b", "owner", 11),
    ];
    await act(async () => {
      rerender(
        <I18nProvider>
          <ChatArea
            member={mkMember(0)}
            members={[mkMember(0)]}
            jumpToMsgId="c-ancient"
          />
          ,
        </I18nProvider>,
      );
    });

    const jump = scrollCalls.find(
      (c) => c.el.getAttribute("data-msg-id") === "c-ancient",
    );
    expect(jump).toBeTruthy();
    expect(jump!.args).toEqual({ block: "center" });
    expect(
      container
        .querySelector('[data-msg-id="c-ancient"]')!
        .classList.contains("chat__msg--located"),
    ).toBe(true);
    // ONE fetch for one target, however many times the thread re-renders.
    expect(loadAround).toHaveBeenCalledTimes(1);
  });

  it("跳轉進房時,房間還一列都沒有就先撈錨點視窗", async () => {
    // 🔴 ANCHOR-FIRST ENTRY (T-48, owner ruling: 「你應該直接打成我們希望的流程」).
    // useChat no longer fetches a newest page when the room is entered at an
    // anchor, so the thread this reactor sees on the first commit is EMPTY. It
    // used to wait for `messages.length > 0` — which, on this path, never comes:
    // the room would sit blank forever. An empty thread has nothing in the DOM,
    // so the fetch branch is exactly where it belongs.
    messages = [];
    const { container } = renderChat(0, "c-ancient");

    expect(loadAround).toHaveBeenCalledWith("c-ancient");
    // Nothing is claimed about a message nobody has fetched yet.
    //
    // ⚠️ THIS USED TO BE `expect(markRead).not.toHaveBeenCalled()`, WHICH
    // MEASURED NOTHING: with `messages = []` there is no ts to stamp, so the
    // line stayed green with the gate mutated to `mayMarkRead = true`. (The
    // gate IS measured — by the case above, whose thread has a row in it.)
    // What is genuinely at stake on THIS path is that an empty room does not
    // pretend: no fallback to the bottom and no verdict on screen while the
    // window is still in the air. (「不准先落到底」 cannot be asserted from
    // here: an empty thread renders no bottom sentinel at all, so a scroll
    // assertion on it would be green whatever the code did.)
    expect(
      resetToLatest,
      "錨點還在飛就先撈一頁最新的,正是這張票拿掉的那格中間畫面",
    ).not.toHaveBeenCalled();
    expect(
      container.querySelector(".chat__jump-miss"),
      "還沒撈完就不准先下任何結論",
    ).toBeNull();
  });

  it("被別的載入超車不是「找不到」—— 重排一次就落在那一則,一句話都不用說", async () => {
    // 🔴 T-48 F3. `loadAround` used to answer a bare false for three unrelated
    // facts, and one of them was "a newer load committed while our two windows
    // were in the air". The message is still there; the screen said 「找不到那則
    // 訊息,可能已經被清掉了」 and, because the fetch latch was already spent,
    // offered no retry and no button to ask for one.
    let calls = 0;
    loadAround.mockImplementation(async () => {
      calls += 1;
      return calls === 1 ? "superseded" : "found";
    });
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });

    // Overtaken once ⇒ tried again, and nothing on screen accused the server of
    // losing a message it still has.
    expect(loadAround).toHaveBeenCalledTimes(2);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();

    // The window lands on the second attempt and the jump does what it came for.
    messages = [mkMsg("c-ancient", "b", "owner", 10), mkMsg("c-after", "b", "owner", 11)];
    await act(async () => {
      rerender(
        <I18nProvider>
          <ChatArea
            member={mkMember(0)}
            members={[mkMember(0)]}
            jumpToMsgId="c-ancient"
          />
        </I18nProvider>,
      );
    });
    expect(
      container
        .querySelector('[data-msg-id="c-ancient"]')!
        .classList.contains("chat__msg--located"),
    ).toBe(true);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("超車到放棄重試,說的也是「被打斷」,不是「訊息不見了」", async () => {
    // The retry is BOUNDED — an unbounded one is a fetch loop — so there has to
    // be an ending, and the ending may not borrow the miss sentence. Two facts,
    // two sentences: this one is true and it tells the reader what to do.
    let calls = 0;
    loadAround.mockImplementation(async () => {
      calls += 1;
      // Every automatic attempt loses the race; so does the FIRST attempt the
      // button asks for. Only the one after that lands — which is only
      // reachable if pressing the button restores the re-schedule budget.
      return calls <= 5 ? "superseded" : "found";
    });
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });

    const notice = container.querySelector(".chat__jump-miss");
    expect(notice).not.toBeNull();
    expect(notice!.textContent).toContain(zh.chat.jumpTargetInterrupted);
    expect(notice!.textContent).not.toContain(zh.chat.jumpTargetMissing);

    // 🔴 …AND THE NEXT STEP IT NAMES HAS TO EXIST (R3-5). The sentence used to
    // read 「再點一次連結可以重試」 and the same link could not re-fire: the jump
    // latch is spent and the hash has not changed, so there is no `hashchange`,
    // no re-render, and the reactor's first guard returns immediately. Asserting
    // the SENTENCE alone would have stayed green through all of that — the thing
    // worth pinning is that the reader has a way through.
    const before = loadAround.mock.calls.length;
    const retry = notice!.querySelector('[data-testid="jump-miss-retry"]');
    expect(retry, "被打斷也要給得出一條真的按得下去的路").not.toBeNull();
    await act(async () => {
      fireEvent.click(retry!);
      await Promise.resolve();
      await Promise.resolve();
    });
    // TWO more attempts: the button's own, and the re-schedule after it was
    // overtaken again. A button that hands back a SPENT budget would stop after
    // the first — 鈕在、按得下去、什麼都沒發生, which is the same defect wearing
    // a different hat.
    expect(
      loadAround.mock.calls.length,
      "按下重試要真的再撈一次,而且拿回完整的重排額度",
    ).toBe(before + 2);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("按下回到最新就結束了那次跳轉 —— 不重排、也不冒出提示", async () => {
    // 🔴 The arrow and the preview strip both mean "take me to the newest
    // message", said by the owner, and they are the one thing allowed to
    // overtake an anchor fetch. Without spending the jump latch here the two
    // fight: the fetch comes back superseded, the reactor re-schedules, and the
    // owner is dragged back to the old message they just asked to leave.
    hasNewer = true;
    let settleJump!: (o: JumpOutcome) => void;
    loadAround.mockImplementation(
      () =>
        new Promise<JumpOutcome>((r) => {
          settleJump = r;
        }),
    );
    messages = [mkMsg("a1", "b", "owner", 100), mkMsg("a2", "b", "owner", 101)];
    const { container } = renderChat(0, "c-ancient");
    expect(loadAround).toHaveBeenCalledTimes(1);

    const arrow = container.querySelector(
      "[data-testid='chat-jump-latest']",
    ) as HTMLButtonElement;
    expect(arrow).not.toBeNull();
    fireEvent.click(arrow);
    expect(resetToLatest).toHaveBeenCalledTimes(1);

    await act(async () => {
      settleJump("superseded");
      await Promise.resolve();
    });

    // The jump is over because the owner ended it — not retried, and above all
    // nothing on screen apologises for a message that was never missing.
    expect(loadAround).toHaveBeenCalledTimes(1);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("讀取失敗說的是「現在讀不到」而且給得出重試,真的不見了才說「被清掉了」", async () => {
    // 🔴 兩個方向,同一支測試,而且缺一不可:只釘「讀取失敗要說新那句」的話,把兩句
    // 合成一句(兩種都說「現在讀不到」)照樣會過;只釘 404 那句的話,合成另一句也會過。
    // 對使用者而言這兩句導向完全相反的下一步 —— 一句叫他別再試,一句叫他再試一次。
    messages = [mkMsg("c1", "b", "owner", 1000)];

    loadAroundResult = "unreachable";
    const failed = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });
    const notice = failed.container.querySelector(".chat__jump-miss")!;
    expect(notice).not.toBeNull();
    expect(notice.textContent).toContain(zh.chat.jumpTargetUnreachable);
    expect(
      notice.textContent,
      "訊息還在,不准說它被清掉了",
    ).not.toContain(zh.chat.jumpTargetMissing);
    // …而且讀者按得到重試。
    expect(
      failed.container.querySelector("[data-testid='jump-miss-retry']"),
    ).not.toBeNull();
    failed.unmount();

    // 反方向:server 真的說沒有這一列,那句就得是「被清掉了」,而且不給重試 ——
    // 再試一次不會有別的答案。
    loadAround.mockClear();
    loadAroundResult = "missing";
    const gone = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });
    const goneNotice = gone.container.querySelector(".chat__jump-miss")!;
    expect(goneNotice.textContent).toContain(zh.chat.jumpTargetMissing);
    expect(goneNotice.textContent).not.toContain(zh.chat.jumpTargetUnreachable);
    expect(
      gone.container.querySelector("[data-testid='jump-miss-retry']"),
    ).toBeNull();
  });

  it("按重試會真的再撈一次,而且撈到就落在那一則", async () => {
    // ⚠️ 這一格是 F3 的形狀在別處復發的地方:落空時 `jumpFetchedRef` 已經寫掉了,
    // 只清它而不清 `jumpConsumedRef`(或反過來),reactor 的第一道 guard 就會直接
    // return —— 按鈕在、按得下去、什麼都不會發生,而且畫面上完全看不出來。
    let calls = 0;
    loadAround.mockImplementation(async () => {
      calls += 1;
      return calls === 1 ? "unreachable" : "found";
    });
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container, rerender } = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });
    expect(loadAround).toHaveBeenCalledTimes(1);

    await act(async () => {
      fireEvent.click(
        container.querySelector("[data-testid='jump-miss-retry']")!,
      );
      await Promise.resolve();
    });
    expect(loadAround, "按了重試就要真的再撈一次").toHaveBeenCalledTimes(2);

    // 這一次撈到了 —— 視窗換上來,跳轉就落在那一則,提示也收掉。
    messages = [mkMsg("c-ancient", "b", "owner", 10), mkMsg("c-after", "b", "owner", 11)];
    await act(async () => {
      rerender(
        <I18nProvider>
          <ChatArea
            member={mkMember(0)}
            members={[mkMember(0)]}
            jumpToMsgId="c-ancient"
          />
        </I18nProvider>,
      );
    });
    expect(
      container
        .querySelector('[data-msg-id="c-ancient"]')!
        .classList.contains("chat__msg--located"),
    ).toBe(true);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("falls back to the bottom only when the target really cannot be located", async () => {
    // The fetch is the difference between "not loaded yet" and "not there".
    // Only the second one may land at the bottom.
    loadAroundResult = "missing";
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const { container } = renderChat(0, "c-ancient");
    await act(async () => {
      await Promise.resolve();
    });

    expect(loadAround).toHaveBeenCalledWith("c-ancient");
    expect(
      scrollCalls.some((c) => c.el.classList.contains("chat__scroll-anchor")),
    ).toBe(true);
    expect(container.querySelector(".chat__msg--located")).toBeNull();
    // 🔴 …AND IT SAYS SO. Landing at the bottom without a word is pixel-for-pixel
    // a jump that worked, which is the silence this whole ticket is about. The
    // notice is asserted by its TEXT, not by the node alone — an empty box would
    // satisfy a class-only assertion.
    const miss = container.querySelector(".chat__jump-miss");
    expect(miss).not.toBeNull();
    // 🔑 And the read watermark is UNBLOCKED again: the jump is over (it failed),
    // the thread is the live tail, and the owner is looking at it. A gate that
    // stayed shut here would be the "never marks read at all" silent failure.
    expect(markRead).toHaveBeenCalledWith(1000);
    expect(miss!.textContent).toContain(zh.chat.jumpTargetMissing);
    // …and the reader can put it away.
    fireEvent.click(miss!.querySelector("button")!);
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("沒有跳轉、或跳轉找到了,都不會冒出「找不到那則訊息」", async () => {
    // The other direction of the same guardrail: a notice that is always on is
    // as uninformative as one that never is. Both no-jump-at-all and a jump
    // that landed have to stay quiet.
    messages = [mkMsg("c1", "b", "owner", 1000)];
    const plain = renderChat(0);
    expect(plain.container.querySelector(".chat__jump-miss")).toBeNull();
    plain.unmount();

    const { container } = renderChat(0, "c1");
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.querySelector(".chat__msg--located")).not.toBeNull();
    expect(container.querySelector(".chat__jump-miss")).toBeNull();
  });

  it("錨點視窗不標已讀 —— 跳過去不等於看過,回到最新那一端才標", async () => {
    // 🔴 The same shape as the defect this ticket removed, pointing the other
    // way: after a jump the thread is a window from the middle of the history,
    // so its last row is NOT the newest message. Marking read to that watermark
    // declares the whole unfetched stretch below it "seen" — messages nobody
    // ever looked at. Owner ruling: mark-read must mean 「我看過了」.
    hasNewer = true;
    messages = [mkMsg("a1", "b", "owner", 100), mkMsg("a2", "b", "owner", 101)];
    const { container, rerender } = renderChat(0, "a1");
    await act(async () => {
      await Promise.resolve();
    });

    // ① entering the anchor window marks nothing read…
    expect(markRead).not.toHaveBeenCalled();

    // …and neither does reaching the bottom of the BOX, which is not the bottom
    // of the THREAD.
    const list = container.querySelector(".chat__messages")!;
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 300,
      scrollTop: 100,
    });
    fireEvent.scroll(list);
    setScrollGeometry(list, {
      scrollHeight: 1000,
      clientHeight: 300,
      scrollTop: 700,
    });
    fireEvent.scroll(list);
    expect(markRead).not.toHaveBeenCalled();

    // ② 🔑 the other direction, and it is NOT optional: gating alone would also
    // be satisfied by a mark-read path that is simply broken forever, which is
    // its own silent failure. Once the walk (or the 回到最新 arrow) reaches the
    // live tail, `hasNewer` goes false and the watermark is stamped — at the
    // real newest message.
    hasNewer = false;
    messages = [...messages, mkMsg("a3", "b", "owner", 102)];
    await act(async () => {
      rerender(
        <I18nProvider>
          <ChatArea
            member={mkMember(0)}
            members={[mkMember(0)]}
            jumpToMsgId="a1"
          />
        </I18nProvider>,
      );
    });

    expect(markRead).toHaveBeenCalledWith(102);
  });
});
