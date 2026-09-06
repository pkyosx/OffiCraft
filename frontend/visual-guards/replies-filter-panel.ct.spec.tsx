// T-118 — the list pages' 篩選列, measured in a real browser, on the real page.
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
// The fix is structural, not a bigger assertion count: THE PANEL COMES FROM
// `RepliesPage`. `RepliesPageStory` mounts the shipped page and this file drives
// it the way the owner does. If you ever replace a mount here with hand-built
// markup "to make the test simpler", you are rebuilding the exact defect this
// file was rewritten to fix.
//
// 🔁 WHAT T-118 CHANGED, AND WHY EVERY ASSERTION BELOW MOVED RATHER THAN DIED.
// owner 2026-09-06 20:07 (c-c3d681fe05da), with a screenshot, restated for this
// page at 20:19 (c-38c7759e6377):
//
//   「我想改一下,不要多filter那一層了,全部拉出來,而任務編號那邊就是按enter
//     或是點外面就視為apply了,然後也不用再顯示14筆已篩選跟那一行跟案件那個
//     子標了,案件跟請示卡都一樣」
//
// So the funnel toggle, the expanding `.filter-panel__form`, the 取消／套用篩選
// row, the 已篩選 strip with its chips and its ×, the 清除全部 button and the
// 「請示卡」 header row ALL STOPPED EXISTING. The gesture this file drove —
// 「click the funnel, type an id, press 套用篩選」— is now 「type an id, press
// Enter」 (or click away). Each assertion below carries a marker saying what it
// used to hold and who overturned it.
//
// WHAT IS ASSERTED IS GEOMETRY AND COMPUTED COLOUR — things the owner can see:
//   (1) the 篩選 row and its fields stay INSIDE the content column and do not
//       push the page into horizontal scrolling, at phone widths. 320px is
//       where that is tightest.
//       🔁 WAS the same question asked of `.filter-panel__form`, the 已篩選
//       strip, the header row and the action row. Three of those four are gone
//       (owner 2026-09-06); the row and its `__fields` box are what is left.
//   (2) the ID field is FINDABLE against the page behind it — it must differ
//       from its surroundings by a border, a fill, or both. Asserted as "the
//       composited border colour is not equal to the composited page
//       background", in BOTH theme families. UNCHANGED by T-118 except that
//       there is no longer a funnel to click before it is on screen.
//   (3) every CJK label on the row keeps its glyphs on ONE line box inside its
//       own box. (The 預設/編輯 pills in set-badge-nowrap burst exactly this
//       way; CJK min-content is ONE CHARACTER — see frontend/.claude/rules/
//       css-layout-traps.md.)
//       🔁 WAS asserted on 取消 / 套用篩選 / 清除全部 / 篩選 — every one of those
//       four labels was deleted by owner 2026-09-06. The CJK labels that
//       survive on this shared row are the 任務頁's dropdown triggers (負責人 /
//       類型 / 狀態), so the assertion moved to the test that mounts them.
//   (4) 🔁 GONE WITH ITS SUBJECT: 「the 取消 / 套用篩選 row stays inside the form
//       box and on ONE row」. Both buttons were removed by owner 2026-09-06.
//       Nothing replaces it, because nothing replaces them — the field commits
//       itself now.
//   (5) several conditions on the shared row WRAP inside the column rather than
//       running out of it, at the narrowest phone width.
//       🔁 WAS asked of the four 已篩選 CHIPS. The strip is gone (owner
//       2026-09-06); the thing that now carries several conditions at once is
//       the FIELDS, so the same question is asked of them.
//   (6) 🔴 THE 篩選 ROW IS NOT AN OVERLAY. It is page content: the list starts
//       BELOW it, and neither the row nor its fields box is
//       `fixed`/`absolute`/`sticky`. owner c-3b5a0aa66550 ruled 「按搜尋時不要再
//       跳出新modal」 AFTER being shown the overlay version, and c-c3d681fe05da's
//       「全部拉出來」 is the opposite of floating it — two separate rulings, and
//       this is their only mechanical witness. jsdom cannot hold it: it computes
//       no layout, so an overlaid row and an in-flow one look identical to every
//       unit test we have.
//       🔁 WAS witnessed by 「opening the panel MOVES the list down」. There is
//       no opening any more, so the witness is the row's permanent place in the
//       flow instead of its effect on appearing.
//   (7) 🆕 the affordances owner removed are really absent in a real browser,
//       and the field is the escape that replaced them.
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
// MUTANTS. The log that used to sit here was DISCARDED, not carried forward:
// every mutant in it was applied against the previous shape (`__form`,
// `__toggle`, `__summary`, `__btn`, `__chip-x`) — CSS rules that no longer
// exist in filter-panel.css and testids no component renders, so keeping those
// notes would have been a stale claim of teeth. The four below were applied,
// run and REVERTED against the shape this file now measures (the file is green
// again after each: 11 passed):
//
// -- IS THE GUARD ATTACHED TO THE PRODUCT? ---------------------------------
//   * FilterPanel.tsx: `return null` from the component => 11 failed, 0 passed.
//     Every test here dies at "element(s) not found" for `replies-filter` /
//     `replies-filter-fields` / the field itself.
//   * RepliesPage.tsx: delete the `<FilterPanel>` element, leaving its
//     `<IdFilterInput>` rendered bare => 7 failed, 4 passed. The four survivors
//     are the two theme-contrast tests, the field-width test and the
//     several-field wrap test — correctly so: the first three ask only about
//     the FIELD (which the mutant leaves on the page) and the last is the one
//     test that mounts `FilterPanel` directly because the single-axis 請示 page
//     cannot produce a multi-field row. THE OLD VERSION OF THIS GUARD COULD NOT
//     FEEL THIS MUTANT AT ALL — that is why it stayed green for a row no page
//     rendered.
//
// -- DO THE ASSERTIONS HAVE TEETH? (CSS mutants, all in filter-panel.css) ---
//   * `.filter-panel { position: fixed; top: 0; left: 0 }` => 2 failed. (6)
//     reds on both of its soft halves — the computed `position` check, and the
//     list no longer starting below the row — and the spacing guard reds too,
//     because a row taken out of flow leaves no gap to measure. The three width
//     tests stay GREEN, and that is honest rather than a hole: a fixed row at
//     `left: 0` is still inside a viewport-wide column, so "does it overflow"
//     genuinely cannot see it. (6) is the assertion that can, and it does.
//   * `.filter-panel__fields { flex-wrap: wrap }` -> `nowrap`, together with
//     the <=520px `flex: 1 1 100%` -> `flex: 0 0 auto` => 1 failed: (5), on
//     「distinct rows the 篩選 fields occupy」. The soft halves report together,
//     which is what they are soft for.
//
// ⚠️ NOT RE-RUN, and stated rather than implied: nothing here re-verifies the
// UNIT-level guards, and no mutant was applied to `idFilter.css` — the
// field-width test's teeth are inherited from the previous round's log and have
// not been re-measured against this shape.
//
// ⚠️ NOT COVERED HERE, and say so rather than implying otherwise: the 任務頁's
// own page has no mount in this file. What is guarded here is the SHELL
// (through the 請示 page) plus the multi-field row; a 任務頁 GEOMETRY guard, if
// anyone wants one, is a second file that drives that page.
//
// But be precise about how big that gap is, because "not covered" reads bigger
// than it is: the 任務頁's WIRING is guarded, just not in a browser. Measured,
// not assumed — renaming that page's `testId="tasks-filter"` (a mutant that
// COMPILES, unlike deleting the element, which only produces a transform error
// and therefore proves nothing) reddens **37 assertions** across
// `TasksPage.test.tsx` / `.id-filter` / `.jump` and the TaskCard suites. So a
// 任務頁 that stops rendering the panel is caught immediately; what nobody
// measures is whether that page's row FITS and stays in flow.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import {
  RepliesPageStory,
  FilterFieldsStory,
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
 * ⚠️ A Range over the whole element would count a caret `<svg>` as a line box,
 * so this walks TEXT NODES only, and counts DISTINCT line tops rather than
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

