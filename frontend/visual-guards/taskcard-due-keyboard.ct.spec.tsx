// The date input reports each keyboard segment edit as a complete date, so a
// guard in jsdom (where fireEvent.change hands over a finished value) cannot
// see a partial edit being saved. This drives Chromium's real segmented input.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardDueStory } from "./stories/TaskCardDueStory";

const saves = (page: any) => page.evaluate(() => window.__dueSaves);
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
