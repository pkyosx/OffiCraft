// MemberDetailPanel · wake-click instant feedback.
//
// Locked here: confirming 喚醒 flips the panel into the "waking" row
// IMMEDIATELY (before server presence catches up) — 更改 ＋ 停止, the row both
// kinds show while waking, with no second 喚醒 to double-fire — a rejected
// activate reverts to the offline row, and once the server lifecycle has
// caught up the local bridge is gone, so a later offline reads as offline.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () =>
      Promise.resolve([
        {
          machineId: "mac-1",
          displayName: "Mac 1",
          online: true,
          isSelf: true,
        },
      ]),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    updateWebhook: () =>
      Promise.resolve({ endpointId: "", purpose: "", status: "enabled", createdTs: 0, token: "" }),
    deleteWebhook: () => Promise.resolve(),
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
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

const wakeLabel = zh.lifecycle.action.spawn;

function actionRow(container: HTMLElement) {
  return Array.from(
    container.querySelectorAll(".mp-identity__buttons button"),
  ).map((b) => [b.getAttribute("data-testid"), b.textContent]);
}

async function confirmWakeSettings() {
  const confirm = document.querySelector<HTMLButtonElement>(".machine-picker__actions .btn--accent")!;
  await waitFor(() => expect(confirm.disabled).toBe(false));
  fireEvent.click(confirm);
}

function renderPanel(onActivate: (machineId?: string) => void | Promise<void>) {
  return render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember()}
        onBack={() => {}}
        onActivate={onActivate}
      />
    </I18nProvider>
  );
}

describe("MemberDetailPanel · wake-pending instant feedback", () => {
  it("confirming wake immediately shows the waking row 更改 ＋ 停止, with no second 喚醒", async () => {
    let resolveActivate!: () => void;
    const onActivate = vi.fn(
      () => new Promise<void>((res) => (resolveActivate = res))
    );
    const utils = renderPanel(onActivate);

    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });

    fireEvent.click(wakeBtn);
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledWith("mac-1"));
    await waitFor(() =>
      expect(actionRow(utils.container)).toEqual([
        ["mp-change", "更改"],
        ["member-action-stop", "停止"],
      ]),
    );
    expect(onActivate).toHaveBeenCalledTimes(1);
    resolveActivate();
  });

  it("a rejected activate reverts to the offline row (retry possible)", async () => {
    const onActivate = vi.fn(() => Promise.reject(new Error("boom")));
    const utils = renderPanel(onActivate);
    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });
    fireEvent.click(wakeBtn);
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(actionRow(utils.container)).toEqual([["member-action-spawn", "喚醒"]]),
    );
    expect(utils.getByText(wakeLabel).closest("button")!.disabled).toBe(false);
  });

  it("once the server lifecycle has flipped to waking, a later offline shows 喚醒 again", async () => {
    const onActivate = vi.fn(async () => {});
    const panel = (over: Partial<Member>) => (
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember(over)}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>
    );
    const utils = render(panel({}));
    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });
    fireEvent.click(wakeBtn);
    await confirmWakeSettings();
    await waitFor(() => expect(onActivate).toHaveBeenCalledTimes(1));

    utils.rerender(panel({ status: "waking", lifecycle: "waking" }));
    await waitFor(() =>
      expect(actionRow(utils.container)).toEqual([
        ["mp-change", "更改"],
        ["member-action-stop", "停止"],
      ]),
    );

    utils.rerender(panel({}));
    await waitFor(() =>
      expect(actionRow(utils.container)).toEqual([["member-action-spawn", "喚醒"]]),
    );
  });
});
