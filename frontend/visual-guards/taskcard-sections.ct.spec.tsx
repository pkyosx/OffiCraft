// The expanded task card's 任務描述 and 工作流程 frames, measured by the
// browser's own layout at phone and desktop width: a long description is cut
// to about six lines with a fade and 展開全文, a short one shows whole with no
// toggle, the 任務描述 bar is its icon, title and chevron with no summary, and
// each title bar folds its body away without folding the card. On a phone a
// step whose badges cannot share a line with its title moves them to the next
// line rather than squeezing the title to one character per line.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test, expect } from "@playwright/experimental-ct-react";
import type { Page } from "@playwright/test";
import { TaskCardSectionsStory, type StoryDescription } from "./stories/TaskCardSectionsStory";

const card = (cmp: any) => cmp.locator(".task-card");

async function mountExpanded(mount: any, page: any, width: number, description: StoryDescription) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardSectionsStory description={description} />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
  await expect(cmp.getByTestId("task-step")).toHaveCount(4);
  return cmp;
}

/** The 任務描述 bar's parts, left to right, and whether it holds one line. */
async function descHeadParts(cmp: any) {
  return cmp.getByTestId("task-desc-section-head").evaluate((el: HTMLElement) => ({
    parts: Array.from(el.children, (c) => c.className),
    text: el.textContent,
    over: el.scrollWidth - el.clientWidth,
  }));
}

async function pageOverflow(page: any) {
  return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
}

for (const width of [390, 1440]) {
  test(`a long description shows about six lines behind 展開全文 and opens to full height @${width}px`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width, "long");
    const body = cmp.getByTestId("task-desc-body");
    const toggle = cmp.getByTestId("task-desc-toggle");
    await expect(toggle).toHaveText("展開全文");
    const clamped = await body.evaluate((el: HTMLElement) => ({
      lines: el.clientHeight / (parseFloat(getComputedStyle(el).fontSize) * 1.55),
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
    }));
    expect.soft(Math.round(clamped.lines), `[${width}px] clamped description lines`).toBe(6);
    expect.soft(clamped.hidden, `[${width}px] text hidden below the clamp`).toBeGreaterThan(1);
    expect.soft(clamped.mask, `[${width}px] clamped description fades`).not.toBe("none");
    const closedHeight = (await body.boundingBox())!.height;

    await toggle.click();
    await expect(toggle).toHaveText("收起");
    await expect(card(cmp)).toHaveAttribute("aria-expanded", "true");
    const open = await body.evaluate((el: HTMLElement) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
      height: el.clientHeight,
    }));
    expect.soft(open.hidden, `[${width}px] opened description still clipped`).toBeLessThanOrEqual(1);
    expect.soft(open.mask, `[${width}px] opened description faded`).toBe("none");
    expect.soft(open.height, `[${width}px] opened description taller`).toBeGreaterThan(closedHeight + 1);
    expect.soft(await pageOverflow(page), `[${width}px] page horizontal scroll`).toBeLessThanOrEqual(0);
  });

  for (const description of ["long", "paragraph", "joined", "short"] as const) {
    test(`the 任務描述 bar shows only its icon, title and chevron for the ${description} description @${width}px`, async ({
      mount,
      page,
    }) => {
      const cmp = await mountExpanded(mount, page, width, description);
      await expect(cmp.getByTestId("task-desc-section-summary")).toHaveCount(0);
      const head = await descHeadParts(cmp);
      expect.soft(head.parts, `[${width}px] 任務描述 bar parts`).toEqual([
        "task-section__icon",
        "task-section__title",
        "task-section__chevron",
      ]);
      expect.soft(head.text, `[${width}px] 任務描述 bar text`).toBe("任務描述");
      expect.soft(head.over, `[${width}px] 任務描述 bar overflows`).toBeLessThanOrEqual(0);
      if (description === "paragraph" && width === 390) {
        await expect(cmp.getByTestId("task-desc-toggle")).toHaveText("展開全文");
      }
    });
  }

  test(`a short description shows whole with no fade and no toggle @${width}px`, async ({ mount, page }) => {
    const cmp = await mountExpanded(mount, page, width, "short");
    await expect(cmp.getByTestId("task-desc-toggle")).toHaveCount(0);
    const body = await cmp.getByTestId("task-desc-body").evaluate((el: HTMLElement) => ({
      hidden: el.scrollHeight - el.clientHeight,
      mask: getComputedStyle(el).maskImage,
    }));
    expect.soft(body.hidden, `[${width}px] short description clipped`).toBeLessThanOrEqual(1);
    expect.soft(body.mask, `[${width}px] short description faded`).toBe("none");
  });

  test(`each title bar folds its own body and the 工作流程 bar then summarises progress @${width}px`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width, "long");
    const wfHead = cmp.getByTestId("task-workflow-section-head");
    await expect(cmp.getByTestId("task-workflow-section-summary")).toHaveText("4 個步驟 · 1/4 完成");

    await wfHead.click();
    await expect(wfHead).toHaveAttribute("aria-expanded", "false");
    await expect(cmp.getByTestId("task-step")).toHaveCount(0);
    await expect(cmp.getByTestId("task-workflow-section-summary")).toHaveText("1/4 完成 · 進行中：草擬週報");
    await expect(card(cmp)).toHaveAttribute("aria-expanded", "true");
    const bar = await wfHead.evaluate((el: HTMLElement) => ({ over: el.scrollWidth - el.clientWidth, h: el.clientHeight }));
    expect.soft(bar.over, `[${width}px] collapsed workflow bar overflows`).toBeLessThanOrEqual(0);

    const descHead = cmp.getByTestId("task-desc-section-head");
    await descHead.click();
    await expect(cmp.locator(".task-card__desc")).toHaveCount(0);
    await expect(cmp.getByTestId("task-desc-section-summary")).toHaveCount(0);
    expect.soft((await descHeadParts(cmp)).text, `[${width}px] folded 任務描述 bar text`).toBe("任務描述");
    await descHead.click();
    await expect(cmp.locator(".task-card__desc")).toBeVisible();
    await wfHead.click();
    await expect(cmp.getByTestId("task-step")).toHaveCount(4);
    expect.soft(await pageOverflow(page), `[${width}px] page horizontal scroll`).toBeLessThanOrEqual(0);
  });
}

