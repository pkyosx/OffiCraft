// Install button on an ONLINE machine — Monitor §2 machine panel.
//
// The button used to be `disabled={(m.isSelf && bootstrapBusy) || m.online}`,
// which collapsed two actions of wildly different danger into one gate:
//   • server-self → a real in-place reinstall that overwrites the LIVE warden
//     (every member on that box drops; irreversible)
//   • any other machine → renders a command to copy; sends nothing
// so a perfectly healthy remote machine could not even show its install command.
//
// The gate is gone. The dangerous half is now guarded by a confirm dialog whose
// wording names the real consequence — and the assertion this file exists for is
// that CANCELLING that dialog fires NO request at all.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));
const bootstrapOnServer = vi.fn(
  async (_id: string) => ({ ok: true, exitCode: 0, log: "" })
);
const getMachineBootCommand = vi.fn(async (_id: string) => "curl … | sh");

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
    getMachineBootCommand: (id: string) => getMachineBootCommand(id),
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

const selfOnline: MachineView = {
  machineId: "m-server-self",
  displayName: "本機",
  online: true,
  isSelf: true,
  ...base,
};

const remoteOnline: MachineView = {
  machineId: "m-alpha",
  displayName: "Alpha",
  online: true,
  isSelf: false,
  ...base,
};

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage install on an online machine", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    bootstrapOnServer.mockResolvedValue({ ok: true, exitCode: 0, log: "" });
    getMachineBootCommand.mockResolvedValue("curl … | sh");
  });

  describe("server-self row, machine online (the destructive half)", () => {
    beforeEach(() => {
      listMachines.mockResolvedValue([selfOnline]);
    });

    it("is clickable and opens a confirm naming the real consequence", async () => {
      renderMonitor();
      const btn = (await screen.findByTestId(
        "mon-install-btn"
      )) as HTMLButtonElement;
      expect(btn.disabled).toBe(false);

      fireEvent.click(btn);

      const dialog = await screen.findByTestId("mon-bootstrap-confirm");
      // Not a shared canned line: it must say what is actually lost.
      expect(dialog.textContent).toContain("覆蓋");
      expect(dialog.textContent).toContain("斷線");
      expect(dialog.textContent).toContain("不可逆");
      expect(dialog.textContent).toContain("本機");
      // Nothing has been asked of the server yet — the dialog is a question.
      expect(bootstrapOnServer).not.toHaveBeenCalled();
      // The classes the CT theme guard measures. jsdom cannot compute colour,
      // but it CAN pin that this dialog wears the already-themed containers —
      // which is what makes the guard's measurements about this dialog.
      expect(dialog.querySelector(".mon-confirm__title")).toBeTruthy();
      expect(dialog.querySelector(".mon-confirm__body")).toBeTruthy();
      // No colour of its own anywhere in the dialog (token layer only).
      for (const el of dialog.querySelectorAll<HTMLElement>("*")) {
        expect(el.style.color).toBe("");
        expect(el.style.backgroundColor).toBe("");
      }
    });

    // 🔴 THE assertion of this file.
    it("sends NO request when the confirm is cancelled", async () => {
      renderMonitor();
      fireEvent.click(await screen.findByTestId("mon-install-btn"));
      await screen.findByTestId("mon-bootstrap-confirm");

      fireEvent.click(screen.getByTestId("mon-bootstrap-cancel-btn"));

      expect(bootstrapOnServer).not.toHaveBeenCalled();
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
    });

    it("runs the in-place install exactly once when confirmed", async () => {
      renderMonitor();
      fireEvent.click(await screen.findByTestId("mon-install-btn"));
      await screen.findByTestId("mon-bootstrap-confirm");

      fireEvent.click(screen.getByTestId("mon-bootstrap-confirm-btn"));

      expect(bootstrapOnServer).toHaveBeenCalledTimes(1);
      expect(bootstrapOnServer).toHaveBeenCalledWith("m-server-self");
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
    });
  });

  describe("remote row, machine online (the harmless half)", () => {
    beforeEach(() => {
      listMachines.mockResolvedValue([remoteOnline]);
    });

    it("can be clicked and shows the install command dialog", async () => {
      renderMonitor();
      const btn = (await screen.findByTestId(
        "mon-install-btn"
      )) as HTMLButtonElement;
      expect(btn.disabled).toBe(false);

      fireEvent.click(btn);

      const dialog = await screen.findByTestId("mon-install-dialog");
      expect(dialog.textContent).toContain("Alpha");
      expect(screen.getByTestId("mon-copy-boot-cmd-btn")).toBeTruthy();
      // The copy-command screen is not the destructive one: no confirm in front
      // of it, and opening it neither installs nor mints a token.
      expect(screen.queryByTestId("mon-bootstrap-confirm")).toBeNull();
      expect(bootstrapOnServer).not.toHaveBeenCalled();
      expect(getMachineBootCommand).not.toHaveBeenCalled();
    });
  });
});
