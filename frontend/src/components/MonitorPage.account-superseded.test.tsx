// AI Sessions 帳號 cell: a running Claude session stays on the account it
// launched on after the machine logs into another one, until it is refocused.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MonSessionView } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import { clearLocale, useEnglishLocale } from "../test/effortOptions";

const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [] as MonSessionView[],
  machines: [],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([] as Member[]),
    listMachines: () => Promise.resolve([]),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([] as OutsourceWorkerView[]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    getBackupHealth: () =>
      Promise.resolve({
        status: "healthy",
        code: "",
        detail: "",
        newestBackupTs: 1785600000,
        newestBackupAgeSecs: 3600,
        staleAfterSecs: 43200,
        sinceTs: null,
        checkedTs: 1785603600,
      }),
    subscribeEvents: () => () => {},
  },
}));

const session = (over: Partial<MonSessionView> = {}): MonSessionView => ({
  id: "mem-eva",
  name: "Eva",
  role: "engineer",
  model: "opus-4.8",
  effort: "",
  machine: "mbp5",
  account: "max@example.test",
  runtime: "claude",
  status: "online",
  contextPct: 42,
  compactionCount: null,
  cost: 3.5,
  bankedCost: null,
  ...over,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage AI Sessions — account still on the launch login", () => {
  beforeEach(() => {
    clearLocale();
    window.location.hash = "";
  });

  it("notes under the account that a session keeps its old account until refocus, only on that session", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ accountSuperseded: true }),
        session({ id: "mem-kip", name: "Kip", account: "ent@example.test", accountSuperseded: false }),
      ],
    });
    renderMonitor();

    const eva = (await screen.findByText("Eva")).closest("tr") as HTMLElement;
    expect(within(eva).getByText("max@example.test")).toBeTruthy();
    expect(within(eva).getByText("仍用舊帳號，重新聚焦後換新帳號")).toBeTruthy();
    const kip = screen.getByText("Kip").closest("tr") as HTMLElement;
    expect(within(kip).queryByText("仍用舊帳號，重新聚焦後換新帳號")).toBeNull();
  });

  it("says the same in English", async () => {
    useEnglishLocale();
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ accountSuperseded: true })],
    });
    renderMonitor();

    const eva = (await screen.findByText("Eva")).closest("tr") as HTMLElement;
    expect(within(eva).getByText("Still on its old account — switches after Refocus")).toBeTruthy();
  });
});
