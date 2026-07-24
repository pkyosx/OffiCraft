// T-9c3c composer lock — ChatArea reply area by member lifecycle.
//
// New spec (owner 2026-07-24, "有時候離線還是沒辦法發訊息"): a LIVE roster member
// (OfficePage wires onWake) can ALWAYS be messaged — the server NEVER gates on
// recipient presence (api_chat.PutChat lands the message regardless, UnreadCounts
// counts it, the member reads it on next boot). So the composer's ONLY lock
// reason is "no queue path at all":
//   • online → the normal input row, no lock, no wake row (direct send).
//   • offline / stopped / waking / stopping (a live member) → composer UNLOCKED
//     (you can type; the message queues) + a wake row above the input (queue
//     notice + an in-place ⚡喚醒 button calling onWake/activateMember).
//   • a peer with NO onWake (a synthetic released/removed peer — read-only,
//     T-661b — or an outsource worker) → LOCKED, the member-panel entry.
//
// This REVERSES T-94c1's extra lock on waking/stopping: those are transient
// presence states an offline member passes through (a fresh wake's 90s TTL —
// the ⚡喚醒 button itself triggers it — and a graceful wind-down), and locking
// on them dropped a message the server was going to queue anyway. That was the
// intermittent "sometimes an offline member can't be messaged" bug. Presence-
// driven: `member` comes from the SSE-refetched roster, so the swap happens on a
// pure prop change (rerender), no reload.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { ChatMessage } from "../api/adapter";
import type { Member, MemberLifecycle, MemberStatus } from "../types";

// Stub the chat hook — these tests exercise only the composer lock, not the
// send path. `mockMessages` lets a test render WITH history (the central
// offline card only appears on an empty thread; the wake row must render either
// way while the member is away).
let mockMessages: ChatMessage[] = [];
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages: mockMessages,
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

beforeEach(() => {
  mockMessages = [];
  // jsdom has no scrollIntoView; the with-history render triggers the entry
  // positioning path which calls it on the bottom sentinel.
  Element.prototype.scrollIntoView = () => {};
});

