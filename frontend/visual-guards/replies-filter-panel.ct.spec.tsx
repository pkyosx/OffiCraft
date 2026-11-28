// The list pages' 篩選, measured in a real browser on the real page.
//
// jsdom lays nothing out and computes no colour, so the vitest suite can say
// whether a field is in the DOM but not whether it fits a phone or can be seen.
// The panel here comes from the shipped `RepliesPage` / `App`
// (stories/RepliesFilterPanelStory.tsx); a story that assembled the row out of
// its own markup once kept this guard green for a layout no page rendered.
//
// The shape measured: on 任務頁, 請示頁 and a 傳承 section page the row holds a
// funnel; the fields open in a panel anchored under it (desktop) or a bottom
// sheet (≤720px).
//
// What is asserted is geometry and computed colour:
//   (1) the panel / sheet and every field in it stay inside the viewport with
//       no horizontal overflow, at desktop and phone widths, and a dropdown
//       opened inside floats above the layer instead of being clipped by it;
//       the desktop panel hangs under the funnel, right edges aligned, with
//       nothing dimming the page, and is dismissed like 顯示方式;
//   (2) the folded row is page content — the list starts below it;
//   (3) the ID field is distinguishable from the box behind it under every
//       built-in theme (a border, a fill or both; deliberately not a ≥3:1
//       threshold — that border ratio is inherited from `.tasks__filter`);
//   (4) emptying the 編號 box is an escape from an applied filter, and 清除篩選
//       shows only while something is filtered;
//   (5) the shell adds no spacing of its own below the row;
//   (6) on 請示頁 the three list labels hold one line inside the column at
//       320, 375, 390, 393 and 1440 in both languages, with the funnel and
//       顯示方式 on the same line beside them on a desktop and on a phone
//       wherever they fit (zh from 375, en from 390; below them otherwise),
//       近期已回覆's 「近 24 小時」 beside them on a desktop and on its own line
//       under the row on a phone, and no sort wording in the row;
//   (7) on 任務頁 and a 傳承 section page the row's count (「5 個任務」) sits at
//       its left end on the funnel's centre line, whole and on one line, at
//       320, 390 and 1440 in both languages.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { BUILTIN_THEME_IDS, expectBuiltinApplied } from "./builtinThemes";
import {
  AppPageStory,
  RepliesPageStory,
} from "./stories/RepliesFilterPanelStory";

type Rgba = { r: number; g: number; b: number; a: number };

function parseColor(s: string): Rgba {
  const rgb = s.match(/rgba?\(([^)]+)\)/i);
  if (rgb) {
    const p = rgb[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  const srgb = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (srgb) {
    const [chans, alpha] = srgb[1].split("/").map((x) => x.trim());
    const c = chans.split(/\s+/).map((x) => parseFloat(x));
    return {
      r: c[0] * 255,
      g: c[1] * 255,
      b: c[2] * 255,
      a: alpha === undefined ? 1 : parseFloat(alpha),
    };
  }
  throw new Error(`unparseable colour: ${s}`);
}

function over(fg: Rgba, bg: Rgba): Rgba {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  };
}

function sameColour(a: Rgba, b: Rgba): boolean {
  return (
    Math.abs(a.r - b.r) < 0.5 &&
    Math.abs(a.g - b.g) < 0.5 &&
    Math.abs(a.b - b.b) < 0.5
  );
}

const FULL_ID = "rc-428906235337"; // 15 chars, the real shape (api_replycards.go:283)

/** The fields live in a layer portalled to <body>, so they are found on the
 * page, not inside the mount. */
async function openFilters(cmp: Locator, page: Page) {
  await cmp.getByTestId("replies-filter-toggle").click();
  await expect(page.locator('[role="dialog"]')).toBeVisible();
}

async function applyId(cmp: Locator, page: Page, id: string) {
  const field = page.getByTestId("filter-reply-card-id");
  await field.fill(id);
  await field.press("Enter");
  await expect(field).toHaveValue(id);
  await expect(cmp.getByTestId("replies-empty")).toBeVisible();
}

