// The label/value rows of the five card types (task, reply — on the 請示 page
// and inside a task's step —, secret, file, lore) are one style: on every card
// each label starts at the rows' left edge in the task card's colour, opacity
// and size, the values start at the same x on every card of one language (the
// label column is one width, at least the longest label of any card), and a
// one-line value is centred on its label. The copy id chip on each
// card's first line is drawn the same way too, and so are the first line's
// icon buttons and status icon (the task card's size). Measured against the task card's
// own rows and id chip in the same test, at phone and desktop width in zh and en.
// jsdom lays nothing out.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { TaskCardReplyCardStory } from "./stories/TaskCardReplyCardStory";
import { ReplyDeskStory } from "./stories/ReplyDeskStory";
import { SecretNoteStory } from "./stories/SecretNoteStory";
import { SharedDocsListStory } from "./stories/SharedDocsListStory";
import { LoreRowHeadStory } from "./stories/LoreRowHeadStory";

type Row = {
  label: string;
  labelX: number;
  labelWidth: number;
  valueX: number;
  labelMid: number;
  valueMid: number;
  valueHeight: number;
  color: string;
  opacity: string;
  fontSize: string;
};
type Meta = { card: string; rows: Row[] };

async function measure(page: Page, cardSelector: string): Promise<Meta[]> {
  return page.evaluate((cardSelector: string) => {
    return [...document.querySelectorAll<HTMLElement>(".card-meta")]
      .filter((m) => m.getClientRects().length > 0)
      .map((meta) => {
        const mb = meta.getBoundingClientRect();
        const card = meta.closest(cardSelector);
        const rows = [...meta.querySelectorAll<HTMLElement>(".card-meta__label")]
          .filter((l) => l.closest(".card-meta") === meta && l.getClientRects().length > 0)
          .map((label) => {
            const value = label.nextElementSibling!;
            const lb = label.getBoundingClientRect();
            const vb = value.getBoundingClientRect();
            const cs = getComputedStyle(label);
            return {
              label: label.textContent ?? "",
              labelX: lb.left - mb.left,
              labelWidth: lb.width,
              valueX: vb.left - mb.left,
              labelMid: (lb.top + lb.bottom) / 2,
              valueMid: (vb.top + vb.bottom) / 2,
              valueHeight: vb.height,
              color: cs.color,
              opacity: cs.opacity,
              fontSize: cs.fontSize,
            };
          });
        return { card: card?.getAttribute("data-testid") ?? card?.className.split(" ")[0] ?? "?", rows };
      })
      .filter((m) => m.rows.length > 0);
  }, cardSelector);
}

async function idChipLook(page: Page): Promise<string> {
  return page.evaluate(() => {
    const chip = document.querySelector(".id-chip");
    if (!chip) return "no id chip";
    const cs = getComputedStyle(chip);
    return [cs.fontFamily, cs.fontSize, cs.fontWeight, cs.color, cs.borderTopColor, cs.padding, cs.borderRadius].join(" | ");
  });
}

/** Size and look of every visible first-line icon button / status icon under
 * `scope`, one string each. */
async function headIconLooks(page: Page, scope: string, cls: string): Promise<string[]> {
  return page.evaluate(
    ([scope, cls]) =>
      [...document.querySelectorAll<HTMLElement>(`${scope} .${cls}`)]
        .filter((b) => b.getClientRects().length > 0)
        .map((b) => {
          const r = b.getBoundingClientRect();
          const svg = b.querySelector("svg")!.getBoundingClientRect();
          const cs = getComputedStyle(b);
          return [r.width, r.height, svg.width, cs.borderRadius, cs.padding].join(" | ");
        }),
    [scope, cls] as const,
  );
}

function expectSameRows(metas: Meta[], ref: Row, gap: number, tag: string) {
  expect(metas.length, `${tag}: some rows measured`).toBeGreaterThan(0);
  for (const meta of metas) {
    const column = Math.max(...meta.rows.map((r) => r.labelWidth));
    expect.soft(ref.valueX - gap, `${tag} ${meta.card}: the label column holds its widest label`).toBeGreaterThanOrEqual(column - 0.5);
    for (const r of meta.rows) {
      const what = `${tag} ${meta.card} 「${r.label}」`;
      expect.soft(r.color, `${what}: label colour`).toBe(ref.color);
      expect.soft(r.opacity, `${what}: label opacity`).toBe(ref.opacity);
      expect.soft(r.fontSize, `${what}: label size`).toBe(ref.fontSize);
      expect.soft(Math.abs(r.labelX), `${what}: label at the rows' left edge`).toBeLessThanOrEqual(0.5);
      expect.soft(Math.abs(r.valueX - ref.valueX), `${what}: value at the task card's value x`).toBeLessThanOrEqual(0.5);
      if (r.valueHeight <= 32) {
        expect.soft(Math.abs(r.labelMid - r.valueMid), `${what}: label centred on its value`).toBeLessThanOrEqual(0.5);
      }
    }
  }
}

