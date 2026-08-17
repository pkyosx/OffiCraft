// T-ee17 — the task title on a reply card must be the thing that shrinks.
//
// The card now names the task its question is about (owner 2026-08-14: a
// contractor's card said who was asking but not WHICH piece of work). Titles
// are free-length — real ones run past 40 characters — so the row needs one
// element that gives way. If none does, the row grows past the card and the
// 查看任務詳情 jump is pushed out of reach, or the card itself starts panning
// sideways.
//
// WHY A REAL BROWSER: this is layout. jsdom resolves no flex box and no
// intrinsic min-width, so `min-width: 0` / `flex: 1 1 auto` / `text-overflow`
// are all inert there — the jsdom tests in RepliesPage.test.tsx prove the title
// is RENDERED and say nothing about whether it FITS. Every assertion below is a
// measured rectangle; none of them can be satisfied by a class name or a CSS
// string, which is exactly the trap this repo keeps warning about.
//
// MUTANT (measured, not assumed): drop `overflow: hidden` from
// `.reply-card__task-title` in replies.css → the card overflows by 880px at
// 390 and 234px at 1040, and (1) goes red at both widths, naming the card.
//
// ⚠️ The FIRST mutant tried here was `min-width: 0`, and it was green — which
// is how we learned that declaration was doing nothing: a flex item's
// automatic minimum size is only `auto` while its overflow is `visible`, so
// `overflow: hidden` had already made the item shrinkable. The redundant
// declaration has since been deleted from the stylesheet. Picking the wrong
// mutant is how a guard gets certified against a no-op.
//
// The short-title control (3) stays green under the real mutant — it is there
// so "everything is clipped" cannot pass as a fix.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  ReplyTaskRefStory,
  ReplyCardLeadRowStory,
} from "./stories/ReplyTaskRefStory";

// Two widths on purpose: the owner reads the Ask page on both a phone and a
// desktop, and a title that fits at 1040 can still burst the row at 390.
const WIDTHS = [390, 1040];