const FIELD_COUNT = { tasks: 5, replies: 4, lore: 3 };
const DROPDOWN = {
  tasks: ["filter-status", "狀態"],
  replies: ["filter-priority", "任務優先序"],
  lore: ["lore-filter-state", "狀態"],
};

for (const pageName of ["tasks", "replies", "lore"] as const) {
  for (const width of [390, 721, 1440]) {
    test(`${pageName} width ${width}: the 篩選 ${width > 720 ? "panel" : "sheet"} holds every field inside the viewport, with no horizontal overflow, and a dropdown in it is not clipped`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: width > 720 ? 900 : 844 });
      const cmp = await mount(<AppPageStory page={pageName} theme="office" wide={width > 1040} />);
      await cmp.getByTestId(`${pageName}-filter-toggle`).click();
      const layer = page.getByTestId(`${pageName}-filter-${width > 720 ? "pop" : "sheet"}`);
      await expect(layer).toBeVisible();

      const m = await layer.evaluate((box) => {
        const r = box.getBoundingClientRect();
        const grid = box.querySelector(".filter-panel__grid") as HTMLElement;
        const fields = Array.from(grid.querySelectorAll<HTMLElement>(".filter-panel__field")).map((f) => {
          const control = f.lastElementChild as HTMLElement;
          const label = f.querySelector(".filter-panel__label") as HTMLElement;
          const c = control.getBoundingClientRect();
          const range = document.createRange();
          range.selectNodeContents(label);
          return {
            label: label.textContent,
            left: c.left,
            right: c.right,
            top: c.top,
            bottom: c.bottom,
            labelLines: new Set(Array.from(range.getClientRects()).map((x) => Math.round(x.top))).size,
          };
        });
        return {
          box: { left: r.left, right: r.right, top: r.top, bottom: r.bottom },
          boxOverflow: box.scrollWidth - box.clientWidth,
          gridOverflow: grid.scrollWidth - grid.clientWidth,
          page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          fields,
        };
      });
      expect(m.page, "page horizontal overflow").toBeLessThanOrEqual(1);
      expect(m.boxOverflow, "layer horizontal overflow").toBeLessThanOrEqual(1);
      expect(m.gridOverflow, "field grid horizontal overflow").toBeLessThanOrEqual(1);
      expect(m.box.left).toBeGreaterThanOrEqual(-0.5);
      expect(m.box.right).toBeLessThanOrEqual(width + 0.5);
      expect(m.box.bottom).toBeLessThanOrEqual((width > 720 ? 900 : 844) + 0.5);
      expect(m.fields).toHaveLength(FIELD_COUNT[pageName]);
      for (const f of m.fields) {
        expect(f.left, `${f.label} left edge`).toBeGreaterThanOrEqual(m.box.left);
        expect(f.right, `${f.label} right edge`).toBeLessThanOrEqual(m.box.right);
        expect(f.top, `${f.label} top edge`).toBeGreaterThanOrEqual(m.box.top);
        expect(f.bottom, `${f.label} bottom edge`).toBeLessThanOrEqual(m.box.bottom);
        expect(f.labelLines, `${f.label} label lines`).toBe(1);
      }
      const rows = new Set(m.fields.slice(1).map((f) => Math.round(f.top))).size;
      expect(rows, "the dropdowns in two columns").toBe(Math.ceil((FIELD_COUNT[pageName] - 1) / 2));
      expect(Math.round(m.fields[0].right - m.fields[0].left), "the ID field spans both columns").toBe(
        Math.round(m.fields[2].right - m.fields[1].left)
      );

      const [dropdown, noun] = DROPDOWN[pageName];
      await page.getByTestId(dropdown).click();
      const pop = page.getByRole("group", { name: noun });
      await expect(pop).toBeVisible();
      const p = (await pop.boundingBox())!;
      expect(p.x).toBeGreaterThanOrEqual(0);
      expect(p.x + p.width).toBeLessThanOrEqual(width);
      expect(p.y).toBeGreaterThanOrEqual(0);
      expect(p.y + p.height).toBeLessThanOrEqual(width > 720 ? 900 : 844);
      const onTop = await pop.evaluate((el) => {
        const r = el.getBoundingClientRect();
        const probes = [
          [r.left + r.width / 2, r.top + 8],
          [r.left + r.width / 2, r.bottom - 8],
        ];
        return probes.every(([x, y]) => el.contains(document.elementFromPoint(x, y)));
      });
      expect(onTop, "the popover paints above the layer at its top and bottom").toBe(true);
      expect(await pop.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
      if (width <= 720) return;

      const funnel = page.getByTestId(`${pageName}-filter-toggle`);
      const f = (await funnel.boundingBox())!;
      expect(m.box.top - (f.y + f.height), "the panel hangs just under the funnel").toBeGreaterThanOrEqual(0);
      expect(m.box.top - (f.y + f.height)).toBeLessThanOrEqual(6);
      expect(Math.abs(m.box.right - (f.x + f.width)), "right edges aligned").toBeLessThanOrEqual(1);
      await expect(page.locator('[data-testid$="-filter-backdrop"]'), "nothing dims the page").toHaveCount(0);
      await expect(layer).not.toHaveAttribute("aria-modal", /.*/);

      await page.keyboard.press("Escape");
      await expect(pop, "Esc closes the open dropdown first").toBeHidden();
      await expect(layer).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(layer).toBeHidden();
      await expect(funnel).toBeFocused();

      await funnel.click();
      await expect(layer).toBeVisible();
      await page.mouse.click(4, (width > 720 ? 900 : 844) - 4);
      await expect(layer, "an outside click closes it").toBeHidden();

      await funnel.click();
      await expect(layer).toBeVisible();
      await funnel.click();
      await expect(layer, "the funnel closes it again").toBeHidden();
    });
  }
}