function makeMember(lifecycle: MemberLifecycle): Member {
  // The collapsed tri-state mirrors the mapper's rule (stopping→online,
  // stopped→offline) so the fixture never carries a status/lifecycle pair the
  // real mapper could not produce.
  const status: MemberStatus =
    lifecycle === "stopping"
      ? "online"
      : lifecycle === "stopped"
        ? "offline"
        : lifecycle;
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    status,
    lifecycle,
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

// onWake defaults to a fn: the primary case is a LIVE roster member (OfficePage
// always wires onWake there), for whom EVERY non-online lifecycle unlocks. Pass
// `undefined` explicitly to model a synthetic released/removed peer (read-only,
// no wake).
function renderChat(
  lifecycle: MemberLifecycle,
  onWake: (() => void) | undefined = vi.fn(),
) {
  const utils = render(
    <I18nProvider>
      <ChatArea member={makeMember(lifecycle)} onWake={onWake} />
    </I18nProvider>,
  );
  const query = () => ({
    input: utils.container.querySelector("textarea.chat__input"),
    locked: utils.container.querySelector(".chat__composer-locked"),
    wakeRow: utils.container.querySelector(".chat__wake-row"),
    wakeBtn: utils.container.querySelector("button.chat__wake-btn"),
  });
  return { ...utils, query };
}

describe("ChatArea composer lock (T-9c3c: a live member is always messageable)", () => {
  it.each<MemberLifecycle>(["offline", "stopped", "waking", "stopping"])(
    "%s member (onWake wired) → composer UNLOCKED (typable) + wake row, NO locked bar",
    (lifecycle) => {
      const { query } = renderChat(lifecycle);
      const { input, locked, wakeRow } = query();
      // The fix: the input is present for EVERY non-online state — the message
      // queues, so none of them may drop the composer.
      expect(input).not.toBeNull();
      expect(locked).toBeNull();
      // The wake row (queue notice) rides above the input.
      expect(wakeRow).not.toBeNull();
    },
  );

  it("online member → the normal input row, no lock, no wake row", () => {
    const { query } = renderChat("online");
    const { input, locked, wakeRow } = query();
    expect(input).not.toBeNull();
    expect(locked).toBeNull();
    expect(wakeRow).toBeNull();
  });

  it("offline wake button: shown+clickable when onWake is wired, calls it once, then shows pending", () => {
    const onWake = vi.fn();
    const { query, getByText } = renderChat("offline", onWake);
    const { wakeBtn } = query();
    expect(wakeBtn).not.toBeNull();
    // Default locale zh → the wake label.
    expect(getByText("喚醒")).toBeTruthy();
    fireEvent.click(wakeBtn!);
    expect(onWake).toHaveBeenCalledTimes(1);
    // Optimistic pending: the button disables + relabels so a double-tap can't
    // fire a second activate.
    expect((wakeBtn as HTMLButtonElement).disabled).toBe(true);
    expect(getByText("喚醒中…")).toBeTruthy();
  });

  it("waking member: the wake button honestly shows 喚醒中… (server-confirmed in flight), disabled", () => {
    // Even with NO local click, a member the server reports as `waking` shows the
    // in-flight label — the optimism handed off to real presence (T-9c3c).
    const { query, getByText } = renderChat("waking", vi.fn());
    const { wakeBtn } = query();
    expect(wakeBtn).not.toBeNull();
    expect((wakeBtn as HTMLButtonElement).disabled).toBe(true);
    expect(getByText("喚醒中…")).toBeTruthy();
  });

  it("offline WITHOUT onWake (released/removed read-only peer) → LOCKED, no input, no wake row", () => {
    // A synthetic released/removed peer (T-661b) is read-only: it gets NO onWake
    // from OfficePage, so its composer must NOT unlock — no typable input (which
    // would promise a queue to a member that is gone) and no wake row. Rendered
    // inline WITHOUT onWake — passing `undefined` to renderChat would trigger its
    // default fn, so the prop is genuinely omitted here.
    const { container } = render(
      <I18nProvider>
        <ChatArea member={makeMember("offline")} />
      </I18nProvider>,
    );
    expect(container.querySelector("textarea.chat__input")).toBeNull();
    expect(container.querySelector(".chat__composer-locked")).not.toBeNull();
    expect(container.querySelector(".chat__wake-row")).toBeNull();
    expect(container.querySelector("button.chat__wake-btn")).toBeNull();
  });

  it("with history the central offline card is absent but the wake row still shows", () => {
    mockMessages = [
      {
        id: "msg-1",
        from: "m1",
        to: "owner",
        body: "hi",
        ts: 1,
        attachments: [],
        replyCardId: null,
      },
    ];
    const { container } = render(
      <I18nProvider>
        <ChatArea member={makeMember("offline")} onWake={vi.fn()} />
      </I18nProvider>,
    );
    // The central card only renders on an empty thread.
    expect(container.querySelector(".chat__offline")).toBeNull();
    // The wake row lives on the composer, so it is present with history too.
    expect(container.querySelector(".chat__wake-row")).not.toBeNull();
    expect(container.querySelector("textarea.chat__input")).not.toBeNull();
  });

  it("a no-onWake locked peer always renders the plain notice, never a clickable bar", () => {
    // The composer only locks now for a peer with no queue path (no onWake) —
    // the released/removed read-only peer, which OfficePage also wires with no
    // onOpenDetail. There is nothing to wake and no live detail to open, so the
    // locked composer is always the plain non-clickable notice: never a button,
    // even if some caller passed onOpenDetail (T-9c3c dropped the dead link arm).
    const onOpenDetail = vi.fn();
    const { container } = render(
      <I18nProvider>
        <ChatArea member={makeMember("offline")} onOpenDetail={onOpenDetail} />
      </I18nProvider>,
    );
    expect(container.querySelector(".chat__composer-locked")).not.toBeNull();
    expect(
      container.querySelector("button.chat__composer-locked"),
    ).toBeNull();
    expect(onOpenDetail).not.toHaveBeenCalled();
  });

  it("lifecycle offline → online on a prop change drops the wake row (SSE refetch path)", () => {
    const { query, rerender } = renderChat("offline", vi.fn());
    expect(query().input).not.toBeNull();
    expect(query().wakeRow).not.toBeNull();
    // The SSE "member" topic makes useMembers refetch → OfficePage passes a NEW
    // member object down. Model that as a plain rerender with online lifecycle.
    rerender(
      <I18nProvider>
        <ChatArea member={makeMember("online")} onWake={vi.fn()} />
      </I18nProvider>,
    );
    expect(query().input).not.toBeNull();
    expect(query().wakeRow).toBeNull();
    // And back offline again → the wake row returns live (no reload either way).
    rerender(
      <I18nProvider>
        <ChatArea member={makeMember("offline")} onWake={vi.fn()} />
      </I18nProvider>,
    );
    expect(query().input).not.toBeNull();
    expect(query().wakeRow).not.toBeNull();
  });

  it("lifecycle offline → waking KEEPS the composer usable (the bug this ticket fixes)", () => {
    // The old spec re-locked on the offline→waking transition — exactly the
    // intermittent "sometimes can't message" window. It must stay unlocked now:
    // the message queues regardless.
    const { query, rerender } = renderChat("offline", vi.fn());
    expect(query().input).not.toBeNull();
    rerender(
      <I18nProvider>
        <ChatArea member={makeMember("waking")} onWake={vi.fn()} />
      </I18nProvider>,
    );
    expect(query().input).not.toBeNull();
    expect(query().locked).toBeNull();
    expect(query().wakeRow).not.toBeNull();
  });
});
