// AI Sessions table — the 活動 column (T-a1d7).
//
// `online` says a session holds an SSE stream. It does NOT say the model is
// running a turn, and the owner's actual question is the second one. This
// column answers it, and the whole point is that it stays HONEST about the
// difference between the four things it can know:
//
//   active  — reported working, with a live start anchor
//   unknown — reported working, then went silent past the server's window: the
//             claim is SHOWN and MARKED, never rewritten into a fabricated idle
//   idle    — reported not-working; the "last ended" line only appears when a
//             turn end was actually OBSERVED
//   never   — nothing was ever reported: a dash, not a guess
//
// 🔴 The verdict is the SERVER's (deriveActivity). What is pinned here is that
// the FE never re-derives it: the same anchor with the same clock renders a
// different sentence purely because the server said a different word. A UI that
// compared the timestamp against a threshold of its own would fail these.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, within, act } from "@testing-library/react";
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
const subscribeEvents = vi.fn(() => () => {});

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => Promise.resolve([]),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => listOutsourceWorkers(),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: (...a: unknown[]) => subscribeEvents(...(a as [])),
  },
}));

/** A fixed "now" so every duration in this file is arithmetic, not a race. */
const NOW_MS = 1_800_000_000_000;
const NOW_S = NOW_MS / 1000;

const session = (over: Partial<MonSessionView> = {}): MonSessionView => ({
  id: "mem-eva",
  name: "Eva",
  role: "assistant",
  model: "opus-4.8",
  effort: "",
  machine: "mbp5",
  account: "",
  runtime: "claude",
  status: "online",
  contextPct: null,
  compactionCount: null,
  cost: null,
  bankedCost: null,
  activityState: "never",
  workingSince: null,
  lastTurnCompletedAt: null,
  ...over,
});

const worker = (over: Partial<OutsourceWorkerView> = {}): OutsourceWorkerView => ({
  id: "ow-1",
  codename: "O-7",
  runtime: "claude",
  model: "opus-4.8",
  effort: "",
  status: "active",
  taskId: "task-1",
  taskTitle: "Migrate the billing importer",
  machine: "mbp5",
  account: null,
  contextPct: null,
  cost: null,
  bankedCost: null,
  activityState: "never",
  workingSince: null,
  lastTurnCompletedAt: null,
  ...over,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage AI Sessions — 活動 column", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(NOW_MS);
    listMembers.mockResolvedValue([]);
    getMonitoring.mockResolvedValue({ accounts: [], sessions: [], machines: [] });
    listOutsourceWorkers.mockResolvedValue([]);
    subscribeEvents.mockClear();
    window.location.hash = "";
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the elapsed turn time for an active session", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ activityState: "active", workingSince: NOW_S - 7 * 60 }),
      ],
    });
    renderMonitor();

    expect(await screen.findByTestId("mon-activity-working")).toHaveProperty(
      "textContent",
      "工作中 7m"
    );
    // An ACTIVE session must not be marked — the mark belongs to `unknown`.
    expect(screen.queryByTestId("mon-activity-unknown")).toBeNull();
  });

  it("marks an unknown session instead of rewriting it into idle", async () => {
    // The SAME anchor as the active case, one hour old. The FE has no
    // threshold of its own: only the server's word changes the rendering.
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({ activityState: "unknown", workingSince: NOW_S - 47 * 60 }),
      ],
    });
    renderMonitor();

    // The claim is still SHOWN (the owner needs to see "claims 47 minutes")…
    expect(await screen.findByTestId("mon-activity-working")).toHaveProperty(
      "textContent",
      "工作中 47m"
    );
    // …and MARKED, so it does not read as a live fact.
    expect(screen.getByTestId("mon-activity-unknown").textContent).toBe(
      "未收到結束"
    );
    // Crucially it did NOT become "idle" — that would be a fabricated
    // observation we never made.
    expect(screen.queryByTestId("mon-activity-idle")).toBeNull();
  });

  // 🔴 THE constraint (frontend/CLAUDE.md: the threshold has one home). These
  // two rows are built so a client that re-derived the verdict from the anchor
  // would get the OPPOSITE answer from the server on both of them. A UI that
  // compares `now - workingSince` against any threshold of its own fails here.
  it("takes the verdict from the server even when the anchor disagrees", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        // Two hours in and the server still says active — a self-deriving UI
        // (any plausible threshold is under two hours) would mark this.
        session({
          id: "long-but-active",
          activityState: "active",
          workingSince: NOW_S - 120 * 60,
        }),
        // Five minutes in and the server says unknown — a self-deriving UI
        // would NOT mark this one.
        session({
          id: "young-but-unknown",
          activityState: "unknown",
          workingSince: NOW_S - 5 * 60,
        }),
      ],
    });
    renderMonitor();

    await screen.findAllByTestId("mon-activity-working");
    const marks = screen.getAllByTestId("mon-activity-unknown");
    expect(marks).toHaveLength(1);
    // The mark belongs to the row the SERVER called unknown — the young one.
    expect(marks[0].closest("tr")!.textContent).toContain("工作中 5m");
    expect(marks[0].closest("tr")!.textContent).not.toContain("2h");
  });

  it("shows how long ago an OBSERVED turn ended", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [
        session({
          activityState: "idle",
          lastTurnCompletedAt: NOW_S - 3 * 60,
        }),
      ],
    });
    renderMonitor();

    expect(await screen.findByTestId("mon-activity-idle")).toHaveProperty(
      "textContent",
      "上次結束 3m 前"
    );
  });

  it("says only 閒置 when no turn end was ever observed — never '0 分鐘前'", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ activityState: "idle", lastTurnCompletedAt: null })],
    });
    renderMonitor();

    const cell = await screen.findByTestId("mon-activity-idle");
    expect(cell.textContent).toBe("閒置");
    expect(cell.textContent).not.toMatch(/前/);
  });

  it("shows a dash — not a duration — for a session that never reported", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session()],
    });
    renderMonitor();

    expect(await screen.findByTestId("mon-activity-never")).toHaveProperty(
      "textContent",
      "—"
    );
    expect(screen.queryByTestId("mon-activity-working")).toBeNull();
    expect(screen.queryByTestId("mon-activity-idle")).toBeNull();
  });

  it("renders the SAME column for an outsource worker row", async () => {
    listOutsourceWorkers.mockResolvedValue([
      worker({ activityState: "active", workingSince: NOW_S - 12 * 60 }),
    ]);
    renderMonitor();

    const row = await screen.findByTestId("mon-outsource-row");
    expect(
      within(row).getByTestId("mon-activity-working").textContent
    ).toBe("工作中 12m");
  });

  it("ticks the duration forward without any network call", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [],
      machines: [],
      sessions: [session({ activityState: "active", workingSince: NOW_S - 60 })],
    });
    renderMonitor();
    expect(await screen.findByTestId("mon-activity-working")).toHaveProperty(
      "textContent",
      "工作中 1m"
    );

    const fetchesBefore = getMonitoring.mock.calls.length;
    // Six ticks of the 30s page clock. advanceTimersByTimeAsync moves the
    // mocked wall clock too, so the anchor ages by exactly this much.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3 * 60_000);
    });

    expect(screen.getByTestId("mon-activity-working").textContent).toBe(
      "工作中 4m"
    );
    // The tick is pure display: it must never poll the server (and it cannot
    // call a model — there is no such path from here at all).
    expect(getMonitoring.mock.calls.length).toBe(fetchesBefore);
  });
});