for (const width of [320, 1040]) {
  test(`width ${width}: a whole 請示卡 id fits the field in the 篩選 layer without overflowing it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    expect(FULL_ID.length, "the id shape this guard is calibrated on").toBe(15);
    const cmp = await mount(<RepliesPageStory theme="office" />);
    await openFilters(cmp, page);
    await applyId(cmp, page, FULL_ID);
    const field = page.getByTestId("filter-reply-card-id");
    const m = await field.evaluate((el: HTMLInputElement) => ({
      scroll: el.scrollWidth - el.clientWidth,
      page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    }));
    expect(m.scroll, "a complete id must fit without the field scrolling").toBeLessThanOrEqual(1);
    expect(m.page, "page horizontal overflow").toBeLessThanOrEqual(1);
  });
}

test("the folded 篩選 row is page content: the list starts below it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="office" />);

  const panel = cmp.getByTestId("replies-filter");
  const below = cmp.getByRole("tabpanel");
  await expect(panel).toBeVisible();
  await expect(below).toBeVisible();
  expect(["fixed", "absolute", "sticky"]).not.toContain(
    await panel.evaluate((node) => getComputedStyle(node).position)
  );
  const panelBox = (await panel.boundingBox())!;
  const belowBox = (await below.boundingBox())!;
  expect(panelBox.height).toBeGreaterThan(0);
  expect(panelBox.y + panelBox.height, "the row's bottom edge vs the list below it")
    .toBeLessThanOrEqual(belowBox.y + 1);
});

test("the ID field is no taller than the pill triggers beside it in the panel", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1040, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="office" />);
  await openFilters(cmp, page);

  const field = (await page.getByTestId("filter-reply-card-id").boundingBox())!;
  const pill = (await page.getByTestId("filter-opener").boundingBox())!;
  expect(pill.height).toBeGreaterThan(0);
  expect(Math.abs(field.height - pill.height), "the id field and the pills keep one height")
    .toBeLessThanOrEqual(1);
});

for (const theme of BUILTIN_THEME_IDS) {
  test(`theme ${theme}: the ID field is distinguishable from the panel behind it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1040, height: 900 });
    const cmp = await mount(<RepliesPageStory theme={theme} />);
    await expectBuiltinApplied(page, theme);
    await openFilters(cmp, page);

    const field = page.getByTestId("filter-reply-card-id");
    await expect(field).toBeVisible();
    const colours = await field.evaluate((node) => {
      const cs = getComputedStyle(node);
      return {
        border: cs.borderTopColor,
        fill: cs.backgroundColor,
        borderWidth: cs.borderTopWidth,
        behind: getComputedStyle(node.closest(".filter-panel__pop")!).backgroundColor,
      };
    });
    const behind = parseColor(colours.behind);
    const border = over(parseColor(colours.border), behind);
    const fill = over(parseColor(colours.fill), behind);
    expect(parseFloat(colours.borderWidth), "the field must declare a border width").toBeGreaterThan(0);
    expect(
      sameColour(border, behind) && sameColour(fill, behind),
      "the field must differ from the panel by a border, a fill, or both"
    ).toBe(false);
  });
}

