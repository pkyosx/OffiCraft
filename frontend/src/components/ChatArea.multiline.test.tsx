// Multi-line composer — ChatArea.
//
// Bug report (2026-07-13): the chat composer was a single-line <input>, so a
// reply could never contain a line break (Shift+Enter did nothing) and a long
// draft was mostly invisible (the input scrolled horizontally with no way to
// see the rest). The composer is now a <textarea> (Enter sends, Shift+Enter
// breaks a line, height follows the draft). These tests pin the contract:
//   • the composer IS a textarea (multi-line capable, vertical growth);
//   • a bare Enter submits (preventDefault — no stray newline in the sent body);
//   • Shift+Enter does NOT submit and is NOT prevented (the textarea's native
//     newline insertion must go through);
//   • a draft containing newlines is sent verbatim (line breaks preserved).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";

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
    </I18nProvider>,
  );
  const input = utils.container.querySelector(
    ".chat__input",
  ) as HTMLTextAreaElement;
  return { ...utils, input };
}

describe("ChatArea multi-line composer", () => {
  beforeEach(() => {
    send.mockClear();
  });

  it("the composer is a TEXTAREA (multi-line capable), not a single-line input", () => {
    const { input } = renderChat();
    expect(input).toBeTruthy();
    expect(input.tagName).toBe("TEXTAREA");
  });

  it("bare Enter submits and is preventDefault'ed (no newline leaks into the send)", async () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "hello" } });
    let prevented = false;
    await act(async () => {
      prevented = !fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(prevented).toBe(true);
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith("hello", undefined);
  });

  it("Shift+Enter does NOT submit and is NOT prevented (native newline goes through)", () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "line one" } });
    // fireEvent returns false when a handler called preventDefault.
    const notPrevented = fireEvent.keyDown(input, {
      key: "Enter",
      shiftKey: true,
    });
    expect(notPrevented).toBe(true);
    expect(send).not.toHaveBeenCalled();
  });

  it("a multi-line draft is sent verbatim — line breaks preserved", async () => {
    const { input } = renderChat();
    fireEvent.change(input, { target: { value: "第一行\n第二行" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(send).toHaveBeenCalledWith("第一行\n第二行", undefined);
  });
});
