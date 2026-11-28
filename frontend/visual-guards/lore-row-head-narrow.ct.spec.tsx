// 傳承 directory cards, section pages and row heads at phone and desktop width:
// the directory's group headings read on one line with their chevron and
// 全部收合 leaves no sideways scroll (its touch box included), the directory's
// toolbar row holds the count left with 篩選 and 全部收合 right and the 條目編號
// field in the funnel's panel,
// the tags (id → type → state → scope) wrap as whole pills, the head's
// actions (編輯, the app-wide .doc-btn--edit, and the 收合 chevron) stay inside
// the head without overlapping them, no open card has a clamped body, the 撰寫人／生效期
// label/value grid stays inside the card, a folded row (讀不到 or folded by its
// 收合) holds one line, the message box's 📎, input and 送出 sit on one row
// inside the card with room to type, the section head's title, bar and legend stay
// inside it, the 適用範圍 menu opens inside the viewport, the inline editor fits,
// the unsaved-changes bar holds one line on the screen's bottom edge without
// covering the last card, a staged card keeps its height with its ↺ marks inside
// it, and a staged state change leaves the scroll position and the card where
// they were.
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
  folded: boolean;
  head: Box | null;
  row: Box | null;
  actions: Box | null;
  actionButtons: { text: string; cls: string; svg: boolean; height: number; box: Box }[];
  badges: { cls: string; box: Box }[];
  badgeOrder: string[];
  bodyOverflows: boolean | null;
  toggle: boolean;
  meta: Box | null;
  metaItems: Box[];
  composer: Box | null;
  composerItems: { id: string; box: Box }[];
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
    rows.push(...g.rows.filter((r) => !r.folded));
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
        return {
          id: row.getAttribute("data-entry-id") ?? "",
          folded: row.getAttribute("data-folded") === "true",
          head: r(row.querySelector(".lore-row__head")),
          row: r(row),
          actions: r(row.querySelector(".lore-row__actions")),
          actionButtons: [...row.querySelectorAll(".lore-row__actions > *")].map((b) => ({
            text: (b.textContent ?? "").trim() || b.getAttribute("title") || "",
            cls: b.className.toString(),
            svg: b.querySelector("svg") !== null,
            height: b.getBoundingClientRect().height,
            box: r(b)!,
          })),
          badges: badges
            ? [...badges.children].map((c) => ({
                cls: (c.className || c.tagName).toString(),
                box: r(c)!,
              }))
            : [],
          badgeOrder: badges
            ? [...badges.children].map((c) => {
                const id = c.getAttribute("data-testid") ?? c.querySelector("[data-testid]")?.getAttribute("data-testid") ?? "";
                // The scope tag is a bare span for a viewer who may not move
                // entries and a wrapped menu trigger for one who may.
                return id === "lore-scope" ? "lore-scope-name" : id;
              })
            : [],
          bodyOverflows: (() => {
            const body = row.querySelector('[data-testid="lore-body"]') as HTMLElement | null;
            return body ? body.scrollHeight > body.clientHeight + 1 : null;
          })(),
          toggle: row.getAttribute("role") === "button",
          meta: r(row.querySelector(".lore-row__meta")),
          metaItems: [...row.querySelectorAll(".lore-row__meta > *")].map((c) => r(c)!),
          composer: r(row.querySelector('[data-testid="lore-message-box"] .task-card__composer')),
          composerItems: [
            ...row.querySelectorAll('[data-testid="lore-message-box"] .task-card__composer > :not([hidden])'),
          ].map((c) => ({ id: c.getAttribute("data-testid") ?? c.tagName, box: r(c)! })),
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
            ":scope > *, .lore-dir-card__head > *, .lore-dir-card__badges > *, .lore-dir-card__usage > *, .lore-section__usage > *, .lore-section__legend > *, .lore-section__title > *",
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
  // id, type, state, scope — in that order, inside the wrapper.
  expect.soft(row.badges.length, `${tag} .lore-row__badges children`).toBe(4);
  expect
    .soft(row.badgeOrder, `${tag} badge order`)
    .toEqual(["lore-entry-id", "lore-type", "lore-state", "lore-scope-name"]);
  if (!row.head) return;
  for (const b of row.badges) {
    expect
      .soft(b.box.right, `${tag} "${b.cls}" right edge past the head`)
      .toBeLessThanOrEqual(row.head.right + 1);
    expect
      .soft(b.box.left, `${tag} "${b.cls}" left edge before the head`)
      .toBeGreaterThanOrEqual(row.head.left - 1);
  }
  expect.soft(row.meta, `${tag} meta grid missing`).not.toBeNull();
  // 撰寫人, its pill, 生效期, the time: two label/value rows.
  expect.soft(row.metaItems.length, `${tag} meta items`).toBe(4);
  if (row.row)
    for (const m of row.metaItems)
      expect.soft(inside(m, row.row), `${tag} meta item outside the card`).toBe(true);
  const mid = (b: Box) => (b.top + b.bottom) / 2;
  if (row.metaItems.length === 4) {
    const [authorLabel, author, effLabel, eff] = row.metaItems;
    expect.soft(Math.abs(mid(authorLabel) - mid(author)), `${tag} 撰寫人 beside its pill`).toBeLessThanOrEqual(4);
    expect.soft(Math.abs(mid(effLabel) - mid(eff)), `${tag} 生效期 beside its time`).toBeLessThanOrEqual(4);
    expect.soft(effLabel.top, `${tag} 生效期 on the row under 撰寫人`).toBeGreaterThanOrEqual(author.bottom - 1);
    expect.soft(Math.abs(author.left - eff.left), `${tag} values share one column`).toBeLessThanOrEqual(1);
  }
  expect.soft(row.composer, `${tag} message box missing`).not.toBeNull();
  expect
    .soft(row.composerItems.map((c) => c.id), `${tag} message box parts`)
    .toEqual(["lore-msg-attach", "lore-msg-input", "lore-msg-send"]);
  row.composerItems.forEach((c, i) => {
    if (row.row) expect.soft(inside(c.box, row.row), `${tag} "${c.id}" outside the card`).toBe(true);
    const next = row.composerItems[i + 1];
    if (next) {
      expect.soft(c.box.right, `${tag} "${c.id}" runs into "${next.id}"`).toBeLessThanOrEqual(next.box.left + 1);
      expect.soft(Math.abs(c.box.bottom - next.box.bottom), `${tag} "${c.id}" and "${next.id}" not on one row`).toBeLessThanOrEqual(4);
    }
  });
  const input = row.composerItems.find((c) => c.id === "lore-msg-input");
  if (input) expect.soft(input.box.right - input.box.left, `${tag} message input too narrow`).toBeGreaterThanOrEqual(150);
  const send = row.composerItems.find((c) => c.id === "lore-msg-send");
  if (send) expect.soft(send.box.bottom - send.box.top, `${tag} 送出 wraps`).toBeLessThanOrEqual(40);
  // Open: the body shows in full and the card itself is the fold toggle.
  expect.soft(row.bodyOverflows, `${tag} body missing or clipped`).toBe(false);
  expect.soft(row.toggle, `${tag} card is not the fold toggle`).toBe(true);
  expect.soft(row.actions, `${tag} actions missing`).not.toBeNull();
  // 編輯 at every width, the app's own edit button: pencil + label, and the
  // 30px .doc-btn box (proves settings.css styles it, not a bare <button>);
  // then the 收合 chevron, no taller than 編輯.
  expect
    .soft(row.actionButtons.map((b) => [b.text, b.cls, b.svg]), `${tag} actions`)
    .toEqual([
      ["編輯", "doc-btn doc-btn--edit", true],
      ["收合", "lore-row__fold lore-row__fold--open", true],
    ]);
  const editHeight = row.actionButtons[0]?.height ?? 0;
  expect.soft(Math.round(editHeight), `${tag} 編輯 height`).toBe(30);
  for (const b of row.actionButtons) {
    expect.soft(b.height, `${tag} "${b.text}" taller than 編輯`).toBeLessThanOrEqual(editHeight + 1);
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

for (const width of [320, 390, 1440]) {
  for (const [visit, dirCollapsed] of [
    ["a first visit", null],
    ["every group open", []],
  ] as const) {
    test(`on ${visit} the directory's group headings read on one line, and its toolbar row holds the count left with 篩選 and 全部收合 right, the 條目編號 field in ${width > 720 ? "the panel" : "the sheet"}, no sideways scroll @${width}px`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<LoreRowHeadStory dirCollapsed={dirCollapsed === null ? null : [...dirCollapsed]} />);
      await expect(cmp.locator('[data-testid="lore-collapse-all"]')).toBeVisible();
      await expect(cmp.locator('[data-testid="lore-directory-filter-summary"]')).toBeVisible();
      const g = await page.evaluate(() => {
        const box = (el: Element | null) => {
          if (!el) return null;
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const se = document.scrollingElement!;
        const lore = document.querySelector(".lore") as HTMLElement | null;
        return {
          pageOver: se.scrollWidth - se.clientWidth,
          loreOver: lore ? lore.scrollWidth - lore.clientWidth : -2,
          lore: box(lore),
          button: box(document.querySelector('[data-testid="lore-collapse-all"]')),
          funnel: box(document.querySelector('[data-testid="lore-directory-filter-toggle"]')),
          toolbar: box(document.querySelector('[data-testid="lore-directory-filter"]')),
          firstHeading: box(document.querySelector('[data-testid^="lore-group-heading-"]')),
          count: (() => {
            const n = document.querySelector('[data-testid="lore-directory-filter-summary"]') as HTMLElement | null;
            if (!n) return null;
            const range = document.createRange();
            range.selectNodeContents(n);
            const t = range.getBoundingClientRect();
            return { text: n.textContent ?? "", box: box(n)!, textMid: (t.top + t.bottom) / 2, cut: n.scrollWidth > n.clientWidth + 0.5 };
          })(),
          idOnPage: document.querySelector('[data-testid="lore-directory-id"]') !== null,
          headings: [...document.querySelectorAll('[data-testid^="lore-group-heading-"]')].map((h) => {
            const label = h.querySelector(".chat-groups__heading-label");
            const range = document.createRange();
            if (label) range.selectNodeContents(label);
            return {
              text: label?.textContent ?? "",
              lines: label ? new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size : 0,
              fontSize: label ? parseFloat(getComputedStyle(label).fontSize) : 0,
              heading: box(h)!,
              label: box(label),
              chevron: box(h.querySelector(".chat-groups__chevron")),
            };
          }),
        };
      });
      console.log(`[lore ${width}] ${visit} directory groups`, JSON.stringify(g));
      assertNoHScroll(width, g);
      expect(g.button, `[${width}px] 全部收合`).not.toBeNull();
      expect(g.lore, `[${width}px] .lore`).not.toBeNull();
      if (g.button && g.lore) {
        expect.soft(g.button.right, `[${width}px] 全部收合 inside .lore`).toBeLessThanOrEqual(g.lore.right + 1);
        expect.soft(g.button.right - g.button.left, `[${width}px] 全部收合 width`).toBeGreaterThanOrEqual(28);
      }
      expect(g.count, `[${width}px] directory count`).not.toBeNull();
      expect(g.funnel, `[${width}px] directory funnel`).not.toBeNull();
      if (g.count && g.funnel && g.button && g.lore && g.toolbar && g.firstHeading) {
        const mid = (b: Box) => (b.top + b.bottom) / 2;
        expect.soft(g.count.text, `[${width}px] count text`).toMatch(/^[\d,]+ 筆傳承$/);
        expect.soft(g.count.cut, `[${width}px] count whole`).toBe(false);
        expect.soft(Math.abs(g.count.box.left - g.toolbar.left), `[${width}px] count left edge`).toBeLessThanOrEqual(1);
        expect.soft(g.count.box.right, `[${width}px] count ends left of the funnel`).toBeLessThanOrEqual(g.funnel.left);
        expect.soft(g.funnel.right, `[${width}px] funnel before 全部收合`).toBeLessThanOrEqual(g.button.left);
        expect.soft(Math.abs(g.button.right - g.toolbar.right), `[${width}px] 全部收合 at the row's right end`).toBeLessThanOrEqual(1);
        expect.soft(Math.abs(mid(g.funnel) - mid(g.button)), `[${width}px] funnel and 全部收合 on one line`).toBeLessThanOrEqual(1);
        expect.soft(Math.abs(g.count.textMid - mid(g.funnel)), `[${width}px] count on the funnel's line`).toBeLessThanOrEqual(2);
        expect.soft(Math.abs(g.funnel.bottom - g.funnel.top - (g.button.bottom - g.button.top)), `[${width}px] funnel and 全部收合 same height`).toBeLessThanOrEqual(0.5);
        expect.soft(g.toolbar.bottom, `[${width}px] toolbar above the groups`).toBeLessThanOrEqual(g.firstHeading.top + 0.5);
      }
      expect.soft(g.idOnPage, `[${width}px] 條目編號 field on the page while the panel is closed`).toBe(false);
      expect(g.headings.map((h) => h.text), `[${width}px] group headings`).toEqual([
        expect.stringMatching(/^所有人 · \d+$/),
        expect.stringMatching(/^正職成員 · \d+$/),
        expect.stringMatching(/^外包成員 · \d+$/),
        expect.stringMatching(/^任務手冊 · \d+$/),
      ]);
      for (const h of g.headings) {
        const tag = `[${width}px] "${h.text}"`;
        expect.soft(h.lines, `${tag} wraps`).toBe(1);
        expect.soft(h.fontSize, `${tag} font size`).toBeGreaterThanOrEqual(13);
        expect.soft(h.label && h.label.right - h.label.left > 0, `${tag} label has width`).toBe(true);
        expect.soft(h.chevron, `${tag} chevron`).not.toBeNull();
        if (h.label && h.chevron && g.lore) {
          expect.soft(h.chevron.left, `${tag} chevron after the label`).toBeGreaterThanOrEqual(h.label.right - 1);
          expect.soft(h.chevron.right, `${tag} chevron inside .lore`).toBeLessThanOrEqual(g.lore.right + 1);
        }
        if (g.lore) expect.soft(h.heading.right, `${tag} heading inside .lore`).toBeLessThanOrEqual(g.lore.right + 1);
      }

      await page.getByTestId("lore-directory-filter-toggle").click();
      const layer = page.getByTestId(width > 720 ? "lore-directory-filter-pop" : "lore-directory-filter-sheet");
      await expect(layer).toBeVisible();
      const field = layer.locator('.filter-panel__field:has([data-testid="lore-directory-id"])');
      await expect(field.locator(".filter-panel__label")).toHaveText("條目編號");
      const lb = (await layer.boundingBox())!;
      expect.soft(lb.x, `[${width}px] filter layer left edge`).toBeGreaterThanOrEqual(0);
      expect.soft(lb.x + lb.width, `[${width}px] filter layer right edge`).toBeLessThanOrEqual(width + 0.5);
      const ib = (await page.getByTestId("lore-directory-id").boundingBox())!;
      expect.soft(ib.x + ib.width, `[${width}px] 條目編號 inside the layer`).toBeLessThanOrEqual(lb.x + lb.width + 0.5);
      if (width > 720 && g.funnel) {
        expect.soft(lb.y, `[${width}px] panel hangs under the funnel`).toBeGreaterThanOrEqual(g.funnel.bottom);
      }
      assertNoHScroll(width, await pageAndLoreOver(page));
    });
  }

}

for (const width of [390, 1440]) {
  test(`the directory lists every target inside its row @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, "");
    const g = await measure(page);
    assertNoHScroll(width, g);
    assertSectionHeadsContained(width, g.sections);
    expect(g.rows.length, `[${width}px] the directory shows no cards`).toBe(0);
  });

  test(`every section page keeps each card's badges, actions and meta line inside it @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, "");
    const g = await measureEverySection(cmp, page);
    assertNoHScroll(width, g);
    expect(g.keys.length, `[${width}px] sections visited`).toBeGreaterThanOrEqual(4);
    expect(g.rows.length, `[${width}px] rows rendered`).toBeGreaterThanOrEqual(5);

    for (const row of g.rows) {
      assertHeadContained(width, row);
      const tag = `[${width}px] ${row.id}`;
      if (width === 1440) {
        const tops = row.badges.map((b) => (b.box.top + b.box.bottom) / 2);
        expect
          .soft(Math.max(...tops) - Math.min(...tops), `${tag} badges not on one line`)
          .toBeLessThanOrEqual(4);
      }
    }
  });

  test(`a manual name longer than a line wraps inside the section head @${width}px`, async ({
    mount,
    page,
  }) => {
    await mountAt(mount, page, width, true, MANUAL);
    const g = await measure(page);
    assertSectionHeadsContained(width, g.sections, 1);
    assertNoHScroll(width, g);
    const head = g.sections.find((s) => s.key === "page");
    expect(head, `[${width}px] section head`).toBeTruthy();
    // name, badges, bar or usage, legend: nothing of the head is missing.
    expect.soft(head!.children.length, `[${width}px] head children`).toBeGreaterThanOrEqual(5);
    const l4 = g.rows.find((r) => r.id === "L-4");
    expect(l4, `[${width}px] L-4 row`).toBeTruthy();
    assertHeadContained(width, l4!);
  });

  test(`a member's section head sits on the page under the back link, not on a card, followed by the toolbar row (count left, 篩選 funnel right) above the list opening ${width > 720 ? "a panel under it" : "a bottom sheet"}, and its legend swatches match the bar @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, MIRA);
    await expect(page.locator('[data-testid="lore-bar-everyone"]')).toBeVisible();
    const look = await page.evaluate(() => {
      const head = document.querySelector('[data-testid="lore-section-head"]')!;
      const bg = (el: Element | null) => (el ? getComputedStyle(el).backgroundColor : null);
      const top = (sel: string) => document.querySelector(sel)?.getBoundingClientRect().top ?? NaN;
      const bottom = (sel: string) => document.querySelector(sel)?.getBoundingClientRect().bottom ?? NaN;
      return {
        head: bg(head),
        headBorder: getComputedStyle(head).borderTopWidth,
        everyoneBar: bg(document.querySelector('[data-testid="lore-bar-everyone"]')),
        ownBar: bg(document.querySelector('[data-testid="lore-bar-own"]')),
        swatches: [...head.querySelectorAll(".lore-dir-card__swatch")].map((el) => bg(el)),
        order: [
          [bottom('[data-testid="lore-back"]'), top(".lore-section__title")],
          [bottom(".lore-section__title"), top('[data-testid="lore-section-bar"]')],
          [bottom('[data-testid="lore-section-summary"]'), top('[data-testid="lore-filter"]')],
          [bottom('[data-testid="lore-filter"]'), top('[data-testid="lore-row"]')],
        ],
        toolbar: (() => {
          const bar = document.querySelector('[data-testid="lore-filter"]');
          const f = document.querySelector('[data-testid="lore-filter-toggle"]');
          const n = document.querySelector('[data-testid="lore-filter-summary"]');
          if (!bar || !f || !n) return null;
          const b = bar.getBoundingClientRect();
          const h = head.getBoundingClientRect();
          const r = f.getBoundingClientRect();
          const c = n.getBoundingClientRect();
          const range = document.createRange();
          range.selectNodeContents(n);
          const text = range.getBoundingClientRect();
          return {
            inHead: head.contains(f),
            rightGap: h.right - r.right,
            countLeftGap: c.left - h.left,
            countBeforeFunnel: r.left - c.right,
            midDelta: Math.abs((r.top + r.bottom) / 2 - (text.top + text.bottom) / 2),
            countCut: (n as HTMLElement).scrollWidth > (n as HTMLElement).clientWidth + 0.5,
            barWithinHead: b.left >= h.left - 0.5 && b.right <= h.right + 0.5,
          };
        })(),
        titleLines: (() => {
          const t = document.querySelector(".lore-section__title")!;
          return new Set([...t.children].map((c) => Math.round(c.getBoundingClientRect().top + c.getBoundingClientRect().height / 2))).size;
        })(),
      };
    });
    expect.soft(look.head, `[${width}px] head background`).toBe("rgba(0, 0, 0, 0)");
    expect.soft(look.headBorder, `[${width}px] head border`).toBe("0px");
    look.order.forEach(([above, below], i) =>
      expect.soft(above, `[${width}px] head order step ${i}`).toBeLessThanOrEqual(below + 0.5),
    );
    expect.soft(look.titleLines, `[${width}px] name and 讀不到 on one line`).toBe(1);
    expect(look.toolbar, `[${width}px] toolbar row with a count and the 篩選 funnel`).not.toBeNull();
    expect.soft(look.toolbar!.inHead, `[${width}px] funnel out of the section head`).toBe(false);
    expect.soft(look.toolbar!.barWithinHead, `[${width}px] toolbar as wide as the head`).toBe(true);
    expect.soft(Math.abs(look.toolbar!.rightGap), `[${width}px] funnel right edge vs head`).toBeLessThanOrEqual(1);
    expect.soft(Math.abs(look.toolbar!.countLeftGap), `[${width}px] count left edge vs head`).toBeLessThanOrEqual(1);
    expect.soft(look.toolbar!.countBeforeFunnel, `[${width}px] count ends left of the funnel`).toBeGreaterThanOrEqual(0);
    expect.soft(look.toolbar!.midDelta, `[${width}px] count centred on the funnel`).toBeLessThanOrEqual(2);
    expect.soft(look.toolbar!.countCut, `[${width}px] count whole`).toBe(false);

    await page.getByTestId("lore-filter-toggle").click();
    const layer = page.getByTestId(width > 720 ? "lore-filter-pop" : "lore-filter-sheet");
    await expect(layer).toBeVisible();
    await expect(page.getByTestId("lore-filter-id")).toBeVisible();
    const lb = (await layer.boundingBox())!;
    expect.soft(lb.x, `[${width}px] filter layer left edge`).toBeGreaterThanOrEqual(0);
    expect.soft(lb.x + lb.width, `[${width}px] filter layer right edge`).toBeLessThanOrEqual(width + 0.5);
    if (width > 720) {
      const fb = (await page.getByTestId("lore-filter-toggle").boundingBox())!;
      expect.soft(lb.y, `[${width}px] panel hangs under the funnel`).toBeGreaterThanOrEqual(fb.y + fb.height);
      expect.soft(Math.abs(lb.x + lb.width - (fb.x + fb.width)), `[${width}px] panel right edge on the funnel's`).toBeLessThanOrEqual(1);
    }
    expect.soft(look.swatches, `[${width}px] legend swatches`).toEqual([look.everyoneBar, look.ownBar]);
    expect.soft(look.everyoneBar, `[${width}px] two bar colours`).not.toBe(look.ownBar);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`the 適用範圍 menu opens inside the viewport @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA);
    const row = cmp.locator('[data-testid="lore-row"]:has(button[data-testid="lore-scope-name"])').first();
    await row.locator('.lore-row__badges button[data-testid="lore-scope-name"]').click();
    const pop = row.locator('[data-testid="lore-scope-options"]');
    await expect(pop).toBeVisible();
    const g = await page.evaluate(() => {
      const el = document.querySelector('[data-testid="lore-scope-options"]')!;
      const b = el.getBoundingClientRect();
      const card = el.closest('[data-testid="lore-row"]')!.getBoundingClientRect();
      return { left: b.left, right: b.right, cardLeft: card.left, cardRight: card.right, vw: window.innerWidth };
    });
    expect.soft(g.left, `[${width}px] menu left edge`).toBeGreaterThanOrEqual(0);
    expect.soft(g.right, `[${width}px] menu right edge`).toBeLessThanOrEqual(g.vw);
    expect.soft(g.left, `[${width}px] menu inside the card`).toBeGreaterThanOrEqual(g.cardLeft - 1);
    expect.soft(g.right, `[${width}px] menu inside the card`).toBeLessThanOrEqual(g.cardRight + 1);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });
}

for (const width of [390, 1440]) {
  test(`a folded 讀不到 row holds id, title, tag and chevron on one line inside the card, and opens into a full card @${width}px`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<LoreRowHeadStory roleCap={100} hash={MIRA} />);
    const folded = cmp.locator('[data-testid="lore-row"][data-folded="true"]');
    await expect(folded.first()).toBeVisible();
    const rows = await page.evaluate(() =>
      [...document.querySelectorAll('[data-testid="lore-row"][data-folded="true"]')].map((row) => {
        const r = (el: Element) => {
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const title = row.querySelector('[data-testid="lore-folded-title"]') as HTMLElement | null;
        return {
          id: row.getAttribute("data-entry-id") ?? "",
          row: r(row),
          parts: [...row.children].map((c) => ({ cls: c.className.toString(), box: r(c) })),
          titleClipped: title ? getComputedStyle(title).textOverflow : null,
        };
      }),
    );
    expect(rows.length, `[${width}px] folded rows`).toBeGreaterThanOrEqual(1);
    for (const f of rows) {
      const tag = `[${width}px] folded ${f.id}`;
      expect.soft(f.parts.length, `${tag} parts`).toBe(4);
      const mids = f.parts.map((p) => (p.box.top + p.box.bottom) / 2);
      expect.soft(Math.max(...mids) - Math.min(...mids), `${tag} not one line`).toBeLessThanOrEqual(4);
      for (const p of f.parts) expect.soft(inside(p.box, f.row), `${tag} "${p.cls}" outside the row`).toBe(true);
      expect.soft(f.titleClipped, `${tag} title ellipsis`).toBe("ellipsis");
      expect.soft(f.parts[3]?.box.right ?? 0, `${tag} chevron at the right edge`).toBeGreaterThan(f.row.right - 40);
    }
    assertNoHScroll(width, await pageAndLoreOver(page));

    const id = rows[0].id;
    await cmp.locator(`[data-testid="lore-row"][data-entry-id="${id}"]`).click();
    const opened = cmp.locator(`[data-testid="lore-row"][data-entry-id="${id}"]`);
    await expect(opened.locator('[data-testid="lore-fold-mark"]')).toBeVisible();
    const g = await measure(page);
    const row = g.rows.find((r) => r.id === id)!;
    expect.soft(row.folded, `[${width}px] ${id} still folded`).toBe(false);
    for (const b of row.actionButtons)
      expect.soft(inside(b.box, row.head!), `[${width}px] ${id} "${b.text}" outside the head`).toBe(true);
    expect.soft(row.metaItems.length, `[${width}px] ${id} meta items`).toBe(4);
    assertNoHScroll(width, g);
  });

  test(`a click on a read card's body folds it to id, title, type and chevron on one line inside the card, the cards below move up, and a click opens it again @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA);
    const open = cmp.locator('[data-testid="lore-row"]:not([data-folded]):not([data-dimmed="true"])');
    const card = open.first();
    const id = (await card.getAttribute("data-entry-id"))!;
    const row = cmp.locator(`[data-testid="lore-row"][data-entry-id="${id}"]`);
    const next = row.locator("xpath=following-sibling::*[1]");
    const before = await page.evaluate(
      ([rowSel]) => {
        const el = document.querySelector(rowSel)!;
        const b = el.getBoundingClientRect();
        const n = el.nextElementSibling!.getBoundingClientRect();
        return { height: b.height, nextTop: n.top };
      },
      [`[data-testid="lore-row"][data-entry-id="${id}"]`],
    );
    await row.locator('[data-testid="lore-body"]').click();
    await expect(row).toHaveAttribute("data-folded", "true");
    const f = await page.evaluate(
      ([rowSel]) => {
        const el = document.querySelector(rowSel)!;
        const r = (e: Element) => {
          const b = e.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const title = el.querySelector('[data-testid="lore-folded-title"]');
        return {
          row: r(el),
          nextTop: el.nextElementSibling!.getBoundingClientRect().top,
          parts: [...el.children].map((c) => ({ id: c.getAttribute("data-testid") ?? c.className.toString(), box: r(c) })),
          titleOverflow: title ? getComputedStyle(title).textOverflow : null,
          titleWhiteSpace: title ? getComputedStyle(title).whiteSpace : null,
        };
      },
      [`[data-testid="lore-row"][data-entry-id="${id}"]`],
    );
    const tag = `[${width}px] folded ${id}`;
    expect
      .soft(f.parts.map((p) => p.id), `${tag} parts`)
      .toEqual(["lore-entry-id", "lore-folded-title", "lore-folded-type", "lore-row__fold"]);
    const mids = f.parts.map((p) => (p.box.top + p.box.bottom) / 2);
    expect.soft(Math.max(...mids) - Math.min(...mids), `${tag} not one line`).toBeLessThanOrEqual(4);
    for (const p of f.parts) expect.soft(inside(p.box, f.row), `${tag} "${p.id}" outside the row`).toBe(true);
    f.parts.forEach((p, i) => {
      const n = f.parts[i + 1];
      if (n) expect.soft(p.box.right, `${tag} "${p.id}" runs into "${n.id}"`).toBeLessThanOrEqual(n.box.left + 1);
    });
    expect.soft([f.titleOverflow, f.titleWhiteSpace], `${tag} title clipping`).toEqual(["ellipsis", "nowrap"]);
    expect.soft(f.parts[3]?.box.right ?? 0, `${tag} chevron at the right edge`).toBeGreaterThan(f.row.right - 40);
    expect.soft(f.row.bottom - f.row.top, `${tag} not shorter than the open card`).toBeLessThan(before.height / 2);
    expect.soft(f.nextTop, `${tag} the card below did not move up`).toBeLessThan(before.nextTop - 1);
    assertNoHScroll(width, await pageAndLoreOver(page));
    await expect(next).toBeVisible();

    await row.click();
    await expect(row).not.toHaveAttribute("data-folded", "true");
    await expect(row).toBeFocused();
    const g = await measure(page);
    const reopened = g.rows.find((r) => r.id === id)!;
    assertHeadContained(width, reopened);
    assertNoHScroll(width, g);
  });

  test(`the member section head's char/cap row and per-source counts each hold one line inside the head @${width}px`, async ({ mount, page }) => {
    await mountAt(mount, page, width, false, MIRA);
    const g = await page.evaluate(() => {
      const head = document.querySelector('[data-testid="lore-section-head"]')!;
      const r = (el: Element | null) => {
        if (!el) return null;
        const b = el.getBoundingClientRect();
        return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
      };
      // Spread of the vertical centres of every box inside: one line keeps them within a few px.
      const spread = (el: Element | null) => {
        if (!el) return Infinity;
        const range = document.createRange();
        range.selectNodeContents(el);
        const mids = [...range.getClientRects()].filter((x) => x.width > 0).map((x) => (x.top + x.bottom) / 2);
        return mids.length === 0 ? Infinity : Math.max(...mids) - Math.min(...mids);
      };
      const usage = head.querySelector('[data-testid="lore-section-usage-row"]');
      const legend = head.querySelector('[data-testid="lore-section-summary"]');
      return {
        head: r(head)!,
        used: r(head.querySelector('[data-testid="lore-section-used"]')),
        cap: r(head.querySelector('[data-testid="lore-section-cap"]')),
        usage: r(usage),
        legend: r(legend),
        legendText: legend?.textContent ?? "",
        usageSpread: spread(usage),
        legendSpread: spread(legend),
        nameToCountGaps: [...head.querySelectorAll(".lore-section__legend-item")].map((item) => {
          const name = item.querySelector(".lore-section__legend-name")!.getBoundingClientRect();
          const texts = [...item.childNodes].filter((n) => n.nodeType === Node.TEXT_NODE && n.textContent!.trim() !== "");
          const range = document.createRange();
          range.selectNodeContents(texts[texts.length - 1]);
          return range.getBoundingClientRect().left - name.right;
        }),
      };
    });
    expect(g.used && g.cap && g.legend, `[${width}px] head rows`).toBeTruthy();
    expect.soft(g.legendText, `[${width}px] legend`).toMatch(/^所有人 \d+\/\d+ · Mira \d+\/\d+$/);
    expect(g.nameToCountGaps, `[${width}px] legend items`).toHaveLength(2);
    for (const gap of g.nameToCountGaps) expect.soft(gap, `[${width}px] a visible space between name and count`).toBeGreaterThanOrEqual(2);
    expect.soft(g.usageSpread, `[${width}px] chars and cap on one line`).toBeLessThanOrEqual(4);
    expect.soft(g.legendSpread, `[${width}px] per-source counts on one line`).toBeLessThanOrEqual(4);
    expect.soft(Math.abs(g.used!.left - g.head.left), `[${width}px] chars on the left`).toBeLessThanOrEqual(1);
    expect.soft(Math.abs(g.cap!.right - g.head.right), `[${width}px] cap on the right`).toBeLessThanOrEqual(1);
    for (const b of [g.usage!, g.legend!]) expect.soft(inside(b, g.head), `[${width}px] row outside the head`).toBe(true);
    assertNoHScroll(width, await pageAndLoreOver(page));
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

  test(`the unsaved-changes bar is one line on the screen's bottom edge, as wide as the app column, and stays there while .lore scrolls @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 600);
    const rows = cmp.locator('[data-testid="lore-row"]');
    for (const i of [0, 1]) await stageOtherType(rows.nth(i));
    const second = rows.nth(1);
    await second.locator('[data-testid="lore-state"]').click();
    await second.locator('[data-testid="lore-state-options"] .lore-row__menu-item:not(.lore-row__menu-item--active)').first().click();
    await expect(cmp.locator('[data-testid="lore-staged-count"]')).toHaveText("3 項變更還沒儲存");
    const read = () =>
      page.evaluate(() => {
        const r = (el: Element) => {
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const bar = document.querySelector('[data-testid="lore-staged-bar"]')!;
        return {
          bar: r(bar),
          main: r(document.querySelector(".app__main")!),
          parts: [...bar.querySelectorAll("button, .lore-savebar__count")].map((c) => ({
            text: (c.textContent ?? "").trim(),
            box: r(c),
          })),
          vw: document.documentElement.clientWidth,
          vh: window.innerHeight,
        };
      });
    const check = (g: Awaited<ReturnType<typeof read>>, when: string) => {
      const tag = `[${width}px ${when}]`;
      expect.soft(Math.abs(g.bar.bottom - g.vh), `${tag} bar not on the bottom edge`).toBeLessThanOrEqual(1);
      expect.soft(Math.abs(g.bar.left - g.main.left), `${tag} bar left vs app column`).toBeLessThanOrEqual(1);
      expect.soft(Math.abs(g.bar.right - g.main.right), `${tag} bar right vs app column`).toBeLessThanOrEqual(1);
      expect.soft(g.bar.left, `${tag} bar left`).toBeGreaterThanOrEqual(0);
      expect.soft(g.bar.right, `${tag} bar right`).toBeLessThanOrEqual(g.vw + 1);
      expect.soft(g.bar.bottom - g.bar.top, `${tag} bar taller than one line`).toBeLessThanOrEqual(50);
      expect.soft(g.parts.map((p) => p.text), `${tag} bar parts`).toEqual(["3 項變更還沒儲存", "全部捨棄", "儲存變更"]);
      const mids = g.parts.map((p) => (p.box.top + p.box.bottom) / 2);
      expect.soft(Math.max(...mids) - Math.min(...mids), `${tag} bar parts not on one line`).toBeLessThanOrEqual(3);
      for (const p of g.parts) expect.soft(inside(p.box, g.bar), `${tag} "${p.text}" outside the bar`).toBe(true);
    };
    check(await read(), "top");
    await page.evaluate(() => {
      const lore = document.querySelector(".lore") as HTMLElement;
      lore.scrollTop = lore.scrollHeight / 2;
    });
    check(await read(), "scrolled");
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`scrolled to the end, the last card ends above the unsaved-changes bar @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 600);
    await stageOtherType(cmp.locator('[data-testid="lore-row"]').first());
    await expect(cmp.locator('[data-testid="lore-staged-bar"]')).toBeVisible();
    await page.evaluate(() => {
      const lore = document.querySelector(".lore") as HTMLElement;
      lore.scrollTop = lore.scrollHeight;
    });
    await expect(cmp.locator('[data-testid="lore-more"]')).toHaveCount(0);
    await page.evaluate(() => {
      const lore = document.querySelector(".lore") as HTMLElement;
      lore.scrollTop = lore.scrollHeight;
    });
    const g = await page.evaluate(() => {
      const rows = document.querySelectorAll('[data-testid="lore-row"]');
      return {
        last: rows[rows.length - 1].getBoundingClientRect().bottom,
        barTop: document.querySelector('[data-testid="lore-staged-bar"]')!.getBoundingClientRect().top,
      };
    });
    expect(g.last, `[${width}px] last card under the bar`).toBeLessThanOrEqual(g.barTop);
  });

  test(`a staged state change leaves the scroll position and the card exactly where they were @${width}px`, async ({ mount, page }) => {
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
      head: await boxOf(page, '[data-testid="lore-section-head"]'),
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
      head: await boxOf(page, '[data-testid="lore-section-head"]'),
      sameNode: await page.evaluate((s: string) => document.querySelector(s) === (window as any).__loreRowBefore, sel),
    };
    console.log(`[lore ${width}] state change`, JSON.stringify({ target, next, before, after }));
    expect.soft(Math.abs(after.scrollTop - before.scrollTop), `[${width}px] scrollTop moved`).toBeLessThanOrEqual(1);
    expect.soft(Math.abs(after.head!.bottom - before.head!.bottom), `[${width}px] the section head changed size`).toBeLessThanOrEqual(0.5);
    expect.soft(Math.abs(after.box!.top - before.box!.top), `[${width}px] ${target} moved on screen`).toBeLessThanOrEqual(0.5);
    expect.soft(after.sameNode, `[${width}px] ${target}'s card was torn down and rebuilt`).toBe(true);
  });
}