for (const width of [320, 390, 1440]) {
  for (const language of ["zh", "en"] as const) {
    test(`width ${width} ${language}: every card's label/value rows and id chip are the task card's`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 1400 });
      await page.evaluate((l) => localStorage.setItem("oc.language", l), language);
      const tag = `[${width}px ${language}]`;

      let cmp = await mount(<TaskCardReplyCardStory language={language} />);
      await cmp.locator(".task-card__title").first().click();
      await expect(cmp.locator('[data-testid="task-reply-card"] .card-meta')).toBeVisible();
      const all = await measure(page, '[data-testid="task-reply-card"], .task-card');
      const task = all.filter((m) => m.card !== "task-reply-card");
      const embedded = all.filter((m) => m.card === "task-reply-card");
      expect(task.length, `${tag} the task card's rows`).toBe(1);
      const ref = task[0].rows[0];
      const gap = await page.evaluate(() => parseFloat(getComputedStyle(document.querySelector(".task-card .card-meta")!).columnGap));
      expect(gap, `${tag} the task card's gap`).toBe(12);
      expectSameRows(task, ref, gap, `${tag} task card`);
      const idChip = await idChipLook(page);
      expect(idChip, `${tag} the task card's id chip`).not.toBe("no id chip");
      const [taskButton] = await headIconLooks(page, ".task-card", "card-head__icon-btn");
      const [taskStatus] = await headIconLooks(page, ".task-card", "card-head__status-icon");
      expect(taskButton, `${tag} the task card's 轉派 button`).toBeDefined();
      expect(taskStatus, `${tag} the task card's status icon`).toBeDefined();
      for (const look of await headIconLooks(page, '[data-testid="task-reply-card"]', "card-head__icon-btn")) {
        expect.soft(look, `${tag} reply card in a step: head button`).toBe(taskButton);
      }
      expectSameRows(embedded, ref, gap, `${tag} reply card in a step`);
      expect(embedded[0].rows.map((r) => r.label), `${tag} the embedded card's rows`).toEqual([
        language === "zh" ? "開卡人" : "Asked by",
      ]);
      await cmp.unmount();

      cmp = await mount(<ReplyDeskStory longAskers />);
      await expect(cmp.locator(".reply-card .card-meta").first()).toBeVisible();
      expectSameRows(await measure(page, ".reply-card"), ref, gap, `${tag} 請示 page`);
      expect.soft(await idChipLook(page), `${tag} 請示 page id chip`).toBe(idChip);
      const deskButtons = await headIconLooks(page, ".reply-card", "card-head__icon-btn");
      expect(deskButtons.length, `${tag} 請示 page head buttons measured`).toBeGreaterThan(0);
      expect.soft(new Set(deskButtons), `${tag} 請示 page head buttons`).toEqual(new Set([taskButton]));
      await cmp.unmount();

      for (const [name, story, card] of [
        ["secret", <SecretNoteStory key="s" />, '[data-testid="secret-row"]'],
        ["file", <SharedDocsListStory key="f" />, ".lore-row"],
        ["lore", <LoreRowHeadStory key="l" hash="#lore/agent/mira" />, ".lore-row"],
      ] as const) {
        cmp = await mount(story);
        await expect(cmp.locator(".card-meta").first()).toBeVisible();
        expectSameRows(await measure(page, card), ref, gap, `${tag} ${name}`);
        expect.soft(await idChipLook(page), `${tag} ${name} id chip`).toBe(idChip);
        const buttons = await headIconLooks(page, card, "card-head__icon-btn");
        expect(buttons.length, `${tag} ${name} head buttons measured`).toBeGreaterThan(0);
        expect.soft(new Set(buttons), `${tag} ${name} head buttons`).toEqual(new Set([taskButton]));
        if (name === "lore") {
          const statuses = await headIconLooks(page, card, "card-head__status-icon");
          expect(statuses.length, `${tag} lore status icons measured`).toBeGreaterThan(0);
          expect.soft(new Set(statuses), `${tag} lore status icon`).toEqual(new Set([taskStatus]));
        }
        await cmp.unmount();
      }
    });
  }
}
