// T-93 — the list pages' 篩選面板, measured in a real browser, on the real page.
//
// WHY THIS FILE EXISTS. Two review rounds recorded the same gap and neither
// closed it: nobody had ever LOOKED at this control. The vitest suite renders
// it in jsdom, which applies no layout engine and computes no colour, so it
// answers "is the input in the DOM" and nothing about whether the owner can
// see it or whether it fits on his phone. The cloud `frontend-ct` job was
// green throughout for the same reason — it ran a set of guards, none of which
// mounts this panel. A green that never rendered the thing is not evidence.
//
// 🔴 AND THAT IS EXACTLY HOW THIS FILE ITSELF FAILED ONCE — READ THIS BEFORE
// EDITING. Its first version (`reply-id-filter.ct.spec.tsx`, with
// `stories/ReplyIdFilterStory.tsx`) measured a `.replies__filters` row that the
// STORY built out of raw markup. Round 2 replaced both list pages' 篩選列 with
// the shared `FilterPanel`, and that row stopped existing on any page — but the
// story kept assembling it, so the guard stayed green while measuring a layout
// the product does not render. It flunked its own standard.
//
// The fix is structural, not a bigger assertion count: THE PANEL NOW COMES FROM
// `RepliesPage`. `RepliesPageStory` mounts the shipped page and this file drives
// it the way the owner does — click the funnel, type an id, press 套用篩選 —
// so deleting the `<FilterPanel>` from that page leaves this guard with nothing
// to measure. Verified, not assumed; see MUTANTS. If you ever replace a mount
// here with hand-built markup "to make the test simpler", you are rebuilding the
// exact defect this file was rewritten to fix.
//
// WHAT IS ASSERTED IS GEOMETRY AND COMPUTED COLOUR — things the owner can see:
//   (1) the panel — its form, its 已篩選 strip and its header row — stays
//       INSIDE the content column and does not push the page into horizontal
//       scrolling, at phone widths. 320px is where that is tightest.
//   (2) the ID field is FINDABLE against the page behind it — it must differ
//       from its surroundings by a border, a fill, or both. Asserted as "the
//       composited border colour is not equal to the composited page
//       background", in BOTH theme families.
//   (3) every CJK label in the shell keeps its glyphs on ONE line box inside
//       its own box: 取消 / 套用篩選 on the action row, 清除全部 on the strip,
//       and 篩選 on the funnel toggle. (The 預設/編輯 pills in set-badge-nowrap
//       burst exactly this way; CJK min-content is ONE CHARACTER — see
//       frontend/.claude/rules/css-layout-traps.md.)
//   (4) the 取消 / 套用篩選 row stays inside the form box and on ONE row.
//   (5) the 已篩選 chips WRAP when several filters are applied, rather than
//       running out of the strip, and 清除全部 stays reachable inside it.
//   (6) 🔴 THE PANEL IS NOT AN OVERLAY. Expanding it MOVES the list below it
//       down, and neither the panel nor its form is `fixed`/`absolute`/`sticky`.
//       owner c-3b5a0aa66550 ruled 「按搜尋時不要再跳出新modal」 AFTER being
//       shown the overlay version, so this is a decision being held, not a
//       style preference — and this is its only mechanical witness. jsdom
//       cannot hold it: it computes no layout, so an overlaid panel and an
//       in-flow one look identical to every unit test we have.
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
// MUTANTS. Every one below was applied, run, and reverted; the file was re-run
// green after each (9 passed). Two kinds, and the first kind is what this
// rewrite is actually about — a CSS mutant proves an ASSERTION has teeth, it
// does not prove the guard is attached to the product. That is the defect being
// fixed here, so it gets its own mutant class:
//
// ── IS THE GUARD ATTACHED TO THE PRODUCT? ──────────────────────────────────
//   · RepliesPage.tsx: delete the `<FilterPanel …>` element, leaving its
//     `<IdFilterInput>` rendered bare ⇒ 8 failed, 1 passed. Every real-page test
//     dies at the funnel — `locator.click: Test timeout … waiting for
//     getByTestId('replies-filter-toggle')`. The one survivor is the
//     several-chip test, and correctly so: it is the single case the 請示 page
//     cannot produce (one filter axis ⇒ never two chips), so it mounts
//     `FilterPanel` directly. THE OLD VERSION OF THIS GUARD COULD NOT FEEL THIS
//     MUTANT AT ALL — that is why it stayed green for a row no page rendered.
//   · FilterPanel.tsx: `return null` from the component ⇒ 9 failed, on
//     "element(s) not found" for `replies-filter-form` / `-summary` / `-toggle`.
//
// ── DO THE ASSERTIONS HAVE TEETH? (CSS mutants, all in filter-panel.css) ────
//   · `.filter-panel__form { position: fixed; top: 0; left: 0 }` ⇒ 4 failed. The
//     overlay test reds on BOTH of its halves (they are `expect.soft` so both
//     report): the computed `position` check, and 「the list below must be pushed
//     down by the opened panel」 — expected ≥ 124, received 0. It ALSO reds the
//     320/390 width tests, because a fixed box shrinks to fit and squeezes the
//     action row; 1040 stays green.
//   · `.filter-panel__btn { padding: 0 20px }` → `0 34px` ⇒ 2 failed, both at
//     width 320, on 「套用篩選 label line boxes」 (received 2, expected 1). 390
//     and 1040 stay GREEN: at 320 the two buttons are `flex: 1` inside a ~240px
//     row, so the extra padding squeezes the label below its own width while CJK
//     min-content (ONE CHARACTER) lets it fold rather than refuse to shrink.
//     That discrimination is why three widths exist.
//   · `.filter-panel__summary { flex-wrap: wrap }` → `nowrap` ⇒ 3 failed. The
//     several-chip test reds on all four of its soft halves — strip overflow
//     (received 3), 「distinct rows the 已篩選 chips occupy」 (received 1),
//     「清除全部 right edge vs the strip」 (214.06 vs ≤212.06) and its line-box
//     count (4) — and the 320/390 width tests red on 「清除全部 label line
//     boxes」.
//   · `.filter-panel__toggle { height: 30px }` → `12px` ⇒ 3 failed, all three
//     widths, on 「篩選 label spilling ABOVE its toggle」 (received 2, expected
//     ≤0.5). This is the mutant that proves assertion (3)'s toggle half executes
//     and has teeth.
//   · `.filter-panel__toggle { white-space: nowrap }` → `normal` ⇒ 9 PASSED, and
//     that is NOT a hole to be closed by tightening the assertion. The header row
//     holds 「請示卡」 (~42px) plus a ~74px toggle inside a ≥276px column, so
//     nothing squeezes the toggle at any width a phone has; the declaration is
//     insurance for a longer title, not something these widths exercise.
//     Recorded because its green was briefly read as "the toggle assertion has
//     no teeth" — the `height: 12px` mutant above is the one that plants a bug
//     this assertion can see.
//   · `.filter-panel__actions { padding-top: 12px }` → `0` ⇒ 9 PASSED. Recorded
//     as a KNOWN uncovered edit: nothing here measures the action row's
//     separation from the fields, only that it fits and stays on one row. Do not
//     read this file as a guard on the panel's internal spacing.
//
// ⚠️ NOT COVERED HERE, and say so rather than implying otherwise: the 任務頁's
// own use of `FilterPanel` has no mount in this file. What is guarded here is
// the SHELL (through the 請示 page) plus the several-chip strip; a 任務頁
// GEOMETRY guard, if anyone wants one, is a second file that drives that page.
//
// But be precise about how big that gap is, because "not covered" reads bigger
// than it is: the 任務頁's WIRING is guarded, just not in a browser. Measured,
// not assumed — renaming that page's `testId="tasks-filter"` (a mutant that
// COMPILES, unlike deleting the element, which only produces a transform error
// and therefore proves nothing) reddens **37 assertions** across
// `TasksPage.test.tsx` / `.id-filter` / `.jump` and the TaskCard suites. So a
// 任務頁 that stops rendering the panel is caught immediately; what nobody
// measures is whether that page's panel FITS and stays in flow.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import {
  RepliesPageStory,
  FilterChipsStory,
} from "./stories/RepliesFilterPanelStory";

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

