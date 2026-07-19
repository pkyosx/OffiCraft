// T-94c1 composer lock — ChatArea reply area by member lifecycle.
//
// This file REVERSES the original M2-4 spec on owner's instruction (2026-07-17,
// "keep sending message even agent offline"). New spec:
//   • offline / stopped → composer UNLOCKED (you can type; the message queues in
//     the backend and the member reads it on wake) + a wake row above the input
//     (queue notice + an in-place ⚡喚醒 button that calls onWake/activateMember).
//   • waking / stopping → still LOCKED (waking is a brief transient; stopping is
//     winding down, so a reply typed then could land in a dying session) — the
//     bar is the member-panel entry, exactly as before.
//   • online → the normal input row, no lock, no wake row.
// The assertions below that flipped from the old file are owner's reversal, not
// a broken test. Presence-driven: `member` comes from the SSE-refetched roster,
// so the swap happens on a pure prop change (rerender), no reload.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { ChatMessage } from "../api/adapter";
import type { Member, MemberLifecycle, MemberStatus } from "../types";

// Stub the chat hook — these tests exercise only the composer lock, not the
// send path. `mockMessages` lets a test render WITH history (the central
// offline card only appears on an empty thread; the wake row must render either
// way while offline/stopped).
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
// always wires onWake there), for whom offline/stopped unlocks. Pass `undefined`
// explicitly to model a synthetic released/removed peer (read-only, no wake).
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

describe("ChatArea composer lock (T-94c1: offline/stopped unlocked)", () => {
  it.each<MemberLifecycle>(["offline", "stopped"])(
    "%s member → composer UNLOCKED (typable) + wake row, NO locked bar",
    (lifecycle) => {
      const { query } = renderChat(lifecycle);
      const { input, locked, wakeRow } = query();
      // The reversal: the input is now present while offline/stopped.
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

  it.each<MemberLifecycle>(["waking", "stopping"])(
    "%s member → LOCKED (not online yet / winding down), no input, no wake row",
    (lifecycle) => {
      const { query } = renderChat(lifecycle);
      const { input, locked, wakeRow } = query();
      expect(input).toBeNull();
      expect(locked).not.toBeNull();
      expect(wakeRow).toBeNull();
    },
  );

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

  it("offline WITHOUT onWake (released/removed read-only peer) → LOCKED, no input, no wake row", () => {
    // A synthetic released/removed peer (T-661b) is read-only: it gets NO onWake
    // from OfficePage, so its offline composer must NOT unlock — no typable input
    // (which would promise a queue to a member that is gone) and no wake row.
    // Rendered inline WITHOUT onWake — note passing `undefined` to renderChat
    // would trigger its default fn, so the prop is genuinely omitted here.
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

  it("waking stays a whole-bar member-panel entry only when onOpenDetail is wired", () => {
    // waking is still locked; the whole bar opens the detail panel when wired.
    const onOpenDetail = vi.fn();
    const wired = render(
      <I18nProvider>
        <ChatArea member={makeMember("waking")} onOpenDetail={onOpenDetail} />
      </I18nProvider>,
    );
    const bar = wired.container.querySelector("button.chat__composer-locked")!;
    expect(bar).not.toBeNull();
    fireEvent.click(bar);
    expect(onOpenDetail).toHaveBeenCalledTimes(1);
    wired.unmount();

    // Not wired → plain non-clickable notice (same no-dead-click rule).
    const bare = render(
      <I18nProvider>
        <ChatArea member={makeMember("waking")} />
      </I18nProvider>,
    );
    expect(
      bare.container.querySelector("button.chat__composer-locked"),
    ).toBeNull();
    expect(bare.container.querySelector(".chat__composer-locked")).not.toBeNull();
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

  it("lifecycle offline → stopping re-locks the composer (the one unsafe window)", () => {
    const { query, rerender } = renderChat("offline", vi.fn());
    expect(query().input).not.toBeNull();
    rerender(
      <I18nProvider>
        <ChatArea member={makeMember("stopping")} onWake={vi.fn()} />
      </I18nProvider>,
    );
    expect(query().input).toBeNull();
    expect(query().locked).not.toBeNull();
    expect(query().wakeRow).toBeNull();
  });
});
