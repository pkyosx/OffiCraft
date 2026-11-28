// The expanded task card's 任務描述 and 工作流程 frames, measured by the
// browser's own layout at phone and desktop width: a long description is cut
// to about six lines with a fade and 展開全文, a short one shows whole with no
// toggle, and each title bar folds its body away without folding the card.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardSectionsStory, type StoryDescription } from "./stories/TaskCardSectionsStory";

async function mountExpanded(mount: any, page: any, width: number, description: StoryDescription) {
  await page.setViewportSize({ width, height: 900 });
  const cmp = await mount(<TaskCardSectionsStory description={description} />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__desc")).toBeVisible();
  await expect(cmp.getByTestId("task-step")).toHaveCount(4);
  return cmp;
}

async function renderedLines(cmp: any) {
  return cmp
    .getByTestId("task-desc-body")
    .evaluate((el: HTMLElement) => el.scrollHeight / (parseFloat(getComputedStyle(el).fontSize) * 1.55));
}

async function summaryLines(cmp: any) {
  const text = await cmp.getByTestId("task-desc-section-summary").textContent();
  return Number(/約 (\d+) 行/.exec(text ?? "")?.[1]);
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
    await expect(cmp.getByTestId("task-desc-section-summary")).toHaveText(/^約 \d+ 行$/);
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
    await expect(cmp).toHaveAttribute("aria-expanded", "true");
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

  for (const description of ["long", "paragraph", "joined"] as const) {
    test(`the 任務描述 bar counts the ${description} description's displayed lines @${width}px`, async ({
      mount,
      page,
    }) => {
      const cmp = await mountExpanded(mount, page, width, description);
      await expect(cmp.getByTestId("task-desc-section-summary")).toHaveText(/^約 \d+ 行$/);
      const measured = await renderedLines(cmp);
      const shown = await summaryLines(cmp);
      expect.soft(Math.abs(shown - measured), `[${width}px] 約 ${shown} 行 vs ${measured.toFixed(2)} laid out`).toBeLessThanOrEqual(1);
      if (description !== "long") {
        const lineBoxes = await cmp.locator(".task-card__desc p").first().evaluate((p: HTMLElement) => {
          const range = document.createRange();
          range.selectNodeContents(p);
          return new Set(Array.from(range.getClientRects(), (r) => Math.round(r.top))).size;
        });
        expect.soft(Math.abs(shown - lineBoxes), `[${width}px] 約 ${shown} 行 vs ${lineBoxes} line boxes`).toBeLessThanOrEqual(1);
      }
      if (description === "joined") expect.soft(shown, `[${width}px] joined source lines`).toBe(1);
      if (description === "paragraph" && width === 390) {
        expect.soft(shown, `[${width}px] wrapped paragraph`).toBeGreaterThan(6);
        await expect(cmp.getByTestId("task-desc-toggle")).toHaveText("展開全文");
      }
      await page.setViewportSize({ width: width === 390 ? 1440 : 390, height: 900 });
      await expect
        .poll(async () => Math.abs((await summaryLines(cmp)) - (await renderedLines(cmp))), {
          message: `[${width}px→resized] count follows the new width`,
        })
        .toBeLessThanOrEqual(1);
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
    await expect(cmp).toHaveAttribute("aria-expanded", "true");
    const bar = await wfHead.evaluate((el: HTMLElement) => ({ over: el.scrollWidth - el.clientWidth, h: el.clientHeight }));
    expect.soft(bar.over, `[${width}px] collapsed workflow bar overflows`).toBeLessThanOrEqual(0);

    const descHead = cmp.getByTestId("task-desc-section-head");
    const shownLines = await summaryLines(cmp);
    await descHead.click();
    await expect(cmp.locator(".task-card__desc")).toHaveCount(0);
    expect.soft(await summaryLines(cmp), `[${width}px] folded summary keeps the count`).toBe(shownLines);
    await descHead.click();
    await expect(cmp.locator(".task-card__desc")).toBeVisible();
    await wfHead.click();
    await expect(cmp.getByTestId("task-step")).toHaveCount(4);
    expect.soft(await pageOverflow(page), `[${width}px] page horizontal scroll`).toBeLessThanOrEqual(0);
  });
}