test("emptying the 編號 box is an escape from an applied filter", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="office" />);

  const empty = cmp.getByTestId("replies-empty");
  await expect(empty).toHaveText("✓ 目前沒有待處理的請示");
  await openFilters(cmp, page);
  await expect(page.getByTestId("replies-filter-clear"), "no 清除篩選 while nothing is filtered")
    .toHaveCount(0);

  await applyId(cmp, page, FULL_ID);
  await expect(empty).toHaveText("沒有符合篩選條件的請示");
  await expect(page.getByTestId("replies-filter-clear")).toBeVisible();
  await expect(cmp.getByTestId("replies-filter-count")).toHaveText("1");

  const field = page.getByTestId("filter-reply-card-id");
  await field.fill("");
  await field.blur();
  await expect(empty).toHaveText("✓ 目前沒有待處理的請示");
  await expect(page.getByTestId("replies-filter-clear")).toHaveCount(0);
  await expect(cmp.getByTestId("replies-filter-count")).toHaveCount(0);
});

test("the 篩選 shell adds no spacing of its own: the gap below the label row is the page's", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="office-light" />);
  await cmp.getByRole("tabpanel").waitFor();

  const m = await cmp.evaluate((root: HTMLElement) => {
    const panel = root.querySelector(".filter-panel") as HTMLElement;
    const head = root.querySelector(".replies__head") as HTMLElement;
    const list = root.querySelector('[role="tabpanel"]') as HTMLElement;
    const tabs = root.querySelector('[role="tablist"]') as HTMLElement;
    const cs = getComputedStyle(panel);
    const headBottom = head.getBoundingClientRect().bottom;
    return {
      hostGap: parseFloat(getComputedStyle(head.parentElement!).rowGap || "0"),
      actual: list.getBoundingClientRect().top - headBottom,
      rowBottomInHead:
        headBottom - Math.max(panel.getBoundingClientRect().bottom, tabs.getBoundingClientRect().bottom),
      panelMargins: [cs.marginTop, cs.marginBottom],
    };
  });
  expect(m.hostGap, "the host must own a real rhythm for this test to mean anything").toBeGreaterThan(0);
  expect(m.panelMargins, "the shell's own vertical margins").toEqual(["0px", "0px"]);
  expect(m.rowBottomInHead, "the label row ends at the lower of the labels and the shell").toBeCloseTo(0, 0);
  expect(m.actual, "gap under the label row").toBeCloseTo(m.hostGap, 0);
});

const TOOLBAR_FITS_ONE_ROW = { zh: 375, en: 390 };

