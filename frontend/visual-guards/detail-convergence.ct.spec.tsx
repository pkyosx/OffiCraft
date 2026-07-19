// HOTSPOT — T-ba6b detail-panel convergence: the shared AgentDetailPanel info
// card is a two-column grid (LEFT 模型/投入度 · RIGHT 機器/Claude Account) for
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
  test(`${kind}: the shared info card renders 模型/投入度 and 機器/Claude Account side-by-side (two-column grid)`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1200, height: 900 });
    const cmp = await mount(story);
    // The FIRST .mp-info2 is the shared model/machine info card (AgentDetailPanel
    // renders it before any afterInfoCards slot).
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
