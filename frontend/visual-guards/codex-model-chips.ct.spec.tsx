// T-129 — the segmented quick-pick rows in the two outsource pickers
// (轉派 dialog · 任務手冊 負責成員 editor): Codex 模型 in both. It covered the
// 轉派 dialog's 投入程度 row too until T-131 made that a dropdown — see below.
//
// Adding gpt-6-astra took the Codex vocabulary from 3 chips to 4, and 4
// `gpt-5.6-*` slugs do not fit one row in a phone's ~300px content column. The
// two ways that goes wrong are opposite, and each picker had one of them:
//
//   .task-reassign__seg wraps → the 4th chip drops onto a row of its own and
//     stretches to the full width, centered. That is the shape tasks.css
//     records as an owner review calling it "broken". 投入程度 was in that
//     shape too (MEASURED: 最高 alone at w=292 of a 302-wide group), which is
//     why it used to be pinned here as well.
//   .manual-seg does NOT wrap → the 4 chips squeeze instead, and each label
//     breaks across three lines (`gpt-` / `5.6-` / `terra`).
//
// The fix for both is a grid, and a grid has its own failures at the OTHER
// widths: 任務手冊 is a page, not a max-width modal, so it keeps growing.
// MEASURED at 1280 with a hard `1fr 1fr`, the group is 958 wide and the chips
// become 4 x 471 on two rows — an 11-character label in a half-width button.
// So this file pins BOTH ends: narrow (no orphan, no squeeze) and wide (one
// row).
//
// T-131 turned 投入程度 into a DROPDOWN (owner 2026-09-08): five English labels
// do not fit one phone-width row and the vocabulary keeps growing. So this file
// no longer measures 投入程度 at all, and the @1280 case at the bottom can no
// longer state its contract against that sibling — see the note there for what
// that costs.
//
// All of it is invisible to the vitest suite (jsdom has no layout engine), so
// the contract lives here as real-browser geometry, asserted as position
// relationships rather than pixel snapshots.
import {
  test,
  expect,
  type Locator,
  type Page,
} from "@playwright/experimental-ct-react";
import {
  ManualCodexChipsStory,
  ReassignCodexChipsStory,
} from "./stories/CodexModelChipsStory";

type Chip = {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
  lines: number;
};

/** How many line boxes the chip's own label occupies. A squeezed chip breaks
 * `gpt-5.6-terra` into three; a chip with room keeps it at one.
 *
 * ⚠️ ASSUMPTION: the chip's label is a bare text node (`Segmented` renders
 * `{o.label}` as the button's only child). `getClientRects()` over a range
 * returns one rect per LINE BOX only for text; wrap the label in a `<span>` or
 * put an icon beside it and the count becomes rects-per-element — 1 however the
 * text wraps. Nothing turns red that day, the assertion just stops meaning
 * anything, so if the chip markup ever gains a child element, re-derive this
 * number instead of trusting it. */
async function readChip(id: string, chip: Locator): Promise<Chip> {
  const box = await chip.boundingBox();
  expect(box, `chip ${id} has no box`).not.toBeNull();
  const lines = await chip.evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    return range.getClientRects().length;
  });
  return { id, ...box!, lines };
}

/** Every chip the picker actually rendered, in DOM order.
 *
 * The denominator is READ OFF THE PICKER, never a literal list in this file. A
 * literal would keep passing while measuring a subset: add a 5th Codex model
 * and the new chip — the one that would be the orphan — is the one the guard
 * cannot see, so it would go red naming an innocent chip, or stay green.
 * (Importing CODEX_MODEL_OPTIONS is not available here: a ct spec's non-JSX
 * imports are really loaded in Node, and ModelEffortEditor reaches `../i18n` →
 * `../api` → `src/api/seeds.ts`, which imports `seeds/*.md?raw` and cannot be
 * parsed by the spec loader. MEASURED: it fails at collection with
 * "SyntaxError: seeds/system_interaction.md: Unexpected token (1:0)".)
 * What each chip REPORTS as its slug is a separate contract, pinned literally
 * by src/components/ModelEffortEditor.test.tsx. */
