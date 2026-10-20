// The profile dropdown's three levels (first level, 帳號安全, 通行密鑰) must
// stay inside the viewport and not spill sideways on a phone. jsdom has no
// layout, so only a browser can say whether a long tunnel host in a passkey row
// wraps or pushes the 288px menu past the screen edge.
//
// The menu is 288px at every width, so a row that does not wrap (mutant:
// drop `overflow-wrap: anywhere` from .profile-dd__pk) is red at 390 AND 1280,
// measured as 114/122px of sideways pan inside the list. 390 additionally pins
// that the right-anchored menu itself stays inside a phone viewport.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ProfilePasskeysStory } from "./stories/ProfilePasskeysStory";

async function measure(page: Page) {
  return page.evaluate(() => {
    const dd = document.querySelector(".profile-dd")!;
    const r = dd.getBoundingClientRect();
    const spills = Array.from(dd.querySelectorAll("*"))
      .filter((el) => el.getBoundingClientRect().right > r.right + 0.5 || el.getBoundingClientRect().left < r.left - 0.5)
      .map((el) => el.className || el.tagName);
    return {
      left: r.left,
      right: r.right,
      viewport: document.documentElement.clientWidth,
      menuOverflow: dd.scrollWidth - dd.clientWidth,
      // .profile-dd__pk-list scrolls vertically, which makes it a horizontal
      // scroll container too: a row too wide for the menu spills INTO it as a
      // sideways pan and never reaches the menu or page numbers above.
      inner: Array.from(dd.querySelectorAll(".profile-dd__pk-list, .profile-dd__pk, .profile-dd__form"))
        .map((el) => el.scrollWidth - el.clientWidth)
        .filter((over) => over > 0),
      page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      spills,
    };
  });
}

function expectContained(m: Awaited<ReturnType<typeof measure>>) {
  expect(m.left).toBeGreaterThanOrEqual(0);
  expect(m.right).toBeLessThanOrEqual(m.viewport);
  expect(m.menuOverflow).toBe(0);
  expect(m.inner).toEqual([]);
  expect(m.page).toBe(0);
  expect(m.spills).toEqual([]);
}

for (const width of [390, 1280]) {
  test(`width ${width}: every level of the menu stays on screen and nothing spills`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ProfilePasskeysStory />);

    await expect(cmp.getByText("帳號安全")).toBeVisible();
    expectContained(await measure(page));

    await cmp.getByText("帳號安全").click();
    await expect(cmp.getByText("通行密鑰", { exact: true })).toBeVisible();
    expectContained(await measure(page));

    await cmp.getByText("通行密鑰", { exact: true }).click();
    await expect(cmp.getByText("可能被複製，建議刪除")).toBeVisible();
    await expect(cmp.getByText("此網址無法使用")).toBeVisible();
    expectContained(await measure(page));
    // The list scrolls vertically inside the menu, so bring each row in first;
    // what is under test is that no delete button sits off to the side.
    for (const del of await cmp.locator(".profile-dd__pk-delete").all()) {
      await del.scrollIntoViewIfNeeded();
      await expect(del).toBeInViewport({ ratio: 1 });
    }
  });
}
