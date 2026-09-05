// T-93 — the 請示 page's new ID 篩選列, measured in a real browser.
//
// WHY THIS FILE EXISTS. Two review rounds recorded the same gap and neither
// closed it: nobody had ever LOOKED at this control. The vitest suite renders
// it in jsdom, which applies no layout engine and computes no colour, so it
// answers "is the input in the DOM" and nothing about whether the owner can
// see it or whether it fits on his phone. The cloud `frontend-ct` job was
// green throughout for the same reason — it ran a set of guards, none of which
// mounts this row. A green that never rendered the thing is not evidence.
//
// WHAT IS ASSERTED IS GEOMETRY AND COMPUTED COLOUR — things the owner can see:
//   (1) the field and 清除篩選 both stay INSIDE the content column, and the
//       row does not push the page into horizontal scrolling, at phone widths.
//       The field is `width: 200px` with a `max-width: 100%`; the button sits
//       beside it in a wrapping flex row. 320px is where that pairing is
//       tightest.
//   (2) the field is FINDABLE against the page behind it — it must differ from
//       its surroundings by a border, a fill, or both. Asserted as "the
//       composited border colour is not equal to the composited page
//       background", in BOTH theme families.
//   (3) 清除篩選 keeps its four CJK glyphs on ONE line box inside its own box.
//       (The 預設/編輯 pills in set-badge-nowrap burst exactly this way.)
//
// 🔴 WHAT THIS GUARD DELIBERATELY DOES NOT ASSERT, and why you should know:
// a ≥3:1 non-text contrast threshold on the field's border. Measured here, the
// border-vs-page ratio is LOW (an independent reviewer computed ~1.35:1 dark,
// ~1.54:1 on a light pack). That number is inherited verbatim from the
// pre-existing `.tasks__filter` pill this control was copied from, so a
// threshold assertion would redden on code this ticket did not write and would
// be a styling decision taken without the owner, who has said he wants to see
// this himself on the trial station. The honest guard is the one above: the
// field must be distinguishable from its background at all, and the empty
// field additionally carries placeholder text at a comfortable ratio. If the
// owner asks for a stronger boundary, raise (2) into a threshold then — do not
// read its absence as "contrast was checked and passed".
//
// MUTANTS (run against this file; landing and restore proven by sha256 of
// idFilter.css, never by git):
//   · `border: …` → `border: none`  ⇒ BOTH theme tests red on "the field must
//     keep a border at all". Assertion red, not a compile red.
//   · add `min-width: 300px`        ⇒ ONLY the 320 test red, on "filter row
//     horizontal overflow". 390 and 1040 stay green — the discrimination the
//     three widths exist for.
//   · `width: 200px; max-width: 100%` → `width: 400px`  ⇒ ALL FIVE STAY GREEN,
//     and that is CORRECT, not a hole: `.id-filter` is a flex item with the
//     default `flex-shrink: 1`, so an oversized width is absorbed by shrinking
//     and no overflow is ever produced. The mutant plants no bug. Recorded
//     because it was run first and its green was briefly read as "assertion (1)
//     has no teeth" — the min-width mutant is the one that plants the real one.
//
// ⚠️ THE STORY'S ANCESTOR CHAIN IS LOAD-BEARING. The first version mounted the
// row under a bare `width: 100%` div with only replies.css imported; the
// container was then free to grow to its content and NO width mutant could ever
// overflow it. It now carries `.app > .app__main > .replies` with chrome.css
// loaded, which is where the 1040 cap and the 22px gutters actually live. A
// bare mount here buys slack and turns this whole file green-by-construction.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import { ReplyIdFilterStory } from "./stories/ReplyIdFilterStory";

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

function sameColour(a: Rgba, b: Rgba): boolean {
  return (
    Math.abs(a.r - b.r) < 0.5 &&
    Math.abs(a.g - b.g) < 0.5 &&
    Math.abs(a.b - b.b) < 0.5
  );
}

/** Line boxes + vertical spill of an element's own text, measured with a Range
 * (one client rect per line box) against the element's border box. */
async function textGeometry(el: Locator) {
  return await el.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(node);
    const rects = Array.from(range.getClientRects());
    return {
      lines: rects.length,
      spillAbove: box.top - Math.min(...rects.map((r) => r.top)),
      spillBelow: Math.max(...rects.map((r) => r.bottom)) - box.bottom,
    };
  });
}

