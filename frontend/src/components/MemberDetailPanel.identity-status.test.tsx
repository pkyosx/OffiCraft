// MemberDetailPanel · the identity card's status line (T-dfae).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getBootstrap: () =>
      Promise.resolve({
        role: "assistant",
        name: "",
        taskType: "",
        context: "",
      }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    createWebhook: () =>
      Promise.resolve({
        endpointId: "",
        purpose: "",
        status: "enabled",
        createdTs: 0,
        token: "",
      }),
    updateWebhook: () =>
      Promise.resolve({
        endpointId: "",
        purpose: "",
        status: "enabled",
        createdTs: 0,
        token: "",
      }),
    deleteWebhook: () => Promise.resolve(),
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

function renderPanel(over: Partial<Member> = {}) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={mkMember(over)} onBack={() => {}} />
    </I18nProvider>
  );
}

describe("MemberDetailPanel identity card status line (T-dfae)", () => {
  beforeEach(() => {
    window.location.hash = "";
  });

  it("shows the presence badge with the member's own role", () => {
    // A role other than the default proves it is read off the member, not
    // hard-coded.
    const { container } = renderPanel({ id: "rex", role: "reviewer" });
    const statusLine = container.querySelector(".mp-identity__status")!;
    expect(statusLine.querySelector(".presence-badge")).not.toBeNull();
    expect(statusLine.textContent).toContain("reviewer");
  });
});
