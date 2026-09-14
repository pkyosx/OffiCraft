// GUARD (T-57 第 4 類) — 「設定改了但還沒生效」 的四條提示行 (T-7f28 機制).
//
// WHY THIS EXISTS: two visual guards were still feeding AgentDetailPanel a
// `modelEffortNote`, a field of the in-place model/effort editor T-7f28 deleted.
// Removing the residue on its own would have left the mechanism that REPLACED
// it — `PendingHint`, the four grey 「→ 要換成 ○○」/「→ 要換到 ○○」 lines — with
// no real-browser coverage. owner 逐字:「這一包就把新機制的檢查一起補上」.
//
// WHY JSDOM CANNOT ANSWER THIS:
//  · The hint must sit UNDER its own value, in its own cell. In jsdom every box
//    is 0×0 at (0,0), so "under" and "beside" and "in the next cell" are all the
//    same measurement. (The narrower question — is it still a block element? —
//    is asserted directly on `tagName`, because MEASUREMENT SHOWED geometry does
//    not answer it: an inline `<span>` following a block value still takes a
//    fresh line box, so a div→span mutant leaves every rect assertion green.
//    It is caught instead by the hint-node COUNT, which selects `div.…`.)
//  · Four lit cells at once is a WIDTH event. The hint lives in a
//    `grid-template-columns: 1fr 1fr` half of `.mp-info2` (~160px at 375px) and
//    carries unbreakable tokens (model slugs, owner-set hostnames). jsdom
//    resolves no grid and no `@media`, so it cannot tell a fitting line from
//    one pushing the page sideways.
//  · The line is meant to be SUBORDINATE to the value it annotates. That is a
//    computed-colour fact; jsdom has no cascade to compute it from.
//  · 「no pending ⇒ the panel is DOM-identical to the pre-feature panel」 is
//    pinned in jsdom by node count, but 「and therefore no taller」 is a pixel
//    claim, measured here against the lit panel rather than a constant.
//
// 🔴 SCOPE — what a green run here does NOT mean.
// The stories hand the panel FINISHED `pending` strings, so `pendingChangeHint()`
// (`src/lib/pendingChange.ts`) never runs. Its red lines — 「回報值未知一律不標」
// and 「相等一律不標」 — are OUT OF SCOPE: rewrite that function to
// `return label(configured)` unconditionally and every test in this file stays
// green. The RULE is guarded by the jsdom suite
// `src/components/AgentDetailPanel.pending-change.test.tsx`, which drives the
// real MemberDetailPanel with a real member fixture. THIS file guards the
// RENDERING (is it its own line? is it subordinate? does an unlit panel really
// render nothing?) and the LAYOUT (do four lit cells survive 375px?).
// Do not restate this guard as covering the rule.
//
// Widths span the phone case, the repo's 720px mobile breakpoint
// (frontend/CLAUDE.md) and a desktop width — narrow and wide fail in opposite
// directions. Screenshots go to `testInfo.outputPath()` so nothing ever lands
// in the repo tree.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import {
  PendingChangeMemberStory,
  PendingChangeWorkerStory,
  PendingChangeNoneStory,
  PendingChangeHeightPairStory,
} from "./stories/PendingChangeStory";

const WIDTHS = [375, 720, 1280] as const;

/** The four cells, each as (value testid, hint testid). `machine`'s value cell
 * is `${p}-machine` — not `${p}-machine-value` — which is exactly the kind of
 * detail a hand-written list gets wrong, hence the count assertion in
 * `expectHintsOnOwnLine` below. */
const CELLS = [
  { cell: "runtime", value: (p: string) => `${p}-runtime-value` },
  { cell: "model", value: (p: string) => `${p}-model-value` },
  { cell: "effort", value: (p: string) => `${p}-effort-value` },
  { cell: "machine", value: (p: string) => `${p}-machine` },
] as const;

/** ASSERTION 1 — the hint is its OWN LINE beneath its own value, not a tail on
 * it.
 *
 * ⚠️ MEASURED, and it corrected me: the geometry half of this does NOT catch a
 * `<div>`→`<span>` mutant. I assumed it would ("a span collapses onto the
 * value's baseline"); it does not, because the hint's SIBLING — the value — is
 * a block, so an inline span still gets a fresh line box under it. With the
 * span mutant applied, every y/x assertion below stayed green at all three
 * widths in both arms. So the element type is asserted EXPLICITLY here rather
 * than inferred from position; the geometry assertions guard the different (and
 * still real) failure of the hint drifting out from under its own value, e.g.
 * a hint reparented into the wrong cell or floated beside the value. */
