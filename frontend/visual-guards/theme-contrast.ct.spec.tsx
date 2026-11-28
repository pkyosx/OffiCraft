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
//   ④ the 任務 nav tab's red / amber due warning triangle stands out (≥3:1, the non-text
//      threshold) from the tab it sits on, idle and active.
//   ⑥ the line under the 狀態 filter's group rows is visible on its dropdown
//      (≥2.5:1: a hairline, not a control; the border token measures 1:1 in office
//      and an overlay veil about 1.3:1 in office-light).
//   ⑤ the task card's red overdue / orange due-soon 到期日 date clears AA
//      (≥4.5:1) as text on its chip, and so does the time left beside it, in
//      those tones and in the muted grey, on the card.
//   ⑦ every workflow step's title clears AA on its status-tinted box, the
//      faded 已取代 row included (its opacity and greyscale applied), and so
//      does the 等待中 reason label on the 等待外部 box — in the built-ins and
//      in every shipped theme file.
//   ⑧ the six step status icons, in every theme: each clears 3:1 on the card;
//      the four live ones (進行中、等我回覆、等待外部、完成) carry a visible hue
//      (OKLCH chroma ≥ 0.06), sit ≥ 30° of hue apart from each other and stand
//      off the title ink (OKLab ΔE ≥ 0.2 from --color-text-strong — a dark text
//      token reads as a black icon at any contrast); all six differ pairwise
//      by OKLab ΔE ≥ 0.04.
//   ⑨ a theme that does not set the --color-step-* tokens still paints each
//      icon in the colour its own pack gives the token that status follows.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ThemeContrastStory } from "./stories/ThemeContrastStory";
import { TaskCardDueStory } from "./stories/TaskCardDueStory";
import { TaskCardSectionsStory } from "./stories/TaskCardSectionsStory";
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

for (const theme of BUILTIN_THEME_IDS) {
  test(`${theme}: the 任務 nav due warning clears 3:1 against an idle and an active tab`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountUnder(mount, page, theme);
    const ratios: Record<string, string> = {};
    for (const tone of ["overdue", "soon"]) {
      for (const state of ["idle", "active"]) {
        const s = await sample(cmp.getByTestId(`due-warning-${tone}-${state}`));
        const zone = foldZone(s.layers);
        ratios[`${tone}/${state}`] = contrast(over(parseColor(s.color), zone), zone).toFixed(2);
      }
    }
    await expectBuiltinApplied(page, theme);
    console.log(`${theme} 任務 due warning contrast: ${JSON.stringify(ratios)}`);
    for (const [which, ratio] of Object.entries(ratios)) {
      expect(Number(ratio), `${which}: ${ratio}:1`).toBeGreaterThanOrEqual(3);
    }
  });
}

for (const theme of BUILTIN_THEME_IDS) {
  test(`${theme}: the task card's red overdue and orange due-soon date, and the time left beside it, clear AA (≥4.5:1)`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 600 });
    await selectBuiltinTheme(page, theme);
    const now = Math.floor(Date.now() / 1000);
    const cmp = await mount(<TaskCardDueStory dueTs={now - 3600} />);
    const ratios: Record<string, string> = {};
    for (const [tone, dueTs] of [
      ["overdue", now - 3600],
      ["soon", now + 3600],
      ["later", now + 30 * 86400],
    ] as const) {
      await cmp.update(<TaskCardDueStory dueTs={dueTs} />);
      for (const [part, loc] of [
        ["date", cmp.getByTestId("task-due")],
        ["left", cmp.getByTestId("task-due-remaining")],
      ] as const) {
        if (tone === "later" && part === "date") continue;
        await expect(loc).toBeVisible();
        const s = await sample(loc);
        const fill = foldZone([s.background, ...s.layers]);
        ratios[`${tone} ${part}`] = contrast(over(parseColor(s.color), fill), fill).toFixed(2);
      }
    }
    await expectBuiltinApplied(page, theme);
    console.log(`${theme} task card due date contrast: ${JSON.stringify(ratios)}`);
    for (const [which, ratio] of Object.entries(ratios)) {
      expect(Number(ratio), `${which}: ${ratio}:1`).toBeGreaterThanOrEqual(4.5);
    }
  });
}

