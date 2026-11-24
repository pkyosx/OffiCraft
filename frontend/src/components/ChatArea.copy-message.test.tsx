import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { resetChatDrafts } from "../lib/chatDraftStore";

let messages: ChatMessage[] = [];

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
    ts: 1,
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
      <ChatArea member={mkMember("m1", "Mira")} members={[mkMember("m2", "Kyle")]} />
    </I18nProvider>,
  );
}

const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;
const copyButtonIn = (row: HTMLElement) =>
  row.querySelector(".chat__msg-copy") as HTMLButtonElement | null;
const showsCheck = (el: Element) => el.querySelector('svg path[d="M20 6 9 17l-5-5"]') !== null;

let clipboard: string[] = [];
let clipboardWorks = true;

beforeEach(() => {
  resetChatDrafts();
  Element.prototype.scrollIntoView = () => {};
  clipboard = [];
  clipboardWorks = true;
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: (text: string) => {
        if (!clipboardWorks) return Promise.reject(new Error("denied"));
        clipboard.push(text);
        return Promise.resolve();
      },
    },
  });
  // The http fallback path; jsdom has no execCommand of its own.
  document.execCommand = () => false;
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function click(el: HTMLElement) {
  await act(async () => {
    fireEvent.click(el);
  });
}

describe("ChatArea 複製訊息", () => {
  it("copies the body's markdown with mentions as plain @名字 — no attachment, no quoted line", async () => {
    messages = [
      mkMsg({ id: "q-1", body: "原本那句" }),
      mkMsg({
        id: "c-1",
        body: "**部署好了**，[@Kyle](/#office/member/m2) 幫忙看 `make ci`\n- 第一點",
        attachments: [
          { id: "a1", url: "/x", filename: "log.txt", mime: "text/plain", isImage: false },
        ],
        replyTo: "q-1",
        replyToChat: { id: "q-1", from: "m1", fromName: "", to: "owner", toName: "", content: "原本那句" },
      }),
    ];
    const { container } = renderChat();

    await click(copyButtonIn(rowOf(container, "c-1"))!);

    expect(clipboard).toEqual(["**部署好了**，@Kyle 幫忙看 `make ci`\n- 第一點"]);
  });

  it("copies a reply card's question: the summary, a blank line, the body as stored", async () => {
    vi.spyOn(api, "getReplyCard").mockResolvedValue({
      id: "rc-1",
      summary: "要不要先備份？",
      body: "**migration 0142** 會改索引，[@Kyle](/#office/member/m2) 說要先備份。\n- 選項見下",
      options: [{ label: "先備份" }],
      attachments: [{ id: "a1", url: "/x", filename: "plan.md", mime: "text/markdown", isImage: false }],
    } as unknown as ReplyCard);
    messages = [mkMsg({ id: "card", body: "要不要先備份？", replyCardId: "rc-1" })];
    const { container } = renderChat();

    await click(copyButtonIn(rowOf(container, "card"))!);

    expect(api.getReplyCard).toHaveBeenCalledWith("rc-1");
    expect(clipboard).toEqual([
      "要不要先備份？\n\n**migration 0142** 會改索引，@Kyle 說要先備份。\n- 選項見下",
    ]);
    expect(within(rowOf(container, "card")).getByRole("status").textContent).toBe("已複製");
  });

  it("turns the copy button into a check for 1.6s after the latest real copy, with 已複製 only for a screen reader, then goes back to 複製訊息", async () => {
    vi.useFakeTimers();
    messages = [mkMsg({ id: "c-1", body: "一段話" })];
    const { container } = renderChat();
    const row = rowOf(container, "c-1");

    await click(copyButtonIn(row)!);

    expect(showsCheck(copyButtonIn(row)!)).toBe(true);
    const status = within(row).getByRole("status");
    expect(status.className).toBe("chat__msg-copy-status");
    expect(status.children).toHaveLength(1);
    expect(status.firstElementChild!.className).toBe("chat__sr-text");
    expect(status.textContent).toBe("已複製");
    expect(copyButtonIn(row)!.getAttribute("aria-label")).toBe("已複製");
    expect(copyButtonIn(row)!.getAttribute("title")).toBe("已複製");

    act(() => {
      vi.advanceTimersByTime(1600);
    });
    expect(within(row).getByRole("status").textContent).toBe("");
    expect(copyButtonIn(row)!.getAttribute("aria-label")).toBe("複製訊息");

    await click(copyButtonIn(row)!);
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    await click(copyButtonIn(row)!);
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(within(row).getByRole("status").textContent).toBe("已複製");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(within(row).getByRole("status").textContent).toBe("");
    expect(copyButtonIn(row)!.getAttribute("aria-label")).toBe("複製訊息");
  });

  it("says 複製失敗 on a reply card whose question could not be read, and copies nothing", async () => {
    vi.spyOn(api, "getReplyCard").mockRejectedValue(new Error("offline"));
    messages = [mkMsg({ id: "card", body: "要不要先備份？", replyCardId: "rc-1" })];
    const { container } = renderChat();

    await click(copyButtonIn(rowOf(container, "card"))!);

    expect(clipboard).toEqual([]);
    expect(within(rowOf(container, "card")).getByRole("status").textContent).toBe("複製失敗");
  });

  it("says 複製失敗, never 已複製, when nothing reached the clipboard", async () => {
    clipboardWorks = false;
    messages = [mkMsg({ id: "c-1", body: "一段話" })];
    const { container } = renderChat();
    const row = rowOf(container, "c-1");

    await click(copyButtonIn(row)!);

    expect(clipboard).toEqual([]);
    const status = within(row).getByRole("status");
    expect(status.classList.contains("chat__msg-copy-status--failed")).toBe(true);
    expect(status.children).toHaveLength(0);
    expect(status.textContent).toBe("複製失敗");
    expect(copyButtonIn(row)!.getAttribute("aria-label")).toBe("複製失敗");
    expect(copyButtonIn(row)!.getAttribute("title")).toBe("複製失敗");
  });

  it("offers 複製 on every message with text, reply cards included", () => {
    messages = [
      mkMsg({ id: "theirs", body: "對方的話" }),
      mkMsg({ id: "mine", from: "owner", to: "m1", body: "我的話", ts: 2 }),
      mkMsg({
        id: "file-only",
        ts: 3,
        attachments: [
          { id: "a1", url: "/x", filename: "p.png", mime: "image/png", isImage: true },
        ],
      }),
      mkMsg({ id: "card", body: "請示", ts: 4, replyCardId: "rc-1" }),
      mkMsg({ id: "gone", from: "owner", to: "m1", body: "刪掉的", ts: 5, deletedTs: 6 }),
    ];
    const { container } = renderChat();

    const offered = messages
      .filter((m) => copyButtonIn(rowOf(container, m.id)) !== null)
      .map((m) => m.id);
    expect(offered).toEqual(["theirs", "mine", "card"]);
  });
});
