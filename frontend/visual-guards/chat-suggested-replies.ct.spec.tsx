// The chat room's 建議回覆 row is mounted in the real ChatArea here, so what is
// measured is the composer it hangs under, at the widths the owner signed off.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatSuggestedRepliesStory } from "./stories/ChatSuggestedRepliesStory";

const LONG = "回報時只講結果、影響和要我決定的事，實作細節留在任務紀錄，".repeat(4).slice(0, 120);
const MANY = Array.from({ length: 20 }, (_, i) => `第 ${i + 1} 句常用的話`);

for (const width of [390, 1280]) {
  test(`${width}px: a 120-character sentence wraps inside the composer, below the input row`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={["繼續", LONG]} />);

    const composer = (await cmp.locator(".chat__composer").boundingBox())!;
    const inputRow = (await cmp.locator(".chat__composer-row").boundingBox())!;
    const row = (await cmp.getByTestId("chat-suggested-replies").boundingBox())!;
    const chip = (await cmp.locator(".suggested-replies__chip").nth(1).boundingBox())!;

    expect(row.y).toBeGreaterThanOrEqual(inputRow.y + inputRow.height);
    expect(chip.x + chip.width).toBeLessThanOrEqual(composer.x + composer.width + 1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    if (width === 390) expect(chip.height).toBeGreaterThan(30);
  });

  test(`${width}px: 20 sentences stop at the row's height cap and scroll inside it, leaving the messages their space`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={MANY} />);

    const row = cmp.getByTestId("chat-suggested-replies");
    const box = (await row.boundingBox())!;
    expect(box.height).toBeLessThanOrEqual(132);
    if (width === 390) {
      const overflow = await row.evaluate((el) => el.scrollHeight > el.clientHeight);
      expect(overflow).toBe(true);
    }
    const thread = (await cmp.locator(".chat__body").boundingBox())!;
    expect(thread.height).toBeGreaterThan(200);
  });

  test(`${width}px: the dialog fits the viewport and every row's three buttons are on screen`, async ({ mount, page }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ChatSuggestedRepliesStory replies={MANY.slice(0, 4)} />);
    await cmp.getByTestId("chat-header-suggested-replies").click();

    const dialog = (await page.getByTestId("chat-suggested-replies-dialog").boundingBox())!;
    expect(dialog.x).toBeGreaterThanOrEqual(0);
    expect(dialog.x + dialog.width).toBeLessThanOrEqual(width);
    const input = (await page.locator(".sugg-edit__input").first().boundingBox())!;
    expect(input.width).toBeGreaterThan(120);
    const remove = (await page.locator(".sugg-edit__btn--remove").first().boundingBox())!;
    expect(remove.x + remove.width).toBeLessThanOrEqual(dialog.x + dialog.width);
  });
}

async function headerBoxes(cmp: import("@playwright/test").Locator) {
  const box = async (sel: string) => (await cmp.locator(sel).boundingBox())!;
  return {
    header: await box(".chat__header"),
    name: await box(".chat__header-text"),
    top: [
      await box("[data-testid=chat-header-tasks]"),
      await box("[data-testid=chat-header-role-settings]"),
      await box("[data-testid=chat-header-suggested-replies]"),
    ],
    bottom: [await box("[data-testid=chat-header-search]"), await box(".chat__gallery-toggle")],
  };
}

const inside = (b: { x: number; y: number; width: number; height: number }, h: typeof b) => {
  expect(b.x).toBeGreaterThanOrEqual(h.x);
  expect(b.x + b.width).toBeLessThanOrEqual(h.x + h.width);
  expect(b.y).toBeGreaterThanOrEqual(h.y);
  expect(b.y + b.height).toBeLessThanOrEqual(h.y + h.height);
};

