// HOTSPOT — T-7fa1 手機高度下告示把聊天空狀態壓成字疊字.
//
// Bug (shipped in v0.5.14, found on 390×667): once the wake DispatchAlert lands
// in `.chat__composer`, the composer grows ~190px. `.chat` is a flex column and
// `.chat__body` is its ONLY shrinkable item (`flex:1; min-height:0`), so the
// body pays for all of it — measured at 390×667: header 69 + body 95 +
// composer 331 = 495, the column adds up exactly. The shrink is correct.
//
// ROOT CAUSE: the body's CONTENT does not shrink with it. `.chat__offline` is
// intrinsically ~128px (56px icon + 18 + title + 8 + hint) with no give, so the
// body ran scrollHeight 152 vs clientHeight 95 — and `.chat__body` had the
// default `overflow: visible`, so those 57px were PAINTED OUTSIDE the pane, on
// top of the wake row and the ⚡喚醒 button below it. Measured overlap: +12px at
// 700px viewport height, +45px at 667px. The 844/900 cases never overlapped,
// which is why four rounds of review missed it — nobody measured a short window.
//
// FIX: `.chat__body { overflow-y: auto; overflow-x: hidden }` — a scroll
// container cannot paint outside its padding box, so NO composer height can
// reach the pane below. `overflow-x: hidden` is not decoration: per the overflow
// spec a `visible` overflow-x next to a non-visible overflow-y is COERCED to
// `auto`, which would hand this pane the sideways iOS rubber-band pan T-55ad had
// to kill on `.chat__messages`.
//
// 🔴 WHY THE ORACLE IS THE *VISIBLE* RECT, NOT getBoundingClientRect().
// `getBoundingClientRect()` reports the LAYOUT box, which a clipper does not
// shrink: after the fix `.chat__offline`'s raw rect still hangs 57px below the
// body and still "intersects" the wake row numerically, even though nothing of
// it is painted there. A raw-bbox assertion would therefore fail on a correct
// fix and pass on a `visibility:hidden` one. So this guard intersects each
// element's rect with every clipping ancestor first — the box a human actually
// sees — and cross-checks it with `document.elementFromPoint`, which answers the
// question that actually matters to the owner: when I tap 喚醒, does the button
// get the tap, or does a stray line of empty-state text?
//
// jsdom cannot reach ANY of this: no layout engine, no overflow coercion, no
// hit-testing. The offline empty state and the wake row both "exist" in the DOM
// in every jsdom test in the suite, which is exactly why they all stayed green
// while the shipped screen was unreadable.
//
// MUTANT (§5): delete the `overflow-y: auto; overflow-x: hidden` pair from
// `.chat__body` in office.css → assertion (1) goes red at every squeezed height.
// See ~/ai_workspace/dev/T-7fa1-overlap-fix.md for the recorded red/green runs,
// including the positive control that deleting ONLY `overflow-y: auto` is a
// NO-OP (the surviving `overflow-x: hidden` coerces y back to `auto`).
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatOfflineNoticeStory } from "./stories/ChatOfflineNoticeStory";

