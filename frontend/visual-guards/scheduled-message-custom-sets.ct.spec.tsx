// HOTSPOT — T-49e7 定期訊息 `custom` cadence, ROUND 2: four set grids
// (幾月 12 · 幾號 31 · 幾點 24 · 幾分 12+) in a detail panel that is 320px wide
// on a phone, and the minute grid has to be USABLE the moment the form opens.
//
// jsdom evaluates no CSS and resolves no layout, so every assertion the vitest
// suite can make about these four groups is about class names, DOM presence and
// checked states. "The grid has class `mp-schedmsg__setgrid`" and "twelve
// minute cells are on screen and can be hit with a finger" are different
// claims, and only the second one is the feature the owner came back about:
// round 1 hid the sixty minute boxes behind a 「細部選擇」 disclosure with a row
// of interval shortcuts standing in for them, and he read the whole control as
// "intervals only — I cannot pick WHICH minute".
//
// So this measures the real thing in real Chromium against the real sheet:
// each cell's on-screen rectangle, what the browser says is painted at that
// rectangle's centre, the four groups' order and geometry, and what clicking
// actually changes. Width is an INPUT (how a grid wraps is a property of the
// column's width), so every assertion runs at the phone-width panel and a
// desktop one.
//
// WHAT IT ACTUALLY MEASURES (real Chromium, HEAD, story = every month/day/hour
// plus minutes {0, 7, 30}; panel overflow 0 and page overflow 0 at both widths):
//
//   group    320px panel                          900px panel
//   幾月     12 cells / 3 rows, grid 87px         12 / 1 row, grid 26px
//   幾號     31 cells / 8 rows, grid 172 of 239px 31 / 2 rows, grid 57px
//   幾點     24 cells / 6 rows, grid 172 of 178px 24 / 2 rows, grid 57px
//   幾分     13 cells / 4 rows, grid 118px        13 / 1 row, grid 26px
//
// The minute row is the one the owner complained about and it needs NO inner
// scrolling at either width (clientHeight == scrollHeight); the 31-day grid is
// the one that hits its cap, so the guard requires what it hides to be
// reachable inside its own scroll box. Every cell is 13x13 CSS px.
//
// MUTANTS, measured on THESE assertions (restored from a scratchpad backup +
// shasum, never `git checkout --`):
//
//   mutant                                          this guard (10)
//   `offered` re-derived per render instead of      🔴 2 — both minute-cell
//     frozen at mount (unticking 7 deletes its           tests, `expect(locator)
//     cell from under the pointer)                       .not.toBeChecked()`
//   `.mp-schedmsg__setgrid--minutes{max-height:26px}` 🔴 1 — narrow only, and the
//     (the sheet-level "cells need inner scrolling")     message names the cell
//                                                        and the 24px
//   全選 hands over `offered.slice(0, 1)`            🔴 2 — both months tests
//   the 幾月 group is not rendered at all            🔴 6 (＋ tsc red)
//
// ⚠️ The capped-grid mutant reddens the NARROW arm only, and that is correct
// rather than a miss: at 900px all thirteen cells fit in one 26px row, so
// nothing is out of view there. It is also why the width is an input.
// ⚠️ NOT re-measured for round 2: round 1 recorded that rewriting these
// assertions as class names + counts leaves them green on a broken sheet. The
// reasoning still applies to the rectangles below — `toHaveClass` and
// `toHaveCount` cannot see a cell that is 24px outside its container — but the
// experiment was run against the OLD assertions, so do not cite it as a
// measurement of these.
import { test, expect } from "@playwright/experimental-ct-react";
import { ScheduledMessagesCustomStory } from "./stories/ScheduledMessagesCustomStory";

// Spelled out rather than imported from the story: the CT bundler rewrites a
// spec's imports into component handles, so a value import sharing that module
// collides with the component's own binding ("already been declared").
const CUSTOM_ID = "sch-custom";
const EDIT = `mp-schedmsg-edit-${CUSTOM_ID}`;

/** The minute cells the group offers by default, plus the stored 7 the story
 * carries — thirteen numbers, in the order they must appear. */
