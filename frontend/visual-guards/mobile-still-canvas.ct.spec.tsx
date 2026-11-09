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
