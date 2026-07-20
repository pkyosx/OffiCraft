// HOTSPOT — T-b0e3 (owner 2026-07-20 截圖): 「委託任務」must render ABOVE
// 模型/機器 in the outsource-worker detail panel (was buried below 最近操作).
// AgentDetailPanel composes cards via plain JSX order — jsdom resolves that
// fine, so a unit test could already assert DOM order. What jsdom CANNOT
// verify is that the reorder didn't also break the real CSS layout (stacking
// context / card spacing) at the widths the owner actually uses. Measured here
// in a real browser at 375/390 (owner's phone) and 1280 (desktop).
//
// Mutant (documented): swap `afterIdentityCards={taskCard}` back to
// `beforeTerminalCards={taskCard}` in WorkerDetailPanel.tsx → 委託任務 renders
// after 最近操作 again → the order assertion reddens at all three widths.
import { test, expect } from "@playwright/experimental-ct-react";
import { WorkerDetailPanelTaskOrderStory } from "./stories/WorkerDetailPanelTaskOrderStory";

for (const width of [375, 390, 1280]) {
  test(`width ${width}: 委託任務 card renders above the 模型/機器 card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<WorkerDetailPanelTaskOrderStory />);

    // Surface-existence first (§1 of the手冊 learnings: an absent element must
    // fail loudly, never silently satisfy an order/overflow assertion below).
    const taskCard = cmp.locator(".mp-worker-task");
    await expect(taskCard).toBeVisible();
    // The model/machine card is the FIRST .mp-info2 (AgentDetailPanel renders
    // it before the worker's own afterInfoCards 狀態/委託人 .mp-info2).
    const modelMachineCard = cmp.locator(".mp-info2").first();
    await expect(modelMachineCard).toBeVisible();

    const taskBox = await taskCard.boundingBox();
    const modelBox = await modelMachineCard.boundingBox();
    expect(taskBox, "task card box").not.toBeNull();
    expect(modelBox, "model/machine card box").not.toBeNull();

    // 委託任務's top must sit clearly ABOVE 模型/機器's top (a real card's
    // height of margin, not a 1px rounding nudge).
    expect(taskBox!.y).toBeLessThan(modelBox!.y - 8);
  });
}
