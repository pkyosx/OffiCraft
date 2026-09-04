// ChatArea — the gap notice (T-b0bb). GUARDS, not probes.
//
// The hook can detect that messages are missing from the middle of a thread,
// but a flag nobody renders is the same as no flag: the server has already
// marked the missing rows READ, so the unread badge stays at zero and nothing
// else on screen differs. These pin the two halves of "giving up is not
// silent":
//
//   1. gapSuspected ⇒ the notice is on screen.
//   2. gapSuspected ⇒ "已到最早訊息" is NOT, even though hasMore is false.
//
// (2) is the one that is easy to get wrong and easy to under-test. `hasMore`
// asks only "might there be history ABOVE the loaded window", and false is its
// honest answer. But the reader does not read it that narrowly — beside a
// thread with a hole in the MIDDLE, "已到最早訊息" reads as "this is the whole
// conversation". Measured on the pre-fix code: after a 40-message burst plus a
// full walk backwards, the thread was missing 10 rows and hasMore was false —
// the UI declared completeness over a hole.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];
let hasMore = false;
let gapSuspected = false;

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
    hasMore,
    loadOlder: vi.fn(() => Promise.resolve()),
    gapSuspected,
  }),
}));

function mkMember(id: string, name: string): Member {
  return {
    id, name, role: "assistant", status: "online", lifecycle: "online",
    model: "opus", effort: "medium", kind: "staff", desiredMachineId: "",
    machine: null, account: null, contextPct: null, estimatedCost: null,
    bankedCost: null, tmuxSession: `member-${id}`, refocusSince: null,
    lastOp: "", lastOpOk: null, lastOpLog: "", lastOpAt: null, unreadCount: 0,
  };
}

function mkMsg(id: string, ts: number): ChatMessage {
  return {
    id, from: "m1", to: "owner", body: `msg ${id}`, ts,
    attachments: [], replyCardId: null,
  };
}

const m1 = mkMember("m1", "Mira");
const renderChat = () =>
  render(
    <I18nProvider>
      <ChatArea member={m1} />
    </I18nProvider>,
  ).container;

const notice = (c: HTMLElement) => c.querySelector(".chat__gap-notice");
const historyStart = (c: HTMLElement) => c.querySelector(".chat__history-start");

beforeEach(() => {
  messages = [mkMsg("a1", 1000), mkMsg("a2", 1001)];
  hasMore = false;
  gapSuspected = false;
  Element.prototype.scrollIntoView = vi.fn();
});

describe("ChatArea: the gap notice", () => {
  it("CONTROL: no gap ⇒ no notice, and the ordinary end-of-history marker shows", () => {
    const c = renderChat();
    expect(notice(c)).toBeNull();
    expect(historyStart(c)).not.toBeNull();
  });

  it("gapSuspected ⇒ the notice is rendered, and it SAYS something", () => {
    gapSuspected = true;
    const c = renderChat();
    const el = notice(c);
    expect(el).not.toBeNull();
    // Not asserting the exact wording (that is i18n's to change), but it must
    // not be an empty decoration — a blank warning warns nobody.
    expect((el!.textContent ?? "").trim().length).toBeGreaterThan(0);
    expect(el!.getAttribute("role")).toBe("status");
  });

  it("🔴 gapSuspected ⇒ '已到最早訊息' is NOT shown, even with hasMore=false", () => {
    gapSuspected = true;
    hasMore = false; // the exact pre-fix state: UI declared completeness
    const c = renderChat();
    expect(historyStart(c)).toBeNull();
    expect(notice(c)).not.toBeNull();
  });

  it("a gap with history still above (hasMore=true) still shows the notice", () => {
    gapSuspected = true;
    hasMore = true;
    const c = renderChat();
    expect(notice(c)).not.toBeNull();
    expect(historyStart(c)).toBeNull();
  });
});
