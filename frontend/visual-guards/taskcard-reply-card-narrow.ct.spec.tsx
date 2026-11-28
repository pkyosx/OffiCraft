// A waiting reply card inside a task card's workflow step, at phone and desktop
// width in zh and en: each option's text keeps at least half its option's
// width (the AI tag goes under it when there is no room beside it), the answer
// box keeps at least half the composer's width, and nothing in the card leaves
// it or scrolls the page sideways. jsdom lays nothing out.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardReplyCardStory } from "./stories/TaskCardReplyCardStory";

type Box = { left: number; right: number; width: number };

for (const width of [320, 390, 1440]) {
  for (const language of ["zh", "en"] as const) {
    test(`width ${width} ${language}: a step's waiting reply card keeps its options and answer box readable and inside it`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 1400 });
      const cmp = await mount(<TaskCardReplyCardStory language={language} />);
      await cmp.locator(".task-card__title").first().click();
      const card = cmp.locator('[data-testid="task-reply-card"]');
      await expect(card.locator(".reply-option").first()).toBeVisible();
      const g = await card.evaluate((el) => {
        const b = (e: Element): Box => {
          const r = e.getBoundingClientRect();
          return { left: r.left, right: r.right, width: r.width };
        };
        const se = document.scrollingElement!;
        return {
          card: b(el),
          options: [...el.querySelectorAll(".reply-option")].map((o) => ({
            option: b(o),
            text: b(o.querySelector(".reply-option__text")!),
            parts: [...o.children].map(b),
          })),
          composer: b(el.querySelector(".chat__composer-row")!),
          composerParts: [...el.querySelectorAll(".chat__composer-row > :not(input[type=file])")].map(b),
          input: b(el.querySelector(".chat__input")!),
          pageOver: se.scrollWidth - se.clientWidth,
        };
      });
      const tag = `[${width}px ${language}]`;
      expect(g.options.length, `${tag} options`).toBe(2);
      for (const [i, o] of g.options.entries()) {
        expect.soft(o.text.width, `${tag} option ${i + 1} text keeps half the option`).toBeGreaterThanOrEqual(o.option.width / 2);
        for (const p of o.parts) {
          expect.soft(p.left, `${tag} option ${i + 1} part inside`).toBeGreaterThanOrEqual(o.option.left - 1);
          expect.soft(p.right, `${tag} option ${i + 1} part inside`).toBeLessThanOrEqual(o.option.right + 1);
        }
        expect.soft(o.option.right, `${tag} option ${i + 1} inside the card`).toBeLessThanOrEqual(g.card.right + 1);
      }
      expect.soft(g.input.width, `${tag} answer box keeps half the composer`).toBeGreaterThanOrEqual(g.composer.width / 2);
      for (const p of g.composerParts) {
        expect.soft(p.left, `${tag} composer part inside`).toBeGreaterThanOrEqual(g.card.left - 1);
        expect.soft(p.right, `${tag} composer part inside`).toBeLessThanOrEqual(g.card.right + 1);
      }
      expect.soft(g.pageOver, `${tag} page scrolls sideways`).toBeLessThanOrEqual(1);
    });
  }
}
