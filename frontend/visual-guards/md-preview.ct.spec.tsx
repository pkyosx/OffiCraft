// T-a1c4 .md preview overlay — real-browser layout the jsdom suite can't see:
// the centered modal panel lays out inside the viewport, the markdown BODY
// renders (a real heading box, proving Markdown.tsx ran — not the raw-source
// tab), and the 下載 action sits in the header as a separate affordance.
//
// T-76cd — the three narrow-width guards below. owner opened a long .md on an
// iPhone and got a panel whose title was cut in half at the top, whose bottom
// stopped mid-paragraph, and which would not scroll at all.
//
// 🔴 WHAT THESE GUARDS ARE AND ARE NOT. A desktop Chromium at 390px wide is NOT
// an iPhone: on iOS a `position: fixed; inset: 0` box resolves against the LARGE
// viewport (the one with the URL bar retracted), so it can be taller than what
// is on screen — and headless Chromium has no such split (measured on the real
// running app under WebKit/iPhone-13 emulation: innerHeight === visualViewport
// .height === 664, panel 32..632, body scrollHeight 36276 / clientHeight 545 and
// scrollable to the last line ⇒ THE REPORT DOES NOT REPRODUCE HERE). So these
// are REGRESSION guards for the fix, not a reproduction of the report. Their
// discriminating power comes from mutants, recorded per test below.
//
// Every assertion measures GEOMETRY (rects, computed style) — never "is the
// class attached". A stylesheet that carries the class names and lays out wrong
// passes a class-name assertion and fails these.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import {
  MarkdownPreviewStory,
  MarkdownPreviewLongStory,
} from "./stories/MarkdownPreviewStory";

/** The phone width owner reads the cockpit at (iPhone 13 CSS pixels). */
const NARROW = { width: 390, height: 664 };
/** The control: a desktop viewport whose behaviour must not move at all. */
const WIDE = { width: 1280, height: 800 };

/** T-6c26 (originally T-415b): wait for the panel's geometry to STOP changing
 * before measuring it.
 *
 * 🔴 MEASURED, not guessed. Sampling the panel every 20 ms straight after mount
 * gives, on the very first sample:
 *     {"top":302.375,"bottom":497.625,"h":195.25}   →  {"top":32,"bottom":768,"h":736}
 * The overlay is `align-items: safe center`, so a panel that has not yet grown
 * to its `max-height` is CENTRED — which puts `top` at 302 instead of 32. Both
 * `toBeVisible()` and `document.fonts.status === "loaded"` are already true at
 * that point, so neither of the waits this file used to do excludes it. On a
 * loaded runner that window stretches, and the read lands inside it.
 *
 * This is a wait, not a tolerance: the settled values are EXACT, and asserting
 * them exactly is what makes the guard able to catch a one-pixel layout shift.
 * A tolerance wide enough to swallow the 270 px transient would swallow the
 * regression too. */
async function settleLayout(page: Page) {
  let prev = "";
  for (let i = 0; i < 60; i++) {
    const now = await page.evaluate(() => {
      const el = document.querySelector(".md-preview__panel");
      if (!el) return "";
      const r = el.getBoundingClientRect();
      return `${r.top}|${r.bottom}|${r.height}|${r.left}|${r.right}`;
    });
    if (now !== "" && now === prev) return;
    prev = now;
    await page.waitForTimeout(25);
  }
}

test("desktop 1024: overlay panel lays out with a rendered markdown body", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  // The overlay portals to `document.body`, so it is NOT under the mount root:
  // every reach for it goes through `page` (T-76cd).
  await mount(<MarkdownPreviewStory />);

  const panel = page.locator(".md-preview__panel");
  await expect(panel).toBeVisible();
  const box = await panel.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.height).toBeGreaterThan(80);
  // Panel stays within the viewport (max-width min(760, 100%)).
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(1024 + 1);

  // The markdown BODY rendered as real elements (heading present) — the whole
  // point of the in-cockpit preview vs the browser's raw-source tab.
  await expect(page.getByRole("heading", { name: "產物顯示架構設計" })).toBeVisible();

  // Preview and download are two actions: the header keeps a 下載 link.
  const dl = page.locator("a.md-preview__download");
  await expect(dl).toBeVisible();
});

