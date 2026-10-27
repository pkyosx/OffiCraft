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

for (const theme of ["dark", "light", "halloween"] as const) {
  test(`${theme}: an expanded group's rows hang 13px in on a 1px guide line running from under its heading to its last row; a collapsed group is its bold heading with the summed unread`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const cmp = await mount(
      <OfficeChatGroupsStory theme={theme} initialGroups={sampleGroups} initialCollapsed={{ "g-lead": true }} />,
    );
    await page.waitForTimeout(300);
    const m = await cmp.locator(".office__members-list").evaluate((list) => {
      const channels = (c: string) => {
        const n = c.match(/[\d.]+/g)!.map(Number);
        // color(srgb r g b / a) is 0–1; rgb()/rgba() is 0–255.
        return c.startsWith("color(") ? [n[0] * 255, n[1] * 255, n[2] * 255, n[3] ?? 1] : [n[0], n[1], n[2], n[3] ?? 1];
      };
      const lum = ([r, g, b]: number[]) => {
        const f = (v: number) => {
          const x = v / 255;
          return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
        };
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
      };
      const bg = channels(getComputedStyle(document.body).backgroundColor);
      const listLeft = list.getBoundingClientRect().left;
      const headings = Array.from(list.querySelectorAll(".chat-groups__heading"), (h) => ({
        text: h.textContent,
        expanded: h.getAttribute("aria-expanded"),
        labelWeight: getComputedStyle(h.querySelector(".chat-groups__heading-label")!).fontWeight,
      }));
      const sections = Array.from(list.querySelectorAll(".chat-groups__section"), (sec) => {
        const cs = getComputedStyle(sec);
        const box = sec.getBoundingClientRect();
        const rows = sec.querySelectorAll(".member-card");
        const heading = sec.previousElementSibling!.getBoundingClientRect();
        const [r, g, b, a] = channels(cs.borderLeftColor);
        const line = [r * a + bg[0] * (1 - a), g * a + bg[1] * (1 - a), b * a + bg[2] * (1 - a)];
        const [hi, lo] = [lum(line), lum(bg)].sort((x, y) => y - x);
        return {
          group: sec.getAttribute("data-testid"),
          indent: Math.round(box.left - listLeft),
          lineWidth: cs.borderLeftWidth,
          lineStyle: cs.borderLeftStyle,
          startsBelowHeading: box.top >= heading.bottom,
          topAtFirstRow: Math.round(rows[0].getBoundingClientRect().top - box.top),
          bottomAtLastRow: Math.round(box.bottom - rows[rows.length - 1].getBoundingClientRect().bottom),
          rowsInside: Array.from(rows).every((row) => row.getBoundingClientRect().left >= box.left + 1),
          contrast: (hi + 0.05) / (lo + 0.05),
        };
      });
      return { headings, sections };
    });

    const ungrouped = theme === "halloween" ? "遊蕩中" : "未分組";
    expect(m.headings).toEqual([
      { text: "核心 · 23", expanded: "false", labelWeight: "700" },
      { text: "工程 · 2", expanded: "true", labelWeight: "700" },
      { text: `${ungrouped} · 2`, expanded: "true", labelWeight: "700" },
    ]);
    expect(m.sections.map(({ contrast: _c, ...rest }) => rest)).toEqual([
      {
        group: "chat-group-section-g-eng",
        indent: 13,
        lineWidth: "1px",
        lineStyle: "solid",
        startsBelowHeading: true,
        topAtFirstRow: 0,
        bottomAtLastRow: 0,
        rowsInside: true,
      },
      {
        group: "chat-group-section-ungrouped",
        indent: 13,
        lineWidth: "1px",
        lineStyle: "solid",
        startsBelowHeading: true,
        topAtFirstRow: 0,
        bottomAtLastRow: 0,
        rowsInside: true,
      },
    ]);
    for (const sec of m.sections) {
      expect(sec.contrast, `${sec.group} guide line contrast`).toBeGreaterThanOrEqual(1.4);
    }
  });
}

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

/** The open 自動加入 cascade: main menu, submenu, the row it hangs off, and
 * its header block. */
