// GUARD — the mark under a matched @name sits on that name in all three
// composers. The textarea's glyphs cannot be measured directly, so the oracles
// are independent of the mark layer: a canvas measureText in the textarea's own
// computed font for x, and the textarea's own box, padding and line height for
// y. Production fonts are loaded because the harness page has none.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { ChatMentionStory } from "./stories/ChatMentionStory";
import { TaskCardMentionStory, ReplyCardMentionStory } from "./stories/ComposerMentionStory";

const here = path.dirname(fileURLToPath(import.meta.url));

async function loadProductionFonts(page: Page) {
  const html = fs.readFileSync(path.join(here, "../index.html"), "utf8");
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
        document.fonts.load('14px "Noto Sans TC"', "請看一下這幾件事"),
        document.fonts.load('14px "Schibsted Grotesk"', "Family Money Eva"),
      ]);
    },
    [href, lang] as const,
  );
  expect(
    await page.evaluate(() => [
      document.fonts.check('14px "Noto Sans TC"', "請"),
      document.fonts.check('14px "Schibsted Grotesk"', "F"),
    ]),
    "control: the production fonts loaded (they come from Google Fonts)",
  ).toEqual([true, true]);
}

async function applyHalloweenColors(page: Page) {
  const theme = JSON.parse(fs.readFileSync(path.join(here, "../../themes/halloween-2026.theme.json"), "utf8"));
  await page.evaluate((colors) => {
    for (const [k, v] of Object.entries(colors)) document.documentElement.style.setProperty(k, v);
  }, theme.colors as Record<string, string>);
}

/** Where the last mark fragment sits against where the textarea draws `before`
 * and the mark's own text, read from a painted frame: scroll and resize
 * syncing land at the next frame, not at React's commit. */
async function measure(page: Page, inputSel: string, before: string) {
  await page.evaluate(
    () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
  );
  return page.evaluate(
    ([sel, before]) => {
      const input = document.querySelector(sel) as HTMLTextAreaElement;
      const layer = input.parentElement!.querySelector(".mention-highlight") as HTMLElement;
      const marks = layer.querySelectorAll(".mention-highlight__mark");
      const mark = marks[marks.length - 1] as HTMLElement;
      const rects = mark.getClientRects();
      const last = rects[rects.length - 1];
      const cs = getComputedStyle(input);
      const ctx = document.createElement("canvas").getContext("2d")!;
      ctx.font = cs.font;
      const ib = input.getBoundingClientRect();
      const lb = layer.getBoundingClientRect();
      const px = (v: string) => parseFloat(v) || 0;
      const markCs = getComputedStyle(mark);
      return {
        input: { left: ib.left, top: ib.top, width: ib.width, height: ib.height },
        layer: { left: lb.left, top: lb.top, width: lb.width, height: lb.height },
        contentLeft: ib.left + px(cs.borderLeftWidth) + px(cs.paddingLeft),
        contentBottom: ib.bottom - px(cs.borderBottomWidth) - px(cs.paddingBottom),
        contentTop: ib.top + px(cs.borderTopWidth) + px(cs.paddingTop),
        lineHeight: px(cs.lineHeight),
        beforeWidth: ctx.measureText(before).width,
        markText: mark.textContent,
        markLeft: last.left,
        markWidth: last.width,
        markMidY: last.top + last.height / 2,
        markTextWidth: ctx.measureText(mark.textContent ?? "").width,
        fragments: rects.length,
        inputScrollTop: input.scrollTop,
        layerScrollTop: layer.scrollTop,
        inputScrollHeight: input.scrollHeight,
        layerScrollHeight: layer.scrollHeight,
        markBg: markCs.backgroundColor,
        inputBg: cs.backgroundColor,
      };
    },
    [inputSel, before] as const,
  );
}

const composers = [
  {
    name: "chat composer",
    story: (width: number) => <ChatMentionStory withRoster={width > 720} />,
    input: ".chat__composer-row textarea.chat__input",
  },
  {
    name: "task card message box",
    story: () => <TaskCardMentionStory />,
    input: '[data-testid="task-msg-input"]',
  },
  {
    name: "reply card answer box",
    story: () => <ReplyCardMentionStory />,
    input: ".reply-composer textarea.chat__input",
  },
];