const MINUTE_CELLS = [0, 5, 7, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55];
const ALL_MONTHS = Array.from({ length: 12 }, (_, i) => i + 1);

/** The two ends of the detail column's real range. */
const PANELS = [
  { name: "narrow panel", panel: 320, viewport: 390 },
  { name: "wide panel", panel: 900, viewport: 1280 },
];

/** Horizontal overflow of one element, read from the live layout. */
async function hOverflow(page: any, selector: string) {
  const got = await page.evaluate((sel: string) => {
    const el =
      sel === ":root"
        ? document.documentElement
        : (document.querySelector(sel) as HTMLElement | null);
    if (!el) return null;
    return { over: el.scrollWidth - el.clientWidth, width: el.clientWidth };
  }, selector);
  expect(got, `[${selector}] never rendered`).not.toBeNull();
  return got as { over: number; width: number };
}

/** Open the card and the stored custom schedule's editor. Nothing else is
 * clicked — every assertion below is about the state the form OPENS in. */
async function openCustomEditor(cmp: any) {
  await cmp.locator(".mp-expand__head").click();
  await cmp.locator(`[data-testid="${EDIT}"]`).click();
  await expect(
    cmp.locator(`[data-testid="${EDIT}-custom-minutes-grid"]`)
  ).toBeVisible();
}

