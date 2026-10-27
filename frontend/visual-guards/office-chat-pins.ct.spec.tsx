// Office sidebar 置頂: the pin toggle after each name and the long-press reorder
// of the pinned section, measured in real Chromium against the real office CSS.
// jsdom has no layout and no pointer timing, so neither the geometry (pin inside
// its row, clear of the unread pill, no sideways scroll at phone widths) nor the
// hold-to-lift gesture can be seen by the unit suite.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { OfficeChatPinsStory } from "./stories/OfficeChatPinsStory";

// Comfortably past ChatPinList's 450ms hold.
const HOLD_MS = 650;

/** A point on the row's role line: clear of the pin button and the pill. */
async function grip(l: Locator) {
  const b = (await l.boundingBox())!;
  return { x: b.x + 80, y: b.y + b.height / 2 + 10 };
}

const names = (cmp: Locator) => cmp.locator(".member-card__name").allTextContents();

async function longPressDrag(page: Page, from: Locator, toY: (y: number) => number) {
  const c = await grip(from);
  await page.mouse.move(c.x, c.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await page.mouse.move(c.x, toY(c.y), { steps: 12 });
  await page.waitForTimeout(300);
  await page.mouse.up();
  await page.waitForTimeout(400);
}

for (const width of [1280, 390, 320]) {
  test(`${width}px: every pin sits inside its row, left of the unread pill, at least 24px, and the page never scrolls sideways`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<OfficeChatPinsStory mobile={width < 720} />);
    await cmp.getByTestId("chat-pin-clara").click();

    for (const tab of ["staff", "outsource"]) {
      if (tab === "outsource") {
        await cmp.getByTestId("office-tab-outsource").click();
        await cmp.getByTestId("chat-pin-ow-71d0").click();
      }
      const m = await page.evaluate(() => ({
        pageOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        rows: Array.from(document.querySelectorAll(".member-card, .outsource-row")).map((row) => {
          const r = (s: string) => row.querySelector(s)?.getBoundingClientRect() ?? null;
          return {
            row: row.getBoundingClientRect(),
            pin: r(".chat-pins__toggle"),
            pill: r(".member-card__unread"),
          };
        }),
      }));
      const at = `[${width}px ${tab}]`;
      expect(m.pageOver, `${at} page scrolls sideways`).toBeLessThanOrEqual(1);
      expect(m.rows.length, `${at} no rows rendered`).toBeGreaterThan(3);
      expect(m.rows.some((r) => r.pill), `${at} the fixture lost its unread pill`).toBe(true);
      for (const [i, { row, pin, pill }] of m.rows.entries()) {
        expect(pin, `${at} row ${i} has no pin`).not.toBeNull();
        expect(pin!.width, `${at} row ${i} pin width`).toBeGreaterThanOrEqual(24);
        expect(pin!.height, `${at} row ${i} pin height`).toBeGreaterThanOrEqual(24);
        expect(pin!.right, `${at} row ${i} pin past its row`).toBeLessThanOrEqual(row.right);
        if (pill) {
          expect(pin!.right, `${at} row ${i} pin runs into the pill`).toBeLessThanOrEqual(pill.left);
        }
      }

      // Collapsed, the rest heading carries its rows' unread (the 99+ row on
      // the staff tab, the outsource rows' 2 on the other).
      await cmp.getByTestId("chat-pins-heading-rest").click();
      const h = await page.evaluate(() => {
        const head = document.querySelector('[data-testid="chat-pins-heading-rest"]')!;
        const pill = head.querySelector(".member-card__unread");
        return {
          pageOver: document.documentElement.scrollWidth - document.documentElement.clientWidth,
          head: head.getBoundingClientRect(),
          list: head.parentElement!.getBoundingClientRect(),
          pill: pill ? pill.getBoundingClientRect() : null,
        };
      });
      expect(h.pageOver, `${at} page scrolls sideways with a collapsed heading`).toBeLessThanOrEqual(1);
      expect(h.head.right, `${at} heading past the list`).toBeLessThanOrEqual(h.list.right + 0.5);
      expect(h.pill, `${at} collapsed heading lost its unread pill`).not.toBeNull();
      expect(h.pill!.left, `${at} heading pill`).toBeGreaterThanOrEqual(h.head.left);
      expect(h.pill!.right, `${at} heading pill`).toBeLessThanOrEqual(h.head.right);
      await cmp.getByTestId("chat-pins-heading-rest").click();
    }
  });
}

test("the section headings read at the muted-text floor (4.5:1) against the page", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  for (const key of ["pinned", "rest"]) {
    const contrast = await cmp.getByTestId(`chat-pins-heading-${key}`).evaluate((el) => {
      const parse = (c: string) => c.match(/[\d.]+/g)!.map(Number);
      const lum = ([r, g, b]: number[]) => {
        const f = (v: number) => {
          const x = v / 255;
          return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
        };
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
      };
      const [hi, lo] = [
        lum(parse(getComputedStyle(el).color)),
        lum(parse(getComputedStyle(document.body).backgroundColor)),
      ].sort((a, b) => b - a);
      return (hi + 0.05) / (lo + 0.05);
    });
    expect(contrast, `${key} heading contrast`).toBeGreaterThanOrEqual(4.5);
  }
});

