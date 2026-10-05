// InlineEdit opened by a host (the machine name's 改名稱 menu item): it mounts
// already editing and reports every close exactly once, since the host
// unmounts it on onClose and would otherwise leave the pencil-only display.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { InlineEdit } from "./InlineEdit";

function renderOpen(value = "old") {
  const onCommit = vi.fn();
  const onClose = vi.fn();
  render(
    <I18nProvider>
      <InlineEdit value={value} onCommit={onCommit} onClose={onClose} ariaLabel="rename" openOnMount />
    </I18nProvider>
  );
  const input = screen.getByRole("textbox", { name: "rename" }) as HTMLInputElement;
  return { onCommit, onClose, input };
}

describe("InlineEdit openOnMount / onClose", () => {
  it("mounts editing: the field is there, focused, holding the value, with no pencil", () => {
    const { input, onClose } = renderOpen("old");
    expect(input.value).toBe("old");
    expect(document.activeElement).toBe(input);
    expect(screen.queryByRole("button", { name: "rename" })).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("without openOnMount it mounts showing the value and the pencil", () => {
    render(
      <I18nProvider>
        <InlineEdit value="old" onCommit={vi.fn()} ariaLabel="rename" />
      </I18nProvider>
    );
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByRole("button", { name: "rename" })).toBeTruthy();
  });

  it("applying a change commits it and closes once", () => {
    const { input, onCommit, onClose } = renderOpen("old");
    fireEvent.change(input, { target: { value: "new" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: "套用" }));
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenCalledWith("new");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("Enter with a change commits it and closes once", () => {
    const { input, onCommit, onClose } = renderOpen("old");
    fireEvent.change(input, { target: { value: "new" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onCommit).toHaveBeenCalledWith("new");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("applying an unchanged value closes once without committing", () => {
    const { onCommit, onClose } = renderOpen("old");
    fireEvent.mouseDown(screen.getByRole("button", { name: "套用" }));
    expect(onCommit).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("cancelling (button or Escape) closes once without committing", () => {
    const a = renderOpen("old");
    fireEvent.change(a.input, { target: { value: "new" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: "取消" }));
    expect(a.onCommit).not.toHaveBeenCalled();
    expect(a.onClose).toHaveBeenCalledTimes(1);
    document.body.innerHTML = "";

    const b = renderOpen("old");
    fireEvent.keyDown(b.input, { key: "Escape" });
    expect(b.onCommit).not.toHaveBeenCalled();
    expect(b.onClose).toHaveBeenCalledTimes(1);
  });
});
