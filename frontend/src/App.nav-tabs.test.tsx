// 主導覽分頁 — the tab strip itself.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./i18n";
import { zh } from "./i18n/locales/zh";
import { mockApi, __resetMock, __setMockNoAppearance } from "./api/mock";
import { TOKEN_KEY } from "./api/auth";

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
vi.mock("./components/SettingsPage", () => ({
  SettingsPage: () => <div data-testid="settings-page" />,
}));

import App from "./App";

const SECRETS_IMG =
  "data:image/png;base64," +
  btoa(String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1));

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

function renderApp() {
  return render(
    <I18nProvider>
      <Capture />
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
    localStorage.clear();
    __resetMock();
    __setMockNoAppearance();
  });

  it("shows the active theme's secrets image in App's secrets tab", async () => {
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    await mockApi.putTheme({
      id: "secrets-icon",
      name: "密鑰圖示測試",
      colors: { "--color-bg": "#101018" },
      navIcons: { secrets: SECRETS_IMG },
    });
    renderApp();
    act(() => ctx.setTheme("secrets-icon"));
    await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe("secrets-icon"));

    const tab = screen.getByText(zh.nav.secrets).closest(".nav-tab");
    expect(tab?.querySelector("img.nav-tab__icon-img")?.getAttribute("src")).toBe(
      SECRETS_IMG,
    );
    expect(tab?.querySelector("svg")).toBeNull();
  });

  it("the nav row is 辦公室 / 請示 / 任務 / 密鑰 / 檔案庫 / 傳承 / 監控, in that order", () => {
    renderApp();
    expect(tabLabels()).toEqual([
      "辦公室",
      "請示",
      "任務",
      "密鑰",
      "檔案庫",
      "傳承",
      "監控",
    ]);
  });

  it.each(["#settings/guide/why", "#guide/why"])(
    "%s opens Settings with the gear active and no nav tab active",
    (hash) => {
      history.replaceState(null, "", window.location.pathname + hash);
      renderApp();
      expect(screen.getByTestId("settings-page")).toBeTruthy();
      expect(screen.getByLabelText("settings").className).toContain(
        "icon-btn--active",
      );
      expect(document.querySelector(".nav-tabs .nav-tab--active")).toBeNull();
    },
  );

  it("a #shared-docs/<id> deep link opens that shared doc under the 檔案庫 tab", async () => {
    history.replaceState(null, "", window.location.pathname + "#shared-docs/sd-3f9a2c71b0e4");
    renderApp();
    expect((await screen.findByTestId("shared-doc-title")).textContent).toBe("Review Rule");
    const active = document.querySelector(".nav-tabs .nav-tab--active");
    expect(active?.textContent).toBe(zh.nav.sharedDocs);
  });

  it("selecting 檔案庫 routes to #shared-docs and mounts the list", async () => {
    renderApp();
    fireEvent.click(screen.getByText(zh.nav.sharedDocs));
    expect(window.location.hash).toBe("#shared-docs");
    expect(await screen.findByTestId("shared-docs-page")).toBeTruthy();
  });

  it("a #secrets deep link opens the Secrets page and marks its tab active", async () => {
    history.replaceState(null, "", window.location.pathname + "#secrets");
    renderApp();
    expect(await screen.findByTestId("secrets-page")).toBeTruthy();
    const active = document.querySelector(".nav-tabs .nav-tab--active");
    expect(active?.textContent).toBe(zh.nav.secrets);
  });

  it("a #lore deep link opens the Lore directory, and Mira's section page opens to the owner's scope menu on each entry", async () => {
    history.replaceState(null, "", window.location.pathname + "#lore");
    renderApp();
    fireEvent.click(
      await waitFor(() => {
        const el = document.querySelector<HTMLElement>(
          '[data-testid="lore-target"][data-section="agent:mira"]',
        );
        if (!el) throw new Error("no directory row for Mira yet");
        return el;
      }),
    );
    const action = await waitFor(() => {
      const el = document.querySelector<HTMLElement>(
        '[data-testid="lore-row"][data-entry-id="L-2"] [data-testid="lore-scope-name"]',
      );
      if (!el) throw new Error("L-2 has no 適用範圍 action yet");
      return el;
    });
    expect(window.location.hash).toBe("#lore/agent/mira");
    expect(action.tagName).toBe("BUTTON");
    expect(action.textContent).toBe("適用範圍");

    fireEvent.click(action);
    await waitFor(() =>
      expect(
        document.querySelector(
          '[data-testid="lore-row"][data-entry-id="L-2"] [data-testid="lore-scope-options"]',
        )?.textContent,
      ).toBe("適用範圍建立者：Mira預設所有人"),
    );
  });
});