test("a long press on a pinned row lifts it, and dropping it one row up reorders without opening a chat", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  const aside = cmp.locator("aside.office__members");
  const row = (name: string) => cmp.locator(".member-card", { hasText: name });
  await cmp.getByTestId("chat-pin-clara").click();
  await cmp.getByTestId("chat-pin-dora").click();
  expect(await names(cmp)).toEqual(["Mira", "Clara", "Dora", "Beto", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);

  const c = await grip(row("Dora"));
  const step = c.y - (await grip(row("Clara"))).y;
  await page.mouse.move(c.x, c.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(1);
  await page.mouse.move(c.x, c.y - step + 6, { steps: 12 });
  await page.waitForTimeout(300);
  await page.mouse.up();
  await page.waitForTimeout(400);

  expect(await names(cmp)).toEqual(["Mira", "Dora", "Clara", "Beto", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  await expect(aside).toHaveAttribute("data-chat", "");
});

test("a pinned row dragged far below its section stays pinned, as the last pin", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  await cmp.getByTestId("chat-pin-clara").click();
  await longPressDrag(page, cmp.locator(".member-card", { hasText: "Mira" }), (y) => y + 600);
  expect((await names(cmp)).slice(0, 3)).toEqual(["Clara", "Mira", "Beto"]);
  await expect(cmp.getByTestId("chat-pin-mira")).toHaveAttribute("aria-pressed", "true");
});

test("a press that moves before the hold ends, or a hold on an unpinned row, lifts nothing", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  await cmp.getByTestId("chat-pin-clara").click();

  const clara = await grip(cmp.locator(".member-card", { hasText: "Clara" }));
  await page.mouse.move(clara.x, clara.y);
  await page.mouse.down();
  await page.mouse.move(clara.x, clara.y - 60, { steps: 4 });
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(0);
  await page.mouse.up();

  const beto = await grip(cmp.locator(".member-card", { hasText: "Beto" }));
  await page.mouse.move(beto.x, beto.y);
  await page.mouse.down();
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(0);
  await page.mouse.up();
  expect(await names(cmp)).toEqual(["Mira", "Clara", "Beto", "Dora", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
});

test("390px touch: a long press then a drag reorders the pinned rows", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory mobile />);
  await cmp.getByTestId("chat-pin-evan").click();
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 });
  const touch = (type: "touchStart" | "touchMove" | "touchEnd", x: number, y: number) =>
    cdp.send("Input.dispatchTouchEvent", { type, touchPoints: type === "touchEnd" ? [] : [{ x, y }] });
  const evan = await grip(cmp.locator(".member-card", { hasText: "Evan" }));
  const mira = await grip(cmp.locator(".member-card", { hasText: "Mira" }));

  await touch("touchStart", evan.x, evan.y);
  await page.waitForTimeout(HOLD_MS);
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(1);
  for (let i = 1; i <= 12; i++) await touch("touchMove", evan.x, evan.y + ((mira.y - 6 - evan.y) * i) / 12);
  await page.waitForTimeout(300);
  await touch("touchEnd", 0, 0);
  await page.waitForTimeout(400);

  expect((await names(cmp)).slice(0, 2)).toEqual(["Evan", "Mira"]);
});

test("from the keyboard, Enter on a pin pins the row and keeps focus on that pin, so Enter again unpins it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  const pin = cmp.getByTestId("chat-pin-clara");
  await pin.focus();

  await page.keyboard.press("Enter");
  expect(await names(cmp)).toEqual(["Mira", "Clara", "Beto", "Dora", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  await expect(pin).toBeFocused();
  await expect(pin).toHaveAttribute("aria-pressed", "true");

  await page.keyboard.press("Enter");
  expect(await names(cmp)).toEqual(["Mira", "Beto", "Clara", "Dora", "Evan", "EvaRhapsodyInboxForwarderStandbyRelay"]);
  await expect(pin).toBeFocused();
  await expect(pin).toHaveAttribute("aria-pressed", "false");
  await expect(cmp.locator("aside.office__members")).toHaveAttribute("data-chat", "");
});

test("from the keyboard, Space lifts a pinned row, ArrowUp moves it and Enter drops it there without opening its chat; Escape puts it back", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  const cmp = await mount(<OfficeChatPinsStory />);
  const aside = cmp.locator("aside.office__members");
  await cmp.getByTestId("chat-pin-clara").click();
  await cmp.getByTestId("chat-pin-dora").click();
  const dora = cmp.locator(".chat-pins__section .member-card", { hasText: "Dora" });

  // The sensor starts listening for the move keys a tick after it lifts.
  await dora.focus();
  await page.keyboard.press("Space");
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(1);
  await page.waitForTimeout(100);
  await page.keyboard.press("ArrowUp");
  await page.waitForTimeout(300);
  await page.keyboard.press("Escape");
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(0);
  await page.waitForTimeout(300);
  expect((await names(cmp)).slice(0, 3)).toEqual(["Mira", "Clara", "Dora"]);

  await dora.focus();
  await page.keyboard.press("Space");
  await expect(cmp.getByTestId("chat-pins-lifted")).toHaveCount(1);
  await page.waitForTimeout(100);
  await page.keyboard.press("ArrowUp");
  await page.waitForTimeout(300);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
  expect((await names(cmp)).slice(0, 3)).toEqual(["Mira", "Dora", "Clara"]);
  await expect(aside).toHaveAttribute("data-chat", "");
});
