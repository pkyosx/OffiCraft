// 機器資訊 table layout: the columns stay put whatever a row is marked with.
//
// The owner's report: adding the 版本太舊 / 未登入 chips, or a row going stale
// (過期 on every cell), widened its cells and moved every column to its right,
// and the three action buttons stacked into two or three rows. jsdom has no
// layout, so this is measured in a real browser on the real MachinesTable.
// The owner then picked the marks on the SAME line as the value, in fixed
// columns, Claude and Codex one width, and a ⚙ with no frame. Claude and Codex
// are sized for a version, one mark and the chevron; a second mark wraps under
// the value inside the cell, because columns wide enough for two marks on one
// line left the default 伺服器這一台 row scrolling at the 1280px desktop. A table wider
// than its frame scrolls inside it with 機器 fixed on the left, so every row
// still names its machine, and 操作 fixed on the right. 機器 is only as wide as
// its widest row's name, id and online dot need (the state word moved into the
// dot's hint), so at the 1280px desktop the default name, real machine names
// and the offline 未生效 row leave 磁碟 in view; a very long name still widens
// the table, which scrolls.
//
// MUTANTS (each verified red):
//   marks inline AND auto table layout (the layout before the fix)
//                                           → column x differs between states
//   marks back on a line of their own       → placement test, row height test
//   Claude / Codex column narrowed           → a single mark wraps (placement test)
//   Claude / Codex column back to 184px      → default-name 1280px fit test
//   a second mark no longer wraps            → placement test (spills), row height test
//   marks wrap as one group                  → placement test (first mark leaves the value's line)
//   trigger's right margin dropped           → en placement test ("too old" wraps)
//   操作 header's left padding back          → en default-name fit test (1px scroll)
//   Codex column a different width           → equal-width test
//   drop `table-layout: fixed` or the column widths → 800px test
//   操作 column not sticky                  → gear off-screen at 760/900px
//   操作 cells transparent                  → a scrolled cell shows through
//   操作 header not sticky                  → header leaves the right edge
//   phone card mode disabled                → phone test (the frame scrolls)
//   `.mon-stale` border back to --color-border → frame contrast below 1.5
//   (in the built-in palette --color-border IS --color-card)
//   no measured min-width (MachinesTable)    → 機器 cut short (sticky, 800px and
//                                              minimum tests), 1280px fit test
//   min-width below 機器's widest row         → 機器 cell cut short (sticky test)
//   min-width measured only on render (no ResizeObserver, no fonts.ready)
//                                           → phone-then-desktop test, name-in-place test
//   names not observed (only the frame)      → name-in-place test
//   measured while the rename field is open  → rename field test
//   no machine: the last minimum kept         → last machine test
//   the 無機器 cell keeps 機器's shade         → last machine test (frame scrolls 24px)
//   machine state word back beside the dot   → dot tests (desktop, phone), 機器 cut
//   dot click no longer pins its hint         → desktop dot test
//   dot without its aria-label               → desktop dot test
//   phone tap area around the dot removed    → phone dot test
//   磁碟 column removed or moved              → column order test
//   磁碟 column narrowed                      → 磁碟 value test
//   機器 column not sticky                    → 機器 left edge moves on scroll
//   機器 cells transparent                    → a scrolled cell shows through
//   frame does not reveal a focused control  → keyboard focus hidden under ⚙ / 機器
//   reveal also runs on a mouse click        → half-covered click test (panel / menu closes)
//   panel without max-height                 → short-window test
//   panel scroll closes it                   → short-window test
//   scroll shades measured only on scroll    → shade test (no cue at rest)
//   scroll shades on the wrong side          → shade test
//   menu focus counting disabled items       → keyboard test
//   menu aligned to the gear's left edge     → menu right edge test
//   ⚙ border back at rest, on hover or open  → frameless ⚙ test
//   ⚙ focus ring removed (`outline: none`)   → keyboard focus ring test
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
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { MonitorMachinesLayoutStory } from "./stories/MonitorMachinesLayoutStory";

const STATES = ["normal", "chips", "stale"] as const;

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

// Wide enough for every row's 機器 cell: each state is its own table here, and
// each table is as wide as its own widest 機器 cell needs, so below this width
// the stale row's longer 機器 cell would move its columns for a reason that has
// nothing to do with the marks (one real table holds every row and has one
// 機器 width).
test("at a 1400px frame, every column starts at the same x in the plain, chipped and stale rows", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={1400} />);
  const edges = await columnEdges(page);
  expect(edges.normal.th).toHaveLength(8);
  expect(edges.normal.td).toHaveLength(8);
  for (const state of ["chips", "stale"] as const) {
    expect(edges[state].th, `${state} header x`).toEqual(edges.normal.th);
    expect(edges[state].td, `${state} cell x`).toEqual(edges.normal.td);
  }
});