for (const theme of THEMES) {
  test(`${theme}: the line under the 狀態 filter's group rows shows on its dropdown`, async ({
    mount,
    page,
  }) => {
    await mountUnder(mount, page, theme);
    const s = await sample(page.getByTestId("ms-group-sep"));
    const pop = foldZone(s.layers);
    const line = over(parseColor(s.background), pop);
    const ratio = contrast(line, pop);
    console.log(`${theme} status filter group line contrast: ${ratio.toFixed(2)}`);
    expect(ratio).toBeGreaterThanOrEqual(2.5);
  });
}

function grayscale({ r, g, b }: Rgba): Rgba {
  const y = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  return { r: y, g: y, b: y, a: 1 };
}

function mix(top: Rgba, under: Rgba, opacity: number): Rgba {
  return over({ ...top, a: opacity }, under);
}

/** A text colour on the step box it sits in, as painted: the box's tint over
 * the card, then the box's own filter and opacity over what is behind it. */
async function stepTextContrast(loc: import("@playwright/test").Locator): Promise<number> {
  const s = await loc.evaluate((el) => {
    const step = el.closest('[data-testid="task-step"]') as HTMLElement;
    const inside: string[] = [];
    for (let n: HTMLElement | null = el as HTMLElement; n; n = n.parentElement) {
      inside.push(getComputedStyle(n).backgroundColor);
      if (n === step) break;
    }
    const outside: string[] = [];
    for (let n = step.parentElement; n; n = n.parentElement) outside.push(getComputedStyle(n).backgroundColor);
    const cs = getComputedStyle(step);
    return { color: getComputedStyle(el).color, inside, outside, opacity: parseFloat(cs.opacity), filter: cs.filter };
  });
  const behind = foldZone(s.outside);
  let box = behind;
  for (let i = s.inside.length - 1; i >= 0; i--) box = over(parseColor(s.inside[i]), box);
  let fg = over(parseColor(s.color), box);
  if (s.filter.includes("grayscale(1)")) {
    fg = grayscale(fg);
    box = grayscale(box);
  }
  return contrast(mix(fg, behind, s.opacity), mix(box, behind, s.opacity));
}

const THEMES_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../../themes");
const BUILTIN_FILES = new Set(BUILTIN_THEME_IDS.map((id) => `${id}.theme.json`));
const OFFICE_COLOURS: Record<string, string> = JSON.parse(
  fs.readFileSync(path.join(THEMES_DIR, "office.theme.json"), "utf8")
).colors;
const PACKS: { id: string; colors: Record<string, string> }[] = fs
  .readdirSync(THEMES_DIR)
  .filter((f) => f.endsWith(".theme.json") && !BUILTIN_FILES.has(f))
  .sort()
  .map((f) => JSON.parse(fs.readFileSync(path.join(THEMES_DIR, f), "utf8")));

/** Paint a pack the way applyThemeToRoot does: each colour as an inline custom
 * property on <html>, over the office :root. */
async function applyPack(page: Page, colors: Record<string, string>) {
  await page.evaluate((c) => {
    for (const [k, v] of Object.entries(c)) document.documentElement.style.setProperty(k, v);
  }, colors);
}

type StepTheme = { kind: "builtin"; id: (typeof BUILTIN_THEME_IDS)[number] } | { kind: "pack"; id: string; colors: Record<string, string> };
const STEP_THEMES: StepTheme[] = [
  ...BUILTIN_THEME_IDS.map((id) => ({ kind: "builtin" as const, id })),
  ...PACKS.map((p) => ({ kind: "pack" as const, id: p.id, colors: p.colors })),
];

