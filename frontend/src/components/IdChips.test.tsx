import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { CopyIdChip } from "./IdChips";

const copyTextMock = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../lib/clipboard", () => ({ copyText: copyTextMock }));

const COPY_ICON = ["rect", "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"];
const CHECK_ICON = ["M20 6 9 17l-5-5"];

function renderChip() {
  const { getByTestId, queryByTestId } = render(
    <CopyIdChip
      value="rc-83935a1b2c3d"
      text="rc-83935a"
      label="複製請示卡完整 ID rc-83935a1b2c3d"
      copiedLabel="已複製"
      className="chip"
      copiedClassName="chip-copied"
      testId="chip"
      copiedTestId="chip-copied"
    />,
  );
  const chip = getByTestId("chip");
  const icon = () =>
    Array.from(chip.querySelector("svg")!.children).map((el) => el.getAttribute("d") ?? el.tagName);
  return { chip, icon, copied: () => queryByTestId("chip-copied") };
}

beforeEach(() => {
  copyTextMock.mockReset();
  copyTextMock.mockResolvedValue(true);
});

describe("CopyIdChip", () => {
  it("leads with the copy icon, copies the full value and then shows a check and 已複製 for a moment", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { chip, icon, copied } = renderChip();
    expect(chip.firstElementChild?.tagName).toBe("svg");
    expect(icon()).toEqual(COPY_ICON);
    expect([chip.textContent, chip.getAttribute("aria-label"), chip.title]).toEqual([
      "#rc-83935a",
      "複製請示卡完整 ID rc-83935a1b2c3d",
      "複製請示卡完整 ID rc-83935a1b2c3d",
    ]);

    fireEvent.click(chip);
    await waitFor(() => expect(icon()).toEqual(CHECK_ICON));
    expect(copyTextMock).toHaveBeenCalledWith("rc-83935a1b2c3d");
    expect(chip.textContent).toBe("#rc-83935a已複製");
    expect(copied()?.getAttribute("role")).toBe("status");

    await act(async () => {
      vi.advanceTimersByTime(1700);
    });
    expect(icon()).toEqual(COPY_ICON);
    expect(copied()).toBeNull();
    vi.useRealTimers();
  });

  it("shows nothing when the clipboard refuses the write", async () => {
    copyTextMock.mockResolvedValue(false);
    const { chip, icon, copied } = renderChip();
    fireEvent.click(chip);
    await waitFor(() => expect(copyTextMock).toHaveBeenCalledTimes(1));
    await act(async () => {});
    expect(icon()).toEqual(COPY_ICON);
    expect(copied()).toBeNull();
  });
});
