// Office sidebar chat groups, measured in real Chromium against the real
// office CSS: the layout at phone widths (no sideways scroll, pills inside
// their headings, menus inside the window), and the gestures jsdom cannot
// drive (mouse drag, touch hold and drag, a touch hold released in place
// leaving everything as it was, keyboard drag).
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { OfficeChatGroupsStory } from "./stories/OfficeChatGroupsStory";
import { sampleGroups } from "./stories/chatGroupsFixtures";

// Past ChatGroupList's 250ms touch hold; the lift is read right after it
// without retrying, so a longer hold fails the touch test.
const TOUCH_HOLD_MS = 380;

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

// The page outlives a test, and so would its touch emulation.
test.afterEach(async ({ page }) => {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
});

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
  test(`${width}px: with groups, a collapsed group's pill, a menu and the new-group entry all fit with no sideways scroll`, async ({
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
      await cmp.getByTestId("chat-group-more-g-eng").click();
      await expect(page.getByTestId("chat-group-menu-sheet")).toBeVisible();
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

test("1280px mouse: a press that moves 8px lifts an ungrouped member at once; over a row of another group that group's rows make room for it; Escape puts everything back unsaved, and dropping it there saves it once, without opening a chat; a press held still opens the member's chat", async ({
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
    await page.mouse.move(from.x, from.y + 8, { steps: 2 });
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

  const beto = (await row(cmp, "Beto").boundingBox())!;
  await page.mouse.move(beto.x + beto.width * 0.6, beto.y + beto.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(600);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  await page.mouse.up();
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "beto");
  expect(await sectionNames(cmp, "g-eng")).toEqual(["Beto", "Evan", "Dora"]);
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
  await page.mouse.move(from.x, to.y + to.height / 2, { steps: 14 });
  await page.waitForTimeout(300);
  await expect(cmp.getByTestId("chat-group-section-g-t2")).toHaveClass(/chat-groups__section--over/);
  expect(await sectionNames(cmp, "g-t2")).toEqual(["Evan"]);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await sectionNames(cmp, "g-t2")).toEqual(["Evan"]);
  expect(await writes(cmp)).toBe("1");
});

test("a press-and-drag on a group heading lifts the group, and dropping it above the first reorders the groups", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const h = (await cmp.getByTestId("chat-group-heading-g-eng").boundingBox())!;
  const top = (await cmp.getByTestId("chat-group-heading-g-lead").boundingBox())!;
  await page.mouse.move(h.x + 60, h.y + h.height / 2);
  await page.mouse.down();
  await page.mouse.move(h.x + 60, top.y - 12, { steps: 12 });
  await page.waitForTimeout(300);
  await page.mouse.up();
  await page.waitForTimeout(400);
  expect(await headings(cmp)).toEqual(["工程 · 2", "核心 · 2", "未分組 · 2"]);
  expect(await sectionNames(cmp, "g-lead")).toEqual(["Mira", "Clara"]);
});

test("390px touch: a 250ms hold lifts a heading or a row and lifting the finger in place leaves the order and folding as they were with no menu; a finger that moves before the hold ends lifts nothing; a hold then a drag moves the member", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const state = async () => ({
    headings: await headings(cmp),
    expanded: await cmp.locator(".chat-groups__heading").evaluateAll((hs) => hs.map((h) => h.getAttribute("aria-expanded"))),
    eng: await sectionNames(cmp, "g-eng"),
    ungrouped: await sectionNames(cmp, "ungrouped"),
    menus: await page.locator("[role='menu'], [role='dialog']").count(),
  });
  const untouched = {
    headings: ["核心 · 2", "工程 · 2", "未分組 · 2"],
    expanded: ["true", "true", "true"],
    eng: ["Beto", "Dora"],
    ungrouped: ["Evan", "EvaRhapsodyInboxForwarderStandbyRelay"],
    menus: 0,
  };

  const head = await grip(cmp.getByTestId("chat-group-heading-g-eng"));
  await t("touchStart", head.x, head.y - 6);
  await page.waitForTimeout(TOUCH_HOLD_MS);
  expect(await cmp.getByTestId("chat-groups-lifted").allTextContents()).toEqual(["工程 · 2"]);
  await t("touchEnd");
  await page.waitForTimeout(300);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  expect(await state()).toEqual(untouched);

  const from = await grip(row(cmp, "Evan"));
  await t("touchStart", from.x, from.y);
  await page.waitForTimeout(TOUCH_HOLD_MS);
  expect(await cmp.getByTestId("chat-groups-lifted").count()).toBe(1);
  await t("touchEnd");
  await page.waitForTimeout(300);
  expect(await state()).toEqual(untouched);

  await t("touchStart", from.x, from.y);
  for (let i = 1; i <= 4; i++) await t("touchMove", from.x, from.y - 20 * i);
  await page.waitForTimeout(TOUCH_HOLD_MS);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
  await t("touchEnd");
  await page.waitForTimeout(300);
  expect(await state()).toEqual(untouched);

  const to = await grip(row(cmp, "Dora"));
  const before = await tops(cmp, ["Beto", "Dora"]);
  await t("touchStart", from.x, from.y);
  await page.waitForTimeout(TOUCH_HOLD_MS);
  for (let i = 1; i <= 12; i++) await t("touchMove", from.x, from.y + ((to.y - 6 - from.y) * i) / 12);
  await page.waitForTimeout(300);
  const during = await tops(cmp, ["Beto", "Dora"]);
  expect({ beto: Math.round(during.Beto - before.Beto), dora: Math.round(during.Dora - before.Dora) }).toEqual({
    beto: 0,
    dora: 77,
  });
  await t("touchEnd");
  await page.waitForTimeout(400);
  expect(await state()).toEqual({
    ...untouched,
    headings: ["核心 · 2", "工程 · 3", "未分組 · 1"],
    eng: ["Beto", "Evan", "Dora"],
    ungrouped: ["EvaRhapsodyInboxForwarderStandbyRelay"],
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

test("1280px: while 篩選 is on, a press-and-drag lifts nothing and the matching groups stay as they are, but a heading's 「⋯」 still opens its menu", async ({
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

test("375px: 篩選 opens as the bottom sheet with its search field first but not focused, and ticking an item there filters the list behind it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-filter").click();
  const sheet = page.getByTestId("chat-filter-menu-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet).toBeFocused();
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

const sheetBoxes = (page: Page) =>
  page.evaluate(() => {
    const box = (sel: string) => {
      const el = document.querySelector(sel);
      if (!el) return null;
      const b = el.getBoundingClientRect();
      return { top: Math.round(b.top), bottom: Math.round(b.bottom) };
    };
    return {
      sheet: box(".chat-groups__sheet"),
      done: box("[data-testid$='-done']"),
      clear: box("[data-testid='chat-filter-menu-clear']"),
      bodyScrolled: document.querySelector(".chat-groups__sheet-body")?.scrollTop ?? null,
    };
  });

for (const [height, sheetTop, bodyEnd] of [
  [667, 200, 129],
  [812, 244, 27],
] as const) {
  test(`375×${height} touch: the 篩選 sheet stops at 70% of the window (top ${sheetTop}) under a dimmed strip, with 清除 and 完成 in its header 12px down, staying there while the list scrolls inside the sheet; 完成 closes it, and a tap on the strip over a heading only closes it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 375, height });
    const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
    const t = await touch(page);
    const tap = async (x: number, y: number) => {
      await t("touchStart", x, y);
      await t("touchEnd");
    };
    const openFilter = async () => {
      const b = (await cmp.getByTestId("chat-filter").boundingBox())!;
      await tap(b.x + b.width / 2, b.y + b.height / 2);
      await expect(page.getByTestId("chat-filter-menu-sheet")).toBeVisible();
    };

    await openFilter();
    await page.getByTestId("chat-filter-menu-unread").click();
    expect(await sheetBoxes(page)).toEqual({
      sheet: { top: sheetTop, bottom: height },
      done: { top: sheetTop + 12, bottom: sheetTop + 52 },
      clear: { top: sheetTop + 12, bottom: sheetTop + 52 },
      bodyScrolled: 0,
    });
    await expect(page.locator(".chat-groups__sheet-backdrop")).toHaveCSS("background-color", "color(srgb 0 0 0 / 0.45)");
    await page.mouse.move(187, sheetTop + 150);
    await page.mouse.wheel(0, 1000);
    await expect.poll(async () => (await sheetBoxes(page)).bodyScrolled).toBe(bodyEnd);
    expect((await sheetBoxes(page)).done).toEqual({ top: sheetTop + 12, bottom: sheetTop + 52 });
    await page.getByTestId("chat-filter-menu-clear").click();
    await expect(cmp.getByTestId("chat-filter")).toHaveText("篩選");
    await page.getByTestId("chat-filter-menu-done").click();
    await expect(page.getByTestId("chat-filter-menu-sheet")).toHaveCount(0);

    await openFilter();
    const lead = cmp.getByTestId("chat-group-heading-g-lead");
    const spot = (await lead.boundingBox())!;
    expect(spot.y + spot.height / 2).toBeLessThan(sheetTop);
    await tap(spot.x + spot.width * 0.4, spot.y + spot.height / 2);
    await expect(page.getByTestId("chat-filter-menu-sheet")).toHaveCount(0);
    await page.waitForTimeout(200);
    await expect(lead).toHaveAttribute("aria-expanded", "true");
    await tap(spot.x + spot.width * 0.4, spot.y + spot.height / 2);
    await expect(lead).toHaveAttribute("aria-expanded", "false");
  });
}

test("375×667 touch: opening 篩選 focuses the sheet, not its search field, whose text is 16px; while it is open neither the page nor the list behind it scrolls, and after 完成 both are where they were and scroll again", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.addStyleTag({ content: ".office__members-list { max-height: 300px; }" });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const scrolled = () =>
    page.evaluate(() => ({ page: window.scrollY, list: document.querySelector(".office__members-list")!.scrollTop }));
  await cmp.locator(".office__members-list").evaluate((l) => (l.scrollTop = 40));
  await page.evaluate(() => window.scrollTo(0, 60));
  expect(await scrolled()).toEqual({ page: 60, list: 40 });

  const f = (await cmp.getByTestId("chat-filter").boundingBox())!;
  await t("touchStart", f.x + f.width / 2, f.y + f.height / 2);
  await t("touchEnd");
  const sheet = page.getByTestId("chat-filter-menu-sheet");
  await expect(sheet).toBeFocused();
  await expect(page.getByTestId("chat-filter-search")).toHaveCSS("font-size", "16px");

  await page.mouse.move(180, 60);
  await page.mouse.wheel(0, 300);
  const head = (await sheet.locator(".chat-groups__sheet-title").boundingBox())!;
  await page.mouse.move(head.x + 10, head.y + 5);
  await page.mouse.wheel(0, -300);
  await t("touchStart", head.x + 10, head.y + 5);
  await t("touchMove", head.x + 10, head.y + 205);
  await t("touchEnd");
  await page.waitForTimeout(200);
  expect(await scrolled()).toEqual({ page: 60, list: 40 });

  await page.getByTestId("chat-filter-menu-done").click();
  await expect(sheet).toHaveCount(0);
  expect(await scrolled()).toEqual({ page: 60, list: 40 });
  expect(
    await page.evaluate(() => [
      document.documentElement.style.overflow,
      (document.querySelector(".office__members-list") as HTMLElement).style.overflow,
    ]),
  ).toEqual(["", ""]);
  await page.mouse.move(180, 60);
  await page.mouse.wheel(0, -300);
  await expect.poll(scrolled).toEqual({ page: 60, list: 0 });
});

test("375×667 with the on-screen part of the page cut to 222px (as by a keyboard) and panned down 50px: with the search field focused, the 篩選 sheet fits that part and its 完成 stays inside it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 667 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-filter").click();
  await page.getByTestId("chat-filter-search").focus();
  await page.getByTestId("chat-filter-search").fill("後端");
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setPageScaleFactor", { pageScaleFactor: 3 });
  const onScreen = () =>
    page.evaluate(() => {
      const vv = window.visualViewport!;
      const box = (sel: string) => {
        const b = document.querySelector(sel)!.getBoundingClientRect();
        return { top: Math.round(b.top - vv.offsetTop), bottom: Math.round(b.bottom - vv.offsetTop) };
      };
      return {
        pan: Math.round(vv.offsetTop),
        height: Math.round(vv.height),
        sheet: box(".chat-groups__sheet"),
        done: box("[data-testid='chat-filter-menu-done']"),
        clear: box("[data-testid='chat-filter-menu-clear']"),
      };
    });
  // The sheet follows visualViewport's resize and scroll events, which
  // Chromium fires at its next frame.
  const fitted = {
    height: 222,
    sheet: { top: 67, bottom: 222 },
    done: { top: 79, bottom: 119 },
    clear: { top: 79, bottom: 119 },
  };
  await expect.poll(async () => ({ ...(await onScreen()), pan: undefined })).toEqual({ ...fitted, pan: undefined });
  const before = (await onScreen()).pan;
  await page.mouse.move(60, 20);
  await page.mouse.wheel(0, 150);
  await expect.poll(onScreen).toEqual({ ...fitted, pan: before + 50 });
  await cdp.send("Emulation.setPageScaleFactor", { pageScaleFactor: 1 });
  await page.getByTestId("chat-filter-menu-done").click();
  await expect(page.getByTestId("chat-filter-menu-sheet")).toHaveCount(0);
  expect(await headings(cmp)).toEqual(["工程 · 1"]);
});

test("375×667: the 「⋯」 sheet and the 自動加入 sheet each keep 完成 in their header 12px below their top, and 完成 closes them", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 667 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} initialTab="outsource" />);
  await cmp.getByTestId("chat-group-more-g-ui").click();
  const menu = await sheetBoxes(page);
  expect(menu.done!.top - menu.sheet!.top).toBe(12);
  expect(menu.sheet!.bottom).toBe(667);
  await page.getByTestId("chat-group-menu-done").click();
  await expect(page.getByTestId("chat-group-menu-sheet")).toHaveCount(0);

  await cmp.getByTestId("chat-group-more-g-ui").click();
  await page.getByTestId("chat-group-menu-autojoin").click();
  const join = await sheetBoxes(page);
  expect(join.done!.top - join.sheet!.top).toBe(12);
  expect(join.sheet!.bottom).toBe(667);
  await page.getByTestId("chat-group-autojoin-done").click();
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(0);
});

test("375×667 touch: 新增群組 and 重新命名 focus a 16px name box on a 44px row, a heading's height; with the list scrolled, 新增群組 scrolls only the list to show its box and the page stays put", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.addStyleTag({ content: ".office__members-list { max-height: 300px; }" });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  const t = await touch(page);
  const tap = async (l: Locator) => {
    const b = (await l.boundingBox())!;
    await t("touchStart", b.x + b.width / 2, b.y + b.height / 2);
    await t("touchEnd");
  };
  const entry = (testId: string) =>
    page.evaluate((testId) => {
      const box = document.querySelector(`[data-testid='${testId}']`)!;
      const input = box.querySelector("input")!;
      const list = document.querySelector(".office__members-list")!;
      return {
        focused: document.activeElement === input,
        font: getComputedStyle(input).fontSize,
        row: Math.round(box.getBoundingClientRect().height),
        heading: Math.round(document.querySelector(".chat-groups__heading-row")!.getBoundingClientRect().height),
        belowListTop: Math.round(box.getBoundingClientRect().top - list.getBoundingClientRect().top),
        page: window.scrollY,
        list: list.scrollTop,
      };
    }, testId);
  await cmp.locator(".office__members-list").evaluate((l) => (l.scrollTop = 100));

  await tap(cmp.getByTestId("chat-group-new"));
  expect(await entry("chat-group-new-entry")).toEqual({
    focused: true,
    font: "16px",
    row: 44,
    heading: 44,
    belowListTop: 0,
    page: 0,
    list: 0,
  });
  await page.keyboard.press("Escape");

  await tap(cmp.getByTestId("chat-group-more-g-eng"));
  await page.getByTestId("chat-group-menu-rename").click();
  expect(await entry("chat-group-rename")).toEqual({
    focused: true,
    font: "16px",
    row: 44,
    heading: 44,
    belowListTop: 212,
    page: 0,
    list: 0,
  });
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

test("1280px: in the 篩選 dropdown Tab moves from the search field through the options, and Tab past the last one closes it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-filter").click();
  await expect(page.getByTestId("chat-filter-search")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByTestId("chat-filter-menu-unread")).toBeFocused();
  await page.getByTestId("chat-filter-menu-idle").focus();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("dialog", { name: "篩選" })).toHaveCount(0);
});

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
  expect(await cmp.locator(".outsource-row__codename").allTextContents()).toEqual(["O-153"]);
});

test("1280px English: a grouped outsource row names the worker by its codename alone", async ({
  mount,
  page,
}) => {
  await page.evaluate(() => localStorage.setItem("oc.language", "en"));
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
  const name = cmp.getByTestId("outsource-row-ow-a18f").locator(".outsource-row__codename");
  await expect(name).toHaveText("O-161");
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

for (const [height, scrolls] of [
  [900, false],
  [240, true],
] as const) {
  test(`1280×${height}: the 自動加入 dialog stops at 70% of the window's height with its title and 完成 in view${scrolls ? ", its task types scrolling between them" : ", its task types without scrolling"}`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height });
    const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} initialTab="outsource" />);
    await cmp.getByTestId("chat-group-more-g-ui").click();
    await page.getByTestId("chat-group-menu-autojoin").click();
    const m = await page.getByTestId("chat-group-autojoin").evaluate((box) => {
      const b = box.getBoundingClientRect();
      const within = (el: Element) => {
        const r = el.getBoundingClientRect();
        return r.top >= b.top && r.bottom <= b.bottom;
      };
      const list = box.querySelector(".chat-groups__autojoin-list")!;
      return {
        capped: b.height <= window.innerHeight * 0.7 + 0.5,
        title: within(box.querySelector("h2")!),
        done: within(box.querySelector('[data-testid="chat-group-autojoin-done"]')!),
        scrolls: list.scrollHeight > list.clientHeight,
      };
    });
    expect(m).toEqual({ capped: true, title: true, done: true, scrolls });
  });
}

test("1280px: 自動加入… opens a 400px dialog centred in the window, titled with the group, its sentence and the task types readable on an opaque card, 「目前在」 at each row's right end; a tick with ungrouped workers asks over it, and a click outside (over a worker row) only closes the question, then the dialog back to the 「⋯」, opening no chat", async ({
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
  // A click outside lands over a worker row: it only answers 都不搬.
  const rowSpot = (await cmp.getByTestId("outsource-row-ow-71d0").boundingBox())!;
  await page.mouse.click(rowSpot.x + rowSpot.width * 0.6, rowSpot.y + rowSpot.height / 2);
  await expect(ask).toHaveCount(0);
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(1);
  await expect(page.getByTestId("chat-group-autojoin-tm-review")).toHaveAttribute("aria-checked", "true");
  await expect(page.getByTestId("chat-group-autojoin-tm-review")).toBeFocused();

  await page.mouse.click(rowSpot.x + rowSpot.width * 0.6, rowSpot.y + rowSpot.height / 2);
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(0);
  await page.waitForTimeout(200);
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
  await expect(more).toBeFocused();
});

test("390px: 自動加入… opens as a bottom sheet the width of the window with 44px rows, its move-in question has 44px rows too, 完成 closes it, and a tap outside (over a worker row) only closes it", async ({
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

  await cmp.getByTestId("chat-group-more-g-ui").click();
  await page.getByTestId("chat-group-menu-autojoin").click();
  const t = await touch(page);
  const rowSpot = (await cmp.getByTestId("outsource-row-ow-a18f").boundingBox())!;
  await t("touchStart", rowSpot.x + rowSpot.width * 0.6, rowSpot.y + rowSpot.height / 2);
  await t("touchEnd");
  await expect(page.getByTestId("chat-group-autojoin")).toHaveCount(0);
  await page.waitForTimeout(200);
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
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

test("1280px: a press held on the 「⋯」 lifts nothing and its release opens the group menu with the group still open; Tab reaches it and Escape hands focus back", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory initialGroups={sampleGroups} />);
  const more = cmp.getByTestId("chat-group-more-g-eng");
  const b = (await more.boundingBox())!;
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(600);
  await expect(cmp.getByTestId("chat-groups-lifted")).toHaveCount(0);
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

for (const [width, by] of [
  [1280, "mouse"],
  [390, "touch"],
] as const) {
  test(`${width}px ${by}: with a 「⋯」 menu open, a ${by === "mouse" ? "click" : "tap"} on a member row or on a heading outside it only closes the menu: no chat opens and nothing folds`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<OfficeChatGroupsStory mobile={width < 720} initialGroups={sampleGroups} />);
    const t = by === "touch" ? await touch(page) : null;
    const press = async (x: number, y: number) => {
      if (t) {
        await t("touchStart", x, y);
        await t("touchEnd");
      } else {
        await page.mouse.click(x, y);
      }
    };
    const menu = page.locator(".chat-groups__menu, .chat-groups__sheet");
    const open = async () => {
      const b = (await cmp.getByTestId("chat-group-more-g-eng").boundingBox())!;
      await press(b.x + b.width / 2, b.y + b.height / 2);
      await expect(menu).toHaveCount(1);
    };
    // Clear of the avatar (which opens the member's details instead).
    const center = async (l: ReturnType<typeof row>) => {
      const b = (await l.boundingBox())!;
      return [b.x + b.width * 0.6, b.y + b.height / 2] as const;
    };

    await open();
    await press(...(await center(row(cmp, "Clara"))));
    await expect(menu).toHaveCount(0);
    await page.waitForTimeout(200);
    await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");

    await open();
    await press(...(await center(cmp.getByTestId("chat-group-heading-g-lead"))));
    await expect(menu).toHaveCount(0);
    await page.waitForTimeout(200);
    await expect(cmp.getByTestId("chat-group-heading-g-lead")).toHaveAttribute("aria-expanded", "true");

    await press(...(await center(row(cmp, "Clara"))));
    await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "clara");
  });
}

test("390px: tapping a group heading's 「⋯」 opens the group's options as the bottom sheet", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatGroupsStory mobile initialGroups={sampleGroups} />);
  await cmp.getByTestId("chat-group-more-g-eng").click();
  const sheet = page.getByTestId("chat-group-menu-sheet");
  await expect(sheet).toBeVisible();
  expect(await sheet.getByRole("menuitem").allTextContents()).toEqual(["重新命名", "刪除群組"]);
});
