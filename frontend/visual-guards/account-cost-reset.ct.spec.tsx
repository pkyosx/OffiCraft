// HOTSPOT — T-53 帳號歸零 (owner ruling rc-5c5d7c7c6dcd). Two things a unit test
// structurally cannot see:
//
//   1. the pill sits INLINE on the account card's cost line, whose side of the
//      head is `flex: none`. Whether it stays inside the card instead of pushing
//      the head open (or spilling past the card's right edge) is a layout
//      question; jsdom reports every box at x=0 with no width, so a pill that
//      breaks the card reads as green there. Mutant: raise .mon-acct__reset's
//      margin-left to 200px → the "inside the card" assertion reddens.
//   2. the confirm is position:fixed. Whether it actually COVERS the figure it
//      is about to destroy — rather than rendering off the visible area — is a
//      real-browser question.
//
// Screenshots go through testInfo.outputPath(), NOT a bare filename: a bare path
// resolves against the cwd and drops PNGs into the working tree unignored, where
// a routine `git add -A` sweeps them into a commit. That is not hypothetical —
// frontend/.gitignore carries a `recon-out/*.png` rule written after 48 such
// strays, and the sibling cost-reset guard reproduced the same defect once.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  AccountCostResetButtonStory,
  AccountCostResetNothingToClearStory,
} from "./stories/AccountCostResetStory";

test("the 歸零 pill sits on the cost line and stays inside the card", async ({
  mount,
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<AccountCostResetButtonStory />);

  const reset = cmp.getByTestId("mon-acct-cost-reset");
  const card = page.locator(".mon-acct");
  await expect(reset).toBeVisible();

  const r = await reset.boundingBox();
  const c = await card.boundingBox();
  expect(r, "reset pill box").not.toBeNull();
  expect(c, "account card box").not.toBeNull();
  // Inside the card, with room to spare on the right. A pill that overflowed
  // would be clipped or would widen the card past its column.
  expect(r!.x + r!.width).toBeLessThanOrEqual(c!.x + c!.width);
  // On the head line, not pushed onto a row of its own: the card's head is the
  // first ~40px of it.
  expect(r!.y - c!.y).toBeLessThan(40);

  await page.screenshot({
    path: testInfo.outputPath("account-cost-reset-1-button.png"),
  });
});

test("the confirm covers the figure, names the amount and says no member is touched", async ({
  mount,
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<AccountCostResetButtonStory />);

  const cost = page.locator(".mon-acct__cost");
  const before = await cost.boundingBox();

  await cmp.getByTestId("mon-acct-cost-reset").click();
  const dialog = cmp.getByTestId("mon-acct-cost-reset-confirm");
  await expect(dialog).toBeVisible();
  // What he is destroying, and — the whole point of this ruling — what he is
  // NOT. Both sentences have to be in front of him before the press.
  await expect(dialog).toContainText("$37");
  await expect(dialog).toContainText("底下成員各自的數字不會被動到");
  await expect(dialog).toContainText("清掉就回不來了");

  const box = await dialog.boundingBox();
  expect(box, "confirm box").not.toBeNull();
  // It really is over the card, not parked below the fold.
  expect(box!.y).toBeLessThan(before!.y + before!.height);
  expect(box!.width).toBeGreaterThan(200);

  await page.screenshot({
    path: testInfo.outputPath("account-cost-reset-2-confirm.png"),
  });
});

test("nothing measured: the figure reads the dash and the button is dead", async ({
  mount,
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<AccountCostResetNothingToClearStory />);

  // The pair that must never disagree — both come off the same null test.
  await expect(page.locator(".mon-acct__cost")).toContainText("—");
  await expect(cmp.getByTestId("mon-acct-cost-reset")).toBeDisabled();

  await page.screenshot({
    path: testInfo.outputPath("account-cost-reset-3-nothing.png"),
  });
});
