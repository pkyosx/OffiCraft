// The 任務頁 summary line's 已逾期 / 即將到期 jump, in a real browser: jsdom has
// no layout, so where the lane lands is only measurable here. `.tasks` is the
// scrollport (lib/scrollPort.ts); the document never scrolls.
import { test, expect } from "@playwright/experimental-ct-react";
import { TasksDueSummaryStory } from "./stories/TasksDueSummaryStory";

for (const { name, w, h } of [
  { name: "1280×800 desktop", w: 1280, h: 800 },
  { name: "390×844 phone", w: 390, h: 844 },
]) {
  test(`${name}: 即將到期 in the summary brings its opened lane's head to the top of the list`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    const cmp = await mount(<TasksDueSummaryStory dueSoon={10} />);
    const jump = cmp.getByRole("button", { name: "即將到期 10", exact: true });
    await expect(jump).toBeVisible();
    await expect(cmp.getByTestId("tasks-summary")).toHaveText("已逾期 6 · 即將到期 10 · 進行中 22");
    const head = cmp.getByTestId("lane-toggle-soon");
    await expect(head).toHaveAttribute("aria-expanded", "false");
    const before = await head.evaluate((el) => {
      const port = el.closest(".tasks")!;
      return {
        offset: el.getBoundingClientRect().top - port.getBoundingClientRect().top,
        portHeight: port.clientHeight,
      };
    });
    expect(before.offset, "the lane must start below the fold").toBeGreaterThan(before.portHeight);

    await jump.click();

    await expect(head).toHaveAttribute("aria-expanded", "true");
    const after = await head.evaluate((el) => {
      const port = el.closest(".tasks")!;
      return {
        offset: el.getBoundingClientRect().top - port.getBoundingClientRect().top,
        scrollTop: port.scrollTop,
        docScroll: document.scrollingElement!.scrollTop,
      };
    });
    expect(after.scrollTop).toBeGreaterThan(0);
    expect(Math.abs(after.offset)).toBeLessThanOrEqual(1);
    expect(after.docScroll).toBe(0);
    await expect(cmp.getByText("ds 任務 1", { exact: true })).toBeInViewport();
  });
}