/** Layout rect ∩ every clipping ancestor's border box = what is really painted. */
const MEASURE = () => {
  const visibleRect = (el: Element) => {
    const r = el.getBoundingClientRect();
    let top = r.top,
      bottom = r.bottom,
      left = r.left,
      right = r.right;
    for (let p = el.parentElement; p; p = p.parentElement) {
      const cs = getComputedStyle(p);
      const pr = p.getBoundingClientRect();
      if (cs.overflowY !== "visible") {
        top = Math.max(top, pr.top);
        bottom = Math.min(bottom, pr.bottom);
      }
      if (cs.overflowX !== "visible") {
        left = Math.max(left, pr.left);
        right = Math.min(right, pr.right);
      }
    }
    return { top, bottom, left, right, h: bottom - top, w: right - left };
  };
  type R = ReturnType<typeof visibleRect>;
  const overlapArea = (a: R, b: R) => {
    if (a.h <= 0 || a.w <= 0 || b.h <= 0 || b.w <= 0) return 0;
    const y = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
    const x = Math.min(a.right, b.right) - Math.max(a.left, b.left);
    return Math.max(0, x) * Math.max(0, y);
  };

  const q = (s: string) => document.querySelector(s)!;
  const empty = q('[data-testid="offline-empty"]');
  const row = q('[data-testid="wake-row"]');
  const btn = q('[data-testid="wake-btn"]');
  const alert = q('[data-testid="chat-wake-undispatched"]');
  const body = q('[data-testid="chat-body"]') as HTMLElement;

  const vEmpty = visibleRect(empty);
  const targets: [string, Element][] = [
    ["wake-row", row],
    ["wake-btn", btn],
    ["dispatch-alert", alert],
  ];

  // Hit test: 9 points spread over each target. None may resolve inside the
  // offline empty state, and each must resolve inside its own target.
  const stolen: string[] = [];
  for (const [name, el] of targets) {
    const r = visibleRect(el);
    for (const fx of [0.08, 0.5, 0.92])
      for (const fy of [0.15, 0.5, 0.85]) {
        const x = r.left + r.w * fx;
        const y = r.top + r.h * fy;
        const hit = document.elementFromPoint(x, y);
        if (!hit) continue;
        if (empty.contains(hit))
          stolen.push(`${name}@(${fx},${fy}) hit the offline empty state`);
        else if (!el.contains(hit) && el !== hit)
          stolen.push(
            `${name}@(${fx},${fy}) hit an outsider: ${(hit as HTMLElement).className || hit.tagName}`,
          );
      }
  }

  return {
    paneH: +body.getBoundingClientRect().height.toFixed(1),
    overlaps: Object.fromEntries(
      targets.map(([n, el]) => [n, +overlapArea(vEmpty, visibleRect(el)).toFixed(1)]),
    ) as Record<string, number>,
    stolen,
    bodyOverflowX: getComputedStyle(body).overflowX,
    bodyOverflowY: getComputedStyle(body).overflowY,
    bodyOverflowPx: body.scrollHeight - body.clientHeight,
    alertClippedPx: (() => {
      const a = alert as HTMLElement;
      return a.scrollHeight - a.clientHeight;
    })(),
    alertText: (alert.textContent || "").replace(/\s+/g, ""),
    alertSteps: alert.querySelectorAll(".dispatch-alert__steps li").length,
  };
};

// 🔴 CALIBRATED TO THE RUNNING APP, NOT GUESSED. What decides whether this bug
// bites is the PANE height (`.chat__body`), and the pane height is a LEFTOVER:
// `.chat` height minus header minus composer. This story's header and composer
// are a hair shorter than production's, so `.chat` heights picked to LOOK like
// device heights reproduce the wrong squeeze — the first draft of this guard
// used chat=528 for 390×700 and quietly measured a 20px roomier pane than the
// phone actually has, which made the mutant miss that case.
//
// Measured on the running app (vite mock adapter, zh, office theme, b61b742):
//   viewport 390×667 ⇒ pane 94.7px, empty state overhangs the wake row 44.8px
//   viewport 390×700 ⇒ pane 127.7px, overhang 11.8px
// Measured in this story: pane = chatHeight − 380.1. Hence 475 and 508, which
// reproduce those two panes to within 0.2px (and reproduce the 44.7 / 11.7px
// overhangs). Assertion (0) pins the calibration so it cannot rot silently.
//
// `squeezed` records whether the empty state genuinely overflows the pane. It
// gates assertion (4) — the roomy control legitimately has nothing to contain.
const CASES: [number, number, string, boolean][] = [
  [560, 179.9, "roomy control — the pane still fits its empty state", false],
  [508, 127.9, "390×700 — iPhone 13/14 with Safari toolbars (app pane 127.7)", true],
  [475, 94.9, "390×667 — iPhone SE, the reported device (app pane 94.7)", true],
  [450, 69.9, "shorter still", true],
  // 420 would compute to a 40px pane, but the pane bottoms out at 48px — its own
  // 24+24 padding, which is not shrinkable. That floor is the worst case the
  // layout can ever produce, so it is the right last case.
  [420, 48, "extreme squeeze — pane at its 48px padding floor", true],
];

