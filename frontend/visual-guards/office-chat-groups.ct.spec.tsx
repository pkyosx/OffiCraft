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

test("an expanded empty group shows a 44px dashed 「把成員拖曳到這裡」 box where its first row would be, with no guide line; a member dragged over it takes its place in the lit group and dropping it there saves once; folded, the group is its heading alone", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const groups = [...sampleGroups, { id: "g-t2", tab: "staff" as const, name: "T2", memberIds: [], taskTypeKeys: [] }];
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={groups} />);
  const drop = cmp.getByTestId("chat-group-drop-g-t2");
  const m = await drop.evaluate((box) => {
    const r = (el: Element) => el.getBoundingClientRect();
    const list = r(box.closest(".office__members-list")!);
    const section = box.parentElement!;
    const cs = getComputedStyle(box);
    const row = r(document.querySelector('[data-testid="chat-group-section-g-eng"] .member-card')!);
    return {
      text: box.textContent,
      left: Math.round(r(box).left - list.left),
      rowLeft: Math.round(row.left - list.left),
      height: Math.round(r(box).height),
      border: `${cs.borderTopWidth} ${cs.borderTopStyle}`,
      guideLine: getComputedStyle(section).borderLeftColor,
    };
  });
  expect(m).toEqual({
    text: "把成員拖曳到這裡",
    left: 16,
    rowLeft: 16,
    height: 44,
    border: "1px dashed",
    guideLine: "rgba(0, 0, 0, 0)",
  });

  await cmp.getByTestId("chat-group-heading-g-t2").click();
  await expect(cmp.getByTestId("chat-group-section-g-t2")).toHaveCount(0);
  await expect(cmp.getByTestId("chat-group-heading-g-t2")).toHaveText("T2 · 0");
  await cmp.getByTestId("chat-group-heading-g-t2").click();

  const from = await grip(row(cmp, "Evan"));
  const to = (await drop.boundingBox())!;
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.move(from.x, to.y + to.height / 2, { steps: 14 });
  await page.waitForTimeout(300);
  await expect(cmp.getByTestId("chat-group-section-g-t2")).toHaveClass(/chat-groups__section--over/);
  expect(await sectionNames(cmp, "g-t2")).toEqual(["Evan"]);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-t2")).toEqual(["Evan"]);
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

for (const [width, reach] of [
  [1280, 13],
  [375, 21],
] as const) {
  test(`${width}px: the toolbar is a frameless 32px row between the tabs and the first heading (${width < 720 ? 44 : 36}px apart), 篩選's icon on the group labels' edge at its left and 全部收合 and 新增群組 at its right, each drawn 28px tall and answering taps ${reach * 2 + 2}px tall`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 812 });
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
    const cmp = await mount(<OfficeChatGroupsStory mobile={width < 720} initialGroups={sampleGroups} />);
    const m = await cmp.getByTestId("chat-groups-toolbar").evaluate((bar, reach) => {
      const r = (el: Element) => el.getBoundingClientRect();
      const box = r(bar);
      const tabs = r(document.querySelector(".office__tabs")!);
      const heading = r(document.querySelector(".chat-groups__heading-row")!);
      const buttons = Array.from(bar.querySelectorAll("button"));
      const answers = (b: Element, x: number, y: number) => document.elementFromPoint(x, y)?.closest("button") === b;
      return {
        tabsToHeading: Math.round(heading.top - tabs.bottom),
        height: Math.round(box.height),
        controls: buttons.map((b) => {
          const c = r(b);
          const cx = c.left + c.width / 2;
          const cy = c.top + c.height / 2;
          const icon = b.getAttribute("aria-label") !== null;
          return {
            name: b.getAttribute("aria-label") ?? b.textContent,
            drawn: icon ? `${Math.round(c.width)}×${Math.round(c.height)}` : `h${Math.round(c.height)}`,
            glyph: Math.round(r(b.querySelector("svg")!).width),
            oneRow: Math.round(cy) === Math.round(box.top + box.height / 2),
            tallTaps: answers(b, cx, cy - reach) && answers(b, cx, cy + reach),
            wideTaps: !icon || (answers(b, cx - reach, cy) && answers(b, cx + reach, cy)),
          };
        }),
        iconPitch: Math.round(r(buttons[2]).left - r(buttons[1]).left),
        filterIconToLabel: Math.round(
          r(bar.querySelector('[data-testid="chat-filter"] svg')!).left -
            r(document.querySelector(".chat-groups__heading-label")!).left,
        ),
        newRightToList: Math.round(
          r(document.querySelector(".office__members-list")!).right -
            r(bar.querySelector('[data-testid="chat-group-new"]')!).right,
        ),
        frame: [getComputedStyle(bar).borderTopWidth, getComputedStyle(bar).backgroundColor],
        pageOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      };
    }, reach);
    const control = (name: string, drawn: string, glyph: number) => ({
      name,
      drawn,
      glyph,
      oneRow: true,
      tallTaps: true,
      wideTaps: true,
    });
    expect(m).toEqual({
      tabsToHeading: width < 720 ? 44 : 36,
      height: 32,
      controls: [control("篩選", "h28", 14), control("全部收合", "28×28", 16), control("新增群組", "28×28", 16)],
      iconPitch: width < 720 ? 44 : 30,
      filterIconToLabel: 0,
      newRightToList: 0,
      frame: ["0px", "rgba(0, 0, 0, 0)"],
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
  expect(await sheet.locator(".chat-groups__menu-item").allTextContents()).toEqual([
    "有未讀2",
    "線上3",
    "喚醒中1",
    "停止中0",
    "已停止0",
    "離線2",
    "工作中0",
    "等待中0",
    "閒置0",
  ]);
  await sheet.getByTestId("chat-filter-search").fill("後端");
  expect(await headings(cmp)).toEqual(["工程 · 1"]);
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto"]);
  await sheet.getByTestId("chat-filter-search").fill("");
  await sheet.getByTestId("chat-filter-menu-online-awake").click();
  expect(await headings(cmp)).toEqual(["核心 · 2", "工程 · 1"]);
  await expect(cmp.getByTestId("chat-filter")).toHaveText("篩選 · 1");
});

