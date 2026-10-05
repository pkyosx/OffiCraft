// 機器詳情 — the machine name menu's first item and the 磁碟 total open the
// same dialog: name, full machine id, OffiCraft disk total, the category bar
// and the breakdown list.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import { machineAction } from "./machineActions.testHelper";
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

/** 40 GiB in all: 4 database, 10 backups, 16 workspaces, 8 conversations, 2 other. */
const usage = (): MachineDiskUsageView => ({
  measuredAt: Math.floor(Date.now() / 1000) - 12 * 60,
  totalBytes: 40 * GIB,
  databaseBytes: 4 * GIB,
  backupsBytes: 10 * GIB,
  databaseMeasuredAt: null,
  workspaceBytes: 16 * GIB,
  conversationBytes: 8 * GIB,
  claudeConversationBytes: 3 * GIB,
  codexConversationBytes: 5 * GIB,
  otherBytes: 2 * GIB,
  members: [
    {
      memberId: "mira",
      name: "Mira",
      rosterStatus: "active",
      workspaceBytes: null,
      conversationBytes: null,
      totalBytes: 9 * GIB,
    },
  ],
  diskFreeBytes: 384 * GIB,
  diskTotalBytes: 931 * GIB,
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

const dialog = () => screen.queryByRole("dialog", { name: "機器詳情" });

/** What the dialog shows, top to bottom. */
function readDialog(d: HTMLElement) {
  return {
    name: within(d).getByTestId("mon-machine-detail-name").textContent,
    id: within(d).getByTestId("mon-machine-detail-id").textContent,
    total: within(d).getByTestId("mon-machine-detail-total").textContent,
    segments: within(d)
      .queryAllByTestId("disk-usage-seg")
      .map((s) => [s.getAttribute("data-segment"), s.style.width]),
    rows: within(d)
      .queryAllByTestId("disk-usage-row")
      .map((r) => r.textContent),
    measured: within(d).queryByTestId("disk-usage-measured")?.textContent ?? null,
  };
}

const MEASURED = {
  name: "Seth 的 Mac Studio",
  id: "m-c9479bc2d696",
  total: "40.0 GB",
  segments: [
    ["database", "10%"],
    ["backups", "25%"],
    ["workspaces", "40%"],
    ["conversations", "20%"],
    ["other", "5%"],
  ],
  rows: [
    "資料庫4.0 GB",
    "備份10.0 GB",
    "成員 workspace 合計16.0 GB",
    "Mira9.0 GB",
    "對話紀錄8.0 GB",
    "Claude3.0 GB",
    "Codex5.0 GB",
    "其他2.0 GB",
    "硬碟剩餘／總容量384.0 GB / 931.0 GB",
  ],
  measured: "量於 12m 前",
};

describe("MonitorPage 機器詳情 dialog", () => {
  beforeEach(() => {
    listMachines.mockReset();
    getMonitoring.mockReset();
    listMachines.mockResolvedValue([
      machine("m-0e7034bd5140", "Eva"),
      machine("m-c9479bc2d696", "Seth 的 Mac Studio"),
    ]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [],
      machines: [card("m-c9479bc2d696", usage()), card("m-0e7034bd5140", null)],
    });
  });

  it("詳情 opens it with the name, the full id, the total, a bar of the categories by share, the list and when it was measured", async () => {
    renderMonitor();
    await screen.findByText("40.0 GB");
    expect(dialog()).toBeNull();
    fireEvent.click(await machineAction("mon-detail-btn", 1));
    const d = dialog()!;
    expect(d.getAttribute("aria-modal")).toBe("true");
    expect(readDialog(d)).toEqual(MEASURED);
    expect(within(d).getByRole("img").getAttribute("aria-label")).toBe(
      "OffiCraft 磁碟用量組成：資料庫 4.0 GB (10%)、備份 10.0 GB (25%)、成員 workspace 合計 16.0 GB (40%)、對話紀錄 8.0 GB (20%)、其他 2.0 GB (5%)"
    );
    expect(screen.queryByRole("menu"), "choosing the item closes the menu").toBeNull();
  });

  it("clicking the 磁碟 total opens the same dialog", async () => {
    renderMonitor();
    const total = await screen.findByTestId("disk-usage-trigger");
    expect(total.textContent).toBe("40.0 GB");
    fireEvent.click(total);
    expect(readDialog(dialog()!)).toEqual(MEASURED);
    // No second, smaller panel opens beside it.
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(screen.queryByTestId("disk-usage-panel")).toBeNull();
  });

  it("under a machine never measured, it says 尚未量測 in place of the bar and the list", async () => {
    renderMonitor();
    await screen.findByText("40.0 GB");
    fireEvent.click(await machineAction("mon-detail-btn", 0));
    expect(readDialog(dialog()!)).toEqual({
      name: "Eva",
      id: "m-0e7034bd5140",
      total: "尚未量測",
      segments: [],
      rows: [],
      measured: null,
    });
    expect(within(dialog()!).queryByRole("img")).toBeNull();
  });

  it("closes via ✕, Esc and the backdrop, but not a click inside the box", async () => {
    renderMonitor();
    const total = await screen.findByTestId("disk-usage-trigger");

    fireEvent.click(total);
    fireEvent.click(screen.getByTestId("mon-machine-detail-close"));
    expect(dialog()).toBeNull();

    fireEvent.click(total);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(dialog()).toBeNull();

    fireEvent.click(total);
    fireEvent.click(within(dialog()!).getByTestId("mon-machine-detail-id"));
    expect(dialog(), "a click inside the box keeps it open").not.toBeNull();
    fireEvent.click(dialog()!);
    expect(dialog()).toBeNull();
  });

  it("wears the account detail's shell", async () => {
    renderMonitor();
    fireEvent.click(await screen.findByTestId("disk-usage-trigger"));
    const d = dialog()!;
    expect(d.className).toBe("mon-detailmodal");
    expect(d.firstElementChild!.className).toBe("mon-detailbox");
    expect(within(d).getByTestId("mon-machine-detail-close").className).toBe("mon-detailclose");
  });

  it("under English, it reads in English", async () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      renderMonitor();
      fireEvent.click(await machineAction("mon-detail-btn", 1));
      const d = screen.getByRole("dialog", { name: "Machine details" });
      expect(within(d).getByText("Machine ID")).toBeTruthy();
      expect(within(d).getByText("OffiCraft disk usage")).toBeTruthy();
      expect(within(d).getByTestId("disk-usage-measured").textContent).toBe("measured 12m ago");
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });
});
