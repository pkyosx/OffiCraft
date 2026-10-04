// Hardware sample AGE on the machine panel — Monitor §2 (T-b36a).
//
// The server already refuses to serve the numbers of an expired hardware
// sample, so cpu/ram/power fall back to a dash. But a machine that has NEVER
// reported hardware shows exactly the same three dashes, and the two are
// completely different facts: one is a box nobody has ever measured (an older
// warden, a host that never ran the probe), the other is a box that WAS
// reporting and then went dark — the one an operator has to go look at.
//
// `hardware_ts` put the distinction on the wire and `hardware_stale` puts the
// server's verdict about it there too (the 90s window has one home; the browser
// never compares timestamps against its own clock). This file is the half that
// makes the distinction reach a human: an expired blank says WHY it is blank.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
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

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
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

const registryRow = (id: string): MachineView => ({
  machineId: id,
  displayName: "seth-m5",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** One monitoring card. `stale` is the SERVER's verdict about the hardware
 * sample — null means no sample was ever taken, and in that case the server
 * sends no stamp and no values either. */
const card = (
  stale: boolean | null,
  values: Pick<
    MonMachineView,
    "cpuPct" | "ramPct" | "batteryPct" | "acPower"
  > = { cpuPct: null, ramPct: null, batteryPct: null, acPower: null }
): MonMachineView => ({
  machine: "m-server-self",
  displayName: "seth-m5",
  agents: 0,
  accounts: [],
  ...values,
  hardwareTs: stale == null ? null : 1_700_000_000,
  hardwareStale: stale,
  // Clean by construction: this fixture is about AGE, not value validity.
  hardwareInvalid: [],
  runtimeCapabilitiesTs: null,
  runtimeCapabilitiesStale: null,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
  diskUsage: null,
});

function mount(machineCard: MonMachineView) {
  listMembers.mockResolvedValue([
    {
      id: "m-server-self",
      name: "warden",
      kind: "warden",
      desiredMachineId: "",
    } as unknown as Member,
  ]);
  listMachines.mockResolvedValue([registryRow("m-server-self")]);
  getMonitoring.mockResolvedValue({
    accounts: [],
    sessions: [],
    machines: [machineCard],
  });
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

const HARDWARE_CELLS = ["mon-cpu", "mon-ram", "mon-power"] as const;

describe("MonitorPage hardware freshness", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("marks the blank cells of an EXPIRED sample so they are not read as 'never measured'", async () => {
    mount(card(true));
    // Wait for the row, then assert on every hardware cell: the whole sample
    // expired together, so no cell may quietly look like an honest absence.
    await screen.findByTestId("mon-cpu");
    for (const testid of HARDWARE_CELLS) {
      const cell = screen.getByTestId(testid);
      expect(cell.textContent).toContain("—");
      const marker = within(cell).getByTestId("mon-hardware-stale");
      expect(marker.textContent).toBeTruthy();
      expect(marker.getAttribute("title")).toBeTruthy();
    }
  });

  it("shows a machine that NEVER reported hardware as a plain dash — nothing expired", async () => {
    mount(card(null));
    await screen.findByTestId("mon-cpu");
    for (const testid of HARDWARE_CELLS) {
      const cell = screen.getByTestId(testid);
      expect(cell.textContent).toContain("—");
      // The sentinel against over-marking: "never told us" is not "went dark".
      // If this dash wore the badge, the column would be back to one face for
      // two worlds — the exact defect this file exists to prevent, inverted.
      expect(within(cell).queryByTestId("mon-hardware-stale")).toBeNull();
    }
  });

  it("shows a FRESH sample's real values — including a real 0 and a real false", async () => {
    // The sentinel against a too-eager marker. 0% CPU and acPower:false are
    // measurements, not absences; a freshness rule that blanks or brands them
    // would be a new way of lying about a healthy machine.
    mount(
      card(false, {
        cpuPct: 0,
        ramPct: 0,
        batteryPct: 0,
        acPower: false,
      })
    );
    await screen.findByTestId("mon-cpu");
    expect(screen.getByTestId("mon-cpu").textContent).toContain("0%");
    expect(screen.getByTestId("mon-ram").textContent).toContain("0%");
    // acPower false must render as "on battery", never fall through to the dash.
    expect(screen.getByTestId("mon-power").textContent).not.toBe("—");
    for (const testid of HARDWARE_CELLS) {
      expect(
        within(screen.getByTestId(testid)).queryByTestId("mon-hardware-stale")
      ).toBeNull();
    }
  });
});
