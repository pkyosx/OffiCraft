// 分享 page rows: the expiry never runs into the revoke icon, and a long name
// truncates in its own column.
//
// The trial-site 1440 screenshot: a link expiring next year reads
// 「2027/11/28 23:34」, which did not fit the 8em expiry column and ran under
// the revoke icon at the row's right end. jsdom has no layout, so this mounts
// the real ShareLinksPage under `.app__main` in the production fonts (the CT
// page ships none, and the fit depends on text width) and measures each row.
//
// MUTANTS (each verified red):
//   desktop expiry column 10em → 8em           → 1280 and 1440 (expiry clipped, under the icon)
//   phone expiry column auto → 4em              → 390 (expiry clipped, under the icon)
//   name `text-overflow: ellipsis` dropped      → every width (the long name)
//   head's 12px back on the row (em columns shrink) → 1280 and 1440 (columns off their headers)
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ShareLinksPageStory } from "./stories/ShareLinksPageStory";
import { LONG_FILENAME } from "./stories/shareLinksFixtures";

const NOW = new Date(2026, 10, 28, 23, 34, 30);

async function loadProductionFonts(page: Page) {
  const html = fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "../index.html"), "utf8");
  const href = html.match(/href="(https:\/\/fonts\.googleapis\.com\/css2\?[^"]+)"/)![1].replace(/&amp;/g, "&");
  const lang = html.match(/<html lang="([^"]+)"/)![1];
  await page.evaluate(
    async ([href, lang]) => {
      document.documentElement.lang = lang;
      const link = document.createElement("link");
      link.rel = "stylesheet";
      link.href = href;
      const loaded = new Promise((resolve, reject) => {
        link.onload = resolve;
        link.onerror = reject;
      });
      document.head.appendChild(link);
      await loaded;
      await Promise.all([
        document.fonts.load('13px "Schibsted Grotesk"', "2027/11/28 23:34 quarterly"),
        document.fonts.load('13px "Noto Sans TC"', "永久分享建立者到期"),
      ]);
    },
    [href, lang] as const
  );
  expect(
    await page.evaluate(() => [
      document.fonts.check('13px "Schibsted Grotesk"', "2"),
      document.fonts.check('13px "Noto Sans TC"', "永"),
    ]),
    "control: the production fonts loaded (they come from Google Fonts)"
  ).toEqual([true, true]);
}

async function measure(page: Page) {
  return page.evaluate(() => {
    const textRight = (el: Element) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      return range.getBoundingClientRect().right;
    };
    const rows = [...document.querySelectorAll('[data-testid="share-link-row"]')].map((row) => {
      const name = row.querySelector(".share-page__name") as HTMLElement;
      const by = row.querySelector(".share-page__by") as HTMLElement;
      const exp = row.querySelector('[data-testid="share-link-expires"]') as HTMLElement;
      const revoke = row.querySelector('[data-testid="share-link-revoke"]')!.getBoundingClientRect();
      const byShown = getComputedStyle(by).display !== "none";
      return {
        name: name.textContent!,
        nameRight: name.getBoundingClientRect().right,
        nameTruncated: name.scrollWidth > name.clientWidth,
        nameEllipsis: getComputedStyle(name).textOverflow,
        byLeft: byShown ? by.getBoundingClientRect().left : null,
        expText: exp.textContent!,
        expLeft: exp.getBoundingClientRect().left,
        expRight: exp.getBoundingClientRect().right,
        expTextRight: textRight(exp),
        expOverflow: exp.scrollWidth - exp.clientWidth,
        revokeLeft: revoke.left,
        revokeRight: revoke.right,
        rowRight: row.getBoundingClientRect().right,
      };
    });
    const head = document.querySelector(".share-page__row--head") as HTMLElement;
    const headShown = getComputedStyle(head).display !== "none";
    const headByLeft = headShown ? head.children[2].getBoundingClientRect().left : null;
    const headExpLeft = headShown ? head.children[3].getBoundingClientRect().left : null;
    const over = (el: Element) => el.scrollWidth - el.clientWidth;
    return {
      rows,
      headByLeft,
      headExpLeft,
      pageOverflow: over(document.documentElement),
      mainOverflow: over(document.querySelector(".app__main")!),
      shareOverflow: over(document.querySelector(".share-page")!),
    };
  });
}

for (const width of [390, 1280, 1440]) {
  test(`${width}px: every expiry fits its column left of the revoke icon, and the long name truncates in place`, async ({
    mount,
    page,
  }) => {
    await page.clock.setFixedTime(NOW);
    await page.setViewportSize({ width, height: 900 });
    await loadProductionFonts(page);
    await mount(<ShareLinksPageStory />);
    await expect(page.getByTestId("share-link-row")).toHaveCount(4);

    const m = await measure(page);
    expect(
      m.rows.filter((r) => /^2027\/11\/28 23:33$/.test(r.expText)).length,
      "control: the next-year links show the year"
    ).toBe(2);

    for (const r of m.rows) {
      const label = `${r.name} (${r.expText || "永久"})`;
      expect(r.expOverflow, `${label}: expiry not clipped by its column`).toBeLessThanOrEqual(0);
      expect(r.expTextRight, `${label}: expiry text ends inside its column`).toBeLessThanOrEqual(r.expRight + 0.5);
      expect(r.expTextRight, `${label}: expiry ends before the revoke icon`).toBeLessThanOrEqual(r.revokeLeft);
      expect(r.nameRight, `${label}: name stays left of the next column`).toBeLessThanOrEqual(
        (r.byLeft ?? r.expLeft) + 0.5
      );
      expect(r.revokeRight, `${label}: revoke icon inside the row`).toBeLessThanOrEqual(r.rowRight + 0.5);
    }

    const long = m.rows.find((r) => r.name === LONG_FILENAME)!;
    expect(long.nameTruncated, "control: the long name does not fit").toBe(true);
    expect(long.nameEllipsis, "the long name truncates with an ellipsis").toBe("ellipsis");
    const short = m.rows.find((r) => r.name !== LONG_FILENAME && r.expText === long.expText)!;
    expect(Math.abs(long.expLeft - short.expLeft), "the long name pushes no column").toBeLessThanOrEqual(0.5);
    expect(Math.abs(long.revokeLeft - short.revokeLeft), "the long name pushes no column").toBeLessThanOrEqual(0.5);

    if (m.headExpLeft !== null) {
      for (const r of m.rows) {
        expect(Math.abs(r.byLeft! - m.headByLeft!), `${r.name}: creator under its header`).toBeLessThanOrEqual(0.5);
        expect(Math.abs(r.expLeft - m.headExpLeft), `${r.name}: expiry under its header`).toBeLessThanOrEqual(0.5);
      }
    }

    expect(m.pageOverflow, "page has no horizontal scroll").toBeLessThanOrEqual(0);
    expect(m.mainOverflow, ".app__main has no horizontal scroll").toBeLessThanOrEqual(0);
    expect(m.shareOverflow, ".share-page has no horizontal scroll").toBeLessThanOrEqual(0);
  });
}
