// IME composition guard — ChatArea composer.
//
// Regression: Chinese/CJK input pressing Enter to CONFIRM an IME candidate was
// read as "send message" (symptom 2), and controlled onChange during an
// unfinished composition duplicated characters (symptom 1). These tests assert
// the composition gate: an Enter fired while composing (isComposing / keyCode
// 229) must NOT call send(); a normal Enter AFTER compositionend must send.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";

// Spy on the chat hook so we can assert whether a send was triggered by an
// Enter keydown, without standing up the whole api adapter.
const send = vi.fn(() => Promise.resolve());
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({ messages: [], messagesPeer: "m1", send }),
}));

const member: Member = {
  id: "m1",
  memberId: "m1",
  name: "Mira",
  role: "assistant",
  status: "online",
  lifecycle: "online",
  model: "opus",
  effort: "medium",
  kind: "assistant",
  desiredMachineId: "",
  machine: null,
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  tmuxSession: "member-m1",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

function renderChat() {
  const utils = render(
    <I18nProvider>
      <ChatArea member={member} />
    </I18nProvider>
  );
  const input = utils.container.querySelector(
    "textarea.chat__input"
  ) as HTMLTextAreaElement;
  return { ...utils, input };
}

describe("ChatArea IME composition gate", () => {
  beforeEach(() => {
    send.mockClear();
  });

  it("does NOT send when Enter fires while composing (isComposing)", () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "ni" } });
    fireEvent.compositionStart(input);
    // The Enter that confirms an IME candidate — carries isComposing / 229.
    fireEvent.keyDown(input, {
      key: "Enter",
      keyCode: 229,
      // jsdom does not stamp nativeEvent.isComposing; the keyCode 229 + our
      // composing ref cover the confirm-Enter case identically.
    });
    expect(send).not.toHaveBeenCalled();
  });

  it("does NOT send on the confirm Enter even without keyCode 229 (ref guard)", () => {
    const { input } = renderChat();
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "你好" } });
    // Some browsers stamp neither isComposing nor 229 on this keydown — the
    // component's own isComposingRef (set by compositionStart) still blocks it.
    fireEvent.keyDown(input, { key: "Enter" });
    expect(send).not.toHaveBeenCalled();
  });

  it("DOES send on Enter after compositionend (candidate confirmed)", async () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "你好" } });
    fireEvent.compositionStart(input);
    fireEvent.compositionEnd(input, { target: { value: "你好" } });
    // A fresh, non-composing Enter → real send. Flush the async submit() so the
    // post-send setDraft("") settles inside act (no test warning).
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("你好", undefined);
  });

  it("Shift+Enter never sends (newline intent)", () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "hi" } });
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(send).not.toHaveBeenCalled();
  });
});
