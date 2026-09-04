// MemberDetailPanel · 改機器 (relocate) control.
//
// Locked here (mirrors the worker panel's 改機器, but placement-only for a
// roster member):
//   1. The 機器 label carries a 改機器 button (data-testid mp-relocate) whenever
//      onRelocate is wired.
//   2. With 2+ online machines the button opens the machine picker; confirming a
//      pick calls onRelocate with the chosen machineId (→ relocateMember at the
//      call site). It NEVER goes through activateMember — a relocate is not a wake.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";
import type { MachineView } from "../types";

const machine = (id: string, displayName: string): MachineView => ({
  machineId: id,
  displayName,
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});
const listMachines = vi.fn<() => Promise<MachineView[]>>(() =>
  Promise.resolve([
    machine("mach-a", "Machine A"),
    machine("mach-b", "Machine B"),
    { ...machine("mach-sleep", "Sleeping Mac"), online: false },
  ]),
);
// Order of the two wire calls a single Change can make. The relocate restarts
// the agent, so the settings PATCH has to be stored BEFORE it goes out.
const wireCalls: string[] = [];
const relocateMember = vi.fn(async (_id: string, _machineId: string) => {
  wireCalls.push("relocate");
});
const patchMember = vi.fn(async (_id: string, _patch: object) => {
  wireCalls.push("patch");
});

