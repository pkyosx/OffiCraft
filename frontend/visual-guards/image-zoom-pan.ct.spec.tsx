// T-7e68 — the zoomed image must actually be REACHABLE, in a real browser.
//
// The bug this guards against shipped because "the wrap has `overflow: auto`"
// was read as "the user can scroll it". It could not: the zoom lived entirely
// in the image's `transform`, which paints bigger pixels without changing the
// layout box, so there was never any scrollable content and every magnified
// edge was clipped away. Nothing in the CSS says that — only geometry does.
//
// So every assertion below measures WHERE THE PIXELS ARE: the painted rect of
// the <img> (a transform is reflected in getBoundingClientRect) against the
// frame's own client box. "Corner reachable" means that corner's coordinates
// land inside the visible box. No assertion here may be satisfied by the
// presence of a property, a class or an element.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { ImageZoomPanStory, WideShortImageZoomStory } from "./stories/ImageZoomPanStory";

type Geometry = {
  image: { left: number; top: number; right: number; bottom: number };
  frame: { left: number; top: number; right: number; bottom: number };
  scrollLeft: number;
  scrollTop: number;
  overflowX: number;
  overflowY: number;
};

/** The painted image rect and the frame's VISIBLE box (client box, so a
 * classic scrollbar's gutter is excluded rather than fudged with a tolerance). */
async function geometry(wrap: Locator): Promise<Geometry> {
  return wrap.evaluate((el) => {
    const img = el.querySelector("img.md-preview__image") as HTMLImageElement;
    const i = img.getBoundingClientRect();
    const w = el.getBoundingClientRect();
    const left = w.left + el.clientLeft;
    const top = w.top + el.clientTop;
    return {
      image: { left: i.left, top: i.top, right: i.right, bottom: i.bottom },
      frame: { left, top, right: left + el.clientWidth, bottom: top + el.clientHeight },
      scrollLeft: el.scrollLeft,
      scrollTop: el.scrollTop,
      overflowX: el.scrollWidth - el.clientWidth,
      overflowY: el.scrollHeight - el.clientHeight,
    };
  });
}

const EPS = 1.5;
const inside = (p: { x: number; y: number }, f: Geometry["frame"]) =>
  p.x >= f.left - EPS && p.x <= f.right + EPS && p.y >= f.top - EPS && p.y <= f.bottom + EPS;

async function zoomTo400(cmp: Locator) {
  for (let i = 0; i < 12; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("400%")).toBeVisible();
}

/** A real mouse drag on the frame, started away from the floating zoom cluster. */
async function drag(page: Page, wrap: Locator, dx: number, dy: number) {
  const box = (await wrap.boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 3;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y + dy, { steps: 8 });
  await page.mouse.up();
}

/** Two touch points centred on the frame, moving apart (or together) from
 * `fromSpread` to `toSpread` px — a real pinch through the input layer, the
 * same channel as the one-finger guard below (`Input.dispatchTouchEvent`, not a
 * synthetic `TouchEvent`), so a handler the browser's touch pipeline never
 * actually reaches cannot pass. */
async function pinch(page: Page, wrap: Locator, fromSpread: number, toSpread: number) {
  const box = (await wrap.boundingBox())!;
  const cx = Math.round(box.x + box.width / 2);
  const cy = Math.round(box.y + box.height / 2);
  const cdp = await page.context().newCDPSession(page);
  const points = (spread: number) =>
    [-1, 1].map((side, i) => ({
      x: cx + (side * spread) / 2,
      y: cy,
      radiusX: 12,
      radiusY: 12,
      force: 1,
      id: i,
    }));
  const send = (type: string, spread: number) =>
    cdp.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : points(spread) });

  const STEPS = 10;
  await send("touchStart", fromSpread);
  for (let i = 1; i <= STEPS; i++) {
    await send("touchMove", fromSpread + ((toSpread - fromSpread) * i) / STEPS);
  }
  await send("touchEnd", toSpread);
  await page.waitForTimeout(200);
}

