// ChatArea 「回覆這則」 (T-4e95) — the owner asked for LINE-style quote-reply:
// every message gets a reply entry, the composer says who is being answered,
// an x returns it to the ordinary send state, and the sent message shows what
// it answered and can be clicked back to it.
//
// Locked here, one test per promise the AC makes:
//   • EVERY row carries the entry — own messages, incoming, attachment-only,
//     server-authored (sender="system") and card rows alike. Card rows are the
//     one shape where it does NOT sit in a bubble corner (they have no bubble).
//     Their QUOTE is still the ordinary quote row — <ChatReplyCard> renders no
//     quote of any kind; the row simply hangs it on its own content column
//     ABOVE the card instead of inside a bubble (ChatArea.reply-card-quote.test.tsx
//     asserts it is inside neither) — an exception the tests state rather than
//     assume;
//   • the banner names the quoted sender and shows a slice of what they said;
//   • the x clears ONLY the target — half-typed text survives it;
//   • sending carries the target, and clears it (the NEXT message is not a
//     reply too);
//   • switching targets, cancelling and re-aiming leaves no stale state;
//   • a quote renders WHAT THE SERVER SENT (`replyToChat`) and nothing else;
//     when the server sent none, one fixed sentence and no dead button.
//
// 🔴 WHAT LEFT THIS FILE ON 2026-08-21, AND WHY IT IS NOT COMING BACK. The wire
// used to carry the quoted id alone and this component fetched the rest
// (useQuotedMessages). Half a dozen tests here pinned the states that fetch
// created — "asked and missed", "not asked yet", the StrictMode double-invoke
// that discarded the answer, the mid-flight re-render that cancelled it. Those
// tests are deleted, not ported: the states are gone, and a test that keeps
// asserting a state the code cannot enter is a test that passes for the wrong
// reason forever.
//
// The api layer is mocked at the useChat seam, matching the other ChatArea
// tests. GEOMETRY IS NOT TESTED HERE — jsdom has no layout engine, so hover
// reveal and the one-line clipping of the quote row live in the Playwright CT
// (visual-guards/chat-reply-to.ct.spec.tsx). A jsdom test that "checked" those
// would be green against a completely unstyled button.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, act, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OwnerNameProvider } from "../hooks/useOwnerName";
import { ChatArea } from "./ChatArea";
import { api } from "../api";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import {
  resetChatDrafts,
  getChatDraft,
  saveChatDraftText,
  updateChatDraftAttachments,
} from "../lib/chatDraftStore";
// The ACTIVE dictionary, not a copy of its strings. These tests assert on the
// i18n VALUE — a literal "回覆這則" here would go red the day someone re-words
// the button, which is not a defect, and would stay green if the label were
// swapped for a different key, which is.
import { zh } from "../i18n/locales/zh";
// The composed (function-valued) half of the same dictionary — `outsourceLabel`
// is 「外包 · 代號」 in one place and this test must not spell it a second time.
import { makeMessages } from "../i18n/compose";
const zhMsg = makeMessages(zh, "zh");

let messages: ChatMessage[] = [];
const send = vi.fn(() => Promise.resolve());

