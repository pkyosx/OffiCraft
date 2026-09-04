// ChatArea — the quote line on a REPLY-CARD row (T-4e95).
//
// `quoteLine` is rendered in TWO places: inside the ordinary bubble, and — for
// a card row, which has no bubble — as `{m.replyCardId && quoteLine}` on the
// row's own content column, above the card. Only the first had a witness, and
// earlier rounds judged the second unreachable "in product". It is reachable:
// `post_chat` exposes BOTH `meta` and `reply_to`, the POST handler copies
// caller meta through wholesale and deletes only the `reply_to` key, and the
// cockpit derives `m.replyCardId` straight from `meta.reply_card_id`
// (api/mappers.ts) with no card-existence check. The server half is measured
// in server/ocserverd/api_chat_reply_card_meta_t4e95_test.go.
//
// So this drives the branch: one row, both fields.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { zh } from "../i18n/locales/zh";

let messages: ChatMessage[] = [];
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
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
  } as Member;
}
function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m1", to: "owner", body: "", ts: 1, attachments: [],
    replyCardId: null, replyCardStatus: null, replyTo: null, replyToChat: null, ...over,
  };
}
const m1 = mkMember("m1", "Mira");

function rowOf(container: HTMLElement, id: string): HTMLElement {
  return container.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;
}

beforeEach(() => {
  Element.prototype.scrollIntoView = function () {} as typeof Element.prototype.scrollIntoView;
  messages = [
    mkMsg({ id: "c-1", from: "m1", body: "第一個問題" }),
    // The row under test: a CARD row that is ALSO a reply.
    mkMsg({
      id: "c-3",
      body: "請示",
      ts: 3,
      replyCardId: "rc-1",
      replyTo: "c-1",
      // The quote as the SERVER attaches it (T-4e95, 2026-08-21) — this row
      // reads `reply_to_chat`, not the thread it happens to be sitting in.
      replyToChat: {
        id: "c-1",
        from: "m1",
        fromName: "",
        to: "owner",
        toName: "",
        content: "第一個問題",
      },
    }),
  ];
});

describe("ChatArea: the card row's quote branch", () => {
  it("renders the quote row EXACTLY ONCE on a message carrying both a card and a reply link", () => {
    const { container } = render(
      <I18nProvider><ChatArea member={m1} /></I18nProvider>,
    );
    const row = rowOf(container, "c-3");
    const quotes = row.querySelectorAll("[data-testid='msg-quote']");
    // Denominator: the branch really did fire — a row with no quote at all
    // would also satisfy "not duplicated", so say both halves.
    expect(quotes.length, "one quote row, not zero and not two").toBe(1);
    // It carries the QUOTED message's sender and text, read off the wire —
    // the card's own body must not be what is shown here.
    expect(quotes[0].textContent).toContain("Mira");
    expect(quotes[0].textContent).toContain("第一個問題");
    // The server sent a quote ⇒ the jump is offered, with its accessible name.
    // Window membership has no say here and never did on this branch: the render
    // condition is `m.replyTo && quoted`, never `messages` (see
    // ChatArea.reply-to.test.tsx's "offers the control even when the quoted
    // message is nowhere in the loaded window").
    const jump = quotes[0].querySelector("[data-testid='msg-quote-jump']")!;
    expect(jump).toBeTruthy();
    expect(jump.getAttribute("aria-label")).toBe(zh.chat.replyQuoteJump);
    // …and it is NOT nested inside a bubble (a card row has none) nor inside
    // the card itself — it is the row's own line above the card.
    expect(quotes[0].closest(".chat__msg-bubble")).toBeNull();
    expect(quotes[0].closest(".reply-card")).toBeNull();
  });

  it("an ordinary row still renders no quote when it has no reply link", () => {
    const { container } = render(
      <I18nProvider><ChatArea member={m1} /></I18nProvider>,
    );
    expect(
      rowOf(container, "c-1").querySelectorAll("[data-testid='msg-quote']").length,
    ).toBe(0);
  });
});
