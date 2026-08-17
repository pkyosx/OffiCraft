// HOTSPOT — the boot-context block page at phone widths.
//
// SUCCESSOR TO `boot-doc-section-row.ct.spec.tsx` (T-c33e). That file measured
// a surface that no longer exists — the per-section rows — and this file has
// since had to give up half of what it inherited, so read the history before
// "restoring" anything:
//
//   1. 還原出廠版 MUST BE ON SCREEN — RETIRED, NOT LOST. The claim was that a
//      recovery button present but pushed off a phone viewport is not a
//      recovery path. It was measured on a TOP-LEVEL restore button, and the
//      owner removed that button on 2026-08-14 (card rc-f1950f4d286e, option 2:
//      "完全照 insight") with the cost stated on the card. So the geometry
//      claim now applies to where the restore actually lives: inside edit mode,
//      in the history list's 初始版本 row. That is what this file measures, and
//      it is a WEAKER guarantee than the retired one by exactly the amount the
//      owner chose to give up — a page whose read failed reaches no restore at
//      all. Do not add a top-level button back to satisfy this file.
//   2. THE CARD MUST NOT SPILL on owner/agent prose carrying a long unbreakable
//      token — UNCHANGED. The old guard measured that on the section-row LABEL;
//      the token now lands in the rendered document (`.doc-md`), so the same
//      fixture text is measured on the same chain — head, card, `.settings`,
//      page.
//      → the mutant that used to bite (`overflow-wrap: anywhere` off
//        `.boot-doc-sec__label`) is replaced by the same declaration on
//        `.doc-md` in settings.css, which is where that class of defect has
//        lived for every other document surface since T-d451.
//
// Ancestor chain is reproduced by class in the story (see its header): mounting
// a bare card at x≈0 buys ~22px of slack the app does not have, which is how
// earlier 390px guards stayed green on a broken phone.
//
// MUTANTS, measured on the real sheet in this browser — reported as measured:
//   drop `overflow-wrap: anywhere` from `.doc-md` (settings.css)
//     → RED at 320 AND 375; green at 390/1040. (The same mutant reddens
//       boot-doc-real-seed.ct.spec.tsx at 320 only, +45px — this fixture's
//       token is longer relative to the column, so it bites one width wider.)
//       Do not read the two greens as the guard being weak, and do not
//       "strengthen" it by loosening the tolerance.
// CONTROL: 1040 (the desktop content column's max width) is expected green for
// every mutant and is NOT counted as coverage — it is there to say the fix did
// not simply move the breakage to desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootDocCardStory } from "./stories/BootDocCardStory";

for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: the card does not spill and the restore stays reachable`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<BootDocCardStory />);

    // The story seeds the document through the real adapter before mounting the
    // page. The document really arrived: the heading carrying the unbreakable
    // token is the whole reason this fixture exists, so measuring before it
    // lands would measure an empty card — a guard that finds nothing to check
    // must not read as a guard that found nothing wrong.
    await expect(cmp.locator(".doc-md")).toContainText("abcdef0123456789");

    // (2) Nothing spills: not the card head, not the card, not the scrollable
    // settings surface, not the page. The surface is measured as well as the
    // page because `.settings` is overflow-y:auto, which coerces overflow-x to
    // auto — it silently absorbs the overflow as an internal pan and leaves the
    // page-level number at 0 (the T-23df lesson).
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        head: over(document.querySelector(".doc-card__head")!),
        note: over(document.querySelector(".doc-card__note")!),
        card: over(document.querySelector(".doc-card")!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }

    // …and the head's own controls really are inside the card, not merely
    // inside a head that grew to fit them. `.doc-card` is the box the reader
    // sees; a button beyond its right edge is off the card even when every
    // scrollWidth above is happy.
    const cardBox = (await cmp.locator(".doc-card").boundingBox())!;
    for (const id of ["doc-card-usage", "doc-card-edit"]) {
      const b = (await cmp.getByTestId(id).boundingBox())!;
      expect(
        b.x + b.width,
        `${id} right edge vs card`
      ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
      expect(b.x, `${id} left edge vs card`).toBeGreaterThanOrEqual(
        cardBox.x - 1
      );
    }

    // (3) And the editor the section rows were replaced by is itself inside the
    // card — a whole-document textarea is new geometry on this page, and a
    // fixed-width one would pan the surface exactly like a long token does.
    await cmp.getByTestId("doc-card-edit").click();
    const editorBox = (await cmp.getByTestId("doc-card-editor").boundingBox())!;
    expect(editorBox.width, "editor width").toBeGreaterThan(80);
    expect(
      editorBox.x + editorBox.width,
      "editor right edge vs card"
    ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
    const editSpill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        card: over(document.querySelector(".doc-card")!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(editSpill)) {
      expect(o, `${where} horizontal overflow while editing`).toBeLessThanOrEqual(1);
    }
    // (4) The recovery path is reachable AT THIS WIDTH. It is behind edit mode
    // now (see the header), so the walk starts here — and the point of doing it
    // on a phone is that every box on the way must be pressable, not merely
    // present: the history entry, then the 初始版本 row inside the list.
    const entry = cmp.getByTestId("doc-history-entry-boot_sequence");
    await expect(entry).toBeVisible();
    const entryBox = (await entry.boundingBox())!;
    expect(entryBox.x, "版本紀錄 left edge").toBeGreaterThanOrEqual(-0.5);
    expect(
      entryBox.x + entryBox.width,
      "版本紀錄 right edge vs viewport"
    ).toBeLessThanOrEqual(width + 0.5);
    expect(entryBox.width, "版本紀錄 tappable width").toBeGreaterThan(40);

    await entry.click();
    const seed = cmp.getByTestId("doc-history-seed-open");
    await expect(seed).toBeVisible();
    const seedBox = (await seed.boundingBox())!;
    expect(seedBox.x, "初始版本 left edge").toBeGreaterThanOrEqual(-0.5);
    expect(
      seedBox.x + seedBox.width,
      "初始版本 right edge vs viewport"
    ).toBeLessThanOrEqual(width + 0.5);
    expect(seedBox.width, "初始版本 tappable width").toBeGreaterThan(40);
  });
}
