import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { MobileStillCanvasStory } from "./stories/MobileStillCanvasStory";

const PHONE = { width: 390, height: 844 };
const LANDSCAPE_PHONE = { width: 844, height: 390 };
const DESKTOP = { width: 1280, height: 800 };

function bodyBackground(page: Page): Promise<string> {
  return page.evaluate(() => getComputedStyle(document.body).backgroundImage);
}

async function expectBodyBackground(page: Page, prefix: string) {
  await expect.poll(() => bodyBackground(page), { timeout: 10_000 }).toMatch(new RegExp(`^url\\("${prefix}`));
}

async function expectTouchDevice(page: Page, touch: boolean) {
  expect(await page.evaluate(() => window.matchMedia("(hover: none)").matches), "control: (hover: none)").toBe(touch);
}

test.beforeEach(async ({ page }) => {
  await page.evaluate(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
});

test.describe("on a touch device", () => {
  test.use({ hasTouch: true });

  test("the built-in desk theme shows its still frame in landscape at 844px wide and in portrait", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(LANDSCAPE_PHONE);
    await mount(<MobileStillCanvasStory theme="builtin-desk" />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "blob:");
    const still = await bodyBackground(page);
    const decoded = await page.evaluate(async (css) => {
      const src = /url\("([^"]+)"\)/.exec(css)![1];
      const img = new Image();
      img.src = src;
      await img.decode();
      return { width: img.naturalWidth, height: img.naturalHeight };
    }, still);
    expect(decoded).toEqual({ width: 1920, height: 1200 });

    await page.setViewportSize(PHONE);
    await expectBodyBackground(page, "blob:");
  });

  test("picking 背景動畫 播放 brings the animation back, and 自動 the still frame again, without a reload", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(LANDSCAPE_PHONE);
    const story = await mount(<MobileStillCanvasStory theme="builtin-desk" appearance />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "blob:");
    const select = story.getByRole("combobox", { name: "背景動畫" });

    await select.selectOption("play");
    await expectBodyBackground(page, "data:image/webp;base64,");

    await select.selectOption("auto");
    await expectBodyBackground(page, "blob:");
  });

  test("a custom theme with an animated canvas shows the still frame", async ({ mount, page }) => {
    await page.setViewportSize(PHONE);
    await mount(<MobileStillCanvasStory theme="custom-animated" />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "blob:");
  });

  test("a custom theme with a still-image canvas keeps its own image in portrait and landscape", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(PHONE);
    await mount(<MobileStillCanvasStory theme="custom-still" />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "data:image/png;base64,");
    await page.setViewportSize(LANDSCAPE_PHONE);
    await expectBodyBackground(page, "data:image/png;base64,");
  });

  test("switching from the desk theme's still frame to a still-image theme and then to office shows each new theme's background", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(PHONE);
    const story = await mount(<MobileStillCanvasStory theme="builtin-desk" />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "blob:");

    await story.getByRole("button", { name: "custom-still" }).click();
    await expectBodyBackground(page, "data:image/png;base64,");

    await story.getByRole("button", { name: "office" }).click();
    await expect.poll(() => bodyBackground(page)).toBe("none");
  });

  test("switching themes before the desk theme's still frame is ready never shows that still on the new theme", async ({
    mount,
    page,
  }) => {
    await page.evaluate(() => {
      const w = window as typeof window & { releaseStill?: () => void; stillMade?: boolean };
      const decode = window.createImageBitmap.bind(window);
      const gate = new Promise<void>((r) => (w.releaseStill = r));
      window.createImageBitmap = (async (...args: Parameters<typeof createImageBitmap>) => {
        await gate;
        return decode(...args);
      }) as typeof createImageBitmap;
      const makeUrl = URL.createObjectURL.bind(URL);
      URL.createObjectURL = (obj) => {
        w.stillMade = true;
        return makeUrl(obj);
      };
    });
    await page.setViewportSize(PHONE);
    const story = await mount(<MobileStillCanvasStory theme="builtin-desk" />);
    await expectTouchDevice(page, true);
    await expectBodyBackground(page, "data:image/webp;base64,");

    await story.getByRole("button", { name: "custom-still" }).click();
    await expectBodyBackground(page, "data:image/png;base64,");

    await page.evaluate(() => (window as typeof window & { releaseStill: () => void }).releaseStill());
    await expect.poll(() => page.evaluate(() => (window as typeof window & { stillMade?: boolean }).stillMade)).toBe(true);
    await page.evaluate(() => new Promise((r) => setTimeout(r, 50)));
    expect(await bodyBackground(page)).toMatch(/^url\("data:image\/png;base64,/);
  });
});

test.describe("on a mouse device", () => {
  test.use({ hasTouch: false });

  test("the built-in desk theme animates at phone width as on desktop", async ({ mount, page }) => {
    await page.setViewportSize(PHONE);
    await mount(<MobileStillCanvasStory theme="builtin-desk" />);
    await expectTouchDevice(page, false);
    await expectBodyBackground(page, "data:image/webp;base64,");
    await page.setViewportSize(DESKTOP);
    await expectBodyBackground(page, "data:image/webp;base64,");
  });

  test("背景動畫 靜止 saved on this device shows the still frame at desktop width", async ({ mount, page }) => {
    await page.evaluate(() => localStorage.setItem("oc.bgMotion", "still"));
    await page.setViewportSize(DESKTOP);
    await mount(<MobileStillCanvasStory theme="builtin-desk" />);
    await expectTouchDevice(page, false);
    await expectBodyBackground(page, "blob:");
  });

  test("at phone width the 背景動畫 select sits beside its name, 靜止 shows the still frame, and 自動 the animation again, without a reload", async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(PHONE);
    const story = await mount(<MobileStillCanvasStory theme="builtin-desk" appearance />);
    await expectTouchDevice(page, false);
    await expectBodyBackground(page, "data:image/webp;base64,");
    const select = story.getByRole("combobox", { name: "背景動畫" });
    const name = story.locator(".param-row__body", { hasText: "背景動畫" });
    const nameBox = (await name.boundingBox())!;
    const selectBox = (await select.boundingBox())!;
    expect(selectBox.x, "select sits beside the name, not under it").toBeGreaterThanOrEqual(nameBox.x + nameBox.width);
    expect(selectBox.y).toBeLessThan(nameBox.y + nameBox.height);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(PHONE.width);

    await select.selectOption("still");
    await expectBodyBackground(page, "blob:");

    await select.selectOption("auto");
    await expectBodyBackground(page, "data:image/webp;base64,");
  });
});
