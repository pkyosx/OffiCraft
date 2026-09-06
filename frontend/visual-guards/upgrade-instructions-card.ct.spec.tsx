// HOTSPOT — 設定 › 換版交代單, the row that carries a path, a sha and two
// buttons at the same time.
//
// WHAT jsdom CANNOT SEE, and why this file exists: the card's vitest file
// (SettingsPage.upgrade-instructions.test.tsx) asserts that an open instruction
// HAS a 標記完成 button and a 收回 button. In jsdom that passes whether the
// buttons are on screen or pushed past the right edge by the sentence beside
// them — there is no layout engine, so "in the DOM" and "reachable" are the
// same sentence. On a phone they are not, and a 收回 control the owner cannot
// reach is one that does not exist.
//
// THE FIXTURE IS AN ORDINARY INSTRUCTION, not a contrived one. Telling the
// assistant WHICH file to check is the point of the feature, so the mock's
// second open row names a migration path and a full 40-character sha. Neither
// can break at a space, and that row is open, so it carries both buttons. That
// combination is the widest this card ever gets, and it is what the owner will
// type on day one.
//
// MUTANTS, measured on the real sheet in this browser and REPORTED AS MEASURED:
//
//   (a) drop `overflow-wrap: anywhere` from `.upgrade-instr__body`
//         → RED at 320 and 375. The sha cannot break, so the body column takes
//           its own min-content width and drags the grid past the card. This
//           is the load-bearing declaration.
//   (b) drop the ≤520px media query (keep the three-column row at every width)
//         → GREEN on the overflow assertions at EVERY width. This header first
//           claimed it went red; it does not, and the difference matters to
//           whoever edits this CSS next. Nothing spills, because (a) lets the
//           body column shrink to a single character — so the row fits by
//           making the sentence unreadable rather than by breaking.
//           What it DOES cost is measured, not asserted from taste: at 320 the
//           instruction text goes from 166px to 36px (about two characters) in
//           a 276px card, and at 375 from 221px to 91px. That is the
//           〈把句子擠成一條〉 trap in css-layout-traps.md, and it is what
//           assertion (4) below pins — the media query is load-bearing for
//           READABILITY, not for overflow. Do not delete it on the strength of
//           a green overflow guard.
//
// CONTROL: 1040 is expected green for every mutant and is NOT counted as
// coverage — it says a fix did not simply move the breakage to desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { UpgradeInstructionsCardStory } from "./stories/UpgradeInstructionsCardStory";

for (const width of [320, 375, 1040]) {
  test(`width ${width}: a long instruction does not spill and both row controls stay on the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<UpgradeInstructionsCardStory />);

    // The list really arrived. A guard that measures an empty card has found
    // nothing to check, which must not read as having found nothing wrong.
    await expect(cmp.getByTestId("set-upgrade-instructions-count")).toBeVisible();

    // The hostile row: open (so it carries BOTH buttons) and carrying the
    // unbreakable path + sha.
    const row = cmp.getByTestId("set-upgrade-instruction-uin-91be07d3a45f");
    await expect(row).toBeVisible();
    await expect(row).toContainText("00084_lore_format_v8.sql");

    // (1) Nothing spills: not the row, not the card, not the scrollable
    // settings surface, not the page. The surface is measured as well as the
    // page because `.settings` is overflow-y:auto, which coerces overflow-x to
    // auto — it absorbs the overflow as an internal pan and leaves the
    // page-level number at 0 (the T-23df lesson).
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        row: over(
          document.querySelector(
            '[data-testid="set-upgrade-instruction-uin-91be07d3a45f"]',
          )!,
        ),
        card: over(
          document.querySelector('[data-testid="set-upgrade-instructions"]')!,
        ),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }

    // (2) …and BOTH controls really are inside the card, not merely inside a
    // row that grew to fit them. The card is the box the reader sees; a control
    // beyond its right edge is off the card even when every scrollWidth above
    // is happy. 收回 is the irreversible one, so it is the one that must never
    // be the button that fell off.
    const cardBox = (await cmp
      .getByTestId("set-upgrade-instructions")
      .boundingBox())!;
    for (const id of [
      "set-upgrade-instruction-done-uin-91be07d3a45f",
      "set-upgrade-instruction-remove-uin-91be07d3a45f",
    ]) {
      const box = (await cmp.getByTestId(id).boundingBox())!;
      expect(box.x + box.width, `${id} right edge vs card`).toBeLessThanOrEqual(
        cardBox.x + cardBox.width + 1,
      );
      expect(box.x, `${id} left edge vs card`).toBeGreaterThanOrEqual(
        cardBox.x - 1,
      );
    }

    // (3) The textarea the owner types into is inside the card too. It is
    // width:100% of a grid the row above can widen, so it is the second thing
    // a spill takes with it.
    const input = (await cmp
      .getByTestId("set-upgrade-instructions-input")
      .boundingBox())!;
    expect(
      input.x + input.width,
      "the compose box right edge vs card",
    ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
    // (4) THE ONE OVERFLOW CANNOT CATCH: the sentence still gets
    // most of the row. Overflow alone cannot catch the failure this card is most likely to have on a phone: a row that fits
    // BECAUSE the text was squeezed to a sliver reads as green everywhere
    // above. Measured on this sheet: with the ≤520px stack the body takes ~60%
    // of the card at 320 and ~67% at 375; with the three-column row forced at
    // every width it drops to ~13% and ~27%. Half is the line between those
    // two worlds.
    const share = await page.evaluate(() => {
      const row = document.querySelector(
        '[data-testid="set-upgrade-instruction-uin-91be07d3a45f"]',
      )!;
      const body = row.querySelector(".upgrade-instr__body")!;
      const card = document.querySelector(
        '[data-testid="set-upgrade-instructions"]',
      )!;
      return (
        body.getBoundingClientRect().width / card.getBoundingClientRect().width
      );
    });
    expect(share, "the instruction text's share of the card").toBeGreaterThan(0.5);
  });
}