// 320 = the narrowest phone still in use and the width where a 200px field
// beside a button is tightest. 390 = the phone width the rest of this suite
// treats as the owner's. 1040 = the desktop content column's max width, the
// control that says a narrow-width fix did not move the breakage to desktop.
for (const width of [320, 390, 1040]) {
  test(`width ${width}: the ID 篩選 row fits, and 清除篩選 keeps its label on one line`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    // A non-empty value is what makes 清除篩選 render at all — an empty field
    // would silently measure only half the row.
    const cmp = await mount(
      <ReplyIdFilterStory theme="dark" initialValue="rc-428906235337" />
    );

    const field = cmp.getByTestId("filter-reply-card-id");
    await expect(field).toBeVisible();
    const clear = cmp.getByTestId("clear-filters");
    await expect(clear).toBeVisible();
    await expect(clear).toHaveText("清除篩選");

    // (1) Nothing escapes the row, the page does not scroll sideways.
    const spill = await page.evaluate(() => {
      const row = document.querySelector(".replies__filters")!;
      return {
        row: row.scrollWidth - row.clientWidth,
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    expect(spill.row, "filter row horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.page, "page horizontal overflow").toBeLessThanOrEqual(1);

    const rowBox = (await cmp.locator(".replies__filters").boundingBox())!;
    const fieldBox = (await field.boundingBox())!;
    expect(
      fieldBox.x + fieldBox.width,
      "ID field right edge vs the filter row"
    ).toBeLessThanOrEqual(rowBox.x + rowBox.width + 1);

    // (3) 清除篩選 keeps its four glyphs on one line, inside its own box.
    const clearGeo = await textGeometry(clear);
    expect(clearGeo.lines, "清除篩選 label line boxes").toBe(1);
    expect(
      clearGeo.spillAbove,
      "清除篩選 label spilling above its button"
    ).toBeLessThanOrEqual(0.5);
    expect(
      clearGeo.spillBelow,
      "清除篩選 label spilling below its button"
    ).toBeLessThanOrEqual(0.5);
  });
}

// The state this ticket ADDED, and the one the geometry loop above cannot
// reach: the owner deletes the value by hand while the hash still carries the
// id, so the field is EMPTY and 清除篩選 is still on screen. Narrowest width
// only — that is where a row that fits when full could still break when the
// button sits beside an empty field with a placeholder in it.
test("width 320: the row still fits when the field is emptied by hand and 清除篩選 stays", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const cmp = await mount(<ReplyIdFilterStory theme="dark" seeded />);

  const clear = cmp.getByTestId("clear-filters");
  await expect(clear).toBeVisible();
  await expect(cmp.getByTestId("filter-reply-card-id")).toHaveValue("");

  const spill = await page.evaluate(() => {
    const row = document.querySelector(".replies__filters")!;
    return {
      row: row.scrollWidth - row.clientWidth,
      page:
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    };
  });
  expect(spill.row, "filter row horizontal overflow").toBeLessThanOrEqual(1);
  expect(spill.page, "page horizontal overflow").toBeLessThanOrEqual(1);

  const clearGeo = await textGeometry(clear);
  expect(clearGeo.lines, "清除篩選 label line boxes").toBe(1);
});

// (2) The field must be distinguishable from the page behind it, in BOTH theme
// families. This is the assertion that would have caught a field whose border
// was deleted or whose fill collapsed into the background — the failure mode
// the topbar guard (theme-contrast ①) records as "an invisible rectangle".
for (const theme of ["dark", "light"] as const) {
  test(`theme ${theme}: the ID field is distinguishable from the page behind it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1040, height: 900 });
    const cmp = await mount(<ReplyIdFilterStory theme={theme} />);

    const field = cmp.getByTestId("filter-reply-card-id");
    await expect(field).toBeVisible();

    const colours = await field.evaluate((node) => {
      const cs = getComputedStyle(node);
      return {
        border: cs.borderTopColor,
        fill: cs.backgroundColor,
        borderWidth: cs.borderTopWidth,
        page: getComputedStyle(document.body).backgroundColor,
      };
    });

    // The border is alpha-composited (color-mix with transparent), so compare
    // what is actually PAINTED, not the declared value.
    const pageBg = parseColor(colours.page);
    const border = over(parseColor(colours.border), pageBg);
    const fill = over(parseColor(colours.fill), pageBg);

    expect(
      parseFloat(colours.borderWidth),
      // NOTE: this half only asks that a border WIDTH is declared. A border
      // painted `transparent` still passes it (measured: that mutant leaves all
      // 5 cases green) — and that is correct, because the contract below is
      // "distinguishable by a border, a fill, or BOTH", and the fill alone
      // satisfies it. Do not read this line as a guard on the border's COLOUR.
      "the field must declare a border width"
    ).toBeGreaterThan(0);
    expect(
      sameColour(border, pageBg) && sameColour(fill, pageBg),
      "the field must differ from the page by a border, a fill, or both"
    ).toBe(false);
  });
}