/** The harness page loads no webfonts; step titles wrap by text width, so
 * measure in the ones index.html ships. */
async function loadProductionFonts(page: Page) {
  const html = fs.readFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "../index.html"), "utf8");
  const href = html.match(/href="(https:\/\/fonts\.googleapis\.com\/css2\?[^"]+)"/)![1].replace(/&amp;/g, "&");
  await page.evaluate(async (href) => {
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
      document.fonts.load('700 13px "Noto Sans TC"', "示意圖任務卡狀態"),
      document.fonts.load('12px "Schibsted Grotesk"', "4h 59m"),
    ]);
  }, href);
  expect(
    await page.evaluate(() => document.fonts.check('700 13px "Noto Sans TC"', "示")),
    "control: the production fonts loaded (they come from Google Fonts)"
  ).toBe(true);
}

type Box = { left: number; right: number; top: number; bottom: number; width: number; height: number };

type StepGeometry = {
  id: string;
  inParallel: boolean;
  group: Box | null;
  above: Box | null;
  step: Box;
  name: Box;
  nameLineHeight: number;
  firstLine: Box;
  firstLineHeight: number;
  pill: Box;
  shield: Box | null;
  marker: Box;
  replyCard: Box | null;
  rowOverflow: number;
};

/** Every step's box, title, time pill, shield and timeline icon as drawn. */
async function stepGeometry(cmp: any): Promise<{ card: Box; steps: StepGeometry[] }> {
  return cmp.locator(".task-timeline").evaluate((tl: HTMLElement) => {
    const box = (el: Element | null) => (el ? (el.getBoundingClientRect().toJSON() as Box) : null);
    const card = tl.closest(".task-card")!;
    return {
      card: box(card)!,
      steps: [...tl.querySelectorAll<HTMLElement>('[data-testid="task-step"]')].map((step, i, all) => {
        const name = step.querySelector<HTMLElement>(".task-step__name")!;
        const row = step.closest(".task-parallel__item, .task-timeline__row")!;
        const parallel = step.closest(".task-parallel");
        const firstLine = name;
        const lineHeight = parseFloat(getComputedStyle(firstLine).lineHeight);
        return {
          id: step.dataset.stepId!,
          inParallel: parallel !== null,
          group: box(parallel),
          above: box(i > 0 ? all[i - 1] : null),
          step: box(step)!,
          name: box(name)!,
          nameLineHeight: parseFloat(getComputedStyle(name).lineHeight),
          firstLine: box(firstLine)!,
          firstLineHeight: lineHeight,
          pill: box(step.querySelector(".task-step__time"))!,
          shield: box(step.querySelector('[data-testid="step-gate"]')),
          marker: box(row.querySelector('[data-testid="step-marker"]'))!,
          replyCard: box(step.querySelector('[data-testid="task-reply-card"]')),
          rowOverflow: step.querySelector(".task-step__row")!.scrollWidth - step.querySelector(".task-step__row")!.clientWidth,
        };
      }),
    };
  });
}

