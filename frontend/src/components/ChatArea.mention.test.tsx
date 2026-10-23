import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, OutsourceWorkerView } from "../api/adapter";
import { pushEscapeLayer } from "../lib/escapeLayers";
import { getChatDraft } from "../lib/chatDraftStore";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";

let messages: ChatMessage[] = [];
const send = vi.fn((_body: string, ..._rest: unknown[]) => Promise.resolve());
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({ messages, peerLastReadTs: 0, send, markRead: vi.fn() }),
}));
let mobile = false;
vi.mock("../hooks/useIsMobile", () => ({ useIsMobile: () => mobile }));

function mkMember(id: string, name: string, roleName: string, kind = "staff", role = "custom"): Member {
  return {
    id,
    name,
    role,
    roleName,
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind,
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

const mira = mkMember("m1", "Mira", "Assistant", "staff", "assistant");
const eva = mkMember("m-eva", "Family Money Eva", "財務");
const warden = mkMember("w-1", "Warden", "", "warden");
const o7: OutsourceWorkerView = {
  id: "ow-7",
  codename: "O-7",
  model: "",
  effort: "",
  status: "active",
  taskId: "t-492",
  taskNo: "T-492",
  taskTitle: "Fix chat mention list",
};
const s3: OutsourceWorkerView = {
  id: "ow-3",
  codename: "S-3",
  model: "",
  effort: "",
  status: "assigned",
  taskId: "",
  taskNo: "",
  taskTitle: "",
};

function renderChat() {
  const utils = render(
    <I18nProvider>
      <ChatArea member={mira} members={[mira, eva, warden]} workers={[o7, s3]} rosterSettled />
    </I18nProvider>,
  );
  const input = utils.container.querySelector("textarea.chat__input") as HTMLTextAreaElement;
  const list = () => utils.container.querySelector('[role="listbox"]');
  const options = () =>
    Array.from(utils.container.querySelectorAll('[role="option"]'), (o) => o.textContent);
  const type = (value: string) => fireEvent.change(input, { target: { value } });
  return { ...utils, input, list, options, type };
}

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
  messages = [];
  mobile = false;
  send.mockReset();
  send.mockImplementation(() => Promise.resolve());
  localStorage.clear();
});
afterEach(() => {
  window.location.hash = "";
});

