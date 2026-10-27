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

const tops = async (cmp: Locator, names: string[]) => {
  const out: Record<string, number> = {};
  for (const n of names) out[n] = (await row(cmp, n).boundingBox())!.y;
  return out;
};
const writes = (cmp: Locator) => cmp.locator("aside.office__members").getAttribute("data-writes");

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
      await expect(page.getByTestId("chat-member-menu-float")).toBeVisible();
    } else {
      await row(cmp, "Evan").focus();
      await page.keyboard.press("ContextMenu");
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
  test(`${theme}: an expanded group's rows sit 9px right of a 1px guide line 6px in, running from under its heading to its last row; a collapsed group is its bold heading with the summed unread`, async ({
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
          lineToRows: Array.from(rows, (row) => Math.round(row.getBoundingClientRect().left - box.left - 1)),
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
        indent: 6,
        lineWidth: "1px",
        lineStyle: "solid",
        startsBelowHeading: true,
        topAtFirstRow: 0,
        bottomAtLastRow: 0,
        lineToRows: [9, 9],
      },
      {
        group: "chat-group-section-ungrouped",
        indent: 6,
        lineWidth: "1px",
        lineStyle: "solid",
        startsBelowHeading: true,
        topAtFirstRow: 0,
        bottomAtLastRow: 0,
        lineToRows: [9, 9],
      },
    ]);
    for (const sec of m.sections) {
      expect(sec.contrast, `${sec.group} guide line contrast`).toBeGreaterThanOrEqual(1.4);
    }
  });
}

test("a long press lifts an ungrouped member; over a row of another group that group's rows make room for it; Escape puts everything back unsaved, and dropping it there saves it once, without opening a chat", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const from = await grip(row(cmp, "Evan"));
  const to = await grip(row(cmp, "Dora"));
  const before = await tops(cmp, ["Beto", "Dora"]);
  const lift = async () => {
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.waitForTimeout(HOLD_MS);
    await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(1);
    await page.mouse.move(from.x, to.y - 6, { steps: 12 });
    await page.waitForTimeout(300);
  };

  await lift();
  const during = await tops(cmp, ["Beto", "Dora"]);
  expect({ beto: Math.round(during.Beto - before.Beto), dora: Math.round(during.Dora - before.Dora) }).toEqual({
    beto: 0,
    dora: Math.round(before.Dora - before.Beto),
  });
  expect(Math.round(before.Dora - before.Beto)).toBe(77);
  await page.keyboard.press("Escape");
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Dora"]);
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  expect(await writes(cmp)).toBe("0");

  await lift();
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Evan", "Dora"]);
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["EvaRhapsodyInboxForwarderStandbyRelay"]);
  expect(await writes(cmp)).toBe("1");
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
});

