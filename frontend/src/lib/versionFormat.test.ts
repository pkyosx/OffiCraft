import { describe, it, expect } from "vitest";
import { formatBuildVersion } from "./versionFormat";

describe("formatBuildVersion", () => {
  it("composes v<yymmdd>-<hhmm>-<shortsha> from git sha + commit time", () => {
    expect(
      formatBuildVersion("14417c9a2b3c4d5e", "2026-07-16T09:30:12+08:00")
    ).toBe("v260716-0930-14417c9");
  });

  it("reads the commit's own recorded wall clock, never the viewer timezone", () => {
    expect(formatBuildVersion("f6f5e1c", "2026-07-04T08:54:28+08:00")).toBe(
      "v260704-0854-f6f5e1c"
    );
    expect(formatBuildVersion("f6f5e1c", "2026-07-04T08:54:28Z")).toBe(
      "v260704-0854-f6f5e1c"
    );
  });

  it("degrades to the short sha alone when the commit time is missing", () => {
    expect(formatBuildVersion("14417c9a2b3c4d5e", null)).toBe("14417c9");
  });

  it("degrades to the short sha alone when the commit time is unparsable", () => {
    expect(formatBuildVersion("f6f5e1c", "yesterday")).toBe("f6f5e1c");
  });

  it("returns empty for an empty sha (never a dangling v-label)", () => {
    expect(formatBuildVersion("", "2026-07-16T09:30:12+08:00")).toBe("");
  });
});
