// The 請示 page's card head, task row and 稍後再看 menu, measured on the real
// page in the real content column. jsdom lays nothing out, so whether a CJK
// button label folds, a task row runs out of its card, or the phone sheet
// leaves the screen is only visible here.
import { test, expect, type MountResult } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { ReplyDeskStory } from "./stories/ReplyDeskStory";
import { QUESTION_BODY, QUESTION_SUMMARY } from "./stories/replyDeskQuestion";
import { centreOf, preferReduced, touch } from "./touchInput";

type Box = { left: number; right: number; top: number; bottom: number };

async function box(l: Locator): Promise<Box> {
  return l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
  });
}

/** Line boxes the element's text occupies: text rects that overlap vertically
 * (a smaller font on the same line sits a few px lower) count as one line. */
async function textLines(l: Locator): Promise<number> {
  return l.evaluate((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const rects = [...range.getClientRects()]
      .filter((r) => r.width > 0 && r.height > 0)
      .sort((a, b) => a.top - b.top);
    let lines = 0;
    let bottom = -Infinity;
    for (const r of rects) {
      if (r.top >= bottom - 1) lines += 1;
      bottom = Math.max(bottom, r.bottom);
    }
    return lines;
  });
}

const inside = (inner: Box, outer: Box, slack = 0.5) =>
  inner.left >= outer.left - slack &&
  inner.right <= outer.right + slack &&
  inner.top >= outer.top - slack &&
  inner.bottom <= outer.bottom + slack;

const overlaps = (a: Box, b: Box) =>
  a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;

/** What the element draws stays within its own box (a label in a box sized for
 * an icon spills out of it). */
async function contentFits(l: Locator): Promise<boolean> {
  return l.evaluate((el) => el.scrollWidth <= el.clientWidth && el.scrollHeight <= el.clientHeight);
}

const HEAD_BUTTONS = {
  zh: { jump: "跳到原訊息", snooze: "稍後再看", expire: "標為過期", unsnooze: "取消延後" },
  en: { jump: "View in chat", snooze: "Later", expire: "Mark expired", unsnooze: "Bring back now" },
};

/** Whether anything in the name column paints into the strip between it and
 * the buttons: the strip is captured with the column shown and with it hidden,
 * and the two only match when every line of it is cut at the column's edge. */
async function nameColumnDrawsPastItsEdge(page: Page, w: Box, a: Box): Promise<boolean> {
  const clip = { x: w.right + 1, y: w.top, width: Math.max(1, a.left - w.right - 2), height: w.bottom - w.top };
  const shown = await page.screenshot({ clip });
  const hidden = await page.screenshot({ clip, style: ".reply-card__who { visibility: hidden !important; }" });
  return !shown.equals(hidden);
}

/** On a phone the head's first line is the avatar, the name column beside it
 * (name, role and an outsource asker's task lines, each one line, cut at the
 * column's edge, the title line with 「…」) and the buttons at its right end;
 * the stamp is alone on the line under them, at the right. Returns the texts
 * that had to be cut. */
async function expectPhoneHeadLines(card: Locator, what: string): Promise<string[]> {
  const head = card.locator(".reply-card__head");
  await head.scrollIntoViewIfNeeded();
  const h = await box(head);
  const a = await box(head.locator(".reply-card__actions"));
  const avatar = await box(head.locator(".reply-card__avatar"));
  const w = await box(head.locator(".reply-card__who"));
  const stamp = head.locator(":scope > .reply-card__waited, :scope > .reply-card__answered-at");
  expect(await contentFits(stamp), `${what}: the stamp shows its whole text`).toBe(true);
  const s = await box(stamp);
  expect(w.top, `${what}: the name beside the avatar, not under it`).toBeLessThan(avatar.bottom);
  expect(w.left, `${what}: the name right of the avatar`).toBeGreaterThanOrEqual(avatar.right);
  expect(w.right, `${what}: the name column ends before the buttons`).toBeLessThanOrEqual(a.left + 0.5);
  expect(await nameColumnDrawsPastItsEdge(card.page(), w, a), `${what}: the name column's lines stop at its edge`).toBe(false);
  const cutTexts: string[] = [];
  for (const line of await head.locator(".reply-card__name, .reply-card__role, .current-task-title").all()) {
    const text = (await line.textContent()) ?? "";
    expect(await textLines(line), `${what}: 「${text}」 on one line`).toBe(1);
    const over = await line.evaluate((el) => el.scrollWidth > el.clientWidth);
    if (!over) continue;
    cutTexts.push(text);
    expect(await line.getAttribute("title"), `${what}: 「${text}」 whole on hover`).toBe(text);
    const drawn = await line.screenshot();
    await line.evaluate((el) => ((el as HTMLElement).style.textOverflow = "clip"));
    const clipped = await line.screenshot();
    await line.evaluate((el) => ((el as HTMLElement).style.textOverflow = ""));
    expect(drawn.equals(clipped), `${what}: 「${text}」 ends in 「…」`).toBe(false);
  }
  expect(inside(a, h), `${what}: the buttons inside the head`).toBe(true);
  expect(Math.abs(a.right - h.right), `${what}: the buttons at the head's right end`).toBeLessThanOrEqual(0.5);
  expect(a.top < avatar.bottom && avatar.top < a.bottom, `${what}: the buttons on the avatar's line`).toBe(true);
  expect(s.top, `${what}: the stamp under the buttons`).toBeGreaterThanOrEqual(a.bottom - 0.5);
  expect(s.top, `${what}: the stamp under the name column`).toBeGreaterThanOrEqual(w.bottom - 0.5);
  expect(Math.abs(s.right - h.right), `${what}: the stamp at the head's right end`).toBeLessThanOrEqual(0.5);
  expect(inside(s, h), `${what}: the stamp inside the head`).toBe(true);
  return cutTexts;
}

