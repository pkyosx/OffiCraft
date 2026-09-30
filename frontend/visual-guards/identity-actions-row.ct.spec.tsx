// HOTSPOT — T-7526 ①: the identity card's action cluster, on both the member
// and the outsource panel (they share `.mp-identity__buttons` in
// member-detail.css).
//
// WHAT THIS GUARD PINS, AND WHY IT CHANGED TWICE (T-ed79)
// It used to pin "更改 ＋ 停止 sit on ONE row" at BOTH widths — true of the
// two-button world (owner 2026-07-31 「全部變成左右並排」). Owner 2026-08-21
// replaced the single 停止 with the three-rung ladder 停止 → 加速停止 →
// 強制停止, and FOUR buttons carrying 4-character labels do not fit one
// phone-width row: the ladder alone wants ~300px at its natural size and the
// card's inner width at 375 is 289. "One row everywhere" is therefore no longer
// a fact about this component, and a guard pinning it pins a dead world.
//
// So the invariant is now width-shaped, and NOTHING it used to catch was
// dropped:
//   * wide — the whole cluster still shares ONE row (the 2026-07-31 ruling,
//     still true wherever there is room).
//   * narrow — the wrap is DESIGNED, not incidental: exactly TWO bands, 更改
//     alone on the first, the WHOLE cluster on the second in owner order, each
//     rung sharing the band evenly.
//   * 🔴 EVERY SHAPE THE LADDER CAN BE. Owner 2026-08-21 「按了才出現」 made the
//     rung set a function of how far up the escalation the actor already is, and
//     owner 2026-08-22 (「同一個按鈕 升級的概念 不是不同按鈕」) collapsed the row
//     to ONE cell that upgrades. So the cluster is TWO buttons (更改 ＋ the
//     cell) — `stopping` offers no 喚醒 (owner rc-2e1c96250169) — and the cell
//     itself carries a different LABEL per stage,
//     which is a distinct measurement because 強制停止 is the longest of the
//     three and a cell that fits 停止 is no evidence that it fits that. The four
//     cases below are therefore still four: they now vary the label inside the
//     one cell instead of the number of cells.
//     🔴 WHAT THIS GUARD STOPPED PINNING, and why: the FOUR- and FIVE-button
//     worst cases are gone from the product, not from the measurement by choice.
//     They existed only because the spent rungs stayed on screen beside the live
//     one. Nothing about them was dropped while still reachable.
//   * both panels — 正職 and 外包 share this shell and this component, and each
//     case below runs against both, so a regression that only reproduces on the
//     outsource panel cannot hide behind the member panel's geometry.
//   * both — no button overlaps another (a real rectangle-intersection test,
//     strictly stronger than the old "停止 starts right of 更改"), no button
//     escapes the card on ANY edge, no label is clipped inside its own button,
//     and the page never gains a horizontal scrollbar.
//
// What jsdom cannot see: `flex-direction`, `flex-wrap` and `@media`. A unit test
// can only pin the DOM (same parent, 更改 first) — flipping the CSS back to
// `column` leaves every vitest assertion green. This spec measures real boxes.
//
// Mutants (all MEASURED, see docs/design/worker-panel-parity-mutants.md):
//   R0  put the ladder back to rungs standing side by side (RUNG_BY_STAGE
//       returning an array again) → red at EVERY width, on BOTH panels, on
//       `the ladder is ONE button, whatever rung it is on`.
//   R1  `.mp-identity__buttons { flex-direction: column }` → DESKTOP red
//       (`one row, whole cluster`). Narrow stays green on purpose: below 720px each
//       direct child already takes a full band, so a column is the same picture.
//   R2  drop the ≤720px `.mp-identity__buttons` rules → narrow red: 更改 and the
//       ladder go back to splitting the card in half and the ladder stacks
//       3-deep inside its half — four bands, not two.
//   R3' pin `.member-actions` to `width: 500px; flex-wrap: nowrap` at ≤720px →
//       narrow red on the CARD BOUNDS (the band shape survives; the row leaves
//       the card).
//   R4  `.member-actions .btn + .btn { margin-left: -40px }` → narrow red on the
//       rectangle-intersection check.
//   R5  lock each rung to `flex: 0 0 52px; overflow: hidden` → narrow red on the
//       label-clipping check, which is the one a bounding box cannot see.
import { test, expect } from "@playwright/experimental-ct-react";
import { IdentityActionsRowStory } from "./stories/IdentityActionsRowStory";
import { CHANGE_TESTID } from "./stories/identityPanelIds";

