// GUARD — the composers' @mention picker stays inside its pane.
//
// jsdom pins the picker's behaviour (ChatArea.mention.test.tsx). What only a
// real browser can answer: with a task title far longer than any phone row, is
// the list still fully on screen, bounded by the composer, tappable, and does
// it leave the page without a horizontal scroll? All comparisons are relative
// to the composer / viewport — CI fonts differ from a dev Mac.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatMentionStory } from "./stories/ChatMentionStory";
import { TaskCardMentionStory, ReplyCardMentionStory } from "./stories/ComposerMentionStory";

for (const width of [390, 1280]) {
  test(`width ${width}: the open @mention list fits the composer and the page does not scroll sideways`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const cmp = await mount(<ChatMentionStory withRoster={width > 720} />);
    const input = cmp.locator("textarea.chat__input");
    await input.click();
    await input.pressSequentially("@");
    const list = cmp.getByRole("listbox");
    await expect(list).toBeVisible();
    await expect(list.getByRole("option")).toHaveCount(3);

    const geo = await page.evaluate(() => {
      const r = (sel: string) => document.querySelector(sel)!.getBoundingClientRect();
      const title = document.querySelectorAll(".chat-mention__meta");
      const longTitle = title[title.length - 1] as HTMLElement;
      return {
        list: r(".chat-mention"),
        row: r(".chat__composer-row"),
        chat: r(".chat"),
        option: r('.chat-mention [role="option"]:last-child'),
        vw: document.documentElement.clientWidth,
        vh: window.innerHeight,
        pageOverflow:
          document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
        listOverflowX:
          document.querySelector(".chat-mention")!.scrollWidth -
          document.querySelector(".chat-mention")!.clientWidth,
        titleClipped: longTitle.scrollWidth > longTitle.clientWidth,
        titleRight: longTitle.getBoundingClientRect().right,
      };
    });

    expect(geo.pageOverflow, "page has no horizontal overflow").toBeLessThanOrEqual(1);
    expect(geo.list.left, "list starts inside the composer row").toBeGreaterThanOrEqual(geo.row.left - 1);
    expect(geo.list.right, "list ends inside the composer row").toBeLessThanOrEqual(geo.row.right + 1);
    expect(geo.list.right, "list ends inside the viewport").toBeLessThanOrEqual(geo.vw);
    expect(geo.list.top, "list top is on screen").toBeGreaterThanOrEqual(0);
    expect(geo.list.top, "list sits inside the chat pane").toBeGreaterThanOrEqual(geo.chat.top - 1);
    expect(geo.list.bottom, "list is above the composer row").toBeLessThanOrEqual(geo.row.top + 1);
    expect(geo.listOverflowX, "list itself does not scroll sideways").toBeLessThanOrEqual(1);
    expect(geo.titleClipped, "long task title is ellipsized, not wrapped wide").toBe(true);
    expect(geo.titleRight, "task title stays inside the list").toBeLessThanOrEqual(geo.list.right + 1);

    // Tappable: the point at the row's centre hits the row itself, nothing on top.
    const hit = await page.evaluate(
      ([x, y]) => document.elementFromPoint(x, y)?.closest('[role="option"]')?.textContent ?? null,
      [geo.option.left + geo.option.width / 2, geo.option.top + geo.option.height / 2],
    );
    expect(hit).toContain("T-492");
    await page.mouse.click(geo.option.left + geo.option.width / 2, geo.option.top + geo.option.height / 2);
    await expect(input).toHaveValue(/^@.+ · O-7 $/);
    await expect(list).toHaveCount(0);
  });
}

// The task card's message box and the reply card's answer box sit mid-page in
// a scroller, so their list opens downward (see ChatMentionList `placement`).
const midPageComposers = [
  {
    name: "task card message box",
    story: () => <TaskCardMentionStory />,
    input: '[data-testid="task-msg-input"]',
    row: ".task-card__composer",
    scroller: ".tasks",
  },
  {
    name: "reply card answer box",
    story: () => <ReplyCardMentionStory />,
    input: ".reply-composer textarea.chat__input",
    row: ".reply-composer .chat__composer-row",
    scroller: ".replies",
  },
];

for (const c of midPageComposers) {
  for (const width of [390, 1280]) {
    test(`${c.name}, width ${width}: the open @mention list sits under the box, inside its row and the viewport, and the page does not scroll sideways`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      const cmp = await mount(c.story());
      const input = cmp.locator(c.input);
      await input.click();
      await input.pressSequentially("@");
      const list = cmp.getByRole("listbox");
      await expect(list).toBeVisible();
      await expect(list.getByRole("option")).toHaveCount(3);

      const geo = await page.evaluate(
        ([rowSel, scrollerSel]) => {
          const r = (sel: string) => document.querySelector(sel)!.getBoundingClientRect();
          const el = document.querySelector(".chat-mention")!;
          const titles = document.querySelectorAll(".chat-mention__meta");
          const longTitle = titles[titles.length - 1] as HTMLElement;
          const scroller = document.querySelector(scrollerSel)!;
          return {
            list: r(".chat-mention"),
            row: r(rowSel),
            option: r('.chat-mention [role="option"]:last-child'),
            vw: document.documentElement.clientWidth,
            vh: window.innerHeight,
            pageOverflow:
              document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
            scrollerOverflowX: scroller.scrollWidth - scroller.clientWidth,
            listOverflowX: el.scrollWidth - el.clientWidth,
            titleClipped: longTitle.scrollWidth > longTitle.clientWidth,
            titleRight: longTitle.getBoundingClientRect().right,
          };
        },
        [c.row, c.scroller],
      );

      expect(geo.pageOverflow, "page has no horizontal overflow").toBeLessThanOrEqual(1);
      expect(geo.scrollerOverflowX, "page scroller has no horizontal overflow").toBeLessThanOrEqual(1);
      expect(geo.list.left, "list starts inside the composer row").toBeGreaterThanOrEqual(geo.row.left - 1);
      expect(geo.list.right, "list ends inside the composer row").toBeLessThanOrEqual(geo.row.right + 1);
      expect(geo.list.right, "list ends inside the viewport").toBeLessThanOrEqual(geo.vw);
      expect(geo.list.top, "list is under the composer row").toBeGreaterThanOrEqual(geo.row.bottom - 1);
      expect(geo.list.bottom, "list bottom is on screen").toBeLessThanOrEqual(geo.vh + 1);
      expect(geo.listOverflowX, "list itself does not scroll sideways").toBeLessThanOrEqual(1);
      expect(geo.titleClipped, "long task title is ellipsized, not wrapped wide").toBe(true);
      expect(geo.titleRight, "task title stays inside the list").toBeLessThanOrEqual(geo.list.right + 1);

      const hit = await page.evaluate(
        ([x, y]) => document.elementFromPoint(x, y)?.closest('[role="option"]')?.textContent ?? null,
        [geo.option.left + geo.option.width / 2, geo.option.top + geo.option.height / 2],
      );
      expect(hit).toContain("T-492");
      await page.mouse.click(geo.option.left + geo.option.width / 2, geo.option.top + geo.option.height / 2);
      await expect(input).toHaveValue(/^@.+ · O-7 $/);
      await expect(list).toHaveCount(0);
    });
  }
}
