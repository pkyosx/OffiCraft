// Multi-line composer — ReplyComposer (the reply card's typed-reply input,
// shared by RepliesPage and the in-chat ChatReplyCard).
//
// Bug report (2026-07-13): the reply input was a single-line <input> — no line
// breaks (Shift+Enter dead) and a long reply mostly invisible. Same fix as the
// chat composer: a <textarea> where Enter submits and Shift+Enter breaks a
// line. These tests pin that contract on the SHARED component so both reply
// surfaces (等我回覆 page + in-chat card) are covered at once.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ReplyComposer } from "./ReplyComposer";

const onSend = vi.fn(() => Promise.resolve());

function renderComposer() {
  const utils = render(
    <I18nProvider>
      <ReplyComposer placeholder="輸入回覆…" onSend={onSend} />
    </I18nProvider>,
  );
  const input = utils.container.querySelector(
    ".chat__input",
  ) as HTMLTextAreaElement;
  return { ...utils, input };
}

describe("ReplyComposer multi-line input", () => {
  beforeEach(() => {
    onSend.mockClear();
  });

  it("the reply input is a TEXTAREA (multi-line capable), not a single-line input", () => {
    const { input } = renderComposer();
    expect(input).toBeTruthy();
    expect(input.tagName).toBe("TEXTAREA");
  });

  it("bare Enter submits the typed reply", async () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "my answer" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledTimes(1);
    expect(onSend).toHaveBeenCalledWith("my answer", []);
  });

  it("Shift+Enter does NOT submit and is NOT prevented (native newline goes through)", () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "line one" } });
    const notPrevented = fireEvent.keyDown(input, {
      key: "Enter",
      shiftKey: true,
    });
    expect(notPrevented).toBe(true);
    expect(onSend).not.toHaveBeenCalled();
  });

  it("a multi-line reply is sent verbatim — line breaks preserved", async () => {
    const { input } = renderComposer();
    fireEvent.change(input, { target: { value: "第一行\n第二行" } });
    await act(async () => {
      fireEvent.keyDown(input, { key: "Enter" });
    });
    expect(onSend).toHaveBeenCalledWith("第一行\n第二行", []);
  });
});
