// LAYOUT GUARD — the wake snapshot's 「近期聊天」 rows (owner screenshot,
// 2026-08-13).
//
// ── WHY THIS FILE EXISTS, AND WHY IT IS A REAL-BROWSER TEST ──────────────────
// The package that shipped this panel had a GREEN unit suite and an ugly
// screen, and the two facts are the same fact: vitest runs in jsdom, which
// applies no layout engine. `display:flex` never resolves, every
// `getBoundingClientRect()` is all-zeroes, and `@media` never evaluates against
// a viewport. So `.mp-resume__chatrow { display: flex }` laying its meta, its
// body and its fold mark out as three HORIZONTAL siblings — the single fact
// behind all three things the owner circled — was structurally invisible to
// every test in the repo. It could not have been caught there. It has to be
// measured in a browser, which is what this file does.
//
// ── WHAT IT MEASURES, AND WHY THOSE DIMENSIONS ───────────────────────────────
// It measures QUANTITIES THAT MOVE WHEN THE LAYOUT BREAKS, never CSS text.
// Asserting `getComputedStyle(row).flexDirection === "column"` would be a
// restatement of the fix in a second place: it would pass on a stylesheet that
// says the right words and lays out wrongly for some other reason (a stray
// float, a nested flex, a width on a child), and it would fail on a correct
// layout reached by different means. The three assertions below are the three
// complaints, each turned into a distance in pixels:
//
//   ① body.left - title.left ......... the body must start on the section
//                                      title's own left edge. It used to sit to
//                                      the RIGHT of the meta column, which is
//                                      what "標頭靠左、內文卻置中" describes.
//   ② mark.left - itsRow.left ........ the fold mark must sit against the left
//                                      edge of the message it belongs to. It
//                                      used to be the third flex item on the
//                                      row, i.e. pushed to the far right, where
//                                      it read as a separate column.
//   ③ ts.left, per row ............... every row's timestamp must sit in the
//                                      SAME place. It used to depend on how
//                                      much room the names left over: short
//                                      names → inline after the id, long names
//                                      → wrapped onto its own line.
//
// ── BOTH WIDTHS ──────────────────────────────────────────────────────────────
// 390 (phone) and 1280 (desktop). ③ in particular is a width-dependent bug —
// the wrap point moves with the container — so a single-width guard could hold
// at one and miss at the other.
//
// ── MUTANT (verified BOTH directions, see the PR body) ───────────────────────
//   RED:    restore `flex-direction: row` on `.mp-resume__chatrow` (i.e. delete
//           the `flex-direction: column` line, which is the pre-fix stylesheet)
//           → ① and ② go red at both widths.
//   RED:    put the timestamp back inside the wrapping party line (drop the
//           `.mp-resume__chatstamp` wrapper so `.mp-resume__chatts` wraps
//           together with the parties) → ③(a) goes red wherever the names are
//           short enough for it to fit inline, which at 1280 is every row.
//   SILENT: unmutated → all green.
import { test, expect } from "@playwright/experimental-ct-react";
import { ResumeChatRowStory } from "./stories/ResumeChatRowStory";

// Sub-pixel slack. Text boxes do not land on integers, and a 1px difference is
// not what any of these three complaints is about — a broken layout moves these
// distances by tens of pixels, not by one.
const SLACK = 1.5;
/** The card's own left padding + border (member-detail.css .mp-resume__chatrow):
 *  the body sits this far inside its card, by design since T-5ae3. */
const FRAME_INSET = 11;
/** How far the card stack may sit from the section title. The frame's inset is
 *  intended; the original defect indented the body by ~325px at 1280. */
const MAX_FRAME_INDENT = 20;