for (const [chatHeight, expectPane, label, squeezed] of CASES) {
  test(`390 wide, chat ${chatHeight}px (${label}): offline empty state never covers the wake row / 喚醒 / notice`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: chatHeight + 172 });
    const cmp = await mount(<ChatOfflineNoticeStory chatHeight={chatHeight} />);
    await expect(cmp.getByTestId("offline-empty")).toBeAttached();
    await expect(cmp.getByTestId("wake-btn")).toBeVisible();
    await expect(cmp.getByTestId("chat-wake-undispatched")).toBeVisible();

    const m = await page.evaluate(MEASURE);

    // (0) calibration: this case must still reproduce the pane height it claims
    // to. If the header or composer ever changes size, this fails LOUDLY instead
    // of letting the device cases drift into a squeeze that no phone has.
    expect(
      m.paneH,
      `case must still reproduce a ${expectPane}px pane (got ${m.paneH})`,
    ).toBeCloseTo(expectPane, 0);

    // (1) 🔴 CORE red→green. Zero PAINTED overlap between the offline empty
    // state and each of the three things below it. This is the assertion the
    // mutant reddens; it is first so nothing can mask it.
    expect(
      m.overlaps,
      `offline empty state must not cover anything below it (px² of visible overlap)`,
    ).toEqual({ "wake-row": 0, "wake-btn": 0, "dispatch-alert": 0 });

    // (2) hit-testing agrees with the geometry: every sampled point on the wake
    // row / ⚡喚醒 / notice reaches its own element, never the empty state.
    expect(m.stolen, "no tap target is stolen by the empty state").toEqual([]);

    // (3) the pane must not become a sideways pan surface. Making it scroll
    // vertically coerces a `visible` overflow-x to `auto` unless it is pinned —
    // the same iOS rubber-band trap T-55ad fixed on `.chat__messages`.
    expect(
      m.bodyOverflowX,
      `.chat__body overflow-x must be clamped (got "${m.bodyOverflowX}")`,
    ).not.toMatch(/auto|scroll/);

    // (4) NOT VACUOUS: at the squeezed heights the empty state must still
    // genuinely overflow the pane. If it ever fits on its own, (1) passes
    // without the containment doing any work and this guard silently stops
    // guarding — the failure mode that let this bug ship in the first place.
    if (squeezed) {
      expect(
        m.bodyOverflowPx,
        "the pane is still too short for the empty state — containment is what is doing the work",
      ).toBeGreaterThan(0);
    }

    // (5) the pane SCROLLS its overflow rather than swallowing it: `hidden`
    // would trade unreadable text for silently amputated text.
    expect(
      m.bodyOverflowY,
      "the pane must be able to scroll its own content, not clip it away",
    ).toMatch(/auto|scroll/);
  });
}

test("390×667: the notice keeps its full information (no shrink-the-notice escape hatch)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 667 });
  const cmp = await mount(<ChatOfflineNoticeStory chatHeight={495} />);
  await expect(cmp.getByTestId("chat-wake-undispatched")).toBeVisible();
  const m = await page.evaluate(MEASURE);

  // T-7fa1's whole point is that the owner can READ what happened and what to do
  // next. Repairing the overlap by trimming the notice would defeat the ticket,
  // so the notice's two diagnostic steps and its body copy are asserted here.
  expect(m.alertSteps, "both diagnostic steps survive on a phone").toBe(2);
  expect(m.alertText.length, "notice copy is not truncated").toBeGreaterThan(60);
  expect(
    m.alertClippedPx,
    "the notice itself is never internally clipped/scrolled",
  ).toBe(0);
});
