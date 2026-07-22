// 辦公室 nav unread badge (T-aed5 — the office red dot, upgraded to a count).
//
// Locked here:
//   1. COUNT, NOT A DOT: the office tab renders the actual total chat unread
//      as a number (owner request), reusing the same .nav-tab__badge pill as
//      the 等我回覆/任務 tabs — no leftover plain dot.
//   2. > 99 clamps to "99+" (the shared badge convention).
//   3. count 0 → NOT RENDERED at all (no empty pill).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";

// The office badge rides useChatUnread; drive it per-test via a hoisted stub.
const state = vi.hoisted(() => ({ chatUnread: 0 }));
vi.mock("./hooks/useChatUnread", () => ({
  useChatUnread: () => state.chatUnread,
}));
// The sibling count hooks and the heavy page bodies are irrelevant to the nav
// badge under test — stub them so App mounts without touching the api layer.
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
// Studio-name seam mount-fetches /api/settings — stub it (like the badge hooks)
// so App mounts without touching the api layer.
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
// 使用說明 is a nav tab now (owner 2026-07-22) — stub it like its siblings.
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));

import App from "./App";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

describe("辦公室 nav unread badge", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.chatUnread = 0;
  });

  it("renders the unread total as a count pill", () => {
    state.chatUnread = 3;
    renderApp();
    const badge = screen.getByTestId("office-unread-badge");
    expect(badge.textContent).toBe("3");
    expect(badge.className).toContain("nav-tab__badge");
  });

  it("clamps counts above 99 to \"99+\"", () => {
    state.chatUnread = 250;
    renderApp();
    expect(screen.getByTestId("office-unread-badge").textContent).toBe("99+");
  });

  it("renders nothing when there is no unread", () => {
    state.chatUnread = 0;
    renderApp();
    expect(screen.queryByTestId("office-unread-badge")).toBeNull();
    // and no leftover plain dot from the old implementation
    expect(screen.queryByTestId("office-unread-dot")).toBeNull();
  });
});
