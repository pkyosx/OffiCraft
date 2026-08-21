// T-cd6f: the five per-theme avatar behaviours, exercised in a real browser and
// recorded as screenshots. These are the owner-requested visible results for
// pool selection, deleting a pool image, empty pools, theme switching, and the
// error-handling behaviour.
//
// Each colour is a DIFFERENT image, so the screenshot itself shows which one
// won — the point of the whole ticket is that the wrong image must never win.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { AvatarChooserScenariosStory } from "./stories/AvatarChooserScenariosStory";

const SHOT_DIR =
  process.env.AVATAR_SCENARIO_SHOT_DIR ?? "test-results/avatar-theme-scenarios";

async function shot(page: Page, name: string) {
  await page.screenshot({ path: `${SHOT_DIR}/${name}.png`, fullPage: true });
}

/** The src the roster row is actually painting, or null when it fell through to
 * the built-in glyph. */
async function rendered(cmp: Locator): Promise<string | null> {
  const img = cmp.getByTestId("rendered-avatar").locator("img");
  return (await img.count()) === 0 ? null : await img.getAttribute("src");
}

async function pick(cmp: Locator, index: number) {
  await cmp.getByRole("button", { name: "選擇圖像" }).click();
  await cmp.getByRole("radio").nth(index).click();
}

test("a choice is per theme: switching to another theme and back restores it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<AvatarChooserScenariosStory />);

  // First visit: no row exists, so the pool's FIRST image renders.
  const first = await rendered(cmp);
  await shot(page, "01-first-visit-renders-the-first-image");

  // Pick the SECOND image in alpha.
  await pick(cmp, 1);
  const inAlpha = await rendered(cmp);
  expect(inAlpha).not.toBe(first);
  await shot(page, "02-alpha-explicit-choice");

  // Beta has its own pool and its own (absent) row, so it starts at ITS first
  // image — not at alpha's choice re-resolved against beta's pool.
  await cmp.getByTestId("to-beta").click();
  const betaDefault = await rendered(cmp);
  expect(betaDefault).not.toBe(inAlpha);
  await shot(page, "03-beta-starts-at-its-own-first-image");

  await pick(cmp, 1);
  const inBeta = await rendered(cmp);
  expect(inBeta).not.toBe(betaDefault);
  await shot(page, "04-beta-explicit-choice");

  // 🔴 The headline requirement: coming back restores alpha's own choice, and
  // beta's choice is untouched.
  await cmp.getByTestId("to-alpha").click();
  expect(await rendered(cmp)).toBe(inAlpha);
  await shot(page, "05-back-in-alpha-the-choice-is-restored");

  await cmp.getByTestId("to-beta").click();
  expect(await rendered(cmp)).toBe(inBeta);
  await shot(page, "06-beta-was-never-overwritten");
});

test("removing the chosen image falls back to the first one, not to a neighbour", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<AvatarChooserScenariosStory />);
  await pick(cmp, 1);
  const chosen = await rendered(cmp);

  await cmp.getByTestId("prune-pool").click();
  // Poll: the pool edit re-renders, and reading once can land before it.
  await expect.poll(() => rendered(cmp)).not.toBe(chosen);
  // The pool now holds one image, and that is what renders. Under the retired
  // index model this is where a member silently inherited another face.
  await expect(cmp.getByTestId("rendered-avatar").locator("img")).toHaveCount(1);
  await shot(page, "07-removed-image-falls-back-to-the-first");
});

test("an empty pool explains itself instead of disappearing", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<AvatarChooserScenariosStory />);
  await cmp.getByTestId("empty-pool").click();

  // The control is gone because there is nothing to choose, but the reason is
  // on screen: a selector that vanishes reads as a broken cockpit.
  await expect(cmp.getByRole("button", { name: "選擇圖像" })).toHaveCount(0);
  await expect(cmp.locator(".avatar-chooser__empty")).toBeVisible();
  // The row itself falls through to the built-in glyph, never a broken image.
  expect(await rendered(cmp)).toBeNull();
  await shot(page, "08-empty-pool-explains-itself");
});

test("a broken image is labelled, not left as a broken-image box", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<AvatarChooserScenariosStory />);
  await cmp.getByTestId("break-pool").click();

  await expect(cmp.locator(".avatar-chooser__broken")).toBeVisible();
  await expect(
    cmp.getByRole("img", { name: "圖片無法顯示" }),
  ).toBeVisible();
  // The roster row degrades to the built-in glyph for the same failure.
  expect(await rendered(cmp)).toBeNull();
  await shot(page, "09-broken-image-is-labelled");
});
