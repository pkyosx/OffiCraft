// HOTSPOT — the 任務 count must stay NEUTRAL and stay READABLE (T-2658).
//
// Why a real browser: both halves of this claim are computed colour.
//   * The fill is `color-mix(in srgb, var(--color-overlay) 13%, transparent)`,
//     which only becomes a colour once a theme's tokens are resolved and the
//     layers behind it (the segmented frame, the nav band, the page, and on an
//     active tab the indigo fill) are composited. jsdom resolves none of that
//     and reports every colour as the literal string, so "is it still red" and
//     "does it clear AA" are both structurally undecidable there — a re-merge
//     back to --color-danger-badge stays green across the whole vitest suite.
//   * The light theme is not a class or a media query: a theme PACK sets its
//     tokens on documentElement (frontend/src/i18n/index.tsx L228-229). The
//     colours that break are the ones a pack re-values, so the pack has to be
//     applied the same way here.
//
// What it pins, per theme × per tab state:
//   1. NOT RED — the count's own fill is not the danger fill the two alert
//      badges carry. This is the requirement in one line: red in this nav means
//      "this one wants you", and an open-task total does not.
//   2. STILL RED — 請示 keeps exactly that danger fill. Neutralising the task
//      count by draining the shared class would satisfy (1) and quietly take
//      the alert colour off the badge that is supposed to have it.
//   3. AA — the 11px digits clear 4.5:1 against what they actually sit on,
//      including the active tab, where the pill is NOT on the page colour.
//   4. GEOMETRY — the count keeps the 18px pill box, and at 390 it does not
//      push the strip wider than the old red pill did (that width is what
//      nav-tabs-narrow.ct.spec.tsx measures the 使用說明 label against).
//
// MUTANT: set `.nav-tab__count { background: var(--color-danger-badge) }` → (1)
// goes red in both themes and no other guard moves. The same mutation is also
// caught statically, by the sabotage cases in scripts/check-token-roles.test.ts.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { NavCountsStory } from "./stories/NavCountsStory";

/** The smurf-village pack (docs/T-081b-evidence/shots-pack/) — a REAL light
 *  theme the owner can import, trimmed to the tokens this strip reads. It
 *  deliberately does NOT carry a token invented for the count: a pack only
 *  carries what it lists, which is exactly why the count is derived from
 *  --color-overlay / --color-text instead of a new slot. */
const LIGHT_PACK: Record<string, string> = {
  "--color-bg": "#c2d492",
  "--color-nav-bg": "rgba(215, 207, 164, 0.8)",
  "--color-topbar-bg": "rgba(215, 207, 164, 0.8)",
  "--color-main-bg": "rgba(233, 228, 199, 0.75)",
  "--color-card": "#fdfbf1",
  "--color-border": "#b9b087",
  "--color-overlay": "#241f0d",
  "--color-text": "#33301f",
  "--color-text-strong": "#1e1c10",
  "--color-text-muted": "#403d2c",
  "--color-indigo": "#dde5c6",
  "--color-danger-badge": "#a8342b",
  "--color-on-danger": "#ffffff",
  "--color-accent": "#4f7a3a",
};

const AA = 4.5;

/** Colour maths in the page. Same recipe as theme-contrast.ct.spec.tsx, plus
 *  the `color(srgb …)` form — that is what Chromium computes a color-mix() to,
 *  and a parser that only knows rgba() reads it as "no background at all",
 *  which silently turns the measurement into one taken on the wrong surface. */
const MEASURE = `(sel) => {
  const parse = (c) => {
    let m = c.match(/rgba?\\(([^)]+)\\)/);
    if (m) {
      const p = m[1].split(/[,\\s\\/]+/).filter(Boolean).map(Number);
      return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
    }
    m = c.match(/color\\(srgb\\s+([^)]+)\\)/);
    if (m) {
      const p = m[1].split(/[\\s\\/]+/).filter(Boolean).map(Number);
      return { r: p[0] * 255, g: p[1] * 255, b: p[2] * 255, a: p.length > 3 ? p[3] : 1 };
    }
    return null;
  };
  const over = (f, b) => ({
    r: f.r * f.a + b.r * (1 - f.a),
    g: f.g * f.a + b.g * (1 - f.a),
    b: f.b * f.a + b.b * (1 - f.a),
    a: 1,
  });
  const lum = (c) => {
    const f = (v) => {
      const s = v / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
  };
  const ratio = (a, b) => {
    const l1 = lum(a), l2 = lum(b);
    return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
  };
  // Fold every ancestor background down onto opaque white: that is the colour
  // the digits actually sit on, which on an active tab is NOT the page.
  const bgOf = (el) => {
    const layers = [];
    for (let n = el; n; n = n.parentElement) {
      const bg = parse(getComputedStyle(n).backgroundColor);
      if (bg && bg.a > 0) layers.push(bg);
    }
    let base = { r: 255, g: 255, b: 255, a: 1 };
    for (let i = layers.length - 1; i >= 0; i--) base = over(layers[i], base);
    return base;
  };
  const el = document.querySelector(sel);
  const cs = getComputedStyle(el);
  const bg = bgOf(el);
  const own = parse(cs.backgroundColor) || { r: 0, g: 0, b: 0, a: 0 };
  const rgb = (c) =>
    Math.round(c.r) + ',' + Math.round(c.g) + ',' + Math.round(c.b);
  return {
    text: el.textContent,
    fontSize: cs.fontSize,
    height: Math.round(el.getBoundingClientRect().height),
    // The fill composited over what is behind it — comparing DECLARED strings
    // would call a 100%-opaque color-mix of the danger token "not red".
    fill: own.a > 0 ? rgb(over(own, bgOf(el.parentElement))) : null,
    fillAlpha: own.a,
    textContrast: +ratio(over(parse(cs.color), bg), bg).toFixed(2),
  };
}`;

