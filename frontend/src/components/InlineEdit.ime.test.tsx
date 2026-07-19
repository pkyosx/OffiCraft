// IME composition guard — InlineEdit.
//
// Regression: pressing Enter to CONFIRM an IME candidate committed the inline
// edit (symptom 2) instead of just confirming the CJK candidate. These tests
// assert the composition gate: an Enter while composing must NOT call onCommit;
// an Enter after compositionend commits normally.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { InlineEdit } from "./InlineEdit";

const onCommit = vi.fn();

function renderEditing(value = "old") {
  const utils = render(
    <I18nProvider>
      <InlineEdit value={value} onCommit={onCommit} ariaLabel="rename" />
    </I18nProvider>
  );
  // Enter edit mode via the pencil button.
  fireEvent.click(utils.getByLabelText("rename"));
  const input = utils.container.querySelector(
    "input.inline-edit__input"
  ) as HTMLInputElement;
  return { ...utils, input };
}

describe("InlineEdit IME composition gate", () => {
  beforeEach(() => {
    onCommit.mockClear();
  });

  it("does NOT commit when Enter fires while composing (keyCode 229)", () => {
    const { input } = renderEditing();
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "新名字" } });
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    expect(onCommit).not.toHaveBeenCalled();
  });

  it("does NOT commit on the confirm Enter (ref guard, no 229)", () => {
    const { input } = renderEditing();
    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "新名字" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onCommit).not.toHaveBeenCalled();
  });

  it("DOES commit on Enter after compositionend", () => {
    const { input } = renderEditing("old");
    fireEvent.compositionStart(input);
    fireEvent.compositionEnd(input, { target: { value: "新名字" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenCalledWith("新名字");
  });
});