/** Line boxes + vertical spill of an element's own TEXT, measured with a Range
 * per text node (one client rect per line box) against the element's border
 * box.
 *
 * ⚠️ A Range over the whole element would count the funnel `<svg>` as a line
 * box, so this walks TEXT NODES only, and counts DISTINCT line tops rather than
 * rects — a label split across two text nodes on one line is still one line. */
async function labelGeometry(el: Locator) {
  return await el.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT);
    const rects: DOMRect[] = [];
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!n.textContent || !n.textContent.trim()) continue;
      const range = document.createRange();
      range.selectNodeContents(n);
      rects.push(...Array.from(range.getClientRects()));
    }
    if (rects.length === 0) throw new Error("no text in this element");
    const tops = new Set(rects.map((r) => Math.round(r.top)));
    return {
      lines: tops.size,
      spillAbove: box.top - Math.min(...rects.map((r) => r.top)),
      spillBelow: Math.max(...rects.map((r) => r.bottom)) - box.bottom,
    };
  });
}

/** Horizontal overflow of one element, and of the page. */
async function overflow(locator: Locator) {
  return await locator.evaluate((node) => ({
    self: node.scrollWidth - node.clientWidth,
    page:
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  }));
}

const FULL_ID = "rc-428906235337"; // 15 chars, the real shape (api_replycards.go:283)

