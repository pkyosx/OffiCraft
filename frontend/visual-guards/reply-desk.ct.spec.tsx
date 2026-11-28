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

/** Whether a cut line ends in 「…」: the line is captured as drawn and again
 * with its ellipsis switched off (a clamped block: the clamp off, held to the
 * same height), and the two only differ when 「…」 was drawn. */
async function drawsEllipsis(line: Locator): Promise<boolean> {
  // A line squeezed to nothing draws nothing, 「…」 included (and has nothing
  // to capture).
  if ((await line.evaluate((el) => el.clientWidth)) < 1) return false;
  await line.scrollIntoViewIfNeeded();
  const drawn = await line.screenshot();
  const saved = await line.evaluate((el) => {
    const s = (el as HTMLElement).style;
    const before = { maxHeight: s.maxHeight, webkitLineClamp: s.webkitLineClamp, textOverflow: s.textOverflow };
    if (getComputedStyle(el).webkitLineClamp !== "none") {
      s.maxHeight = `${el.clientHeight}px`;
      s.webkitLineClamp = "unset";
    } else s.textOverflow = "clip";
    return before;
  });
  const clipped = await line.screenshot();
  await line.evaluate((el, before) => Object.assign((el as HTMLElement).style, before), saved);
  return !drawn.equals(clipped);
}

const isCut = (l: Locator) => l.evaluate((el) => el.scrollWidth > el.clientWidth + 0.5 || el.scrollHeight > el.clientHeight + 1);

/** Every line of the 開卡人 value — name, role, an outsource asker's task line
 * (chip and type) and task title — that is too long for it ends in 「…」, and
 * its whole text is on hover. Returns the texts that had to be cut. */
async function expectOpenerCutWithEllipsis(meta: Locator, what: string, oneLine: boolean): Promise<string[]> {
  const cutTexts: string[] = [];
  for (const line of await meta.locator(".reply-card__name, .reply-card__role, .outsource-row__task-line, .outsource-row__chip, .outsource-row__type, .current-task-title").all()) {
    const text = (await line.textContent()) ?? "";
    if (oneLine) expect(await textLines(line), `${what}: 「${text}」 on one line`).toBe(1);
    if (!(await isCut(line))) continue;
    cutTexts.push(text);
    // The task line's words are its type's (the chip's title names its
    // action, and its number is the task's id, drawn whole elsewhere).
    const typeEl = (await line.evaluate((el) => el.matches(".outsource-row__task-line, .outsource-row__chip")))
      ? meta.locator(".outsource-row__type")
      : line;
    expect(await typeEl.getAttribute("title"), `${what}: 「${text}」 whole on hover`).toBe(await typeEl.textContent());
    expect(await drawsEllipsis(line), `${what}: 「${text}」 ends in 「…」`).toBe(true);
  }
  return cutTexts;
}

/** What a card's first line is on every device: the state icon, then the
 * card's own id, then at the head's right end the buttons (drawn only with a
 * mouse; on touch they are in the long-press menu and stay in the
 * accessibility tree), the stamp (the 64px pill on a waiting or snoozed card,
 * none on a settled one) and the chevron, last. That right end shares the id's
 * line, or on a card too narrow for both takes a line of its own, whole and
 * still at the right. Under it, the 開卡人 chip and an outsource asker's task
 * stay inside the card, every cut line ending in 「…」. Returns the texts that
 * had to be cut and the boxes of the stamp (when there is one) and chevron. */