async function cascade(page: Page) {
  return page.evaluate(() => {
    const box = (el: Element) => {
      const r = el.getBoundingClientRect();
      return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
    };
    const main = document.querySelector('[data-testid="chat-group-menu-panel"]')!;
    const sub = document.querySelector('[data-testid="chat-group-menu-autojoin-panel"]')!;
    const head = sub.querySelector(".chat-groups__menu-head")!;
    const firstItem = sub.querySelector("[role='menuitemcheckbox']")!;
    const opener = document.querySelector('[data-testid="chat-group-menu-autojoin"]')!;
    const parse = (c: string) => c.match(/[\d.]+/g)!.map(Number);
    const lum = ([r, g, b]: number[]) => {
      const f = (v: number) => {
        const x = v / 255;
        return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const subBg = getComputedStyle(sub).backgroundColor;
    const text = head.querySelector(".chat-groups__menu-head-text")!;
    const [hi, lo] = [lum(parse(getComputedStyle(text).color)), lum(parse(subBg))].sort((a, b) => b - a);
    return {
      main: box(main),
      sub: box(sub),
      head: box(head),
      firstItemTop: box(firstItem).top,
      openerTop: box(opener).top,
      subBgAlpha: parse(subBg)[3] ?? 1,
      headText: head.textContent,
      lines: Array.from(text.children, (c) => c.textContent),
      textContrast: (hi + 0.05) / (lo + 0.05),
      flip: sub.parentElement!.hasAttribute("data-flip"),
      viewport: window.innerWidth,
    };
  });
}

test("1280px: the 自動加入 submenu hangs off its row like one menu, overlapping the main menu by 2px, with its explanation inside its own opaque surface", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-group-heading-g-ui").click({ button: "right", position: { x: 20, y: 12 } });
  await page.getByTestId("chat-group-menu-autojoin").click();
  const m = await cascade(page);

  expect(m.flip).toBe(false);
  expect(Math.round((m.sub.left - m.main.right) * 10) / 10).toBe(-2);
  expect(Math.round(m.firstItemTop - m.openerTop)).toBe(0);
  expect(m.headText).toBe("自動加入勾選的任務類型，之後指派的新外包會自動加入這個群組。");
  expect(m.lines).toEqual(["勾選的任務類型，", "之後指派的新外包會自動加入這個群組。"]);
  expect(m.subBgAlpha).toBe(1);
  expect(m.head.left).toBeGreaterThanOrEqual(m.sub.left);
  expect(m.head.right).toBeLessThanOrEqual(m.sub.right);
  expect(m.head.top).toBeGreaterThanOrEqual(m.sub.top);
  expect(m.textContrast).toBeGreaterThanOrEqual(4.5);
});

test("721px: when the 自動加入 submenu fits on neither side, the menu slides left so the cascade fits, judged by the submenu's own width", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 721, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  const heading = cmp.getByTestId("chat-group-heading-g-ui");
  const hb = (await heading.boundingBox())!;
  await heading.click({ button: "right", position: { x: hb.width - 4, y: 12 } });
  await page.getByTestId("chat-group-menu-autojoin").click();
  const m = await cascade(page);

  expect(m.flip).toBe(false);
  expect(Math.round((m.sub.left - m.main.right) * 10) / 10).toBe(-2);
  expect(Math.round(m.firstItemTop - m.openerTop)).toBe(0);
  expect(m.sub.right).toBeLessThanOrEqual(m.viewport - 8);
  expect(m.main.left).toBeGreaterThanOrEqual(8);
});

test("1280px: opened near the window's right edge, the 自動加入 submenu flips to the left of the main menu, overlapping it by 2px", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  const heading = cmp.getByTestId("chat-group-heading-g-ui");
  const hb = (await heading.boundingBox())!;
  await heading.evaluate(
    (el, y) =>
      el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, button: 2, clientX: 1240, clientY: y })),
    hb.y + 12,
  );
  await page.getByTestId("chat-group-menu-autojoin").click();
  const m = await cascade(page);

  expect(m.flip).toBe(true);
  expect(Math.round((m.main.left - m.sub.right) * 10) / 10).toBe(-2);
  expect(Math.round(m.firstItemTop - m.openerTop)).toBe(0);
  expect(m.main.right).toBeLessThanOrEqual(m.viewport - 8);
  expect(m.sub.left).toBeGreaterThanOrEqual(8);
});

for (const [width, reach] of [
  [1280, 15],
  [390, 21],
] as const) {
  test(`${width}px: each group heading ends in a 「⋯」 at the list's right edge, a collapsed group's unread pill sits 11px left of it, and the ⋯ answers taps ${reach * 2 + 2}px wide`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(
      <OfficeChatGroupsStory mobile={width < 720} initialGroups={sampleGroups} initialCollapsed={{ "g-lead": true }} />,
    );
    const m = await cmp.getByTestId("chat-group-more-g-lead").evaluate((more, reach) => {
      const r = more.getBoundingClientRect();
      const list = more.closest(".office__members-list")!.getBoundingClientRect();
      const pill = more.parentElement!.querySelector(".member-card__unread")!.getBoundingClientRect();
      const cx = r.left + r.width / 2;
      const cy = r.top + r.height / 2;
      const hits = [
        [cx - reach, cy],
        [cx + reach, cy],
        [cx, cy - reach],
        [cx, cy + reach],
      ].map(([x, y]) => document.elementFromPoint(x, y)?.closest(".chat-groups__more") === more);
      return {
        rightEdge: Math.round(list.right - r.right),
        pillGap: Math.round(r.left - pill.right),
        pillText: more.parentElement!.querySelector(".member-card__unread")!.textContent,
        hits,
      };
    }, reach);
    expect(m).toEqual({ rightEdge: 0, pillGap: 11, pillText: "3", hits: [true, true, true, true] });
  });
}

test("1280px: holding the 「⋯」 lifts nothing and its click opens the group menu with the group still open; Tab reaches it and Escape hands focus back", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const more = cmp.getByTestId("chat-group-more-g-eng");
  const b = (await more.boundingBox())!;
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.up();
  await expect(page.getByRole("menu", { name: "工程" })).toBeVisible();
  await expect(cmp.getByTestId("chat-group-heading-g-eng")).toHaveAttribute("aria-expanded", "true");
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Dora"]);
  await page.keyboard.press("Escape");
  await expect(more).toBeFocused();

  await cmp.getByTestId("chat-group-heading-g-lead").focus();
  await page.keyboard.press("Tab");
  await expect(cmp.getByTestId("chat-group-more-g-lead")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("menu", { name: "核心" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(cmp.getByTestId("chat-group-more-g-lead")).toBeFocused();
});

test("390px: tapping a group heading's 「⋯」 opens the group's options as the bottom sheet", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-group-more-g-eng").click();
  const sheet = page.getByTestId("chat-group-menu-sheet");
  await expect(sheet).toBeVisible();
  expect(await sheet.getByRole("menuitem").allTextContents()).toEqual(["重新命名", "刪除群組"]);
});
