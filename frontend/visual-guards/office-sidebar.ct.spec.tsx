// HOTSPOT 3 — 辦公室側欄不塌 / 不溢出.
//
// `.office` is `display:grid; grid-template-columns: 264px 1fr` — the roster
// rail is a FIXED 264px track. jsdom resolves no grid, so a regression that
// collapsed the rail, or let a long member name push content past the rail's
// right edge (min-width:0 / ellipsis rot), is invisible to the unit suite.
// These assertions measure the real grid in a real browser.
import { test, expect } from "@playwright/experimental-ct-react";
import { OfficeSidebarStory } from "./stories/OfficeSidebarStory";

test("desktop 1200: roster rail holds its fixed width and does not collapse", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  const rail = cmp.locator(".office__members");
  await expect(rail).toBeVisible();
  const railBox = await rail.boundingBox();
  expect(railBox, "rail box").not.toBeNull();
  // INVARIANT 1 — the rail is the ~264px grid track, not collapsed to ~0 and
  // not blown out to the whole page. Tolerance covers padding/border/rounding.
  expect(railBox!.width).toBeGreaterThan(230);
  expect(railBox!.width).toBeLessThan(300);
});

test("desktop 1200: a pathologically long member name does not overflow the rail", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  const rail = cmp.locator(".office__members");
  const railBox = await rail.boundingBox();
  const cards = cmp.locator(".member-card");
  const n = await cards.count();
  expect(n).toBeGreaterThan(0);
  // INVARIANT 2 — every roster card stays within the rail's right edge. A card
  // spilling past it is the "long name blew out the column" regression the
  // long-name fixture is built to trigger.
  for (let i = 0; i < n; i++) {
    const b = await cards.nth(i).boundingBox();
    expect(b, `card ${i} box`).not.toBeNull();
    expect(b!.x + b!.width, `card ${i} right edge <= rail right edge`).toBeLessThanOrEqual(
      railBox!.x + railBox!.width + 1
    );
  }
});

test("desktop 1200: the roster does not force a horizontal scrollbar on the page", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  await mount(<OfficeSidebarStory />);
  // INVARIANT 3 — content width never exceeds the viewport (no sideways scroll).
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth
  );
  expect(overflow).toBeLessThanOrEqual(1);
});

// T-66a8 sidebar tab switcher — the tab bar layout is a CSS-grid/flex property
// jsdom can't resolve (side-by-side tabs, the active underline's rendered
// border, the tabs + recruit button staying inside the fixed rail). These
// assertions measure it in a real browser.
test("desktop 1200: the two tabs sit side by side within the rail", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  const rail = cmp.locator(".office__members");
  const railBox = await rail.boundingBox();
  const staff = cmp.locator('[data-testid="office-tab-staff"]');
  const outsource = cmp.locator('[data-testid="office-tab-outsource"]');
  const sb = await staff.boundingBox();
  const ob = await outsource.boundingBox();
  expect(sb, "staff tab box").not.toBeNull();
  expect(ob, "outsource tab box").not.toBeNull();
  // Side by side (same row): the outsource tab starts at/after the staff tab's
  // right edge, and their tops line up.
  expect(ob!.x).toBeGreaterThanOrEqual(sb!.x + sb!.width - 1);
  expect(Math.abs(ob!.y - sb!.y)).toBeLessThanOrEqual(1);
  // Both stay inside the rail's right edge (badges/labels don't blow it out).
  expect(ob!.x + ob!.width).toBeLessThanOrEqual(railBox!.x + railBox!.width + 1);
});

test("desktop 1200: the selected tab paints a visible underline; the inactive one does not", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  // The story mounts with 正職 active. Its bottom border is a solid (non-zero,
  // non-transparent) rule; the inactive 外包 tab's is transparent.
  const activeBorder = await cmp
    .locator('[data-testid="office-tab-staff"]')
    .evaluate((el) => {
      const s = getComputedStyle(el);
      return { width: s.borderBottomWidth, color: s.borderBottomColor };
    });
  const inactiveColor = await cmp
    .locator('[data-testid="office-tab-outsource"]')
    .evaluate((el) => getComputedStyle(el).borderBottomColor);
  expect(parseFloat(activeBorder.width)).toBeGreaterThan(0);
  // A painted colour has a non-zero alpha; transparent resolves to alpha 0.
  expect(activeBorder.color).not.toMatch(/rgba\(.*,\s*0\)\s*$/);
  expect(inactiveColor).toMatch(/rgba\(.*,\s*0\)\s*$/);
});

// T-5557 owner visual tune — two properties jsdom can't see (a computed
// font-weight, and the rendered Y of two border-bottoms across two grid
// columns). Measured in a real browser.
test("desktop 1200: the tab label is not bold (owner: 不需要特別是粗體)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  // Both tab labels carry the app's standard title weight (600), NOT the 700
  // bold that read as an out-of-place font. Guard the whole family (<700) so a
  // regression back to bold — 700 or "bold" — trips it.
  const labels = cmp.locator(".office__tab-label");
  const n = await labels.count();
  expect(n).toBeGreaterThan(0);
  for (let i = 0; i < n; i++) {
    const w = await labels.nth(i).evaluate((el) => getComputedStyle(el).fontWeight);
    expect(parseInt(w, 10), `tab label ${i} font-weight not bold`).toBeLessThan(700);
  }
});

test("desktop 1200: the tab bar divider aligns with the chat header divider (owner: 底線對齊)", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  // The sidebar tab bar's baseline border-bottom and the right-column chat
  // header's border-bottom must sit at the same Y. Element rect.bottom is the
  // outer edge, i.e. below the 1px border — the divider's own line. Owner圈圖:
  // the two lines were visibly different heights; T-5557 pads the tab row down
  // to catch the taller (avatar-bearing) chat header. Chat header is the fixed
  // baseline.
  const tabsBottom = await cmp
    .locator(".office__tabs")
    .evaluate((el) => el.getBoundingClientRect().bottom);
  const headerBottom = await cmp
    .locator(".chat__header")
    .evaluate((el) => el.getBoundingClientRect().bottom);
  expect(Math.abs(tabsBottom - headerBottom), "two dividers' Y within 1px").toBeLessThanOrEqual(1);
});

test("desktop 1200: the recruit button sits at the rail bottom and does not overflow it", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1200, height: 800 });
  const cmp = await mount(<OfficeSidebarStory />);
  const rail = cmp.locator(".office__members");
  const railBox = await rail.boundingBox();
  const recruit = cmp.locator(".office__recruit");
  const rb = await recruit.boundingBox();
  expect(rb, "recruit button box").not.toBeNull();
  // Within the rail horizontally…
  expect(rb!.x).toBeGreaterThanOrEqual(railBox!.x - 1);
  expect(rb!.x + rb!.width).toBeLessThanOrEqual(railBox!.x + railBox!.width + 1);
  // …and in the rail's lower half (pinned below the switched list).
  expect(rb!.y).toBeGreaterThan(railBox!.y + railBox!.height / 2);
});
