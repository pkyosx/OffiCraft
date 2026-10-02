// 機器資訊 table layout: the columns stay put whatever a row is marked with.
//
// The owner's report: adding the 版本太舊 / 未登入 chips, or a row going stale
// (過期 on every cell), widened its cells and moved every column to its right,
// and the three action buttons stacked into two or three rows. jsdom has no
// layout, so this is measured in a real browser on the real MachinesTable.
// The owner then picked the marks on the SAME line as the value, in fixed
// columns, Claude and Codex one width, and a ⚙ with no frame.
//
// MUTANTS (each verified red):
//   marks inline AND auto table layout (the layout before the fix)
//                                           → column x differs between states
//   marks back on a line of their own       → same-line test, row height test
//   Claude / Codex column narrowed           → a mark spills out of its cell
//   Codex column a different width           → equal-width test
//   drop `table-layout: fixed` or the column widths → 800px test
//   操作 column not sticky                  → gear off-screen at 760/900px
//   操作 cells transparent                  → a scrolled cell shows through
//   操作 header not sticky                  → header leaves the right edge
//   phone card mode disabled                → phone test (the frame scrolls)
//   `.mon-stale` border back to --color-border → frame contrast below 1.5
//   (in the built-in palette --color-border IS --color-card)
//   drop the table's min-width               → 機器 name cut short at 800px
//   menu focus counting disabled items       → keyboard test
//   menu aligned to the gear's left edge     → menu right edge test
//   ⚙ border back at rest, on hover or open  → frameless ⚙ test
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

test("at the 1000px desktop content width, every column starts at the same x in the plain, chipped and stale rows", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory />);
  expect((await overflow(page)).frame, "the table fits the desktop content width without scrolling").toBe(0);
  const edges = await columnEdges(page);
  expect(edges.normal.th).toHaveLength(7);
  expect(edges.normal.td).toHaveLength(7);
  for (const state of ["chips", "stale"] as const) {
    expect(edges[state].th, `${state} header x`).toEqual(edges.normal.th);
    expect(edges[state].td, `${state} cell x`).toEqual(edges.normal.td);
  }
});

const MARKED_CELLS = ["mon-claude-version", "mon-codex-version", "mon-cpu", "mon-ram", "mon-power"];

/** For every marked cell of the mounted rows: whether each mark shares the
 * value's line, and whether value, marks and chevron all stay inside the cell. */
async function markPlacement(page: Page) {
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
        out.push({
          id: `${section.getAttribute("data-state")} ${id}`,
          marks: line.querySelectorAll(".mon-cell-marks > *").length,
          sameLine: parts.every((el) => Math.abs(mid(box(el)) - mid(value)) <= 2),
          inside: [value, ...parts.map(box)].every((r) => r.left >= c.left && r.right <= right + 0.5),
        });
      }
    }
    return out;
  }, MARKED_CELLS);
}

for (const width of [1000, 900]) {
  test(`at ${width}px a mark sits on the value's line, and the most a cell carries fits its column`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: width + 500, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} />);
    const placed = await markPlacement(page);
    const marked = Object.fromEntries(placed.map((p) => [p.id, p.marks]));
    // Control: the worst cells really are carrying two marks each.
    expect(marked["chips mon-claude-version"]).toBe(2);
    expect(marked["chips mon-codex-version"]).toBe(2);
    expect(marked["stale mon-claude-version"]).toBe(2);
    expect(marked["stale mon-codex-version"]).toBe(2);
    expect(marked["stale mon-cpu"]).toBe(1);
    for (const p of placed) {
      expect(p, p.id).toEqual({ ...p, sameLine: true, inside: true });
    }
  });
}

test("a marked row is no taller than a plain one", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory />);
  const heights = await page.evaluate(() =>
    Array.from(document.querySelectorAll("[data-state] tbody tr")).map((tr) =>
      Math.round(tr.getBoundingClientRect().height)
    )
  );
  expect(heights).toHaveLength(3);
  expect(heights[1], "chips row").toBe(heights[0]);
  expect(heights[2], "stale row").toBe(heights[0]);
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

for (const width of [760, 900]) {
  test(`at ${width}px the ⚙ stays inside the scrolled frame and opens its menu`, async ({ mount, page }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["stale"]} />);
    expect((await overflow(page)).frame, "control: the frame does scroll at this width").toBeGreaterThan(0);
    const frame = (await page.locator(".mon-table-wrap").boundingBox())!;
    const gear = page.getByRole("button", { name: "機器操作（伺服器這一台）" });
    const g = (await gear.boundingBox())!;
    expect(g.x).toBeGreaterThanOrEqual(frame.x);
    expect(g.x + g.width).toBeLessThanOrEqual(frame.x + frame.width);
    await gear.click();
    await expect(page.getByRole("menuitem")).toHaveText(["安裝", "解除安裝", "刪除"]);
  });

  test(`at ${width}px the 操作 column covers what scrolls under it and stays on the frame's right edge`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: width + 20, height: 900 });
    await mount(<MonitorMachinesLayoutStory width={width} states={["stale"]} />);
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

test("narrower desktop: the table scrolls inside its own frame, the page does not", async ({ mount, page }) => {
  await page.setViewportSize({ width: 820, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={800} />);
  const over = await overflow(page);
  expect(over.page).toBeLessThanOrEqual(1);
  expect(over.monitor, ".monitor scrolls vertically and would swallow a sideways spill").toBeLessThanOrEqual(1);
  const edges = await columnEdges(page);
  expect(edges.stale.td).toEqual(edges.normal.td);
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
