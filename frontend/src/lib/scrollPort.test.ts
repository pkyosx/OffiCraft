import { describe, it, expect, afterEach } from "vitest";
import { scrollParent, viewportSpanOf } from "./scrollPort";

// The pixel half of T-6630 ③ (how far a collapsing card actually moves) is
// measured in visual-guards/taskcard-collapse-anchor.ct.spec.tsx — jsdom has no
// layout engine. What is worth pinning here is the CHOICE of box: picking the
// document instead of `.tasks` is the failure that makes every correction a
// silent no-op, and it looks identical to a correction that works.

/** jsdom reports 0 for every layout box, so the scroll geometry is declared. */
function sized(el: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(el, "scrollHeight", {
    value: scrollHeight,
    configurable: true,
  });
  Object.defineProperty(el, "clientHeight", {
    value: clientHeight,
    configurable: true,
  });
  return el;
}

// jsdom leaves `document.scrollingElement` UNDEFINED, which is why the module
// falls back to documentElement — without that fallback every caller here would
// throw in the unit suite while working in a browser.
const ROOT = document.scrollingElement ?? document.documentElement;

function mount(html: string) {
  document.body.innerHTML = html;
  return document.body.firstElementChild as HTMLElement;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("scrollParent", () => {
  it("picks the overflow ancestor that has content to scroll", () => {
    const outer = mount(
      `<div style="overflow-y:auto"><div class="mid"><span class="leaf"></span></div></div>`
    );
    sized(outer, 2000, 800);
    const leaf = outer.querySelector(".leaf")!;
    expect(scrollParent(leaf)).toBe(outer);
  });

  it("skips an overflow ancestor whose content fits — it absorbs no scrolling", () => {
    const outer = mount(
      `<div style="overflow-y:auto"><span class="leaf"></span></div>`
    );
    sized(outer, 400, 400);
    const leaf = outer.querySelector(".leaf")!;
    expect(scrollParent(leaf)).toBe(ROOT);
  });

  it("takes the NEAREST scrolling ancestor when the chain has two", () => {
    const outer = mount(
      `<div style="overflow-y:auto"><div class="inner" style="overflow-y:scroll"><span class="leaf"></span></div></div>`
    );
    sized(outer, 2000, 800);
    const inner = sized(outer.querySelector(".inner") as HTMLElement, 1200, 300);
    expect(scrollParent(outer.querySelector(".leaf")!)).toBe(inner);
  });

  it("falls back to the document when nothing on the chain scrolls", () => {
    const outer = mount(`<div><span class="leaf"></span></div>`);
    expect(scrollParent(outer.querySelector(".leaf")!)).toBe(ROOT);
  });
});

describe("viewportSpanOf", () => {
  it("measures an inner scrollport from its own box, not the window's", () => {
    const outer = mount(`<div style="overflow-y:auto"></div>`);
    sized(outer, 2000, 500);
    outer.getBoundingClientRect = () =>
      ({ top: 120, bottom: 620 }) as DOMRect;
    expect(viewportSpanOf(outer)).toEqual({ top: 120, bottom: 620 });
  });

  it("measures the document scrollport from the window", () => {
    const span = viewportSpanOf(ROOT);
    expect(span.top).toBe(0);
    expect(span.bottom).toBe(window.innerHeight);
  });
});
