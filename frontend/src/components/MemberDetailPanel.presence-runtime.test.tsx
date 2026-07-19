// MemberDetailPanel · presence-gated runtime identity (T-2860).
//
// Owner contract: 機器 + Claude Account are RUNTIME facts — they exist only while
// the agent is actually up. When the member is NOT awakened (presence outside
// online/waking) both cells must read a bare dash, never a desired_machine
// residual nor a stale/banked monitoring-session value that leaked through
// joinSessionRuntime. Once awakened, the real running machine + its bound
// account show through. Locked here as two scenarios.

import { describe, it, expect, vi } from "vitest";
import { render, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
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

// A member carrying runtime identity (as joinSessionRuntime would have set it):
// machine + account are populated regardless of presence — the panel is what
// decides whether to surface them.
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
    desiredMachineId: "seth-m5",
    machine: "seth-m5",
    account: "eva-claude",
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

const dash = zh.mp.dash;

function renderPanel(member: Member) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={member} onBack={() => {}} />
    </I18nProvider>
  );
}

// The 機器 / Claude Account cell — the mp-field carrying both stacked values.
function runtimeIdentityCell(container: HTMLElement): HTMLElement {
  const label = within(container).getByText(zh.mp.claudeAccount);
  return label.closest(".mp-field") as HTMLElement;
}

describe("MemberDetailPanel · presence-gated machine + account", () => {
  it("not awakened (offline) shows dash for both machine and Claude Account", async () => {
    const { container } = renderPanel(
      mkMember({ status: "offline", lifecycle: "offline" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    // No stale residual leaks: neither the desired/observed machine id nor the
    // banked session account is rendered.
    expect(within(cell).queryByText("seth-m5")).toBeNull();
    expect(within(cell).queryByText("eva-claude")).toBeNull();
    // Both values read the honest dash.
    expect(within(cell).getAllByText(dash)).toHaveLength(2);
  });

  it("stopped (post-run) still shows dash — banked telemetry must not linger", async () => {
    const { container } = renderPanel(
      mkMember({ status: "offline", lifecycle: "stopped" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    expect(within(cell).queryByText("seth-m5")).toBeNull();
    expect(within(cell).queryByText("eva-claude")).toBeNull();
    expect(within(cell).getAllByText(dash)).toHaveLength(2);
  });

  it("awakened (online) shows the real running machine and its bound account", async () => {
    const { container } = renderPanel(
      mkMember({ status: "online", lifecycle: "online" })
    );
    const cell = await waitFor(() => runtimeIdentityCell(container));

    expect(within(cell).getByText("seth-m5")).toBeTruthy();
    expect(within(cell).getByText("eva-claude")).toBeTruthy();
  });
});
