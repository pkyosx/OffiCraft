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
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
};

const ONLINE_MACHINE_B = {
  ...ONLINE_MACHINE,
  machineId: "mach-b",
  displayName: "Mac B",
  isSelf: false,
};

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([ONLINE_MACHINE, ONLINE_MACHINE_B]),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
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

async function confirmWakeSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(".machine-picker__actions .btn--accent")!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

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
    await confirmWakeSettings();
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );

    pending = false;
    await waitFor(() => expect(wakeBtn(container).disabled).toBe(false));
    fireEvent.click(wakeBtn(container));
    await confirmWakeSettings();

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
    await confirmWakeSettings();
    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).not.toBeNull(),
    );

    rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({ id: "kyle", name: "Kyle" })}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    await waitFor(() =>
      expect(queryByTestId("mp-wake-undispatched")).toBeNull(),
    );
  });

  it("an IN-FLIGHT wake's verdict never lands on the member switched to (review r2 SHOULD-1)", async () => {
    // 🔴 The member-keyed reset above is a RESET, not a CANCEL. The owner
    // presses 喚醒 on A and — while the request is still out — clicks B. The
    // reset runs on the switch, THEN A's verdict resolves and writes into a
    // panel that is already showing B: "B's wake was never sent", about a wake
    // the owner never asked for on B. Identical lie, one code path over.
    let resolveActivate: (r: MemberActivateResult) => void = () => {};
    const onActivate = vi.fn(
      () =>
        new Promise<MemberActivateResult>((res) => {
          resolveActivate = res;
        }),
    );
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
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));

    // The owner moves on BEFORE the server answers.
    rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({ id: "kyle", name: "Kyle" })}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>,
    );

    // …and only now does A's activate come back undispatched.
    resolveActivate({ activationPending: true });
    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-wake-undispatched")).toBeNull();
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
    await confirmWakeSettings();

    const alert = await findByTestId("mp-wake-undispatched");
    expect(alert.textContent).toContain(zh.dispatchAlert.wakeTitle);
    // …and explicitly NOT the relocate twin.
    expect(alert.textContent).not.toContain(zh.dispatchAlert.relocateTitle);
    // The STEPS are per-kind too (review r2 NIT-6): the wake half must keep its
    // hedged pair and must not borrow the relocate half's certainty.
    expect(alert.textContent).toContain(zh.dispatchAlert.wakeStep2);
    expect(alert.textContent).not.toContain(zh.dispatchAlert.relocateStep1);
  });
});

/** An awake member — the only state in which `member.machine` is an OBSERVATION
 * rather than a desired_machine residual (see the self-heal tests below). */
const mkAwake = (over: Partial<Member> = {}): Member =>
  mkMember({ status: "online", lifecycle: "online", ...over });

/** Fire a relocate through the unified 更改 dialog (the standalone 改機器 button
 * is gone) and wait until the handler has actually been called.
 *
 * The dialog is what WRITES the pin now, so the timeline below has one more
 * beat than it used to: the member enters observed-on-and-pinned-to `mach-a`
 * (nothing outstanding), the dialog moves the pin to `mach-b`, and the parent's
 * refetch is what makes the panel see `desiredMachineId: "mach-b"` — which is
 * when a "not dispatched" verdict becomes visible at all. That beat is
 * `afterRefetch` below, not test scaffolding: the pin is written even when
 * nothing was dispatched, so this is the state the owner really lands on. */
