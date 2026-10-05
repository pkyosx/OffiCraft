// 機器資訊 table layout: the columns stay put whatever a row is marked with.
//
// The owner's report: adding the 版本太舊 / 未登入 chips, or a row going stale
// (過期 on every cell), widened its cells and moved every column to its right,
// and the three action buttons stacked into two or three rows. jsdom has no
// layout, so this is measured in a real browser on the real MachinesTable.
// The owner then picked the marks on the SAME line as the value, in fixed
// columns. Claude and Codex are 150px, enough for a
// version, one mark and the chevron. A cell never wraps: when any row carries
// two marks (版本太舊 and 未登入), that column grows to its widest cell, in every
// row of the table alike, each column on its own, and the table scrolls if it no
// longer fits; columns wide enough for two marks all the time left the default
// 伺服器這一台 row scrolling at the 1280px desktop. A table wider
// than its frame scrolls inside it with 機器 fixed on the left, so every row
// still names its machine. There is no 操作 column: the machine's name is the
// trigger of the row's operations menu (詳情, 改名稱, install, uninstall, delete), the
// same menu as the Claude and Codex versions, and the 機器 cell reads online
// dot, name, not-in-effect exclamation. 機器 is only as wide as its widest
// row's items need (the words live in their hints), so at the 1280px desktop
// the default name, real machine names and the offline not-in-effect row leave
// 磁碟 in view; a very long name still widens the table, which scrolls.
//
// MUTANTS (each verified red):
//   marks inline AND auto table layout (the layout before the fix)
//                                           → column x differs between states
//   marks back on a line of their own       → placement test, row height test
//   Claude / Codex column back to 184px      → default-name 1280px fit test, 150px test
//   the runtime cells wrap again (marks one by one, or as a group)
//                                           → placement test, row height test
//   column fixed at 150px, not grown to its content
//                                           → placement test (spills), two-mark 1280px test
//   grown from the first row only, not the widest
//                                           → one-table placement test, one-table width test
//   one width for both (Claude's, or the wider of the two)
//                                           → own-measure test (claude2 / codex2)
//   not measured again when only the telemetry changes
//                                           → telemetry-change test
//   trigger's right margin dropped           → own-measure test (chevron short of the edge)
//   not measured again on a language switch (runtime cells not observed)
//                                           → language switch test
//   the whole measurement skipped while a rename field is open
//                                           → rename-field-and-two-marks test
//   drop `table-layout: fixed` or the column widths → 800px test
//   操作 column or the pencil back           → column order test, name menu test
//   a menu item missing, 刪除 enabled on the server-self row
//                                           → name menu test
//   改名稱 not opening the field              → rename field tests
//   dot back after the name                  → cell order tests (desktop, phone)
//   phone card mode disabled                → phone test (the frame scrolls)
//   phone: name trigger left `nowrap`        → long-name card tests (.monitor
//                                              +71px / +29px at 375px)
//   phone: wrapped name lines centred        → long-name card tests (lines start)
//   phone: name menu not capped beside the dot → long-name card tests (dot alone
//                                              on a line above the name)
//   phone: dot centred on the wrapped name   → long-name card tests (dot on line 1)
//   phone: trigger back to inline-flex       → long-name card tests (chevron at
//                                              the card's right edge)
//   name's last character and the chevron not kept together (`nowrap` off
//   `.mon-machine-name__tail`)              → every card width sweep
//   back to an end padding on the name that the chevron pulls back over
//                                           → production-font sweeps of the
//                                              （…） name and the zh names that
//                                              fill a line (chevron alone on a line)
//   chevron without `height: 1lh` / its 1.5em fallback
//                                           → long-name card tests (chevron off the last line)
//   chevron with no gap after the word       → long-name card tests
//   `.mon-stale` border back to --color-border → frame contrast below 1.5
//   (in the built-in palette --color-border IS --color-card)
//   no measured min-width (MachinesTable)    → 機器 cut short (sticky, 800px and
//                                              minimum tests), 1280px fit test
//   min-width below 機器's widest row         → 機器 cell cut short (sticky test)
//   min-width measured only on render (no ResizeObserver, no fonts.ready)
//                                           → phone-then-desktop test, name-in-place test
//   names not observed (only the frame)      → name-in-place test
//   frame not observed (only the names)      → phone-then-desktop same-size test
//   measured while the rename field is open  → rename field test
//   no machine: the last minimum kept         → last machine test
//   the 尚無機器 cell keeps 機器's shade       → last machine test (frame scrolls 24px)
//   machine state word back beside the dot   → dot tests (desktop, phone), 機器 cut
//   dot click no longer pins its hint         → desktop dot test
//   dot without its aria-label               → desktop dot test
//   phone tap area around the dot removed    → phone dot test
//   未生效 back as text beside the dot        → dot tests (desktop, phone)
//   exclamation on an effective row too      → mark tests (desktop, phone)
//   exclamation without its aria-label       → desktop mark test
//   exclamation click no longer pins         → desktop mark test
//   dot's tap area over the exclamation      → phone mark test
//   磁碟 column removed or moved              → column order test
//   磁碟 column narrowed (back to 96px)       → 磁碟 value tests (both fonts), column width test
//   磁碟 total without its chevron            → 磁碟 value tests
//   機器 column not sticky                    → 機器 left edge moves on scroll
//   機器 cells transparent                    → a scrolled cell shows through
//   frame does not reveal a focused control  → keyboard focus past the right edge / under 機器
//   reveal also runs on a mouse click        → half-covered click test (dialog / menu closes)
//   right shade still on the last cell, not the frame
//                                           → shade test
//   detail box without max-height            → short-window test
//   detail title not allowed to wrap, or the bar a fixed width
//                                           → phone detail tests
//   bar segments not at their share, two categories one colour, a swatch
//   not its segment's colour                → detail bar test
//   scroll shades measured only on scroll    → shade test (no cue at rest)
//   scroll shades on the wrong side          → shade test
//   menu focus counting disabled items       → name menu test
//   name trigger's focus ring removed (`outline: none`) → keyboard focus ring test
//   name's tap area reaching right over the gap → phone dot / mark tests
//   frame's `overscroll-behavior-x` left `auto`, or set to `contain`
//   (`contain` still rubber-bands; only `none` holds the pinned columns)
//                                           → overscroll test
//   `overscroll-behavior-x: none` on `.mon-table` instead of the frame
//                                           → overscroll test
//   `overscroll-behavior: none` on both axes → overscroll test (y), mouse wheel test
//   a line on 機器's edge at rest, on every cell or on the header alone
//                                           → line test (cell box-shadow at rest)
//   the line's shade on at rest              → line test (shade opacity at rest)
//   scrolled, the shade drawn without its 1px line → line test (shade box-shadow)
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { MonitorMachinesLayoutStory, type MachinesLayoutState } from "./stories/MonitorMachinesLayoutStory";
import { LONG_NAME } from "./stories/monitorMachinesLayoutNames";

const STATES = ["normal", "old"] as const;

async function columnEdges(page: Page) {
  return page.evaluate((states) => {
    const out: Record<string, { th: number[]; td: number[] }> = {};
    for (const state of states) {
      const section = document.querySelector(`[data-state="${state}"]`)!;
      const xs = (sel: string) =>
        Array.from(section.querySelectorAll(sel)).map((el) =>
          Math.round((el as HTMLElement).getBoundingClientRect().left)
        );
      out[state] = { th: xs("thead th"), td: xs("tbody td") };
    }
    return out;
  }, [...STATES]);
}

// Each state is its own table here: a one-mark row in a table of its own puts
// every column where a plain one does. (Two marks widen their column, which in
// one real table moves every row alike; the one-table tests cover that.)
test("at a 1400px frame, every column starts at the same x in the plain and one-mark tables", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={1400} states={[...STATES]} />);
  const edges = await columnEdges(page);
  expect(edges.normal.th).toHaveLength(7);
  expect(edges.normal.td).toHaveLength(7);
  expect(edges.old.th, "old header x").toEqual(edges.normal.th);
  expect(edges.old.td, "old cell x").toEqual(edges.normal.td);
});

const MARKED_CELLS = ["mon-claude-version", "mon-codex-version", "mon-cpu", "mon-ram", "mon-power"] as const;

/** For every marked cell of the mounted rows, labelled in order by `states`
 * (one table per state or one table of them all): where each mark sits
 * relative to the value ("same" line or a line "below" it), and whether value,
 * marks and chevron all stay inside the cell without overlapping. */
async function markPlacement(page: Page, ids: readonly string[], states: readonly string[]) {
  return page.evaluate(([ids, states]) => {
    const box = (el: Element) => el.getBoundingClientRect();
    const out = [];
    const rows = Array.from(document.querySelectorAll("[data-state] tbody tr"));
    for (const [r, row] of rows.entries()) {
      for (const id of ids) {
        const cell = row.querySelector(`[data-testid="${id}"]`)!;
        const line = cell.querySelector(".mon-cell-line")!;
        const value = box(line.firstElementChild!);
        const parts = [
          ...Array.from(line.querySelectorAll(".mon-cell-marks > *")),
          ...Array.from(cell.querySelectorAll(".runtime-menu__chevron")),
        ];
        const c = box(cell);
        // The right padding is the gap to the next column; nothing runs into it.
        const right = c.right - parseFloat(getComputedStyle(cell).paddingRight);
        const mid = (r: DOMRect) => (r.top + r.bottom) / 2;
        const marks = Array.from(line.querySelectorAll(".mon-cell-marks > *")).map(box);
        const placement = marks.map((r) =>
          Math.abs(mid(r) - mid(value)) <= 2 ? "same" : r.top >= value.bottom - 0.5 ? "below" : "mixed"
        );
        const all = [value, ...parts.map(box)];
        const overlap = all.some((a, i) =>
          all.some((b, j) => j > i && a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5)
        );
        out.push({
          id: `${states[r]} ${id}`,
          placement,
          inside: all.every((r) => r.left >= c.left && r.right <= right + 0.5 && r.top >= c.top && r.bottom <= c.bottom),
          overlap,
        });
      }
    }
    return out;
  }, [ids, states] as const);
}

const PLACED = [
  ["normal", 0, 0, 0, 0, 0],
  ["old", 1, 0, 0, 0, 0],
  ["chips", 2, 2, 0, 0, 0],
  ["stale", 2, 2, 1, 1, 1],
] as const;
// Every mark shares the value's line. CPU, RAM and 電源 carry at most one.
const PLACEMENT = { 0: [], 1: ["same"], 2: ["same", "same"] } as const;
const expectedPlacement = (ids: readonly string[]) =>
  PLACED.flatMap(([state, ...counts]) =>
    MARKED_CELLS.flatMap((id, i) =>
      ids.includes(id) ? [{ id: `${state} ${id}`, placement: PLACEMENT[counts[i]], inside: true, overlap: false }] : []
    )
  );

// en runs the Claude and Codex cells only: its "stale" chip does not fit the
// 72px CPU and RAM columns beside a dash, which these columns never claimed.
// In one table the plain row comes first, so a column grown from its first row
// alone leaves the two-mark rows spilling.
for (const together of [false, true]) {
  for (const [language, width, ids] of [
    ["zh", 1000, MARKED_CELLS],
    ["zh", 900, MARKED_CELLS],
    ["en", 1000, MARKED_CELLS.slice(0, 2)],
  ] as const) {
    test(`${language}, ${width}px, ${together ? "one table" : "a table per row"}: every mark sits on the value's line and the most a cell carries stays inside its column`, async ({
      mount,
      page,
    }) => {
      const states = ["normal", "old", "chips", "stale"] as const;
      await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
      await page.setViewportSize({ width: width + 500, height: 900 });
      await mount(<MonitorMachinesLayoutStory width={width} states={[...states]} together={together} />);
      expect(await markPlacement(page, ids, states)).toEqual(expectedPlacement(ids));
    });
  }
}

