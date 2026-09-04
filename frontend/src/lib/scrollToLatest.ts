// lib/scrollToLatest.ts — land the chat thread on its NEWEST message.
//
// The defect it exists to close, measured in the isolated environment:
// THE OLD TARGET WAS THE WRONG ROW. The 「有新訊息」 chip scrolled to
// `newMsgAnchorId` — the FIRST unseen message — so a burst of ten arrivals left
// the reader on message 1 with five more still below the fold. The divider
// marks where the unread block STARTS; the jump is for reaching the END of it.
//
// ⚠️ NOT SMOOTH, deliberately. An animation is interrupted and restarted by
// every reflow, which reads as the thread lurching. The hash jump is instant
// for the same reason.
//
// 🔴 IT DOES NOT CORRECT THE LANDING, AND THAT IS A SIGNED DECISION (T-48,
// owner rc-6c27f486ef9d 「拿掉。圖片／卡片展開把目標擠走我接受」). This function
// used to hold a ResizeObserver that re-scrolled for 2.6s so that content above
// the target growing late — an image decoding to its real height, an inline
// reply card refetching — could not push the row back out of view. Measured
// cost of removing it: the newest row ends up 418px below the fold on a 433px
// viewport. The owner named that cost and took it.
//
// ⇒ DO NOT ADD A CORRECTION LOOP BACK HERE. Three attempts died on the same
// rock: a loop that writes `scrollTop` cannot tell its own re-scroll from the
// reader's, so it either yanks a reader who has moved on or gives up on the
// case it was written for. What ships instead is a PURE-READ ResizeObserver in
// ChatArea that only re-answers {@link isLatestRowInView}, so the 回到最新 arrow
// comes back rather than the viewport being dragged. An observer that only
// reads and a loop that writes are not interchangeable.
//
// (The module used to name a third source, "markdown 重排". It was never real —
// measured 0px across code blocks, tables and long quotes, byte-identical at
// t+0/+300ms/+2300ms — so it is not repeated here.)

/**
 * Scroll `scroller` so the LAST `[data-msg-id]` row it contains is fully
 * visible. One scroll, no follow-up: see the note above.
 */
export function scrollToLatest(scroller: HTMLElement): void {
  const rows = scroller.querySelectorAll<HTMLElement>("[data-msg-id]");
  const latest = rows[rows.length - 1];
  if (!latest) return;
  latest.scrollIntoView({ block: "end" });
}

// ─────────────────────────────────────────────────────────────────────────────

/** The only tolerance left in the newest-row test, and it is sized against
 * FRACTIONAL PIXELS, not against anything in the layout. `getBoundingClientRect`
 * returns sub-pixel values and a scroll position lands on a fraction of a device
 * pixel, so a row that is exactly flush measures as ±0.5px off; measured
 * residues on the settled landing are +0.13 (1280px) and -0.50 (390px).
 *
 * 🔴 IT IS NOT ALLOWED TO GROW TO COVER A LAYOUT DISTANCE, and after
 * {@link isLatestRowInView} it cannot need to: the gap, the padding and the
 * zero-height sentinel below the last row are no longer inside the quantity
 * being compared. The number this replaced (`AT_LATEST_PX = 4` in ChatArea) was
 * exactly that mistake — it was measured against the container's bottom, so the
 * 12px flex gap sat inside the distance and 4px could never absorb it, and the
 * comment beside it asserted that it did. Anyone tempted to raise this to clear
 * a gap is re-creating that bug with a bigger number: fix the measurement, not
 * the tolerance. */
const SUBPIXEL_PX = 1;

/**
 * Is the NEWEST message row inside `scroller`'s viewport?
 *
 * 🔴 THIS IS NOT "IS THE BOX SCROLLED TO THE BOTTOM", and the difference is a
 * shipped bug (T-48). The owner's condition for the 回到最新 arrow is 「不在最新
 * 訊息時有個向下箭頭」 — a question about a ROW. The box's own bottom answers a
 * different question, because the box holds things that are not the newest row:
 * `.chat__messages` is a flex column with `gap: 12px` and ChatArea renders a
 * zero-height `endRef` sentinel after the last message, so the newest row's
 * bottom sits 12px ABOVE the scrollable bottom. `scrollToLatest` lands the row
 * flush with the viewport — the honest landing — and a container-bottom test
 * then reported 12px of "still below the fold" and put the arrow back, every
 * single time, on a viewport where the newest message was fully visible
 * (measured, 12/12 runs across both widths: `lastRowBottomGap` 0.13/-0.50 while
 * the container distance read 12/11).
 *
 * Measuring the ROW instead of the BOX also deletes the fact that used to rot:
 * nothing here knows or cares what the gap is, so changing it in CSS cannot
 * silently break the arrow. The e2e probe that pins this runs the same flow with
 * `gap: 40px` forced on, and it is the case that stays green.
 *
 * Returns true for an empty thread — there is no newest row to be out of view.
 */
export function isLatestRowInView(scroller: HTMLElement): boolean {
  const rows = scroller.querySelectorAll<HTMLElement>("[data-msg-id]");
  const latest = rows[rows.length - 1];
  if (!latest) return true;
  // The same row `scrollToLatest` scrolls to — the two must never disagree
  // about which row "the latest" is, which is why they live in one file.
  return (
    latest.getBoundingClientRect().bottom -
      scroller.getBoundingClientRect().bottom <=
    SUBPIXEL_PX
  );
}
