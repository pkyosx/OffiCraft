import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, OutsourceWorkerView } from "../api/adapter";
import { pushEscapeLayer } from "../lib/escapeLayers";
import { getChatDraft } from "../lib/chatDraftStore";

let messages: ChatMessage[] = [];
const send = vi.fn((_body: string, ..._rest: unknown[]) => Promise.resolve());
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({ messages, peerLastReadTs: 0, send }),
}));
let mobile = false;
vi.mock("../hooks/useIsMobile", () => ({ useIsMobile: () => mobile }));

function mkMember(id: string, name: string, roleName: string, kind = "staff"): Member {
  return {
    id,
    name,
    role: "assistant",
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

const mira = mkMember("m1", "Mira", "助理");
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
  it("typing @ → lists staff (not wardens) with role, and outsource with task number and title", () => {
    const { input, list, options, type } = renderChat();
    expect(list()).toBeNull();
    type("hi @");
    expect(options()).toEqual([
      "Mira助理",
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
    expect(options()).toEqual(["Mira助理"]);
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
});

describe("ChatArea mention links in message bodies", () => {
  it("known member → button opening their detail over this chat; released worker → plain label", () => {
    messages = [
      {
        id: "c-1",
        from: "m1",
        to: "owner",
        body: "問 [@Family Money Eva](/#office/member/m-eva) 和 [@外包 · O-9](/#office/worker/ow-9)",
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

  it("live outsource worker link → opens the worker detail over this chat", () => {
    messages = [
      {
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "[@外包 · O-7](/#office/worker/ow-7)",
        ts: 1000,
        attachments: [],
        replyCardId: null,
      },
    ];
    const { container } = renderChat();
    fireEvent.click(container.querySelector("button.md-mention")!);
    expect(window.location.hash).toBe("#office/chat/m1/worker/ow-7");
  });
});