/** Drive the real page the way the owner does.
 *
 * 🔁 WAS 「funnel → type → 套用篩選」, and it waited for the 已篩選 strip to prove
 * the filter had landed. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da): there
 * is no funnel, no button and no strip. The commit is Enter, and the landing is
 * witnessed by the list itself — an id the mock does not hold is a 404, which
 * renders the ordinary filtered-empty result. */
async function applyId(cmp: Locator, id: string) {
  await cmp.getByTestId("filter-reply-card-id").fill(id);
  await cmp.getByTestId("filter-reply-card-id").press("Enter");
  await expect(cmp.getByTestId("filter-reply-card-id")).toHaveValue(id);
  await expect(cmp.getByTestId("replies-empty")).toBeVisible();
}

// 320 = the narrowest phone still in use, and the width where the row is
// tightest. 390 = the phone width the rest of this suite treats as the owner's.
// 1040 = the desktop content column's max width, the control that says a
// narrow-width fix did not move the breakage to desktop.
for (const width of [320, 390, 1040]) {
  test(`width ${width}: the 篩選列 fits inside the content column`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<RepliesPageStory theme="dark" />);

    // 🔁 WAS 「apply a filter, then RE-OPEN, because that is the only state in
    // which the form, the 已篩選 strip and the header row are all on screen at
    // once」. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da): every field is
    // permanently on screen, so the whole row is measurable from first paint.
    // A filter is still applied afterwards, because a field HOLDING a full id
    // is wider than an empty one and that is the tighter case.
    const panel = cmp.getByTestId("replies-filter");
    const fields = cmp.getByTestId("replies-filter-fields");
    const field = cmp.getByTestId("filter-reply-card-id");
    for (const el of [panel, fields, field]) {
      await expect(el).toBeVisible();
    }
    await applyId(cmp, FULL_ID);

    // (1) Nothing escapes the row's boxes, and the page does not scroll
    // sideways.
    for (const [name, el] of [
      ["篩選 row", panel],
      ["fields box", fields],
    ] as const) {
      const o = await overflow(el);
      expect(o.self, `${name} horizontal overflow`).toBeLessThanOrEqual(1);
      expect(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);
    }

    const fieldsBox = (await fields.boundingBox())!;
    const fieldBox = (await field.boundingBox())!;
    expect(
      fieldBox.x + fieldBox.width,
      "ID field right edge vs the fields box"
    ).toBeLessThanOrEqual(fieldsBox.x + fieldsBox.width + 1);
    expect(
      fieldBox.x,
      "ID field left edge vs the fields box"
    ).toBeGreaterThanOrEqual(fieldsBox.x - 1);
  });
}

