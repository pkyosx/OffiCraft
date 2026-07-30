// mappers — the activity seam (T-a1d7).
//
// The three activity fields are ADDITIVE-OPTIONAL on the wire, which is the
// whole back-compat contract: a server that predates them, or one that defaults
// them away, must leave the cockpit working and must not produce a fabricated
// reading. And because the wire types the verdict as a bare `string`, this
// mapper is the ONE place an unrecognised word can be caught before it reaches
// a render switch.

import { describe, it, expect } from "vitest";
import { toMonitoring, toOutsourceWorker, toActivityState } from "./mappers";
import type { WireMonitoring, WireOutsourceWorker } from "./wire";

describe("toActivityState — the closed-set narrower", () => {
  it("passes the four real words through", () => {
    for (const w of ["active", "idle", "unknown", "never"] as const) {
      expect(toActivityState(w)).toBe(w);
    }
  });

  it("floors anything it cannot read to 'never', not to a live-looking state", () => {
    // "never" is the only honest answer to a word we do not understand: it
    // renders as a dash. Falling back to "idle" would assert we know the
    // session is not working; "active" would be worse still.
    for (const w of ["working", "ACTIVE", "", "busy", undefined]) {
      expect(toActivityState(w)).toBe("never");
    }
  });
});

describe("toMonSession — activity passthrough", () => {
  const monitoring = (over: Record<string, unknown>): WireMonitoring =>
    ({
      accounts: [],
      machines: [],
      sessions: [
        {
          id: "m-1",
          name: "Eva",
          role: "assistant",
          runtime: "claude",
          model: "opus",
          effort: "",
          machine: "",
          account: "",
          presence: "online",
          context_pct: null,
          cost: null,
          banked_cost: null,
          tokens: null,
          ...over,
        },
      ],
    }) as unknown as WireMonitoring;

  it("carries the server's verdict and both anchors verbatim", () => {
    const [s] = toMonitoring(
      monitoring({
        activity_state: "active",
        working_since: 1000.5,
        last_turn_completed_at: 900,
      })
    ).sessions;
    expect(s.activityState).toBe("active");
    expect(s.workingSince).toBe(1000.5);
    expect(s.lastTurnCompletedAt).toBe(900);
  });

  it("reads an OLDER server (fields absent) as never, with no stamps", () => {
    const [s] = toMonitoring(monitoring({})).sessions;
    expect(s.activityState).toBe("never");
    expect(s.workingSince).toBeNull();
    expect(s.lastTurnCompletedAt).toBeNull();
  });

  it("keeps an unknown verdict distinct from active — it never re-derives", () => {
    // Same anchor, different word: the mapper does not look at the timestamp
    // to decide anything. The threshold lives on the server.
    const [s] = toMonitoring(
      monitoring({ activity_state: "unknown", working_since: 1000 })
    ).sessions;
    expect(s.activityState).toBe("unknown");
    expect(s.workingSince).toBe(1000);
  });
});

describe("toOutsourceWorker — the same seam, the other wire", () => {
  const wire = (over: Record<string, unknown>): WireOutsourceWorker =>
    ({
      id: "ow-1",
      codename: "O-7",
      status: "active",
      task_id: "t-1",
      ...over,
    }) as unknown as WireOutsourceWorker;

  it("carries the verdict and anchors for a worker row", () => {
    const w = toOutsourceWorker(
      wire({
        activity_state: "idle",
        working_since: null,
        last_turn_completed_at: 1234,
      })
    );
    expect(w.activityState).toBe("idle");
    expect(w.workingSince).toBeNull();
    expect(w.lastTurnCompletedAt).toBe(1234);
  });

  it("reads an older server's worker row as never", () => {
    const w = toOutsourceWorker(wire({}));
    expect(w.activityState).toBe("never");
    expect(w.workingSince).toBeNull();
    expect(w.lastTurnCompletedAt).toBeNull();
  });
});
