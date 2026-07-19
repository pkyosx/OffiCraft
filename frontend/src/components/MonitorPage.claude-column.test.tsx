// claude CLI version column — Monitor §2 machine table (T-7c5b).
//
// The warden-probed claude version is a table column next to Status: a
// machine that reported one shows it verbatim, a machine that never probed
// shows the honest dash. The old T-97ee name-stack small line (version +
// credential source + warning/no-cred badges) is gone entirely — the probe
// data still arrives on the wire but only the version is displayed.

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

const machine = (
  id: string,
  claude: Pick<
    MachineView,
    "claudeVersion" | "claudeCredSource" | "claudeSubReadable"
  >
): MachineView => ({
  machineId: id,
  displayName: id,
  online: true,
  isSelf: false,
  binStatus: null,
  ...claude,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage claude version column", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
  });

  it("shows the reported version in the claude column", async () => {
    listMachines.mockResolvedValue([
      machine("m-probed", {
        claudeVersion: "2.1.212",
        claudeCredSource: "file",
        claudeSubReadable: true,
      }),
    ]);
    renderMonitor();
    const cell = await screen.findByTestId("mon-claude-version");
    expect(cell.textContent).toBe("2.1.212");
  });

  it("shows a dash when the machine never reported a version", async () => {
    listMachines.mockResolvedValue([
      machine("m-old-warden", {
        claudeVersion: null,
        claudeCredSource: null,
        claudeSubReadable: null,
      }),
    ]);
    renderMonitor();
    const cell = await screen.findByTestId("mon-claude-version");
    expect(cell.textContent).toBe("—");
  });

  it("heads the claude column next to Status and keeps the empty row spanning it", async () => {
    listMachines.mockResolvedValue([]);
    const { container } = renderMonitor();
    await screen.findAllByRole("columnheader");
    const table = container.querySelector("table.mon-table")!;
    const headers = Array.from(table.querySelectorAll("thead th"));
    expect(headers[2].textContent).toBe("Claude");
    // The empty-state cell spans the header count — a stale colSpan after a
    // column change misaligns the whole table without failing anything else.
    const emptyCell = table.querySelector("tbody td[colspan]")!;
    expect(emptyCell.getAttribute("colspan")).toBe(String(headers.length));
  });

  it("renders no name-stack claude line and no credential badges", async () => {
    listMachines.mockResolvedValue([
      machine("m-no-cred", {
        claudeVersion: "2.1.212",
        claudeCredSource: "none",
        claudeSubReadable: false,
      }),
      machine("m-sub-unreadable", {
        claudeVersion: "2.1.211",
        claudeCredSource: "keychain",
        claudeSubReadable: false,
      }),
    ]);
    renderMonitor();
    await screen.findAllByTestId("mon-claude-version");
    expect(screen.queryByTestId("mon-claude-line")).toBeNull();
    expect(screen.queryByTestId("mon-claude-none-badge")).toBeNull();
    expect(screen.queryByTestId("mon-claude-sub-badge")).toBeNull();
  });
});