/** Drive the real page the way the owner does: funnel → type → 套用篩選. */
async function applyId(cmp: Locator, id: string) {
  await cmp.getByTestId("replies-filter-toggle").click();
  await cmp.getByTestId("filter-reply-card-id").fill(id);
  await cmp.getByTestId("replies-filter-apply").click();
  await expect(cmp.getByTestId("replies-filter-summary")).toBeVisible();
}

// 320 = the narrowest phone still in use, and the width where the two action
// buttons beside each other are tightest. 390 = the phone width the rest of this
// suite treats as the owner's. 1040 = the desktop content column's max width,
// the control that says a narrow-width fix did not move the breakage to desktop.
for (const width of [320, 390, 1040]) {
  test(`width ${width}: the open 篩選面板 fits, and its CJK labels keep to one line`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<RepliesPageStory theme="dark" />);

    // Apply a filter, then RE-OPEN: that is the only state in which the form,
    // the 已篩選 strip and the header row are all on screen at once. Measuring
    // any lesser state measures half the panel.
    await applyId(cmp, FULL_ID);
    await cmp.getByTestId("replies-filter-toggle").click();

    const form = cmp.getByTestId("replies-filter-form");
    const summary = cmp.getByTestId("replies-filter-summary");
    const field = cmp.getByTestId("filter-reply-card-id");
    const cancel = cmp.getByTestId("replies-filter-cancel");
    const apply = cmp.getByTestId("replies-filter-apply");
    const clear = cmp.getByTestId("replies-filter-clear");
    const toggle = cmp.getByTestId("replies-filter-toggle");
    for (const el of [form, summary, field, cancel, apply, clear, toggle]) {
      await expect(el).toBeVisible();
    }
    await expect(cancel).toHaveText("取消");
    await expect(apply).toHaveText("套用篩選");
    await expect(clear).toHaveText("清除全部");

    // (1) Nothing escapes the panel's boxes, and the page does not scroll
    // sideways.
    for (const [name, el] of [
      ["form", form],
      ["summary strip", summary],
      ["header row", cmp.locator(".filter-panel__header")],
      ["action row", cmp.locator(".filter-panel__actions")],
    ] as const) {
      const o = await overflow(el);
      expect(o.self, `${name} horizontal overflow`).toBeLessThanOrEqual(1);
      expect(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);
    }

    const formBox = (await form.boundingBox())!;
    const fieldBox = (await field.boundingBox())!;
    expect(
      fieldBox.x + fieldBox.width,
      "ID field right edge vs the panel form"
    ).toBeLessThanOrEqual(formBox.x + formBox.width + 1);

    // (4) The 取消 / 套用篩選 row: both buttons inside the form, side by side on
    // ONE row.
    const cancelBox = (await cancel.boundingBox())!;
    const applyBox = (await apply.boundingBox())!;
    for (const [name, b] of [
      ["取消", cancelBox],
      ["套用篩選", applyBox],
    ] as const) {
      expect(b.x, `${name} left edge vs the panel form`).toBeGreaterThanOrEqual(
        formBox.x - 1
      );
      expect(
        b.x + b.width,
        `${name} right edge vs the panel form`
      ).toBeLessThanOrEqual(formBox.x + formBox.width + 1);
    }
    expect(
      Math.abs(cancelBox.y - applyBox.y),
      "取消 and 套用篩選 must share one row"
    ).toBeLessThanOrEqual(1);

    // (3) Every CJK label in the shell, on one line box, inside its own box.
    for (const [name, el] of [
      ["取消", cancel],
      ["套用篩選", apply],
      ["清除全部", clear],
      ["篩選", toggle],
    ] as const) {
      const box = name === "篩選" ? "toggle" : "button";
      const geo = await labelGeometry(el);
      expect(geo.lines, `${name} label line boxes`).toBe(1);
      expect(
        geo.spillAbove,
        `${name} label spilling above its ${box}`
      ).toBeLessThanOrEqual(0.5);
      expect(
        geo.spillBelow,
        `${name} label spilling below its ${box}`
      ).toBeLessThanOrEqual(0.5);
    }
  });
}

