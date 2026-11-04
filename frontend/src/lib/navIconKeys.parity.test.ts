import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it, expect } from "vitest";
import { NAV_ICON_KEYS } from "./themeBundleCore";

const HERE = dirname(fileURLToPath(import.meta.url));
const SERVER = join(HERE, "..", "..", "..", "server", "ocserverd");

function goAllowedKeys(): string[] {
  const output = execFileSync("go", ["run", ".", "nav-icon-keys"], {
    cwd: SERVER,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  return JSON.parse(output) as string[];
}

describe("nav icon keys · Go/TS parity", { timeout: 120_000 }, () => {
  it("keeps the client keys in the approved tab order", () => {
    expect(NAV_ICON_KEYS).toEqual([
      "office",
      "replies",
      "tasks",
      "lore",
      "secrets",
      "monitor",
      "guide",
    ]);
  });

  it("sends every client icon key to the server allowlist", () => {
    expect(NAV_ICON_KEYS).toEqual(goAllowedKeys());
  });
});
