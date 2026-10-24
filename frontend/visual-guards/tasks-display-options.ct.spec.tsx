// The 任務頁 顯示方式 button and its panel, on the real page in a real browser:
// its label must clear AA in every built-in theme (closed and open, since the
// open state paints a fill), and the button must hold the filter row's right
// edge at desktop and phone widths without pushing the page sideways.
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

for (const width of [1280, 390, 320]) {
  test(`width ${width}: 顯示方式 holds the filter row's right edge and its panel stays on screen`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<AppPageStory page="tasks" theme="office-light" />);
    const trigger = cmp.getByTestId("display-options");
    await expect(trigger).toBeVisible();

    const row = (await cmp.getByTestId("tasks-filter").boundingBox())!;
    const button = (await trigger.boundingBox())!;
    expect(Math.abs(row.x + row.width - (button.x + button.width))).toBeLessThanOrEqual(1);

    await trigger.click();
    const panel = (await cmp.getByTestId("display-options-panel").boundingBox())!;
    expect(Math.abs(panel.x + panel.width - (button.x + button.width))).toBeLessThanOrEqual(1);
    expect(panel.x).toBeGreaterThanOrEqual(0);
    expect(panel.y).toBeGreaterThan(button.y + button.height - 1);

    const overflow = await horizontalOverflow(page);
    expect(overflow.page, "page horizontal overflow").toBeLessThanOrEqual(1);
    expect(overflow.list, "list horizontal overflow").toBeLessThanOrEqual(1);
  });
}
