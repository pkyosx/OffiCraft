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

const pill = (name: string) => screen.getByRole("button", { name });
const noPill = (runtime: "Claude" | "Codex") =>
  expect(
    screen.queryAllByRole("button").filter((b) => (b.getAttribute("aria-label") ?? "").startsWith(runtime))
  ).toEqual([]);

describe("MonitorPage runtime action menu", () => {
  it("under installed Claude and Codex, each version is its own menu trigger named after the runtime, holding its chips; Claude offers 登入 and 升級 Claude Code, Codex only 登入", async () => {
    await mount({ claude: installed(false), codex: installed(true) });
    const claude = pill("Claude 2.1.300 操作");
    expect(claude.getAttribute("aria-haspopup")).toBe("menu");
    expect(claude.textContent).toBe("2.1.300未登入");
    expect(claude.contains(screen.getByTestId("mon-claude-logged-out"))).toBe(true);
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("2.1.300未登入");
    fireEvent.click(claude);
    expect(claude.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByRole("menuitem").map((el) => el.textContent)).toEqual(["登入", "升級 Claude Code"]);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(claude.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(claude);

    const codex = pill("Codex 2.1.300 操作");
    expect(codex.textContent).toBe("2.1.300");
    fireEvent.keyDown(codex, { key: "ArrowDown" });
    expect(codex.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByRole("menuitem").map((el) => el.textContent)).toEqual(["登入"]);
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "登入" }));
  });

  it("under a stale report, the stale marker rides inside the trigger", async () => {
    listMachines.mockResolvedValue([row(null)]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [],
      machines: [{ ...card({ claude: installed(true), codex: installed(null) }), runtimeCapabilitiesStale: true }],
    });
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    await screen.findByTestId("mon-codex-version");
    await act(async () => {
      await Promise.resolve();
    });
    expect(pill("Codex 2.1.300 操作").contains(screen.getByTestId("mon-codex-stale"))).toBe(true);
  });

  it("under 登入 in the Codex column, the Codex sign-in dialog opens for that machine", async () => {
    await mount({ claude: installed(true), codex: installed(false) });
    fireEvent.click(pill("Codex 2.1.300 操作"));
    fireEvent.click(screen.getByRole("menuitem", { name: "登入" }));
    expect(startRuntimeLogin).toHaveBeenCalledWith("m-box", "codex");
    expect(screen.getByRole("dialog").getAttribute("aria-label")).toBe("登入 Codex");
    expect(screen.queryByTestId("runtime-login-replace-hint")).toBeNull();
  });

  it("under a Codex reported not installed, its cell is plain text while Claude's is a trigger", async () => {
    await mount({ claude: installed(true), codex: { installed: false, loggedIn: null, version: null } });
    expect(pill("Claude 2.1.300 操作")).toBeTruthy();
    noPill("Codex");
    expect(screen.getByTestId("mon-codex-version").textContent).toBe("未安裝");
  });

  it("under 登入, the sign-in dialog opens for that machine", async () => {
    await mount({ claude: installed(true), codex: installed(true) });
    fireEvent.click(pill("Claude 2.1.300 操作"));
    fireEvent.click(screen.getByRole("menuitem", { name: "登入" }));
    expect(startRuntimeLogin).toHaveBeenCalledWith("m-box", "claude");
    expect(screen.getByTestId("runtime-login-preparing").textContent).toBe("正在請 工作站 準備登入…");
    expect(screen.getByTestId("runtime-login-replace-hint")).toBeTruthy();
  });

  it("under a Claude reported not installed, its cell is plain text", async () => {
    await mount({ claude: { installed: false, loggedIn: null, version: null }, codex: installed(true) });
    expect(screen.getByTestId("mon-claude-version").textContent).toBe("未安裝");
    noPill("Claude");
    expect(pill("Codex 2.1.300 操作")).toBeTruthy();
  });

  it("under no capability report but a registry version, the trigger is offered; with neither, it is not", async () => {
    await mount(null, "2.1.200");
    expect(pill("Claude 2.1.200 操作").textContent).toBe("2.1.200");
    noPill("Codex");
    cleanup();
    await mount(null, null);
    noPill("Claude");
    noPill("Codex");
  });
});
