// MemberDetailPanel · 最近操作 failure REASON (成員啟動失敗原因全鏈可見).
//
// Locked here:
//   1. A FAILED op whose receipt carried a structured reason renders it as an
//      always-visible one-line summary (no expand needed — the 2026-07-13
//      incident showed a bare "✕ 啟動 失敗" tells the owner nothing).
//   2. An old record WITHOUT a reason renders status-only, exactly as before
//      (no fabricated cause), while the collapsible log stays available.
//   3. A SUCCEEDED op that carried a reason RENDERS it too, in the amber
//      note style rather than the danger style (T-201: the pre-trust verdict
//      no longer refuses the spawn, it reports through last_op_reason, and
//      this panel is the only renderer of that field — gating the line on
//      failure would leave the value visible to the API and to nobody).
//   4. A SUCCEEDED op with no reason still renders status-only.

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
    listScheduledMessages: () => Promise.resolve([]),
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

  // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb for this action, and it is
  // 喚醒. The receipt used to say 啟動 while the button right above it said
  // 喚醒 — the same act under two names on one screen.
  it("names the start op with the same verb the button uses (喚醒, not 啟動)", () => {
    const { container } = renderPanel(mkMember());
    expect(container.querySelector(".mp-lastop__verb")?.textContent).toBe("喚醒");
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

  it("renders no reason line on a successful op that carried none", () => {
    const { queryByTestId } = renderPanel(
      mkMember({ lastOpOk: true, lastOpLog: "", lastOpReason: "" }),
    );
    expect(queryByTestId("mp-lastop-reason")).toBeNull();
  });

  // T-201. The pre-trust verdict reports instead of refusing the spawn, so the
  // member comes up OK and the verdict rides along on the receipt. If this
  // line is gated on failure the owner sees only "✓ 喚醒 成功" and the verdict
  // exists solely in the database — a silent failure traded for a loud one.
  const VERDICT =
    "pretrust_unverified: claude did not confirm the pre-trust marker; claude said: (empty)";

  it("shows the reason on a SUCCEEDED op, styled as a note rather than a failure", () => {
    const { getByTestId, container } = renderPanel(
      mkMember({ lastOpOk: true, lastOpLog: "", lastOpReason: VERDICT }),
    );
    const line = getByTestId("mp-lastop-reason");
    expect(line.textContent).toBe(VERDICT);
    // Amber note, not the danger red of a failed op.
    expect(line.className).toContain("mp-lastop__reason--note");
    expect(container.querySelector(".mp-lastop__head--ok")).not.toBeNull();
    expect(container.querySelector(".mp-lastop__head--fail")).toBeNull();
  });

  it("keeps the failure reason in the danger style (no note modifier)", () => {
    const { getByTestId } = renderPanel(mkMember());
    expect(getByTestId("mp-lastop-reason").className).not.toContain(
      "mp-lastop__reason--note",
    );
  });

  it("offers the collapsible log on a SUCCEEDED op that carried one", () => {
    const { container } = renderPanel(
      mkMember({
        lastOpOk: true,
        lastOpReason: VERDICT,
        lastOpLog: "claude said: (empty)\nmarker file was never read",
      }),
    );
    expect(container.querySelector(".mp-lastop__toggle")).not.toBeNull();
  });
});
