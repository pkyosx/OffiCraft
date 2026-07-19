// T-187c — Playwright Component-Testing config for the VISUAL GUARDS.
//
// Why this exists: the vitest suite runs in jsdom (vite.config.ts), which
// applies no layout engine — `offsetHeight` is always 0, flex/grid never
// resolve, and @media never evaluates against a viewport. So every "is the
// pixel actually there / in the right place" contract is structurally
// invisible to it (a `height:5px→0` mutant on the progress bar stays green
// across the whole suite). These guards mount the REAL components against the
// REAL app CSS in a REAL Chromium and assert geometry invariants with
// tolerance — the layer jsdom cannot reach.
//
// Kept OFF the fast path's default globs: specs are *.ct.spec.tsx under
// visual-guards/, which vite.config.ts's test.exclude removes from vitest.
import { defineConfig, devices } from "@playwright/experimental-ct-react";

export default defineConfig({
  testDir: "./visual-guards",
  testMatch: "**/*.ct.spec.tsx",
  snapshotDir: "./visual-guards/__snapshots__",
  fullyParallel: true,
  // CI must never pass because someone left a .only in a guard.
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [["list"]],
  use: {
    // --strictPort equivalent: pin the CT dev server to a fixed, uncommon port
    // so a co-located agent's dev server (5173/5230+) never collides, and fail
    // loudly rather than silently hopping ports.
    ctPort: 5241,
    trace: "off",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
