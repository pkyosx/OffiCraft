// HOTSPOT — 設定 › 角色誌 › 啟動程序 on a phone (T-bac4, replacing T-6278).
//
// THE DEFECT UNDERNEATH ALL THREE SHAPES IS A GEOMETRY DEFECT, AND IT WAS
// REPORTED FROM A PHONE. The page originally rendered both documents in full,
// stacked, so 啟動程序（Codex CLI）sat thousands of pixels below the fold; the
// owner scrolled to the end of the first card, read the card's bottom edge as
// the end of the page, and reported the second document as missing. Nothing was
// broken in the DOM, which is exactly why jsdom cannot see it — the sibling
// test (src/components/SettingsPage.boot-nav.test.tsx) pins the STATE, and this
// file pins the consequence he actually met: BOTH DOCUMENTS ARE VISIBLE AS
// SEPARATE ROWS ON ONE SCREEN.
//
// T-6278 answered it by collapsing both cards. The owner replaced that shape
// outright («我覺得呈現方式不好,可以改成像任務手冊那樣嗎») with the 任務手冊 hub's
// nav rows, which is what this now measures.
//
// ⚠️ THE FIXTURE'S LENGTH IS NOT LOAD-BEARING — this header claimed it was, and
// the independent review DISPROVED it by measurement: cut the seeded documents
// from 40 sections to 1 and all five tests stay GREEN. Nothing asserted here
// depends on the documents' height, because the index renders no document at
// all. The length is kept only so the fixture still resembles the real page.
// Do not re-promote it to a guarantee: if you want the stacked shape to fail on
// GEOMETRY rather than on a missing testid, that assertion does not exist yet
// and would have to be written.
//
// MUTANTS — planted and run on THIS browser for this file, and reported as the
// run printed them, including the part that is weaker than it looks:
//   * SHAPE REVERT: put T-6278's stacked+collapsible shape back verbatim (two
//     <BootDocPage> inside `.settings-stack`) → all 4 tests RED at every width.
//     ⚠️ HONEST LIMIT: they die in the walk, on `boot-entry-claude` not
//     existing — NOT on the geometry assertion. So this mutant proves the file
//     notices the shape being reverted; it does NOT exercise (1). Do not quote
//     it as evidence that the fold check works.
//   * GEOMETRY (the one that does exercise (1)): a 900px spacer between the two
//     rows, ids intact → RED on (1) at 320/390/402/1040 with the codex row
//     bottom at 1208.5 / 1176.5 / 1176.5 / 1176.5 against the 844 fold. This is
//     the assertion's real discriminating power, measured.
//   * ACCESSIBILITY: give both rows one shared aria-label (T-6278's actual
//     defect — its collapse toggles announced the same name, rebuilding "two
//     documents you cannot tell apart" in the accessibility tree) → RED on (2)
//     at every width, count 0 instead of 1.
//   * CROSSWIRE (added by the independent review, and it earns its place): hard
//     -code `docKey="claude"` in the bootDoc view → RED, but ONLY on the jsdom
//     wiring test, not here. That one test is the sole defence against a save
//     landing on the wrong runtime; do not delete it as redundant.
// Both figures above were re-measured by the reviewer on a clean clone and came
// back identical.
// WIDTHS: 402 is the owner's own phone; 390 and 320 bracket it (320 is the
// narrowest supported). Width is an INPUT dimension here — the row title is the
// longest string on the page, so the narrow end is where it would wrap or
// overflow first.
// CONTROL, and it is a WEAK one here: 1040 (the desktop content column) is
// green in the clean tree, but BOTH mutants above take it red too — the index
// shape has no width-dependent behaviour to separate phone from desktop. It is
// kept because a future change that breaks only desktop should still be seen,
// not because it isolates anything today.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootNavStory } from "./stories/BootNavStory";
import { zh } from "../src/i18n/locales/zh";

const s = zh.settings;

/** Walk in the way a person does: 設定 landing → 角色誌 → 啟動程序. */
async function openBootIndex(page: import("@playwright/test").Page) {
  await page.getByText(s.roles).first().click();
  await page.getByText(s.bootName).first().click();
  await expect(page.getByTestId("boot-entry-claude")).toBeVisible();
}

for (const width of [320, 390, 402, 1040]) {
  test(`width ${width}: both boot documents are rows on one screen`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await mount(<BootNavStory />);
    await openBootIndex(page);

    // (1) THE CORE red→green. He must SEE that a second document exists
    // without discovering it by scrolling. Both rows, whole, inside the first
    // screen — bottom edge, not just the top, or a row half off the fold would
    // pass.
    for (const [name, tid] of [
      ["claude", "boot-entry-claude"],
      ["codex", "boot-entry-codex"],
    ] as const) {
      const row = page.getByTestId(tid);
      await expect(row).toBeVisible();
      const box = (await row.boundingBox())!;
      expect(box.y, `[${width}] ${name} row top`).toBeGreaterThanOrEqual(0);
      expect(
        box.y + box.height,
        `[${width}] ${name} row bottom vs the 844px fold`
      ).toBeLessThanOrEqual(844);
    }

    // (2) …and the names reach a screen reader, not just the eye. T-6278's
    // review sent that build back exactly here: an aria-label on its collapse
    // toggle overrode both headings, so BOTH buttons reported 展開這份文件 and
    // this lookup matched nothing.
    for (const name of [s.bootClaudeName, s.bootCodexName]) {
      await expect(
        page.getByRole("button", { name: new RegExp(escapeRe(name)) }),
        `[${width}] accessible name: ${name}`
      ).toHaveCount(1);
    }

    // (3) The index carries NO document body. Not collapsed — absent. Without
    // this, "both rows fit" would only be true of this fixture's length.
    expect(await page.locator(".doc-md").count(), "open documents").toBe(0);

    // (4) Nothing spills sideways. The row titles carry the longest words on
    // the page (「啟動程序（Claude Code）」) next to a fixed-size icon and a
    // chevron, which is the geometry most likely to overflow the narrowest
    // phone.
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      const entries = document.querySelector(".set-entries")!;
      return {
        entries: over(entries),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `[${width}] ${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }
  });
}

test("a row opens THAT runtime's page, and the other is not on it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<BootNavStory />);
  await openBootIndex(page);

  await page.getByTestId("boot-entry-claude").click();
  await expect(page.locator(".doc-md")).toHaveCount(1);
  // ABSENT, not merely closed: the two runtimes' third step means opposite
  // things (claude attaches `ocagent listen` itself; codex must NOT — the
  // sidecar does), so a page showing both invites copying one over the other,
  // which stops that runtime's agents ever coming online, silently.
  await expect(page.getByText(s.bootCodexName)).toHaveCount(0);

  // And the trail leads back, so the other runtime is one press away rather
  // than a trip out to 角色誌 and back in.
  await page.locator(".crumbs__link", { hasText: s.bootName }).click();
  await expect(page.getByTestId("boot-entry-codex")).toBeVisible();
});

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
