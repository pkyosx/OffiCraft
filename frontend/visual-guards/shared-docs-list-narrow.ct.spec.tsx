// 檔案庫 list at phone and desktop width. On a phone (320/375/390) each row is
// one stacked block in the lore card's order: the ID badge and the kind chip
// on the first line with 上傳新版本／永久刪除 at its right (at 320 the kind chip
// wraps whole under the ID, like the lore card's badges), then the name across
// the row's full width, the description, and 最後修改 last — with no sideways
// scroll on the page or the table's own wrapper, and the header row hidden.
// At 1280 it is the four-column table: header shown, ID (with the kind chip
// after it) → 檔案 → 最後修改 → 操作 side by side, a long name clipped inside
// its own cell at both widths. Above the list, the count of files sits at the
// bar's left end on ＋ 新增's line.
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
  nameCut: boolean;
  pencil: Box | null;
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
      rows: [...document.querySelectorAll('[data-testid="shared-doc-row"]')].map((row) => {
        const name = row.querySelector(".shared-docs__name");
        return {
          id: row.getAttribute("data-doc-id") ?? "",
          row: r(row)!,
          cells: [...row.children].map((c) => ({ cls: c.className.toString(), box: r(c)! })),
          kind: r(row.querySelector('[data-testid^="shared-doc-kind-"]')),
          name: r(name),
          nameCut: !!name && name.scrollWidth > name.clientWidth + 0.5,
          pencil: r(row.querySelector('[data-testid="shared-doc-edit-info"]')),
          description: r(row.querySelector(".shared-docs__file-description")),
          idBadge: r(row.querySelector('[data-testid="shared-doc-id"]')),
          updated: r(row.querySelector('[data-testid="shared-doc-updated"]')),
          actions: r(row.querySelector(".shared-docs__actions")),
          buttons: [...row.querySelectorAll(".shared-docs__actions button")].map((b) => r(b)!),
        };
      }),
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

for (const width of [320, 375, 390]) {
  test(`at ${width}px every row stacks ID, kind and actions over a full-width name, then the description and 最後修改, with nothing scrolling sideways`, async ({ mount, page }) => {
    await mountAt(mount, page, width);
    const g = await measure(page);
    expect.soft(g.pageOver, "page horizontal scroll").toBeLessThanOrEqual(1);
    expect.soft(g.wrapOver, ".shared-docs__table-wrap missing").not.toBe(-2);
    expect.soft(g.wrapOver, "table wrapper horizontal scroll").toBeLessThanOrEqual(1);
    expect.soft(g.theadDisplay, "header row").toBe("none");
    expect(g.rows.length).toBe(3);
    expect.soft(g.rows.filter((row) => row.nameCut).length, "long names clipped with …").toBeGreaterThanOrEqual(2);
    for (const row of g.rows) {
      const tag = `[${width}px] ${row.id}`;
      expect.soft(row.cells.length, `${tag} cells`).toBe(4);
      for (const c of row.cells) {
        expect.soft(c.box.right, `${tag} "${c.cls}" right edge past the row`).toBeLessThanOrEqual(row.row.right + 1);
        expect.soft(c.box.left, `${tag} "${c.cls}" left edge before the row`).toBeGreaterThanOrEqual(row.row.left - 1);
      }
      if (g.wrap) expect.soft(inside(row.row, g.wrap), `${tag} row outside the wrapper`).toBe(true);
      for (const [part, box] of [
        ["kind chip", row.kind],
        ["name", row.name],
        ["pencil", row.pencil],
        ["ID badge", row.idBadge],
        ["最後修改", row.updated],
        ["actions", row.actions],
      ] as const) {
        expect.soft(box, `${tag} ${part} missing`).not.toBeNull();
        if (box) expect.soft(inside(box, row.row), `${tag} ${part} outside the row`).toBe(true);
      }
      expect.soft(row.buttons.length, `${tag} 上傳新版本 + 永久刪除`).toBe(2);
      for (const b of row.buttons) expect.soft(inside(b, row.row), `${tag} action button outside the row`).toBe(true);
      if (!row.kind || !row.name || !row.pencil || !row.idBadge || !row.updated || !row.actions) continue;

      expect.soft(row.idBadge.left - row.row.left, `${tag} ID badge not at the row's left edge`).toBeLessThanOrEqual(4);
      if (width === 320 && row.kind.top >= row.idBadge.bottom - 1) {
        expect.soft(row.kind.left - row.row.left, `${tag} wrapped kind chip not at the row's left edge`).toBeLessThanOrEqual(4);
        expect.soft(Math.abs(row.actions.top - row.idBadge.top), `${tag} actions not level with the ID`).toBeLessThanOrEqual(2);
      } else {
        expect.soft(row.kind.left, `${tag} kind chip not after the ID`).toBeGreaterThanOrEqual(row.idBadge.right - 1);
        expect.soft(Math.abs(mid(row.kind) - mid(row.idBadge)), `${tag} kind chip not on the ID's line`).toBeLessThanOrEqual(4);
        expect.soft(Math.abs(mid(row.actions) - mid(row.idBadge)), `${tag} actions not on the ID's line`).toBeLessThanOrEqual(4);
      }
      for (const [part, box] of [["ID badge", row.idBadge], ["kind chip", row.kind]] as const)
        expect.soft(row.actions.left, `${tag} actions overlap the ${part}`).toBeGreaterThanOrEqual(box.right + 1);
      expect.soft(row.row.right - row.actions.right, `${tag} actions not at the right edge`).toBeLessThanOrEqual(4);

      const firstLineBottom = Math.max(row.idBadge.bottom, row.kind.bottom, row.actions.bottom);
      expect.soft(row.name.top, `${tag} name not under the first line`).toBeGreaterThanOrEqual(firstLineBottom - 1);
      expect.soft(row.name.left - row.row.left, `${tag} name not at the row's left edge`).toBeLessThanOrEqual(4);
      if (row.nameCut)
        expect.soft(row.row.right - row.pencil.right, `${tag} clipped name + pencil not using the full width`).toBeLessThanOrEqual(4);
      let above = row.name.bottom;
      if (row.description) {
        expect.soft(row.description.top, `${tag} description not under the name`).toBeGreaterThanOrEqual(above - 1);
        above = row.description.bottom;
      }
      expect.soft(row.updated.top, `${tag} 最後修改 not on the last line`).toBeGreaterThanOrEqual(above - 1);
    }
  });
}

