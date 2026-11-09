// ChatArea 「刪除」 — the owner recalls one of their own ordinary messages sent
// less than 24 hours ago. The entry sits in the bubble's corner, asks first,
// and the row then reads 「此訊息已刪除」, which opens the original in the
// 看原訊息 overlay.
// A quote of a recalled message says 「已刪除的訊息」 and carries no text.
//
// The thread comes from a mocked useChat (as in the other ChatArea tests); the
// recall call goes through the real api seam, spied.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, within, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { codeForStatus } from "../api/errorCodes";
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
  terminalAttachCommand: "tmux -L officraft attach -t member-m1",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

const NOW = Date.now() / 1000;
const HOUR = 60 * 60;

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "owner",
    to: "m1",
    body: "",
    ts: NOW - 60,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    replyTo: null,
    replyToChat: null,
    recalledTs: 0,
    taskId: null,
    ...over,
  };
}

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea key={member.id} member={member} />
    </I18nProvider>,
  );
}

const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;
const recallButtonIn = (row: HTMLElement) =>
  within(row).queryByRole("button", { name: "刪除" });
const recalledOpenIn = (row: HTMLElement) =>
  row.querySelector(".chat__msg-recalled__open") as HTMLButtonElement | null;

describe("ChatArea 刪除", () => {
  beforeEach(() => {
    resetChatDrafts();
    Element.prototype.scrollIntoView = () => {};
    messages = [];
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("offers 刪除 only on the owner's own ordinary messages younger than 24 hours", () => {
    messages = [
      mkMsg({ id: "own-fresh", body: "剛送出的訊息" }),
      mkMsg({ id: "own-23h", body: "快一天前", ts: NOW - 23 * HOUR }),
      mkMsg({ id: "own-25h", body: "一天多前", ts: NOW - 25 * HOUR }),
      mkMsg({ id: "theirs", from: "m1", to: "owner", body: "對方的訊息" }),
      mkMsg({ id: "task", body: "[TaskID=T-1] 任務訊息", taskId: "T-1" }),
      mkMsg({ id: "card", body: "帶卡的訊息", replyCardId: "rc-1" }),
      mkMsg({ id: "done", body: "刪除過的", recalledTs: NOW - 30 }),
    ];
    const { container } = renderChat();
    const offered = messages
      .filter((m) => recallButtonIn(rowOf(container, m.id)) !== null)
      .map((m) => m.id);
    expect(offered).toEqual(["own-fresh", "own-23h"]);
    // The corner reserves room for both controls on a bubble that carries it.
    expect(
      rowOf(container, "own-fresh").querySelector(".chat__msg-bubble")!.className,
    ).toBe("chat__msg-bubble chat__msg-bubble--acts2");
    expect(
      rowOf(container, "own-25h").querySelector(".chat__msg-bubble")!.className,
    ).toBe("chat__msg-bubble chat__msg-bubble--acts1");
  });

  it("asks first, recalls by id on confirm, and the row turns into 「此訊息已刪除」", async () => {
    messages = [mkMsg({ id: "c-9", body: "打錯字的訊息" })];
    const recall = vi
      .spyOn(api, "recallChatMessage")
      .mockResolvedValue({ ...messages[0], recalledTs: NOW });
    const { container, getByTestId, queryByTestId } = renderChat();

    fireEvent.click(recallButtonIn(rowOf(container, "c-9"))!);
    const dialog = getByTestId("chat-recall-confirm");
    expect(dialog.querySelector(".confirm-modal__body")!.textContent).toBe(
      "刪除這則訊息？對方會收到刪除通知，原文你仍可點開查看。此動作無法復原。",
    );
    expect(recall).not.toHaveBeenCalled();

    fireEvent.click(getByTestId("chat-recall-confirm-btn"));
    await waitFor(() => expect(queryByTestId("chat-recall-confirm")).toBeNull());
    expect(recall.mock.calls).toEqual([["c-9"]]);

    const row = rowOf(container, "c-9");
    expect(recalledOpenIn(row)!.textContent).toBe("此訊息已刪除");
    expect(recalledOpenIn(row)!.getAttribute("aria-haspopup")).toBe("dialog");
    expect(row.textContent).not.toContain("打錯字的訊息");
    expect(row.querySelector(".chat__msg-bubble")).toBeNull();
    expect(recallButtonIn(row)).toBeNull();
  });

  it("cancel closes the dialog without calling the server", () => {
    messages = [mkMsg({ id: "c-9", body: "留著" })];
    const recall = vi.spyOn(api, "recallChatMessage");
    const { container, getByTestId, queryByTestId } = renderChat();
    fireEvent.click(recallButtonIn(rowOf(container, "c-9"))!);
    fireEvent.click(within(getByTestId("chat-recall-confirm")).getByText("取消"));
    expect(queryByTestId("chat-recall-confirm")).toBeNull();
    expect(recall).not.toHaveBeenCalled();
    expect(rowOf(container, "c-9").textContent).toContain("留著");
  });

  it("a recalled row keeps its send time, drops 已讀, shows none of the original, and opens it in the 看原訊息 overlay", async () => {
    messages = [
      mkMsg({
        id: "r-1",
        body: "原本的內容",
        recalledTs: NOW - 10,
        attachments: [
          { id: "a1", url: "/x", filename: "p.pdf", mime: "application/pdf", isImage: false },
        ],
      }),
      mkMsg({ id: "plain", body: "普通的一則" }),
    ];
    const get = vi
      .spyOn(api, "getChatMessage")
      .mockResolvedValue({ ...messages[0], body: "原本的內容，讀回來的完整版" });
    const { container } = renderChat();
    const row = rowOf(container, "r-1");
    const control = rowOf(container, "plain");

    expect(row.className).toBe("chat__msg chat__msg--me");
    expect(row.querySelector(".chat__msg-time")).not.toBeNull();
    expect(row.querySelector(".chat__msg-read")).toBeNull();
    expect(row.textContent).not.toContain("原本的內容");
    expect(row.textContent).not.toContain("p.pdf");
    expect(control.textContent).toContain("普通的一則");
    expect(document.querySelector(".md-preview")).toBeNull();

    await act(async () => {
      fireEvent.click(recalledOpenIn(row)!);
    });
    expect(get.mock.calls).toEqual([["r-1"]]);
    const overlay = document.querySelector(".md-preview")!;
    expect(overlay).toBeTruthy();
    expect(overlay.textContent).toContain("原本的內容，讀回來的完整版");
    // The thread row stays the placeholder while the overlay is open.
    expect(row.textContent).not.toContain("原本的內容");

    fireEvent.click(overlay.querySelector(".md-preview__close")!);
    expect(document.querySelector(".md-preview")).toBeNull();
    expect(recalledOpenIn(rowOf(container, "r-1"))!.textContent).toBe("此訊息已刪除");
    expect(rowOf(container, "r-1").textContent).not.toContain("原本的內容");
  });

  it("an attachment-only deleted message opens with its attachment and no text block", async () => {
    messages = [
      mkMsg({
        id: "r-2",
        recalledTs: NOW - 10,
        attachments: [
          { id: "att-img", url: "/api/chat/attachment/att-img", filename: "shot.png", mime: "image/png", isImage: true },
        ],
      }),
    ];
    vi.spyOn(api, "getChatMessage").mockResolvedValue(messages[0]);
    const { container } = renderChat();
    expect(rowOf(container, "r-2").querySelector("img")).toBeNull();

    await act(async () => {
      fireEvent.click(recalledOpenIn(rowOf(container, "r-2"))!);
    });
    const overlay = document.querySelector(".md-preview")!;
    expect(overlay.querySelectorAll(".chat__msg-attachments img.chat__msg-image")).toHaveLength(1);
    expect(overlay.querySelector(".md-preview__md")).toBeNull();
  });

  it("says the overlay's own failure sentence beside the recalled row when the read fails", async () => {
    messages = [mkMsg({ id: "r-1", body: "原本的內容", recalledTs: NOW - 10 })];
    vi.spyOn(api, "getChatMessage").mockRejectedValue(new Error("offline"));
    const { container } = renderChat();
    await act(async () => {
      fireEvent.click(recalledOpenIn(rowOf(container, "r-1"))!);
    });
    expect(document.querySelector(".md-preview")).toBeNull();
    expect(
      rowOf(container, "r-1").querySelector("[data-testid='msg-quote-error']"),
    ).not.toBeNull();
  });

  it("a recall made in another owner tab turns a held row deleted from its delta alone, with no request", async () => {
    const sinks: Array<(topic: string, delta?: unknown) => void> = [];
    vi.spyOn(api, "subscribeEvents").mockImplementation((fn) => {
      sinks.push(fn as (topic: string, delta?: unknown) => void);
      return () => {};
    });
    messages = [
      mkMsg({ id: "old-1", body: "很早送出的那則", ts: NOW - 3 * HOUR }),
      mkMsg({ id: "old-2", body: "另一則", ts: NOW - 2 * HOUR }),
    ];
    const get = vi.spyOn(api, "getChatMessage");
    const { container } = renderChat();

    const emit = (names: Record<string, string>, recalledTs?: number) =>
      act(async () => {
        for (const sink of sinks)
          sink("chat", { topic: "chat", names, ids: Object.values(names), ...(recalledTs ? { recalledTs } : {}) });
        await Promise.resolve();
      });
    await emit({ id: "old-2", from: "owner", to: "m1" });
    expect(rowOf(container, "old-2").textContent).toContain("另一則");

    await emit({ id: "old-1", from: "owner", to: "m1" }, NOW - 5);
    expect(recalledOpenIn(rowOf(container, "old-1"))).not.toBeNull();
    expect(rowOf(container, "old-1").textContent).not.toContain("很早送出的那則");
    expect(rowOf(container, "old-2").textContent).toContain("另一則");
    expect(get).not.toHaveBeenCalled();
  });

  it("a quote of a recalled message says 「已刪除的訊息」 and carries no text", () => {
    messages = [
      mkMsg({
        id: "q-recalled",
        from: "m1",
        to: "owner",
        body: "回你剛剛那句",
        replyTo: "gone-1",
        replyToChat: {
          id: "gone-1",
          from: "owner",
          fromName: "",
          to: "m1",
          toName: "",
          content: "",
          recalled: true,
        },
      }),
      mkMsg({
        id: "q-plain",
        from: "m1",
        to: "owner",
        body: "回另一句",
        replyTo: "kept-1",
        replyToChat: {
          id: "kept-1",
          from: "owner",
          fromName: "",
          to: "m1",
          toName: "",
          content: "還在的那句",
          recalled: false,
        },
      }),
    ];
    const { container } = renderChat();
    const bodyOf = (id: string) =>
      rowOf(container, id).querySelector(".chat__msg-quote__body")!.textContent;
    expect(bodyOf("q-recalled")).toBe("已刪除的訊息");
    expect(bodyOf("q-plain")).toBe("還在的那句");
  });

  it("says the server's reason in the dialog when the recall is refused, and leaves the message alone", async () => {
    messages = [mkMsg({ id: "c-9", body: "還在" })];
    const reason =
      "chat message 'c-9' was sent more than 24 hours ago — only messages younger than 24 hours can be recalled";
    const recall = vi
      .spyOn(api, "recallChatMessage")
      .mockRejectedValue(
        new ApiError("http 409 for POST /api/chat/c-9/recall", 409, codeForStatus(409), reason),
      );
    const { container, getByTestId } = renderChat();
    fireEvent.click(recallButtonIn(rowOf(container, "c-9"))!);
    fireEvent.click(getByTestId("chat-recall-confirm-btn"));
    await waitFor(() =>
      expect(
        getByTestId("chat-recall-confirm").querySelector(".confirm-modal__error")?.textContent,
      ).toBe(reason),
    );
    expect(recall.mock.calls).toEqual([["c-9"]]);
    expect(rowOf(container, "c-9").textContent).toContain("還在");
    expect(recalledOpenIn(rowOf(container, "c-9"))).toBeNull();
  });

  it("falls back to its own sentence when the failure carries no server reason", async () => {
    messages = [mkMsg({ id: "c-9", body: "還在" })];
    vi.spyOn(api, "recallChatMessage").mockRejectedValue(new Error("offline"));
    const { container, getByTestId } = renderChat();
    fireEvent.click(recallButtonIn(rowOf(container, "c-9"))!);
    fireEvent.click(getByTestId("chat-recall-confirm-btn"));
    await waitFor(() =>
      expect(
        getByTestId("chat-recall-confirm").querySelector(".confirm-modal__error")?.textContent,
      ).toBe("刪除失敗，請稍後重試"),
    );
  });
});
