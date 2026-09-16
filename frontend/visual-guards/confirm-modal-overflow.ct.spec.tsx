// HOTSPOT (T-192) — the shared confirm dialog must stay READABLE on a short
// window, because the only reason a confirm body exists is to be read before an
// irreversible action.
//
// 🔴 THE FIXTURE IS THE GUARD'S OWN. This file used to mount the real
// `forceDoneConfirmBody` copy, on the reasoning that the defect was a property
// of that paragraph's height (499.2px en / 394.6px zh, measured in a real
// browser). Owner ruling rc-bdfd2fc07305 then cut that body to one sentence,
// which FITS — and a body that fits makes "the box must scroll" a claim about
// nothing. The subject here is the shared shell's behaviour when any caller
// passes more content than the window holds, so the story now owns an over-tall
// fixture and no product string can shorten this guard into vacuity.
//
// THE DEFECT, measured in a real browser before the fix: `.confirm-modal` is
// `position: fixed` + `align-items: center`, and NEITHER it nor
// `.confirm-modal__box` declared any `overflow-y`. The box's height comes only
// from its content and does not track the viewport, so on a window shorter than
// that the box is centred THROUGH the scrim and the TOP of the paragraph sits
// above y=0 — with `scrollHeight - clientHeight === 0` on the box AND on the
// page, i.e. nothing anywhere can scroll it back. The buttons stay clickable
// down to ~397px, so the danger is silent: the dialog looks operable while the
// body it carries is off-screen.
//
// WHY CT AND NOT jsdom: every term in that sentence is layout. jsdom applies no
// layout engine — `scrollHeight`/`clientHeight` are 0, `getBoundingClientRect()`
// is all zeroes, and a stylesheet's `max-height` is never resolved. A vitest
// test asserting "the body is visible" passes identically with and without the
// fix. This is exactly the blind spot that let the defect ship: before this
// file there was NO height guard on this dialog at all.
//
// WHAT EACH TEST HAS DISCRIMINATING POWER OVER:
//   ① "scrolls at a short window"  → the fix's two halves. Drop `overflow-y`
//     and `scrollHeight - clientHeight` goes back to 0; drop the `max-height`
//     and the box grows past the scrim so the same subtraction is 0 again AND
//     the top-of-body assertion goes negative. Either mutant is red.
//   ② "content that fits is untouched" → the no-regression arm for the twelve
//     OTHER callers of this shared shell. If a future "fix" caps the box at a
//     constant (`max-height: 300px`) or forces `overflow-y: scroll`, a one-line
//     delete confirm grows a scrollbar / a scroll range and this goes red.
//
// MUTANT (RUN, recorded in the ticket): delete `max-height: 100%` +
// `max-height: calc(100dvh - 40px)` + `overflow-y: auto` from
// `.confirm-modal__box` → ① red, ② still green.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  ConfirmModalOverflowStory,
  ConfirmModalShortStory,
} from "./stories/ConfirmModalOverflowStory";

const BOX = ".confirm-modal__box";

/** Short enough that the story's fixture body cannot fit, and still wide/tall
 * enough to be a window a person really uses (a half-height laptop split, a
 * landscape phone). */
const SHORT = { width: 900, height: 420 };

test("the confirm box scrolls, and its body starts on screen, at a short window", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(SHORT);
  const cmp = await mount(<ConfirmModalOverflowStory />);

  const box = cmp.locator(BOX);
  await expect(box).toBeVisible();

  const m = await box.evaluate((el) => ({
    scrollH: el.scrollHeight,
    clientH: el.clientHeight,
    rectTop: el.getBoundingClientRect().top,
    rectBottom: el.getBoundingClientRect().bottom,
    overflowY: getComputedStyle(el).overflowY,
  }));

  // PREMISE FIRST: this content must really be taller than the window, or
  // "it scrolls" would be trivially true and this guard would be theatre. If
  // the copy is ever shortened past this point the guard fails LOUDLY here
  // rather than passing vacuously.
  expect(
    m.scrollH,
    "the fixture body must exceed the viewport, else this guard proves nothing"
  ).toBeGreaterThan(SHORT.height);

  // ① THE FIX: there is a real scroll range on the box itself.
  expect(m.overflowY, "the box must be the scrolling element").not.toBe(
    "visible"
  );
  expect(
    m.scrollH - m.clientH,
    "the box must have somewhere to scroll to — without this the top of the body is unreachable"
  ).toBeGreaterThan(0);

  // ② AND the box is inside the window, so the FIRST line of the paragraph is
  // on screen at rest. This is the half `overflow-y` alone does not give you:
  // an uncapped box centred through the scrim starts above y=0.
  expect(
    m.rectTop,
    "the top of the dialog must not be above the top of the window"
  ).toBeGreaterThanOrEqual(-1);
  expect(
    m.rectBottom,
    "the bottom of the dialog must not be below the bottom of the window"
  ).toBeLessThanOrEqual(SHORT.height + 1);

  // The paragraph's own first pixel row, not just the box's: the copy element
  // must begin inside the visible slice of the box.
  const copyTop = await cmp
    .locator('[data-testid="overflow-copy"]')
    .evaluate((el) => el.getBoundingClientRect().top);
  expect(
    copyTop,
    "the body paragraph must START on screen, not above it"
  ).toBeGreaterThanOrEqual(m.rectTop - 1);
  expect(copyTop, "…and inside the window").toBeLessThan(SHORT.height);

  // And the scroll range is real, not a number the engine merely reports:
  // scrolling to the end must actually move the box.
  const after = await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
    return el.scrollTop;
  });
  expect(
    after,
    "the browser must accept a real scroll offset on this box"
  ).toBeGreaterThan(0);

  // With the box scrolled to the end, the ACTIONS row is on screen — the
  // reader can reach the decision after reading the body.
  const actions = await cmp
    .locator(".confirm-modal__actions")
    .evaluate((el) => el.getBoundingClientRect());
  expect(actions.bottom, "the action row must be reachable").toBeLessThanOrEqual(
    SHORT.height + 1
  );
});

test("a confirm whose content fits is untouched — no cap, no scrollbar", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(SHORT);
  const cmp = await mount(<ConfirmModalShortStory />);

  const box = cmp.locator(BOX);
  await expect(box).toBeVisible();

  const m = await box.evaluate((el) => ({
    scrollH: el.scrollHeight,
    clientH: el.clientHeight,
    offsetW: (el as HTMLElement).offsetWidth,
    clientW: el.clientWidth,
    rectTop: el.getBoundingClientRect().top,
    rectBottom: el.getBoundingClientRect().bottom,
  }));

  // Sanity: this arm must be the SHORT case, or it is measuring the same thing
  // as the test above.
  expect(
    m.scrollH,
    "the short fixture must fit inside the window"
  ).toBeLessThan(SHORT.height);

  expect(
    m.scrollH - m.clientH,
    "a dialog that fits must have nothing to scroll"
  ).toBe(0);
  // `overflow-y: auto` paints no scrollbar when there is no overflow; a
  // `scroll` would steal width here and this catches it.
  expect(
    m.offsetW - m.clientW,
    "no scrollbar gutter may appear on a dialog that fits"
  ).toBeLessThanOrEqual(2);
  // Still vertically centred, exactly as before the fix.
  const topGap = m.rectTop;
  const bottomGap = SHORT.height - m.rectBottom;
  expect(
    Math.abs(topGap - bottomGap),
    "a dialog that fits must stay centred"
  ).toBeLessThanOrEqual(2);
});
