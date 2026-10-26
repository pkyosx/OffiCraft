// Drives Chromium's real date and time inputs: they take no typing, a picker
// choice (simulated with fill) is saved only on Enter, blur or an outside
// click, and the picker fields share one line at phone widths.
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

test("digits typed into the date and time keep their values, and a picked date is saved once, on Enter", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  const input = cmp.getByTestId("task-due-input");
  const time = cmp.getByTestId("task-due-time-input");
  await expect(input).toHaveValue("2099-10-30");
  await input.focus();
  await page.keyboard.type("1115");
  await page.keyboard.press("Backspace");
  await page.keyboard.press("ArrowUp");
  await expect(input).toHaveValue("2099-10-30");
  await time.focus();
  await page.keyboard.type("0230P");
  await expect(time).toHaveValue("");
  await expect(cmp.getByTestId("task-due-pop")).toBeVisible();

  await input.fill("2099-11-15");
  await expect(cmp.getByTestId("task-due-date-pick")).toHaveText("2099/11/15");
  expect(await saves(page)).toEqual([]);
  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 11, 15)]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
});

test("a picked date is committed once by clicking outside", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").fill("2099-01-30");
  expect(await saves(page)).toEqual([]);

  await cmp.getByTestId("outside").click();
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 1, 30)]);
});

test("Escape discards a picked date", async ({ mount, page }) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").fill("2099-11-30");
  await page.keyboard.press("Escape");
  expect(await saves(page)).toEqual([]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
});

test("the date's ✕ after a picked date keeps the popover open, and ✓ then saves only the clear", async ({ mount, page }) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").fill("2099-11-30");
  await cmp.getByTestId("task-due-date-clear").click();
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(1);
  expect(await saves(page)).toEqual([]);
  await cmp.getByTestId("task-due-apply").click();
  expect(await saves(page)).toEqual([null]);
});

test("the time's ✕ takes the picked time off and leaves the date", async ({ mount, page }) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-time-input").fill("15:41");
  await cmp.getByTestId("task-due-time-clear").click();
  await expect(cmp.getByTestId("task-due-time-pick")).toHaveText("不指定時間");
  await cmp.getByTestId("task-due-apply").click();
  expect(await saves(page)).toEqual([]);
});

test("clicking the chip again closes the popover for good and saves once", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  const chip = cmp.getByTestId("task-due");
  await chip.click();
  await cmp.getByTestId("task-due-input").fill("2099-11-30");
  await chip.click();
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
  await page.waitForTimeout(100);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
  expect(await saves(page)).toEqual([await endOfDay(page, 2099, 11, 30)]);
});

test("a time picked after tabbing over from the date is saved once, on Enter, as that local minute", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  await cmp.getByTestId("task-due-input").focus();
  const time = cmp.getByTestId("task-due-time-input");
  // Tab walks the date's segments and its picker button before leaving it.
  for (let i = 0; i < 6; i++) {
    if (await time.evaluate((el) => el === document.activeElement)) break;
    await page.keyboard.press("Tab");
  }
  await expect(time).toBeFocused();
  await expect(cmp.getByTestId("task-due-pop")).toBeVisible();
  await time.fill("14:30");
  await expect(cmp.getByTestId("task-due-time-pick")).toHaveText("14:30");
  expect(await saves(page)).toEqual([]);

  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([
    await page.evaluate(() => Math.floor(new Date(2099, 9, 30, 14, 30, 0).getTime() / 1000)),
  ]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
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
  expect(await leadSaves(page)).toEqual([20160]);
});

test("picking 小時 in the unit select keeps the popover open and saves nothing; Enter then saves 2 小時 as 120 minutes", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  const lead = cmp.getByTestId("task-due-lead");
  const unit = cmp.getByTestId("task-due-lead-unit");
  await lead.focus();
  await page.keyboard.type("2");
  await page.keyboard.press("Tab");
  await expect(unit).toBeFocused();
  await unit.selectOption("hour");
  await expect(unit).toHaveValue("hour");
  await expect(cmp.getByTestId("task-due-pop")).toBeVisible();
  expect(await saves(page)).toEqual([]);

  await lead.focus();
  await page.keyboard.press("Enter");
  expect(await leadSaves(page)).toEqual([120]);
  await expect(cmp.getByTestId("task-due-pop")).toHaveCount(0);
});

