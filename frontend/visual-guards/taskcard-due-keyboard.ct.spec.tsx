// The date input reports each keyboard segment edit as a complete date, so a
// guard in jsdom (where fireEvent.change hands over a finished value) cannot
// see a partial edit being saved. This drives Chromium's real segmented input.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardDueStory } from "./stories/TaskCardDueStory";

const saves = (page: any) => page.evaluate(() => window.__dueSaves);
const leadSaves = (page: any) => page.evaluate(() => window.__leadSaves);
const endOfDay = (page: any, y: number, m: number, d: number) =>
  page.evaluate(
    ([yy, mm, dd]: number[]) =>
      Math.floor(new Date(yy, mm - 1, dd, 23, 59, 59).getTime() / 1000),
    [y, m, d]
  );

test.use({ locale: "en-US" });

test("typing a date segment by segment saves nothing until Enter", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  const input = cmp.getByTestId("task-due-input");
  await expect(input).toHaveValue("2099-10-30");
  await input.focus();

  await page.keyboard.type("1");
  await expect(input).toHaveValue("2099-01-30");
  await page.keyboard.type("1");
  await expect(input).toHaveValue("2099-11-30");
  await page.keyboard.type("15");
  await expect(input).toHaveValue("2099-11-15");
  expect(await saves(page)).toEqual([]);
  await expect(cmp.getByTestId("task-due-pop")).toBeVisible();

  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 11, 15)]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
});

test("a partial edit is committed once by clicking outside", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  const input = cmp.getByTestId("task-due-input");
  await input.focus();
  await page.keyboard.type("1");
  expect(await saves(page)).toEqual([]);

  await cmp.getByTestId("outside").click();
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 1, 30)]);
});

test("Escape discards a typed edit", async ({ mount, page }) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").focus();
  await page.keyboard.type("11");
  await page.keyboard.press("Escape");
  expect(await saves(page)).toEqual([]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
});

test("清除 after a typed edit saves only the clear", async ({ mount, page }) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").focus();
  await page.keyboard.type("11");
  await cmp.getByTestId("task-due-clear").click();
  expect(await saves(page)).toEqual([null]);
});

test("clicking the chip again closes the popover for good and saves once", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  const chip = cmp.getByTestId("task-due");
  await chip.click();
  await cmp.getByTestId("task-due-input").focus();
  await page.keyboard.type("11");
  await chip.click();
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
  await page.waitForTimeout(100);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 11, 30)]);
});

test("a lead typed digit by digit is saved once, on Enter", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-lead").focus();
  await page.keyboard.type("14");
  expect(await saves(page)).toEqual([]);
  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 10, 30)]);
  expect(await leadSaves(page)).toEqual([14]);
});

test("unparseable lead text is not saved as the workspace setting", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-lead").focus();
  await page.keyboard.type("e");
  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([]);
  await expect(cmp.getByText("快到期提醒要是 1 到 30 的整數天數，這次沒有變更")).toBeVisible();
});

const OVERDUE_2020 = Math.floor(new Date(2020, 0, 1, 23, 59, 59).getTime() / 1000);

for (const width of [320, 390]) {
  test(`the 到期日 row with its overdue icon, and the due popover, stay inside the card at ${width}px`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 700 });
    const cmp = await mount(<TaskCardDueStory dueTs={OVERDUE_2020} />);
    const card = (await cmp.locator(".task-card").boundingBox())!;
    const content = (await cmp.locator(".task-card__meta").boundingBox())!;
    const date = (await cmp.getByTestId("task-due").boundingBox())!;
    const icon = (await cmp.getByTestId("task-due-alert").boundingBox())!;
    expect(icon.x).toBeGreaterThanOrEqual(date.x + date.width);
    expect(Math.abs(icon.y + icon.height / 2 - (date.y + date.height / 2))).toBeLessThan(2);
    expect(icon.x + icon.width).toBeLessThanOrEqual(content.x + content.width);
    expect(
      await cmp.locator(".task-card").evaluate((el) => el.scrollWidth - el.clientWidth)
    ).toBe(0);

    await cmp.getByTestId("task-due").click();
    const pop = (await cmp.getByTestId("task-due-pop").boundingBox())!;
    const input = (await cmp.getByTestId("task-due-lead").boundingBox())!;
    const unit = (await cmp.locator(".task-card__due-lead-unit").boundingBox())!;
    expect(pop.x).toBeGreaterThanOrEqual(card.x);
    expect(pop.x + pop.width).toBeLessThanOrEqual(content.x + content.width);
    expect(pop.y).toBeGreaterThan(date.y);
    expect(input.width).toBeGreaterThanOrEqual(80);
    expect(unit.x).toBeGreaterThanOrEqual(input.x + input.width);
    expect(Math.abs(unit.y + unit.height / 2 - (input.y + input.height / 2))).toBeLessThan(2);
    expect(unit.x + unit.width).toBeLessThanOrEqual(pop.x + pop.width);
  });
}
