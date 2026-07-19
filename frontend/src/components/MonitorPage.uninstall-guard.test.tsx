// Uninstall members guard — Monitor §2 machine panel.
//
// Clicking "uninstall" on a machine that still has members ACTUALLY ONLINE on
// it (status online + observed machine === machineId, warden excluded — the
// same criterion as the server's 409 gate) must warn first and advise taking
// them offline; a machine whose agents are all offline keeps the direct
// confirm, even when offline members are still BOUND to it via
// desiredMachineId. Proceed runs the same uninstall. A warden member still
// carrying the one-shot desired_state="uninstall" intent renders the row's
// button as the disabled "uninstalling…" transitional state.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView } from "../types";

const uninstallMachine = vi.fn((_id: string) =>
  Promise.resolve({ memberId: "w-1", host: "m-1", dispatched: true })
);
const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
    uninstallMachine: (id: string) => uninstallMachine(id),
  },
}));

function member(over: Partial<Member> & Pick<Member, "id" | "kind" | "desiredMachineId">): Member {
  return {
    memberId: over.id,
    name: over.id,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

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

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

describe("MonitorPage uninstall members guard", () => {
  beforeEach(() => {
    uninstallMachine.mockClear();
    // m-1 has a member ACTUALLY ONLINE on it (occupied); m-2 carries only an
    // OFFLINE bound member (+ its warden) — all-offline = not occupied.
    listMachines.mockResolvedValue([machine("m-1", "Alpha"), machine("m-2", "Beta")]);
    listMembers.mockResolvedValue([
      member({ id: "w-1", kind: "warden", desiredMachineId: "m-1", name: "warden-1" }),
      member({ id: "w-2", kind: "warden", desiredMachineId: "m-2", name: "warden-2" }),
      member({
        id: "mira",
        kind: "assistant",
        desiredMachineId: "m-1",
        machine: "m-1",
        status: "online",
        name: "Mira",
      }),
      member({
        id: "bob",
        kind: "assistant",
        desiredMachineId: "m-2",
        machine: null,
        status: "offline",
        lifecycle: "offline",
        name: "Bob",
      }),
    ]);
  });

  it("warns and lists the bound member when the machine still has members", async () => {
    renderMonitor();
    const btn = (await screen.findAllByTestId("mon-uninstall-btn"))[0];
    fireEvent.click(btn);

    const warn = await screen.findByTestId("mon-uninstall-warn");
    expect(warn).toBeTruthy();
    expect(screen.getByTestId("mon-uninstall-warn-members").textContent).toContain(
      "Mira"
    );
    expect(screen.queryByTestId("mon-uninstall-confirm")).toBeNull();
    expect(uninstallMachine).not.toHaveBeenCalled();
  });

  it("goes straight to the plain confirm when every member on the machine is offline", async () => {
    // m-2 still has Bob BOUND to it (desiredMachineId) — but he is offline, so
    // the new actual-online criterion must not warn.
    renderMonitor();
    const btn = (await screen.findAllByTestId("mon-uninstall-btn"))[1];
    fireEvent.click(btn);

    expect(await screen.findByTestId("mon-uninstall-confirm")).toBeTruthy();
    expect(screen.queryByTestId("mon-uninstall-warn")).toBeNull();
  });

  it("renders the disabled uninstalling transitional state while the intent is pending", async () => {
    // A warden's own member id IS the machine id — the transitional lookup
    // joins the machine row onto the roster by that identity.
    listMembers.mockResolvedValue([
      member({
        id: "m-1",
        kind: "warden",
        desiredMachineId: "",
        desiredState: "uninstall",
        name: "warden-1",
      }),
      member({ id: "m-2", kind: "warden", desiredMachineId: "", name: "warden-2" }),
    ]);
    renderMonitor();

    await waitFor(() => {
      const btns = screen.getAllByTestId("mon-uninstall-btn");
      expect((btns[0] as HTMLButtonElement).disabled).toBe(true);
      expect(btns[0].textContent).toContain("解除安裝中");
    });
    // The sibling machine's verb stays the plain enabled label.
    const btns = screen.getAllByTestId("mon-uninstall-btn");
    expect((btns[1] as HTMLButtonElement).disabled).toBe(false);
    expect(btns[1].textContent).toContain("解除安裝");
    expect(btns[1].textContent).not.toContain("解除安裝中");
  });

  it("proceed on the warning runs the uninstall for that machine", async () => {
    renderMonitor();
    const btn = (await screen.findAllByTestId("mon-uninstall-btn"))[0];
    fireEvent.click(btn);
    await screen.findByTestId("mon-uninstall-warn");

    fireEvent.click(screen.getByTestId("mon-uninstall-warn-proceed-btn"));
    await waitFor(() => expect(uninstallMachine).toHaveBeenCalledWith("m-1"));
  });

  it("cancel on the warning closes it without uninstalling", async () => {
    renderMonitor();
    const btn = (await screen.findAllByTestId("mon-uninstall-btn"))[0];
    fireEvent.click(btn);
    const warn = await screen.findByTestId("mon-uninstall-warn");

    fireEvent.click(warn.querySelector("button.btn--ghost") as HTMLButtonElement);
    await waitFor(() => expect(screen.queryByTestId("mon-uninstall-warn")).toBeNull());
    expect(uninstallMachine).not.toHaveBeenCalled();
  });
});
