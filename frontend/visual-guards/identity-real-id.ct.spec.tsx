// HOTSPOT — T-5dab: the identity badge carries the member's REAL id now. That
// string is longer than the retired `MB-XXX###` label (an outsource id is 15
// chars vs 9) and it contains hyphens, which are legal break opportunities; the
// badge's height is pinned at 21px, so a break spills out of it rather than
// growing it.
//
// MEASURED on this card before the fix (real InlineEdit + real action cluster in
// the chain, long member name): the badge pushed past the card's right edge by
// +7px @375, +22px @360 and +62px @320. The retired label overflowed only at
// ≤320 (+20px) — i.e. this change widened an existing narrow-screen defect into
// the phone widths people actually use, which is why the CSS moved with it
// (`.mp-identity__line` wraps, the NAME ellipses, the badge never shrinks).
//
// jsdom can see none of this: it resolves no line boxes and never evaluates
// `@media`, so the vitest half (MemberDetailPanel.identity-id.test.tsx) can only
// pin WHAT the badge renders, never whether it fits.
//
// Mutants — every line below was RUN at this granularity, not reasoned about
// (an independent review caught an earlier version of this block claiming a red
// that only happened when three declarations were removed together):
//   M1a drop `text-overflow: ellipsis` alone
//       → RED at 375 and 320 on "truncation shows an ellipsis". Note that
//         assertion is declaration-level ON PURPOSE: a rendered "…" has no
//         geometric trace at all, so no box measurement can see it.
//   M1b drop `overflow: hidden` alone          → GREEN
//   M1c drop `min-width: 0` alone              → GREEN
//   M1e drop `white-space: nowrap` from `.mp-identity__name` alone → GREEN
//       (structurally unmeasurable on these fixtures: the CJK name already has
//       break opportunities and the latin one has none either way.)
//   M1d drop all four together
//       → RED at 375 and 320 on "name right edge (unbreakable name)".
//     So M1b, M1c and M1e are each individually UNGUARDED here — they are
//     redundant with one another for the geometry, and only their joint absence
//     is caught. Do not read this file as covering any one of them alone.
//   M2  drop `white-space: nowrap` from `.mp-identity__id`
//       → GREEN. `flex: none` plus the wrapping row already give the badge a
//         full line at every width measured here, so nothing on this card can
//         force it to break today. The declaration is kept as defence for a
//         longer id shape than the roster mints now, and is NOT guarded.
//   M3  drop `flex-wrap: wrap` from `.mp-identity__line`
//       → RED at 320 on "name still visible" (the name is squeezed to nothing).
//
import { test, expect } from "@playwright/experimental-ct-react";
import { IdentityRealIdStory } from "./stories/IdentityRealIdStory";

// The longest id shape the roster mints (an outsource contractor: `ow-` + 12
// hex) paired with a real long member name — the badge competing for space
// exactly as it does on the busiest real card. The short pair is a seed member.
const LONG_ID = "ow-7eed74b85026";
const LONG_NAME = "OffiCraft 自動化測試員";
const SHORT_ID = "mira";
const SHORT_NAME = "銀月";

const VIEWPORTS = [
  { name: "desktop", width: 1200, height: 900 },
  { name: "phone", width: 375, height: 780 },
  // The narrowest width the panel is expected to survive; the pre-fix overflow
  // was worst here (+62px) and the old label already leaked (+20px).
  { name: "narrowest", width: 320, height: 780 },
];

