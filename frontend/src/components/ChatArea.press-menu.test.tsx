// ChatArea on a touch screen: a long press on a message opens its action menu.
// The thread comes from a mocked useChat (as in the other ChatArea tests).
// jsdom has no PointerEvent, so the presses are MouseEvents carrying the
// pointerType a finger reports; `(hover: none)` is stubbed on, as on a phone,
// unless a test says the screen also has a mouse.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { resetChatDrafts } from "../lib/chatDraftStore";
import { api } from "../api";

let messages: ChatMessage[] = [];
const NOW = 1_800_000_000;

vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: () => new Map(),
}));
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
    terminalAttachCommand: `tmux -L officraft attach -t member-${id}`,
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
    ts: NOW - 60,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    replyTo: null,
    replyToChat: null,
    ...over,
  };
}

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={mkMember("m1", "Mira")} members={[]} />
    </I18nProvider>,
  );
}

const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;
const bubbleOf = (c: HTMLElement, id: string) =>
  rowOf(c, id).querySelector(".chat__msg-bubble, .reply-card") as HTMLElement;
const menus = () => Array.from(document.querySelectorAll("[role='menu']"));
const menuLabels = () =>
  Array.from(document.querySelectorAll("[role='menuitem']")).map((b) => b.textContent);

function pointer(type: string, el: Element, pointerType: string, x = 10, y = 10) {
  const e = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, clientY: y });
  Object.defineProperty(e, "pointerType", { value: pointerType });
  act(() => {
    el.dispatchEvent(e);
  });
}

function choose(label: string) {
  const item = within(menus()[0] as HTMLElement).getByText(label);
  pointer("pointerdown", item, "touch");
  fireEvent.click(item, { detail: 1 });
}

function tap(el: Element) {
  pointer("pointerdown", el, "mouse");
  return fireEvent.click(el);
}

function tapBackdrop() {
  const backdrop = document.querySelector("[data-testid='chat-msg-sheet-backdrop']")!;
  pointer("pointerdown", backdrop, "touch");
  fireEvent.click(backdrop);
}

function hold(el: Element, ms: number, pointerType = "touch") {
  pointer("pointerdown", el, pointerType);
  act(() => {
    vi.advanceTimersByTime(ms);
  });
  pointer("pointerup", el, pointerType);
}

let clipboard: string[] = [];
let hoverNone = true;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
  vi.setSystemTime(NOW * 1000);
  hoverNone = true;
  resetChatDrafts();
  Element.prototype.scrollIntoView = () => {};
  Range.prototype.getBoundingClientRect = () => new DOMRect(0, 0, 10, 10);
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: (query: string) => ({
      matches: query === (hoverNone ? "(hover: none)" : "(hover: hover)"),
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }),
  });
  clipboard = [];
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: (text: string) => {
        clipboard.push(text);
        return Promise.resolve();
      },
    },
  });
  messages = [
    mkMsg({ id: "theirs", body: "對方的一段話" }),
    mkMsg({ id: "mine", from: "owner", to: "m1", body: "我剛送的" }),
    mkMsg({ id: "mine-old", from: "owner", to: "m1", body: "兩天前的", ts: NOW - 2 * 86400 }),
    mkMsg({ id: "card", body: "要不要先備份？", replyCardId: "rc-1" }),
  ];
});

