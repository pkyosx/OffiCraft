// 檔案庫 list at phone and desktop width. At 390 each row is one stacked block
// — kind icon, name and description on top with 上傳新版本／永久刪除 at its
// right, then the ID badge and 最後修改 — with no sideways scroll on the page or
// the table's own wrapper, and the header row hidden. At 1280 it is the
// four-column table: header shown, ID → 檔案 → 最後修改 → 操作 side by side,
// a long name clipped inside its own cell at both widths.
//
// Every measurement scores a missing element as a FAILURE — a renamed class
// must not turn these checks into no-ops.
import { test, expect } from "@playwright/experimental-ct-react";
import { SharedDocsListStory } from "./stories/SharedDocsListStory";

type Box = { left: number; right: number; top: number; bottom: number };
type RowGeom = {
  id: string;
  row: Box;
  cells: { cls: string; box: Box }[];
  kind: Box | null;
  name: Box | null;
  fileText: Box | null;
  description: Box | null;
  idBadge: Box | null;
  updated: Box | null;
  actions: Box | null;
  buttons: Box[];
};

async function measure(page: any): Promise<{
  pageOver: number;
  wrapOver: number;
  wrap: Box | null;
  theadDisplay: string | null;
  rows: RowGeom[];
}> {
  return page.evaluate(() => {
    const r = (el: Element | null) => {
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const se = document.scrollingElement!;
    const wrap = document.querySelector(".shared-docs__table-wrap") as HTMLElement | null;
    const thead = document.querySelector('[data-testid="shared-docs-table"] thead');
    return {
      pageOver: se.scrollWidth - se.clientWidth,
      wrapOver: wrap ? wrap.scrollWidth - wrap.clientWidth : -2,
      wrap: r(wrap),
      theadDisplay: thead ? getComputedStyle(thead).display : null,
      rows: [...document.querySelectorAll('[data-testid="shared-doc-row"]')].map((row) => ({
        id: row.getAttribute("data-doc-id") ?? "",
        row: r(row)!,
        cells: [...row.children].map((c) => ({ cls: c.className.toString(), box: r(c)! })),
        kind: r(row.querySelector(".shared-docs__kind")),
        name: r(row.querySelector(".shared-docs__name")),
        fileText: r(row.querySelector(".shared-docs__file-text")),
        description: r(row.querySelector(".shared-docs__file-description")),
        idBadge: r(row.querySelector('[data-testid="shared-doc-id"]')),
        updated: r(row.querySelector('[data-testid="shared-doc-updated"]')),
        actions: r(row.querySelector(".shared-docs__actions")),
        buttons: [...row.querySelectorAll(".shared-docs__actions button")].map((b) => r(b)!),
      })),
    };
  });
}

const inside = (inner: Box, outer: Box) =>
  inner.left >= outer.left - 1 &&
  inner.right <= outer.right + 1 &&
  inner.top >= outer.top - 1 &&
  inner.bottom <= outer.bottom + 1;
const mid = (b: Box) => (b.top + b.bottom) / 2;

async function mountAt(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<SharedDocsListStory />);
  await expect(cmp.locator('[data-testid="shared-doc-row"]')).toHaveCount(3);
  return cmp;
}

