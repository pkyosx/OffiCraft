// HOTSPOT — T-6630: 開備註與關備註都不改變捲動位置.
//
// Owner (2026-08-15, superseding T-4e39): 「我要整個畫面不移動,只是單純往下展開,
// 而收合時,就是向上收合,整個畫面不能移動」. T-4e39 had shipped the opposite
// bargain — a keepAnchored() correction that re-scrolled `.tasks` on OPEN so the
// clicked row kept its viewport y. Re-scrolling IS the screen moving, so that
// ticket deleted the correction (and src/lib/scrollAnchor.ts with it).
//
// Owner (2026-08-16, second acceptance round) then moved the note OUT of the
// card entirely:「備註不是很常按,可以放在 step 的右下角,點開再跳出另一個 Modal
// 打開嗎?」. The note now opens in the portalled `MarkdownPreviewOverlay`, so
// nothing in the column grows or shrinks when a note is read. The ruling above
// did not go away with it — it got STRICTER: with no reflow at all, there is no
// legitimate reason for `.tasks` to move by a single pixel, in either direction,
// at any scroll offset.
//
// ─────────────────────────────────────────────────────────────────────────────
// 🔴 WHAT RETIRED WITH THE OVERLAY REDESIGN, AND WHAT REPLACED IT
// (written out because a silently deleted assertion is indistinguishable from
// coverage that was quietly lost.)
//
//   RETIRED · "收合時下方那列上移量 = 消失的高度" (`rows["s-6"].top` moving by
//     exactly ±`removed`, in both the open and the collapse tests). It had a
//     SUBJECT: a note that opened INSIDE the step and pushed the rows under it
//     down by its own height. There is no such height any more — the note never
//     enters the column — so the assertion has nothing to measure and cannot be
//     rewritten, only replaced.
//     REPLACED BY: `maxScroll` and every measured row's `top`/`height` being
//     IDENTICAL across an open and across a close. That is the stronger form of
//     the same worry (「畫面不能移動」): the old assertion allowed the column to
//     reflow as long as the arithmetic added up; this one forbids the reflow.
//     It is also the assertion that catches a reader that was accidentally
//     rendered in-card again instead of through the portal.
//
//   RETIRED · the whole `tall` (備註比視窗高, `noteRepeat: 8`) half of the case
//     matrix, and the 「row taller than the scrollport」 non-vacuity checks that
//     went with it. Their point was that a note too tall to reveal forced the
//     old correction into its hardest branch. Note length no longer reaches the
//     layout at all, so at `noteRepeat: 8` the column is byte-for-byte the same
//     column — the case is a duplicate run, not a corner.
//     REPLACED BY: nothing, deliberately. The property it guarded (the reveal
//     clause of a scroll correction) is guarded directly by the strict equality
//     on `scrollTop`, which does not care how tall the note is.
//
//   RETIRED · the four 「collapsing at the end of the scroll range — the
//     browser's clamp is the only movement allowed」 tests, and the measured
//     clamp table in the old header (1280 短備註 815→683 forced −132, 390 短備註
//     902→680 forced −222, 1280 長備註 1359→683, 390 長備註 1399→680). The clamp
//     was a PHYSICAL consequence of the scrollable range SHRINKING when a note
//     folded away. Reading a note no longer changes the range, so the range
//     never shrinks, nothing is ever clamped, and those numbers describe a
//     layout that no longer exists. They are recorded here as retired rather
//     than left in place, because a stale measured table reads as evidence.
//     REPLACED BY: the `maxScroll` equality mentioned above — which is the
//     precondition the clamp needed, asserted directly. If the range ever starts
//     moving again, that assertion reddens BEFORE any clamp can bite.
//
//   KEPT AND RE-POINTED · everything about `.tasks` being the scrollport, about
//     Playwright's click scrolling an off-screen target into view, and the
//     strict (not approximate) equality on `scrollTop`.
// ─────────────────────────────────────────────────────────────────────────────
//
// WHAT THIS FILE ASSERTS NOW:
//   * `.tasks`' scrollTop is IDENTICAL across opening the reader and across
//     closing it. Not "close to" — exactly equal. That is the whole feature.
//   * the scrollport is the INNER container (`.tasks`) — measured on the live
//     site, `document.scrollHeight` equals the window height at every width, so
//     asserting on `document.scrollingElement` would be measuring a box that
//     NEVER scrolls and would stay green against ANY implementation, including
//     one that shoves `.tasks` by 500px. Every test proves that premise before
//     it measures anything.
//   * geometry, not a call log: nothing here checks whether scrollIntoView was
//     called. That API re-targets every scrollport on the ancestor chain, so its
//     absence is what the numbers below prove, not the reverse.
//   * a click is only a click if the button is already on screen. Playwright's
//     `.click()` SCROLLS AN OFF-SCREEN TARGET INTO VIEW FIRST, which moves the
//     scrollport by hundreds of px and has nothing to do with the component —
//     T-4e39's header reported "collapsing step 1 moves step 5 from 375 to 907"
//     and that number was exactly that harness scroll. So every entry this file
//     presses is asserted to be inside the scrollport first.
//
// MUTANT REGISTER — five mutants, each planted IN PLACE on the declaration
// named (not appended at the end of the file, which would be a different
// program), run against all three note guards, and observed. Counts below are
// this file's own (9 tests) and EXPIRE if the case list changes; re-plant and
// re-measure rather than editing the prose.
//   M1 · TaskCard.tsx — render the entry as a <div> instead of a <button>
//        ⇒ HERE 9 failed / 0 passed, all with the same message: "the step rows
//        are gone — the press collapsed the card instead of opening the
//        reader". A <div> is not exempt from onCardToggleClick's closest()
//        filter, so pressing 備註 folds the whole card and there is no column
//        left to measure. The property that is actually being broken (the press
//        must not collapse the card) is named and owned by
//        taskcard-note-entry.ct.spec.tsx, where M1 is 6 red.
//   M2 · tasks.css `.task-step__note-open` — delete `min-height: 44px`
//        ⇒ HERE 9 passed / 0 failed. The column gets shorter and everything
//        still holds still; the 44px floor is the entry guard's property
//        (2 red there, measured 29px). Registered so this green is not read as
//        touch-target coverage.
//   M3 · TaskCard.tsx, the note entry's onClick — open the reader AND shove the
//        scrollport: `const sc = document.querySelector(".tasks") as HTMLElement
//        | null; if (sc) sc.scrollTop += 120;` before `setNoteModal({…})`
//        ⇒ HERE 5 failed / 4 passed. Red: the four "opening the reader does not
//        move the scrollport" cases and the sequence test — 390 phone 712 → 832
//        (both themes), 1280 desktop 683 → 803 (both themes), sequence s-2
//        252 → 372. The four "closing" cases stay green, correctly: they take
//        their baseline AFTER the open, so the shove is already in it.
//   M4 · TaskCard.tsx, the overlay's `onClose` — shove on the way out instead:
//        same two lines with `-= 120`, before `setNoteModal(null)`
//        ⇒ HERE 5 failed / 4 passed. Red: the four "closing the reader does not
//        move the scrollport" cases and the sequence test — 390 phone 712 → 592,
//        1280 desktop 683 → 563, sequence s-2 252 → 132. The four "opening"
//        cases stay green, correctly — they never close anything.
//        🔴 M3 AND M4 TOGETHER ARE THE POINT: either one alone leaves half this
//        file green, which is what proves the open half and the close half are
//        separately owned rather than one riding on the other.
//        ⚠️ Both were first planted as `(document.querySelector(".tasks") as
//        HTMLElement | null)!.scrollTop += 120`, which THREW in the two guards
//        that mount bare. A mutant that crashes the component is not a mutant of
//        the property under test; the null-guarded form above is the honest one.
//   M5 · TaskCard.tsx — render the entry on every step (`{step.note && (` →
//        `{true && (`) ⇒ HERE 9 passed / 0 failed. Every step in this story has
//        a note anyway, so the mutant is a no-op for this fixture. It is the
//        disclosure guard's mutant (3 red there).
//
// 🔴 THIS FILE ALSO GUARDS THE OTHER DIRECTION. The same ticket's ③ ADDS a
// scroll correction — collapsing a whole task card brings that card back to the
// fold (taskcard-collapse-anchor.ct.spec.tsx). The two rulings are opposite on
// purpose and live in the same component, so if that correction ever leaks onto
// the note entry, the scrollTop assertions here are what reddens.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteAnchorStory } from "./stories/TaskCardNoteAnchorStory";

