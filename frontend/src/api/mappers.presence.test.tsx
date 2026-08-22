// The presence NARROWING seam (T-59d6) — one contract, both member and worker
// paths.
//
// WHY this file exists: every presence surface downstream is exhaustive over the
// five-state union (`presenceVisual`'s no-default switch). The wire, however,
// types `presence` as a bare `string` (frozen spec), so a word this build does
// not recognise — an older/newer server, a typo, a state added backend-first —
// can only be caught HERE. If it leaks past this seam it falls off that switch
// and renders `class="lifecycle-dot lifecycle-dot--undefined"`: a dot with no
// colour AND no accessible name on a `role="img"` element. That is worse than
// wrong, it is invisible: nothing else in the app would go red.
//
// So the assertions below are deliberately on BOTH channels — the colour class
// and the accessible name — and the member path is asserted through a REAL
// render of the shared PresenceBadge, not just the mapper's return value.

import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { toMember, toOutsourceWorker, toPresence } from "./mappers";
import { PresenceBadge } from "../components/PresenceBadge";
import type { WireMember, WireOutsourceWorker } from "./wire";

function mkWireMember(over: Partial<WireMember>): WireMember {
  return {
    id: "m-1",
    avatar_icon_id: null,
    name: "Mira",
    role_key: "assistant",
    role_name: "助理",
    kind: "assistant",
    model: "Opus 4.6",
    actual_model: "",
    actual_runtime: "",
    actual_effort: "",
    actual_machine: "",
    refocus_op: "",
    refocus_deadline: 0,
    effort: "medium",
    runtime: "claude",
    presence: "offline",
    roster_status: "offline",
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
    ...over,
  };
}

function renderBadge(wirePresence: string) {
  const member = toMember(mkWireMember({ presence: wirePresence }));
  const { container } = render(
    <I18nProvider>
      <PresenceBadge member={member} />
    </I18nProvider>,
  );
  return container.querySelector(".lifecycle-dot") as HTMLElement;
}

describe("presence narrowing at the mapper seam (T-59d6)", () => {
  it("keeps every real state verbatim, on both the member and the worker path", () => {
    for (const p of [
      "offline",
      "waking",
      "online",
      "stopping",
      "stopped",
    ] as const) {
      expect(toPresence(p)).toBe(p);
      expect(toMember(mkWireMember({ presence: p })).lifecycle).toBe(p);
    }
  });

  it("turns an out-of-union wire word into undefined (the one place it can be caught)", () => {
    expect(toPresence("banana")).toBeUndefined();
    expect(toPresence("")).toBeUndefined();
    expect(toPresence(undefined)).toBeUndefined();
    // Near-misses matter most: a backend-first rename is exactly how an unknown
    // word reaches a shipped frontend.
    expect(toPresence("Online")).toBeUndefined();
    expect(toPresence("stoped")).toBeUndefined();
  });

  it("MEMBER path: an out-of-union wire presence still renders a NAMED, coloured dot", () => {
    const dot = renderBadge("banana");
    // The regression this locks: `lifecycle-dot--undefined` (no colour rule, no
    // label). The honest floor is a real offline dot.
    expect(dot.className).toBe("lifecycle-dot lifecycle-dot--offline");
    expect(dot.className).not.toContain("undefined");
    // role="img" with no accessible name is an unreachable element for a screen
    // reader — presence would exist ONLY as a colour nobody can see.
    expect(dot.getAttribute("role")).toBe("img");
    expect(dot.getAttribute("aria-label")).toBe(zh.office.presence.offline);
    // …and it must never be the live green.
    expect(dot.className).not.toContain("online-awake");
  });

  it("MEMBER path: a real state is unaffected by the narrowing", () => {
    const dot = renderBadge("online");
    expect(dot.className).toBe("lifecycle-dot lifecycle-dot--online-awake");
    expect(dot.getAttribute("aria-label")).toBe(
      zh.office.presence["online-awake"],
    );
  });

  it("member `status` (the frozen tri-state) also floors an unknown word to offline", () => {
    // status and lifecycle are two projections of the SAME word — they must
    // agree about what counts as recognised, or the roster tint and the dot
    // could tell different stories.
    expect(toMember(mkWireMember({ presence: "banana" })).status).toBe(
      "offline",
    );
    expect(toMember(mkWireMember({ presence: "stopping" })).status).toBe(
      "online",
    );
    expect(toMember(mkWireMember({ presence: "stopped" })).status).toBe(
      "offline",
    );
  });

  it("WORKER path: absence stays undefined (released / never dispatched is a real distinction)", () => {
    const wire = {
      id: "ow-1",
      codename: "O-7",
      task_id: "t-1",
      presence: "banana",
    } as unknown as WireOutsourceWorker;
    expect(toOutsourceWorker(wire).presence).toBeUndefined();
    expect(
      toOutsourceWorker({ ...wire, presence: "waking" } as WireOutsourceWorker)
        .presence,
    ).toBe("waking");
  });
});
