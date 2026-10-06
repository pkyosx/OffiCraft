// T-081b round 6 guards — two contrast facts that only exist as COMPUTED
// colours (alpha compositing + color-mix), measured against real app CSS under
// every built-in theme AND a real light pack (see ThemeContrastStory):
//
//   ① 頂列 (topbar) input boundary. The field's fill is --color-bg on a bar
//      painted --color-topbar-bg — a pairing this ticket's three zone tokens
//      introduced, and one a light theme collapses to ~1.11:1. The field stays
//      findable because it carries a BORDER, so the border must stay clearly
//      distinguishable from the bar it sits on (≥3:1, the non-text threshold).
//      Delete the border and the field becomes an invisible rectangle.
//   ② .login__hint on the login card must clear WCAG AA (≥4.5:1) as normal text
//      in every theme. It used to be a hard-coded color-mix off --color-text,
//      which no light theme could lift past ~3.3:1.
//   ③ each built-in's main text-on-surface token pairs clear AA.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ThemeContrastStory } from "./stories/ThemeContrastStory";
import {
  BUILTIN_THEME_IDS,
  expectBuiltinApplied,
  selectBuiltinTheme,
} from "./builtinThemes";

type Rgba = { r: number; g: number; b: number; a: number };

function parseColor(s: string): Rgba {
  const rgb = s.match(/rgba?\(([^)]+)\)/i);
  if (rgb) {
    const p = rgb[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  const srgb = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (srgb) {
    const [chans, alpha] = srgb[1].split("/").map((x) => x.trim());
    const c = chans.split(/\s+/).map((x) => parseFloat(x));
    return {
      r: c[0] * 255,
      g: c[1] * 255,
      b: c[2] * 255,
      a: alpha === undefined ? 1 : parseFloat(alpha),
    };
  }
  throw new Error(`unparseable colour: ${s}`);
}

function over(fg: Rgba, bg: Rgba): Rgba {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  };
}

function relLuminance({ r, g, b }: Rgba): number {
  const lin = (v: number) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(fg: Rgba, bg: Rgba): number {
  const l1 = relLuminance(fg);
  const l2 = relLuminance(bg);
  const [hi, lo] = l1 >= l2 ? [l1, l2] : [l2, l1];
  return (hi + 0.05) / (lo + 0.05);
}

type Sample = {
  color: string;
  background: string;
  borderColor: string;
  borderWidth: string;
  borderStyle: string;
  layers: string[];
};

// Read an element's own colours plus every ancestor background, so a
// translucent zone (the topbar's rgba fill) folds down to the pixels painted.
async function sample(loc: import("@playwright/test").Locator): Promise<Sample> {
  return loc.evaluate((el) => {
    const cs = getComputedStyle(el);
    const layers: string[] = [];
    let node: HTMLElement | null = el.parentElement;
    while (node) {
      layers.push(getComputedStyle(node).backgroundColor);
      node = node.parentElement;
    }
    return {
      color: cs.color,
      background: cs.backgroundColor,
      borderColor: cs.borderTopColor,
      borderWidth: cs.borderTopWidth,
      borderStyle: cs.borderTopStyle,
      layers,
    };
  });
}

function foldZone(layers: string[]): Rgba {
  let bg: Rgba = { r: 255, g: 255, b: 255, a: 1 };
  for (let i = layers.length - 1; i >= 0; i--) {
    const layer = parseColor(layers[i]);
    if (layer.a === 0) continue;
    bg = over(layer, bg);
  }
  return bg;
}

const THEMES = [...BUILTIN_THEME_IDS, "light pack"] as const;
type Theme = (typeof THEMES)[number];

async function mountUnder(
  mount: import("@playwright/experimental-ct-react").ComponentFixtures["mount"],
  page: Page,
  theme: Theme
) {
  await page.setViewportSize({ width: 1280, height: 600 });
  if (theme !== "light pack") await selectBuiltinTheme(page, theme);
  const cmp = await mount(<ThemeContrastStory />);
  if (theme === "light pack") await cmp.getByTestId("apply-light").click();
  return cmp;
}

for (const theme of THEMES) {
  test(`${theme}: the topbar input is bounded against the bar it sits on`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountUnder(mount, page, theme);

    await cmp.getByRole("button", { name: "改名" }).click();
    const input = cmp.locator(".topbar .inline-edit__input");
    await expect(input).toBeVisible();

    const s = await sample(input);
    if (theme !== "light pack") await expectBuiltinApplied(page, theme);
    const zone = foldZone(s.layers);
    const fill = over(parseColor(s.background), zone);
    const border = over(parseColor(s.borderColor), fill);

    // A border that is not painted is not a boundary.
    expect(s.borderStyle).not.toBe("none");
    expect(parseFloat(s.borderWidth)).toBeGreaterThan(0);
    // The fill alone does NOT separate the field from the bar under a light
    // theme — this is the pairing the zone tokens introduced, recorded here so
    // the guard is honest about what it is protecting against.
    // The BOUNDARY is what must hold, in both directions.
    expect(contrast(border, zone)).toBeGreaterThanOrEqual(3);
    expect(contrast(border, fill)).toBeGreaterThanOrEqual(3);
  });

  test(`${theme}: the login hint clears WCAG AA (≥4.5:1)`, async ({ mount, page }) => {
    const cmp = await mountUnder(mount, page, theme);

    const hint = cmp.locator(".login__hint");
    const s = await sample(hint);
    if (theme !== "light pack") await expectBuiltinApplied(page, theme);
    const card = foldZone(s.layers);
    const fg = over(parseColor(s.color), card);
    const ratio = contrast(fg, card);
    expect(ratio).toBeGreaterThanOrEqual(4.5);
    // The built-in dark theme must not REGRESS from the 4.72 it shipped with.
    if (theme === "office") expect(ratio).toBeGreaterThanOrEqual(4.72);
  });
}