test("the unit select is in the muted 前 colour while the lead follows the workspace, and in the field text colour once a number is typed", async ({
  mount,
}) => {
  const cmp = await mount(<TaskCardDueStory />);
  await cmp.getByTestId("task-due").click();
  const colour = (loc: any) => loc.evaluate((el: Element) => getComputedStyle(el).color);
  const lead = cmp.getByTestId("task-due-lead");
  const unit = cmp.getByTestId("task-due-lead-unit");
  const muted = await colour(cmp.locator(".task-card__due-lead-before"));
  expect(await colour(unit)).toBe(muted);
  await lead.fill("3");
  expect(await colour(unit)).toBe(await colour(lead));
});

test("with no due date, the date, time, lead and unit fields share one muted text colour, one background and full opacity", async ({
  mount,
}) => {
  const cmp = await mount(<TaskCardDueStory dueTs={null as never} />);
  await cmp.getByTestId("task-due").click();
  const look = (loc: any) =>
    loc.evaluate((el: Element) => {
      const s = getComputedStyle(el);
      return {
        text: s.getPropertyValue("-webkit-text-fill-color"),
        background: s.backgroundColor,
        opacity: s.opacity,
      };
    });
  const leadPlaceholder = await cmp
    .getByTestId("task-due-lead")
    .evaluate((el: Element) => getComputedStyle(el, "::placeholder").getPropertyValue("-webkit-text-fill-color"));
  const muted = await cmp
    .locator(".task-card__due-lead-before")
    .evaluate((el: Element) => getComputedStyle(el).color);
  const date = await look(cmp.getByTestId("task-due-date-pick"));
  expect(date).toEqual({ text: muted, background: date.background, opacity: "1" });
  expect(await look(cmp.getByTestId("task-due-time-pick"))).toEqual(date);
  expect(await look(cmp.getByTestId("task-due-lead"))).toEqual(date);
  expect(await look(cmp.getByTestId("task-due-lead-unit"))).toEqual(date);
  expect(leadPlaceholder).toBe(muted);
});

test("unparseable lead text does not replace the task's own lead with the workspace setting", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<TaskCardDueStory dueSoonLeadMinutes={7200} />);
  await cmp.getByTestId("task-due").click();
  const lead = cmp.getByTestId("task-due-lead");
  await expect(lead).toHaveValue("5");
  await lead.focus();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("e");
  expect(await lead.evaluate((el: HTMLInputElement) => el.validity.badInput)).toBe(true);
  await page.keyboard.press("Enter");
  expect(await saves(page)).toEqual([]);
  expect(await leadSaves(page)).toEqual([]);
  await expect(cmp.getByText("快到期提醒要是 1 分鐘到 30 天之間的整數，這次沒有變更")).toBeVisible();
});

const OVERDUE_2020 = Math.floor(new Date(2020, 0, 1, 23, 59, 59).getTime() / 1000);
const OVERDUE_2020_1541 = Math.floor(new Date(2020, 0, 1, 15, 41, 0).getTime() / 1000);

const sameLine = (a: { y: number; height: number }, b: { y: number; height: number }) =>
  Math.abs(a.y + a.height / 2 - (b.y + b.height / 2));