const MARKED_CELLS = ["mon-claude-version", "mon-codex-version", "mon-cpu", "mon-ram", "mon-power"] as const;

/** For every marked cell of the mounted rows: where each mark sits relative to
 * the value ("same" line or a line "below" it), and whether value, marks and
 * chevron all stay inside the cell without overlapping. */
async function markPlacement(page: Page, ids: readonly string[]) {
  return page.evaluate((ids) => {
    const box = (el: Element) => el.getBoundingClientRect();
    const out = [];
    for (const section of Array.from(document.querySelectorAll("[data-state]"))) {
      for (const id of ids) {
        const cell = section.querySelector(`[data-testid="${id}"]`)!;
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
          id: `${section.getAttribute("data-state")} ${id}`,
          placement,
          inside: all.every((r) => r.left >= c.left && r.right <= right + 0.5 && r.top >= c.top && r.bottom <= c.bottom),
          overlap,
        });
      }
    }
    return out;
  }, ids);
}

const PLACED = [
  ["normal", 0, 0, 0, 0, 0],
  ["old", 1, 0, 0, 0, 0],
  ["chips", 2, 2, 0, 0, 0],
  ["stale", 2, 2, 1, 1, 1],
] as const;
// The first mark shares the value's line; in the Claude and Codex cells a
// second one goes on the line below. CPU, RAM and 電源 carry at most one.
const PLACEMENT = { 0: [], 1: ["same"], 2: ["same", "below"] } as const;
const expectedPlacement = (ids: readonly string[]) =>
  PLACED.flatMap(([state, ...counts]) =>
    MARKED_CELLS.flatMap((id, i) =>
      ids.includes(id) ? [{ id: `${state} ${id}`, placement: PLACEMENT[counts[i]], inside: true, overlap: false }] : []
    )
  );

// en runs the Claude and Codex cells only: its "stale" chip does not fit the
// 72px CPU and RAM columns beside a dash, which these columns never claimed.
for (const [language, width, ids] of [
  ["zh", 1000, MARKED_CELLS],
  ["zh", 900, MARKED_CELLS],
  ["en", 1000, MARKED_CELLS.slice(0, 2)],
] as const) {
  test(`${language}, ${width}px: one mark sits on the value's line, a second wraps under it, and the most a cell carries stays inside its column`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: width + 500, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["normal", "old", "chips", "stale"]} />);
    expect(await markPlacement(page, ids)).toEqual(expectedPlacement(ids));
  });
}

test("the columns run 機器, Claude, Codex, CPU, RAM, 電源, 磁碟, 操作 at their fixed widths", async ({ mount, page }) => {
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
    ["磁碟", 96],
    ["操作", 56],
  ]);
  expect(heads[0][0]).toBe("機器");
  const order = await page.evaluate(() =>
    Array.from(document.querySelectorAll("tbody td")).map((td) => td.getAttribute("data-testid"))
  );
  expect(order.slice(5, 7)).toEqual(["mon-power", "mon-disk"]);
});

test("the 磁碟 total and 尚未量測 each sit on one line inside the column", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
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
});

test("a row with one mark per cell is no taller than a plain one; a second mark adds no more than its own line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal", "old", "chips", "stale"]} />);
  const rows = await page.evaluate(() => ({
    heights: Array.from(document.querySelectorAll("[data-state] tbody tr")).map((tr) =>
      Math.round(tr.getBoundingClientRect().height)
    ),
    mark: document.querySelector('[data-state="chips"] [data-testid="mon-claude-too-old"]')!.getBoundingClientRect().height,
  }));
  const [normal, old, chips, stale] = rows.heights;
  expect(rows.heights).toHaveLength(4);
  expect(old, "one mark").toBe(normal);
  expect(stale, "stale row").toBe(chips);
  expect(chips - normal, "a second mark wraps under the value").toBeGreaterThan(0);
  expect(chips - normal, "by one line of chips at most").toBeLessThanOrEqual(Math.ceil(rows.mark) + 2);
});

