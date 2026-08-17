// Which box actually scrolls a given element, and what part of it is on screen.
//
// The cockpit scrolls in an INNER container (`.tasks` carries overflow-y:auto;
// `document.scrollHeight` equals the window height at every measured width), so
// anything that needs to reason about "the fold" has to find that container
// first — reading or writing `document.scrollingElement.scrollTop` measures a
// box that never moves, which is the shape of a guard that passes on every
// implementation including none.
//
// 🔴 This module exists for ONE caller: collapsing a whole task card
// (TaskCard, T-6630 ③ — owner:「收和整個任務時,最後應該要定位到那則任務」).
// It is NOT a licence to re-add a scroll correction to the step-note
// disclosure: opening and closing a NOTE must leave the scrollport untouched
// (same ticket, ①). The two live in the same component and pull in opposite
// directions, on purpose, because they are answers to two different questions
// the owner asked.
//
// `scrollIntoView` is deliberately not used by that caller: its block/nearest
// handling differs between engines and it re-targets every scrollport on the
// ancestor chain, which is a different action from "put this one card back".

/** A vertical span in viewport coordinates. */
export type Span = { top: number; bottom: number };

/**
 * The nearest ancestor that actually scrolls `el` vertically, or the document
 * scrolling element when nothing on the chain does.
 *
 * "Actually scrolls" needs BOTH halves: an `overflow-y:auto` box whose content
 * fits absorbs no scrolling, and treating it as the scrollport would silently
 * make every correction a no-op.
 */
export function scrollParent(el: Element): Element {
  const doc = el.ownerDocument;
  const root = doc.scrollingElement ?? doc.documentElement;
  let node: Element | null = el.parentElement;
  while (node && node !== root) {
    const style = node.ownerDocument.defaultView?.getComputedStyle(node);
    const overflowY = style?.overflowY ?? "";
    if (
      (overflowY === "auto" ||
        overflowY === "scroll" ||
        overflowY === "overlay") &&
      node.scrollHeight > node.clientHeight
    ) {
      return node;
    }
    node = node.parentElement;
  }
  return root;
}

/** The visible span of a scrollport, in viewport coordinates. */
export function viewportSpanOf(container: Element): Span {
  const doc = container.ownerDocument;
  const root = doc.scrollingElement ?? doc.documentElement;
  if (container === root) {
    return { top: 0, bottom: doc.defaultView?.innerHeight ?? root.clientHeight };
  }
  const r = container.getBoundingClientRect();
  return { top: r.top, bottom: r.top + container.clientHeight };
}