/** A one-finger swipe through the same input layer, for asserting that the
 * travel a zoom created is actually reachable BY TOUCH rather than only by the
 * mouse the rest of this file uses. Started off-centre so it does not land on
 * the floating zoom cluster. */
async function swipe(page: Page, wrap: Locator, dx: number, dy: number) {
  const box = (await wrap.boundingBox())!;
  const x = Math.round(box.x + box.width * 0.15);
  const y = Math.round(box.y + box.height * 0.5);
  const cdp = await page.context().newCDPSession(page);
  const touch = (type: string, px: number, py: number) =>
    cdp.send("Input.dispatchTouchEvent", {
      type,
      touchPoints: type === "touchEnd" ? [] : [{ x: px, y: py, radiusX: 12, radiusY: 12, force: 1 }],
    });
  await touch("touchStart", x, y);
  for (let i = 1; i <= 10; i++) await touch("touchMove", x + (dx * i) / 10, y + (dy * i) / 10);
  await touch("touchEnd", x + dx, y + dy);
  await settleScroll(page, wrap);
}

/** T-6c26: wait for the frame's scroll offsets to STOP changing, instead of
 * betting a fixed 600 ms that momentum has finished. Two reads that agree end
 * it; the loop is bounded so a frame that never settles still fails on the
 * assertion that follows rather than hanging here. This does not weaken any
 * assertion — everything the callers assert is asserted afterwards, unchanged. */
async function settleScroll(page: Page, wrap: Locator) {
  let prev = { x: -1, y: -1 };
  for (let i = 0; i < 60; i++) {
    const now = await wrap.evaluate((el) => ({ x: el.scrollLeft, y: el.scrollTop }));
    if (now.x === prev.x && now.y === prev.y) return;
    prev = now;
    await page.waitForTimeout(50);
  }
}

async function mountStory(mount: (c: JSX.Element) => Promise<Locator>, page: Page) {
  await page.setViewportSize({ width: 900, height: 700 });
  await mount(<ImageZoomPanStory />);
  // The overlay portals to `document.body` (T-76cd), so the mount root does not
  // contain it. `body` is the smallest root that does.
  const cmp = page.locator("body");
  const wrap = page.locator(".md-preview__image-wrap");
  await expect(wrap).toBeVisible();
  // The fit box is measured on load; wait for a settled non-zero image rect.
  await expect
    .poll(async () => (await geometry(wrap)).image.right - (await geometry(wrap)).image.left)
    .toBeGreaterThan(100);
  return { cmp, wrap };
}

test("at 100% the image sits wholly inside the frame with nothing to scroll", async ({ mount, page }) => {
  const { wrap } = await mountStory(mount, page);
  const g = await geometry(wrap);
  expect(g.image.left).toBeGreaterThanOrEqual(g.frame.left - EPS);
  expect(g.image.right).toBeLessThanOrEqual(g.frame.right + EPS);
  expect(g.image.top).toBeGreaterThanOrEqual(g.frame.top - EPS);
  expect(g.image.bottom).toBeLessThanOrEqual(g.frame.bottom + EPS);
  expect(g.overflowX).toBeLessThanOrEqual(1);
  expect(g.overflowY).toBeLessThanOrEqual(1);
});

test("zooming to 400% grows the LAYOUT, so the frame has real overflow to travel", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  const before = await geometry(wrap);
  const fitW = before.image.right - before.image.left;
  const fitH = before.image.bottom - before.image.top;

  await zoomTo400(cmp);
  const after = await geometry(wrap);

  // The painted image really is four times the fitted box…
  expect(after.image.right - after.image.left).toBeGreaterThan(fitW * 3.9);
  expect(after.image.bottom - after.image.top).toBeGreaterThan(fitH * 3.9);
  // …and the frame knows about it. This is the number that was 0 before the
  // fix: a pure `transform: scale()` leaves scrollWidth === clientWidth, and
  // then there is nowhere for any pan — drag, scrollbar or key — to go.
  expect(after.overflowX).toBeGreaterThan(fitW * 2);
  expect(after.overflowY).toBeGreaterThan(100);
});