async function stageOtherType(row: any) {
  await row.locator('[data-testid="lore-type"]').click();
  await row
    .locator('[data-testid="lore-type-options"] .lore-row__menu-item:not(.lore-row__menu-item--active)')
    .first()
    .click();
  await expect(row).toHaveAttribute("data-staged", "true");
}

async function stageOtherState(row: any) {
  await row.locator('[data-testid="lore-state"]').click();
  await row
    .locator('[data-testid="lore-state-options"] .lore-row__menu-item:not(.lore-row__menu-item--active)')
    .first()
    .click();
}

/** Every card's box, keyed by id. */
async function cardBoxes(page: any): Promise<Record<string, Box>> {
  return page.evaluate(() =>
    Object.fromEntries(
      [...document.querySelectorAll('[data-testid="lore-row"]')].map((row) => {
        const b = row.getBoundingClientRect();
        return [row.getAttribute("data-entry-id"), { left: b.left, right: b.right, top: b.top, bottom: b.bottom }];
      }),
    ),
  );
}

for (const width of [360, 390, 1440]) {
  test(`a staged card is as tall as before, the cards below stay put, and every ↺ sits inside its card @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 900);
    const row = cmp.locator('[data-testid="lore-row"]:has(button[data-testid="lore-scope-name"])').first();
    const id = await row.getAttribute("data-entry-id");
    const before = await cardBoxes(page);
    await stageOtherType(row);
    await stageOtherState(row);
    await row.locator('button[data-testid="lore-scope-name"]').click();
    await row
      .locator('[data-testid="lore-scope-options"] .lore-row__scope-option:not(.lore-row__menu-item--active)')
      .first()
      .click();
    await expect(row.locator('[data-testid^="lore-revert-"]')).toHaveCount(3);
    const after = await cardBoxes(page);
    for (const [k, b] of Object.entries(before)) {
      if (!after[k]) continue;
      expect.soft(Math.abs(after[k].bottom - after[k].top - (b.bottom - b.top)), `[${width}px] ${k} height changed (staged: ${id})`).toBeLessThanOrEqual(0.5);
      expect.soft(Math.abs(after[k].top - b.top), `[${width}px] ${k} moved (staged: ${id})`).toBeLessThanOrEqual(0.5);
    }
    await assertMarksInside(page, width);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`a card under a long manual name keeps its height when its type and state are staged @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, true, MANUAL, 900);
    const row = cmp.locator('[data-entry-id="L-4"]');
    const before = await cardBoxes(page);
    await stageOtherType(row);
    await stageOtherState(row);
    await expect(row.locator('[data-testid^="lore-revert-"]')).toHaveCount(2);
    const after = await cardBoxes(page);
    for (const [k, b] of Object.entries(before)) {
      if (!after[k]) continue;
      expect.soft(Math.abs(after[k].bottom - after[k].top - (b.bottom - b.top)), `[${width}px] ${k} height changed`).toBeLessThanOrEqual(0.5);
    }
    await assertMarksInside(page, width);
    assertNoHScroll(width, await pageAndLoreOver(page));
  });

  test(`a staged card shows keyboard focus as the focus ring, not the dashed frame @${width}px`, async ({ mount, page }) => {
    const cmp = await mountAt(mount, page, width, false, MIRA, 900);
    const row = cmp.locator('[data-testid="lore-row"]').first();
    await stageOtherType(row);
    const staged = await row.evaluate((el: HTMLElement) => getComputedStyle(el).outlineStyle);
    expect.soft(staged, `[${width}px] staged frame`).toBe("dashed");
    await page.keyboard.press("Shift");
    const focused = await row.evaluate((el: HTMLElement) => {
      el.focus();
      const probe = document.createElement("span");
      probe.style.color = "var(--color-accent)";
      el.appendChild(probe);
      const accent = getComputedStyle(probe).color;
      probe.remove();
      const cs = getComputedStyle(el);
      return { visible: el.matches(":focus-visible"), style: cs.outlineStyle, color: cs.outlineColor, accent };
    });
    expect(focused.visible, `[${width}px] focus-visible`).toBe(true);
    expect.soft(focused.style, `[${width}px] focus ring style`).toBe("solid");
    expect.soft(focused.color, `[${width}px] focus ring colour`).toBe(focused.accent);
  });
}

async function assertMarksInside(page: any, width: number) {
  const marks = await page.evaluate(() =>
    [...document.querySelectorAll('[data-testid^="lore-revert-"]')].map((m) => {
      const b = m.getBoundingClientRect();
      const c = m.closest('[data-testid="lore-row"]')!.getBoundingClientRect();
      return {
        id: m.getAttribute("data-testid"),
        mark: { left: b.left, right: b.right, top: b.top, bottom: b.bottom },
        card: { left: c.left, right: c.right, top: c.top, bottom: c.bottom },
        size: [Math.round(b.width), Math.round(b.height)],
      };
    }),
  );
  expect(marks.length, `[${width}px] ↺ marks`).toBeGreaterThan(0);
  for (const m of marks) {
    expect.soft(inside(m.mark, m.card), `[${width}px] ${m.id} outside its card`).toBe(true);
    expect.soft(m.size, `[${width}px] ${m.id} size`).toEqual([16, 16]);
  }
}