vi.mock("../api", () => ({
  api: {
    listMachines: () => listMachines(),
    relocateMember: (id: string, machineId: string) =>
      relocateMember(id, machineId),
    activateMember: vi.fn(),
    patchMember: (id: string, patch: object) => patchMember(id, patch),
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
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "mach-a",
    machine: null,
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

function renderPanel(over: Partial<Member> = {}) {
  const onRelocate = vi.fn(async (machineId: string) => {
    await relocateMember("mira", machineId);
  });
  const onActivate = vi.fn(async () => ({ activationPending: false }));
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember(over)}
        onBack={vi.fn()}
        onActivate={onActivate}
        onRelocate={onRelocate}
      />
    </I18nProvider>,
  );
  return { ...utils, onActivate, onRelocate };
}

beforeEach(() => {
  listMachines.mockClear();
  relocateMember.mockClear();
  patchMember.mockClear();
  wireCalls.length = 0;
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MemberDetailPanel — unified wake/change settings", () => {
  it("keeps the detail fields read-only and sends an offline setting once through activate", async () => {
    const { getByTestId, queryByTestId, onActivate } = renderPanel();
    expect(queryByTestId("mp-relocate")).toBeNull();
    expect(queryByTestId("mp-model-effort-edit")).toBeNull();

    // The wake entry is gated on the machine registry (0 online ⇒ disabled),
    // and that registry loads asynchronously — click too early and nothing
    // opens.
    await waitFor(() =>
      expect((getByTestId("member-action-spawn") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("member-action-spawn"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(onActivate).toHaveBeenCalledWith("mach-b"));
  });

  it("shows the observed machine plus a pending target, then removes the target after arrival", async () => {
    const initial = mkMember({
      status: "online", lifecycle: "online", machine: "mach-a", desiredMachineId: "mach-b",
    });
    const { getByTestId, queryByTestId, rerender } = render(
      <I18nProvider><MemberDetailPanel member={initial} onBack={vi.fn()} /></I18nProvider>,
    );
    await waitFor(() => expect(getByTestId("mp-machine").textContent).toContain("Machine A"));
    expect(getByTestId("mp-machine-pending").textContent).toContain("→ 要換到 Machine B");

    rerender(
      <I18nProvider><MemberDetailPanel member={mkMember({
        status: "online", lifecycle: "online", machine: "mach-b", desiredMachineId: "mach-b",
      })} onBack={vi.fn()} /></I18nProvider>,
    );
    expect(queryByTestId("mp-machine-pending")).toBeNull();
  });

  it("uses Change to apply an awake member's settings through one relocate", async () => {
    const { getByTestId, onActivate, onRelocate } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-a",
    });
    fireEvent.click(getByTestId("mp-change"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(onRelocate).toHaveBeenCalledWith("mach-b"));
    expect(onActivate).not.toHaveBeenCalled();
  });

  it("omits the dialog's comparison note when the card has no reported model to compare with", async () => {
    // The note pointed at a dash unconditionally, which reads as "this agent
    // never reported" — but for a member that is merely not awake the cell is
    // empty by the presence contract, not by absence of reports.
    const { getByTestId } = renderPanel({ actualModel: "" });
    await waitFor(() =>
      expect((getByTestId("member-action-spawn") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("member-action-spawn"));
    const note = getByTestId("mp-settings-intent-note").textContent ?? "";
    expect(note).toContain(zh.mp.settingsIntentNote);
    expect(note).not.toContain(zh.mp.settingsIntentNoteReported);
  });

  it("saves an OFFLINE member's settings without waking it (creator ruling r3)", async () => {
    // Two capabilities the unified dialog had silently removed: editing an
    // offline member's model/effort without starting it, and re-pinning it for
    // its next wake. Both are restored through this second action, and the
    // discriminating assertion is that activate is NOT reached — otherwise
    // "save without waking" wakes.
    const { getByTestId, onActivate, onRelocate } = renderPanel();
    await waitFor(() =>
      expect((getByTestId("member-action-spawn") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("member-action-spawn"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    fireEvent.change(getByTestId("me-model-input"), { target: { value: "haiku" } });
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(getByTestId("mp-settings-save-only"));

    await waitFor(() => expect(wireCalls).toEqual(["patch", "relocate"]));
    expect(patchMember).toHaveBeenCalledWith(
      "mira",
      expect.objectContaining({ model: "haiku" }),
    );
    expect(onRelocate).toHaveBeenCalledWith("mach-b");
    expect(onActivate).not.toHaveBeenCalled();
  });

  it("offers no save-without-waking action for a LIVE member (that is what 更改 is)", async () => {
    const { getByTestId, queryByTestId } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-a",
    });
    fireEvent.click(getByTestId("mp-change"));
    await waitFor(() => expect(getByTestId("me-runtime-select")).toBeTruthy());
    expect(queryByTestId("mp-settings-save-only")).toBeNull();
  });

  it("keeps a pin on an OFFLINE machine instead of silently re-pinning it", async () => {
    // 🔴 Regression guard (independent review r2). The select lists online
    // machines, so an earlier fix seeded the first ONLINE machine whenever the
    // pin was asleep — which made `machineChanged` true the moment the dialog
    // opened, and editing only the MODEL moved the member off the machine the
    // owner had parked it on. Both halves are asserted: the pin stays selectable
    // (labelled offline) AND a model-only edit dispatches no relocate.
    const { getByTestId, onRelocate } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-sleep",
      desiredMachineId: "mach-sleep",
    });
    fireEvent.click(getByTestId("mp-change"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(3));
    expect(select.value).toBe("mach-sleep");
    const sleepingOption = Array.from(select.options).find(
      (o) => o.value === "mach-sleep",
    )!;
    expect(sleepingOption.textContent).toContain("Sleeping Mac");
    // It must SAY it is offline — an entry indistinguishable from the online ones
    // invites the owner to pick it…
    expect(sleepingOption.textContent).toContain(
      zh.machine.picker.offlineOptionSuffix,
    );
    // …and it must not be pickable: keeping the owner's own pin visible is the
    // reason it exists; moving a live member onto a machine with no warden would
    // wind it down into nothing, with the deferred-move signal suppressing the
    // alert that would otherwise say so.
    expect(sleepingOption.disabled).toBe(true);

    fireEvent.change(getByTestId("me-model-input"), { target: { value: "haiku" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(wireCalls).toEqual(["patch"]));
    expect(onRelocate).not.toHaveBeenCalled();
  });

  it("hides the save-without-waking action for a WAKING member too (its confirm activates)", async () => {
    const { getByTestId, queryByTestId } = renderPanel({
      status: "waking",
      lifecycle: "waking",
      machine: "mach-a",
    });
    await waitFor(() =>
      expect((getByTestId("member-action-spawn") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.click(getByTestId("member-action-spawn"));
    await waitFor(() => expect(getByTestId("me-runtime-select")).toBeTruthy());
    expect(queryByTestId("mp-settings-save-only")).toBeNull();
    // …and a waking member is not offered 更改 either: its confirm is an
    // activate, and 更改 promises a graceful handover (guard gap MED-5).
    expect(queryByTestId("mp-change")).toBeNull();
  });

  it("still prints the pending target for an OFFLINE member, beside a dashed cell", async () => {
    // 🔴 REVERSED in T-7f28. This used to assert the opposite: an `awake &&`
    // gate hid the hint whenever the member was not up, on the reasoning that
    // showing the pin would leak desired state into an observed cell.
    //
    // The gate over-corrected. Offline is precisely when the owner has NO other
    // way to tell a re-pin has not taken effect — the machine cell is a dash
    // either way, so hiding the hint made "moved" and "not moved yet" look
    // identical. The presence contract is about not presenting intent AS
    // observation, and both halves of that still hold below: the 機器 cell
    // stays dashed, and the pin appears only in the labelled 「→ 要換到」 line.
    const { queryByTestId, getByTestId } = renderPanel({
      status: "offline",
      lifecycle: "offline",
      machine: "mach-a",
      desiredMachineId: "mach-b",
    });
    await waitFor(() => expect(listMachines).toHaveBeenCalled());
    expect(getByTestId("mp-machine-pending").textContent).toContain(
      "→ 要換到 Machine B",
    );
    // …and the OBSERVED cell is still honest about not observing anything.
    expect(getByTestId("mp-machine").textContent).toBe("—");
    expect(queryByTestId("mp-relocate-undispatched")).toBeNull();
  });

  it("prints nothing at all when the pin and the last landing agree", async () => {
    // The no-clutter half of the owner's condition (2026-07-31:「但又不想要畫面
    // 太雜亂」): no pending change ⇒ no node, not an empty one.
    const { queryByTestId } = renderPanel({
      status: "offline",
      lifecycle: "offline",
      machine: "mach-b",
      desiredMachineId: "mach-b",
    });
    await waitFor(() => expect(listMachines).toHaveBeenCalled());
    expect(queryByTestId("mp-machine-pending")).toBeNull();
  });

  it("stores the launch settings BEFORE the relocate that restarts the agent", async () => {
    const { getByTestId } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-a",
    });
    fireEvent.click(getByTestId("mp-change"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    // Both halves of the one settings block change, so both wire calls fire.
    fireEvent.change(getByTestId("me-model-input"), { target: { value: "sonnet" } });
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(wireCalls).toEqual(["patch", "relocate"]));
    expect(patchMember).toHaveBeenCalledWith(
      "mira",
      expect.objectContaining({ model: "sonnet" }),
    );
  });
});
