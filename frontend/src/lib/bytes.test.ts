import { describe, it, expect } from "vitest";
import { formatBytes } from "./bytes";

describe("formatBytes", () => {
  it.each([
    ["zero", 0, "0.0 KB"],
    ["less than a kilobyte", 512, "0.5 KB"],
    ["exactly one kilobyte", 1024, "1.0 KB"],
    ["just under a megabyte that rounds up", 1024 * 1024 - 1, "1.0 MB"],
    ["just under a megabyte that does not round up", 1024 * 1023.9, "1023.9 KB"],
    ["exactly one megabyte", 1024 ** 2, "1.0 MB"],
    ["one and a half megabytes", 1.5 * 1024 ** 2, "1.5 MB"],
    ["just under a gigabyte that rounds up", 1024 ** 3 - 1, "1.0 GB"],
    ["exactly one gigabyte", 1024 ** 3, "1.0 GB"],
    ["a measured station total", 43748512 * 1024, "41.7 GB"],
    ["a 1 TB volume", 1000240963584, "931.5 GB"],
    ["exactly one terabyte", 1024 ** 4, "1.0 TB"],
    ["beyond the largest unit", 2048 * 1024 ** 4, "2048.0 TB"],
    ["a negative input", -5, "0.0 KB"],
  ])("under %s (%d bytes) it reads %s", (_name, bytes, expected) => {
    expect(formatBytes(bytes as number)).toBe(expected);
  });
});
