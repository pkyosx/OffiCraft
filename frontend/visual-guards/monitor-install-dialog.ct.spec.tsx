// 安裝機器 dialog: every command, the install command included, is readable on
// screen (it wraps inside its box rather than running off the edge), and every
// OS and AI tool tab's steps fit the dialog at desktop and phone widths; a dialog taller than a phone screen
// scrolls inside itself instead of losing its top and bottom off screen.
import { test, expect } from "@playwright/experimental-ct-react";
import { MonitorInstallDialogStory } from "./stories/MonitorInstallDialogStory";

const OSES = ["mac", "linux", "windows"] as const;
const RUNTIMES = ["claude", "codex"] as const;

for (const [width, height] of [[1280, 900], [375, 667]]) {
  test(`at ${width}px every OS and AI tool tab fits the dialog and every command is shown whole`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height });
    await mount(<MonitorInstallDialogStory />);
    const cmd = page.getByTestId("mon-boot-cmd");
    await expect(cmd).toContainText("install.sh?code=");

    for (const [os, runtime] of OSES.flatMap((o) => RUNTIMES.map((r) => [o, r] as const))) {
      await page.getByTestId(`mon-install-tab-${os}`).click();
      await page.getByTestId(`mon-install-runtime-${runtime}`).click();
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
      expect(m.overflowing, `${os}/${runtime}: nothing pokes out of the dialog box`).toBe(0);
      expect(m.codeScrolls, `${os}/${runtime}: every command wraps, no hidden part`).toBe(false);
      expect(m.pageScrolls, `${os}/${runtime}: no page-level horizontal scroll`).toBe(false);
      expect(m.inView, `${os}/${runtime}: the dialog box stays inside the screen`).toBe(true);
    }
  });
}