for (const width of [1000, 900, 800]) {
  test(`at ${width}px the Claude and Codex columns are one width in every state`, async ({ mount, page }) => {
    await page.setViewportSize({ width: width + 500, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} />);
    const widths = await page.evaluate(() =>
      Array.from(document.querySelectorAll("[data-state]")).map((section) => {
        const w = (sel: string) => Math.round(section.querySelector(sel)!.getBoundingClientRect().width);
        return {
          state: section.getAttribute("data-state"),
          claude: w('[data-testid="mon-claude-version"]'),
          codex: w('[data-testid="mon-codex-version"]'),
          claudeHead: w("thead th:nth-child(2)"),
          codexHead: w("thead th:nth-child(3)"),
        };
      })
    );
    const first = widths[0].claude;
    expect(first, "the column has a real width").toBeGreaterThan(100);
    for (const row of widths) {
      expect(row, row.state!).toEqual({ state: row.state, claude: first, codex: first, claudeHead: first, codexHead: first });
    }
  });
}

test("the ⚙ draws no frame at rest, on hover or with its menu open", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const gear = page.getByRole("button", { name: "機器操作（伺服器這一台）" });
  const frame = () =>
    gear.evaluate((el) => {
      const cs = getComputedStyle(el);
      const probe = document.createElement("canvas").getContext("2d")!;
      const alpha = (css: string) => {
        probe.clearRect(0, 0, 1, 1);
        probe.fillStyle = "#000";
        probe.fillStyle = css;
        probe.fillRect(0, 0, 1, 1);
        return probe.getImageData(0, 0, 1, 1).data[3];
      };
      const sides = ["Top", "Right", "Bottom", "Left"] as const;
      const border = sides.some(
        (s) => parseFloat(cs[`border${s}Width`]) > 0 && cs[`border${s}Style`] !== "none" && alpha(cs[`border${s}Color`]) > 0
      );
      const outline = cs.outlineStyle !== "none" && parseFloat(cs.outlineWidth) > 0;
      return { border, outline };
    });
  expect(await frame(), "at rest").toEqual({ border: false, outline: false });
  await gear.hover();
  expect(await frame(), "hovered").toEqual({ border: false, outline: false });
  await gear.click();
  await expect(page.getByRole("menu")).toBeVisible();
  // Off the gear, so the open state is measured without :hover.
  await page.mouse.move(0, 0);
  expect(await frame(), "open").toEqual({ border: false, outline: false });
});

test("the ⚙ shows a focus ring when reached by keyboard", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const gear = page.getByRole("button", { name: "機器操作（伺服器這一台）" });
  // Tab, not gear.focus(): :focus-visible only applies on keyboard-driven focus.
  for (let i = 0; i < 30 && !(await gear.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(gear).toBeFocused();
  const ring = await gear.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { focusVisible: el.matches(":focus-visible"), style: cs.outlineStyle, width: parseFloat(cs.outlineWidth) };
  });
  expect(ring.focusVisible, "control: keyboard focus is :focus-visible").toBe(true);
  expect(ring.style).not.toBe("none");
  expect(ring.width).toBeGreaterThan(0);
});

