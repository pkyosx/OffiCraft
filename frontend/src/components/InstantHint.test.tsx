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
    const trigger = renderHint("seth-m1 未登入 Claude\nMac Mini 未登入 Codex");
    fireEvent.mouseEnter(trigger);
    expect(lines(screen.getByRole("tooltip"))).toEqual([
      "seth-m1 未登入 Claude",
      "Mac Mini 未登入 Codex",
    ]);
  });

  it("under a page scroll while shown, the hint closes instead of floating away from its trigger", () => {
    const trigger = renderHint("Claude 未登入");
    fireEvent.mouseEnter(trigger);
    fireEvent.scroll(window);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("under a scroll inside an inner scrolling box while shown, the hint closes too (scroll does not bubble, so only a capturing listener hears it)", () => {
    render(
      <div data-testid="scroller" style={{ overflow: "auto" }}>
        <InstantHint hint="Claude 未登入" data-testid="trigger">
          !
        </InstantHint>
      </div>,
    );
    fireEvent.mouseEnter(screen.getByTestId("trigger"));
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent.scroll(screen.getByTestId("scroller"));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("under a window resize while shown, the hint closes", () => {
    const trigger = renderHint("Claude 未登入");
    fireEvent.mouseEnter(trigger);
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent(window, new Event("resize"));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  function renderPair() {
    render(
      <>
        <InstantHint hint="甲" data-testid="a">!</InstantHint>
        <InstantHint hint="乙" data-testid="b">!</InstantHint>
      </>,
    );
    return { a: screen.getByTestId("a"), b: screen.getByTestId("b") };
  }
  const shown = () => screen.queryAllByRole("tooltip").map((h) => h.textContent);

  it("under a hover on a second trigger while the first is focused, only the second's hint shows and only it is pointed at, by its own id", () => {
    const { a, b } = renderPair();
    fireEvent.focus(a);
    const aId = a.getAttribute("aria-describedby");
    expect(shown()).toEqual(["甲"]);
    fireEvent.mouseEnter(b);
    expect(shown()).toEqual(["乙"]);
    expect(a.hasAttribute("aria-describedby")).toBe(false);
    const bId = b.getAttribute("aria-describedby");
    expect(bId).not.toBe(aId);
    expect(document.getElementById(bId!)?.textContent).toBe("乙");
  });

  it("under a hover or focus on another trigger while one is pinned, the pinned hint closes and only the new one shows, in either direction", () => {
    const { a, b } = renderPair();
    fireEvent.click(a);
    fireEvent.mouseEnter(b);
    expect(shown()).toEqual(["乙"]);
    fireEvent.mouseLeave(b);
    expect(shown()).toEqual([]);

    fireEvent.click(b);
    fireEvent.focus(a);
    expect(shown()).toEqual(["甲"]);
  });

  it("under Enter or Space on the focused trigger, the key does not reach an enclosing handler, while Tab does", () => {
    const parentKeys: string[] = [];
    render(
      <div onKeyDown={(e) => parentKeys.push(e.key)}>
        <InstantHint hint="Claude 未登入" data-testid="trigger">
          !
        </InstantHint>
      </div>,
    );
    const trigger = screen.getByTestId("trigger");
    fireEvent.focus(trigger);
    fireEvent.keyDown(trigger, { key: "Enter" });
    fireEvent.keyDown(trigger, { key: " " });
    fireEvent.keyDown(trigger, { key: "Tab" });
    expect(parentKeys).toEqual(["Tab"]);
  });

  // jsdom's viewport is 1024 x 768.
  it("under room below the trigger, the hint sits 6px under it, left-aligned with it", () => {
    stubRects({ top: 100, bottom: 116, left: 50 }, { width: 200, height: 40 });
    fireEvent.mouseEnter(renderHint("Claude 未登入"));
    const hint = screen.getByRole("tooltip");
    expect([hint.style.top, hint.style.left, hint.style.visibility]).toEqual(["122px", "50px", ""]);
  });

  it("under a trigger padded for a larger tap area, the hint is placed against the content, not the padding", () => {
    stubRects({ top: 97, bottom: 130, left: 46 }, { width: 200, height: 40 });
    render(
      <InstantHint hint="Claude 未登入" data-testid="trigger" style={{ padding: "3px 6px 14px 4px" }}>
        !
      </InstantHint>,
    );
    fireEvent.mouseEnter(screen.getByTestId("trigger"));
    const hint = screen.getByRole("tooltip");
    expect([hint.style.top, hint.style.left]).toEqual(["122px", "50px"]);
  });

  it("under a padded trigger with no room below, the hint sits 6px above the content", () => {
    stubRects({ top: 737, bottom: 770, left: 46 }, { width: 200, height: 40 });
    render(
      <InstantHint hint="Claude 未登入" data-testid="trigger" style={{ padding: "3px 6px 14px 4px" }}>
        !
      </InstantHint>,
    );
    fireEvent.mouseEnter(screen.getByTestId("trigger"));
    expect(screen.getByRole("tooltip").style.top).toBe("694px");
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