test("dragging brings every magnified corner of the image into the visible frame", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  await zoomTo400(cmp);

  // At 400% the far corner is off the frame — that IS the owner's complaint.
  const zoomed = await geometry(wrap);
  expect(inside({ x: zoomed.image.right, y: zoomed.image.bottom }, zoomed.frame)).toBe(false);

  // Drag the content up-left → the BOTTOM-RIGHT corner pixel travels into view.
  for (let i = 0; i < 10; i++) await drag(page, wrap, -300, -200);
  const atBottomRight = await geometry(wrap);
  expect(
    inside({ x: atBottomRight.image.right, y: atBottomRight.image.bottom }, atBottomRight.frame),
    "the image's bottom-right corner must be draggable into the visible frame",
  ).toBe(true);
  // …and the drag genuinely travelled: the opposite corner is now off-frame.
  expect(inside({ x: atBottomRight.image.left, y: atBottomRight.image.top }, atBottomRight.frame)).toBe(false);

  // Drag back the other way → the TOP-LEFT corner pixel comes back into view.
  for (let i = 0; i < 10; i++) await drag(page, wrap, 300, 200);
  const atTopLeft = await geometry(wrap);
  expect(
    inside({ x: atTopLeft.image.left, y: atTopLeft.image.top }, atTopLeft.frame),
    "the image's top-left corner must be draggable back into the visible frame",
  ).toBe(true);
});

// The scrollbar route. Setting scrollLeft/scrollTop is exactly what dragging
// the frame's scrollbar does — and it only moves anything because the zoom is
// carried as layout. The assertion is still geometric: after travelling to the
// far end, the corner pixel must be inside the visible box.
test("the frame's own scroll travel reaches the far corner", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  await zoomTo400(cmp);
  await wrap.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
    el.scrollTop = el.scrollHeight;
  });
  const g = await geometry(wrap);
  expect(g.scrollLeft).toBeGreaterThan(0);
  expect(g.scrollTop).toBeGreaterThan(0);
  expect(
    inside({ x: g.image.right, y: g.image.bottom }, g.frame),
    "scrolling the frame to its end must put the bottom-right corner in view",
  ).toBe(true);
});

// Keyboard-only, at 150% so the travel is short. ⚠️ Do NOT calibrate this to a
// fixed number of presses: an arrow key scrolls ~39px in Chromium but only
// ~11.5px in WebKit, so "16 presses" silently means "arrives" in one engine and
// "gives up a third of the way" in the other. Press until the offset stops
// moving instead — that is engine-independent and it is also the real question
// (can the keyboard reach the end, not does it travel at some rate).
/** Press arrow keys (bounded) until the image's bottom-right corner is inside
 * the visible frame, and hand back the geometry the caller should assert on.
 *
 * ⚠️ Do NOT rewrite this as "press until the scroll offset stops changing".
 * That proxy broke twice on WebKit and neither break was a product fault:
 * Desktop Safari swallows the FIRST arrow key on a freshly focused scroll
 * container (measured: scrollLeft 0 → 0 → 28 → 40 over three presses), so a
 * loop that stops at the first no-op quits during the warm-up; and WebKit
 * ANIMATES key scrolling, so a loop that stops on a stall can also stop
 * mid-animation and measure a position the user never sees settle. Chromium
 * does neither, which is exactly why a Chromium-only guard was happy.
 *
 * Asking the real question instead is both engine-independent and stricter: an
 * implementation whose keyboard genuinely cannot pan never satisfies it and the
 * caller's assertions still fail on a frame that never moved. */
async function pressUntilCornerVisible(page: Page, wrap: Locator, maxPresses = 200) {
  const keys = ["ArrowRight", "ArrowDown"];
  for (let i = 0; i < maxPresses; i++) {
    const g = await geometry(wrap);
    if (inside({ x: g.image.right, y: g.image.bottom }, g.frame)) return g;
    await page.keyboard.press(keys[i % keys.length]);
    await page.waitForTimeout(30);
  }
  return geometry(wrap);
}