async function expectHeadLine(card: Locator, what: string, mode: "touch" | "mouse"): Promise<{ cut: string[]; stamp: Box | null; fold: Box }> {
  const head = card.locator(".reply-card__head");
  await head.scrollIntoViewIfNeeded();
  const h = await box(head);
  const c = await box(card);
  const icon = await box(head.getByTestId("reply-status-icon"));
  const id = await box(head.getByTestId("reply-card-id"));
  const end = await box(head.locator(".reply-card__head-end"));
  const fold = await box(head.locator(".reply-card__fold"));
  const mid = (b: Box) => (b.top + b.bottom) / 2;
  expect(Math.abs(mid(icon) - mid(id)), `${what}: the state icon and the id share a line`).toBeLessThanOrEqual(1);
  expect(id.left, `${what}: the id after the state icon`).toBeGreaterThanOrEqual(icon.right);
  expect(icon.left - h.left, `${what}: the state icon leads the line`).toBeLessThanOrEqual(0.5);
  expect(await textLines(head.getByTestId("reply-card-id")), `${what}: the id on one line`).toBe(1);
  expect(Math.abs(end.right - h.right), `${what}: the right end at the head's right end`).toBeLessThanOrEqual(0.5);
  expect(inside(end, c), `${what}: the right end inside the card`).toBe(true);
  expect(Math.abs(fold.right - end.right), `${what}: the chevron last`).toBeLessThanOrEqual(0.5);
  const sameLine = Math.abs(mid(end) - mid(id)) <= 1;
  if (sameLine) expect(end.left, `${what}: the right end clear of the id`).toBeGreaterThanOrEqual(id.right);
  else expect(end.top, `${what}: the right end whole on a line under the id`).toBeGreaterThanOrEqual(id.bottom - 0.5);
  let buttonsRight = -Infinity;
  for (const button of await head.locator(".reply-card__actions button").all()) {
    const name = await button.getAttribute("aria-label");
    const b = await box(button);
    if (mode === "touch") {
      expect(Math.max(b.right - b.left, b.bottom - b.top), `${what}: ${name} drawn on the head`).toBeLessThanOrEqual(1);
      continue;
    }
    expect(b.right - b.left, `${what}: ${name} drawn`).toBeGreaterThan(1);
    expect(Math.abs(mid(b) - mid(end)), `${what}: ${name} on the right end's line`).toBeLessThanOrEqual(1);
    buttonsRight = Math.max(buttonsRight, b.right);
  }
  if (mode === "touch") expect(sameLine, `${what}: without the buttons the right end shares the id's line`).toBe(true);
  await expect(head.getByRole("button", { name: "跳到原訊息", exact: true }), `${what}: 跳到原訊息 in the accessibility tree`).toHaveCount(1);
  let stamp: Box | null = null;
  if ((await head.locator(".reply-card__waited").count()) > 0) {
    const pill = head.locator(".reply-card__waited-pill");
    await expect(pill, `${what}: the pill drawn`).toBeVisible();
    await expect(pill.locator("svg"), `${what}: the pill's icon`).toHaveCount(1);
    expect(await contentFits(pill), `${what}: the pill's content inside it`).toBe(true);
    expect(await textLines(pill), `${what}: the pill on one line`).toBe(1);
    stamp = await box(pill);
    expect(stamp.right - stamp.left, `${what}: the pill 64px wide`).toBeCloseTo(64, 0);
    expect(Math.abs(mid(stamp) - mid(end)), `${what}: the pill on the right end's line`).toBeLessThanOrEqual(1);
    expect(stamp.right, `${what}: the pill before the chevron`).toBeLessThanOrEqual(fold.left + 0.5);
    if (mode === "mouse") expect(stamp.left, `${what}: the pill after the buttons`).toBeGreaterThanOrEqual(buttonsRight - 0.5);
  }
  const meta = card.locator(".reply-card__meta");
  const value = await box(meta.locator(".reply-card__meta-value"));
  expect(inside(value, c), `${what}: the 開卡人 value inside the card`).toBe(true);
  for (const part of await meta.locator(".reply-card__opener, .reply-card__worker-task").all()) {
    expect(inside(await box(part), value), `${what}: ${await part.textContent()} inside the 開卡人 value`).toBe(true);
  }
  const phone = (await card.page().viewportSize())!.width <= 720;
  const cut = await expectOpenerCutWithEllipsis(meta, what, phone);
  return { cut, stamp, fold };
}

/** The task box right under the first line: the task-number chip (the box's
 * only button, 查看任務, led by the jump icon) and priority on one line, the
 * title on one line under them, cut with 「…」 when long, and the due date
 * under that exactly when the task has one. */
/** The story's cards whose task has a due date. */
const CARDS_WITH_DUE = new Set(["rc-desk-1", "rc-desk-2"]);

async function expectTaskRow(card: Locator, cardBox: Box) {
  const row = card.getByTestId("reply-task-ref");
  await row.scrollIntoViewIfNeeded();
  cardBox = await box(card);
  const rowBox = await box(row);
  const id = await card.getAttribute("data-reply-card-id");
  expect(inside(rowBox, cardBox), `${id}: task box inside the card`).toBe(true);
  expect(rowBox.top, `${id}: task box under the first line`).toBeGreaterThanOrEqual((await box(card.locator(".reply-card__head"))).bottom);
  expect(rowBox.bottom, `${id}: task box above the title`).toBeLessThanOrEqual((await box(card.locator(".reply-card__summary"))).top);
  for (const part of await row.locator("[data-testid='reply-task-no'], [data-testid='reply-task-priority'], .reply-card__task-title, .reply-card__task-due").all()) {
    expect(inside(await box(part), rowBox), `${id}: ${await part.textContent()} inside the task box`).toBe(true);
  }
  const jump = row.getByRole("button", { name: "查看任務", exact: true });
  await expect(row.getByRole("button"), `${id}: the chip is the box's only button`).toHaveCount(1);
  await expect(jump).toHaveAttribute("data-testid", "reply-task-no");
  await expect(jump).toHaveAttribute("title", "查看任務");
  await expect(jump, "named by aria-label, not only by the title fallback").toHaveAttribute("aria-label", "查看任務");
  await expect(jump.locator("svg"), `${id}: the jump icon in the chip`).toHaveCount(1);
  expect(await textLines(jump), `${id}: the task number on one line`).toBe(1);
  const j = await box(jump);
  const svg = await box(jump.locator("svg"));
  expect(svg.right, `${id}: the jump icon left of the number`).toBeLessThanOrEqual(j.left + (j.right - j.left) / 2);
  const jMid = (j.top + j.bottom) / 2;
  const priority = row.getByTestId("reply-task-priority");
  if ((await priority.count()) > 0) {
    const p = await box(priority);
    expect(Math.abs((p.top + p.bottom) / 2 - jMid), `${id}: the priority on the task number's line`).toBeLessThanOrEqual(1);
  }
  const title = row.locator(".reply-card__task-title");
  let below = j.bottom;
  if ((await title.count()) > 0) {
    const t = await box(title);
    expect(t.top, `${id}: the title under the task number`).toBeGreaterThanOrEqual(j.bottom - 0.5);
    expect(await textLines(title), "the task title on one line").toBe(1);
    if (await isCut(title)) {
      await expect(title, "the cut task title whole on hover").toHaveAttribute("title", (await title.textContent()) ?? "");
      expect(await drawsEllipsis(title), `${id}: the cut task title ends in 「…」`).toBe(true);
    }
    below = t.bottom;
  }
  const due = row.locator(".reply-card__task-due");
  await expect(due, `${id}: a due line exactly when the task has a due date`).toHaveCount(CARDS_WITH_DUE.has(id ?? "") ? 1 : 0);
  if (CARDS_WITH_DUE.has(id ?? "")) {
    expect((await box(due)).top, `${id}: the due line under the title`).toBeGreaterThanOrEqual(below - 0.5);
  }
}