for (const width of [320, 375, 390, 393, 1440]) {
  for (const language of ["zh", "en"] as const) {
    const oneRow = width >= TOOLBAR_FITS_ONE_ROW[language];
    test(`width ${width}, ${language}: the three list labels keep one line inside the column, each label whole or cut with 「…」, the 篩選 and 顯示方式 buttons share a line ${oneRow ? "with them" : "below them"}, the 24h note ${width > 720 ? "beside them" : "on its own line under the row"}, and the row names no sort`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<RepliesPageStory theme="office" />);
      const tabs = cmp.getByRole("tab");
      await expect(tabs).toHaveCount(3);
      // The answered count only shows once that list is loaded; it is the
      // widest the row gets.
      await cmp.getByTestId("replies-tab-answered").click();
      await expect(cmp.getByTestId("replies-tab-count-answered")).toBeVisible();
      await cmp.getByTestId("replies-tab-waiting").click();

      const m = await cmp.evaluate((root: HTMLElement) => {
        const r = (el: Element) => {
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const list = root.querySelector('[role="tablist"]') as HTMLElement;
        const column = root.querySelector(".replies") as HTMLElement;
        const labels = Array.from(list.querySelectorAll<HTMLElement>(".replies__tab-label")).map((l) => {
          const range = document.createRange();
          range.selectNodeContents(l);
          return {
            text: l.textContent,
            title: l.title,
            lines: new Set(Array.from(range.getClientRects()).map((x) => Math.round(x.top))).size,
            cut: l.scrollWidth > l.clientWidth + 0.5,
            ellipsis: getComputedStyle(l).textOverflow,
          };
        });
        return {
          list: r(list),
          listOverflow: list.scrollWidth - list.clientWidth,
          column: r(column),
          tabs: Array.from(list.querySelectorAll('[role="tab"]')).map(r),
          labels,
          headText: (root.querySelector(".replies__head") as HTMLElement).textContent,
          funnel: r(root.querySelector('[data-testid="replies-filter-toggle"]')!),
          display: r(root.querySelector('[data-testid="display-options"]')!),
          page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          columnOverflow: column.scrollWidth - column.clientWidth,
        };
      });
      expect(m.page, "page horizontal overflow").toBeLessThanOrEqual(0);
      expect(m.columnOverflow, "column horizontal overflow").toBeLessThanOrEqual(0);
      expect(m.listOverflow, "the labels overflow their control").toBeLessThanOrEqual(0);
      expect(m.list.left).toBeGreaterThanOrEqual(m.column.left - 0.5);
      expect(m.list.right).toBeLessThanOrEqual(m.column.right + 0.5);
      const tops = m.tabs.map((t) => Math.round(t.top));
      expect(new Set(tops).size, `labels on one line: ${tops}`).toBe(1);
      for (const t of m.tabs) {
        expect(t.left).toBeGreaterThanOrEqual(m.list.left - 0.5);
        expect(t.right).toBeLessThanOrEqual(m.list.right + 0.5);
      }
      for (const l of m.labels) {
        expect(l.lines, `${l.title} on one line`).toBe(1);
        expect(l.title, "the full label in its title").toBe(l.text);
        if (l.cut) expect(l.ellipsis, `${l.title} cut with 「…」`).toBe("ellipsis");
      }
      if (language === "zh" || width > 390) {
        expect(m.labels.filter((l) => l.cut), "every label whole").toEqual([]);
      }

      const mid = (b: { top: number; bottom: number }) => (b.top + b.bottom) / 2;
      const tools = [m.funnel, m.display];
      expect(Math.abs(mid(m.funnel) - mid(m.display)), "the buttons on one line").toBeLessThanOrEqual(2);
      for (const b of tools) expect(b.right).toBeLessThanOrEqual(m.column.right + 0.5);
      expect(m.headText).not.toContain(language === "zh" ? "開卡時間" : "Opened");
      if (oneRow) {
        expect(Math.abs(mid(m.funnel) - mid(m.list)), "the buttons beside the labels").toBeLessThanOrEqual(2);
        expect(m.funnel.left, "the buttons after the labels").toBeGreaterThanOrEqual(m.list.right);
      } else {
        expect(m.funnel.top, "the buttons on the line below the labels").toBeGreaterThanOrEqual(m.list.bottom);
      }

      await cmp.getByTestId("replies-tab-answered").click();
      const h = await cmp.evaluate((root: HTMLElement) => {
        const r = (el: Element) => {
          const b = el.getBoundingClientRect();
          return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
        };
        const hint = root.querySelector('[data-testid="replies-answered-window"]') as HTMLElement;
        const range = document.createRange();
        range.selectNodeContents(hint);
        return {
          hint: r(hint),
          lines: new Set(Array.from(range.getClientRects()).map((x) => Math.round(x.top))).size,
          list: r(root.querySelector('[role="tablist"]')!),
          funnel: r(root.querySelector('[data-testid="replies-filter-toggle"]')!),
          display: r(root.querySelector('[data-testid="display-options"]')!),
          panel: r(root.querySelector('[role="tabpanel"]')!),
          page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        };
      });
      expect(h.page, "page horizontal overflow with the note shown").toBeLessThanOrEqual(0);
      expect(h.lines, "the note on one line").toBe(1);
      expect(h.hint.left).toBeGreaterThanOrEqual(m.column.left - 0.5);
      expect(h.hint.right).toBeLessThanOrEqual(m.column.right + 0.5);
      expect(h.hint.bottom, "the note above the list").toBeLessThanOrEqual(h.panel.top);
      expect(Math.abs(h.funnel.top - m.funnel.top), "the note moves no button").toBeLessThanOrEqual(0.5);
      expect(Math.abs(h.display.left - m.display.left), "the note moves no button").toBeLessThanOrEqual(0.5);
      if (width > 720) {
        expect(Math.abs(mid(h.hint) - mid(h.list)), "the note beside the labels").toBeLessThanOrEqual(2);
        expect(h.hint.left, "the note after the labels").toBeGreaterThanOrEqual(h.list.right);
      } else {
        expect(h.hint.top, "the note under the row").toBeGreaterThanOrEqual(Math.max(h.list.bottom, h.funnel.bottom));
      }
    });
  }
}


