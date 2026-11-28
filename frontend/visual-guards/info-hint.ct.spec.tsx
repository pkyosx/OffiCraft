// The ⓘ after a title: a tap pins its explanation in a bubble anchored to the
// icon, inside the viewport, without moving the page; a tap elsewhere closes
// it; a tap on an ⓘ inside a row never opens the row; Esc inside a dialog
// closes only the bubble. jsdom has no layout and no real hit-testing, so the
// anchoring, the overflow and "the icon never wraps alone" live here.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { InfoHintStory } from "./stories/InfoHintStory";
import { zh } from "../src/i18n/locales/zh";

const s = zh.settings;

async function pageOverflow(page: Page) {
  return page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
}

/** The bubble sits 6px under (or over) the icon's box, starting at its left
 * edge unless pulled back inside the 8px viewport margin, and is whole on screen. */
async function expectAnchored(page: Page, icon: Locator) {
  const tip = page.getByRole("tooltip");
  await expect(tip).toBeVisible();
  const i = (await icon.boundingBox())!;
  const t = (await tip.boundingBox())!;
  const vw = page.viewportSize()!.width;
  const below = Math.abs(t.y - (i.y + i.height + 6)) <= 1;
  const above = Math.abs(t.y + t.height - (i.y - 6)) <= 1;
  expect(below || above, `bubble ${t.y}..${t.y + t.height} vs icon ${i.y}..${i.y + i.height}`).toBe(true);
  expect(t.x === 8 || t.x + t.width === vw - 8 || Math.abs(t.x - i.x) <= 1, `bubble x ${t.x} vs icon x ${i.x}`).toBe(true);
  expect(t.x).toBeGreaterThanOrEqual(8);
  expect(t.x + t.width).toBeLessThanOrEqual(vw - 8 + 0.5);
  expect(await pageOverflow(page)).toBe(0);
}

/** Every ⓘ sits on the same line as the last character of the title before it. */
async function orphanIcons(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>(".info-hint__glue")).flatMap((box) => {
      const title = box.firstChild;
      if (!title || title.nodeType !== Node.TEXT_NODE || !title.textContent) return [];
      const r = document.createRange();
      r.setStart(title, title.textContent.length - 1);
      r.setEnd(title, title.textContent.length);
      const last = r.getBoundingClientRect();
      const icon = box.querySelector(".info-hint")!.getBoundingClientRect();
      const mid = (icon.top + icon.bottom) / 2;
      const beside = icon.left >= last.right - 0.5 && icon.left - last.right <= 8;
      return mid >= last.top && mid <= last.bottom && beside ? [] : [title.textContent];
    }),
  );
}

/** Rows whose title (with its ⓘ) runs into the control beside it. */
async function titlesIntoControl(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>(".param-row")).flatMap((row) => {
      const icon = row.querySelector(".param-row__name .info-hint")?.getBoundingClientRect();
      const control = row.querySelector(".param-pct, .set-toggle")?.getBoundingClientRect();
      return icon && control && icon.right > control.left ? [row.querySelector(".param-row__name")!.textContent] : [];
    }),
  );
}

async function tapOutside(page: Page) {
  await page.mouse.click(2, page.viewportSize()!.height - 2);
}

for (const width of [390, 1440]) {
  test.describe(`width ${width}`, () => {
    test.beforeEach(async ({ page }) => {
      await page.setViewportSize({ width, height: 844 });
    });

    test("進階設定: a tap opens the bubble at the icon, the page does not overflow, a tap elsewhere closes it, no icon wraps alone, and no title runs into its control", async ({ mount, page }) => {
      await mount(<InfoHintStory mode="settings" />);
      await page.getByTestId("settings-advanced-entry").click();
      const icon = page.getByRole("img", { name: `關於 ${s.agentTokenTtl}` });
      await expect(icon).toBeVisible();
      await expect(page.getByRole("img", { name: `關於 ${s.sessionTtl}` })).toHaveCount(0);
      expect(await page.getByRole("tooltip").count()).toBe(0);
      expect(await orphanIcons(page)).toEqual([]);
      expect(await titlesIntoControl(page)).toEqual([]);

      const target = (await icon.evaluate((el) => {
        const r = el.getBoundingClientRect();
        const a = getComputedStyle(el, "::after");
        return { w: r.width - 2 * parseFloat(a.left), h: r.height - 2 * parseFloat(a.top) };
      }));
      expect(target.w).toBeGreaterThanOrEqual(24);
      expect(target.h).toBeGreaterThanOrEqual(24);

      await icon.click();
      await expectAnchored(page, icon);
      await expect(page.getByRole("tooltip")).toHaveText(s.agentTokenTtlSub);
      await expect(page.locator("#param-agent-ttl")).not.toBeFocused();

      await tapOutside(page);
      await expect(page.getByRole("tooltip")).toHaveCount(0);
    });

    test("全域情境: tapping a row's ⓘ shows its text and does not open the row", async ({ mount, page }) => {
      await mount(<InfoHintStory mode="settings" />);
      await page.getByTestId("settings-global-context-entry").click();
      const row = page.getByTestId("boot-doc-entry-system_interaction");
      const icon = row.getByTestId("info-hint");
      await icon.click();
      await expectAnchored(page, icon);
      await expect(page.getByRole("tooltip")).toHaveText(s.systemSub);
      await expect(row).toBeVisible();
      await expect(page.getByRole("heading", { name: s.systemName })).toHaveCount(0);
      await expect(page.getByTestId("boot-doc-entry-task_closeout").getByTestId("info-hint")).toHaveCount(0);
      expect(await orphanIcons(page)).toEqual([]);

      await tapOutside(page);
      await expect(page.getByRole("tooltip")).toHaveCount(0);
      await expect(row).toBeVisible();
    });

    test("頭像選單: tapping a row's ⓘ shows its text inside the viewport and stays on the first level", async ({ mount, page }) => {
      await mount(<InfoHintStory mode="menu" />);
      await expect(page.getByRole("button", { name: zh.profile.accountSecurity }).getByTestId("info-hint")).toHaveCount(0);
      const row = page.getByRole("button", { name: zh.profile.pushContactEmail });
      await expect(row).toHaveAccessibleDescription(`${zh.profile.pushContactEmailUnsetWarning} ${zh.profile.pushContactEmailPrivacy}`);
      const icon = row.getByTestId("info-hint");
      await icon.click();
      await expectAnchored(page, icon);
      await expect(page.getByRole("tooltip")).toHaveText(zh.profile.pushContactEmailPrivacy);
      await expect(page.locator(".profile-dd__back")).toHaveCount(0);
      await expect(page.getByText(zh.profile.pushContactEmailUnsetWarning)).toBeVisible();
    });

    test("對話框: Esc closes only the bubble, and the next Esc closes the dialog", async ({ mount, page }) => {
      await mount(<InfoHintStory mode="dialog" />);
      await page.getByTestId("info-hint").click();
      await expect(page.getByRole("tooltip")).toHaveText("對話框裡的說明");
      await page.keyboard.press("Escape");
      await expect(page.getByRole("tooltip")).toHaveCount(0);
      await expect(page.getByRole("dialog")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(page.getByTestId("dialog-closed")).toBeVisible();
    });
  });
}

test("a wrapping title keeps its last character and the ⓘ on one line at every width", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<InfoHintStory mode="wrap" />);
  await expect(page.getByTestId("info-hint").first()).toBeVisible();
  expect(await orphanIcons(page)).toEqual([]);
});
