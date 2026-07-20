// GUARD (T-a706) — 請示頁卡片 header avatar is a real click + keyboard target.
//
// Owner (2026-07-21 screenshot): the 請示 page card avatar was the one place
// in the cockpit whose avatar did NOT open the member panel. jsdom (vitest)
// already proves the routing logic (which hash the click produces — see
// RepliesPage.test.tsx); what it CANNOT prove is whether a real browser
// actually delivers the click/keyboard event once real CSS/layout is in
// play, and whether Enter/Space native button activation actually fires (no
// JS keydown handler backs it — this is the browser's own default action for
// a focused <button>, which jsdom does not implement).
//
// MUTANT (verified red→green, see task conclusion doc): delete the `onClick`
// prop wiring in ReplyCardAvatarButton.tsx → every test below goes red
// (click and keyboard alike, since both ultimately fire the same handler).
// Deleting the `aria-label`/`title` leaves click/keyboard green but reddens
// the accessible-name assertion.
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardAvatarStory } from "./stories/ReplyCardAvatarStory";
import { zh } from "../src/i18n/locales/zh";

for (const width of [1280, 390]) {
  test(`width ${width}: clicking the avatar opens the profile`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardAvatarStory />);
    await expect(cmp.getByTestId("open-count")).toHaveText("0");
    await cmp.locator(".reply-card__avatar").click();
    await expect(cmp.getByTestId("open-count")).toHaveText("1");
  });

  test(`width ${width}: the avatar is keyboard-reachable (Tab) and Enter/Space both activate it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardAvatarStory />);
    const avatarBtn = cmp.locator(".reply-card__avatar");

    await page.keyboard.press("Tab");
    await expect(avatarBtn).toBeFocused();

    await page.keyboard.press("Enter");
    await expect(cmp.getByTestId("open-count")).toHaveText("1");

    await page.keyboard.press("Space");
    await expect(cmp.getByTestId("open-count")).toHaveText("2");
  });
}

test("the avatar exposes an accessible name (aria-label survives real CSS: Avatar's inner glyphs are aria-hidden)", async ({
  mount,
}) => {
  const cmp = await mount(<ReplyCardAvatarStory />);
  const byRole = cmp.getByRole("button", { name: zh.office.viewProfile });
  await expect(byRole).toBeVisible();
});

test("focus-visible paints a non-transparent ring on the avatar button", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<ReplyCardAvatarStory />);
  await page.keyboard.press("Tab");
  const shadow = await cmp
    .locator(".reply-card__avatar")
    .evaluate((el) => getComputedStyle(el).boxShadow);
  expect(shadow, "focused avatar paints a box-shadow ring").not.toBe("none");
});
