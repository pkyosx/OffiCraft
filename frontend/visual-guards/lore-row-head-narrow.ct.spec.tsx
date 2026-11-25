// 傳承 section headers and row headers at phone and desktop width: the badges
// wrap as whole pills, the 屬於 chip never gets squeezed to its glyph with the
// caret spilling out, only a name longer than a whole line ellipsizes, and the
// checkbox and the actions (提到最新／編輯, or 「⋯」 on a phone) stay inside the
// head without overlapping the badges.
//
// Soft assertions on purpose: a page-level overflow failing first would
// otherwise hide which per-badge check a mutant also broke.
//
// Every measurement scores a missing element as a FAILURE — a renamed class
// must not turn these checks into no-ops.
import { test, expect } from "@playwright/experimental-ct-react";
import { LoreRowHeadStory } from "./stories/LoreRowHeadStory";

type Box = { left: number; right: number; top: number; bottom: number };
type RowGeom = {
  id: string;
  head: Box | null;
  row: Box | null;
  select: Box | null;
  actions: Box | null;
  actionButtons: { text: string; box: Box }[];
  badges: { cls: string; box: Box }[];
  mark: Box | null;
  chip: Box | null;
  chipEditable: boolean;
  caret: Box | null;
  glyph: Box | null;
  name: (Box & { scrollW: number; clientW: number }) | null;
};

async function mountAt(mount: any, page: any, width: number, longManual: boolean) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<LoreRowHeadStory longManual={longManual} />);
  const toggles = cmp.locator('[data-testid="lore-section-toggle"]');
  await expect(toggles.first()).toBeVisible();
  for (let i = 0; i < (await toggles.count()); i++) await toggles.nth(i).click();
  await expect(cmp.locator('[data-testid="lore-row"]').first()).toBeVisible();
  await expect(cmp.locator('[data-entry-id="L-4"]')).toBeVisible();
  return cmp;
}

