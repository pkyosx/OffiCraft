// T-1500 — Playwright config for the PAINT GUARDS (pre-React theme paint).
//
// Separate from playwright-ct.config.ts because these are not component tests:
// they load the REAL built artifact (dist/) over HTTP and sample real animation
// frames. The CT runner mounts components against a dev server and never
// produces a dist/index.html, so it cannot host them.
//
// It is NOT a new CI gate. `npm run test:ct` runs the CT config and then this
// one, so both live inside bin/ci.sh's existing step 4c. That is deliberate: the
// three guards this ticket adds (validator / artifact shape / zero flash) each
// sit in a DIFFERENT existing host —
//   * validator + artifact shape → vitest (step 4b), no browser needed;
//   * zero flash + injection     → here, inside step 4c.
// so no single gate being dropped for cost can take all three with it.
//
// Two servers, because the two scenarios differ only in what the SERVER knows:
//   mode=ok            — the server recognises the owner's theme (happy path)
//   mode=unknown-theme — it does not, so the stale picture must be dropped
// The 400 ms settings delay is not padding: the flash this ticket fixes IS the
// wait for /api/settings, and a zero-latency answer would remove the very window
// under test.
//
// The two ports are NOT pinned. They used to be 4318 and 4319, which meant two
// working copies running the guards at once fought over the same pair and the
// loser died on a bind it could never win. allocateFreePorts() asks the kernel
// for a pair nobody is using, and the specs read the resulting URLs out of
// PAINT_GUARD_OK_URL / PAINT_GUARD_UNKNOWN_URL rather than knowing a number.
// Set either variable yourself and this config leaves that server to you.
import { defineConfig, devices } from "@playwright/test";
import { allocateFreePorts } from "./paint-guards/freePort";

const DIST = process.env.PAINT_GUARD_DIST ?? "dist";

interface StubSpec {
  /** The env var the specs read this server's base URL from. */
  urlVar: "PAINT_GUARD_OK_URL" | "PAINT_GUARD_UNKNOWN_URL";
  mode: "ok" | "unknown-theme";
}

const STUBS: StubSpec[] = [
  { urlVar: "PAINT_GUARD_OK_URL", mode: "ok" },
  { urlVar: "PAINT_GUARD_UNKNOWN_URL", mode: "unknown-theme" },
];

// Only the stubs this config still has to start need a port, so an operator who
// supplied one URL by hand does not have a port allocated for a server nobody
// will launch.
const selfHosted = STUBS.filter((stub) => !process.env[stub.urlVar]);
const ports = allocateFreePorts(selfHosted.length);

const webServer = selfHosted.map((stub, i) => {
  const port = ports[i];
  // The workers that run the specs are forked from this process, so writing the
  // URL here is how it reaches them. This is the ONLY place the number exists.
  process.env[stub.urlVar] = `http://localhost:${port}`;
  return {
    command: `node paint-guards/settingsStub.mjs --port ${port} --dist ${DIST} --mode ${stub.mode} --delay 400`,
    url: `http://localhost:${port}/api/settings`,
    reuseExistingServer: false,
    stdout: "pipe" as const,
  };
});

export default defineConfig({
  testDir: "./paint-guards",
  testMatch: "**/*.paint.spec.ts",
  // Frame timing is the measurement. Two pages sampling rAF on one machine
  // perturb each other, so these run one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  // A flake here is a real signal about first-paint timing; retrying would hide
  // exactly what the guard exists to see.
  retries: 0,
  reporter: [["list"]],
  timeout: 60_000,
  use: { trace: "off", ...devices["Desktop Chrome"] },
  projects: [{ name: "paint-guards" }],
  webServer,
});