// The two widths the ticket measured, plus the two shipped themes. 390×844 is
// the owner's phone, 1280×800 the desktop window. (The 備註比視窗高 variants that
// used to double this list retired with the in-card note — see the header.)
const CASES = [
  { name: "390×844 phone · dark", w: 390, h: 844, theme: "dark" as const },
  { name: "390×844 phone · light", w: 390, h: 844, theme: "light" as const },
  { name: "1280×800 desktop · dark", w: 1280, h: 800, theme: "dark" as const },
  { name: "1280×800 desktop · light", w: 1280, h: 800, theme: "light" as const },
];

const ENTRY = "[data-testid='step-note-open']";

async function mountExpanded(mount: any, page: any, c: (typeof CASES)[number]) {
  await page.setViewportSize({ width: c.w, height: c.h });
  const cmp = await mount(<TaskCardNoteAnchorStory theme={c.theme} />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

/** The scrollport's state plus the viewport box of every step row asked for. */
async function measure(page: any, ids: string[]) {
  return page.evaluate((ids: string[]) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    if (!sc) return null;
    const doc = document.scrollingElement!;
    const rows: Record<string, { top: number; height: number }> = {};
    for (const id of ids) {
      const step = document.querySelector(
        `[data-step-id='${id}']`
      ) as HTMLElement | null;
      if (!step) return null;
      const r = step.getBoundingClientRect();
      rows[id] = { top: r.top, height: r.height };
    }
    const sr = sc.getBoundingClientRect();
    return {
      scrollTop: sc.scrollTop,
      maxScroll: sc.scrollHeight - sc.clientHeight,
      viewTop: sr.top,
      viewBottom: sr.top + sc.clientHeight,
      viewHeight: sc.clientHeight,
      docScroll: doc.scrollHeight - doc.clientHeight,
      rows,
    };
  }, ids);
}

/** `.tasks` is the box that scrolls and the document is not — the shape the
 * live site has. Without it every assertion below would be vacuous. */
function assertScrollportPremise(m: any, label: string) {
  expect(m, `${label}: the fixture must render the step rows`).not.toBeNull();
  expect(m.maxScroll, `${label}: \`.tasks\` must be the scrollport`).toBeGreaterThan(50);
  expect(
    m.docScroll,
    `${label}: the document must not be scrolling`
  ).toBeLessThanOrEqual(1);
}

/** Park at a scroll offset that is genuinely in the MIDDLE of the range, so
 * "scrollTop did not change" cannot be true for the wrong reason: at 0 an
 * upward shove is swallowed by the browser's own clamp, and at the maximum a
 * downward one is — either end would hide exactly the defect this file exists
 * to catch. The requested position is therefore pulled `EDGE` px inside both
 * ends (the last step cannot be put a third of the way down a column that ends
 * just below it), and the result is asserted to still have headroom both ways. */
const EDGE = 40;
async function park(page: any, stepId: string, frac: number) {
  await page.evaluate(
    ({ id, frac, edge }: { id: string; frac: number; edge: number }) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const step = document.querySelector(
        `[data-step-id='${id}']`
      ) as HTMLElement;
      if (!sc || !step) throw new Error(`park: sc=${!!sc} step(${id})=${!!step}`);
      const want = sc.getBoundingClientRect().top + sc.clientHeight * frac;
      const max = sc.scrollHeight - sc.clientHeight;
      const target = sc.scrollTop + step.getBoundingClientRect().top - want;
      sc.scrollTop = Math.min(Math.max(target, edge), max - edge);
    },
    { id: stepId, frac, edge: EDGE }
  );
  const m = await measure(page, [stepId]);
  expect(
    m!.scrollTop,
    `${stepId}: parked at the very top — a scrollport that cannot move up proves nothing`
  ).toBeGreaterThan(20);
  expect(
    m!.maxScroll - m!.scrollTop,
    `${stepId}: parked at the very end — a scrollport that cannot move down proves nothing`
  ).toBeGreaterThan(20);
}