async function mountAllSteps(
  mount: import("@playwright/experimental-ct-react").ComponentFixtures["mount"],
  page: Page,
  theme: StepTheme
) {
  await page.setViewportSize({ width: 1280, height: 1400 });
  if (theme.kind === "builtin") await selectBuiltinTheme(page, theme.id);
  else await applyPack(page, theme.colors);
  const cmp = await mount(<TaskCardSectionsStory description="short" steps="all" />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.getByTestId("task-step")).toHaveCount(9);
  if (theme.kind === "builtin") await expectBuiltinApplied(page, theme.id);
  else
    expect(
      await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue("--color-card").trim()),
      `--color-card at capture time — is ${theme.id} really applied?`
    ).toBe(theme.colors["--color-card"]);
  return cmp;
}

for (const theme of STEP_THEMES) {
  test(`${theme.id}: every step title clears AA (≥4.5:1) on its status-tinted box`, async ({ mount, page }) => {
    const cmp = await mountAllSteps(mount, page, theme);
    const steps = cmp.getByTestId("task-step");
    const ratios: Record<string, string> = {};
    for (let i = 0; i < 9; i++) {
      const step = steps.nth(i);
      const id = (await step.getAttribute("data-step-id"))!;
      ratios[id] = (await stepTextContrast(step.locator(".task-step__name"))).toFixed(2);
    }
    ratios["s-external reason"] = (
      await stepTextContrast(cmp.getByTestId("step-waiting-reason").locator(".task-step__waiting-label"))
    ).toFixed(2);
    console.log(`${theme.id} step title contrast: ${JSON.stringify(ratios)}`);
    for (const [which, ratio] of Object.entries(ratios)) {
      expect.soft(Number(ratio), `${which}: ${ratio}:1`).toBeGreaterThanOrEqual(4.5);
    }
  });
}

function oklab({ r, g, b }: Rgba): [number, number, number] {
  const lin = (v: number) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  const [R, G, B] = [lin(r), lin(g), lin(b)];
  const l = Math.cbrt(0.4122214708 * R + 0.5363325363 * G + 0.0514459929 * B);
  const m = Math.cbrt(0.2119034982 * R + 0.6806995451 * G + 0.1073969566 * B);
  const s = Math.cbrt(0.0883024619 * R + 0.2817188376 * G + 0.6299787005 * B);
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ];
}
const deltaE = (x: Rgba, y: Rgba) => {
  const [p, q] = [oklab(x), oklab(y)];
  return Math.hypot(p[0] - q[0], p[1] - q[1], p[2] - q[2]);
};
const chroma = (x: Rgba) => Math.hypot(oklab(x)[1], oklab(x)[2]);
const hue = (x: Rgba) => ((Math.atan2(oklab(x)[2], oklab(x)[1]) * 180) / Math.PI + 360) % 360;
const hueGap = (x: Rgba, y: Rgba) => {
  const d = Math.abs(hue(x) - hue(y));
  return Math.min(d, 360 - d);
};

const STEP_STATUSES = ["pending", "in_progress", "waiting_owner", "waiting_external", "done", "superseded"] as const;
const LIVE_STATUSES = new Set(["in_progress", "waiting_owner", "waiting_external", "done"]);

/** Each status icon as painted, over the card disc the marker draws under it. */
async function markerColours(page: Page) {
  const out: Record<string, { icon: Rgba; card: Rgba }> = {};
  for (const status of STEP_STATUSES) {
    const s = await sample(page.locator(`.task-timeline__marker--${status}`).first());
    const card = foldZone([s.background, ...s.layers]);
    out[status] = { icon: over(parseColor(s.color), card), card };
  }
  return out;
}

async function resolveToken(page: Page, tok: string): Promise<string> {
  return page.evaluate((t) => {
    const probe = document.createElement("div");
    probe.style.color = `var(${t})`;
    document.body.appendChild(probe);
    const c = getComputedStyle(probe).color;
    probe.remove();
    return c;
  }, tok);
}

