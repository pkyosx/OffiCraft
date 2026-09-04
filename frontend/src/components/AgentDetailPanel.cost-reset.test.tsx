// AgentDetailPanel · 成本歸零 (T-53, owner ruling rc-7dea0deefa63 option 0
// 「最小、不可逆」).
//
// The button destroys a figure nothing else in the system holds — there is no
// per-charge ledger and no undo route — so what is pinned here is not "the
// button calls the API" but the three things that make it safe to have at all:
//
//   1. the click alone NEVER fires it; the confirm is the whole mechanism;
//   2. it offers nothing to destroy when there is nothing measured;
//   3. a failure SAYS SO. Both success and silent failure leave the cell
//      reading the dash, so the owner cannot tell them apart from the value.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member, MachineView } from "../types";

const listMachines = vi.fn<() => Promise<MachineView[]>>(() => Promise.resolve([]));

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

function mkMember(over: Partial<Member> = {}): Member {
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
    desiredMachineId: "",
    machine: "",
    actualMachine: "",
    account: null,
    contextPct: null,
    estimatedCost: 1.5,
    bankedCost: 4,
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

async function renderPanel(
  onResetCost: (() => Promise<void>) | undefined,
  over: Partial<Member> = {},
) {
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember(over)}
        onBack={vi.fn()}
        onResetCost={onResetCost}
      />
    </I18nProvider>,
  );
  await waitFor(() => expect(listMachines).toHaveBeenCalled());
  return utils;
}

beforeEach(() => {
  listMachines.mockClear();
});

describe("AgentDetailPanel · 成本歸零", () => {
  it("does not reset on the click itself — only after the confirm", async () => {
    const onResetCost = vi.fn(async () => {});
    const { findByTestId } = await renderPanel(onResetCost);

    fireEvent.click(await findByTestId("mp-cost-reset"));
    // The whole safety mechanism: the irreversible call has NOT happened yet.
    expect(onResetCost).not.toHaveBeenCalled();
    await findByTestId("mp-cost-reset-confirm");

    fireEvent.click(await findByTestId("mp-cost-reset-confirm-btn"));
    await waitFor(() => expect(onResetCost).toHaveBeenCalledTimes(1));
  });

  it("cancelling the confirm resets nothing", async () => {
    const onResetCost = vi.fn(async () => {});
    const { findByTestId, queryByTestId, getByText } = await renderPanel(onResetCost);

    fireEvent.click(await findByTestId("mp-cost-reset"));
    await findByTestId("mp-cost-reset-confirm");
    fireEvent.click(getByText("取消"));

    await waitFor(() => expect(queryByTestId("mp-cost-reset-confirm")).toBeNull());
    expect(onResetCost).not.toHaveBeenCalled();
  });

  it("offers nothing to destroy when nothing has been measured", async () => {
    const onResetCost = vi.fn(async () => {});
    const { findByTestId } = await renderPanel(onResetCost, {
      estimatedCost: null,
      bankedCost: null,
    });

    // Same condition the cell renders as the dash, so the button and the value
    // can never disagree about whether there is anything there.
    const btn = (await findByTestId("mp-cost-reset")) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
  });

  it("keeps the confirm open and says so when the reset fails", async () => {
    const onResetCost = vi.fn(async () => {
      throw new Error("boom");
    });
    const { findByTestId, getByText } = await renderPanel(onResetCost);

    fireEvent.click(await findByTestId("mp-cost-reset"));
    fireEvent.click(await findByTestId("mp-cost-reset-confirm-btn"));

    // A silent failure and a success both leave the cell reading the dash —
    // the message is the only thing that tells them apart.
    await waitFor(() => expect(getByText("歸零失敗，數字沒有被清掉。")).toBeTruthy());
    await findByTestId("mp-cost-reset-confirm");
  });

  it("renders no button at all on a surface that does not offer the action", async () => {
    const { queryByTestId } = await renderPanel(undefined);
    expect(queryByTestId("mp-cost-reset")).toBeNull();
  });
});
