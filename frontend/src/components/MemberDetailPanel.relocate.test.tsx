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
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});
const listMachines = vi.fn<() => Promise<MachineView[]>>(() =>
  Promise.resolve([machine("mach-a", "Machine A"), machine("mach-b", "Machine B")]),
);
const relocateMember = vi.fn(async (_id: string, _machineId: string) => {});

vi.mock("../api", () => ({
  api: {
    listMachines: () => listMachines(),
    relocateMember: (id: string, machineId: string) =>
      relocateMember(id, machineId),
    activateMember: vi.fn(),
    patchMember: vi.fn(),
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

function renderPanel() {
  const onRelocate = vi.fn(async (machineId: string) => {
    await relocateMember("mira", machineId);
  });
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember()}
        onBack={vi.fn()}
        onRelocate={onRelocate}
      />
    </I18nProvider>,
  );
  return { ...utils, onRelocate };
}

beforeEach(() => {
  listMachines.mockClear();
  relocateMember.mockClear();
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MemberDetailPanel — 改機器 relocate", () => {
  it("renders the 編輯 button next to the 機器 label (label parity with 模型)", async () => {
    const { getByTestId } = renderPanel();
    await waitFor(() => {
      expect(getByTestId("mp-relocate")).toBeTruthy();
    });
    // The 機器 control reads 「編輯」 — the SAME i18n key the 模型 edit button uses
    // (owner: 「一樣叫做編輯就好了」). Behaviour is still relocate-only (the picker).
    expect(getByTestId("mp-relocate").textContent).toContain(zh.settings.edit);
  });

  it("with TWO online machines opens the picker and relocates to the chosen machine", async () => {
    const { getByTestId, onRelocate } = renderPanel();

    // Wait for useMachines to load the two online machines (button enabled).
    await waitFor(() => {
      expect(
        (getByTestId("mp-relocate") as HTMLButtonElement).disabled,
      ).toBe(false);
    });

    fireEvent.click(getByTestId("mp-relocate"));
    // 2+ online → the picker opens (never an auto-relocate).
    const select = getByTestId("machine-picker-select") as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(getByTestId("machine-picker-confirm"));

    await waitFor(() => {
      expect(onRelocate).toHaveBeenCalledWith("mach-b");
    });
    // Placement-only: it routed through relocateMember, never activateMember.
    expect(relocateMember).toHaveBeenCalledWith("mira", "mach-b");
  });
});