// (5) + (3) SEVERAL CONDITIONS ON THE SHARED ROW.
//
// 🔁 REPLACES 「width 320: several 已篩選 chips WRAP inside the strip, and 清除
// 全部 stays reachable」. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da):
// 「也不用再顯示14筆已篩選跟那一行」— the strip, its chips and 清除全部 are gone,
// so there is nothing left to lay four chips out from. The QUESTION is
// unchanged: several conditions sharing one row at the narrowest phone width
// must WRAP inside the column rather than run out of it. What carries several
// conditions now is the FIELDS.
//
// ⚠️ This is the ONE test here that does not drive the 請示 page: that page
// filters on a single axis and cannot produce a multi-field row. It mounts the
// shipped `FilterPanel` and the shipped `MultiSelectFilter` with a dictated
// option list — see the story's second export for why that trade is made here
// and nowhere else.
test("width 320: several 篩選 fields WRAP inside the row, and their CJK labels keep to one line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const cmp = await mount(<FilterFieldsStory theme="dark" />);

  const fields = cmp.getByTestId("replies-filter-fields");
  await expect(fields).toBeVisible();

  // 🔴 soft from here down, ON PURPOSE: `flex-wrap: nowrap` (or dropping the
  // ≤520px full-width rule) breaks the row in several places at once, and a
  // hard first failure would leave the others unproven. "which parts of the row
  // broke" is the useful output.
  const o = await overflow(fields);
  expect.soft(o.self, "篩選 fields horizontal overflow").toBeLessThanOrEqual(1);
  expect.soft(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);

  // WRAP, not one runaway row: an id box plus three dropdown triggers cannot
  // share one line in a ~240px column, so if they all report the same top the
  // row is overflowing rather than wrapping.
  const rows = await fields.evaluate(
    (node) =>
      new Set(
        Array.from(node.children).map((n) =>
          Math.round(n.getBoundingClientRect().top)
        )
      ).size
  );
  expect.soft(rows, "distinct rows the 篩選 fields occupy").toBeGreaterThan(1);

  const fieldsBox = (await fields.boundingBox())!;
  for (const testId of ["filter-executor", "filter-type", "filter-status"]) {
    const trigger = cmp.getByTestId(testId);
    const box = (await trigger.boundingBox())!;
    expect.soft(
      box.x + box.width,
      `${testId} right edge vs the fields box`
    ).toBeLessThanOrEqual(fieldsBox.x + fieldsBox.width + 1);

    // (3) The CJK labels that survive on this row, on ONE line box, inside
    // their own box.
    const geo = await labelGeometry(trigger);
    expect.soft(geo.lines, `${testId} label line boxes`).toBe(1);
    expect
      .soft(geo.spillAbove, `${testId} label spilling above its trigger`)
      .toBeLessThanOrEqual(0.5);
    expect
      .soft(geo.spillBelow, `${testId} label spilling below its trigger`)
      .toBeLessThanOrEqual(0.5);
  }
});

