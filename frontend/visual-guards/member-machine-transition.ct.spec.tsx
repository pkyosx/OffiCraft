// HOTSPOT — T-927a: the member panel's 機器 block gained a SECOND line to fit.
//
// This is the replacement for the MEMBER half of relocate-progress-720 (see the
// note at the top of that file). The old guard measured whether the 機器 field
// block still held once 改機器 turned into 「更換中…」 plus a notice line under it.
// That control is gone: relocate folded into the unified settings submit, and the
// panel's visible pending state is now a 「→ 要換到 ○○」 hint line in that same
// 機器 block, carrying a machine id. Same block, same failure mode (an extra line
// whose content is a long unbreakable token), different occupant.
//
// Also measured here, and new: the identity ACTION row now holds two buttons for
// an online member (Stop plus 更改) where it held one.
//
// jsdom answers neither question — no flex, no @media, every width 0 — so the
// vitest suites cover the state machine and this covers the pixels. Widths span
// the phone case, the repo's 720px mobile breakpoint (frontend/AGENTS.md) and a
// desktop width, because narrow and wide can fail in opposite directions.
//
// Machine ids the registry does not know fall back to the raw id, so the story
// stages long labels without inventing mock machines — the realistic worst case
// (real machine ids in this fleet are long).
//
// Screenshots are written for the owner; path overridable so nothing lands in the
// repo tree by default:
//   MEMBER_TRANSITION_SHOT_DIR=/abs/dir npx playwright test \
//     -c playwright-ct.config.ts member-machine-transition
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { MemberMachineTransitionStory } from "./stories/RelocateProgressStory";

const WIDTHS = [375, 720, 1280] as const;
const SHOT_DIR =
  process.env.MEMBER_TRANSITION_SHOT_DIR ?? "test-results/member-transition";

/** Two different overflow shapes, both required — and the first one is the one a
 * boundingBox check CANNOT see. A block element's BOX is constrained by its
 * container, so text that refuses to wrap overflows the box while the rect still
 * measures as fitting (verified: forcing `white-space: nowrap` on the hint line
 * leaves every rect assertion green). What moves is `scrollWidth`.
 *
 * So: (a) no element inside the panel may have content wider than itself, and
 * (b) no ancestor may have absorbed the overflow into a horizontal scrollbar —
 * `documentElement` alone is not enough, because anything with `overflow-y: auto`
 * gets `overflow-x: auto` too and swallows it silently (T-49fb). */
async function expectNoOverflow(page: Page, width: number) {
  const worst = await page.evaluate(() => {
    // SCOPE: the two subtrees this guard is about, plus the page itself. A
    // whole-document walk is not usable here — the panel legitimately contains
    // deliberate scroll regions (the tmux command line is a horizontally
    // scrollable `code`), and flagging those turns the guard into noise.
    const roots = [
      document.querySelector<HTMLElement>(".mp-identity__actions"),
      ...Array.from(
        document.querySelectorAll<HTMLElement>(".mp-field"),
      ).filter((el) =>
        el.querySelector('[data-testid="mp-machine-pending"]'),
      ),
    ].filter((el): el is HTMLElement => el != null);
    let worstDelta =
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth;
    let worstWhere = worstDelta > 0 ? "html (the page scrolls sideways)" : "";
    for (const root of roots) {
      for (const el of [root, ...Array.from(root.querySelectorAll<HTMLElement>("*"))]) {
        // A deliberately scrollable element is allowed to hold wider content.
        const overflowX = getComputedStyle(el).overflowX;
        if (overflowX === "auto" || overflowX === "scroll") continue;
        const delta = el.scrollWidth - el.clientWidth;
        if (delta > worstDelta) {
          worstDelta = delta;
          worstWhere = `${el.tagName.toLowerCase()}.${el.className || "(no class)"}`;
        }
      }
    }
    return { delta: worstDelta, where: worstWhere };
  });
  expect(
    worst.delta,
    `horizontal overflow at ${width}px, worst offender: ${worst.where}`,
  ).toBeLessThanOrEqual(1);
}

/** …plus the plain geometry check: the element's own right edge stays inside the
 * viewport. Measured on the element itself, never a parent — a parent rect is
 * usually clamped back to the viewport and looks fine (T-d451). */
async function expectRightEdgeInside(
  el: Locator,
  width: number,
  what: string,
) {
  const box = await el.boundingBox();
  expect(box, `${what} box`).not.toBeNull();
  expect(
    box!.x + box!.width,
    `${what} right edge at ${width}px`,
  ).toBeLessThanOrEqual(width + 1);
}

for (const width of WIDTHS) {
  test(`width ${width}: the 機器 block holds observed machine + 「→ 要換到」 transition`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<MemberMachineTransitionStory />);

    const transition = cmp.getByTestId("mp-machine-pending");
    await expect(transition).toBeVisible({ timeout: 10_000 });
    // Positive control: the block really is carrying BOTH values, so a layout that
    // passes by rendering nothing cannot pass this guard.
    await expect(cmp.getByTestId("mp-machine")).toContainText("eva-m5");
    await expect(transition).toContainText("要換到");

    const block = page
      .locator(".mp-field")
      .filter({ has: page.getByTestId("mp-machine-pending") });
    await expectRightEdgeInside(block, width, "機器 field block");
    await expectRightEdgeInside(transition, width, "transition label");
    await expectNoOverflow(page, width);

    // The action row for an online member: Stop plus the unified 更改 entry.
    const actions = page.locator(".mp-identity__actions");
    await expect(cmp.getByTestId("mp-change")).toBeVisible();
    await expectRightEdgeInside(actions, width, "identity action row");

    await page.screenshot({ path: `${SHOT_DIR}/member-transition-${width}.png` });
  });
}
