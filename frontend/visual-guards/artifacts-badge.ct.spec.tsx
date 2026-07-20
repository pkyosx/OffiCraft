// T-3dc5 artifact set — real-browser layout guards the jsdom suite can't see:
// the 「產物 N」 badge occupies a visible box in the badge row, its popover opens
// with a laid-out list that stays inside the viewport, and the empty-set case
// renders NO badge box at all (the design's load-bearing negative, proven in
// real layout — not just DOM absence).
//
// T-49fb reshaped this file twice over: the popover lost its 檔案/圖片/連結
// tabs (one list now), and it gained a page-overflow contract + click-outside
// dismissal.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  TaskCardArtifactsStory,
  TaskCardNoArtifactsStory,
  TaskCardRaggedArtifactsStory,
  TaskCardSameNameArtifactsStory,
} from "./stories/TaskCardArtifactsStory";
import { TaskArtifactsRightEdgeStory } from "./stories/TaskArtifactsRightEdgeStory";
import { TaskArtifactsOverflowStory } from "./stories/TaskArtifactsOverflowStory";

test("desktop 1024: 產物 badge is visible and opens a laid-out popover listing every kind", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);

  const badge = cmp.getByTestId("task-artifacts-badge");
  await expect(badge).toBeVisible();
  const badgeBox = await badge.boundingBox();
  expect(badgeBox, "badge must have a layout box").not.toBeNull();
  // Not collapsed: the count badge is a real, tappable chip.
  expect(badgeBox!.height).toBeGreaterThanOrEqual(16);
  expect(badgeBox!.width).toBeGreaterThan(24);
  await expect(badge).toContainText("3");

  await badge.click();
  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();

  // T-49fb: NO tabs — and all three kinds are on screen at once, which is the
  // whole point of dropping them. A mutant that restores the tabbed body shows
  // only the 檔案 rows and reddens the row count.
  await expect(cmp.locator(".task-artifacts__tab")).toHaveCount(0);
  await expect(cmp.getByRole("tab")).toHaveCount(0);
  await expect(cmp.locator(".task-artifacts__item")).toHaveCount(3);

  const popBox = await popover.boundingBox();
  expect(popBox).not.toBeNull();
  expect(popBox!.height).toBeGreaterThan(60);
  expect(popBox!.x).toBeGreaterThanOrEqual(0);
  expect(popBox!.x + popBox!.width).toBeLessThanOrEqual(1024 + 1);

  // The .md file row's chip IS the preview trigger now (T-7bc2): a <button>
  // whose accessible name is the visible filename text, not a separate
  // 眼睛 button.
  await expect(cmp.getByRole("button", { name: "design.md" })).toBeVisible();
});

test("desktop 1024: the three kinds keep their visual distinction inside the one list", async ({ mount, page }) => {
  // Dropping the tabs must NOT flatten the kinds. Each row still announces what
  // it is by its leading mark: an image by its 44px thumbnail, a link by the
  // anchor chip that navigates, a file by a plain download chip.
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  // Image: a real thumbnail box, not a text chip.
  const thumb = cmp.locator(".task-artifacts__thumb");
  await expect(thumb).toHaveCount(1);
  const thumbBox = await thumb.boundingBox();
  expect(thumbBox!.width).toBeGreaterThanOrEqual(40);
  expect(thumbBox!.height).toBeGreaterThanOrEqual(40);

  // Link: the only row whose chip is an <a class="task-artifacts__link">.
  await expect(cmp.locator("a.task-artifacts__link")).toHaveCount(1);

  // Rows all sit on ONE rhythm even though they arrive from two renderers.
  // Measure the GAP (previous bottom → next top), not top-to-top: the rows
  // have different heights (the image row is as tall as its 44px thumbnail),
  // so a top-to-top delta would vary for a perfectly even list.
  //
  // The seam that matters is file/image → link, which crosses the renderer
  // boundary. The mutant this catches is `.task-artifacts__body` losing its
  // flex column: the seam then collapses to 0 while the in-group gaps stay
  // 6px. (Measured: swapping `display: contents` on the two wrappers for their
  // own flex columns renders identically and does NOT redden this — the body's
  // gap already spaces them. See the note in tasks.css.)
  const rects = await cmp
    .locator(".task-artifacts__item")
    .evaluateAll((els) =>
      els.map((el) => {
        const r = el.getBoundingClientRect();
        return { top: r.top, bottom: r.bottom };
      }),
    );
  expect(rects.length).toBe(3);
  const gaps = rects.slice(1).map((r, i) => r.top - rects[i].bottom);
  for (const g of gaps) expect(Math.abs(g - gaps[0])).toBeLessThanOrEqual(1.5);
  // …and the rhythm is the list's real gap, not a collapsed 0.
  expect(gaps[0]).toBeGreaterThan(3);
});

