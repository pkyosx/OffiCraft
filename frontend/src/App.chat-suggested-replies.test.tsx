import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";
import { mockApi, __resetMock } from "./api/mock";
import { useChatSuggestedRepliesContext } from "./hooks/useChatSuggestedReplies";

function ListsProbe({ testId }: { testId: string }) {
  return <pre data-testid={testId}>{JSON.stringify(useChatSuggestedRepliesContext().lists)}</pre>;
}

vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => <ListsProbe testId="office-lists" /> }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => <ListsProbe testId="replies-lists" /> }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => <ListsProbe testId="tasks-lists" /> }));
vi.mock("./components/MonitorPage", () => ({ MonitorPage: () => null }));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));
vi.mock("./components/UserGuidePage", () => ({ GuidePage: () => null }));

import App from "./App";

describe("App", () => {
  beforeEach(() => {
    history.replaceState(null, "", window.location.pathname);
    localStorage.clear();
    __resetMock();
    vi.restoreAllMocks();
  });

  it("gives the office, 請示 and 任務 pages the same members' chat 建議回覆 from one read", async () => {
    await mockApi.putChatSuggestedReplies("mira", ["Mira 專屬"]);
    const read = vi.spyOn(mockApi, "getChatSuggestedReplies");
    render(
      <I18nProvider>
        <App />
      </I18nProvider>,
    );
    await waitFor(() =>
      expect(screen.getByTestId("office-lists").textContent).toBe('{"mira":["Mira 專屬"]}'),
    );
    fireEvent.click(screen.getByText(zh.nav.replies));
    expect((await screen.findByTestId("replies-lists")).textContent).toBe('{"mira":["Mira 專屬"]}');
    fireEvent.click(screen.getByText(zh.nav.tasks));
    expect((await screen.findByTestId("tasks-lists")).textContent).toBe('{"mira":["Mira 專屬"]}');
    expect(read).toHaveBeenCalledTimes(1);
  });
});
