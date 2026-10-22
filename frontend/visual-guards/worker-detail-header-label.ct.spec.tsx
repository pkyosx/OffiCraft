// The outsource-worker detail identity card carries the chat header's lines:
// the codename, then the task line (task-id chip and the SHORT task-type label,
// never the title), then the FULL task title on its own line. jsdom
// (WorkerDetailPanel.test.tsx) covers the text; this guard covers what jsdom
// cannot — at the owner's phone widths and on a desktop, the long title wraps
// inside the card instead of widening the row or the page. The fixture's
// title is deliberately long and unshortened.
//
// Mutant (documented): render the title with `clamp` in WorkerDetailPanel.tsx
// → it stays on two clamped lines → the "wraps onto more than two lines"
// assertion reddens at 375/390.
import { test, expect } from "@playwright/experimental-ct-react";
import { WorkerDetailPanelIdentityStory } from "./stories/WorkerDetailPanelIdentityStory";

const TITLE =
  "拆發包 per-agent 白名單閘:發包全員可用,成本控制只靠 outsourceParallelCap(owner 2026-07-20 裁決)";

for (const width of [375, 390, 1280]) {
  test(`width ${width}: the worker's identity card shows the short type label, then the full task title wrapped inside the card, and never overflows`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<WorkerDetailPanelIdentityStory />);

    const line = cmp.getByTestId("worker-detail-task-line-ow-1");
    const title = cmp.getByTestId("worker-detail-header-task-title");
    await expect(line).toBeVisible();
    await expect(title).toBeVisible();
    expect(await line.textContent()).toBe("t-23cf5291a001OffiCraft 開發");
    expect(await title.textContent()).toBe(TITLE);

    const m = await page.evaluate(() => {
      const card = document.querySelector(".mp-identity") as HTMLElement;
      const l = document.querySelector('[data-testid="worker-detail-task-line-ow-1"]') as HTMLElement;
      const t = document.querySelector('[data-testid="worker-detail-header-task-title"]') as HTMLElement;
      const c = card.getBoundingClientRect();
      const lb = l.getBoundingClientRect();
      const tb = t.getBoundingClientRect();
      return {
        cardRight: c.right,
        cardOverflow: card.scrollWidth - card.clientWidth,
        lineRight: lb.right,
        lineBottom: lb.bottom,
        titleTop: tb.top,
        titleRight: tb.right,
        titleLines: Math.round(tb.height / parseFloat(getComputedStyle(t).lineHeight)),
        pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      };
    });
    expect(m.cardOverflow, "card does not overflow").toBeLessThanOrEqual(0);
    expect(m.lineRight, "task line inside the card").toBeLessThanOrEqual(m.cardRight);
    expect(m.titleRight, "title inside the card").toBeLessThanOrEqual(m.cardRight);
    expect(m.titleTop, "title under the task line").toBeGreaterThanOrEqual(m.lineBottom);
    if (width < 720) {
      expect(m.titleLines, "the full title wraps rather than clamping").toBeGreaterThan(2);
    }
    expect(m.pageOverflow, "no page hscroll").toBeLessThanOrEqual(1);
  });
}