/** The parallel stage's header row and the bracket its items draw beside the spine. */
async function stageGeometry(cmp: any): Promise<{ headerMarkers: number; label: Box; bracket: Box; bracketGap: number }> {
  return cmp.locator(".task-parallel").evaluate((stage: HTMLElement) => {
    const row = stage.closest(".task-timeline__row")!;
    const label = stage.querySelector(".task-parallel__label")!.getBoundingClientRect();
    const items = [...stage.querySelectorAll<HTMLElement>(".task-parallel__item")];
    const piece = (item: HTMLElement) => {
      const cs = getComputedStyle(item, "::before");
      const r = item.getBoundingClientRect();
      const top = r.top + parseFloat(cs.top);
      const left = r.left + parseFloat(cs.left);
      return { top, bottom: top + parseFloat(cs.height), left, right: left + parseFloat(cs.width), visible: cs.content !== "none" && parseFloat(cs.borderRightWidth) > 0 };
    };
    const pieces = items.map(piece).filter((p) => p.visible);
    const labelMid = (label.top + label.bottom) / 2;
    const headerMarkers = [...row.querySelectorAll('[data-testid="step-marker"]')].filter((m) => {
      const r = m.getBoundingClientRect();
      return r.top <= labelMid && r.bottom >= labelMid;
    }).length;
    const bracketGap = Math.max(0, ...pieces.slice(1).map((p, i) => p.top - pieces[i].bottom));
    return {
      headerMarkers,
      bracketGap,
      label: label.toJSON(),
      bracket: pieces.length === 0
        ? { top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0 }
        : {
            top: Math.min(...pieces.map((p) => p.top)),
            bottom: Math.max(...pieces.map((p) => p.bottom)),
            left: Math.min(...pieces.map((p) => p.left)),
            right: Math.max(...pieces.map((p) => p.right)),
            width: 0, height: 0,
          },
    };
  });
}

const overlaps = (a: Box, b: Box) =>
  a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5;

