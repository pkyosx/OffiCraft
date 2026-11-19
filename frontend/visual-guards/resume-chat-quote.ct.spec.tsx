// LAYOUT GUARD — the quote strip on the wake snapshot's chat rows (T-9871).
//
// ── WHY A REAL BROWSER ───────────────────────────────────────────────────────
// Everything this file asserts is geometry, and jsdom has none: no flex, no
// wrapping, no container queries, every box all-zeroes. The unit suite can
// prove the quote's TEXT is on screen and is structurally unable to notice that
// it is 200px wider than the card it sits in.
//
// ── THE TRAP THIS EXISTS FOR ─────────────────────────────────────────────────
// The chat pane's quote row is built for a chat bubble: one ellipsised line of
// names, and the whole row a click target. This card is rendered by the member / worker
// DETAIL PANEL, which is NARROWER than the chat pane, and anything copied from
// the bubble's row is silently unprotected here. Nothing goes red; the strip
// just runs past the card's edge. So this row has no control, and its own
// answers (wrap the parties, break the excerpt, stretch to the card) are
// measured here at both ends of the width range.
//
// ── ORDERING ─────────────────────────────────────────────────────────────────
// The per-surface assertions come BEFORE the page-level scroll one on purpose:
// a page-level failure aborts the test, and an aborted test never runs the
// assertions under it — reading "it failed" as "the ones below were checked" is
// how a mutant gets credited to the wrong line.
//
// ── MUTANTS (each verified to redden ITS OWN assertion, in this order) ───────
//   drop `overflow-wrap: anywhere` from `.mp-resume__chatquotebody`
//        → ③ 「the excerpt is not breaking」, +105px at 320px, 1280 green.
//   `white-space: nowrap` on `.mp-resume__chatquoteparties`
//        → ② 「the quoted parties line does not fit its own box」, +27px at 320px.
//        (② is measured BEFORE ③ for this reason: ③ contains it and would
//        otherwise swallow the attribution.)
//   move the quote JSX below the message body
//        → ④ 「the quote must sit above the message body」, at BOTH widths.
//
//   SILENT, and recorded rather than papered over: `flex-wrap: nowrap` on the
//   parties line, and deleting `align-self: stretch` from the strip. Neither
//   moves anything — the party spans shrink and their text wraps on its own,
//   and a breakable excerpt's min-content is one character. So this file does
//   NOT hold those two declarations; the CSS says so beside them.
import { test, expect } from "@playwright/experimental-ct-react";
import { ResumeChatQuoteStory } from "./stories/ResumeChatQuoteStory";

// Sub-pixel slack. Text boxes do not land on integers; every defect this file
// is about moves things by tens of pixels.
const SLACK = 1.5;
/** `.mp-resume__chatrow`'s own left padding + border (member-detail.css). */
const FRAME_INSET = 11;

