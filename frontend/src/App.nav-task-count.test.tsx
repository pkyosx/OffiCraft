// 任務 nav count — the neutral one (T-2658).
//
// The three nav counts used to be the same red pill. Red in this cockpit means
// "this one wants you": 辦公室 unread and 等我回覆 are things the owner has to
// act on, while the task total is only how much work is open — so a perfectly
// healthy 7 read as seven problems (owner, c-f6d16cbb5fa4).
//
// Locked here:
//   1. PARTITION: the 任務 count carries .nav-tab__count and does NOT carry
//      .nav-tab__badge, while 等我回覆 and 辦公室 keep .nav-tab__badge. This is
//      the assertion that goes red if someone "unifies the badges" again, in
//      either direction. Colour itself is not visible to jsdom — that half is
//      measured in a real browser by visual-guards/nav-count-neutral.ct.spec.tsx;
//      this file pins the class partition those measurements hang off.
//   2. UNCHANGED COUNTING: 0 renders nothing, > 99 clamps to "99+". The look
//      changed; what the number means did not.
//   3. NAMED FOR A SCREEN READER: the count carries an accessible name saying
//      what it counts. Without it the tab announces as "任務 7" and a reader
//      never learns what the 7 is. Queried through getByRole(name) on purpose:
//      dropping the name (or the role that carries it) reddens this test.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";

const state = vi.hoisted(() => ({ taskCount: 0, replyCount: 0, chatUnread: 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => state.taskCount }));
vi.mock("./hooks/useReplyCardCount", () => ({
  useReplyCardCount: () => state.replyCount,
}));
vi.mock("./hooks/useChatUnread", () => ({
  useChatUnread: () => state.chatUnread,
}));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
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

describe("任務 nav count", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.taskCount = 0;
    state.replyCount = 0;
    state.chatUnread = 0;
  });

  it("is its own neutral element, not the red alert pill", () => {
    state.taskCount = 7;
    renderApp();
    const count = screen.getByTestId("tasks-badge");
    expect(count.textContent).toBe("7");
    expect(count.classList.contains("nav-tab__count")).toBe(true);
    // The whole point: it must not ride the danger-pill class, whose fill,
    // text and ring are pinned to the danger tokens in check-token-roles.mjs.
    expect(count.classList.contains("nav-tab__badge")).toBe(false);
  });

  it("leaves 等我回覆 and 辦公室 on the red pill", () => {
    // The other half of the partition. Making the task count neutral by
    // draining the shared class would satisfy the test above and quietly take
    // the alert colour off the two badges that are supposed to have it.
    state.taskCount = 7;
    state.replyCount = 2;
    state.chatUnread = 3;
    renderApp();
    for (const id of ["replies-badge", "office-unread-badge"]) {
      expect(screen.getByTestId(id).classList.contains("nav-tab__badge")).toBe(
        true,
      );
      expect(screen.getByTestId(id).classList.contains("nav-tab__count")).toBe(
        false,
      );
    }
  });

  it("renders nothing at all when nothing is open", () => {
    // Assert BEFORE unmounting: querying an unmounted container returns null
    // for every implementation, so an assertion after unmount() would hold even
    // for one that renders an empty pill at 0.
    const view = renderApp();
    expect(screen.queryByTestId("tasks-badge")).toBeNull();
    view.unmount();
  });

  it("clamps above 99", () => {
    state.taskCount = 250;
    renderApp();
    expect(screen.getByTestId("tasks-badge").textContent).toBe("99+");
  });

  it("tells a screen reader what the number counts", () => {
    state.taskCount = 7;
    renderApp();
    // zh is the default dictionary: 「7件未結案」.
    expect(screen.getByRole("img", { name: "7件未結案" })).toBe(
      screen.getByTestId("tasks-badge"),
    );
  });

  it("names the clamped count as it is shown, not as the raw total", () => {
    // A name reading "250件未結案" over a pill showing "99+" would tell the two
    // audiences different things.
    state.taskCount = 250;
    renderApp();
    expect(screen.getByTestId("tasks-badge").getAttribute("aria-label")).toBe(
      "99+件未結案",
    );
  });
});