test("390px: the tasks, role and suggested-replies buttons stack over search and gallery at the right, leaving a long name whole without growing the header", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const name = "GF Eva Slack Inbox";
  const bare = await mount(<ChatSuggestedRepliesStory name={name} />);
  const bareHeight = (await bare.locator(".chat__header").boundingBox())!.height;
  await bare.unmount();

  const cmp = await mount(<ChatSuggestedRepliesStory replies={["繼續"]} allHeaderButtons name={name} />);
  const { header, name: text, top, bottom } = await headerBoxes(cmp);
  [...top, ...bottom].forEach((b) => {
    inside(b, header);
    expect(b.width).toBeGreaterThanOrEqual(30);
    expect(b.height).toBeGreaterThanOrEqual(30);
  });
  for (const row of [top, bottom]) {
    row.forEach((b, i) => {
      expect(b.y).toBeCloseTo(row[0].y, 0);
      if (i > 0) expect(b.x).toBeGreaterThanOrEqual(row[i - 1].x + row[i - 1].width);
    });
  }
  expect(bottom[0].y).toBeGreaterThanOrEqual(top[0].y + top[0].height);
  expect(top[2].x + top[2].width).toBeCloseTo(bottom[1].x + bottom[1].width, 0);
  expect(text.x + text.width).toBeLessThanOrEqual(top[0].x);
  const nameSpan = cmp.locator(".chat__header-name > span:last-child");
  expect(await nameSpan.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  expect(header.height).toBeCloseTo(bareHeight, 0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test("390px: with only search and gallery, the two sit in one row at the header's right edge", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory name="GF Eva Slack Inbox" />);
  const header = (await cmp.locator(".chat__header").boundingBox())!;
  const search = (await cmp.getByTestId("chat-header-search").boundingBox())!;
  const gallery = (await cmp.locator(".chat__gallery-toggle").boundingBox())!;
  inside(search, header);
  inside(gallery, header);
  expect(search.y).toBeCloseTo(gallery.y, 0);
  expect(gallery.x).toBeGreaterThanOrEqual(search.x + search.width);
  expect(header.x + header.width - (gallery.x + gallery.width)).toBeLessThanOrEqual(12);
});

test("1280px: all five header buttons stay in one row", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["繼續"]} allHeaderButtons name="GF Eva Slack Inbox" />);
  const { header, top, bottom } = await headerBoxes(cmp);
  const row = [...top, ...bottom];
  row.forEach((b, i) => {
    inside(b, header);
    expect(b.y).toBeCloseTo(row[0].y, 0);
    if (i > 0) expect(b.x).toBeGreaterThanOrEqual(row[i - 1].x + row[i - 1].width);
  });
});

// Focus order across a blur, a save and a click only exists in a real browser;
// jsdom never moves focus to what was clicked.
test("emptying a row and clicking the row below it edits the sentence clicked", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙", "丁"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(1).fill("");
  await inputs.nth(2).click();

  await expect(inputs.nth(2)).toBeFocused();
  await page.keyboard.press("End");
  await page.keyboard.type("x");
  await expect(inputs.nth(2)).toHaveValue("丙x");

  await page.locator("#chat-suggested-replies-title").click();
  await expect(inputs).toHaveCount(3);
  expect(await inputs.evaluateAll((els) => els.map((el) => (el as HTMLInputElement).value))).toEqual(["甲", "丙x", "丁"]);
});

test("emptying a row and pressing delete on the row below it deletes the sentence pressed", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙", "丁"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(1).fill("");
  await page.locator(".sugg-edit__btn--remove").nth(2).click();

  await expect(inputs).toHaveCount(2);
  expect(await inputs.evaluateAll((els) => els.map((el) => (el as HTMLInputElement).value))).toEqual(["甲", "丁"]);
});

test("editing a row and moving to another row saves at once when no row is blank", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(0).fill("甲改");
  await inputs.nth(1).click();

  await expect(cmp.locator(".suggested-replies__chip").first()).toHaveText("甲改");
});