test("narrow 390: popover stays within the phone viewport", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();
  const box = await popover.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390 + 1);
});

test("narrow 390: popover stays in-viewport even when its badge is pinned to the right edge (T-2ca0)", async ({ mount, page }) => {
  // The T-2ca0 bug: 產物 is the rightmost badge, so on a phone its anchor sits
  // far right; the then-current absolute/left:0 + fixed-width popover spilled
  // past the right edge. This story forces that anchor position, which the
  // pre-existing 390 guard never did (its badge wrapped to the left).
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<TaskArtifactsRightEdgeStory />);
  await cmp.getByTestId("task-artifacts-badge").click();

  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();
  const box = await popover.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(390 + 1);
});

// ── T-49fb ① — the sideways-scroll contract ────────────────────────────────
// The owner's report was NOT "the panel is clipped" (T-2ca0 fixed that) but
// "the whole page scrolls sideways and the left chips are cut off". Measured at
// 375px in the real app, the popover's box (left = card content edge = 37px,
// width = a flat 340px) put its right edge at 377 > 375; the surplus climbed
// the card's padding and landed on `.tasks`, whose overflow-y:auto forces
// overflow-x:auto — so the LIST grew a horizontal scrollbar and swiping it
// carried #T-23cf / 自由代辦 off the left edge.
//
// The contract is two-way on purpose: nothing may scroll sideways, AND the
// panel must stay a usable width. A one-way "no overflow" assertion would go
// green on a popover clamped to 0.
test("narrow 375: opening the popover grows NO horizontal scroll anywhere (T-49fb)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  const cmp = await mount(<TaskArtifactsOverflowStory />);

  const overflow = () =>
    page.evaluate(() => {
      const de = document.documentElement;
      const list = document.querySelector(".tasks") as HTMLElement | null;
      const card = document.querySelector(".task-card") as HTMLElement | null;
      // -2 is the never-rendered sentinel, matching the repo spelling in
      // taskcard-longtoken-wrap.ct.spec.tsx (T-c514) so both guards grep alike.
      //
      // Measured, so nobody over-trusts the number: the VALUE buys no detection
      // power on its own. -2 passes `toBeLessThanOrEqual(1)` just as -1 did, so
      // swapping -1→-2 does not by itself survive someone relaxing the exact
      // assertions below to a tolerance (verified: rotted selector + relaxed
      // assertions + no `missing` check ⇒ green under BOTH sentinels).
      // What actually makes this guard non-vacuous is the explicit `missing`
      // boolean, asserted on both sides below — the same role T-c514's
      // companion `.not.toBe(-2)` line plays there. Keep that assertion; the
      // sentinel is a readability convention, not the safety net.
      if (!list || !card) return { missing: true, page: -2, tasks: -2, card: -2 };
      return {
        missing: false,
        page: de.scrollWidth - de.clientWidth,
        tasks: list.scrollWidth - list.clientWidth,
        card: card.scrollWidth - card.clientWidth,
      };
    });

  // Surfaces must EXIST — a renamed class must report missing, not pass quietly.
  const before = await overflow();
  expect(before.missing, ".tasks / .task-card surfaces must exist").toBe(false);
  expect(before.page).toBe(0);
  expect(before.tasks).toBe(0);

  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  const after = await overflow();
  // Re-checked on BOTH sides, not just `before`: the surfaces are re-queried
  // after the click, so a change that unmounts the card while the popover is
  // open would otherwise reach the `toBe(0)` lines with sentinel values.
  expect(after.missing, ".tasks / .task-card surfaces must still exist").toBe(false);
  expect(after.page, "documentElement must not scroll sideways").toBe(0);
  expect(after.tasks, ".tasks must not scroll sideways").toBe(0);
  expect(after.card, ".task-card must not scroll sideways").toBe(0);

  // …and the panel is still a panel: inside the card, and wide enough to read.
  const pop = (await cmp.locator(".task-artifacts").boundingBox())!;
  const card = (await cmp.locator(".task-card").boundingBox())!;
  expect(pop.width).toBeGreaterThanOrEqual(240);
  expect(pop.x).toBeGreaterThanOrEqual(card.x - 1);
  expect(pop.x + pop.width).toBeLessThanOrEqual(card.x + card.width + 1);

  // The long branch label is truncated by T-90df's ellipsis, not by widening
  // the row — the chip stays inside the panel.
  const chipRight = await cmp
    .locator(".task-artifacts__chip")
    .first()
    .evaluate((el) => el.getBoundingClientRect().right);
  expect(chipRight).toBeLessThanOrEqual(pop.x + pop.width + 1);
});