async function expectHintsOnOwnLine(cmp: Locator, prefix: string) {
  for (const { cell, value } of CELLS) {
    const hint = cmp.getByTestId(`${prefix}-${cell}-pending`);
    expect(
      await hint.evaluate((el) => el.tagName),
      `${cell}: the hint must be a block element of its own, not an inline tail on the value`,
    ).toBe("DIV");
    const valueBox = await cmp.getByTestId(value(prefix)).boundingBox();
    const hintBox = await cmp.getByTestId(`${prefix}-${cell}-pending`).boundingBox();
    expect(valueBox, `${cell} value box`).not.toBeNull();
    expect(hintBox, `${cell} hint box`).not.toBeNull();
    expect(
      hintBox!.y,
      `${cell}: hint must start BELOW the bottom of its value (a <span> mutant puts them on one line)`,
    ).toBeGreaterThanOrEqual(valueBox!.y + valueBox!.height - 1);
    expect(
      Math.abs(hintBox!.x - valueBox!.x),
      `${cell}: hint left edge must align with its value's`,
    ).toBeLessThanOrEqual(2);
  }
}

/** ASSERTION 2 — four lit cells fit. Two different overflow shapes, and the
 * first is the one a boundingBox check CANNOT see: a block's BOX is clamped by
 * its container, so non-wrapping text overflows while the rect still measures
 * as fitting. What moves is `scrollWidth`. Second, no ancestor may have
 * swallowed the overflow into a scrollbar (anything with `overflow-y:auto` gets
 * `overflow-x:auto` too).
 *
 * 🔴 The subtree pick is itself a measurement, so it asserts its own size: a
 * typo'd testid selects the empty set and this function then passes trivially,
 * which looks exactly like success. */
async function expectNoOverflow(page: Page, width: number, prefix: string) {
  const worst = await page.evaluate((p: string) => {
    const hints = Array.from(
      document.querySelectorAll<HTMLElement>(`[data-testid$="-pending"]`),
    ).filter((el) => (el.dataset.testid ?? "").startsWith(`${p}-`));
    const cards = Array.from(document.querySelectorAll<HTMLElement>(".mp-info2"));
    let worstDelta =
      document.documentElement.scrollWidth - document.documentElement.clientWidth;
    let worstWhere = worstDelta > 0 ? "html (the page scrolls sideways)" : "";
    for (const root of [...cards, ...hints]) {
      for (const el of [root, ...Array.from(root.querySelectorAll<HTMLElement>("*"))]) {
        const overflowX = getComputedStyle(el).overflowX;
        if (overflowX === "auto" || overflowX === "scroll") continue;
        const delta = el.scrollWidth - el.clientWidth;
        if (delta > worstDelta) {
          worstDelta = delta;
          worstWhere = `${el.tagName.toLowerCase()}.${el.className || "(no class)"}`;
        }
      }
    }
    return { delta: worstDelta, where: worstWhere, hintCount: hints.length, cardCount: cards.length };
  }, prefix);
  // Self-check FIRST: if the pick is empty the overflow verdict below is
  // vacuous, and a vacuous pass is byte-identical to a real one.
  expect(worst.hintCount, `measurement self-check: hint subtrees picked for "${prefix}"`).toBe(4);
  expect(worst.cardCount, "measurement self-check: .mp-info2 cards picked").toBe(1);
  expect(
    worst.delta,
    `horizontal overflow at ${width}px, worst offender: ${worst.where}`,
  ).toBeLessThanOrEqual(1);
}

/** ASSERTION 5 — the positive control, run in EVERY test that measures a lit
 * panel. It makes 「the component rendered nothing」 and 「the hints rendered but
 * said the wrong thing」 both impossible to pass as success. */
async function expectLit(cmp: Locator, prefix: string) {
  // the values themselves really are on screen…
  await expect(cmp.getByTestId(`${prefix}-machine`)).toContainText("eva-m5");
  await expect(cmp.getByTestId(`${prefix}-model-value`)).toContainText("claude-sonnet-4-7");
  // …and each hint carries the wording its cell is supposed to use: a machine
  // is a PLACE (要換到), the other three are VALUES (要換成).
  await expect(cmp.getByTestId(`${prefix}-machine-pending`)).toContainText("要換到");
  await expect(cmp.getByTestId(`${prefix}-machine-pending`)).toContainText("seth-m1");
  await expect(cmp.getByTestId(`${prefix}-model-pending`)).toContainText("要換成");
  await expect(cmp.getByTestId(`${prefix}-model-pending`)).toContainText("claude-opus-4-8");
  await expect(cmp.getByTestId(`${prefix}-runtime-pending`)).toContainText("要換成");
  await expect(cmp.getByTestId(`${prefix}-effort-pending`)).toContainText("要換成");
}

