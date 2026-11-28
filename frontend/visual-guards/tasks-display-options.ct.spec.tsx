// The 任務頁 顯示方式 button and its panel, on the real page in a real browser:
// its label must clear AA in every built-in theme (closed and open, since the
// open state paints a fill), and the button must hold the filter row's right
// edge at desktop and phone widths without pushing the page sideways; a
// non-default choice puts a dot inside the button's own box. On a computer its
// panel hangs under the button; on a phone (任務頁 and 請示頁 alike) it is the
// 篩選 bottom sheet, with the select and its longest choice fully visible.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { AppPageStory } from "./stories/RepliesFilterPanelStory";
import { BUILTIN_THEME_IDS, expectBuiltinApplied } from "./builtinThemes";

type Rgb = [number, number, number];

function contrast(a: Rgb, b: Rgb): number {
  const lum = ([r, g, bl]: Rgb) => {
    const lin = (v: number) => {
      const c = v / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(bl);
  };
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** The label colour against every background painted beneath it. Computed
 * colours arrive in whatever space color-mix chose (oklab here), so the
 * browser composites them itself on a 1×1 canvas, from the root up to the
 * element. */
async function labelContrast(loc: Locator): Promise<number> {
  const { fg, bg } = await loc.evaluate((el) => {
    for (const a of el.getAnimations()) a.finish();
    const layers: string[] = [];
    for (let n: Element | null = el; n; n = n.parentElement) {
      layers.unshift(getComputedStyle(n).backgroundColor);
    }
    const ctx = document.createElement("canvas").getContext("2d")!;
    const paint = (fill: string) => {
      ctx.fillStyle = fill;
      ctx.fillRect(0, 0, 1, 1);
    };
    const read = () => Array.from(ctx.getImageData(0, 0, 1, 1).data.slice(0, 3));
    paint("#fff");
    layers.forEach(paint);
    const under = read();
    paint(getComputedStyle(el).color);
    return { fg: read(), bg: under };
  });
  return contrast(fg as Rgb, bg as Rgb);
}

async function horizontalOverflow(page: Page) {
  return page.evaluate(() => ({
    page: document.documentElement.scrollWidth - window.innerWidth,
    list: Math.max(
      0,
      ...Array.from(document.querySelectorAll<HTMLElement>(".tasks")).map(
        (el) => el.scrollWidth - el.clientWidth
      )
    ),
  }));
}

/** Where the open 顯示方式 layer sits, and whether its select shows every
 * choice whole (text plus the native arrow inside the select's content box). */
async function displayLayer(page: Page, selectTestId: string) {
  return page.evaluate((selectTestId) => {
    const r = (el: Element | null) => {
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const sel = document.querySelector<HTMLSelectElement>(`[data-testid="${selectTestId}"]`)!;
    const cs = getComputedStyle(sel);
    const ctx = document.createElement("canvas").getContext("2d")!;
    ctx.font = cs.font;
    return {
      sheet: r(document.querySelector('[data-testid="display-options-sheet"]')),
      pop: r(document.querySelector('[data-testid="display-options-panel"]')),
      button: r(document.querySelector('[data-testid="display-options"]')),
      select: r(sel),
      room: sel.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight),
      widest: Math.max(...Array.from(sel.options).map((o) => ctx.measureText(o.text).width)),
      vw: window.innerWidth,
      vh: window.innerHeight,
      page: document.documentElement.scrollWidth - window.innerWidth,
    };
  }, selectTestId);
}

const SELECT_ARROW = 20;

function expectDisplayLayer(width: number, g: Awaited<ReturnType<typeof displayLayer>>) {
  expect(g.page, `[${width}px] page horizontal overflow`).toBeLessThanOrEqual(1);
  expect(g.select!.left, `[${width}px] select left edge`).toBeGreaterThanOrEqual(0);
  expect(g.select!.right, `[${width}px] select right edge`).toBeLessThanOrEqual(g.vw);
  if (width <= 720) {
    expect(g.pop, `[${width}px] no anchored panel on a phone`).toBeNull();
    const sh = g.sheet!;
    expect(sh, `[${width}px] bottom sheet`).not.toBeNull();
    expect(Math.abs(sh.bottom - g.vh), `[${width}px] sheet on the bottom edge`).toBeLessThanOrEqual(1);
    expect(sh.left).toBeLessThanOrEqual(0.5);
    expect(sh.right).toBeGreaterThanOrEqual(g.vw - 0.5);
    expect(sh.bottom - sh.top, `[${width}px] sheet at most 70% tall`).toBeLessThanOrEqual(g.vh * 0.7 + 1);
    expect(g.select!.left).toBeGreaterThanOrEqual(sh.left);
    expect(g.select!.right).toBeLessThanOrEqual(sh.right);
    expect(g.select!.top).toBeGreaterThanOrEqual(sh.top);
    expect(g.select!.bottom).toBeLessThanOrEqual(sh.bottom);
    expect(g.widest + SELECT_ARROW, `[${width}px] the longest choice whole in the select`).toBeLessThanOrEqual(g.room);
  } else {
    expect(g.sheet, `[${width}px] no sheet on a computer`).toBeNull();
    const pop = g.pop!;
    expect(pop, `[${width}px] anchored panel`).not.toBeNull();
    expect(Math.abs(pop.right - g.button!.right), `[${width}px] panel right edge on the button's`).toBeLessThanOrEqual(1);
    expect(pop.top, `[${width}px] panel under the button`).toBeGreaterThan(g.button!.bottom - 1);
    expect(pop.left).toBeGreaterThanOrEqual(0);
  }
}

for (const theme of BUILTIN_THEME_IDS) {
  test(`${theme}: the 顯示方式 label clears AA (≥4.5:1) closed and open`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    const cmp = await mount(<AppPageStory page="tasks" theme={theme} />);
    const trigger = cmp.getByTestId("display-options");
    await expect(trigger).toBeVisible();
    await expectBuiltinApplied(page, theme);

    await page.mouse.move(0, 0);
    const closed = await labelContrast(trigger);
    await trigger.click();
    await expect(cmp.getByTestId("display-options-panel")).toBeVisible();
    await page.mouse.move(0, 0);
    const opened = await labelContrast(trigger);
    console.log(`${theme} 顯示方式 contrast: closed ${closed.toFixed(2)}, open ${opened.toFixed(2)}`);
    expect(closed).toBeGreaterThanOrEqual(4.5);
    expect(opened).toBeGreaterThanOrEqual(4.5);
  });
}

for (const width of [1440, 1280, 390, 375, 360, 320]) {
  test(`width ${width}: 顯示方式 holds the filter row's right edge with the funnel right beside it, its badge unclipped, and it opens ${width > 720 ? "a panel under it" : "the bottom sheet"} with nothing clipped`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<AppPageStory page="tasks" theme="office-light" />);
    const trigger = cmp.getByTestId("display-options");
    await expect(trigger).toBeVisible();

    await expect(trigger).toHaveAccessibleName("顯示方式");
    const labelWidth = (await trigger.locator(".display-options__label").boundingBox())!.width;
    if (width <= 390) expect(labelWidth, "phone shows the icon only").toBeLessThanOrEqual(1);
    else expect(labelWidth).toBeGreaterThan(20);

    const row = (await cmp.getByTestId("tasks-filter").boundingBox())!;
    const button = (await trigger.boundingBox())!;
    expect(Math.abs(row.x + row.width - (button.x + button.width))).toBeLessThanOrEqual(1);

    const funnel = (await cmp.getByTestId("tasks-filter-toggle").boundingBox())!;
    expect(Math.abs(funnel.y - button.y), "funnel and 顯示方式 share one line").toBeLessThanOrEqual(1);
    expect(Math.abs(funnel.height - button.height)).toBeLessThanOrEqual(1);
    expect(button.x - (funnel.x + funnel.width)).toBeGreaterThan(0);
    expect(button.x - (funnel.x + funnel.width)).toBeLessThanOrEqual(10);

    // The default 未結案 is one condition. The badge overhangs the funnel's
    // top-right corner and must not be cut by the page's scroll box above it.
    const badge = cmp.getByTestId("tasks-filter-count");
    await expect(badge).toHaveText("1");
    const b = (await badge.boundingBox())!;
    const scrollTop = await page.evaluate(() => document.querySelector(".tasks")!.getBoundingClientRect().top);
    expect(b.y, "badge top inside the scroll box").toBeGreaterThanOrEqual(scrollTop - 0.5);
    expect(b.y).toBeLessThan(funnel.y);
    expect(b.x + b.width).toBeGreaterThan(funnel.x + funnel.width);

    await trigger.click();
    await expect(page.getByTestId(width > 720 ? "display-options-panel" : "display-options-sheet")).toBeVisible();
    expectDisplayLayer(width, await displayLayer(page, "group-by"));

    const overflow = await horizontalOverflow(page);
    expect(overflow.page, "page horizontal overflow").toBeLessThanOrEqual(1);
    expect(overflow.list, "list horizontal overflow").toBeLessThanOrEqual(1);

    await expect(cmp.getByTestId("display-options-changed")).toHaveCount(0);
    await page.getByTestId("group-by").selectOption("executor");
    await expect(page.getByTestId("display-options-sheet")).toHaveCount(0);
    await expect(page.getByTestId("display-options-panel")).toHaveCount(0);
    const dot = (await cmp.getByTestId("display-options-changed").boundingBox())!;
    expect(dot.width, "a non-default choice dots the button").toBeGreaterThan(4);
    expect(dot.x).toBeGreaterThanOrEqual(button.x);
    expect(dot.x + dot.width).toBeLessThanOrEqual(button.x + button.width);
    expect(dot.y).toBeGreaterThanOrEqual(button.y);
    await expect(trigger).toHaveAccessibleName("顯示方式：已改為負責人");
  });
}

for (const width of [1440, 390, 375]) {
  test(`width ${width}: the 請示頁's 顯示方式 opens ${width > 720 ? "a panel under it" : "the bottom sheet"} with every 排序依據 choice whole`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<AppPageStory page="replies" theme="office-light" />);
    await cmp.getByTestId("display-options").click();
    await expect(page.getByTestId(width > 720 ? "display-options-panel" : "display-options-sheet")).toBeVisible();
    expectDisplayLayer(width, await displayLayer(page, "replies-sort"));
    if (width <= 720) {
      await page.getByTestId("display-options-backdrop").click({ position: { x: 10, y: 10 } });
      await expect(page.getByTestId("display-options-sheet")).toHaveCount(0);
    }
  });
}
