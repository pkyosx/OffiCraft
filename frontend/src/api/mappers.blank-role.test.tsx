// A member with NO role is not the 特助 — the mapper seam, and the two surfaces
// that read what it returns.
//
// WHY this file exists: both member mappers used to floor a blank wire role to
// "assistant". That literal dates from the era when assistant was the only
// role; once roles became custom it stopped meaning "the only role" and started
// meaning "the ADMIN role", so a member the server described as role-less came
// out of the mapper claiming to be the one member with the most authority — and
// three different surfaces believed it: the role label printed 特助, the avatar
// picked the assistant face, and the roster sort pinned it beside the real one.
// The server never fabricated any of it; the fabrication lived here.
//
// The assertions are on BOTH the mapper's return value AND a real render,
// because the mapper returning "" is only half the claim — the other half is
// that nothing downstream turns "" back into a role.

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { toMember, toMonitoring } from "./mappers";
import { avatarKindForMember } from "../lib/avatarKind";
import { PresenceBadge } from "../components/PresenceBadge";
import type { WireMember, WireMonSession } from "./wire";

function mkWireMember(over: Partial<WireMember>): WireMember {
  return {
    id: "m-1",
    name: "Ada",
    role_key: "",
    role_name: "",
    kind: "staff",
    model: "",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    runtime: "claude",
    presence: "offline",
    roster_status: "active",
    owner_id: "owner",
    desired_machine_id: "m-server-self",
    desired_state: "offline",
    machine: "",
    last_op: "",
    last_op_at: 0,
    forced_stop_at: 0,
    last_op_log: "",
    last_op_reason: "",
    refocus_since: 0,
    schema_version: 1,
    unread_count: 0,
    terminal_attach_command: "tmux -L officraft attach -t member-m-1",
    ...over,
  };
}

const mkWireSession = (over: Partial<WireMonSession> = {}): WireMonSession => ({
  id: "m-1",
  name: "Ada",
  role: "",
  account: "",
  effort: "",
  machine: "",
  model: "",
  presence: "offline",
  runtime: "claude",
  ...over,
});

const monRole = (over: Partial<WireMonSession> = {}) =>
  toMonitoring({ sessions: [mkWireSession(over)], machines: [], accounts: [] })
    .sessions[0].role;

describe("a blank wire role stays blank through the mapper", () => {
  it("toMember passes a blank role_key through instead of calling it assistant", () => {
    expect(toMember(mkWireMember({ role_key: "" })).role).toBe("");
  });

  it("toMonSession passes a blank session role through", () => {
    expect(monRole({ role: "" })).toBe("");
  });

  it("still carries a real role verbatim — assistant included", () => {
    // The positive control: without it, a mapper that blanked EVERY role would
    // pass the two assertions above.
    expect(toMember(mkWireMember({ role_key: "assistant" })).role).toBe(
      "assistant"
    );
    expect(toMember(mkWireMember({ role_key: "r-abc123" })).role).toBe(
      "r-abc123"
    );
    expect(monRole({ role: "assistant" })).toBe("assistant");
  });
});

describe("what the surfaces do with a role-less member", () => {
  it("the role badge does not print 特助", () => {
    const member = toMember(mkWireMember({ role_key: "", role_name: "" }));
    const { container } = render(
      <I18nProvider>
        <PresenceBadge member={member} />
      </I18nProvider>
    );
    expect(container.textContent).not.toContain(zh.office.role.assistant);
  });

  it("the badge still prints 特助 for the real assistant", () => {
    // Same render, one field different — the half that proves the assertion
    // above is reading a live surface and not an empty container.
    const member = toMember(
      mkWireMember({ role_key: "assistant", role_name: "Assistant" })
    );
    const { container } = render(
      <I18nProvider>
        <PresenceBadge member={member} />
      </I18nProvider>
    );
    expect(container.textContent).toContain(zh.office.role.assistant);
  });

  it("the avatar is the ordinary member face, not the assistant's", () => {
    expect(avatarKindForMember(toMember(mkWireMember({ role_key: "" })))).toBe(
      "member"
    );
    expect(
      avatarKindForMember(toMember(mkWireMember({ role_key: "assistant" })))
    ).toBe("assistant");
  });
});
