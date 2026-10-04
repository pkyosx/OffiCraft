import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonAccountView } from "../types";

const getMonitoring = vi.fn(async () => ({
  accounts: [] as MonAccountView[],
  sessions: [],
  machines: [],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([] as Member[]),
    listMachines: () => Promise.resolve([] as MachineView[]),
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

/** Local-time epoch seconds for 2026-10-02 (plus `days`) at h:mm, so HH:mm reads
 * the same in any timezone. */
const localTs = (h: number, mi: number, days = 0) =>
  new Date(2026, 9, 2 + days, h, mi, 0, 0).getTime() / 1000;

const acct = (account: string, limitReached: MonAccountView["limitReached"]): MonAccountView => ({
  account,
  accountLabel: null,
  displayName: account,
  machine: "seth-m5",
  cost: null,
  fiveHour: null,
  sevenDay: null,
  limitReached,
});

describe("MonitorPage account card limit line", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(2026, 9, 2, 10, 0, 0, 0));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("under a limit in force, that account's card says so with the reset time, and an account without one says nothing", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [
        acct("limited", { code: "rate_limit", resetsAt: localTs(14, 5), ts: localTs(9, 0) }),
        acct("fine", null),
      ],
      sessions: [],
      machines: [],
    });
    const { container } = render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>,
    );
    const line = await screen.findByTestId("mon-acct-limit-reached");
    expect(line.outerHTML).toBe(
      '<div class="mon-acct__limit" data-testid="mon-acct-limit-reached">已達上限 · 今天 14:05 重置</div>',
    );
    const cards = Array.from(container.querySelectorAll(".mon-acct"));
    expect(cards).toHaveLength(2);
    expect(cards.map((c) => c.querySelectorAll('[data-testid="mon-acct-limit-reached"]').length)).toEqual([1, 0]);
  });

  it("under a reset after midnight, the line says tomorrow", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [acct("limited", { code: "rate_limit", resetsAt: localTs(4, 30, 1), ts: localTs(9, 0) })],
      sessions: [],
      machines: [],
    });
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>,
    );
    expect((await screen.findByTestId("mon-acct-limit-reached")).textContent).toBe(
      "已達上限 · 明天 04:30 重置",
    );
  });

  it("under nothing else re-rendering, the reset day rolls over at local midnight", async () => {
    vi.useRealTimers();
    vi.useFakeTimers({ toFake: ["Date", "setTimeout", "clearTimeout"], shouldAdvanceTime: true });
    vi.setSystemTime(new Date(2026, 9, 2, 23, 50, 0, 0));
    getMonitoring.mockResolvedValue({
      accounts: [acct("limited", { code: "rate_limit", resetsAt: localTs(4, 30, 1), ts: localTs(9, 0) })],
      sessions: [],
      machines: [],
    });
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>,
    );
    const line = await screen.findByTestId("mon-acct-limit-reached");
    expect(line.textContent).toBe("已達上限 · 明天 04:30 重置");
    const fetches = getMonitoring.mock.calls.length;
    // findByTestId resolves on the DOM commit, which can land before React runs
    // the card's effects; jumping the clock first would arm the midnight timer
    // after midnight, and the line would never roll over.
    await act(async () => {});

    act(() => {
      vi.advanceTimersByTime(11 * 60 * 1000);
    });
    expect(getMonitoring.mock.calls.length).toBe(fetches);
    expect(screen.getByTestId("mon-acct-limit-reached").textContent).toBe("已達上限 · 今天 04:30 重置");
  });

  it("under a limit with no reset time, the line names the limit alone", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [acct("limited", { code: "rate_limit", resetsAt: null, ts: localTs(9, 0) })],
      sessions: [],
      machines: [],
    });
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>,
    );
    expect((await screen.findByTestId("mon-acct-limit-reached")).textContent).toBe("已達上限");
  });
});
