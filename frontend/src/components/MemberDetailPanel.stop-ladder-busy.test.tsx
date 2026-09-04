// MemberDetailPanel · the 停止 ladder must not fire twice on two clicks.
//
// The worker panel has carried an in-flight guard since it grew the ladder
// (WorkerDetailPanel's stopBusy: every rung returns early while one is in
// flight). The member panel wired its rungs straight at the props, so a double
// click sent deactivateMember twice — and the whole premise of T-ed79 is that
// 正職 and 外包 walk the SAME ladder, which makes a guard one side has and the
// other does not a gap this ticket opened rather than an old one.
//
// The rungs are DELIBERATELY tested as one: they share a single flag because
// they are one escalation. Since owner 2026-08-22 they are also literally one
// BUTTON (「同一個按鈕 升級的概念」), so the flag now guards a slot rather than a
// row — a stop still in flight must not let the cell that replaces it fire.
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member, MachineView } from "../types";

const machine: MachineView = {
  machineId: "mach-a",
  displayName: "Machine A",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([machine]),
    relocateMember: vi.fn(),
    activateMember: vi.fn(),
    patchMember: vi.fn(),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "mach-a",
    machine: "mach-a",
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

/** A rung whose promise this test controls, so "in flight" is a real state and
 * not a race the test hopes to win. */
function deferred() {
  let release!: () => void;
  const promise = new Promise<void>((res) => {
    release = res;
  });
  return { promise, release };
}

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MemberDetailPanel — the 停止 ladder holds while a rung is in flight", () => {
  it("sends ONE deactivate for two clicks on 停止", async () => {
    const gate = deferred();
    const onDeactivate = vi.fn(() => gate.promise);
    const { getByTestId } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember()}
          onBack={vi.fn()}
          onActivate={vi.fn()}
          onRelocate={vi.fn()}
          onDeactivate={onDeactivate}
        />
      </I18nProvider>,
    );
    const stop = getByTestId("member-action-stop");
    fireEvent.click(stop);
    fireEvent.click(stop);
    await waitFor(() => expect(onDeactivate).toHaveBeenCalledTimes(1));

    // …and the guard RELEASES: a stop that failed or was undone has to stay
    // pressable, or one click would take the button out of service for good.
    gate.release();
    await waitFor(() =>
      expect((stop as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(stop);
    await waitFor(() => expect(onDeactivate).toHaveBeenCalledTimes(2));
  });

  it("holds 加速停止 while 停止 is still in flight", async () => {
    const gate = deferred();
    const onDeactivate = vi.fn(() => gate.promise);
    const onAcceleratedStop = vi.fn(async () => {});
    const { getByTestId } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({
            status: "online",
            lifecycle: "stopping",
            // 🔴 The stage the ladder reads is the SERVER's acceptance gate, not
            // presence alone: the 下線 arm is desired-offline + a live session.
            // Without the intent this fixture is a member nothing has asked to
            // stop, and 加速停止 would not exist to click twice.
            desiredState: "offline",
          })}
          onBack={vi.fn()}
          onActivate={vi.fn()}
          onRelocate={vi.fn()}
          onDeactivate={onDeactivate}
          onAcceleratedStop={onAcceleratedStop}
        />
      </I18nProvider>,
    );
    // In `stopping` the panel offers 喚醒 ＋ the ONE ladder cell, which at this
    // stage IS 加速停止 (owner 2026-08-22 — 停止 is not kept beside it any more).
    // The panel mounts already at that stage, so LADDER_ARM_MS is not in play
    // and the in-flight case this pins is the one the owner reaches by pressing
    // 加速停止 twice.
    const accelerated = getByTestId("member-action-accelerated-stop");
    fireEvent.click(accelerated);
    fireEvent.click(accelerated);
    await waitFor(() => expect(onAcceleratedStop).toHaveBeenCalledTimes(1));
  });
});
