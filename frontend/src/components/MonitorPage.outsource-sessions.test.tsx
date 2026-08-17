// AI Sessions table — outsource workers (O-xx) render alongside members (T-1bcf).
//
// The owner's AI Sessions panel used to list ONLY salaried members; outsource
// workers burn context and cost the same way, so they now appear in the SAME
// table. This test proves: (1) each outsource worker gets a row with its
// codename + task context and its runtime columns, (2) a worker that never
// reported a column shows the honest dash — never a fabricated value, and
// (3) the existing member rows are NOT disturbed by the addition.
//
// T-e12c re-sources those runtime columns: a worker row's model / context /
// cost / effort / machine / account now come from the worker's OWN row in the unified
// `GET /api/monitoring` sessions array (joined by its `ow-` id), never from the
// outsource-workers endpoint's CONFIGURED model/effort — that pair is always
// populated, which is exactly why an unreported effort was invisible here. Same
// test file also pins the missing-value guard (see the guard describe below).
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

const session = (over: Partial<MonSessionView> = {}): MonSessionView => ({
  id: "mem-eva",
  name: "Eva",
  role: "engineer",
  model: "opus-4.8",
  effort: "",
  machine: "mbp5",
  account: "eva@example.test",
  runtime: "claude",
  status: "online",
  contextPct: 42,
  compactionCount: null,
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
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({
          id: "ow-1",
          model: "opus-4.8",
          effort: "high",
          machine: "obs-mbp5",
          account: "obs@example.test",
          contextPct: 71,
          cost: 5.25,
          bankedCost: 1.75,
        }),
      ],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    const cells = within(row);
    // outsource identity label 「外包 · 代號」 (T-3ed8, owner 2026-07-20: the
    // 「外包 · 」prefix now carries the outsource distinction — the standalone
    // badge is gone) + task-context sub-line (so the reader sees WHAT it does)
    expect(cells.getByText("外包 · O-7")).toBeTruthy();
    expect(cells.getByText("Migrate the billing importer")).toBeTruthy();
    // machine / account / model — all three off the SESSION. The worker DTO's
    // own "mbp5" / "pool@example.test" are the dispatch target and the DTO's
    // account fold; they are the negative controls here.
    expect(cells.getByText("obs-mbp5")).toBeTruthy();
    expect(cells.getByText("obs@example.test")).toBeTruthy();
    expect(cells.queryByText("mbp5")).toBeNull();
    expect(cells.queryByText("pool@example.test")).toBeNull();
    expect(cells.getByText("opus-4.8")).toBeTruthy();
    // context %
    expect(cells.getByText("71%")).toBeTruthy();
    // est.$ = live + banked = 5.25 + 1.75 = 7 (formatCost renders "$7")
    expect(cells.getByText("$7")).toBeTruthy();
    // the row is distinguished from a member row by the 「外包 · 」label prefix
    // (asserted above), no longer by a standalone tag chip.
    expect(cells.queryByText("外包")).toBeNull();
  });

  it("does not also render an outsource roster row as a salaried AI session", async () => {
    // The member-list contract now includes ow-* rows, but this table has its
    // own worker lane below and must not duplicate or reclassify them.
    listMembers.mockResolvedValue([
      {
        id: "mira", name: "Mira", role: "assistant", roleName: "", kind: "assistant",
        status: "online", lifecycle: "online", model: "", effort: "medium", runtime: "claude",
        machine: "mbp5", desiredMachineId: "mbp5", account: "", contextPct: null, estimatedCost: null, bankedCost: null,
        tmuxSession: "", refocusSince: null, lastOp: "", lastOpOk: null, lastOpLog: "", lastOpAt: null, unreadCount: 0,
      } as Member,
      {
        id: "ow-in-list", name: "O-12", role: "", roleName: "", kind: "outsource",
        status: "online", lifecycle: "online", model: "", effort: "medium", runtime: "codex",
        machine: "mbp5", desiredMachineId: "mbp5", account: "", contextPct: null, estimatedCost: null, bankedCost: null,
        tmuxSession: "", refocusSince: null, lastOp: "", lastOpOk: null, lastOpLog: "", lastOpAt: null, unreadCount: 0,
      } as Member,
    ]);
    getMonitoring.mockResolvedValue({
      accounts: [], machines: [], sessions: [session(), session({ id: "ow-in-list", name: "O-12" })],
    });
    renderMonitor();

    expect(await screen.findByText("Eva")).toBeTruthy();
    expect(screen.queryByText("O-12")).toBeNull();
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

  // ── T-e12c: the worker row's telemetry columns come from the SESSION ──────
  // The worker DTO keeps supplying identity; model / effort / context / cost
  // come from the session joined by id. These two are a pair: one proves the
  // reported value wins, the other proves an unreported one stays blank instead
  // of silently falling back to the configured value (the old bug's shape).

  it("shows the worker's REPORTED effort, not the effort it was configured with", async () => {
    listOutsourceWorkers.mockResolvedValue([worker({ effort: "high" })]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ id: "ow-1", model: "sonnet-4.9", effort: "low" })],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(within(row).getByText("low")).toBeTruthy();
    expect(within(row).queryByText("high")).toBeNull();
    // …and the model column is the reported one too, not the configured
    // "opus-4.8" the worker fixture carries.
    expect(within(row).getByText("sonnet-4.9")).toBeTruthy();
    expect(within(row).queryByText("opus-4.8")).toBeNull();
    // …and it appears exactly ONCE: the worker's session row rides the same
    // array the member rows come from, so the member lane must skip it rather
    // than draw a second, identity-less row for the same agent.
    expect(screen.getAllByText("sonnet-4.9")).toHaveLength(1);
  });

  it("leaves the effort blank when the worker never reported one, even though it is configured", async () => {
    listOutsourceWorkers.mockResolvedValue([worker({ effort: "high" })]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ id: "ow-1", effort: "" })],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(within(row).queryByText("high")).toBeNull();
  });

  // 機器 / 帳號 (owner ruling, card rc-4a83a5723896 option ①). The worker DTO's
  // `machine` is the SPAWN DISPATCH TARGET — server projectWorker prefers the
  // in-memory target over the observed host — so on a surface that must show
  // reported state it is an intent, not a fact. The owner was told the price
  // (a just-dispatched worker shows an empty 機器 cell until it connects) and
  // took it, so these two pin the blank as the WANTED outcome.

  it("shows the OBSERVED machine and account, not the ones the worker DTO carries", async () => {
    listOutsourceWorkers.mockResolvedValue([
      worker({ machine: "dispatch-target", account: "dto@example.test" }),
    ]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({
          id: "ow-1",
          machine: "observed-host",
          account: "observed@example.test",
        }),
      ],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(within(row).getByText("observed-host")).toBeTruthy();
    expect(within(row).getByText("observed@example.test")).toBeTruthy();
    expect(within(row).queryByText("dispatch-target")).toBeNull();
    expect(within(row).queryByText("dto@example.test")).toBeNull();
  });

  it("dashes 機器/帳號 for a worker that was dispatched but has not reported yet", async () => {
    listOutsourceWorkers.mockResolvedValue([
      worker({ machine: "dispatch-target", account: "dto@example.test" }),
    ]);
    getMonitoring.mockResolvedValue({ accounts: [], machines: [], sessions: [] });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    // The dispatch target must NOT stand in for a host that never checked in.
    expect(within(row).queryByText("dispatch-target")).toBeNull();
    expect(within(row).queryByText("dto@example.test")).toBeNull();
    const cells = row.querySelectorAll("td");
    expect(cells[1].textContent).toBe("—");
    expect(cells[2].textContent).toBe("—");
  });

  it("dashes 機器/帳號 when the session is live but reported neither", async () => {
    listOutsourceWorkers.mockResolvedValue([
      worker({ machine: "dispatch-target", account: "dto@example.test" }),
    ]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ id: "ow-1", machine: "", account: "", contextPct: 71 })],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    const cells = row.querySelectorAll("td");
    expect(cells[1].textContent).toBe("—");
    expect(cells[2].textContent).toBe("—");
    // …and the blank account keeps the member row's muted styling, unchanged.
    expect(cells[2].className).toContain("mon-muted");
  });

  it("dashes the worker's telemetry columns when it has no session row at all", async () => {
    listOutsourceWorkers.mockResolvedValue([worker({ effort: "high" })]);
    getMonitoring.mockResolvedValue({ accounts: [], machines: [], sessions: [] });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    // identity survives; every telemetry column is the honest dash
    expect(within(row).getByText("外包 · O-7")).toBeTruthy();
    expect(within(row).queryByText("high")).toBeNull();
    expect(within(row).queryByText("opus-4.8")).toBeNull();
    expect(within(row).queryByText("$7")).toBeNull();
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

// ── T-e12c: the missing-value guard ────────────────────────────────────────────
// A blank effort has two causes that used to look identical: "this session has
// reported nothing yet" and "this session is reporting and effort never
// arrives". The second is a defect and is how a missing field survived for
// months. A session that is online AND has landed some other telemetry-only
// value is provably reporting, so its blank effort gets a marker; a session with
// no evidence of reporting keeps the plain silent blank.
describe("MonitorPage AI Sessions — missing-effort guard", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    listOutsourceWorkers.mockResolvedValue([]);
    window.location.hash = "";
  });

  it("marks a member row that is reporting telemetry but sent no effort", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ effort: "", status: "online", contextPct: 42 })],
    });
    renderMonitor();

    const row = (await screen.findByText("Eva")).closest("tr")!;
    expect(within(row).getByTestId("mon-effort-missing")).toBeTruthy();
  });

  it("leaves a member row that is reporting nothing at all unmarked", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({
          effort: "",
          status: "offline",
          contextPct: null,
          cost: null,
          bankedCost: null,
          account: "",
        }),
      ],
    });
    renderMonitor();

    const row = (await screen.findByText("Eva")).closest("tr")!;
    expect(within(row).queryByTestId("mon-effort-missing")).toBeNull();
  });

  it("does not mark a row whose effort actually arrived", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ effort: "high", status: "online", contextPct: 42 })],
    });
    renderMonitor();

    const row = (await screen.findByText("Eva")).closest("tr")!;
    expect(within(row).getByText("high")).toBeTruthy();
    expect(within(row).queryByTestId("mon-effort-missing")).toBeNull();
  });

  it("marks an outsource row that is reporting telemetry but sent no effort", async () => {
    listOutsourceWorkers.mockResolvedValue([worker({ effort: "high" })]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "ow-1", effort: "", status: "online", contextPct: 71 }),
      ],
    });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(within(row).getByTestId("mon-effort-missing")).toBeTruthy();
  });

  it("leaves an outsource row with no session row at all unmarked", async () => {
    listOutsourceWorkers.mockResolvedValue([worker({ effort: "high" })]);
    getMonitoring.mockResolvedValue({ accounts: [], machines: [], sessions: [] });
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(within(row).queryByTestId("mon-effort-missing")).toBeNull();
  });
});
