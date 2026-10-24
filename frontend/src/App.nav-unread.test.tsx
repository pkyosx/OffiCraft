// 辦公室 nav unread badge (T-aed5 — the office red dot, upgraded to a count).
//
// Locked here:
//   1. COUNT, NOT A DOT: the office tab renders the actual total chat unread
//      as a number (owner request), reusing the same .nav-tab__badge pill as
//      the 等我回覆 tab.
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

const segmentsOf = (pill: HTMLElement) =>
  Array.from(pill.children, (seg) => [seg.className, seg.textContent]);

describe("任務 nav badge", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.chatUnread = 0;
  });

  it("shows open, due-soon and overdue as gray, amber and red segments of one labelled pill", () => {
    state.taskCount = { open: 12, total: 15, overdue: 2, dueSoon: 1, dueNextChangeTs: null };
    renderApp();
    const pill = screen.getByTestId("tasks-badge");
    expect(segmentsOf(pill)).toEqual([
      ["nav-tab__seg nav-tab__seg--open", "12"],
      ["nav-tab__seg nav-tab__seg--due-soon", "1"],
      ["nav-tab__seg nav-tab__seg--overdue", "2"],
    ]);
    expect(pill.getAttribute("role")).toBe("img");
    expect(pill.getAttribute("aria-label")).toBe("進行中 12 個，即將到期 1 個，已逾期 2 個");
  });

  it("leaves out a segment whose count is zero, in the pill and its label", () => {
    state.taskCount = { open: 4, total: 4, overdue: 3, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    const pill = screen.getByTestId("tasks-badge");
    expect(segmentsOf(pill)).toEqual([
      ["nav-tab__seg nav-tab__seg--open", "4"],
      ["nav-tab__seg nav-tab__seg--overdue", "3"],
    ]);
    expect(pill.getAttribute("aria-label")).toBe("進行中 4 個，已逾期 3 個");
  });

  it("shows only the gray segment when nothing is due soon or overdue", () => {
    state.taskCount = { open: 4, total: 4, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    const pill = screen.getByTestId("tasks-badge");
    expect(segmentsOf(pill)).toEqual([["nav-tab__seg nav-tab__seg--open", "4"]]);
    expect(pill.getAttribute("aria-label")).toBe("進行中 4 個");
  });

  it("renders no pill when no task is open", () => {
    state.taskCount = { open: 0, total: 6, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    expect(screen.queryByTestId("tasks-badge")).toBeNull();
  });

  it("clamps each segment above 99 to \"99+\" while the label keeps the real counts", () => {
    state.taskCount = { open: 300, total: 300, overdue: 120, dueSoon: 99, dueNextChangeTs: null };
    renderApp();
    const pill = screen.getByTestId("tasks-badge");
    expect(segmentsOf(pill).map(([, text]) => text)).toEqual(["99+", "99", "99+"]);
    expect(pill.getAttribute("aria-label")).toBe("進行中 300 個，即將到期 99 個，已逾期 120 個");
  });
});
