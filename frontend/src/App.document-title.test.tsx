// 瀏覽器分頁標題跟隨 org name (T-a9f1 — owner ask: "Can title align with our
// org name"). The browser-tab title (document.title) must follow the studio
// name the owner sets in the topbar, and must fall back to the localized
// default (t.orgName) when the owner has not named the studio.
//
// useOrgName is the seam: it already resolves orgName to the caller's fallback
// (t.orgName) when the stored value is empty/unloaded, so the two branches
// below are (1) a named studio → title is that name, and (2) unset → title is
// t.orgName. We drive both by mocking the hook's resolved orgName.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";

// Sibling mount-fetch hooks + heavy page bodies are irrelevant here — stub them
// so App mounts without touching the api layer (same seam as the sibling App
// tests). useOrgName is mocked per-test via the mutable holder below.
vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));

// null → simulate "owner has not named the studio": echo the fallback the way
// the real hook does. A string → simulate a server-backed studio name.
let mockStoredName: string | null = null;
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({
    orgName: mockStoredName && mockStoredName.length > 0 ? mockStoredName : fallback,
    setOrgName: () => {},
  }),
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
    </I18nProvider>
  );
}

describe("瀏覽器分頁標題跟隨 org name", () => {
  beforeEach(() => {
    window.location.hash = "";
    document.title = "";
    mockStoredName = null;
  });

  it("sets document.title to the owner's studio name", () => {
    mockStoredName = "GoFreight 工作室";
    renderApp();
    expect(document.title).toBe("GoFreight 工作室");
  });

  it("falls back to the localized default when the studio is unnamed", () => {
    mockStoredName = null;
    renderApp();
    expect(document.title).toBe(zh.orgName);
  });
});
