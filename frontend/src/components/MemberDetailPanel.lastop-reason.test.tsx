// MemberDetailPanel · 最近操作 failure REASON (成員啟動失敗原因全鏈可見).
//
// Locked here:
//   1. A FAILED op whose receipt carried a structured reason renders it as an
//      always-visible one-line summary (no expand needed — the 2026-07-13
//      incident showed a bare "✕ 啟動 失敗" tells the owner nothing).
//   2. An old record WITHOUT a reason renders status-only, exactly as before
//      (no fabricated cause), while the collapsible log stays available.
//   3. A SUCCESSFUL op never renders a reason line.

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { vi } from "vitest";
import { I18nProvider } from "../i18n";
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

const REASON =
  'session_already_exists: tmux session "member-mira" is already live (clobber-guard refused to stomp it)';

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
    lastOp: "start",
    lastOpOk: false,
    lastOpLog: "spawn refused",
    lastOpReason: REASON,
    lastOpAt: 1_752_400_000,
    unreadCount: 0,
    ...over,
  };
}

function renderPanel(member: Member) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={member} onBack={() => {}} />
    </I18nProvider>,
  );
}

describe("MemberDetailPanel 最近操作 failure reason", () => {
  it("shows the structured reason as an always-visible one-line summary", () => {
    const { getByTestId } = renderPanel(mkMember());
    expect(getByTestId("mp-lastop-reason").textContent).toBe(REASON);
  });

  it("renders status-only for an old record without a reason (never fabricated)", () => {
    const { queryByTestId, container } = renderPanel(
      mkMember({ lastOpReason: "" }),
    );
    expect(queryByTestId("mp-lastop-reason")).toBeNull();
    // The failure block itself still renders (status + collapsible log).
    expect(container.querySelector(".mp-lastop__head--fail")).not.toBeNull();
    expect(container.querySelector(".mp-lastop__toggle")).not.toBeNull();
  });

  it("renders no reason line on a successful op", () => {
    const { queryByTestId } = renderPanel(
      mkMember({ lastOpOk: true, lastOpLog: "", lastOpReason: "" }),
    );
    expect(queryByTestId("mp-lastop-reason")).toBeNull();
  });
});