// ── T-49fb ③ — click-outside dismissal ─────────────────────────────────────
test("click outside closes the popover; clicks inside and on the badge do not (T-49fb)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  const badge = cmp.getByTestId("task-artifacts-badge");
  const popover = cmp.locator(".task-artifacts");

  await badge.click();
  await expect(popover).toBeVisible();

  // Inside → stays open. The header is inert chrome, so this isolates the
  // dismissal logic from any row's own click handler.
  await cmp.locator(".task-artifacts__header").click();
  await expect(popover).toBeVisible();

  // Outside → closes.
  await page.mouse.click(900, 700);
  await expect(popover).toHaveCount(0);

  // The badge must still OPEN on the next click. This is the classic bug in
  // this pattern: if the outside-click listener treated the trigger as
  // "outside", mousedown would close and the click would re-open (or the two
  // would race and the panel would never appear).
  await badge.click();
  await expect(popover).toBeVisible();
  await badge.click();
  await expect(popover).toHaveCount(0);
});

test("Esc closes the popover (T-49fb)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(cmp.locator(".task-artifacts")).toHaveCount(0);
});

test("opening the image Lightbox does not dismiss the popover (T-49fb)", async ({ mount, page }) => {
  // The portal trap: if the Lightbox / MarkdownPreviewOverlay were portalled to
  // <body>, a `contains()`-based outside check would call them "outside" and
  // clicking a thumbnail or 預覽 would kill the popover under them. They render
  // INSIDE the popover subtree — this pins that, so a future refactor to a
  // portal reddens here instead of silently regressing.
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  const popover = cmp.locator(".task-artifacts");
  await expect(popover).toBeVisible();

  await cmp.locator(".task-artifacts__thumb").click();
  await expect(cmp.locator(".chat__lightbox")).toBeVisible();
  await expect(popover).toBeVisible();

  // Dismissing the Lightbox by its own backdrop leaves the popover standing.
  await cmp.locator(".chat__lightbox").click({ position: { x: 5, y: 5 } });
  await expect(cmp.locator(".chat__lightbox")).toHaveCount(0);
  await expect(popover).toBeVisible();

  // Same contract for the .md 預覽 overlay — the other in-popover overlay.
  // T-7bc2: the file chip itself is the trigger now (no separate 眼睛 button).
  await cmp.getByRole("button", { name: "design.md" }).click();
  await expect(cmp.locator(".md-preview")).toBeVisible();
  await expect(popover).toBeVisible();
});

// T-90df — the alignment guard. This is the ONLY layer that can prove the
// owner's requirement: jsdom computes no layout, so the vitest suite can assert
// the markup and the title= but never that the chips came out equal width or
// that the buttons line up. Reverting the CSS (chip back to sizing with its
// text) reddens this and nothing else.
test("desktop 1024: a short and an overlong name give EQUAL chip widths and one action column", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardRaggedArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  // The two FILE rows (the short name and the overlong one) lead the list.
  const chips = cmp.locator(".task-artifacts__item .task-artifacts__chip");
  const chipBoxes = await chips.evaluateAll((els) =>
    els.slice(0, 2).map((el) => el.getBoundingClientRect().width),
  );
  // Chip width must be name-INDEPENDENT: the short and long rows agree.
  expect(chipBoxes.length).toBe(2);
  expect(Math.abs(chipBoxes[0] - chipBoxes[1])).toBeLessThanOrEqual(1);

  // …and the CHIP itself stays inside the panel (the ellipsis did its job).
  // Measure the chip, NOT the row: `.task-artifacts__item` is a block-level
  // flex container whose width is parent-determined, so an overflowing child
  // never widens its rect — asserting on the row would pass even when a long
  // chip spills hundreds of px past the panel edge.
  const panel = (await cmp.locator(".task-artifacts").boundingBox())!;
  const chipRights = await chips.evaluateAll((els) =>
    els.map((el) => el.getBoundingClientRect().right),
  );
  for (const right of chipRights) {
    expect(right).toBeLessThanOrEqual(panel.x + panel.width + 1);
  }

  // The trailing action columns start at the SAME x — that is 「動作鈕垂直
  // 對齊成一欄」 in measurable form. Compared between the two FILE rows only:
  // an image row legitimately starts its actions 44px+gap further right
  // (the thumbnail sits outside the chip), which is why the cross-kind
  // contract is stated on the RIGHT edge in the next test, not the left.
  const actionLefts = await cmp
    .locator(".task-artifacts__item .task-artifacts__actions")
    .evaluateAll((els) =>
      els.slice(0, 2).map((el) => el.getBoundingClientRect().left),
    );
  expect(actionLefts.length).toBe(2);
  expect(Math.abs(actionLefts[0] - actionLefts[1])).toBeLessThanOrEqual(1);
});

