// LAYOUT GUARD — resume-summary pointers to steps sitting on answered cards.
//
// jsdom can verify the label and mapping, but it cannot verify that a long
// pointer stays inside the task block. This guard measures the shipped
// ResumeSummaryCard + member-detail.css in Chromium at a phone and desktop
// width, under the built-in and light theme token sets.
//
// MUTANT: remove `overflow-wrap: anywhere` from the answered-card block or
// its step name, or make the block wider than its task. The phone case then
// grows the task/page sideways and the bounded-box assertions go red.
import { test, expect } from "@playwright/experimental-ct-react";
import { ResumeAnsweredCardStory } from "./stories/ResumeAnsweredCardStory";

for (const theme of ["office", "light"] as const) {
  for (const width of [390, 1280]) {
    test(`${theme} ${width}px: answered-card pointers stay inside the task`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ResumeAnsweredCardStory theme={theme} />);
      await cmp.getByTestId("mp-resume-toggle").click();

      const pointer = cmp.getByTestId("mp-resume-task-answered-card");
      const steps = cmp.getByTestId("mp-resume-answered-card-step");
      await expect(pointer).toBeVisible();
      await expect(steps).toHaveCount(1);

      const metrics = await pointer.evaluate((el) => {
        const task = el.parentElement;
        const taskRow = task?.querySelector(".mp-resume__taskrow");
        const style = getComputedStyle(el);
        const step = el.querySelector(".mp-resume__answeredcardstep");
        return {
          pointer: el.getBoundingClientRect().toJSON(),
          task: task?.getBoundingClientRect().toJSON() ?? null,
          taskRow: taskRow?.getBoundingClientRect().toJSON() ?? null,
          scrollWidth: el.scrollWidth,
          clientWidth: el.clientWidth,
          stepScrollWidth: step?.scrollWidth ?? -1,
          stepClientWidth: step?.clientWidth ?? -1,
          borderWidth: style.borderTopWidth,
          color: style.color,
          background: style.backgroundColor,
        };
      });

      expect(metrics.task, "the real task wrapper must be present").not.toBeNull();
      expect(metrics.taskRow, "the real task row must be present").not.toBeNull();
      expect(metrics.pointer.x).toBeGreaterThanOrEqual(metrics.task!.x - 1);
      expect(metrics.pointer.x + metrics.pointer.width).toBeLessThanOrEqual(
        metrics.task!.x + metrics.task!.width + 1,
      );
      expect(metrics.scrollWidth - metrics.clientWidth).toBeLessThanOrEqual(1);
      expect(metrics.stepScrollWidth - metrics.stepClientWidth).toBeLessThanOrEqual(1);
      expect(metrics.borderWidth).toBe("1px");
      expect(metrics.color).not.toBe("rgba(0, 0, 0, 0)");
      expect(metrics.background).not.toBe("rgba(0, 0, 0, 0)");

      const pageOverflow = await page.evaluate(
        () =>
          document.scrollingElement!.scrollWidth -
          document.scrollingElement!.clientWidth,
      );
      expect(pageOverflow, `[${theme}/${width}px] page must not scroll sideways`).toBeLessThanOrEqual(1);
    });
  }
}