test("a row edited before pressing add is saved when Esc closes the dialog", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__add").click();
  await page.locator(".sugg-edit__input").nth(0).fill("甲改");
  await page.locator(".sugg-edit__add").click();
  await page.keyboard.press("Escape");

  await expect(cmp.locator(".suggested-replies__chip")).toHaveText(["甲改", "乙"]);
});

test("a row edited before pressing add is saved when blank dialog space is clicked, and the blank rows go", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__add").click();
  await page.locator(".sugg-edit__input").nth(0).fill("甲改");
  await page.locator(".sugg-edit__add").click();
  await page.locator("#chat-suggested-replies-title").click();

  await expect(page.locator(".sugg-edit__input")).toHaveCount(2);
  await expect(cmp.locator(".suggested-replies__chip")).toHaveText(["甲改", "乙"]);
});

test("a row emptied before tabbing to its buttons is removed when Esc closes the dialog", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__input").nth(1).fill("");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Escape");

  await expect(cmp.locator(".suggested-replies__chip")).toHaveText(["甲", "丙"]);
});

test("a row emptied before pressing add is removed when blank dialog space is clicked", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙", "丁"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__input").nth(1).fill("");
  await page.locator(".sugg-edit__add").click();
  await page.locator("#chat-suggested-replies-title").click();

  await expect(page.locator(".sugg-edit__input")).toHaveCount(3);
  await expect(cmp.locator(".suggested-replies__chip")).toHaveText(["甲", "丙", "丁"]);
});

test("pressing add puts the cursor on the new row", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__add").click();
  const inputs = page.locator(".sugg-edit__input");

  await expect(inputs).toHaveCount(2);
  await expect(inputs.nth(1)).toBeFocused();
  await page.keyboard.type("乙");
  await page.keyboard.press("Escape");
  await expect(cmp.locator(".suggested-replies__chip")).toHaveText(["甲", "乙"]);
});

test("a row edited before the add that fills the list to twenty is saved when Esc closes the dialog", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const nineteen = Array.from({ length: 19 }, (_, i) => `句${i + 1}`);
  const cmp = await mount(<ChatSuggestedRepliesStory replies={nineteen} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__input").nth(0).fill("改");
  await page.locator(".sugg-edit__add").click();
  await expect(page.locator(".sugg-edit__input")).toHaveCount(20);
  await expect(page.locator(".sugg-edit__input").nth(19)).toBeFocused();
  await page.keyboard.press("Escape");

  await expect(cmp.locator(".suggested-replies__chip").first()).toHaveText("改");
  await expect(cmp.locator(".suggested-replies__chip")).toHaveCount(19);
});

test("a row of only spaces waits like an empty one, so clicking the row below edits the sentence clicked", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(1).fill("   ");
  await inputs.nth(2).click();
  await page.keyboard.press("End");
  await page.keyboard.type("x");
  await page.locator("#chat-suggested-replies-title").click();

  expect(await inputs.evaluateAll((els) => els.map((el) => (el as HTMLInputElement).value))).toEqual(["甲", "丙x"]);
});

test("after pressing add, typing in another row stays in that row", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  await page.locator(".sugg-edit__add").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(0).click();
  await page.keyboard.press("End");
  await page.keyboard.type("xy");

  await expect(inputs.nth(0)).toBeFocused();
  await expect(inputs.nth(0)).toHaveValue("甲xy");
});

test("the dialog is 520px wide on a desktop", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1440, height: 790 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const dialog = (await page.getByTestId("chat-suggested-replies-dialog").boundingBox())!;
  expect(dialog.width).toBe(520);
});

test("emptying the bottom row and clicking blank dialog space leaves nothing focused", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const cmp = await mount(<ChatSuggestedRepliesStory replies={["甲", "乙", "丙"]} />);
  await cmp.getByTestId("chat-header-suggested-replies").click();
  const inputs = page.locator(".sugg-edit__input");
  await inputs.nth(2).fill("");
  await page.locator("#chat-suggested-replies-title").click();

  await expect(inputs).toHaveCount(2);
  await expect(page.locator("body")).toBeFocused();
});