// 🔴 THREE widths, one on each side of the ≤720px media query and one ON it.
// 720 is INSIDE the query (`max-width: 720px` is inclusive) and is the width
// where the band layout is closest to fitting on one line — an off-by-one in the
// breakpoint shows up here and nowhere else.
const VIEWPORTS = [
  { name: "desktop-1280", width: 1280, height: 900, narrow: false },
  { name: "boundary-720", width: 720, height: 900, narrow: true },
  { name: "phone-375", width: 375, height: 780, narrow: true },
];

// Both panels render this cluster from the same component; only the panel-owned
// 更改 button's testid differs.
const PANELS = ["member", "worker"] as const;

// Which rung the ONE ladder cell is, per stage. 更改 leads every case because it
// is the panel's own button and renders before the group.
const RUNG = {
  none: "member-action-stop",
  soft: "member-action-accelerated-stop",
  accelerated: "member-action-force-stop",
} as const;

// 🔴 FOUR shapes — the four (status, stage) pairs the panels derive from the
// wire. `ids` is a function of the panel because only 更改's testid differs.
const CASES = [
  { status: "online-awake" as const, stage: "none" as const },
  { status: "online-awake" as const, stage: "soft" as const },
  { status: "stopping" as const, stage: "soft" as const },
  { status: "stopping" as const, stage: "accelerated" as const },
];

function idsFor(panel: (typeof PANELS)[number], c: (typeof CASES)[number]) {
  return [CHANGE_TESTID[panel], RUNG[c.stage]];
}

type Box = { x: number; y: number; width: number; height: number };

const SAME_ROW = 4;

