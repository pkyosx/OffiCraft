import { afterEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { InstantHint } from "./InstantHint";

function renderHint(hint: string) {
  render(
    <div style={{ overflow: "hidden" }}>
      <InstantHint hint={hint} data-testid="trigger" className="mark">
        !
      </InstantHint>
    </div>,
  );
  return screen.getByTestId("trigger");
}

function stubRects(trigger: Partial<DOMRect>, hint: Partial<DOMRect>) {
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (
    this: Element,
  ) {
    const r = this.classList.contains("instant-hint") ? hint : trigger;
    return { top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0, x: 0, y: 0, ...r } as DOMRect;
  });
}

const lines = (el: HTMLElement) => Array.from(el.children).map((c) => c.textContent);

describe("InstantHint", () => {
  afterEach(() => vi.restoreAllMocks());

  it("under no hover or focus, no hint is in the document and the trigger has no native title", () => {
    const trigger = renderHint("Claude 未登入");
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(trigger.hasAttribute("title")).toBe(false);
    expect(trigger.hasAttribute("aria-describedby")).toBe(false);
  });

  it("under mouseenter, the hint shows at once in <body>, outside the clipping host, and the trigger points at it", () => {
    const trigger = renderHint("Claude 未登入");
    fireEvent.mouseEnter(trigger);
    const hint = screen.getByRole("tooltip");
    expect(lines(hint)).toEqual(["Claude 未登入"]);
    expect(hint.parentElement).toBe(document.body);
    expect(trigger.getAttribute("aria-describedby")).toBe(hint.id);
    expect(hint.className).toBe("instant-hint");
  });

  it("under mouseleave after mouseenter, the hint is gone and the trigger no longer points at it", () => {
    const trigger = renderHint("Claude 未登入");
    fireEvent.mouseEnter(trigger);
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent.mouseLeave(trigger);
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(trigger.hasAttribute("aria-describedby")).toBe(false);
  });

  it("under keyboard focus, the hint shows, and blur removes it", () => {
    const trigger = renderHint("Claude 未登入");
    expect(trigger.tabIndex).toBe(0);
    fireEvent.focus(trigger);
    expect(lines(screen.getByRole("tooltip"))).toEqual(["Claude 未登入"]);
    fireEvent.blur(trigger);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("under a hint with a newline, each part is its own line", () => {
    const trigger = renderHint("未登入 Claude\n要換到的 Mac Mini 未登入 Codex");
    fireEvent.mouseEnter(trigger);
    expect(lines(screen.getByRole("tooltip"))).toEqual([
      "未登入 Claude",
      "要換到的 Mac Mini 未登入 Codex",
    ]);
  });

  it("under a page scroll while shown, the hint closes instead of floating away from its trigger", () => {
    const trigger = renderHint("Claude 未登入");
    fireEvent.mouseEnter(trigger);
    fireEvent.scroll(window);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  // jsdom's viewport is 1024 x 768.
  it("under room below the trigger, the hint sits 6px under it, left-aligned with it", () => {
    stubRects({ top: 100, bottom: 116, left: 50 }, { width: 200, height: 40 });
    fireEvent.mouseEnter(renderHint("Claude 未登入"));
    const hint = screen.getByRole("tooltip");
    expect([hint.style.top, hint.style.left, hint.style.visibility]).toEqual(["122px", "50px", ""]);
  });

  it("under no room below the trigger, the hint sits 6px above it", () => {
    stubRects({ top: 740, bottom: 756, left: 50 }, { width: 200, height: 40 });
    fireEvent.mouseEnter(renderHint("Claude 未登入"));
    expect(screen.getByRole("tooltip").style.top).toBe("694px");
  });

  it("under a trigger near the right edge, the hint is pulled back inside the viewport with an 8px margin", () => {
    stubRects({ top: 100, bottom: 116, left: 1000 }, { width: 200, height: 40 });
    fireEvent.mouseEnter(renderHint("Claude 未登入"));
    expect(screen.getByRole("tooltip").style.left).toBe("816px");
  });
});
