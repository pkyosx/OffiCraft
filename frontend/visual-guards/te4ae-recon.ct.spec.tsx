// RECON ONLY (T-e4ae) — NOT a guard. This file measures the current squeeze and
// the candidate fixes so the owner can rule on the trade-off with numbers rather
// than adjectives. It asserts nothing about the product; it prints.
//
// Both stories reuse the real TaskCard ancestor chain. The six-column story is
// grounded in the existing worker-panel-parity design table; the five-column
// story is retained as a comparison against the already shipped T-4aa0 fixture.
import { test } from "@playwright/experimental-ct-react";
import {
  TaskCardWideTableOverflowStory,
} from "./stories/TaskCardQuoteOverflowStory";
import { TaskCardSixColTableStory } from "./stories/TaskCardSixColTableStory";

const VARIANTS: { name: string; css: string; contentAwareFloor?: number }[] = [
  { name: "A0-baseline", css: "" },
  {
    name: "B1-th-nowrap",
    css: ".doc-md th { white-space: nowrap; }",
  },
  {
    name: "B2-th-td-nowrap",
    css: ".doc-md th, .doc-md td { white-space: nowrap; }",
  },
  {
    name: "B3-table-min-width-520",
    // Use a fixed floor rather than `min(520px, max-content)`: the latter
    // resolves to the already-clamped width and is therefore a no-op.
    css: ".doc-md table { min-width: 520px; max-width: 100%; }",
  },
  {
    name: "B4-cell-min-width-96",
    // Put the width floor on cells, not on the table: this keeps long content
    // wrappable within a readable column while forcing the table itself to
    // become the horizontal scroller when the viewport is narrow.
    css: ".doc-md th, .doc-md td { min-width: 96px; }",
  },
  {
    name: "B5-cell-min-width-120",
    css: ".doc-md th, .doc-md td { min-width: 120px; }",
  },
  {
    name: "H3-content-aware-96",
    // A short column (for example the #/id column) keeps its intrinsic width;
    // a longer column receives the floor needed to avoid one-character wraps.
    css: ".doc-md th, .doc-md td { min-width: min(96px, max-content); }",
  },
  {
    name: "H4-content-aware-120",
    css: ".doc-md th, .doc-md td { min-width: min(120px, max-content); }",
  },
  {
    name: "H5-fit-content-96",
    css: ".doc-md th, .doc-md td { min-width: fit-content(96px); }",
  },
  {
    name: "H6-fit-content-120",
    css: ".doc-md th, .doc-md td { min-width: fit-content(120px); }",
  },
  {
    name: "H7-content-aware-js-96",
    // CSS cannot branch on rendered text length. This recon variant simulates
    // the renderer-level policy: measure each column's unwrapped content, keep
    // short columns intrinsic, and apply the floor only to long columns.
    css: ".doc-md th { white-space: nowrap; }",
    contentAwareFloor: 96,
  },
  {
    name: "H8-content-aware-js-120",
    css: ".doc-md th { white-space: nowrap; }",
    contentAwareFloor: 120,
  },
];

type Measurement = {
  cardInner: number;
  descW: number;
  descOver: number;
  listOver: number;
  pageOver: number;
  tableW: number;
  tableScrollW: number;
  tableClientW: number;
  tableOver: number;
  thCount: number;
  thLines: number[];
  thText: string[];
  thW: number[];
  tdMaxLines: number;
  tableH: number;
  scrollLeftAfterSet50: number;
};

