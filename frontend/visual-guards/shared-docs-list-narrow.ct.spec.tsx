// 檔案庫 list at phone and desktop width: every document is its own card —
// the lore card (.lore-row) itself, same radius, background, border and
// padding — with the lore list's gap between consecutive cards, and no table
// at any width. Inside a card, the ID badge and the kind chip sit on the first
// line with 上傳新版本／永久刪除 at its right (below 390 the kind chip wraps whole
// under the ID, like the lore card's badges), then the name across the card's
// full width, the description, and 最後修改 last — with no sideways scroll on
// the page or the list. Above the list, the count of files sits at the bar's
// left end on ＋ 新增's line.
//
// Every measurement scores a missing element as a FAILURE — a renamed class
// must not turn these checks into no-ops.
import { test, expect } from "@playwright/experimental-ct-react";
import { SharedDocsListStory } from "./stories/SharedDocsListStory";

type Box = { left: number; right: number; top: number; bottom: number };
type CardLook = { radius: string; background: string; border: string; padding: string };
type RowGeom = {
  id: string;
  row: Box;
  look: CardLook;
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
  listOver: number;
  list: Box | null;
  tables: number;
  listGap: string | null;
  loreLook: CardLook;
  loreGap: string;
  rows: RowGeom[];
}> {
  return page.evaluate(() => {
    const r = (el: Element | null) => {
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const look = (el: Element) => {
      const cs = getComputedStyle(el);
      return {
        radius: cs.borderRadius,
        background: cs.backgroundColor,
        border: `${cs.borderTopWidth} ${cs.borderTopStyle} ${cs.borderTopColor}`,
        padding: cs.padding,
      };
    };
    const se = document.scrollingElement!;
    const list = document.querySelector('[data-testid="shared-docs-list"]') as HTMLElement | null;
    const loreList = document.createElement("div");
    loreList.className = "lore-section__body";
    const loreCard = document.createElement("div");
    loreCard.className = "lore-row";
    loreList.appendChild(loreCard);
    document.body.appendChild(loreList);
    const loreLook = look(loreCard);
    const loreGap = getComputedStyle(loreList).rowGap;
    loreList.remove();
    return {
      pageOver: se.scrollWidth - se.clientWidth,
      listOver: list ? list.scrollWidth - list.clientWidth : -2,
      list: r(list),
      tables: document.querySelectorAll('[data-testid="shared-docs-page"] table').length,
      listGap: list ? getComputedStyle(list).rowGap : null,
      loreLook,
      loreGap,
      rows: [...document.querySelectorAll('[data-testid="shared-doc-row"]')].map((row) => {
        const name = row.querySelector(".shared-docs__name");
        return {
          id: row.getAttribute("data-doc-id") ?? "",
          row: r(row)!,
          look: look(row),
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

for (const width of [320, 375, 390, 1440]) {
  test(`at ${width}px every file is its own lore card, a gap between consecutive cards, each stacking ID, kind and actions over a full-width name, then the description and 最後修改, with no table and nothing scrolling sideways`, async ({ mount, page }) => {
    await mountAt(mount, page, width);
    const g = await measure(page);
    expect.soft(g.pageOver, "page horizontal scroll").toBeLessThanOrEqual(1);
    expect.soft(g.listOver, "list missing").not.toBe(-2);
    expect.soft(g.listOver, "list horizontal scroll").toBeLessThanOrEqual(1);
    expect.soft(g.tables, "table on the page").toBe(0);
    expect.soft(g.listGap, "gap between cards is the lore list's").toBe(g.loreGap);
    expect(g.rows.length).toBe(3);
    if (width < 720)
      expect.soft(g.rows.filter((row) => row.nameCut).length, "long names clipped with …").toBeGreaterThanOrEqual(2);
    g.rows.forEach((row, i) => {
      const tag = `[${width}px] ${row.id}`;
      expect.soft(row.look, `${tag} card look differs from the lore card`).toEqual(g.loreLook);
      expect.soft(parseFloat(row.look.radius), `${tag} card corners`).toBeGreaterThan(0);
      const prev = g.rows[i - 1];
      if (prev) {
        expect.soft(row.row.top - prev.row.bottom, `${tag} no visible gap after the previous card`).toBeGreaterThanOrEqual(6);
        expect.soft(row.row.top - prev.row.bottom, `${tag} gap not the lore list's`).toBeCloseTo(parseFloat(g.loreGap), 0);
      }
      expect.soft(row.cells.length, `${tag} cells`).toBe(4);
      for (const c of row.cells) {
        expect.soft(c.box.right, `${tag} "${c.cls}" right edge past the card`).toBeLessThanOrEqual(row.row.right + 1);
        expect.soft(c.box.left, `${tag} "${c.cls}" left edge before the card`).toBeGreaterThanOrEqual(row.row.left - 1);
      }
      if (g.list) expect.soft(inside(row.row, g.list), `${tag} card outside the list`).toBe(true);
      for (const [part, box] of [
        ["kind chip", row.kind],
        ["name", row.name],
        ["pencil", row.pencil],
        ["ID badge", row.idBadge],
        ["最後修改", row.updated],
        ["actions", row.actions],
      ] as const) {
        expect.soft(box, `${tag} ${part} missing`).not.toBeNull();
        if (box) expect.soft(inside(box, row.row), `${tag} ${part} outside the card`).toBe(true);
      }
      expect.soft(row.buttons.length, `${tag} 上傳新版本 + 永久刪除`).toBe(2);
      for (const b of row.buttons) expect.soft(inside(b, row.row), `${tag} action button outside the card`).toBe(true);
      if (!row.kind || !row.name || !row.pencil || !row.idBadge || !row.updated || !row.actions) return;

      const padLeft = parseFloat(row.look.padding.split(" ").at(-1)!);
      const padRight = parseFloat(row.look.padding.split(" ")[1] ?? row.look.padding);
      const contentLeft = row.row.left + 1 + padLeft;
      const contentRight = row.row.right - 1 - padRight;
      expect.soft(row.idBadge.left - contentLeft, `${tag} ID badge not at the card's left edge`).toBeLessThanOrEqual(2);
      if (width < 390 && row.kind.top >= row.idBadge.bottom - 1) {
        expect.soft(row.kind.left - contentLeft, `${tag} wrapped kind chip not at the card's left edge`).toBeLessThanOrEqual(2);
        expect.soft(Math.abs(row.actions.top - row.idBadge.top), `${tag} actions not level with the ID`).toBeLessThanOrEqual(2);
      } else {
        expect.soft(row.kind.left, `${tag} kind chip not after the ID`).toBeGreaterThanOrEqual(row.idBadge.right - 1);
        expect.soft(Math.abs(mid(row.kind) - mid(row.idBadge)), `${tag} kind chip not on the ID's line`).toBeLessThanOrEqual(4);
        expect.soft(Math.abs(mid(row.actions) - mid(row.idBadge)), `${tag} actions not on the ID's line`).toBeLessThanOrEqual(4);
      }
      for (const [part, box] of [["ID badge", row.idBadge], ["kind chip", row.kind]] as const)
        expect.soft(row.actions.left, `${tag} actions overlap the ${part}`).toBeGreaterThanOrEqual(box.right + 1);
      expect.soft(contentRight - row.actions.right, `${tag} actions not at the right edge`).toBeLessThanOrEqual(2);

      const firstLineBottom = Math.max(row.idBadge.bottom, row.kind.bottom, row.actions.bottom);
      expect.soft(row.name.top, `${tag} name not under the first line`).toBeGreaterThanOrEqual(firstLineBottom - 1);
      expect.soft(row.name.left - contentLeft, `${tag} name not at the card's left edge`).toBeLessThanOrEqual(2);
      if (row.nameCut)
        expect.soft(contentRight - row.pencil.right, `${tag} clipped name + pencil not using the full width`).toBeLessThanOrEqual(2);
      let above = row.name.bottom;
      if (row.description) {
        expect.soft(row.description.top, `${tag} description not under the name`).toBeGreaterThanOrEqual(above - 1);
        above = row.description.bottom;
      }
      expect.soft(row.updated.top, `${tag} 最後修改 not on the last line`).toBeGreaterThanOrEqual(above - 1);
    });
  });
}

for (const width of [320, 390, 1440]) {
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
