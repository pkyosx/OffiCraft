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
