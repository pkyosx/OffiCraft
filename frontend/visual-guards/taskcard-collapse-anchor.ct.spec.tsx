// HOTSPOT — T-6630 ③, measured in a real browser.
//
// Owner 2026-08-16:「收和整個任務時,最後應該要定位到那則任務,現在好像會跑掉」.
//
// WHY A CT GUARD: every claim here is a scroll position, and jsdom has no
// layout engine — `scrollTop`, `getBoundingClientRect()` and the browser's own
// clamping of the scroll range are all zero there, so a jsdom version of this
// file would pass against an empty implementation.
//
// WHAT IS MEASURED, AND WHY IT IS THESE THINGS:
//  · `.tasks` is the box that scrolls in production (`document.scrollingElement`
//    never moves — measured). Reading the document instead is the mistake that
//    makes this whole file green on any implementation, including none.
//  · A LIST of cards, not one: "跑掉" is about where the card ends up relative
//    to the rest of the column. Cards above give it an offset; cards below give
//    the range it can move within. A short list HIDES the bug — the browser
//    clamps `scrollTop` into the shrunken range and that accidentally brings the
//    card back — so the count is part of the fixture's argument, not decoration.
//  · Real clicks are only ever made on something already on screen. Playwright's
//    `.click()` scrolls an off-screen target into view first, and that scroll
//    would be measured here as if the component had done it (T-6630 ① lost a
//    whole headline number to exactly this). Every click below is a DOM
//    `element.click()` on an element proven to be inside the scrollport.
//
// MUTANT REGISTER (planted and observed, not assumed):
//  M1  delete the collapse effect in TaskCard (the `wasExpanded` layout effect)
//      ⇒ "a card whose top ran off the fold comes back" FAILS at both widths:
//      the card keeps top -296 and, being ~250px tall collapsed, is entirely
//      above the viewport (visible=false).
//  M2  make the correction unconditional (drop `if (top >= view.top) return`)
//      ⇒ "collapsing with the head on screen moves nothing" FAILS: the card is
//      dragged from top 104 up to the fold, i.e. the screen moves under a user
//      who never asked it to.
//  M3  measure `document.scrollingElement` instead of `.tasks` in the component
//      ⇒ same failure as M1 (the document never scrolls here, so the correction
//      writes to a box with no range).
// 🔴 What the register also SHOWS: the third test (the clamped last card)
// survives all three mutants, and that is not a gap to be fixed — there the
// browser's own clamp does the positioning, so it cannot tell a working
// correction from none. It is here for the BOUNDARY (the card can still be read
// when it cannot be brought to the fold). Do not read it as coverage of the fix.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardCollapseAnchorStory } from "./stories/TaskCardCollapseAnchorStory";

const CASES = [
  { name: "1280×800 desktop", w: 1280, h: 800 },
  { name: "390×844 phone", w: 390, h: 844 },
];

/** The 5th card of 12 — cards above it AND enough below to keep a scroll range. */
const TARGET = "t-c5";

type Snap = {
  scrollTop: number;
  maxScroll: number;
  viewTop: number;
  viewBottom: number;
  cardTop: number;
  cardBottom: number;
  cardH: number;
  visible: boolean;
};

async function measure(page: any, id: string): Promise<Snap> {
  return page.evaluate((tid: string) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    const card = document.querySelector(
      `[data-task-id='${tid}']`
    ) as HTMLElement;
    const r = card.getBoundingClientRect();
    const sr = sc.getBoundingClientRect();
    return {
      scrollTop: sc.scrollTop,
      maxScroll: sc.scrollHeight - sc.clientHeight,
      viewTop: sr.top,
      viewBottom: sr.top + sc.clientHeight,
      cardTop: r.top,
      cardBottom: r.bottom,
      cardH: r.height,
      visible: r.bottom > sr.top + 1 && r.top < sr.top + sc.clientHeight - 1,
    };
  }, id);
}

/** Click the card's head — used only while the head is provably on screen. */
async function clickHead(page: any, id: string) {
  await page.evaluate((tid: string) => {
    const card = document.querySelector(
      `[data-task-id='${tid}']`
    ) as HTMLElement;
    (card.querySelector(".task-card__head") as HTMLElement).click();
  }, id);
}

/**
 * Collapse from a scroll position where the head is NOT reachable: click a step
 * name inside the scrollport. A step row is non-interactive, so it goes through
 * the card's own toggle — the way a reader actually closes a card they scrolled
 * into.
 */
