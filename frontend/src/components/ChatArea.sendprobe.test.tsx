// PROBE — NOT a guard. Read-only recon for t-b0bb04a499f5 step 2, item 3:
// "is the composer's failure-restore still live today, and which failures reach
// it?"  useChat is mocked, so this pins ChatArea's OWN behaviour as a function
// of whether send() rejects. Pair it with useChat.sendprobe.test.ts, which
// measures WHICH failures make send() reject.
//
// ⚠️ OWNER RULING 2026-08-31: this behaviour STAYS. The optimistic-append fix
// (adopt postChat's return value) was scoped out — owner chose to fix only the
// "a whole stretch of messages goes missing" defect. So the assertions below
// are not a wish list; they record a deliberately accepted trade, and a future
// reader should not "repair" them without a new ruling.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

let messages: ChatMessage[] = [];
const send = vi.fn<(b: string) => Promise<void>>();

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    peerLastReadTs: 0,
    send,
    markRead: vi.fn(() => Promise.resolve()),
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

const renderChat = (m: Member) =>
  render(
    <I18nProvider>
      <ChatArea member={m} />
    </I18nProvider>,
  );

const input = (c: HTMLElement) =>
  c.querySelector(".chat__input") as HTMLTextAreaElement;

describe("PROBE t-b0bb: the composer's failure-restore", () => {
  const m1 = mkMember("m1", "Mira");
  beforeEach(() => {
    messages = [];
    send.mockReset();
    Element.prototype.scrollIntoView = vi.fn();
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  it("STILL LIVE: a REJECTING send() puts the words back in the composer", async () => {
    send.mockRejectedValue(new Error("post failed"));
    const c = renderChat(m1).container;
    fireEvent.change(input(c), { target: { value: "會被還原的字" } });
    fireEvent.click(c.querySelector(".chat__send") as HTMLElement);
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(input(c).value).toBe("會被還原的字"));
    // eslint-disable-next-line no-console
    console.log(`[PROBE] send REJECTS -> composer value = ${JSON.stringify(input(c).value)}`);
  });

  it("a RESOLVING send() clears the composer (this is the POST-ok/GET-failed shape)", async () => {
    send.mockResolvedValue(undefined);
    const c = renderChat(m1).container;
    fireEvent.change(input(c), { target: { value: "送出去的字" } });
    fireEvent.click(c.querySelector(".chat__send") as HTMLElement);
    await waitFor(() => expect(send).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(input(c).value).toBe(""));
    // eslint-disable-next-line no-console
    console.log(
      `[PROBE] send RESOLVES -> composer value = ${JSON.stringify(input(c).value)}; ` +
        `messages rendered = ${messages.length}`,
    );
  });
});