/** The line each `longAskers` card has too long for a phone's name column, and
 * the widths it is too long at. */
const LONG_ASKER_LINES: Record<string, { text: string; widths: number[] }> = {
  "rc-desk-long-name": { text: "GF Eva Rhapsody Inbox", widths: [320, 390] },
  "rc-desk-long-role": { text: "OffiCraft 入門介紹影片（繁中，4 章＋完整版）", widths: [320, 390] },
  "rc-desk-long-task": { text: "OffiCraft 入門介紹影片（繁中，4 章＋完整版）的旁白、字幕與封面", widths: [320, 390] },
};

async function noPageOverflow(page: Page) {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(m.scroll, "the page does not scroll sideways").toBeLessThanOrEqual(m.client);
}

for (const width of [320, 390, 1440]) {
  test.describe(() => {
    test.use({ hasTouch: width <= 720 });
    test(`width ${width}${width <= 720 ? ", touch" : ""}: 複製, 跳到原訊息, 稍後再看 and 標為過期 sit together on one line in the head, ${width <= 720 ? "32px apart centre to centre (26px buttons, 6px apart)" : "26px apart centre to centre (22px buttons, 4px apart)"}, each icon inside its own button, clear of each other and of the waited stamp${width <= 720 ? ", the buttons at the right end of the name's line beside the avatar and the name column, whose name, role and task lines stay one line each, cut with an ellipsis however long, and the stamp under them at the right on waiting and snoozed cards" : ""}, and the task row with its chips stays inside its card`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ReplyDeskStory longAskers />);
      expect(await page.evaluate(() => matchMedia("(hover: none)").matches), "control: the touch-sized buttons").toBe(width <= 720);
      await expect(cmp.getByTestId("waiting-card")).toHaveCount(6);

      for (const card of await cmp.getByTestId("waiting-card").all()) {
        await card.scrollIntoViewIfNeeded();
        const cardBox = await box(card);
        const names = ["複製訊息", ...Object.values(HEAD_BUTTONS.zh).filter((n) => n !== "取消延後")];
        const waited = await box(card.getByTestId("waited"));
        expect(inside(waited, cardBox), "the waited stamp inside the card").toBe(true);
        const boxes: Box[] = [];
        for (const name of names) {
          const button = card.getByRole("button", { name, exact: true });
          await expect(button).toHaveAttribute("title", name);
          expect(await contentFits(button), `${name}'s icon inside its button`).toBe(true);
          const b = await box(button);
          expect(inside(b, cardBox), `${name} inside the card`).toBe(true);
          expect(overlaps(b, waited), `${name} clear of the waited stamp`).toBe(false);
          for (const other of boxes) expect(overlaps(b, other), `${name} clear of the button before it`).toBe(false);
          boxes.push(b);
        }
        const mids = boxes.map((b) => (b.top + b.bottom) / 2);
        expect(Math.max(...mids) - Math.min(...mids), `${names.join(", ")} share one line`).toBeLessThanOrEqual(1);
        const side = width <= 720 ? 26 : 22;
        for (const [i, b] of boxes.entries()) {
          expect(b.right - b.left, `${names[i]} ${side}px wide`).toBeCloseTo(side, 0);
          expect(b.bottom - b.top, `${names[i]} ${side}px tall`).toBeCloseTo(side, 0);
          if (i === 0) continue;
          const prev = boxes[i - 1];
          const pitch = width <= 720 ? 32 : 26;
          expect((b.left + b.right) / 2 - (prev.left + prev.right) / 2, `${names[i]} ${pitch}px from the button before it, centre to centre`).toBeCloseTo(pitch, 0);
        }
        if (width <= 720) {
          const what = `waiting, ${await card.locator(".reply-card__name").textContent()}, ${await card.getByTestId("waited").textContent()}`;
          const cut = await expectPhoneHeadLines(card, what);
          const mustCut = LONG_ASKER_LINES[(await card.getAttribute("data-reply-card-id")) ?? ""];
          if (mustCut?.widths.includes(width)) expect(cut, `${what}: control, the long line is cut`).toContain(mustCut.text);
        }
        const row = card.getByTestId("reply-task-ref");
        if ((await row.count()) === 0) continue;
        const rowBox = await box(row);
        expect(inside(rowBox, cardBox), "task row inside the card").toBe(true);
        expect(rowBox.bottom, "task row above the head").toBeLessThanOrEqual((await box(card.locator(".reply-card__head"))).top);
        for (const part of await row.locator(".reply-card__task-chips > *, .reply-card__task-title, .reply-card__task-jump").all()) {
          expect(inside(await box(part), rowBox), `${await part.textContent()} inside the task row`).toBe(true);
        }
        for (const chip of await row.locator(".reply-card__task-chips > *").all()) {
          expect(await textLines(chip), `${await chip.textContent()} on one line`).toBe(1);
        }
        expect(await textLines(row.getByTestId("reply-task-jump"))).toBe(1);
      }
      await noPageOverflow(page);
      if (width > 720) return;

      await cmp.getByTestId("snoozed-toggle").click();
      await expectPhoneHeadLines(cmp.getByTestId("snoozed-card"), "snoozed");
      const unsnooze = await box(cmp.getByTestId("snoozed-card").getByRole("button", { name: "取消延後", exact: true }));
      expect(unsnooze.right - unsnooze.left, "取消延後 26px wide").toBeCloseTo(26, 0);
      expect(unsnooze.bottom - unsnooze.top, "取消延後 26px tall").toBeCloseTo(26, 0);
      await noPageOverflow(page);
    });
  });

  test.describe(() => {
    test.use({ hasTouch: width <= 720 });
    test(`width ${width}: the 稍後再看 menu is on screen with every preset and the custom row inside it${
      width <= 720 ? " (a bottom sheet, its fields at least 16px so iOS does not zoom in on them)" : ""
    }`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ReplyDeskStory />);
      const card = cmp.getByTestId("waiting-card").first();
      await card.getByTestId("snooze-card").click();
      const menu = page.locator('[data-testid^="snooze-menu-"]');
      await expect(menu).toBeVisible();
      const menuBox = await box(menu);
      const view = { left: 0, top: 0, right: width, bottom: 900 };
      expect(inside(menuBox, view), "the menu is on screen").toBe(true);
      if (width <= 720) {
        expect(Math.round(menuBox.bottom)).toBe(900);
        expect(Math.round(menuBox.right - menuBox.left)).toBe(width);
      }
      for (const part of await menu.locator(".reply-snooze__preset, .reply-snooze__custom-row > *").all()) {
        expect(inside(await box(part), menuBox), `${await part.textContent()} inside the menu`).toBe(true);
      }
      const tops = await menu
        .locator(".reply-snooze__custom-row > *")
        .evaluateAll((els) => els.map((el) => Math.round(el.getBoundingClientRect().top + el.getBoundingClientRect().height / 2)));
      expect(Math.max(...tops) - Math.min(...tops), "the custom row is one line").toBeLessThanOrEqual(2);
      await menu.getByTestId("snooze-amount").fill("90");
      await menu.getByTestId("snooze-unit").selectOption("day");
      expect(await textLines(menu.getByTestId("snooze-preview"))).toBe(1);
      const menuSize = await menu.evaluate((e) => parseFloat(getComputedStyle(e).fontSize));
      for (const id of ["snooze-amount", "snooze-unit"]) {
        const field = menu.getByTestId(id);
        await field.focus();
        await expect(field).toBeFocused();
        const size = await field.evaluate((e) => parseFloat(getComputedStyle(e).fontSize));
        if (width <= 720) expect(size, `${id} font size on a touch screen`).toBeGreaterThanOrEqual(16);
        else expect(size, `${id} keeps the menu's size`).toBe(menuSize);
      }
      await noPageOverflow(page);
    });
  });
}