test("the keyboard reaches the far corner — panning is not mouse-only", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  for (let i = 0; i < 2; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("150%")).toBeVisible();

  const zoomed = await geometry(wrap);
  expect(inside({ x: zoomed.image.right, y: zoomed.image.bottom }, zoomed.frame)).toBe(false);

  await wrap.focus();
  const g = await pressUntilCornerVisible(page, wrap);

  expect(g.scrollLeft, "arrow keys must actually move the frame").toBeGreaterThan(0);
  expect(
    inside({ x: g.image.right, y: g.image.bottom }, g.frame),
    "arrow keys on the focused frame must reach the bottom-right corner",
  ).toBe(true);
});

test("returning to 100% recentres the image with no leftover pan offset", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  const at100 = await geometry(wrap);
  await zoomTo400(cmp);
  for (let i = 0; i < 6; i++) await drag(page, wrap, -300, -200);
  expect((await geometry(wrap)).scrollLeft).toBeGreaterThan(0);

  for (let i = 0; i < 12; i++) await cmp.getByRole("button", { name: "縮小" }).click();
  await expect(cmp.getByText("100%")).toBeVisible();

  const back = await geometry(wrap);
  expect(back.scrollLeft).toBe(0);
  expect(back.scrollTop).toBe(0);
  expect(back.image.left).toBeCloseTo(at100.image.left, 0);
  expect(back.image.top).toBeCloseTo(at100.image.top, 0);
  expect(back.image.right).toBeCloseTo(at100.image.right, 0);
});

// The controls must survive the travel. An absolutely positioned child of a
// scroll container is laid out against its PADDING box, so a zoom cluster
// parented to the frame rides the content: panned to the far corner at 400% it
// measured x ≈ -2031, i.e. two thousand pixels outside the frame. Every other
// assertion in this file stayed green through that — Playwright scrolls a
// control back into view before clicking it, so "the button still works" is not
// the same question as "the user can still see it".
test("the zoom controls stay on screen while the image is panned", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  await zoomTo400(cmp);
  await wrap.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
    el.scrollTop = el.scrollHeight;
  });
  const cluster = page.locator(".md-preview__zoom");
  const c = (await cluster.boundingBox())!;
  const f = (await geometry(wrap)).frame;
  expect(c.x, "the zoom cluster must not travel out of the frame with the content").toBeGreaterThanOrEqual(f.left - EPS);
  expect(c.x + c.width).toBeLessThanOrEqual(f.right + EPS);
  expect(c.y).toBeGreaterThanOrEqual(f.top - EPS);
  expect(c.y + c.height).toBeLessThanOrEqual(f.bottom + EPS);
});

// The readout must not lie. This aspect ratio is fitted by the frame's WIDTH,
// so the stylesheet's `max-width: 100%` still had room to resolve against the
// zoomed parent and grew the image a second time: at "200%" the painted box was
// 2872px against a 718px fit box — 4×, not 2×. Percentages of a box that is
// itself the zoom cannot be left switched on.
test("the zoom percentage is the true magnification for a width-fitted image", async ({ mount, page }) => {
  await page.setViewportSize({ width: 900, height: 700 });
  await mount(<WideShortImageZoomStory />);
  // The overlay portals to `document.body` (T-76cd), so the mount root does not
  // contain it. `body` is the smallest root that does.
  const cmp = page.locator("body");
  const wrap = page.locator(".md-preview__image-wrap");
  await expect(wrap).toBeVisible();
  await expect.poll(async () => (await geometry(wrap)).image.right - (await geometry(wrap)).image.left).toBeGreaterThan(100);

  const fit = await geometry(wrap);
  const fitW = fit.image.right - fit.image.left;
  const fitH = fit.image.bottom - fit.image.top;

  for (let i = 0; i < 4; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("200%")).toBeVisible();

  const at200 = await geometry(wrap);
  expect(at200.image.right - at200.image.left, "200% must paint exactly twice the fitted width").toBeCloseTo(fitW * 2, 0);
  expect(at200.image.bottom - at200.image.top, "200% must paint exactly twice the fitted height").toBeCloseTo(fitH * 2, 0);
  // And the frame's scrollable extent agrees with what is painted — the pan
  // range has to be the real one, not one derived from a stale layout box.
  expect(at200.overflowX).toBeCloseTo(fitW * 2 - (at200.frame.right - at200.frame.left), 0);
});

