// The install button's WORD, not its action — Monitor §2 machine panel.
//
// One button used to say 「安裝」 for two situations that are not the same thing:
// a machine that already has a warden (the click overwrites it) and one that has
// none (the click sets it up). Owner 2026-08-20, looking at the cockpit: 「已經
// 安裝過的應該是要叫做（重新安裝）才對？」
//
// 🔴 What this file pins is the PROXY, and the proxy is deliberately imperfect.
// The server keeps NO "was this machine ever installed" field (T-ce3d), so
// offline is not the negation of installed: an installed-but-powered-off machine
// is byte-identical to one that was never touched. Owner ruled to use `online`
// as the proxy anyway, because the ACTION is `install --force` either way and
// only the word differs. So these assertions are:
//   • online  ⇒ 「重新安裝」 — always correct (a live warden IS talking to us)
//   • offline ⇒ 「安裝」    — correct for never-installed, KNOWINGLY wrong for
//                            an installed machine that is powered off
// A future change that adds the durable field should flip the offline arm and
// delete this comment — not delete the test.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";
import { machineAction } from "./machineActions.testHelper";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    bootstrapOnServer: () => Promise.resolve({ ok: true, exitCode: 0, log: "" }),
    getMachineBootCommand: () => Promise.resolve("curl … | sh"),
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

const base = {
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

function machine(over: Partial<MachineView>): MachineView {
  return {
    machineId: "m-alpha",
    displayName: "Alpha",
    online: false,
    isSelf: false,
    ...base,
    ...over,
  };
}

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("install button label follows the machine's online state", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  it("says 重新安裝 on an ONLINE machine (a warden is live there)", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const btn = await machineAction("mon-install-btn");
    expect(btn.textContent).toContain("重新安裝");
  });

  it("says 安裝 on an OFFLINE machine (nothing is talking to us)", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    const btn = await machineAction("mon-install-btn");
    expect(btn.textContent).toContain("安裝");
    // Not merely "contains 安裝" — 重新安裝 contains that substring too, so the
    // offline arm has to be pinned by the ABSENCE of the prefix or this test
    // passes against the exact regression it exists to catch.
    expect(btn.textContent).not.toContain("重新");
  });

  it("holds on the server-self row too — that is the destructive one", async () => {
    listMachines.mockResolvedValue([
      machine({ machineId: "m-server-self", displayName: "本機", online: true, isSelf: true }),
    ]);
    renderMonitor();
    const btn = await machineAction("mon-install-btn");
    expect(btn.textContent).toContain("重新安裝");
  });
});
