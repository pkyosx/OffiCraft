// The band above the tabs that holds the app-wide banners: it must line up with
// the content column and leave no gap through which the page canvas shows.
// Pure layout, so only a real browser can measure it. The banner is inserted by
// hand because the mock adapter never makes one appear on its own.
import { test, expect } from "@playwright/experimental-ct-react";
import { NavTabsNarrowStory } from "./stories/NavTabsNarrowStory";

for (const width of [390, 1280, 1440]) {
  for (const layout of ["narrow", "wide"] as const) {
    test(`a banner in the band lines up with the tab strip and the band touches the top bar and the tabs @${width} ${layout}`, async ({
      mount,
      page,
    }) => {
      await page.setViewportSize({ width, height: 844 });
      const cmp = await mount(<NavTabsNarrowStory />);
      await expect(cmp.locator(".nav-tabs__seg")).toBeVisible();

      const empty = await page.evaluate((wide) => {
        if (wide) document.documentElement.dataset.layout = "wide";
        else delete document.documentElement.dataset.layout;
        const band = document.querySelector(".app__notices") as HTMLElement;
        const topbar = document.querySelector(".topbar") as HTMLElement;
        const tabs = document.querySelector(".nav-tabs") as HTMLElement;
        return {
          bandHeight: band.getBoundingClientRect().height,
          topbarToTabs: tabs.getBoundingClientRect().top - topbar.getBoundingClientRect().bottom,
        };
      }, layout === "wide");
      expect(empty).toEqual({ bandHeight: 0, topbarToTabs: 0 });

      const m = await page.evaluate(() => {
        const band = document.querySelector(".app__notices") as HTMLElement;
        const banner = document.createElement("div");
        banner.className = "status-banner";
        banner.textContent = "banner";
        band.append(banner);
        const r = (sel: string) => (document.querySelector(sel) as HTMLElement).getBoundingClientRect();
        const b = banner.getBoundingClientRect();
        const seg = r(".nav-tabs__seg");
        const tabs = r(".nav-tabs");
        const bandBox = band.getBoundingClientRect();
        return {
          bannerLeft: b.left - seg.left,
          bannerRight: b.right - seg.right,
          bandLeft: bandBox.left - tabs.left,
          bandRight: bandBox.right - tabs.right,
          topbarToBand: bandBox.top - r(".topbar").bottom,
          bandToTabs: tabs.top - bandBox.bottom,
          bandBackground: getComputedStyle(band).backgroundColor === getComputedStyle(document.querySelector(".nav-tabs") as HTMLElement).backgroundColor,
          overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        };
      });
      expect(m).toEqual({
        bannerLeft: 0,
        bannerRight: 0,
        bandLeft: 0,
        bandRight: 0,
        topbarToBand: 0,
        bandToTabs: 0,
        bandBackground: true,
        overflow: 0,
      });
    });
  }
}
