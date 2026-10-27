// Office sidebar chat groups, measured in real Chromium against the real
// office CSS: the layout at phone widths (no sideways scroll, pills inside
// their headings, menus inside the window), and the gestures jsdom cannot
// drive (long-press lift and drag, a touch long-press released in place
// opening the menu, keyboard drag).
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { OfficeChatGroupsStory } from "./stories/OfficeChatGroupsStory";
import { sampleGroups } from "./stories/chatGroupsFixtures";

// Comfortably past ChatGroupList's 450ms hold.
const HOLD_MS = 650;

const row = (cmp: Locator, name: string) => cmp.locator(".member-card", { hasText: name }).first();
async function grip(l: Locator) {
  const b = (await l.boundingBox())!;
  return { x: b.x + 80, y: b.y + b.height / 2 + 6 };
}
const sectionNames = (cmp: Locator, id: string) =>
  cmp.getByTestId(`chat-group-section-${id}`).locator(".member-card__name").allTextContents();
const headings = (cmp: Locator) => cmp.locator(".chat-groups__heading").allTextContents();

async function touch(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 });
  return (type: "touchStart" | "touchMove" | "touchEnd", x = 0, y = 0) =>
    cdp.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : [{ x, y }] });
}

async function measure(page: Page) {
  return page.evaluate(() => {
    const r = (el: Element) => el.getBoundingClientRect();
    return {
      pageOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      viewport: window.innerWidth,
      headings: Array.from(document.querySelectorAll(".chat-groups__heading")).map((h) => ({
        head: r(h),
        list: r(h.parentElement!),
        pill: h.querySelector(".member-card__unread") ? r(h.querySelector(".member-card__unread")!) : null,
      })),
      menus: Array.from(document.querySelectorAll(".chat-groups__menu-panel, .chat-groups__sheet, .chat-groups__dialog")).map(
        (m) => r(m),
      ),
    };
  });
}

for (const width of [1280, 390, 320]) {
  test(`${width}px: with groups, a collapsed group's pill, the member menu and the new-group entry all fit with no sideways scroll`, async ({
    mount,
    page,
  }) => {
    const mobile = width < 720;
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(
      <OfficeChatGroupsStory mobile={mobile} initialGroups={sampleGroups} initialCollapsed={{ "g-lead": true }} />,
    );
    const at = `[${width}px]`;
    let m = await measure(page);
    expect(m.pageOver, `${at} page scrolls sideways`).toBeLessThanOrEqual(1);
    expect(m.headings.length, `${at} headings`).toBe(3);
    const pilled = m.headings.filter((h) => h.pill);
    expect(pilled.length, `${at} the collapsed group lost its pill`).toBe(1);
    for (const h of m.headings) {
      expect(h.head.right, `${at} heading past the list`).toBeLessThanOrEqual(h.list.right + 0.5);
      if (h.pill) {
        expect(h.pill.left).toBeGreaterThanOrEqual(h.head.left);
        expect(h.pill.right).toBeLessThanOrEqual(h.head.right);
      }
    }

    if (mobile) {
      const t = await touch(page);
      const g = await grip(row(cmp, "Evan"));
      await t("touchStart", g.x, g.y);
      await page.waitForTimeout(HOLD_MS);
      await t("touchEnd");
      await expect(page.getByTestId("chat-member-menu-sheet")).toBeVisible();
    } else {
      await row(cmp, "Evan").click({ button: "right", position: { x: 120, y: 20 } });
      await page.getByTestId("chat-member-menu-move").click();
    }
    m = await measure(page);
    expect(m.pageOver, `${at} page scrolls sideways with the menu open`).toBeLessThanOrEqual(1);
    expect(m.menus.length, `${at} no menu`).toBeGreaterThan(0);
    for (const box of m.menus) {
      expect(box.left, `${at} menu off the left edge`).toBeGreaterThanOrEqual(0);
      expect(box.right, `${at} menu off the right edge`).toBeLessThanOrEqual(m.viewport + 0.5);
    }
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");

    await cmp.getByTestId("chat-group-new").click();
    await page.keyboard.type("一個相當長的群組名稱用來看看輸入框在窄螢幕會不會撐開");
    m = await measure(page);
    expect(m.pageOver, `${at} page scrolls sideways with the entry open`).toBeLessThanOrEqual(1);
  });
}

