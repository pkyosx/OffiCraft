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
    terminalAttachCommand: "tmux -L officraft attach -t member-mira",
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

describe("MemberDetailPanel — the 停止 ladder", () => {
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
    // In `stopping` the ONE ladder cell at this stage IS 加速停止.
    // The panel mounts already at that stage, so LADDER_ARM_MS is not in play
    // and the in-flight case this pins is the one the owner reaches by pressing
    // 加速停止 twice.
    const accelerated = getByTestId("member-action-accelerated-stop");
    fireEvent.click(accelerated);
    fireEvent.click(accelerated);
    await waitFor(() => expect(onAcceleratedStop).toHaveBeenCalledTimes(1));
  });

  it.each([
    ["member-action-stop", { lifecycle: "online" }],
    ["member-action-accelerated-stop", { lifecycle: "stopping", desiredState: "offline" }],
    ["member-action-stop", { lifecycle: "waking", status: "waking" }],
  ] as const)(
    "a rejected %s on %j says 操作失敗，請稍後重試",
    async (testId, over) => {
      const reject = vi.fn(async () => {
        throw new Error("http 500");
      });
      const { getByTestId, findByTestId, queryByTestId } = render(
        <I18nProvider>
          <MemberDetailPanel
            member={mkMember(over)}
            onBack={vi.fn()}
            onActivate={vi.fn()}
            onRelocate={vi.fn()}
            onDeactivate={reject}
            onAcceleratedStop={reject}
          />
        </I18nProvider>,
      );
      expect(queryByTestId("mp-stop-error")).toBeNull();
      fireEvent.click(getByTestId(testId));
      expect((await findByTestId("mp-stop-error")).textContent).toBe(
        "操作失敗，請稍後重試",
      );
    },
  );

  it("a retry of a rejected 停止 clears 操作失敗，請稍後重試", async () => {
    const onDeactivate = vi
      .fn<() => Promise<void>>()
      .mockRejectedValueOnce(new Error("http 500"))
      .mockResolvedValueOnce(undefined);
    const { getByTestId, findByTestId, queryByTestId } = render(
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
    fireEvent.click(getByTestId("member-action-stop"));
    await findByTestId("mp-stop-error");
    await waitFor(() =>
      expect((getByTestId("member-action-stop") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("member-action-stop"));
    await waitFor(() => expect(onDeactivate).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(queryByTestId("mp-stop-error")).toBeNull());
  });

  it("after 強制停止, while the session is still connected, the one ladder button stays 強制停止", () => {
    const { container } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({
            lifecycle: "stopping",
            desiredState: "offline",
            refocusOp: "",
            forcedStopLive: true,
          })}
          onBack={vi.fn()}
          onActivate={vi.fn()}
          onRelocate={vi.fn()}
          onDeactivate={vi.fn()}
          onAcceleratedStop={vi.fn()}
          onForceStop={vi.fn()}
        />
      </I18nProvider>,
    );
    expect(
      Array.from(container.querySelectorAll("[data-testid^='member-action-']"))
        .map((b) => [b.getAttribute("data-testid"), b.textContent]),
    ).toEqual([["member-action-force-stop", "強制停止"]]);
  });

  it("a rejected 強制停止 closes its confirm and says 操作失敗，請稍後重試", async () => {
    const onForceStop = vi.fn(async () => {
      throw new Error("http 500");
    });
    const { getByTestId, findByTestId, queryByTestId } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({
            lifecycle: "stopping",
            desiredState: "offline",
            refocusOp: "accelerated_stop",
          })}
          onBack={vi.fn()}
          onActivate={vi.fn()}
          onRelocate={vi.fn()}
          onDeactivate={vi.fn()}
          onForceStop={onForceStop}
        />
      </I18nProvider>,
    );
    fireEvent.click(getByTestId("member-action-force-stop"));
    fireEvent.click(await findByTestId("mp-force-stop-confirm-btn"));
    expect((await findByTestId("mp-stop-error")).textContent).toBe(
      "操作失敗，請稍後重試",
    );
    expect(onForceStop).toHaveBeenCalledTimes(1);
    expect(queryByTestId("mp-force-stop-confirm")).toBeNull();
  });
});
