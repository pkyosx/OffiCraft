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
import { localTestWorkers } from "./local-test-workers";

const testWorkers = localTestWorkers();

export default defineConfig({
  testDir: "./visual-guards",
  testMatch: "**/*.ct.spec.tsx",
  snapshotDir: "./visual-guards/__snapshots__",
  fullyParallel: true,
  ...(testWorkers === undefined ? {} : { workers: testWorkers }),
  // CI must never pass because someone left a .only in a guard.
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [["list"]],
  use: {
    // A PREFERRED port, not a pinned one. 5241 is uncommon enough that a
    // co-located agent's dev server (5173/5230+) does not normally collide —
    // but when something IS already on it, Vite quietly moves to the next free
    // port and the run continues.
    //
    // ⚠️ This comment used to claim the opposite ("--strictPort equivalent …
    // fail loudly rather than silently hopping ports"). It was false in both
    // halves: there is no `strictPort` here, and the observed behaviour is the
    // hop. MEASURED (T-70c9): with 127.0.0.1:5241 held by another listener,
    // `npx playwright test -c playwright-ct.config.ts` ran 14 tests, all passed,
    // rc 0.
    //
    // The hop is LOAD-BEARING, so do not "make the old comment true" by adding
    // `strictPort: true`. It is what lets CI runs in SEPARATE clones overlap —
    // the one form of parallelism this repo supports now that bin/ci.sh refuses
    // a second run in the same working copy (T-70c9, bin/lib/ci-lock.sh).
    // Pinning the port would make the second clone's run die on a port clash and
    // take that capability away.
    ctPort: 5241,
    trace: "off",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
