// The monitor page leads with 帳號資訊 (T-5e71). The backup verdict lives under
// 設定 › 系統更新與備份 — see SettingsPage.backup-health.test.tsx.

import { describe, it, expect, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([]),
    listMachines: () => Promise.resolve([]),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

function renderPage() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>,
  );
}

describe("monitor page · section order", () => {
  it("leads with 帳號資訊", async () => {
    const utils = renderPage();
    await waitFor(() =>
      expect(utils.getByText(zh.monitor.accountsTitle)).toBeTruthy(),
    );
    const firstTitle = utils.container.querySelector(".mon-section__title");
    expect(firstTitle?.textContent).toBe(zh.monitor.accountsTitle);
  });
});