test("desktop 1024: every kind puts its action column on the same right edge", async ({ mount, page }) => {
  // Cross-kind consistency (T-90df requirement ①). Chip widths differ by the
  // 44px thumbnail on an image row — what must agree is the RIGHT edge the
  // action buttons flush to, since that is what the eye reads as 「對齊」.
  // Pre-T-49fb this needed three tab clicks; the one list makes it one shot.
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardRaggedArtifactsStory />);
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();

  const rights = await cmp
    .locator(".task-artifacts__item .task-artifacts__actions")
    .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().right));
  expect(rights.length).toBeGreaterThanOrEqual(3);
  for (const right of rights) {
    expect(Math.abs(right - rights[0])).toBeLessThanOrEqual(1);
  }

  // Every kind renders a chip whose full name is on title= (requirement ②).
  const titles = await cmp
    .locator(".task-artifacts__item .task-artifacts__chip")
    .evaluateAll((els) => els.map((el) => el.getAttribute("title") ?? ""));
  expect(titles.length).toBeGreaterThanOrEqual(3);
  for (const title of titles) expect(title.length).toBeGreaterThan(0);
});

test("empty set: NO 產物 badge renders (the load-bearing negative)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<TaskCardNoArtifactsStory />);
  await expect(cmp.getByTestId("task-artifacts-badge")).toHaveCount(0);
});

// ── T-6338 — two artifacts pinned under the IDENTICAL filename must still
// read as two distinct, safely-deletable rows (owner 2026-07-20 report). The
// fixture also pins an IDENTICAL createdTs on both, which is the worst case
// the ticket calls out: a minute-resolution timestamp alone would print the
// same string on both rows, so the guard is only non-vacuous if it forces
// that collision and still finds the rows distinct (via the per-row id ref
// tag). Desktop AND phone width both run this — jsdom cannot see either.
for (const vp of [
  { label: "desktop 1024", width: 1024, height: 800 },
  { label: "narrow 390", width: 390, height: 780 },
] as const) {
  test(`${vp.label}: two same-named artifacts render as two DISTINCT, in-viewport rows (T-6338)`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const cmp = await mount(<TaskCardSameNameArtifactsStory />);
    await cmp.getByTestId("task-artifacts-badge").click();
    const popover = cmp.locator(".task-artifacts");
    await expect(popover).toBeVisible();

    const rows = cmp.locator(".task-artifacts__item");
    await expect(rows).toHaveCount(2);

    // Both rows do share the identical filename — that premise must hold, or
    // this guard is testing nothing.
    const names = await cmp
      .locator(".task-artifacts__chip-name")
      .evaluateAll((els) => els.map((el) => el.textContent ?? ""));
    expect(names.length).toBe(2);
    expect(names[0]).toBe("DEMO-CUST_demo.mp4");
    expect(names[0]).toBe(names[1]);

    // …yet the FULL row text must differ — the load-bearing assertion. This
    // only passes if some rendered text (the meta line's time + ref tag)
    // breaks the tie; deleting that line collapses both rows back to
    // "DEMO-CUST_demo.mp4" and reddens here.
    const rowTexts = await rows.evaluateAll((els) =>
      els.map((el) => el.textContent ?? ""),
    );
    expect(rowTexts.length).toBe(2);
    expect(rowTexts[0]).not.toBe(rowTexts[1]);

    // The meta line itself must be present and non-empty on both rows (not
    // just "some other text differed by accident" — e.g. a stray key prop).
    const metas = await cmp
      .locator(".task-artifacts__chip-meta")
      .evaluateAll((els) => els.map((el) => el.textContent ?? ""));
    expect(metas.length).toBe(2);
    for (const m of metas) expect(m.length).toBeGreaterThan(0);
    expect(metas[0]).not.toBe(metas[1]);

    // Each row keeps its own delete button (nothing about disambiguation may
    // cost the owner the ability to remove either one).
    await expect(cmp.locator(".task-artifacts__remove")).toHaveCount(2);

    // Still a laid-out panel fully inside the viewport at this width — the
    // second line must not push the popover off-screen.
    const box = (await popover.boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(vp.width + 1);

    // No row's chip spills past the panel edge either (T-49fb's failure mode
    // for a two-line chip would be the meta line forcing extra width).
    const chipRights = await cmp
      .locator(".task-artifacts__chip")
      .evaluateAll((els) => els.map((el) => el.getBoundingClientRect().right));
    for (const right of chipRights) {
      expect(right).toBeLessThanOrEqual(box.x + box.width + 1);
    }
  });
}