async function clickBodyOnScreen(page: any, id: string) {
  const hit = await page.evaluate((tid: string) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    const card = document.querySelector(
      `[data-task-id='${tid}']`
    ) as HTMLElement;
    const sr = sc.getBoundingClientRect();
    const viewTop = sr.top;
    const viewBottom = sr.top + sc.clientHeight;
    const target = (
      Array.from(
        card.querySelectorAll("[data-testid='task-step'] .task-step__name")
      ) as HTMLElement[]
    ).find((s) => {
      const r = s.getBoundingClientRect();
      return r.top > viewTop && r.bottom < viewBottom;
    });
    if (!target) return false;
    target.click();
    return true;
  }, id);
  // A click on something off screen would be a different experiment.
  expect(hit, "no part of the card was on screen to click").toBe(true);
}

async function mountList(mount: any, page: any, c: (typeof CASES)[number], cards: number) {
  await page.setViewportSize({ width: c.w, height: c.h });
  const cmp = await mount(<TaskCardCollapseAnchorStory cards={cards} />);
  await expect(page.locator("[data-testid='task-card']")).toHaveCount(cards);
  return cmp;
}

async function expandAndVerify(page: any, id: string) {
  await clickHead(page, id);
  await expect(
    page.locator(`[data-task-id='${id}'] .task-card__workflow`)
  ).toBeVisible();
}

async function expectCollapsed(page: any, id: string) {
  await expect(
    page.locator(`[data-task-id='${id}'] .task-card__workflow`)
  ).toHaveCount(0);
}

