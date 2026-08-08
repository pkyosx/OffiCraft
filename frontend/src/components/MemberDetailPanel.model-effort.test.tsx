// MemberDetailPanel · model/effort 快選編輯器 + 名字 inline-edit (owner M2).
//
// Locked here:
//   1. The in-place editor is the SHARED <ModelEffortEditor>: model = quick
//      chips (fable/opus/sonnet/haiku) + a free custom string input (blank ⇒
//      server default), effort = a low/medium/high/max dropdown. A chip click fills
//      the input; a typed string overrides the chip; 儲存 PATCHes the member
//      with the drafted launch intents.
//   2. The member NAME is editable in the panel via the shared pencil
//      InlineEdit → onRename (patchMember at the call site).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

const patchMember = vi.fn(async (_id: string, patch: object) => ({
  ...mkMember(),
  ...(patch as Partial<Member>),
}));

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([{ machineId: "mach-1", displayName: "Mac", online: true }]),
    patchMember: (id: string, patch: object) => patchMember(id, patch),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    updateWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    deleteWebhook: () => Promise.resolve(),
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
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

function renderPanel(over: Partial<Member> = {}) {
  const onRename = vi.fn();
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember(over)}
        onBack={() => {}}
        onRename={onRename}
      />
    </I18nProvider>
  );
  return { ...utils, onRename };
}

/** Open the unified settings dialog. The wake/change entry is gated on the
 * machine registry (0 online machines => dead affordance) and that registry
 * loads asynchronously - clicking before it lands hits a disabled button and
 * opens nothing. */
async function openSettings(utils: { getByTestId: (id: string) => HTMLElement }) {
  await waitFor(() =>
    expect(
      (utils.getByTestId("member-action-spawn") as HTMLButtonElement).disabled,
    ).toBe(false),
  );
  fireEvent.click(utils.getByTestId("member-action-spawn"));
}

async function confirmSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(".machine-picker__actions .btn--accent")!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

beforeEach(() => {
  patchMember.mockClear();
});

describe("MemberDetailPanel · model/effort quick-pick editor", () => {
  it("opens the shared chips editor; a chip fills the free input; save PATCHes", async () => {
    // A reported pair distinct from the configured one: the editor must seed
    // from the SETTING (T-e12c), never from what the session happens to run.
    const utils = renderPanel({ actualModel: "claude-sonnet-4.9", actualEffort: "low" });
    await openSettings(utils);

    // The current model pre-fills the free input; the matching chip is active.
    const input = utils.getByTestId("me-model-input") as HTMLInputElement;
    expect(input.value).toBe("opus");
    expect(
      (utils.getByTestId("me-effort-select") as HTMLSelectElement).value
    ).toBe("medium");

    // Quick chip → fills the input (the input stays the single truth).
    fireEvent.click(utils.getByTestId("me-model-chip-sonnet"));
    expect(
      (utils.getByTestId("me-model-input") as HTMLInputElement).value
    ).toBe("sonnet");

    // Effort is a plain dropdown (closed low/medium/high/max vocabulary).
    fireEvent.change(utils.getByTestId("me-effort-select"), {
      target: { value: "high" },
    });

    await confirmSettings();
    await waitFor(() =>
      expect(patchMember).toHaveBeenCalledWith("mira", {
        runtime: "claude",
        model: "sonnet",
        effort: "high",
      })
    );
  });

  it("switching provider clears Claude's model pick and offers Codex suggestions plus an override", async () => {
    const utils = renderPanel();
    await openSettings(utils);
    fireEvent.change(utils.getByTestId("me-runtime-select"), {
      target: { value: "codex" },
    });
    const modelInput = utils.getByTestId("me-codex-model-select-input") as HTMLInputElement;
    expect(modelInput.value).toBe("");
    expect(utils.queryByTestId("me-model-chip-opus")).toBeNull();
    fireEvent.click(utils.getByTestId("me-codex-model-select-chip-gpt-5.6-terra"));
    fireEvent.click(utils.getByTestId("me-codex-model-select-chip-gpt-5.6-luna"));
    await confirmSettings();
    await waitFor(() =>
      expect(patchMember).toHaveBeenCalledWith("mira", {
        runtime: "codex",
        model: "gpt-5.6-luna",
        effort: "medium",
      })
    );
  });

  it("a hand-typed custom string overrides the chips; blank means default", async () => {
    const utils = renderPanel();
    await openSettings(utils);
    fireEvent.click(utils.getByTestId("me-model-chip-fable"));
    fireEvent.change(utils.getByTestId("me-model-input"), {
      target: { value: "claude-x-preview" },
    });
    await confirmSettings();
    await waitFor(() =>
      expect(patchMember).toHaveBeenCalledWith("mira", {
        runtime: "claude",
        model: "claude-x-preview",
        effort: "medium",
      })
    );

    // Blank input → "" (server/CLI default), never a fabricated pick.
    await openSettings(utils);
    fireEvent.change(utils.getByTestId("me-model-input"), {
      target: { value: "" },
    });
    await confirmSettings();
    await waitFor(() =>
      expect(patchMember).toHaveBeenLastCalledWith("mira", {
        runtime: "claude",
        model: "",
        effort: "medium",
      })
    );
  });
});

// T-e12c — owner ruling 2026-07-31:「成員面板以及監控台，一定要顯示回報回來的
// 狀態，不能顯示設定值」. The info card states what the member is RUNNING; the
// settings dialog above stays the one place the launch intent is shown and
// written. Both halves are pinned here so neither can quietly absorb the other.
describe("MemberDetailPanel · 投入度 readout vs configured launch intent", () => {
  it("reads out the REPORTED effort, not the configured one", () => {
    const utils = renderPanel({
      status: "online",
      lifecycle: "online",
      effort: "medium",
      actualEffort: "high",
    });
    const readout = utils.getByTestId("mp-effort-value").textContent ?? "";
    expect(readout).toContain("high");
    expect(readout).not.toContain("medium");
  });

  it("blanks the readout when nothing was reported, never the configured value", () => {
    const utils = renderPanel({
      status: "online",
      lifecycle: "online",
      effort: "medium",
    });
    expect(utils.getByTestId("mp-effort-value").textContent).toBe("—");
  });
});

describe("MemberDetailPanel · 名字 inline-edit", () => {
  it("renames through the pencil InlineEdit and commits via onRename", () => {
    const utils = renderPanel();
    fireEvent.click(utils.getByLabelText(zh.mp.rename));
    const input = utils.getByLabelText(zh.mp.rename, {
      selector: "input",
    }) as HTMLInputElement;
    expect(input.value).toBe("Mira");
    fireEvent.change(input, { target: { value: "Mira Prime" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(utils.onRename).toHaveBeenCalledWith("Mira Prime");
  });
});