async function readChips(group: Locator, prefix: string): Promise<Chip[]> {
  const cells = await group.getByRole("radio").all();
  expect(cells.length, `${prefix}: the picker rendered chips`).toBeGreaterThan(0);
  const chips: Chip[] = [];
  for (const cell of cells) {
    const testid = await cell.getAttribute("data-testid");
    expect(testid, `${prefix}: every chip carries a data-testid`).toBeTruthy();
    expect(testid, `${prefix}: chip testids are ${prefix}-*`).toContain(`${prefix}-`);
    chips.push(await readChip(testid!.slice(prefix.length + 1), cell));
  }
  return chips;
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

function groupOf(page: Page, anchorTestId: string) {
  return page
    .locator('[role="radiogroup"]')
    .filter({ has: page.getByTestId(anchorTestId) });
}

/** The narrow contract: even rows, nothing left over, nothing squeezed. */
async function assertNoOrphanChip(
  where: string,
  group: Locator,
  prefix: string,
  width: number
) {
  const chips = await readChips(group, prefix);
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
        `${c.id} (alone on its row at y=${Math.round(c.y)}, w=${Math.round(c.width)} ` +
        `of group w=${Math.round(groupWidth)}; the other ${chips.length - 1} ` +
        `of ${chips.length} chips share ${rows.length - 1} row(s))`
    ),
    `${where}: no chip is left alone on its own row at ${width}px`
  ).toEqual([]);

  // 2. No chip stretches to own the whole row — the visual half of the same
  //    complaint (one chip as wide as the control itself).
  const full = chips.filter((c) => c.width > groupWidth * 0.9);
  expect(
    full.map(
      (c) => `${c.id} (w=${Math.round(c.width)} of group w=${Math.round(groupWidth)})`
    ),
    `${where}: no chip spans the full width of the picker at ${width}px`
  ).toEqual([]);

  // 3. No label wraps. This is the opposite failure (`.manual-seg` has no
  //    flex-wrap): the chips stay on one row by breaking their own text.
  const wrapped = chips.filter((c) => c.lines > 1);
  expect(
    wrapped.map((c) => `${c.id} (label on ${c.lines} lines, w=${Math.round(c.width)})`),
    `${where}: every chip keeps its label on ONE line at ${width}px`
  ).toEqual([]);
}

type Mount = Parameters<Parameters<typeof test>[1]>[0]["mount"];

async function openReassignCodex(mount: Mount, page: Page, width: number) {
  await page.setViewportSize({ width, height: 900 });
  await mount(<ReassignCodexChipsStory />);
  await page.getByTestId("reassign-kind-outsource").click();
  await page.getByTestId("reassign-runtime").selectOption("codex");
}

async function openManualCodex(mount: Mount, page: Page, width: number) {
  await page.setViewportSize({ width, height: 900 });
  await mount(<ManualCodexChipsStory widthPx={width} />);
  await page.getByTestId("manual-assignee-edit").click();
  await page.getByTestId("manual-assignee-runtime").selectOption("codex");
}

test("轉派 dialog 模型: the Codex model chips leave no orphan at 390px", async ({
  mount,
  page,
}) => {
  await openReassignCodex(mount, page, 390);
  await assertNoOrphanChip(
    "轉派 dialog 模型",
    groupOf(page, "reassign-model-gpt-6-astra"),
    "reassign-model",
    390
  );
});

// 任務手冊 is a PAGE: its picker keeps every width between a phone and a desk,
// so one sample cannot stand for the narrow end. These three are the widths a
// width-derived track count got wrong, MEASURED by sweeping 320→1600 in 10px
// steps against `repeat(auto-fit, minmax(120px, 1fr))`:
//
//   320  group 238 — ONE track: four full-width chips stacked, 4 rows.
//   390  group 308 — two tracks. The phone this ticket started from.
//   520  group 438 — THREE tracks: 3 chips then a 4th alone on row two. The
//        middle of a 470–588 band, and the exact shape the class exists to
//        prevent, reintroduced between the two widths that were checked.
//
// With 4 chips, an ODD track count is the bug, so the layout may never derive
// one from the available width: settings.css writes 4 and 2 literally. These
// cases are what makes that non-negotiable.
for (const width of [320, 390, 520]) {
  test(`任務手冊 負責成員 模型: the Codex model chips leave no orphan at ${width}px`, async ({
    mount,
    page,
  }) => {
    await openManualCodex(mount, page, width);
    await assertNoOrphanChip(
      `任務手冊 負責成員 模型 @${width}`,
      groupOf(page, "manual-assignee-model-gpt-6-astra"),
      "manual-assignee-model",
      width
    );
  });
}