for (const width of [320, 390]) {
  for (const [label, dueTs] of [
    ["without a time", OVERDUE_2020],
    ["with 15:41", OVERDUE_2020_1541],
  ] as const) {
    test(`at ${width}px the 到期日 row with its red date and the days overdue, and the due popover ${label} with date and time on one line and 「[n] [unit] 前」 on one line, stay inside the card`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 700 });
      const cmp = await mount(<TaskCardDueStory dueTs={dueTs} />);
      const card = (await cmp.locator(".task-card").boundingBox())!;
      const content = (await cmp.locator(".task-card__meta").boundingBox())!;
      const chip = (await cmp.getByTestId("task-due").boundingBox())!;
      expect(chip.x + chip.width).toBeLessThanOrEqual(content.x + content.width);
      const left = (await cmp.getByTestId("task-due-remaining").boundingBox())!;
      expect(left.x + left.width).toBeLessThanOrEqual(content.x + content.width);
      expect(
        await cmp.locator(".task-card").evaluate((el) => el.scrollWidth - el.clientWidth)
      ).toBe(0);

      await cmp.getByTestId("task-due").click();
      const pop = (await cmp.getByTestId("task-due-pop").boundingBox())!;
      const datePick = cmp.getByTestId("task-due-date-pick");
      await expect(datePick).toHaveText("2020/01/01");
      const date = (await datePick.boundingBox())!;
      const time = (await cmp.getByTestId("task-due-time-pick").boundingBox())!;
      const last = (await cmp.locator(".task-card__due-when > :last-child").boundingBox())!;
      const input = (await cmp.getByTestId("task-due-lead").boundingBox())!;
      const unit = (await cmp.getByTestId("task-due-lead-unit").boundingBox())!;
      const before = (await cmp.locator(".task-card__due-lead-before").boundingBox())!;
      expect(pop.x).toBeGreaterThanOrEqual(card.x);
      expect(pop.x + pop.width).toBeLessThanOrEqual(content.x + content.width);
      expect(pop.y).toBeGreaterThan(chip.y);
      expect(date.x).toBeGreaterThanOrEqual(pop.x);
      expect(await datePick.evaluate((el) => el.clientWidth - el.scrollWidth)).toBeGreaterThanOrEqual(0);
      expect(sameLine(time, date)).toBeLessThan(2);
      expect(time.x).toBeGreaterThanOrEqual(date.x + date.width);
      expect(sameLine(last, date)).toBeLessThan(2);
      expect(last.x + last.width).toBeLessThanOrEqual(pop.x + pop.width);
      expect(input.width).toBeGreaterThanOrEqual(60);
      expect(unit.x).toBeGreaterThanOrEqual(input.x + input.width);
      expect(before.x).toBeGreaterThanOrEqual(unit.x + unit.width);
      expect(sameLine(unit, input)).toBeLessThan(2);
      expect(sameLine(before, input)).toBeLessThan(2);
      expect(before.x + before.width).toBeLessThanOrEqual(pop.x + pop.width);
      expect(before.x + before.width).toBeLessThanOrEqual(content.x + content.width);
      expect(Math.abs(time.x - unit.x)).toBeLessThan(1);
      expect(Math.abs(time.x + time.width - (before.x + before.width))).toBeLessThan(1);
      const apply = (await cmp.getByTestId("task-due-apply").boundingBox())!;
      const cancel = (await cmp.getByTestId("task-due-cancel").boundingBox())!;
      expect(sameLine(cancel, apply)).toBeLessThan(2);
      expect(cancel.x).toBeGreaterThanOrEqual(apply.x + apply.width);
      expect(cancel.x + cancel.width).toBeLessThanOrEqual(pop.x + pop.width);
      expect(apply.y).toBeGreaterThan(input.y + input.height);
      expect(
        await cmp.locator(".task-card").evaluate((el) => el.scrollWidth - el.clientWidth)
      ).toBe(0);
    });
  }
}

test("on a desktop-wide card the due popover keeps a picker's width, with date and time on one line, and the time field lined up over 「[unit] 前」 at one width with or without a time", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 700 });
  const cmp = await mount(<TaskCardDueStory dueTs={OVERDUE_2020_1541} />);
  await cmp.getByTestId("task-due").click();
  const pop = (await cmp.getByTestId("task-due-pop").boundingBox())!;
  const date = (await cmp.getByTestId("task-due-date-pick").boundingBox())!;
  const time = (await cmp.getByTestId("task-due-time-pick").boundingBox())!;
  await expect(cmp.getByTestId("task-due-time-pick")).toHaveText("15:41");
  expect(pop.width).toBeLessThanOrEqual(300);
  expect(sameLine(time, date)).toBeLessThan(2);
  expect(time.x).toBeGreaterThanOrEqual(date.x + date.width);
  expect(time.x + time.width).toBeLessThanOrEqual(pop.x + pop.width);
  const unit = (await cmp.getByTestId("task-due-lead-unit").boundingBox())!;
  const before = (await cmp.locator(".task-card__due-lead-before").boundingBox())!;
  expect(Math.abs(time.x - unit.x)).toBeLessThan(1);
  expect(Math.abs(time.x + time.width - (before.x + before.width))).toBeLessThan(1);
  await cmp.getByTestId("task-due-time-clear").click();
  await expect(cmp.getByTestId("task-due-time-pick")).toHaveText("不指定時間");
  expect((await cmp.getByTestId("task-due-time-pick").boundingBox())!).toEqual(time);
  await cmp.getByTestId("task-due-time-input").fill("09:05");
  await expect(cmp.getByTestId("task-due-time-pick")).toHaveText("09:05");
  expect((await cmp.getByTestId("task-due-time-pick").boundingBox())!).toEqual(time);
  expect((await cmp.getByTestId("task-due-date-pick").boundingBox())!).toEqual(date);
  // iOS Safari draws the unit select narrower than 「不指定時間」.
  await page.addStyleTag({ content: ".task-card__due-lead-unit { flex: none; width: 40px; }" });
  const narrowWithTime = (await cmp.getByTestId("task-due-time-pick").boundingBox())!;
  await cmp.getByTestId("task-due-time-clear").click();
  expect((await cmp.getByTestId("task-due-time-pick").boundingBox())!).toEqual(narrowWithTime);
});
