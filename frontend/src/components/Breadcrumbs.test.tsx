// Breadcrumbs (T-8f6e) — the settings tree's shared top-of-page navigation.
//
//   1. Renders every segment in order, separated by ›; non-terminal segments
//      are clickable buttons, the terminal one is the current page (plain
//      text + aria-current, never a dead button).
//   2. Clicking a segment fires ITS jump only.
//   3. mono segments carry the manual-key styling class.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";

function renderCrumbs(items: Crumb[]) {
  return render(
    <I18nProvider>
      <Breadcrumbs items={items} />
    </I18nProvider>
  );
}

describe("Breadcrumbs", () => {
  it("renders segments in order — links for parents, plain current for the last", () => {
    const utils = renderCrumbs([
      { label: "設定", onClick: () => {} },
      { label: "任務手冊", onClick: () => {} },
      { label: "review-pr", mono: true },
    ]);

    const nav = utils.container.querySelector("nav.crumbs");
    expect(nav).toBeTruthy();
    expect(nav!.textContent).toBe("設定›任務手冊›review-pr");

    // Parents are buttons; the terminal segment is NOT a button and is marked
    // as the current page.
    expect(utils.getByRole("button", { name: "設定" })).toBeTruthy();
    expect(utils.getByRole("button", { name: "任務手冊" })).toBeTruthy();
    const here = utils.getByText("review-pr");
    expect(here.tagName).not.toBe("BUTTON");
    expect(here.getAttribute("aria-current")).toBe("page");
    // mono segments keep the manual-key styling.
    expect(here.className).toContain("manual-key");
  });

  it("a single-segment breadcrumb (the 設定 root) renders as the current page only", () => {
    const utils = renderCrumbs([{ label: "設定" }]);
    const here = utils.getByText("設定");
    expect(here.tagName).not.toBe("BUTTON");
    expect(here.getAttribute("aria-current")).toBe("page");
    expect(utils.container.querySelector(".crumbs__sep")).toBeNull();
  });

  it("clicking a segment fires its own jump only", () => {
    const onRoot = vi.fn();
    const onMid = vi.fn();
    const utils = renderCrumbs([
      { label: "設定", onClick: onRoot },
      { label: "角色誌", onClick: onMid },
      { label: "助理" },
    ]);

    fireEvent.click(utils.getByRole("button", { name: "角色誌" }));
    expect(onMid).toHaveBeenCalledTimes(1);
    expect(onRoot).not.toHaveBeenCalled();

    fireEvent.click(utils.getByRole("button", { name: "設定" }));
    expect(onRoot).toHaveBeenCalledTimes(1);
  });
});

// The landmark's ACCESSIBLE NAME — deliberately queried by role+name, never by
// text. An aria-label is not a text node, which is why the whole suite (and
// GuidePage's `queryByText("設定") === null`, written to prove the guide had
// left Settings) sailed over a landmark that was still announcing 設定 on the
// 使用說明 tab. Every assertion below reads what a screen reader would compute,
// so a name that drifts away from the visible trail reddens here.
describe("Breadcrumbs · the landmark's accessible name", () => {
  it("is the trail's own root, so it names the region the reader is in", () => {
    // Settings tree → 設定.
    const s = renderCrumbs([
      { label: "設定", onClick: () => {} },
      { label: "角色誌" },
    ]);
    expect(
      s.getByRole("navigation", { name: "設定" }),
      "a settings trail's landmark must be named 設定"
    ).toBeTruthy();
    // RTL binds queries to document.body, not to the container, so the settings
    // trail has to GO before the negative assertion below can mean anything —
    // otherwise it would match the leftover render and redden for the wrong
    // reason. (It did, on the first run of this test.)
    s.unmount();

    // 使用說明 tab → 使用說明. This is the case the pack broke: same component,
    // different region, and the name must follow the region, not the component.
    const g = renderCrumbs([
      { label: "使用說明", onClick: () => {} },
      { label: "介面說明" },
    ]);
    expect(
      g.getByRole("navigation", { name: "使用說明" }),
      "the guide trail's landmark must be named 使用說明, not 設定"
    ).toBeTruthy();
    expect(
      g.queryByRole("navigation", { name: "設定" }),
      "a trail outside Settings must not announce itself as Settings"
    ).toBeNull();
  });

  // The name rides the crumb label, so it is localized by construction — but
  // "by construction" is the kind of claim that stops being true silently.
  it("is localized in all three locales, following the crumb label", () => {
    for (const root of ["設定", "Settings", "宗門設定", "使用箋註", "User guide"]) {
      const u = renderCrumbs([{ label: root, onClick: () => {} }, { label: "x" }]);
      expect(
        u.getByRole("navigation", { name: root }),
        `the landmark must be named ${root}`
      ).toBeTruthy();
      u.unmount();
    }
  });

  // An unnamed landmark is honest; a wrongly-named one is not. With no
  // segments there is no region to name, so no name is emitted at all.
  it("emits no name at all when there is no trail", () => {
    const u = renderCrumbs([]);
    const nav = u.container.querySelector("nav.crumbs")!;
    expect(nav).toBeTruthy();
    expect(nav.getAttribute("aria-label")).toBeNull();
  });
});
