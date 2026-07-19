// MemberDetailPanel · the identity card grows NO role-settings gear (T-dfae).
//
// This file replaces MemberDetailPanel.role-gear.test.tsx, which locked the
// OPPOSITE and was deleted when owner moved the control again. The move has
// three surfaces and each needs its own lock, or "we took it off X" is only a
// claim:
//   1. roster row      — MemberCard.click.test.tsx §4
//   2. THIS panel      — here
//   3. chat header     — ChatArea.header-actions.test.tsx (the positive home)
//
// Owner's history on this control: it shipped on the roster row (2faa5ce),
// moved into this panel (1st ruling), then moved out of the panel entirely and
// onto the chat window header (2nd ruling, 2026-07-17, with a screenshot). The
// panel's status line is back to pure presence.
//
// The assertions below are STRUCTURAL (no button on the status line) rather
// than keyed to the old testid/class. A negative written against a name that no
// component uses any more is unfalsifiable — nothing could ever bring it back,
// so it would pass forever no matter what regressed. A regression here would
// arrive under some NEW name, and the structural form still catches it.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
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
  return render(
    <I18nProvider>
      <MemberDetailPanel member={mkMember(over)} onBack={() => {}} />
    </I18nProvider>
  );
}

describe("MemberDetailPanel identity card — no role gear (T-dfae)", () => {
  beforeEach(() => {
    window.location.hash = "";
  });

  it("the identity card's status line carries presence ONLY — no jump control", () => {
    const { container } = renderPanel();
    const statusLine = container.querySelector(".mp-identity__status")!;

    // Positive control FIRST: the status line exists and really renders the
    // presence badge. Without this, every negative below would also pass on a
    // panel that rendered no status line at all (or on a typo'd selector),
    // which is exactly how these rot into decoration.
    expect(statusLine).not.toBeNull();
    expect(statusLine.querySelector(".presence-badge")).not.toBeNull();

    // The real assertion: no clickable control of ANY name on that line. The
    // gear was a <button> whatever it was called, so this catches a comeback
    // under a new testid/class/label.
    expect(statusLine.querySelector("button")).toBeNull();
    expect(statusLine.querySelector("a")).toBeNull();
    expect(statusLine.querySelector("svg")).toBeNull();
  });

  it("the role settings jump is not offered anywhere in the panel", () => {
    const { queryByLabelText, queryByTestId } = renderPanel();
    // Keyed off the LIVE label — the string the chat header's button really
    // renders today (ChatArea.header-actions.test.tsx proves it is live). A
    // dead string here would make this assertion unfalsifiable.
    expect(queryByLabelText(zh.chat.roleSettingsLink)).toBeNull();
    // The old testid, for the specific 2faa5ce-era regression.
    expect(queryByTestId("mp-role-settings-mira")).toBeNull();
  });

  it("the status line still SHOWS the role — the gear went, the presence did not", () => {
    // The complement of the negatives above, and the reason they are not just
    // "the status line is empty": removing the jump must not remove the role
    // itself. The PresenceBadge legitimately renders member.role as its sub
    // text (that is the badge's job — dot + role), so a different role than the
    // default proves it is read off the member, not hard-coded.
    const { container } = renderPanel({ id: "rex", role: "reviewer" });
    const statusLine = container.querySelector(".mp-identity__status")!;
    expect(statusLine.textContent).toContain("reviewer");
    expect(statusLine.querySelector("button")).toBeNull();
  });
});
