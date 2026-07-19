// MemberDetailPanel · wake-click instant feedback.
//
// Locked here: clicking 喚醒 flips the panel into the "waking" visual state
// IMMEDIATELY (before server presence catches up) — the wake button itself
// swaps to a disabled "喚醒中…" (the Monitor machine table's install-busy
// presentation; double-click guard included) — and a server lifecycle flip
// (waking) clears the local bridge; a rejected activate reverts to offline.

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

const wakeLabel = zh.lifecycle.action.spawn;
const pendingLabel = zh.mp.wakePendingNote;

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
  it("clicking wake immediately swaps the button to a disabled 喚醒中…", async () => {
    let resolveActivate!: () => void;
    const onActivate = vi.fn(
      () => new Promise<void>((res) => (resolveActivate = res))
    );
    const utils = renderPanel(onActivate);

    // Wait for the machine registry (single online machine → auto-use, no picker).
    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });

    fireEvent.click(wakeBtn);
    expect(onActivate).toHaveBeenCalledWith("mac-1");
    // Instant waking state: the wake button ITSELF carries the in-progress
    // label (machine-install style) and disables (no double fire).
    const pendingBtn = utils.getByText(pendingLabel).closest("button")!;
    expect(pendingBtn.disabled).toBe(true);
    fireEvent.click(pendingBtn);
    expect(onActivate).toHaveBeenCalledTimes(1);
    resolveActivate();
  });

  it("a rejected activate reverts to the offline visual (retry possible)", async () => {
    const onActivate = vi.fn(() => Promise.reject(new Error("boom")));
    const utils = renderPanel(onActivate);
    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });
    fireEvent.click(wakeBtn);
    utils.getByText(pendingLabel);
    await waitFor(() => expect(utils.queryByText(pendingLabel)).toBeNull());
    expect(utils.getByText(wakeLabel).closest("button")!.disabled).toBe(false);
  });

  it("the server lifecycle flip to waking clears the local pending bridge", async () => {
    const onActivate = vi.fn(async () => {});
    const utils = render(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember()}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>
    );
    const wakeBtn = await waitFor(() => {
      const btn = utils.getByText(wakeLabel).closest("button")!;
      expect(btn.disabled).toBe(false);
      return btn;
    });
    fireEvent.click(wakeBtn);
    utils.getByText(pendingLabel);

    // Server presence caught up: the refetched member arrives as waking.
    utils.rerender(
      <I18nProvider>
        <MemberDetailPanel
          member={mkMember({ lifecycle: "waking" })}
          onBack={() => {}}
          onActivate={onActivate}
        />
      </I18nProvider>
    );
    // The local bridge clears: the spawn button is back to its plain rescue
    // label (server-driven waking keeps it enabled as the force-revive path).
    await waitFor(() => expect(utils.queryByText(pendingLabel)).toBeNull());
    expect(utils.getByText(wakeLabel).closest("button")).not.toBeNull();
  });
});
