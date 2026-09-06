// App.lore-toggle.test.tsx — T-33, the UI row of the feature-switch table.
//
// 🔴 THE OFF HALF IS THE HALF THAT ROTS SILENTLY, so it is the half this file
// is built around. With the switch off there is nothing on screen, and 「the
// tab is correctly hidden」 renders EXACTLY the same pixels as 「App crashed
// before it drew the strip」, 「the label moved」, or 「the whole lore feature
// was deleted」. An `expect(queryByText(...)).toBeNull()` alone is green in all
// four worlds.
//
// 🔴 EVERY ABSENCE BELOW THEREFORE CARRIES TWO CONTROLS:
//   1. the SAME render also asserts sibling tabs ARE present, so a crashed or
//      empty strip cannot pass; and
//   2. the SAME assertion is re-run with the switch ON, where the tab must
//      appear — which is what proves the label and the query are right.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";

// The switch value one render sees. Mutable so a single mocked module can serve
// both states without re-mocking per test.
let loreEnabled = false;

vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({
  OfficePage: () => <div data-testid="office-page" />,
}));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));
// The lore page is stubbed with a MARKER rather than null: the question this
// file asks is 「did App mount it at all」, and a null stub cannot answer that.
vi.mock("./components/LorePage", () => ({
  LorePage: () => <div data-testid="lore-page" />,
}));
vi.mock("./hooks/sharedServerSettings", () => ({
  loadServerSettings: () => Promise.resolve({ loreEnabled }),
  adoptServerSettings: () => {},
  refreshServerSettings: () => Promise.resolve({ loreEnabled }),
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

describe("傳承 feature switch (T-33)", () => {
  beforeEach(() => {
    loreEnabled = false;
    history.replaceState(null, "", window.location.pathname);
  });

  it("關著的時候不顯示傳承分頁 —— 而且是分頁沒了，不是整條列沒了", async () => {
    renderApp();
    // Control ①: the strip really rendered. Without this, a crashed App would
    // satisfy the assertion below perfectly.
    await waitFor(() => expect(tabLabels().length).toBeGreaterThan(0));
    const labels = tabLabels();
    for (const sibling of [zh.nav.office, zh.nav.tasks, zh.nav.monitor, zh.nav.guide]) {
      expect(labels).toContain(sibling);
    }
    // The claim.
    expect(labels).not.toContain(zh.lore.title);
    expect(screen.queryByTestId("lore-page")).toBeNull();
  });

  it("打開之後就顯示 —— 這是上一條的對照組，同一個查詢、同一個標籤", async () => {
    loreEnabled = true;
    renderApp();
    // Control ②: the exact label and query the OFF test used do find the tab
    // once the switch is on. This is what makes the absence above a statement
    // about the switch rather than about a renamed label.
    expect(await screen.findByText(zh.lore.title)).toBeTruthy();
    const labels = tabLabels();
    expect(labels).toContain(zh.lore.title);
    // 傳承 keeps its ruled position (owner 2026-09-02:「傳承應該放在案件右邊」)
    // when it comes back — a switch that reinstates the tab in the wrong place
    // has not reinstated the tab the owner asked for.
    expect(labels.indexOf(zh.lore.title)).toBe(labels.indexOf(zh.nav.tasks) + 1);
  });

  it("關著的時候 #lore 深連結不會開出一個每個請求都被拒絕的頁", async () => {
    history.replaceState(null, "", window.location.pathname + "#lore");
    renderApp();
    // Control: the app routed somewhere real — the default tab is mounted.
    expect(await screen.findByTestId("office-page")).toBeTruthy();
    expect(screen.queryByTestId("lore-page")).toBeNull();
  });

  it("打開之後同一個 #lore 深連結就開得起來 —— 上一條的對照組", async () => {
    loreEnabled = true;
    history.replaceState(null, "", window.location.pathname + "#lore");
    renderApp();
    expect(await screen.findByTestId("lore-page")).toBeTruthy();
  });
});
