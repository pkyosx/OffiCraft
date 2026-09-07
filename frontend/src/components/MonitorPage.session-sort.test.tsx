// §3 AI 會話 — column sorting over the MERGED member + outsource list (T-135).
//
// Two things this file exists to hold shut:
//
//  1. THE MERGE MUST NOT LOSE A ROW. The table now builds one array out of two
//     lanes, and a merge bug (a filter that eats the other lane, a key
//     collision) removes rows silently — the table simply renders fewer. So the
//     row count is asserted against 正職列數 + 外包列數 in BOTH the default
//     state and a sorted one. Pinning only the sorted state would be useless
//     here: the default screen is deliberately unchanged, which is exactly the
//     state a merge bug can hide in.
//
//  2. THE DEFAULT SCREEN IS NOT THE MERGED ORDER. Owner ruling: until a column
//     is clicked the table reads 正職在前、外包在後 with each lane in the order
//     it already had — including the worker lane's own 依任務建立時間新→舊
//     (useOutsourceWorkers.sortWorkers). Interleaving is what a CLICK buys.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
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
const listOutsourceWorkers = vi.fn(
  async (): Promise<OutsourceWorkerView[]> => []
);

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
  taskCreatedTs: 1000,
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

/** Every row of the §3 table, in the order it is painted, regardless of lane. */
function sessionRows(): HTMLElement[] {
  return Array.from(
    document.querySelectorAll<HTMLElement>("tr[data-session-row]")
  );
}

/** The text of one column of the §3 table, top row to bottom row. */
function column(index: number): string[] {
  return sessionRows().map(
    (r) => r.querySelectorAll("td")[index].textContent?.trim() ?? ""
  );
}

const memberNames = () =>
  sessionRows().map(
    (r) => r.querySelector(".mon-member__name")?.textContent?.trim() ?? ""
  );