async function measure(page: any): Promise<{
  pageOver: number;
  loreOver: number;
  rows: RowGeom[];
  sections: { key: string; head: Box; children: { cls: string; box: Box }[] }[];
}> {
  return page.evaluate(() => {
    const r = (el: Element | null) => {
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const se = document.scrollingElement!;
    const lore = document.querySelector(".lore") as HTMLElement | null;
    const rows = [...document.querySelectorAll('[data-testid="lore-row"]')].map(
      (row) => {
        const badges = row.querySelector(".lore-row__badges");
        const chip = row.querySelector('[data-testid="lore-scope-name"]');
        const name = row.querySelector(".lore-row__scope-name") as HTMLElement | null;
        return {
          id: row.getAttribute("data-entry-id") ?? "",
          head: r(row.querySelector(".lore-row__head")),
          row: r(row),
          select: r(row.querySelector('[data-testid="lore-select"]')),
          actions: r(row.querySelector(".lore-row__actions")),
          actionButtons: [...row.querySelectorAll(".lore-row__actions > :not(.lore-row__more), .lore-row__more > button")].map(
            (b) => ({ text: (b.textContent ?? "").trim() || b.getAttribute("aria-label") || "", box: r(b)! })
          ),
          badges: badges
            ? [...badges.children].map((c) => ({
                cls: (c.className || c.tagName).toString(),
                box: r(c)!,
              }))
            : [],
          mark: r(row.querySelector(".lore-row__expand-mark")),
          chip: r(chip),
          chipEditable: !!chip?.classList.contains("lore-row__scope-chip--editable"),
          caret: r(row.querySelector(".lore-row__scope-caret")),
          glyph: r(row.querySelector(".lore-row__scope-glyph")),
          name: name
            ? { ...r(name)!, scrollW: name.scrollWidth, clientW: name.clientWidth }
            : null,
        };
      }
    );
    const sections = [...document.querySelectorAll('[data-testid="lore-section"]')].map((sec) => {
      const head = sec.querySelector('[data-testid="lore-section-toggle"]')!;
      return {
        key: sec.getAttribute("data-section") ?? "",
        head: r(head)!,
        children: [...head.querySelectorAll(":scope > *, .lore-section__usage > *")].map((c) => ({
          cls: (c.className || c.tagName).toString(),
          box: r(c)!,
        })),
      };
    });
    return {
      pageOver: se.scrollWidth - se.clientWidth,
      loreOver: lore ? lore.scrollWidth - lore.clientWidth : -2,
      rows,
      sections,
    };
  });
}

const inside = (inner: Box, outer: Box) =>
  inner.left >= outer.left - 1 &&
  inner.right <= outer.right + 1 &&
  inner.top >= outer.top - 1 &&
  inner.bottom <= outer.bottom + 1;

function assertHeadContained(width: number, row: RowGeom) {
  const tag = `[${width}px] ${row.id}`;
  expect.soft(row.head, `${tag} .lore-row__head missing`).not.toBeNull();
  // type, id, status, scope — all four, inside the wrapper.
  expect.soft(row.badges.length, `${tag} .lore-row__badges children`).toBe(4);
  expect.soft(row.chip, `${tag} scope chip missing`).not.toBeNull();
  expect.soft(row.name, `${tag} scope name missing`).not.toBeNull();
  if (!row.head) return;
  for (const b of row.badges) {
    expect
      .soft(b.box.right, `${tag} "${b.cls}" right edge past the head`)
      .toBeLessThanOrEqual(row.head.right + 1);
    expect
      .soft(b.box.left, `${tag} "${b.cls}" left edge before the head`)
      .toBeGreaterThanOrEqual(row.head.left - 1);
  }
  if (row.chip) {
    expect
      .soft(row.chip.right, `${tag} scope chip right edge past the head`)
      .toBeLessThanOrEqual(row.head.right + 1);
    if (row.name)
      expect.soft(inside(row.name, row.chip), `${tag} scope name outside the chip`).toBe(true);
    if (row.glyph)
      expect.soft(inside(row.glyph, row.chip), `${tag} scope glyph outside the chip`).toBe(true);
    if (row.chipEditable) {
      expect.soft(row.caret, `${tag} editable chip has no caret`).not.toBeNull();
      if (row.caret)
        expect.soft(inside(row.caret, row.chip), `${tag} caret outside the chip`).toBe(true);
    }
  }
  expect.soft(row.mark, `${tag} expand mark missing`).not.toBeNull();
  if (row.mark) {
    expect.soft(inside(row.mark, row.head), `${tag} expand mark outside the head`).toBe(true);
  }
  expect.soft(row.select, `${tag} checkbox missing`).not.toBeNull();
  if (row.select) expect.soft(inside(row.select, row.head), `${tag} checkbox outside the head`).toBe(true);
  expect.soft(row.actions, `${tag} actions missing`).not.toBeNull();
  // A phone shows only 「⋯」; a desktop shows the size, 提到最新 and 編輯.
  expect.soft(row.actionButtons.length, `${tag} actions`).toBe(width <= 720 ? 1 : 3);
  for (const b of row.actionButtons) {
    expect.soft(inside(b.box, row.head), `${tag} "${b.text}" outside the head`).toBe(true);
    if (row.row) expect.soft(inside(b.box, row.row), `${tag} "${b.text}" outside the card`).toBe(true);
    for (const badge of row.badges) {
      const overlapX = Math.min(b.box.right, badge.box.right) - Math.max(b.box.left, badge.box.left);
      const overlapY = Math.min(b.box.bottom, badge.box.bottom) - Math.max(b.box.top, badge.box.top);
      expect
        .soft(overlapX > 1 && overlapY > 1, `${tag} "${b.text}" overlaps "${badge.cls}"`)
        .toBe(false);
    }
  }
}

function assertSectionHeadsContained(width: number, sections: { key: string; head: Box; children: { cls: string; box: Box }[] }[]) {
  expect.soft(sections.length, `[${width}px] sections rendered`).toBeGreaterThanOrEqual(4);
  for (const s of sections) {
    for (const c of s.children) {
      expect
        .soft(inside(c.box, s.head), `[${width}px] section ${s.key} "${c.cls}" outside its header`)
        .toBe(true);
    }
  }
}

function assertNoHScroll(width: number, g: { pageOver: number; loreOver: number }) {
  expect.soft(g.pageOver, `[${width}px] page horizontal scroll`).toBeLessThanOrEqual(1);
  expect.soft(g.loreOver, `[${width}px] .lore missing`).not.toBe(-2);
  expect.soft(g.loreOver, `[${width}px] .lore horizontal scroll`).toBeLessThanOrEqual(1);
}

for (const width of [390, 1440]) {
  test(`normal-length scope names fit whole @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false);
    const g = await measure(page);
    assertNoHScroll(width, g);
    assertSectionHeadsContained(width, g.sections);
    expect(g.rows.length, `[${width}px] rows rendered`).toBeGreaterThanOrEqual(5);

    const l4 = g.rows.find((r) => r.id === "L-4");
    // The owner's case: pinned, ⚙ 任務 scope, editable chip with a caret.
    expect(l4?.chipEditable, `[${width}px] L-4 chip should be the editable one`).toBe(true);

    for (const row of g.rows) {
      assertHeadContained(width, row);
      const tag = `[${width}px] ${row.id}`;
      if (row.name)
        expect
          .soft(row.name.scrollW, `${tag} normal-length scope name truncated`)
          .toBeLessThanOrEqual(row.name.clientW + 1);
      if (width === 1440) {
        const tops = row.badges.map((b) => (b.box.top + b.box.bottom) / 2);
        expect
          .soft(Math.max(...tops) - Math.min(...tops), `${tag} badges not on one line`)
          .toBeLessThanOrEqual(4);
      } else if (row.mark && row.head) {
        expect
          .soft(row.mark.right, `${tag} expand mark not at the head's right edge`)
          .toBeGreaterThanOrEqual(row.head.right - 2);
      }
    }
  });

  test(`a name longer than a line ellipsizes inside the head @${width}px`, async ({
    mount,
    page,
  }) => {
    await mountAt(mount, page, width, true);
    const g = await measure(page);
    assertNoHScroll(width, g);
    const l4 = g.rows.find((r) => r.id === "L-4");
    expect(l4, `[${width}px] L-4 row`).toBeTruthy();
    assertHeadContained(width, l4!);
    expect(l4!.name, `[${width}px] L-4 name`).not.toBeNull();
    expect
      .soft(l4!.name!.scrollW, `[${width}px] L-4 long name should ellipsize`)
      .toBeGreaterThan(l4!.name!.clientW + 1);
  });
}
