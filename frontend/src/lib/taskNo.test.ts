// Pins for deriveTaskNo (taskNo.ts) — the frontend mirror of `TaskNo` in
// server/ocserverd/domain.go.
//
// The first two cases are COPIED VERBATIM from the server's own pin
// (server/ocserverd/api_tasks_test.go:25-32). That is deliberate: the two
// sides derive the same number from the same id with no shared code, so the
// only thing keeping them honest is that both are nailed to the SAME FACTS.
// If the rule ever changes on the server, the two test files disagree loudly
// instead of the two implementations drifting apart in silence. Keep them in
// sync — if you change a case here, change it there too.
//
// The remaining cases are malformed / boundary ids. Each asserts what the Go
// original does for that input, not what would be "smarter" — a mirror that
// invents its own handling for the ugly inputs is no longer a mirror.

import { describe, it, expect } from "vitest";
import { deriveTaskNo } from "./taskNo";

describe("deriveTaskNo", () => {
  // ── shared with the server pin ─────────────────────────────────────────────

  it("takes the first four hex chars after the prefix", () => {
    expect(deriveTaskNo("t-7d40aabbccdd")).toBe("T-7d40");
  });

  it("does NOT truncate an id shorter than four hex chars", () => {
    // Go guards with `len(hex) > 4` before slicing; anything that unwraps to
    // a fixed-width take (e.g. a /^t-(.{4})/ match) diverges right here.
    expect(deriveTaskNo("t-ab")).toBe("T-ab");
  });

  // ── real-world shape ──────────────────────────────────────────────────────

  it("shortens a real t-<hex12> id to the number shown on the card", () => {
    // The bug this helper exists for: the dep fallback used to print the left
    // side of this expectation instead of the right.
    expect(deriveTaskNo("t-1d8292a2f8db")).toBe("T-1d82");
  });

  // (No pin for an id of exactly four hex chars: `slice` clamps, so no
  // plausible wrong implementation can answer anything but "T-abcd" there.
  // A pin nothing can break is not protection — the truncation boundary is
  // covered from both sides by the two cases above and below instead.)

  // ── malformed / boundary ids ──────────────────────────────────────────────

  it("trims the prefix rather than dropping two chars unconditionally", () => {
    // strings.TrimPrefix returns the string UNCHANGED when the prefix is
    // absent. A hand-rolled slice(2, 6) would answer "T-c123" here.
    expect(deriveTaskNo("abc123")).toBe("T-abc1");
  });

  it("keeps a prefixless id shorter than the prefix itself intact", () => {
    expect(deriveTaskNo("x")).toBe("T-x");
  });

  it("yields the bare 'T-' for an empty id, matching the server", () => {
    // Not a fabricated placeholder and not an exception: this is literally
    // what Go's TaskNo("") returns. An id is never empty in practice, so the
    // only job of this pin is to keep the mirror faithful if it ever is.
    expect(deriveTaskNo("")).toBe("T-");
  });

  it("yields the bare 'T-' for a prefix with no hex body", () => {
    expect(deriveTaskNo("t-")).toBe("T-");
  });

  it("truncates an over-long id to four chars like any other", () => {
    expect(deriveTaskNo("t-" + "f".repeat(64))).toBe("T-ffff");
  });
});