test("the 操作 column is one ⚙ button whose menu lists the row's operations", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["normal"]} />);
  const gear = page.getByRole("button", { name: "機器操作（伺服器這一台）" });
  await expect(gear).toBeVisible();
  const cell = await page.locator("tbody td").last().boundingBox();
  const g = await gear.boundingBox();
  expect(g!.height, "the gear stays one control high").toBeLessThanOrEqual(32);
  expect(g!.x + g!.width).toBeLessThanOrEqual(cell!.x + cell!.width);

  await gear.click();
  const items = page.getByRole("menuitem");
  await expect(items).toHaveText(["重新安裝", "解除安裝", "刪除"]);
  const pop = await page.getByRole("menu").boundingBox();
  expect(Math.round(pop!.x + pop!.width), "the menu lines up with the gear's right edge").toBe(
    Math.round(g!.x + g!.width)
  );
  await expect(page.getByTestId("mon-delete-btn")).toBeDisabled();
  await expect(page.getByTestId("mon-install-btn")).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByTestId("mon-uninstall-btn")).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByTestId("mon-delete-btn"), "a disabled item is still reachable, to hear why").toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(page.getByTestId("mon-install-btn")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(items).toHaveCount(0);
  await expect(gear).toBeFocused();
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
const LONG_GEAR = "機器操作（Seth 的 Mac Studio（辦公室三樓靠窗））";

for (const width of [760, 900, 996]) {
  test(`at ${width}px the ⚙ stays inside the scrolled frame and opens its menu`, async ({ mount, page }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["long"]} />);
    expect((await overflow(page)).frame, "control: the frame does scroll at this width").toBeGreaterThan(0);
    const frame = (await page.locator(".mon-table-wrap").boundingBox())!;
    const gear = page.getByRole("button", { name: LONG_GEAR });
    const g = (await gear.boundingBox())!;
    expect(g.x).toBeGreaterThanOrEqual(frame.x);
    expect(g.x + g.width).toBeLessThanOrEqual(frame.x + frame.width);
    await gear.click();
    await expect(page.getByRole("menuitem")).toHaveText(["重新安裝", "解除安裝", "刪除"]);
  });

  test(`at ${width}px the 操作 column covers what scrolls under it and stays on the frame's right edge`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["long"]} />);
    const wrap = page.locator(".mon-table-wrap");
    const lastCell = page.locator("tbody td").last();
    const lastHead = page.locator("thead th").last();

    // At scrollLeft 0 the cells to the left of 操作 run under it. Painted
    // opaque, hiding them changes no pixel inside the 操作 cell.
    const box = (await lastCell.boundingBox())!;
    const clip = { x: box.x, y: box.y, width: box.width, height: box.height };
    const covered = await page.screenshot({ clip });
    await page.evaluate(() => {
      const cells = document.querySelectorAll("tbody td:not(:last-child)");
      cells.forEach((el) => ((el as HTMLElement).style.visibility = "hidden"));
    });
    const bare = await page.screenshot({ clip });
    await page.evaluate(() => {
      const cells = document.querySelectorAll("tbody td:not(:last-child)");
      cells.forEach((el) => ((el as HTMLElement).style.visibility = ""));
    });
    expect(covered.equals(bare), "a scrolled cell shows through the 操作 cell").toBe(true);

    for (const scroll of [0, 40, 10_000]) {
      await wrap.evaluate((el, x) => (el.scrollLeft = x), scroll);
      const frame = (await wrap.boundingBox())!;
      const head = (await lastHead.boundingBox())!;
      const cell = (await lastCell.boundingBox())!;
      // The frame has a 1px border.
      expect(Math.round(head.x + head.width), `header at scrollLeft ${scroll}`).toBe(Math.round(frame.x + frame.width - 1));
      expect(Math.round(cell.x + cell.width), `cell at scrollLeft ${scroll}`).toBe(Math.round(frame.x + frame.width - 1));
      const hit = await page.evaluate(
        ([x, y]) => !!document.elementFromPoint(x, y)?.closest("tbody td:last-child"),
        [cell.x + cell.width / 2, cell.y + cell.height / 2]
      );
      expect(hit, `the ⚙ cell is on top at scrollLeft ${scroll}`).toBe(true);
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
    const gear = (await page.getByRole("button", { name: LONG_GEAR }).boundingBox())!;
    expect(gear.x + gear.width).toBeLessThanOrEqual(frame.x + frame.width);

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

/** The painted opacity of the two "more under here" shades on the first row. */
async function shades(page: Page) {
  return page.evaluate(() => {
    const row = document.querySelector(".mon-table--machines tbody tr")!;
    const op = (el: Element, pseudo: string) => Number(getComputedStyle(el, pseudo).opacity);
    return { left: op(row.firstElementChild!, "::after"), right: op(row.lastElementChild!, "::before") };
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
  await expect.poll(() => shades(page), "at rest the 磁碟 column is under 操作").toEqual({ left: 0, right: 1 });

  // Widening the window, with no scroll and no new data, is what changes it.
  await page.setViewportSize({ width: 1300, height: 900 });
  expect(await page.locator(".mon-table-wrap").evaluate((el) => el.scrollWidth - el.clientWidth), "control: it fits").toBe(0);
  await expect.poll(() => shades(page), "a table that fits has nothing under its pinned columns").toEqual({
    left: 0,
    right: 0,
  });

  await page.setViewportSize({ width: 996, height: 900 });
  await expect.poll(() => shades(page), "narrowed again, the 磁碟 column is back under 操作").toEqual({
    left: 0,
    right: 1,
  });
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

test("at 996px a control reached by keyboard is scrolled out from under the pinned 操作 and 機器 columns", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1016, height: 900 });
  // The very long name: the row that still scrolls at 996px.
  await mount(<MonitorMachinesLayoutStory width={996} states={["long"]} />);
  const wrap = page.locator(".mon-table-wrap");
  const disk = page.getByTestId("disk-usage-trigger");
  // Control: at rest the 磁碟 value sits under the pinned ⚙ column.
  const covered = await disk.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return !(hit === el || el.contains(hit));
  });
  expect(covered, "control: the 磁碟 value starts under the ⚙ column").toBe(true);

  for (let i = 0; i < 30 && !(await disk.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(disk).toBeFocused();
  expect(await wrap.evaluate((el) => el.scrollLeft), "the frame scrolled to the focused control").toBeGreaterThan(0);
  expect(await focusedOnTop(page), "the focused 磁碟 value is not hidden under ⚙").toBe(true);

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
  // 磁碟 value half under the ⚙ column (996px frame).
  await page.setViewportSize({ width: 996, height: 900 });
  const story = await mount(<MonitorMachinesLayoutStory width={1200} states={["long"]} />);
  const wrap = page.locator(".mon-table-wrap");
  const disk = page.getByTestId("disk-usage-trigger");
  const diskAt = await page.evaluate(() => {
    const wrap = document.querySelector(".mon-table-wrap")!;
    const trigger = document.querySelector('[data-testid="disk-usage-trigger"]')!.getBoundingClientRect();
    const gear = document.querySelector(".mon-table--machines tbody td:last-child")!.getBoundingClientRect();
    wrap.scrollLeft += trigger.left + 20 - gear.left;
    const t = document.querySelector('[data-testid="disk-usage-trigger"]')!.getBoundingClientRect();
    const g = document.querySelector(".mon-table--machines tbody td:last-child")!.getBoundingClientRect();
    return { x: t.left + 10, y: t.top + t.height / 2, covered: t.right > g.left, scrollLeft: wrap.scrollLeft };
  });
  expect(diskAt.covered, "control: the 磁碟 value is partly under ⚙").toBe(true);
  await nextFrames(page);
  await page.mouse.click(diskAt.x, diskAt.y);
  await nextFrames(page);
  await expect(disk).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByTestId("disk-usage-panel")).toHaveCount(1);
  expect(await wrap.evaluate((el) => el.scrollLeft)).toBe(diskAt.scrollLeft);

  // Claude version half under the 機器 column (760px frame).
  await page.mouse.click(1, 1);
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

test("on a short window the 磁碟 breakdown stays inside the window and scrolls inside itself", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 220 });
  await mount(<MonitorMachinesLayoutStory width={1200} states={["normal"]} />);
  await page.getByTestId("disk-usage-trigger").click();
  const panel = page.getByTestId("disk-usage-panel");
  await expect(panel).toBeVisible();
  const box = await panel.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, scrolls: el.scrollHeight > el.clientHeight };
  });
  expect(box.scrolls, "control: the breakdown is taller than the window allows").toBe(true);
  expect(box.top).toBeGreaterThanOrEqual(8);
  expect(box.bottom).toBeLessThanOrEqual(220 - 8);
  const scrolled = await panel.evaluate(async (el) => {
    el.scrollTop = el.scrollHeight;
    // The scroll event is dispatched on the next frame.
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
    return el.scrollTop;
  });
  expect(scrolled, "control: the breakdown did scroll").toBeGreaterThan(0);
  await expect(panel, "scrolling the breakdown itself does not close it").toBeVisible();
});