test("the columns run 機器, Claude, Codex, CPU, RAM, 電源, 磁碟 at their fixed widths", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const heads = await page.evaluate(() =>
    Array.from(document.querySelectorAll("thead th")).map((th) => [
      th.textContent!.trim(),
      Math.round(th.getBoundingClientRect().width),
    ])
  );
  expect(heads.slice(1)).toEqual([
    ["Claude", 150],
    ["Codex", 150],
    ["CPU", 72],
    ["RAM", 72],
    ["電源", 88],
    ["磁碟", 112],
  ]);
  expect(heads[0][0]).toBe("機器");
  const order = await page.evaluate(() =>
    Array.from(document.querySelectorAll("tbody td")).map((td) => td.getAttribute("data-testid"))
  );
  expect(order.slice(5, 7)).toEqual(["mon-power", "mon-disk"]);
});

for (const fonts of ["harness", "production"] as const) {
test(`${fonts} fonts: the 磁碟 total with its chevron, and 尚未量測, each sit on one line inside the column`, async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  if (fonts === "production") await loadProductionFonts(page);
  await mount(<MonitorMachinesLayoutStory states={["normal", "chips"]} />);
  const cells = await page.evaluate(() =>
    Array.from(document.querySelectorAll('[data-testid="mon-disk"]')).map((cell) => {
      const value = cell.querySelector(".disk-usage__trigger, .disk-usage__unmeasured")!;
      const c = cell.getBoundingClientRect();
      const v = value.getBoundingClientRect();
      const lineHeight = parseFloat(getComputedStyle(value).lineHeight) || 20;
      return {
        text: value.textContent,
        inside: v.left >= c.left && v.right <= c.right + 0.5,
        oneLine: v.height < lineHeight * 1.8,
        unclipped: value.scrollWidth <= value.clientWidth + 1,
      };
    })
  );
  expect(cells).toEqual([
    { text: "1023.9 GB", inside: true, oneLine: true, unclipped: true },
    { text: "尚未量測", inside: true, oneLine: true, unclipped: true },
  ]);
  expect(
    await page.locator('[data-testid="disk-usage-trigger"] .disk-usage__chevron svg').count(),
    "the total carries the chevron the row's other triggers carry"
  ).toBe(1);
});
}

for (const [language, together] of [
  ["zh", false],
  ["zh", true],
  ["en", true],
] as const) {
  test(`${language}, ${together ? "one table" : "a table per row"}: a row with one or two marks per cell is no taller than a plain one`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: 1500, height: 900 });
    // A plain row both first and last: a table's last row draws no bottom
    // border, so each row is held to the plain row in the same position.
    await mount(
      <MonitorMachinesLayoutStory states={["normal", "old", "chips", "stale", "claude2", "codex2", "normal"]} together={together} />
    );
    const rows = await page.evaluate(() =>
      Array.from(document.querySelectorAll("[data-state] tbody tr")).map((tr) => ({
        last: tr === tr.parentElement!.lastElementChild,
        height: tr.getBoundingClientRect().height,
      }))
    );
    expect(rows).toHaveLength(7);
    const plain = (last: boolean) => (last ? rows[6] : rows[0]).height;
    expect(rows.map((r) => r.height), "every row as tall as a plain one").toEqual(rows.map((r) => plain(r.last)));
  });
}

/** Per table: the Claude and Codex widths of its header and of every row. */
async function runtimeWidths(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-state]")).map((section) => {
      const w = (el: Element) => Math.round(el.getBoundingClientRect().width * 100) / 100;
      const all = (sel: string) => Array.from(section.querySelectorAll(sel)).map(w);
      return {
        state: section.getAttribute("data-state")!,
        claude: [w(section.querySelector("thead th:nth-child(2)")!), ...all('[data-testid="mon-claude-version"]')],
        codex: [w(section.querySelector("thead th:nth-child(3)")!), ...all('[data-testid="mon-codex-version"]')],
      };
    })
  );
}

// 150 is the CSS's own number. The rows are ones whose content is well inside
// it on any font: en "2.1.286 too old" fills all but ~2px of the column here,
// so a font a few px wider grows that column (on one line, as it should) and
// it is left to the placement tests.
for (const [language, states] of [
  ["zh", ["normal", "old", "named", "long"]],
  ["en", ["normal", "named", "long"]],
] as const) {
  for (const width of [1000, 800]) {
    test(`${language}, ${width}px: with no cell carrying two marks, Claude and Codex are 150px`, async ({ mount, page }) => {
      await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
      await page.setViewportSize({ width: width + 500, height: 900 });
      await mount(<MonitorMachinesLayoutStory width={width} states={[...states]} />);
      for (const t of await runtimeWidths(page)) {
        expect(t, t.state).toEqual({ state: t.state, claude: [150, 150], codex: [150, 150] });
      }
    });
  }
}

/** The 機器資訊 table of each mounted section, measured once settled. */
async function settledWidths(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  return runtimeWidths(page);
}

// The expected widths come from mounts of a single row each, not numbers.
test("each column grows to its own widest cell: two marks in Claude alone leave Codex at 150px, and the other way round", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={1400} states={["chips", "claude2", "codex2"]} />);
  const [chips, claude2, codex2] = await settledWidths(page);
  const grown = chips.claude[0];
  expect(grown, "control: two marks need more than 150px").toBeGreaterThan(150);
  expect(chips.codex[0], "control: two marks need more than 150px").toBeGreaterThan(150);
  expect(claude2, "Claude carries two marks").toEqual({ state: "claude2", claude: [grown, grown], codex: [150, 150] });
  expect(codex2, "Codex carries two marks").toEqual({ state: "codex2", claude: [150, 150], codex: chips.codex });
  // Grown to fit, not past it: the widest cell's chevron ends on the column's
  // content edge (the trigger's own right padding sits in the cell's).
  const ends = await page.evaluate(() =>
    ["mon-claude-version", "mon-codex-version"].map((id) => {
      const cell = document.querySelector(`[data-state="chips"] [data-testid="${id}"]`)!;
      const edge = cell.getBoundingClientRect().right - parseFloat(getComputedStyle(cell).paddingRight);
      return Math.abs(cell.querySelector(".runtime-menu__chevron")!.getBoundingClientRect().right - edge) <= 1;
    })
  );
  expect(ends, "the chevron ends on the content edge").toEqual([true, true]);
});

test("in one table every row's Claude and Codex cells take the widest cell's width, wherever that row sits", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  const alone = await mount(<MonitorMachinesLayoutStory width={1400} states={["chips"]} />);
  const [chips] = await settledWidths(page);
  await alone.unmount();
  for (const order of [
    ["normal", "chips", "old"],
    ["chips", "normal", "old"],
    ["normal", "old", "chips"],
  ] as const) {
    const c = await mount(<MonitorMachinesLayoutStory width={1400} states={[...order]} together />);
    const [t] = await settledWidths(page);
    const col = (w: number) => [w, w, w, w];
    expect(t, order.join(", ")).toEqual({ state: "together", claude: col(chips.claude[0]), codex: col(chips.codex[0]) });
    await c.unmount();
  }
});

// The page's machine list stays the same object while only the telemetry
// changes, so the table re-renders without new rows. Codex going from never
// probed (a plain dash) to installed with two marks swaps the cell's element
// for the menu's trigger, which no size observer was watching.
test("when a machine's runtime state changes, the column follows it both ways", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  const alone = await mount(<MonitorMachinesLayoutStory width={1400} states={["codex2"]} together />);
  const [codex2] = await settledWidths(page);
  await alone.unmount();
  expect(codex2.codex[0], "control: two marks need more than 150px").toBeGreaterThan(150);

  const story = await mount(<MonitorMachinesLayoutStory width={1400} states={["nocodex"]} together />);
  expect((await settledWidths(page))[0].codex).toEqual([150, 150]);
  await expect(page.getByTestId("mon-codex-version")).toHaveText("—");
  await story.update(<MonitorMachinesLayoutStory width={1400} states={["codex2"]} together />);
  await expect.poll(async () => (await runtimeWidths(page))[0].codex).toEqual(codex2.codex);
  await story.update(<MonitorMachinesLayoutStory width={1400} states={["nocodex"]} together />);
  await expect.poll(async () => (await runtimeWidths(page))[0].codex).toEqual([150, 150]);
});

test("opening a grown column's version menu keeps every column where it was and opens under the trigger", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal", "chips"]} together />);
  const before = await settledWidths(page);
  const edges = () =>
    page.evaluate(() => Array.from(document.querySelectorAll("thead th")).map((th) => th.getBoundingClientRect().left));
  const x = await edges();
  const trigger = page.getByTestId("mon-claude-version").nth(1).getByRole("button");
  await trigger.click();
  const pop = page.getByRole("menu");
  await expect(pop).toBeVisible();
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  expect(await runtimeWidths(page), "widths with the menu open").toEqual(before);
  expect(await edges(), "columns with the menu open").toEqual(x);
  const t = (await trigger.boundingBox())!;
  const p = (await pop.boundingBox())!;
  expect({
    left: Math.round(p.x) === Math.round(t.x),
    below: Math.abs(p.y - (t.y + t.height - 1)) <= 1,
    wide: p.width >= t.width - 0.5,
  }).toEqual({ left: true, below: true, wide: true });
});

const NAME_MENU = "機器操作（伺服器這一台）";

test("the machine's name shows a focus ring when reached by keyboard", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const trigger = page.getByRole("button", { name: NAME_MENU });
  // Tab, not focus(): :focus-visible only applies on keyboard-driven focus.
  for (let i = 0; i < 30 && !(await trigger.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(trigger).toBeFocused();
  const ring = await trigger.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { focusVisible: el.matches(":focus-visible"), style: cs.outlineStyle, width: parseFloat(cs.outlineWidth) };
  });
  expect(ring.focusVisible, "control: keyboard focus is :focus-visible").toBe(true);
  expect(ring.style).not.toBe("none");
  expect(ring.width).toBeGreaterThan(0);
});

test("the machine's name is the trigger of the row's operations menu, the same control as the version menus", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const trigger = page.getByRole("button", { name: NAME_MENU });
  await expect(trigger).toHaveText("伺服器這一台");
  const version = page.getByTestId("mon-claude-version").getByRole("button");
  const look = (el: Element) => {
    const cs = getComputedStyle(el);
    return { className: el.className, chevron: !!el.querySelector(".runtime-menu__chevron"), radius: cs.borderTopLeftRadius };
  };
  expect(await trigger.evaluate(look)).toEqual(await version.evaluate(look));
  await expect(page.getByRole("button", { name: "機器改名" }), "no pencil beside the name").toHaveCount(0);

  for (const key of ["Enter", " "]) {
    await trigger.focus();
    await page.keyboard.press(key);
    await expect(page.getByRole("menu"), `${JSON.stringify(key)} opens the menu`).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("menu")).toHaveCount(0);
    await expect(trigger).toBeFocused();
  }

  await trigger.click();
  const items = page.getByRole("menuitem");
  await expect(items).toHaveText(["詳情", "改名稱", "重新安裝", "解除安裝", "刪除"]);
  const t = (await trigger.boundingBox())!;
  const pop = (await page.getByRole("menu").boundingBox())!;
  expect({
    left: Math.round(pop.x) === Math.round(t.x),
    below: Math.abs(pop.y - (t.y + t.height - 1)) <= 1,
  }, "the menu opens under the name, on its left edge").toEqual({ left: true, below: true });
  await expect(page.getByTestId("mon-delete-btn"), "the server-self row cannot be deleted").toBeDisabled();
  await expect(page.getByTestId("mon-detail-btn")).toBeFocused();
  for (const id of ["mon-rename-btn", "mon-install-btn", "mon-uninstall-btn", "mon-delete-btn", "mon-detail-btn"]) {
    await page.keyboard.press("ArrowDown");
    await expect(page.getByTestId(id)).toBeFocused();
  }
  await page.mouse.click(5, 880);
  await expect(items, "a click outside closes it").toHaveCount(0);
});

/** Left edge and vertical middle of each item of the first 機器 cell. */
async function cellItems(page: Page, state: string) {
  return page.evaluate((state) => {
    const td = document.querySelector(`[data-state="${state}"] tbody td:first-child`)!;
    const at = (sel: string) => {
      const r = td.querySelector(sel)!.getBoundingClientRect();
      return { x: r.left, right: r.right, mid: (r.top + r.bottom) / 2 };
    };
    return {
      dot: at('[data-testid="mon-machine-online"]'),
      name: at(".mon-table__strong"),
      mark: at('[data-testid="mon-cutover-warning"]'),
    };
  }, state);
}