const COUNT_WORDING = {
  tasks: { zh: /^\d+ 個任務$/, en: /^\d+ tasks?$/ },
  lore: { zh: /^\d+ 筆傳承$/, en: /^\d+ (entry|entries)$/ },
};

for (const pageName of ["tasks", "lore"] as const) {
  for (const width of [320, 390, 1440]) {
    for (const language of ["zh", "en"] as const) {
      test(`${pageName} width ${width}, ${language}: the count sits whole on one line at the 篩選 row's left end, on the funnel's centre line, inside the column`, async ({
        mount,
        page,
      }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
        const cmp = await mount(<AppPageStory page={pageName} theme="office" wide={width > 1040} />);
        const summary = cmp.getByTestId(`${pageName}-filter-summary`);
        await expect(summary).toHaveText(COUNT_WORDING[pageName][language]);

        const m = await summary.evaluate((el: HTMLElement) => {
          const r = (e: Element) => {
            const b = e.getBoundingClientRect();
            return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
          };
          const row = el.closest(".filter-panel")!;
          const range = document.createRange();
          range.selectNodeContents(el);
          return {
            count: r(el),
            text: range.getBoundingClientRect(),
            row: r(row),
            main: r(el.closest(".app__main")!),
            funnel: r(row.querySelector(".filter-panel__trigger")!),
            lines: new Set(Array.from(range.getClientRects()).map((x) => Math.round(x.top))).size,
            cut: el.scrollWidth > el.clientWidth + 0.5,
            page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          };
        });
        const mid = (b: { top: number; bottom: number }) => (b.top + b.bottom) / 2;
        expect(m.page, "page horizontal overflow").toBeLessThanOrEqual(0);
        expect(m.lines, "the count on one line").toBe(1);
        expect(m.cut, "the count whole").toBe(false);
        expect(Math.abs(m.count.left - m.row.left), "the count at the row's left end").toBeLessThanOrEqual(1);
        expect(m.count.right, "the count left of the funnel").toBeLessThanOrEqual(m.funnel.left);
        expect(Math.abs(mid(m.text) - mid(m.funnel)), "the count's text on the funnel's centre line").toBeLessThanOrEqual(2);
        expect(m.row.left).toBeGreaterThanOrEqual(m.main.left - 0.5);
        expect(m.row.right).toBeLessThanOrEqual(m.main.right + 0.5);
      });
    }
  }
}