for (const width of [320, 375, 390, 1440]) {
  for (const language of ["zh", "en"] as const) {
    test(`each step keeps its time pill on the title's first line, its shield on the corner and its own icon on the timeline beside its title line (a parallel stage's steps bracketed together, its header with no icon), and the task's time pill sits on the 步驟 N/M line @${width}px ${language}`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 1400 });
      await loadProductionFonts(page);
      const cmp = await mount(<TaskCardSectionsStory description="short" steps="all" language={language} />);
      await cmp.locator(".task-card__head").first().click();
      await expect(cmp.getByTestId("task-step")).toHaveCount(9);
      await expect(cmp.getByTestId("task-reply-card")).toBeVisible();
      const g = await stepGeometry(cmp);
      const at = `[${width}px ${language}]`;

      const fromRight = g.steps.map((s) => s.step.right - s.pill.right);
      const topLevelPillRight = g.steps.filter((s) => !s.inParallel).map((s) => s.pill.right);
      expect.soft(Math.max(...fromRight) - Math.min(...fromRight), `${at} pill inset from the step's right edge differs`).toBeLessThanOrEqual(0.5);
      expect.soft(Math.max(...topLevelPillRight) - Math.min(...topLevelPillRight), `${at} top-level pills' right edges differ`).toBeLessThanOrEqual(0.5);

      for (const s of g.steps) {
        const id = `${at} ${s.id}`;
        const pillMid = (s.pill.top + s.pill.bottom) / 2;
        expect.soft(pillMid, `${id} pill below the title's first line`).toBeLessThan(s.name.top + s.nameLineHeight);
        expect.soft(pillMid, `${id} pill above the title`).toBeGreaterThan(s.name.top);
        expect.soft(s.pill.left, `${id} pill outside the step (left)`).toBeGreaterThanOrEqual(s.step.left);
        expect.soft(s.pill.right, `${id} pill outside the step (right)`).toBeLessThanOrEqual(s.step.right);
        expect.soft(s.name.right, `${id} title runs under the pill`).toBeLessThanOrEqual(s.pill.left + 0.5);
        expect.soft(s.name.left, `${id} title outside the step`).toBeGreaterThanOrEqual(s.step.left);
        expect.soft(s.pill.left - s.name.right, `${id} title column stops short of the pill`).toBeLessThanOrEqual(8.5);
        expect.soft(s.rowOverflow, `${id} step header overflows`).toBeLessThanOrEqual(0);

        const markerMid = (s.marker.top + s.marker.bottom) / 2;
        const lineMid = s.firstLine.top + s.firstLineHeight / 2;
        expect.soft(Math.abs(markerMid - lineMid), `${id} timeline icon off the first text line`).toBeLessThanOrEqual(1.5);
        expect.soft(s.marker.right, `${id} timeline icon runs into the box`).toBeLessThanOrEqual((s.group ?? s.step).left);

        if (s.replyCard) {
          expect.soft(s.replyCard.right, `${id} reply card past the step`).toBeLessThanOrEqual(s.step.right);
        }
      }

      const markerLefts = g.steps.map((s) => s.marker.left);
      expect.soft(Math.max(...markerLefts) - Math.min(...markerLefts), `${at} timeline icons not on one line`).toBeLessThanOrEqual(0.5);
      const grouped = g.steps.filter((s) => s.inParallel);
      expect(grouped.map((s) => s.id), `${at} steps in the parallel stage`).toEqual(["s-par-1", "s-par-2"]);
      const stage = await stageGeometry(cmp);
      expect.soft(stage.headerMarkers, `${at} icons on the stage header row`).toBe(0);
      const firstIcon = grouped[0].marker;
      const lastIcon = grouped[grouped.length - 1].marker;
      expect.soft(stage.bracket.top, `${at} bracket starts below the first grouped icon's top`).toBeLessThanOrEqual(firstIcon.top);
      expect.soft(stage.bracket.bottom, `${at} bracket ends above the last grouped icon's bottom`).toBeGreaterThanOrEqual(lastIcon.bottom);
      expect.soft(stage.bracketGap, `${at} bracket broken between grouped icons`).toBeLessThanOrEqual(0.5);
      expect.soft(stage.bracket.top, `${at} bracket climbs onto the stage header`).toBeGreaterThan(stage.label.bottom);
      expect.soft(stage.bracket.left, `${at} bracket over the icons`).toBeGreaterThanOrEqual(firstIcon.right);
      expect.soft(stage.bracket.right, `${at} bracket inside the stage frame`).toBeLessThanOrEqual(grouped[0].group!.left);

      const gated = g.steps.filter((s) => s.shield);
      expect(gated.map((s) => s.id), `${at} steps with a shield`).toEqual(["s-par-2", "s-gate-pending", "s-gate-waiting", "s-gate-done"]);
      for (const s of gated) {
        const shield = s.shield!;
        const id = `${at} ${s.id}`;
        expect.soft(overlaps(shield, s.pill), `${id} shield over the pill`).toBe(false);
        expect.soft(shield.right, `${id} shield past the card`).toBeLessThanOrEqual(g.card.right);
        expect.soft(shield.left < s.step.right && shield.right > s.step.right, `${id} shield not on the box's right edge`).toBe(true);
        expect.soft(shield.top < s.step.top && shield.bottom > s.step.top, `${id} shield not on the box's top edge`).toBe(true);
        if (s.above) {
          expect.soft(overlaps(shield, s.above), `${id} shield over the step above`).toBe(false);
        }
        if (s.group) {
          expect.soft(shield.right, `${id} shield past the parallel stage (right)`).toBeLessThanOrEqual(s.group.right);
          expect.soft(shield.top, `${id} shield past the parallel stage (top)`).toBeGreaterThanOrEqual(s.group.top);
        }
      }

      const progress = await cmp.locator(".task-card__progress").evaluate((el: HTMLElement) => {
        const box = (sel: string) => el.querySelector(sel)!.getBoundingClientRect().toJSON() as Box;
        return { text: box('[data-testid="task-progress"]'), pill: box('[data-testid="task-elapsed"]'), card: el.closest(".task-card")!.getBoundingClientRect().toJSON() as Box };
      });
      const textMid = (progress.text.top + progress.text.bottom) / 2;
      const pillMid = (progress.pill.top + progress.pill.bottom) / 2;
      expect.soft(Math.abs(textMid - pillMid), `${at} task time pill off the 步驟 N/M line`).toBeLessThanOrEqual(1.5);
      expect.soft(progress.pill.left, `${at} task time pill before the 步驟 N/M text`).toBeGreaterThanOrEqual(progress.text.right);
      expect.soft(progress.pill.right, `${at} task time pill past the card`).toBeLessThanOrEqual(progress.card.right);
      expect.soft(await pageOverflow(page), `${at} page horizontal scroll`).toBeLessThanOrEqual(0);
    });
  }
}
