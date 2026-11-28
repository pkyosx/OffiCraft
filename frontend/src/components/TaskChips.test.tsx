import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { TaskIdChip } from "./TaskChips";

describe("TaskIdChip", () => {
  it("leads the number with the jump icon and jumps without the click reaching the card around it", () => {
    const onOpen = vi.fn();
    const onCard = vi.fn();
    const { getByTestId } = render(
      <div onClick={onCard}>
        <TaskIdChip taskNo="T-812" label="查看任務" onOpen={onOpen} testId="chip" />
      </div>,
    );
    const chip = getByTestId("chip");
    expect(
      Array.from(chip.childNodes).map((n) =>
        n.nodeName === "svg" ? (n as Element).querySelector("polyline")?.getAttribute("points") : n.textContent,
      ),
    ).toEqual(["15 3 21 3 21 9", "#T-812"]);
    expect([chip.getAttribute("aria-label"), chip.title]).toEqual(["查看任務", "查看任務"]);
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onCard).not.toHaveBeenCalled();
  });
});