for (const width of [390, 1280]) {
  test(`${width}px: chat rows are left-aligned, the fold mark hugs its message, and the timestamp does not move`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ResumeChatRowStory />);

    const title = cmp.getByTestId("story-section-title");
    const rows = cmp.getByTestId("mp-resume-chat-row");
    const bodies = cmp.locator(".mp-resume__chatbody");
    const stamps = cmp.getByTestId("mp-resume-chat-ts");
    const mark = cmp.getByTestId("mp-resume-chat-body-omitted");

    await expect(title).toBeVisible();
    await expect(mark).toBeVisible();

    // Anti-vacuity: the fixture really did mount the three rows the assertions
    // below index into. A guard that measured an empty list would pass every
    // one of them.
    await expect(rows).toHaveCount(3);
    await expect(bodies).toHaveCount(3);
    await expect(stamps).toHaveCount(3);

    const box = async (loc: ReturnType<typeof cmp.locator>) => {
      const b = await loc.boundingBox();
      if (!b) throw new Error("element has no box — it is not laid out");
      return b;
    };

    const titleBox = await box(title);

    // ── ① every part of a message shares ONE left edge, and every row agrees
    //      with every other row ─────────────────────────────────────────────
    // Every row, not just the first: the old layout's indent depended on how
    // wide that row's own meta column happened to be, so the rows disagreed
    // with each other as well as with the title.
    //
    // ⚠️ THIS USED TO MEASURE AGAINST THE SECTION TITLE, and it no longer can:
    // T-5ae3 gave each message a VISIBLE FRAME (owner 2026-08-15, screenshot of
    // two messages running into each other), and a framed card cannot both show
    // a boundary and bleed to the section's edge — it is inset by its own
    // padding + border, measured 11px. What the guard was actually written to
    // catch is the OLD row-flex bug, where the body started tens of pixels in
    // from its own row and each row disagreed with the next. That is still
    // caught, and more precisely: the body must sit on ITS OWN ROW's content
    // edge, and every row must land on the same x.
    //
    // The section title is still read — as the OUTER bound. The inset is a
    // small deliberate one, not the ~325px runaway the original defect had, so
    // it is asserted as a bounded range rather than dropped.
    const rowXs: number[] = [];
    for (let i = 0; i < 3; i++) {
      const bodyBox = await box(bodies.nth(i));
      const rowBox = await box(rows.nth(i));
      expect(
        Math.abs(bodyBox.x - rowBox.x - FRAME_INSET),
        `[${width}px] row ${i}: body must sit on its own card's content edge ` +
          `(row.x=${rowBox.x}, body.x=${bodyBox.x}, expected inset ${FRAME_INSET})`,
      ).toBeLessThanOrEqual(SLACK);
      rowXs.push(bodyBox.x);
    }
    for (let i = 1; i < rowXs.length; i++) {
      expect(
        Math.abs(rowXs[i] - rowXs[0]),
        `[${width}px] row ${i} starts at x=${rowXs[i]} but row 0 at ${rowXs[0]} — ` +
          `rows must agree with each other`,
      ).toBeLessThanOrEqual(SLACK);
    }
    // …and the whole stack stays close to the section title: a small frame
    // inset is intended, a runaway indent is the defect this file exists for.
    //
    // 🔴 BOUNDED ON BOTH SIDES. The assertion this replaced compared the body
    // to the title with a two-way |…| ≤ 1.5px, and rewriting it as a one-way
    // ceiling silently dropped the other half: independent review put
    // `margin-left: -60px` on the row and the whole stack walked LEFT out of
    // the section with this file still green. A frame inset is a small POSITIVE
    // number by construction, so the floor is 0 — the cards may not start left
    // of the heading they belong to.
    const indent = rowXs[0] - titleBox.x;
    expect(
      indent,
      `[${width}px] the card stack is indented ${indent}px from ` +
        `the section title — the frame's inset should be small and deliberate`,
    ).toBeLessThanOrEqual(MAX_FRAME_INDENT);
    expect(
      indent,
      `[${width}px] the card stack starts ${-indent}px LEFT of the section ` +
        `title — cards must not escape the section they sit in`,
    ).toBeGreaterThanOrEqual(-SLACK);

    // ── ② the fold mark hugs the message it is talking about ────────────────
    // Measured as a HORIZONTAL distance from its own row's left edge. The old
    // layout put it at the far right of the row, tens of pixels away and
    // reading as a separate column.
    const markBox = await box(mark);
    const markRowBox = await box(rows.nth(2));
    expect(
      Math.abs(markBox.x - markRowBox.x - FRAME_INSET),
      `[${width}px] the fold mark must sit on its own card's content edge ` +
        `(row.x=${markRowBox.x}, mark.x=${markBox.x}, expected inset ${FRAME_INSET})`,
    ).toBeLessThanOrEqual(SLACK);
    // …and it belongs to THAT row, vertically: below its body, above the next
    // row. Without this, a mark that had escaped its row entirely (absolutely
    // positioned at the container's left edge, say) would satisfy the x test.
    const thirdBody = await box(bodies.nth(2));
    expect(
      markBox.y,
      `[${width}px] the fold mark must sit under the body it describes`,
    ).toBeGreaterThanOrEqual(thirdBody.y + thirdBody.height - SLACK);
    expect(
      markBox.y,
      `[${width}px] the fold mark must stay inside its own row`,
    ).toBeLessThanOrEqual(markRowBox.y + markRowBox.height + SLACK);

    // ── ③ the timestamp is in the same place on every row ───────────────────
    // The fixture deliberately mixes short and long party names, which is what
    // used to move it. The invariant is stated in the two ways it actually
    // failed, and NOT as "same offset from the row top" — that would be a
    // stricter thing than the owner asked for and a thing no layout can
    // promise: when the names are long enough the party line wraps to two
    // lines and everything under it legitimately moves down. What must not
    // vary is WHICH LINE the timestamp is on and where that line starts.
    const parties = cmp.locator(".mp-resume__chatparties");
    await expect(parties).toHaveCount(3);
    const stampX: number[] = [];
    for (let i = 0; i < 3; i++) {
      const s = await box(stamps.nth(i));
      const r = await box(rows.nth(i));
      const p = await box(parties.nth(i));
      stampX.push(s.x - r.x);
      // (a) ALWAYS ON ITS OWN LINE — strictly below the party line, never
      // inline after the id. This is the half that discriminates: in the old
      // layout a short-named row put the timestamp on the SAME baseline as the
      // ids, so `s.y` and `p.y` were equal and this fails.
      expect(
        s.y,
        `[${width}px] row ${i}: the timestamp must sit on its own line below ` +
          `the parties, not inline after the id (parties.y=${p.y} h=${p.height}, ts.y=${s.y})`,
      ).toBeGreaterThanOrEqual(p.y + p.height - SLACK);
    }
    // (b) …and that line always starts at the same place across rows.
    for (let i = 1; i < stampX.length; i++) {
      expect(
        Math.abs(stampX[i] - stampX[0]),
        `[${width}px] every row's timestamp must start at the same x ` +
          `(row0=${stampX[0]}, row${i}=${stampX[i]})`,
      ).toBeLessThanOrEqual(SLACK);
    }

    // ── anti-vacuity for ③ ──────────────────────────────────────────────────
    // "All three timestamps agree" is also satisfied by three rows that are
    // identical, which would make the mixed-name fixture pointless. Prove the
    // fixture really is asymmetric where it claims to be: the two party lines
    // have DIFFERENT widths, so the timestamps agreeing is a fact about the
    // layout and not about the data.
    const from0 = await box(cmp.getByTestId("mp-resume-chat-from").nth(0));
    const from1 = await box(cmp.getByTestId("mp-resume-chat-from").nth(1));
    expect(
      Math.abs(from1.width - from0.width),
      "fixture must exercise short AND long party names",
    ).toBeGreaterThan(10);

    // ── page-level: none of this introduced sideways scroll ─────────────────
    const overflow = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth,
    );
    expect(
      overflow,
      `[${width}px] the chat block must not scroll sideways`,
    ).toBeLessThanOrEqual(1);
  });
}
