import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { RuntimeActionMenu, type RuntimeActionItem } from "./RuntimeActionMenu";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

/** jsdom lays nothing out, so the trigger and menu boxes are stubbed. */
function stubRects(trigger: { top: number; left: number; width: number; height: number }) {
  const box = { width: 80, height: 34 };
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const r = this.getAttribute("role") === "menu"
      ? { top: 0, left: 0, ...box }
      : this.getAttribute("aria-haspopup") === "menu"
        ? trigger
        : { top: 0, left: 0, width: 0, height: 0 };
    return { ...r, right: r.left + r.width, bottom: r.top + r.height, x: r.left, y: r.top, toJSON: () => r } as DOMRect;
  });
}

const LABEL = "Codex 0.159.2 操作";

function menu(items: RuntimeActionItem[]) {
  return (
    <RuntimeActionMenu label={LABEL} testIdPrefix="mon-codex" items={items}>
      <span>0.159.2</span>
      <span data-testid="chip">未登入</span>
    </RuntimeActionMenu>
  );
}

const login = (onSelect: () => void = () => {}): RuntimeActionItem => ({
  key: "login",
  label: "登入",
  icon: <svg data-testid="login-icon" />,
  onSelect,
});

const trigger = () => screen.getByRole("button", { name: LABEL });

describe("RuntimeActionMenu", () => {
  it("under items, the version and its chips are the content of one menu button named by the label", () => {
    render(menu([login()]));
    const t = trigger();
    expect(t.getAttribute("aria-haspopup")).toBe("menu");
    expect(t.getAttribute("aria-expanded")).toBe("false");
    expect(t.textContent).toBe("0.159.2未登入");
    expect(t.contains(screen.getByTestId("chip"))).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under a click on the trigger, opens a menu of its items and picking one runs it and closes the menu", () => {
    const onLogin = vi.fn();
    render(menu([login(onLogin)]));
    fireEvent.click(trigger());
    expect(trigger().getAttribute("aria-expanded")).toBe("true");
    const pop = screen.getByRole("menu", { name: LABEL });
    expect(pop.textContent).toBe("登入");
    expect(screen.getAllByRole("menuitem").map((el) => el.textContent)).toEqual(["登入"]);
    expect(screen.getByRole("menuitem").contains(screen.getByTestId("login-icon"))).toBe(true);

    fireEvent.click(screen.getByRole("menuitem", { name: "登入" }));
    expect(onLogin).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger().getAttribute("aria-expanded")).toBe("false");
  });

  it("under a second click on the trigger, the open menu closes", () => {
    render(menu([login()]));
    fireEvent.click(trigger());
    expect(trigger().getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(trigger());
    expect(trigger().getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under ArrowDown on the trigger, the menu opens with its first item focused; Esc closes it and refocuses the trigger", () => {
    render(menu([login()]));
    trigger().focus();
    fireEvent.keyDown(trigger(), { key: "ArrowDown" });
    expect(trigger().getAttribute("aria-expanded")).toBe("true");
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "登入" }));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger().getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(trigger());
  });

  it("under other keys on the closed trigger, nothing opens", () => {
    render(menu([login()]));
    fireEvent.keyDown(trigger(), { key: "ArrowUp" });
    fireEvent.keyDown(trigger(), { key: "a" });
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under Esc or a click outside, the open menu closes without running anything", () => {
    const onLogin = vi.fn();
    render(
      <div>
        <span data-testid="outside">x</span>
        {menu([login(onLogin)])}
      </div>
    );
    fireEvent.click(trigger());
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.click(trigger());
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.mouseDown(screen.getByTestId("outside"));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(onLogin).not.toHaveBeenCalled();
  });

  it("under a host inside an overflow:auto wrapper, the menu is rendered outside it, on <body>, and focuses its first item", () => {
    render(
      <div data-testid="wrap" style={{ overflow: "auto" }}>
        {menu([login(), { key: "upgrade", label: "升級", onSelect: () => {} }])}
      </div>
    );
    fireEvent.click(trigger());
    const pop = screen.getByRole("menu");
    expect(screen.getByTestId("wrap").contains(pop)).toBe(false);
    expect(pop.parentElement).toBe(document.body);
    expect(getComputedStyle(pop).visibility).not.toBe("hidden");
    expect(document.activeElement).toBe(screen.getByTestId("mon-codex-menu-login"));
    fireEvent.keyDown(pop, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-codex-menu-upgrade"));
    fireEvent.keyDown(pop, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-codex-menu-login"));
  });

  it("under a scroll or a resize anywhere, the open menu closes", () => {
    render(menu([login()]));
    fireEvent.click(trigger());
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.scroll(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(trigger());
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent(window, new Event("resize"));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under a mousedown inside the portalled menu, it stays open until the item's click", () => {
    const onLogin = vi.fn();
    render(menu([login(onLogin)]));
    fireEvent.click(trigger());
    const item = screen.getByTestId("mon-codex-menu-login");
    fireEvent.mouseDown(item);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.click(item);
    expect(onLogin).toHaveBeenCalledTimes(1);
  });

  it("under no items, the content renders as plain text with no button", () => {
    const { container, rerender } = render(menu([login()]));
    expect(screen.queryByRole("button")).not.toBeNull();
    rerender(menu([]));
    expect(screen.queryByRole("button")).toBeNull();
    expect(container.textContent).toBe("0.159.2未登入");
  });

  it("under room below, the menu sits flush under the trigger, left-aligned and at least as wide, and the pair square off where they meet", () => {
    stubRects({ top: 100, left: 200, width: 120, height: 24 });
    render(menu([login()]));
    fireEvent.click(trigger());
    const pop = screen.getByRole("menu");
    expect(pop.style.top).toBe("123px");
    expect(pop.style.left).toBe("200px");
    expect(pop.style.minWidth).toBe("120px");
    expect(pop.getAttribute("data-placement")).toBe("below");
    expect(trigger().getAttribute("data-placement")).toBe("below");
    fireEvent.keyDown(window, { key: "Escape" });
    expect(trigger().getAttribute("data-placement")).toBeNull();
  });

  it("under no room below, the menu sits flush above the trigger instead", () => {
    stubRects({ top: window.innerHeight - 30, left: 200, width: 120, height: 24 });
    render(menu([login()]));
    fireEvent.click(trigger());
    const pop = screen.getByRole("menu");
    expect(pop.style.top).toBe(`${window.innerHeight - 30 + 1 - 34}px`);
    expect(pop.getAttribute("data-placement")).toBe("above");
    expect(trigger().getAttribute("data-placement")).toBe("above");
  });
});
