// ChatArea · 就地喚醒沒有送出去 (T-7fa1).
//
// 🔴 WHY THIS SURFACE HAS ITS OWN FILE. The in-chat ⚡喚醒 row (T-94c1) carries
// its OWN optimistic `wakePending` — it does not share MemberDetailPanel's. So
// fixing only the detail panel would leave the chat button sitting on
// 「喚醒中…」 forever for exactly the same wake, and nothing in the panel's tests
// could see it. (The ticket only named the detail panel; this third dead-end
// surface came out of recon.)
//
// Transition-shaped for the reason spelled out in
// MemberDetailPanel.wake-undispatched.test.tsx: every instant renders fine on
// the broken build; only the sequence is wrong.
//
// Locked here:
//   1. POSITIVE: click → {activationPending: true} → the button leaves
//      「喚醒中…」 (usable again) and the notice appears.
//   2. NEGATIVE: click → {activationPending: false} → the pending state SURVIVES
//      and no notice appears.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member, MemberActivateResult } from "../types";

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages: [],
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

beforeEach(() => {
  Element.prototype.scrollIntoView = () => {};
});

function makeMember(): Member {
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
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

function renderChat(
  onWake: () => void | Promise<MemberActivateResult | void>,
) {
  const utils = render(
    <I18nProvider>
      <ChatArea member={makeMember()} onWake={onWake} />
    </I18nProvider>,
  );
  const wakeBtn = () => {
    const b = utils.container.querySelector(
      "button.chat__wake-btn",
    ) as HTMLButtonElement | null;
    // Paired existence assertion (手冊 §1) — a renamed class must fail here,
    // not quietly neuter every `disabled` read below.
    expect(b, "the in-chat wake button must exist").not.toBeNull();
    return b!;
  };
  return { ...utils, wakeBtn };
}

describe("ChatArea · in-chat wake that was never dispatched (T-7fa1)", () => {
  it("leaves 喚醒中… and shows the notice when activation_pending is true", async () => {
    const onWake = vi.fn(async () => ({ activationPending: true }));
    const { wakeBtn, queryByTestId } = renderChat(onWake);

    expect(wakeBtn().disabled).toBe(false);
    expect(queryByTestId("chat-wake-undispatched")).toBeNull();

    fireEvent.click(wakeBtn());
    await waitFor(() => expect(onWake).toHaveBeenCalledTimes(1));

    await waitFor(() =>
      expect(queryByTestId("chat-wake-undispatched")).not.toBeNull(),
    );
    // Re-enabled: the owner can act on the notice and retry.
    await waitFor(() => expect(wakeBtn().disabled).toBe(false));
  });

  it("keeps 喚醒中… when activation_pending is false (a real wake)", async () => {
    const onWake = vi.fn(async () => ({ activationPending: false }));
    const { wakeBtn, queryByTestId } = renderChat(onWake);

    fireEvent.click(wakeBtn());
    await waitFor(() => expect(onWake).toHaveBeenCalledTimes(1));

    await waitFor(() => expect(wakeBtn().disabled).toBe(true));
    expect(queryByTestId("chat-wake-undispatched")).toBeNull();
  });

  it("treats a void-returning onWake as before (no fabricated failure)", async () => {
    const onWake = vi.fn(async () => {});
    const { wakeBtn, queryByTestId } = renderChat(onWake);

    fireEvent.click(wakeBtn());
    await waitFor(() => expect(onWake).toHaveBeenCalledTimes(1));

    await waitFor(() => expect(wakeBtn().disabled).toBe(true));
    expect(queryByTestId("chat-wake-undispatched")).toBeNull();
  });
});
