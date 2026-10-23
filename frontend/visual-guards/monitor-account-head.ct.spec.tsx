// 瞭望塔 account card head: name, 詳情, 估計 figure and 歸零 never overlap.
//
// The owner's phone report: under the halloween copy the account name was
// squeezed into three lines under 「魔力帳號詳情 🔮」, the 估計 figure lost its
// last digit, and 歸零 sat against it. jsdom has no layout, so this is measured
// in a real browser on the real AccountCard, in the production fonts (the CT
// page ships none, and the squeeze depends on text width).
//
// MUTANTS (each verified red):
//   phone rules dropped (one row again)        → phone tests (overlap)
//   head not wrapping                           → phone tests (figure beside the name)
//   name left unbreakable (`overflow-wrap` off) → the e-mail name (runs under 詳情)
//   head wraps on the desktop too               → 1280px test
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { AccountCardHeadStory, type AccountCardHeadWording } from "./stories/AccountCardHeadStory";

const DETAIL_LABEL: Record<AccountCardHeadWording, string> = {
  default: "帳號詳情",
  halloween: "魔力帳號詳情 🔮",
};

const CASES = [
  { displayName: "eva-claude-max", cost: 66481 },
  { displayName: "seth-claude-enterprise", cost: 123456 },
  { displayName: "seth.wang@gofreight.com", cost: 123456 },
];

/** The harness page loads no webfonts, so text is measured in the runner's
 * fallback font. This loads the ones index.html ships, with its `lang`. */
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
        document.fonts.load('600 15px "Noto Sans TC"', "魔力帳號詳情估計歸零"),
        document.fonts.load('600 15px "Schibsted Grotesk"', "seth-claude-enterprise $0"),
      ]);
    },
    [href, lang] as const
  );
  expect(
    await page.evaluate(() => [
      document.fonts.check('600 15px "Noto Sans TC"', "魔"),
      document.fonts.check('600 15px "Schibsted Grotesk"', "s"),
    ]),
    "control: the production fonts loaded (they come from Google Fonts)"
  ).toEqual([true, true]);
}

interface Box {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

/** The head's four parts as drawn: the name's and the figure's TEXT boxes (not
 * their containers, which can be wider than the ink), and the two buttons. The
 * figure is bare text beside 歸零, so it is read through a Range. */
async function measureHead(page: Page) {
  return page.evaluate(() => {
    const box = (r: DOMRect) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom });
    const textBox = (el: Element) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      return box(range.getBoundingClientRect());
    };
    const lineCount = (el: Element) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      return new Set(Array.from(range.getClientRects()).map((r) => Math.round(r.top))).size;
    };
    const card = document.querySelector(".mon-acct")!;
    const name = document.querySelector(".mon-acct__name")!;
    const detail = document.querySelector(".mon-acct__detailbtn")!;
    const reset = document.querySelector(".mon-acct__reset")!;
    const costEl = document.querySelector(".mon-acct__cost")!;
    const figure = document.createRange();
    figure.setStartBefore(costEl.firstChild!);
    figure.setEndBefore(reset);
    const monitor = document.querySelector(".monitor")!;
    const cardStyle = getComputedStyle(card);
    const cardRect = card.getBoundingClientRect();
    return {
      card: {
        left: cardRect.left + parseFloat(cardStyle.paddingLeft),
        right: cardRect.right - parseFloat(cardStyle.paddingRight),
        top: cardRect.top,
        bottom: cardRect.bottom,
      },
      name: textBox(name),
      detail: box(detail.getBoundingClientRect()),
      figure: box(figure.getBoundingClientRect()),
      figureText: figure.toString().trim(),
      reset: box(reset.getBoundingClientRect()),
      detailText: detail.textContent,
      detailLines: lineCount(detail),
      resetLines: lineCount(reset),
      detailOverflow: detail.scrollWidth - detail.clientWidth,
      resetOverflow: reset.scrollWidth - reset.clientWidth,
      cardOverflow: card.scrollWidth - card.clientWidth,
      monitorOverflow: monitor.scrollWidth - monitor.clientWidth,
      pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    };
  });
}

function overlaps(a: Box, b: Box) {
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
}

function inside(inner: Box, outer: Box) {
  return inner.left >= outer.left - 0.5 && inner.right <= outer.right + 0.5;
}

for (const wording of ["default", "halloween"] as const) {
  for (const { displayName, cost } of CASES) {
    test(`${wording} copy at 390px: ${displayName} with $${cost} — the four parts of the head neither overlap nor clip`, async ({
      mount,
      page,
    }, testInfo) => {
      await page.setViewportSize({ width: 390, height: 844 });
      await loadProductionFonts(page);
      const cmp = await mount(<AccountCardHeadStory displayName={displayName} cost={cost} wording={wording} />);
      await expect(cmp.locator(".mon-acct__detailbtn")).toHaveText(DETAIL_LABEL[wording]);

      const m = await measureHead(page);
      expect(m.figureText).toBe(`估計 $${cost.toLocaleString("en-US")}`);

      const parts = { name: m.name, detail: m.detail, figure: m.figure, reset: m.reset };
      const names = Object.keys(parts) as (keyof typeof parts)[];
      for (const [i, a] of names.entries()) {
        expect(inside(parts[a], m.card), `${a} stays inside the card`).toBe(true);
        for (const b of names.slice(i + 1)) {
          expect(overlaps(parts[a], parts[b]), `${a} and ${b} do not overlap`).toBe(false);
        }
      }
      expect(m.figure.top, "估計 and 歸零 start on the line below the name and 詳情").toBeGreaterThanOrEqual(
        Math.max(m.name.bottom, m.detail.bottom)
      );
      expect(m.detailLines, "詳情 on one line").toBe(1);
      expect(m.resetLines, "歸零 on one line").toBe(1);
      expect(m.detailOverflow, "詳情 label not cut").toBeLessThanOrEqual(0);
      expect(m.resetOverflow, "歸零 label not cut").toBeLessThanOrEqual(0);
      expect(m.cardOverflow, "card does not scroll sideways").toBeLessThanOrEqual(0);
      expect(m.monitorOverflow, ".monitor does not scroll sideways").toBeLessThanOrEqual(0);
      expect(m.pageOverflow, "page does not scroll sideways").toBeLessThanOrEqual(0);

      await page.screenshot({ path: testInfo.outputPath(`account-head-390-${wording}-${displayName}.png`) });
    });
  }
}

test("halloween copy at 1280px: the whole head stays on one line", async ({ mount, page }, testInfo) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await loadProductionFonts(page);
  const cmp = await mount(
    <AccountCardHeadStory displayName="seth-claude-enterprise" cost={123456} wording="halloween" />
  );
  await expect(cmp.locator(".mon-acct__detailbtn")).toHaveText(DETAIL_LABEL.halloween);

  const m = await measureHead(page);
  const parts = [m.name, m.detail, m.figure, m.reset];
  for (const a of parts) {
    for (const b of parts) {
      expect(a.top < b.bottom && b.top < a.bottom, "every part shares one line").toBe(true);
    }
  }
  expect(m.name.right).toBeLessThanOrEqual(m.detail.left);
  expect(m.detail.right).toBeLessThanOrEqual(m.figure.left);
  expect(m.figure.right).toBeLessThanOrEqual(m.reset.left);

  await page.screenshot({ path: testInfo.outputPath("account-head-1280-halloween.png") });
});
