// HOTSPOT — 設定 › 簽章金鑰, the row that is widest in the state every install
// starts in.
//
// WHAT jsdom CANNOT SEE, and why this file exists: the card's vitest file
// (SettingsPage.signing-keys-t62.test.tsx) asserts that a retired key HAS a
// remove button. In jsdom that assertion passes whether the button is on screen
// or pushed past the right edge by the row beside it — there is no layout
// engine, so "in the DOM" and "reachable" are the same sentence. On a phone
// they are not, and a revocation control that is off-screen is a control that
// does not exist.
//
// THE FIXTURE IS THE DEFAULT STATE, not a contrived one. The mock ring's first
// key carries `created_ts: 0`, which renders as the long "in use since before
// this was recorded" sentence — so the widest row in this card is exactly the
// row every install that has never rotated shows on first open, with the
// remove button on it after one rotation.
//
// MUTANTS, measured on the real sheet in this browser — REPORTED AS MEASURED,
// including the one that contradicted what this header first claimed:
//
//   (a) drop `flex-wrap: wrap` from `.signing-keys__row`
//         → GREEN at every width. This header originally asserted it went red;
//           it does not, and the difference matters to whoever edits this CSS
//           next: `flex-wrap` is NOT what holds this row together. Without it
//           the row still fits, because the created-at sentence wraps INSIDE
//           its own span and the row shrinks to suit.
//   (b) `flex-wrap: nowrap` + `white-space: nowrap` on the same rule
//         → RED at 320 and 375 (the row cannot break anywhere, so the sentence
//           and the button run past the card), green at 1040.
//
// So what this guard actually protects is "the row's content is allowed to
// break", not any one declaration. Do not "restore" (a) as a load-bearing rule
// on the strength of a comment.
//
// ⚠️ The fixture had to be fixed before either measurement meant anything: the
// mock ring first used `k-mock0`, while the server mints `k-` + 16 hex
// (keyring.go newKeyID). A mock modelling a NARROWER row than production hides
// exactly the defect this file is here to catch, and with it in place mutant
// (b) was green too.
//
// CONTROL: 1040 is expected green for every mutant and is NOT counted as
// coverage — it is there to say a fix did not simply move the breakage to
// desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { SigningKeysCardStory } from "./stories/SigningKeysCardStory";

for (const width of [320, 375, 1040]) {
  test(`width ${width}: the ring does not spill and the remove control stays on the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<SigningKeysCardStory />);

    // The ring really arrived. A guard that measures an empty card has found
    // nothing to check, which must not read as having found nothing wrong.
    await expect(cmp.getByTestId("set-signing-keys-count")).toBeVisible();

    // Rotate so a RETIRED key exists — a ring with one key has no remove button
    // at all, and the geometry claim below would be vacuous.
    await cmp.getByTestId("set-signing-keys-rotate").click();
    const retired = cmp.locator('[data-signing="no"]').first();
    await expect(retired).toBeVisible();

    // (1) Nothing spills: not a row, not the card, not the scrollable settings
    // surface, not the page. The surface is measured as well as the page
    // because `.settings` is overflow-y:auto, which coerces overflow-x to auto
    // — it absorbs the overflow as an internal pan and leaves the page-level
    // number at 0 (the T-23df lesson).
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        row: over(document.querySelector('[data-signing="no"]')!),
        card: over(document.querySelector('[data-testid="set-signing-keys"]')!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }

    // (2) …and the remove button really is inside the card, not merely inside a
    // row that grew to fit it. The card is the box the reader sees; a control
    // beyond its right edge is off the card even when every scrollWidth above
    // is happy.
    const cardBox = (await cmp.getByTestId("set-signing-keys").boundingBox())!;
    const btnBox = (await retired.locator("button").boundingBox())!;
    expect(
      btnBox.x + btnBox.width,
      "remove button right edge vs card",
    ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
    expect(btnBox.x, "remove button left edge vs card").toBeGreaterThanOrEqual(
      cardBox.x - 1,
    );
  });
}