for (const width of [1440, 1728]) {
  for (const language of ["zh", "en"]) {
    test(`width ${width}, ${language}: 跳到原訊息 and 標為過期 sit at the same x on every waiting card whatever the waited stamp's length, and the snoozed card's return date stays inside its card`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<ReplyDeskStory />);
      const cards = cmp.getByTestId("waiting-card");
      await expect(cards).toHaveCount(3);
      const lead = language === "zh" ? "已等你" : "Waiting";
      expect(await cmp.getByTestId("waited").allTextContents()).toEqual([`${lead} 5m`, `${lead} 2h 57m`, `${lead} 12d 23h`]);

      const label = HEAD_BUTTONS[language as "zh" | "en"];
      const jumpLefts: number[] = [];
      const expireLefts: number[] = [];
      for (const card of await cards.all()) {
        jumpLefts.push((await box(card.getByRole("button", { name: label.jump, exact: true }))).left);
        expireLefts.push((await box(card.getByRole("button", { name: label.expire, exact: true }))).left);
      }
      expect(Math.max(...jumpLefts) - Math.min(...jumpLefts), `跳到原訊息 lefts ${jumpLefts}`).toBeLessThanOrEqual(0.5);
      expect(Math.max(...expireLefts) - Math.min(...expireLefts), `標為過期 lefts ${expireLefts}`).toBeLessThanOrEqual(0.5);

      await cmp.getByTestId("snoozed-toggle").click();
      const snoozed = cmp.getByTestId("snoozed-card");
      await expect(snoozed).toHaveCount(1);
      const snoozedBox = await box(snoozed);
      const until = snoozed.getByTestId("snoozed-until");
      expect(inside(await box(until), snoozedBox), "the return date inside the snoozed card").toBe(true);
      expect(await textLines(until), "the return date on one line").toBe(1);
      for (const name of [label.jump, label.unsnooze, label.expire]) {
        const button = snoozed.getByRole("button", { name, exact: true });
        expect(inside(await box(button), snoozedBox), `${name} inside the snoozed card`).toBe(true);
        expect(await contentFits(button), `${name}'s icon inside its button`).toBe(true);
      }
      await noPageOverflow(page);
    });
  }
}

