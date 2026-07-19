// 座艙 topbar 版本顯示 (T-e9d1 round 3 — owner final: the topbar shows NO build
// identity at all; the unified v<yymmdd>-<hhmm>-<shortsha> label lives in
// Settings › 軟體更新 only). Locked here: even a build with a resolved r-N
// release tag renders no topbar version chip.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";

// The sibling mount-fetch hooks and heavy page bodies are irrelevant to the
// topbar under test — stub them so App mounts without touching the api layer
// (same seam as App.nav-unread.test.tsx).
vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
// Feed an already-resolved version: even a fully identified build renders no
// topbar chip.
vi.mock("./hooks/useVersion", () => ({
  useVersion: () => ({
    version: {
      version: "v260714-0930-4dc8956",
      gitSha: "4dc8956aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      gitTime: "2026-07-14T09:30:00Z",
      catalogHash: "deadbeef",
      updateAvailable: false,
      latestVersion: null,
    },
    loading: false,
    error: false,
    refresh: () => {},
  }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));

import App from "./App";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>
  );
}

describe("topbar 版本顯示", () => {
  beforeEach(() => {
    window.location.hash = "";
  });

  it("renders no version chip in the topbar", () => {
    const { container } = renderApp();
    expect(screen.queryByTestId("topbar-version")).toBeNull();
    expect(container.querySelector(".topbar__version")).toBeNull();
  });
});
