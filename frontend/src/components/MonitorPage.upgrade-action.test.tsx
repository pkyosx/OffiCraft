// One-click upgrade in the machine ACTION GROUP (T-5f01 rework) — Monitor §2.
//
// The owner killed the separate 版本 column/badge: the upgrade button now
// lives in the row's action group. Visibility = installed (warden online —
// an offline warden has no downstream to command and self-updates on its
// next connect). Enablement = the server's fingerprint verdict says a newer
// build exists ("stale"); "current"/unknown render the button disabled with
// the honest reason as tooltip. Clicking POSTs /api/machines/{id}/upgrade
// once and latches the button into the disabled 升級中 face until the verdict
// ITSELF converges to "current" on a later refetch (reconcile-by-refetch —
// the FE never fabricates the convergence).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { MachineView, UpgradeResultView } from "../types";

const upgradeMachine = vi.fn(
  async (id: string): Promise<UpgradeResultView> => ({
    memberId: id,
    machineId: id,
    dispatched: true,
  })
);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
// Captured SSE topic subscribers — firing one with a "machine" topic drives
// the SAME reconcile-by-refetch path the real downlink does (useMachines).
const topicSubscribers = new Set<(topic: string) => void>();

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([]),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: (onTopic: (topic: string) => void) => {
      topicSubscribers.add(onTopic);
      return () => topicSubscribers.delete(onTopic);
    },
    upgradeMachine: (id: string) => upgradeMachine(id),
  },
}));

const machine = (
  over: Partial<MachineView> & Pick<MachineView, "machineId">
): MachineView => ({
  displayName: over.machineId,
  online: true,
  isSelf: false,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
  ...over,
});

async function renderMonitor() {
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
  await waitFor(() =>
    expect(screen.getAllByTestId("mon-upgrade-btn").length).toBeGreaterThan(0)
  );
}

describe("MonitorPage upgrade action (in the machine action group)", () => {
  beforeEach(() => {
    upgradeMachine.mockClear();
    listMachines.mockClear();
    topicSubscribers.clear();
  });

  it("shows the button only on installed (online) rows; enabled ONLY when stale", async () => {
    listMachines.mockResolvedValue([
      machine({ machineId: "m-old", binStatus: "stale" }),
      machine({ machineId: "m-cur", binStatus: "current" }),
      machine({ machineId: "m-unk", binStatus: null }),
      // NOT installed/reachable — no button at all (self-updates on reconnect).
      machine({ machineId: "m-off", binStatus: "stale", online: false }),
    ]);
    await renderMonitor();

    // The old 版本 column/badge is GONE — the verdict surfaces only through
    // the action button's state.
    expect(screen.queryAllByTestId("mon-bin-status")).toHaveLength(0);

    // 3 online rows carry the button (DOM order = row order); the offline row none.
    const btns = screen.getAllByTestId("mon-upgrade-btn") as HTMLButtonElement[];
    expect(btns).toHaveLength(3);
    // stale → enabled; current/unknown → disabled with the honest tooltip.
    expect(btns.map((b) => b.disabled)).toEqual([false, true, true]);
    expect(btns.map((b) => b.textContent)).toEqual(["升級", "升級", "升級"]);
    expect(btns[0].getAttribute("title")).toBeNull();
    expect(btns[1].getAttribute("title")).toBe("已是最新版");
    expect(btns[2].getAttribute("title")).toBe(
      "尚未回報版本指紋,無法判斷是否有新版"
    );
  });

  it("clicking POSTs once and wears 升級中 (disabled) until the verdict converges to current", async () => {
    // A FRESH array per call — the real adapter never returns the same array
    // instance twice, and an identical reference would let React bail out of
    // the machines state update, silently skipping the latch-release effect.
    listMachines.mockImplementation(async () => [
      machine({ machineId: "m-old", binStatus: "stale", online: true }),
    ]);
    // Hold the POST open so the test can tell the two 升級中 faces apart: the
    // in-flight transition (upgradeBusy) vs the post-dispatch LATCH.
    let resolveUpgrade!: () => void;
    upgradeMachine.mockImplementationOnce(
      (id: string) =>
        new Promise<UpgradeResultView>((resolve) => {
          resolveUpgrade = () =>
            resolve({ memberId: id, machineId: id, dispatched: true });
        })
    );
    await renderMonitor();

    const btn = screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    expect(btn.textContent).toBe("升級");
    fireEvent.click(btn);

    await waitFor(() => expect(upgradeMachine).toHaveBeenCalledTimes(1));
    expect(upgradeMachine).toHaveBeenCalledWith("m-old");
    // In-flight: 升級中, disabled (the upgradeBusy transition).
    await waitFor(() =>
      expect(screen.getByTestId("mon-upgrade-btn").textContent).toBe("升級中…")
    );
    expect(
      (screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement).disabled
    ).toBe(true);

    // Settle the POST completely (dispatch + the post-dispatch refetch that
    // STILL reports stale + the busy release). From here on, any 升級中 face
    // can only come from the LATCH — never the in-flight transition.
    await act(async () => {
      resolveUpgrade();
    });
    const latched = screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement;
    expect(latched.textContent).toBe("升級中…");
    expect(latched.disabled).toBe(true);

    // Drive ANOTHER refetch that still reports stale and assert the latch
    // HOLDS — a latch that released on "any refetch" (instead of only on
    // convergence) would flip back to 升級 here.
    const callsBefore = listMachines.mock.calls.length;
    await act(async () => {
      topicSubscribers.forEach((fn) => fn("machine"));
    });
    await waitFor(() =>
      expect(listMachines.mock.calls.length).toBeGreaterThan(callsBefore)
    );
    const held = screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement;
    expect(held.textContent).toBe("升級中…");
    expect(held.disabled).toBe(true);

    // Now the machine converges: a later refetch (SSE topic) reports
    // "current" → the latch releases into the plain disabled up-to-date face.
    listMachines.mockResolvedValue([
      machine({ machineId: "m-old", binStatus: "current", online: true }),
    ]);
    act(() => {
      topicSubscribers.forEach((fn) => fn("machine"));
    });
    await waitFor(() =>
      expect(screen.getByTestId("mon-upgrade-btn").textContent).toBe("升級")
    );
    const healed = screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement;
    expect(healed.disabled).toBe(true);
    expect(healed.getAttribute("title")).toBe("已是最新版");
    // Still exactly one POST — convergence is observed, never re-fired.
    expect(upgradeMachine).toHaveBeenCalledTimes(1);
  });

  it("a dispatched=false answer (raced offline) surfaces the hint banner, no latch", async () => {
    upgradeMachine.mockResolvedValueOnce({
      memberId: "m-old",
      machineId: "m-old",
      dispatched: false,
    });
    listMachines.mockResolvedValue([
      machine({ machineId: "m-old", binStatus: "stale", online: true }),
    ]);
    await renderMonitor();

    fireEvent.click(screen.getByTestId("mon-upgrade-btn"));
    await waitFor(() =>
      expect(
        screen.queryByText("機器離線,無法下發升級(上線時會自動更新)")
      ).not.toBeNull()
    );
    // Not latched — the command never reached a downstream; the row can retry.
    const btn = screen.getByTestId("mon-upgrade-btn") as HTMLButtonElement;
    expect(btn.textContent).toBe("升級");
    expect(btn.disabled).toBe(false);
  });
});
