// toMember / toOutsourceWorker pass the station's attach command through
// UNTOUCHED, and read its absence as an old server (T-139).

import { describe, it, expect } from "vitest";
import { toMember, toOutsourceWorker } from "./mappers";
import type { WireMember, WireOutsourceWorker } from "./wire";

// A namespaced station's command — nothing the mapper holds could invent it.
const SERVED = "tmux -L officraft-seth attach -t member-m-1a2b";

function wireMember(over: Partial<WireMember>): WireMember {
  return { id: "m-1a2b", name: "Mira", ...over } as WireMember;
}

function wireWorker(over: Partial<WireOutsourceWorker>): WireOutsourceWorker {
  return { id: "ow-1a2b", codename: "O-1", ...over } as WireOutsourceWorker;
}

describe("terminal_attach_command passthrough", () => {
  it("hands the served command to both agent kinds byte-for-byte", () => {
    expect(
      toMember(wireMember({ terminal_attach_command: SERVED }))
        .terminalAttachCommand,
    ).toBe(SERVED);
    expect(
      toOutsourceWorker(wireWorker({ terminal_attach_command: SERVED }))
        .terminalAttachCommand,
    ).toBe(SERVED);
  });

  it("reads an ABSENT field as \"\" — an old server, never a rebuilt command", () => {
    // 🔴 The failure mode being pinned: a mapper that "helpfully" fell back to
    // `tmux -L officraft attach -t member-<id>` here would be indistinguishable
    // from a correct answer on the main station and wrong on every other one.
    const m = toMember(wireMember({}));
    expect(m.terminalAttachCommand).toBe("");
    const w = toOutsourceWorker(wireWorker({}));
    expect(w.terminalAttachCommand).toBe("");
    // …and the id it would have been rebuilt from IS present, so the "" above
    // is a decision and not a missing input.
    expect(m.id).toBe("m-1a2b");
    expect(w.id).toBe("ow-1a2b");
  });
});
