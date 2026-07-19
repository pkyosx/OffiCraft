// Hardware telemetry join — Monitor §2 machine panel.
//
// Regression lock: the monitoring telemetry card is keyed by the host
// machine-id (the warden's own id, e.g. `m-server-self`) — the SAME value as
// the registry row's `machineId`. So a machine row whose machine-id matches a
// telemetry card MUST render that card's CPU/RAM/power numbers, NOT the honest
// dash. The old code bounced through the warden member's `desiredMachineId`
// (empty for a self-hosting warden), so every hardware cell showed "—" even
// though the card carried real cpu_pct=17.2. This test would have caught that.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
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
    subscribeEvents: () => () => {},
  },
}));

const machine = (id: string, displayName: string): MachineView => ({
  machineId: id,
  displayName,
  online: true,
  isSelf: false,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

const card = (machineId: string): MonMachineView => ({
  machine: machineId,
  displayName: "seth-m5",
  agents: 0,
  accounts: [],
  cpuPct: 17.2,
  ramPct: 41,
  batteryPct: 88,
  acPower: false,
  binStatus: null,
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

describe("MonitorPage hardware telemetry join", () => {
  beforeEach(() => {
    // A self-hosting warden: its member.desiredMachineId is empty. The registry
    // row and the telemetry card agree on the machine-id `m-server-self`.
    listMembers.mockResolvedValue([
      {
        id: "m-server-self",
        name: "warden",
        kind: "warden",
        desiredMachineId: "",
      } as unknown as Member,
    ]);
    listMachines.mockResolvedValue([machine("m-server-self", "seth-m5")]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [],
      machines: [card("m-server-self")],
    });
  });

  it("renders the machine's cpu/ram/power from the matching telemetry card, not a dash", async () => {
    renderMonitor();
    // Wait for the row to mount by its stable id badge.
    await screen.findByText("m-server-self");
    // The hardware cells carry the real numbers keyed by machine-id.
    expect(await screen.findByText("17.2%")).toBeTruthy();
    expect(screen.getByText("41%")).toBeTruthy();
    // Battery power cell: 🔋 88% (on battery).
    expect(screen.getByText(/88%/)).toBeTruthy();
  });
});
