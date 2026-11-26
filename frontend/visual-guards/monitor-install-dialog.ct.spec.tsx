// 安裝機器 dialog: every command, the install command included, is readable on
// screen (it wraps inside its box rather than running off the edge), and every
// OS tab's steps fit the dialog at desktop and phone widths; a dialog taller than a phone screen
// scrolls inside itself instead of losing its top and bottom off screen. The
// command rows all share one layout (aligned boxes, one icon-only button size),
// and an expired command, or one that could not be fetched, keeps that layout.
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorInstallDialogStory } from "./stories/MonitorInstallDialogStory";

const OSES = ["mac", "linux", "windows"] as const;

for (const [width, height] of [[1280, 900], [375, 667]]) {
  test(`at ${width}px every OS tab fits the dialog and every command is shown whole`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height });
    await mount(<MonitorInstallDialogStory />);
    const cmd = page.getByTestId("mon-boot-cmd");
    await expect(cmd).toContainText("install.sh?code=");

    for (const os of OSES) {
      await page.getByTestId(`mon-install-tab-${os}`).click();
      const m = await page.evaluate(() => {
        const box = document.querySelector(".mon-install__box") as HTMLElement;
        const b = box.getBoundingClientRect();
        const overflowing = [...box.querySelectorAll<HTMLElement>("*")].filter((el) => {
          const r = el.getBoundingClientRect();
          return r.right > b.right + 0.5 || r.left < b.left - 0.5;
        }).length;
        return {
          overflowing,
          codeScrolls: [...box.querySelectorAll<HTMLElement>("code")].some(
            (code) => code.scrollWidth > code.clientWidth + 1
          ),
          pageScrolls: document.documentElement.scrollWidth > window.innerWidth,
          inView: b.top >= 0 && b.bottom <= window.innerHeight,
        };
      });
      expect(m.overflowing, `${os}: nothing pokes out of the dialog box`).toBe(0);
      expect(m.codeScrolls, `${os}: every command wraps, no hidden part`).toBe(false);
      expect(m.pageScrolls, `${os}: no page-level horizontal scroll`).toBe(false);
      expect(m.inView, `${os}: the dialog box stays inside the screen`).toBe(true);
    }
  });
}

// Every command row looks the same: on every OS tab and at both widths the
// command boxes share one left and one right edge (the WSL step's box and the
// install command's box included), and every button beside them is the same
// icon-only square.
type Rect = { left: number; right: number; width: number; height: number };
async function rows(page: import("@playwright/test").Page) {
  return page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>(".mon-install__cmd-body")].map((body) => {
      const r = (el: Element) => {
        const b = el.getBoundingClientRect();
        return { left: b.left, right: b.right, width: b.width, height: b.height };
      };
      const button = body.querySelector("button") as HTMLButtonElement;
      return {
        box: r(body.querySelector("code") as HTMLElement),
        button: r(button),
        buttonText: button.innerText.trim(),
        buttonName: button.getAttribute("aria-label"),
        hasIcon: button.querySelector("svg") != null,
      };
    })
  );
}
const near = (a: number, b: number) => Math.abs(a - b) <= 0.5;

for (const [width, height] of [[1280, 900], [390, 844]]) {
  test(`at ${width}px every command box lines up and every button is the same icon`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height });
    await mount(<MonitorInstallDialogStory />);
    await expect(page.getByTestId("mon-boot-cmd")).toContainText("install.sh?code=");

    let first: { box: Rect; button: Rect } | null = null;
    for (const os of OSES) {
      await page.getByTestId(`mon-install-tab-${os}`).click();
      const found = await rows(page);
      expect(found.length, `${os}: command rows`).toBe(os === "windows" ? 2 : 1);
      for (const row of found) {
        first ??= row;
        expect(near(row.box.left, first.box.left), `${os}: box left edge`).toBe(true);
        expect(near(row.box.right, first.box.right), `${os}: box right edge`).toBe(true);
        expect(near(row.button.width, first.button.width), `${os}: button width`).toBe(true);
        expect(near(row.button.height, first.button.height), `${os}: button height`).toBe(true);
        expect(near(row.button.right, first.button.right), `${os}: button right edge`).toBe(true);
        expect(row.box.right <= row.button.left, `${os}: button sits beside the box`).toBe(true);
        expect(row.buttonText, `${os}: button shows no text`).toBe("");
        expect(row.buttonName, `${os}: button is named 複製`).toBe("複製");
        expect(row.hasIcon, `${os}: button shows an icon`).toBe(true);
      }
    }
  });
}