// The BOUNDARY itself. Every case above and the 1280 case below sit on one
// side or the other of settings.css's existing 720px breakpoint, and an even
// track count is even on either side of it — so all of them stay green if the
// breakpoint moves. These two widths are one 10px step apart across it and
// assert the flip, which is the only thing that reddens when the breakpoint
// itself changes.
//
// The number asserted is the COLUMN count (chips on the first row), not the
// row count: rows depend on how many models exist, columns do not, so adding a
// 5th Codex model must not redden these.
// MEASURED against this build: 720 → group 638, 4 x 311 on 2 rows (2 columns);
//                              730 → group 648, 4 x 155 on 1 row  (4 columns).
for (const { width, columns } of [
  { width: 720, columns: 2 },
  { width: 730, columns: 4 },
]) {
  test(`任務手冊 負責成員 模型 @${width}: the 720px breakpoint gives ${columns} columns`, async ({
    mount,
    page,
  }) => {
    await openManualCodex(mount, page, width);
    const group = groupOf(page, "manual-assignee-model-gpt-6-astra");
    const chips = await readChips(group, "manual-assignee-model");
    const rows = rowsOf(chips);
    expect(
      rows[0].length,
      `任務手冊 負責成員 模型 @${width}: the chips lay out in ${columns} columns. ` +
        `This is settings.css's 720px breakpoint; a different count here means ` +
        `the breakpoint moved, and the cases at 320/390/520/1280 cannot see that ` +
        `because they are all on one side of it or the other ` +
        `(rows measured: ${rows.map((r) => r.length).join("+")} of ${chips.length} chips)`
    ).toBe(columns);
    await assertNoOrphanChip(
      `任務手冊 負責成員 模型 @${width}`,
      group,
      "manual-assignee-model",
      width
    );
  });
}

// The wide end, which the 390px cases cannot see: fixing the phone by splitting
// the chips in two costs the desktop — MEASURED with a hard `1fr 1fr` the group
// is 958 wide at 1280 and the 4 chips become 4 x 471 on TWO rows, an
// 11-character label in a half-width button.
//
// ⚠️ WHAT THIS CASE LOST IN T-131. Until then the contract was stated against
// 投入程度 — the identical control one section below, whose cells gave the
// columns these chips had to agree with — so a wrong track count reddened by
// disagreeing with a real sibling rather than with a number in this file.
// 投入程度 is a dropdown now (owner 2026-09-08), so that sibling is gone and
// there is nothing left to state the contract against except the picker's own
// shape: ONE row, no orphan, no squeeze. That is strictly weaker — a track
// count of 4 and of 8 both keep 4 chips on one row at 1280 — and nothing in
// this repo now pins the model row's columns to anything outside itself.
test("任務手冊 負責成員 模型 @1280: the chips stay on one row", async ({
  mount,
  page,
}) => {
  await openManualCodex(mount, page, 1280);

  const modelGroup = groupOf(page, "manual-assignee-model-gpt-6-astra");
  const model = await readChips(modelGroup, "manual-assignee-model");
  const groupWidth = Math.round((await modelGroup.boundingBox())!.width);

  const modelRows = rowsOf(model);
  expect(
    modelRows.map(
      (r) => `[${r.map((c) => `${c.id} w=${Math.round(c.width)}`).join(", ")}]`
    ),
    `任務手冊 負責成員 模型 @1280: the ${model.length} Codex model chips fit on ONE ` +
      `row in their ${groupWidth}px group`
  ).toHaveLength(1);

  await assertNoOrphanChip("任務手冊 負責成員 模型 @1280", modelGroup, "manual-assignee-model", 1280);
});