describe("MonitorPage AI Sessions — column sort", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    listOutsourceWorkers.mockResolvedValue([]);
    window.location.hash = "";
  });

  // ── the row-count guard ──────────────────────────────────────────────────
  // 正職列數 = sessions minus the ow- prefixed ones minus warden/outsource
  // roster kinds; 外包列數 = the whole worker list. Their sum is the merged
  // list, in every sort state.

  it("renders one row per member session plus one per outsource worker", async () => {
    listMembers.mockResolvedValue([
      { id: "mem-eva", name: "Eva", kind: "staff" } as Member,
      { id: "mem-kai", name: "Kai", kind: "staff" } as Member,
      // negative controls: neither of these is an AI session row
      { id: "warden-1", name: "Warden", kind: "warden" } as Member,
      { id: "ow-2", name: "O-9", kind: "outsource" } as Member,
    ]);
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva" }),
        session({ id: "mem-kai", name: "Kai" }),
        session({ id: "warden-1", name: "Warden" }),
        session({ id: "ow-1" }),
        session({ id: "ow-2" }),
      ],
    });
    listOutsourceWorkers.mockResolvedValue([
      worker({ id: "ow-1", codename: "O-7", taskCreatedTs: 2000 }),
      worker({ id: "ow-2", codename: "O-9", taskCreatedTs: 1000 }),
    ]);
    renderMonitor();

    await screen.findByText("Eva");
    // 2 member rows (warden and the ow- session are both excluded) + 2 workers
    expect(screen.getAllByTestId("mon-outsource-row")).toHaveLength(2);
    expect(sessionRows()).toHaveLength(4);

    // …and the merge must still hold every one of them once a column reorders
    // the two lanes into each other.
    fireEvent.click(screen.getByTestId("mon-sort-member"));
    expect(sessionRows()).toHaveLength(4);
    expect(screen.getAllByTestId("mon-outsource-row")).toHaveLength(2);
    fireEvent.click(screen.getByTestId("mon-sort-member"));
    expect(sessionRows()).toHaveLength(4);
  });

  // ── B3: the untouched default screen ─────────────────────────────────────

  it("lists members before outsource workers until a column is picked", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      // Zoe BEFORE Ada: the member lane keeps the order the feed gave it. A
      // default that is secretly "sorted by 成員" would swap these two — and
      // could not be caught by the member-vs-worker split alone, because the
      // 外包 · prefix is CJK and sorts after Latin names anyway.
      sessions: [
        session({ id: "mem-zoe", name: "Zoe" }),
        session({ id: "mem-ada", name: "Ada" }),
      ],
    });
    listOutsourceWorkers.mockResolvedValue([
      worker({ id: "ow-1", codename: "O-7", taskCreatedTs: 2000 }),
      worker({ id: "ow-2", codename: "O-9", taskCreatedTs: 1000 }),
    ]);
    renderMonitor();

    await screen.findByText("Zoe");
    expect(memberNames()).toEqual([
      "Zoe",
      "Ada",
      "外包 · O-7",
      "外包 · O-9",
    ]);
  });

  it("keeps the worker lane's own 任務建立時間新→舊 order by default", async () => {
    // O-9 has the NEWER bound task, so useOutsourceWorkers puts it first. The
    // merge must not quietly re-order the lane back into fetch order.
    listOutsourceWorkers.mockResolvedValue([
      worker({ id: "ow-1", codename: "O-7", taskCreatedTs: 1000 }),
      worker({ id: "ow-2", codename: "O-9", taskCreatedTs: 5000 }),
    ]);
    renderMonitor();

    await screen.findAllByTestId("mon-outsource-row");
    expect(memberNames()).toEqual(["外包 · O-9", "外包 · O-7"]);
  });

  // ── A2: two states, and the merge a click produces ───────────────────────

  it("interleaves the two lanes once a column is picked, and only then", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", machine: "beta" }),
        session({ id: "ow-1", machine: "alpha" }),
      ],
    });
    listOutsourceWorkers.mockResolvedValue([worker({ id: "ow-1" })]);
    renderMonitor();

    await screen.findByText("Eva");
    expect(column(1)).toEqual(["beta", "alpha"]);

    fireEvent.click(screen.getByTestId("mon-sort-machine"));
    expect(column(1)).toEqual(["alpha", "beta"]);
  });

  it("toggles between 升 and 降 forever, never back to the default order", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", machine: "beta" }),
        session({ id: "ow-1", machine: "alpha" }),
      ],
    });
    listOutsourceWorkers.mockResolvedValue([worker({ id: "ow-1" })]);
    renderMonitor();

    await screen.findByText("Eva");
    const header = screen.getByTestId("mon-sort-machine");

    fireEvent.click(header);
    expect(column(1)).toEqual(["alpha", "beta"]);
    fireEvent.click(header);
    expect(column(1)).toEqual(["beta", "alpha"]);
    // a third click is 升 again — NOT the 正職在前 default (which would also
    // read ["beta", "alpha"] here, hence the machine-column check below)
    fireEvent.click(header);
    expect(column(1)).toEqual(["alpha", "beta"]);
  });

  it("switching to another column starts that one ascending", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", machine: "beta", cost: 9, bankedCost: null }),
        session({ id: "ow-1", machine: "alpha", cost: 1, bankedCost: null }),
      ],
    });
    listOutsourceWorkers.mockResolvedValue([worker({ id: "ow-1" })]);
    renderMonitor();

    await screen.findByText("Eva");
    const machine = screen.getByTestId("mon-sort-machine");
    fireEvent.click(machine);
    fireEvent.click(machine); // now 降冪 on 機器
    expect(column(1)).toEqual(["beta", "alpha"]);

    fireEvent.click(screen.getByTestId("mon-sort-estCost"));
    expect(column(5)).toEqual(["$1", "$9"]);
  });

  // ── the numeric columns order by magnitude, not by printed text ──────────

  it("orders context by percentage, not by the string the cell prints", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", contextPct: 42 }),
        session({ id: "mem-kai", name: "Kai", contextPct: 100 }),
        session({ id: "mem-lin", name: "Lin", contextPct: 7 }),
      ],
    });
    renderMonitor();

    await screen.findByText("Eva");
    fireEvent.click(screen.getByTestId("mon-sort-context"));
    // lexicographically "100%" < "42%" < "7%"; by magnitude it is the reverse
    expect(column(4)).toEqual(["7%", "42%", "100%"]);
  });

  // ── the dash rule ────────────────────────────────────────────────────────

  it("sinks rows whose cell shows a dash to the bottom in BOTH directions", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", machine: "beta" }),
        session({ id: "mem-kai", name: "Kai", machine: "" }),
        session({ id: "mem-lin", name: "Lin", machine: "alpha" }),
      ],
    });
    renderMonitor();

    await screen.findByText("Eva");
    const header = screen.getByTestId("mon-sort-machine");

    fireEvent.click(header);
    expect(column(1)).toEqual(["alpha", "beta", "—"]);
    fireEvent.click(header);
    expect(column(1)).toEqual(["beta", "alpha", "—"]);
  });

  it("sinks an outsource row with no session at all to the bottom too", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ id: "mem-eva", name: "Eva", machine: "beta" })],
    });
    // no ow-1 row in `sessions` ⇒ every telemetry column is a dash
    listOutsourceWorkers.mockResolvedValue([worker({ id: "ow-1" })]);
    renderMonitor();

    await screen.findByText("Eva");
    const header = screen.getByTestId("mon-sort-machine");

    fireEvent.click(header);
    expect(column(1)).toEqual(["beta", "—"]);
    fireEvent.click(header);
    expect(column(1)).toEqual(["beta", "—"]);
  });

  // ── accessibility ────────────────────────────────────────────────────────

  it("marks the sorted column with aria-sort and leaves the others none", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session()],
    });
    renderMonitor();

    await screen.findByText("Eva");
    const th = (col: string) =>
      screen.getByTestId(`mon-sort-${col}`).closest("th")!;

    expect(th("machine").getAttribute("aria-sort")).toBe("none");

    fireEvent.click(screen.getByTestId("mon-sort-machine"));
    expect(th("machine").getAttribute("aria-sort")).toBe("ascending");
    expect(th("model").getAttribute("aria-sort")).toBe("none");

    fireEvent.click(screen.getByTestId("mon-sort-machine"));
    expect(th("machine").getAttribute("aria-sort")).toBe("descending");
  });

  it("sorts from the keyboard — the header is a focusable button", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ id: "mem-eva", name: "Eva", machine: "beta" }),
        session({ id: "mem-kai", name: "Kai", machine: "alpha" }),
      ],
    });
    renderMonitor();

    await screen.findByText("Eva");
    const header = screen.getByTestId("mon-sort-machine");
    // A real <button> is Tab-reachable and fires on Enter/Space without a
    // hand-rolled key handler; asserting the tag is asserting both.
    expect(header.tagName).toBe("BUTTON");
    header.focus();
    expect(document.activeElement).toBe(header);

    fireEvent.click(header);
    expect(column(1)).toEqual(["alpha", "beta"]);
  });
});