test("a member dragged into 未分組 is shown at its default-order place there, not under the pointer, and lands there", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const from = await grip(row(cmp, "Clara"));
  const relay = await grip(row(cmp, "EvaRhapsody"));
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.move(from.x, relay.y, { steps: 16 });
  await page.waitForTimeout(300);
  const placeholder = (await cmp.locator(".chat-groups__row--placeholder").boundingBox())!;
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["Clara", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  const evanNow = (await row(cmp, "Evan").boundingBox())!.y;
  expect(Math.round(placeholder.y - evanNow)).toBe(-77);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-lead")).toEqual(["Mira"]);
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["Clara", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  expect(await writes(cmp)).toBe("1");
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

test("390px touch: a long press opens the menu beside the lifted row while the finger is still down; lifting in place keeps it usable, moving on closes it and drags instead", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const items = (id: string) => page.getByTestId(id).locator(".chat-groups__menu-item").allTextContents();

  const head = await grip(cmp.getByTestId("chat-group-heading-g-eng"));
  await t("touchStart", head.x, head.y - 6);
  await page.waitForTimeout(HOLD_MS);
  await expect(page.getByTestId("chat-group-menu-float")).toBeVisible();
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveText("工程 · 2");
  await t("touchEnd");
  await page.waitForTimeout(300);
  await expect(page.getByTestId("chat-group-menu-float")).toBeVisible();
  expect(await items("chat-group-menu-float")).toEqual(["重新命名", "刪除群組"]);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  await expect(cmp.getByTestId("chat-group-heading-g-eng")).toHaveAttribute("aria-expanded", "true");
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Dora"]);
  const rename = (await page.getByTestId("chat-group-menu-rename").boundingBox())!;
  await t("touchStart", rename.x + 20, rename.y + rename.height / 2);
  await t("touchEnd");
  await expect(page.getByTestId("chat-group-menu-float")).toHaveCount(0);
  await expect(cmp.getByTestId("chat-group-rename").locator("input")).toHaveValue("工程");
  await page.keyboard.press("Escape");

  const from = await grip(row(cmp, "Evan"));
  const to = await grip(row(cmp, "Dora"));
  const before = await tops(cmp, ["Beto", "Dora"]);
  await t("touchStart", from.x, from.y);
  await page.waitForTimeout(HOLD_MS);
  await expect(page.getByTestId("chat-member-menu-float")).toBeVisible();
  expect(await items("chat-member-menu-float")).toEqual(["核心", "工程", "未分組", "新群組…"]);
  for (let i = 1; i <= 3; i++) await t("touchMove", from.x, from.y + ((to.y - 6 - from.y) * i) / 12);
  await expect(page.getByTestId("chat-member-menu-float")).toHaveCount(0);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(1);
  for (let i = 4; i <= 12; i++) await t("touchMove", from.x, from.y + ((to.y - 6 - from.y) * i) / 12);
  await page.waitForTimeout(300);
  const during = await tops(cmp, ["Beto", "Dora"]);
  expect({ beto: Math.round(during.Beto - before.Beto), dora: Math.round(during.Dora - before.Dora) }).toEqual({
    beto: 0,
    dora: 77,
  });
  await t("touchEnd");
  await page.waitForTimeout(400);
  await expect(page.getByTestId("chat-member-menu-float")).toHaveCount(0);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Evan", "Dora"]);
  expect(await sectionNames(cmp, "ungrouped")).toEqual(["EvaRhapsodyInboxForwarderStandbyRelay"]);
});

test("375×812 touch: the hold menu floats 8px below the lifted row, or above it when there is no room below, never over it and 12px inside the window; tapping outside closes it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const cases = [
    { what: "Mira", el: row(cmp, "Mira"), menu: "chat-member-menu-float" },
    { what: "EvaRhapsody", el: row(cmp, "EvaRhapsody"), menu: "chat-member-menu-float" },
    { what: "工程", el: cmp.getByTestId("chat-group-heading-g-eng"), menu: "chat-group-menu-float" },
  ];
  const seen: Record<string, unknown> = {};
  for (const c of cases) {
    const before = (await c.el.boundingBox())!;
    const at = await grip(c.el);
    await t("touchStart", at.x, at.y - (c.what === "工程" ? 6 : 0));
    await page.waitForTimeout(HOLD_MS);
    const panel = (await page.getByTestId(c.menu).boundingBox())!;
    const lifted = (await cmp.getByTestId("chat-groups-lifted").boundingBox())!;
    const apart =
      panel.y >= lifted.y + lifted.height ||
      panel.y + panel.height <= lifted.y ||
      panel.x >= lifted.x + lifted.width ||
      panel.x + panel.width <= lifted.x;
    const below = panel.y >= lifted.y + lifted.height;
    seen[c.what] = {
      side: below ? "below" : "above",
      apart,
      gap: Math.round(below ? panel.y - (before.y + before.height) : before.y - (panel.y + panel.height)),
      inside: panel.x >= 12 && panel.y >= 12 && panel.x + panel.width <= 363 && panel.y + panel.height <= 800,
    };
    await t("touchEnd");
    await page.waitForTimeout(200);
    await expect(page.getByTestId(c.menu)).toBeVisible();
    const tab = (await page.getByTestId("office-tab-staff").boundingBox())!;
    await t("touchStart", tab.x + 10, tab.y + 10);
    await t("touchEnd");
    await expect(page.getByTestId(c.menu)).toHaveCount(0);
  }
  expect(seen).toEqual({
    Mira: { side: "below", apart: true, gap: 8, inside: true },
    EvaRhapsody: { side: "above", apart: true, gap: 8, inside: true },
    工程: { side: "below", apart: true, gap: 8, inside: true },
  });
});