afterEach(() => {
  delete (window as unknown as { matchMedia?: unknown }).matchMedia;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("ChatArea long-press sheet", () => {
  it("offers each message's own actions plus 選取文字", () => {
    const { container } = renderChat();

    hold(bubbleOf(container, "theirs"), 500);
    expect(menuLabels()).toEqual(["回覆這則", "複製訊息", "放大閱讀", "選取文字"]);
    tapBackdrop();

    hold(bubbleOf(container, "mine"), 500);
    expect(menuLabels()).toEqual(["回覆這則", "複製訊息", "刪除", "選取文字"]);
    tapBackdrop();

    hold(bubbleOf(container, "mine-old"), 500);
    expect(menuLabels()).toEqual(["回覆這則", "複製訊息", "選取文字"]);
    tapBackdrop();

    hold(bubbleOf(container, "card"), 500);
    expect(menuLabels()).toEqual(["回覆這則", "複製訊息", "選取文字"]);
  });

  it("opens only after a 500ms hold that stays put, and only for a finger", () => {
    const { container } = renderChat();
    const bubble = bubbleOf(container, "theirs");

    hold(bubble, 499);
    expect(menus()).toHaveLength(0);

    pointer("pointerdown", bubble, "touch", 10, 10);
    pointer("pointermove", bubble, "touch", 10, 22);
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(menus()).toHaveLength(0);

    hold(bubble, 600, "mouse");
    expect(menus()).toHaveLength(0);

    hold(bubble, 500);
    expect(menus()).toHaveLength(1);
  });

  it("opens for a finger on a screen that also has a mouse", () => {
    hoverNone = false;
    const { container } = renderChat();

    hold(bubbleOf(container, "theirs"), 600, "mouse");
    expect(menus()).toHaveLength(0);

    hold(bubbleOf(container, "theirs"), 500);
    expect(menuLabels()).toEqual(["回覆這則", "複製訊息", "放大閱讀", "選取文字"]);
  });

  it("keeps one menu open at a time, and a tap on the backdrop closes it", () => {
    const { container } = renderChat();

    hold(bubbleOf(container, "theirs"), 500);
    hold(bubbleOf(container, "mine"), 500);
    expect(menus()).toHaveLength(1);
    expect(menuLabels()).toContain("刪除");
    expect(rowOf(container, "mine").classList.contains("chat__msg--pressed")).toBe(true);
    expect(rowOf(container, "theirs").classList.contains("chat__msg--pressed")).toBe(false);

    // The click a lifting finger leaves behind lands on the backdrop, or on an
    // item when the message sat low on the screen; neither counts.
    fireEvent.click(document.querySelector("[data-testid='chat-msg-sheet-backdrop']")!);
    fireEvent.click(within(menus()[0] as HTMLElement).getByText("刪除"), { detail: 1 });
    expect(menus()).toHaveLength(1);
    expect(document.querySelector("[data-testid='chat-delete-confirm']")).toBeNull();

    tapBackdrop();
    expect(menus()).toHaveLength(0);
    expect(rowOf(container, "mine").classList.contains("chat__msg--pressed")).toBe(false);

    hold(bubbleOf(container, "mine"), 500);
    choose("刪除");
    expect(document.querySelector("[data-testid='chat-delete-confirm']")).not.toBeNull();
  });

  it("after a long press, the first keyboard activation of a message's button is not swallowed", async () => {
    const { container } = renderChat();
    hold(bubbleOf(container, "mine"), 500);
    tapBackdrop();
    expect(menus()).toHaveLength(0);
    await act(async () => {
      fireEvent.click(within(rowOf(container, "theirs")).getByRole("button", { name: "複製訊息" }), { detail: 0 });
    });
    expect(clipboard).toEqual(["對方的一段話"]);
  });

  it("does what the item says and closes; a 複製 that worked leaves only words for a screen reader, a failed one shows 複製失敗 on the message", async () => {
    const { container } = renderChat();

    hold(bubbleOf(container, "theirs"), 500);
    choose("回覆這則");
    expect(menus()).toHaveLength(0);
    expect(
      container.querySelector("[data-testid='chat-reply-banner']")?.textContent,
    ).toContain("對方的一段話");

    hold(bubbleOf(container, "theirs"), 500);
    await act(async () => {
      choose("複製訊息");
    });
    expect(menus()).toHaveLength(0);
    expect(clipboard).toEqual(["對方的一段話"]);
    const status = within(rowOf(container, "theirs")).getByRole("status");
    expect(status.className).toBe("chat__msg-copy-status");
    expect(status.innerHTML).toBe('<span class="chat__sr-text">已複製</span>');

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error("denied")) },
    });
    hold(bubbleOf(container, "theirs"), 500);
    await act(async () => {
      choose("複製訊息");
    });
    expect(menus()).toHaveLength(0);
    expect(status.classList.contains("chat__msg-copy-status--failed")).toBe(true);
    expect(status.innerHTML).toBe("複製失敗");
  });

  it("選取文字 enters the message's selection mode in the composer's place: 複製 copies what is selected, shows its result on itself and stays, 回覆 quotes it and ends the mode with the draft intact and the input focused", async () => {
    const { container } = renderChat();
    const bar = () => document.querySelector("[data-testid='chat-select-bar']");
    const button = (id: string) =>
      document.querySelector(`[data-testid='${id}']`) as HTMLButtonElement;
    const selectionChanged = () =>
      act(() => {
        document.dispatchEvent(new Event("selectionchange"));
      });

    const input = container.querySelector<HTMLTextAreaElement>(".chat__input")!;
    fireEvent.change(input, { target: { value: "還沒送出的草稿" } });
    hold(bubbleOf(container, "theirs"), 500);
    choose("選取文字");

    expect(container.querySelector<HTMLElement>(".chat__composer")!.hidden).toBe(true);
    expect(rowOf(container, "theirs").classList.contains("chat__msg--selecting")).toBe(true);
    expect(container.querySelector(".chat__messages")!.classList.contains("chat__messages--selecting")).toBe(true);
    expect(window.getSelection()!.toString()).toBe("對方的一段話");
    selectionChanged();
    expect(button("chat-select-copy").disabled).toBe(false);
    expect(button("chat-select-reply").disabled).toBe(false);
    // The message in the mode takes no long press of its own.
    hold(bubbleOf(container, "theirs"), 500);
    expect(menus()).toHaveLength(0);

    const text = rowOf(container, "theirs").querySelector(".chat__msg-text p")!.firstChild!;
    const select = () =>
      act(() => {
        const range = document.createRange();
        range.setStart(text, 3);
        range.setEnd(text, 5);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(range);
      });
    select();
    selectionChanged();
    await act(async () => {
      fireEvent.click(button("chat-select-copy"));
    });
    expect(clipboard).toEqual(["一段"]);
    expect(button("chat-select-copy").querySelector("svg")).not.toBeNull();
    expect(button("chat-select-copy").textContent).toBe(
      button("chat-select-copy").querySelector(".chat__sr-text")!.textContent,
    );
    expect(button("chat-select-copy").textContent).toBe("已複製");
    expect(bar()!.querySelector("[role='status']")!.textContent).toBe("已複製");
    expect(rowOf(container, "theirs").classList.contains("chat__msg--selecting")).toBe(true);
    act(() => {
      window.getSelection()!.removeAllRanges();
    });
    selectionChanged();
    expect(button("chat-select-copy").disabled).toBe(true);
    expect(button("chat-select-reply").disabled).toBe(true);
    select();
    selectionChanged();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error("denied")) },
    });
    await act(async () => {
      fireEvent.click(button("chat-select-copy"));
    });
    expect(button("chat-select-copy").textContent).toBe("複製失敗");
    expect(bar()!.querySelector("[role='status']")!.textContent).toBe("複製失敗");

    fireEvent.click(button("chat-select-reply"));
    expect(container.querySelector("[data-testid='chat-reply-banner']")?.textContent).toContain(
      "一段",
    );
    expect(bar()).toBeNull();
    expect(rowOf(container, "theirs").classList.contains("chat__msg--selecting")).toBe(false);
    expect(container.querySelector<HTMLElement>(".chat__composer")!.hidden).toBe(false);
    expect(input.value).toBe("還沒送出的草稿");
    expect(document.activeElement).toBe(input);
  });

  it("in selection mode with nothing selected, 複製 and 回覆 are off; 取消, Esc and a tap outside the message end the mode, a reply card back folded or open as it was", async () => {
    const { container } = renderChat();
    const bar = () => document.querySelector("[data-testid='chat-select-bar']");
    const button = (id: string) =>
      document.querySelector(`[data-testid='${id}']`) as HTMLButtonElement;
    const enter = () => {
      hold(bubbleOf(container, "theirs"), 500);
      choose("選取文字");
      expect(bar()).not.toBeNull();
    };

    enter();
    act(() => {
      window.getSelection()!.removeAllRanges();
      document.dispatchEvent(new Event("selectionchange"));
    });
    expect(button("chat-select-copy").disabled).toBe(true);
    expect(button("chat-select-reply").disabled).toBe(true);
    expect(button("chat-select-copy").textContent).toBe("複製");

    fireEvent.click(button("chat-select-cancel"));
    expect(bar()).toBeNull();

    enter();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(bar()).toBeNull();

    enter();
    fireEvent.click(rowOf(container, "theirs").querySelector(".chat__msg-text")!);
    expect(bar()).not.toBeNull();
    fireEvent.click(container.querySelector(".chat__messages")!);
    expect(bar()).toBeNull();
    expect(rowOf(container, "theirs").classList.contains("chat__msg--selecting")).toBe(false);

    const cardRow = () => rowOf(container, "card");
    const folded = () => cardRow().querySelector("[data-testid='chat-reply-card-expand']") !== null;
    const enterCard = () => {
      hold(bubbleOf(container, "card"), 500);
      choose("選取文字");
      expect(bar()).not.toBeNull();
      expect(folded()).toBe(false);
    };
    expect(folded()).toBe(true);
    enterCard();
    tap(cardRow().querySelector(".reply-card__summary")!);
    tap(cardRow().querySelector(".reply-card")!);
    expect(folded()).toBe(false);
    expect(bar()).not.toBeNull();
    fireEvent.click(button("chat-select-cancel"));
    expect(folded()).toBe(true);

    enterCard();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(folded()).toBe(true);

    enterCard();
    fireEvent.click(container.querySelector(".chat__messages")!);
    expect(folded()).toBe(true);

    enterCard();
    act(() => {
      const range = document.createRange();
      range.selectNodeContents(cardRow().querySelector(".reply-card__summary")!);
      window.getSelection()!.removeAllRanges();
      window.getSelection()!.addRange(range);
      document.dispatchEvent(new Event("selectionchange"));
    });
    await act(async () => {
      fireEvent.click(button("chat-select-copy"));
    });
    expect(clipboard).toEqual(["要不要先備份？"]);
    expect(folded()).toBe(false);
    fireEvent.click(button("chat-select-cancel"));
    expect(folded()).toBe(true);

    tap(cardRow().querySelector("[data-testid='chat-reply-card-expand']")!);
    expect(folded()).toBe(false);
    enterCard();
    fireEvent.click(button("chat-select-cancel"));
    expect(folded()).toBe(false);
  });

  it("in selection mode a link, an image or the quote in the message does nothing, and the mode stays; after 取消 they work again", async () => {
    const getOriginal = vi.spyOn(api, "getChatMessage");
    messages = [
      ...messages,
      mkMsg({
        id: "rich",
        body: "看 [這份](https://example.com/plan) 再說",
        attachments: [{ id: "a1", url: "/x", filename: "p.png", mime: "image/png", isImage: true }],
        replyTo: "theirs",
        replyToChat: { id: "theirs", from: "m1", fromName: "", to: "owner", toName: "", content: "對方的一段話" },
      }),
    ];
    const { container } = renderChat();
    hold(bubbleOf(container, "rich"), 500);
    choose("選取文字");
    const row = rowOf(container, "rich");

    expect(tap(row.querySelector(".chat__msg-text a")!)).toBe(false);
    tap(row.querySelector(".chat__msg-image")!);
    fireEvent.keyDown(row.querySelector(".chat__msg-image")!, { key: "Enter" });
    tap(row.querySelector("[data-testid='msg-quote-jump']")!);

    expect(getOriginal).not.toHaveBeenCalled();
    expect(document.querySelector("[role='dialog']")).toBeNull();
    expect(row.classList.contains("chat__msg--selecting")).toBe(true);
    expect(document.querySelector("[data-testid='chat-select-bar']")).not.toBeNull();

    fireEvent.click(document.querySelector("[data-testid='chat-select-cancel']")!);
    expect(row.classList.contains("chat__msg--selecting")).toBe(false);
    expect(tap(row.querySelector(".chat__msg-text a")!)).toBe(true);
    expect(fireEvent.keyDown(row.querySelector(".chat__msg-text a")!, { key: "Enter" })).toBe(true);
    fireEvent.keyDown(row.querySelector(".chat__msg-image")!, { key: "Enter" });
    expect(document.querySelector("[role='dialog']")).not.toBeNull();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    await act(async () => {
      tap(row.querySelector("[data-testid='msg-quote-jump']")!);
    });
    expect(getOriginal).toHaveBeenCalledWith("theirs");
  });
});