// 320 is the narrow end this card actually meets — the detail panel is a side
// panel, so it is NARROWER than the chat pane the quote line was designed in,
// which is the whole reason the pane's own narrow-width rule is no help here.
for (const width of [320, 1280]) {
  test(`${width}px: the quote strip stays inside its card and above the message it answers`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<ResumeChatQuoteStory />);

    const rows = cmp.getByTestId("mp-resume-chat-row");
    const quotes = cmp.getByTestId("mp-resume-chat-quote");
    const bodies = cmp.locator(".mp-resume__chatbody");

    // Anti-vacuity: the fixture really mounted three rows, two of which are
    // replies. Every assertion below indexes into these.
    await expect(rows).toHaveCount(3);
    await expect(quotes).toHaveCount(2);
    await expect(bodies).toHaveCount(3);
    // …and the third row draws no strip at all: a quote slot on a message that
    // is not a reply would read as a quote that failed to load.
    await expect(
      rows.nth(2).getByTestId("mp-resume-chat-quote"),
    ).toHaveCount(0);

    const box = async (loc: ReturnType<typeof cmp.locator>) => {
      const b = await loc.boundingBox();
      if (!b) throw new Error("element has no box — it is not laid out");
      return b;
    };
    /** How far this element's CONTENT sticks out of its own box.
     *
     * 🔴 A BOUNDING BOX CANNOT ANSWER THIS, and that is why the helper exists.
     * The strip is `align-self: stretch`, so its box is the card's content
     * width whatever happens inside it: an excerpt that will not break, or a
     * parties line that will not wrap, spills INK past the frame while
     * `boundingBox()` keeps reporting the tidy width it was given. Measured
     * with the mutants below — dropping `overflow-wrap` moved this by 105px at
     * 320px and moved every bounding box by nothing at all. */
    const inkOverflow = (loc: ReturnType<typeof cmp.locator>) =>
      loc.evaluate((el) => el.scrollWidth - el.clientWidth);

    const sectionBox = await box(cmp.getByTestId("story-section"));

    for (let i = 0; i < 2; i++) {
      const rowBox = await box(rows.nth(i));
      const qBox = await box(quotes.nth(i));

      // ── ① the strip starts on its card's content edge ────────────────────
      // Same left edge as the body and the fold mark: every part of a message
      // shares one edge (the contract resume-chat-row-align.ct.spec.tsx holds
      // for the parts that existed before this one).
      expect(
        Math.abs(qBox.x - rowBox.x - FRAME_INSET),
        `[${width}px] row ${i}: the quote must start on its card's content ` +
          `edge (row.x=${rowBox.x}, quote.x=${qBox.x})`,
      ).toBeLessThanOrEqual(SLACK);

      // ── ② THE NAMES FIT, at whatever length. Asserted on the parties line
      //      ITSELF and BEFORE the whole-strip measurement below, because the
      //      strip contains it: a single "the strip overflows" assertion would
      //      go red for either cause and name neither. The names have to
      //      give way on their own by wrapping.
      const parties = quotes.nth(i).locator(".mp-resume__chatquoteparties");
      if ((await parties.count()) > 0) {
        expect(
          await inkOverflow(parties),
          `[${width}px] row ${i}: the quoted parties line does not fit its own ` +
            `box`,
        ).toBeLessThanOrEqual(1);
        const pBox = await box(parties);
        expect(
          pBox.x + pBox.width,
          `[${width}px] row ${i}: the quoted parties line runs past the ` +
            `quote's own box (parties.right=${pBox.x + pBox.width}, ` +
            `quote.right=${qBox.x + qBox.width})`,
        ).toBeLessThanOrEqual(qBox.x + qBox.width + SLACK);
      }

      // ── ③ …and its CONTENT ends inside it. This is the assertion the
      //      unbreakable 60-rune excerpt is in the fixture for: an excerpt with
      //      no break opportunity runs out through the frame's right edge.
      expect(
        await inkOverflow(quotes.nth(i)),
        `[${width}px] row ${i}: the quote's content sticks out of the quote's ` +
          `own box — the excerpt is not breaking`,
      ).toBeLessThanOrEqual(1);
      // …and the strip's own box ends inside the card too. Same edge, the other
      // failure mode: a strip that took fit-content instead of stretching.
      expect(
        qBox.x + qBox.width,
        `[${width}px] row ${i}: the quote runs past its card's right edge ` +
          `(quote.right=${qBox.x + qBox.width}, row.right=${rowBox.x + rowBox.width})`,
      ).toBeLessThanOrEqual(rowBox.x + rowBox.width - FRAME_INSET + SLACK);
      // …and the card itself has not been widened past the section it sits in,
      // which is what a non-shrinking child does to a column-flex parent with
      // `align-items: flex-start` (T-4aa0).
      expect(
        rowBox.x + rowBox.width,
        `[${width}px] row ${i}: the card is wider than its section ` +
          `(row.right=${rowBox.x + rowBox.width}, section.right=${sectionBox.x + sectionBox.width})`,
      ).toBeLessThanOrEqual(sectionBox.x + sectionBox.width + SLACK);
    }

    // ── ④ the quote sits ABOVE the message body, and inside its own row ─────
    // A quote printed under the reply reads as a postscript to it rather than
    // as the thing being answered.
    for (let i = 0; i < 2; i++) {
      const qBox = await box(quotes.nth(i));
      const bBox = await box(bodies.nth(i));
      const rowBox = await box(rows.nth(i));
      expect(
        qBox.y + qBox.height,
        `[${width}px] row ${i}: the quote must sit above the message body ` +
          `(quote.bottom=${qBox.y + qBox.height}, body.y=${bBox.y})`,
      ).toBeLessThanOrEqual(bBox.y + SLACK);
      expect(
        qBox.y,
        `[${width}px] row ${i}: the quote must stay inside its own card`,
      ).toBeGreaterThanOrEqual(rowBox.y - SLACK);
    }

    // ── page-level, LAST: none of this introduced sideways scroll ───────────
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
