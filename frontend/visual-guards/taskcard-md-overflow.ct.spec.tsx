// HOTSPOT — 任務卡 markdown 內文撐出卡片 (T-4aa0).
//
// Bug (owner, phone screenshot 2026-08-15): a task description's quote block ran
// past the card's right edge and the whole page could be dragged sideways.
//
// 🔴 THE CIRCLED BLOCK WAS NOT THE CAUSE. Measured at 390px, the quote's own
// min-content is 67px and it overflows by 0; what is far wider than the card
// can give it is `.task-card__desc` itself. Hiding the children one at a time
// settles it causally: hide the `<pre>` and the overflow goes to 0, hide the
// quote (or the paragraph) and nothing moves. The quote was the visible tenant
// of a container something else had widened.
//
// NOR IS THE `<pre>` THE WHOLE STORY — the first version of this comment said
// it was, and independent review (T-9b37) measured that too narrow: a WIDE
// TABLE overflows on its own with no code fence in the document at all, and by
// more (+675 at 390px). Any child whose min-content exceeds the card does it.
// The fix is at the container, so one change binds them all; both fixtures are
// asserted below so a future fix aimed at `pre` alone cannot pass.
//
// Mechanism: `.task-card__desc-block` is a COLUMN flex container. It carried
// `align-items: flex-start`, which sizes each item to fit-content — and
// fit-content floors at min-content, which the un-breakable shell command inside
// the `<pre>` pushes to 479. Removing it lets the description stretch to the
// card, and `.doc-md pre` then scrolls inside itself, which is what
// settings.css already says that rule is for.
//
// Measured non-fixes, recorded so the next reader does not spend the round on
// them: `min-width: 0` on the description, on any single ancestor, or on the
// whole chain — no change; `overflow-x: hidden` on the description — no change.
// The intuitive fix for "a flex child will not shrink" does not apply, because
// this is the item's CROSS size, not its automatic minimum size.
//
// jsdom cannot see any of this (no layout engine), so it is a CT guard in real
// Chromium against the real tasks.css. 390 (the owner's phone) and 320 (the
// narrowest phone we support) are both asserted — width is an INPUT dimension.
//
// MUTANT (verified red→green): put `align-items: flex-start` back on
// `.task-card__desc-block` → all four cases go red, assertion (1) naming the
// LIST (+148px at 390 with the full ancestor chain; it was +104 when the story
// mounted bare, which is the 44px of `.app__main` padding this fixture used to
// be missing). +218 at 320. The WIDE TABLE cases print their own pair — +173 at
// 390 and +243 at 320 — so all four do NOT report one number; a reader who
// takes +148 as "the" mutant figure is reading the `<pre>` pair only.
// Numbers here are what the mutant printed — an earlier header quoted the
// CARD's overflow against the LIST assertion, which review caught, so quote a
// number only with the assertion it came from.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  TaskCardQuoteOverflowStory,
  TaskCardWideTableOverflowStory,
} from "./stories/TaskCardQuoteOverflowStory";

async function mountExpanded(mount: any, page: any, width: number, story: any) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(story);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
  return cmp;
}