test("at 1280px the list stays the four-column table with its header, the kind chip after the ID", async ({ mount, page }) => {
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
    const idCell = row.cells[0]?.box;
    expect.soft(row.kind, `${tag} kind chip missing`).not.toBeNull();
    if (idCell && row.kind && row.idBadge) {
      expect.soft(inside(row.kind, idCell), `${tag} kind chip outside the ID cell`).toBe(true);
      expect.soft(row.kind.left, `${tag} kind chip not after the ID`).toBeGreaterThanOrEqual(row.idBadge.right - 1);
      expect.soft(Math.abs(mid(row.kind) - mid(row.idBadge)), `${tag} kind chip not on the ID's line`).toBeLessThanOrEqual(2);
    }
    const fileCell = row.cells[1]?.box;
    for (const [part, box] of [["name", row.name], ["description", row.description]] as const)
      if (fileCell && box) expect.soft(box.right, `${tag} ${part} runs out of the 檔案 cell`).toBeLessThanOrEqual(fileCell.right + 1);
  }
});

for (const width of [320, 390, 1280]) {
  for (const language of ["zh", "en"] as const) {
    test(`width ${width}, ${language}: the file count sits whole on one line at the bar's left end, on ＋ 新增's line`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<SharedDocsListStory />);
      const count = cmp.getByTestId("shared-docs-count");
      await expect(count).toHaveText(language === "zh" ? "3 份檔案" : "3 files");
      const m = await count.evaluate((el: HTMLElement) => {
        const r = (e: Element | Range) => {
          const b = e.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const bar = el.closest(".shared-docs__bar")!;
        const range = document.createRange();
        range.selectNodeContents(el);
        const se = document.scrollingElement!;
        return {
          count: r(el),
          text: r(range),
          bar: r(bar),
          add: r(bar.querySelector('[data-testid="shared-docs-add-menu"]')!),
          lines: new Set(Array.from(range.getClientRects()).map((x) => Math.round(x.top))).size,
          cut: el.scrollWidth > el.clientWidth + 0.5,
          pageOver: se.scrollWidth - se.clientWidth,
        };
      });
      expect.soft(m.pageOver, "page horizontal scroll").toBeLessThanOrEqual(1);
      expect.soft(m.lines, "count on one line").toBe(1);
      expect.soft(m.cut, "count whole").toBe(false);
      expect.soft(Math.abs(m.count.left - m.bar.left), "count at the bar's left end").toBeLessThanOrEqual(1);
      expect.soft(Math.abs(m.add.right - m.bar.right), "＋ 新增 at the bar's right end").toBeLessThanOrEqual(1);
      expect.soft(m.count.right, "count left of ＋ 新增").toBeLessThanOrEqual(m.add.left);
      expect.soft(Math.abs(mid(m.text) - mid(m.add)), "count on ＋ 新增's line").toBeLessThanOrEqual(2);
    });
  }
}