// DoD ① — at phone width the whole panel lands inside the visible area, and the
// close button with it: an overlay you cannot dismiss is worse than one you
// cannot read. Measured against the visual viewport, not against a class list.
//
// 🔴 EVERY TALLY BELOW IS "x failed / y passed" OUT OF THE 4 TESTS IN THIS FILE,
// under exactly this command — the denominator is part of the measurement and an
// earlier version of this file omitted it:
//     npx playwright test -c playwright-ct.config.ts visual-guards/md-preview.ct.spec.tsx
// (Passing the bare string `md-preview.ct.spec.tsx` instead is a SUBSTRING match
// and also picks up chat-md-preview and reply-card-md-preview: 16 tests, 2
// skipped, so a green run reads "14 passed". Those were the numbers an earlier
// revision quoted with no command attached, which is why they matched nothing.)
//
// MUTANTS, and the exact declaration each one needs — this matters, see M-B0:
//   · `.md-preview__panel { max-height: none }` (the shape the prior round's
//     35%-confidence hypothesis predicts) ⇒ 3 failed / 1 passed. Within THIS
//     test the line that reddens is `panel.bottom <= vh`, and `panel.top >= 0`
//     PASSES — because the shipped stylesheet is `align-items: safe center`, so
//     an overflowing panel keeps its top edge instead of spilling equally off
//     both ends. That is positive evidence `safe center` does its job; do not
//     "correct" this note back to "reddens on the negative panel top", which is
//     what the world looked like BEFORE `safe center` and was never measured.
//   · `@media(max-width:600px){ .md-preview__close{ position: relative;
//     left: 100vw } }` ⇒ 1 failed / 3 passed — this test alone, on
//     `close.right <= vw`. The sibling `{ position: relative; top: -9999px }`
//     also reddens it alone.
//   · M-B0, the near-miss worth writing down: the same rule WITHOUT
//     `position: relative` — bare `{ left: 100vw }` — is completely INERT,
//     because `.md-preview__close` is `position: static` and `left` does not
//     apply to it. Measured: 4 passed. A mutant that changes no geometry proves
//     nothing about a geometry assertion, and it looks exactly like a guard with
//     no discriminating power. Always name the declaration, not the intent.
test("narrow 390: the whole preview panel, close button included, is inside the visible area", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(NARROW);
  // The overlay portals to `document.body`, so it is NOT under the mount root:
  // every reach for it goes through `page` (T-76cd).
  await mount(<MarkdownPreviewLongStory />);
  const panel = page.locator(".md-preview__panel");
  await expect(panel).toBeVisible();
  await expect(page.locator(".md-preview__md")).toBeVisible();
  await settleLayout(page);

  const seen = await page.evaluate(() => {
    const r = (s: string) => document.querySelector(s)!.getBoundingClientRect();
    const panel = r(".md-preview__panel");
    const header = r(".md-preview__header");
    const close = r(".md-preview__close");
    return {
      vh: window.innerHeight,
      vw: window.innerWidth,
      panel: { top: panel.top, bottom: panel.bottom, left: panel.left, right: panel.right },
      header: { top: header.top, bottom: header.bottom },
      close: { top: close.top, bottom: close.bottom, left: close.left, right: close.right },
      panelMaxHeight: getComputedStyle(document.querySelector(".md-preview__panel")!).maxHeight,
    };
  });

  // The panel itself — all four edges.
  expect(seen.panel.top).toBeGreaterThanOrEqual(0);
  expect(seen.panel.bottom).toBeLessThanOrEqual(seen.vh + 1);
  expect(seen.panel.left).toBeGreaterThanOrEqual(0);
  expect(seen.panel.right).toBeLessThanOrEqual(seen.vw + 1);

  // The header is the half owner lost first, and the × inside it is the exit.
  expect(seen.header.top).toBeGreaterThanOrEqual(0);
  expect(seen.close.top).toBeGreaterThanOrEqual(0);
  expect(seen.close.bottom).toBeLessThanOrEqual(seen.vh + 1);
  expect(seen.close.right).toBeLessThanOrEqual(seen.vw + 1);

  // The cap has to RESOLVE to a length, not sit there as an unresolved keyword:
  // `none` is exactly the failure the hypothesis names.
  expect(seen.panelMaxHeight).not.toBe("none");

  // And it must be pressable, not merely painted.
  await expect(page.locator(".md-preview__close")).toBeEnabled();
  await page.locator(".md-preview__close").click();
});