// The chat room's message actions on this page's cards: 複製 in the head for
// the mouse; for a finger, a long press opens the same sheet, and 選取文字 the
// same selection mode.
const QUESTION_TEXT = `${QUESTION_SUMMARY}\n\n${QUESTION_BODY.replace("[@Mira](/#office/member/mira)", "@Mira")}`;
const CHECK_PATH = 'svg path[d="M20 6 9 17l-5-5"]';
const QUESTION_LAST_LINE = QUESTION_BODY.split("\n\n").at(-1)!;
const cardOf = (page: Page, id: string) => page.locator(`[data-reply-card-id="${id}"]`);

/** In the mode the card is not the fold toggle it is otherwise, and is open. */
async function expectInMode(card: Locator) {
  await expect(card).toHaveClass(/reply-card--selecting/);
  await expect(card).not.toHaveAttribute("role", /./);
  await expect(card).not.toHaveAttribute("aria-expanded", /./);
  await expect(card).not.toHaveAttribute("aria-label", /./);
  await expect(card.locator(".reply-card__summary")).toBeVisible();
}

function selectAll(card: Locator) {
  return card.evaluate((c) => {
    const r = document.createRange();
    r.selectNodeContents(c);
    window.getSelection()!.removeAllRanges();
    window.getSelection()!.addRange(r);
  });
}

/** What 複製 wrote: the title, then the body to its last line, and nothing of
 * the head (asker, 已等你). */
async function expectTitleAndBodyCopied(page: Page, title: string, lastLine: string) {
  await expect(page.getByTestId("chat-select-copy").locator(CHECK_PATH)).toHaveCount(1);
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied.startsWith(`${title}\n`), copied).toBe(true);
  expect(copied.endsWith(lastLine), copied).toBe(true);
  expect(copied).not.toMatch(/已等你|已回覆|Mira 經理|mira/);
}

async function mountDesk(
  mount: (c: React.JSX.Element) => Promise<MountResult>,
  page: Page,
  width: number,
  height: number,
  props: { cardReadFails?: boolean; cardReadHangs?: boolean } = {},
) {
  await page.setViewportSize({ width, height });
  const cmp = await mount(<ReplyDeskStory questions {...props} />);
  await expect(cardOf(page, "rc-desk-q")).toBeVisible();
  return cmp;
}

