// AgentDetailPanel · the four "changed, not applied yet" hints (T-7f28).
//
// The owner asked for this on a condition:「我是希望可設定的部分，在正在更換的
// 過程時，都可以標注預期要換成什麼，但又不想要畫面太雜亂」— so there are two
// halves to pin, and the SECOND is the one a careless change breaks:
//
//   1. a KNOWN reported value that differs from the configured one shows a hint;
//   2. everything else renders NOTHING — no node, no placeholder, no spacer.
//
// (2) includes the case that motivated the whole ticket: a report that never
// arrived. An unknown value must not be dressed up as either state — neither
// "already applied" (the old behaviour, which substituted the configured value)
// nor "pending" (which would guess).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member, MachineView } from "../types";

const machine = (id: string, displayName: string): MachineView => ({
  machineId: id,
  displayName,
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
  cutoverEffect: null,
});
const listMachines = vi.fn<() => Promise<MachineView[]>>(() =>
  Promise.resolve([machine("mach-a", "Machine A"), machine("mach-b", "Machine B")]),
);

vi.mock("../api", () => ({
  api: {
    listMachines: () => listMachines(),
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

/** A member with every configurable cell CONFIGURED and REPORTED alike — the
 * steady state, where the panel must look exactly as it did before this
 * feature existed. Each test perturbs one cell. */
function mkSettled(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    runtime: "claude",
    actualRuntime: "claude",
    model: "opus",
    actualModel: "opus",
    effort: "medium",
    actualEffort: "medium",
    kind: "assistant",
    desiredMachineId: "mach-a",
    machine: "mach-a",
    actualMachine: "mach-a",
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

async function renderPanel(over: Partial<Member> = {}) {
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel member={mkSettled(over)} onBack={vi.fn()} />
    </I18nProvider>,
  );
  await waitFor(() => expect(listMachines).toHaveBeenCalled());
  return utils;
}

const CELLS = ["runtime", "model", "effort", "machine"] as const;

beforeEach(() => {
  listMachines.mockClear();
});

describe("AgentDetailPanel · pending-change hints", () => {
  it("shows nothing on any cell when everything reported matches what is configured", async () => {
    const { queryByTestId } = await renderPanel();
    for (const cell of CELLS) {
      expect(queryByTestId(`mp-${cell}-pending`)).toBeNull();
    }
  });

  it("keeps the info card DOM byte-identical to the pre-feature panel when nothing is pending", async () => {
    // The strongest form of 「不想要畫面太雜亂」: not "no visible text", but no
    // NODE. An empty <div> passes a text assertion and still adds a row of
    // margin to every settled panel in the fleet — which is the complaint the
    // owner would actually make.
    const { container } = await renderPanel();
    // Pending hints are the only BLOCK-level hints in the info card — the two
    // that legitimately live there (the model's provenance tag, the effort's
    // raw value) are inline <span>s inside a value row.
    const card = container.querySelector(".mp-info2")!;
    expect(card.querySelectorAll("div.mp-field__hint").length).toBe(0);
    expect(card.querySelectorAll("span.mp-field__hint").length).toBeGreaterThan(0);
  });

  it.each([
    ["runtime", { runtime: "codex" } as Partial<Member>, "Codex"],
    ["model", { model: "claude-opus-5" } as Partial<Member>, "claude-opus-5"],
    ["effort", { effort: "high" } as Partial<Member>, "high"],
  ])(
    "marks %s with the value it is changing TO once the setting diverges",
    async (cell, patch, expected) => {
      const { getByTestId, queryByTestId } = await renderPanel(patch);
      expect(getByTestId(`mp-${cell}-pending`).textContent).toContain(expected);
      // …and only that cell. A hint that leaked onto its neighbours would be
      // exactly the clutter the owner asked us to avoid.
      for (const other of CELLS.filter((c) => c !== cell)) {
        expect(queryByTestId(`mp-${other}-pending`)).toBeNull();
      }
    },
  );

  it("marks the machine cell with the pin's display name, not its raw id", async () => {
    const { getByTestId } = await renderPanel({ desiredMachineId: "mach-b" });
    const hint = getByTestId("mp-machine-pending").textContent ?? "";
    expect(hint).toContain("Machine B");
    expect(hint).not.toContain("mach-b");
  });

  it.each(CELLS)(
    "stays silent on %s when nothing has ever been reported for it",
    async (cell) => {
      // 🔴 The ticket's red line. Before T-7f28 an unreported runtime rendered
      // the CONFIGURED value in the readout, so changing the setting flipped
      // the panel instantly and a change that had not taken effect was
      // indistinguishable from one that had. Blank in, blank out — and no
      // pending hint either, because we do not know that anything is pending.
      const blanked: Partial<Member> = {
        runtime: { actualRuntime: "" as const },
        model: { actualModel: "" },
        effort: { actualEffort: "" },
        machine: { machine: null, actualMachine: "" },
      }[cell];
      const { queryByTestId, getByTestId } = await renderPanel(blanked);
      expect(queryByTestId(`mp-${cell}-pending`)).toBeNull();
      if (cell !== "machine") {
        expect(getByTestId(`mp-${cell}-value`).textContent).toBe("—");
      }
    },
  );

  it("reads the runtime cell off the report, never off the setting", async () => {
    // The one cell that was actively lying: the readout served the configured
    // runtime, so it changed the instant the owner saved.
    const { getByTestId } = await renderPanel({
      runtime: "codex",
      actualRuntime: "claude",
    });
    expect(getByTestId("mp-runtime-value").textContent).toBe("Claude Code");
    expect(getByTestId("mp-runtime-pending").textContent).toContain("Codex");
  });

  it("keeps a pending relocation visible after the member goes offline", async () => {
    // The live `machine` blanks when the session ends; the durable last landing
    // is what keeps the comparison possible. Without it, going offline would
    // erase the evidence that the move is still outstanding.
    const { getByTestId } = await renderPanel({
      status: "offline",
      lifecycle: "offline",
      machine: null,
      actualMachine: "mach-a",
      desiredMachineId: "mach-b",
    });
    expect(getByTestId("mp-machine-pending").textContent).toContain("Machine B");
    expect(getByTestId("mp-machine").textContent).toBe("—");
  });
});

describe("AgentDetailPanel · wind-down note", () => {
  it("says the change is being applied, with a ceiling, instead of when the last refocus was", async () => {
    const deadline = 1_800_000_000;
    const { getByTestId, queryByTestId } = await renderPanel({
      refocusSince: deadline - 120,
      refocusOp: "runtime/model",
      refocusDeadline: deadline,
      model: "claude-opus-5",
    });
    const note = getByTestId("mp-wind-down-note").textContent ?? "";
    expect(note).toContain("正在收尾以套用你的改動");
    expect(note).toContain(new Date(deadline * 1000).toLocaleTimeString());
    // The history line it replaces must be gone — two lines saying different
    // things about the same window is worse than either alone.
    expect(queryByTestId("mp-refocus-since")).toBeNull();
    expect(note).not.toContain("上次重新聚焦");
  });

  it("keeps the plain history line for a handover the owner did not cause", async () => {
    // A context-pressure handover is not applying anything of the owner's, so
    // announcing "applying your change" there would be a lie.
    const { queryByTestId } = await renderPanel({
      refocusSince: 1_800_000_000,
      refocusOp: "context_high",
      refocusDeadline: 1_800_000_120,
    });
    expect(queryByTestId("mp-wind-down-note")).toBeNull();
  });

  it("says nothing when no wind-down is in flight", async () => {
    const { queryByTestId } = await renderPanel();
    expect(queryByTestId("mp-wind-down-note")).toBeNull();
  });
});

describe("AgentDetailPanel · the removed in-place editor (T-7f28)", () => {
  it("offers no edit entry point on the model/effort cell", async () => {
    // Kyle's condition for deleting dead code: pin the ABSENCE, do not just
    // remove the code. The editor hung off an optional prop no caller ever
    // passed, so nothing failed when it rotted — and nothing would fail if a
    // future change quietly re-grew it beside the settings dialog that is the
    // real editor. Two disagreeing ways to change one setting on one screen is
    // the state this guards against.
    const { queryByTestId } = await renderPanel();
    for (const id of [
      "mp-model-effort-edit",
      "mp-model-effort-editor",
      "mp-model-effort-save",
      "mp-model-effort-configured",
    ]) {
      expect(queryByTestId(id)).toBeNull();
    }
  });
});