for (const [height, scrolls] of [
  [800, false],
  [360, true],
] as const) {
  test(`1280×${height}: the 篩選 dropdown stays inside the window${scrolls ? ", scrolling inside itself" : " without scrolling"}`, async ({ mount, page }) => {
    await page.setViewportSize({ width: 1280, height });
    const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
    await cmp.getByTestId("chat-filter").click();
    const m = await page.getByTestId("chat-filter-menu-panel").evaluate((panel) => {
      const r = panel.getBoundingClientRect();
      return {
        inside: r.top >= 8 && r.bottom <= window.innerHeight - 8,
        scrolls: panel.scrollHeight > panel.clientHeight,
        sections: Array.from(panel.querySelectorAll(".chat-groups__menu-section"), (h) => h.textContent),
      };
    });
    expect(m).toEqual({ inside: true, scrolls, sections: ["未讀", "連線", "狀態"] });
  });
}

test("1280px: on the 外包 tab 篩選 reads each worker's own unread, presence dot and task type", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-filter").click();
  const menu = page.getByRole("menu", { name: "篩選" });
  expect(await menu.locator(".chat-groups__menu-item").allTextContents()).toEqual([
    "有未讀1",
    "線上3",
    "喚醒中1",
    "停止中0",
    "已停止0",
    "離線1",
    "工作中0",
    "等待中0",
    "閒置0",
  ]);
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

/** The open 自動加入 dialog: its box, the window, its rows and how its words read. */
async function autoJoinDialog(page: Page) {
  return page.getByTestId("chat-group-autojoin").evaluate((box) => {
    const r = (el: Element) => el.getBoundingClientRect();
    const parse = (c: string) => c.match(/[\d.]+/g)!.map(Number);
    const lum = ([red, g, b]: number[]) => {
      const f = (v: number) => {
        const x = v / 255;
        return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * f(red) + 0.7152 * f(g) + 0.0722 * f(b);
    };
    const bg = parse(getComputedStyle(box).backgroundColor);
    const ratio = (el: Element) => {
      const [hi, lo] = [lum(parse(getComputedStyle(el).color)), lum(bg)].sort((a, b) => b - a);
      return (hi + 0.05) / (lo + 0.05);
    };
    const b = r(box);
    const hint = box.querySelector(".chat-groups__dialog-hint")!;
    const owner = box.querySelector(".chat-groups__autojoin-owner")!;
    const ownerRow = owner.closest("button")!;
    return {
      title: box.querySelector("h2")!.textContent,
      hint: hint.textContent,
      rows: Array.from(box.querySelectorAll("[role='checkbox']"), (row) => [
        row.querySelector(".chat-groups__autojoin-label")!.textContent,
        row.querySelector(".chat-groups__autojoin-owner")?.textContent ?? "",
        row.getAttribute("aria-checked"),
      ]),
      rowHeights: Array.from(box.querySelectorAll("[role='checkbox']"), (row) => Math.round(r(row).height)),
      ownerToRowEnd: Math.round(r(ownerRow).right - r(owner).right),
      done: box.querySelector('[data-testid="chat-group-autojoin-done"]')!.textContent,
      opaque: (bg[3] ?? 1) === 1,
      hintReadable: ratio(hint) >= 4.5,
      ownerReadable: ratio(owner) >= 4.5,
      box: { left: Math.round(b.left), top: Math.round(b.top), right: Math.round(b.right), bottom: Math.round(b.bottom) },
      window: { width: window.innerWidth, height: window.innerHeight },
    };
  });
}

const AUTO_JOIN_ROWS = [
  ["OffiCraft · 前端 UI", "", "true"],
  ["OffiCraft · PR 審查（含外部 PR 接待）", "", "false"],
  ["Eva GF VE Tickets", "目前在「營運」", "false"],
  ["Long-term Context · Lessons Refine", "", "false"],
];

test("1280px: 自動加入… opens a 400px dialog centred in the window, titled with the group, its sentence and the task types readable on an opaque card, 「目前在」 at each row's right end; a tick with ungrouped workers asks over it, and a click outside closes it back to the 「⋯」", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  const more = cmp.getByTestId("chat-group-more-g-ui");
  await more.click();
  await page.getByTestId("chat-group-menu-autojoin").click();
  const m = await autoJoinDialog(page);
  expect(m).toEqual({
    title: "自動加入 · 前端外包",
    hint: "勾選的任務類型，該類型的外包會自動加入這個群組。",
    rows: AUTO_JOIN_ROWS,
    rowHeights: [32, 32, 32, 32],
    ownerToRowEnd: 10,
    done: "完成",
    opaque: true,
    hintReadable: true,
    ownerReadable: true,
    box: { left: 440, top: m.box.top, right: 840, bottom: m.box.bottom },
    window: { width: 1280, height: 900 },
  });
  expect(Math.round((m.box.top + m.box.bottom) / 2)).toBe(450);
  await expect(page.getByTestId("chat-group-autojoin-tm-fe")).toBeFocused();

  await page.getByTestId("chat-group-autojoin-tm-review").click();
  const ask = page.getByTestId("chat-groups-bring-in");
  await expect(ask).toBeVisible();
  const top = await ask.evaluate((el) => {
    const b = el.getBoundingClientRect();
    return document.elementFromPoint(b.left + b.width / 2, b.top + 10)?.closest("[role='dialog']")?.getAttribute("data-testid");
  });
  expect(top).toBe("chat-groups-bring-in");
  expect(
    await ask.evaluate((el) => ({
      title: el.querySelector("h2")!.textContent,
      rows: Array.from(el.querySelectorAll("[role='checkbox']"), (row) => {
        const place = row.querySelector(".chat-groups__autojoin-owner")!;
        return [
          row.querySelector(".chat-groups__autojoin-label")!.textContent,
          place.textContent,
          row.getAttribute("aria-checked"),
          Math.round(row.getBoundingClientRect().right - place.getBoundingClientRect().right),
        ];
      }),
    })),
  ).toEqual({
    title: "要把目前的「OffiCraft · PR 審查（含外部 PR 接待）」外包搬進「前端外包」嗎？",
    rows: [
      ["外包 · O-158", "未分組", "true", 10],
      ["外包 · O-154", "未分組", "true", 10],
    ],
  });
  await page.keyboard.press("Escape");
  await expect(ask).toHaveCount(0);
  await expect(page.getByTestId("chat-group-autojoin-tm-review")).toHaveAttribute("aria-checked", "true");
  await expect(page.getByTestId("chat-group-autojoin-tm-review")).toBeFocused();

  await page.mouse.click(20, 20);
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(0);
  await expect(more).toBeFocused();
});

test("390px: 自動加入… opens as a bottom sheet the width of the window with 44px rows, its move-in question has 44px rows too, and 完成 closes it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-group-more-g-ui").click();
  await page.getByTestId("chat-group-menu-autojoin").click();
  const m = await autoJoinDialog(page);
  expect({ rows: m.rows, rowHeights: m.rowHeights, left: m.box.left, right: m.box.right, bottom: m.box.bottom }).toEqual({
    rows: AUTO_JOIN_ROWS,
    rowHeights: [44, 44, 44, 44],
    left: 0,
    right: 390,
    bottom: 900,
  });
  await page.getByTestId("chat-group-autojoin-tm-review").click();
  expect(
    await page
      .getByTestId("chat-groups-bring-in")
      .locator("[role='checkbox']")
      .evaluateAll((rows) => rows.map((r) => Math.round(r.getBoundingClientRect().height))),
  ).toEqual([44, 44]);
  await page.getByTestId("chat-groups-bring-in-none").click();
  await page.getByTestId("chat-group-autojoin-done").click();
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(0);
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