test("width 1440, mouse: 複製 in each card's head writes the whole question and turns into a check without folding the card; a mouse selection still quotes into the answer box", async ({
  mount,
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await mountDesk(mount, page, 1440, 900);
  const card = cardOf(page, "rc-desk-q");
  const copy = card.locator(".reply-card__actions .chat__msg-copy");
  await expect(copy).toHaveAttribute("aria-label", "複製訊息");
  for (const other of await page.getByTestId("waiting-card").all()) {
    const head = await box(other.locator(".reply-card__head"));
    const c = await box(other.locator(".chat__msg-copy"));
    const jump = await box(other.getByRole("button", { name: "跳到原訊息", exact: true }));
    expect(inside(c, head), "複製 inside the head").toBe(true);
    expect(Math.abs((c.top + c.bottom) / 2 - (jump.top + jump.bottom) / 2), "複製 on the buttons' line").toBeLessThanOrEqual(1);
  }

  await copy.click();
  await expect(copy).toHaveAttribute("aria-label", "已複製");
  await expect(copy.locator(CHECK_PATH)).toHaveCount(1);
  const status = card.locator(".reply-card__copy").getByRole("status");
  await expect(status).toHaveText("已複製");
  expect((await status.boundingBox())?.width ?? 0, "words beside the check").toBeLessThanOrEqual(1);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(QUESTION_TEXT);
  await expect(card).toHaveAttribute("aria-expanded", "false");

  await card.locator(".reply-card__summary").click();
  await expect(card).toHaveAttribute("aria-expanded", "true");
  await expect(card.locator(".reply-composer textarea")).toBeVisible();
  const summary = card.locator(".reply-card__summary p");
  const s = (await summary.boundingBox())!;
  await page.mouse.move(s.x + 2, s.y + s.height / 2);
  await page.mouse.down();
  await page.mouse.move(s.x + 60, s.y + s.height / 2, { steps: 5 });
  await page.mouse.up();
  const selected = await page.evaluate(() => window.getSelection()!.toString());
  expect(selected.length).toBeGreaterThan(0);
  const reply = page.getByTestId("chat-selection-reply");
  await expect(reply).toBeVisible();
  const r = (await reply.boundingBox())!;
  expect(r.y, "回覆這段 sits under the selection").toBeGreaterThan(s.y);
  expect(r.y + r.height, "回覆這段 docked at the bottom").toBeLessThan(900 - 40);
  await reply.click();
  await expect(card.getByTestId("reply-composer-quote")).toContainText(selected);
  await expect(card.locator(".reply-composer textarea")).toBeFocused();
  await expect(card).toHaveAttribute("aria-expanded", "true");

  await page.mouse.move(s.x + 20, s.y + 4);
  await page.mouse.down();
  await page.waitForTimeout(700);
  await page.mouse.up();
  await expect(page.getByTestId("chat-msg-menu"), "a mouse held down opened the sheet").toHaveCount(0);
  await expect(page.getByTestId("chat-select-bar")).toHaveCount(0);
});

test("width 1440, mouse: 複製 on a card whose read fails says 複製失敗 in the head and writes nothing", async ({
  mount,
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.evaluate(() => navigator.clipboard.writeText("before"));
  await mountDesk(mount, page, 1440, 900, { cardReadFails: true });
  const card = cardOf(page, "rc-desk-q");
  await card.locator(".chat__msg-copy").click();
  const failed = card.locator(".chat__msg-copy-status--failed");
  await expect(failed).toHaveText("複製失敗");
  await expect(failed).toBeVisible();
  expect(inside(await box(failed), await box(card)), "複製失敗 inside the card").toBe(true);
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("before");
  await expect(card).toHaveAttribute("aria-expanded", "false");
});

for (const [width, height] of [
  [320, 700],
  [390, 844],
] as const) {
  test.describe(() => {
    test.use({ hasTouch: true });

    test(`width ${width}, touch: a long press on a card opens the chat's sheet with 複製訊息・選取文字 only (no 回覆這則, waiting or answered: the answer box is on the card), and neither folds the card nor selects its words; an answered card's buttons sit at the right end of the name's line with its time under them at the right`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height);
      expect(await page.evaluate(() => matchMedia("(hover: none)").matches)).toBe(true);
      const card = cardOf(page, "rc-desk-q");
      const menu = page.getByTestId("chat-msg-menu");
      await expect(card).toHaveCSS("user-select", "none");

      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      await expect(card, "a tap opens the card").toHaveAttribute("aria-expanded", "true");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      await expect(card).toHaveAttribute("aria-expanded", "false");
      await page.waitForTimeout(600);
      await expect(menu, "a tap opened the sheet").toHaveCount(0);

      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await expect(menu).toBeVisible();
      await expect(menu.getByRole("menuitem")).toHaveText(["複製訊息", "選取文字"]);
      await expect(menu.getByTestId("chat-msg-menu-reply")).toHaveCount(0);
      await expect(menu.getByRole("menuitem").first()).toBeFocused();
      await expect(menu.getByRole("menuitem").first(), "a finger's press drew a focus ring").toHaveCSS("outline-style", "none");
      await expect(card).toHaveClass(/reply-card--pressed/);
      await expect(card).toHaveCSS("outline-style", "solid");
      await expect(card, "a focus ring on the card").toHaveCSS("box-shadow", "none");
      await expect(card, "the press folded or unfolded the card").toHaveAttribute("aria-expanded", "false");
      expect(await page.evaluate(() => window.getSelection()!.toString()), "the press selected words").toBe("");
      await page.waitForTimeout(300);
      const sheet = (await menu.boundingBox())!;
      expect(Math.abs(sheet.y + sheet.height - height), "the sheet sits on the screen's bottom edge").toBeLessThanOrEqual(0.5);
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(menu).toHaveCount(0);
      await expect(card).not.toHaveClass(/reply-card--pressed/);
      await expect(card).toHaveAttribute("aria-expanded", "false");

      const copy = await box(card.locator(".chat__msg-copy"));
      const jump = await box(card.getByRole("button", { name: "跳到原訊息", exact: true }));
      expect(copy.bottom - copy.top, "複製 is shorter than the head's other buttons").toBeGreaterThanOrEqual(jump.bottom - jump.top - 0.5);
      expect(copy.right - copy.left, "複製 is narrower than the head's buttons are tall").toBeGreaterThanOrEqual(jump.bottom - jump.top - 0.5);
      await touch(page, await centreOf(card.locator(".chat__msg-copy")), { holdMs: 700 });
      await page.waitForTimeout(200);
      await expect(menu, "a long press on a head button opened the sheet").toHaveCount(0);

      await page.getByTestId("answered-toggle").tap();
      const answered = cardOf(page, "rc-desk-done");
      await expect(answered).toBeVisible();
      await expectPhoneHeadLines(answered, "answered");
      await touch(page, await centreOf(answered.locator(".reply-card__summary")), { holdMs: 700 });
      await expect(menu.getByRole("menuitem")).toHaveText(["複製訊息", "選取文字"]);
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(menu).toHaveCount(0);

      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      await expect(card).toHaveAttribute("aria-expanded", "true");
      const answer = card.locator(".reply-composer textarea");
      await expect(answer).toBeVisible();
      await expect(answer, "the answer box cannot be selected in by a finger").toHaveCSS("user-select", "text");
    });

    test(`width ${width}, touch: 複製訊息 from the sheet writes the question and shows nothing once the sheet closes; a failed read shows 複製失敗 on the card, over the name beside the buttons, which stay where they were`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      const first = await mountDesk(mount, page, width, height);
      const card = cardOf(page, "rc-desk-q");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-copy").tap();
      await expect(page.getByTestId("chat-msg-menu")).toHaveCount(0);
      await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(QUESTION_TEXT);
      const status = card.locator(".reply-card__copy").getByRole("status");
      await expect(status).toHaveText("已複製");
      expect((await status.boundingBox())?.width ?? 0, "words shown for a copy that worked").toBeLessThanOrEqual(1);
      await expect(card.locator(".chat__msg-copy-status--failed")).toHaveCount(0);
      await expect(card).toHaveAttribute("aria-expanded", "false");
      await first.unmount();

      await mountDesk(mount, page, width, height, { cardReadFails: true });
      const whoBefore = await box(card.locator(".reply-card__who"));
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-copy").tap();
      const failed = card.locator(".chat__msg-copy-status--failed");
      await expect(failed).toHaveText("複製失敗");
      await expect(failed).toBeVisible();
      await expectPhoneHeadLines(card, "a failed copy");
      const whoDuring = await box(card.locator(".reply-card__who"));
      expect(whoDuring.right - whoDuring.left, "the name column keeps its width while 複製失敗 shows").toBeCloseTo(whoBefore.right - whoBefore.left, 0);
      const failedBox = await box(failed);
      expect(inside(failedBox, await box(card)), "複製失敗 inside the card").toBe(true);
      for (const button of await card.locator(".reply-card__actions button").all()) {
        expect(overlaps(failedBox, await box(button)), `複製失敗 clear of ${await button.getAttribute("aria-label")}`).toBe(false);
      }
      expect(await failed.isVisible(), "control: 複製失敗 still showing when measured").toBe(true);
    });

    test(`width ${width}, touch: 選取文字 opens the card to its title and question with the rest of the page blurred and out of reach, its link and mention do nothing, 複製 and 回覆 take the selection from a bar at the screen's bottom that the page ends above, and 回覆 leaves the card open with the quote in its answer box`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountDesk(mount, page, width, height);
      await preferReduced(page, false);
      const card = cardOf(page, "rc-desk-q");
      const bar = page.getByTestId("chat-select-bar");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();

      await expect(bar).toBeVisible();
      await expect(bar).toBeFocused();
      await expect(bar.getByRole("button")).toHaveText(["取消", "複製", "回覆"]);
      await expectInMode(card);
      await expect(card.locator(".reply-card__body")).toBeVisible();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await expect(card).toHaveCSS("user-select", "auto");
      await expect(card).toHaveCSS("filter", "none");
      await expect(card).toHaveJSProperty("inert", false);
      for (const hidden of [
        "[data-testid='reply-task-ref']",
        ".reply-card__head",
        ".reply-card__name",
        "[data-testid='waited']",
        ".reply-card__actions",
        ".reply-card__answer-side",
        ".reply-composer",
      ])
        await expect(card.locator(hidden).first(), `${hidden} in the mode`).toBeHidden();
      for (const other of [cardOf(page, "rc-desk-1"), page.getByTestId("replies-filter"), page.getByTestId("waiting-toggle")]) {
        await expect(other).toHaveCSS("filter", /blur/);
        await expect(other).toHaveCSS("pointer-events", "none");
        await expect(other).toHaveJSProperty("inert", true);
      }

      const b = (await bar.boundingBox())!;
      expect(Math.abs(b.y + b.height - height), "the bar is the bottom of the screen").toBeLessThanOrEqual(0.5);
      expect(Math.abs(b.width - width), "the bar spans the screen").toBeLessThanOrEqual(0.5);
      const list = (await page.locator(".replies").boundingBox())!;
      expect(list.y + list.height, "the page runs on under the bar").toBeLessThanOrEqual(b.y + 0.5);

      const hashBefore = await page.evaluate(() => location.hash);
      let popups = 0;
      page.context().on("page", () => popups++);
      const actions = card.locator(".reply-card__body").locator("a, button");
      await expect(actions).toHaveCount(2);
      for (const action of await actions.all()) await action.tap();
      await card.locator(".reply-card__summary").tap();
      await page.waitForTimeout(300);
      expect(popups, "a link in the card opened").toBe(0);
      expect(await page.evaluate(() => location.hash), "a mention or link in the card navigated").toBe(hashBefore);
      await expect(bar).toBeVisible();
      await expect(card.locator(".reply-card__body"), "a tap on the card folded it").toBeVisible();
      await touch(page, await centreOf(card.locator(".reply-card__body p").nth(2)), { holdMs: 700 });
      await expect(page.getByTestId("chat-msg-menu"), "a long press in the mode opened the sheet").toHaveCount(0);
      await expect(bar).toBeVisible();

      await page.evaluate(() => window.getSelection()!.removeAllRanges());
      await expect(page.getByTestId("chat-select-copy")).toBeDisabled();
      await expect(page.getByTestId("chat-select-reply")).toBeDisabled();
      await selectAll(card);
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await expect(page.getByTestId("chat-select-reply")).toBeEnabled();
      await page.getByTestId("chat-select-copy").tap();
      await expectTitleAndBodyCopied(page, QUESTION_SUMMARY, QUESTION_LAST_LINE);
      const picked = await card.locator(".reply-card__body p").nth(1).evaluate((p) => {
        const text = p.firstChild!;
        const r = document.createRange();
        r.setStart(text, 0);
        r.setEnd(text, 3);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
        return r.toString();
      });
      await page.getByTestId("chat-select-copy").tap();
      await expect(page.getByTestId("chat-select-copy").locator(CHECK_PATH)).toHaveCount(1);
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(picked);
      await expect(bar).toBeVisible();

      await page.getByTestId("chat-select-reply").tap();
      await expect(bar).toHaveCount(0);
      await expect(card).toHaveAttribute("aria-expanded", "true");
      await expect(card.getByTestId("reply-composer-quote")).toContainText(picked);
      await expect(card.locator(".reply-composer textarea")).toBeFocused();
      await expect(cardOf(page, "rc-desk-1")).toHaveCSS("filter", "none");
      await expect(cardOf(page, "rc-desk-1")).toHaveJSProperty("inert", false);
      await expect(card).toHaveCSS("user-select", "none");
    });

    test(`width ${width}, touch: the selection mode ends on 取消, Esc or a tap outside the card, each putting the card back folded or open as it was`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height);
      const card = cardOf(page, "rc-desk-q");
      const bar = page.getByTestId("chat-select-bar");
      const enter = async () => {
        await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
        await page.getByTestId("chat-msg-menu-select").tap();
        await expect(bar).toBeVisible();
        await expectInMode(card);
      };
      for (const [how, leave] of [
        ["取消", () => page.getByTestId("chat-select-cancel").tap()],
        ["Esc", () => page.keyboard.press("Escape")],
        ["a tap outside", () => cardOf(page, "rc-desk-1").tap({ force: true })],
      ] as const) {
        await enter();
        await leave();
        await expect(bar, how).toHaveCount(0);
        await expect(card, `${how} left the card open`).toHaveAttribute("aria-expanded", "false");
        await expect(cardOf(page, "rc-desk-1")).toHaveJSProperty("inert", false);
        await expect(cardOf(page, "rc-desk-1"), `${how} also opened the card under it`).toHaveAttribute("aria-expanded", "false");
        if (how !== "a tap outside") await expect(card.locator(".chat__msg-copy")).toBeFocused();
      }

      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      await expect(card).toHaveAttribute("aria-expanded", "true");
      await enter();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(card, "取消 folded a card that was open").toHaveAttribute("aria-expanded", "true");
      await expect(card.locator(".reply-composer textarea")).toBeVisible();
    });

    test(`width ${width}, touch: with reduced motion or transparency the selection mode fades the rest of the page instead of blurring it, and it is just as out of reach`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height);
      await preferReduced(page, true);
      const card = cardOf(page, "rc-desk-q");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await expect(page.getByTestId("chat-select-bar")).toBeVisible();
      const other = cardOf(page, "rc-desk-1");
      await expect(other).toHaveCSS("filter", "none");
      await expect(other).toHaveCSS("opacity", "0.2");
      await expect(other).toHaveCSS("pointer-events", "none");
      await expect(other).toHaveJSProperty("inert", true);
      await expect(card).toHaveCSS("opacity", "1");
    });

    test(`width ${width}, touch: an answered card in the mode copies a selection but offers no 回覆`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountDesk(mount, page, width, height);
      await page.getByTestId("answered-toggle").tap();
      const card = cardOf(page, "rc-desk-done");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await expect(card.locator(".reply-card__body")).toBeVisible();
      await expect(card.locator(".reply-card__answer-side")).toBeHidden();
      await expect(card.locator(".reply-card__head")).toBeHidden();
      await selectAll(card);
      await expect(page.getByTestId("chat-select-copy")).toBeEnabled();
      await expect(page.getByTestId("chat-select-reply")).toBeDisabled();
      await page.getByTestId("chat-select-copy").tap();
      await expectTitleAndBodyCopied(page, "週報要附上監控截圖嗎？", "附上的話我會放在第二節。");
    });

    test(`width ${width}, touch: on a waiting secret card the sheet is 複製訊息・選取文字 too, and the mode never copies the value typed into its masked box`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountDesk(mount, page, width, height);
      const card = cardOf(page, "rc-desk-secret");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await expect(page.getByTestId("chat-msg-menu").getByRole("menuitem")).toHaveText(["複製訊息", "選取文字"]);
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(page.getByTestId("chat-msg-menu")).toHaveCount(0);
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      const masked = card.getByTestId("reply-secret-input");
      await expect(masked).toBeFocused();
      await page.keyboard.type("hunter2-secret");
      await expect(masked).toHaveValue("hunter2-secret");

      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await expect(masked).toBeHidden();
      await selectAll(card);
      await page.getByTestId("chat-select-copy").tap();
      await expectTitleAndBodyCopied(page, "部署金鑰", "放到 deploy 主機");
      expect(await page.evaluate(() => navigator.clipboard.readText())).not.toContain("hunter2");
      await page.getByTestId("chat-select-cancel").tap();
      await expect(masked).toHaveValue("hunter2-secret");
    });

    test(`width ${width}, touch: a card still being read enters the mode with its title only, the loading line not shown`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height, { cardReadHangs: true });
      const card = cardOf(page, "rc-desk-q");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      await expect(page.getByTestId("chat-select-bar")).toBeVisible();
      await expect(card.getByTestId("card-loading")).toHaveCount(1);
      await expect(card.getByTestId("card-loading")).toBeHidden();
      await expect(card.locator(".reply-card__summary")).toBeVisible();
      await page.getByTestId("chat-select-cancel").tap();
      await expect(card).toHaveAttribute("aria-expanded", "false");
    });
  });
}