for (const { name, panel, viewport } of PANELS) {
  test(`${name} (${panel}px): every minute cell is painted, hittable and inside the panel with nothing expanded first`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    await openCustomEditor(cmp);

    // (1) NON-VACUITY: the thirteen cells the story's selection implies are
    // really there, in sorted order — the stored 7 among the default twelve.
    const grid = cmp.locator(`[data-testid="${EDIT}-custom-minutes-grid"]`);
    const boxes = grid.locator("input[type=checkbox]");
    await expect(boxes).toHaveCount(MINUTE_CELLS.length);
    const labels = await grid.locator(".mp-schedmsg__setbox span").allInnerTexts();
    expect(
      labels.map((t) => Number(t.trim())),
      "the minute grid does not offer 0, 5 … 55 plus the stored 7, in order"
    ).toEqual(MINUTE_CELLS);

    // (2) CORE: each cell has a real painted rectangle, sits inside the grid's
    // own visible box (so no scrolling is needed to see it), and the element
    // the browser reports at its centre belongs to that cell. That last part is
    // what "點得到" means — a box covered by an overlay, clipped to zero, or
    // scrolled out of its container has a DOM node and a class and is still
    // unreachable.
    const probe = await page.evaluate(
      ({ gridSel, panelSel, ids }: any) => {
        const g = document.querySelector(gridSel) as HTMLElement;
        const p = document.querySelector(panelSel) as HTMLElement;
        const gr = g.getBoundingClientRect();
        const pr = p.getBoundingClientRect();
        const els = ids.map(
          (id: string) =>
            document.querySelector(`[data-testid="${id}"]`) as HTMLElement
        );
        // PASS 1 — geometry, read from the UNSCROLLED layout. This is the claim
        // "no scrolling INSIDE the group is needed to see a default minute", so
        // it has to be measured before anything scrolls.
        const geo = els.map((el: HTMLElement) => {
          const r = el.getBoundingClientRect();
          return {
            w: Math.round(r.width),
            h: Math.round(r.height),
            outOfGrid: Math.round(
              Math.max(gr.top - r.top, r.bottom - gr.bottom, gr.left - r.left, r.right - gr.right)
            ),
            outOfPanel: Math.round(Math.max(pr.left - r.left, r.right - pr.right)),
          };
        });
        // PASS 2 — hit testing, which needs the cell inside the WINDOW: a detail
        // panel taller than the window is ordinary, and `elementFromPoint` is
        // blind outside the viewport, so without scrolling every cell below the
        // fold reports "(nothing)" and the check degenerates into an assertion
        // about the window's height. What it still catches is exactly what it
        // should: an overlay on top of the cell, a zero-size box, or an ancestor
        // clipping it away.
        return els.map((el: HTMLElement, i: number) => {
          el.scrollIntoView({ block: "center" });
          const hit = el.getBoundingClientRect();
          const at = document.elementFromPoint(
            hit.left + hit.width / 2,
            hit.top + hit.height / 2
          );
          return {
            id: ids[i],
            ...geo[i],
            hitsSelf:
              at === el ||
              (at !== null && el.contains(at)) ||
              (at !== null && at.contains(el)),
            hitTag: at ? at.tagName + "." + at.className : "(nothing)",
          };
        });
      },
      {
        gridSel: `[data-testid="${EDIT}-custom-minutes-grid"]`,
        panelSel: '[data-testid="panel"]',
        ids: MINUTE_CELLS.map((n) => `${EDIT}-custom-minutes-${n}`),
      }
    );

    for (const c of probe) {
      expect(c.w, `${c.id} is ${c.w}px wide at ${panel}px`).toBeGreaterThanOrEqual(10);
      expect(c.h, `${c.id} is ${c.h}px tall at ${panel}px`).toBeGreaterThanOrEqual(10);
      expect(
        c.outOfGrid,
        `${c.id} sits ${c.outOfGrid}px outside the minute grid's visible box at ${panel}px — the owner has to scroll inside the group to find a default minute`
      ).toBeLessThanOrEqual(1);
      expect(
        c.outOfPanel,
        `${c.id} sits ${c.outOfPanel}px outside the detail panel at ${panel}px`
      ).toBeLessThanOrEqual(1);
      expect(
        c.hitsSelf,
        `a click at the centre of ${c.id} lands on ${c.hitTag} at ${panel}px`
      ).toBe(true);
    }

    // (3) …and a real click on the off-grid 7 toggles that value, not a
    // neighbour. The stored 7 starts ticked; one click clears it.
    const seven = cmp.locator(`[data-testid="${EDIT}-custom-minutes-7"]`);
    await expect(seven).toBeChecked();
    await seven.click();
    await expect(seven).not.toBeChecked();
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-minutes-5"]`)
    ).not.toBeChecked();
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-minutes-30"]`)
    ).toBeChecked();
  });

  test(`${name} (${panel}px): all four set groups are on screen, largest unit first, and none of them widens the column`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    await openCustomEditor(cmp);

    const groups = ["months", "days", "hours", "minutes"];
    const geo = await page.evaluate(
      ({ prefix, names, panelSel }: any) => {
        const p = document.querySelector(panelSel) as HTMLElement;
        const pr = p.getBoundingClientRect();
        return names.map((n: string) => {
          const el = document.querySelector(
            `[data-testid="${prefix}-custom-${n}"]`
          ) as HTMLElement | null;
          if (!el) return { n, missing: true };
          const r = el.getBoundingClientRect();
          const grid = el.querySelector(
            `[data-testid="${prefix}-custom-${n}-grid"]`
          ) as HTMLElement | null;
          return {
            n,
            missing: false,
            top: Math.round(r.top),
            w: Math.round(r.width),
            h: Math.round(r.height),
            outOfPanel: Math.round(Math.max(pr.left - r.left, r.right - pr.right)),
            boxes: grid
              ? grid.querySelectorAll("input[type=checkbox]").length
              : 0,
            gridVisibleH: grid ? grid.clientHeight : 0,
            gridFullH: grid ? grid.scrollHeight : 0,
          };
        });
      },
      {
        prefix: EDIT,
        names: groups,
        panelSel: '[data-testid="panel"]',
      }
    );

    const counts: Record<string, number> = {
      months: 12,
      days: 31,
      hours: 24,
      minutes: MINUTE_CELLS.length,
    };
    for (const g of geo) {
      expect(g.missing, `the ${g.n} group is not rendered at all`).toBe(false);
      expect(g.boxes, `the ${g.n} group renders ${g.boxes} boxes`).toBe(
        counts[g.n]
      );
      expect(g.w, `the ${g.n} group is ${g.w}px wide at ${panel}px`).toBeGreaterThan(
        80
      );
      expect(g.h, `the ${g.n} group is ${g.h}px tall at ${panel}px`).toBeGreaterThan(
        60
      );
      expect(
        g.outOfPanel,
        `the ${g.n} group sits ${g.outOfPanel}px outside the panel at ${panel}px`
      ).toBeLessThanOrEqual(1);
      // Bounded means CONTAINED, not clipped: a grid taller than its cap has to
      // be scrollable inside its own box, and no grid may eat the column.
      expect(
        g.gridVisibleH,
        `the ${g.n} grid is ${g.gridVisibleH}px tall at ${panel}px — it has taken over the panel`
      ).toBeLessThanOrEqual(220);
    }
    // Largest unit first: reading the four headings downwards spells the same
    // sentence the row summary prints. Tops are strictly increasing.
    const tops = geo.map((g: any) => g.top);
    expect(
      tops,
      `the four groups are stacked ${JSON.stringify(tops)} — not 月 → 號 → 點 → 分`
    ).toEqual([...tops].sort((a, b) => a - b));
    expect(new Set(tops).size).toBe(4);

    // The 31-day grid is the tall one: whatever its cap hides must be reachable
    // inside the grid's own scroll box.
    const days = geo.find((g: any) => g.n === "days") as any;
    if (days.gridFullH > days.gridVisibleH) {
      const reachable = await page.evaluate((sel: string) => {
        const g = document.querySelector(sel) as HTMLElement;
        g.scrollTop = g.scrollHeight;
        return g.scrollTop > 0;
      }, `[data-testid="${EDIT}-custom-days-grid"]`);
      expect(
        reachable,
        `the day grid clips ${days.gridFullH - days.gridVisibleH}px that nothing can scroll to at ${panel}px`
      ).toBe(true);
    }

    // Neither the panel nor the page gained horizontal scroll. Pixels, not a
    // class name.
    const panelBox = await hOverflow(page, '[data-testid="panel"]');
    expect(
      panelBox.over,
      `the detail panel hides ${panelBox.over}px horizontally at ${panel}px — a set grid widened the column`
    ).toBeLessThanOrEqual(1);
    const doc = await hOverflow(page, ":root");
    expect(
      doc.over,
      `the page scrolls ${doc.over}px sideways at ${panel}px`
    ).toBeLessThanOrEqual(1);
  });

  test(`${name} (${panel}px): the 幾月 group's 全選 and 清除 move all twelve boxes`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    await openCustomEditor(cmp);

    const monthBox = (n: number) =>
      cmp.locator(`[data-testid="${EDIT}-custom-months-${n}"]`);
    const summary = cmp.locator(`[data-testid="${EDIT}-custom-months-summary"]`);

    // The story opens on every month, so 清除 is the direction with something
    // to lose — running it first makes 全選 a real change rather than a no-op.
    const wholeYear = (await summary.innerText()).trim();
    expect(wholeYear.length, "the months summary renders nothing").toBeGreaterThan(0);

    const clear = cmp.locator(`[data-testid="${EDIT}-custom-months-clear"]`);
    await expect(clear).toBeVisible();
    await clear.click();
    for (const n of ALL_MONTHS) await expect(monthBox(n)).not.toBeChecked();
    const emptied = (await summary.innerText()).trim();
    expect(
      emptied,
      "clearing the months left the summary saying what it said when all twelve were ticked"
    ).not.toBe(wholeYear);

    // 🔴 全選 LISTS every month — the wire has no "all" value, so this has to
    // move twelve real checkboxes and not flip a marker.
    const all = cmp.locator(`[data-testid="${EDIT}-custom-months-all"]`);
    await expect(all).toBeVisible();
    await all.click();
    for (const n of ALL_MONTHS) await expect(monthBox(n)).toBeChecked();
    expect((await summary.innerText()).trim()).toBe(wholeYear);

    // …and it hit ONE group: the neighbouring day set is untouched by either
    // button, so "全選" is not a form-wide reset wearing a group's label.
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-days-1"]`)
    ).toBeChecked();
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-minutes-7"]`)
    ).toBeChecked();

    // Both buttons are inside the panel and hittable, not just present.
    for (const [label, loc] of [
      ["clear", clear],
      ["all", all],
    ] as const) {
      const box = await loc.boundingBox();
      expect(box, `the months ${label} button has no box at ${panel}px`).not.toBeNull();
      expect(
        box!.width,
        `the months ${label} button is ${box!.width}px wide at ${panel}px`
      ).toBeGreaterThan(20);
      expect(box!.height).toBeGreaterThan(12);
    }
  });
}

