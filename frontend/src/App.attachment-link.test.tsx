import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { api } from "./api";

vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", async () => {
  const { Markdown } = await import("./components/Markdown");
  return {
    OfficePage: () => <Markdown source="**文件註解**：[設計.md](/api/chat/attachment/att-aaaaaaaaaaaa)" />,
  };
});
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));

import App from "./App";

describe("App", () => {
  beforeEach(() => {
    history.replaceState(null, "", window.location.pathname);
    vi.restoreAllMocks();
  });

  it("previews an attachment linked from a page's markdown in place", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# 設計稿" })) as unknown as typeof fetch;
    vi.spyOn(api, "getChatAttachmentShareLink").mockResolvedValue("/api/chat/attachment/x?sig=s");
    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );
    expect(fireEvent.click(screen.getByRole("link", { name: "設計.md" }))).toBe(false);
    await waitFor(() => expect(screen.getByRole("heading", { name: "設計稿" })).toBeTruthy());
    expect(document.body.querySelector(".md-preview[role=dialog]")?.getAttribute("aria-label")).toBe("設計.md");
  });
});