for (const [width, height] of [
  [320, 700],
  [390, 640],
] as const) {
  test.describe(() => {
    test.use({ hasTouch: true });
    test(`width ${width}x${height}, touch: in the mode a selection ending near the bar is scrolled 48px clear of it, and the page never scrolls back up to the card's top`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height);
      const card = cardOf(page, "rc-desk-q");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-select").tap();
      const bar = page.getByTestId("chat-select-bar");
      await expect(bar).toBeFocused();
      const barTop = (await bar.boundingBox())!.y;
      const list = page.locator(".replies");
      const last = card.locator(".reply-card__body p").last();
      await list.evaluate((l, p) => {
        l.scrollTop += p!.getBoundingClientRect().bottom - (l.getBoundingClientRect().bottom - 10);
      }, await last.elementHandle());
      expect((await card.boundingBox())!.y, "the card's top is above the page").toBeLessThan((await list.boundingBox())!.y);
      const scrolled = await list.evaluate((l) => l.scrollTop);
      await last.evaluate((p) => {
        const text = p.firstChild!;
        const r = document.createRange();
        r.setStart(text, 0);
        r.setEnd(text, text.textContent!.length);
        window.getSelection()!.removeAllRanges();
        window.getSelection()!.addRange(r);
      });
      const endBottom = () =>
        page.evaluate(() => {
          const rects = window.getSelection()!.getRangeAt(0).getClientRects();
          return rects[rects.length - 1].bottom;
        });
      expect(await endBottom(), "the selection does not start near the bar").toBeGreaterThan(barTop - 48);
      await expect.poll(endBottom, { message: "the selection's end stays by the bar" }).toBeLessThanOrEqual(barTop - 48 + 0.5);
      expect(await list.evaluate((l) => l.scrollTop), "the page scrolled up").toBeGreaterThan(scrolled);
      const settled = await list.evaluate((l) => l.scrollTop);
      await page.setViewportSize({ width, height: height - 40 });
      await page.waitForTimeout(200);
      expect((await card.boundingBox())!.y, "a shorter screen took the page back to the card's top").toBeLessThan((await list.boundingBox())!.y);
      expect(Math.abs((await list.evaluate((l) => l.scrollTop)) - settled), "a shorter screen scrolled the page").toBeLessThanOrEqual(1);
    });
  });
}