const inOrder = (c: Awaited<ReturnType<typeof cellItems>>) => ({
  order: c.dot.right <= c.name.x && c.name.right <= c.mark.x,
  oneLine: [c.name, c.mark].every((b) => Math.abs(b.mid - c.dot.mid) <= 2),
});

test("desktop: the 機器 cell reads online dot, name, then the exclamation, on one line", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["stale"]} />);
  expect(inOrder(await cellItems(page, "stale"))).toEqual({ order: true, oneLine: true });
});

test("a rename chosen from the menu opens the field in place of the name, and closing it gives focus back to the name", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["stale"]} />);
  const trigger = page.getByRole("button", { name: NAME_MENU });
  await trigger.click();
  await page.getByRole("menuitem", { name: "改名稱" }).click();
  const field = page.getByRole("textbox", { name: "機器改名" });
  await expect(field).toBeFocused();
  await expect(field).toHaveValue("伺服器這一台");
  await expect(trigger).toHaveCount(0);
  const order = await page.evaluate(() => {
    const td = document.querySelector("tbody td:first-child")!;
    const x = (sel: string) => td.querySelector(sel)!.getBoundingClientRect();
    const dot = x('[data-testid="mon-machine-online"]');
    const edit = x(".inline-edit--editing");
    const mark = x('[data-testid="mon-cutover-warning"]');
    return dot.right <= edit.left && edit.right <= mark.left;
  });
  expect(order, "the field takes the name's place between the dot and the exclamation").toBe(true);
  await page.keyboard.press("Escape");
  await expect(field).toHaveCount(0);
  await expect(trigger).toBeFocused();
});

/** Contrast ratio of the chip's painted frame against the card it sits on. */
async function staleFrameContrast(page: Page) {
  return page.evaluate(() => {
    const chip = document.querySelector('[data-testid="mon-hardware-stale"]') as HTMLElement;
    const probe = document.createElement("canvas").getContext("2d")!;
    const rgba = (css: string) => {
      probe.clearRect(0, 0, 1, 1);
      probe.fillStyle = "#000";
      probe.fillStyle = css;
      probe.fillRect(0, 0, 1, 1);
      return Array.from(probe.getImageData(0, 0, 1, 1).data);
    };
    const card = rgba(getComputedStyle(document.documentElement).getPropertyValue("--color-card").trim());
    const [r, g, b, a] = rgba(getComputedStyle(chip).borderTopColor);
    const over = [r, g, b].map((c, i) => (c * a + card[i] * (255 - a)) / 255);
    const lum = (rgb: number[]) => {
      const [R, G, B] = rgb.map((v) => {
        const s = v / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * R + 0.7152 * G + 0.0722 * B;
    };
    const [hi, lo] = [lum(over), lum(card.slice(0, 3))].sort((x, y) => y - x);
    return (hi + 0.05) / (lo + 0.05);
  });
}

const LIGHT: Record<string, string> = {
  "--color-card": "#ffffff",
  "--color-text": "#232733",
  "--color-text-muted": "#6b7280",
  "--color-border": "#e5e7eb",
  "--color-overlay": "#000",
};

for (const theme of ["dark", "light"] as const) {
  test(`the 過期 chip draws a visible frame in the ${theme} palette`, async ({ mount, page }) => {
    await page.setViewportSize({ width: 1500, height: 900 });
    if (theme === "light") {
      await page.evaluate((vars) => {
        for (const [k, v] of Object.entries(vars)) document.documentElement.style.setProperty(k, v);
      }, LIGHT);
    }
    await mount(<MonitorMachinesLayoutStory states={["stale"]} />);
    expect(await staleFrameContrast(page)).toBeGreaterThanOrEqual(1.5);
  });
}

type TextBox = { left: number; right: number; top: number; bottom: number };
declare global {
  interface Window {
    /** The name's text lines (one box per line, whatever elements split the
     * text) and its last character's box, chevron excluded. */
    __nameText: (trigger: Element) => { lines: TextBox[]; lastChar: TextBox };
  }
}

/** Defines `window.__nameText` for the mounted page. A range over the whole
 * name would also return the boxes of the elements inside it (the chevron). */
async function installNameText(page: Page) {
  await page.evaluate(() => {
    window.__nameText = (trigger) => {
      const name = trigger.querySelector(".mon-table__strong")!;
      const walker = document.createTreeWalker(name, NodeFilter.SHOW_TEXT);
      const lines: TextBox[] = [];
      let last: Text | null = null;
      while (walker.nextNode()) {
        const node = walker.currentNode as Text;
        if (!node.data) continue;
        last = node;
        const range = document.createRange();
        range.selectNodeContents(node);
        for (const r of Array.from(range.getClientRects())) {
          if (r.width === 0) continue;
          const line = lines.find((l) => Math.min(l.bottom, r.bottom) - Math.max(l.top, r.top) > (r.bottom - r.top) / 2);
          if (line) {
            line.left = Math.min(line.left, r.left);
            line.right = Math.max(line.right, r.right);
          } else lines.push({ left: r.left, right: r.right, top: r.top, bottom: r.bottom });
        }
      }
      lines.sort((a, b) => a.top - b.top);
      const range = document.createRange();
      const lastCodePoint = Array.from(last!.data).pop()!;
      range.setStart(last!, last!.data.length - lastCodePoint.length);
      range.setEnd(last!, last!.data.length);
      const c = Array.from(range.getClientRects()).filter((r) => r.width > 0).pop()!;
      return { lines, lastChar: { left: c.left, right: c.right, top: c.top, bottom: c.bottom } };
    };
  });
}

/** The harness page loads no webfonts (see nav-tabs-narrow.ct.spec.tsx), so
 * text is measured in the runner's fallback font. This loads the ones
 * index.html ships, with its `lang`, before the story mounts. */
async function loadProductionFonts(page: Page) {
  const html = fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "../index.html"), "utf8");
  const href = html.match(/href="(https:\/\/fonts\.googleapis\.com\/css2\?[^"]+)"/)![1].replace(/&amp;/g, "&");
  const lang = html.match(/<html lang="([^"]+)"/)![1];
  await page.evaluate(
    async ([href, lang]) => {
      document.documentElement.lang = lang;
      const link = document.createElement("link");
      link.rel = "stylesheet";
      link.href = href;
      const loaded = new Promise((resolve, reject) => {
        link.onload = resolve;
        link.onerror = reject;
      });
      document.head.appendChild(link);
      await loaded;
      await Promise.all([
        document.fonts.load('600 13.5px "Noto Sans TC"', "辦公室三樓靠窗（）"),
        document.fonts.load('600 13.5px "Schibsted Grotesk"', "Seth"),
      ]);
    },
    [href, lang] as const
  );
  expect(
    await page.evaluate(() => [document.fonts.check('600 13.5px "Noto Sans TC"', "窗）"), document.fonts.check('600 13.5px "Schibsted Grotesk"', "S")]),
    "control: the production fonts loaded (they come from Google Fonts)"
  ).toEqual([true, true]);
}

async function overflow(page: Page) {
  return page.evaluate(() => {
    const over = (sel: string) =>
      Math.max(0, ...Array.from(document.querySelectorAll(sel)).map((el) => el.scrollWidth - el.clientWidth));
    return {
      page: document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
      monitor: over(".monitor"),
      frame: over(".mon-table-wrap"),
    };
  });
}

// 996px is the monitor page's content width at a 1280px (or any wider) desktop.
// The very long name is the row that still scrolls there.
const LONG_MENU = `機器操作（${LONG_NAME}）`;

/** Whether every item of the open menu is inside the window and is what a
 * click at its centre would hit (nothing pinned or shaded lies over it). */
async function menuOnTop(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll("[role='menuitem']")).map((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return {
        inside: r.left >= 0 && r.top >= 0 && r.right <= window.innerWidth && r.bottom <= window.innerHeight,
        onTop: !!hit && (hit === el || el.contains(hit)),
      };
    })
  );
}

for (const width of [760, 900, 996]) {
  test(`at ${width}px the name stays on the frame's left edge as the frame scrolls, and its menu opens inside the window, on top`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["long"]} />);
    expect((await overflow(page)).frame, "control: the frame does scroll at this width").toBeGreaterThan(0);
    const wrap = page.locator(".mon-table-wrap");
    const trigger = page.getByRole("button", { name: LONG_MENU });
    const start = (await trigger.boundingBox())!;
    for (const scroll of [0, 10_000]) {
      await wrap.evaluate((el, x) => (el.scrollLeft = x), scroll);
      await nextFrames(page);
      const frame = (await wrap.boundingBox())!;
      const t = (await trigger.boundingBox())!;
      expect({ x: Math.round(t.x), inFrame: t.x >= frame.x && t.x + t.width <= frame.x + frame.width }, `scrollLeft ${scroll}`).toEqual({
        x: Math.round(start.x),
        inFrame: true,
      });
      await trigger.click();
      await expect(page.getByRole("menuitem")).toHaveText(["詳情", "改名稱", "重新安裝", "解除安裝", "刪除"]);
      expect(await menuOnTop(page), `menu at scrollLeft ${scroll}`).toEqual(
        Array(5).fill({ inside: true, onTop: true })
      );
      // A fixed menu does not follow the frame, so a scroll closes it.
      await wrap.evaluate((el) => (el.scrollLeft = el.scrollLeft === 0 ? 40 : 0));
      await expect(page.getByRole("menu"), "scrolling the frame closes the menu").toHaveCount(0);
    }
  });
}

