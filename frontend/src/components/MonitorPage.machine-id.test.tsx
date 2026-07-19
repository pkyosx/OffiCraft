// Machine id badge — Monitor §2 machine panel.
//
// Each machine row shows its stable machine id (the warden member's own id /
// token sub) beside the editable display name, mirroring the member detail
// panel's id badge. The id is the machine's identity and is never editable.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
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
});