test("the group headings read at the muted-text floor (4.5:1) against the page", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const ratios = await cmp.locator(".chat-groups__heading").evaluateAll((els) => {
    const parse = (c: string) => c.match(/[\d.]+/g)!.map(Number);
    const lum = ([r, g, b]: number[]) => {
      const f = (v: number) => {
        const x = v / 255;
        return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const bg = lum(parse(getComputedStyle(document.body).backgroundColor));
    return els.map((el) => {
      const [hi, lo] = [lum(parse(getComputedStyle(el).color)), bg].sort((a, b) => b - a);
      return (hi + 0.05) / (lo + 0.05);
    });
  });
  expect(ratios.length).toBe(3);
  for (const r of ratios) expect(r).toBeGreaterThanOrEqual(4.5);
});

test("a long press lifts an ungrouped member and dropping it on a row of another group puts it there, without opening a chat", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const from = await grip(row(cmp, "Evan"));
  const to = await grip(row(cmp, "Dora"));
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(1);
  await page.mouse.move(from.x, to.y - 6, { steps: 12 });
  await page.waitForTimeout(300);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Evan", "Dora"]);
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["EvaRhapsodyInboxForwarderStandbyRelay"]);
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
});

test("a long press on a group heading lifts the group, and dropping it above the first reorders the groups", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const h = (await cmp.getByTestId("chat-group-heading-g-eng").boundingBox())!;
  const top = (await cmp.getByTestId("chat-group-heading-g-lead").boundingBox())!;
  await page.mouse.move(h.x + 60, h.y + h.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.move(h.x + 60, top.y - 12, { steps: 12 });
  await page.waitForTimeout(300);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await headings(cmp)).toEqual(["工程 · 2", "核心 · 2", "未分組 · 2"]);
  expect(await sectionNames(cmp, "g-lead")).toEqual(["Mira", "Clara"]);
});

test("a press that moves before the hold ends lifts nothing", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const g = await grip(row(cmp, "Evan"));
  await page.mouse.move(g.x, g.y);
  await page.mouse.down();
  await page.mouse.move(g.x, g.y - 80, { steps: 4 });
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  await page.mouse.up();
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
});

test("390px touch: a long press released in place opens the heading's menu; a long press then a drag moves the member instead", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const head = await grip(cmp.getByTestId("chat-group-heading-g-eng"));
  await t("touchStart", head.x, head.y - 6);
  await page.waitForTimeout(HOLD_MS);
  await t("touchEnd");
  await expect(page.getByTestId("chat-group-menu-sheet")).toBeVisible();
  await expect(page.getByTestId("chat-group-menu-rename")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("chat-group-menu-sheet")).toHaveCount(0);

  const from = await grip(row(cmp, "Evan"));
  const to = await grip(row(cmp, "Dora"));
  await t("touchStart", from.x, from.y);
  await page.waitForTimeout(HOLD_MS);
  for (let i = 1; i <= 12; i++) await t("touchMove", from.x, from.y + ((to.y - 6 - from.y) * i) / 12);
  await page.waitForTimeout(300);
  await t("touchEnd");
  await page.waitForTimeout(400);
  await expect(page.getByTestId("chat-member-menu-sheet")).toHaveCount(0);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Evan", "Dora"]);
});

test("from the keyboard, Space lifts a member, ArrowUp moves it and Enter drops it there without opening its chat", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  await row(cmp, "Clara").focus();
  await page.keyboard.press("Space");
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(1);
  // The sensor starts listening for the move keys a tick after it lifts.
  await page.waitForTimeout(100);
  await page.keyboard.press("ArrowUp");
  await page.waitForTimeout(300);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  expect(await sectionNames(cmp, "g-lead")).toEqual(["Clara", "Mira"]);
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
});
