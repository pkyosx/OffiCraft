// forced_stop_live reaches the stop ladder through both mappers, and
// forced_stop_at does not: forced_stop_at outlives the session, so reading it
// would put the next ordinary stop straight on 強制停止.

import { describe, it, expect } from "vitest";
import { toMember, toOutsourceWorker } from "./mappers";
import { stopLadderStageOf } from "../components/MemberActionButtons";
import type { WireMember, WireOutsourceWorker } from "./wire";

const stoppingAfterForce = {
  presence: "stopping",
  desired_state: "offline",
  refocus_since: 0,
  refocus_op: "",
  refocus_deadline: 0,
  forced_stop_at: 1_790_911_977,
};

function wireMember(over: Partial<WireMember>): WireMember {
  return { id: "m-1", name: "Kip", ...stoppingAfterForce, ...over } as WireMember;
}

function wireWorker(over: Partial<WireOutsourceWorker>): WireOutsourceWorker {
  return { id: "ow-1", name: "O-1", ...stoppingAfterForce, ...over } as WireOutsourceWorker;
}

describe("forced_stop_live through the mappers to the stop ladder", () => {
  it("a stop already forced, session still connected, stays on 強制停止 for both kinds", () => {
    expect(stopLadderStageOf(toMember(wireMember({ forced_stop_live: true })))).toBe("accelerated");
    expect(stopLadderStageOf(toOutsourceWorker(wireWorker({ forced_stop_live: true })))).toBe(
      "accelerated",
    );
  });

  it("an ordinary stop after an earlier force-stop starts at 加速停止 for both kinds", () => {
    expect(stopLadderStageOf(toMember(wireMember({ forced_stop_live: false })))).toBe("soft");
    expect(stopLadderStageOf(toOutsourceWorker(wireWorker({ forced_stop_live: false })))).toBe(
      "soft",
    );
  });

  it("a server that does not send the field reads as not forced", () => {
    expect(toMember(wireMember({})).forcedStopLive).toBe(false);
    expect(toOutsourceWorker(wireWorker({})).forcedStopLive).toBe(false);
  });
});
