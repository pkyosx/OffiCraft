import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";

const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));
const startRuntimeUpgrade = vi.fn();

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([] as Member[]),
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
    startRuntimeUpgrade: (...a: unknown[]) => startRuntimeUpgrade(...a),
    getRuntimeUpgrade: () => Promise.reject(new Error("unused")),
    subscribeEvents: () => () => {},
  },
}));

const row: MachineView = {
  machineId: "m-box",
  displayName: "工作站",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

type Caps = MonMachineView["runtimeCapabilities"];

const card = (caps: Caps, stale = false): MonMachineView => ({
  machine: "m-box",
  displayName: "工作站",
  agents: 0,
  accounts: [],
  cpuPct: null,
  ramPct: null,
  batteryPct: null,
  acPower: null,
  hardwareTs: null,
  hardwareStale: null,
  hardwareInvalid: [],
  runtimeCapabilities: caps,
  runtimeCapabilitiesTs: 1_700_000_000,
  runtimeCapabilitiesStale: stale,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
  diskUsage: null,
});

const claude = (belowNotifyMinimum: boolean | null) => ({
  installed: true,
  loggedIn: true,
  version: "2.1.200",
  belowNotifyMinimum,
});
const codex = { installed: true, loggedIn: true, version: "0.52.0", belowNotifyMinimum: true };

async function mount(machine: MonMachineView) {
  listMachines.mockResolvedValue([row]);
  getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [machine] });
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
  await screen.findByTestId("mon-claude-version");
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  startRuntimeUpgrade.mockReset().mockReturnValue(new Promise(() => {}));
});
afterEach(cleanup);

describe("MonitorPage Claude upgrade", () => {
  it("under 升級 Claude Code in the Claude menu, the upgrade dialog opens for that machine and starts a claude upgrade", async () => {
    await mount(card({ claude: claude(true), codex }));
    fireEvent.click(screen.getByRole("button", { name: "Claude 2.1.200 操作" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "升級 Claude Code" }));
    expect(startRuntimeUpgrade).toHaveBeenCalledWith("m-box", "claude");
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("升級 Claude Code");
    expect(screen.getByTestId("runtime-upgrade-preparing").textContent).toBe("正在請 工作站 開始升級…");
  });

  it("under a Claude version below the notification minimum, the Claude cell shows 版本太舊; at or above it, or unknown, it does not; Codex renders the same field the same way", async () => {
    await mount(card({ claude: claude(true), codex }));
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("2.1.200版本太舊");
    // The server sends below_notify_minimum for claude alone; the cell does not
    // tell the runtimes apart.
    expect(screen.getByTestId("mon-codex-version").textContent).toBe("0.52.0版本太舊");
    cleanup();
    await mount(card({ claude: claude(false), codex }));
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("2.1.200");
    cleanup();
    await mount(card({ claude: claude(null), codex }));
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("2.1.200");
  });

  it("under stale telemetry, 版本太舊 still rides beside the version it qualifies", async () => {
    await mount(card({ claude: claude(true), codex }, true));
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("2.1.200版本太舊過期");
  });
});
