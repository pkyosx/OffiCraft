// `viewerMayForceTaskDone()` — BOTH ARMS, including the one the rest of the
// suite cannot reach (T-192).
//
// 🔴 WHY THIS FILE EXISTS: THE REVIEWER'S SURVIVING MUTANT M2b. The function is
// `USE_MOCK || hasToken()`, and every other vitest file runs in MOCK mode — so
// `USE_MOCK` is `true`, `||` short-circuits, and `hasToken()` is NEVER
// EVALUATED. Replacing the real-mode arm with a constant `true` left the whole
// suite green. The arm with zero coverage was the only one that decides
// anything: in real mode a page with no owner token is a caller the server's
// route floor refuses, and offering it a 強制結案 button is the exact failure
// the gate exists to prevent.
//
// HOW THE ARM IS REACHED. `USE_MOCK` is `import.meta.env.VITE_USE_MOCK !==
// "false"`, computed once at MODULE-EVALUATION time. A plain `vi.stubEnv` after
// the import is therefore too late — the constant is already frozen. So each
// test stubs the env FIRST and then `vi.resetModules()` + a dynamic `import()`,
// which re-evaluates the module against the stubbed env. (The sibling
// AuthGate.mfa.test.tsx solves the same problem with `vi.mock`, which is not
// available here: the module under test is the one that would be mocked.)
//
// ⚠️ WHAT THIS FILE DOES NOT PROVE, and must not be read as proving: that the
// cockpit's rendering is a permission check. `Gated(principalAdminAgent, …)` in
// server/ocserverd/routes.go is what refuses a plain member, and it refuses
// them whatever this function answers. What is pinned here is only that the
// cockpit does not OFFER a control that could then only ever 403.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { TOKEN_KEY } from "./auth";

/** Re-evaluate api/index.ts against whatever env is stubbed right now. */
async function loadGate() {
  vi.resetModules();
  return await import("./index");
}

beforeEach(() => {
  localStorage.clear();
  // The token has a build-time fallback (`VITE_OC_TOKEN`, see api/auth.ts).
  // Pinned to "" in every test here so that "no token" really means no token —
  // otherwise a machine with that variable set would read `true` for the
  // negative arm and the test would pass for the wrong reason.
  vi.stubEnv("VITE_OC_TOKEN", "");
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
  localStorage.clear();
});

describe("viewerMayForceTaskDone — real backend mode", () => {
  it("🔴 NO owner token ⇒ the control is NOT offered", async () => {
    vi.stubEnv("VITE_USE_MOCK", "false");
    const { viewerMayForceTaskDone, USE_MOCK } = await loadGate();
    // The arm under test is really the real one. Without this the test would
    // also pass in mock mode — where it proves nothing at all, which is exactly
    // how the mutant survived.
    expect(USE_MOCK).toBe(false);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();

    expect(viewerMayForceTaskDone()).toBe(false);
  });

  it("an owner token ⇒ the control IS offered", async () => {
    vi.stubEnv("VITE_USE_MOCK", "false");
    localStorage.setItem(TOKEN_KEY, "owner-jwt");
    const { viewerMayForceTaskDone, USE_MOCK } = await loadGate();
    expect(USE_MOCK).toBe(false);

    expect(viewerMayForceTaskDone()).toBe(true);
  });

  it("follows the token as it is minted and cleared — it is read per call, not captured at load", async () => {
    // A session that expires mid-visit must stop being offered the button, and
    // one that logs in must start being offered it, WITHOUT a page reload. A
    // module-level `const offered = hasToken()` would satisfy both tests above
    // and fail this one.
    vi.stubEnv("VITE_USE_MOCK", "false");
    const { viewerMayForceTaskDone } = await loadGate();
    expect(viewerMayForceTaskDone()).toBe(false);

    localStorage.setItem(TOKEN_KEY, "owner-jwt");
    expect(viewerMayForceTaskDone()).toBe(true);

    localStorage.removeItem(TOKEN_KEY);
    expect(viewerMayForceTaskDone()).toBe(false);
  });
});

describe("viewerMayForceTaskDone — mock mode", () => {
  it("is offered with no token, because mock mode has no wall to be outside of", async () => {
    // NOT a loophole, and the reason is `AuthGate`: in mock mode the login wall
    // never renders, so the only principal that exists is the one the mock
    // serves. A token test there would say "not the owner" about the only
    // viewer there is — and the rest of the suite would lose the whole control.
    vi.stubEnv("VITE_USE_MOCK", "true");
    const { viewerMayForceTaskDone, USE_MOCK } = await loadGate();
    expect(USE_MOCK).toBe(true);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();

    expect(viewerMayForceTaskDone()).toBe(true);
  });
});
