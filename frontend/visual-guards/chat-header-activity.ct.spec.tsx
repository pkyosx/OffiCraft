// T-492: the chat header's member-activity word must fit the existing one-line
// sub row at phone and desktop widths. The row competes with three header
// buttons for width; the role text is what gives, the word never wraps, shrinks
// or pushes the buttons off the edge, and the header keeps the height it has
// without the word (.chat__gallery hangs off that height).
//
// Everything below compares boxes against each other in a real browser (CT
// pages lack the production fonts, so no absolute widths).
//
// Mutants (measured, each red at 390 only):
//   1. drop `white-space: nowrap` from .activity-word → the word wraps one
//      character per line → "header height unchanged by the word" red.
//   2. drop `display: flex` from .chat__header-sub → the word drops under the
//      role → "header height unchanged by the word" red.
//   3. drop `min-width: 0` from `.chat__header-sub .presence-badge` → the role
//      stops giving way and the word lands under the buttons → "word stays left
//      of the header buttons" red.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatHeaderActivityStory } from "./stories/ChatHeaderActivityStory";

const LONG_ROLE = "Platform Reliability and Developer Experience Lead";

for (const width of [390, 1280]) {
  for (const state of ["busy", "waiting", "idle"] as const) {
    test(`width ${width}, ${state}: the activity word fits the header's one-line sub row`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(
        <ChatHeaderActivityStory mobile={width < 720} state={state} roleName={LONG_ROLE} />,
      );
      const withWord = cmp.getByTestId("header-with");
      await expect(withWord.getByTestId("chat-header-activity")).toBeVisible();

      const m = await page.evaluate(() => {
        const box = (el: Element) => el.getBoundingClientRect();
        const header = document.querySelector('[data-testid="header-with"]')!;
        const bare = document.querySelector('[data-testid="header-without"]')!;
        const word = header.querySelector('[data-testid="chat-header-activity"]')!;
        const free = document.querySelector('[data-testid="free-word"] [data-testid="chat-header-activity"]')!;
        const badge = header.querySelector(".presence-badge")!;
        const role = header.querySelector(".presence-badge__sub") as HTMLElement;
        const buttons = Array.from(header.querySelectorAll("button")).map(box);
        return {
          headerH: box(header).height,
          bareH: box(bare).height,
          word: box(word),
          freeW: box(free).width,
          badge: box(badge),
          roleClipped: role.scrollWidth > role.clientWidth,
          firstButtonLeft: Math.min(...buttons.map((b) => b.left)),
          lastButtonRight: Math.max(...buttons.map((b) => b.right)),
          vw: document.documentElement.clientWidth,
          hscroll: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        };
      });

      expect(Math.abs(m.headerH - m.bareH), "header height unchanged by the word").toBeLessThanOrEqual(0.5);
      expect(
        Math.abs(m.word.top + m.word.height / 2 - (m.badge.top + m.badge.height / 2)),
        "word on the badge's line",
      ).toBeLessThanOrEqual(2);
      expect(Math.abs(m.word.width - m.freeW), "word keeps its natural width").toBeLessThanOrEqual(0.5);
      expect(m.word.right, "word stays left of the header buttons").toBeLessThanOrEqual(m.firstButtonLeft);
      expect(m.lastButtonRight, "buttons stay on screen").toBeLessThanOrEqual(m.vw);
      if (width === 390) expect(m.roleClipped, "the role text is what gives way").toBe(true);
      expect(m.hscroll, "no page hscroll").toBeLessThanOrEqual(1);
    });
  }

  test(`width ${width}: hovering the word shows its hint inside the viewport`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(
      <ChatHeaderActivityStory mobile={width < 720} state="busy" roleName={LONG_ROLE} />,
    );
    await cmp.getByTestId("header-with").getByTestId("chat-header-activity").hover();
    const hint = page.getByRole("tooltip");
    await expect(hint).toBeVisible();
    const r = await hint.evaluate((el) => {
      const b = el.getBoundingClientRect();
      return { left: b.left, right: b.right, vw: document.documentElement.clientWidth };
    });
    expect(r.left).toBeGreaterThanOrEqual(0);
    expect(r.right).toBeLessThanOrEqual(r.vw);
  });
}
