// HOTSPOT — 任務卡自由文字長 token 溢出 (T-4974).
//
// Bug (owner, iPhone screenshot 2026-07-19): a task card's baton/description
// text carried an unbreakable long token
// (`twin(desired_state/desired_machine_id/refocus_since/bank_balance/...)`).
// `.task-card__desc.doc-md` / `.task-step__dod .doc-md` /
// `.task-card__waiting-md.doc-md` declared NO overflow-wrap, so the token set the
// card's min-content to its full width, pushed the card past the 390px viewport
// and the whole PAGE gained a horizontal scrollbar — the owner could drag the
// page left/right. Fix: `overflow-wrap: anywhere` on those three free-text
// surfaces (anywhere, not break-word, so min-content shrinks and the card binds
// to the viewport).
//
// jsdom is blind to this (no layout engine, no @media, offsetWidth 0), so it is
// a CT guard measured in real Chromium against the real tasks.css — width is an
// INPUT dimension, so both 390 (phone) and 1280 (desktop) are asserted.
//
// MUTANT (§5, verified red→green): delete `overflow-wrap: anywhere` from ANY of
// the three .doc-md rules in tasks.css → that surface's token stops breaking,
// widens the card, and assertion (1) page-no-hscroll goes red at 390px.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardLongTokenStory } from "./stories/TaskCardLongTokenStory";

async function mountExpanded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardLongTokenStory />);
  // expand the card so the description + step DoD render.
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
  return cmp;
}

async function assertNoOverflow(page: any, width: number) {
  // (1) CORE red→green: the page must never scroll sideways — this is the
  // owner's exact symptom ("可以左右滑動").
  const page_ = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(
    page_,
    `[${width}px] page must have no horizontal scroll (got +${page_}px)`
  ).toBeLessThanOrEqual(1);

  // (2) each free-text surface must fit its own box — the token broke, so the
  // content width never exceeds the visible width. Pins each fix individually so
  // a mutant on one .doc-md rule names the surface it broke.
  for (const sel of [
    ".task-card__desc",
    ".task-step__dod .doc-md",
    ".task-card__waiting-md",
  ]) {
    const over = await page.evaluate((s: string) => {
      const el = document.querySelector(s) as HTMLElement | null;
      return el ? el.scrollWidth - el.clientWidth : -1;
    }, sel);
    expect(over, `[${width}px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }
}

test("390px: long-token free text never widens the card/page", async ({
  mount,
  page,
}) => {
  await mountExpanded(mount, page, 390);
  await assertNoOverflow(page, 390);
});

test("1280px: the wrap fix does not break the desktop layout", async ({
  mount,
  page,
}) => {
  await mountExpanded(mount, page, 1280);
  await assertNoOverflow(page, 1280);
});
