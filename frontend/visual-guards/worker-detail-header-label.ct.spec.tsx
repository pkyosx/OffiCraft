// HOTSPOT — T-b0e3 (owner 2026-07-20 截圖): the outsource-worker detail
// header's title-row slot used to render the FULL task title/description
// sentence (a long string); it must render the SAME short task-type label the
// roster row shows instead. jsdom (WorkerDetailPanel.test.tsx) already covers
// the DOM-text assertion; this guard covers what jsdom cannot — that the
// header row still fits on the owner's actual phone width (375/390) without
// forcing a page-level horizontal scrollbar, now that a real long-string task
// is bound (the fixture's taskTitle is the ticket's own long title, kept
// deliberately unshortened so a regression back to rendering it would blow
// the row out at these widths).
//
// Mutant (documented): swap the title-row span back to `{worker.taskTitle}`
// in WorkerDetailPanel.tsx → the long sentence returns to the header → this
// overflow assertion reddens at 375/390 (the desktop width has more slack and
// may still pass, which is exactly why the mobile widths are the ones that
// matter here — owner 主要在手機上看座艙).
import { test, expect } from "@playwright/experimental-ct-react";
import { WorkerDetailPanelTaskOrderStory } from "./stories/WorkerDetailPanelTaskOrderStory";

for (const width of [375, 390]) {
  test(`width ${width}: worker-detail header renders the short type label and never overflows`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<WorkerDetailPanelTaskOrderStory />);

    const header = cmp.getByTestId("worker-detail-header-task");
    await expect(header).toBeVisible();

    // (1) the behavior this guard exists to hold: the short label, not the
    // full title/description sentence.
    await expect(header).toContainText("OffiCraft 開發");
    const headerText = (await header.textContent()) ?? "";
    expect(headerText).not.toContain("outsourceParallelCap");

    // (2) the header row itself never blows past its viewport width — measure
    // the row ELEMENT, not a parent flex/block container (T-d451 hotspot: the
    // parent's rect can stay narrow while its child overflows it).
    const headerBox = await header.boundingBox();
    expect(headerBox, "header row box").not.toBeNull();
    expect(headerBox!.x + headerBox!.width).toBeLessThanOrEqual(width + 1);

    // (3) page-level invariant: nothing widens the document past the viewport.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth
    );
    expect(overflow).toBeLessThanOrEqual(1);
  });
}
