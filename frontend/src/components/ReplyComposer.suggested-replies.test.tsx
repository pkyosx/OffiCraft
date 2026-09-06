// 建議回覆 under the reply card's composer (T-122).
//
// owner 2026-09-06: 「我想要可以在下面有幾個建議的回覆，像一般客服聊天訊息那樣」
// —— and the one thing he named as forbidden: 「不要點一下就直接送出」. A reply
// card's answer closes the card for good, so a mis-tapped chip that SENT would
// cost a decision, not a keystroke. That is what this file holds.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ReplyComposer } from "./ReplyComposer";
import { __resetMock, __setMockSuggestedReplies } from "../api/mock";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";

const onSend = vi.fn(() => Promise.resolve());

function renderComposer() {
  const utils = render(
    <I18nProvider>
      <ReplyComposer placeholder="輸入回覆…" onSend={onSend} />
    </I18nProvider>
  );
  const input = utils.container.querySelector(
    ".chat__input"
  ) as HTMLTextAreaElement;
  return { ...utils, input };
}

/** Wait past any timer a "send it for them anyway" implementation could hide
 * behind. A microtask flush would not see one — the point of the delay is that
 * a debounced send is still a send. */
async function settleRealTime() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 600));
  });
}

describe("ReplyComposer 建議回覆", () => {
  beforeEach(() => {
    onSend.mockClear();
    __resetMock();
    resetAllSharedSnapshots();
  });
  afterEach(() => {
    __resetMock();
    resetAllSharedSnapshots();
  });

  it("shows the owner's configured sentences under the reply input", async () => {
    __setMockSuggestedReplies(["收到，照這樣做", "先擱著，這週不碰"]);
    const { findByTestId } = renderComposer();
    const row = await findByTestId("reply-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做", "先擱著，這週不碰"]
    );
  });

  it("puts the row AFTER the input, not above it", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByTestId, input } = renderComposer();
    const row = await findByTestId("reply-suggested-replies");
    // Node.DOCUMENT_POSITION_FOLLOWING — the row comes later in the document
    // than the box it belongs to. 「在下面」 is the owner's whole request.
    expect(input.compareDocumentPosition(row) & 4).toBeTruthy();
  });

  it("shows nothing, and leaves the input usable, when none are configured", async () => {
    const { queryByTestId, input } = renderComposer();
    await settleRealTime();
    expect(queryByTestId("reply-suggested-replies")).toBeNull();
    fireEvent.change(input, { target: { value: "手打的回覆" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("手打的回覆", []);
  });

  it("puts the clicked sentence in the input and sends NOTHING", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));

    expect(input.value).toBe("收到，照這樣做");
    // The owner still has to press send. A delayed send is a send: wait past a
    // timer before believing nothing happened.
    await settleRealTime();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("sends the picked sentence only once the owner presses Enter, and sends it verbatim", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("收到，照這樣做", []);
  });

  it("keeps a reply already being typed, adding the sentence after it", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.change(input, { target: { value: "我看過了" } });
    fireEvent.click(await findByText("收到，照這樣做"));
    expect(input.value).toBe("我看過了\n收到，照這樣做");
  });

  it("leaves the picked sentence editable — the owner can change it before sending", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    fireEvent.change(input, { target: { value: "收到，但先等我確認" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("收到，但先等我確認", []);
  });

  it("puts the caret back in the input so typing continues where the sentence ended", async () => {
    __setMockSuggestedReplies(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    await waitFor(() => expect(document.activeElement).toBe(input));
  });
});
