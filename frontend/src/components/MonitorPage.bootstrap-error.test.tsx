// Bootstrap-on-server error transparency — Monitor §2 machine panel.
//
// Regression lock: a thrown bootstrapOnServer rejection (e.g. the 503
// "ocwarden binary is not available" gate) must surface the SERVER'S error
// detail in the failure block, not only the generic "安裝請求失敗" — the old
// catch discarded the ApiError entirely, leaving the owner with no fix hint.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import { ApiError } from "../api/errors";
import type { Member, MachineView, MonMachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));
const bootstrapOnServer = vi.fn();

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    bootstrapOnServer: (id: string) => bootstrapOnServer(id),
    subscribeEvents: () => () => {},
  },
}));

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage bootstrap-on-server error surface", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([
      {
        machineId: "m-server-self",
        displayName: "本機",
        online: false,
        isSelf: true,
        binStatus: null,
        claudeVersion: null,
        claudeCredSource: null,
        claudeSubReadable: null,
      },
    ]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  it("surfaces the server's 503 detail when the install request rejects", async () => {
    const detail = "ocwarden binary is not available (bin/ocwarden absent on server)";
    bootstrapOnServer.mockRejectedValue(
      new ApiError(
        "http 503 for POST /api/machines/m-server-self/bootstrap-here",
        503,
        "service_unavailable",
        detail
      )
    );
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-install-btn"));

    const banner = await screen.findByText(new RegExp(detail.replace(/[()]/g, "\\$&")));
    expect(banner.textContent).toContain("安裝請求失敗");
    expect(banner.textContent).toContain(detail);
  });

  it("keeps the generic message when the rejection carries no envelope detail", async () => {
    bootstrapOnServer.mockRejectedValue(new Error("network down"));
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-install-btn"));

    expect((await screen.findByText("安裝請求失敗")).textContent).toBe("安裝請求失敗");
  });
});
