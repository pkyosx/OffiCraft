// ChatArea 的 mark-read 閘門：「看過」必須真的是 owner 在看。
//
// 🔴 這是這張票的立論本身，而它一度沒有任何護欄。改動前這個性質由
// `listChat`（會標已讀）／`peekChat`（不會）二選一承載，有 5 支測試釘著；
// `peekChat` 併回 `listChat`（commit 2bb49d1f）之後，性質整個落在
// ChatArea 的一行 `if (!windowActive) return;` 上，那 5 支測試卻跟著沒了 ——
// 把那一行刪掉，前端 2570 支測試全綠。這個檔就是補回那條牙齒。
//
// 真的 ChatArea + 真的 useChat，只有 api 這一層是假的，所以斷言的是
// 「POST /api/chat/mark-read 到底有沒有送出去」，不是某個內部旗標。

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

const OWNER = "owner";
const PEER = "m-9d2f0b1a7c34";

const log: ChatMessage[] = [];
const markChatRead = vi.fn(async () => ({}));

vi.mock("../api", () => ({
  api: {
    listChat: async (withId: string) =>
      log.filter((m) => m.from === withId || m.to === withId),
    listChatWindow: async () => [],
    listChatReads: async () => [],
    markChatRead: (...args: unknown[]) =>
      (markChatRead as unknown as (...a: unknown[]) => Promise<unknown>)(
        ...args,
      ),
    postChat: async () => log[log.length - 1],
    subscribeEvents: () => () => {},
    getOutsourceWorker: async () => ({}),
  },
}));

function mkMember(unreadCount: number): Member {
  return {
    id: PEER,
    name: "Beto",
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
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount,
  };
}

function view() {
  const m = mkMember(1);
  return (
    <I18nProvider>
      <ChatArea member={m} members={[m]} workers={[]} onWake={undefined} />
    </I18nProvider>
  );
}

/** jsdom has no real focus model — drive the two halves `isWindowActive()`
 * reads (`visibilityState` + `hasFocus()`) and fire the event the hook listens
 * to, exactly as a real focus/blur would. */
function setWindowActive(active: boolean) {
  document.hasFocus = () => active;
  window.dispatchEvent(new Event(active ? "focus" : "blur"));
}

async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 30));
  });
}

beforeEach(() => {
  log.length = 0;
  markChatRead.mockClear();
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
  log.push({
    id: "c1",
    from: PEER,
    to: OWNER,
    body: "背景時到達的訊息",
    ts: 1000,
    attachments: [],
    replyCardId: null,
  });
});

describe("ChatArea — 「看過」需要 owner 真的在看", () => {
  it("視窗不在前景時，載進來的訊息不會送出 mark-read", async () => {
    document.hasFocus = () => false;
    const { container } = render(view());

    // 先確認訊息真的載進來了 —— 否則「沒送 mark-read」可能只是因為畫面是空的。
    await waitFor(() =>
      expect(container.querySelector(".chat__msg-bubble")).not.toBeNull(),
    );
    await settle();

    expect(markChatRead).not.toHaveBeenCalled();
  });

  it("回到前景之後，累積的訊息才被標成已讀，而且標到最新那一則", async () => {
    document.hasFocus = () => false;
    const { container } = render(view());
    await waitFor(() =>
      expect(container.querySelector(".chat__msg-bubble")).not.toBeNull(),
    );
    await settle();
    expect(markChatRead).not.toHaveBeenCalled();

    await act(async () => {
      setWindowActive(true);
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(markChatRead).toHaveBeenCalledWith({
      peer: PEER,
      lastReadTs: 1000,
    });
  });
});
