// Machine id badge — Monitor §2 machine panel.
//
// Each machine row shows its stable machine id (the warden member's own id /
// token sub) beside the editable display name, mirroring the member detail
// panel's id badge. The id is the machine's identity and is never editable.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
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

describe("MonitorPage machine id badge", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([
      machine("m-0e7034bd5140", "Eva"),
      machine("m-6e332737fc31", "Seth-M1"),
    ]);
  });

  it("shows each machine's stable id beside its display name", async () => {
    renderMonitor();
    const ids = await screen.findAllByTestId("mon-machine-id");
    const texts = ids.map((el) => el.textContent);
    expect(texts).toContain("m-0e7034bd5140");
    expect(texts).toContain("m-6e332737fc31");
  });

  it("keeps the id chip and the online dot in the SAME cell as the name", async () => {
    // T-674d: 機器 and 狀態 were two columns, which left the name cell narrow
    // enough that the id chip wrapped to a second line on every row. Merging
    // them is the fix, so the invariant to hold is structural — all three live
    // in one <td>. Asserting only "the id renders" would keep passing if a
    // later change split the columns back apart.
    renderMonitor();
    const ids = await screen.findAllByTestId("mon-machine-id");
    const cell = ids[0].closest("td")!;
    expect(cell.querySelector(".mon-machine-id")).toBeTruthy();
    expect(cell.querySelector('[data-testid="mon-machine-online"]')).toBeTruthy();
    // …and there is no separate 狀態 column left holding a second dot.
    expect(screen.getAllByTestId("mon-machine-online").length).toBe(ids.length);
  });
});

describe("MonitorPage machine online dot", () => {
  const tooltipLines = () =>
    screen.queryAllByRole("tooltip").map((h) => Array.from(h.children).map((l) => l.textContent));

  /** The 機器 cell of each row, keyed by its id chip. */
  async function machineCells() {
    const ids = await screen.findAllByTestId("mon-machine-id");
    return Object.fromEntries(ids.map((id) => [id.textContent!, id.closest("td")!]));
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
    expect(Object.keys(cells)).toEqual(["m-0e7034bd5140", "m-6e332737fc31"]);
    // The cell's visible text is the name and the id; the state word is gone.
    expect(Object.values(cells).map((td) => td.textContent)).toEqual(["Evam-0e7034bd5140", "Seth-M1m-6e332737fc31"]);
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
