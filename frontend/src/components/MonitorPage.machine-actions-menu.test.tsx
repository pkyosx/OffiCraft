// The 操作 column is one ⚙ button per machine row; its menu carries the row's
// install / uninstall / delete with their enable rules unchanged.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

describe("the machine row's ⚙ operations menu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
  });

  it("under an online remote machine gives 重新安裝, 解除安裝 and 刪除, all enabled, behind a gear named for the machine", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const gear = await screen.findByRole("button", { name: "機器操作（Alpha）" });
    expect(gear.getAttribute("aria-haspopup")).toBe("menu");
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(gear);
    const items = screen.getAllByRole("menuitem") as HTMLButtonElement[];
    expect(items.map((i) => [i.textContent, i.getAttribute("aria-disabled") === "true", i.title])).toEqual([
      ["重新安裝", false, ""],
      ["解除安裝", false, ""],
      ["刪除", false, ""],
    ]);
  });

  it("under an offline machine gives 安裝 and a disabled 解除安裝 that says why", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", { name: "機器操作（Alpha）" }));
    const items = screen.getAllByRole("menuitem") as HTMLButtonElement[];
    expect(items.map((i) => [i.textContent, i.getAttribute("aria-disabled") === "true", i.title])).toEqual([
      ["安裝", false, ""],
      ["解除安裝", true, "機器離線，無法解除安裝"],
      ["刪除", false, ""],
    ]);
  });

  it("under the server-self row gives a disabled 刪除", async () => {
    listMachines.mockResolvedValue([
      machine({ machineId: "m-server-self", displayName: "本機", online: true, isSelf: true }),
    ]);
    renderMonitor();
    expect((await machineAction("mon-delete-btn")).getAttribute("aria-disabled")).toBe("true");
  });

  it("under a chosen item that opens a dialog, focus is back on the gear, and returns there once the dialog is closed with a click", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const gear = await screen.findByRole("button", { name: "機器操作（Alpha）" });
    fireEvent.click(gear);
    fireEvent.click(screen.getByTestId("mon-install-btn"));
    const dialog = await screen.findByTestId("mon-install-dialog");
    expect(document.activeElement).toBe(gear);
    // A click on the dialog's close button moves focus there before the dialog
    // goes away, as a real browser does.
    const closeBtn = dialog.querySelector("button.mon-cmd__close") as HTMLButtonElement;
    closeBtn.focus();
    fireEvent.click(closeBtn);
    await waitFor(() => expect(screen.queryByTestId("mon-install-dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(gear));
  });

  it("under focus moved to another control while the dialog is open, closing the dialog leaves it there", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const gear = await screen.findByRole("button", { name: "機器操作（Alpha）" });
    fireEvent.click(gear);
    fireEvent.click(screen.getByTestId("mon-install-btn"));
    const dialog = await screen.findByTestId("mon-install-dialog");
    const elsewhere = document.getElementById("mon-onboard-entry") as HTMLButtonElement;
    elsewhere.focus();
    fireEvent.click(dialog.querySelector("button.mon-cmd__close") as HTMLButtonElement);
    await waitFor(() => expect(screen.queryByTestId("mon-install-dialog")).toBeNull());
    await new Promise((r) => setTimeout(r, 20));
    expect(document.activeElement).toBe(elsewhere);
  });

  it("under arrow keys, a disabled item is reached like any other", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", { name: "機器操作（Alpha）" }));
    const menu = screen.getByRole("menu");
    await waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("mon-install-btn")));
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-uninstall-btn"));
    expect(screen.getByTestId("mon-uninstall-btn").getAttribute("aria-disabled")).toBe("true");
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("mon-delete-btn"));
  });

  it("under a disabled item, choosing it does nothing and the menu stays open", async () => {
    listMachines.mockResolvedValue([machine({ online: false })]);
    renderMonitor();
    fireEvent.click(await screen.findByRole("button", { name: "機器操作（Alpha）" }));
    fireEvent.click(screen.getByTestId("mon-uninstall-btn"));
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(screen.queryByTestId("mon-uninstall-confirm")).toBeNull();
    expect(screen.queryByTestId("mon-uninstall-warn")).toBeNull();
  });

  it("under Esc closes the menu", async () => {
    listMachines.mockResolvedValue([machine({ online: true })]);
    renderMonitor();
    const gear = await screen.findByRole("button", { name: "機器操作（Alpha）" });
    fireEvent.click(gear);
    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
  });
});