// ── the two arms, at three widths ────────────────────────────────────────────
//
// Written out per arm rather than looped over a `{ Story }` table: Playwright's
// component-test transform rewrites the IMPORTED IDENTIFIER at the `mount()`
// call site, so a component reached through a variable does not survive it
// (measured — it fails to collect with "Identifier … has already been
// declared"). The shared body lives in `runArm` instead.
async function runArm(
  mounted: Locator,
  page: Page,
  prefix: string,
  width: number,
  shotPath: string,
) {
  await expect(mounted.getByTestId(`${prefix}-machine-pending`)).toBeVisible({
    timeout: 10_000,
  });
  await expectLit(mounted, prefix);
  await expectHintsOnOwnLine(mounted, prefix);
  await expectNoOverflow(page, width, prefix);
  await page.screenshot({ path: shotPath });
}

for (const width of WIDTHS) {
  test(`成員面板 width ${width}: four pending hints each hold their own line and fit`, async ({
    mount,
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1100 });
    const cmp = await mount(<PendingChangeMemberStory />);
    await runArm(cmp, page, "mp", width, testInfo.outputPath(`pending-mp-${width}.png`));
  });

  test(`外包面板 width ${width}: four pending hints each hold their own line and fit`, async ({
    mount,
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1100 });
    const cmp = await mount(<PendingChangeWorkerStory />);
    await runArm(
      cmp,
      page,
      "worker-detail",
      width,
      testInfo.outputPath(`pending-worker-detail-${width}.png`),
    );
  });
}

// ── ASSERTION 4: the hint must read as subordinate to its value ──────────────
test("the hint's colour differs from its value's (measured, not read off a token name)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 1100 });
  await mount(<PendingChangeMemberStory />);
  await expect(page.getByTestId("mp-machine-pending")).toBeVisible({ timeout: 10_000 });

  const pairs = await page.evaluate(() =>
    ["runtime", "model", "effort", "machine"].map((cell) => {
      const valueId = cell === "machine" ? "mp-machine" : `mp-${cell}-value`;
      const v = document.querySelector<HTMLElement>(`[data-testid="${valueId}"]`);
      const h = document.querySelector<HTMLElement>(`[data-testid="mp-${cell}-pending"]`);
      return {
        cell,
        value: v ? getComputedStyle(v).color : null,
        hint: h ? getComputedStyle(h).color : null,
      };
    }),
  );
  // Self-check: every colour actually resolved. A null here would make the
  // inequality below pass for the wrong reason.
  expect(pairs.filter((p) => p.value && p.hint), "measurement self-check").toHaveLength(4);
  for (const p of pairs) {
    expect(p.hint, `${p.cell}: hint colour must not equal its value's`).not.toBe(p.value);
  }
});

// ── ASSERTION 3: the negative control ────────────────────────────────────────
test("nothing pending ⇒ not one hint node is rendered", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 1100 });
  const cmp = await mount(<PendingChangeNoneStory />);
  // Positive control for THIS arm: the panel really did render — otherwise
  // "zero hints" would be satisfied by an empty page.
  await expect(cmp.getByTestId("mp-machine")).toContainText("eva-m5");
  await expect(cmp.locator("div.mp-field__hint")).toHaveCount(0);
  for (const { cell } of CELLS) {
    await expect(cmp.getByTestId(`mp-${cell}-pending`)).toHaveCount(0);
  }
});

test("…and the unlit info card is measurably shorter than the lit one", async ({
  mount,
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 720, height: 1600 });
  const cmp = await mount(<PendingChangeHeightPairStory />);
  const lit = cmp.locator('[data-surface="lit"] .mp-info2');
  const dark = cmp.locator('[data-surface="dark"] .mp-info2');
  await expect(lit).toBeVisible({ timeout: 10_000 });
  await expect(dark).toBeVisible();
  // Positive control: the lit half is lit, the dark half is dark — in the SAME
  // mount, so "both panels rendered nothing" cannot satisfy the comparison.
  await expect(
    cmp.locator('[data-surface="lit"] div.mp-field__hint'),
  ).toHaveCount(4);
  await expect(
    cmp.locator('[data-surface="dark"] div.mp-field__hint'),
  ).toHaveCount(0);

  const litBox = await lit.boundingBox();
  const darkBox = await dark.boundingBox();
  expect(litBox).not.toBeNull();
  expect(darkBox).not.toBeNull();
  // MEASURED against each other at the SAME width — no pixel constant. Four
  // extra 13px lines cannot cost less than one line of anything, so a
  // PendingHint that renders a node when it should render nothing shows up here
  // as the two heights converging.
  expect(
    litBox!.height - darkBox!.height,
    "four lit hint lines must make the info card taller than the same card unlit",
  ).toBeGreaterThan(12);
  // …and the dark card must not have grown a spacer of its own: whatever the
  // lit card costs, the dark one costs strictly less.
  expect(darkBox!.height).toBeLessThan(litBox!.height);

  await page.screenshot({ path: testInfo.outputPath("pending-height-pair-720.png") });
});