// ONE overflow, ONE scrollbar. The frame's caps are viewport units, but the
// frame does not get the whole viewport — the overlay inset, the panel border,
// the header and the body padding come off first. At 900x500 the frame's
// `min-height: 360px` / `max-height: 70vh` both overshot the 340px the body
// actually had, so `.md-preview__body` grew a scrollbar of its own BEHIND the
// frame's: two scrollbars for one overflow. Relaxing only the min-height to
// `min(360px, 50vh)` left 10px of it, so this asserts the body is quiet at
// every height, not just the one that was reported.
for (const height of [420, 500, 560, 700]) {
  test(`a ${height}px-tall viewport gives the image ONE scrollbar, not two`, async ({ mount, page }) => {
    await page.setViewportSize({ width: 900, height });
    await mount(<ImageZoomPanStory />);
    const cmp = page.locator("body");
    const wrap = page.locator(".md-preview__image-wrap");
    await expect(wrap).toBeVisible();
    await expect.poll(async () => (await geometry(wrap)).image.right - (await geometry(wrap)).image.left).toBeGreaterThan(100);

    const body = page.locator(".md-preview__body");
    const bodyOverflow = await body.evaluate((el) => el.scrollHeight - el.clientHeight);
    expect(bodyOverflow, "the panel body must not scroll behind the image frame's own scrollbar").toBeLessThanOrEqual(1);

    // …and the frame is still a real pan surface at this height: it must fit
    // the image at 100% and genuinely overflow once zoomed. A cap that merely
    // collapsed the frame to nothing would also silence the body.
    const at100 = await geometry(wrap);
    expect(at100.overflowY, "at 100% the image fits its frame").toBeLessThanOrEqual(1);
    expect(at100.frame.bottom - at100.frame.top, "the frame must not collapse").toBeGreaterThan(200);
    await zoomTo400(cmp);
    const at400 = await geometry(wrap);
    expect(at400.overflowX, "zoomed, the frame still owns real travel").toBeGreaterThan(100);
    expect(at400.overflowY).toBeGreaterThan(100);
    expect(await body.evaluate((el) => el.scrollHeight - el.clientHeight)).toBeLessThanOrEqual(1);
  });
}

// ⚠️ This scrolls DOWN on purpose. The first version wheeled UP from
// `scrollY === 0`, where the page cannot move in that direction no matter what
// the handler does — the assertion was true of every possible implementation,
// including one with no `preventDefault` at all. Down is the direction with
// 3000px of page underneath it to actually lose.
//
// What actually holds the page still is a PAIR, and it is not the obvious half:
// deleting `e.preventDefault()` from the wheel handler leaves this green,
// because `.md-preview__image-wrap`'s `overscroll-behavior: contain` already
// stops the scroll chaining out to the document. Only removing BOTH moves the
// page (measured: scrollY 120). So do not read a green here as "the JS handler
// is doing its job", and do not drop the CSS on the grounds that the JS covers
// it — in Chromium it is the other way round.
// TOUCH — REMOVED 2026-08-20 on the owner's instruction (「移除觸控手勢測試」,
// card rc-6ee58072e1da).
//
// WHAT WAS HERE: five Chromium-only guards driving real input-layer touch
// events through CDP — one-finger pan, two-finger spread, pinch-then-swipe,
// pinch closed, and "the page behind must not zoom".
//
// WHY THEY WENT: they went red intermittently on branches that had not touched
// the frontend at all — three times over two unrelated branches. Each one costs
// a whole CI round, and worse, it teaches every reader to discount a red. The
// root cause was never found, and the two cheap outs (a longer timeout,
// retries) only hide it.
//
// 🔴 WHAT IS NOW UNGUARDED, written down so nobody reads this gap as "covered
// elsewhere": the gesture-ownership SPLIT still holds in the component — one
// finger belongs to the BROWSER (`onPanPointerDown` bails out on
// `pointerType === "touch"`, so the native scroll container pans with inertia),
// two fingers belong to US (T-043e, owner 2026-07-31: 「在手機上二指撐開，要放
// 大的是圖片本身，頁面不動」, claimed via `touch-action: pan-x pan-y` plus the
// component's own pinch handler). That DECISION is still pinned by the
// branch-level unit test in MarkdownPreviewOverlay.test.tsx. What is no longer
// verified is that the real gestures still WORK end to end — delete the
// one-finger bail-out to "add touch support" and every pan would be applied
// twice, with nothing in CI to say so.

