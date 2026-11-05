// GUARD — the chat composer's file input is the anchor iOS hangs its
// photo/camera/file menu on, so it needs a real on-screen box above the input
// row while staying out of the way of pointer, keyboard and screen reader.
// Where the menu finally lands is UIKit's decision and only a real iPhone shows
// it; this measures the box handed to UIKit.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatMentionStory } from "./stories/ChatMentionStory";

const INPUT = ".chat__composer-row input.chat__file-input";

for (const width of [390, 430, 1440]) {
  test(`width ${width}: the file input has a transparent box above the row and clicks at its center and on the row controls reach what is underneath`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const cmp = await mount(<ChatMentionStory withRoster={width > 720} />);
    await expect(cmp.locator("textarea.chat__input")).toBeVisible();

    const geo = await page.evaluate((inputSelector) => {
      const box = (sel: string) => {
        const r = document.querySelector(sel)!.getBoundingClientRect();
        return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, w: r.width, h: r.height };
      };
      const centerHit = (sel: string) => {
        const r = document.querySelector(sel)!.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)!;
        return hit.closest(".chat__attach, .chat__input, .chat__send, .chat__file-input")?.className ?? hit.className;
      };
      const input = document.querySelector(inputSelector)!;
      return {
        input: box(inputSelector),
        row: box(".chat__composer-row"),
        attach: box(".chat__attach"),
        vw: document.documentElement.clientWidth,
        vh: window.innerHeight,
        opacity: getComputedStyle(input).opacity,
        pageOverflow: document.scrollingElement!.scrollWidth - document.scrollingElement!.clientWidth,
        hitAtInput: centerHit(inputSelector),
        hitAtAttach: centerHit(".chat__attach"),
        hitAtTextarea: centerHit(".chat__input"),
        hitAtSend: centerHit(".chat__send"),
      };
    }, INPUT);

    expect(geo.input.w).toBeGreaterThan(0);
    expect(geo.input.h).toBeGreaterThan(0);
    expect(geo.input.bottom).toBe(geo.row.top);
    expect(geo.input.left).toBe(geo.attach.left);
    expect(geo.input.left).toBeGreaterThanOrEqual(0);
    expect(geo.input.right).toBeLessThanOrEqual(geo.vw);
    expect(geo.input.top).toBeGreaterThanOrEqual(0);
    expect(geo.input.bottom).toBeLessThanOrEqual(geo.vh);
    expect(geo.opacity).toBe("0");
    expect(geo.pageOverflow).toBe(0);
    expect(geo.hitAtInput).toBe("chat__composer");
    expect(geo.hitAtAttach).toBe("chat__attach");
    expect(geo.hitAtTextarea).toBe("chat__input");
    expect(geo.hitAtSend).toBe("chat__send");
  });
}

test("the attach button opens a multi-file chooser and the picked file is staged for preview", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mount(<ChatMentionStory withRoster={false} />);

  const chooserPromise = page.waitForEvent("filechooser");
  await cmp.locator(".chat__attach").click();
  const chooser = await chooserPromise;
  expect(chooser.isMultiple()).toBe(true);

  await chooser.setFiles({ name: "note.txt", mimeType: "text/plain", buffer: Buffer.from("hello") });
  await expect(cmp.locator(".chat__preview-file-name")).toContainText("note.txt");
  await expect(cmp.locator(".chat__send")).toBeEnabled();
});

test("keyboard moves from the attach button to the textarea and the screen reader lists the attach button, the textarea and the send button", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mount(<ChatMentionStory withRoster={false} />);
  await cmp.locator("textarea.chat__input").fill("draft");

  const activeInRow = () =>
    page.evaluate(() => {
      const el = document.activeElement as HTMLElement;
      return el.closest(".chat__composer-row") ? el.className : "outside-row";
    });

  await cmp.locator(".chat__attach").focus();
  await page.keyboard.press("Tab");
  expect(await activeInRow()).toBe("chat__input");
  await page.keyboard.press("Shift+Tab");
  expect(await activeInRow()).toBe("chat__attach");
  await page.keyboard.press("Shift+Tab");
  expect(await activeInRow()).toBe("outside-row");

  await expect(cmp.locator(".chat__composer-row")).toMatchAriaSnapshot(`
    - /children: equal
    - button "附加檔案":
      - img
    - textbox "回覆 Cara…": draft
    - button "送出":
      - img
  `);
});