for (const vp of VIEWPORTS) for (const panel of PANELS) for (const c of CASES) {
  const ALL = idsFor(panel, c);
  test(`${vp.name} / ${panel} / ${c.status}+${c.stage} (${ALL.length} buttons): 更改 ＋ the one 停止 ladder cell lay out without overlapping or leaving the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const cmp = await mount(
      <IdentityActionsRowStory status={c.status} stage={c.stage} panel={panel} />,
    );
    // 🔴 EXACTLY ONE ladder cell, measured in the real layout. The geometry
    // assertions below all iterate `ALL`, so a build that put the spent rungs
    // back beside the live one would simply not measure them — this is what
    // makes the count itself a pinned fact at every width and on both panels.
    const ladderCells = await cmp
      .locator(
        "[data-testid='member-action-stop'], [data-testid='member-action-accelerated-stop'], [data-testid='member-action-force-stop']",
      )
      .count();
    expect(ladderCells, "the ladder is ONE button, whatever rung it is on").toBe(1);

    const boxes: Record<string, Box> = {};
    for (const id of ALL) {
      const el = cmp.getByTestId(id);
      await expect(el, `${id} is rendered`).toBeVisible();
      const b = await el.boundingBox();
      expect(b, `${id} box`).not.toBeNull();
      boxes[id] = b!;
    }
    const card = (await cmp.getByTestId("story-identity").boundingBox())!;

    // ---- NO OVERLAP. Every pair of buttons, both axes. A layout that has run
    // out of room and started stacking things on top of each other fails here
    // no matter which direction it ran out in.
    for (let i = 0; i < ALL.length; i++) {
      for (let j = i + 1; j < ALL.length; j++) {
        const a = boxes[ALL[i]];
        const b = boxes[ALL[j]];
        const overlapX = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x);
        const overlapY = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y);
        expect(
          Math.min(overlapX, overlapY),
          `${ALL[i]} and ${ALL[j]} overlap by ${overlapX}x${overlapY}`,
        ).toBeLessThanOrEqual(1);
      }
    }

    // ---- INSIDE THE CARD, on every edge (the old version only checked the two
    // horizontal ones, which a vertical spill walked straight through).
    for (const id of ALL) {
      const b = boxes[id];
      expect(b.x, `${id} left edge`).toBeGreaterThanOrEqual(card.x - 1);
      expect(b.x + b.width, `${id} right edge`).toBeLessThanOrEqual(card.x + card.width + 1);
      expect(b.y, `${id} top edge`).toBeGreaterThanOrEqual(card.y - 1);
      expect(b.y + b.height, `${id} bottom edge`).toBeLessThanOrEqual(card.y + card.height + 1);
    }

    // ---- READABLE. A button squeezed until its own label wraps or clips is not
    // "fitting" — bounding boxes alone would call that a pass.
    for (const id of ALL) {
      const fit = await cmp.getByTestId(id).evaluate((el) => ({
        sw: el.scrollWidth,
        cw: el.clientWidth,
        sh: el.scrollHeight,
        ch: el.clientHeight,
      }));
      expect(fit.sw, `${id} label clipped horizontally`).toBeLessThanOrEqual(fit.cw + 1);
      expect(fit.sh, `${id} label clipped vertically`).toBeLessThanOrEqual(fit.ch + 1);
      expect(boxes[id].width, `${id} tap width`).toBeGreaterThan(48);
    }

    // ---- BANDS: rows of buttons whose tops line up, in document order.
    const bands: string[][] = [];
    for (const id of ALL) {
      const last = bands[bands.length - 1];
      if (last && Math.abs(boxes[last[0]].y - boxes[id].y) < SAME_ROW) last.push(id);
      else bands.push([id]);
    }
    // Reading order never runs backwards: left to right inside a band, top to
    // bottom across bands. This is what keeps the owner's escalation ORDER
    // (停止 → 加速停止 → 強制停止) a VISUAL fact and not just a DOM one.
    for (const band of bands) {
      for (let i = 1; i < band.length; i++) {
        expect(boxes[band[i]].x, `${band[i]} sits right of ${band[i - 1]}`).toBeGreaterThan(
          boxes[band[i - 1]].x,
        );
      }
    }
    for (let i = 1; i < bands.length; i++) {
      expect(boxes[bands[i][0]].y, `band ${i} sits below band ${i - 1}`).toBeGreaterThan(
        boxes[bands[i - 1][0]].y + SAME_ROW,
      );
    }

    if (!vp.narrow) {
      // Owner 2026-07-31 「全部變成左右並排」, still true wherever there is room:
      // ONE band holding all four. A `flex-direction: column` regression dies here.
      expect(bands.map((b) => b.length), "one row, whole cluster").toEqual([ALL.length]);
    } else {
      // 🔴 The wrap is DESIGNED (owner: 不要在窄螢幕擠成一團). Exactly two bands:
      // 更改 alone, then the whole ladder. Anything else — the ladder split
      // across lines, or a rung sharing 更改's line — is the "crammed" shape.
      // 更改 leads a band of its own; nothing after it may share that band, and
      // the ladder stays contiguous and in owner order wherever it lands.
      expect(bands[0], "band 1 = 更改 alone").toEqual([CHANGE_TESTID[panel]]);
      expect(
        bands.slice(1).flat(),
        "the rest of the cluster keeps its order below 更改",
      ).toEqual(ALL.slice(1));
      expect(bands.length, "at most two bands — 不要擠成一團").toBeLessThanOrEqual(2);

      // The ladder band spans the card rather than huddling in the right margin
      // (the pre-≤720px shape kept every "same row / inside the card" assertion
      // TRUE while doing exactly that, which is why SPREAD has to be measured).
      const rest = ALL.slice(1);
      const first = boxes[rest[0]];
      const last = boxes[rest[rest.length - 1]];
      expect(
        last.x + last.width - first.x,
        "the ladder spans the card, not the right margin",
      ).toBeGreaterThan(card.width * 0.7);
      expect(boxes[CHANGE_TESTID[panel]].width, "更改 spans the card too").toBeGreaterThan(
        card.width * 0.7,
      );
      // Evenly, not one fat rung beside a sliver.
      const widths = rest.map((id) => boxes[id].width);
      expect(
        Math.max(...widths) - Math.min(...widths),
        "the rungs share the band",
      ).toBeLessThan(card.width * 0.2);
    }

    // The page itself never gains a horizontal scrollbar because of the cluster.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });
}
