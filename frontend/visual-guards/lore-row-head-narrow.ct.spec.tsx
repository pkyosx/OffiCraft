// 傳承 directory rows, section pages and row headers at phone and desktop
// width: the badges
// wrap as whole pills, the 屬於 chip never gets squeezed to its glyph with the
// caret spilling out, only a name longer than a whole line ellipsizes, and the
// checkbox and the actions (提到最新／編輯, or 「⋯」 on a phone) stay inside the
// head without overlapping the badges. The badges read id → type → state →
// scope, and the ▸/▾ mark is there exactly when the clamped body overflows. The section header's CJK labels stay on
// one line, the batch bar and the inline editor fit, and a state change leaves
// the scroll position and the card where they were.
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
  actions: Box | null;
  actionButtons: { text: string; box: Box }[];
  badges: { cls: string; box: Box }[];
  badgeOrder: string[];
  mark: Box | null;
  bodyOverflows: boolean | null;
  toggle: boolean;
  chip: Box | null;
  chipEditable: boolean;
  caret: Box | null;
  glyph: Box | null;
  name: (Box & { scrollW: number; clientW: number }) | null;
};

const MIRA = "#lore/agent/mira";
const MANUAL = "#lore/manual/tm-mock";

async function mountAt(
  mount: any,
  page: any,
  width: number,
  longManual: boolean,
  hash = MANUAL,
  height = 900,
) {
  await page.setViewportSize({ width, height });
  const cmp = await mount(<LoreRowHeadStory longManual={longManual} hash={hash} />);
  if (hash === "") {
    await expect(cmp.locator('[data-testid="lore-target"]').first()).toBeVisible();
  } else {
    await expect(cmp.locator('[data-testid="lore-row"]').first()).toBeVisible();
  }
  return cmp;
}

