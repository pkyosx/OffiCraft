import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { MachineDiskUsageView, MachineView, MonMachineView } from "../types";

const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([]),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
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

const GIB = 1024 ** 3;

const machine = (id: string, displayName: string): MachineView => ({
  machineId: id,
  displayName,
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

const usage = (totalGib: number): MachineDiskUsageView => ({
  measuredAt: Math.floor(Date.now() / 1000) - 60,
  totalBytes: Math.round(totalGib * GIB),
  databaseBytes: null,
  backupsBytes: null,
  databaseMeasuredAt: null,
  workspaceBytes: Math.round(totalGib * GIB),
  conversationBytes: null,
  claudeConversationBytes: null,
  codexConversationBytes: null,
  otherBytes: null,
  members: [],
  diskFreeBytes: null,
  diskTotalBytes: null,
});

const card = (machineId: string, diskUsage: MachineDiskUsageView | null): MonMachineView => ({
  machine: machineId,
  displayName: machineId,
  agents: 0,
  accounts: [],
  cpuPct: null,
  ramPct: null,
  batteryPct: null,
  acPower: null,
  hardwareTs: null,
  hardwareStale: null,
  hardwareInvalid: [],
  runtimeCapabilitiesTs: null,
  runtimeCapabilitiesStale: null,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
  diskUsage,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

function machinesTable(): HTMLTableElement {
  return document.querySelector(".mon-table--machines") as HTMLTableElement;
}

describe("MonitorPage 磁碟 column", () => {
  beforeEach(() => {
    listMachines.mockReset();
    getMonitoring.mockReset();
  });

  it("puts 磁碟 last, after 電源, and each row shows its own machine's total", async () => {
    listMachines.mockResolvedValue([
      machine("m-alpha", "alpha"),
      machine("m-beta", "beta"),
      machine("m-gamma", "gamma"),
      machine("m-delta", "delta"),
    ]);
    // Listed out of row order, so a join by position would land on the wrong row.
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [],
      machines: [card("m-beta", usage(7.5)), card("m-gamma", null), card("m-alpha", usage(41.7))],
    });
    renderMonitor();
    await screen.findByText("m-delta");
    await screen.findByText("41.7 GB");

    const table = machinesTable();
    expect(Array.from(table.querySelectorAll("thead th")).map((th) => th.textContent)).toEqual([
      "機器",
      "Claude",
      "Codex",
      "CPU",
      "RAM",
      "電源",
      "磁碟",
    ]);
    const rows = Array.from(table.querySelectorAll("tbody tr")).map((tr) => {
      const disk = within(tr as HTMLElement).getByTestId("mon-disk");
      return [
        within(tr as HTMLElement).getByTestId("mon-machine-id").textContent,
        disk.textContent,
        disk.getAttribute("data-label"),
        Array.from(tr.children).indexOf(disk),
      ];
    });
    expect(rows).toEqual([
      ["m-alpha", "41.7 GB", "磁碟", 6],
      ["m-beta", "7.5 GB", "磁碟", 6],
      ["m-gamma", "尚未量測", "磁碟", 6],
      ["m-delta", "尚未量測", "磁碟", 6],
    ]);
  });

  it("spans every column with the empty-table message when there is no machine", async () => {
    listMachines.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    renderMonitor();
    const empty = await screen.findByText(/^尚無機器/);
    const table = machinesTable();
    expect(table.querySelectorAll("thead th")).toHaveLength(7);
    expect(empty.closest("td")!.getAttribute("colspan")).toBe("7");
  });
});