for (const c of CASES) {
  test(`[${c.name}] a card whose top ran off the fold comes back when you collapse it`, async ({
    mount,
    page,
  }) => {
    await mountList(mount, page, c, 12);
    await expandAndVerify(page, TARGET);

    // Read your way down inside the open card: its top edge is now 300px above
    // the fold — the owner's situation, not a contrived one.
    await page.evaluate((tid: string) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const card = document.querySelector(
        `[data-task-id='${tid}']`
      ) as HTMLElement;
      sc.scrollTop +=
        card.getBoundingClientRect().top - sc.getBoundingClientRect().top + 300;
    }, TARGET);

    const before = await measure(page, TARGET);
    expect(
      before.cardH,
      "premise: the open card must be taller than the scrollport"
    ).toBeGreaterThan(before.viewBottom - before.viewTop);
    expect(
      before.cardTop,
      "premise: the card's top edge must be above the fold"
    ).toBeLessThan(before.viewTop);

    await clickBodyOnScreen(page, TARGET);
    await expectCollapsed(page, TARGET);
    const after = await measure(page, TARGET);

    // NON-VACUITY: without a correction the card keeps the top it had, and the
    // collapsed card is short enough that this puts it ENTIRELY off screen.
    // If a fixture change ever made "do nothing" pass this test, it fails here
    // instead, loudly.
    expect(
      before.cardTop + after.cardH,
      "the fixture no longer reproduces the bug — doing nothing would leave the card visible"
    ).toBeLessThan(after.viewTop);

    // NOT CLAMPED: this case proves the component moved, not the browser.
    expect(
      after.scrollTop,
      "the scroll range is still long enough that the browser's clamp is not doing the work"
    ).toBeLessThan(after.maxScroll);

    expect(after.visible, "the collapsed task must be on screen").toBe(true);
    expect(
      after.cardTop - after.viewTop,
      "the collapsed task's top edge sits at the fold"
    ).toBeCloseTo(0, 0);
  });

  test(`[${c.name}] collapsing with the head already on screen moves nothing at all`, async ({
    mount,
    page,
  }) => {
    // The half that was never broken, and the half a correction can break. The
    // owner asked for ONE thing: 定位到那則任務. When you can already see the
    // task, "positioning" means not moving.
    await mountList(mount, page, c, 12);
    await expandAndVerify(page, TARGET);
    await page.evaluate((tid: string) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const card = document.querySelector(
        `[data-task-id='${tid}']`
      ) as HTMLElement;
      sc.scrollTop +=
        card.getBoundingClientRect().top - sc.getBoundingClientRect().top - 100;
    }, TARGET);

    const before = await measure(page, TARGET);
    expect(
      before.cardTop,
      "premise: the head is on screen, below the fold"
    ).toBeGreaterThan(before.viewTop);

    await clickHead(page, TARGET);
    await expectCollapsed(page, TARGET);
    const after = await measure(page, TARGET);

    expect(
      after.scrollTop,
      `collapsing scrolled \`.tasks\` (${before.scrollTop} → ${after.scrollTop})`
    ).toBe(before.scrollTop);
    expect(
      after.cardTop - before.cardTop,
      "the card's top edge moved"
    ).toBeCloseTo(0, 0);
  });

  test(`[${c.name}] expanding a card whose top is above the fold moves nothing`, async ({
    mount,
    page,
  }) => {
    // The direction the correction must NOT have. ③ answers "where do I end up
    // when the task goes away"; nothing goes away when a card OPENS, so ①'s
    // rule still owns that direction:「只是單純往下展開,整個畫面不能移動」.
    // A collapsed card is only ~250px tall, so reading down a list routinely
    // leaves one with its top just above the fold — clicking its lower half is
    // the ordinary way to open it, and the screen must stay put.
    await mountList(mount, page, c, 12);

    // Park the (collapsed) target with its top above the fold.
    await page.evaluate((tid: string) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const card = document.querySelector(
        `[data-task-id='${tid}']`
      ) as HTMLElement;
      sc.scrollTop +=
        card.getBoundingClientRect().top - sc.getBoundingClientRect().top + 40;
    }, TARGET);

    const before = await measure(page, TARGET);
    expect(
      before.cardTop,
      "premise: the card's top must be above the fold"
    ).toBeLessThan(before.viewTop);
    expect(
      before.visible,
      "premise: part of the card must still be on screen to click"
    ).toBe(true);

    // Click the card's own body, on screen — `element.click()` does not scroll.
    const hit = await page.evaluate((tid: string) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const card = document.querySelector(
        `[data-task-id='${tid}']`
      ) as HTMLElement;
      const sr = sc.getBoundingClientRect();
      const target = (
        Array.from(card.querySelectorAll(".task-card__title")) as HTMLElement[]
      ).find((t) => {
        const r = t.getBoundingClientRect();
        return r.top > sr.top && r.bottom < sr.top + sc.clientHeight;
      });
      if (!target) return false;
      target.click();
      return true;
    }, TARGET);
    expect(hit, "no on-screen part of the collapsed card to click").toBe(true);
    await expect(
      page.locator(`[data-task-id='${TARGET}'] .task-card__workflow`)
    ).toBeVisible();

    const after = await measure(page, TARGET);
    expect(
      after.scrollTop,
      `expanding scrolled \`.tasks\` (${before.scrollTop} → ${after.scrollTop})`
    ).toBe(before.scrollTop);
    expect(
      after.cardTop - before.cardTop,
      "the card's top edge moved on expand"
    ).toBeCloseTo(0, 0);
  });

  test(`[${c.name}] the LAST card in a short list stays fully readable even though the clamp stops it short`, async ({
    mount,
    page,
  }) => {
    // 🔴 The physical limit, measured rather than assumed. Collapsing the last
    // card shortens the scroll range under the scroll offset, so the card CANNOT
    // be brought to the fold — there is nothing left below to scroll. What must
    // still hold is the owner's actual ask: you end up looking at that task.
    await mountList(mount, page, c, 3);
    const LAST = "t-c3";
    await expandAndVerify(page, LAST);
    await page.evaluate((tid: string) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const card = document.querySelector(
        `[data-task-id='${tid}']`
      ) as HTMLElement;
      sc.scrollTop +=
        card.getBoundingClientRect().top - sc.getBoundingClientRect().top + 600;
    }, LAST);

    const before = await measure(page, LAST);
    expect(before.cardTop, "premise: the card's top is above the fold").toBeLessThan(
      before.viewTop
    );

    await clickBodyOnScreen(page, LAST);
    await expectCollapsed(page, LAST);
    const after = await measure(page, LAST);

    // The clamp really is what stops it — otherwise this case would be a
    // duplicate of the first test rather than the boundary it claims to be.
    expect(
      after.scrollTop,
      "premise: the scroll offset is pinned at the end of the range"
    ).toBe(after.maxScroll);
    expect(
      after.cardTop,
      "the clamp means the card lands BELOW the fold, not at it"
    ).toBeGreaterThan(after.viewTop);
    // …and the whole collapsed card is nevertheless on screen.
    expect(after.visible).toBe(true);
    expect(
      after.cardBottom,
      "the collapsed card must fit entirely inside the scrollport"
    ).toBeLessThanOrEqual(after.viewBottom + 1);
  });
}
