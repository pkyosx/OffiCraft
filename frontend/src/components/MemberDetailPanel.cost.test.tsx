// MemberDetailPanel · 花費 = live + banked (T-14 項目 2).
//
// The member and the worker panel now share one `totalCostOf` in agentDetailVm.
// Before this file every member fixture carried `bankedCost: null`, so dropping
// the banked term from that shared function turned only the WORKER tests red —
// the member side would have shipped a silently wrong cost. These lock the
// member half of the same rule.

import { describe, it, expect, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
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
    model: "opus",
    effort: "medium",
    kind: "staff",
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

function renderPanel(member: Member) {
  return render(
    <I18nProvider>
      <MemberDetailPanel member={member} onBack={() => {}} />
    </I18nProvider>
  );
}

describe("MemberDetailPanel · 花費 = live + banked", () => {
  it("sums the live session cost and the banked historical cost", async () => {
    const { findByTestId } = renderPanel(
      mkMember({ estimatedCost: 2, bankedCost: 5 }),
    );
    // 2 + 5 = 7 → "$7", never "$2": the banked spend of prior re-execs counts.
    await waitFor(async () =>
      expect((await findByTestId("mp-cost")).textContent).toBe("$7"),
    );
  });

  it("shows banked-only cost when there is no live session", async () => {
    const { findByTestId } = renderPanel(
      mkMember({ estimatedCost: null, bankedCost: 4 }),
    );
    await waitFor(async () =>
      expect((await findByTestId("mp-cost")).textContent).toBe("$4"),
    );
  });
});
