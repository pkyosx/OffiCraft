// LINE/Slack-style day dividers in the chat stream — component-level pins:
// the stream renders ONE centered date pill per local calendar day (今天 /
// 昨天 / a dated label), messages land under their own day's divider, and a
// same-day thread gets exactly one. The pure grouping/label math is pinned in
// lib/dateFormat.test.ts; these tests pin the ChatArea wiring (day-group
// wrapper + pill per group + i18n label). The clock is FROZEN via fake
// timers — no Date.now() flakiness — and all message ts are built with the
// local-time Date constructor so the pins hold in any timezone.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "b",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
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
    tmuxSession: "member-b",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

/** Local-time epoch seconds for y/m/d h:mm (month 1-based). */
function ts(y: number, mo: number, d: number, h = 12, mi = 0): number {
  return new Date(y, mo - 1, d, h, mi, 0, 0).getTime() / 1000;
}

function mkMsg(id: string, tsSec: number, from = "b", to = "owner"): ChatMessage {
  return {
    id,
    from,
    to,
    body: `msg ${id}`,
    ts: tsSec,
    attachments: [],
    replyCardId: null,
  };
}

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={mkMember()} members={[mkMember()]} />
    </I18nProvider>,
  );
}

// Frozen clock: 2026-07-13 10:00 local (a Monday).
const NOW = new Date(2026, 6, 13, 10, 0, 0, 0);

beforeEach(() => {
  localStorage.clear();
  vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
  Element.prototype.scrollIntoView = vi.fn();
  messages = [];
});

afterEach(() => {
  vi.useRealTimers();
});

function pillLabels(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll(".chat__day-pill")).map(
    (el) => el.textContent ?? "",
  );
}

describe("chat day dividers", () => {
  it("a same-day thread renders exactly ONE divider (今天)", () => {
    messages = [
      mkMsg("a", ts(2026, 7, 13, 8, 0)),
      mkMsg("b1", ts(2026, 7, 13, 9, 30), "owner", "b"),
      mkMsg("c", ts(2026, 7, 13, 9, 59)),
    ];
    const { container } = renderChat();
    expect(pillLabels(container)).toEqual(["今天"]);
    // All three messages live under the single day group.
    expect(
      container.querySelectorAll(".chat__day-group [data-msg-id]"),
    ).toHaveLength(3);
  });

  it("a cross-三日 thread renders 前天日期 / 昨天 / 今天 pills in stream order", () => {
    messages = [
      mkMsg("d1", ts(2026, 7, 11, 23, 59)), // Saturday
      mkMsg("d2a", ts(2026, 7, 12, 0, 0)), // midnight edge → next day
      mkMsg("d2b", ts(2026, 7, 12, 18, 0)),
      mkMsg("d3", ts(2026, 7, 13, 9, 0)),
    ];
    const { container } = renderChat();
    expect(pillLabels(container)).toEqual(["7月11日 (週六)", "昨天", "今天"]);
    const groups = container.querySelectorAll(".chat__day-group");
    expect(groups).toHaveLength(3);
    // The midnight-edge message belongs to the SECOND day's group.
    expect(groups[1].querySelectorAll("[data-msg-id]")).toHaveLength(2);
  });

  it("a prior-year day carries the year in its label", () => {
    messages = [
      mkMsg("old", ts(2025, 12, 31, 8, 0)), // Wednesday
      mkMsg("new", ts(2026, 7, 13, 9, 0)),
    ];
    const { container } = renderChat();
    expect(pillLabels(container)).toEqual(["2025年12月31日 (週三)", "今天"]);
  });

  it("per-message times keep the existing hh:mm format (unchanged by dividers)", () => {
    messages = [mkMsg("a", ts(2026, 7, 13, 9, 5))];
    const { container } = renderChat();
    const time = container.querySelector(".chat__msg-time");
    expect(time?.textContent).toMatch(/09:05/);
  });
});

// T-5a02 手機版對話窗跑版: jsdom can't measure CSS layout, so the Playwright
// evidence carries the visual pin (390/360px: time renders whole, bubble takes
// the freed width, no horizontal overflow). What vitest CAN lock is the exact
// selector chain office.css keys on — the send time must live in the sidemeta
// column INSIDE the msg-line row (.chat__msg > .chat__msg-line >
// .chat__msg-sidemeta > .chat__msg-time), for BOTH sides. If a refactor moves
// the timestamp out of that chain, the flex:none/nowrap column and the mobile
// width cap silently stop applying — this pin makes that loud.
describe("T-5a02 timestamp column structure", () => {
  it("peer and own messages both nest the time in .chat__msg-line > .chat__msg-sidemeta", () => {
    messages = [
      mkMsg("peer", ts(2026, 7, 13, 9, 5)), // from b → owner (peer side)
      mkMsg("mine", ts(2026, 7, 13, 9, 6), "owner", "b"), // owner → b (me side)
    ];
    const { container } = renderChat();
    const msgs = container.querySelectorAll(".chat__msg");
    expect(msgs.length).toBe(2);
    for (const msg of Array.from(msgs)) {
      const time = msg.querySelector(
        ".chat__msg-line > .chat__msg-sidemeta > .chat__msg-time",
      );
      expect(time).not.toBeNull();
      expect(time?.textContent).toMatch(/09:0[56]/);
    }
    expect(msgs[1].className).toContain("chat__msg--me");
  });
});
