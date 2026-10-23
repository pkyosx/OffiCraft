// 辦公室 nav unread badge (T-aed5 — the office red dot, upgraded to a count).
//
// Locked here:
//   1. COUNT, NOT A DOT: the office tab renders the actual total chat unread
//      as a number (owner request), reusing the same .nav-tab__badge pill as
//      the 等我回覆/任務 tabs.
//   2. > 99 clamps to "99+" (the shared badge convention).
//   3. count 0 → NOT RENDERED at all (no empty pill).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";

// The office badge rides useChatUnread; drive it per-test via a hoisted stub.
const state = vi.hoisted(() => ({
  chatUnread: 0,
  taskCount: { open: 0, total: 0, overdue: 0, dueSoon: 0, dueNextChangeTs: null },
}));
vi.mock("./hooks/useChatUnread", () => ({
  useChatUnread: () => state.chatUnread,
}));
// The sibling count hooks and the heavy page bodies are irrelevant to the nav
// badge under test — stub them so App mounts without touching the api layer.
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({
  useTaskCount: () => state.taskCount,
}));
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
  });
});

describe("任務 nav due badge", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.chatUnread = 0;
  });

  it("shows overdue plus due-soon as a separate red count beside the open count", () => {
    state.taskCount = { open: 7, total: 9, overdue: 2, dueSoon: 3, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-badge").textContent).toBe("7");
    const due = screen.getByTestId("tasks-due-badge");
    expect(due.textContent).toBe("5");
    expect(due.className).toContain("nav-tab__badge--due");
    expect(due.getAttribute("aria-label")).toBe("已逾期或即將到期的任務 5");
  });

  it("clamps the due count above 99 to \"99+\"", () => {
    state.taskCount = { open: 300, total: 300, overdue: 60, dueSoon: 45, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-due-badge").textContent).toBe("99+");
  });

  it("renders no due badge when nothing is overdue or due soon", () => {
    state.taskCount = { open: 4, total: 4, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-badge").textContent).toBe("4");
    expect(screen.queryByTestId("tasks-due-badge")).toBeNull();
  });
});
