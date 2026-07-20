// GUARD (T-9ca5) — step-level 等待外部 badge + reason row.
//
// Contract: a step in waiting_external gets its OWN badge (step-waiting-external
// / .task-step-badge--waiting-external, distinct from the amber owner 等我回覆)
// and a .task-step__waiting reason row that ALWAYS-STACKS — the 等待中 label on
// line 1, the markdown reason on line 2 — at every width. The fixture reason is
// 3 chars, so it trivially fits beside the label; its landing on line 2 is the
// CSS (flex-basis:100% in a wrap row), not overflow — the same argument the
// task-level card-reflow guard used to make (that guard was removed in T-c514
// along with the task-level waiting block it measured; this is now the only
// place the always-stack contract is pinned).
//
// jsdom is blind to the layout (no engine, offsetHeight 0), so this is a CT
// guard in real Chromium at 390 (phone) + 1280 (desktop).
//
// MUTANT (verified red→green): delete `flex-basis: 100%` from
// .task-step__waiting-md → the 3-char reason goes INLINE beside the label →
// assertion (2) STACKED-A goes red.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardStepWaitingStory } from "./stories/TaskCardStepWaitingStory";

async function mountExpanded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardStepWaitingStory />);
  // expand the card so the step timeline (and its waiting row) renders.
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.getByTestId("step-waiting-reason")).toBeVisible();
  return cmp;
}

for (const width of [1280, 390]) {
  test(`width ${width}: step 等待外部 badge + reason row stacks`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width);

    // (1) the step carries its OWN external-wait badge, distinct from 等我回覆.
    await expect(cmp.getByTestId("step-waiting-external")).toBeVisible();

    // (2) the reason row always-stacks: label on line 1, reason md on line 2.
    const row = cmp.getByTestId("step-waiting-reason");
    const label = row.locator(".task-step__waiting-label");
    const md = row.locator(".task-step__waiting-md");
    const labelBox = (await label.boundingBox())!;
    const mdBox = (await md.boundingBox())!;
    expect(labelBox, "label box").not.toBeNull();
    expect(mdBox, "reason md box").not.toBeNull();
    // STACKED-A: reason's top starts a real line below the label's top.
    expect(mdBox.y - labelBox.y).toBeGreaterThan(8);
    // STACKED-B: reason begins on a NEW flex line — left edge at/left-of label.
    expect(mdBox.x).toBeLessThanOrEqual(labelBox.x + 1);
  });
}
