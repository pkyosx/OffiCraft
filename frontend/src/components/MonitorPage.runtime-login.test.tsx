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
const startRuntimeLogin = vi.fn();

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
    startRuntimeLogin: (...a: unknown[]) => startRuntimeLogin(...a),
    cancelRuntimeLogin: () => Promise.resolve(),
    getRuntimeLogin: () => Promise.reject(new Error("unused")),
    subscribeEvents: () => () => {},
  },
}));

const row = (claudeVersion: string | null): MachineView => ({
  machineId: "m-box",
  displayName: "工作站",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion,
  claudeCredSource: null,
  claudeSubReadable: null,
});

type Caps = MonMachineView["runtimeCapabilities"];

const card = (caps: Caps): MonMachineView => ({
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
  runtimeCapabilitiesStale: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

const installed = (loggedIn: boolean | null) => ({ installed: true, loggedIn, version: "2.1.300" });

async function mount(caps: Caps | null, claudeVersion: string | null = null) {
  listMachines.mockResolvedValue([row(claudeVersion)]);
  getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: caps ? [card(caps)] : [] });
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
  startRuntimeLogin.mockReset().mockReturnValue(new Promise(() => {}));
});
afterEach(cleanup);

describe("MonitorPage runtime action menu", () => {
  it("under an installed Claude, its cell carries the ⋯ menu with 登入, and the Codex cell carries none", async () => {
    await mount({ claude: installed(false), codex: installed(true) });
    const claudeCell = screen.getByTestId("mon-claude-version");
    expect(claudeCell.textContent).toBe("2.1.300未登入");
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    expect(screen.getAllByRole("menuitem").map((el) => el.textContent)).toEqual(["登入"]);
    expect(screen.getByTestId("mon-codex-version").textContent).toBe("2.1.300");
    expect(screen.queryByTestId("mon-codex-menu")).toBeNull();
  });

  it("under 登入, the sign-in dialog opens for that machine", async () => {
    await mount({ claude: installed(true), codex: installed(true) });
    fireEvent.click(screen.getByTestId("mon-claude-menu"));
    fireEvent.click(screen.getByTestId("mon-claude-menu-login"));
    expect(startRuntimeLogin).toHaveBeenCalledWith("m-box", "claude");
    expect(screen.getByTestId("runtime-login-preparing").textContent).toBe("正在請 工作站 準備登入…");
    expect(screen.getByTestId("runtime-login-replace-hint")).toBeTruthy();
  });

  it("under a Claude reported not installed, there is no menu", async () => {
    await mount({ claude: { installed: false, loggedIn: null, version: null }, codex: installed(true) });
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("未安裝");
    expect(screen.queryByTestId("mon-claude-menu")).toBeNull();
  });

  it("under no capability report but a registry version, the menu is offered; with neither, it is not", async () => {
    await mount(null, "2.1.200");
    expect(screen.getByTestId("mon-claude-menu")).toBeTruthy();
    cleanup();
    await mount(null, null);
    expect(screen.queryByTestId("mon-claude-menu")).toBeNull();
  });
});