for (const width of [760, 900, 996]) {
  test(`at ${width}px the table scrolls inside its frame with 機器 fixed on the left, uncut and covering what passes under it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["normal", "chips", "stale", "long"]} />);
    const over = await overflow(page);
    expect(over.page, "the page itself does not scroll sideways").toBeLessThanOrEqual(0);
    expect(over.monitor).toBeLessThanOrEqual(0);
    expect(over.frame, "control: the frame does scroll at this width").toBeGreaterThan(0);

    const wraps = page.locator(".mon-table-wrap");
    const machineCells = page.locator('[data-state="long"] tbody td:first-child');
    const machineHead = page.locator('[data-state="long"] thead th:first-child');
    const frame = (await wraps.last().boundingBox())!;
    const startCell = (await machineCells.boundingBox())!;
    const startHead = (await machineHead.boundingBox())!;
    // The frame has a 1px border.
    expect(Math.round(startCell.x)).toBe(Math.round(frame.x + 1));

    await page.evaluate(() => document.querySelectorAll(".mon-table-wrap").forEach((el) => (el.scrollLeft = 10_000)));
    expect(await wraps.last().evaluate((el) => el.scrollLeft), "control: the frame scrolled").toBeGreaterThan(0);
    const cell = (await machineCells.boundingBox())!;
    const head = (await machineHead.boundingBox())!;
    expect({ cell: Math.round(cell.x), head: Math.round(head.x) }, "機器 stays on the frame's left edge").toEqual({
      cell: Math.round(startCell.x),
      head: Math.round(startHead.x),
    });
    const name = (await page.getByRole("button", { name: LONG_MENU }).boundingBox())!;
    expect(name.x + name.width, "the name's menu is inside the frame").toBeLessThanOrEqual(frame.x + frame.width);

    const fit = await page.evaluate(() =>
      Array.from(document.querySelectorAll("[data-state] tbody td:first-child")).map((td) => {
        const name = td.querySelector(".mon-machine-name")!;
        const right = td.getBoundingClientRect().right - parseFloat(getComputedStyle(td).paddingRight);
        return {
          spill: Math.max(...Array.from(name.children).map((el) => el.getBoundingClientRect().right)) - right > 0.5,
          clipped: Array.from(name.querySelectorAll("*")).some((el) => el.scrollWidth > el.clientWidth + 1),
        };
      })
    );
    expect(fit, "nothing in a 機器 cell runs out of it or is cut short").toEqual([
      { spill: false, clipped: false },
      { spill: false, clipped: false },
      { spill: false, clipped: false },
      { spill: false, clipped: false },
    ]);

    // Scrolled all the way, the cells to the right of 機器 run under it. Painted
    // opaque, hiding them changes no pixel inside the 機器 cell.
    const clip = { x: cell.x, y: cell.y, width: cell.width, height: cell.height };
    const covered = await page.screenshot({ clip });
    const hide = (v: string) =>
      page.evaluate((v) => {
        document
          .querySelectorAll('[data-state="long"] tbody td:not(:first-child)')
          .forEach((el) => ((el as HTMLElement).style.visibility = v));
      }, v);
    await hide("hidden");
    const bare = await page.screenshot({ clip });
    await hide("");
    expect(covered.equals(bare), "a scrolled cell shows through the 機器 cell").toBe(true);
  });
}

/** The painted opacity of the two "more this way" shades: the left one on
 * the pinned 機器 cell of the first row, the right one on the frame. */
async function shades(page: Page) {
  return page.evaluate(() => {
    const row = document.querySelector(".mon-table--machines tbody tr")!;
    const frame = document.querySelector(".mon-table-frame")!;
    const op = (el: Element, pseudo: string) => Number(getComputedStyle(el, pseudo).opacity);
    return { left: op(row.firstElementChild!, "::after"), right: op(frame, "::after") };
  });
}

test("at 996px a shade marks the side where more of the table is scrolled away, and neither shows when it fits", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 996, height: 900 });
  // Capped to the viewport, so widening the window alone lets it fit.
  await mount(<MonitorMachinesLayoutStory width={1200} states={["long"]} />);
  const frameWidth = await page.locator(".mon-table-wrap").evaluate((el) => Math.round(el.getBoundingClientRect().width));
  expect(frameWidth, "control: the frame is the 1280px desktop's 996px").toBe(996);
  await expect.poll(() => shades(page), "at rest the 磁碟 column is past the right edge").toEqual({ left: 0, right: 1 });

  // Widening the window, with no scroll and no new data, is what changes it.
  await page.setViewportSize({ width: 1300, height: 900 });
  expect(await page.locator(".mon-table-wrap").evaluate((el) => el.scrollWidth - el.clientWidth), "control: it fits").toBe(0);
  await expect.poll(() => shades(page), "a table that fits has nothing past either edge").toEqual({
    left: 0,
    right: 0,
  });

  await page.setViewportSize({ width: 996, height: 900 });
  await expect.poll(() => shades(page), "narrowed again, the 磁碟 column is back past the right edge").toEqual({
    left: 0,
    right: 1,
  });
  // The right shade hangs on the frame's inner right edge over the full
  // height, and stays there as the frame scrolls.
  const shadeBox = () =>
    page.evaluate(() => {
      const frame = document.querySelector(".mon-table-frame")!;
      const wrap = frame.querySelector(".mon-table-wrap")!.getBoundingClientRect();
      const cs = getComputedStyle(frame, "::after");
      return {
        right: parseFloat(cs.right),
        top: parseFloat(cs.top),
        bottom: parseFloat(cs.bottom),
        width: parseFloat(cs.width),
        frameIsWrap: Math.round(frame.getBoundingClientRect().width) === Math.round(wrap.width),
        events: cs.pointerEvents,
      };
    });
  expect(await shadeBox()).toEqual({ right: 1, top: 1, bottom: 1, width: 24, frameIsWrap: true, events: "none" });
  await page.locator(".mon-table-wrap").evaluate((el) => (el.scrollLeft = 40));
  await expect.poll(() => shades(page), "part way, both edges have more beyond them").toEqual({ left: 1, right: 1 });
  await page.locator(".mon-table-wrap").evaluate((el) => (el.scrollLeft = 10_000));
  await expect.poll(() => shades(page), "scrolled to the end, 機器 covers what scrolled away").toEqual({
    left: 1,
    right: 0,
  });
});

/** Whether the focused control is the topmost thing at its own centre. */
async function focusedOnTop(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement;
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return !!hit && (hit === el || el.contains(hit));
  });
}

test("at 996px a control reached by keyboard is scrolled fully into the frame and out from under the pinned 機器 column", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  // The very long name: the row that still scrolls at 996px.
  await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const wrap = page.locator(".mon-table-wrap");
  const disk = page.getByTestId("disk-usage-trigger");
  // Control: at rest the 磁碟 value lies past the frame's right edge.
  const cut = await disk.evaluate((el) => {
    const wrap = el.closest(".mon-table-wrap")!;
    const inner = wrap.getBoundingClientRect().left + wrap.clientLeft + wrap.clientWidth;
    return el.getBoundingClientRect().right > inner;
  });
  expect(cut, "control: the 磁碟 value starts past the frame's right edge").toBe(true);

  for (let i = 0; i < 30 && !(await disk.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(disk).toBeFocused();
  expect(await wrap.evaluate((el) => el.scrollLeft), "the frame scrolled to the focused control").toBeGreaterThan(0);
  expect(await focusedOnTop(page), "the focused 磁碟 value is in view").toBe(true);
  expect(
    await disk.evaluate((el) => {
      const wrap = el.closest(".mon-table-wrap")!;
      const inner = wrap.getBoundingClientRect().left + wrap.clientLeft + wrap.clientWidth;
      return el.getBoundingClientRect().right <= inner + 0.5;
    }),
    "the focused 磁碟 value is whole inside the frame"
  ).toBe(true);

  const claude = page.getByTestId("mon-claude-version").getByRole("button");
  for (let i = 0; i < 10 && !(await claude.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Shift+Tab");
  }
  await expect(claude).toBeFocused();
  expect(await focusedOnTop(page), "the focused Claude version is not hidden under 機器").toBe(true);
});

/** Lets a scroll queued by the last input land, and its listeners run. */
async function nextFrames(page: Page) {
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
}

test("clicking the uncovered part of a control half under a pinned column opens it and leaves the frame where it was", async ({
  mount,
  page,
}) => {
  // 磁碟 value half past the frame's right edge (996px frame).
  await page.setViewportSize({ width: 996, height: 900 });
  const story = await mount(<MonitorMachinesLayoutStory width={1200} states={["long"]} />);
  const wrap = page.locator(".mon-table-wrap");
  const diskAt = await page.evaluate(() => {
    const wrap = document.querySelector(".mon-table-wrap")!;
    const inner = () => wrap.getBoundingClientRect().left + wrap.clientLeft + wrap.clientWidth;
    const trigger = document.querySelector('[data-testid="disk-usage-trigger"]')!.getBoundingClientRect();
    wrap.scrollLeft += trigger.left + 20 - inner();
    const t = document.querySelector('[data-testid="disk-usage-trigger"]')!.getBoundingClientRect();
    return { x: t.left + 10, y: t.top + t.height / 2, covered: t.right > inner(), scrollLeft: wrap.scrollLeft };
  });
  expect(diskAt.covered, "control: the 磁碟 value is partly past the right edge").toBe(true);
  await nextFrames(page);
  await page.mouse.click(diskAt.x, diskAt.y);
  await nextFrames(page);
  await expect(page.getByTestId("mon-machine-detail-modal")).toHaveCount(1);
  expect(await wrap.evaluate((el) => el.scrollLeft)).toBe(diskAt.scrollLeft);

  // Claude version half under the 機器 column (760px frame).
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("mon-machine-detail-modal")).toHaveCount(0);
  await page.setViewportSize({ width: 760, height: 900 });
  await story.update(<MonitorMachinesLayoutStory width={760} states={["normal"]} />);
  const claudeAt = await page.evaluate(() => {
    const wrap = document.querySelector(".mon-table-wrap")!;
    wrap.scrollLeft = 0;
    const trigger = document.querySelector('[data-testid="mon-claude-version"] button')!.getBoundingClientRect();
    const machine = document.querySelector(".mon-table--machines tbody td:first-child")!.getBoundingClientRect();
    wrap.scrollLeft += trigger.left + 20 - machine.right;
    const t = document.querySelector('[data-testid="mon-claude-version"] button')!.getBoundingClientRect();
    const m = document.querySelector(".mon-table--machines tbody td:first-child")!.getBoundingClientRect();
    return { x: t.right - 10, y: t.top + t.height / 2, covered: t.left < m.right, scrollLeft: wrap.scrollLeft };
  });
  expect(claudeAt.covered, "control: the Claude version is partly under 機器").toBe(true);
  await nextFrames(page);
  await page.mouse.click(claudeAt.x, claudeAt.y);
  await nextFrames(page);
  await expect(page.getByRole("menu")).toHaveCount(1);
  expect(await wrap.evaluate((el) => el.scrollLeft)).toBe(claudeAt.scrollLeft);
});

test("on a short window the machine detail stays inside the window and scrolls inside itself", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 220 });
  await mount(<MonitorMachinesLayoutStory width={1200} states={["normal"]} />);
  await page.getByTestId("disk-usage-trigger").click();
  const box = page.locator(".mon-detailbox");
  await expect(box).toBeVisible();
  const at = await box.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, scrolls: el.scrollHeight > el.clientHeight };
  });
  expect(at.scrolls, "control: the detail is taller than the window allows").toBe(true);
  expect(at.top).toBeGreaterThanOrEqual(0);
  expect(at.bottom).toBeLessThanOrEqual(220);
  const scrolled = await box.evaluate(async (el) => {
    el.scrollTop = el.scrollHeight;
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    return el.scrollTop;
  });
  expect(scrolled, "control: the detail did scroll").toBeGreaterThan(0);
  await expect(box, "scrolling the detail itself does not close it").toBeVisible();
});

/** The open machine detail's bar: each segment's share of the bar's width as
 * drawn, next to the share it was given, and each segment's colour next to its
 * list row's swatch. */
async function barGeometry(page: Page) {
  return page.evaluate(() => {
    const bar = document.querySelector('[data-testid="disk-usage-bar"]')!;
    const width = bar.getBoundingClientRect().width;
    return Array.from(bar.querySelectorAll<HTMLElement>('[data-testid="disk-usage-seg"]')).map((seg) => {
      const key = seg.getAttribute("data-segment")!;
      const swatch = document.querySelector(`.disk-usage__swatch[data-segment="${key}"]`)!;
      return {
        key,
        drawn: Math.round((seg.getBoundingClientRect().width / width) * 1000) / 10,
        given: Math.round(parseFloat(seg.style.width) * 10) / 10,
        colour: getComputedStyle(seg).backgroundColor,
        swatch: getComputedStyle(swatch).backgroundColor,
      };
    });
  });
}

test("the machine detail's bar draws each category at its share, one colour per category that its list row's swatch repeats, taken from the theme", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  await page.getByTestId("disk-usage-trigger").click();
  const segs = await barGeometry(page);
  expect(segs.map((s) => s.key)).toEqual(["database", "backups", "workspaces", "conversations", "other"]);
  for (const s of segs) {
    // Within half a point: a sliver is drawn at least 2px wide.
    expect(Math.abs(s.drawn - s.given), `${s.key} drawn at its share`).toBeLessThanOrEqual(0.5);
    expect(s.swatch, `${s.key} swatch matches its segment`).toBe(s.colour);
  }
  expect(new Set(segs.map((s) => s.colour)).size, "five categories, five colours").toBe(5);
  // A theme that re-values the tokens (a light one does) re-colours the bar.
  await page.evaluate(() => document.documentElement.style.setProperty("--color-icon-blue", "rgb(1, 2, 3)"));
  expect((await barGeometry(page))[0].colour).toBe("rgb(1, 2, 3)");
});

// The detail on a phone: the box fits the window, the bar shrinks with it, and
// a long name and the id wrap instead of pushing the page sideways. Measured
// under the fonts production loads.
const DETAIL_LONG_NAME = "Seth 的 Mac Studio（辦公室三樓靠窗）eva-m5-warden-build-farm-node-0001-us-west";
for (const width of [320, 375, 390, 414]) {
  test(`${width}px phone: the machine detail fits the window, the bar shrinks and a long name wraps`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    await loadProductionFonts(page);
    await mount(<MonitorMachinesLayoutStory states={["named"]} name={DETAIL_LONG_NAME} />);
    await page.getByTestId("disk-usage-trigger").click();
    await expect(page.getByTestId("mon-machine-detail-modal")).toBeVisible();
    const geo = await page.evaluate(() => {
      const r = (sel: string) => document.querySelector(sel)!.getBoundingClientRect();
      const box = document.querySelector(".mon-detailbox")!;
      const b = box.getBoundingClientRect();
      const inner = box.clientWidth - parseFloat(getComputedStyle(box).paddingLeft) - parseFloat(getComputedStyle(box).paddingRight);
      const title = r('[data-testid="mon-machine-detail-name"]');
      const close = r('[data-testid="mon-machine-detail-close"]');
      const bar = r('[data-testid="disk-usage-bar"]');
      const lineHeight = parseFloat(getComputedStyle(document.querySelector('[data-testid="mon-machine-detail-name"]')!).lineHeight) || 20;
      const inBox = (x: DOMRect) => x.left >= b.left - 0.5 && x.right <= b.right + 0.5;
      const se = document.scrollingElement!;
      return {
        pageOverflow: se.scrollWidth - se.clientWidth,
        boxInWindow: b.left >= 0 && b.right <= window.innerWidth,
        boxScrollsSideways: box.scrollWidth > box.clientWidth + 1,
        barFillsBox: Math.abs(bar.width - inner) <= 1,
        titleWraps: title.height > lineHeight * 1.5,
        titleAndCloseInBox: inBox(title) && inBox(close) && title.right <= close.left + 0.5,
        rowsInBox: Array.from(box.querySelectorAll(".mon-detailrow, .disk-usage__row")).every((el) =>
          inBox(el.getBoundingClientRect())
        ),
      };
    });
    expect(geo).toEqual({
      pageOverflow: 0,
      boxInWindow: true,
      boxScrollsSideways: false,
      barFillsBox: true,
      titleWraps: true,
      titleAndCloseInBox: true,
      rowsInBox: true,
    });
  });
}

test("narrower desktop: the table scrolls inside its own frame, the page does not", async ({ mount, page }) => {
  await page.setViewportSize({ width: 820, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={800} states={["normal", "old", "chips"]} />);
  const over = await overflow(page);
  expect(over.page).toBeLessThanOrEqual(1);
  expect(over.monitor, ".monitor scrolls vertically and would swallow a sideways spill").toBeLessThanOrEqual(1);
  // Each state is its own table with its own 機器 width, so the fixed columns
  // are compared by width, not by x.
  const widths = await page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-state]")).map((section) =>
      Array.from(section.querySelectorAll("tbody td"))
        .slice(1)
        .map((td) => Math.round(td.getBoundingClientRect().width))
    )
  );
  expect(widths.map((w) => w.slice(2))).toEqual([
    [72, 72, 88, 112],
    [72, 72, 88, 112],
    [72, 72, 88, 112],
  ]);
  expect(widths.map((w) => w.slice(0, 2).map((x) => (x === 150 ? 150 : x > 150 ? "grown" : x)))).toEqual([
    [150, 150],
    [150, 150],
    ["grown", "grown"],
  ]);
  const spill = await page.evaluate(() => {
    const parts = Array.from(document.querySelector(".mon-machine-name")!.children);
    const right = Math.max(...parts.map((el) => el.getBoundingClientRect().right));
    const cell = document.querySelector("tbody td")!.getBoundingClientRect();
    return Math.round(right - cell.right);
  });
  expect(spill, "the 機器 cell's name and badge stay inside their column").toBeLessThanOrEqual(0);
  const clipped = await page.evaluate(() =>
    Array.from(document.querySelectorAll(".mon-machine-name *"))
      .filter((el) => el.scrollWidth > el.clientWidth + 1)
      .map((el) => el.textContent)
  );
  expect(clipped, "nothing in the 機器 cell is cut short").toEqual([]);
});

test("phone: the card mode does not scroll the page", async ({ mount, page }) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await mount(<MonitorMachinesLayoutStory />);
  expect(await overflow(page)).toEqual({ page: 0, monitor: 0, frame: 0 });
});

test("at 996px the frame that scrolls sideways holds its sideways overscroll and passes vertical overscroll on", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 500 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const scroller = await page.locator(".mon-table--machines tbody td:first-child").evaluate((td) => {
    let el = td.parentElement;
    while (el && getComputedStyle(el).overflowX === "visible") el = el.parentElement;
    if (!el) return null;
    const scrolls = el.scrollWidth > el.clientWidth;
    el.scrollLeft = 40;
    const cs = getComputedStyle(el);
    return { scrolls, scrolled: el.scrollLeft, overscroll: { x: cs.overscrollBehaviorX, y: cs.overscrollBehaviorY } };
  });
  expect(scroller, "control: the table sits in a sideways scroll container").not.toBeNull();
  expect({ scrolls: scroller!.scrolls, scrolled: scroller!.scrolled }, "control: it really scrolls").toEqual({
    scrolls: true,
    scrolled: 40,
  });
  // Read off the computed style because the rubber band itself cannot be
  // observed here: headless Chromium never bounces, and a synthesized trackpad
  // gesture does not scroll at all on CI; only a real trackpad shows it.
  expect(scroller!.overscroll).toEqual({ x: "none", y: "auto" });
});

test("a mouse wheel over the table still scrolls the monitor page vertically", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1016, height: 200 });
  // The app shell gives .monitor the window's height, which makes it, not the
  // window, the page's vertical scroller.
  await page.addStyleTag({ content: "html, body, #root { height: 100%; margin: 0; }" });
  await mount(<MonitorMachinesLayoutStory width={996} />);
  const monitor = page.locator(".monitor");
  const room = await monitor.evaluate((el) => el.scrollHeight - el.clientHeight);
  expect(room, "control: the page is taller than the window").toBeGreaterThan(100);
  expect(await monitor.evaluate((el) => el.scrollTop), "control: the page starts at the top").toBe(0);
  const cell = (await page.locator(".mon-table--machines tbody td:nth-child(3)").first().boundingBox())!;
  const at = { x: cell.x + cell.width / 2, y: cell.y + cell.height / 2 };
  expect(
    await page.evaluate(({ x, y }) => !!document.elementFromPoint(x, y)?.closest(".mon-table-wrap"), at),
    "control: the pointer is over the sideways-scrolling frame"
  ).toBe(true);
  await page.mouse.move(at.x, at.y);
  await page.mouse.wheel(0, 100);
  await expect.poll(() => monitor.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
});

/** 機器's edge in the header and the first row: the cell's own box-shadow (a
 * line drawn at rest would sit there), and its shade's opacity and box-shadow,
 * whose first layer is the 1px line. Computed values, like `shades`: sampled
 * pixels on the CI runner miss even the shade, though it paints locally and on
 * the real site. */
async function edgeLine(page: Page) {
  return page.evaluate(() => {
    const read = (sel: string) => {
      const cell = document.querySelector(sel)!;
      const after = getComputedStyle(cell, "::after");
      return { own: getComputedStyle(cell).boxShadow, opacity: after.opacity, shade: after.boxShadow };
    };
    return {
      head: read(".mon-table--machines thead th:first-child"),
      row: read(".mon-table--machines tbody td:first-child"),
    };
  });
}

test("at rest no line is drawn at 機器's edge in the header or the rows; scrolled, both carry the line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 500 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const wrap = page.locator(".mon-table-wrap");
  expect(
    await wrap.evaluate((el) => ({ scrollLeft: el.scrollLeft, scrollable: el.scrollWidth - el.clientWidth > 20 })),
    "control: the frame sits at its left end and can scroll past 20px"
  ).toEqual({ scrollLeft: 0, scrollable: true });
  const rest = await edgeLine(page);
  expect(
    { head: { own: rest.head.own, opacity: rest.head.opacity }, row: { own: rest.row.own, opacity: rest.row.opacity } },
    "at rest neither 機器 cell draws a line and the shade carrying it is off"
  ).toEqual({ head: { own: "none", opacity: "0" }, row: { own: "none", opacity: "0" } });

  await wrap.evaluate((el) => (el.scrollLeft = 20));
  expect(await wrap.evaluate((el) => el.scrollLeft), "control: the frame scrolled 20px").toBe(20);
  // The 1px line (overlay at 14%), then the shade (shadow at 30%).
  const lit = {
    own: "none",
    opacity: "1",
    shade:
      "color(srgb 1 1 1 / 0.14) 1px 0px 0px 0px inset, color(srgb 0 0 0 / 0.3) 20px 0px 16px -14px inset",
  };
  await expect
    .poll(() => edgeLine(page), "scrolled, the header's and the row's shade are on and open with the 1px line")
    .toEqual({ head: lit, row: lit });
});

/** The numbers that decide whether the 1280px desktop scrolls: the table's
 * measured minimum, its frame, and how far it scrolls. */
async function fitAt(page: Page) {
  return page.evaluate(() => {
    const wrap = document.querySelector(".mon-table-wrap") as HTMLElement;
    const table = wrap.querySelector("table") as HTMLElement;
    return {
      minWidth: parseFloat(getComputedStyle(table).minWidth),
      frame: wrap.clientWidth,
      overflow: wrap.scrollWidth - wrap.clientWidth,
    };
  });
}

test("at 996px (the 1280px desktop) a table of real-length machine names fits its frame and shows 磁碟 without scrolling", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["named"]} />);
  const fit = await fitAt(page);
  expect(fit.frame, "control: the frame is the 1280px desktop's (996px less its border)").toBe(994);
  expect(fit.minWidth, "control: the table has a measured minimum, past its fixed columns").toBeGreaterThan(700);
  expect(fit.minWidth).toBeLessThanOrEqual(fit.frame);
  expect(fit.overflow).toBe(0);
  await expect.poll(() => shades(page)).toEqual({ left: 0, right: 0 });
  const disk = await page.getByTestId("disk-usage-trigger").evaluate((el) => {
    const r = el.getBoundingClientRect();
    const wrap = el.closest(".mon-table-wrap")!;
    const inner = wrap.getBoundingClientRect().left + wrap.clientLeft + wrap.clientWidth;
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return { onTop: !!hit && (hit === el || el.contains(hit)), inFrame: r.right <= inner };
  });
  expect(disk).toEqual({ onTop: true, inFrame: true });
});

// Every new site's own machine is named 伺服器這一台, so this is the row most
// tables have. With no mark or one, it has to fit the 1280px desktop with room
// to spare: fonts on a real machine measure a few pixels wider than here. The
// room left is more than the 56px 操作 column took, less what the name's
// chevron and trigger padding add.
for (const [language, state] of [
  ["zh", "normal"],
  ["en", "normal"],
  ["zh", "old"],
  ["en", "old"],
] as const) {
  test(`${language}, at 996px (the 1280px desktop) the ${state} 伺服器這一台 row fits with 48px to spare and 磁碟 inside the frame`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: 1016, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={996} states={[state]} />);
    await expect(page.locator(".mon-machine-name .mon-table__strong")).toHaveText("伺服器這一台");
    const fit = await fitAt(page);
    expect(fit.frame, "control: the frame is the 1280px desktop's (996px less its border)").toBe(994);
    expect(fit.minWidth, "control: the table has a measured minimum, past its fixed columns").toBeGreaterThan(700);
    expect(fit.overflow).toBe(0);
    expect(fit.frame - fit.minWidth, "room to spare").toBeGreaterThanOrEqual(48);
    const box = async (sel: string) => (await page.locator(sel).boundingBox())!;
    const [diskHead, diskCell, diskValue, frame] = await Promise.all([
      box(".mon-table--machines thead th:nth-child(7)"),
      box('[data-testid="mon-disk"]'),
      box('[data-testid="disk-usage-trigger"]'),
      box(".mon-table-wrap"),
    ]);
    const right = (b: { x: number; width: number }) => b.x + b.width;
    // The frame has a 1px border.
    const inner = right(frame) - 1;
    expect({
      head: right(diskHead) <= inner + 0.5,
      cell: right(diskCell) <= inner + 0.5,
      value: right(diskValue) <= inner + 0.5,
    }).toEqual({ head: true, cell: true, value: true });
    await expect.poll(() => shades(page), "nothing lies past either edge").toEqual({ left: 0, right: 0 });
  });
}

// The owner's case: Claude and Codex both carrying 版本太舊 and 未登入. Nothing
// wraps; the two columns grow, and the table scrolls by exactly what its
// minimum exceeds 994px (zh fits since there is no 操作 column; en still
// scrolls), with 機器 pinned, the shades on the right side and the sideways
// overscroll held. A plain row keeps its height.
for (const language of ["zh", "en"] as const) {
  test(`${language}, at 996px (the 1280px desktop) two marks in Claude and Codex grow both columns, nothing wraps, and the table scrolls only by what its minimum exceeds the frame`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: 1016, height: 900 });
    // The chipped row in a table of its own: one table of both rows would
    // need distinct ids, and the real 伺服器這一台 row's id is what decides the fit.
    const states = ["chips", "normal"] as const;
    await mount(<MonitorMachinesLayoutStory width={996} states={[...states]} />);
    const [t] = await settledWidths(page);
    const fit = await fitAt(page);
    expect(fit.frame, "control: the frame is the 1280px desktop's (996px less its border)").toBe(994);
    expect(t.claude[0], "Claude grew").toBeGreaterThan(150);
    expect(t.codex[0], "Codex grew").toBeGreaterThan(150);
    // scrollWidth rounds the table's fractional width, so a pixel either way.
    const exceeds = Math.max(0, fit.minWidth - fit.frame);
    expect(
      exceeds === 0 ? fit.overflow === 0 : Math.abs(fit.overflow - exceeds) <= 1,
      `the table scrolls by what its minimum exceeds the frame (${fit.overflow} vs ${exceeds})`
    ).toBe(true);
    expect(await markPlacement(page, MARKED_CELLS.slice(0, 2), states)).toEqual(
      states.flatMap((state) =>
        MARKED_CELLS.slice(0, 2).map((id) => ({
          id: `${state} ${id}`,
          placement: state === "chips" ? ["same", "same"] : [],
          inside: true,
          overlap: false,
        }))
      )
    );
    const heights = await page.evaluate(() =>
      Array.from(document.querySelectorAll("tbody tr")).map((tr) => tr.getBoundingClientRect().height)
    );
    expect(heights[0], "the two-mark row is as tall as the plain one").toBe(heights[1]);

    const wrap = page.locator(".mon-table-wrap").first();
    if (fit.overflow === 0) {
      await expect.poll(() => shades(page), "a table that fits has nothing past either edge").toEqual({ left: 0, right: 0 });
      return;
    }
    const machine = page.locator("tbody tr").first().locator("td").first();
    const start = (await machine.boundingBox())!.x;
    await expect.poll(() => shades(page), "at rest the far columns are past the right edge").toEqual({ left: 0, right: 1 });
    await wrap.evaluate((el) => (el.scrollLeft = 10_000));
    expect(await wrap.evaluate((el) => el.scrollLeft), "control: the frame scrolled").toBe(fit.overflow);
    await expect.poll(() => shades(page), "scrolled to the end, 機器 covers what scrolled away").toEqual({ left: 1, right: 0 });
    const frame = (await wrap.boundingBox())!;
    const disk = (await page.locator("tbody tr").first().locator("td").last().boundingBox())!;
    expect({
      machine: Math.round((await machine.boundingBox())!.x) === Math.round(start),
      // The frame has a 1px border; the table's width is not a whole pixel.
      lastColumnAtEdge: Math.abs(disk.x + disk.width - (frame.x + frame.width - 1)) <= 1,
      overscroll: await wrap.evaluate((el) => getComputedStyle(el).overscrollBehaviorX),
    }).toEqual({ machine: true, lastColumnAtEdge: true, overscroll: "none" });
  });
}

test("the table's minimum is the fixed columns plus the widest 機器 cell's content, so 機器 is never cut and never padded out", async ({
  mount,
  page,
}) => {
  // Just above the phone breakpoint: the narrowest frame a table keeps.
  await page.setViewportSize({ width: 740, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={720} states={["normal", "stale", "named", "long"]} />);
  const rows = await page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-state]")).map((section) => {
      const td = section.querySelector("tbody td") as HTMLElement;
      const name = td.querySelector(".mon-machine-name") as HTMLElement;
      const cs = getComputedStyle(td);
      const gap = parseFloat(getComputedStyle(name).columnGap);
      const items = Array.from(name.children).map((el) => el.getBoundingClientRect());
      const content = items.reduce((sum, r) => sum + r.width, 0) + gap * (items.length - 1);
      const right = td.getBoundingClientRect().right - parseFloat(cs.paddingRight);
      return {
        state: section.getAttribute("data-state"),
        slack: Math.round(td.getBoundingClientRect().width - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - content),
        spill: Math.max(...items.map((r) => r.right)) - right > 0.5,
        clipped: Array.from(name.querySelectorAll("*")).some((el) => el.scrollWidth > el.clientWidth + 1),
        scrolls: (() => {
          const wrap = section.querySelector(".mon-table-wrap") as HTMLElement;
          return wrap.scrollWidth > wrap.clientWidth;
        })(),
      };
    })
  );
  // Each frame is narrower than its table, so 機器 sits at its minimum: the
  // content plus at most the pixel lost to rounding, whatever the name.
  expect(rows.map(({ slack, ...r }) => ({ ...r, tight: slack >= 0 && slack <= 1 }))).toEqual([
    { state: "normal", spill: false, clipped: false, scrolls: true, tight: true },
    { state: "stale", spill: false, clipped: false, scrolls: true, tight: true },
    { state: "named", spill: false, clipped: false, scrolls: true, tight: true },
    { state: "long", spill: false, clipped: false, scrolls: true, tight: true },
  ]);
});

test("at 996px a very long machine name widens the table, which scrolls, rather than being cut", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const fit = await fitAt(page);
  expect(fit.overflow, "control: the long name does not fit 996px").toBeGreaterThan(0);
  const name = page.locator(".mon-machine-name .mon-table__strong");
  await expect(name).toHaveText(LONG_NAME);
  const cut = await name.evaluate((el) => {
    const td = el.closest("td")!;
    const right = td.getBoundingClientRect().right - parseFloat(getComputedStyle(td).paddingRight);
    const dot = td.querySelector('[data-testid="mon-machine-online"]')!.getBoundingClientRect();
    return { clipped: el.scrollWidth > el.clientWidth + 1, dotInside: dot.right <= right + 0.5 };
  });
  expect(cut).toEqual({ clipped: false, dotInside: true });
});

/** The measured minimum as MachinesTable set it, and what it does to the
 * table and its frame. */
async function measured(page: Page) {
  return page.evaluate(() => {
    const wrap = document.querySelector(".mon-table-wrap") as HTMLElement;
    const table = wrap.querySelector("table") as HTMLElement;
    return {
      variable: table.style.getPropertyValue("--mon-machines-min-width"),
      table: Math.round(table.getBoundingClientRect().width),
      overflow: wrap.scrollWidth - wrap.clientWidth,
    };
  });
}

/** `measured` once a minimum is set and fonts and size observers are done. */
async function settled(page: Page) {
  await expect.poll(async () => (await measured(page)).variable, "control: a minimum is measured").not.toBe("");
  await page.evaluate(() => document.fonts.ready);
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  return measured(page);
}

// The story's 996px box less its border, 1px on each side; CSS alone sets it.
const FRAME = 994;
const px = (variable: string) => parseFloat(variable);

// The expected minimums below come from a fresh mount of the same data, not
// from numbers: a name's width differs by a few px between machines' fonts
// (CI measured 3px wider than a dev Mac), and a fresh mount reaches its value
// through the render-time measurement, not the re-measure under test.

// The phone card mode hides the header, so nothing can be measured there; the
// table must measure again once the window is wide enough for columns, though
// nothing in it re-renders.
test("mounted on a phone, then widened to the 1280px desktop, the table measures its minimum", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  const desktop = await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  const expected = await settled(page);
  expect(expected.table, "control: mounted on the desktop, the default name fits").toBe(FRAME);
  await desktop.unmount();

  await page.setViewportSize({ width: 390, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  await page.evaluate(() => document.fonts.ready);
  expect(
    await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
    "control: rows are cards"
  ).toBe("block");
  expect(await measured(page), "control: nothing measured in card mode").toEqual({ variable: "", table: 390, overflow: 0 });
  await page.setViewportSize({ width: 1016, height: 900 });
  await expect.poll(() => measured(page)).toEqual(expected);
});

// The same crossing when no name item changes size. Today card mode sets the
// cell's text a half pixel larger, so the names' own observers notice the
// crossing too; with that size pinned equal, only the frame's observer can.
test("mounted on a phone, then widened, the table measures its minimum even when no name item changes size", async ({ mount, page }) => {
  const SAME_SIZE = ".mon-machine-name > * { font-size: 13px !important; }";
  const items = () =>
    page.evaluate(() =>
      Array.from(document.querySelectorAll(".mon-machine-name > *")).map((el) => {
        const r = el.getBoundingClientRect();
        return [r.width, r.height];
      })
    );
  await page.setViewportSize({ width: 1016, height: 900 });
  const desktop = await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  await page.addStyleTag({ content: SAME_SIZE });
  await desktop.update(<MonitorMachinesLayoutStory width={996} states={["normal"]} name="m5" />);
  const expected = await settled(page);
  const desktopItems = await items();
  await desktop.unmount();

  await page.setViewportSize({ width: 390, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} name="m5" />);
  await page.evaluate(() => document.fonts.ready);
  expect(
    await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
    "control: rows are cards"
  ).toBe("block");
  expect(await items(), "control: no name item changes size in card mode").toEqual(desktopItems);
  expect(await measured(page), "control: nothing measured in card mode").toEqual({ variable: "", table: 390, overflow: 0 });
  await page.setViewportSize({ width: 1016, height: 900 });
  expect(await items(), "control: no name item changed size on the way back").toEqual(desktopItems);
  await expect.poll(() => measured(page)).toEqual(expected);
});

// A rename lands as new text in the name without this table re-rendering (and
// so does a web font arriving); the minimum follows the name both ways.
test("a machine name that changes in place is measured again, longer and shorter", async ({ mount, page }) => {
  const LONG = LONG_NAME;
  const SHORT = "m5";
  await page.setViewportSize({ width: 1016, height: 900 });
  const alone = async (name: string) => {
    const c = await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} name={name} />);
    const m = await settled(page);
    await c.unmount();
    return m;
  };
  const long = await alone(LONG);
  const short = await alone(SHORT);

  await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  const before = await settled(page);
  expect(px(long.variable), "control: the long name's minimum is wider than the frame").toBeGreaterThan(FRAME);
  expect(long.overflow, "control: the long name scrolls the frame").toBeGreaterThan(0);
  expect(px(short.variable), "control: the short name needs less than the default one").toBeLessThan(px(before.variable));

  // The name's last character shares an element with the chevron; both text
  // nodes are rewritten, the chevron stays.
  const name = page.locator(".mon-machine-name .mon-table__strong");
  const setName = (text: string) =>
    name.evaluate((el, [head, last]) => {
      el.firstChild!.textContent = head;
      el.querySelector(".mon-machine-name__tail")!.firstChild!.textContent = last;
    }, [text.slice(0, -1), text.slice(-1)] as const);
  await setName(LONG);
  await expect.poll(() => measured(page)).toEqual(long);
  await setName(SHORT);
  await expect.poll(() => measured(page)).toEqual(short);
});

/** Opens the first row's rename field from its name menu. */
async function openRename(page: Page) {
  await page.getByRole("button", { name: /^機器操作/ }).first().click();
  await page.getByRole("menuitem", { name: "改名稱" }).click();
}

// An open rename field stretches over the 機器 cell's slack (flex: 1), so a
// measurement taken while it is open would pin the table at whatever width it
// had: the frame's, here. Narrowing the window with the field still open shows
// that in the table's width as well.
test("an open rename field leaves the table's minimum as it was, and the table still narrows to it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} />);
  const closed = await settled(page);
  expect(closed.table, "control: the table fills the frame").toBe(FRAME);
  expect(px(closed.variable), "control: the minimum leaves 機器 some slack").toBeLessThan(FRAME);
  const before = (await page.locator(".mon-machine-name__menu").boundingBox())!.width;
  await openRename(page);
  await expect(page.getByRole("textbox", { name: "機器改名" }), "control: the field is open").toBeFocused();
  await expect
    .poll(async () => (await page.locator(".mon-machine-name .inline-edit--editing").boundingBox())!.width > before + 20, "control: the field fills the cell's slack")
    .toBe(true);
  // Two frames for the size observers to have run.
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  expect(await measured(page)).toEqual(closed);
  await page.setViewportSize({ width: 780, height: 900 });
  const frame = await page.locator(".mon-table-wrap").evaluate((el) => el.clientWidth);
  expect(frame, "control: the narrowed frame is below the minimum").toBeLessThan(px(closed.variable));
  await expect
    .poll(() => measured(page))
    .toEqual({ variable: closed.variable, table: px(closed.variable), overflow: px(closed.variable) - frame });
  await expect(page.getByRole("textbox", { name: "機器改名" }), "control: the field is still open").toBeVisible();
});

// Only the 機器 cell's measurement waits for the field to close: a runtime
// column that grows meanwhile still grows, and the table's minimum follows it
// by exactly that much, keeping 機器's share as it was.
test("with a rename field open, two marks arriving in Claude still widen Claude, and 機器's share of the minimum stays", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  const alone = await mount(<MonitorMachinesLayoutStory width={996} states={["claude2"]} together />);
  const [claude2] = await settledWidths(page);
  await alone.unmount();
  expect(claude2.claude[0], "control: two marks need more than 150px").toBeGreaterThan(150);

  const story = await mount(<MonitorMachinesLayoutStory width={996} states={["normal"]} together />);
  const closed = await settled(page);
  const [plain] = await runtimeWidths(page);
  await openRename(page);
  await expect(page.getByRole("textbox", { name: "機器改名" }), "control: the field is open").toBeFocused();
  await story.update(<MonitorMachinesLayoutStory width={996} states={["claude2"]} together />);
  await expect.poll(async () => (await runtimeWidths(page))[0].claude).toEqual(claude2.claude);
  const grown = claude2.claude[0] - plain.claude[0];
  await expect
    .poll(async () => px((await measured(page)).variable), "the minimum grows by Claude's growth alone")
    .toBeCloseTo(px(closed.variable) + grown, 0);
  await expect(page.getByRole("textbox", { name: "機器改名" }), "control: the field is still open").toBeVisible();
});

// A language switch changes the words in the runtime cells (未登入 → signed
// out) without re-rendering the table's rows or resizing its frame; only the
// cells' own size observers can notice it.
test("switching the language without remounting measures the Claude and Codex columns again", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  const alone = async (language: "zh" | "en") => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    const c = await mount(<MonitorMachinesLayoutStory width={1400} states={["chips"]} together />);
    const [w] = await settledWidths(page);
    await c.unmount();
    return w;
  };
  const zh = await alone("zh");
  const en = await alone("en");
  expect(en.claude[0], "control: the two languages need different widths").not.toBe(zh.claude[0]);

  await page.evaluate(() => localStorage.setItem("oc.language", "zh"));
  const story = await mount(<MonitorMachinesLayoutStory width={1400} states={["chips"]} together />);
  expect(await settledWidths(page), "zh, as mounted").toEqual([zh]);
  await story.update(<MonitorMachinesLayoutStory width={1400} states={["chips"]} together language="en" />);
  await expect(page.locator("thead th").first(), "control: switched in place").toHaveText("Machine");
  await expect.poll(() => runtimeWidths(page), "en, switched in place").toEqual([en]);
  await story.update(<MonitorMachinesLayoutStory width={1400} states={["chips"]} together language="zh" />);
  await expect.poll(() => runtimeWidths(page), "zh again, switched in place").toEqual([zh]);
});

test("when the last machine goes, its minimum goes with it and the 尚無機器 row does not scroll", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  const table = await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const long = await settled(page);
  expect(long, "control: a long name's minimum scrolls the frame").toEqual({
    variable: long.variable,
    table: px(long.variable),
    overflow: px(long.variable) - FRAME,
  });
  expect(long.overflow, "control: a long name's minimum scrolls the frame").toBeGreaterThan(0);
  await table.update(<MonitorMachinesLayoutStory width={996} states={["long"]} empty />);
  await expect(page.locator("tbody td"), "control: the 尚無機器 row").toHaveText("尚無機器,請先新增機器 / 上線");
  await expect.poll(() => measured(page)).toEqual({ variable: "", table: FRAME, overflow: 0 });
  await expect.poll(() => shades(page)).toEqual({ left: 0, right: 0 });
});

// The 機器 cell shows the online state as a dot only; the word is the dot's
// hint, the same InstantHint the roster's presence dot uses.
const machineText = (page: Page) =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-state] tbody td:first-child")).map((td) => (td as HTMLElement).innerText.replace(/\s+/g, " ").trim())
  );

test("desktop: the 機器 cell shows the online dot without its word; hover shows the word, a click pins it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={1400} states={["normal", "stale"]} />);
  expect(await machineText(page)).toEqual(["伺服器這一台", "伺服器這一台"]);
  const dots = page.getByTestId("mon-machine-online");
  await expect(dots).toHaveCount(2);
  expect(await dots.evaluateAll((els) => els.map((el) => [el.getAttribute("role"), el.getAttribute("aria-label")]))).toEqual([
    ["img", "線上"],
    ["img", "離線"],
  ]);
  const box = await dots.first().boundingBox();
  expect({ w: Math.round(box!.width), h: Math.round(box!.height) }, "the dot keeps its 9px circle").toEqual({ w: 9, h: 9 });

  const tip = page.getByRole("tooltip");
  await expect(tip).toHaveCount(0);
  await dots.first().hover();
  await expect(tip).toHaveText("線上");
  await page.mouse.move(0, 0);
  await expect(tip).toHaveCount(0);

  await dots.last().click();
  await expect(tip).toHaveText("離線");
  await page.mouse.move(0, 0);
  await expect(tip, "a click keeps it open after the pointer leaves").toHaveText("離線");
  const t = (await tip.boundingBox())!;
  const d = (await dots.last().boundingBox())!;
  expect(t.y, "the hint opens just below the dot").toBeGreaterThanOrEqual(d.y + d.height);
  expect(t.y).toBeLessThanOrEqual(d.y + d.height + 12);
  await page.mouse.click(5, 5);
  await expect(tip).toHaveCount(0);
});

test.describe("phone card mode on a touch screen", () => {
  test.use({ hasTouch: true });

  test("390px: the 機器 line shows the dot without its word, and a tap on or just beside the dot shows the word", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await mount(<MonitorMachinesLayoutStory states={["normal", "stale"]} />);
    expect(await page.evaluate(() => window.matchMedia("(hover: none)").matches), "control: a touch screen").toBe(true);
    expect(
      await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
      "control: rows are cards"
    ).toBe("block");
    expect(await machineText(page)).toEqual(["伺服器這一台", "伺服器這一台"]);
    const dots = page.getByTestId("mon-machine-online");
    const tip = page.getByRole("tooltip");
    await dots.first().tap();
    await expect(tip).toHaveText("線上");
    await page.touchscreen.tap(5, 5);
    await expect(tip).toHaveCount(0);

    // A finger lands beside a 9px dot, not on it: 8px below its centre still counts.
    const d = (await dots.last().boundingBox())!;
    const at = { x: d.x + d.width / 2, y: d.y + d.height / 2 + 8 };
    expect(
      await page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.getAttribute("data-testid"), at),
      "the widened tap area is the dot's"
    ).toBe("mon-machine-online");
    await page.touchscreen.tap(at.x, at.y);
    await expect(tip).toHaveText("離線");
    expect(await overflow(page), "the open hint does not scroll the page").toEqual({ page: 0, monitor: 0, frame: 0 });
  });
});

// A proven not-in-effect cutover shows the members' warning exclamation at the
// end of the 機器 cell, after the name; its sentence is the hint and the
// accessible name.
const NOT_IN_EFFECT =
  "未生效：這台機器上的成員還在更新前啟動的環境裡執行。先把這台機器上的成員全部停止，再把這些成員喚醒，就會生效，機器本身不用動。";

type Box = { x: number; y: number; width: number; height: number };
const overlaps = (a: Box, b: Box) =>
  a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

/** The stale row's name, dot and exclamation. */
async function markBoxes(page: Page) {
  const row = page.locator('[data-state="stale"] tbody td:first-child');
  const box = async (sel: string) => (await row.locator(sel).boundingBox())!;
  return {
    name: await box(".mon-table__strong"),
    dot: await box('[data-testid="mon-machine-online"]'),
    mark: await box('[data-testid="mon-cutover-warning"]'),
  };
}

function inViewport(b: Box, vw: number, vh: number) {
  return b.x >= 0 && b.y >= 0 && b.x + b.width <= vw && b.y + b.height <= vh;
}

test("desktop: only the not-in-effect row has the exclamation; hover shows the sentence, a click pins it, and neither covers the name or the dot", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={996} states={["normal", "stale"]} />);
  const marks = page.getByTestId("mon-cutover-warning");
  await expect(marks).toHaveCount(1);
  await expect(page.locator('[data-state="stale"] [data-testid="mon-cutover-warning"]')).toHaveCount(1);
  await expect(page.getByRole("img", { name: NOT_IN_EFFECT })).toHaveCount(1);

  const b = await markBoxes(page);
  expect(
    { dotFirst: b.dot.x + b.dot.width <= b.name.x, afterName: b.mark.x >= b.name.x + b.name.width, sameLine: overlaps({ ...b.mark, x: b.dot.x }, b.dot) },
    "the exclamation ends the line the dot starts"
  ).toEqual({ dotFirst: true, afterName: true, sameLine: true });

  const tip = page.getByRole("tooltip");
  await expect(tip).toHaveCount(0);
  await marks.hover();
  await expect(tip).toHaveText(NOT_IN_EFFECT);
  await page.mouse.move(0, 0);
  await expect(tip).toHaveCount(0);

  await marks.click();
  await page.mouse.move(0, 0);
  await expect(tip, "a click keeps it open after the pointer leaves").toHaveText(NOT_IN_EFFECT);
  const t = (await tip.boundingBox())!;
  expect({
    inside: inViewport(t, 1280, 900),
    coversName: overlaps(t, b.name),
    coversDot: overlaps(t, b.dot),
    belowMark: t.y >= b.mark.y + b.mark.height,
  }).toEqual({ inside: true, coversName: false, coversDot: false, belowMark: true });
  await page.mouse.click(5, 5);
  await expect(tip).toHaveCount(0);
});

test.describe("phone card mode on a touch screen: the exclamation", () => {
  test.use({ hasTouch: true });

  test("390px: a tap on the exclamation shows its sentence inside the window; a tap just left of the dot is the dot's, just right of it is not", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await mount(<MonitorMachinesLayoutStory states={["normal", "stale"]} />);
    expect(await page.evaluate(() => window.matchMedia("(hover: none)").matches), "control: a touch screen").toBe(true);
    expect(
      await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
      "control: rows are cards"
    ).toBe("block");
    await expect(page.getByTestId("mon-cutover-warning")).toHaveCount(1);
    const b = await markBoxes(page);
    expect({
      markOnName: overlaps(b.mark, b.name),
      markOnDot: overlaps(b.mark, b.dot),
      afterDot: b.mark.x >= b.dot.x + b.dot.width,
    }).toEqual({ markOnName: false, markOnDot: false, afterDot: true });

    // Who takes a tap: 1px inside the exclamation's left edge, and 1px either
    // side of the dot's circle. Right of the dot is the gap before the name,
    // which belongs to neither.
    const hit = (x: number, y: number) =>
      page.evaluate(({ x, y }) => document.elementFromPoint(x, y)?.closest("[data-testid]")?.getAttribute("data-testid") ?? null, { x, y });
    const midY = b.mark.y + b.mark.height / 2;
    const dotMid = b.dot.y + b.dot.height / 2;
    expect({
      markEdge: await hit(b.mark.x + 1, midY),
      leftOfDot: await hit(b.dot.x - 1, dotMid),
      rightOfDot: (await hit(b.dot.x + b.dot.width + 1, dotMid)) === "mon-machine-online",
    }).toEqual({ markEdge: "mon-cutover-warning", leftOfDot: "mon-machine-online", rightOfDot: false });

    const tip = page.getByRole("tooltip");
    await page.touchscreen.tap(b.mark.x + b.mark.width / 2, midY);
    await expect(tip).toHaveText(NOT_IN_EFFECT);
    const t = (await tip.boundingBox())!;
    expect({
      inside: inViewport(t, 390, 900),
      coversName: overlaps(t, b.name),
      coversDot: overlaps(t, b.dot),
    }).toEqual({ inside: true, coversName: false, coversDot: false });
    expect(await overflow(page), "the open hint does not scroll the page").toEqual({ page: 0, monitor: 0, frame: 0 });
    await page.touchscreen.tap(5, 5);
    await expect(tip).toHaveCount(0);
  });
});

test.describe("phone card mode on a touch screen: the name's menu", () => {
  test.use({ hasTouch: true });

  test("390px: the 機器 line reads dot, name, exclamation, and a tap on the name opens its menu inside the window", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await mount(<MonitorMachinesLayoutStory states={["normal", "stale"]} />);
    expect(
      await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
      "control: rows are cards"
    ).toBe("block");
    // A card may wrap the line; the items still read in this order.
    const c = await cellItems(page, "stale");
    const seq = [c.dot, c.name, c.mark];
    expect(
      seq.slice(1).map((b, i) => b.mid > seq[i].mid + 2 || (Math.abs(b.mid - seq[i].mid) <= 2 && b.x >= seq[i].right)),
      "dot, name, exclamation in reading order"
    ).toEqual([true, true]);
    expect(Math.abs(c.name.mid - c.dot.mid) <= 2, "the dot sits on the name's line").toBe(true);
    const labels = await page.evaluate(() =>
      Array.from(document.querySelectorAll('[data-state="stale"] tbody td')).map((td) => td.getAttribute("data-label"))
    );
    expect(labels, "no 操作 line in the card").toEqual(["機器", "Claude", "Codex", "CPU", "RAM", "電源", "磁碟"]);
    const name = page.locator('[data-state="stale"] .mon-table__strong');
    const n = (await name.boundingBox())!;
    await page.touchscreen.tap(n.x + n.width / 2, n.y + n.height / 2);
    await expect(page.getByRole("menuitem")).toHaveText(["詳情", "改名稱", "安裝", "解除安裝", "刪除"]);
    expect(await menuOnTop(page)).toEqual(Array(5).fill({ inside: true, onTop: true }));
    expect(await overflow(page), "the open menu does not scroll the page").toEqual({ page: 0, monitor: 0, frame: 0 });
    await expect(page.getByRole("tooltip"), "the tap was the name's, not the dot's").toHaveCount(0);
  });
});

// A card has no sideways scroller (see monitor-table-longtoken.ct.spec.tsx),
// so a name that cannot wrap pushes the page sideways. The name wraps inside
// its trigger, as it did before it became one; desktop keeps it on one line.
const PHONE_LONG_NAMES = [
  "Seth's MacBook Pro M5 Max office desk by the window",
  "eva-m5-warden-build-farm-node-0001-us-west",
];
// "stale" adds the not-in-effect exclamation after the id chip.
const PHONE_LONG_CASES: { width: number; name: string; state: MachinesLayoutState }[] = [
  ...[320, 375, 390].flatMap((width) => PHONE_LONG_NAMES.map((name) => ({ width, name, state: "named" as const }))),
  { width: 375, name: PHONE_LONG_NAMES[0], state: "stale" },
  { width: 320, name: PHONE_LONG_NAMES[0], state: "stale" },
];
for (const { width, name: longName, state } of PHONE_LONG_CASES) {
  test(`${width}px card, ${state} row, name "${longName}": the name wraps inside the card, dot on its first line, chevron after its last word`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await mount(<MonitorMachinesLayoutStory states={[state]} name={longName} />);
    await installNameText(page);
    expect(
      await page.locator("tbody tr").first().evaluate((tr) => getComputedStyle(tr).display),
      "control: rows are cards"
    ).toBe("block");
    expect(await overflow(page)).toEqual({ page: 0, monitor: 0, frame: 0 });
    if (state === "stale") await expect(page.getByTestId("mon-cutover-warning")).toBeVisible();
    const trigger = page.getByRole("button", { name: `機器操作（${longName}）` });
    const chevron = trigger.locator(".runtime-menu__chevron");
    await expect(chevron).toBeVisible();
    const geo = await trigger.evaluate((el) => {
      const r = (e: Element) => e.getBoundingClientRect();
      const mid = (b: { top: number; bottom: number }) => (b.top + b.bottom) / 2;
      const td = r(el.closest("td")!);
      const t = r(el);
      const name = r(el.querySelector(".mon-table__strong")!);
      const ch = r(el.querySelector(".runtime-menu__chevron")!);
      const icon = r(el.querySelector(".runtime-menu__chevron svg")!);
      const dot = r(el.closest("td")!.querySelector('[data-testid="mon-machine-online"]')!);
      const { lines, lastChar } = window.__nameText(el);
      const first = lines[0];
      return {
        wraps: lines.length > 1,
        linesStartTogether: lines.every((l) => Math.abs(l.left - first.left) <= 0.5),
        dotBesideName: dot.right <= name.left && dot.top >= t.top && dot.bottom <= t.bottom,
        // Text rects are as tall as the font, not the line, so both centres
        // are compared, never a pixel offset.
        dotOnFirstLine: Math.abs(mid(dot) - mid(first)) <= 2,
        triggerInCard: t.left >= td.left && t.right <= td.right + 0.5,
        chevronInTrigger: ch.width > 0 && ch.right <= t.right + 0.5,
        // The gap is 6px; under 3 the chevron touches the word.
        chevronAfterLastWord: ch.left - lastChar.right >= 3 && ch.left - lastChar.right <= 8,
        chevronOnLastLine: Math.abs(mid(icon) - mid(lastChar)) <= 1.5,
      };
    });
    expect(geo).toEqual({
      wraps: true,
      linesStartTogether: true,
      dotBesideName: true,
      dotOnFirstLine: true,
      triggerInCard: true,
      chevronInTrigger: true,
      chevronAfterLastWord: true,
      chevronOnLastLine: true,
    });
    await trigger.click();
    // The stale row is offline: its machine is installed, not reinstalled.
    await expect(page.getByRole("menuitem")).toHaveText(["詳情", "改名稱", state === "stale" ? "安裝" : "重新安裝", "解除安裝", "刪除"]);
    expect(await menuOnTop(page)).toEqual(Array(5).fill({ inside: true, onTop: true }));
    expect(await overflow(page), "the open menu does not scroll the page").toEqual({ page: 0, monitor: 0, frame: 0 });
  });
}

// The chevron follows the name's last word in text flow, so wherever "name +
// chevron" just misses the last line it could drop to the next line alone, in
// a band of widths that sits at a different place for each name. Each name is
// swept across the card widths, every 1px, in one mount.
//
// Under the fonts production loads (Noto Sans TC, see loadProductionFonts) a
// full-width ） at the end of a line may be set half-width to make the line
// fit (CSS text-spacing-trim), so a name ending in ） stays on a line its full
// width overflows; anything hung after it — the end padding the name used to
// carry — no longer counts, and the chevron dropped to a line of its own over
// 25px of card widths (the trial site at 382-406px). The harness's fallback
// font has no such trimming, which is why the sweep runs under both.
const SWEEP_NAMES = [
  "Seth 的 Mac Studio（辦公室三樓靠窗）",
  "eva-m5-warden-build-farm-node-0001-us-west",
  "Seth's MacBook Pro M5 Max office desk by the window",
  "build-farm-node-07 west rack",
];
const SWEEP_WIDTHS = Array.from({ length: 720 - 280 + 1 }, (_, i) => 280 + i);
// Names that just fill one line at common phone widths, found by bisection on
// their length: the widths where the last word and the chevron are most
// likely to part.
const FILL_WIDTHS = [320, 360, 375, 390, 414];
const FILL_ZH = "辦公室三樓靠窗邊的工作站主機用來編譯與測試的那一台機器";
const FILL_EN = "build farm node west rack office desk by the window spare unit for nightly release jobs".split(" ");
const FILL_FAMILIES = [
  { id: "zh, ending in a full-width ）", max: FILL_ZH.length, name: (n: number) => `Seth 的 Mac Studio（${FILL_ZH.slice(0, n)}）` },
  { id: "en, whole words", max: FILL_EN.length, name: (n: number) => FILL_EN.slice(0, n).join(" ") },
];

async function sweepWidths(page: Page) {
  const trigger = page.getByTestId("mon-machine-menu");
  const bad: string[] = [];
  for (const width of SWEEP_WIDTHS) {
    await page.setViewportSize({ width, height: 900 });
    const g = await trigger.evaluate((el) => {
      const mid = (b: { top: number; bottom: number }) => (b.top + b.bottom) / 2;
      const ch = el.querySelector(".runtime-menu__chevron")!.getBoundingClientRect();
      const icon = el.querySelector(".runtime-menu__chevron svg")!.getBoundingClientRect();
      const { lastChar } = window.__nameText(el);
      const se = document.scrollingElement!;
      return {
        card: getComputedStyle(el.closest("tr")!).display === "block",
        gap: Math.round((ch.left - lastChar.right) * 100) / 100,
        off: Math.round((mid(icon) - mid(lastChar)) * 100) / 100,
        pageOverflow: se.scrollWidth - se.clientWidth,
      };
    });
    if (!g.card || g.gap < 3 || g.gap > 8 || Math.abs(g.off) > 1.5 || g.pageOverflow > 0)
      bad.push(`${width}px ${JSON.stringify(g)}`);
  }
  return bad;
}

for (const fonts of ["harness", "production"] as const) {
  for (const sweepName of SWEEP_NAMES) {
    test(`${fonts} fonts, card widths 280-720px every 1px, name "${sweepName}": the chevron stays after the last character, on its line`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width: SWEEP_WIDTHS[0], height: 900 });
      if (fonts === "production") await loadProductionFonts(page);
      await mount(<MonitorMachinesLayoutStory states={["named"]} name={sweepName} />);
      await installNameText(page);
      await expect(page.getByRole("button", { name: `機器操作（${sweepName}）` })).toBeVisible();
      expect(await sweepWidths(page), "widths where the chevron left the last character, or the page scrolls").toEqual([]);
    });
  }

  for (const family of FILL_FAMILIES) {
    test(`${fonts} fonts, names that just fill one line at ${FILL_WIDTHS.join(", ")}px (${family.id}), swept across 280-720px every 1px: the chevron stays after the last character`, async ({
      mount,
      page,
    }) => {
      // Up to five names, each swept over 441 widths: ~15s alone, more on a busy machine.
      test.slow();
      if (fonts === "production") await loadProductionFonts(page);
      const story = await mount(<MonitorMachinesLayoutStory states={["named"]} name={family.name(1)} />);
      await installNameText(page);
      const trigger = page.getByTestId("mon-machine-menu");
      const oneLine = async (n: number) => {
        await story.update(<MonitorMachinesLayoutStory states={["named"]} name={family.name(n)} />);
        await expect(trigger).toHaveAccessibleName(`機器操作（${family.name(n)}）`);
        return (await trigger.evaluate((el) => window.__nameText(el).lines.length)) === 1;
      };
      const names: string[] = [];
      for (const width of FILL_WIDTHS) {
        await page.setViewportSize({ width, height: 900 });
        let lo = 1;
        let hi = family.max;
        while (lo < hi) {
          const n = Math.ceil((lo + hi) / 2);
          if (await oneLine(n)) lo = n;
          else hi = n - 1;
        }
        expect(lo, `control: at ${width}px a name of this family fills the line before it runs out`).toBeLessThan(family.max);
        names.push(family.name(lo));
      }
      const bad: string[] = [];
      for (const name of new Set(names)) {
        await story.update(<MonitorMachinesLayoutStory states={["named"]} name={name} />);
        await expect(trigger).toHaveAccessibleName(`機器操作（${name}）`);
        for (const b of await sweepWidths(page)) bad.push(`"${name}" ${b}`);
      }
      expect(bad, "widths where the chevron left the last character, or the page scrolls").toEqual([]);
    });
  }
}
