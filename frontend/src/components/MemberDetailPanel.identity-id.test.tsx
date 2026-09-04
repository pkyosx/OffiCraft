// MemberDetailPanel · the identity badge carries the REAL member id (T-5dab).
//
// Owner 2026-08-15 (rc-2a6b96d0fb0c ①): the badge used to render the derived
// `MB-XXX###` label, so the same person had two identities — the one every
// other surface addresses them by (`mira`, `ow-…`, `m-…`) and a prettier one
// that could not be used to look anything up. The badge now renders
// `member.id` itself.
//
// The assertions are keyed to the member's OWN id (passed per case), not to a
// literal: a test that expected a fixed string would stay green if the badge
// started rendering some other member's id, or a constant.

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getBootstrap: () => Promise.resolve({ context: "" }),
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

function renderBadge(over: Partial<Member> = {}) {
  const { container } = render(
    <I18nProvider>
      <MemberDetailPanel member={mkMember(over)} onBack={() => {}} />
    </I18nProvider>
  );
  return container.querySelector(".mp-identity__id");
}

describe("MemberDetailPanel identity badge — real id (T-5dab)", () => {
  // Every id SHAPE the roster actually mints, so a badge that only handled the
  // short seed ids would not slip through: seed member, hired member, and an
  // outsource contractor (the longest of the three).
  const IDS = ["mira", "m-3417933c8632", "ow-7eed74b85026"];

  for (const id of IDS) {
    it(`renders the member's own id verbatim (${id})`, () => {
      const badge = renderBadge({ id });
      expect(badge).not.toBeNull();
      expect(badge!.textContent).toBe(id);
    });
  }

  it("renders no MB-XXX### label anywhere in the panel", () => {
    // Panel-wide, not badge-only: the point of the ruling is that the derived
    // label is gone from the surface, not that it moved somewhere else on it.
    const { container } = render(
      <I18nProvider>
        <MemberDetailPanel member={mkMember({ id: "mira" })} onBack={() => {}} />
      </I18nProvider>
    );
    expect(container.textContent).not.toMatch(/MB-[A-Z]{3}\d{3}/);
  });

  it("the badge tracks the member it is given, not the first one rendered", () => {
    // Falsifies a hard-coded or captured value: two different members in the
    // same test file must produce two different badges.
    expect(renderBadge({ id: "mira" })!.textContent).not.toBe(
      renderBadge({ id: "kyle" })!.textContent
    );
  });
});
