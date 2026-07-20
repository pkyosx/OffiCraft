// HOTSPOT — 任務卡自由文字長 token 溢出 (T-4974).
//
// Bug (owner, iPhone screenshot 2026-07-19): a task card's baton/description
// text carried an unbreakable long token
// (`twin(desired_state/desired_machine_id/refocus_since/bank_balance/...)`).
// `.task-card__desc.doc-md` / `.task-step__dod .doc-md` / the waiting reason's
// `.doc-md` declared NO overflow-wrap, so the token set the card's min-content
// to its full width, pushed the card past the 390px viewport and the whole PAGE
// gained a horizontal scrollbar — the owner could drag the page left/right.
// Fix: `overflow-wrap: anywhere` on those three free-text surfaces (anywhere,
// not break-word, so min-content shrinks and the card binds to the viewport).
//
// T-c514: the waiting reason's surface used to be `.task-card__waiting-md`;
// that task-level block was removed as a duplicate, so this guard now measures
// the reason where it actually renders — `.task-step__waiting-md`, which
// carries its own `overflow-wrap: anywhere` and is the same class of bug.
// Retargeted rather than dropped: leaving the dead selector in the list would
// have gone VACUOUS, not red (assertion (2) scores a missing element as -1,
// which passes `toBeLessThanOrEqual(1)`), silently retiring a third of the
// guard. The story's step was given the long token to match.
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
    ".task-step__waiting-md",
  ]) {
    const over = await page.evaluate((s: string) => {
      const el = document.querySelector(s) as HTMLElement | null;
      return el ? el.scrollWidth - el.clientWidth : -2;
    }, sel);
    // -2 = the surface never rendered. Scored as a FAILURE, not a pass: the
    // old sentinel (-1) satisfied `toBeLessThanOrEqual(1)`, so a selector that
    // rotted or a fixture that stopped producing the surface would retire this
    // assertion in silence — exactly the trap T-c514 walked into when the
    // task-level waiting row was deleted out from under this list.
    //
    // T-d451 raised the stakes: it removed the per-surface `overflow-wrap`
    // rules from tasks.css, so the surfaces below now inherit the fix from the
    // `.doc-md` base (settings.css) and THIS guard is the only thing measuring
    // that the inheritance actually reaches them.
    expect(over, `[${width}px] ${sel} missing (never rendered)`).not.toBe(-2);
    expect(over, `[${width}px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }

  // (3) T-c21e — the dep row's NON-VACUITY sentinels. The row is flex-NOWRAP
  // and only `.task-card__dep-title` may give way (min-width: 0); the new 狀態
  // badge, ⏱/✓, 「等 T-xxxx」 and ↗ are all `flex: none; nowrap` and form a
  // hard floor. Adding the badge raised that floor days after 1ea673e stopped
  // the phone scrolling sideways, so the row belongs under this guard.
  //
  // A per-row `scrollWidth - clientWidth` check IS asserted below, and the
  // story of why is worth keeping.
  //
  // An earlier draft of this file deleted that check and justified the
  // deletion in a comment beginning "Verified:" — claiming the row's own
  // overflow number was structurally pinned at 0 because the row is
  // `width: 100%` of a card that grows with it. Review re-ran the same
  // mutant. The claim was false: with `min-width: 600px` on
  // `.task-card__dep-status` the row reports rowOverflow=367 (rowW=358) and
  // the per-row check fails loudly. The structural argument was wrong too —
  // at 390px the card is clamped by the viewport, so clientWidth tops out at
  // 358 rather than following the content.
  //
  // So the check was never fake protection; it was working protection deleted
  // on a measurement nobody took. It is restored. Recorded at length because
  // the sentence was doing real damage: a confident "Verified:" is exactly
  // what stops the next reader from re-measuring, and this file's whole
  // header is about assertions that quietly go vacuous.
  //
  // It buys REAL detection, and the case that proves it is the one someone
  // will actually write. Under the plain 600px mutant both this check and (1)
  // fail, and (1) is asserted first — which is what an earlier revision of
  // this comment saw before concluding, wrongly, that this check could only
  // ever localise what (1) already caught. The argument offered for that was
  // "the dep row is inside the card, so a row that cannot fit bursts the page
  // too". Measured at 390px, it is false:
  //
  //   mutant                                    (1)      per-row
  //   badge min-width:600px                     +351 ✗   367 ✗
  //   + .task-card { overflow: hidden }            0 ✓   367 ✗
  //   + .task-card__deps { overflow: hidden }      0 ✓   367 ✗
  //
  // Adding `overflow: hidden` to a container is the most common way anyone
  // "fixes" a page that scrolls sideways. It turns (1) green while the layout
  // is still broken — worse than before, because the content is now CLIPPED
  // rather than reachable by scrolling. That regression is invisible to (1)
  // and visible only here.
  //
  // Note the shape of the mistake, since this file keeps attracting it: the
  // first version of this block deleted a working guard by claiming a
  // measurement it never took; the second restored it but talked it down with
  // an unmeasured structural argument. Both were reasoning where a number was
  // available. If the next reader wants to weaken this check, take the
  // measurement.
  //
  // Known slack, measured and left alone: assertion (1) needs roughly +160px
  // of extra badge width before it trips at 390px (a min-width sweep found
  // 60/90/120/160 all green, 600 red). It is a cliff detector, not a
  // crowding detector. Not tightened here — the threshold is shared with
  // other tickets' surfaces and narrowing it is their call, not this row's.
  //
  // The non-vacuity sentinels below are still owed: they prove (1) and the
  // per-row check were measuring a dep row that actually had a badge on it —
  // otherwise a fixture drift retires the dep coverage in silence, exactly
  // the trap T-c514 walked into.
  const depSel = ".task-card__waiting--dep";
  const dep = await page.evaluate((s: string) => {
    const el = document.querySelector(s);
    if (!el) return { present: false, badge: false, overflow: 0 };
    return {
      present: true,
      badge: !!el.querySelector('[data-testid="task-dep-status"]'),
      // Restored (see the note above): this DOES go red — measured 367 under
      // a 600px-min-width mutant, not the 0 an earlier comment claimed.
      overflow: el.scrollWidth - el.clientWidth,
    };
  }, depSel);
  expect(dep.present, `[${width}px] ${depSel} missing (never rendered)`).toBe(
    true
  );
  expect(
    dep.badge,
    `[${width}px] dep row rendered WITHOUT its 狀態 badge — (1) is no longer measuring what T-c21e added`
  ).toBe(true);
  expect(
    dep.overflow,
    `[${width}px] the dep row itself overflows by ${dep.overflow}px — the badge + ⏱ + 編號 + ↗ floor no longer fits`
  ).toBeLessThanOrEqual(1);
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