// (5) The 已篩選 strip with SEVERAL filters applied. One chip fits anywhere;
// four is the 任務頁's real shape (編號 + 執行者 + 狀態 + 類型) on this same
// shared strip, and it is the only state in which "wraps" and "runs out of the
// box" tell apart. Narrowest width, because that is where they diverge.
//
// ⚠️ This is the ONE test here that does not drive the 請示 page: that page
// filters on a single axis and cannot produce a second chip. It mounts the
// shipped `FilterPanel` with a dictated chip list — see the story's second
// export for why that trade is made here and nowhere else.
test("width 320: several 已篩選 chips WRAP inside the strip, and 清除全部 stays reachable", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const cmp = await mount(
    <FilterChipsStory
      theme="dark"
      chipLabels={[
        `編號：${FULL_ID}`,
        "執行者：陳小明",
        "狀態：進行中",
        "類型：功能開發",
      ]}
    />
  );

  const summary = cmp.getByTestId("replies-filter-summary");
  const clear = cmp.getByTestId("replies-filter-clear");
  await expect(summary).toBeVisible();
  await expect(clear).toBeVisible();

  // 🔴 soft from here down, ON PURPOSE: `flex-wrap: nowrap` breaks the strip in
  // several places at once, and a hard first failure would leave the others
  // unproven. "which parts of the strip broke" is the useful output.
  const o = await overflow(summary);
  expect.soft(o.self, "已篩選 strip horizontal overflow").toBeLessThanOrEqual(1);
  expect.soft(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);

  // WRAP, not one runaway row: four chips plus a count, a label and 清除全部
  // cannot share one line in a ~240px column, so if they all report the same top
  // the strip is overflowing rather than wrapping.
  const chips = cmp.getByTestId("replies-filter-chip");
  await expect(chips).toHaveCount(4);
  const rows = await chips.evaluateAll(
    (nodes) =>
      new Set(nodes.map((n) => Math.round(n.getBoundingClientRect().top))).size
  );
  expect.soft(rows, "distinct rows the 已篩選 chips occupy").toBeGreaterThan(1);

  const stripBox = (await summary.boundingBox())!;
  const clearBox = (await clear.boundingBox())!;
  expect.soft(
    clearBox.x + clearBox.width,
    "清除全部 right edge vs the strip"
  ).toBeLessThanOrEqual(stripBox.x + stripBox.width + 1);
  expect.soft(
    clearBox.y + clearBox.height,
    "清除全部 bottom edge vs the strip"
  ).toBeLessThanOrEqual(stripBox.y + stripBox.height + 1);
  const clearGeo = await labelGeometry(clear);
  expect.soft(clearGeo.lines, "清除全部 label line boxes").toBe(1);
});

