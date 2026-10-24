// 辦公室 nav unread badge (T-aed5 — the office red dot, upgraded to a count).
//
// Locked here:
//   1. COUNT, NOT A DOT: the office tab renders the actual total chat unread
//      as a number (owner request), reusing the same .nav-tab__badge pill as
//      the 等我回覆 tab.
//   2. > 99 clamps to "99+" (the shared badge convention).
//   3. count 0 → NOT RENDERED at all (no empty pill).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
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

describe("任務 nav badge", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.chatUnread = 0;
  });

  const tasksTab = () => screen.getByRole("button", { name: /^任務/ });

  it("shows the open count in the plain red pill, labelled with all three counts", () => {
    state.taskCount = { open: 12, total: 15, overdue: 2, dueSoon: 1, dueNextChangeTs: null };
    renderApp();
    const badge = screen.getByTestId("tasks-badge");
    expect(badge.textContent).toBe("12");
    expect(badge.className).toBe("nav-tab__badge");
    expect(screen.getByRole("button", { name: "任務 已逾期 2 · 即將到期 1 · 進行中 12" })).toBe(tasksTab());
  });

  it("paints the clock red when any task is overdue", () => {
    state.taskCount = { open: 12, total: 15, overdue: 2, dueSoon: 1, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-due-clock").className).toBe(
      "nav-tab__due-clock nav-tab__due-clock--overdue",
    );
  });

  it("paints the clock amber when tasks are due soon but none is overdue", () => {
    state.taskCount = { open: 4, total: 4, overdue: 0, dueSoon: 3, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-due-clock").className).toBe(
      "nav-tab__due-clock nav-tab__due-clock--soon",
    );
    expect(screen.getByRole("button", { name: "任務 即將到期 3 · 進行中 4" })).toBe(tasksTab());
  });

  it("shows no clock when nothing is overdue or due soon", () => {
    state.taskCount = { open: 4, total: 4, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-badge").textContent).toBe("4");
    expect(screen.queryByTestId("tasks-due-clock")).toBeNull();
  });

  it("hovering the tab shows the three counts, leaving out a zero due part", () => {
    state.taskCount = { open: 4, total: 4, overdue: 3, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    fireEvent.mouseEnter(tasksTab());
    expect(screen.getByRole("tooltip").textContent).toBe("已逾期 3 · 進行中 4");
  });

  it("renders no pill, clock or tooltip when no task is open", () => {
    state.taskCount = { open: 0, total: 6, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    expect(screen.queryByTestId("tasks-badge")).toBeNull();
    expect(screen.queryByTestId("tasks-due-clock")).toBeNull();
    fireEvent.mouseEnter(tasksTab());
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it.each([
    { open: 99, overdue: 40, dueSoon: 9, badge: "99", label: "任務 已逾期 40 · 即將到期 9 · 進行中 99" },
    { open: 300, overdue: 120, dueSoon: 99, badge: "99+", label: "任務 已逾期 120 · 即將到期 99 · 進行中 300" },
  ])("shows an open count of $open as \"$badge\" while the label keeps the real counts", ({ open, overdue, dueSoon, badge, label }) => {
    state.taskCount = { open, total: open, overdue, dueSoon, dueNextChangeTs: null };
    renderApp();
    expect(screen.getByTestId("tasks-badge").textContent).toBe(badge);
    expect(screen.getByRole("button", { name: label })).toBe(tasksTab());
  });

  it("clicking the tab opens the 任務 page", () => {
    state.taskCount = { open: 4, total: 4, overdue: 0, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    fireEvent.click(tasksTab());
    expect(window.location.hash).toBe("#tasks");
  });

  it("tapping the clock pins the three counts open instead of opening the 任務 page", () => {
    window.location.hash = "#office";
    state.taskCount = { open: 4, total: 4, overdue: 3, dueSoon: 0, dueNextChangeTs: null };
    renderApp();
    const clock = screen.getByTestId("tasks-due-clock");
    fireEvent.mouseEnter(tasksTab());
    fireEvent.click(clock);
    expect(window.location.hash).toBe("#office");
    fireEvent.mouseLeave(tasksTab());
    expect(screen.getByRole("tooltip").textContent).toBe("已逾期 3 · 進行中 4");
    fireEvent.click(clock);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });
});