// 🔴 (6) THE 篩選列 IS NOT AN OVERLAY.
//
// 🔁 WAS witnessed by 「opening it moves the list down」. OVERTURNED BY owner
// 2026-09-06 (c-c3d681fe05da):「全部拉出來」— there is no opening left to
// observe. The RULE is not overturned; it is doubly held (c-3b5a0aa66550,
// 「按搜尋時不要再跳出新modal」, said after he was shown the overlay version).
// So the witness becomes the row's permanent place in the flow: the list starts
// below it, and nothing here is taken out of flow. A `position: fixed` row
// leaves the list starting at the top of the column, underneath it.
test("the 篩選列 is page content, not an overlay: the list starts below it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);

  const panel = cmp.getByTestId("replies-filter");
  const below = cmp.locator(".replies__section").first();
  await expect(panel).toBeVisible();
  await expect(below).toBeVisible();

  // Neither the shell nor its fields box may be taken out of flow.
  const positions = await panel.evaluate((node) => ({
    row: getComputedStyle(node).position,
    fields: getComputedStyle(node.querySelector(".filter-panel__fields")!)
      .position,
  }));
  // 🔴 soft, ON PURPOSE: an overlay breaks BOTH halves of this test (out of
  // flow AND the list no longer starts below it), and a hard failure on the
  // first would hide whether the second still has teeth. "one red" and "both
  // red" have to be distinguishable in the output.
  for (const [name, pos] of Object.entries(positions)) {
    expect.soft(
      ["fixed", "absolute", "sticky"],
      `the ${name} must stay in page flow, not float over it`
    ).not.toContain(pos);
  }

  const panelBox = (await panel.boundingBox())!;
  const belowBox = (await below.boundingBox())!;
  expect.soft(
    panelBox.height,
    "the row must have real height for this test to mean anything"
  ).toBeGreaterThan(0);
  expect.soft(
    panelBox.y + panelBox.height,
    "the row's bottom edge vs the list below it"
  ).toBeLessThanOrEqual(belowBox.y + 1);
});

