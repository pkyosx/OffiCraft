// T-3b90 — the account card must state how old its usage number is, and must
// not paint a present-tense alarm on a number nobody is refreshing.
//
// The owner opened this page with no codex agent running anywhere and read
// "7 天窗 · 用量 43% · 時間 15.15% · 過熱" in red. Nothing was miscalculated:
// used% is a snapshot frozen at the last report, time% is recomputed from the
// wall clock on every load, and the badge compares the two. So the badge lit
// itself, and would have gone out two days later, with no new data at all.
//
// Both halves are asserted on what the OWNER SEES, not on view-model fields:
//   1. the age is printed next to the usage number — at every moment, so the
//      answer to "is this old?" never depends on catching the page in time;
//   2. the 過熱 badge is absent when the BE withheld its verdict, and still
//      present when the BE stands behind it.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonAccountView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [] as MonAccountView[],
  sessions: [],
  machines: [],
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
    // Rendering MonitorPage now also mounts BackupHealthSection (arrived with
    // PR #79 while this branch was in flight). It is nothing to do with the
    // account card, but an unstubbed api method throws during commit and takes
    // the whole page down — so stub it to a healthy, uninteresting value.
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

const nowSecs = () => Date.now() / 1000;

/** The owner's account, reported once and then left alone for `ageSecs`.
 * `overheated` is what the BE decided — the card is never allowed to re-derive
 * it from the two percentages. */
const staleAcct = (ageSecs: number, overheated: boolean): MonAccountView => ({
  account: "codex:seth",
  accountLabel: null,
  displayName: "seth-m5-codex",
  machine: "seth-m5",
  cost: null,
  limitReached: null,
  fiveHour: null,
  sevenDay: {
    usagePct: 43,
    timePct: 15.15,
    measuredAt: nowSecs() - ageSecs,
    overheated,
  },
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage account usage age (T-3b90)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listMembers.mockResolvedValue([]);
    listMachines.mockResolvedValue([]);
  });

  it("prints how old the usage number is, next to the number itself", async () => {
    getMonitoring.mockResolvedValue({
      // Three days since anybody reported — the owner's situation.
      accounts: [staleAcct(3 * 24 * 3600, false)],
      sessions: [],
      machines: [],
    });
    renderMonitor();

    const ages = await screen.findAllByTestId("mon-usage-age");
    // "量於 3d 前" — the duration must be the real one. An implementation that
    // printed the SERVING time would answer "is this old?" with "no" forever,
    // so assert the number, not merely that some age label rendered.
    expect(ages.map((n) => n.textContent).join("|")).toMatch(/3d/);

    // And the number itself is still there: withholding it would take away the
    // one thing the card is for, which is a different silence, not a fix.
    expect(screen.getByText(/43%/)).toBeTruthy();
  });

  it("does not paint 過熱 on a snapshot the server declined to judge", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [staleAcct(3 * 24 * 3600, false)],
      sessions: [],
      machines: [],
    });
    renderMonitor();

    await screen.findAllByTestId("mon-usage-age");
    // ⚠️ NOT COVERAGE OF THIS CHANGE — read before trusting it. `overheated`
    // is handed in as a prop here, so this passes with the fix fully removed;
    // it only tests that the card obeys the flag it is given. Its red under
    // the ageText mutant comes entirely from the findAllByTestId precondition
    // above, not from this line. The BE half of "no verdict → no badge" is
    // pinned where it actually happens: paceVerdict (Go) and the wire→view
    // seam (api/mappers.mon-account.test.ts). Kept as a regression guard
    // against the card ever re-deriving heat from the two percentages itself.
    //
    // Matched loosely because the badge renders as "· 過熱" — an exact-string
    // query returns null either way. That was found by the positive control
    // below, which is the only reason this line is not doubly vacuous.
    expect(screen.queryByText(/過熱/)).toBeNull();
  });

  it("still paints 過熱 when the server stands behind the verdict", async () => {
    // Positive control. Without it, deleting the badge outright would pass —
    // and the fix is "stop describing a clock", not "stop warning".
    getMonitoring.mockResolvedValue({
      accounts: [staleAcct(30, true)],
      sessions: [],
      machines: [],
    });
    renderMonitor();

    expect(await screen.findByText(/過熱/)).toBeTruthy();
    // A fresh window states its age too — the reader never has to assume.
    expect(screen.getAllByTestId("mon-usage-age").length).toBeGreaterThan(0);
  });

  it("says nothing about age when nothing stamped the snapshot", async () => {
    // Honest-null: an unstamped number must NOT be dressed up as "measured 1m
    // ago". Silence here is the truthful output.
    //
    // ⚠️ Also passes with the fix removed (no stamp → no label, trivially), so
    // this is a FORWARD guard against someone later back-filling `measuredAt`
    // with the serving time — not evidence that this change works.
    getMonitoring.mockResolvedValue({
      accounts: [
        {
          ...staleAcct(0, false),
          sevenDay: {
            usagePct: 43,
            timePct: 15.15,
            measuredAt: null,
            overheated: false,
          },
        },
      ],
      sessions: [],
      machines: [],
    });
    renderMonitor();

    expect(await screen.findByText(/43%/)).toBeTruthy();
    expect(screen.queryByTestId("mon-usage-age")).toBeNull();
  });
});