describe("ChatArea @mention picker", () => {
  it("typing @ → lists staff (not wardens) with the same role label as the office list, and outsource with task number and title", () => {
    const { input, list, options, type } = renderChat();
    expect(list()).toBeNull();
    type("hi @");
    expect(options()).toEqual([
      "Mira特助",
      "Family Money Eva財務",
      "外包 · O-7T-492Fix chat mention list",
      "外包 · S-3",
    ]);
    const first = list()!.querySelector('[role="option"]')!;
    expect(input.getAttribute("aria-activedescendant")).toBe(first.id);
    expect(first.getAttribute("aria-selected")).toBe("true");
  });

  it("text after @ → filters by name, task number, bare digits, title and role", () => {
    const { options, type } = renderChat();
    type("@eva");
    expect(options()).toEqual(["Family Money Eva財務"]);
    type("@T-492");
    expect(options()).toEqual(["外包 · O-7T-492Fix chat mention list"]);
    type("@492");
    expect(options()).toEqual(["外包 · O-7T-492Fix chat mention list"]);
    type("@mention");
    expect(options()).toEqual(["外包 · O-7T-492Fix chat mention list"]);
    type("@財務");
    expect(options()).toEqual(["Family Money Eva財務"]);
  });

  it("no row matches → no list, while a matching query shows one", () => {
    const { list, type } = renderChat();
    type("@zzz");
    expect(list()).toBeNull();
    type("@mi");
    expect(list()).not.toBeNull();
  });

  it("ArrowDown then Enter → replaces @query with the picked name and a space, no send", () => {
    const { input, list, type } = renderChat();
    type("ask @");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    const prevented = !fireEvent.keyDown(input, { key: "Enter" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("ask @Family Money Eva ");
    expect(input.selectionStart).toBe(22);
    expect(list()).toBeNull();
    expect(send).not.toHaveBeenCalled();
  });

  it("ArrowUp from the first row wraps to the last; Tab picks it", () => {
    const { input, type } = renderChat();
    type("@");
    fireEvent.keyDown(input, { key: "ArrowUp" });
    fireEvent.keyDown(input, { key: "Tab" });
    expect(input.value).toBe("@外包 · S-3 ");
  });

  it("clicking a row → picks it", () => {
    const { container, input, type } = renderChat();
    type("@");
    fireEvent.click(container.querySelectorAll('[role="option"]')[2]);
    expect(input.value).toBe("@外包 · O-7 ");
  });

  it("Esc while open → list closes, text unchanged, event consumed so the host layer stays open", () => {
    const hostClosed = vi.fn();
    const release = pushEscapeLayer({ onEscape: hostClosed, element: () => null });
    try {
      const { input, list, type } = renderChat();
      type("@mi");
      const prevented = !fireEvent.keyDown(input, { key: "Escape" });
      expect(prevented).toBe(true);
      expect(list()).toBeNull();
      expect(input.value).toBe("@mi");
      expect(hostClosed).not.toHaveBeenCalled();
      fireEvent.keyDown(input, { key: "Escape" });
      expect(hostClosed).toHaveBeenCalledTimes(1);
    } finally {
      release();
    }
  });

  it("Enter while IME is composing → no pick, list stays", () => {
    const { input, options, type } = renderChat();
    type("@mir");
    fireEvent.compositionStart(input);
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(input.value).toBe("@mir");
    expect(options()).toEqual(["Mira特助"]);
  });

  it("phone: Enter while open → picks (prevented, so no newline) and does not send", () => {
    mobile = true;
    const { input, type } = renderChat();
    type("@mi");
    const prevented = !fireEvent.keyDown(input, { key: "Enter" });
    expect(prevented).toBe(true);
    expect(input.value).toBe("@Mira ");
    expect(send).not.toHaveBeenCalled();
  });

  it("desktop Enter with the list closed → sends the body with mentions as links", async () => {
    const { input, type } = renderChat();
    type("@Fam");
    fireEvent.keyDown(input, { key: "Enter" });
    type("@Family Money Eva 和 @外包 · O-7 看一下 `cc @Mira`");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(send).toHaveBeenCalledTimes(1);
    expect(send.mock.calls[0][0]).toBe(
      "[@Family Money Eva](/#office/member/m-eva) 和 [@外包 · O-7](/#office/worker/ow-7) 看一下 `cc @Mira`",
    );
  });

  it("send fails → composer and draft get the plain @name text back, not the links", async () => {
    send.mockImplementation(() => Promise.reject(new Error("offline")));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { input, type } = renderChat();
    type("@Mira 在嗎");
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(send.mock.calls[0][0]).toBe("[@Mira](/#office/member/m1) 在嗎");
    expect(input.value).toBe("@Mira 在嗎");
    expect(getChatDraft("m1")?.text).toBe("@Mira 在嗎");
  });

  it("a mention in the draft → marked in the box, and Backspace after it removes it whole", () => {
    const { container, input, type } = renderChat();
    type("ask @Mira now");
    expect(
      Array.from(container.querySelectorAll(".mention-highlight__mark"), (m) => m.textContent),
    ).toEqual(["@Mira"]);
    input.setSelectionRange(9, 9);
    expect(fireEvent.keyDown(input, { key: "Backspace" })).toBe(false);
    expect(input.value).toBe("ask  now");
    expect(send).not.toHaveBeenCalled();
  });
});

describe("ChatArea mention links in message bodies", () => {
  it("known member → button with their roster name opening their detail over this chat; released worker → written label", () => {
    messages = [
      {
        id: "c-1",
        from: "m1",
        to: "owner",
        body: "問 [@Eva 舊名](/#office/member/m-eva) 和 [@外包 · O-9](/#office/worker/ow-9)",
        ts: 1000,
        attachments: [],
        replyCardId: null,
      },
    ];
    const { container } = renderChat();
    const body = container.querySelector(".chat__msg-text")!;
    expect(body.textContent).toBe("問 @Family Money Eva 和 @外包 · O-9");
    expect(body.querySelector("a")).toBeNull();
    const buttons = body.querySelectorAll("button.md-mention");
    expect(Array.from(buttons, (b) => b.textContent)).toEqual(["@Family Money Eva"]);
    fireEvent.click(buttons[0]);
    expect(window.location.hash).toBe("#office/chat/m1/member/m-eva");
  });

  it("live outsource worker link → shows its outsource label and opens the worker detail over this chat", () => {
    messages = [
      {
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "[@O-7](/#office/worker/ow-7)",
        ts: 1000,
        attachments: [],
        replyCardId: null,
      },
    ];
    const { container } = renderChat();
    const button = container.querySelector("button.md-mention")!;
    expect(button.textContent).toBe("@外包 · O-7");
    fireEvent.click(button);
    expect(window.location.hash).toBe("#office/chat/m1/worker/ow-7");
  });

  it("reply quote line and reply banner → mention links shown as plain @name", () => {
    messages = [
      {
        id: "c-1",
        from: "m1",
        to: "owner",
        body: "[@Family Money Eva](/#office/member/m-eva) 看一下",
        ts: 1000,
        attachments: [],
        replyCardId: null,
      },
      {
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "好",
        ts: 1001,
        attachments: [],
        replyCardId: null,
        replyTo: "c-1",
        replyToChat: {
          id: "c-1",
          from: "m1",
          fromName: "",
          to: "owner",
          toName: "",
          content: "[@Family Money Eva](/#office/member/m-eva) 看一下",
        },
      },
    ];
    const { container } = renderChat();
    const quote = container.querySelector(".chat__msg-quote__body")!;
    expect(quote.textContent).toBe("@Family Money Eva 看一下");
    expect(quote.getAttribute("title")).toBe("@Family Money Eva 看一下");
    fireEvent.click(
      container.querySelector('[data-msg-id="c-1"] .chat__msg-reply')!,
    );
    expect(container.querySelector(".chat__reply-banner__body")!.textContent).toBe(
      "@Family Money Eva 看一下",
    );
  });

  it("full view of a message → mention opens the detail and the overlay closes", async () => {
    messages = [
      {
        id: "c-1",
        from: "m1",
        to: "owner",
        body: "問 [@Eva](/#office/member/m-eva)",
        ts: 1000,
        attachments: [],
        replyCardId: null,
      },
    ];
    const { container } = renderChat();
    fireEvent.click(container.querySelector("button.chat__msg-expand")!);
    const mention = await waitFor(() => {
      const b = document.body.querySelector(".md-preview button.md-mention");
      expect(b).not.toBeNull();
      return b!;
    });
    expect(mention.textContent).toBe("@Family Money Eva");
    fireEvent.click(mention);
    expect(window.location.hash).toBe("#office/chat/m1/member/m-eva");
    expect(document.body.querySelector(".md-preview")).toBeNull();
  });
});

describe("ChatArea reply card in the thread", () => {
  it("answer box → offers this room's roster, sends the mention as a link, and the answer opens the detail over this chat", async () => {
    __resetMock();
    __injectMockReplyCard({
      id: "rc-9",
      from: "m1",
      kind: "decision",
      summary: "誰來接？",
      body: "",
      options: [],
      selectMode: "single",
      status: "waiting",
      attachments: [],
      createdTs: 1000,
      answeredTs: null,
      chatMessageId: "c-9",
      answer: null,
    });
    messages = [
      { id: "c-9", from: "m1", to: "owner", body: "誰來接？", ts: 1000, attachments: [], replyCardId: "rc-9" },
    ];
    const answerSpy = vi.spyOn(api, "answerReplyCard");
    const { container } = renderChat();
    fireEvent.click(container.querySelector("[data-testid='chat-reply-card-expand']")!);
    const box = await waitFor(() => {
      const el = container.querySelector(".reply-composer textarea") as HTMLTextAreaElement | null;
      expect(el).not.toBeNull();
      return el!;
    });
    fireEvent.change(box, { target: { value: "@fam" } });
    expect(
      Array.from(container.querySelectorAll(".reply-composer [role='option']"), (o) => o.textContent),
    ).toEqual(["Family Money Eva財務"]);
    fireEvent.keyDown(box, { key: "Enter" });
    expect(box.value).toBe("@Family Money Eva ");
    await act(async () => {
      fireEvent.keyDown(box, { key: "Enter" });
    });
    expect(answerSpy.mock.calls[0][1].text).toBe("[@Family Money Eva](/#office/member/m-eva)");
    const mention = await waitFor(() => {
      const b = container.querySelector("[data-testid='final-answer'] button.md-mention");
      expect(b).not.toBeNull();
      return b!;
    });
    fireEvent.click(mention);
    expect(window.location.hash).toBe("#office/chat/m1/member/m-eva");
  });
});