for (const c of composers) {
  for (const width of [390, 1280]) {
    for (const theme of ["default", "halloween"] as const) {
      test(`${c.name}, width ${width}, ${theme} theme: the mark covers the @name on its line and is tinted`, async ({
        mount,
        page,
      }) => {
        await page.setViewportSize({ width, height: 844 });
        await loadProductionFonts(page);
        if (theme === "halloween") await applyHalloweenColors(page);
        const cmp = await mount(c.story(width));
        const input = cmp.locator(c.input);
        await input.fill("請看\n@Family Money Eva");
        await expect(cmp.locator(".mention-highlight__mark")).toHaveText("@Family Money Eva");

        const g = await measure(page, c.input, "");
        for (const k of ["left", "top", "width", "height"] as const) {
          expect(Math.abs(g.layer[k] - g.input[k]), `layer ${k} matches the textarea`).toBeLessThanOrEqual(0.5);
        }
        expect(g.fragments, "the name fits on one line here").toBe(1);
        expect(Math.abs(g.markLeft - (g.contentLeft + g.beforeWidth)), "mark starts where the @ is drawn").toBeLessThanOrEqual(1);
        expect(Math.abs(g.markWidth - g.markTextWidth), "mark is as wide as the drawn name").toBeLessThanOrEqual(1);
        expect(Math.abs(g.markMidY - (g.contentTop + g.lineHeight * 1.5)), "mark sits on the second line").toBeLessThanOrEqual(3);
        expect(g.markBg, "mark has a tint").not.toBe("rgba(0, 0, 0, 0)");
        expect(g.markBg, "mark tint differs from the box").not.toBe(g.inputBg);
      });
    }

    test(`${c.name}, width ${width}: past the height cap the mark scrolls with the text and stays on the last line`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      await loadProductionFonts(page);
      const cmp = await mount(c.story(width));
      const input = cmp.locator(c.input);
      const lines = ["一", "二", "三", "四", "五", "六", "七", "八"].map((n) => `第${n}行`);
      await input.fill(`${lines.join("\n")}\n@Family Money Eva`);
      await input.evaluate((el) => (el.scrollTop = el.scrollHeight));
      await expect(cmp.locator(".mention-highlight__mark")).toHaveText("@Family Money Eva");

      const g = await measure(page, c.input, "");
      expect(g.inputScrollTop, "control: the textarea really scrolled").toBeGreaterThan(0);
      expect(g.layerScrollTop, "layer scrolled with it").toBe(g.inputScrollTop);
      expect(Math.abs(g.layerScrollHeight - g.inputScrollHeight), "same wrapped height").toBeLessThanOrEqual(1);
      expect(Math.abs(g.markMidY - (g.contentBottom - g.lineHeight / 2)), "mark sits on the last visible line").toBeLessThanOrEqual(3);
      expect(Math.abs(g.markLeft - (g.contentLeft + g.beforeWidth)), "mark starts where the @ is drawn").toBeLessThanOrEqual(1);
    });
  }
}

test("chat composer, width 390: a name pushed onto a wrapped line is marked on that line", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await loadProductionFonts(page);
  const cmp = await mount(<ChatMentionStory withRoster={false} />);
  const input = cmp.locator(".chat__composer-row textarea.chat__input");
  await input.fill("please take a careful look at these items @Family Money Eva");
  await expect(cmp.locator(".mention-highlight__mark")).toHaveText("@Family Money Eva");

  const g = await measure(page, ".chat__composer-row textarea.chat__input", "");
  const drawnLines = Math.round((g.input.height - (g.contentTop - g.input.top) * 2) / g.lineHeight);
  expect(drawnLines, "control: the text wraps in the textarea").toBeGreaterThan(1);
  expect(Math.abs(g.layerScrollHeight - g.inputScrollHeight), "same wrapped height").toBeLessThanOrEqual(1);
  expect(Math.abs(g.markMidY - (g.contentBottom - g.lineHeight / 2)), "the name's last part is on the textarea's last line").toBeLessThanOrEqual(3);
});