test("narrower desktop: the table scrolls inside its own frame, the page does not", async ({ mount, page }) => {
  await page.setViewportSize({ width: 820, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={800} />);
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
  expect(widths).toEqual([
    [150, 150, 72, 72, 88, 96, 56],
    [150, 150, 72, 72, 88, 96, 56],
    [150, 150, 72, 72, 88, 96, 56],
  ]);
  const spill = await page.evaluate(() => {
    const parts = Array.from(document.querySelector(".mon-machine-name")!.children);
    const right = Math.max(...parts.map((el) => el.getBoundingClientRect().right));
    const cell = document.querySelector("tbody td")!.getBoundingClientRect();
    return Math.round(right - cell.right);
  });
  expect(spill, "the 機器 cell's name, id and badge stay inside their column").toBeLessThanOrEqual(0);
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
  expect(fit.minWidth, "control: the table has a measured minimum").toBeGreaterThan(900);
  expect(fit.minWidth).toBeLessThanOrEqual(fit.frame);
  expect(fit.overflow).toBe(0);
  await expect.poll(() => shades(page)).toEqual({ left: 0, right: 0 });
  const disk = await page.getByTestId("disk-usage-trigger").evaluate((el) => {
    const r = el.getBoundingClientRect();
    const gear = document.querySelector(".mon-table--machines tbody td:last-child")!.getBoundingClientRect();
    const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return { onTop: !!hit && (hit === el || el.contains(hit)), leftOfGear: r.right <= gear.left };
  });
  expect(disk).toEqual({ onTop: true, leftOfGear: true });
});

// Every new site's own machine is named 伺服器這一台, so this is the row most
// tables have. It has to fit the 1280px desktop with room to spare: fonts on a
// real machine measure a few pixels wider than here. The offline 未生效 row has
// the widest 機器 cell of the plain states; it fits too in zh (en's longer
// "not effective" still scrolls it by about 10px).
for (const [language, state] of [
  ["zh", "normal"],
  ["en", "normal"],
  ["zh", "stale"],
] as const) {
  test(`${language}, at 996px (the 1280px desktop) the ${state} 伺服器這一台 row fits with 24px to spare and 磁碟 clear of ⚙`, async ({
    mount,
    page,
  }) => {
    await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
    await page.setViewportSize({ width: 1016, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={996} states={[state]} />);
    await expect(page.locator(".mon-machine-name .mon-table__strong")).toHaveText("伺服器這一台");
    const fit = await fitAt(page);
    expect(fit.frame, "control: the frame is the 1280px desktop's (996px less its border)").toBe(994);
    expect(fit.minWidth, "control: the table has a measured minimum").toBeGreaterThan(800);
    expect(fit.overflow).toBe(0);
    if (state === "normal") expect(fit.frame - fit.minWidth, "room to spare").toBeGreaterThanOrEqual(24);
    const box = async (sel: string) => (await page.locator(sel).boundingBox())!;
    const [diskHead, gearHead, diskCell, gearCell, diskValue, frame] = await Promise.all([
      box(".mon-table--machines thead th:nth-child(7)"),
      box(".mon-table--machines thead th:nth-child(8)"),
      box('[data-testid="mon-disk"]'),
      box(".mon-table--machines tbody td:last-child"),
      box('[data-testid="disk-usage-trigger"]'),
      box(".mon-table-wrap"),
    ]);
    const right = (b: { x: number; width: number }) => b.x + b.width;
    expect({
      head: right(diskHead) <= gearHead.x + 0.5,
      cell: right(diskCell) <= gearCell.x + 0.5,
      value: right(diskValue) <= gearCell.x + 0.5,
      gearInFrame: right(gearCell) <= right(frame) + 0.5,
    }).toEqual({ head: true, cell: true, value: true, gearInFrame: true });
  });
}

test("the table's minimum is the fixed columns plus the widest 機器 cell's content, so 機器 is never cut and never padded out", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 780, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={760} states={["normal", "stale", "named", "long"]} />);
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
        scrolls: (section.querySelector(".mon-table-wrap") as HTMLElement).scrollWidth > 760,
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
  await expect(name).toHaveText("Seth 的 Mac Studio（辦公室三樓靠窗）");
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

// The story's 996px box less its 1px border; CSS alone sets it.
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

// A rename lands as new text in the name without this table re-rendering (and
// so does a web font arriving); the minimum follows the name both ways.
test("a machine name that changes in place is measured again, longer and shorter", async ({ mount, page }) => {
  const LONG = "Seth 的 Mac Studio（辦公室三樓靠窗）";
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

  const name = page.locator(".mon-machine-name .mon-table__strong");
  await name.evaluate((el, text) => (el.textContent = text), LONG);
  await expect.poll(() => measured(page)).toEqual(long);
  await name.evaluate((el, text) => (el.textContent = text), SHORT);
  await expect.poll(() => measured(page)).toEqual(short);
});

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
  const before = (await page.locator(".mon-machine-name > .inline-edit").boundingBox())!.width;
  await page.getByRole("button", { name: "機器改名" }).click();
  await expect(page.getByRole("textbox", { name: "機器改名" }), "control: the field is open").toBeFocused();
  await expect
    .poll(async () => (await page.locator(".mon-machine-name > .inline-edit--editing").boundingBox())!.width > before + 20, "control: the field fills the cell's slack")
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

test("when the last machine goes, its minimum goes with it and the 無機器 row does not scroll", async ({ mount, page }) => {
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
  await expect(page.locator("tbody td"), "control: the 無機器 row").toHaveText("尚無機器,請先新增機器 / 上線");
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
  expect(await machineText(page)).toEqual(["伺服器這一台 m-server-self", "伺服器這一台 m-server-self 未生效"]);
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
    expect(await machineText(page)).toEqual(["伺服器這一台 m-server-self", "伺服器這一台 m-server-self 未生效"]);
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