/** Visit every section page from the directory and measure its rows. */
async function measureEverySection(cmp: any, page: any) {
  const keys: string[] = await cmp
    .locator('[data-testid="lore-target"]')
    .evaluateAll((els: Element[]) => els.map((e) => e.getAttribute("data-section") ?? ""));
  const rows: RowGeom[] = [];
  let worstOver = { pageOver: 0, loreOver: 0 };
  for (const key of keys) {
    await cmp.locator(`[data-testid="lore-target"][data-section="${key}"]`).click();
    await expect(cmp.locator('[data-testid="lore-section-head"]')).toBeVisible();
    await expect(cmp.locator('[data-testid="lore-section-body"]')).not.toContainText("載入中");
    const g = await measure(page);
    rows.push(...g.rows);
    worstOver = {
      pageOver: Math.max(worstOver.pageOver, g.pageOver),
      loreOver: g.loreOver === -2 ? -2 : Math.max(worstOver.loreOver, g.loreOver),
    };
    await cmp.locator('[data-testid="lore-back"]').click();
    await expect(cmp.locator('[data-testid="lore-target"]').first()).toBeVisible();
  }
  return { rows, ...worstOver, keys };
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
          badgeOrder: badges
            ? [...badges.children].map(
                (c) => c.getAttribute("data-testid") ?? c.querySelector("[data-testid]")?.getAttribute("data-testid") ?? "",
              )
            : [],
          mark: r(row.querySelector(".lore-row__expand-mark")),
          bodyOverflows: (() => {
            const body = row.querySelector('[data-testid="lore-body"]') as HTMLElement | null;
            return body ? body.scrollHeight > body.clientHeight + 1 : null;
          })(),
          toggle: row.getAttribute("role") === "button",
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
    const sections = [
      ...document.querySelectorAll('[data-testid="lore-target"], [data-testid="lore-section-head"]'),
    ].map((head) => {
      return {
        key: head.getAttribute("data-section") ?? "page",
        head: r(head)!,
        children: [
          ...head.querySelectorAll(
            ":scope > *, .lore-section__usage > *, .lore-dir-card__head > *, .lore-dir-card__badges > *, .lore-dir-card__usage > *",
          ),
        ].map((c) => ({
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
  // id, type, status, scope — all four, in that order, inside the wrapper.
  expect.soft(row.badges.length, `${tag} .lore-row__badges children`).toBe(4);
  expect
    .soft(row.badgeOrder, `${tag} badge order`)
    .toEqual(["lore-entry-id", "lore-type", "lore-state", "lore-scope"]);
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
  expect.soft(row.bodyOverflows, `${tag} body missing`).not.toBeNull();
  expect
    .soft(row.mark !== null, `${tag} expand mark shown iff the clamped body overflows`)
    .toBe(row.bodyOverflows === true);
  expect.soft(row.toggle, `${tag} row is a toggle iff the clamped body overflows`).toBe(row.bodyOverflows === true);
  if (row.mark) {
    expect.soft(inside(row.mark, row.head), `${tag} expand mark outside the head`).toBe(true);
  }
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

function assertSectionHeadsContained(
  width: number,
  sections: { key: string; head: Box; children: { cls: string; box: Box }[] }[],
  min = 4,
) {
  expect.soft(sections.length, `[${width}px] section heads rendered`).toBeGreaterThanOrEqual(min);
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
  test(`the directory lists every target inside its row @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, "");
    const g = await measure(page);
    assertNoHScroll(width, g);
    assertSectionHeadsContained(width, g.sections);
    expect(g.rows.length, `[${width}px] the directory shows no cards`).toBe(0);
  });

  test(`normal-length scope names fit whole on every section page @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, "");
    const g = await measureEverySection(cmp, page);
    assertNoHScroll(width, g);
    expect(g.keys.length, `[${width}px] sections visited`).toBeGreaterThanOrEqual(4);
    expect(g.rows.length, `[${width}px] rows rendered`).toBeGreaterThanOrEqual(5);
    // Both kinds of row must be on screen, or the iff check above proves nothing.
    expect.soft(g.rows.some((r) => r.bodyOverflows === true), `[${width}px] an overflowing body`).toBe(true);
    expect.soft(g.rows.some((r) => r.bodyOverflows === false), `[${width}px] a one-line body`).toBe(true);

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
    await mountAt(mount, page, width, true, MANUAL);
    const g = await measure(page);
    assertSectionHeadsContained(width, g.sections, 1);
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

async function pageAndLoreOver(page: any): Promise<{ pageOver: number; loreOver: number }> {
  return page.evaluate(() => {
    const se = document.scrollingElement!;
    const lore = document.querySelector(".lore") as HTMLElement | null;
    return {
      pageOver: se.scrollWidth - se.clientWidth,
      loreOver: lore ? lore.scrollWidth - lore.clientWidth : -2,
    };
  });
}

async function boxOf(page: any, selector: string): Promise<Box | null> {
  return page.evaluate((sel: string) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const b = el.getBoundingClientRect();
    return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
  }, selector);
}

for (const width of [390, 1440]) {
  test(`.lore is the height-bound scroll container under .app > .app__main @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, MIRA, 500);
    const g = await page.evaluate(() => {
      const lore = document.querySelector(".lore") as HTMLElement;
      return {
        chain: [lore.parentElement?.className, lore.parentElement?.parentElement?.className],
        overflowY: getComputedStyle(lore).overflowY,
        clientH: lore.clientHeight,
        scrollH: lore.scrollHeight,
        bottom: lore.getBoundingClientRect().bottom,
        vh: window.innerHeight,
        docOverY: document.scrollingElement!.scrollHeight - document.scrollingElement!.clientHeight,
      };
    });
    console.log(`[lore ${width}] scroll container`, JSON.stringify(g));
    expect(g.chain).toEqual(["app__main", "app"]);
    expect(g.overflowY).toBe("auto");
    expect.soft(g.scrollH, "Mira's section should be taller than the scrollport").toBeGreaterThan(g.clientH);
    expect.soft(g.bottom).toBeLessThanOrEqual(g.vh + 1);
    expect.soft(g.docOverY, "the document itself scrolls instead of .lore").toBeLessThanOrEqual(1);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`directory labels stay on one line @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, "");
    const labels = await page.evaluate(() =>
      [...document.querySelectorAll('[data-testid="lore-target"]')].flatMap((head) =>
        [...head.querySelectorAll(
          ".lore-dir-card__name, .lore-badge, .lore-dir-card__usage-text",
        )].map((el) => {
          const range = document.createRange();
          range.selectNodeContents(el);
          const tops = new Set([...range.getClientRects()].map((r) => Math.round(r.top)));
          const b = el.getBoundingClientRect();
          return { cls: el.className, text: el.textContent ?? "", lines: tops.size, width: b.width, height: b.height };
        }),
      ),
    );
    console.log(`[lore ${width}] section labels`, JSON.stringify(labels));
    expect(labels.length, `[${width}px] section labels`).toBeGreaterThanOrEqual(16);
    for (const l of labels) {
      expect.soft(l.lines, `[${width}px] "${l.text}" (${l.cls}) wraps`).toBe(1);
    }
  });

  test(`each directory card keeps its chevron top-right and its bar beside the numbers @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, "");
    const cards = await page.evaluate(() =>
      [...document.querySelectorAll('[data-testid="lore-target"]')].map((card) => {
        const box = (sel: string) => {
          const el = card.querySelector(sel);
          if (!el) return null;
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const c = card.getBoundingClientRect();
        return {
          key: card.getAttribute("data-section") ?? "",
          card: { left: c.left, right: c.right, top: c.top, bottom: c.bottom },
          name: box(".lore-dir-card__name"),
          // The name's FIRST line: a long manual name wraps, and the chevron
          // belongs on its first line, not beside the middle of the block.
          nameLine: (() => {
            const el = card.querySelector(".lore-dir-card__name");
            if (!el) return null;
            const range = document.createRange();
            range.selectNodeContents(el);
            const f = range.getClientRects()[0];
            return f ? { top: f.top, bottom: f.bottom } : null;
          })(),
          mark: box(".lore-dir-card__mark"),
          bar: box(".lore-dir-card__bar"),
          text: box(".lore-dir-card__usage-text"),
        };
      }),
    );
    expect(cards.length, `[${width}px] directory cards`).toBeGreaterThanOrEqual(4);
    // One card per row: every card spans the same width.
    const widths = new Set(cards.map((c) => Math.round(c.card.right - c.card.left)));
    expect.soft(widths.size, `[${width}px] cards share one width`).toBe(1);
    let bars = 0;
    for (const c of cards) {
      expect.soft(c.mark, `[${width}px] ${c.key} chevron`).not.toBeNull();
      if (c.mark && c.name) {
        expect.soft(c.card.right - c.mark.right, `[${width}px] ${c.key} chevron at the right edge`).toBeLessThan(24);
      }
      if (c.mark && c.nameLine) {
        const mid = (b: { top: number; bottom: number }) => (b.top + b.bottom) / 2;
        expect.soft(Math.abs(mid(c.mark) - mid(c.nameLine)), `[${width}px] ${c.key} chevron centred on the name's first line`).toBeLessThanOrEqual(3);
      }
      if (c.bar && c.text) {
        bars++;
        expect.soft(c.bar.right, `[${width}px] ${c.key} bar ends before the numbers`).toBeLessThanOrEqual(c.text.left);
        expect.soft(Math.abs((c.bar.top + c.bar.bottom) / 2 - (c.text.top + c.text.bottom) / 2), `[${width}px] ${c.key} bar beside the numbers`).toBeLessThan(6);
        expect.soft(c.bar.right - c.bar.left, `[${width}px] ${c.key} bar has room`).toBeGreaterThan(80);
      }
    }
    expect(bars, `[${width}px] cards with a bar`).toBeGreaterThanOrEqual(3);
  });

  test(`讀不到 is in the danger colour on the directory @${width}px`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<LoreRowHeadStory roleCap={100} />);
    await expect(cmp.locator('[data-testid="lore-section-dropped"]').first()).toBeVisible();
    const colours = await page.evaluate(() => {
      const dropped = document.querySelector('[data-testid="lore-target"] [data-testid="lore-section-dropped"]');
      // Probe inside the card: the theme's tokens may be scoped below <body>.
      const probe = document.createElement("span");
      probe.style.color = "var(--color-danger)";
      (dropped?.parentElement ?? document.body).appendChild(probe);
      const danger = getComputedStyle(probe).color;
      probe.remove();
      return { danger, dropped: dropped ? getComputedStyle(dropped).color : null };
    });
    expect(colours.dropped, `[${width}px] 讀不到 badge is in the danger colour`).toBe(colours.danger);
  });

  test(`the inline editor fits its card @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false);
    const row = cmp.locator('[data-entry-id="L-4"]');
    if (width <= 720) await row.locator('[data-testid="lore-more-actions"]').click();
    await row.locator('[data-testid="lore-edit"]').click();
    await expect(row.locator('[data-testid="lore-editor"]')).toBeVisible();
    const g = await page.evaluate(() => {
      const r = (el: Element) => {
        const b = el.getBoundingClientRect();
        return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
      };
      const row = document.querySelector('[data-entry-id="L-4"]')!;
      const editor = row.querySelector('[data-testid="lore-editor"]')!;
      return {
        row: r(row),
        editor: r(editor),
        parts: [...editor.querySelectorAll("input, textarea, button, .lore-row__editor-label")].map((c) => ({
          text: (c.textContent ?? "").trim().slice(0, 12) || c.getAttribute("data-testid") || c.tagName,
          box: r(c),
        })),
        vw: document.documentElement.clientWidth,
      };
    });
    console.log(`[lore ${width}] editor`, JSON.stringify({ row: g.row, editor: g.editor, vw: g.vw }));
    expect.soft(inside(g.editor, g.row), `[${width}px] editor outside its card`).toBe(true);
    expect.soft(g.editor.right, `[${width}px] editor past the viewport`).toBeLessThanOrEqual(g.vw + 1);
    expect(g.parts.length, `[${width}px] editor parts`).toBeGreaterThanOrEqual(6);
    for (const p of g.parts) {
      expect.soft(inside(p.box, g.editor), `[${width}px] "${p.text}" outside the editor`).toBe(true);
    }
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`the type menu opens inside the card @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false);
    const row = cmp.locator('[data-entry-id="L-4"]');
    await row.locator('[data-testid="lore-type"]').click();
    await expect(row.locator('[data-testid="lore-type-options"]')).toBeVisible();
    const pop = await boxOf(page, '[data-entry-id="L-4"] [data-testid="lore-type-options"]');
    const card = await boxOf(page, '[data-entry-id="L-4"]');
    expect(pop, `[${width}px] type menu`).not.toBeNull();
    expect.soft(pop!.left, `[${width}px] type menu left`).toBeGreaterThanOrEqual(card!.left - 1);
    expect.soft(pop!.right, `[${width}px] type menu right`).toBeLessThanOrEqual(card!.right + 1);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`提到最新 moves the card to the front without moving what is in view, and 前往 brings it back @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 500);
    const ids: string[] = await cmp
      .locator('[data-testid="lore-row"]')
      .evaluateAll((els: Element[]) => els.map((e) => e.getAttribute("data-entry-id") ?? ""));
    expect(ids.length, `[${width}px] Mira's cards`).toBeGreaterThanOrEqual(8);
    // The reader looks at `anchor` at the top of the view and bumps the card
    // right under it, so the card lands above what they are reading.
    const mid = Math.floor(ids.length / 2);
    const anchorId = ids[mid - 1]!;
    const target = ids[mid]!;
    const sel = `[data-testid="lore-row"][data-entry-id="${target}"]`;
    const anchorSel = `[data-testid="lore-row"][data-entry-id="${anchorId}"]`;
    await page.evaluate((s: string) => {
      const lore = document.querySelector(".lore") as HTMLElement;
      const row = document.querySelector(s) as HTMLElement;
      lore.scrollTop += row.getBoundingClientRect().top - lore.getBoundingClientRect().top - 10;
    }, anchorSel);
    const anchorBefore = await page.evaluate((s: string) => {
      const lore = document.querySelector(".lore") as HTMLElement;
      const row = document.querySelector(s) as HTMLElement;
      return { id: row.dataset.entryId!, top: row.getBoundingClientRect().top, scrollTop: lore.scrollTop };
    }, anchorSel);
    await expect(cmp.locator(sel)).toBeInViewport();
    expect(anchorBefore.scrollTop, `[${width}px] .lore did not scroll`).toBeGreaterThan(0);

    const row = cmp.locator(sel);
    if (width <= 720) {
      await row.locator('[data-testid="lore-more-actions"]').click();
    }
    await row.locator('[data-testid="lore-bump"]').click();
    const toast = cmp.locator('[data-testid="lore-toast"]');
    await expect(toast).toBeVisible();
    await expect
      .poll(async () => (await cmp.locator('[data-testid="lore-row"]').evaluateAll((els: Element[]) => els.map((e) => e.getAttribute("data-entry-id")))).indexOf(target))
      .toBeLessThan(ids.indexOf(target));

    const anchorAfter = await boxOf(page, `[data-testid="lore-row"][data-entry-id="${anchorBefore.id}"]`);
    console.log(`[lore ${width}] bump`, JSON.stringify({ target, anchorBefore, anchorAfter }));
    expect.soft(Math.abs(anchorAfter!.top - anchorBefore.top), `[${width}px] ${anchorBefore.id} moved on screen`).toBeLessThanOrEqual(1);
    const t = await boxOf(page, '[data-testid="lore-toast"]');
    const vw = await page.evaluate(() => document.documentElement.clientWidth);
    expect.soft(t!.bottom, `[${width}px] toast below the viewport`).toBeLessThanOrEqual(500 + 1);
    expect.soft(t!.right, `[${width}px] toast past the viewport`).toBeLessThanOrEqual(vw + 1);
    assertNoHScroll(width, await pageAndLoreOver(page));

    await cmp.locator('[data-testid="lore-toast-go"]').click();
    const moved = await page.evaluate((s: string) => {
      const lore = document.querySelector(".lore") as HTMLElement;
      const row = document.querySelector(s) as HTMLElement;
      const a = lore.getBoundingClientRect();
      const b = row.getBoundingClientRect();
      return { inView: b.top >= a.top - 1 && b.top < a.bottom, focused: document.activeElement === row };
    }, sel);
    expect.soft(moved.inView, `[${width}px] 前往 did not scroll to ${target}`).toBe(true);
    expect.soft(moved.focused, `[${width}px] 前往 did not focus ${target}`).toBe(true);
  });

  test(`a state change leaves the scroll position and the card where they were @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 500);
    const ids = await cmp.locator('[data-testid="lore-row"]').evaluateAll((els: Element[]) =>
      els.map((e) => e.getAttribute("data-entry-id")),
    );
    const target = ids[Math.floor(ids.length / 2)]!;
    const sel = `[data-testid="lore-row"][data-entry-id="${target}"]`;
    await page.evaluate((s: string) => {
      const lore = document.querySelector(".lore") as HTMLElement;
      const row = document.querySelector(s) as HTMLElement;
      lore.scrollTop += row.getBoundingClientRect().top - lore.getBoundingClientRect().top - 120;
    }, sel);
    const before = {
      scrollTop: await page.evaluate(() => (document.querySelector(".lore") as HTMLElement).scrollTop),
      box: await boxOf(page, sel),
    };
    await page.evaluate((s: string) => {
      (window as any).__loreRowBefore = document.querySelector(s);
    }, sel);
    expect(before.scrollTop, `[${width}px] .lore did not scroll`).toBeGreaterThan(0);

    const row = cmp.locator(sel);
    const next = (await row.locator('[data-testid="lore-state"]').textContent())?.trim() === "置頂" ? "active" : "pinned";
    await row.locator('[data-testid="lore-state"]').click();
    await row.locator(`[data-testid="lore-state-${next}"]`).click();
    await expect(row.locator('[data-testid="lore-state"]')).toHaveText(next === "pinned" ? "置頂" : "生效中");

    const after = {
      scrollTop: await page.evaluate(() => (document.querySelector(".lore") as HTMLElement).scrollTop),
      box: await boxOf(page, sel),
      sameNode: await page.evaluate((s: string) => document.querySelector(s) === (window as any).__loreRowBefore, sel),
    };
    console.log(`[lore ${width}] state change`, JSON.stringify({ target, next, before, after }));
    expect.soft(Math.abs(after.scrollTop - before.scrollTop), `[${width}px] scrollTop moved`).toBeLessThanOrEqual(1);
    expect.soft(Math.abs(after.box!.top - before.box!.top), `[${width}px] ${target} moved on screen`).toBeLessThanOrEqual(1);
    expect.soft(after.sameNode, `[${width}px] ${target}'s card was torn down and rebuilt`).toBe(true);
  });
}