/** An entry a user could not see is an entry a user could not press — and
 * Playwright would scroll it into view, which is a scrollport move this file
 * would then blame on the component. Every real click is gated on this. */
async function assertEntryOnScreen(page: any, stepId: string, label: string) {
  const box = await page.evaluate((id: string) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    const btn = document.querySelector(
      `[data-step-id='${id}'] [data-testid='step-note-open']`
    ) as HTMLElement;
    const r = btn.getBoundingClientRect();
    const sr = sc.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, viewTop: sr.top, viewBottom: sr.top + sc.clientHeight };
  }, stepId);
  expect(
    box.top,
    `${label}: the ${stepId} entry is above the scrollport — Playwright would scroll it into view and this test would measure the harness`
  ).toBeGreaterThanOrEqual(box.viewTop);
  expect(
    box.bottom,
    `${label}: the ${stepId} entry is below the fold — same problem`
  ).toBeLessThanOrEqual(box.viewBottom);
}

function pressEntry(cmp: any, stepId: string) {
  return cmp.locator(`[data-step-id='${stepId}'] ${ENTRY}`).click();
}

/** Nothing in the column may have reflowed: same range, same rows, same boxes.
 * This is what replaced the old "the row below rose by exactly the note's
 * height" family — the note is not in the column at all any more, so the
 * correct claim is that the column is untouched. */