/** The long lines of the `longAskers` cards and the head widths they are too
 * long for. The task title and the task line (chip and type) are longer than
 * the name column on every device, so every run measures a cut line ending in
 * 「…」; at 390 on touch the long name still fits beside the pill. */
const LONG_TASK_TITLE = "OffiCraft 入門介紹影片（繁中，4 章＋完整版）的旁白、字幕與封面，另含英文與日文版本的字幕翻譯、章節縮圖、配樂授權確認、各平台上架描述撰寫，以及上線後第一週的觀看數據整理與回報，並把觀眾留言中的常見問題整理成下一版的修改清單、附上每一章的平均觀看秒數與跳出點分析";
const LONG_TASK_LINE = "T-9021034多語系入門影片的旁白錄製、字幕校對、封面設計、章節切分、配樂授權確認、各平台上架前的最終品質檢查與上線後數據回報流程";

/** The open card's divider: right under `above` (its last meta row, or the
 * title on a chat card with none) and the full width of the card's content, a
 * visible 1px line, with the question starting further below it than the
 * card's own 12px gap. */
async function expectDivider(card: Locator, above: Locator, tag: string) {
  const divider = card.locator(".reply-card__divider");
  await expect(divider, `${tag}: one divider`).toHaveCount(1);
  const g = await divider.evaluate((el, aboveEl) => {
    const r = (e: Element) => {
      const b = e.getBoundingClientRect();
      return { left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    };
    const cardEl = el.closest(".reply-card")!;
    const cs = getComputedStyle(cardEl);
    const c = r(cardEl);
    return {
      line: r(el),
      prev: el.previousElementSibling === aboveEl,
      above: r(aboveEl as Element),
      next: el.nextElementSibling ? r(el.nextElementSibling) : null,
      contentLeft: c.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft),
      contentRight: c.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight),
      bg: getComputedStyle(el).backgroundColor,
    };
  }, await above.elementHandle());
  expect(g.prev, `${tag}: the divider comes right after the meta`).toBe(true);
  expect(g.line.bottom - g.line.top, `${tag}: a 1px line`).toBeCloseTo(1, 0);
  expect(g.line.top - g.above.bottom, `${tag}: space between the meta and the line`).toBeCloseTo(14, 0);
  expect(g.line.left, `${tag}: the line starts at the content's left`).toBeCloseTo(g.contentLeft, 0);
  expect(g.line.right, `${tag}: the line ends at the content's right`).toBeCloseTo(g.contentRight, 0);
  expect(g.bg, `${tag}: the line is drawn`).not.toMatch(/^rgba\(\d+, \d+, \d+, 0\)$|^transparent$/);
  expect(g.next, `${tag}: something under the line`).not.toBeNull();
  expect(g.next!.top - g.line.bottom, `${tag}: the question starts 18px under the line`).toBeCloseTo(18, 0);
}

/** Pick one of the three list labels above the cards. */
async function selectTab(page: Page, view: "waiting" | "snoozed" | "answered", gesture: "click" | "tap") {
  await page.getByTestId(`replies-tab-${view}`)[gesture]();
}

async function noPageOverflow(page: Page) {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(m.scroll, "the page does not scroll sideways").toBeLessThanOrEqual(m.client);
}