for (const viewport of WIDTHS) {
  test(`${viewport}px: a long task title is clipped INSIDE its row, not past it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 720 });
    const cmp = await mount(<ReplyTaskRefStory />);

    const card = cmp.getByTestId("card-long");
    const row = card.getByTestId("reply-task-ref");
    const title = card.locator(".reply-card__task-title");
    const jump = card.getByTestId("reply-task-jump");
    await expect(title).toBeVisible();

    // (1) CORE red→green: nothing in the chain gets WIDER than the viewport.
    //
    // ⚠️ Measure the CHAIN, not just the row and the card. A row that refuses
    // to shrink simply GROWS, and the width it takes has to end up somewhere:
    // it can be absorbed by any ancestor that scrolls, so a pair of assertions
    // one and two levels up can both look innocent while the scroll pane and
    // the page get dragged wide behind them. Reaching all the way to
    // document.scrollingElement is what closes that gap.
    //
    // This comment used to claim the row-only shape "stayed green under the
    // mutant" — a witness with no teeth. RE-RUN ON THIS TREE (I copied the
    // spec, cut its assertions back to that first-version shape — row and card
    // only — and ran it twice): with the stylesheet untouched it is 2 passed;
    // with `overflow: hidden` dropped from `.reply-card__task-title` it FAILS
    // at both widths, and it fails on the ROW assertion first — row
    // scrollWidth - clientWidth is 894 at 390px and 252 at 1040px. So the
    // row-only judgement has teeth. The original green came from the mutant
    // that was tried against it — dropping `min-width: 0`, which the same
    // commit later established is a NO-OP under `overflow: hidden` — not from
    // any weakness in what was being measured.
    const pageOver = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth,
    );
    expect(pageOver, "the page must not pan sideways").toBeLessThanOrEqual(1);

    for (const [name, target] of [
      ["card", card],
      ["scroll pane", page.locator(".replies")],
    ] as const) {
      const box = await target.evaluate((el) => ({
        width: el.getBoundingClientRect().width,
        over: el.scrollWidth - el.clientWidth,
      }));
      expect(
        box.width,
        `${name} must stay within the viewport`,
      ).toBeLessThanOrEqual(viewport + 1);
      expect(
        box.over,
        `${name} must not overflow horizontally`,
      ).toBeLessThanOrEqual(1);
    }

    // (2) The title is the element that gave way — it is clipped, and the jump
    // button is still fully inside the row. Without this, (1) could be
    // satisfied by the jump button being the one that got squashed away.
    const titleOverflow = await title.evaluate(
      (el) => el.scrollWidth - el.clientWidth,
    );
    expect(
      titleOverflow,
      "the long title must be the clipped element",
    ).toBeGreaterThan(0);

    const rowBox = await row.boundingBox();
    const jumpBox = await jump.boundingBox();
    expect(rowBox && jumpBox).toBeTruthy();
    expect(
      jumpBox!.x + jumpBox!.width,
      "查看任務詳情 must stay inside the row",
    ).toBeLessThanOrEqual(rowBox!.x + rowBox!.width + 1);

    // (3) CONTROL — a title that fits is NOT clipped. This is what separates
    // "the long one is clipped" from "titles are always clipped"; it stays
    // green under the mutant, so it is not just a second copy of (1).
    const shortTitle = cmp
      .getByTestId("card-short")
      .locator(".reply-card__task-title");
    const shortOverflow = await shortTitle.evaluate(
      (el) => el.scrollWidth - el.clientWidth,
    );
    expect(
      shortOverflow,
      "a title that fits must be shown whole",
    ).toBeLessThanOrEqual(1);
  });
}

// T-ee17 acceptance round — the 任務資訊 row LEADS the card (owner 2026-08-14:
//「這個不能夠放到最一開始嗎？」). Moving a row changes layout, so the narrow
// widths get their own real-browser witness: the row now shares the top of the
// card with nothing above it, and it still has to fit and still has to clip its
// own title rather than push the card sideways.
//
// The DOM-order half of the contract is asserted in jsdom
// (RepliesPage.test.tsx / TasksPage.jump.test.tsx). Here it is asserted as
// GEOMETRY — where the owner's eye actually lands — because those are two
// different claims: CSS can paint a later element higher, and a DOM-order
// assertion cannot see that. Both halves mount the real component, so moving
// the row back below the summary reddens both.
const LEAD_WIDTHS = [320, 390];

for (const viewport of LEAD_WIDTHS) {
  test(`${viewport}px: the task row sits at the TOP of the card, above the summary`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 720 });
    const cmp = await mount(<ReplyCardLeadRowStory />);

    const card = cmp.getByTestId("chat-reply-card");
    const row = cmp.getByTestId("reply-task-ref");
    const summary = card.locator(".reply-card__summary");
    await expect(row).toBeVisible();
    await expect(summary).toBeVisible();

    // (1) CORE red→green: the row's whole box is ABOVE the summary's first
    // pixel. Measured, so "it is earlier in the DOM but painted below" cannot
    // pass here — and so a row moved back under the body fails at both widths.
    const rowBox = (await row.boundingBox())!;
    const summaryBox = (await summary.boundingBox())!;
    const cardBox = (await card.boundingBox())!;
    expect(
      rowBox.y + rowBox.height,
      "the task row must end before the summary starts",
    ).toBeLessThanOrEqual(summaryBox.y + 1);

    // (2) Nothing else is above it either: it is the card's first content, not
    // merely somewhere above the summary.
    expect(
      rowBox.y - cardBox.y,
      "the task row must be the first thing in the card",
    ).toBeLessThanOrEqual(rowBox.height + 2);

    // (3) The move must not have cost the row its fit. Same contract as the
    // truncation tests above, re-asserted in its new position and at the
    // narrowest width the cockpit supports: the title is the element that
    // gives way, and nothing pans sideways.
    const titleOverflow = await card
      .locator(".reply-card__task-title")
      .evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(
      titleOverflow,
      "the long title must still be the clipped element",
    ).toBeGreaterThan(0);

    const pageOver = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth,
    );
    expect(pageOver, "the page must not pan sideways").toBeLessThanOrEqual(1);
    const rowOver = await row.evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(rowOver, "the row must not overflow").toBeLessThanOrEqual(1);
  });
}