test("at 390px every row is a stacked block with all its parts inside it and nothing scrolls sideways", async ({ mount, page }) => {
  await mountAt(mount, page, 390);
  const g = await measure(page);
  expect.soft(g.pageOver, "page horizontal scroll").toBeLessThanOrEqual(1);
  expect.soft(g.wrapOver, ".shared-docs__table-wrap missing").not.toBe(-2);
  expect.soft(g.wrapOver, "table wrapper horizontal scroll").toBeLessThanOrEqual(1);
  expect.soft(g.theadDisplay, "header row").toBe("none");
  expect(g.rows.length).toBe(3);
  for (const row of g.rows) {
    const tag = `[390px] ${row.id}`;
    expect.soft(row.cells.length, `${tag} cells`).toBe(4);
    for (const c of row.cells) {
      expect.soft(c.box.right, `${tag} "${c.cls}" right edge past the row`).toBeLessThanOrEqual(row.row.right + 1);
      expect.soft(c.box.left, `${tag} "${c.cls}" left edge before the row`).toBeGreaterThanOrEqual(row.row.left - 1);
    }
    if (g.wrap) expect.soft(inside(row.row, g.wrap), `${tag} row outside the wrapper`).toBe(true);
    for (const [part, box] of [
      ["kind icon", row.kind],
      ["name", row.name],
      ["ID badge", row.idBadge],
      ["最後修改", row.updated],
      ["actions", row.actions],
    ] as const) {
      expect.soft(box, `${tag} ${part} missing`).not.toBeNull();
      if (box) expect.soft(inside(box, row.row), `${tag} ${part} outside the row`).toBe(true);
    }
    expect.soft(row.buttons.length, `${tag} 上傳新版本 + 永久刪除`).toBe(2);
    for (const b of row.buttons) expect.soft(inside(b, row.row), `${tag} action button outside the row`).toBe(true);
    if (!row.kind || !row.name || !row.idBadge || !row.updated || !row.actions) continue;
    expect.soft(row.idBadge.top, `${tag} ID badge not under the name`).toBeGreaterThanOrEqual(row.name.bottom - 1);
    if (row.description)
      expect.soft(row.idBadge.top, `${tag} ID badge not under the description`).toBeGreaterThanOrEqual(row.description.bottom - 1);
    expect.soft(Math.abs(mid(row.idBadge) - mid(row.updated)), `${tag} ID and 最後修改 not on one line`).toBeLessThanOrEqual(4);
    expect.soft(row.updated.left, `${tag} 最後修改 not after the ID`).toBeGreaterThanOrEqual(row.idBadge.right - 1);
    expect.soft(row.actions.left, `${tag} actions not right of the name`).toBeGreaterThanOrEqual(row.name.right - 1);
    expect.soft(row.actions.bottom, `${tag} actions not on the name's line`).toBeLessThanOrEqual(row.idBadge.top + 1);
    expect.soft(row.row.right - row.actions.right, `${tag} actions not at the right edge`).toBeLessThanOrEqual(16);
    expect.soft(row.fileText, `${tag} name/description block missing`).not.toBeNull();
    if (row.fileText)
      expect
        .soft(row.kind.bottom - row.kind.top, `${tag} kind icon taller than the name and description`)
        .toBeLessThanOrEqual(row.fileText.bottom - row.fileText.top + 1);
  }
});

test("at 1280px the list stays the four-column table with its header", async ({ mount, page }) => {
  await mountAt(mount, page, 1280);
  const g = await measure(page);
  expect.soft(g.pageOver, "page horizontal scroll").toBeLessThanOrEqual(1);
  expect.soft(g.wrapOver, "table wrapper horizontal scroll").toBeLessThanOrEqual(1);
  expect.soft(g.theadDisplay, "header row").toBe("table-header-group");
  const display = await page.evaluate(() => {
    const row = document.querySelector('[data-testid="shared-doc-row"]');
    return row ? [getComputedStyle(row).display, ...[...row.children].map((c) => getComputedStyle(c).display)] : [];
  });
  expect.soft(display, "row and cell display").toEqual(["table-row", "table-cell", "table-cell", "table-cell", "table-cell"]);
  for (const row of g.rows) {
    const tag = `[1280px] ${row.id}`;
    expect.soft(row.cells.length, `${tag} cells`).toBe(4);
    const mids = row.cells.map((c) => mid(c.box));
    expect.soft(Math.max(...mids) - Math.min(...mids), `${tag} cells not on one line`).toBeLessThanOrEqual(1);
    row.cells.forEach((c, i) => {
      const next = row.cells[i + 1];
      if (next) expect.soft(c.box.right, `${tag} "${c.cls}" not left of "${next.cls}"`).toBeLessThanOrEqual(next.box.left + 1);
    });
    if (row.kind) expect.soft(row.kind.bottom - row.kind.top, `${tag} kind icon`).toBe(44);
    const fileCell = row.cells[1]?.box;
    for (const [part, box] of [["name", row.name], ["description", row.description]] as const)
      if (fileCell && box) expect.soft(box.right, `${tag} ${part} runs out of the 檔案 cell`).toBeLessThanOrEqual(fileCell.right + 1);
  }
});