test("width 1440: 跳到原訊息, 稍後再看, 標為過期 and 複製整張卡片內容 sit together on one line in the head, 32px apart centre to centre (22px buttons, 10px apart), each icon inside its own button, clear of each other and of the waited stamp, and the task box with its task-number chip stays inside its card", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const cmp = await mount(<ReplyDeskStory longAskers />);
  expect(await page.evaluate(() => matchMedia("(hover: none)").matches), "control: a mouse").toBe(false);
  await expect(cmp.getByTestId("waiting-card")).toHaveCount(7);

  for (const card of await cmp.getByTestId("waiting-card").all()) {
    await card.scrollIntoViewIfNeeded();
    const cardBox = await box(card);
    const names = [...Object.values(HEAD_BUTTONS.zh).filter((n) => n !== "取消延後"), "複製整張卡片內容"];
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
    for (const [i, b] of boxes.entries()) {
      expect(b.right - b.left, `${names[i]} 22px wide`).toBeCloseTo(22, 0);
      expect(b.bottom - b.top, `${names[i]} 22px tall`).toBeCloseTo(22, 0);
      if (i === 0) continue;
      const prev = boxes[i - 1];
      expect(b.left - prev.right, `${names[i]} 10px from the button before it`).toBeCloseTo(10, 0);
      expect((b.left + b.right) / 2 - (prev.left + prev.right) / 2, `${names[i]} 32px from the button before it, centre to centre`).toBeCloseTo(32, 0);
    }
    if ((await card.getByTestId("reply-task-ref").count()) > 0) await expectTaskRow(card, cardBox);
  }
  await noPageOverflow(page);
});

const HEAD_SCENES = [
  { width: 320, mode: "touch" },
  { width: 390, mode: "touch" },
  { width: 320, mode: "mouse" },
  { width: 390, mode: "mouse" },
  { width: 640, mode: "touch" },
  { width: 700, mode: "mouse" },
  { width: 720, mode: "touch" },
  { width: 721, mode: "touch" },
  { width: 1440, mode: "mouse" },
] as const;

for (const { width, mode } of HEAD_SCENES) {
  test.describe(() => {
    test.use({ hasTouch: mode === "touch" });
    test(`width ${width}, ${mode}: every card's first line leads with its state icon and id and ends at the head's right end with the chevron, at the same x on every card — waiting and snoozed cards with the 64px pill before it (an hourglass and one unit, or the clock and MM/DD, the words only in its title), after the buttons with a mouse; the 開卡人 chip and task stay inside the card, their cut lines ending in 「…」`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ReplyDeskStory longAskers questions />);
      expect(await page.evaluate(() => matchMedia("(hover: none)").matches), `control: ${mode}`).toBe(mode === "touch");
      const waiting = cmp.getByTestId("waiting-card");
      await expect(waiting).toHaveCount(9);
      const pills = cmp.getByTestId("waited-pill");
      await expect(pills).toHaveText(["1m", "5m", "40m", "45m", "50m", "55m", "2h", "12d", "30d"]);
      const full = ["已等你 1 分", "已等你 5 分", "已等你 40 分", "已等你 45 分", "已等你 50 分", "已等你 55 分", "已等你 2 小時 57 分", "已等你 12 天 23 小時 30 分", "已等你 30 天"];
      for (const [i, pill] of (await pills.all()).entries()) {
        await expect(pill).toHaveAttribute("title", full[i]);
        await expect(pill).toHaveAttribute("aria-label", full[i]);
      }

      const rights: number[] = [];
      const pillRights: number[] = [];
      const cutByCard = new Map<string, string[]>();
      for (const card of await waiting.all()) {
        const what = `waiting ${await card.getAttribute("data-reply-card-id")}`;
        const { cut, stamp, fold } = await expectHeadLine(card, what, mode);
        const cardBox = await box(card);
        rights.push(fold.right);
        pillRights.push(stamp!.right);
        cutByCard.set((await card.getAttribute("data-reply-card-id")) ?? "", cut);
        if ((await card.getByTestId("reply-task-ref").count()) > 0) await expectTaskRow(card, cardBox);
      }
      expect(await isCut(cardOf(page, "rc-desk-1").locator(".reply-card__task-title")), "control: a task row title is cut").toBe(true);
      if (width <= 390)
        expect(await isCut(cardOf(page, "rc-desk-long-task-row").locator(".reply-card__task-title")), "control: the task row with no due date has its title cut").toBe(true);
      const longTaskCut = cutByCard.get("rc-desk-long-task") ?? [];
      if (width <= 720) expect(longTaskCut, "control: the long task title is cut").toContain(LONG_TASK_TITLE);
      if (width <= 720) expect(longTaskCut.filter((t) => LONG_TASK_LINE.includes(t) && t.length > 20), "control: the long task line is cut").not.toEqual([]);
      if (width === 390 && mode === "touch") {
        expect(cutByCard.get("rc-desk-long-name"), "the long name whole beside the pill").not.toContain("GF Eva Rhapsody Inbox");
        await expect(cardOf(page, "rc-desk-long-name").getByText("GF Eva Rhapsody Inbox", { exact: true })).toBeVisible();
      }
      await noPageOverflow(page);

      await selectTab(page, "snoozed", "click");
      const snoozed = cmp.getByTestId("snoozed-card");
      const until = snoozed.getByTestId("snoozed-pill");
      await expect(until).toHaveText(/^\d{2}\/\d{2}$/);
      await expect(until).toHaveAttribute("title", /回來$/);
      await expect(until.locator('svg polyline[points="12 7 12 12 15 14"]'), "the 稍後再看 clock").toHaveCount(1);
      const snoozedHead = await expectHeadLine(snoozed, "snoozed", mode);
      rights.push(snoozedHead.fold.right);
      pillRights.push(snoozedHead.stamp!.right);

      await selectTab(page, "answered", "click");
      const answered = cardOf(page, "rc-desk-done");
      await expect(answered.locator(".reply-card__answered-at")).toHaveText(/^已回覆 /);
      const answeredHead = await expectHeadLine(answered, "answered", mode);
      expect(answeredHead.stamp, "an answered card has no pill").toBeNull();
      rights.push(answeredHead.fold.right);
      expect(Math.max(...rights) - Math.min(...rights), `chevron right edges ${rights}`).toBeLessThanOrEqual(0.5);
      expect(Math.max(...pillRights) - Math.min(...pillRights), `pill right edges ${pillRights}`).toBeLessThanOrEqual(0.5);
      await noPageOverflow(page);
    });
  });
}