// Released-worker codename cache. The REAL hook lazily fetches
// GET /api/outsource-workers/{id}; here it is a fixed map keyed off the ids it
// is HANDED — which is exactly the seam the quoted-sender test below needs, and
// the reason it is a filter rather than a constant map: an id the component
// never puts into `unknownOwIds` never reaches this mock and never resolves.
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(ids.filter((id) => id === "ow-rel").map((id) => [id, "R-2"])),
}));
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
    tmuxSession: `member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

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

/** The quote the SERVER attaches to a reply (`reply_to_chat`). Written as a
 * helper so every fixture builds it the one way the wire does: the content is
 * already whitespace-collapsed and already shortened server-side, and
 * `fromName` / `toName` are "" on every read the browser makes (the thread
 * resolves both names from its roster, exactly as it does for a message's own
 * sender and recipient).
 *
 * `to` is a REQUIRED positional argument, not an option with a default: the
 * quoted message's addressee is the half a caller is most likely to leave to
 * chance, and the cross-conversation case below turns entirely on it being
 * something other than this thread's peer. */
function mkQuote(id: string, from: string, to: string, content: string) {
  return { id, from, fromName: "", to, toName: "", content };
}

const m1 = mkMember("m1", "Mira");
const m2 = mkMember("m2", "Kyle");

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea key={m1.id} member={m1} />
    </I18nProvider>,
  );
}

function pngFile(name: string): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
}

const input = (c: HTMLElement) =>
  c.querySelector(".chat__input") as HTMLTextAreaElement;
const banner = (c: HTMLElement) =>
  c.querySelector("[data-testid='chat-reply-banner']");
const replyButtons = (c: HTMLElement) =>
  Array.from(c.querySelectorAll(".chat__msg-reply")) as HTMLButtonElement[];
const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;

let scrolled: Element[] = [];

describe("ChatArea 回覆這則", () => {
  beforeEach(() => {
    resetChatDrafts();
    send.mockClear();
    // jsdom has no layout engine and therefore no scrollIntoView. Stubbed to a
    // recorder, the same way ChatArea.unread-jump.test.tsx does: what these
    // tests pin is WHICH element was asked to come into view, never geometry.
    scrolled = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled.push(this);
    } as typeof Element.prototype.scrollIntoView;
    messages = [
      mkMsg({ id: "c-1", from: "m1", body: "第一個問題" }),
      mkMsg({ id: "c-2", from: "owner", to: "m1", body: "我的回應", ts: 2 }),
      // A card row and an attachment-only row: both are messages, and the AC
      // says EVERY message gets the entry — these are the two shapes a
      // bubble-corner button could not have covered.
      mkMsg({ id: "c-3", body: "請示", ts: 3, replyCardId: "rc-1" }),
      mkMsg({
        id: "c-4",
        ts: 4,
        attachments: [
          { id: "a1", url: "/x", filename: "p.png", mime: "image/png", isImage: true },
        ],
      }),
      // 🔴 A SERVER-AUTHORED ROW. `sender="system"` (T-ba04 reassign handover)
      // is a real message with a real id, and since the `replyable` gate was
      // deleted on 2026-08-21 it carries the entry like everything else — and
      // the entry WORKS, per the server's own gate rather than per one manual
      // run: POST's `reply_to` gate lives in `api_chat.go`'s
      // `HandlePostChatApiChatPost`, which asks `ListChatByIDs` whether the id
      // exists and 400s only when it comes back empty. There is no sender
      // condition in it, so a `sender="system"` row is as replyable as any
      // other. It was missing from this table, which is how a row that renders
      // differently from every other one gets no coverage at all.
      mkMsg({ id: "c-5", from: "system", to: "m1", body: "任務已轉手", ts: 5 }),
      // (its real wire shape: the server addresses a handover to the MEMBER, so
      // in the owner's thread it is an inter-agent row and starts collapsed —
      // the test below expands it rather than pretending it is addressed to the
      // owner, because a fixture that is not the shape the server emits guards
      // the wrong row.)
    ];
  });

  it("EVERY message row carries a reply entry — incoming, own, card, attachment-only and server-authored alike", () => {
    const { container } = renderChat();
    // Inter-agent runs (the server-authored handover among them) render
    // COLLAPSED by default. Expand them so this table sees every row it lists
    // rather than silently skipping the shapes that start folded.
    for (const toggle of Array.from(
      container.querySelectorAll(".chat__inter-toggle"),
    )) {
      fireEvent.click(toggle);
    }
    expect(replyButtons(container)).toHaveLength(messages.length);
    // …and each one really belongs to a row, so a pile of buttons stacked in one
    // corner could not pass.
    for (const m of messages) {
      expect(rowOf(container, m.id).querySelector(".chat__msg-reply")).toBeTruthy();
    }
  });

  it("the entry sits in the bubble's own corner slot, beside 放大閱讀", () => {
    const { container } = renderChat();
    // Owner 2026-08-20: out on the row it read as belonging to the thread
    // rather than to this message. Bubble-shaped messages carry it in the same
    // corner slot as 放大閱讀 — and the slot is INSIDE the bubble, which is
    // what makes it look like part of the message.
    for (const id of ["c-1", "c-2", "c-4"]) {
      const entry = rowOf(container, id).querySelector(".chat__msg-reply")!;
      expect(entry.closest(".chat__msg-actions")).toBeTruthy();
      expect(entry.closest(".chat__msg-bubble")).toBeTruthy();
    }
    // An INCOMING text bubble carries BOTH controls, in ONE slot — two corners
    // would be two places to look.
    const slot = rowOf(container, "c-1").querySelector(".chat__msg-actions")!;
    expect(slot.querySelector(".chat__msg-reply")).toBeTruthy();
    expect(slot.querySelector(".chat__msg-expand")).toBeTruthy();

    // The declared exception: a reply-card row has no bubble to put a corner
    // on, so its entry stays on the row. Stated as a test so the exception is
    // a decision on record rather than something that quietly happened.
    const cardEntry = rowOf(container, "c-3").querySelector(".chat__msg-reply")!;
    expect(cardEntry.closest(".chat__msg-bubble")).toBeNull();
  });

  // ── the accessibility surface ──────────────────────────────────────────────
  //
  // r18 F2: a reviewer stripped the aria-label AND title off all three controls,
  // removed the focus hand-off, and blanked the attachment excerpt — and all 666
  // frontend tests stayed green. Every test in this block exists because that
  // mutant survived.

  it("every control this feature adds has an accessible name", () => {
    // Without an accessible name all three are announced as 「按鈕」 and a
    // screen-reader user has three indistinguishable ones.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "回它",
        ts: 2,
        replyTo: "c-1",
        // The control is offered when the server sent a snapshot. Without one
        // the row prints 「這則訊息已不存在」 and offers nothing to open — see
        // the GONE test below.
        replyToChat: mkQuote("c-1", "m1", "owner", "他說的"),
      }),
    ];
    const { container } = renderChat();

    // Guard against the vacuous version of this test: an empty dictionary value
    // would make every comparison below "" === "" and prove nothing.
    for (const v of [
      zh.chat.replyAction,
      zh.chat.replyCancel,
      zh.chat.replyQuoteJump,
    ]) {
      expect(v.length, "the dictionary value must not be empty").toBeGreaterThan(0);
    }

    const entry = rowOf(container, "c-1").querySelector(".chat__msg-reply")!;
    expect(entry.getAttribute("aria-label")).toBe(zh.chat.replyAction);
    expect(entry.getAttribute("title")).toBe(zh.chat.replyAction);

    // The jump lives on the reply's own row and has a visible label too — but
    // that label is DELETED OUTRIGHT below 520px of pane (`@container chat-pane`
    // in office.css), not trimmed: nothing in the stylesheet can ellipsise it.
    // So the accessible name may not depend on it being rendered at all.
    const jump = rowOf(container, "c-2").querySelector(
      "[data-testid='msg-quote-jump']",
    )!;
    expect(jump.getAttribute("aria-label")).toBe(zh.chat.replyQuoteJump);
    expect(jump.getAttribute("title")).toBe(zh.chat.replyQuoteJump);

    fireEvent.click(entry);
    const x = container.querySelector(".chat__reply-banner__x")!;
    expect(x.getAttribute("aria-label")).toBe(zh.chat.replyCancel);
    expect(x.getAttribute("title")).toBe(zh.chat.replyCancel);
  });

  it("tells the accessibility tree WHICH sentence is the quotation", () => {
    // 🔴 THE GAP THIS PINS WAS MEASURED IN A REAL BROWSER, on a real <ChatArea>
    // rather than a CT story: the quote row was a bare <div> (role null,
    // aria-label null), so a reply linearised into
    //   "Mira. Mira. 他說的. 跳到原訊息. 我回的"
    // (verbatim from that measurement — the button said 「跳到原訊息」 then and
    // says 「看原訊息」 now; the shape is the point, not the string)
    // — the same name twice, two sentences running together, and NOTHING saying
    // which one is being quoted and which one this person is saying now. That
    // distinction is the entire feature; a screen-reader user was the one
    // audience it never reached.
    //
    // `.chat__msg-quote` is the only construct in this frontend that embeds
    // someone else's sentence inside another person's message, so this is not
    // the app's general icon/landmark posture — it is this feature's own hole.
    // The row therefore carries role="blockquote" and a NAME, asserted here
    // through getByRole so it is the computed accessibility tree being read,
    // not the attribute we happened to type.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "我回的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: mkQuote("c-1", "m1", "owner", "他說的"),
      }),
      // A reply whose ORIGINAL IS GONE: the server sent `reply_to` and no
      // `reply_to_chat`, so there is no sender to name and naming one would be
      // a coin flip (the banner's own rule). It must still announce itself as a
      // quotation — that is the state most at risk of losing the role, because
      // it is the one with the least in it.
      mkMsg({
        id: "c-3",
        from: "owner",
        to: "m1",
        body: "回更早的",
        ts: 3,
        replyTo: "c-far",
        replyToChat: null,
      }),
    ];
    // Guard against the vacuous version: an empty dictionary value would make
    // the name assertions below compare "" with "" and prove nothing.
    expect(zh.chat.replyQuoteRole.length).toBeGreaterThan(0);
    expect(
      zh.chat.replyQuoteRoleWho(`Mira → ${zh.user}`).length,
    ).toBeGreaterThan(zh.chat.replyQuoteRole.length);

    const { container } = renderChat();

    // Named with the person being quoted when we have resolved one…
    const quoted = within(rowOf(container, "c-2")).getByRole("blockquote", {
      name: zh.chat.replyQuoteRoleWho(`Mira → ${zh.user}`),
    });
    // …and the row really is the quote line, not something else that happens to
    // carry the name.
    expect(quoted.querySelector(".chat__msg-quote__body")?.textContent).toBe(
      "他說的",
    );
    // The glyph beside it is decorative: the row already SAYS it is a quote, so
    // an unnamed `img` node next to that name is noise a screen reader reads out
    // for nothing.
    expect(quoted.querySelector("svg")?.getAttribute("aria-hidden")).toBe(
      "true",
    );

    // …and generically when we have not. No name, no claim — the same rule the
    // banner and `quoteWho` already follow. Read SYNCHRONOUSLY on the first
    // render, with no waitFor: there is nothing in flight, so a row that only
    // became correct later would be a row that was wrong first.
    const goneRow = within(rowOf(container, "c-3")).getByRole("blockquote", {
      name: zh.chat.replyQuoteRole,
    });
    expect(goneRow.textContent).toContain(zh.chat.replyQuoteGone);

    // The message's OWN body is not inside the quotation — if it were, the tree
    // would be back to one undivided run of text.
    expect(
      within(rowOf(container, "c-2"))
        .getByRole("blockquote")
        .textContent?.includes("我回的"),
    ).toBe(false);
  });

  it("clicking the entry puts the caret in the composer", () => {
    // The point of the whole control is 「我要回這一則」 — landing the owner
    // anywhere but the input means the next thing they type goes nowhere.
    const { container } = renderChat();
    expect(document.activeElement).not.toBe(input(container));

    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);

    expect(document.activeElement).toBe(input(container));
  });

  it("cancelling with the x gives the focus BACK to the composer", () => {
    // r18 F1. The x unmounts itself, and a focused element leaving the document
    // hands focus to <body>: a keyboard user who cancels one reply is dropped at
    // the top of the page and has to Tab through the entire thread to get back
    // to the input. Reproduced before the fix.
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    const x = container.querySelector(".chat__reply-banner__x") as HTMLElement;
    // Focus really is on the x first — otherwise "focus ends up in the input"
    // could be satisfied by it having never left.
    x.focus();
    expect(document.activeElement).toBe(x);

    fireEvent.click(x);

    expect(banner(container)).toBeNull();
    expect(document.activeElement).toBe(input(container));
  });

  it("quotes a text-less original as a NAMED, EMPTY line — not as a missing one", () => {
    // The server sends `content: ""` for an original that carried only
    // attachments, and says so in the spec: "" is an ORDINARY value, and the way
    // a missing original is said is the ABSENCE of the whole reply_to_chat
    // object. Two different facts about the conversation — "there was nothing to
    // quote" vs "there is nothing to quote FROM" — and the row must not fold one
    // into the other.
    //
    // ⚠️ THIS REPLACES an earlier test that asserted a 「（附件）」 label here.
    // That label was invented by the browser out of the quoted message's
    // attachment list, which the browser only had because it was resolving the
    // quote itself. It no longer resolves anything, and the owner ruled the
    // empty content legal rather than have the server invent text for it.
    messages = [
      mkMsg({
        id: "c-att",
        from: "m1",
        to: "owner",
        ts: 1,
        attachments: [
          {
            id: "a1",
            url: "/x",
            filename: "p.png",
            mime: "image/png",
            isImage: true,
          },
        ],
      }),
      mkMsg({
        id: "c-reply",
        from: "owner",
        to: "m1",
        body: "收到",
        ts: 2,
        replyTo: "c-att",
        replyToChat: mkQuote("c-att", "m1", "owner", ""),
      }),
    ];
    const { container } = renderChat();

    const quote = rowOf(container, "c-reply").querySelector(".chat__msg-quote")!;
    // The sender IS known, so the row names them — with the addressee, which is
    // the shape every quote row draws.
    expect(quote.querySelector(".chat__msg-quote__who")?.textContent).toBe(
      `Mira → ${zh.user}`,
    );
    // …the body is empty, honestly…
    expect(quote.querySelector(".chat__msg-quote__body")?.textContent).toBe("");
    // …and it is NOT the "gone" sentence, which is the whole point.
    expect(quote.textContent).not.toContain(zh.chat.replyQuoteGone);
  });

  it("clicking it names the quoted sender AND addressee, and quotes what they said, above the input", () => {
    const { container } = renderChat();
    expect(banner(container)).toBeNull();

    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);

    const b = banner(container)!;
    expect(b).toBeTruthy();
    // 「寄件者 → 收件者」, whole string: the banner is the SECOND code path to
    // that sentence (it resolves its target from the loaded window, not from
    // `reply_to_chat`), and it drew the sender alone for the same reason the
    // quote row did — a quoted line that reads as if it had been said at you.
    expect(b.querySelector(".chat__reply-banner__who")?.textContent).toBe(
      zh.chat.replyingTo(`Mira → ${zh.user}`),
    );
    expect(b.textContent).toContain("第一個問題");
    // The banner belongs to the COMPOSER, not the thread: it must sit inside
    // the footer, above the input row. Placing it in the message list would
    // satisfy every text assertion above and be the wrong feature.
    expect(b.closest(".chat__composer")).toBeTruthy();
  });

  it("the x cancels the reply and DOES NOT touch what has already been typed", () => {
    const { container } = renderChat();
    fireEvent.change(input(container), { target: { value: "打到一半的字" } });
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    expect(banner(container)).toBeTruthy();

    fireEvent.click(container.querySelector(".chat__reply-banner__x")!);

    expect(banner(container)).toBeNull();
    // 🔴 THE WHOLE POINT OF THIS TEST. Cancelling a reply is not cancelling the
    // message; a composer that emptied itself here would throw away work the
    // owner never asked to lose.
    expect(input(container).value).toBe("打到一半的字");
  });

  it("sending carries the target — and the NEXT message is not a reply too", async () => {
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "答案" } });
    fireEvent.click(container.querySelector(".chat__send")!);

    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("答案", undefined, "c-1"),
    );
    expect(banner(container)).toBeNull();

    // The second send is the discriminating half: a target that survived its
    // own send would silently attach itself to everything after it.
    send.mockClear();
    fireEvent.change(input(container), { target: { value: "另一句" } });
    fireEvent.click(container.querySelector(".chat__send")!);
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("另一句", undefined, undefined),
    );
  });

  it("re-aiming, cancelling and aiming again leaves no stale target behind", async () => {
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.click(rowOf(container, "c-2").querySelector(".chat__msg-reply")!);
    expect(banner(container)!.textContent).toContain("我的回應");

    fireEvent.click(container.querySelector(".chat__reply-banner__x")!);
    fireEvent.click(rowOf(container, "c-3").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "第三次" } });
    fireEvent.click(container.querySelector(".chat__send")!);

    // c-3, not c-1 and not c-2: the send must carry the LAST aim, and a
    // cancelled one must not come back.
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("第三次", undefined, "c-3"),
    );
  });

  it("a sent reply shows what it answered, and the control opens that message IN FULL", async () => {
    // 🔴 THIS TEST WAS ABOUT SCROLLING UNTIL 2026-08-21. The control used to
    // scroll the thread to the quoted row and flash it, which is why the old
    // version asserted `chat__msg--located` and a scrollIntoView on c-1. The
    // owner replaced that with 「撈那一則、跳 modal」: nothing scrolls, the one
    // message is read back and opened in the shared full-view overlay.
    //
    // The old assertions are DELETED rather than kept alongside: they described
    // a behaviour the component can no longer perform, and the highlight they
    // named still exists for a DIFFERENT entry point (the reply-card /
    // hash-route jump), so keeping them here would have quietly re-pointed this
    // test at machinery the quote row does not drive.
    messages = [
      ...messages,
      mkMsg({
        id: "c-5",
        from: "owner",
        to: "m1",
        body: "答案",
        ts: 5,
        replyTo: "c-1",
        replyToChat: mkQuote("c-1", "m1", "owner", "第一個問題"),
      }),
    ];
    // What comes BACK is deliberately longer than what the row shows: the row
    // carries the server's 60-rune excerpt, the overlay carries the whole body.
    // A component that opened the excerpt it already had would pass a weaker
    // version of this test and fail this one.
    const full = "第一個問題" + "，還有後面很長的一整段".repeat(12);
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(mkMsg({ id: "c-1", from: "m1", to: "owner", body: full }));
    const { container } = renderChat();

    const quote = rowOf(container, "c-5").querySelector(".chat__msg-quote")!;
    expect(quote.textContent).toContain("Mira");
    expect(quote.textContent).toContain("第一個問題");
    // It is part of the MESSAGE, not a strip beside it (owner 2026-08-20).
    expect(quote.closest(".chat__msg-bubble")).toBeTruthy();

    const jump = quote.querySelector("[data-testid='msg-quote-jump']")!;
    expect(jump).toBeTruthy();
    scrolled = [];
    await act(async () => {
      fireEvent.click(jump);
    });

    // ① it asked the server for THAT message, by id.
    expect(get).toHaveBeenCalledWith("c-1");
    // ② the overlay is open, titled with the quoted SENDER (not the id, and not
    // the person doing the replying), carrying the WHOLE body.
    const overlay = document.querySelector(".md-preview")!;
    expect(overlay, "the original opens in the shared full-view overlay").toBeTruthy();
    expect(overlay.querySelector(".md-preview__title")?.textContent).toContain("Mira");
    expect(overlay.textContent).toContain("後面很長的一整段");
    // ③ nothing scrolled. The redesign's whole point is that the thread does not
    // move under the reader — and a scroll would also be the old behaviour
    // surviving beside the new one.
    expect(scrolled).toEqual([]);
    // ④ and no row was flashed: `--located` belongs to the reply-card jump now.
    expect(container.querySelector(".chat__msg--located")).toBeNull();
    get.mockRestore();
  });

  it("offers the control even when the quoted message is nowhere in the loaded window", async () => {
    // 🔴 THE CASE THAT USED TO HIDE THE BUTTON, AND IS THE COMMON ONE. `c-far`
    // is not in `messages` at all: under the old design the row asked
    // `messageById.has(m.replyTo)`, got false, and rendered no control — so the
    // owner's ordinary reply, which almost always quotes something far above the
    // window, offered nothing. The server sends the snapshot either way, and the
    // read behind the control is by id, so the window has no say any more.
    messages = [
      mkMsg({
        id: "c-7",
        from: "owner",
        to: "m1",
        body: "回很久以前那則",
        ts: 7,
        replyTo: "c-far",
        replyToChat: mkQuote("c-far", "m1", "owner", "很久以前說的"),
      }),
    ];
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue(
        mkMsg({ id: "c-far", from: "m1", to: "owner", body: "很久以前說的全文" }),
      );
    const { container } = renderChat();

    // Precondition: the target really is absent from the thread.
    expect(rowOf(container, "c-far")).toBeNull();
    const jump = rowOf(container, "c-7").querySelector(
      "[data-testid='msg-quote-jump']",
    ) as HTMLButtonElement;
    expect(jump, "the window must not decide whether this is offered").not.toBeNull();

    await act(async () => {
      fireEvent.click(jump);
    });
    expect(get).toHaveBeenCalledWith("c-far");
    expect(document.querySelector(".md-preview")?.textContent).toContain(
      "很久以前說的全文",
    );
    get.mockRestore();
  });

  it("says so, in place and once, when that one read fails", async () => {
    // A read that failed is NOT the same fact as an original that is gone, and
    // the two must not be drawn with the same words. The quote LINE keeps
    // showing what the server sent; the failure is said beside the button.
    messages = [
      mkMsg({
        id: "c-8",
        from: "owner",
        to: "m1",
        body: "回它",
        ts: 8,
        replyTo: "c-1",
        replyToChat: mkQuote("c-1", "m1", "owner", "第一個問題"),
      }),
    ];
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockRejectedValue(new Error("boom"));
    const { container } = renderChat();
    const row = rowOf(container, "c-8");

    await act(async () => {
      fireEvent.click(row.querySelector("[data-testid='msg-quote-jump']")!);
    });

    // ① said, where it happened.
    expect(
      row.querySelector("[data-testid='msg-quote-error']")?.textContent,
    ).toBe(zh.chat.replyQuoteOpenFailed);
    // ② the quote line is UNTOUCHED — it must not turn into the 「已不存在」
    // assertion, which is a claim about the world this failure cannot support.
    const quote = row.querySelector(".chat__msg-quote__body")!;
    expect(quote.textContent).toBe("第一個問題");
    expect(row.textContent).not.toContain(zh.chat.replyQuoteGone);
    // ③ no overlay opened on a body nobody fetched.
    expect(document.querySelector(".md-preview")).toBeNull();
    // ④ AND IT DOES NOT TRY AGAIN. Let every microtask and timer-free effect
    // settle: a retry, a queued repair or an effect keyed on the error state
    // would show up as a second call here. This is the smaller sibling of
    // ChatArea.quote-no-fetch.test.tsx's guard.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(get, "a failure is final — no retry, no queue").toHaveBeenCalledTimes(1);
    get.mockRestore();
  });

  it("offers the reply entry on EVERY row, the owner's own conversation or not", () => {
    // 🔴 THIS TEST WAS THE EXACT OPPOSITE UNTIL 2026-08-21, and the reversal is
    // the owner's. It used to assert that an inter-agent row (Mira→Kyle) carries
    // NO entry, because the server refused a reply_to that crossed conversations
    // and the button would have 400'd on every press.
    //
    // The owner removed that refusal for one reason, stated as the requirement:
    // 「引用另外兩個人對話裡的一句話來介入詢問」. So the entry on an inter-agent
    // row is not a stray affordance, it is the feature — and the old assertion,
    // left in place, would have quietly held the product back to the shape the
    // ruling replaced.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他問我的" }),
      mkMsg({ id: "c-own", from: "owner", to: "m1", body: "我回的", ts: 2 }),
      mkMsg({ id: "c-ia", from: "m1", to: "m9", body: "我轉給別人的", ts: 3 }),
    ];
    const { container } = renderChat();

    for (const id of ["c-1", "c-own"]) {
      expect(
        rowOf(container, id).querySelector(".chat__msg-reply"),
        `${id} should keep its entry`,
      ).not.toBeNull();
    }
    // The inter-agent run renders collapsed, so open it before looking — a row
    // that is merely absent would satisfy the assertion below without the
    // component doing anything.
    fireEvent.click(
      container.querySelector(".chat__inter [aria-expanded]") as HTMLElement,
    );
    const ia = rowOf(container, "c-ia");
    expect(ia, "the inter-agent row is on screen once expanded").not.toBeNull();
    expect(
      ia.querySelector(".chat__msg-reply"),
      "the owner must be able to step into another pair's line by quoting it",
    ).not.toBeNull();

    // …and the entry really aims at THAT message: pressing it must put the
    // inter-agent line in the composer banner, not the thread's peer. Without
    // this half, an entry that existed but pointed somewhere else would pass.
    fireEvent.click(ia.querySelector(".chat__msg-reply")!);
    expect(banner(container)!.textContent).toContain("我轉給別人的");
  });

  it("names the owner's own message with the owner's label, never the raw id", () => {
    // The commonest thing to reply to is your own last line, and nameOf had no
    // owner branch — this is the first display path that feeds the owner's own
    // id into it, so it fell through to the raw "owner" and the banner read
    // 「正在回覆 owner」 next to a topbar that says 「CEO（你）」.
    messages = [
      mkMsg({ id: "c-own", from: "owner", to: "m1", body: "我自己說的那句" }),
    ];
    const { container } = renderChat();

    fireEvent.click(rowOf(container, "c-own").querySelector(".chat__msg-reply")!);
    const text = banner(container)!.textContent ?? "";
    expect(text).not.toContain("owner");
    expect(text).toContain("CEO");
  });

  // ⚠️ DELETED 2026-08-21: "opens a collapsed inter-agent block rather than
  // offering a jump that goes nowhere". That test pinned the second half of
  // `locateMessage` — a quote pointing into a COLLAPSED inter-agent run was in
  // `messages` (so the button appeared) but not in the DOM (so the scroll found
  // nothing), and the fix was to expand the block and scroll on the next paint.
  // Nothing in that sentence exists any more: there is no window check, no
  // expand-then-scroll, and no scroll. The scenario is covered by "offers the
  // control even when the quoted message is nowhere in the loaded window" above,
  // which is the general case that swallowed it.

  it("does not spill a failed send into whichever room is on screen when it fails", async () => {
    // The restore runs after an await, and the owner can leave in the meantime.
    // Reviewed and reproduced back when this component was REUSED across peers:
    // the previous room's text AND its reply target were restored into the next
    // room and persisted into that room's draft. Leaving now unmounts the room
    // (`key={peerId}`, R13-5), so what this pins is the other half — that the
    // words are still SOMEWHERE. The target half is worse than untidy, and it got worse on
    // 2026-08-21: the server's cross-conversation refusal was deleted, so a
    // target belonging to another room is no longer 400'd on send — it is
    // ACCEPTED, and the message goes out to the new peer quoting a sentence from
    // a conversation it has nothing to do with. The failure mode went from a
    // visible refusal to a silent mis-send, which is why this is guarded.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    // A STAGED FILE TOO. Every earlier version of this test sent text only, so
    // writing `attachments: []` into the store passed all of them — the third
    // of the three things a failed send can lose had nothing standing on it.
    fireEvent.change(
      container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("p.png")] } },
    );
    await waitFor(() =>
      expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1),
    );
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea key={m2.id} member={m2} />
      </I18nProvider>,
    );
    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    // The room on screen is untouched…
    expect(input(container).value).toBe("");
    expect(banner(container)).toBeNull();
    expect(getChatDraft("m2")).toBeUndefined();

    // …AND — the half this test used to be missing — the words are still
    // somewhere. Three negative assertions are also all satisfied by throwing
    // the message away, which is what the first version of the guard actually
    // did: the optimistic clear had already deleted m1's draft, so the early
    // return left the text, the attachment and the reply target nowhere at all.
    // A reviewer replaced the whole restore with an unconditional `return` and
    // all 128 ChatArea tests stayed green. This is that missing assertion.
    const kept = getChatDraft("m1");
    expect(kept?.text).toBe("給 m1 的");
    expect(kept?.replyTo).toBe("c-1");
    expect(kept?.attachments).toHaveLength(1);
    expect(kept?.attachments[0].filename).toBe("p.png");
  });

  it("puts a failed send back ON SCREEN when the owner never left the room", async () => {
    // The room's draft is not enough on its own: if the owner is still looking
    // at this conversation, the words have to come back to the composer they
    // vanished from. A reviewer wrote the store and then returned immediately,
    // never restoring the composer — and all 129 tests stayed green, because
    // every one of them switched rooms or unmounted first. This is the case
    // nothing was standing on.
    //
    // 🔴 ALL THREE HALVES, INCLUDING THE FILE (T-48, R13-2). The staged rows
    // come back by a DIFFERENT route from the text now — the text is component
    // state and is set here, the files are the room's draft slice and come back
    // because the composer subscribes to it. A restore that put the words back
    // and dropped the attachment would satisfy every other assertion in this
    // file, which is exactly the shape this ticket has shipped twice.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(
      container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("p.png")] } },
    );
    await waitFor(() =>
      expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1),
    );
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    expect(input(container).value, "optimistically cleared").toBe("");
    expect(
      container.querySelectorAll(".chat__preview-thumb").length,
      "the staged file is optimistically cleared too",
    ).toBe(0);

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(input(container).value).toBe("給 m1 的");
    expect(banner(container), "still aimed at what it was answering").not.toBeNull();
    await waitFor(() =>
      expect(
        container.querySelectorAll(".chat__preview-thumb").length,
        "and the file is back in the composer it vanished from",
      ).toBe(1),
    );
  });

  it("fills only what the room does not already hold, field by field", async () => {
    // The first version of the store write was all-or-nothing: it wrote nothing
    // at all if the room held ANYTHING. So a room the owner had gone back to
    // and put one thing into swallowed the rest of the failed message. The rule
    // is per field — the same one the on-screen restore uses.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea key={m2.id} member={m2} />
      </I18nProvider>,
    );
    // m1 is not empty any more — but what it holds is only TEXT.
    saveChatDraftText("m1", "我後來又打的");

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(getChatDraft("m1")).toEqual({
      // theirs wins — two texts cannot share one composer
      text: "我後來又打的",
      attachments: [],
      // …but the reply target had nothing to collide with, so it survives
      replyTo: "c-1",
    });
  });

  it("does not clobber a picture the owner staged in that room while the send was away", async () => {
    // The case the comment above the fix names in words — "go back to that room,
    // stage one image and type nothing" — and which nothing was standing on: a
    // reviewer made the attachments field write the snapshot UNCONDITIONALLY and
    // all 1310 tests stayed green. The room's own picture would be overwritten,
    // and since an empty draft is deleted outright there is no way back.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea key={m2.id} member={m2} />
      </I18nProvider>,
    );
    // Back in m1 the owner staged a picture and typed nothing.
    updateChatDraftAttachments("m1", () => [
      {
        key: "k-theirs",
        dataUri: "data:image/png;base64,AAAA",
        filename: "後來貼的.png",
        mime: "image/png",
        size: 4,
        isImage: true,
      },
    ]);

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    const kept = getChatDraft("m1");
    // Theirs survives…
    expect(kept?.attachments).toHaveLength(1);
    expect(kept?.attachments[0].filename).toBe("後來貼的.png");
    // …and the fields the room had nothing in still come back.
    expect(kept?.text).toBe("給 m1 的");
    expect(kept?.replyTo).toBe("c-1");
  });

  it("gives the room back its OWN reply target, not the failed send's", async () => {
    // Polarity pin for the replyTo field. Inverting it left all 1310 tests
    // green: every other case has the room holding no target at all, so both
    // polarities produce the same answer there.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({ id: "c-2", from: "m1", to: "owner", body: "他說的第二句", ts: 2 }),
    ];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea key={m2.id} member={m2} />
      </I18nProvider>,
    );
    // Back in m1 the owner aimed at a DIFFERENT message and typed something.
    saveChatDraftText("m1", "我後來又打的", "c-2");

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    const kept = getChatDraft("m1");
    expect(kept?.text).toBe("我後來又打的");
    expect(kept?.replyTo, "the room's own aim, not the failed send's").toBe(
      "c-2",
    );
  });

  it("does not re-aim the composer at the failed send when the owner has already aimed somewhere else", async () => {
    // 🔴 THE SAME "only fill what the room does not already hold" rule as the
    // draft text beside it — but for the reply TARGET, and in the room the
    // owner never left. The sequence: aim at c-1, Enter (the target is cleared
    // optimistically), and while the send is still in flight re-aim at c-2. The
    // send then fails. Putting c-1 back would silently overwrite the aim the
    // owner can SEE in the banner, and the next Enter would hang the reply on
    // the wrong message — a wrong send, not just a tidy-up.
    //
    // The production guard for this is `setReplyToId((cur) => (cur ? cur : …))`
    // and its `cur ?` arm had ZERO coverage (v8: counts [0]): flattening it to
    // `setReplyToId(replyToSnapshot)` left the WHOLE suite green — and with
    // this test present that same flattening is the only red in it. Its twin
    // for the draft text was witnessed; this one was not. Everything else in this
    // group changes rooms or unmounts first, which is exactly why none of them
    // can reach the on-screen restore at all.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "第一句" }),
      mkMsg({ id: "c-2", from: "m1", to: "owner", body: "第二句", ts: 2 }),
    ];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 c-1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    // Optimistically cleared, send still in flight.
    expect(banner(container), "the aim went with the send").toBeNull();

    // The owner re-aims at a DIFFERENT message while the send is away.
    fireEvent.click(rowOf(container, "c-2").querySelector(".chat__msg-reply")!);
    const bodyOf = (c: HTMLElement) =>
      c.querySelector(".chat__reply-banner__body")?.textContent;
    expect(bodyOf(container), "now aimed at c-2").toBe("第二句");

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    // The banner must still be showing what the OWNER aimed at, not what the
    // failed send was aimed at.
    expect(banner(container)).not.toBeNull();
    expect(bodyOf(container), "the owner's later aim wins").toBe("第二句");
    // And sending now must carry c-2 — the banner and the wire agreeing is the
    // part that actually costs a wrong message when it breaks.
    send.mockClear();
    fireEvent.change(input(container), { target: { value: "給 c-2 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("給 c-2 的", undefined, "c-2"),
    );
  });

  it("keeps a failed send when the owner has left the conversation entirely", async () => {
    // Same defect, second door: 跳頁 while the send is in flight unmounts the
    // composer, so restoring into component state discards it just as quietly
    // as restoring into the wrong room did. The room's draft is the only place
    // that outlives the component.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, unmount } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    unmount();
    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(getChatDraft("m1")).toEqual({
      text: "給 m1 的",
      attachments: [],
      replyTo: "c-1",
    });
  });

  it("a reply whose original is GONE says one fixed sentence, at once and for good", () => {
    // The state the server describes as `reply_to` set and `reply_to_chat`
    // absent: the quoted message was cleared, or the member that held it is
    // gone. It is SETTLED — the server rebuilt this snapshot on the read that
    // produced this very row, so there is nothing left to try.
    //
    // 🔴 READ SYNCHRONOUSLY, AND THAT IS HALF THE TEST. No `waitFor`: the
    // sentence has to be right on the FIRST frame, because a row that only
    // became correct later would have been WRONG first — and "wrong first,
    // right later" is precisely the shape this redesign deleted. The version of
    // this test that shipped before 2026-08-21 asserted the opposite: an
    // ellipsis first, the miss second.
    //
    // That nothing later CHANGES it is a separate claim and lives in its own
    // file (ChatArea.quote-no-fetch.test.tsx), which flushes every effect and
    // microtask and then asserts the api was never touched.
    messages = [
      mkMsg({
        id: "c-9",
        from: "owner",
        to: "m1",
        body: "回覆很久以前那則",
        ts: 9,
        replyTo: "c-longgone",
        replyToChat: null,
      }),
    ];
    const { container } = renderChat();
    const quote = rowOf(container, "c-9").querySelector(".chat__msg-quote")!;

    // ① right immediately — and NOT via the ellipsis that used to mean
    // "the by-id read has not landed yet".
    expect(quote.textContent).toContain(zh.chat.replyQuoteGone);
    expect(quote.textContent, "no spinner: nothing is in flight").not.toContain(
      "\u2026",
    );
    // ② NO jump control: an affordance that scrolls nowhere is worse than a
    // line that never offered one.
    expect(quote.querySelector("[data-testid='msg-quote-jump']")).toBeNull();
  });

  it("the banner says something TRUE about a target outside the loaded window — never the row's 「已不存在」 assertion", () => {
    // The banner used to fall back to the PEER's name whenever the quote had not
    // come back. That is a claim, not a placeholder: this conversation has only
    // two people, so the fallback is a coin flip printed as a fact.
    //
    // 🔴 AND THE SECOND CLAIM IS JUST AS WRONG, which is what this test is for
    // now. The banner resolves its target from the LOADED WINDOW alone
    // (`messageById`) — no fetch — so "not found here" says nothing whatever
    // about whether the message exists. The reachable path needs no hard reload:
    // scroll up to load scrollback, aim at an old row, switch to another member
    // and come back (the thread reloads only the newest page). The message is
    // still on the server, the send still succeeds, and the reply's own quote
    // row comes back complete — measured. Printing 「這則訊息已不存在」 in that
    // state is a claim the owner can disprove by sending the message.
    //
    // So the banner carries a STATE-INDEPENDENT sentence, and the assertive one
    // is reserved for the quote row, where the server's answer earns it.
    //
    // The fixture is that path in miniature: a saved draft aimed at an id the
    // loaded window does not contain. There is no 「…」 phase on the way — nothing
    // is being waited for, so the banner is in its final state on frame one.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    saveChatDraftText("m1", "", "c-longgone");
    const { container } = renderChat();

    const b = banner(container)!;
    expect(b).not.toBeNull();
    expect(b.textContent).toContain(zh.chat.replyingToEarlier);
    expect(
      b.textContent,
      "the banner cannot see past the loaded window, so it may not assert the " +
        "message is gone",
    ).not.toContain(zh.chat.replyQuoteGone);
    expect(b.textContent, "never the peer's name").not.toContain("Mira");
    expect(b.textContent, "and never a spinner").not.toContain("\u2026");

    // …and the two sentences really are different strings, or the assertion
    // above would be vacuous the day someone points both keys at one value.
    expect(zh.chat.replyingToEarlier).not.toContain(zh.chat.replyQuoteGone);
  });

  it("names a QUOTED released outsource worker by codename, exactly as its own row does", () => {
    // 🔴 ONE MEMBER, TWO IDENTITIES. `unknownOwIds` — the list fed to the lazy
    // codename lookup — was built from `m.from` / `m.to` only, and the quote row
    // renders a THIRD id: `nameOf(m.replyToChat.from)`. So a released worker
    // whose own row said 「外包 · R-2」 was printed as the raw `ow-rel` the
    // moment someone quoted it, and the row's accessible name degraded with it
    // (「引用 ow-rel」). Both halves are asserted, because the aria-label is the
    // only thing a screen-reader user gets.
    //
    // The fixture is deliberate: the quoted sender appears NOWHERE else in the
    // window (no row of its own), so nothing but the quote can put it into the
    // lookup. A test where the worker also has a visible row would pass with the
    // bug still in place.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "先看這個" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "我回外包那句",
        ts: 2,
        replyTo: "c-far",
        replyToChat: mkQuote("c-far", "ow-rel", "owner", "外包那句話"),
      }),
    ];
    const { container } = renderChat();

    const quote = rowOf(container, "c-2").querySelector(".chat__msg-quote")!;
    expect(quote.querySelector(".chat__msg-quote__who")?.textContent).toBe(
      `${zhMsg.outsourceLabel("R-2")} → ${zh.user}`,
    );
    expect(
      quote.querySelector(".chat__msg-quote__who")?.textContent,
      "never the raw id the roster could not resolve",
    ).not.toBe("ow-rel");
    expect(quote.getAttribute("aria-label")).toBe(
      zh.chat.replyQuoteRoleWho(`${zhMsg.outsourceLabel("R-2")} → ${zh.user}`),
    );
  });

  it("draws the quote from the WIRE, never from the message it can see", () => {
    // 🔴 THE ONE TEST THAT SEPARATES THE TWO DESIGNS. Both rows are in the
    // loaded window, so the old shape would have resolved the quote by looking
    // `replyTo` up locally and rendering c-1's own body. The server's snapshot
    // deliberately says something else here, and the row must show the server's
    // version: the wire is the source, and "it is on screen anyway" is not a
    // shortcut the component is allowed to take.
    //
    // This is not a hypothetical difference. The server SHORTENS the content
    // (60 runes) and collapses its whitespace; a component that quietly fell
    // back to the local body would render long, multi-line quotes for exactly
    // the messages a reader can already see, and correct ones for the rest —
    // an inconsistency nobody would think to look for.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "第一個問題" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "答案",
        ts: 2,
        replyTo: "c-1",
        replyToChat: mkQuote("c-1", "m1", "owner", "伺服器組出來的那一行"),
      }),
    ];
    const { container } = renderChat();

    const body = rowOf(container, "c-2").querySelector(
      ".chat__msg-quote__body",
    )!;
    expect(body.textContent).toBe("伺服器組出來的那一行");
    expect(
      body.textContent,
      "the local copy of the quoted message must not win",
    ).not.toBe("第一個問題");
    // …and the JUMP is still offered, because that question IS answered locally:
    // the target really is in the window. The two must not have been collapsed
    // into one lookup.
    expect(
      rowOf(container, "c-2").querySelector("[data-testid='msg-quote-jump']"),
    ).not.toBeNull();
  });

  it("a reply target does not follow the owner into the next conversation", async () => {
    // The peer-switch block clears it in the same render-phase adjustment that
    // swaps the draft, and its comment says MUST — but nothing was standing on
    // that line: deleting it outright left all 241 ChatArea tests green. The
    // failure it prevents used to be a loud one — the server refused a reply_to
    // that crossed conversations, so a leftover target 400'd on every send and
    // the composer's console.warn swallowed it. That refusal was DELETED on
    // 2026-08-21 (owner: quoting sideways is the use case), which makes this
    // guard MORE load-bearing, not less: the send now SUCCEEDS, and a message to
    // the new peer arrives carrying a quote row built from the previous room —
    // assembled faithfully by the server and shown to the recipient as context.
    // Do not delete this on the belief that the server still catches it.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    expect(banner(container), "aimed in m1").not.toBeNull();

    rerender(
      <I18nProvider>
        <ChatArea key={m2.id} member={m2} />
      </I18nProvider>,
    );
    expect(banner(container), "m2 inherits nothing").toBeNull();

    // Positive control: coming BACK must restore m1's own target, or "clear it
    // always" would pass this test too.
    rerender(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} />
      </I18nProvider>,
    );
    expect(banner(container), "m1 keeps its own").not.toBeNull();
  });

  // ── the quote says WHO TO, and it is the ORIGINAL's addressee ──────────────
  //
  // 🔴 THE ONE CELL THIS WHOLE FIELD EXISTS FOR. Since 2026-08-21 a reply may
  // quote a line out of a conversation the replier is in neither end of, and a
  // quote row that names only the sender then reads as though that line had
  // been said HERE. The addressee it must print is the QUOTED message's own —
  // and the plausible wrong answer (this thread's peer) is available, adjacent,
  // and identical-looking in every same-conversation fixture in this file.
  //
  // So the fixture makes the two DIFFER: the quoted line went m2 → ow-rel, and
  // this window is m1. Neither end of the quoted message is a participant in
  // the thread drawing it.
  //
  // MUTANT ①: drop `to` from the wire (`ChatReplyQuoteDTO`) — the quote row can
  // no longer name an addressee and this test is what says so.
  // MUTANT ②: wire the recipient to this thread's peer (`member.id`) instead of
  // `quoted.to`. MEASURED, not assumed: four tests in this file redden, not one
  // — the other three quote a message of THIS conversation whose addressee is
  // the owner, and the mutant renames him to the peer, which those tests happen
  // to notice. What only THIS test can say is the part that matters: the two
  // answers are DIFFERENT PEOPLE here, so a green run cannot mean "the peer was
  // the right answer all along". Do not delete the other three as redundant;
  // do not read this note as claiming exclusivity it does not have.
  it("names the QUOTED message's own addressee, not this thread's peer", () => {
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "先看這個" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "你們那句我插一句",
        ts: 2,
        replyTo: "c-elsewhere",
        replyToChat: mkQuote("c-elsewhere", "m2", "ow-rel", "那個 leak 在 warden"),
      }),
    ];
    // m2 is on the ROSTER but not in this thread — so its name resolving here
    // proves the row went through `nameOf`, and the id it fed in came off the
    // quote rather than off the window.
    const { container } = render(
      <I18nProvider>
        <ChatArea key={m1.id} member={m1} members={[m1, m2]} />
      </I18nProvider>,
    );

    const who = rowOf(container, "c-2").querySelector(
      ".chat__msg-quote__who",
    )!.textContent;
    // Whole-string equality, both halves at once: a partial match on the sender
    // would pass against a row that printed the sender alone.
    expect(who).toBe("Kyle → ow-rel");
    // …and it is emphatically NOT this window's peer, which is the answer a
    // recipient read off the wrong object produces.
    expect(who, "the peer of THIS thread is not the quoted addressee").not.toBe(
      `Kyle → ${m1.name}`,
    );
    // The accessible name carries the same pair — the a11y tree is the only
    // place a screen-reader user learns who was being addressed.
    expect(
      rowOf(container, "c-2")
        .querySelector(".chat__msg-quote")!
        .getAttribute("aria-label"),
    ).toBe(zh.chat.replyQuoteRoleWho("Kyle → ow-rel"));
  });

  // ── the owner is called what HE called himself ─────────────────────────────
  //
  // 🔴 REPORTED FROM THE RUNNING COCKPIT: the profile pill read 「韓立（你）」
  // and this thread called the same person 「市長（你）」 — the 仙俠 theme's
  // DEFAULT word for the human (`t.user`), which is what the owner branch of
  // `nameOf` was printing. One person, two names, one screen.
  //
  // MUTANT: put `return t.user` back in `nameOf`'s owner branch — the first
  // test below goes red and the second stays green, because the second is
  // exactly the state in which the default IS the right answer.
  describe("the owner's own name", () => {
    beforeEach(() => {
      messages = [
        mkMsg({ id: "c-1", from: "owner", to: "m1", body: "我說的" }),
        mkMsg({
          id: "c-2",
          from: "m1",
          to: "owner",
          body: "收到",
          ts: 2,
          replyTo: "c-1",
          replyToChat: mkQuote("c-1", "owner", "m1", "我說的"),
        }),
      ];
    });

    it("prints the nickname he set, not the theme's default word for him", () => {
      const { container } = render(
        <I18nProvider>
          <OwnerNameProvider value="韓立">
            <ChatArea key={m1.id} member={m1} />
          </OwnerNameProvider>
        </I18nProvider>,
      );
      // Anti-vacuity: the two strings must actually differ, or every assertion
      // below is true of both the fixed and the broken component.
      expect(zh.user).not.toBe("韓立");

      expect(
        rowOf(container, "c-2").querySelector(".chat__msg-quote__who")
          ?.textContent,
      ).toBe(`韓立 → ${m1.name}`);
      // The composer banner is the other surface that names him, and it
      // resolves the name by a different route (the loaded window, not
      // `reply_to_chat`) — the two must not disagree about who he is.
      fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
      expect(
        banner(container)!.querySelector(".chat__reply-banner__who")?.textContent,
      ).toBe(zh.chat.replyingTo(`韓立 → ${m1.name}`));
    });

    it("falls back to the localized default when he has set none", () => {
      // No provider — which is also what a settings read that FAILED resolves
      // to, deliberately: a failure may never masquerade as a name.
      const { container } = renderChat();
      expect(
        rowOf(container, "c-2").querySelector(".chat__msg-quote__who")
          ?.textContent,
      ).toBe(`${zh.user} → ${m1.name}`);
    });
  });
});
