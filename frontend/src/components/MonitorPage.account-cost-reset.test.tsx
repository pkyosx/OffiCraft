// 帳號歸零 (T-53, owner ruling rc-5c5d7c7c6dcd「分開：帳號卡自己一份數字，清它不動
// 成員」) — the account card's own reset button.
//
// The two things worth pinning from the cockpit side, because both fail
// silently:
//   · the press is gated behind a confirm that NAMES the figure and says no
//     member is touched — the sentence the owner reads before an irreversible
//     action, and the only place the separation is stated to him;
//   · a FAILED reset keeps the dialog open and says so. Success and silent
//     failure look identical on the card afterwards (the figure renders the
//     dash either way), so without this the owner cannot tell them apart.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
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
const resetAccountCost = vi.fn(async (_account: string) => ({
  account: "acct-123/9f8e-uuid",
  clearedCost: 1234.5,
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    resetAccountCost: (account: string) => resetAccountCost(account),
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

const acct = (over: Partial<MonAccountView> = {}): MonAccountView => ({
  account: "acct-123/9f8e-uuid",
  accountLabel: "eva@example.test(Example Org)",
  displayName: "Eva 的帳號",
  machine: "mbp5",
  cost: 1234.5,
  fiveHour: null,
  sevenDay: null,
  ...over,
});

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  getMonitoring.mockResolvedValue({
    accounts: [acct()],
    sessions: [],
    machines: [],
  });
  resetAccountCost.mockResolvedValue({
    account: "acct-123/9f8e-uuid",
    clearedCost: 1234.5,
  });
});

describe("account cost reset", () => {
  it("asks before it fires, and the question names the figure and says no member is touched", async () => {
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-acct-cost-reset"));

    // Nothing has been destroyed yet — the click opens a question, never the
    // action, because the action cannot be undone.
    expect(resetAccountCost).not.toHaveBeenCalled();
    const confirm = await screen.findByTestId("mon-acct-cost-reset-confirm");
    // The amount is READ OFF THE CARD, not written into the string, so the
    // sentence cannot name a figure other than the one about to be destroyed.
    expect(within(confirm).getByText(/\$1,235/)).toBeTruthy();
    expect(within(confirm).getByText(/底下成員各自的數字不會被動到/)).toBeTruthy();

    fireEvent.click(within(confirm).getByTestId("mon-acct-cost-reset-confirm-btn"));
    expect(resetAccountCost).toHaveBeenCalledWith("acct-123/9f8e-uuid");
  });

  it("keeps the dialog open and reports the error when the reset fails", async () => {
    resetAccountCost.mockRejectedValueOnce(new Error("boom"));
    renderMonitor();
    fireEvent.click(await screen.findByTestId("mon-acct-cost-reset"));
    const confirm = await screen.findByTestId("mon-acct-cost-reset-confirm");
    fireEvent.click(within(confirm).getByTestId("mon-acct-cost-reset-confirm-btn"));

    // Still open, and saying why. A dialog that closed on failure would leave
    // the owner believing a figure was cleared that is still there.
    expect(await screen.findByText(/歸零失敗/)).toBeTruthy();
    expect(screen.getByTestId("mon-acct-cost-reset-confirm")).toBeTruthy();
  });

  it("offers nothing to press when the card has nothing measured", async () => {
    getMonitoring.mockResolvedValue({
      accounts: [acct({ cost: null })],
      sessions: [],
      machines: [],
    });
    renderMonitor();
    // Same condition the card renders the dash for, so the button and the
    // figure can never disagree about whether there is anything to clear.
    expect(await screen.findByTestId("mon-acct-cost-reset")).toHaveProperty(
      "disabled",
      true
    );
  });
});
