// 主導覽分頁 — the tab strip itself.
//
// Added when 使用說明 was promoted out of Settings into a top-level tab
// (owner 2026-07-22:「user guide 改放在 tab 中,監控的右邊,不要放在 settings
// 裡」). What is locked here is the part a screenshot conveys and a component
// test otherwise cannot:
//   1. ORDER — 辦公室 / 請示 / 任務 / 監控 / 使用說明, with 使用說明 LAST, i.e.
//      immediately to the right of 監控. The owner's红框 was a position, so
//      position is the requirement; "the tab exists somewhere" would not be.
//   2. It routes: clicking it writes #guide and renders the guide page.
//   3. It is a NAV tab, not a settings sub-page — opening Settings deactivates
//      it, exactly like every sibling tab.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";

// Count hooks + heavy page bodies are irrelevant to the tab strip — stub them
// so App mounts without touching the api layer (same seam as the sibling App
// tests).
vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
// The guide body itself is exercised in GuidePage.test.tsx; here we only need
// to see WHICH page App mounted.
vi.mock("./components/UserGuidePage", () => ({
  GuidePage: () => <div data-testid="guide-page" />,
}));

import App from "./App";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

function tabLabels(): string[] {
  return Array.from(document.querySelectorAll(".nav-tabs .nav-tab")).map((el) =>
    (el.querySelector("span:not(.nav-tab__badge)")?.textContent ?? "").trim(),
  );
}

describe("主導覽分頁", () => {
  beforeEach(() => {
    history.replaceState(null, "", window.location.pathname);
  });

  it("使用說明 is the last tab, immediately right of 監控", () => {
    renderApp();
    const labels = tabLabels();
    expect(labels).toEqual([
      zh.nav.office,
      zh.nav.replies,
      zh.nav.tasks,
      zh.nav.monitor,
      zh.nav.guide,
    ]);
    expect(labels.indexOf(zh.nav.guide)).toBe(
      labels.indexOf(zh.nav.monitor) + 1,
    );
  });

  // Every nav tab navigates through the hash (lib/hashRoute), and the browser
  // delivers `hashchange` asynchronously — hence the await. That is the
  // pre-existing mechanism, not something this tab introduced.
  it("selecting it routes to #guide and mounts the guide page", async () => {
    renderApp();
    expect(screen.queryByTestId("guide-page")).toBeNull();
    fireEvent.click(screen.getByText(zh.nav.guide));
    expect(window.location.hash).toBe("#guide");
    expect(await screen.findByTestId("guide-page")).toBeTruthy();
    expect(
      screen.getByText(zh.nav.guide).closest(".nav-tab")?.className,
    ).toContain("nav-tab--active");
  });

  it("a #guide/<slug> deep link opens the guide tab", () => {
    history.replaceState(null, "", window.location.pathname + "#guide/why");
    renderApp();
    expect(screen.getByTestId("guide-page")).toBeTruthy();
    expect(
      screen.getByText(zh.nav.guide).closest(".nav-tab")?.className,
    ).toContain("nav-tab--active");
  });

  it("opening Settings deactivates it, like every other tab", () => {
    history.replaceState(null, "", window.location.pathname + "#guide");
    renderApp();
    fireEvent.click(screen.getByLabelText("settings"));
    expect(screen.queryByTestId("guide-page")).toBeNull();
    expect(
      screen.getByText(zh.nav.guide).closest(".nav-tab")?.className,
    ).not.toContain("nav-tab--active");
  });
});