for (const vp of VIEWPORTS) {
  test(`${vp.name}: the real id badge renders whole, unbroken, inside the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const cmp = await mount(
      <IdentityRealIdStory id={LONG_ID} name={LONG_NAME} />
    );

    const badge = cmp.locator(".mp-identity__id");
    await expect(badge).toBeVisible();
    // WHOLE — the id is the one string on this row that must never be
    // truncated; a clipped id addresses nobody.
    await expect(badge).toHaveText(LONG_ID);

    // UNBROKEN — line-box count, not height: a hyphen break produces a second
    // rect while the CSS-pinned height stays 21px, so a height assertion alone
    // would sail past it.
    expect(
      await badge.evaluate((el) => el.getClientRects().length),
      `${vp.name}: badge line boxes`
    ).toBe(1);
    const spill = await badge.evaluate((el) => ({
      x: el.scrollWidth - el.clientWidth,
      y: el.scrollHeight - el.clientHeight,
    }));
    expect(spill.x, `${vp.name}: badge horizontal clipping`).toBeLessThanOrEqual(1);
    expect(spill.y, `${vp.name}: badge vertical clipping`).toBeLessThanOrEqual(1);

    // INSIDE THE CARD at both ends — this is the assertion that was failing
    // before the CSS moved.
    const box = (await badge.boundingBox())!;
    const card = (await cmp.getByTestId("story-identity").boundingBox())!;
    expect(box.x, `${vp.name}: badge left edge`).toBeGreaterThanOrEqual(card.x - 1);
    expect(
      box.x + box.width,
      `${vp.name}: badge right edge`
    ).toBeLessThanOrEqual(card.x + card.width + 1);

    // …and the id does not win its space by evicting the name entirely. The
    // name may ellipse (that is the trade), but it must still be on screen.
    const nameBox = (await cmp.locator(".mp-identity__name").boundingBox())!;
    expect(nameBox.width, `${vp.name}: name still visible`).toBeGreaterThan(20);

    if (vp.name === "desktop") {
      // On a desktop-width card the pair still shares ONE row — the wrap is the
      // narrow-screen release valve, not the everyday layout.
      expect(
        Math.abs(box.y - nameBox.y),
        "desktop: name and badge share a row"
      ).toBeLessThan(box.height);
    }
  });

  test(`${vp.name}: an unbreakable long name cannot push the card open`, async ({
    mount,
    page,
  }) => {
    // A member name is free text the owner types. A CJK name wraps by itself,
    // but a long latin one has no break opportunity at all — and it shares the
    // row with a badge that is now `flex: none`. Without the name half being
    // allowed to ellipse, that name sets the row's min-content width and the
    // whole card grows past the viewport.
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const cmp = await mount(
      <IdentityRealIdStory id={LONG_ID} name="OffiCraftAutomationTesterLongName" />
    );
    const card = (await cmp.getByTestId("story-identity").boundingBox())!;
    expect(
      card.width,
      `${vp.name}: card width vs viewport`
    ).toBeLessThanOrEqual(vp.width);
    const badge = (await cmp.locator(".mp-identity__id").boundingBox())!;
    expect(
      badge.x + badge.width,
      `${vp.name}: badge right edge (unbreakable name)`
    ).toBeLessThanOrEqual(card.x + card.width + 1);
    // The name itself must stay inside too — it is the half that yields, and
    // yielding means ellipsing, not spilling over the card's edge.
    const nameBox = (await cmp.locator(".mp-identity__name").boundingBox())!;
    expect(
      nameBox.x + nameBox.width,
      `${vp.name}: name right edge (unbreakable name)`
    ).toBeLessThanOrEqual(card.x + card.width + 1);

    if (vp.width < 720) {
      // …and yielding means TRUNCATED, not merely boxed. The box geometry above
      // is satisfied by `min-width: 0` alone, which lets the box shrink while
      // the text runs on underneath — so the box test cannot tell "clipped" from
      // "spilling out of a small box". This pair can.
      const name = cmp.locator(".mp-identity__name");
      const clip = await name.evaluate((el) => ({
        overflowing: el.scrollWidth - el.clientWidth,
        textOverflow: getComputedStyle(el).textOverflow,
      }));
      expect(
        clip.overflowing,
        `${vp.name}: name is actually clipped, not just boxed`
      ).toBeGreaterThan(0);
      // The ellipsis itself leaves NO geometric trace (same box, same scroll
      // width) — a rendered "…" can only be told apart from a hard cut by
      // reading the declaration, so this one assertion is deliberately
      // declaration-level. It is here because a hard cut mid-glyph reads as a
      // rendering bug to the owner, not as "there is more name".
      expect(
        clip.textOverflow,
        `${vp.name}: truncation shows an ellipsis`
      ).toBe("ellipsis");
    }
  });

  test(`${vp.name}: the long-id fixture really is the crowded one`, async ({
    mount,
    page,
  }) => {
    // Directional control. Without it the "it fits" result above would also
    // pass on a roomy card, and the two fixtures would be one test twice.
    await page.setViewportSize({ width: vp.width, height: vp.height });

    const shortCmp = await mount(
      <IdentityRealIdStory id={SHORT_ID} name={SHORT_NAME} />
    );
    const shortW = (await shortCmp.locator(".mp-identity__id").boundingBox())!
      .width;
    await shortCmp.unmount();

    const longCmp = await mount(
      <IdentityRealIdStory id={LONG_ID} name={LONG_NAME} />
    );
    const longW = (await longCmp.locator(".mp-identity__id").boundingBox())!
      .width;

    expect(longW, `${vp.name}: long badge vs short badge`).toBeGreaterThan(
      shortW
    );
  });
}
