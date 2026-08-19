// lib/sharedSnapshot.ts — ONE in-flight request + ONE cached answer for a
// snapshot that several independent mount-fetch callers all want at the same
// moment (T-8115).
//
// WHY THIS EXISTS. In 2026-07, `GET /api/settings` measured 639,270 bytes
// uncompressed on the production install — 373 kB gzipped, of which
// `custom_themes` alone was 626,721 (98%) — and FIVE unrelated consumers each
// fetched it for themselves on a single cockpit load: the topbar studio name
// (hooks/useOrgName), the topbar owner nickname (hooks/useOwnerName), the
// display-pref login reconcile (i18n/index.tsx), the 外包 parallel cap
// (hooks/useOutsourceWorkers) and the onboarding banner
// (components/OnboardingBanner). Same URL, same second, five downloads. Nothing
// about that was a *correctness* requirement — each caller simply had no way to
// know the others existed.
//
// ⚠️ THAT PARAGRAPH IS HISTORY AND IS WRITTEN IN THE PAST TENSE ON PURPOSE. The
// number has already been wrong once: measured again in 2026-08 the same field
// was 1,592,133 bytes, about 2.5× the figure above. A measurement left in the
// present tense becomes a lie on a schedule, and the next reader cannot tell a
// figure taken today from one that went stale two tickets ago — which is why
// this was not simply updated to the newer number. T-83ef then moved the themes
// out to their own resource, so the settings payload is now a few hundred
// characters and the SIZE half of the rationale is spent.
//
// What still holds is the other half, and it is enough on its own: five
// unrelated consumers, same URL, same second, five requests. The single-flight
// merge earns its keep on the request count whatever the payload weighs.
//
// The four properties this gives them, in the order they matter:
//
//   1. MERGE (single-flight) — callers that ask while a request is already in
//      the air get THAT promise, not a second request. This is what collapses
//      the五-way mount storm into one round trip; it needs no cache at all.
//   2. CACHE — once an answer lands it is served to later askers with no
//      request. A remount (tab switch, panel open/close) is then free.
//   3. GENERATION GUARD — every request remembers the generation it started
//      in. `adopt` / `invalidate` bump the generation, so a response that was
//      already in flight when a newer truth arrived is handed back to its own
//      caller but NEVER written over the newer value. Without this, a slow GET
//      issued before a PATCH can land after it and silently restore the value
//      the owner just changed.
//   4. EXPLICIT INVALIDATION — see below. There is no TTL.
//
// 🔴 WHEN DOES THE CACHE GO STALE? A cache that never expires and a screen that
// lies are the same thing, so this must have an answer:
//   - `adopt(value)` — THIS tab saved successfully and holds the server's
//     echoed effective values. That echo IS the new truth; it replaces the
//     cached copy outright (no refetch).
//   - `invalidate()` — the identity changed (login / auth-expired). The cached
//     answer belonged to a different session and must not survive it.
//   - `refresh()` — a caller that must NOT be answered from memory (the
//     onboarding poll watching for a state transition; 設定's 存檔測連通
//     read-back, whose whole job is to prove the server agrees). Always a real
//     request; its answer is adopted.
//
// ⚠️ KNOWN, ACCEPTED BOUNDARY (owner is aware — do NOT paper over it): the
// server sends no realtime notification when settings change, so a change made
// in ANOTHER tab or on another device is invisible here until something calls
// `refresh()` or the page reloads. Only this tab's own saves invalidate.
//
// Dependency-free ON PURPOSE: `src/test/setup.ts` imports this module to reset
// every snapshot between tests (same shape as resetChatDrafts). Importing the
// api layer here would drag it into every test file's registry BEFORE that
// file's own `vi.mock("../api")` is registered.

export interface SharedSnapshot<T> {
  /** The cached answer, the in-flight request, or a new request — in that
   * order of preference. */
  load(): Promise<T>;
  /** Always a real request (bypasses both the cache and the in-flight merge);
   * its answer is adopted unless the generation moved on beneath it. */
  refresh(): Promise<T>;
  /** Install a value known to be current (a save echo) and retire every
   * request already in flight. */
  adopt(value: T): void;
  /** Drop the cached answer and retire every request in flight. */
  invalidate(): void;
}

const registry = new Set<{ reset: () => void }>();

/** Test seam: drop every shared snapshot's module-level state. Called from
 * src/test/setup.ts between tests — module-level caches must not leak from one
 * test into the next. */
export function resetAllSharedSnapshots(): void {
  for (const s of registry) s.reset();
}

export function createSharedSnapshot<T>(fetcher: () => Promise<T>): SharedSnapshot<T> {
  let cached: { value: T } | undefined;
  let inflight: Promise<T> | undefined;
  let generation = 0;

  function issue(): Promise<T> {
    const started = generation;
    const p = fetcher().then(
      (value) => {
        // GENERATION GUARD: a newer truth (adopt/invalidate) landed while this
        // request was in the air — hand the caller what it asked for, but do
        // not let a stale answer overwrite the newer one.
        if (started === generation) {
          cached = { value };
          if (inflight === p) inflight = undefined;
        }
        return value;
      },
      (err) => {
        // A failure is never cached: the next caller retries honestly rather
        // than inheriting a permanent "it was broken once".
        if (inflight === p) inflight = undefined;
        throw err;
      },
    );
    return p;
  }

  return {
    load() {
      if (cached) return Promise.resolve(cached.value);
      if (inflight) return inflight;
      const p = issue();
      inflight = p;
      return p;
    },
    refresh() {
      return issue();
    },
    adopt(value: T) {
      generation += 1;
      inflight = undefined;
      cached = { value };
    },
    invalidate() {
      generation += 1;
      inflight = undefined;
      cached = undefined;
    },
  };
}

// Registration is separate from creation so `createSharedSnapshot` stays a pure
// factory (usable in a test without joining the global reset set).
export function registerSharedSnapshot<T>(snapshot: SharedSnapshot<T>): SharedSnapshot<T> {
  registry.add({ reset: () => snapshot.invalidate() });
  return snapshot;
}
