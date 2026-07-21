// T-0b41-p2 — dual-layer theme reconcile, REAL browser.
//
// The unit suite (src/i18n/index.test.tsx) can see the data-theme attribute and
// localStorage, but jsdom applies no CSS, so it CANNOT prove the theme visually
// takes effect — a `:root[data-theme="xian"]` variable swap that never repaints
// is invisible to it. These guards mount the provider against the REAL theme.css
// in Chromium and assert the swatch's resolved --color-bg:
//   office #191c24 → rgb(25, 28, 36) ; xian #14100a → rgb(20, 16, 10).
import { test, expect } from "@playwright/experimental-ct-react";
import { DisplayPrefsReconcileStory } from "./stories/DisplayPrefsReconcileStory";

const OFFICE_BG = "rgb(25, 28, 36)";
const XIAN_BG = "rgb(20, 16, 10)";

test.beforeEach(async ({ page }) => {
  // Fresh origin state each run: the localStorage cache + any token from a prior
  // guard must not bleed in. Set on the CT harness page BEFORE mount.
  await page.evaluate(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
});

test("pre-auth: the localStorage cache drives the first paint (no server)", async ({
  mount,
  page,
}) => {
  // Seed the cache to xian and mount with NO token — /api/settings is
  // unreachable pre-auth, so the cache is the only source of the first paint.
  await page.evaluate(() => localStorage.setItem("oc.theme", "xian"));
  const cmp = await mount(<DisplayPrefsReconcileStory />);
  const swatch = cmp.getByTestId("swatch");
  // The swatch paints the xian canvas — the cached theme VISUALLY took effect,
  // not just the attribute (a real browser resolved the variable swap).
  await expect(swatch).toHaveAttribute("data-theme-name", "xian");
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(XIAN_BG);
  }).toPass();
});

test("login: the server value reconciles in and visually replaces the cache", async ({
  mount,
  page,
}) => {
  // Cache is empty → the pre-auth first paint is the office default. The mock
  // server holds xian, applied when the login mints a token (oc-auth-login).
  const cmp = await mount(<DisplayPrefsReconcileStory serverTheme="xian" />);
  const swatch = cmp.getByTestId("swatch");
  // Pre-auth: office default is what paints.
  await expect(swatch).toHaveAttribute("data-theme-name", "office");
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(OFFICE_BG);
  }).toPass();

  // Log in → reconcile pulls the server's xian and applies it.
  await cmp.getByTestId("login").click();
  await expect(swatch).toHaveAttribute("data-theme-name", "xian");
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(XIAN_BG);
  }).toPass();

  // …and the server value is written back to the cache so the NEXT pre-auth
  // first paint is already correct (the dual layer's whole point).
  const cached = await page.evaluate(() => localStorage.getItem("oc.theme"));
  expect(cached).toBe("xian");
});
