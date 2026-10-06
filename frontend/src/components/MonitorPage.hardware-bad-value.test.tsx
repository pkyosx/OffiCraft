// Wrongly-typed hardware values on the machine panel — Monitor §2 (T-aad2).
//
// T-b36a split "blank" into two worlds and put a badge on the second: nobody
// ever measured this box, vs it WAS measured and the sample expired. There is a
// third, and it was invisible.
//
// The telemetry blocks are permissive by owner ruling (rc-55861dd893c6): a
// warden that sends `cpu_pct: "47"` gets a 200, the value is stored verbatim,
// and the reader — which needs a number — serves null. The resulting row was
// byte-for-byte the row of a machine with no CPU probe at all, so a broken
// reporter and a host that has never reported were the same three dashes on
// screen. `hardware_invalid` puts the difference on the wire; this file is the
// half that makes it reach a human, and the half that keeps it from over-firing
// on the healthy rows.
//
// It reaches the human THROUGH THE PRODUCTION JOIN, which is why this file
// mounts both feeds rather than handing the component one object. The machine
// table iterates registry rows from `listMachines` (MachineDTO — identity,
// online, actions) and looks the hardware up per row from `getMonitoring`
// (MonitoringMachineDTO). `hardware_invalid` rides the SECOND one, next to the
// values it explains. A fixture that skipped the join would prove the chip
// renders and prove nothing about whether the field survives the trip to the
// cell — and "fixed but never reaches the screen" is the same as unfixed.

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

/** One FRESH monitoring card. `invalid` is the server's per-key verdict; the
 * values it names are null exactly as they arrive from the server, because a
 * value it could not read is a value it does not serve. */
const card = (
  invalid: string[],
  values: Partial<
    Pick<MonMachineView, "cpuPct" | "ramPct" | "batteryPct" | "acPower">
  > = {}
): MonMachineView => ({
  machine: "m-server-self",
  displayName: "seth-m5",
  agents: 0,
  accounts: [],
  cpuPct: null,
  ramPct: null,
  batteryPct: null,
  acPower: null,
  ...values,
  // Fresh by construction: this file is about value VALIDITY, and a stale row
  // would supply its own (different) explanation for the same blanks.
  hardwareTs: Math.floor(Date.now() / 1000),
  hardwareStale: false,
  hardwareInvalid: invalid,
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

describe("MonitorPage wrongly-typed hardware values", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("marks the cell of a value that was reported but is unreadable", async () => {
    mount(card(["cpu_pct"]));
    const cpu = await screen.findByTestId("mon-cpu");
    expect(cpu.textContent).toContain("—");
    const marker = within(cpu).getByTestId("mon-hardware-bad");
    expect(marker.textContent).toBeTruthy();
    // The hint is what turns a badge into an instruction; a wordless chip is
    // one more thing to guess at.
    expect(marker.getAttribute("title")).toBeTruthy();
  });

  it("marks ONLY the broken cell — a bad cpu_pct says nothing about ram", async () => {
    mount(card(["cpu_pct"], { ramPct: 61 }));
    await screen.findByTestId("mon-cpu");
    const ram = screen.getByTestId("mon-ram");
    expect(ram.textContent).toContain("61%");
    expect(within(ram).queryByTestId("mon-hardware-bad")).toBeNull();
    expect(
      within(screen.getByTestId("mon-power")).queryByTestId("mon-hardware-bad")
    ).toBeNull();
  });

  it("leaves a machine that never measured anything unmarked", async () => {
    // THE POINT OF THE WHOLE TICKET, as a comparison: this row and the first
    // one both show a dash for CPU, and before hardware_invalid existed they
    // were indistinguishable. Marking this one instead would just move the lie.
    mount(card([]));
    await screen.findByTestId("mon-cpu");
    for (const testid of ["mon-cpu", "mon-ram", "mon-power"]) {
      expect(
        within(screen.getByTestId(testid)).queryByTestId("mon-hardware-bad")
      ).toBeNull();
    }
  });

  it("never marks a healthy row, including a real 0 and a real false", async () => {
    // The false-positive sentinel. 0% and "on battery" are measurements; a mark
    // on them would be a new way of calling a healthy machine broken.
    mount(card([], { cpuPct: 0, ramPct: 0, batteryPct: 0, acPower: false }));
    await screen.findByTestId("mon-cpu");
    expect(screen.getByTestId("mon-cpu").textContent).toContain("0%");
    expect(screen.getByTestId("mon-power").textContent).not.toBe("—");
    for (const testid of ["mon-cpu", "mon-ram", "mon-power"]) {
      expect(
        within(screen.getByTestId(testid)).queryByTestId("mon-hardware-bad")
      ).toBeNull();
    }
  });

  it("marks the power cell for either of the two keys behind it", async () => {
    // The power cell renders acPower AND batteryPct, so it must answer for
    // both — a broken battery_pct with a healthy ac_power still leaves half
    // that cell unreadable, and a cell that only watched one key would say
    // nothing about the other.
    mount(card(["battery_pct"], { acPower: true }));
    const power = await screen.findByTestId("mon-power");
    expect(within(power).getByTestId("mon-hardware-bad")).toBeTruthy();
  });

  it("keeps the bad-value mark distinct from the stale mark", async () => {
    // Same chip for both would collapse the two worlds back into one: "nobody
    // has looked lately" and "the reporter is sending garbage" send an operator
    // to different places, and a shared badge answers neither question.
    mount(card(["cpu_pct"]));
    const cpu = await screen.findByTestId("mon-cpu");
    expect(within(cpu).queryByTestId("mon-hardware-stale")).toBeNull();
    expect(within(cpu).getByTestId("mon-hardware-bad")).toBeTruthy();
  });
});