// ── The tick has to be visible, and only a real browser can say so ───────────
// A grid whose entire purpose is "which ones did I tick" is useless if checked
// and unchecked cannot be told apart. That distinction is drawn by the browser
// from `accent-color`, which jsdom does not compute and no class-name assertion
// can see: `.mp-schedmsg__setbox input` had the same class either way while
// sitting at ~1.1:1 against the card under the built-in dark palette.
//
// LIGHT_PACK is the palette of a REAL shipped theme pack, copied verbatim from
// visual-guards/stories/ThemeContrastStory.tsx (value-imported here rather than
// shared, because the CT bundler rewrites a spec's imports into component
// handles). Do not hand-tune these values: tuned, the guard stops reporting a
// fact about anything a user can ship.
const LIGHT_PACK: Record<string, string> = {
  "--color-bg": "#c2d492",
  "--color-card": "#fdfbf1",
  "--color-text": "#33301f",
  "--color-text-strong": "#1e1c10",
  "--color-text-muted": "#403d2c",
  "--color-border": "#b0ae83",
  "--color-accent": "#2b450b",
  "--color-overlay": "#241f0d",
};

type Rgb = { r: number; g: number; b: number; a: number };

function parseRgb(s: string): Rgb {
  const m = s.match(/rgba?\(([^)]+)\)/i);
  if (m) {
    const p = m[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  // Chromium reports some resolved colours in the CSS Color 4 form.
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

function over(fg: Rgb, bg: Rgb): Rgb {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  };
}

function contrast(a: Rgb, b: Rgb): number {
  const lin = (v: number) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  const lum = (c: Rgb) =>
    0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

for (const theme of ["built-in dark", "light pack"] as const) {
  // Both the oldest group and the NEWEST one: the months grid is new markup,
  // and "the sheet already handles the others" is exactly the assumption that
  // ships a group nobody can read.
  for (const [group, cell] of [
    ["minutes", 7],
    ["months", 3],
  ] as const) {
    test(`${theme}: a ticked ${group} box is distinguishable from an unticked one`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width: 1280, height: 900 });
      const cmp = await mount(<ScheduledMessagesCustomStory width={900} />);
      if (theme === "light pack") {
        await page.evaluate((pack: Record<string, string>) => {
          for (const [k, v] of Object.entries(pack))
            document.documentElement.style.setProperty(k, v);
        }, LIGHT_PACK);
      }
      await openCustomEditor(cmp);

      const box = cmp.locator(`[data-testid="${EDIT}-custom-${group}-${cell}"]`);
      await expect(box).toBeChecked();

      // The tick's colour, and every background painted behind the cell folded
      // down to the pixels actually on screen.
      const read = await box.evaluate((el: HTMLElement) => {
        const layers: string[] = [];
        let node: HTMLElement | null = el;
        while (node) {
          layers.push(getComputedStyle(node).backgroundColor);
          node = node.parentElement;
        }
        return { accent: getComputedStyle(el).accentColor, layers };
      });

      let bg: Rgb = { r: 255, g: 255, b: 255, a: 1 };
      for (let i = read.layers.length - 1; i >= 0; i--) {
        const layer = parseRgb(read.layers[i]);
        if (layer.a === 0) continue;
        bg = over(layer, bg);
      }
      // `auto` would mean the sheet says nothing and the browser picks — which
      // is not a fact this guard can measure, and not what the control declares.
      expect(read.accent).not.toBe("auto");
      const accent = over(parseRgb(read.accent), bg);

      // 3:1, the non-text threshold for a UI component's state.
      const ratio = contrast(accent, bg);
      expect(
        ratio,
        `the checked fill sits at ${ratio.toFixed(2)}:1 against the cell it is painted on under the ${theme} — an owner cannot see which ${group} are selected`
      ).toBeGreaterThanOrEqual(3);
    });
  }
}
