// T-0b41-p2 / T-16a1 P35 — dual-layer theme reconcile, REAL browser.
//
// The unit suite (src/i18n/index.test.tsx) can see the data-theme-name and
// localStorage, but jsdom applies no CSS, so it CANNOT prove a theme visually
// takes effect — a custom bundle's setProperty colour swap that never repaints
// is invisible to it. These guards mount the provider against the REAL theme.css
// in Chromium and assert the swatch's resolved --color-bg.
//
// office is the only built-in now (修仙 and every other theme is an imported
// custom bundle, T-16a1 P35), so the dual layer is exercised with a custom
// bundle ("Midnight"): office base #191c24 → rgb(25, 28, 36); the bundle's
// #010203 → rgb(1, 2, 3).
import { test, expect } from "@playwright/experimental-ct-react";
import { DisplayPrefsReconcileStory } from "./stories/DisplayPrefsReconcileStory";

const OFFICE_BG = "rgb(25, 28, 36)";
const MIDNIGHT_BG = "rgb(1, 2, 3)";

test.beforeEach(async ({ page }) => {
  // Fresh origin state each run: the localStorage cache + any token from a prior
  // guard must not bleed in. Set on the CT harness page BEFORE mount.
  await page.evaluate(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });
});

test("pre-auth: the cache carries the active id, a not-yet-loaded custom bundle paints the office base", async ({
  mount,
  page,
}) => {
  // Seed the cache to a custom bundle id and mount with NO token — /api/settings
  // is unreachable pre-auth, so the bundle is not loaded yet. The cache drives
  // the ACTIVE id, but a dangling custom id safely paints the office base until
  // the reconcile brings its colours.
  await page.evaluate(() => localStorage.setItem("oc.theme", "midnight"));
  const cmp = await mount(<DisplayPrefsReconcileStory />);
  const swatch = cmp.getByTestId("swatch");
  // The cached id is honored as the active theme (dual-layer cache read)…
  await expect(swatch).toHaveAttribute("data-theme-name", "midnight");
  // …but with no bundle yet, the paint is the office base, not the bundle colour.
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(OFFICE_BG);
  }).toPass();
});

test("login: the server bundle reconciles in and visually takes effect", async ({
  mount,
  page,
}) => {
  // Cache is empty → the pre-auth first paint is the office default. The mock
  // server holds the Midnight custom bundle as the active theme, adopted when
  // the login mints a token (oc-auth-login).
  const cmp = await mount(<DisplayPrefsReconcileStory serverBundle />);
  const swatch = cmp.getByTestId("swatch");
  // Pre-auth: office default is what paints.
  await expect(swatch).toHaveAttribute("data-theme-name", "office");
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(OFFICE_BG);
  }).toPass();

  // Log in → reconcile pulls the server's custom bundle set + active id and
  // applies its colours via setProperty.
  await cmp.getByTestId("login").click();
  await expect(swatch).toHaveAttribute("data-theme-name", "midnight");
  await expect(async () => {
    const bg = await swatch.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg).toBe(MIDNIGHT_BG);
  }).toPass();

  // …and the server value is written back to the cache so the NEXT pre-auth
  // first paint already carries the active id (the dual layer's whole point).
  const cached = await page.evaluate(() => localStorage.getItem("oc.theme"));
  expect(cached).toBe("midnight");
});
