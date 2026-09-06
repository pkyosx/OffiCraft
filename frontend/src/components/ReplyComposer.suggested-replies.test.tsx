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
import {
  __resetMock,
  __setMockSuggestedRepliesReplyCard,
  __setMockSuggestedRepliesTaskMessage,
} from "../api/mock";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";
import * as shared from "../hooks/sharedServerSettings";

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
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做", "先擱著，這週不碰"]);
    const { findByTestId } = renderComposer();
    const row = await findByTestId("reply-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做", "先擱著，這週不碰"]
    );
  });

  it("reads the REPLY-CARD list, never the task-message one", async () => {
    // 🔴 The two lists are separate settings by owner ruling (chat
    // c-85c28e708b81「任務跟請示卡要是不同的參數設定」). A composer that fell back
    // to the other list would look right in every test that seeds both.
    __setMockSuggestedRepliesTaskMessage(["這是任務用的"]);
    const { queryByTestId } = renderComposer();
    await settleRealTime();
    expect(queryByTestId("reply-suggested-replies")).toBeNull();
  });

  it("still shows its own chips when the task-message list is empty", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    __setMockSuggestedRepliesTaskMessage([]);
    const { findByTestId } = renderComposer();
    const row = await findByTestId("reply-suggested-replies");
    expect([...row.querySelectorAll("button")].map((b) => b.textContent)).toEqual(
      ["收到，照這樣做"]
    );
  });

  it("puts the row AFTER the input, not above it", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
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
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));

    expect(input.value).toBe("收到，照這樣做");
    // The owner still has to press send. A delayed send is a send: wait past a
    // timer before believing nothing happened.
    await settleRealTime();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("sends the picked sentence only once the owner presses Enter, and sends it verbatim", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("收到，照這樣做", []);
  });

  it("keeps a reply already being typed, adding the sentence after it — and STILL sends nothing", async () => {
    // 🔴 THE NO-SEND HALF IS THE POINT OF THIS CASE, not a bonus. The other
    // "sends NOTHING" test starts from an empty draft, where a send would be
    // refused anyway (`canSend` is false) — so it certifies almost nothing.
    // THIS is the path an owner is actually on when a mis-tap costs a
    // decision: half a sentence typed, then a chip clicked. An independent
    // review sent a real answer through here while the whole suite stayed
    // green.
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.change(input, { target: { value: "我看過了" } });
    fireEvent.click(await findByText("收到，照這樣做"));
    expect(input.value).toBe("我看過了\n收到，照這樣做");
    await settleRealTime();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("leaves the picked sentence editable — the owner can change it before sending", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    fireEvent.change(input, { target: { value: "收到，但先等我確認" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("收到，但先等我確認", []);
  });

  it("puts the caret back in the input so typing continues where the sentence ended", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const { findByText, input } = renderComposer();
    fireEvent.click(await findByText("收到，照這樣做"));
    await waitFor(() => expect(document.activeElement).toBe(input));
  });

  // 🔴 A SETTINGS READ THAT RESOLVES THE WRONG SHAPE MUST NOT TAKE THE BOX DOWN.
  //
  // Commit 7d4e5874 fixed this damage on the CALL side — a settings read that
  // THREW unmounted the whole card, message box and half-typed draft included.
  // An independent review found the same damage one door along: a read that
  // RESOLVES an object whose lists are absent / null / a string sails past that
  // try/catch and throws during RENDER instead (`replies.length` of undefined,
  // `replies.map` of a string). The cost is identical — the owner's unsent words
  // vanish — so the assertion is not merely "no chips": it is that he can still
  // type and still send.
  it.each([
    ["absent", {}],
    ["null", { suggestedRepliesReplyCard: null }],
    ["a string", { suggestedRepliesReplyCard: "收到" }],
  ])("keeps the reply box usable when the settings read answers %s", async (_l, payload) => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(shared, "loadServerSettings").mockResolvedValue(
      payload as Awaited<ReturnType<typeof shared.loadServerSettings>>
    );
    const { queryByTestId, input } = renderComposer();
    await settleRealTime();

    expect(queryByTestId("reply-suggested-replies")).toBeNull();
    fireEvent.change(input, { target: { value: "手打的回覆" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("手打的回覆", []);
    vi.restoreAllMocks();
  });

  // 🔴 A CHIP CLICK THAT THROWS IS INVISIBLE TO EVERY OTHER GUARD IN THIS FILE.
  //
  // Measured: `fireEvent.click` on a handler that throws does NOT rethrow
  // synchronously, so the ten jsdom guards around it stay green while the chip
  // blows up in a real browser — React unmounts the tree and the draft goes with
  // it. The uncaught error IS observable on `window`'s error event, so that is
  // what this listens for. Without this case the whole click path — onPick,
  // appendSuggestion, the focus hop — is unguarded against throwing.
  //
  // ⚠️ HOW TO MUTATE THIS HONESTLY, because the obvious way lies. Putting the
  // throw at the TOP of `onPick` reddens four neighbouring cases too — but NOT
  // because they detected the throw: they fail because the visible effect never
  // happened at all (`input.value` is empty). That mutant proves the effect is
  // covered, not the blind spot. Put the throw AFTER `setDraft` and the focus
  // hop instead: then thirteen cases stay green and only this one fires, which
  // is what "this is the only guard on the click path" actually means.
  it("clicking a chip raises NOTHING, and the box still works afterwards", async () => {
    __setMockSuggestedRepliesReplyCard(["收到，照這樣做"]);
    const raised: unknown[] = [];
    const onErr = (e: ErrorEvent) => raised.push(e.error ?? e.message);
    window.addEventListener("error", onErr);
    try {
      const { findByText, input } = renderComposer();
      fireEvent.click(await findByText("收到，照這樣做"));
      await settleRealTime();
      expect(raised).toEqual([]);
      expect(input.value).toBe("收到，照這樣做");
      await act(async () => {
        fireEvent.keyDown(input, { key: "Enter" });
      });
      expect(onSend).toHaveBeenCalledWith("收到，照這樣做", []);
    } finally {
      window.removeEventListener("error", onErr);
    }
  });
});