// DoD ② — a document taller than one screen is readable to its LAST line. The
// assertion is "the sentinel line is inside the body's own box after scrolling",
// which is the user-facing question; `overflow-y: auto` in the stylesheet is a
// property, not a reachability proof (T-7e68 learnt that the hard way).
//
// 🔴 THE SCROLLING HAS TO BE DRIVEN BY A GESTURE, not by `scrollIntoView`.
// MEASURED: with `.md-preview__body { overflow-y: hidden }` planted at narrow
// width, a `scrollIntoViewIfNeeded`-based version of this test stayed GREEN
// (14 passed) — a clipped box is still programmatically scrollable, so the
// script reaches a line the user's finger never can. Driving the wheel asks the
// user's question instead, and the same mutant reddens.
//
// MUTANT (measured): `@media (max-width:600px){ .md-preview__body{ overflow-y:
// hidden } }` ⇒ 1 failed / 3 passed, red on this test alone (on
// `reached.scrolled > 0`).
test("narrow 390: a document longer than the screen scrolls to its last line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(NARROW);
  // The overlay portals to `document.body`, so it is NOT under the mount root:
  // every reach for it goes through `page` (T-76cd).
  await mount(<MarkdownPreviewLongStory />);
  const body = page.locator(".md-preview__body");
  await expect(body).toBeVisible();
  const last = page.getByText("最後一行 LAST_LINE_T76CD");
  await expect(last).toBeAttached();

  // PREMISE: the fixture really is longer than the box, otherwise "it scrolls"
  // is satisfied by a document that never needed to.
  const overflow = await body.evaluate((el) => el.scrollHeight - el.clientHeight);
  expect(overflow).toBeGreaterThan(400);

  // Ask the end question ("is the last line on screen yet"), with a bounded
  // number of wheel deliveries — never a fixed count calibrated to one engine's
  // scroll step, and never "until the offset stops changing" (that stops in the
  // warm-up or mid-animation; see the image-zoom guard's notes).
  await body.hover();
  const onScreen = async () =>
    page.evaluate(() => {
      const b = document.querySelector(".md-preview__body")!;
      const br = b.getBoundingClientRect();
      const el = [...b.querySelectorAll("*")].find(
        (e) => e.children.length === 0 && e.textContent?.includes("LAST_LINE_T76CD"),
      )!;
      const r = el.getBoundingClientRect();
      return r.top >= br.top - 1 && r.bottom <= br.bottom + 1;
    });
  for (let i = 0; i < 200 && !(await onScreen()); i += 1) {
    await page.mouse.wheel(0, 3000);
  }

  const reached = await page.evaluate(() => {
    const b = document.querySelector(".md-preview__body")!;
    const br = b.getBoundingClientRect();
    const el = [...b.querySelectorAll("*")].find(
      (e) => e.children.length === 0 && e.textContent?.includes("LAST_LINE_T76CD"),
    )!;
    const r = el.getBoundingClientRect();
    return {
      scrolled: b.scrollTop,
      inside: r.top >= br.top - 1 && r.bottom <= br.bottom + 1,
      insideViewport: r.top >= 0 && r.bottom <= window.innerHeight + 1,
    };
  });
  expect(reached.scrolled).toBeGreaterThan(0);
  expect(reached.inside).toBe(true);
  expect(reached.insideViewport).toBe(true);
});

