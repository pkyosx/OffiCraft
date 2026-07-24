// T-3738 visual guards for 設定 › 主題管理 (ThemeSettings list view):
//
//   ① the row TAGS (內建 / 自訂) clear WCAG AA (≥4.5:1), and the old 用詞 tag is
//      gone (owner: label a row as custom, don't spell out what it changed). The
//      fills resolve through color-mix(), so the shipped contrast is a
//      COMPUTED-COLOUR fact jsdom cannot see. We sample the rendered
//      foreground/background colour off each tag, composite the fill down to an
//      opaque colour, and compute the WCAG contrast ratio.
//   ② the built-in office row and a custom row line up their trailing action
//      column at 390 and 1280 — the built-in row carries the SAME three icon
//      buttons for alignment; its download is active (owner: 辦公室主題不用擋
//      下載) while edit/delete stay inert placeholders, so both rows flush their
//      action buttons to the same right edge.
//
// Both are proven against the REAL app CSS + real ancestor chain (.app__main),
// with a custom theme seeded the way production seeds it (see the story).
import { test, expect } from "@playwright/experimental-ct-react";
import { ThemeSettingsListStory } from "./stories/ThemeSettingsListStory";

// ── colour helpers (run in Node, on strings pulled from getComputedStyle) ──
type Rgba = { r: number; g: number; b: number; a: number };

function parseColor(s: string): Rgba {
  const rgb = s.match(/rgba?\(([^)]+)\)/i);
  if (rgb) {
    const parts = rgb[1].split(/[,/]/).map((x) => x.trim());
    return {
      r: parseFloat(parts[0]),
      g: parseFloat(parts[1]),
      b: parseFloat(parts[2]),
      a: parts[3] !== undefined ? parseFloat(parts[3]) : 1,
    };
  }
  // color(srgb r g b / a) — channels are 0..1 fractions.
  const srgb = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (srgb) {
    const [chans, alpha] = srgb[1].split("/").map((x) => x.trim());
    const c = chans.split(/\s+/).map((x) => parseFloat(x));
    return {
      r: c[0] * 255,
      g: c[1] * 255,
      b: c[2] * 255,
      a: alpha !== undefined ? parseFloat(alpha) : 1,
    };
  }
  throw new Error(`unparseable colour: ${s}`);
}

// Composite a (possibly translucent) colour over an opaque backdrop (src-over).
function over(fg: Rgba, bg: Rgba): Rgba {
  const a = fg.a;
  return {
    r: fg.r * a + bg.r * (1 - a),
    g: fg.g * a + bg.g * (1 - a),
    b: fg.b * a + bg.b * (1 - a),
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

// Pull the rendered foreground colour and the effective (composited-to-opaque)
// background colour of a tag, walking up the ancestor chain to resolve any
// translucency in the fill — so the ratio reflects the pixels actually painted.
async function sampleTagColours(cmp: import("@playwright/test").Locator) {
  const { fgStr, bgLayers } = await cmp.evaluate((el) => {
    const c = getComputedStyle(el).color;
    const layers: string[] = [];
    let node: HTMLElement | null = el as HTMLElement;
    while (node) {
      layers.push(getComputedStyle(node).backgroundColor);
      node = node.parentElement;
    }
    return { fgStr: c, bgLayers: layers };
  });
  const fg = parseColor(fgStr);
  // Fold the background layers from the outermost opaque one inward.
  let bg: Rgba = { r: 25, g: 28, b: 36, a: 1 }; // office --color-bg backstop
  for (let i = bgLayers.length - 1; i >= 0; i--) {
    const layer = parseColor(bgLayers[i]);
    if (layer.a === 0) continue;
    bg = over(layer, bg);
  }
  return { fg, bg, ratio: contrast(fg, bg) };
}

async function mountSeeded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<ThemeSettingsListStory />);
  await cmp.getByTestId("seed").click();
  // The reconcile adds the custom row; wait for both tags to exist.
  await expect(cmp.locator(".ts-tag")).toHaveCount(2);
  return cmp;
}

for (const width of [390, 1280]) {
  test(`width ${width}: 內建 built-in tag clears WCAG AA (≥4.5:1)`, async ({ mount, page }) => {
    const cmp = await mountSeeded(mount, page, width);
    // The built-in tag is the .ts-tag that is NOT the custom variant.
    const tag = cmp.locator(".ts-tag:not(.ts-tag--custom)");
    await expect(tag).toHaveCount(1);
    const { ratio } = await sampleTagColours(tag);
    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });

  test(`width ${width}: 自訂 custom tag exists and clears WCAG AA (≥4.5:1)`, async ({ mount, page }) => {
    const cmp = await mountSeeded(mount, page, width);
    const tag = cmp.locator(".ts-tag--custom");
    await expect(tag).toHaveCount(1);
    const { ratio } = await sampleTagColours(tag);
    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });

  test(`width ${width}: the 用詞 wording badge is no longer rendered`, async ({ mount, page }) => {
    // The seeded custom theme carries a wording overlay, yet no 用詞 badge shows —
    // the badge was removed (the wording MECHANISM stays; only the label is gone).
    const cmp = await mountSeeded(mount, page, width);
    await expect(cmp.locator(".ts-tag--wording")).toHaveCount(0);
  });

  test(`width ${width}: built-in and custom rows align their action column`, async ({ mount, page }) => {
    const cmp = await mountSeeded(mount, page, width);
    const rows = cmp.locator(".ts-list > .ts-row");
    await expect(rows).toHaveCount(2);

    const rects = await rows.evaluateAll((els) =>
      els.map((row) =>
        Array.from(row.querySelectorAll(".ts-icon-btn")).map((b) => {
          const r = b.getBoundingClientRect();
          return { left: r.left, right: r.right };
        })
      )
    );
    const [builtin, custom] = rects;

    // The built-in row carries the SAME count of action buttons as the custom
    // row — this is what makes the columns line up.
    expect(builtin.length).toBe(3);
    expect(custom.length).toBe(3);

    // Each of the three columns lines up left AND right across the two rows.
    for (let i = 0; i < 3; i++) {
      expect(Math.abs(builtin[i].left - custom[i].left)).toBeLessThanOrEqual(1);
      expect(Math.abs(builtin[i].right - custom[i].right)).toBeLessThanOrEqual(1);
    }

    // …and both rows flush their trailing button to the same right edge.
    const builtinRight = builtin[builtin.length - 1].right;
    const customRight = custom[custom.length - 1].right;
    expect(Math.abs(builtinRight - customRight)).toBeLessThanOrEqual(1);
  });

  test(`width ${width}: office download is active; edit/delete stay inert placeholders`, async ({ mount, page }) => {
    const cmp = await mountSeeded(mount, page, width);
    const builtinBtns = cmp.locator(".ts-list > .ts-row").first().locator(".ts-icon-btn");
    await expect(builtinBtns).toHaveCount(3);
    // The download icon is an active export now (owner: 辦公室主題不用擋下載)…
    await expect(builtinBtns.nth(0)).toBeEnabled();
    // …edit/delete remain inert placeholders that only keep the row aligned.
    await expect(builtinBtns.nth(1)).toBeDisabled();
    await expect(builtinBtns.nth(2)).toBeDisabled();
    // The custom row's buttons stay enabled (the placeholders must not have
    // disabled the real actions).
    const customBtns = cmp.locator(".ts-list > .ts-row").nth(1).locator(".ts-icon-btn");
    await expect(customBtns).toHaveCount(3);
    for (let i = 0; i < 3; i++) {
      await expect(customBtns.nth(i)).toBeEnabled();
    }
  });
}
