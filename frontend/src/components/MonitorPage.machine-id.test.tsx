// Machine id — Monitor §2 machine panel. The stable machine id (the warden
// member's own id / token sub) is not on the row; it is shown in full in the
// machine's 詳情 dialog.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import { machineAction } from "./machineActions.testHelper";
import type { Member, MachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
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

const machine = (id: string, displayName: string, online = true): MachineView => ({
  machineId: id,
  displayName,
  online,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage machine id", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([
      machine("m-0e7034bd5140", "Eva"),
      machine("m-6e332737fc31", "Seth-M1"),
    ]);
  });

  it("the row carries no id pill: the 機器 cell is the online dot and the name", async () => {
    renderMonitor();
    const names = await screen.findAllByTestId("mon-machine-menu");
    expect(names.map((n) => n.closest("td")!.textContent)).toEqual(["Eva", "Seth-M1"]);
    expect(screen.queryByTestId("mon-machine-id")).toBeNull();
    expect(document.querySelector(".mon-machine-id")).toBeNull();
    expect(screen.queryByText("m-0e7034bd5140")).toBeNull();
  });

  it("the full id is in the machine's 詳情 dialog", async () => {
    renderMonitor();
    fireEvent.click(await machineAction("mon-detail-btn", 1));
    const dialog = screen.getByRole("dialog", { name: "機器詳情" });
    expect(within(dialog).getByTestId("mon-machine-detail-name").textContent).toBe("Seth-M1");
    expect(within(dialog).getByTestId("mon-machine-detail-id").textContent).toBe("m-6e332737fc31");
  });

  it("keeps the online dot in the SAME cell as the name, one per row", async () => {
    // 機器 and 狀態 are one column; asserting only "the dot renders" would keep
    // passing if a later change split the columns back apart.
    renderMonitor();
    const names = await screen.findAllByTestId("mon-machine-menu");
    const cell = names[0].closest("td")!;
    expect(cell.querySelector('[data-testid="mon-machine-online"]')).toBeTruthy();
    expect(screen.getAllByTestId("mon-machine-online").length).toBe(names.length);
  });
});

describe("MonitorPage machine online dot", () => {
  const tooltipLines = () =>
    screen.queryAllByRole("tooltip").map((h) => Array.from(h.children).map((l) => l.textContent));

  /** The 機器 cell of each row, keyed by the machine's name. */
  async function machineCells() {
    const names = await screen.findAllByTestId("mon-machine-menu");
    return Object.fromEntries(names.map((n) => [n.textContent!, n.closest("td")!]));
  }

  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([
      machine("m-0e7034bd5140", "Eva", true),
      machine("m-6e332737fc31", "Seth-M1", false),
    ]);
  });

  it("shows the dot alone in the 機器 cell, with its state as the dot's accessible name", async () => {
    renderMonitor();
    const cells = await machineCells();
    expect(Object.keys(cells)).toEqual(["Eva", "Seth-M1"]);
    // The cell's visible text is the name alone; the state word is gone.
    expect(Object.values(cells).map((td) => td.textContent)).toEqual(["Eva", "Seth-M1"]);
    expect(screen.getAllByRole("img", { name: /^(線上|離線)$/ }).map((el) => el.getAttribute("aria-label"))).toEqual([
      "線上",
      "離線",
    ]);
    expect(tooltipLines()).toEqual([]);
  });

  it("under a hover on each dot, its state shows at once and leaves with the pointer", async () => {
    renderMonitor();
    await machineCells();
    const [on, off] = screen.getAllByTestId("mon-machine-online");
    expect([on.hasAttribute("title"), off.hasAttribute("title")]).toEqual([false, false]);
    fireEvent.mouseEnter(on);
    expect(tooltipLines()).toEqual([["線上"]]);
    fireEvent.mouseLeave(on);
    expect(tooltipLines()).toEqual([]);
    fireEvent.mouseEnter(off);
    expect(tooltipLines()).toEqual([["離線"]]);
    fireEvent.mouseLeave(off);
    expect(tooltipLines()).toEqual([]);
  });

  it("under a click or tap on a dot, its state stays shown after the pointer leaves, and a second click closes it", async () => {
    renderMonitor();
    await machineCells();
    const [on, off] = screen.getAllByTestId("mon-machine-online");
    fireEvent.click(off);
    expect(tooltipLines()).toEqual([["離線"]]);
    expect(screen.getByRole("tooltip").className).toBe("instant-hint instant-hint--pinned");
    fireEvent.mouseLeave(off);
    fireEvent.blur(off);
    expect(tooltipLines()).toEqual([["離線"]]);
    fireEvent.click(off);
    expect(tooltipLines()).toEqual([]);

    fireEvent.click(on);
    expect(tooltipLines()).toEqual([["線上"]]);
    fireEvent.click(document.body);
    expect(tooltipLines()).toEqual([]);
  });

  it("under English, the dot is named and hinted in English", async () => {
    window.localStorage.setItem("oc.language", "en");
    try {
      renderMonitor();
      await machineCells();
      const [on, off] = screen.getAllByTestId("mon-machine-online");
      expect([on.getAttribute("aria-label"), off.getAttribute("aria-label")]).toEqual(["Online", "Offline"]);
      fireEvent.click(off);
      expect(tooltipLines()).toEqual([["Offline"]]);
    } finally {
      window.localStorage.removeItem("oc.language");
    }
  });
});
