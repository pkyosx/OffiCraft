// MemberDetailPanel · 告示的衛生條件 (T-7fa1, review r1 rework).
//
// 🔴 EVERY TEST HERE EXISTS BECAUSE A REVIEWER MUTANT WAS **GREEN**. The first
// round proved the notice APPEARS when it should and STAYS AWAY when it should
// not. It proved nothing about the notice's lifetime or its identity, and the
// independent review found four separate holes by injecting mutants that no
// test caught:
//
//   ME — deleting the "a fresh attempt clears the previous verdict" reset  → GREEN
//   MF — swapping kind="wake" for kind="relocate" (testId unchanged)       → GREEN
//   (plus a probe proving the notice survives a MEMBER SWITCH — the panel is
//    given no `key` by either caller, so it is a prop change, not a remount)
//   (plus the relocate notice never self-healing after the move landed)
//
// A stale or mislabelled notice is the SAME failure mode this ticket exists to
// remove — an assertion on screen that is not true. So each of these asserts a
// property of the notice OVER TIME or about WHICH notice it is, not merely that
// one rendered.

import { describe, it, expect, vi } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { zh } from "../i18n/locales/zh";
import type {
  Member,
  MemberActivateResult,
  MemberRelocateResult,
} from "../types";

const ONLINE_MACHINE = {
  machineId: "mach-a",
  displayName: "Mac",
  online: true,
  isSelf: true,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([ONLINE_MACHINE]),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: true,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

const wakeBtn = (c: HTMLElement) => {
  const b = c.querySelector(
    '[data-testid="member-action-spawn"]',
  ) as HTMLButtonElement | null;
  expect(b, "the wake button must exist").not.toBeNull();
  return b!;
};

describe("MemberDetailPanel · the notice does not outlive its truth (T-7fa1)", () => {
  it("a RETRY clears the previous verdict before the new one lands (mutant ME)", async () => {
    // First attempt is undispatched, second one actually goes out. If the reset
    // is deleted the stale notice rides straight through the successful retry
    // and tells the owner nothing was sent — while a wake IS in flight.
    let pending = true;
    const onActivate = vi.fn(
      async (): Promise<MemberActivateResult> => ({
        activationPending: pending,
      }),
    );
    const { container, queryByTestId } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember()}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    await waitFor(() => expect(wakeBtn(container).disabled).toBe(false));
    fireEvent.click(wakeBtn(container));
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );

    pending = false;
    await waitFor(() => expect(wakeBtn(container).disabled).toBe(false));
    fireEvent.click(wakeBtn(container));

    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).toBeNull(),
    );
    // …and the panel is back to the honest in-progress state.
    expect(wakeBtn(container).disabled).toBe(true);
  });

  it("the notice does NOT follow the owner onto a different member (review r1 SHOULD-1)", async () => {
    // Neither OfficePage nor MonitorPage passes a `key`, so swapping which
    // member the panel shows is a PROP CHANGE. Both members here are offline,
    // so the lifecycle-keyed reset never fires — only a member-keyed one saves
    // this. A residual notice would assert that a wake failed for someone the
    // owner never tried to wake.
    const onActivate = vi.fn(async () => ({ activationPending: true }));
    const { container, queryByTestId, rerender } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember()}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    await waitFor(() => expect(wakeBtn(container).disabled).toBe(false));
    fireEvent.click(wakeBtn(container));
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );

    rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({ id: "kyle", name: "Kyle", memberId: "MB-DEV001" })}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).toBeNull(),
    );
  });

  it("says WAKE, not RELOCATE — the kind is asserted by content (mutant MF)", async () => {
    // MF swapped kind="wake" → kind="relocate" and every test stayed green,
    // because they all keyed on the data-testid, which the kind does not change.
    // Two notices with one identity is a drift waiting to happen, so pin the
    // actual leading text.
    const onActivate = vi.fn(async () => ({ activationPending: true }));
    const { container, findByTestId } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember()}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    await waitFor(() => expect(wakeBtn(container).disabled).toBe(false));
    fireEvent.click(wakeBtn(container));

    const alert = await findByTestId("mp-wake-undispatched");
    expect(alert.textContent).toContain(zh.dispatchAlert.wakeTitle);
    // …and explicitly NOT the relocate twin.
    expect(alert.textContent).not.toContain(zh.dispatchAlert.relocateTitle);
  });
});

describe("MemberDetailPanel · the relocate notice self-heals (review r1 SHOULD-2)", () => {
  it("clears once the member's CURRENT machine reaches the pinned one", async () => {
    // The copy promises "the server keeps retrying in the background". Before
    // this there was no path back: the flag was cleared only by ANOTHER
    // relocate, so a move that the cadence successfully landed left the panel
    // insisting it had not — indefinitely.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const before = mkMember({ desiredMachineId: "mach-b", machine: "mach-a" });
    const { getByTestId, queryByTestId, rerender } = render(
      <I18nProvider>
        <MemberDetailPanel
          member={before}
          onBack={() => {}}
          onRelocate={onRelocate}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect((getByTestId("mp-relocate") as HTMLButtonElement).disabled).toBe(
        false,
      ),
    );
    fireEvent.click(getByTestId("mp-relocate"));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    // The background retry lands: observed machine == pinned machine.
    rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({ desiredMachineId: "mach-a", machine: "mach-a" })}
          onBack={() => {}}
          onRelocate={onRelocate}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).toBeNull(),
    );
  });
});