const TEXT_PAIRS: [fg: string, bg: string][] = [
  ...["--color-text", "--color-text-strong", "--color-text-muted", "--color-accent", "--color-danger"].flatMap(
    (fg) =>
      ["--color-card", "--color-main-bg", "--color-nav-bg", "--color-topbar-bg"].map(
        (bg) => [fg, bg] as [string, string]
      )
  ),
  ["--color-on-accent", "--color-accent"],
  ["--color-on-danger", "--color-danger-badge"],
  ["--color-on-indigo", "--color-indigo"],
  ["--color-warn-fg", "--color-warn-bg"],
];

// office's --color-border equals its card colour on purpose (its hairlines are
// overlay veils), so the 3:1 border floor is a light-theme promise only.
const BORDER_PAIRS: Record<string, [fg: string, bg: string][]> = {
  office: [],
  "office-light": [
    ["--color-border", "--color-card"],
    ["--color-border", "--color-main-bg"],
  ],
};

for (const theme of BUILTIN_THEME_IDS) {
  test(`${theme}: main text-on-surface pairs clear AA (≥4.5:1) and borders 3:1`, async ({
    mount,
    page,
  }) => {
    await mountUnder(mount, page, theme);
    const pairs = [...TEXT_PAIRS, ...BORDER_PAIRS[theme]];
    const resolved = await page.evaluate((tokens: string[]) => {
      const probe = document.createElement("div");
      document.body.appendChild(probe);
      const out: Record<string, string> = {};
      for (const tok of tokens) {
        probe.style.color = `var(${tok})`;
        out[tok] = getComputedStyle(probe).color;
      }
      probe.remove();
      return out;
    }, [...new Set(pairs.flat())]);
    await expectBuiltinApplied(page, theme);

    const failures: string[] = [];
    for (const [fg, bg] of pairs) {
      const floor = fg === "--color-border" ? 3 : 4.5;
      const base = parseColor(resolved[bg]);
      const ratio = contrast(over(parseColor(resolved[fg]), base), base);
      if (ratio < floor) failures.push(`${fg} on ${bg}: ${ratio.toFixed(2)}:1 < ${floor}`);
    }
    expect(failures).toEqual([]);
  });
}
