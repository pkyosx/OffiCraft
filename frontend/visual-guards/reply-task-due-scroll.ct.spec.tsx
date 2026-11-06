import { expect, test } from "@playwright/experimental-ct-react";
import { ReplyTaskDueScrollStory } from "./stories/ReplyTaskDueScrollStory";
import { TaskCardDueStory } from "./stories/TaskCardDueStory";

for (const viewport of [
  { width: 1440, height: 790 },
  { width: 390, height: 844 },
]) {
  test(`${viewport.width}x${viewport.height}: due-soon reply tasks keep scrolling inside the card list`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize(viewport);
    const cmp = await mount(<ReplyTaskDueScrollStory />);
    const sizes = await page.evaluate(() => {
      const page = document.scrollingElement!;
      const replies = document.querySelector('[data-testid="reply-scroll"]') as HTMLElement;
      return {
        pageHeight: page.scrollHeight,
        pageClientHeight: page.clientHeight,
        repliesHeight: replies.scrollHeight,
        repliesClientHeight: replies.clientHeight,
        repliesOverflowY: getComputedStyle(replies).overflowY,
      };
    });

    expect(sizes.pageHeight, "the document must not scroll").toBe(
      sizes.pageClientHeight
    );
    expect(sizes.repliesOverflowY, "the reply list owns vertical scrolling").toMatch(
      /auto|scroll/
    );
    expect(sizes.repliesHeight, "the reply list contains more cards than fit").toBeGreaterThan(
      sizes.repliesClientHeight
    );
    await cmp.getByTestId("reply-scroll").evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    expect(
      await cmp.getByTestId("reply-scroll").evaluate((el) => el.scrollTop),
      "the reply list can scroll independently"
    ).toBeGreaterThan(0);

    const dueStatus = cmp
      .getByTestId("reply-task-due-1")
      .locator(".task-card__due-status");
    await expect(dueStatus).toHaveText(/\S/);
    await expect(dueStatus).not.toHaveAttribute("aria-hidden", "true");
  });
}

test("the task page keeps its own due-date display and positioning", async ({
  mount,
}) => {
  const dueTs = Math.floor(Date.now() / 1000) + 60 * 60;
  const cmp = await mount(
    <TaskCardDueStory dueTs={dueTs} dueSoonLeadMinutes={2 * 60} />
  );
  const dueDate = cmp.getByTestId("task-due");
  const dueStatus = cmp.getByTestId("task-due-alert");

  await expect(dueDate).toBeVisible();
  await expect(dueStatus).toHaveText("快到期");
  const describedBy = await dueDate.getAttribute("aria-describedby");
  expect(describedBy?.split(/\s+/)).toContain(await dueStatus.getAttribute("id"));
  expect(
    await cmp.locator(".task-card__due").evaluate((el) => getComputedStyle(el).position)
  ).toBe("relative");
  expect(
    await dueDate.evaluate((el) => getComputedStyle(el).position)
  ).toBe("static");
});
