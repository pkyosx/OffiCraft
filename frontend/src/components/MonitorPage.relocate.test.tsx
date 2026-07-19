// Monitor entry · MemberDetailPanel 改機器 (relocate) wiring.
//
// The Monitor page's AI-session rows open the SAME shared MemberDetailPanel the
// office uses. The owner constitution says the panel must behave IDENTICALLY no
// matter which entry opened it — so the Monitor entry must offer 改機器 too (it
// was the only member-detail surface still missing onRelocate). This test locks:
//   1. Opening a session's detail from the Monitor entry shows the 改機器 button
//      (data-testid mp-relocate) — proving onRelocate is wired at this entry.
//   2. Clicking it (with a single online machine → direct relocate, no picker)
//      routes through relocateMember(id, machineId) — a placement, never a wake
//      (never activateMember).
//
// Red/green guard: with onRelocate absent at the Monitor entry, MemberDetailPanel
// renders NO mp-relocate button, so assertion (1) fails — this test is red before
// the fix and green after (not a dead assertion).

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

const mkMember = (over: Partial<Member> = {}): Member => ({
  id: "mem-eva",
  memberId: "MB-AST001",
  name: "Eva",
  role: "assistant",
  status: "online",
  lifecycle: "online",
  model: "opus-4.8",
  effort: "medium",
  kind: "assistant",
  desiredMachineId: "mach-a",
  machine: "mach-a",
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
  machine: "mach-a",
  account: "",
  status: "online",
  contextPct: 42,
  cost: null,
  bankedCost: null,
  ...over,
});

const listMembers = vi.fn(async (): Promise<Member[]> => [mkMember()]);
const listMachines = vi.fn(async (): Promise<MachineView[]> => [
  machine("mach-a", "Machine A"),
]);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [session()] as MonSessionView[],
  machines: [],
}));
const relocateMember = vi.fn(async (_id: string, _machineId: string) => {});

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    listWebhooks: () => Promise.resolve([]),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    relocateMember: (id: string, machineId: string) =>
      relocateMember(id, machineId),
    activateMember: vi.fn(),
    deactivateMember: vi.fn(),
    forceStopMember: vi.fn(),
    refocusMember: vi.fn(),
    patchMember: vi.fn(),
    subscribeEvents: () => () => {},
  },
}));

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

beforeEach(() => {
  window.location.hash = "";
  listMembers.mockResolvedValue([mkMember()]);
  listMachines.mockResolvedValue([machine("mach-a", "Machine A")]);
  getMonitoring.mockResolvedValue({
    accounts: [],
    sessions: [session()],
    machines: [],
  });
  relocateMember.mockClear();
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MonitorPage entry — MemberDetailPanel 改機器", () => {
  it("shows the 改機器 button once a session's detail is opened (onRelocate wired)", async () => {
    renderMonitor();
    // Open the member's detail from the Monitor AI-session row.
    fireEvent.click(await screen.findByText("Eva"));
    await waitFor(() => {
      expect(screen.getByTestId("mp-relocate")).toBeTruthy();
    });
  });

  it("relocates through relocateMember when the 改機器 button is clicked", async () => {
    renderMonitor();
    fireEvent.click(await screen.findByText("Eva"));

    // One online machine → clicking relocates straight to it (no picker).
    await waitFor(() => {
      expect(
        (screen.getByTestId("mp-relocate") as HTMLButtonElement).disabled
      ).toBe(false);
    });
    fireEvent.click(screen.getByTestId("mp-relocate"));

    await waitFor(() => {
      expect(relocateMember).toHaveBeenCalledWith("mem-eva", "mach-a");
    });
  });
});
