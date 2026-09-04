// ChatArea — rendering a quote reaches for NOTHING (T-4e95, owner ruling
// 2026-08-21).
//
// 🔴 THIS FILE IS THE WITNESS THAT THE STATE MACHINE IS REALLY GONE, and it is
// the only jsdom test that a re-introduced lookup cannot satisfy by happening
// to return the right answer. (`17_chat_reply_to.spec.js` counts the same thing
// in a real browser, far more slowly.)
//
// What it replaces: the wire used to carry the quoted message's ID alone, so
// <ChatArea> resolved the rest — from the loaded window if it was there, and
// otherwise with a by-ids read (`useQuotedMessages`, deleted with this change).
// That read could fail; a failure was drawn as a placeholder that was sometimes
// a lie; the lie was repaid when the next SSE event arrived. Every one of those
// behaviours draws the SAME PIXELS whether it is right or wrong, which is why
// each of the ~600 lines of tests that grew around them could pass while the
// feature was broken in the browser.
//
// Every other reply-to test asserts what is ON SCREEN. This one asserts what
// was NOT DONE to put it there, because "the fetch is gone" is not a pixel.
//
// IT LIVES IN ITS OWN FILE ON PURPOSE. The api client is replaced here by a
// recording proxy, and that proxy is not a working client — <ChatReplyCard>
// would break against it. So the fixtures here carry no reply-card row, and the
// mock stays out of the main reply-to file, which needs a real one.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { resetChatDrafts } from "../lib/chatDraftStore";
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

/** Every property the component tree pulls off the api client, RECORDED rather
 * than refused so a failure can name the call that broke the rule.
 *
 * Reading a property is what is counted, not invoking it: a caller that grabs
 * `api.getChatMessage` and holds it has already broken the rule this file
 * exists for.
 *
 * `getChatMessage` is the one member that answers with something usable — the
 * second test below CLICKS, and a proxy that handed back `[]` there would make
 * "the click worked" indistinguishable from "the click did nothing". */
const apiCalls: string[] = [];
/** Armed by the ONE test that needs the read to still be in flight when it
 * looks at the button. Null everywhere else, so every other test keeps the
 * immediate answer it was written against. */
let holdQuote: Promise<void> | null = null;
const quotedOriginal = {
  id: "c-1",
  from: "m1",
  to: "owner",
  body: "他說的，而且後面還有很長一段只有全文才看得到的內容",
  ts: 1,
  attachments: [],
  replyCardId: null,
  replyCardStatus: null,
  replyTo: null,
  replyToChat: null,
};
vi.mock("../api", () => ({
  USE_MOCK: true,
  api: new Proxy(
    {},
    {
      get(_t, prop) {
        apiCalls.push(String(prop));
        if (prop === "getChatMessage") {
          return async () => {
            if (holdQuote) await holdQuote;
            return quotedOriginal;
          };
        }
        return () => Promise.resolve([]);
      },
    },
  ),
}));

const member: Member = {
  id: "m1",
  name: "Mira",
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
  tmuxSession: "member-m1",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m1",
    to: "owner",
    body: "",
    ts: 1,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    replyTo: null,
    replyToChat: null,
    ...over,
  };
}

const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;