// 🔴 (6) THE PANEL IS NOT AN OVERLAY. owner c-3b5a0aa66550,「按搜尋時不要再跳出新
// modal」, said AFTER he was shown the overlay version — so this is a decision
// being held, not a style preference. The witness is mechanical, and it is taken
// on the real page: open the panel by clicking the funnel the owner clicks, and
// require the list below to MOVE. A `position: fixed` panel leaves it exactly
// where it was.
test("the 篩選面板 is page content, not an overlay: opening it moves the list down", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);

  const below = cmp.locator(".replies__section").first();
  await expect(below).toBeVisible();
  const beforeY = (await below.boundingBox())!.y;

  await cmp.getByTestId("replies-filter-toggle").click();
  const form = cmp.getByTestId("replies-filter-form");
  await expect(form).toBeVisible();

  // Neither the shell nor the expanded form may be taken out of flow.
  const positions = await form.evaluate((node) => ({
    form: getComputedStyle(node).position,
    shell: getComputedStyle(node.closest(".filter-panel")!).position,
  }));
  // 🔴 soft, all three, ON PURPOSE: an overlay breaks BOTH halves of this test
  // (out of flow AND the list stays put), and a hard failure on the first would
  // hide whether the second still has teeth. "one red" and "both red" have to be
  // distinguishable in the output.
  for (const [name, pos] of Object.entries(positions)) {
    expect.soft(
      ["fixed", "absolute", "sticky"],
      `the ${name} must stay in page flow, not float over it`
    ).not.toContain(pos);
  }

  const formBox = (await form.boundingBox())!;
  const afterY = (await below.boundingBox())!.y;
  expect.soft(
    afterY - beforeY,
    "the list below must be pushed down by the opened panel"
  ).toBeGreaterThanOrEqual(formBox.height - 1);
  // …and it must end up BELOW the form, not under it.
  expect.soft(
    formBox.y + formBox.height,
    "the form's bottom edge vs the list below it"
  ).toBeLessThanOrEqual(afterY + 1);
});

// The state the loop above cannot reach: the draft field is EMPTY while a filter
// is still APPLIED — the owner cleared the box but has not pressed 套用篩選, so
// the strip and its 清除全部 are still on screen beside an empty field.
// Narrowest width only: that is where a panel which fits when full could still
// break with a placeholder in the box.
test("width 320: the panel still fits with an empty draft field and a filter still applied", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);

  await applyId(cmp, FULL_ID);
  await cmp.getByTestId("replies-filter-toggle").click();
  const field = cmp.getByTestId("filter-reply-card-id");
  await field.fill("");
  await expect(field).toHaveValue("");
  await expect(cmp.getByTestId("replies-filter-clear")).toBeVisible();

  for (const [name, sel] of [
    ["form", ".filter-panel__form"],
    ["summary strip", ".filter-panel__summary"],
  ] as const) {
    const o = await overflow(cmp.locator(sel));
    expect(o.self, `${name} horizontal overflow`).toBeLessThanOrEqual(1);
    expect(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);
  }

  const applyGeo = await labelGeometry(cmp.getByTestId("replies-filter-apply"));
  expect(applyGeo.lines, "套用篩選 label line boxes").toBe(1);
});

