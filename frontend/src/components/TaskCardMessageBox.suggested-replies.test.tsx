// 建議回覆 under the task card's 傳訊息給 {executor}… box (T-122).
//
// The second of the two boxes the owner named on 2026-09-06, and NOT the same
// component as the first: the reply card uses ReplyComposer, this box is the
// composer inside TaskCard. Its own guard, because "the shared row works" says
// nothing about whether this box mounted it, put it under the input, or wired
// the click to its own draft.
//
// The forbidden behaviour is the same one: 「不要點一下就直接送出」. A message to
// an executor cannot be recalled.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TasksPage } from "./TasksPage";
import {
  __resetMock,
  __injectMockTask,
  __setMockSuggestedRepliesReplyCard,
  __setMockSuggestedRepliesTaskMessage,
} from "../api/mock";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import { api } from "../api";
import type { TaskView } from "../api/adapter";

function mkTask(over: Partial<TaskView> = {}): TaskView {
  return {
    id: "task-sugg-1",
    taskNo: "T-3001",
    title: "可傳話的",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "staff",
    executorId: "mira",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <TasksPage />
    </I18nProvider>
  );
}

/** Wait past any timer a "send it for them anyway" implementation could hide
 * behind — a debounced send is still a send, and a microtask flush cannot see
 * one. */
async function settleRealTime() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 600));
  });
}

describe("task card message box — 建議回覆", () => {
  beforeEach(() => {
    __resetMock();
    resetAllSharedSnapshots();
    window.location.hash = "";
  });
  afterEach(() => {
    vi.restoreAllMocks();
    __resetMock();
    resetAllSharedSnapshots();
  });

  it("shows the owner's configured sentences under the message input", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做", "先擱著，這週不碰"]);
    __injectMockTask(mkTask());
    const { findByTestId } = renderPage();
    const row = await findByTestId("task-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做", "先擱著，這週不碰"]
    );
  });

  it("reads the TASK-MESSAGE list, never the reply-card one", async () => {
    // 🔴 The two lists are separate settings by owner ruling (chat
    // c-85c28e708b81「任務跟請示卡要是不同的參數設定」). A box that fell back to the
    // other list would look right in every test that seeds both.
    __setMockSuggestedRepliesReplyCard(["這是請示卡用的"]);
    __injectMockTask(mkTask());
    const { queryByTestId } = renderPage();
    await settleRealTime();
    expect(queryByTestId("task-suggested-replies")).toBeNull();
  });

  it("still shows its own chips when the reply-card list is empty", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __setMockSuggestedRepliesReplyCard([]);
    __injectMockTask(mkTask());
    const { findByTestId } = renderPage();
    const row = await findByTestId("task-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做"]
    );
  });

  it("puts the row AFTER the input, not above it", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const { findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    const row = await findByTestId("task-suggested-replies");
    // Node.DOCUMENT_POSITION_FOLLOWING — the row comes later in the document
    // than the box it belongs to. 「在下面」 is the owner's whole request.
    expect(input.compareDocumentPosition(row) & 4).toBeTruthy();
  });

  it("shows nothing, and leaves the message box usable, when none are configured", async () => {
    __injectMockTask(mkTask());
    const spy = vi.spyOn(api, "postTaskMessage");
    const { queryByTestId, findByTestId } = renderPage();
    const input = await findByTestId("task-msg-input");
    await settleRealTime();
    expect(queryByTestId("task-suggested-replies")).toBeNull();

    fireEvent.change(input, { target: { value: "手打的訊息" } });
    fireEvent.click(await findByTestId("task-msg-send"));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    expect(spy.mock.calls[0][1].body).toBe("手打的訊息");
  });

  it("puts the clicked sentence in the input and sends NOTHING", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const spy = vi.spyOn(api, "postTaskMessage");
    const { findByText, findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.click(await findByText("收到，照這樣做"));

    expect(input.value).toBe("收到，照這樣做");
    await settleRealTime();
    expect(spy).not.toHaveBeenCalled();
  });

  it("sends the picked sentence only once the owner presses send, and sends it verbatim", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const spy = vi.spyOn(api, "postTaskMessage");
    const { findByText, findByTestId } = renderPage();
    await findByTestId("task-msg-input");
    fireEvent.click(await findByText("收到，照這樣做"));
    fireEvent.click(await findByTestId("task-msg-send"));

    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    expect(spy.mock.calls[0][0]).toBe("task-sugg-1");
    expect(spy.mock.calls[0][1].body).toBe("收到，照這樣做");
  });

  it("keeps a message already being typed, adding the sentence after it — and STILL sends nothing", async () => {
    // 🔴 THE NO-SEND HALF IS THE POINT OF THIS CASE. See the twin comment in
    // ReplyComposer.suggested-replies.test.tsx: the empty-draft "sends
    // NOTHING" test is refused by `canSend` before it proves anything, and an
    // independent review posted a real message to the executor through THIS
    // path while the whole suite stayed green.
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const spy = vi.spyOn(api, "postTaskMessage");
    const { findByText, findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "我看過了" } });
    fireEvent.click(await findByText("收到，照這樣做"));
    expect(input.value).toBe("我看過了\n收到，照這樣做");
    await settleRealTime();
    expect(spy).not.toHaveBeenCalled();
  });

  // 🔴 THE SECOND MOUNT POINT'S CLICK PATH IS ITS OWN CODE, SO IT NEEDS ITS OWN
  // GUARD. Measured: `fireEvent.click` does not rethrow a handler's error, so a
  // chip that blows up here leaves every other case in this file green while the
  // real browser unmounts the card and takes the half-typed message with it. A
  // throw placed AFTER the visible effect is invisible to all of them — this is
  // the only case that sees it.
  it("clicking a chip raises NOTHING, and the box still works afterwards", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const raised: unknown[] = [];
    const onErr = (e: ErrorEvent) => raised.push(e.error ?? e.message);
    window.addEventListener("error", onErr);
    try {
      const { findByText, findByTestId } = renderPage();
      const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
      fireEvent.click(await findByText("收到，照這樣做"));
      await settleRealTime();
      expect(raised).toEqual([]);
      expect(input.value).toBe("收到，照這樣做");
    } finally {
      window.removeEventListener("error", onErr);
    }
  });

  it("offers no sentences on a task with no executor, where the box itself is inert", async () => {
    __setMockSuggestedRepliesTaskMessage(["收到，照這樣做"]);
    __injectMockTask(
      mkTask({ id: "task-sugg-2", executorKind: "outsource", executorId: "" })
    );
    const { queryByTestId, findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    expect(input.disabled).toBe(true);
    await settleRealTime();
    expect(queryByTestId("task-suggested-replies")).toBeNull();
  });
});