for (const [width, target] of [
  [1280, 36],
  [375, 44],
] as const) {
  test(`${width}px: the toolbar is one ${width < 720 ? 46 : 44}px strip right under the tabs, 篩選 at its left and 全部收合 and 新增群組 at its right, each answering taps ${target}px tall`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 812 });
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
    const cmp = await mount(<OfficeChatGroupsStory mobile={width < 720} initialGroups={sampleGroups} />);
    const m = await cmp.getByTestId("chat-groups-toolbar").evaluate((bar) => {
      const r = (el: Element) => el.getBoundingClientRect();
      const box = r(bar);
      const tabs = r(document.querySelector(".office__tabs")!);
      return {
        gapUnderTabs: Math.round(box.top - tabs.bottom),
        height: Math.round(box.height),
        controls: Array.from(bar.querySelectorAll("button"), (b) => ({
          name: b.getAttribute("aria-label") ?? b.textContent,
          height: Math.round(r(b).height),
          oneRow: Math.round(r(b).top + r(b).height / 2) === Math.round(box.top + box.height / 2),
          inside: r(b).left >= box.left && r(b).right <= box.right,
        })),
        filterLeft: Math.round(r(bar.querySelector('[data-testid="chat-filter"]')!).left - box.left),
        newRight: Math.round(box.right - r(bar.querySelector('[data-testid="chat-group-new"]')!).right),
        pageOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      };
    });
    const control = (name: string) => ({ name, height: target, oneRow: true, inside: true });
    expect(m).toEqual({
      gapUnderTabs: 12,
      height: width < 720 ? 46 : 44,
      controls: [control("篩選"), control("全部收合"), control("新增群組")],
      filterLeft: 5,
      newRight: 5,
      pageOver: 0,
    });
  });
}

test("1280px: while 篩選 is on, a long press lifts nothing and the matching groups stay as they are, but a heading's 「⋯」 still opens its menu", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-filter").click();
  await page.getByTestId("chat-filter-menu-unread").click();
  await page.keyboard.press("Escape");
  await expect(cmp.getByTestId("chat-filter")).toHaveText("篩選 · 1");
  expect(await headings(cmp)).toEqual(["核心 · 1", "未分組 · 1"]);

  const from = await grip(row(cmp, "Clara"));
  const to = await grip(row(cmp, "EvaRhapsody"));
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.move(from.x, to.y, { steps: 12 });
  await page.waitForTimeout(300);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  await page.mouse.up();
  await page.waitForTimeout(300);
  expect(await sectionNames(cmp, "g-lead")).toEqual(["Clara"]);
  expect(await writes(cmp)).toBe("0");

  await cmp.getByTestId("chat-group-more-g-lead").click();
  await expect(page.getByRole("menu", { name: "核心" })).toBeVisible();
});

test("375px: 篩選 opens as the bottom sheet, its search field first, and ticking an item there filters the list behind it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-filter").click();
  const sheet = page.getByTestId("chat-filter-menu-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet.getByTestId("chat-filter-search")).toBeFocused();
  expect(await sheet.locator(".chat-groups__menu-item").allTextContents()).toEqual(["有未讀2", "線上3", "工作中0"]);
  await sheet.getByTestId("chat-filter-search").fill("後端");
  expect(await headings(cmp)).toEqual(["工程 · 1"]);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto"]);
  await sheet.getByTestId("chat-filter-search").fill("");
  await sheet.getByTestId("chat-filter-menu-online").click();
  expect(await headings(cmp)).toEqual(["核心 · 2", "工程 · 1"]);
  await expect(cmp.getByTestId("chat-filter")).toHaveText("篩選 · 1");
});

test("1280px: on the 外包 tab 篩選 reads each worker's own unread, presence dot and task type", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-filter").click();
  const menu = page.getByRole("menu", { name: "篩選" });
  expect(await menu.locator(".chat-groups__menu-item").allTextContents()).toEqual(["有未讀1", "線上3", "工作中0"]);
  await page.getByTestId("chat-filter-search").fill("lessons");
  expect(await cmp.locator(".outsource-row__codename").allTextContents()).toEqual(["外包 · O-153"]);
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

// The 「⋯」 is the menu's only mouse opener; a sidebar widened here puts it
// close enough to the window's right edge to leave the submenu short of room.
const wideSidebar = (px: number) =>
  `.office__members { width: ${px}px !important; max-width: none !important; flex: none !important; }`;

test("1280px: a right-click on a group heading or a member row gets the browser's own menu: the event is not cancelled and no app menu opens", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  await page.evaluate(() => {
    (window as unknown as { seen: string[] }).seen = [];
    window.addEventListener("contextmenu", (e) => {
      (window as unknown as { seen: string[] }).seen.push(`${(e.target as Element).closest("[data-testid], .member-card")?.className.split(" ")[0]}:${e.defaultPrevented}`);
    });
  });
  await cmp.getByTestId("chat-group-heading-g-eng").click({ button: "right", position: { x: 40, y: 12 } });
  await page.keyboard.press("Escape");
  await row(cmp, "Evan").click({ button: "right", position: { x: 120, y: 20 } });
  await page.keyboard.press("Escape");
  expect(await page.evaluate(() => (window as unknown as { seen: string[] }).seen)).toEqual([
    "chat-groups__heading:false",
    "member-card:false",
  ]);
  await expect(page.getByRole("menu")).toHaveCount(0);
});