async function assertFits(page: any, width: number) {
  const m = await page.evaluate(() => {
    const q = (s: string) => document.querySelector(s) as HTMLElement | null;
    const doc = document.scrollingElement!;
    const card = q(".task-card");
    const list = q(".tasks");
    const desc = q(".task-card__desc");
    const pre = q(".task-card__desc pre");
    const quote = q(".task-card__desc blockquote");
    const table = q(".task-card__desc table");
    const box = (el: HTMLElement | null) =>
      el
        ? { w: Math.round(el.getBoundingClientRect().width), over: el.scrollWidth - el.clientWidth }
        : null;
    return {
      page: doc.scrollWidth - doc.clientWidth,
      listOver: list ? list.scrollWidth - list.clientWidth : -2,
      cardOver: card ? card.scrollWidth - card.clientWidth : -2,
      cardInner: card ? card.clientWidth : -2,
      desc: box(desc),
      pre: box(pre),
      quote: box(quote),
      table: box(table),
    };
  });

  // (1) CORE red→green — the surface the user actually drags. In production the
  // DOCUMENT never scrolls sideways (measured by the T-9b37 review, before AND
  // after the fix): `.tasks` is the scroller, so this is where the owner's
  // symptom lives. The page check below is kept as a second, weaker net.
  expect(m.listOver, `[${width}px] .tasks never rendered`).not.toBe(-2);
  expect(
    m.listOver,
    `[${width}px] the task list scrolls sideways by +${m.listOver}px — this is the ` +
      `surface the phone actually drags`
  ).toBeLessThanOrEqual(1);
  // Kept as a second net, and worth being honest about: on THIS fixture it is
  // 0 before and after the fix, because `.tasks` absorbs the overflow into its
  // own scrollbar. It is not coverage — it is the case where some future change
  // removes that absorption and the whole page starts dragging again.
  expect(m.page, `[${width}px] page scrolls sideways by +${m.page}px`).toBeLessThanOrEqual(1);

  // (2) …and the card itself must not be overflowed from inside, so a container
  // that merely CLIPS the overflow cannot turn (1) green while the content is
  // still unreachable.
  expect(m.cardOver, `[${width}px] card content overflows by +${m.cardOver}px`).toBeLessThanOrEqual(1);

  // (3) the description must be bound BY the card rather than sizing itself to
  // its widest child — this is the layer the measurement named.
  expect(m.desc, `[${width}px] description never rendered`).not.toBeNull();
  expect(
    m.desc!.w,
    `[${width}px] description is ${m.desc!.w}px inside a ${m.cardInner}px card`
  ).toBeLessThanOrEqual(m.cardInner);

  // (4) NON-VACUITY + the other half of the contract. The surfaces that are
  // allowed to scroll must still be present, and the <pre> must keep scrolling
  // INSIDE itself — a "fix" that made the code block wrap or clip would satisfy
  // (1)-(3) while destroying the thing settings.css keeps overflow-x:auto for.
  for (const [name, box] of [
    ["pre", m.pre],
    ["blockquote", m.quote],
    ["table", m.table],
  ] as const) {
    expect(box, `[${width}px] ${name} never rendered — the fixture stopped covering it`).not.toBeNull();
  }
  expect(
    m.pre!.over,
    `[${width}px] the code block no longer scrolls inside itself (over=${m.pre!.over}); ` +
      `its long line has to go somewhere, and clipping it is not the fix`
  ).toBeGreaterThan(0);
  expect(
    m.quote!.over,
    `[${width}px] the quote block overflows its own box by +${m.quote!.over}px`
  ).toBeLessThanOrEqual(1);
}

for (const width of [390, 320]) {
  test(`${width}px: a description with a code block never widens the card/list`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width, <TaskCardQuoteOverflowStory />);
    await assertFits(page, width);
  });

  // The second, INDEPENDENT cause. A wide table overflows with no code fence in
  // the document at all, and by more than the fence did. Both die on the same
  // container fix, so this is not a second bug — it is the case that keeps a
  // future `pre`-shaped fix from passing while the real one is still broken.
  test(`${width}px: a wide table with no code block is bound too`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width, <TaskCardWideTableOverflowStory />);
    const m = await page.evaluate(() => {
      const q = (s: string) => document.querySelector(s) as HTMLElement | null;
      const list = q(".tasks");
      const pre = q(".task-card__desc pre");
      return {
        listOver: list ? list.scrollWidth - list.clientWidth : -2,
        hasPre: !!pre,
      };
    });
    // Non-vacuity: the fixture must really carry no code fence, or this case
    // silently becomes a duplicate of the one above.
    expect(m.hasPre, `[${width}px] fixture grew a <pre>; this case no longer isolates the table`).toBe(false);
    expect(m.listOver, `[${width}px] .tasks never rendered`).not.toBe(-2);
    expect(
      m.listOver,
      `[${width}px] a table-only description still scrolls the list by +${m.listOver}px`
    ).toBeLessThanOrEqual(1);
  });
}