async function measure(page: any, label: string): Promise<Measurement | null> {
  const m = await page.evaluate(() => {
    const q = (s: string) => document.querySelector(s) as HTMLElement | null;
    const card = q(".task-card");
    const desc = q(".task-card__desc");
    const list = q(".tasks");
    const doc = document.scrollingElement!;
    const table = q(".task-card__desc table");
    if (!table) return null;
    const ths = Array.from(table.querySelectorAll("th")) as HTMLElement[];
    const tds = Array.from(table.querySelectorAll("td")) as HTMLElement[];
    const lineHeight = (el: HTMLElement) => {
      const lh = getComputedStyle(el).lineHeight;
      const px = parseFloat(lh);
      return Number.isFinite(px) ? px : parseFloat(getComputedStyle(el).fontSize) * 1.2;
    };
    const lines = (el: HTMLElement) => {
      const cs = getComputedStyle(el);
      const inner =
        el.getBoundingClientRect().height -
        parseFloat(cs.paddingTop) -
        parseFloat(cs.paddingBottom) -
        parseFloat(cs.borderTopWidth) -
        parseFloat(cs.borderBottomWidth);
      return Math.max(1, Math.round(inner / lineHeight(el)));
    };
    return {
      cardInner: card ? card.clientWidth : -2,
      descW: desc ? Math.round(desc.getBoundingClientRect().width) : -2,
      descOver: desc ? desc.scrollWidth - desc.clientWidth : -2,
      listOver: list ? list.scrollWidth - list.clientWidth : -2,
      pageOver: doc.scrollWidth - doc.clientWidth,
      tableW: Math.round(table.getBoundingClientRect().width),
      tableScrollW: table.scrollWidth,
      tableClientW: table.clientWidth,
      tableOver: table.scrollWidth - table.clientWidth,
      thCount: ths.length,
      thLines: ths.map(lines),
      thText: ths.map((e) => (e.textContent || "").trim()),
      thW: ths.map((e) => Math.round(e.getBoundingClientRect().width)),
      tdMaxLines: tds.length ? Math.max(...tds.map(lines)) : 0,
      tableH: Math.round(table.getBoundingClientRect().height),
    };
  });
  if (!m) return null;

  // A table with no overflow snaps back to 0; a real table scroller retains a
  // positive scrollLeft. This is the behavioural check, not an inference from
  // scrollWidth alone.
  const scrolls = await page.evaluate(() => {
    const t = document.querySelector(".task-card__desc table") as HTMLElement | null;
    if (!t) return null;
    t.scrollLeft = 50;
    const got = t.scrollLeft;
    t.scrollLeft = 0;
    return got;
  });
  const result = { ...m, scrollLeftAfterSet50: scrolls ?? -1 };
  console.log(`RECON ${label} ${JSON.stringify(result)}`);
  return result;
}

async function applyContentAwareFloor(page: any, floor: number) {
  await page.evaluate((floorPx: number) => {
    const table = document.querySelector(".task-card__desc table") as HTMLElement | null;
    if (!table) return;
    const cells = Array.from(table.querySelectorAll("th, td")) as HTMLElement[];
    const columns = new Map<number, HTMLElement[]>();
    cells.forEach((cell) => {
      const col = (cell.parentElement ? Array.from(cell.parentElement.children).indexOf(cell) : 0);
      const bucket = columns.get(col) ?? [];
      bucket.push(cell);
      columns.set(col, bucket);
    });

    const maxByColumn = new Map<number, number>();
    columns.forEach((columnCells, col) => {
      let max = 0;
      columnCells.forEach((cell) => {
        // A detached inline-block clone gives the cell's intrinsic unwrapped
        // width without letting the table's current 100%-wide layout distort
        // the measurement.
        const clone = cell.cloneNode(true) as HTMLElement;
        const cs = getComputedStyle(cell);
        clone.style.position = "absolute";
        clone.style.left = "-100000px";
        clone.style.top = "0";
        clone.style.display = "inline-block";
        clone.style.width = "max-content";
        clone.style.minWidth = "0";
        clone.style.maxWidth = "none";
        clone.style.whiteSpace = "nowrap";
        clone.style.boxSizing = cs.boxSizing;
        clone.style.font = cs.font;
        clone.style.lineHeight = cs.lineHeight;
        clone.style.padding = cs.padding;
        clone.style.border = cs.border;
        document.body.appendChild(clone);
        max = Math.max(max, clone.getBoundingClientRect().width);
        clone.remove();
      });
      maxByColumn.set(col, max);
    });

    cells.forEach((cell) => {
      const col = cell.parentElement ? Array.from(cell.parentElement.children).indexOf(cell) : 0;
      const max = maxByColumn.get(col) ?? 0;
      // A short column keeps its measured intrinsic width; only a column whose
      // unwrapped content exceeds the floor is capped at the readable minimum.
      cell.style.minWidth = max > 0 ? `${Math.ceil(Math.min(max, floorPx))}px` : "";
    });
  }, floor);
}

for (const story of [
  { name: "5col", node: <TaskCardWideTableOverflowStory /> },
  { name: "6col", node: <TaskCardSixColTableStory /> },
]) {
  for (const width of [390, 320]) {
    for (const v of VARIANTS) {
      test(`recon ${story.name} @${width} ${v.name}`, async ({ mount, page }) => {
        await page.setViewportSize({ width, height: 900 });
        const cmp = await mount(story.node);
        if (v.css) await page.addStyleTag({ content: v.css });
        await cmp.locator(".task-card__head").first().click();
        await cmp.locator(".task-card__desc").first().waitFor();
        if (v.contentAwareFloor) await applyContentAwareFloor(page, v.contentAwareFloor);
        const result = await measure(page, `${story.name} @${width} ${v.name}`);
        if (!result) throw new Error("recon fixture did not render a markdown table");
        await page.screenshot({
          path: `recon-out/${story.name}-${width}-${v.name}.png`,
          fullPage: false,
        });
      });
    }
  }
}