for (const theme of STEP_THEMES) {
  test(`${theme.id}: the six step status icons clear 3:1 on the card and read as six different colours`, async ({
    mount,
    page,
  }) => {
    await mountAllSteps(mount, page, theme);
    const m = await markerColours(page);
    const ink = parseColor(await resolveToken(page, "--color-text-strong"));
    const failures: string[] = [];
    const report: Record<string, string> = {};
    for (const s of STEP_STATUSES) {
      const { icon, card } = m[s];
      const ratio = contrast(icon, card);
      report[s] = `${ratio.toFixed(2)}:1 C${chroma(icon).toFixed(3)} h${hue(icon).toFixed(0)}`;
      if (ratio < 3) failures.push(`${s} on card ${ratio.toFixed(2)}:1 < 3`);
      if (!LIVE_STATUSES.has(s)) continue;
      if (chroma(icon) < 0.06) failures.push(`${s} chroma ${chroma(icon).toFixed(3)} < 0.06`);
      if (deltaE(icon, ink) < 0.2) failures.push(`${s} ΔE ${deltaE(icon, ink).toFixed(3)} from title ink < 0.2`);
    }
    for (let i = 0; i < STEP_STATUSES.length; i++) {
      for (let j = i + 1; j < STEP_STATUSES.length; j++) {
        const [a, b] = [STEP_STATUSES[i], STEP_STATUSES[j]];
        const d = deltaE(m[a].icon, m[b].icon);
        if (d < 0.04) failures.push(`${a}/${b} ΔE ${d.toFixed(3)} < 0.04`);
        if (LIVE_STATUSES.has(a) && LIVE_STATUSES.has(b)) {
          const gap = hueGap(m[a].icon, m[b].icon);
          if (gap < 30) failures.push(`${a}/${b} hue ${gap.toFixed(0)}° apart < 30°`);
        }
      }
    }
    console.log(`${theme.id} step icons: ${JSON.stringify(report)}`);
    expect(failures).toEqual([]);
  });
}

const STEP_TOKEN_DEFAULTS: Record<(typeof STEP_STATUSES)[number], [step: string, follows: string]> = {
  pending: ["--color-step-pending", "--color-text-muted"],
  in_progress: ["--color-step-progress", "--color-step-progress"],
  waiting_owner: ["--color-step-waiting-owner", "--color-dot-waking"],
  waiting_external: ["--color-step-waiting-external", "--color-wait-external"],
  done: ["--color-step-done", "--color-success"],
  superseded: ["--color-step-superseded", "--color-status-duplicated"],
};

test("a shipped theme that leaves the step tokens out paints each step icon in its own followed colour", async ({
  mount,
  page,
}) => {
  const pack = PACKS.find((p) => p.id === "national-day-2026")!;
  const newTokens = Object.values(STEP_TOKEN_DEFAULTS)
    .map(([step]) => step)
    .filter((t) => t !== "--color-step-progress");
  expect(newTokens.filter((t) => t in pack.colors), "precondition: the pack omits the step tokens").toEqual([]);
  await mountAllSteps(mount, page, { kind: "pack", id: pack.id, colors: pack.colors });
  for (const status of STEP_STATUSES) {
    const follows = STEP_TOKEN_DEFAULTS[status][1];
    expect(pack.colors[follows], `precondition: the pack sets ${follows}`).toBeTruthy();
    const marker = page.locator(`.task-timeline__marker--${status}`).first();
    const painted = await marker.evaluate((el) => getComputedStyle(el).color);
    expect(painted, `${status} icon follows the pack's ${follows}`).toBe(await resolveToken(page, follows));
    expect(painted, `${status} icon is the pack's colour, not office's`).not.toBe(
      await page.evaluate((v) => {
        const probe = document.createElement("div");
        probe.style.color = v;
        document.body.appendChild(probe);
        const c = getComputedStyle(probe).color;
        probe.remove();
        return c;
      }, OFFICE_COLOURS[follows])
    );
  }
});