test("an expired command keeps the same box and button slot, says so under the box, and regenerates back to a copy icon", async ({
  mount,
  page,
}) => {
  await page.clock.install();
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<MonitorInstallDialogStory />);
  const cmd = page.getByTestId("mon-boot-cmd");
  await expect(cmd).toContainText("install.sh?code=");
  const before = (await rows(page))[0];

  await page.clock.fastForward(600_000);

  const button = page.getByTestId("mon-copy-boot-cmd-btn");
  await expect(button).toHaveAttribute("aria-label", "重新產生指令");
  await expect(page.getByText("指令已過期，請重新產生。")).toBeVisible();
  const after = (await rows(page))[0];
  expect(near(after.box.left, before.box.left) && near(after.box.right, before.box.right)).toBe(true);
  expect(near(after.button.width, before.button.width)).toBe(true);
  expect(after.buttonText).toBe("");
  const msgBelow = await page.evaluate(() => {
    const code = document.querySelector('[data-testid="mon-boot-cmd"]')!.getBoundingClientRect();
    const msg = [...document.querySelectorAll(".mon-install__cmd-msg")].find(
      (el) => el.textContent !== ""
    )!.getBoundingClientRect();
    return msg.top >= code.bottom && near(msg.left, code.left);
    function near(a: number, b: number) {
      return Math.abs(a - b) <= 0.5;
    }
  });
  expect(msgBelow, "the expired line sits under the box").toBe(true);

  const oldText = await cmd.textContent();
  await button.click();
  await expect(button).toHaveAttribute("aria-label", "複製");
  await expect(cmd).not.toHaveText(oldText ?? "");
  await expect(page.getByText("指令已過期，請重新產生。")).toHaveCount(0);
});

test("when the command cannot be fetched, the empty box keeps its place and the button's height", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<MonitorInstallDialogStory fetchFails />);
  await expect(page.getByText("取得指令失敗")).toBeVisible();
  await expect(page.getByTestId("mon-copy-boot-cmd-btn")).toHaveAttribute(
    "aria-label",
    "重新產生指令"
  );
  await page.getByTestId("mon-install-tab-windows").click();
  const [wsl, boot] = await rows(page);
  expect(near(boot.box.left, wsl.box.left), "empty box left edge").toBe(true);
  expect(near(boot.box.right, wsl.box.right), "empty box right edge").toBe(true);
  expect(near(boot.box.height, boot.button.height), "empty box as tall as its button").toBe(true);
  expect(near(boot.button.width, wsl.button.width), "button width").toBe(true);
});

test("after a copy, 已複製 is read out under the box but not shown", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<MonitorInstallDialogStory />);
  await expect(page.getByTestId("mon-boot-cmd")).toContainText("install.sh?code=");
  // CT has no clipboard permission; a resolving stub stands in for a copy that works.
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: () => Promise.resolve() },
      configurable: true,
    });
  });
  const noteTop = () =>
    page.evaluate(() => document.querySelector(".mon-install__note")!.getBoundingClientRect().top);
  const before = await noteTop();
  await page.getByTestId("mon-copy-boot-cmd-btn").click();
  await expect(page.getByTestId("mon-copy-boot-cmd-btn")).toHaveAttribute("aria-label", "已複製");

  const said = page.getByRole("status").filter({ hasText: "已複製" });
  await expect(said).toHaveCount(1);
  const shown = await said.locator("span").evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { width: r.width, height: r.height, clip: getComputedStyle(el).clipPath };
  });
  expect(shown.width <= 1 && shown.height <= 1, "the words take no visible room").toBe(true);
  expect(shown.clip, "the words are clipped away").toBe("inset(50%)");
  expect(near(await noteTop(), before), "nothing under the box moves down").toBe(true);
});