function assertNoReflow(before: any, after: any, ids: string[], label: string) {
  expect(
    after.maxScroll,
    `${label}: the scrollable range changed (${before.maxScroll} → ${after.maxScroll}) — something reflowed the column`
  ).toBeCloseTo(before.maxScroll, 0);
  for (const id of ids) {
    expect(
      after.rows[id].top,
      `${label}: ${id} moved (${before.rows[id].top} → ${after.rows[id].top})`
    ).toBeCloseTo(before.rows[id].top, 0);
    expect(
      after.rows[id].height,
      `${label}: ${id} changed height (${before.rows[id].height} → ${after.rows[id].height})`
    ).toBeCloseTo(before.rows[id].height, 0);
  }
}

/** `measure` returns null when a row it was asked for has vanished — which is
 * what happens when the press collapsed the whole card instead of opening the
 * reader. Named, so that failure reads as itself and not as a TypeError. */
function assertRowsSurvived(m: any, label: string) {
  expect(
    m,
    `${label}: the step rows are gone — the press collapsed the card instead of opening the reader`
  ).not.toBeNull();
}

const ROWS = ["s-5", "s-6"];

for (const c of CASES) {
  test(`[${c.name}] opening the reader does not move the scrollport`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, c);
    await park(page, "s-5", 0.3);

    const before = await measure(page, ROWS);
    assertScrollportPremise(before, c.name);
    await assertEntryOnScreen(page, "s-5", c.name);

    await pressEntry(cmp, "s-5");

    // NON-VACUITY: the reader really did open. Without this the equalities
    // below would pass on a button that does nothing at all.
    await expect(page.locator(".md-preview")).toBeVisible();
    await expect(page.locator(".md-preview")).toContainText("第 5 步做到哪");

    const after = await measure(page, ROWS);
    assertRowsSurvived(after, `${c.name}: open`);
    // ① THE FEATURE: the scrollport did not move. Exactly, not approximately.
    expect(
      after!.scrollTop,
      `${c.name}: opening the reader scrolled \`.tasks\` (${before!.scrollTop} → ${after!.scrollTop})`
    ).toBe(before!.scrollTop);
    // ② …and it did not move because nothing under it moved either: the reader
    //    is a portal, so the column it left behind is the column it found.
    assertNoReflow(before, after, ROWS, `${c.name}: open`);
  });

  test(`[${c.name}] closing the reader does not move the scrollport`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, c);
    await park(page, "s-5", 0.3);
    await assertEntryOnScreen(page, "s-5", c.name);
    await pressEntry(cmp, "s-5");
    await expect(page.locator(".md-preview")).toBeVisible();

    // Measured with the reader OPEN — so this test owns the close on its own
    // and cannot pass by inheriting the open half's answer.
    const before = await measure(page, ROWS);
    assertScrollportPremise(before, c.name);

    await page.locator(".md-preview__close").click();
    await expect(page.locator(".md-preview")).toHaveCount(0);

    const after = await measure(page, ROWS);
    assertRowsSurvived(after, `${c.name}: close`);
    expect(
      after!.scrollTop,
      `${c.name}: closing the reader scrolled \`.tasks\` (${before!.scrollTop} → ${after!.scrollTop})`
    ).toBe(before!.scrollTop);
    assertNoReflow(before, after, ROWS, `${c.name}: close`);
  });
}