// The state the loop above cannot reach: the field is EMPTY while a filter is
// still APPLIED — the owner cleared the box but has not pressed Enter and has
// not clicked away, so the list is still narrowed beside an empty field.
// Narrowest width only: that is where a row which fits when full could still
// break with a placeholder in the box.
//
// 🔁 KEPT — only the gesture and the surviving boxes moved. It used to reach
// this state through the funnel and prove it by the 清除全部 still on screen;
// owner 2026-09-06 removed both, and typing-without-committing is what keeps
// the state reachable at all (`onCommit` fires on Enter and blur, and on
// nothing else).
test("width 320: the row still fits with an empty field and a filter still applied", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);

  await applyId(cmp, FULL_ID);
  const field = cmp.getByTestId("filter-reply-card-id");
  // `fill("")` types without committing: no Enter, and focus stays in the box.
  await field.fill("");
  await expect(field).toHaveValue("");
  // The filter is still on — this is the whole point of the state.
  await expect(cmp.getByTestId("replies-empty")).toBeVisible();

  for (const [name, sel] of [
    ["篩選 row", ".filter-panel"],
    ["fields box", ".filter-panel__fields"],
  ] as const) {
    const o = await overflow(cmp.locator(sel));
    expect(o.self, `${name} horizontal overflow`).toBeLessThanOrEqual(1);
    expect(o.page, "page horizontal overflow").toBeLessThanOrEqual(1);
  }
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
//
// 🔁 KEPT — only the funnel click before it is gone (owner 2026-09-06).
test("the field is sized to the id it holds — a whole id fits, with little to spare", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1040, height: 900 });
  expect(FULL_ID.length, "the id shape this guard is calibrated on").toBe(15);
  const cmp = await mount(<RepliesPageStory theme="dark" />);
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
//
// 🔁 KEPT — only the funnel click before it is gone (owner 2026-09-06).
for (const theme of ["dark", "light"] as const) {
  test(`theme ${theme}: the ID field is distinguishable from the page behind it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1040, height: 900 });
    const cmp = await mount(<RepliesPageStory theme={theme} />);

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

// (7) 🔁 REPLACES 「the × on a 已篩選 chip is a real box, not a squeezed line」.
//
// WHAT THAT TEST HELD, AND WHY IT CANNOT BE KEPT: round N found that the chip's
// × had shipped without `padding: 0`, so a UA default squeezed its 13px glyph
// into a 6px vertical line — a defect every unit test missed because jsdom
// applies no UA stylesheet. OVERTURNED BY owner 2026-09-06 (c-c3d681fe05da):
// 「也不用再顯示14筆已篩選跟那一行」. The chip, its ×, and the strip they lived on
// were all deleted, so there is no box left to measure.
//
// What survives is the REQUIREMENT the × served: the owner must have a way OUT
// of a filter he did not mean to apply, and it must be present on screen rather
// than remembered. That is now the 編號 box itself — empty it and commit. So
// this test pins, in a real browser, that the removed affordances are really
// absent (a leftover would be a second way to say one thing) and that the
// replacement escape actually works end to end.
test("the removed 已篩選 affordances are gone, and emptying the 編號 box is the escape that replaced them", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<RepliesPageStory theme="dark" />);

  await expect(cmp.getByTestId("replies-filter")).toBeVisible();
  // ⚠️ The escape is witnessed through the EMPTY-STATE SENTENCE, not through a
  // card count: this story's mock seam serves no waiting cards, so "the list
  // came back" has nothing to count. The two sentences are a real, load-bearing
  // distinction of their own (「你回完了」 vs 「還有卡,只是沒有一張符合」), and
  // they flip in exactly the two directions this test needs.
  const empty = cmp.getByTestId("replies-empty");
  await expect(empty).toHaveText("✓ 目前沒有待處理的請示");

  for (const testId of [
    "replies-filter-toggle",
    "replies-filter-form",
    "replies-filter-apply",
    "replies-filter-cancel",
    "replies-filter-summary",
    "replies-filter-chip",
    "replies-filter-chip-x",
    "replies-filter-clear",
  ]) {
    await expect(
      cmp.getByTestId(testId),
      `${testId} must not exist any more`
    ).toHaveCount(0);
  }
  for (const sel of [".filter-panel__header", ".filter-panel__title"]) {
    await expect(
      cmp.locator(sel),
      `${sel} — the 請示卡 sub-title row must not exist any more`
    ).toHaveCount(0);
  }

  // The escape, end to end: apply an id nothing matches (the page says so), then
  // empty the box and commit by clicking away (「點外面」) — the page must go
  // back to speaking as if no filter had ever been applied.
  await applyId(cmp, FULL_ID);
  await expect(empty).toHaveText("沒有符合篩選條件的請示");

  const field = cmp.getByTestId("filter-reply-card-id");
  await field.fill("");
  await field.blur();
  await expect(empty).toHaveText("✓ 目前沒有待處理的請示");
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
// 🔁 KEPT verbatim — this test never touched the funnel, and T-118 did not
// change the rule. (Its recorded mutant — put `margin-bottom: 12px` back on
// `.filter-panel` ⇒ 38 vs 26 — was run against the previous shape and has NOT
// been re-run; see the mutant note in this file's header.)
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