describe("ChatArea: a quote costs no request", () => {
  beforeEach(() => {
    resetChatDrafts();
    holdQuote = null;
    apiCalls.length = 0;
    Element.prototype.scrollIntoView = function () {} as typeof Element.prototype.scrollIntoView;
  });

  it("renders a present quote AND a missing one without touching the api", async () => {
    // Both shapes are on screen at once. The second is the one the deleted
    // machine used to chase: `replyTo` set, no `replyToChat`, and — crucially —
    // the quoted id is NOT in the loaded window, which is exactly the condition
    // that used to trigger the by-ids read.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "有的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: {
          id: "c-1",
          from: "m1",
          fromName: "",
          to: "owner",
          toName: "",
          content: "他說的",
        },
      }),
      mkMsg({
        id: "c-3",
        from: "owner",
        to: "m1",
        body: "沒有的",
        ts: 3,
        replyTo: "c-longgone",
        replyToChat: null,
      }),
    ];

    const { container } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>,
    );

    // Both rows are painted, so this is not vacuously true of a blank screen.
    expect(
      rowOf(container, "c-2").querySelector(".chat__msg-quote__body")?.textContent,
    ).toBe("他說的");
    expect(
      rowOf(container, "c-3").querySelector(".chat__msg-quote")?.textContent,
    ).toContain(zh.chat.replyQuoteGone);

    // A keystroke re-renders the thread — that is what used to recompute the
    // read's effect key and fire it. Then let every effect and microtask settle,
    // so a read scheduled and not yet sent still counts.
    fireEvent.change(
      container.querySelector(".chat__input") as HTMLTextAreaElement,
      { target: { value: "一" } },
    );
    await act(async () => {
      await Promise.resolve();
    });

    expect(
      apiCalls,
      "rendering a quote — present or missing — must reach for nothing",
    ).toEqual([]);
  });

  // ── 🔴 ONE CLICK, ONE REQUEST ────────────────────────────────────────────
  //
  // THIS IS THE MOST IMPORTANT ASSERTION IN THE FEATURE, and it is the reason a
  // read was allowed back into this component at all.
  //
  // The 2026-08-21 redesign deleted a background refetcher (useQuotedMessages):
  // it decided for itself which quoted ids were still owed, kept that debt
  // across renders and peer switches, retried, and repaired earlier wrong
  // answers when later events arrived. Twenty rounds of review produced more
  // blocking findings out of that one machine than out of anything else in the
  // task, because every one of its states drew the same pixels whether it was
  // right or wrong.
  //
  // The redesign then put ONE read back — `api.getChatMessage`, behind a click.
  // The shapes are not the same and the difference is entirely behavioural, not
  // structural: a person presses a button, one message is asked for once, the
  // answer is used immediately, and a failure is said out loud and forgotten.
  // Nothing about the code SHAPE stops that from growing back into the machine;
  // this test does. If it ever has to be relaxed, the machine is back.
  //
  // What it therefore pins, and each half matters:
  //   ① a click fires EXACTLY ONE request;
  //   ② repaints do not fire another — including a repaint caused by an
  //      inbound message, which is precisely when the deleted collector ran;
  //   ③ nothing at all is asked for before the click.
  it("a click on the quote costs exactly one request, and repainting costs none", async () => {
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "有的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: {
          id: "c-1",
          from: "m1",
          fromName: "",
          to: "owner",
          toName: "",
          content: "他說的",
        },
      }),
    ];

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>,
    );
    // ③ the paint alone asked for nothing.
    expect(apiCalls).toEqual([]);

    const jump = rowOf(container, "c-2").querySelector(
      "[data-testid='msg-quote-jump']",
    ) as HTMLButtonElement;
    expect(jump, "the control is on the row").not.toBeNull();

    await act(async () => {
      fireEvent.click(jump);
    });

    // ① exactly one, and it is the by-id read and nothing else.
    expect(
      apiCalls.filter((c) => c === "getChatMessage"),
      "one click must produce exactly one by-id read",
    ).toHaveLength(1);
    expect(
      apiCalls.filter((c) => c !== "getChatMessage"),
      "and nothing else may ride along with it",
    ).toEqual([]);
    // The answer really was used — otherwise ① is satisfied by a handler that
    // fires a request and throws the result away.
    expect(document.querySelector(".md-preview")?.textContent).toContain(
      "只有全文才看得到",
    );

    // ①b A DOUBLE-CLICK IS ONE INTENT, NOT TWO. Two clicks in the same tick both
    // see the PRE-UPDATE React state, which is why the handler latches on a ref
    // rather than on state — measured: with the ref removed, this fires twice
    // and everything else in the file stays green. An impatient owner
    // double-clicking a control that has not answered yet is the ordinary way
    // this happens.
    apiCalls.length = 0;
    await act(async () => {
      fireEvent.click(jump);
      fireEvent.click(jump);
    });
    expect(
      apiCalls.filter((c) => c === "getChatMessage"),
      "a double-click is still one request",
    ).toHaveLength(1);

    // ② now make the thread repaint the way it does in life: a new message
    // arrives and the parent re-renders. The deleted collector fired HERE.
    messages = [
      ...messages,
      mkMsg({ id: "c-3", from: "m1", to: "owner", body: "又一句", ts: 3 }),
    ];
    await act(async () => {
      rerender(
        <I18nProvider>
          <ChatArea member={member} />
        </I18nProvider>,
      );
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(
      apiCalls.filter((c) => c === "getChatMessage"),
      "a repaint after the click must not ask again",
    ).toHaveLength(1);
  });

  // ── the overlay this opens is reachable by keyboard ───────────────────────
  //
  // Measured before this existed: opening the full-view overlay left
  // `document.activeElement` on the button OUTSIDE the portal, so Tab walked the
  // page behind the backdrop and a keyboard user who pressed 「看原訊息」 got a
  // dialog they were not in. The fix lives in MarkdownPreviewOverlay (it focuses
  // its root on mount and restores on unmount); this asserts it through the path
  // the quote row actually takes, because that is the entry point the redesign
  // added and the one nobody had walked.
  it("moves focus into the overlay it opens, and hands it back on close", async () => {
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "有的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: {
          id: "c-1",
          from: "m1",
          fromName: "",
          to: "owner",
          toName: "",
          content: "他說的",
        },
      }),
    ];
    const { container } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>,
    );
    const jump = rowOf(container, "c-2").querySelector(
      "[data-testid='msg-quote-jump']",
    ) as HTMLButtonElement;
    jump.focus();
    expect(document.activeElement).toBe(jump);

    await act(async () => {
      fireEvent.click(jump);
    });
    const overlay = document.querySelector(".md-preview") as HTMLElement;
    expect(overlay).toBeTruthy();
    expect(
      (document.activeElement as HTMLElement | null)?.closest(".md-preview"),
      "focus must land inside the dialog — aria-modal is a promise, not a behaviour",
    ).toBe(overlay);

    // Esc closes it as the top layer, and focus comes back to the control that
    // was pressed — not to <body>, which would restart the next Tab from the
    // top of the cockpit.
    await act(async () => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    expect(document.querySelector(".md-preview")).toBeNull();
    expect(
      document.activeElement,
      "closing must return focus to the button that opened it",
    ).toBe(jump);
  });

  // ── and the control stays PRESSABLE while its read is in flight ───────────
  //
  // 🔴 THIS IS THE WITNESS THE `disabled` REMOVAL NEVER HAD. The first version
  // of this feature carried `{ id, state: "loading" | "error" }` and put
  // `disabled` on the button while the read was in flight. Measured in a real
  // Chromium: disabling the FOCUSED button blurs it, so `document.activeElement`
  // was already <body> by the time the overlay mounted, the overlay captured
  // <body> as the element to hand focus back to, and closing it dropped a
  // keyboard user at the top of the page. r22fix deleted the loading state for
  // that reason and left nothing behind that would notice it coming back: I
  // restored `d7752781`'s ChatArea.tsx verbatim (the `disabled` attribute and
  // all) and every test in this file stayed GREEN.
  //
  // ⚠️ AND BE EXACT ABOUT WHICH ASSERTION HAS THE TEETH. jsdom does NOT blur an
  // element when it is disabled, so the focus assertions here would not go red
  // on their own — that half of the mechanism is only visible in a real engine.
  // What catches the regression in this layer is the plain `disabled` read: put
  // the attribute back and the first expect below fails. The focus assertions
  // are kept because they state the property the attribute would break, and they
  // are cheap; do not read them as the guard.
  //
  // The double-click protection this replaced is NOT lost — `quoteBusyRef` is
  // what refuses the second click, and the "exactly one request" test above is
  // its witness. That is the trade: a ref that refuses, not an attribute that
  // blurs.
  it("stays enabled while its read is in flight, so the overlay gets a real opener", async () => {
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "有的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: {
          id: "c-1",
          from: "m1",
          fromName: "",
          to: "owner",
          toName: "",
          content: "他說的",
        },
      }),
    ];
    let release!: () => void;
    holdQuote = new Promise<void>((r) => {
      release = r;
    });

    const { container } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>,
    );
    const jump = rowOf(container, "c-2").querySelector(
      "[data-testid='msg-quote-jump']",
    ) as HTMLButtonElement;
    jump.focus();

    await act(async () => {
      fireEvent.click(jump);
    });

    // The read really has not answered yet — otherwise everything below is
    // asserted about the settled state and proves nothing.
    expect(
      document.querySelector(".md-preview"),
      "the read must still be in flight for this test to mean anything",
    ).toBeNull();
    expect(
      jump.disabled,
      "the control must not be disabled mid-read: disabling a focused button blurs it, and the overlay then captures <body> as its opener",
    ).toBe(false);
    expect(
      document.activeElement,
      "and focus must still be on the button that was pressed",
    ).toBe(jump);

    await act(async () => {
      release();
      await holdQuote;
    });

    expect(document.querySelector(".md-preview")).toBeTruthy();
    await act(async () => {
      fireEvent.keyDown(window, { key: "Escape" });
    });
    expect(
      document.activeElement,
      "the opener the overlay handed focus back to is the button, not <body>",
    ).toBe(jump);
  });
});