test("1280px: the 自動加入 submenu hangs off its row like one menu, overlapping the main menu by 2px, with its explanation inside its own opaque surface", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-group-more-g-ui").click();
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
  await page.addStyleTag({ content: wideSidebar(330) });
  await cmp.getByTestId("chat-group-more-g-ui").click();
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
  await page.addStyleTag({ content: wideSidebar(1150) });
  await cmp.getByTestId("chat-group-more-g-ui").click();
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
  test(`${width}px: every heading keeps a 「⋯」 slot and an unread pill slot, so 「⋯」 sits at one x with or without a pill, the pill slot is the member rows' pill slot, and no name widens or narrows with unread; each 「⋯」 answers taps ${reach * 2 + 2}px wide`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    // Touch emulation left on by an earlier test in this worker would make the
    // desktop width read as a touch screen ((hover: none)).
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
    const cmp = await mount(
      <OfficeChatGroupsStory mobile={width < 720} initialGroups={sampleGroups} initialCollapsed={{ "g-lead": true }} />,
    );
    const m = await cmp.locator(".office__members-list").evaluate((list, reach) => {
      const box = (el: Element) => el.getBoundingClientRect();
      const edge = box(list).right;
      const span = (el: Element) => `${Math.round(edge - box(el).right)}–${Math.round(edge - box(el).left)}`;
      const heading = (row: Element) => {
        const more = row.querySelector(".chat-groups__more");
        const pill = row.querySelector(".chat-groups__heading-unread");
        let hits: boolean[] | null = null;
        if (more) {
          const r = box(more);
          const cx = r.left + r.width / 2;
          const cy = r.top + r.height / 2;
          hits = [
            [cx - reach, cy],
            [cx + reach, cy],
            [cx, cy - reach],
            [cx, cy + reach],
          ].map(([x, y]) => document.elementFromPoint(x, y)?.closest(".chat-groups__more") === more);
        }
        return {
          text: row.querySelector(".chat-groups__heading-label")!.textContent,
          more: more ? span(more) : null,
          slot: span(row.querySelector(".chat-groups__pill-slot")!),
          pill: pill ? `${pill.textContent}@${span(pill)}` : null,
          hits,
        };
      };
      return {
        headings: Array.from(list.querySelectorAll(".chat-groups__heading-row"), heading),
        rows: Array.from(list.querySelectorAll(".member-card"), (row) => ({
          name: row.querySelector(".member-card__name")!.textContent,
          body: Math.round(box(row.querySelector(".member-card__body")!).width),
          slot: span(row.querySelector(".member-card__unread-slot")!),
          pill: row.querySelector(".member-card__unread")?.textContent ?? null,
        })),
      };
    }, reach);
    const all = [true, true, true, true];
    const more = width < 720 ? "53–97" : "53–85";
    expect(m.headings).toEqual([
      { text: "核心 · 2", more, slot: "13–47", pill: "3@13–31", hits: all },
      { text: "工程 · 2", more, slot: "13–47", pill: null, hits: all },
      { text: "未分組 · 2", more: null, slot: "13–47", pill: null, hits: null },
    ]);
    const body = m.rows[0].body;
    expect(m.rows).toEqual([
      { name: "Beto", body, slot: "13–47", pill: null },
      { name: "Dora", body, slot: "13–47", pill: null },
      { name: "Evan", body, slot: "13–47", pill: null },
      { name: "EvaRhapsodyInboxForwarderStandbyRelay", body, slot: "13–47", pill: "99+" },
    ]);
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
