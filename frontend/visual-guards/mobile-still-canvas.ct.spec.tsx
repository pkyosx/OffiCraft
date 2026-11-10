import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { MobileStillCanvasStory } from "./stories/MobileStillCanvasStory";

const PHONE = { width: 390, height: 844 };
const DESKTOP = { width: 1280, height: 800 };

function bodyBackground(page: Page): Promise<string> {
  return page.evaluate(() => getComputedStyle(document.body).backgroundImage);
}

async function expectBodyBackground(page: Page, prefix: string) {
  await expect.poll(() => bodyBackground(page), { timeout: 10_000 }).toMatch(new RegExp(`^url\\("${prefix}`));
}

test.beforeEach(async ({ page }) => {
  await page.evaluate(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
});

test("the built-in desk theme shows a still frame at phone width and the animation on desktop, switching live on resize", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(DESKTOP);
  await mount(<MobileStillCanvasStory theme="builtin-desk" />);
  await expectBodyBackground(page, "data:image/webp;base64,");

  await page.setViewportSize(PHONE);
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

  await page.setViewportSize(DESKTOP);
  await expectBodyBackground(page, "data:image/webp;base64,");
});

test("背景動畫 靜止 saved on this device shows the still frame at desktop width", async ({ mount, page }) => {
  await page.evaluate(() => localStorage.setItem("oc.bgMotion", "still"));
  await page.setViewportSize(DESKTOP);
  await mount(<MobileStillCanvasStory theme="builtin-desk" />);
  await expectBodyBackground(page, "blob:");
});

test("picking 背景動畫 播放 at phone width brings the animation back, and 靜止 on desktop the still frame, without a reload", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(PHONE);
  const story = await mount(<MobileStillCanvasStory theme="builtin-desk" appearance />);
  await expectBodyBackground(page, "blob:");
  const select = story.getByRole("combobox", { name: "背景動畫" });

  await select.selectOption("play");
  await expectBodyBackground(page, "data:image/webp;base64,");

  await page.setViewportSize(DESKTOP);
  await select.selectOption("still");
  await expectBodyBackground(page, "blob:");

  await select.selectOption("auto");
  await expectBodyBackground(page, "data:image/webp;base64,");
});

test("a custom theme with an animated canvas opened at phone width shows the still frame", async ({ mount, page }) => {
  await page.setViewportSize(PHONE);
  await mount(<MobileStillCanvasStory theme="custom-animated" />);
  await expectBodyBackground(page, "blob:");
});

test("a custom theme with a still-image canvas keeps its own image at phone width", async ({ mount, page }) => {
  await page.setViewportSize(PHONE);
  await mount(<MobileStillCanvasStory theme="custom-still" />);
  await expectBodyBackground(page, "data:image/png;base64,");
  await page.setViewportSize(DESKTOP);
  await expectBodyBackground(page, "data:image/png;base64,");
});

test("at phone width, switching from the desk theme's still frame to a still-image theme and then to office shows each new theme's background", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(PHONE);
  const story = await mount(<MobileStillCanvasStory theme="builtin-desk" />);
  await expectBodyBackground(page, "blob:");

  await story.getByRole("button", { name: "custom-still" }).click();
  await expectBodyBackground(page, "data:image/png;base64,");

  await story.getByRole("button", { name: "office" }).click();
  await expect.poll(() => bodyBackground(page)).toBe("none");
});

test("at phone width, switching themes before the desk theme's still frame is ready never shows that still on the new theme", async ({
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
  await expectBodyBackground(page, "data:image/webp;base64,");

  await story.getByRole("button", { name: "custom-still" }).click();
  await expectBodyBackground(page, "data:image/png;base64,");

  await page.evaluate(() => (window as typeof window & { releaseStill: () => void }).releaseStill());
  await expect.poll(() => page.evaluate(() => (window as typeof window & { stillMade?: boolean }).stillMade)).toBe(true);
  await page.evaluate(() => new Promise((r) => setTimeout(r, 50)));
  expect(await bodyBackground(page)).toMatch(/^url\("data:image\/png;base64,/);
});
