import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ClampedText } from "./ClampedText";

// jsdom lays nothing out: the body reports one height unit per character
// against a three-line box of 30, at a line height of 10.
function fakeLayout() {
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.dataset.testid === "clamped-text-body" ? (this.textContent ?? "").length : 0;
  });
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(function (this: HTMLElement) {
    return this.dataset.testid === "clamped-text-body" ? 30 : 0;
  });
  const real = window.getComputedStyle.bind(window);
  vi.spyOn(window, "getComputedStyle").mockImplementation((el, pseudo) =>
    (el as HTMLElement).dataset.testid === "clamped-text-body"
      ? ({ getPropertyValue: () => "", lineHeight: "10px", fontSize: "10px" } as unknown as CSSStyleDeclaration)
      : real(el, pseudo),
  );
}

function view(text: string, onLineCount?: (n: number) => void) {
  return <I18nProvider><ClampedText text={text} lines={3} onLineCount={onLineCount} /></I18nProvider>;
}

describe("ClampedText", () => {
  afterEach(() => vi.restoreAllMocks());

  it("reports the laid-out height in lines, clamped or open", () => {
    fakeLayout();
    const onLineCount = vi.fn();
    const { getByTestId } = render(view("字".repeat(120), onLineCount));
    expect(onLineCount).toHaveBeenLastCalledWith(12);
    fireEvent.click(getByTestId("clamped-text-toggle"));
    expect(onLineCount).toHaveBeenLastCalledWith(12);
  });

  it("reports no line count before the text is laid out", () => {
    const onLineCount = vi.fn();
    render(view("字".repeat(120), onLineCount));
    expect(onLineCount).not.toHaveBeenCalled();
  });

  it("drops 收起 when the open text shrinks to fit under the clamp", () => {
    fakeLayout();
    const { getByTestId, queryByTestId, rerender } = render(view("字".repeat(120)));
    fireEvent.click(getByTestId("clamped-text-toggle"));
    expect(getByTestId("clamped-text-toggle").textContent).toContain(zh.common.textShowLess);

    rerender(view("字".repeat(20)));
    expect(queryByTestId("clamped-text-toggle")).toBeNull();
    expect(getByTestId("clamped-text-body").classList.contains("clamped-text__body--clamped")).toBe(true);
  });
});