// owner 2026-09-06 (rc-44347fc49338): 「好像太寬了」, then the reason —「請示卡的
// ID好像都是固定長度？」. He is right: api_replycards.go:283 mints "rc-" + 12 hex,
// so every 請示卡 id is exactly 15 characters, while the field was a flat 200px
// chosen with no reference to that.
//
// This turns his aesthetic note into something MEASURABLE, which is the only
// form it can be kept in: a whole id must be fully visible in the field, and the
// field must not be much wider than the id it holds. A pixel number would have
// gone stale the first time anyone touched the font; this asks about the
// RELATIONSHIP between the box and its content, so it survives that.
test("the field is sized to the id it holds — a whole id fits, with little to spare", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1040, height: 900 });
  expect(FULL_ID.length, "the id shape this guard is calibrated on").toBe(15);
  const cmp = await mount(<RepliesPageStory theme="dark" />);
  await cmp.getByTestId("replies-filter-toggle").click();
  const field = cmp.getByTestId("filter-reply-card-id");
  await field.fill(FULL_ID);

  const m = await field.evaluate((el: HTMLInputElement) => {
    // Measure the text the field actually holds by cloning its type face onto a
    // detached span — the input itself reports no text metrics.
    const cs = getComputedStyle(el);
    const probe = document.createElement("span");
    probe.style.font = cs.font;
    probe.style.letterSpacing = cs.letterSpacing;
    probe.style.whiteSpace = "pre";
    probe.style.position = "absolute";
    probe.style.visibility = "hidden";
    probe.textContent = el.value;
    document.body.appendChild(probe);
    const textW = probe.getBoundingClientRect().width;
    probe.remove();
    return {
      textW,
      clientW: el.clientWidth, // inside the border, padding included
      scrollW: el.scrollWidth,
      padL: parseFloat(cs.paddingLeft),
      padR: parseFloat(cs.paddingRight),
    };
  });

  // (1) The whole id is visible — no clipping, nothing to scroll to. This is
  // the half that breaks if someone shrinks the field.
  expect(
    m.scrollW - m.clientW,
    "a complete id must fit without the field scrolling"
  ).toBeLessThanOrEqual(1);

  // (2) …and the box is not much wider than what it holds. This is the half
  // owner actually complained about. The room for the text is the content box
  // minus its padding; allow one character of slack for the caret and rounding.
  const contentW = m.clientW - m.padL - m.padR;
  const perChar = m.textW / FULL_ID.length;
  expect(
    contentW - m.textW,
    "slack between the field and the id it holds (px)"
  ).toBeLessThanOrEqual(perChar * 2);
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
    const cmp = await mount(<RepliesPageStory theme={theme} />);
    await cmp.getByTestId("replies-filter-toggle").click();

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
      // painted `transparent` still passes it — and that is correct, because
      // the contract below is "distinguishable by a border, a fill, or BOTH",
      // and the fill alone satisfies it. Do not read this line as a guard on
      // the border's COLOUR.
      "the field must declare a border width"
    ).toBeGreaterThan(0);
    expect(
      sameColour(border, pageBg) && sameColour(fill, pageBg),
      "the field must differ from the page by a border, a fill, or both"
    ).toBe(false);
  });
}

// 🔴 THE OWNER'S × — the control he asked for by name, measured as a BOX.
//
// WHY THIS TEST EXISTS, and why it is not "one more assertion": round N fixed
// this control's COLOUR and OPACITY (it had rendered as a dot: 12px, 75%,
// accent on accent-soft) and shipped it with `width/height: 18px` but NO
// `padding: 0`. A <button> carries a UA default padding — measured
// 6px/6px/1px/1px in Chromium — and the box is `border-box`, so the content box
// came out 6px wide and the 13px glyph was squeezed back into a vertical line.
// The defect the fix was written to close came back one layer down, and every
// green in the suite stayed green: jsdom applies no UA stylesheet and computes
// no layout, so the unit tests could not see it, and no CT had ever measured
// this control. An independent reviewer of dbef7ff3 found it by looking.
//
// So the assertions below are about the GLYPH and the TARGET, in a real engine:
//   (a) the rendered svg is not collapsed on either axis — this is the one that
//       reddens if the `padding: 0` reset is ever dropped again;
//   (b) the button's own box is the 18px circle the design calls for;
//   (c) the POINTER TARGET clears 24×24 (the ::after), because the comment
//       above the CSS rule promises a 24px minimum and a promise no machine
//       checks is how (a) happened in the first place.
//
// MUTANTS (each applied, run, reverted; file green again after each):
//   * delete `padding: 0` from `.filter-panel__chip-x`  ⇒ (a) reddens —
//     svg width collapses to 6px. This is the ACTUAL historical defect, and it
//     is the reason this test is written against the svg's box rather than the
//     button's: the button stayed 18px throughout, so asserting the BUTTON
//     alone would have passed on the broken code.
//   * delete the `.filter-panel__chip-x::after` rule ⇒ (c) reddens (target
//     stays 18, under the 24 the CSS comment claims).
//   * remove the `<FilterPanel>` from RepliesPage ⇒ nothing to measure; the
//     test errors rather than passing vacuously, same as the rest of this file.
test("the × on a 已篩選 chip is a real box, not a squeezed line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);
  await applyId(cmp, FULL_ID);

  const x = cmp.getByTestId("replies-filter-chip-x").first();
  await expect(x).toBeVisible();

  const m = await x.evaluate((node) => {
    const svg = node.querySelector("svg")!;
    const s = svg.getBoundingClientRect();
    const b = node.getBoundingClientRect();
    const after = getComputedStyle(node, "::after");
    const inset = parseFloat(after.insetBlockStart || "0"); // negative = grown
    return {
      svgW: s.width,
      svgH: s.height,
      btnW: b.width,
      btnH: b.height,
      padding: getComputedStyle(node).padding,
      targetW: b.width - 2 * inset,
      targetH: b.height - 2 * inset,
    };
  });

  // (a) THE GLYPH. 13px nominal; allow a hair for sub-pixel rounding, but a
  // collapsed axis (the 6px historical failure) is nowhere near this.
  expect
    .soft(m.svgW, `the × glyph's width (padding was ${m.padding})`)
    .toBeGreaterThanOrEqual(12);
  expect
    .soft(m.svgH, `the × glyph's height (padding was ${m.padding})`)
    .toBeGreaterThanOrEqual(12);
  // …and it must not be a LINE: the two axes must be within a pixel of each
  // other. A 6×13 passes any single-axis floor you set at 12 on the tall axis.
  expect
    .soft(Math.abs(m.svgW - m.svgH), "the × glyph must be square, not a line")
    .toBeLessThanOrEqual(1);

  // (b) THE VISIBLE CIRCLE — the size owner circled in variant D.
  expect.soft(m.btnW, "the × button's visible width").toBeGreaterThanOrEqual(17);
  expect.soft(m.btnH, "the × button's visible height").toBeGreaterThanOrEqual(17);

  // (c) THE POINTER TARGET — what the CSS comment promises out loud.
  expect
    .soft(m.targetW, "the × pointer target's width (the ::after)")
    .toBeGreaterThanOrEqual(24);
  expect
    .soft(m.targetH, "the × pointer target's height (the ::after)")
    .toBeGreaterThanOrEqual(24);
});