test.describe(() => {
  test.use({ hasTouch: true });
  test("width 390, touch: a card in the mode that is snoozed from elsewhere moves to 已延後 below the screen and stays in the mode, brought up above the bar", async ({
    mount,
    page,
  }) => {
    await mountDesk(mount, page, 390, 844);
    await page.getByTestId("snoozed-toggle").tap();
    await expect(page.getByTestId("snoozed-card")).toHaveCount(1);
    const card = cardOf(page, "rc-desk-q");
    await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
    await page.getByTestId("chat-msg-menu-select").tap();
    const bar = page.getByTestId("chat-select-bar");
    await expect(bar).toBeFocused();
    const before = await card.elementHandle();

    await page.evaluate(() =>
      (window as unknown as { __ocSnooze: (id: string, untilTs: number) => void }).__ocSnooze(
        "rc-desk-q",
        Date.now() / 1000 + 10 * 86_400,
      ),
    );
    await expect(page.getByTestId("snoozed-card")).toHaveCount(2);
    const moved = page.locator('[data-testid="snoozed-card"][data-reply-card-id="rc-desk-q"]');
    await expect(moved).toHaveClass(/reply-card--selecting/);
    expect(await moved.evaluate((el, old) => el !== old, before), "the card was not remounted").toBe(true);
    await expect(bar).toBeVisible();
    await page.waitForTimeout(300);
    const barTop = (await bar.boundingBox())!.y;
    const listTop = (await page.locator(".replies").boundingBox())!.y;
    const title = (await moved.locator(".reply-card__summary").boundingBox())!;
    expect(title.y, "the moved card's title is above the page").toBeGreaterThanOrEqual(listTop - 0.5);
    expect(title.y + title.height, "the moved card's title is under the bar").toBeLessThanOrEqual(barTop + 0.5);
  });
});