// The −/+ are the only way to zoom without a gesture, on the one surface whose
// whole job is to be driven by a thumb. 40px is the floor every mobile
// hit-target guideline agrees on; they shipped at 28px. Measured at phone width
// because that is where it matters, and off the RENDERED box rather than the
// stylesheet so a later padding/media-query change cannot shrink them back
// while the declaration still reads 40.
test("the zoom controls are thumb-sized at phone width", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<ImageZoomPanStory />);
  // The overlay portals to `document.body` (T-76cd), so the mount root does not
  // contain it. `body` is the smallest root that does.
  const cmp = page.locator("body");
  await expect(page.locator(".md-preview__image-wrap")).toBeVisible();
  const buttons = page.locator(".md-preview__zoom button");
  await expect(buttons).toHaveCount(2);
  for (const name of ["縮小", "放大"]) {
    const box = (await cmp.getByRole("button", { name }).boundingBox())!;
    expect(box.width, `the ${name} control must be at least 40px wide`).toBeGreaterThanOrEqual(40);
    expect(box.height, `the ${name} control must be at least 40px tall`).toBeGreaterThanOrEqual(40);
  }
});

test("wheel-zoom over the image does not scroll the page behind the overlay", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  expect(await page.evaluate(() => document.documentElement.scrollHeight > window.innerHeight),
    "the story must have a scrollable page for this assertion to mean anything").toBe(true);

  const box = (await wrap.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 3);
  for (let i = 0; i < 2; i++) await page.mouse.wheel(0, 120);

  await expect(cmp.getByText("50%")).toBeVisible();
  expect(await page.evaluate(() => window.scrollY), "the page must stay put while the image zooms").toBe(0);
});

// A resize moves the 100% box, so the zoom factor has to be recomputed against
// the NEW fit or the readout starts lying. Measured before the fix: 300% at
// 900x700 stayed 2154px wide after shrinking to 500x420, where the true fit is
// ~394px — a real ~5.5x still calling itself 300%. The transform version did
// not drift this way, so this is a regression guard, not a nicety.
test("the zoom factor survives a window resize instead of drifting", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  for (let i = 0; i < 8; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("300%")).toBeVisible();

  await page.setViewportSize({ width: 500, height: 420 });
  // Settle: the resize listener re-measures and React re-renders off that.
  await expect.poll(async () => Math.round((await geometry(wrap)).image.right - (await geometry(wrap)).image.left))
    .toBeLessThan(2000);

  const after = await geometry(wrap);
  const painted = after.image.right - after.image.left;
  // The honest 100% width at THIS viewport, read straight off the element with
  // its inline size stripped — the same box the component claims to zoom from.
  const fitNow = await wrap.evaluate((el) => {
    const img = el.querySelector("img.md-preview__image") as HTMLImageElement;
    const keep = { w: img.style.width, h: img.style.height, mw: img.style.maxWidth, mh: img.style.maxHeight };
    img.style.width = ""; img.style.height = ""; img.style.maxWidth = ""; img.style.maxHeight = "";
    const w = img.getBoundingClientRect().width;
    img.style.width = keep.w; img.style.height = keep.h; img.style.maxWidth = keep.mw; img.style.maxHeight = keep.mh;
    return w;
  });

  await expect(cmp.getByText("300%")).toBeVisible();
  expect(painted / fitNow, "what is painted must still be 3x the CURRENT fit box").toBeCloseTo(3, 1);
});
