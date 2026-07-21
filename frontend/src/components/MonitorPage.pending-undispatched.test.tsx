// Monitor entry · the *_pending verdict survives ITS call site (T-7fa1).
//
// The Monitor page opens the SAME shared MemberDetailPanel the office does, but
// through its OWN handlers (they refetch via this page's roster hook, not
// OfficePage's). So "OfficePage returns the result" proves nothing about this
// entry — dropping the `return` here alone would leave the Monitor entry with
// the original permanent 「喚醒中…」 while every office test stayed green.
//
// This file also carries the ONLY relocate coverage of the family: 改機器 had
// the identical break (`relocation_pending` produced since T-8655, never read),
// and this is the surface whose relocate wiring already has a home here.
//
// Locked here:
//   1. 喚醒 → activateMember resolves {activationPending: true} → the notice
//      appears (i.e. MonitorPage returned the verdict to the panel).
//   2. NEGATIVE: {activationPending: false} → no notice.
//   3. 改機器 → relocateMember resolves {relocationPending: true} → the relocate
//      notice appears.
//   4. NEGATIVE: {relocationPending: false} → no notice.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, MonSessionView } from "../types";

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

// OFFLINE on purpose: the wake button only exists for a member that is not
// already up, and the panel's wakePending bridge only holds while the lifecycle
// stays offline/stopped — the exact window the bug lived in.
const mkMember = (over: Partial<Member> = {}): Member => ({
  id: "mem-eva",
  memberId: "MB-AST001",
  name: "Eva",
  role: "assistant",
  status: "offline",
  lifecycle: "offline",
  model: "opus-4.8",
  effort: "medium",
  kind: "assistant",
  desiredMachineId: "mach-a",
  // 🔴 machine (WHERE IT IS) must differ from desiredMachineId (WHERE IT WAS
  // PINNED) for a relocate to be pending at all — equal means the move already
  // landed, which is precisely the self-heal signal added for review r1
  // SHOULD-2. A fixture where they matched would suppress the notice and make
  // the positive relocate test assert nothing.
  machine: "mach-b",
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  tmuxSession: "member-eva",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
  ...over,
});

const session = (over: Partial<MonSessionView> = {}): MonSessionView => ({
  id: "mem-eva",
  name: "Eva",
  role: "assistant",
  model: "opus-4.8",
  effort: "",
  // Must agree with the member fixture's `machine`: MonitorPage runs the roster
  // member through joinSessionRuntime (lib/runtime.ts), and a non-empty session
  // `machine` OVERRIDES the member's own. A session still reporting the PINNED
  // machine here would mean "the move already landed" and (correctly) suppress
  // the not-landed notice — an inconsistent fixture, not a bug.
  machine: "mach-b",
  account: "",
  status: "offline",
  contextPct: 42,
  cost: null,
  bankedCost: null,
  ...over,
});

const activateMember = vi.fn(async (_id: string, _machineId?: string) => ({
  activationPending: false,
}));
const relocateMember = vi.fn(async (_id: string, _machineId: string) => ({
  relocationPending: false,
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([mkMember()]),
    listMachines: () => Promise.resolve([machine("mach-a", "Machine A")]),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [session()], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    listWebhooks: () => Promise.resolve([]),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    activateMember: (id: string, machineId?: string) =>
      activateMember(id, machineId),
    relocateMember: (id: string, machineId: string) =>
      relocateMember(id, machineId),
    deactivateMember: vi.fn(),
    forceStopMember: vi.fn(),
    refocusMember: vi.fn(),
    patchMember: vi.fn(),
    subscribeEvents: () => () => {},
  },
}));

beforeEach(() => {
  window.location.hash = "";
  activateMember.mockClear();
  activateMember.mockResolvedValue({ activationPending: false });
  relocateMember.mockClear();
  relocateMember.mockResolvedValue({ relocationPending: false });
  Element.prototype.scrollIntoView = vi.fn();
});

/** Open the member detail from a Monitor AI-session row. */
async function openDetail() {
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>,
  );
  fireEvent.click(await screen.findByText("Eva"));
}

async function clickWake() {
  const btn = await waitFor(() => {
    const b = document.querySelector(
      '[data-testid="member-action-spawn"]',
    ) as HTMLButtonElement | null;
    expect(b, "the wake button must exist").not.toBeNull();
    expect(b!.disabled, "the wake button must be enabled").toBe(false);
    return b!;
  });
  fireEvent.click(btn);
}

describe("Monitor entry · undispatched activate (T-7fa1)", () => {
  it("shows the notice when activation_pending comes back true", async () => {
    activateMember.mockResolvedValue({ activationPending: true });
    await openDetail();
    await clickWake();
    await waitFor(() =>
      expect(screen.queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );
  });

  it("shows NO notice when the wake was actually dispatched", async () => {
    await openDetail();
    await clickWake();
    await waitFor(() => expect(activateMember).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("mp-wake-undispatched")).toBeNull();
  });
});

describe("Monitor entry · undispatched relocate (T-7fa1)", () => {
  it("shows the relocate notice when relocation_pending comes back true", async () => {
    relocateMember.mockResolvedValue({ relocationPending: true });
    await openDetail();
    await waitFor(() =>
      expect(
        (screen.getByTestId("mp-relocate") as HTMLButtonElement).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByTestId("mp-relocate"));
    await waitFor(() =>
      expect(screen.queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );
  });

  it("shows NO relocate notice when the move landed", async () => {
    await openDetail();
    await waitFor(() =>
      expect(
        (screen.getByTestId("mp-relocate") as HTMLButtonElement).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByTestId("mp-relocate"));
    await waitFor(() => expect(relocateMember).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("mp-relocate-undispatched")).toBeNull();
  });
});
