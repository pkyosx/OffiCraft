// AI Sessions table — outsource workers (O-xx) render alongside members (T-1bcf).
//
// The owner's AI Sessions panel used to list ONLY salaried members; outsource
// workers burn context and cost the same way, so they now appear in the SAME
// table. This test proves: (1) each outsource worker gets a row with its
// codename + task context and its runtime columns, (2) a worker that never
// reported a column shows the honest dash — never a fabricated value, and
// (3) the existing member rows are NOT disturbed by the addition.
//
// T-cf32 also adds: the whole outsource row is clickable (owner ruling, card
// rc-d3dad3e0c6b5 option 0), navigating to the office page's EXISTING worker
// detail route — same whole-row affordance as the member SessionRow. Layout
// (the long task title wrapping instead of stretching the table) is a geometry
// contract jsdom cannot see, guarded separately in
// visual-guards/monitor-outsource-sub-wrap.ct.spec.tsx.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within, fireEvent } from "@testing-library/react";
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
    // Routing rides on the real window.location.hash (lib/hashRoute.ts) — reset
    // it so a route left by a previous test cannot leak into the next.
    window.location.hash = "";
  });

  it("lists an outsource worker with its codename, task context and runtime columns", async () => {
    listOutsourceWorkers.mockResolvedValue([worker()]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    const cells = within(row);
    // outsource identity label 「外包 · 代號」 (T-3ed8, owner 2026-07-20: the
    // 「外包 · 」prefix now carries the outsource distinction — the standalone
    // badge is gone) + task-context sub-line (so the reader sees WHAT it does)
    expect(cells.getByText("外包 · O-7")).toBeTruthy();
    expect(cells.getByText("Migrate the billing importer")).toBeTruthy();
    // machine / account / model
    expect(cells.getByText("mbp5")).toBeTruthy();
    expect(cells.getByText("pool@example.test")).toBeTruthy();
    expect(cells.getByText("opus-4.8")).toBeTruthy();
    // context %
    expect(cells.getByText("71%")).toBeTruthy();
    // est.$ = live + banked = 5.25 + 1.75 = 7 (formatCost renders "$7")
    expect(cells.getByText("$7")).toBeTruthy();
    // the row is distinguished from a member row by the 「外包 · 」label prefix
    // (asserted above), no longer by a standalone tag chip.
    expect(cells.queryByText("外包")).toBeNull();
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
    expect(within(row).getByText("外包 · O-9")).toBeTruthy();
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

  // T-cf32 — owner ruling (card rc-d3dad3e0c6b5, option 0): the whole outsource
  // row is clickable, the SAME affordance as the member SessionRow, and it
  // navigates to the office page's EXISTING worker detail route
  // (#office/worker/<id>) — a real destination, not an invented one, and not a
  // separate avatar hit-target (that option was shown to the owner and declined).
  it("clicking the outsource row navigates to that worker's office detail route", async () => {
    listOutsourceWorkers.mockResolvedValue([worker()]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    fireEvent.click(row);

    expect(window.location.hash).toBe("#office/worker/ow-1");
  });

  it("navigates on Enter and Space for keyboard parity with the member row", async () => {
    listOutsourceWorkers.mockResolvedValue([worker()]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(row.getAttribute("role")).toBe("button");
    expect(row.getAttribute("tabindex")).toBe("0");

    fireEvent.keyDown(row, { key: "Enter" });
    expect(window.location.hash).toBe("#office/worker/ow-1");

    window.location.hash = "";
    fireEvent.keyDown(row, { key: " " });
    expect(window.location.hash).toBe("#office/worker/ow-1");
  });

  // SENTINEL — the member row's own click-through must be UNCHANGED by the
  // outsource row change: it still carries role/tabindex and still routes to
  // Monitor's own member detail (#monitor/member/<id>), NOT the office worker
  // route. Proves the new outsource affordance did not disturb the member path.
  it("SENTINEL: the member row still routes to #monitor/member/<id>, untouched", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      sessions: [session()],
      machines: [],
    });
    listMembers.mockResolvedValue([
      { id: "mem-eva", name: "Eva", role: "engineer", status: "online" } as Member,
    ]);
    listOutsourceWorkers.mockResolvedValue([worker()]);
    renderMonitor();

    await screen.findByTestId("mon-outsource-row");
    const memberRow = screen.getByText("Eva").closest("tr")!;
    expect(memberRow.getAttribute("role")).toBe("button");
    expect(memberRow.getAttribute("tabindex")).toBe("0");

    fireEvent.click(memberRow);
    expect(window.location.hash).toBe("#monitor/member/mem-eva");
  });
});
