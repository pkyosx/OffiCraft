// MemberDetailPanel · 喚醒沒有送出去 (T-7fa1).
//
// 🔴 WHY THESE ARE TRANSITION TESTS, NOT RENDER SNAPSHOTS. The bug this file
// guards was invisible to every static assertion that already existed: the
// panel rendered CORRECTLY at every individual instant — the waking row
// (更改 ＋ 停止) is the right thing to show right after a click. What was wrong was the SEQUENCE:
// the state entered on click had no exit when the activate came back saying
// nothing had been dispatched, so it stayed forever. A snapshot of "after the
// click it shows the waking row" passes on the broken build and the fixed one alike.
// So each test here drives click → resolve → assert the state MOVED.
//
// Locked here:
//   1. POSITIVE: click → activate resolves {activationPending: true} → the
//      optimistic waking row is ROLLED BACK (the wake button is usable again)
//      and the notice appears.
//   2. NEGATIVE: click → activate resolves {activationPending: false} → the
//      optimistic waking row SURVIVES and no notice appears. Only asserting the
//      positive half would pass a mutant that just never sets wakePending.
//   3. A void-returning handler (the pre-T-7fa1 contract) keeps the old
//      behaviour — the panel must not invent a failure it was not told about.

import { describe, it, expect, vi } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member, MemberActivateResult } from "../types";

const ONLINE_MACHINE = {
  machineId: "mach-a",
  displayName: "Mac",
  online: true,
  isSelf: true,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

vi.mock("../api", () => ({
  api: {
    // ONE online machine → the wake button is enabled and auto-uses it (no
    // picker), so a click fires the activate directly.
    listMachines: () => Promise.resolve([ONLINE_MACHINE]),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    patchMember: () => Promise.resolve({}),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "tmux -L officraft attach -t member-mira",
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

function renderPanel(
  onActivate: (
    machineId?: string,
  ) => void | Promise<MemberActivateResult | void>,
) {
  return render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember()}
        onBack={() => {}}
        onActivate={onActivate}
      />
    </I18nProvider>,
  );
}

/** The wake button, found the way the owner finds it: the enabled action in the
 * identity header. It exists only while the member is not up or coming up. */
function wakeButton(container: HTMLElement): HTMLButtonElement {
  const btn = container.querySelector('[data-testid="member-action-spawn"]') as HTMLButtonElement | null;
  // Existence assertion paired with every read (手冊 §1): a renamed class must
  // fail loudly here, not silently turn every assertion below into a no-op.
  expect(btn, "the wake button must exist").not.toBeNull();
  return btn!;
}

/** The row a waking member shows: 更改 ＋ 停止, and no 喚醒 to fire twice. */
function expectWakingRow(container: HTMLElement) {
  expect(
    Array.from(container.querySelectorAll(".mp-identity__buttons button")).map(
      (b) => [b.getAttribute("data-testid"), b.textContent],
    ),
  ).toEqual([
    ["mp-change", "更改"],
    ["member-action-stop", "停止"],
  ]);
}

async function confirmWakeSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(".machine-picker__actions .btn--accent")!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

describe("MemberDetailPanel · wake that was never dispatched (T-7fa1)", () => {
  it("rolls the optimistic waking row back and shows the notice when activation_pending is true", async () => {
    const onActivate = vi.fn(async () => ({ activationPending: true }));
    const { container, queryByTestId } = renderPanel(onActivate);

    const btn = wakeButton(container);
    await waitFor(() => expect(btn.disabled).toBe(false));
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();

    fireEvent.click(btn);
    await confirmWakeSettings();

    // MID-FLIGHT: the optimistic state is entered (this is correct behaviour —
    // and it is exactly the state that used to be permanent).
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));

    // AFTER the verdict: the state MOVED. Both halves matter — a fix that only
    // showed the notice but left the button disabled would still strand the
    // owner with no way to retry.
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );
    await waitFor(() => expect(wakeButton(container).disabled).toBe(false));
  });

  it("keeps the optimistic waking row when activation_pending is false (a real wake)", async () => {
    const onActivate = vi.fn(async () => ({ activationPending: false }));
    const { container, queryByTestId } = renderPanel(onActivate);

    const btn = wakeButton(container);
    await waitFor(() => expect(btn.disabled).toBe(false));

    fireEvent.click(btn);
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));

    // The member is still offline (presence has not caught up yet) — so the
    // panel must STAY pending. Clearing it here would put an idle-looking wake
    // button in front of a wake that IS in progress, and invite a double wake.
    await waitFor(() => expectWakingRow(container));
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();
  });

  it("treats a void-returning handler as before (no fabricated failure)", async () => {
    const onActivate = vi.fn(async () => {});
    const { container, queryByTestId } = renderPanel(onActivate);

    const btn = wakeButton(container);
    await waitFor(() => expect(btn.disabled).toBe(false));

    fireEvent.click(btn);
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));

    await waitFor(() => expectWakingRow(container));
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();
  });
});