// DoD ③ — the CONTROL. The fix is deliberately NOT wrapped in a media query
// (`dvh` equals `vh` where there is no retracting browser chrome, and
// `safe center` equals `center` for a panel that fits), so the claim "only the
// narrow width moved" has to be measured rather than asserted by construction.
// These numbers are the desktop geometry read off the UNCHANGED stylesheet at
// 1280x800: panel width min(760, 100%) = 760, height = 800 - 2*32 = 736,
// horizontally centred at 260..1020.
//
// MUTANTS (measured, 5 tests in this file). TWO of them, and the pair is the
// point — they fail this test for unrelated reasons:
//   · `width: min(760px, 100%)` → `min(700px, 100%)` ⇒ 1 failed / 4 passed, on
//     the width line.
//   · `--md-preview-inset: 32px` → `0px` ON THE BASE RULE ONLY ⇒ 1 failed /
//     4 passed, on `panel.top` (received 16).
//
// ⚠️ THAT SECOND ONE WAS WRONGLY RECORDED AS RETIRED, and the correction is more
// useful than the mutant. Zeroing the property alone leaves `padding-bottom:
// calc(32px + …)` — a LITERAL since the var() fix — so only three sides go to
// zero: the content box is 768px, not 800, and centring a 736px panel in it
// gives top = 16. The earlier note claimed "4 passed, identical at 32..768"; that
// was measured on a DIFFERENT mutant, one that zeroed the property AND the
// padding-bottom literal (which does measure 4 passed, because then all four
// sides go to zero and the panel really is centred at 32..768 again). Both runs
// were real; the record named neither declaration, so it described neither. Name
// the declarations.
//
// 🔴 WHAT THIS CONTROL CANNOT DO, stated because the narrative around it is
// easy to oversell: reverting md-preview.css wholesale to origin/main leaves all
// three of these tests GREEN (measured: 4 passed). They are regression guards
// for a fix whose mechanism lives in `dvh`/`lvh`, and Chromium — the only engine
// this project's CT config runs — has dvh == lvh == vh, so the change is
// invisible to it by construction. Mutants reddening does NOT mean the CSS
// change itself is covered here.
//
// The instruments that CAN see it are both ad-hoc and NOT in CI, which is the
// honest gap in this file:
//   · an lvh/dvh-gap simulation (rewrite every `dvh` length to be N px shorter
//     than its `vh` twin), which is what turns the image-frame unit split into
//     a red "ONE scrollbar" run;
//   · a no-`dvh`-engine probe (rewrite `dvh`→`xvh`, an unknown unit), which is
//     what caught `var()` destroying the parse-time fallback.
// Both are described in md-preview.css's header.
//
// 🔴 ADDING A WEBKIT PROJECT DOES NOT CLOSE EITHER GAP — AND IT IS WORSE THAN A
// NO-OP, because it hands back numbers that look like the real thing. MEASURED
// in WebKit 2311 against a page that HAS a viewport meta:
//   · desktop 1280x720 — `lvh == dvh == svh == vh == 720`, `CSS.supports` true
//     for both `100dvh` and `safe center`. No split to see, nothing to catch.
//   · the `iPhone 13` device profile — also `lvh == dvh == svh == 664`. A
//     retracting URL bar is a real-device behaviour; emulation does not have one.
// And against a page with NO viewport meta — which is exactly what
// `frontend/playwright/index.html` is — the same iPhone 13 profile reports
// `lvh 664` but `dvh == vh == 1669` and `innerHeight 1668`: a split that is
// INVERTED (dvh > lvh is impossible on a real device) and ~4x too large,
// produced by mobile emulation falling back to the 980px layout viewport. That
// is the number a WebKit CT project would actually give this repo today, and
// building a guard on it would be building on noise. (Do not "fix" it by adding
// the meta to the harness either — that changes the layout every existing CT
// guard was calibrated against.)
test("desktop 1280: the panel geometry is unchanged by the narrow-width fix", async ({
  mount,
  page,
}) => {
  await page.setViewportSize(WIDE);
  // The overlay portals to `document.body`, so it is NOT under the mount root:
  // every reach for it goes through `page` (T-76cd).
  await mount(<MarkdownPreviewLongStory />);
  await expect(page.locator(".md-preview__panel")).toBeVisible();
  await settleLayout(page);

  const seen = await page.evaluate(() => {
    const el = document.querySelector(".md-preview__panel")!;
    const r = el.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, width: r.width };
  });
  // T-6c26 — which of these five are INVARIANTS and which are arithmetic, since
  // "five exact numbers" reads as five independent claims and it is not:
  //   · width 760      — an invariant: `width: min(760px, 100%)`, the panel's own cap.
  //   · left 260 / right 1020 — DERIVED from width + `justify-content: center` at
  //     1280 (260 = (1280-760)/2). They are not extra claims about the panel; what
  //     they still catch is the CENTRING, so they stay.
  //   · top 32 / bottom 768  — an invariant with a PRECONDITION: the 32 px desktop
  //     inset, true only once the panel has grown to its max-height. Until then
  //     `align-items: safe center` centres a shorter panel and top reads ~302.
  //     settleLayout above is what makes that precondition hold; do not delete it
  //     and do not paper over it with a tolerance (mutant: inset 32→33 must stay
  //     red at ONE pixel — measured).
  expect(seen.width).toBe(760);
  expect(seen.left).toBe(260);
  expect(seen.right).toBe(1020);
  expect(seen.top).toBe(32);
  expect(seen.bottom).toBe(768);

  // The desktop overlay still scrolls its long body — the control is a control,
  // not a licence for the wide case to break quietly.
  const overflow = await page
    .locator(".md-preview__body")
    .evaluate((el) => el.scrollHeight - el.clientHeight);
  expect(overflow).toBeGreaterThan(400);
});