for (const width of [320, 390, 1440]) {
  test.describe(() => {
    test.use({ hasTouch: width <= 720 });
    test(`width ${width}: the 稍後再看 menu is on screen with every preset and the custom row inside it${
      width <= 720 ? " (a bottom sheet, its fields at least 16px so iOS does not zoom in on them)" : ""
    }`, async ({ mount, page }) => {
      await page.setViewportSize({ width, height: 900 });
      const cmp = await mount(<ReplyDeskStory />);
      const card = cmp.getByTestId("waiting-card").first();
      if (width <= 720) {
        // A touch-only screen reaches 稍後再看 through the long-press menu.
        await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
        await page.getByTestId("chat-msg-menu-snooze").tap();
      } else {
        await card.getByTestId("snooze-card").click();
      }
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
    test(`width ${width}, ${language}: the waited stamp is a pill of one width on every card — an hourglass and one unit, the full wait in its title and accessible name — so 跳到原訊息 and 標為過期 sit at the same x on every waiting card; the snoozed card's pill is the 稍後再看 clock and MM/DD, the full return time in its title and name, and fits the same width`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      const cmp = await mount(<ReplyDeskStory />);
      const cards = cmp.getByTestId("waiting-card");
      await expect(cards).toHaveCount(3);
      const pills = cmp.getByTestId("waited-pill");
      await expect(pills).toHaveText(["5m", "2h", "12d"]);
      const full =
        language === "zh"
          ? ["已等你 5 分", "已等你 2 小時 57 分", "已等你 12 天 23 小時 30 分"]
          : ["Waiting 5 minutes", "Waiting 2 hours 57 minutes", "Waiting 12 days 23 hours 30 minutes"];
      for (const [i, pill] of (await pills.all()).entries()) {
        await expect(pill).toHaveAttribute("aria-label", full[i]);
        await expect(pill).toHaveAttribute("title", full[i]);
        await expect(cards.nth(i).getByRole("img", { name: full[i], exact: true })).toBeVisible();
        expect(await contentFits(pill), `${full[i]}: the pill's content inside it`).toBe(true);
        expect((await box(pill)).right - (await box(pill)).left, `${full[i]}: the pill 64px wide`).toBeCloseTo(64, 0);
        const stamp = await box(cards.nth(i).getByTestId("waited"));
        expect(stamp.right - stamp.left, `${full[i]}: the stamp no wider than its pill`).toBeCloseTo(64, 0);
      }
      await expect(cards.locator(".reply-card__waited"), "no words beside the pill").toHaveText(["5m", "2h", "12d"]);

      const label = HEAD_BUTTONS[language as "zh" | "en"];
      const jumpLefts: number[] = [];
      const expireLefts: number[] = [];
      for (const card of await cards.all()) {
        jumpLefts.push((await box(card.getByRole("button", { name: label.jump, exact: true }))).left);
        expireLefts.push((await box(card.getByRole("button", { name: label.expire, exact: true }))).left);
      }
      expect(Math.max(...jumpLefts) - Math.min(...jumpLefts), `跳到原訊息 lefts ${jumpLefts}`).toBeLessThanOrEqual(0.5);
      expect(Math.max(...expireLefts) - Math.min(...expireLefts), `標為過期 lefts ${expireLefts}`).toBeLessThanOrEqual(0.5);

      await selectTab(page, "snoozed", "click");
      const snoozed = cmp.getByTestId("snoozed-card");
      await expect(snoozed).toHaveCount(1);
      const snoozedBox = await box(snoozed);
      const until = snoozed.getByTestId("snoozed-pill");
      await expect(until).toBeVisible();
      await expect(until).toHaveText(/^\d{2}\/\d{2}$/);
      const words = (await until.getAttribute("title")) ?? "";
      expect(words, "control: the full return time").toMatch(language === "zh" ? /^\d{4}\/\d{1,2}\/\d{1,2}（.） \d{2}:\d{2} 回來$/ : /^Back /);
      await expect(until).toHaveAttribute("aria-label", words);
      await expect(until.locator('svg polyline[points="12 7 12 12 15 14"]'), "the 稍後再看 clock").toHaveCount(1);
      expect(inside(await box(until), snoozedBox), "the return date inside the snoozed card").toBe(true);
      expect(await contentFits(until), "MM/DD inside the pill").toBe(true);
      expect((await box(until)).right - (await box(until)).left, "the snoozed pill as wide as the waited ones").toBeCloseTo(64, 0);
      for (const name of [label.jump, label.unsnooze, label.expire]) {
        const button = snoozed.getByRole("button", { name, exact: true });
        expect(inside(await box(button), snoozedBox), `${name} inside the snoozed card`).toBe(true);
        expect(await contentFits(button), `${name}'s icon inside its button`).toBe(true);
      }
      await noPageOverflow(page);
    });
  }
}

test("width 1440: the waited pill is one width whatever the wait — 1m, 11m, 23h, 3d and the widest, 999d — each showing one unit inside it", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const waits = [70, 11 * 60 + 30, 23 * 3600 + 59 * 60, 3 * 86_400 + 4 * 3600, 999 * 86_400 + 3600];
  const cmp = await mount(<ReplyDeskStory waits={waits} />);
  const pillOf = (i: number) => cmp.locator(`[data-reply-card-id="rc-desk-wait-${i}"]`).getByTestId("waited-pill");
  const widths: number[] = [];
  for (const [i, short] of ["1m", "11m", "23h", "3d", "999d"].entries()) {
    const pill = pillOf(i);
    await pill.scrollIntoViewIfNeeded();
    await expect(pill).toHaveText(short);
    expect(await contentFits(pill), `${short} inside its pill`).toBe(true);
    expect(await textLines(pill), `${short} on one line`).toBe(1);
    const b = await box(pill);
    widths.push(b.right - b.left);
  }
  expect(Math.max(...widths) - Math.min(...widths), `pill widths ${widths}`).toBeLessThanOrEqual(0.5);
});

test.describe(() => {
  test.use({ hasTouch: true });
  test("width 1024, touch: a touch tablet draws the same pill as a desktop, no words", async ({ mount, page }) => {
    await page.setViewportSize({ width: 1024, height: 900 });
    const cmp = await mount(<ReplyDeskStory />);
    expect(await page.evaluate(() => matchMedia("(hover: none) and (min-width: 721px)").matches), "control: a touch tablet").toBe(true);
    await expect(cmp.getByTestId("waiting-card")).toHaveCount(3);
    await expect(cmp.getByTestId("waiting-card").locator(".reply-card__waited")).toHaveText(["5m", "2h", "12d"]);
    for (const card of await cmp.getByTestId("waiting-card").all()) await expectHeadLine(card, "tablet", "touch");
    await selectTab(page, "snoozed", "tap");
    await expect(cmp.getByTestId("snoozed-until")).toHaveText(/^\d{2}\/\d{2}$/);
    await expectHeadLine(cmp.getByTestId("snoozed-card"), "tablet snoozed", "touch");
    await noPageOverflow(page);
  });
});

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

for (const width of [320, 390, 1440]) {
  for (const language of ["zh", "en"]) {
    test(`width ${width}, ${language}: an open card, waiting or answered, draws a line under its last meta row and its question starts below it; a folded card has none`, async ({
      mount,
      page,
    }) => {
      await page.evaluate((lang) => localStorage.setItem("oc.language", lang), language);
      await mountDesk(mount, page, width, 900);
      await expect(cardOf(page, "rc-desk-2").locator(".reply-card__divider")).toHaveCount(0);
      const waiting = cardOf(page, "rc-desk-q");
      await waiting.locator(".reply-card__summary").click();
      await expect(waiting.locator(".reply-card__body")).toBeVisible();
      await expectDivider(waiting, waiting.locator(".reply-card__meta"), `${width} ${language} waiting`);

      await selectTab(page, "answered", "click");
      const answered = cardOf(page, "rc-desk-done");
      await expect(answered.locator(".reply-card__divider")).toHaveCount(0);
      await answered.locator(".reply-card__summary").click();
      await expect(answered.locator(".reply-card__body")).toBeVisible();
      await expectDivider(answered, answered.locator(".reply-card__meta"), `${width} ${language} answered`);
      await noPageOverflow(page);
    });
  }
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
  await expect(copy).toHaveAttribute("aria-label", "複製整張卡片內容");
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

    test(`width ${width}, touch: a long press on a card opens the chat's sheet with the head's actions, each with its head icon, in the order 跳到原訊息・稍後再看・標為過期・複製整張卡片內容・選取文字 on a waiting card and 跳到原訊息・複製整張卡片內容・選取文字 on an answered one, never 回覆這則 (the answer box is on the card) — and neither folds the card nor selects its words; an answered card's head draws no buttons`, async ({
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
      await expect(menu.getByRole("menuitem")).toHaveText(["跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容", "選取文字"]);
      await expect(menu.getByTestId("chat-msg-menu-reply")).toHaveCount(0);
      // The head button's own glyph, not a look-alike.
      for (const [key, name] of [["jump", "跳到原訊息"], ["snooze", "稍後再看"], ["expire", "標為過期"]] as const) {
        const glyph = (l: Locator) => l.locator("svg").evaluate((svg) => svg.innerHTML);
        expect(await glyph(menu.getByTestId(`chat-msg-menu-${key}`)), `${name}'s icon`).toBe(
          await glyph(card.getByRole("button", { name, exact: true })),
        );
      }
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

      await selectTab(page, "answered", "tap");
      const answered = cardOf(page, "rc-desk-done");
      await expect(answered).toBeVisible();
      await expectHeadLine(answered, "answered", "touch");
      await touch(page, await centreOf(answered.locator(".reply-card__summary")), { holdMs: 700 });
      await expect(menu.getByRole("menuitem")).toHaveText(["跳到原訊息", "複製整張卡片內容", "選取文字"]);
      await touch(page, { x: 4, y: 4 }, { holdMs: 50 });
      await expect(menu).toHaveCount(0);

      await selectTab(page, "waiting", "tap");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 80 });
      await expect(card).toHaveAttribute("aria-expanded", "true");
      const answer = card.locator(".reply-composer textarea");
      await expect(answer).toBeVisible();
      await expect(answer, "the answer box cannot be selected in by a finger").toHaveCSS("user-select", "text");
    });

    test(`width ${width}, touch: 複製整張卡片內容 from the sheet writes the question and shows nothing once the sheet closes; a failed read shows 複製失敗 on the card at the right end of its first line, over the pill for the moment it shows, the pill and the id keeping their places`, async ({
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
      const idBefore = await box(card.getByTestId("reply-card-id"));
      const pillBefore = await box(card.getByTestId("waited-pill"));
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await page.getByTestId("chat-msg-menu-copy").tap();
      const failed = card.locator(".chat__msg-copy-status--failed");
      await expect(failed).toHaveText("複製失敗");
      await expect(failed).toBeVisible();
      const pillDuring = (await expectHeadLine(card, "a failed copy", "touch")).stamp!;
      const idDuring = await box(card.getByTestId("reply-card-id"));
      expect(idDuring.left, "the id keeps its place while 複製失敗 shows").toBeCloseTo(idBefore.left, 0);
      expect(pillDuring.left, "the pill keeps its place while 複製失敗 shows").toBeCloseTo(pillBefore.left, 0);
      const failedBox = await box(failed);
      expect(overlaps(failedBox, pillDuring), "control: 複製失敗 lies on the pill").toBe(true);
      expect(Math.abs((failedBox.top + failedBox.bottom) / 2 - (pillDuring.top + pillDuring.bottom) / 2), "複製失敗 centred on the pill").toBeLessThanOrEqual(1);
      // Inset past the label's rounded corners, where the pill shows through.
      const failedClip = { x: failedBox.left + 6, y: failedBox.top + 1, width: failedBox.right - failedBox.left - 12, height: failedBox.bottom - failedBox.top - 2 };
      const withPill = await page.screenshot({ clip: failedClip, animations: "disabled" });
      const withoutPill = await page.screenshot({ clip: failedClip, animations: "disabled", style: ".reply-card__waited-pill { visibility: hidden !important; }" });
      expect(withPill.equals(withoutPill), "複製失敗 drawn over the pill, not under it").toBe(true);
      const head = await box(card.locator(".reply-card__head"));
      expect(inside(failedBox, head), "複製失敗 inside the head").toBe(true);
      expect(Math.abs(failedBox.right - head.right), "複製失敗 at the head's right end").toBeLessThanOrEqual(0.5);
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
        ".reply-card__meta",
        "[data-testid='waited']",
        ".reply-card__actions",
        ".reply-card__answer-side",
        ".reply-composer",
      ])
        await expect(card.locator(hidden).first(), `${hidden} in the mode`).toBeHidden();
      for (const other of [cardOf(page, "rc-desk-1"), page.locator(".replies__head")]) {
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
      await selectTab(page, "answered", "tap");
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

    test(`width ${width}, touch: on a waiting secret card the sheet is the same five actions, and the mode never copies the value typed into its masked box`, async ({
      mount,
      page,
      context,
    }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await mountDesk(mount, page, width, height);
      const card = cardOf(page, "rc-desk-secret");
      await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
      await expect(page.getByTestId("chat-msg-menu").getByRole("menuitem")).toHaveText(["跳到原訊息", "稍後再看", "標為過期", "複製整張卡片內容", "選取文字"]);
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

    test(`width ${width}, touch: the sheet's 稍後再看 opens the head's 稍後再看 menu as a bottom sheet with every preset on screen, and a preset moves the card to 稍後再看; there the sheet offers 取消延後, which brings it back; 標為過期 opens the head's confirmation on screen; 跳到原訊息 goes to the chat`, async ({
      mount,
      page,
    }) => {
      await mountDesk(mount, page, width, height);
      const menu = page.getByTestId("chat-msg-menu");
      const press = async (id: string) => {
        await touch(page, await centreOf(cardOf(page, id).locator(".reply-card__summary")), { holdMs: 700 });
        await expect(menu).toBeVisible();
      };
      const view = { left: 0, top: 0, right: width, bottom: height };

      await press("rc-desk-q");
      await page.getByTestId("chat-msg-menu-snooze").tap();
      await expect(menu).toHaveCount(0);
      const sheet = page.getByTestId("snooze-menu-rc-desk-q");
      await expect(sheet).toBeVisible();
      await page.waitForTimeout(300);
      const sheetBox = await box(sheet);
      expect(inside(sheetBox, view), "the 稍後再看 sheet on screen").toBe(true);
      expect(Math.abs(sheetBox.bottom - height), "the 稍後再看 sheet on the screen's bottom edge").toBeLessThanOrEqual(0.5);
      for (const preset of await sheet.locator(".reply-snooze__preset").all())
        expect(inside(await box(preset), sheetBox), `${await preset.textContent()} inside the sheet`).toBe(true);
      await sheet.getByTestId("snooze-preset-1d").tap();
      await expect(sheet).toHaveCount(0);
      await expect(page.locator('[data-testid="waiting-card"][data-reply-card-id="rc-desk-q"]')).toHaveCount(0);

      await selectTab(page, "snoozed", "tap");
      const snoozed = page.locator('[data-testid="snoozed-card"][data-reply-card-id="rc-desk-q"]');
      await expect(snoozed).toHaveCount(1);
      await press("rc-desk-q");
      await expect(menu.getByRole("menuitem")).toHaveText(["跳到原訊息", "取消延後", "標為過期", "複製整張卡片內容", "選取文字"]);
      await page.getByTestId("chat-msg-menu-unsnooze").tap();
      await expect(snoozed).toHaveCount(0);
      await selectTab(page, "waiting", "tap");
      await expect(page.locator('[data-testid="waiting-card"][data-reply-card-id="rc-desk-q"]')).toHaveCount(1);

      await press("rc-desk-1");
      await page.getByTestId("chat-msg-menu-expire").tap();
      const confirm = page.getByTestId("expire-confirm-btn");
      await expect(confirm).toBeVisible();
      expect(inside(await box(confirm), view), "標為過期's confirm button on screen").toBe(true);
      await confirm.tap();
      await expect(page.locator('[data-testid="waiting-card"][data-reply-card-id="rc-desk-1"]')).toHaveCount(0);

      await press("rc-desk-q");
      await page.getByTestId("chat-msg-menu-jump").tap();
      await expect.poll(() => page.evaluate(() => location.hash)).toMatch(/^#office\/chat\//);
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
  test("width 390, touch: a card in the mode that is snoozed from elsewhere stays under 等待回覆 as a snoozed card at the end of the list and in the mode, brought up above the bar", async ({
    mount,
    page,
  }) => {
    await mountDesk(mount, page, 390, 844);
    await expect(page.getByTestId("snoozed-card")).toHaveCount(0);
    const card = cardOf(page, "rc-desk-q");
    await touch(page, await centreOf(card.locator(".reply-card__summary")), { holdMs: 700 });
    await page.getByTestId("chat-msg-menu-select").tap();
    const bar = page.getByTestId("chat-select-bar");
    await expect(bar).toBeFocused();

    await page.evaluate(() =>
      (window as unknown as { __ocSnooze: (id: string, untilTs: number) => void }).__ocSnooze(
        "rc-desk-q",
        Date.now() / 1000 + 10 * 86_400,
      ),
    );
    await expect(page.getByTestId("snoozed-card")).toHaveCount(1);
    await expect(page.getByTestId("replies-tab-waiting")).toHaveAttribute("aria-selected", "true");
    const moved = page.locator('[data-testid="snoozed-card"][data-reply-card-id="rc-desk-q"]');
    await expect(moved).toHaveClass(/reply-card--selecting/);
    await expect(bar).toBeVisible();
    await page.waitForTimeout(300);
    const barTop = (await bar.boundingBox())!.y;
    const listTop = (await page.locator(".replies").boundingBox())!.y;
    const title = (await moved.locator(".reply-card__summary").boundingBox())!;
    expect(title.y, "the moved card's title is above the page").toBeGreaterThanOrEqual(listTop - 0.5);
    expect(title.y + title.height, "the moved card's title is under the bar").toBeLessThanOrEqual(barTop + 0.5);
  });
});
