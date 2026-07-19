// AI Sessions table — outsource workers (O-xx) render alongside members (T-1bcf).
//
// The owner's AI Sessions panel used to list ONLY salaried members; outsource
// workers burn context and cost the same way, so they now appear in the SAME
// table. This test proves: (1) each outsource worker gets a row with its
// codename + task context and its runtime columns, (2) a worker that never
// reported a column shows the honest dash — never a fabricated value, and
// (3) the existing member rows are NOT disturbed by the addition.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MonSessionView } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [] as MonSessionView[],
  machines: [],
}));
const listOutsourceWorkers = vi.fn(async (): Promise<OutsourceWorkerView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => Promise.resolve([]),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => listOutsourceWorkers(),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

const session = (over: Partial<MonSessionView> = {}): MonSessionView => ({
  id: "mem-eva",
  name: "Eva",
  role: "engineer",
  model: "opus-4.8",
  effort: "",
  machine: "mbp5",
  account: "eva@example.test",
  status: "online",
  contextPct: 42,
  cost: 3.5,
  bankedCost: null,
  ...over,
});

const worker = (over: Partial<OutsourceWorkerView> = {}): OutsourceWorkerView => ({
  id: "ow-1",
  codename: "O-7",
  model: "opus-4.8",
  effort: "high",
  taskId: "task-1",
  taskTitle: "Migrate the billing importer",
  machine: "mbp5",
  account: "pool@example.test",
  contextPct: 71,
  cost: 5.25,
  bankedCost: 1.75,
  ...over,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage AI Sessions — outsource workers", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    listOutsourceWorkers.mockResolvedValue([]);
  });

  it("lists an outsource worker with its codename, task context and runtime columns", async () => {
    listOutsourceWorkers.mockResolvedValue([worker()]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    const cells = within(row);
    // codename + task-context sub-line (so the reader sees WHAT it is doing)
    expect(cells.getByText("O-7")).toBeTruthy();
    expect(cells.getByText("Migrate the billing importer")).toBeTruthy();
    // machine / account / model
    expect(cells.getByText("mbp5")).toBeTruthy();
    expect(cells.getByText("pool@example.test")).toBeTruthy();
    expect(cells.getByText("opus-4.8")).toBeTruthy();
    // context %
    expect(cells.getByText("71%")).toBeTruthy();
    // est.$ = live + banked = 5.25 + 1.75 = 7 (formatCost renders "$7")
    expect(cells.getByText("$7")).toBeTruthy();
    // an outsource tag distinguishes the row from a member row
    expect(cells.getByText("外包")).toBeTruthy();
  });

  it("shows an honest dash for every column the worker never reported", async () => {
    listOutsourceWorkers.mockResolvedValue([
      worker({
        id: "ow-2",
        codename: "O-9",
        model: "",
        machine: "",
        account: null,
        contextPct: null,
        cost: null,
        bankedCost: null,
        taskTitle: "",
        taskTypeName: "",
        taskNo: "",
      }),
    ]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    // codename still shows; every unreported column is a dash (never fabricated)
    expect(within(row).getByText("O-9")).toBeTruthy();
    const dashCells = within(row).getAllByText("—");
    // machine, account, model, context, est.$, and the task-context sub-line
    expect(dashCells.length).toBeGreaterThanOrEqual(5);
  });

  it("adds outsource rows WITHOUT disturbing the existing member rows", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [session()],
      machines: [],
    });
    listOutsourceWorkers.mockResolvedValue([worker(), worker({ id: "ow-2", codename: "O-9" })]);
    renderMonitor();

    // both outsource rows present
    const rows = await screen.findAllByTestId("mon-outsource-row");
    expect(rows).toHaveLength(2);
    // the member session row is still rendered next to them (not broken)
    expect(screen.getByText("Eva")).toBeTruthy();
  });
});
