// T-129 — the Codex 模型 quick-pick chips at 390px, in BOTH pickers that render
// CODEX_MODEL_OPTIONS (轉派 dialog · 任務手冊 負責成員 editor).
//
// Adding gpt-6-astra took the vocabulary from 3 chips to 4, and 4 `gpt-5.6-*`
// slugs do not fit one row in a phone's ~300px content column. The two ways
// that goes wrong are opposite, and each picker had one of them:
//
//   .task-reassign__seg wraps → the 4th chip drops onto a row of its own and
//     stretches to the full width, centered. That is the shape tasks.css
//     records as an owner review calling it "broken".
//   .manual-seg does NOT wrap → the 4 chips squeeze instead, and each label
//     breaks across three lines (`gpt-` / `5.6-` / `terra`).
//
// Both are invisible to the vitest suite (jsdom has no layout engine), so the
// contract lives here as real-browser geometry: no chip alone on a row, no chip
// spanning the row, no label on more than one line.
import { test, expect, type Locator } from "@playwright/experimental-ct-react";
import {
  ManualCodexChipsStory,
  ReassignCodexChipsStory,
} from "./stories/CodexModelChipsStory";

const SLUGS = ["gpt-6-astra", "gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna"];

type Chip = {
  slug: string;
  x: number;
  y: number;
  width: number;
  height: number;
  lines: number;
};

/** How many line boxes the chip's own label occupies. A squeezed chip breaks
 * `gpt-5.6-terra` into three; a chip with room keeps it at one. */
async function readChip(slug: string, chip: Locator): Promise<Chip> {
  const box = await chip.boundingBox();
  expect(box, `chip ${slug} has no box`).not.toBeNull();
  const lines = await chip.evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    return range.getClientRects().length;
  });
  return { slug, ...box!, lines };
}

/** Chips whose vertical extents overlap are on the same visual row. */
function rowsOf(chips: Chip[]): Chip[][] {
  const rows: Chip[][] = [];
  for (const c of [...chips].sort((a, b) => a.y - b.y)) {
    const row = rows.find((r) =>
      r.some((o) => c.y < o.y + o.height && o.y < c.y + c.height)
    );
    if (row) row.push(c);
    else rows.push([c]);
  }
  return rows;
}

async function assertNoOrphanChip(where: string, group: Locator, prefix: string) {
  const chips: Chip[] = [];
  for (const slug of SLUGS) {
    const chip = group.getByTestId(`${prefix}-${slug}`);
    await expect(chip, `${where}: chip ${slug} is rendered`).toBeVisible();
    chips.push(await readChip(slug, chip));
  }

  const groupWidth = (await group.boundingBox())!.width;

  // 1. No chip sits alone on a row the others share — the orphan. This is the
  //    contract the owner's "read as broken" was about, so it is asserted
  //    FIRST: whatever else is wrong, the message names the lone chip.
  const rows = rowsOf(chips);
  const orphans =
    rows.length > 1 ? rows.filter((r) => r.length === 1).flat() : [];
  expect(
    orphans.map(
      (c) =>
        `${c.slug} (alone on its row at y=${Math.round(c.y)}, w=${Math.round(c.width)} ` +
        `of group w=${Math.round(groupWidth)}; the other ${chips.length - 1} ` +
        `chips share ${rows.length - 1} row(s))`
    ),
    `${where}: no Codex model chip is left alone on its own row at 390px`
  ).toEqual([]);

  // 2. No chip stretches to own the whole row — the visual half of the same
  //    complaint (one chip as wide as the control itself).
  const full = chips.filter((c) => c.width > groupWidth * 0.9);
  expect(
    full.map(
      (c) => `${c.slug} (w=${Math.round(c.width)} of group w=${Math.round(groupWidth)})`
    ),
    `${where}: no Codex model chip spans the full width of the picker`
  ).toEqual([]);

  // 3. No label wraps. This is the opposite failure (`.manual-seg` has no
  //    flex-wrap): the chips stay on one row by breaking their own text.
  const wrapped = chips.filter((c) => c.lines > 1);
  expect(
    wrapped.map((c) => `${c.slug} (label on ${c.lines} lines, w=${Math.round(c.width)})`),
    `${where}: every Codex model chip keeps its slug on ONE line at 390px`
  ).toEqual([]);
}

test("轉派 dialog: the 4 Codex model chips leave no orphan at 390px", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mount(<ReassignCodexChipsStory />);

  await page.getByTestId("reassign-kind-outsource").click();
  await page.getByTestId("reassign-runtime").selectOption("codex");

  const group = page
    .locator('[role="radiogroup"]')
    .filter({ has: page.getByTestId("reassign-model-gpt-6-astra") });
  await assertNoOrphanChip("轉派 dialog", group, "reassign-model");
});

test("任務手冊 負責成員: the 4 Codex model chips leave no orphan at 390px", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await mount(<ManualCodexChipsStory widthPx={390} />);

  await page.getByTestId("manual-assignee-edit").click();
  await page.getByTestId("manual-assignee-runtime").selectOption("codex");

  const group = page
    .locator('[role="radiogroup"]')
    .filter({ has: page.getByTestId("manual-assignee-model-gpt-6-astra") });
  await assertNoOrphanChip("任務手冊 負責成員", group, "manual-assignee-model");
});
