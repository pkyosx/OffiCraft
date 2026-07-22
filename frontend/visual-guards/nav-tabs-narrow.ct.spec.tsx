// HOTSPOT — the 使用說明 nav tab must be FINDABLE on a phone (T-68f1 · RC3-3).
//
// This pack's whole purpose is that a first-time owner can find the product
// guide. It moved 使用說明 out of Settings and made it the FIFTH top-level tab,
// which put it last in a strip that already overflows on a phone — and the
// owner reads the console mostly on a phone (the same premise
// worker-detail-header-label.ct.spec.tsx:16 is built on).
//
// Why a real browser: the strip's overflow is pure layout. jsdom resolves no
// flex, evaluates no @media and reports every width as 0, so "is the fifth tab
// legible at 390" is structurally undecidable there — a padding change that
// pushes the whole label offscreen stays green across the entire vitest suite.
// Measured before the fix (real Chromium, real App): at 390 the strip was
// scrollWidth 479 / clientWidth 390 and the tab occupied [352..452] — the 38px
// on screen were all icon and padding, so NOT ONE of the four label characters
// was visible. The disclosure sentence that tells a phone reader to swipe the
// strip lives in docs/guide/mobile.md, i.e. INSIDE the tab nobody can find.
//
// It deliberately asserts on the LABEL's box, not the button's: a button whose
// visible sliver is entirely icon is exactly the state that shipped, and
// exactly the state a button-level assertion calls a pass.
//
// MUTANT (verified red, see ow52-68f1-fixround4-impl.md): revert the @media
// (max-width: 720px) paddings in chrome.css (`.nav-tabs` 12px → 22px and
// `.nav-tab` `0 8px` → `0 12px`) → "the 使用說明 label must be legible at 390"
// goes red on visibleLabel, and no other guard moves.
import { test, expect, type Locator } from "@playwright/experimental-ct-react";
import { NavTabsNarrowStory } from "./stories/NavTabsNarrowStory";

/** Geometry of the LAST nav tab (使用說明) relative to the scroll viewport. */
async function measure(strip: Locator) {
  return await strip.evaluate((strip: HTMLElement) => {
    const tabs = Array.from(
      strip.querySelectorAll(".nav-tab")
    ) as HTMLElement[];
    const last = tabs[tabs.length - 1];
    const label = last.querySelector("span") as HTMLElement;
    const view = strip.getBoundingClientRect();
    const t = last.getBoundingClientRect();
    const l = label.getBoundingClientRect();
    const clip = (r: DOMRect) =>
      Math.max(0, Math.min(r.right, view.right) - Math.max(r.left, view.left));
    return {
      tabCount: tabs.length,
      labelText: (label.textContent ?? "").trim(),
      scrollW: strip.scrollWidth,
      clientW: strip.clientWidth,
      tabLeft: Math.round(t.left),
      tabRight: Math.round(t.right),
      tabWidth: Math.round(t.width),
      labelLeft: Math.round(l.left),
      labelRight: Math.round(l.right),
      labelWidth: Math.round(l.width),
      visibleTab: Math.round(clip(t)),
      visibleLabel: Math.round(clip(l)),
    };
  });
}

for (const width of [320, 390, 414]) {
  test(`nav strip geometry @${width}`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 844 });
    const cmp = await mount(<NavTabsNarrowStory />);
    await expect(cmp.locator(".nav-tabs .nav-tab").nth(4)).toBeVisible();
    const m = await measure(cmp.locator(".nav-tabs"));
    // Printed verbatim so the impl report can quote a measurement rather than
    // an arithmetic claim about paddings.
    console.log(`@${width} ` + JSON.stringify(m));

    expect(m.tabCount, "the strip must carry all five top-level tabs").toBe(5);
    expect(m.labelText, "the last tab is the guide tab").toBe("使用說明");
    expect(m.clientW).toBe(width);

    if (width >= 390) {
      // The bar this pack has to clear: enough of the LABEL on screen to read
      // as words. One CJK glyph at 14px is ~13.5px, so 36px is between two and
      // three characters. Measured: 3px before the fix, 49px of a 54px label
      // after — the threshold sits far above the broken state and 13px below
      // the fixed one, so ordinary font-metric drift cannot flip it either way.
      expect(
        m.visibleLabel,
        `the 使用說明 label must be legible at ${width}, not just its icon`
      ).toBeGreaterThanOrEqual(36);
    }
    if (width >= 414) {
      expect(
        m.visibleTab,
        "at 414 the guide tab must be fully on screen"
      ).toBe(m.tabWidth);
    }
  });
}