async function relocateInto(
  getByTestId: (id: string) => HTMLElement,
  target: "mach-a" | "mach-b" = "mach-b",
) {
  await waitFor(() =>
    expect((getByTestId("mp-change") as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(getByTestId("mp-change"));
  const select = document.querySelector<HTMLSelectElement>(
    "select.machine-picker__select",
  )!;
  await waitFor(() => expect(select.options).toHaveLength(2));
  fireEvent.change(select, { target: { value: target } });
  const confirm = document.querySelector<HTMLButtonElement>(
    ".machine-picker__actions .btn--accent",
  )!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

/** The member as the panel enters: observed exactly where it is pinned, so
 * nothing is outstanding and no verdict can be hiding. */
const pinnedHome = (over: Partial<Member> = {}): Member =>
  mkAwake({ desiredMachineId: "mach-a", machine: "mach-a", ...over });

const panel = (member: Member, onRelocate: MemberDetailPanelRelocate) => (
  <I18nProvider>
    <MemberDetailPanel member={member} onBack={() => {}} onRelocate={onRelocate} />
  </I18nProvider>
);

type MemberDetailPanelRelocate = (
  machineId: string,
) => void | Promise<MemberRelocateResult | void>;

// Relocate is no longer a standalone panel affordance: it is folded into the
// unified settings submit. But the notice's HYGIENE — how long it lives, which
// member it belongs to, whether it can come back — is a property of the NOTICE,
// not of the entry point. These ran under `describe.skip` for one round, and
// that skip was hiding removed BEHAVIOUR, not a retired button: the member panel
// dropped `useRelocateMachine` for a bare `useState`, and with it the self-heal,
// the observability gate, the member-keyed reset and the in-flight subject
// guard. The hook still holds them for the outsource panel; only this panel lost
// them. Restored in the panel; each one is pinned by the test that goes red when
// it is taken away again.
describe("relocate notice self-heals", () => {
  it("clears once the member's OBSERVED machine reaches the pinned one", async () => {
    // The copy promises "the server keeps retrying in the background". Before
    // this there was no path back: the flag was cleared only by ANOTHER
    // relocate, so a move that the cadence successfully landed left the panel
    // insisting it had not — indefinitely.
    //
    // 🔴 The "landed" step moves ONLY `machine` (review r2 NIT-3). Moving the
    // pin with it could not tell "the member reached the pin" (a landed move)
    // apart from "the pin retreated to the member" (no move at all) — and only
    // one of those is what this self-heal claims to detect.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    // The refetch: the pin moved, the member has not.
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    // The background retry lands: the pin is untouched, the member arrives.
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-b" }), onRelocate));

    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).toBeNull(),
    );
  });

  it("stays quiet until the refetch moves the pin (the momentary render guard)", async () => {
    // 🔴 The render-time `!relocateLanded` short-circuit is the OTHER half of
    // the pair, and it is not redundant: between the submit and the parent's
    // refetch the panel still holds the pre-move props, where observed === pin.
    // `relocateLanded` is therefore ALREADY true and does not change, so the
    // latch effect never re-runs — without this guard the verdict would appear
    // over a member the panel is simultaneously drawing as sitting at home, and
    // nothing would ever take it down again.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    await waitFor(() => expect(onRelocate).toHaveBeenCalledTimes(1));
    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();

    // …and it is not swallowed either: the refetch makes it visible.
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );
  });

  it("does NOT clear on a placement nobody can observe (review r2 SHOULD-2)", async () => {
    // 🔴 `member.machine` is not a pure observation. The server's observedHost
    // falls back to `desired_machine_id` when the hub has no session and
    // telemetry has nothing, so for a member that is not awake the equality
    // `machine === desiredMachineId` is true BY CONSTRUCTION — it encodes "we
    // cannot see it", and reading it as "it arrived" retires the notice on a
    // move that never happened. The panel already refuses to PRINT that value
    // (機器 renders 「—」 outside online/waking); it must not believe it either.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    // The member drops offline; `machine` now MIRRORS the pin because nobody
    // can see it — the exact shape of the false "landed".
    rerender(panel(mkMember({ desiredMachineId: "mach-b", machine: "mach-b" }), onRelocate));

    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull();
  });

  it("still shows the verdict for an UNPINNED member with no machine (review r2 NIT-1)", async () => {
    // The null/"" guards inside `landed` are load-bearing: without them an
    // unpinned member (desiredMachineId "" → null) that is nowhere observable
    // compares null === null, reads as "landed", and SWALLOWS a verdict the
    // server just gave. A notice that never renders is the silence this ticket
    // exists to remove.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId } = render(
      panel(mkAwake({ desiredMachineId: "", machine: null }), onRelocate),
    );

    await relocateInto(getByTestId);
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );
    // …and it is the RELOCATE half of the copy, steps included — the wake steps
    // hedge two possible causes, which is the wrong shape for this flag.
    const alert = getByTestId("mp-relocate-undispatched");
    expect(alert.textContent).toContain(zh.dispatchAlert.relocateTitle);
    expect(alert.textContent).toContain(zh.dispatchAlert.relocateStep1);
    expect(alert.textContent).not.toContain(zh.dispatchAlert.wakeStep2);
  });

  it("does not RESURRECT once healed, even if the member drifts off the pin (review r2 SHOULD-4)", async () => {
    // 🔴 Two guards clear this notice and they are NOT interchangeable: the
    // effect is a LATCH (cleared for good), the render guard is momentary. The
    // reviewer deleted the effect and all 908 tests stayed green — yet without
    // it a member that lands and then drifts off the pin RESURRECTS a verdict
    // about an attempt that is long over. Whoever tidies away the "redundant"
    // guard next must be told by a red test which one does the work.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-b" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).toBeNull(),
    );

    // The member is later moved AWAY from the pin by something else entirely.
    // The old verdict must stay dead.
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));

    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();
  });

  it("an IN-FLIGHT relocate's verdict never lands on the member switched to (relocate twin of r2 SHOULD-1)", async () => {
    // The subject reset below is a reset, not a cancel — the wake half needed
    // both guards and so does this one. Written at the same time as the guard,
    // because "the mechanism is obviously right" is exactly what the last two
    // rounds' green mutants were.
    let resolveRelocate: (r: MemberRelocateResult) => void = () => {};
    const onRelocate = vi.fn(
      () =>
        new Promise<MemberRelocateResult>((res) => {
          resolveRelocate = res;
        }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    await waitFor(() => expect(onRelocate).toHaveBeenCalledTimes(1));

    // The owner switches members while the relocate is still in flight — and
    // the member switched TO is one whose pin is outstanding, so a leaked
    // verdict would be perfectly visible on it.
    rerender(
      panel(
        mkAwake({
          id: "kyle",
          name: "Kyle",
          desiredMachineId: "mach-b",
          machine: "mach-a",
        }),
        onRelocate,
      ),
    );

    resolveRelocate({ relocationPending: true });
    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();
  });

  it("drops the previous verdict when a fresh attempt is fired", async () => {
    // The wake half has this and so does the hook; the hand-written twin did not
    // (independent review r3). Without it a relocate that failed and was then
    // retried SUCCESSFULLY keeps its "nothing was dispatched" alert on screen —
    // a notice about an attempt that is over, which is the exact lie this whole
    // notice family exists to remove.
    const verdicts: MemberRelocateResult[] = [
      { relocationPending: true },
      { relocationPending: false },
    ];
    const onRelocate = vi.fn(async () => verdicts.shift()!);
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    // Second attempt — back to the machine it is observed on, so the pin really
    // changes again — and this time the move IS dispatched.
    await relocateInto(getByTestId, "mach-a");
    await waitFor(() => expect(onRelocate).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).toBeNull(),
    );
  });

  it("does not wipe a live verdict just because the dialog was opened and cancelled", async () => {
    // 🔴 Pins WHERE the fresh-attempt clear lives (independent review r2). Moving
    // it into `openSettings` keeps the sibling test green while making
    // open-then-cancel erase a verdict about a move that is still outstanding —
    // the notice would vanish with nothing having changed.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );
    await relocateInto(getByTestId);
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    fireEvent.click(getByTestId("mp-change"));
    const cancel = document.querySelector<HTMLButtonElement>(
      ".machine-picker__actions .btn--ghost",
    )!;
    fireEvent.click(cancel);

    await new Promise((r) => setTimeout(r, 0));
    expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull();
  });

  it("closes the settings dialog when the owner switches members", async () => {
    // 🔴 The dialog is PREFILLED from the member it was opened for, and neither
    // caller passes a `key`, so switching members is a prop change: an open
    // dialog survives holding the previous member's values, and one confirm
    // writes them to somebody else (independent review r3). The member-keyed
    // reset must therefore cover the draft, not only the notices.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: false }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );
    await waitFor(() =>
      expect((getByTestId("mp-change") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("mp-change"));
    expect(queryByTestId("me-runtime-select")).not.toBeNull();

    rerender(
      panel(
        mkAwake({
          id: "kyle",
          name: "Kyle",
          desiredMachineId: "mach-a",
          machine: "mach-a",
        }),
        onRelocate,
      ),
    );
    await waitFor(() => expect(queryByTestId("me-runtime-select")).toBeNull());
  });

  it("does NOT follow the owner onto a different member (relocate twin of r1 SHOULD-1)", async () => {
    // The wake notice was given a member-keyed reset in round 1; the relocate
    // notice had none. It was MASKED: before the observability gate `landed` was
    // accidentally true for most not-observable members and quietly swallowed
    // the leak. Same class, same panel, same lie — asserted here so it cannot
    // come back.
    const onRelocate = vi.fn(
      async (): Promise<MemberRelocateResult> => ({ relocationPending: true }),
    );
    const { getByTestId, queryByTestId, rerender } = render(
      panel(pinnedHome(), onRelocate),
    );

    await relocateInto(getByTestId);
    rerender(panel(mkAwake({ desiredMachineId: "mach-b", machine: "mach-a" }), onRelocate));
    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).not.toBeNull(),
    );

    rerender(
      panel(
        mkAwake({
          id: "kyle",
          name: "Kyle",
          desiredMachineId: "mach-b",
          machine: "mach-a",
        }),
        onRelocate,
      ),
    );

    await waitFor(() =>
      expect(queryByTestId("mp-relocate-undispatched")).toBeNull(),
    );
  });
});
