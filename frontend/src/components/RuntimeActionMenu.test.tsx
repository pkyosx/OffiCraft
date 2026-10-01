import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { RuntimeActionMenu } from "./RuntimeActionMenu";

afterEach(cleanup);

describe("RuntimeActionMenu", () => {
  it("under a click on the trigger, opens a menu of its items and picking one runs it and closes the menu", () => {
    const onLogin = vi.fn();
    render(
      <RuntimeActionMenu
        label="操作"
        testIdPrefix="mon-claude"
        items={[{ key: "login", label: "登入", onSelect: onLogin }]}
      />
    );
    const trigger = screen.getByTestId("mon-claude-menu");
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.click(trigger);
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const items = screen.getAllByRole("menuitem").map((el) => el.textContent);
    expect(items).toEqual(["登入"]);

    fireEvent.click(screen.getByTestId("mon-claude-menu-login"));
    expect(onLogin).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under Esc or a click outside, the open menu closes without running anything", () => {
    const onLogin = vi.fn();
    render(
      <div>
        <span data-testid="outside">x</span>
        <RuntimeActionMenu
          label="操作"
          testIdPrefix="mon-claude"
          items={[{ key: "login", label: "登入", onSelect: onLogin }]}
        />
      </div>
    );
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();

    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.mouseDown(screen.getByTestId("outside"));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(onLogin).not.toHaveBeenCalled();
  });

  it("under a host inside an overflow:auto wrapper, the menu is rendered outside it, on <body>, and focuses its first item", () => {
    render(
      <div data-testid="wrap" style={{ overflow: "auto" }}>
        <RuntimeActionMenu
          label="操作"
          testIdPrefix="mon-claude"
          items={[
            { key: "login", label: "登入", onSelect: () => {} },
            { key: "upgrade", label: "升級", onSelect: () => {} },
          ]}
        />
      </div>
    );
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    const pop = screen.getByRole("menu");
    expect(screen.getByTestId("wrap").contains(pop)).toBe(false);
    expect(pop.parentElement).toBe(document.body);
    expect(getComputedStyle(pop).visibility).not.toBe("hidden");
    expect(document.activeElement).toBe(screen.getByTestId("mon-claude-menu-login"));
    fireEvent.keyDown(pop, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-claude-menu-upgrade"));
    fireEvent.keyDown(pop, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-claude-menu-login"));
  });

  it("under Esc, the menu closes and focus returns to the trigger", () => {
    render(
      <RuntimeActionMenu label="操作" testIdPrefix="mon-claude" items={[{ key: "login", label: "登入", onSelect: () => {} }]} />
    );
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(screen.getByTestId("mon-claude-menu"));
  });

  it("under a scroll or a resize anywhere, the open menu closes", () => {
    render(
      <RuntimeActionMenu label="操作" testIdPrefix="mon-claude" items={[{ key: "login", label: "登入", onSelect: () => {} }]} />
    );
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.scroll(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent(window, new Event("resize"));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("under a mousedown inside the portalled menu, it stays open until the item's click", () => {
    const onLogin = vi.fn();
    render(
      <RuntimeActionMenu label="操作" testIdPrefix="mon-claude" items={[{ key: "login", label: "登入", onSelect: onLogin }]} />
    );
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    const item = screen.getByTestId("mon-claude-menu-login");
    fireEvent.mouseDown(item);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.click(item);
    expect(onLogin).toHaveBeenCalledTimes(1);
  });

  it("under no items, renders nothing at all", () => {
    const { container, rerender } = render(
      <RuntimeActionMenu label="操作" testIdPrefix="mon-codex" items={[{ key: "x", label: "x", onSelect: () => {} }]} />
    );
    expect(container.querySelector("[data-testid='mon-codex-menu']")).not.toBeNull();
    rerender(<RuntimeActionMenu label="操作" testIdPrefix="mon-codex" items={[]} />);
    expect(container.innerHTML).toBe("");
  });
});
