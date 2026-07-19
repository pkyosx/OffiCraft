// HOTSPOT — 手機聊天視圖橫向拖動 (T-55ad).
//
// Bug (owner, iPhone): "I chat view in iphone i can drag the chat left and
// right which is not necessary i need only scroll up and down." A phone user
// must scroll only up/down; the thread must never pan sideways.
//
// ROOT CAUSE (measured at 390×844, iPhone 13 profile — see kyle-55ad-impl.md):
// the PAGE never overflows horizontally (document.scrollingElement scrollWidth
// == clientWidth == 390 in every case). The one horizontally-pannable surface
// in the whole chat ancestor chain is `.chat__messages` itself — office.css
// declares only `overflow-y: auto` on it, and per the CSS overflow spec a
// `visible` overflow-x is COERCED to `auto` whenever overflow-y is auto/scroll.
// So the vertical message list silently became a horizontal scroll container,
// and iOS Safari rubber-band-pans such containers on touch even when nothing
// overflows. (Chromium desktop does not reproduce the elastic rubber-band, so
// this is asserted on the computed property — the pannable VALUE — not on a
// synthetic drag.) The fix pins `overflow-x: hidden` so it computes to hidden,
// not auto; the wide code block keeps its OWN in-block scroll via the <pre>.
//
// jsdom cannot reach this: it resolves no `overflow` computed style against a
// layout engine and never applies the auto↔visible coercion. Hence a CT guard.
//
// MUTANT (§5): delete `overflow-x: hidden` from `.chat__messages` → overflow-x
// computes back to `auto` and assertion (1) goes red. Verified red→green in
// kyle-55ad-impl.md.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatMessagesStory } from "./stories/ChatMessagesStory";

test("390px: .chat__messages is NOT a horizontal pan surface", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mount(<ChatMessagesStory />);
  const messages = cmp.getByTestId("chat-messages");
  await expect(messages).toBeVisible();

  // (1) CORE red→green: the pannable values are `auto` / `scroll`. After the fix
  // overflow-x must compute to `hidden` (or `clip`) — anything auto/scroll means
  // the thread pans sideways on iOS.
  const overflowX = await messages.evaluate(
    (el) => getComputedStyle(el).overflowX
  );
  expect(
    overflowX,
    `.chat__messages overflow-x must be clamped (got "${overflowX}")`
  ).not.toMatch(/auto|scroll/);

  // (2) overflow-y stays scrollable — the fix must not kill vertical scroll.
  const overflowY = await messages.evaluate(
    (el) => getComputedStyle(el).overflowY
  );
  expect(overflowY, "overflow-y must remain auto/scroll").toMatch(/auto|scroll/);

  // (3) page-level invariant: nothing widens the document past the viewport.
  const pageOverflow = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(pageOverflow, "page has no horizontal overflow").toBeLessThanOrEqual(1);

  // (4) wide content keeps its OWN in-block horizontal scroll: the <pre> is the
  // legitimate scroll container for long code, so the fix must not flatten it.
  const pre = cmp.getByTestId("chat-code");
  const preScroll = await pre.evaluate((el) => ({
    overflowX: getComputedStyle(el).overflowX,
    scrollable: el.scrollWidth - el.clientWidth,
  }));
  expect(preScroll.overflowX, "<pre> keeps overflow-x auto").toMatch(
    /auto|scroll/
  );
  expect(
    preScroll.scrollable,
    "<pre> still scrolls its long code line horizontally"
  ).toBeGreaterThan(1);
});
