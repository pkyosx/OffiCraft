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
  __setMockSuggestedReplies,
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
    executorKind: "member",
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
    __setMockSuggestedReplies(["收到，照這樣做", "先擱著，這週不碰"]);
    __injectMockTask(mkTask());
    const { findByTestId } = renderPage();
    const row = await findByTestId("task-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做", "先擱著，這週不碰"]
    );
  });

  it("puts the row AFTER the input, not above it", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
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
    __setMockSuggestedReplies(["收到，照這樣做"]);
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
    __setMockSuggestedReplies(["收到，照這樣做"]);
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

  it("keeps a message already being typed, adding the sentence after it", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    __injectMockTask(mkTask());
    const { findByText, findByTestId } = renderPage();
    const input = (await findByTestId("task-msg-input")) as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "我看過了" } });
    fireEvent.click(await findByText("收到，照這樣做"));
    expect(input.value).toBe("我看過了\n收到，照這樣做");
  });

  it("offers no sentences on a task with no executor, where the box itself is inert", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
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