// 🔴 THE SHELL MUST NOT ADD SPACING OF ITS OWN — owner circled the gap it made.
//
// 2026-09-06, c-d6c59eaa1c5a: a phone screenshot of 請示卡頁 with NO filter
// applied, a red ring round the empty band between 請示卡 and 待核准, and one
// line: 「另外怎麼有一段空白」.
//
// Measured rather than guessed: `.filter-panel` carried `margin-bottom: 12px`,
// and BOTH host pages are flex columns that already space their own children
// (`.replies` gap 26px, `.tasks` gap 22px). Nothing collapses a flex gap against
// a margin — they ADD — so the row below sat 38px away on a page whose rhythm is
// 26px. Neither number is wrong on its own, which is exactly why no reviewer saw
// it and no test could: it is only visible as a shape, on a screen.
//
// The rule this pins is the general one, not the number: the HOST owns the
// rhythm between its children, so the distance from this shell to the next row
// must be the host's own gap and nothing more.
//
// MUTANT: put `margin-bottom: 12px` back on `.filter-panel` ⇒ this reddens
// (38 vs 26). Applied, run, reverted.
test("the 篩選 shell adds no spacing of its own: the gap below it is the page's", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="light" />);
  await cmp.locator(".replies__section-title").first().waitFor();

  const m = await cmp.evaluate((root: HTMLElement) => {
    const panel = root.querySelector(".filter-panel") as HTMLElement;
    const host = panel.parentElement as HTMLElement;
    const next = panel.nextElementSibling as HTMLElement;
    return {
      hostGap: parseFloat(getComputedStyle(host).rowGap || "0"),
      actual: next.getBoundingClientRect().top - panel.getBoundingClientRect().bottom,
      panelMarginBottom: getComputedStyle(panel).marginBottom,
      nextMarginTop: getComputedStyle(next).marginTop,
    };
  });

  // Positive control: a host that is not spacing its children would make the
  // comparison below vacuous.
  expect(m.hostGap, "the host must own a real rhythm for this test to mean anything")
    .toBeGreaterThan(0);
  expect(
    m.actual,
    `gap under the 篩選 shell (panel margin-bottom ${m.panelMarginBottom}, next margin-top ${m.nextMarginTop})`
  ).toBeCloseTo(m.hostGap, 0);
});