type Measured = {
  text: string;
  fontSize: string;
  height: number;
  fill: string | null;
  fillAlpha: number;
  textContrast: number;
};

const measure = (page: Page, sel: string) =>
  page.evaluate(
    new Function("sel", `return (${MEASURE})(sel)`) as never,
    sel
  ) as Promise<Measured>;

async function applyTheme(page: Page, light: boolean): Promise<void> {
  await page.evaluate(
    ([pack, on]) => {
      for (const [k, v] of Object.entries(pack as Record<string, string>)) {
        if (on) document.documentElement.style.setProperty(k, v);
        else document.documentElement.style.removeProperty(k);
      }
    },
    [LIGHT_PACK, light] as const
  );
  // .nav-tab transitions colour and background over 0.15s. Measuring mid-flight
  // reads a blend that is on nobody's screen — and it is a BLEND, so it can be
  // either side of a threshold depending on machine speed.
  await page.waitForTimeout(400);
}

async function selectTab(cmp: Locator, page: Page, label: string): Promise<void> {
  await cmp.locator(".nav-tab", { hasText: label }).first().click();
  await page.waitForTimeout(400);
}

for (const theme of ["built-in dark", "light pack"] as const) {
  const light = theme === "light pack";

  test(`${theme}: the 任務 count is not the alert pill, and 請示 still is`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const cmp = await mount(<NavCountsStory />);
    await applyTheme(page, light);

    // 辦公室 is the landing tab, so 任務 starts inactive; then select it, because
    // the active tab is the one case where the pill does not sit on the page.
    for (const [state, label] of [
      ["inactive", "辦公室"],
      ["active", "任務"],
    ] as const) {
      await selectTab(cmp, page, label);

      const tasks = await measure(page, '[data-testid="tasks-badge"]');
      const replies = await measure(page, '[data-testid="replies-badge"]');

      // (2) first: if 請示 lost its red, the comparison in (1) is vacuous.
      const danger = light ? "168,52,43" : "186,89,83";
      expect(
        replies.fill,
        `${theme}/${state}: 請示 must keep the danger fill`
      ).toBe(danger);

      // (1) the task count is not wearing that fill.
      expect(
        tasks.fill,
        `${theme}/${state}: the 任務 count must not be the alert colour`
      ).not.toBe(danger);
      // …and its fill is a VEIL, inside a band on both sides. Above the band an
      // opaque neutral still reads as a status chip rather than a count; below
      // it the pill fades to nothing and the owner silently gets the bare-number
      // look he was shown and did NOT pick (rc-b3ceb8820fa8). `> 0` alone would
      // let a 0.5% "let's make it quieter" tweak deliver exactly that.
      expect(tasks.fillAlpha).toBeGreaterThanOrEqual(0.08);
      expect(tasks.fillAlpha).toBeLessThan(0.5);

      // (3) the digits clear AA on whatever they ended up sitting on.
      expect(tasks.fontSize).toBe("11px");
      expect(
        tasks.textContrast,
        `${theme}/${state}: the count reads ${tasks.textContrast}:1`
      ).toBeGreaterThanOrEqual(AA);

      // (4) same pill box as before, so nothing below or beside it moves.
      expect(tasks.text).toBe("7");
      expect(tasks.height).toBe(18);
    }
  });
}

for (const width of [390, 768, 1440] as const) {
  test(`at ${width} the count does not widen the tab strip`, async ({
    mount,
    page,
  }) => {
    // The neutral count keeps the red pill's geometry on purpose: the 使用說明
    // label's visibility at phone width is measured against this strip's
    // scrollWidth in nav-tabs-narrow.ct.spec.tsx, and a wider count would eat
    // into it from four tabs away.
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<NavCountsStory />);
    const strip = cmp.locator(".nav-tabs__seg");

    const box = await strip.evaluate((seg: HTMLElement) => {
      const count = seg.querySelector(
        '[data-testid="tasks-badge"]'
      ) as HTMLElement;
      const c = count.getBoundingClientRect();
      return {
        countWidth: Math.round(c.width),
        countHeight: Math.round(c.height),
        // The count must be laid out INSIDE the scrollable content, not
        // spilling out of the row it belongs to.
        overflowsRow: c.height > seg.getBoundingClientRect().height,
        // Nothing here may push the PAGE sideways at any width.
        docOverflow:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });

    // 18px min-width + 5px padding each side around one digit — the same box
    // the red pill occupies. A jump here is a layout change, not a colour one.
    expect(box.countWidth).toBe(18);
    expect(box.countHeight).toBe(18);
    expect(box.overflowsRow).toBe(false);
    expect(box.docOverflow).toBe(0);
  });
}
