// T-d593 — the unread badge's 1px ring must be a THEMEABLE slot.
//
// owner 2026-08-01 (rc-1d57d0adc87d 選②), verbatim: 「外框完全自由,不留下限
// (主題調到看不見也算你的選擇)」. Before this ticket the ring was
// `outline: 1px solid var(--color-bg)` at all three rules — a theme could only
// move it by moving the whole page background.
//
// WHY THIS IS A CT GUARD AND NOT jsdom: jsdom does not compute CSS. It resolves
// no `var()`, loads no stylesheet, and `getComputedStyle(el).outlineColor` there
// answers about a browser default that never ships. Every assertion below reads
// the colour the browser ACTUALLY painted, which is the only form of this claim
// worth pinning. The companion source scan (src/components/badgeRing.test.ts)
// covers what jsdom CAN answer, and — importantly — runs in the unit cell
// (make test-frontend-unit), which is a different cloud cell from this file's
// (make test-frontend-ct).
//
// 🔴 DISCRIMINATING POWER, stated honestly per assertion, because two of the
// three would have passed BEFORE this change and must not be miscounted as
// evidence for it:
//   * "default matches --color-bg"      → ZERO power for this diff (the old CSS
//     hardcoded exactly that). It is here for a DIFFERENT invariant: existing
//     themes must look unchanged. Keep it, don't credit it.
//   * "ring follows --color-bg when the ring slot is unset" → also zero power
//     for the diff; it pins the ALIAS default specifically (a solid #191c24
//     default would fail it, and that mutant is real — it would silently break
//     every theme that repaints --color-bg).
//   * "setting --color-danger-badge-ring repaints all three" → THE ONE that
//     fails on the pre-change tree. Mutant run recorded in the ticket: reverting
//     the three CSS rules to var(--color-bg) turns exactly this test red.
import { test, expect } from "@playwright/experimental-ct-react";
import { BadgeRingStory } from "./stories/BadgeRingStory";

const RING_IDS = ["ring-nav", "ring-tab", "ring-card"] as const;

/** outlineColor as the browser resolved it, per badge rule. */
async function ringColours(cmp: { getByTestId: (id: string) => { evaluate: (fn: (e: Element) => string) => Promise<string> } }) {
  const out: Record<string, string> = {};
  for (const id of RING_IDS) {
    out[id] = await cmp
      .getByTestId(id)
      .evaluate((el) => getComputedStyle(el as Element).outlineColor);
  }
  return out;
}

test("default: all three badge rules paint the ring in the page colour (existing themes unchanged)", async ({ mount, page }) => {
  const cmp = await mount(<BadgeRingStory />);
  const pageColour = await page.evaluate(() =>
    getComputedStyle(
      // Resolve --color-bg the same way the ring's alias default does.
      document.documentElement
    ).getPropertyValue("--color-bg").trim()
  );
  expect(pageColour, "--color-bg must be defined by theme.css").not.toBe("");

  // Paint a probe with the raw token so we compare resolved rgb() to resolved
  // rgb() rather than "#191c24" to "rgb(25, 28, 36)".
  const expected = await page.evaluate((raw) => {
    const probe = document.createElement("span");
    probe.style.outlineColor = raw;
    document.body.appendChild(probe);
    const v = getComputedStyle(probe).outlineColor;
    probe.remove();
    return v;
  }, pageColour);

  const got = await ringColours(cmp as never);
  for (const id of RING_IDS) {
    expect(got[id], `${id} ring must default to the page colour`).toBe(expected);
  }
});

test("the ring slot repaints ALL THREE rules (this is the assertion the pre-change tree fails)", async ({ mount, page }) => {
  const cmp = await mount(<BadgeRingStory />);
  const before = await ringColours(cmp as never);

  // A colour that is nothing else in the palette, so a pass cannot be a
  // coincidence of some other token resolving to the same value.
  await page.evaluate(() => {
    document.documentElement.style.setProperty("--color-danger-badge-ring", "rgb(1, 222, 3)");
  });
  const after = await ringColours(cmp as never);

  for (const id of RING_IDS) {
    expect(after[id], `${id} must follow --color-danger-badge-ring`).toBe("rgb(1, 222, 3)");
    expect(after[id], `${id} must actually have CHANGED (guards against both values being equal by accident)`).not.toBe(before[id]);
  }
});

test("owner's ruling: a theme MAY make the ring vanish, and nothing stops it", async ({ mount, page }) => {
  // 「主題調到看不見也算你的選擇」— the ring set to the fill colour, which the
  // retired MIN_PILL_VS_PAGE floor used to forbid. This must simply work.
  const cmp = await mount(<BadgeRingStory />);
  const fill = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--color-danger-badge").trim()
  );
  await page.evaluate((f) => {
    document.documentElement.style.setProperty("--color-danger-badge-ring", f);
  }, fill);

  const got = await ringColours(cmp as never);
  const bg = await cmp
    .getByTestId("ring-nav")
    .evaluate((el) => getComputedStyle(el as Element).backgroundColor);
  for (const id of RING_IDS) {
    expect(got[id], `${id} ring must be paintable to the fill colour`).toBe(bg);
  }
});

test("alias default: with the ring slot unset, moving --color-bg still moves the ring", async ({ mount, page }) => {
  // Pins that the default is `var(--color-bg)` and NOT a baked solid. A solid
  // default is the tempting mutant, and it would silently break every existing
  // theme that repaints --color-bg — the ring would stay on the built-in navy
  // while its page moved.
  const cmp = await mount(<BadgeRingStory />);
  await page.evaluate(() => {
    document.documentElement.style.setProperty("--color-bg", "rgb(4, 5, 201)");
  });
  const got = await ringColours(cmp as never);
  for (const id of RING_IDS) {
    expect(got[id], `${id} ring must still follow --color-bg while its own slot is unset`).toBe("rgb(4, 5, 201)");
  }
});