test("[390×844 phone · dark] three notes read one after another never move the scrollport", async ({
  mount,
  page,
}) => {
  // The owner's follow-up shape: three notes down the column, read one after
  // another. Under T-4e39 every one of these presses re-scrolled the container;
  // the point of the sequence is that now neither the opening nor the closing of
  // any of them does, and that reading one note does not leave the next one
  // displaced.
  //
  // 🔴 節點 8, NOT 節點 9 (which the pre-overlay version of this test used).
  // With every note out of the column, the column is short enough that 節點 9's
  // entry — the last thing in it — is only fully on screen at the very END of
  // the scroll range. There, a downward shove is swallowed by the browser's own
  // clamp and "scrollTop did not change" would be true no matter what the
  // component did. MEASURED: `.tasks` maxScroll 923, s-9's entry bottom reaches
  // the fold at scrollTop 883.6 of a 923 maximum, i.e. 39px of headroom. 節點 8
  // parks with room on both sides and keeps the assertion's power.
  const cmp = await mountExpanded(mount, page, CASES[0]);
  for (const id of ["s-2", "s-5", "s-8"]) {
    const label = `${CASES[0].name} · ${id}`;
    await park(page, id, 0.3);
    const before = await measure(page, [id]);
    assertScrollportPremise(before, label);
    await assertEntryOnScreen(page, id, label);

    await pressEntry(cmp, id);
    await expect(page.locator(".md-preview")).toBeVisible();
    const opened = await measure(page, [id]);
    assertRowsSurvived(opened, `${label}: open`);
    expect(
      opened!.scrollTop,
      `${label}: opening scrolled \`.tasks\` (${before!.scrollTop} → ${opened!.scrollTop})`
    ).toBe(before!.scrollTop);
    assertNoReflow(before, opened, [id], `${label}: open`);

    await page.locator(".md-preview__close").click();
    await expect(page.locator(".md-preview")).toHaveCount(0);
    const closed = await measure(page, [id]);
    assertRowsSurvived(closed, `${label}: close`);
    expect(
      closed!.scrollTop,
      `${label}: closing scrolled \`.tasks\` (${opened!.scrollTop} → ${closed!.scrollTop})`
    ).toBe(opened!.scrollTop);
    assertNoReflow(opened, closed, [id], `${label}: close`);
  }
});