// DoD ④ (T-76cd round 3) — the SHORT-VIEWPORT rule gets its own direct
// assertion. `@media (max-height: 500px)` cuts the overlay inset to 8px so a
// phone held sideways has room for a usable image frame; until this test existed
// NOTHING checked panel geometry under 500px tall — the three specs that measure
// the panel run at 664, 800 and 844 CSS px of HEIGHT, all above the breakpoint —
// and breaking the media query's numbers was caught only INDIRECTLY, by
// image-zoom-pan's "ONE scrollbar" guards noticing the body had started to
// overflow. Indirect coverage names the wrong file when it fails.
//
// MUTANTS (measured, `npx playwright test -c playwright-ct.config.ts
// visual-guards/md-preview.ct.spec.tsx`, 5 tests in this file):
//   · media block `--md-preview-inset: 8px` → `32px` ⇒ 1 failed / 4 passed here
//     (panel.top 8 → 32), every other test green.
//   · media block `max-height: calc(100dvh - 16px)` → `calc(100dvh - 64px)`
//     ⇒ 1 failed / 4 passed here. It fires on `panel.top` (received 32), NOT on
//     the height line: a 326px panel centred in a 374px content box starts at
//     8 + 24 = 32. The height does change too (374 → 326); `top` is simply
//     asserted first. Recorded as what fired, not as what one would predict.
test("landscape 844x390: the short-viewport rule gives the panel the screen, minus 8px", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 844, height: 390 });
  // The overlay portals to `document.body`, so it is NOT under the mount root:
  // every reach for it goes through `page` (T-76cd).
  await mount(<MarkdownPreviewLongStory />);
  await expect(page.locator(".md-preview__panel")).toBeVisible();
  await expect(page.locator(".md-preview__md")).toBeVisible();
  await settleLayout(page);

  const seen = await page.evaluate(() => {
    const r = (s: string) => document.querySelector(s)!.getBoundingClientRect();
    const p = r(".md-preview__panel");
    const c = r(".md-preview__close");
    return {
      vh: window.innerHeight,
      vw: window.innerWidth,
      panel: { top: +p.top.toFixed(1), bottom: +p.bottom.toFixed(1), height: +p.height.toFixed(1) },
      close: { top: +c.top.toFixed(1), bottom: +c.bottom.toFixed(1), right: +c.right.toFixed(1) },
      overlayPadTop: getComputedStyle(document.querySelector(".md-preview")!).paddingTop,
    };
  });

  // The inset really did shrink — asserted as GEOMETRY (where the panel starts),
  // not as "the media query exists".
  expect(seen.panel.top).toBe(8);
  expect(seen.panel.bottom).toBe(382);
  expect(seen.panel.height).toBe(374);
  expect(seen.overlayPadTop).toBe("8px");

  // …and the panel is still wholly on screen with a reachable close button,
  // which is the whole point of spending the inset.
  expect(seen.panel.top).toBeGreaterThanOrEqual(0);
  expect(seen.panel.bottom).toBeLessThanOrEqual(seen.vh + 1);
  expect(seen.close.top).toBeGreaterThanOrEqual(0);
  expect(seen.close.bottom).toBeLessThanOrEqual(seen.vh + 1);
  expect(seen.close.right).toBeLessThanOrEqual(seen.vw + 1);

  // The body still scrolls at this height — a panel that fits by refusing to
  // show its content would satisfy every line above.
  const overflow = await page
    .locator(".md-preview__body")
    .evaluate((el) => el.scrollHeight - el.clientHeight);
  expect(overflow).toBeGreaterThan(400);
});
