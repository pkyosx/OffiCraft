// HOTSPOT — T-ba6b detail-panel convergence: the shared AgentDetailPanel info
// card is a two-column grid (LEFT 模型/思考強度 · RIGHT 機器/Claude Account) for
// BOTH the member and the outsource worker, since both now render through the
// SAME component.
//
// `.mp-info2 { display:grid; grid-template-columns:1fr 1fr }` (member-detail.css).
// jsdom resolves no grid and reports the two `.mp-field` columns STACKED (same
// x, second below first), so the unit suite cannot tell a converged two-column
// card from a regressed stacked one. This spec measures the real grid in a real
// browser for each identity. Mutant (documented): delete `grid-template-columns`
// → the fields stack → the "side-by-side" assertion reddens for both stories.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  MemberDetailConvergenceStory,
  WorkerDetailConvergenceStory,
} from "./stories/AgentDetailConvergenceStory";

const stories = {
  member: <MemberDetailConvergenceStory />,
  worker: <WorkerDetailConvergenceStory />,
};

for (const [kind, story] of Object.entries(stories)) {
  test(`${kind}: the shared info card renders 模型/思考強度 and 機器/Claude Account side-by-side (two-column grid)`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1200, height: 900 });
    const cmp = await mount(story);
    // The shared model/machine info card is the panel's only .mp-info2.
    const info = cmp.locator(".mp-info2").first();
    await expect(info).toBeVisible();
    const fields = info.locator(":scope > .mp-field");
    expect(await fields.count()).toBe(2);
    const left = await fields.nth(0).boundingBox();
    const right = await fields.nth(1).boundingBox();
    expect(left, "left field box").not.toBeNull();
    expect(right, "right field box").not.toBeNull();
    // SIDE-BY-SIDE A — the RIGHT column starts clearly to the right of the LEFT
    // column (a real fraction of the card width, not a 1px sub-pixel nudge).
    expect(right!.x).toBeGreaterThan(left!.x + left!.width / 2);
    // SIDE-BY-SIDE B — they share the same row: the two column tops line up
    // (tolerance for border/rounding), which a STACKED layout could never do.
    expect(Math.abs(right!.y - left!.y)).toBeLessThan(4);
  });
}

// The 運行狀況 card's boxes: 目前狀態 first and full width, then 記憶用量 and
// 估計$ side by side on a desktop; all three stacked in that order on a phone;
// without activity only the two. Mutant (documented): drop the 目前狀態 box's
// `grid-column: 1 / -1` → at 1280 it takes one column and 記憶用量 lands beside
// it → the "full width" and "next row" assertions redden.
const ACTIVITY = {
  state: "busy" as const,
  busySince: Date.now() / 1000 - 180,
  subagents: [
    { id: "a1", parentId: null, name: "Explore", description: "盤點 ACE-16005 的 Backlog 票，順便把重複的合併", startedTs: Date.now() / 1000 - 120 },
  ],
  waits: [],
};
const runtimeStories = {
  member: (activity: typeof ACTIVITY | null) => <MemberDetailConvergenceStory activity={activity} />,
  worker: (activity: typeof ACTIVITY | null) => <WorkerDetailConvergenceStory activity={activity} />,
};

for (const [kind, story] of Object.entries(runtimeStories)) {
  for (const width of [390, 1280]) {
    test(`${kind} width ${width}: the 運行狀況 card leads with a boxed 目前狀態, then 記憶用量 and 估計$, and without activity has only the two`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(story(ACTIVITY));
      const measure = () =>
        page.evaluate(() => {
          const cells = document.querySelector(".mp-runtime__cells") as HTMLElement;
          const boxes = Array.from(cells.children).map((el) => {
            const r = el.getBoundingClientRect();
            const s = getComputedStyle(el);
            return {
              cls: el.className,
              left: r.left,
              right: r.right,
              top: r.top,
              bottom: r.bottom,
              border: s.borderTopStyle,
              radius: s.borderTopLeftRadius,
            };
          });
          const c = cells.getBoundingClientRect();
          return {
            boxes,
            cellsLeft: c.left,
            cellsRight: c.right,
            overflow: cells.scrollWidth - cells.clientWidth,
            page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          };
        });
      const m = await measure();
      expect(m.boxes.map((b) => b.cls)).toEqual(["mp-cell mp-activity", "mp-cell", "mp-cell"]);
      const [now, ctx, cost] = m.boxes;
      expect([now.border, now.radius], "目前狀態 is boxed like the others").toEqual([ctx.border, ctx.radius]);
      expect(Math.abs(now.left - m.cellsLeft), "目前狀態 starts at the row's left").toBeLessThan(1);
      expect(Math.abs(now.right - m.cellsRight), "目前狀態 spans the whole row").toBeLessThan(1);
      expect(ctx.top, "記憶用量 sits below 目前狀態").toBeGreaterThanOrEqual(now.bottom);
      if (width >= 720) {
        expect(Math.abs(cost.top - ctx.top), "記憶用量 and 估計$ share a row").toBeLessThan(1);
        expect(cost.left, "估計$ is right of 記憶用量").toBeGreaterThanOrEqual(ctx.right);
      } else {
        expect(cost.top, "估計$ is stacked under 記憶用量").toBeGreaterThanOrEqual(ctx.bottom);
      }
      expect(m.overflow, "the box row does not overflow").toBeLessThanOrEqual(0);
      expect(m.page, "no page hscroll").toBeLessThanOrEqual(1);
      await cmp.unmount();

      await mount(story(null));
      const bare = await measure();
      expect(bare.boxes.map((b) => b.cls)).toEqual(["mp-cell", "mp-cell"]);
      if (width >= 720) {
        expect(Math.abs(bare.boxes[1].top - bare.boxes[0].top), "the two boxes share a row").toBeLessThan(1);
      }
    });
  }
}
