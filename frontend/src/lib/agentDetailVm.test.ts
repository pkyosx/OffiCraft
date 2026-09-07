import { describe, it, expect } from "vitest";
import {
  buildAgentDetailVm,
  machineOptions,
  totalCostOf,
  type AgentDetailVmInput,
} from "./agentDetailVm";

function mkMachine(machineId: string, online: boolean, displayName?: string) {
  return { machineId, displayName: displayName ?? `名稱-${machineId}`, online };
}

const offlineLabel = (name: string) => `${name}（離線）`;

describe("machineOptions", () => {
  it("offers every online machine and nothing else when the pin is already online", () => {
    const options = machineOptions(
      [mkMachine("m-1", true), mkMachine("m-2", true), mkMachine("m-3", false)],
      "m-1",
      offlineLabel,
    );
    expect(options).toEqual([
      { machineId: "m-1", label: "名稱-m-1", offline: false },
      { machineId: "m-2", label: "名稱-m-2", offline: false },
    ]);
  });

  it("keeps an offline PIN in the list, labelled and flagged offline", () => {
    // Dropping it would silently move an agent the owner deliberately parked,
    // and the <select> would sit on a value matching no option.
    const options = machineOptions(
      [mkMachine("m-1", true), mkMachine("m-9", false)],
      "m-9",
      offlineLabel,
    );
    expect(options).toEqual([
      { machineId: "m-1", label: "名稱-m-1", offline: false },
      { machineId: "m-9", label: "名稱-m-9（離線）", offline: true },
    ]);
  });

  it("falls back to the raw id when the pin is not in the registry at all", () => {
    expect(machineOptions([], "m-gone", offlineLabel)).toEqual([
      { machineId: "m-gone", label: "m-gone（離線）", offline: true },
    ]);
  });

  it("adds no pinned entry when there is no pin", () => {
    expect(machineOptions([mkMachine("m-1", false)], "", offlineLabel)).toEqual(
      [],
    );
  });
});

describe("totalCostOf", () => {
  it("is null only when BOTH sources are absent", () => {
    expect(totalCostOf(null, null)).toBeNull();
    expect(totalCostOf(undefined, undefined)).toBeNull();
  });

  it("counts one present source, reading the missing half as no cost yet", () => {
    expect(totalCostOf(1.5, null)).toBe(1.5);
    expect(totalCostOf(null, 2.25)).toBe(2.25);
    expect(totalCostOf(1.5, 2.25)).toBe(3.75);
  });

  it("keeps a real zero rather than collapsing it to the dash", () => {
    expect(totalCostOf(0, null)).toBe(0);
  });
});

function mkInput(over: Partial<AgentDetailVmInput> = {}): AgentDetailVmInput {
  return {
    testIdPrefix: "mp",
    online: true,
    awake: true,
    runtime: "claude",
    actualRuntime: "claude",
    actualModel: "Opus 4.6",
    actualEffort: "high",
    pending: {},
    machineText: "名稱-m-1",
    accountText: "team-a@corp",
    contextPct: 42,
    compactionCount: 1,
    liveCost: 1,
    bankedCost: 2,
    refocusSince: null,
    refocusOp: undefined,
    refocusDeadline: null,
    refocusSubmittedNote: "已送出",
    refocusSinceLabel: (x) => x,
    lastOp: "start",
    lastOpStartText: "開機",
    lastOpStopText: "停止",
    lastOpOk: true,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: 1,
    terminalAttachCommand: "tmux -L officraft attach -t member-m-1",
    terminalHint: "hint",
    terminalUnavailable: "no command",
    ...over,
  };
}

describe("buildAgentDetailVm", () => {
  it("reads out the REPORTED model and effort, and says so", () => {
    const vm = buildAgentDetailVm(mkInput());
    expect(vm.model).toBe("Opus 4.6");
    expect(vm.effort).toBe("high");
    // 🔴 Always true, on BOTH kinds (owner ruling `rc-b8d219446b13`, option
    // [0]). Without the tag the row is indistinguishable from the configured
    // value the settings dialog edits.
    expect(vm.modelIsReported).toBe(true);
  });

  it("blanks model and effort when the agent is not awake", () => {
    // Reported columns are durable and outlive the session that wrote them, so
    // ungated they would keep stating a model nothing is running (owner ruling
    // `rc-8a129bc3a188`, option [1]).
    const vm = buildAgentDetailVm(mkInput({ awake: false }));
    expect(vm.model).toBe("");
    expect(vm.effort).toBe("");
  });

  it("blanks model and effort when awake but nothing was ever reported", () => {
    const vm = buildAgentDetailVm(
      mkInput({ actualModel: undefined, actualEffort: undefined }),
    );
    expect(vm.model).toBe("");
    expect(vm.effort).toBe("");
  });

  it("never substitutes the configured runtime for an unreported one", () => {
    const vm = buildAgentDetailVm(
      mkInput({ runtime: "codex", actualRuntime: "" }),
    );
    expect(vm.runtime).toBe("codex");
    expect(vm.reportedRuntime).toBe("");
  });

  it("floors an unset runtime to claude", () => {
    expect(buildAgentDetailVm(mkInput({ runtime: undefined })).runtime).toBe(
      "claude",
    );
  });

  it.each([
    ["start", "開機"],
    ["worker_start", "開機"],
    ["stop", "停止"],
    ["worker_stop", "停止"],
  ])("renders %s as its verb", (lastOp, verb) => {
    expect(buildAgentDetailVm(mkInput({ lastOp })).lastOpVerb).toBe(verb);
  });

  it("shows an unrecognised op verbatim rather than guessing a verb", () => {
    const vm = buildAgentDetailVm(mkInput({ lastOp: "refocus" }));
    expect(vm.lastOp).toBe("refocus");
    expect(vm.lastOpVerb).toBe("refocus");
  });

  it("reads a missing op as no op at all", () => {
    const vm = buildAgentDetailVm(mkInput({ lastOp: undefined }));
    expect(vm.lastOp).toBe("");
    expect(vm.lastOpVerb).toBe("");
  });

  it("normalises every honest-unknown to the shape the panel dashes", () => {
    const vm = buildAgentDetailVm(
      mkInput({
        contextPct: undefined,
        compactionCount: undefined,
        liveCost: undefined,
        bankedCost: undefined,
        refocusSince: undefined,
        lastOpOk: undefined,
        lastOpLog: undefined,
        lastOpReason: undefined,
        lastOpAt: undefined,
      }),
    );
    expect(vm.contextPct).toBeNull();
    expect(vm.compactionCount).toBeNull();
    expect(vm.cost).toBeNull();
    expect(vm.refocusSince).toBeNull();
    expect(vm.lastOpOk).toBeNull();
    expect(vm.lastOpLog).toBe("");
    expect(vm.lastOpReason).toBe("");
    expect(vm.lastOpAt).toBeNull();
  });

  it("leaves onRefocus undefined when the wrapper wired no handler", () => {
    expect(buildAgentDetailVm(mkInput()).onRefocus).toBeUndefined();
  });

  it("wraps a wired onRefocus so the panel always gets a promise", async () => {
    let calls = 0;
    const vm = buildAgentDetailVm(
      mkInput({
        onRefocus: () => {
          calls += 1;
          return undefined;
        },
      }),
    );
    await vm.onRefocus!();
    expect(calls).toBe(1);
  });
});
