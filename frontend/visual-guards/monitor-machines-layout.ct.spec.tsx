// 機器資訊 table layout: the columns stay put whatever a row is marked with.
//
// The owner's report: adding the 版本太舊 / 未登入 chips, or a row going stale
// (過期 on every cell), widened its cells and moved every column to its right,
// and the three action buttons stacked into two or three rows. jsdom has no
// layout, so this is measured in a real browser on the real MachinesTable.
//
// MUTANTS (each verified red):
//   drop `table-layout: fixed`              → column x differs between states
//   drop the CellStack (marks inline again) → column x differs between states
//   `.mon-stale` border back to --color-border → frame contrast below 1.5
//   (in the built-in palette --color-border IS --color-card)
//   drop the table's min-width               → 機器 name cut short at 800px
//   menu focus counting disabled items       → keyboard test
//   menu aligned to the gear's left edge     → menu right edge test
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
  const edges = await columnEdges(page);
  expect(edges.normal.th).toHaveLength(7);
  expect(edges.normal.td).toHaveLength(7);
  for (const state of ["chips", "stale"] as const) {
    expect(edges[state].th, `${state} header x`).toEqual(edges.normal.th);
    expect(edges[state].td, `${state} cell x`).toEqual(edges.normal.td);
  }
});

test("a mark sits on its own line under the value, inside its column", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  await mount(<MonitorMachinesLayoutStory states={["stale"]} />);
  const boxes = await page.evaluate(() => {
    const box = (el: Element | null) => (el as HTMLElement).getBoundingClientRect();
    const out = [];
    for (const id of ["mon-claude-version", "mon-codex-version", "mon-cpu", "mon-ram", "mon-power"]) {
      const cell = document.querySelector(`[data-testid="${id}"]`)!;
      const stack = cell.querySelector(".mon-cell-stack")!;
      const value = stack.firstElementChild!;
      const marks = stack.querySelector(".mon-cell-marks")!;
      const c = box(cell);
      const v = box(value);
      const m = box(marks);
      out.push({ id, below: m.top >= v.bottom - 0.5, inside: m.left >= c.left && m.right <= c.right });
    }
    return out;
  });
  for (const b of boxes) {
    expect(b, b.id).toEqual({ id: b.id, below: true, inside: true });
  }
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
  await expect(page.getByTestId("mon-install-btn"), "a disabled item is skipped").toBeFocused();
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

test("narrower desktop: the table scrolls inside its own frame, the page does not", async ({ mount, page }) => {
  await page.setViewportSize({ width: 820, height: 900 });
  await mount(<MonitorMachinesLayoutStory width={800} />);
  const over = await page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
  );
  expect(over).toBeLessThanOrEqual(1);
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
  const over = await page.evaluate(
    () => document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth
  );
  expect(over).toBeLessThanOrEqual(1);
});
