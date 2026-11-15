// Pins the recovery of the shared SSE downlink from a PERMANENT failure —
// the case the browser explicitly does NOT handle, and the one nothing in this
// app could see.
//
// The shape of the bug this file exists to keep dead:
//   `EventSource` retries by itself only while it can. On a non-200 response, a
//   401, or a wrong `Content-Type` the spec has it FAIL the connection and move
//   it to CLOSED — permanently, no retry, ever. The old code had no `onerror`
//   at all, so `sseSource` stayed non-null pointing at that corpse; every later
//   `ensureSseSource()` early-returned on it, `onopen` never fired again, and
//   the reconnect resync never ran. The cockpit froze, in silence, until a
//   manual reload. Owner's report (2026-08-21): 「有時候…要 refresh page 才會更
//   新」.
//
// Two things are therefore asserted TOGETHER throughout, and neither is
// sufficient alone:
//   (1) it comes BACK — a new connection is built; and
//   (2) coming back REFETCHES — the rebuilt connection's first open fans the
//       full resync, because the stream has no replay (spec/sse.md §2.1) and
//       everything emitted during the outage is otherwise lost forever.
// A "fix" that satisfies (1) without (2) is strictly worse than the bug: it
// replaces a visible stall with an invisible hole.

// MEASURED MUTANTS — every guard below was made to fail on purpose, and each one
// is re-runnable by hand: apply the edit to api/http.ts, run this file, put it
// back. Re-measured 2026-08-27 after independent review round 3, and extended
// 2026-08-31 after round 4 (M3 split into M3/M3b, M14 and R1b added); 25
// caught, plus 2 equivalent mutants documented below rather than falsely
// pinned.
//
// ⚠️ THREE ROUND-4 FIXES CARRY NO MUTANT, and saying so is part of the record:
//   · the WHATWG readyState constants in this file's `FakeEventSource` were
//     inverted (CONNECTING/OPEN swapped). That is a defect in the TEST DOUBLE,
//     not in production — nothing in api/http.ts can be mutated to detect it,
//     because production reads only `!== CLOSED` and CLOSED was already right.
//     What it endangered was a FUTURE assertion, so the fix is the constants
//     themselves and there is nothing to redden.
//   · the "15s" in one test title (the constant is 8000; 15000 is the ceiling
//     asserted) and a missing space in .claude/rules/data-layer.md are prose.
//   Recording them as uncaught is the point: an unmeasurable fix listed beside
//   measured ones, with no red to its name, is how a table starts lying.
//
// 🔴 CORRECTION, LEFT IN ON PURPOSE. An earlier version of this header claimed
// these were "single-assertion attributions, not cascades". THAT WAS FALSE and
// review round 1 measured it. The reds are honest — each test independently
// needs the path being broken, so the fan-out is SEMANTIC, not test pollution —
// but "each mutant maps to one named assertion" was not true, and a wrong
// description of evidence is worse than none: it tells the next person there is
// nothing left to check. The `reds` column is the claim now, not an adjective.
//
// 🔴 SECOND CORRECTION, ALSO LEFT IN. Round 4 caught this table overstating
// M3. It read `if (status === 401 || status === 403)` → `if (false)` / 2 reds,
// which is a WEAK mutant: killing both arms at once, its 2 reds proved only the
// 401 arm, and the row's presence told the next reader the 403 arm was covered.
// It was not — review measured `→ if (status === 401)` GREEN across all 2475
// tests. Same failure mode as the correction above, one level deeper: the
// evidence was real, its SCOPE was not, and a row that names a condition while
// only testing half of it is the most expensive kind of wrong. M3 is now the
// single-arm mutant and M3b is its mirror; a two-arm condition needs two rows.
//
//   #    the edit                                              reds  the assertion that names it
//   ---  ----------------------------------------------------  ----  ---------------------------
//   M1   delete the whole `es.onerror = …` handler (THE BUG)     13   "a permanently CLOSED stream is REBUILT"
//   M2   `if (opened || sseGapPending)` → `if (opened)`           4   "the rebuilt connection's FIRST open fans a FULL resync"
//   M2b  delete `sseGapPending = true` in the CLOSED branch       4   "a NEW subscriber mounting during the outage…"
//   M3   `… === 401 || … === 403` → `if (status === 401)`         1   "403 STOPS the retry loop too…"
//   M3b  `… === 401 || … === 403` → `if (status === 403)`         1   "401 STOPS the retry loop…"
//   M4   `if (es.readyState !== 2 …)` → `if (false)`              1   "a TRANSIENT error … does NOT tear the connection down"
//   M5   `const idx = Math.min(sseRetryAttempt, …)` → `= 0`       1   "repeated failures BACK OFF"
//   M6   drop `&& sseVisibilityHandler === null`                  1   "rebuilding does NOT stack a second foreground listener"
//   M7   restore `&& sseSource` on the last-unsubscribe teardown  1   "the LAST unsubscribe during the retry window…"
//   M10  delete `ctrl.abort()` in the probe                       1   "the probe ABORTS the stream it opened"
//   M11  delete the `if (sseSource !== es) return` stale guard    1   "a handler from a connection we already replaced is IGNORED"
//   M12  probe's `if (!t) return 401` → `return 0`                1   "no token is answered as UNAUTHORIZED…"
//   M13  neuter the probe deadline (`setTimeout(() => {}, …)`)    1   "a probe that never answers is ENDED by its deadline"
//   N1   drop the try/catch inside `resyncAll`'s fan               1   "one subscriber throwing SYNCHRONOUSLY…"
//   N1b  drop the try/catch inside the live delta fan              1   "a throwing subscriber does not stop an ordinary delta…"
//   N3   delete `sseGapPending = false` (never clear the debt)     2   "the debt is discharged ONCE…"
//   N6   `SSE_PROBE_TIMEOUT_MS` 8000 → 600000                      1   "the probe deadline is BOUNDED, not merely present"
//   M14  delete `handleUnauthorized()` in ensureSseSource's           1   "the second door reaches the SAME conclusion…"
//        decline arm (leave only `setSseState("unauthorized")`)
//   R1   `if (!evt || …)` → drop the `!evt`                           1   http.sse-malformed-frames.test.ts, the "null" row
//   R1b  `typeof evt.topic !== "string" || evt.topic === ""`          3   http.sse-malformed-frames.test.ts, the three
//        → `!evt.topic` (truthy is not a type)                            `{"topic": <non-string>}` rows
//   R2   drop the try/catch inside `setSseState`'s fan               4   "a listener throwing on 'connecting' does NOT cancel the reconnect"
//   R5   drop the try/catch on the immediate first `cb(sseState)`    3   "a listener that throws on its very FIRST call…"
//
// ⚠️ TWO MUTANTS DELIBERATELY LEFT UNCAUGHT, because they are EQUIVALENT and a
// test for them would be a lie. R3/R4 revert "mechanism before broadcast" at two
// call sites (schedule-then-announce, teardown-then-announce). Measured 2×2 with
// R2: the ordering only changes behaviour when the isolation is ALSO gone
// (isolation off + announce-first = permanently frozen; every other combination
// recovers). While the isolation stands, no observation can separate them. They
// are kept as a second line and documented at the call site rather than pinned
// by an assertion that would pass either way — writing that assertion is exactly
// the mistake this file has already made twice.
// 🔴 AND THE EQUIVALENCE EXPIRES. It holds only while `setSseState` isolates its
// callbacks. Remove that isolation and the ordering becomes the sole defence
// against a permanently frozen cockpit, at which point these stop being
// equivalent and MUST be pinned here. Whoever weakens that try/catch owns this
// paragraph.
//
// PROVENANCE, because it says something about where the holes were:
//   round 1 review → M2b was a REAL DEFECT in the shipped code (not a
//     hypothetical); M10/M11/M12 were mutants that SURVIVED it.
//   round 3 review → R1 was a REGRESSION THIS BRANCH SHIPPED: splitting an
//     over-broad try/catch narrowed the protection and a `null` frame began
//     throwing out of onmessage. R2 was a pre-existing hole in the THIRD
//     fan-out, upstream of the retry scheduler — its failure mode is this
//     ticket's original bug reached through another door. R5 (the immediate
//     `cb(sseState)`) was found by probing behaviour, NOT by the review's
//     enumeration: that search was `for (const cb of`, and this hand-off is a
//     bare call, so it was invisible to the pattern. A denominator is only as
//     honest as the pattern that produced it.
//   round 2 review → N1 was a REAL DEFECT (P3, found on the head); N3 and N6
//     were mutants that survived, and both survived for the SAME reason: the
//     test named the property but never reached the line. N3's test added a
//     subscriber to a healthy stream, so no second connection ever opened. N6's
//     test advanced time BY THE CONSTANT IT WAS CHECKING — a tautology about
//     that number, green at any value.
//   M12 needed its assertion strengthened before it would die: the difference
//     was entirely OUTSIDE this module (whether oc-auth-expired reaches the auth
//     layer), so a test that only watched this module's own state was blind.
// 🔑 The recurring lesson across all four: an assertion has to be reached, and
// it has to be anchored to something it does not itself define.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import {
  httpApi,
  SSE_RESYNC_TOPICS,
  sseConnectionState,
  SSE_PROBE_TIMEOUT_MS,
  SSE_STALL_TIMEOUT_MS,
  __resetSseDownlinkForTests,
} from "./http";
import { AUTH_EXPIRED_EVENT } from "./client";
import { TOKEN_KEY } from "./auth";
import { useMembers } from "../hooks/useMembers";
import { useChat } from "../hooks/useChat";

const hookApi = vi.hoisted(() => ({
  listMembers: vi.fn(async () => [
    {
      id: "m-open",
      name: "m-open",
      status: "active",
      lifecycle: "online",
      unreadCount: 0,
    },
  ]),
  getMember: vi.fn(async () => {
    throw new Error("no per-item read expected");
  }),
  listChat: vi.fn(async () => []),
  listChatReads: vi.fn(async () => []),
  listChatWindow: vi.fn(async () => []),
  getFirstUnreadChat: vi.fn(async () => null),
  markChatRead: vi.fn(async () => undefined),
}));

vi.mock("./index", async () => {
  const { httpApi: real } = await import("./http");
  return { api: { ...hookApi, subscribeEvents: real.subscribeEvents } };
});

// WHATWG EventSource readyState (HTML §9.2). 🔴 CORRECTED round 4: this file
// used to declare `CONNECTING = 1` and set `readyState = 0 // OPEN` in `open()`
// — CONNECTING and OPEN swapped relative to the spec. It was harmless because
// production only ever asks `readyState !== 2`, and 2 (CLOSED) was the one
// value that was right; but the moment anyone adds a `readyState === OPEN`
// branch, a fake with inverted constants tests it green against the wrong
// value. Left named rather than inlined so the disagreement can be seen.
const CONNECTING = 0;
const OPEN = 1;
const CLOSED = 2;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  readyState: number = CONNECTING;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  addEventListener(type: string, cb: (e: MessageEvent) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), cb]);
  }
  /** The server's named `event: heartbeat` frame. */
  heartbeat(): void {
    for (const cb of this.listeners.get("heartbeat") ?? []) {
      cb({ type: "heartbeat", data: "{}" } as MessageEvent);
    }
  }
  close(): void {
    this.closed = true;
    this.readyState = CLOSED;
  }
  /** The browser's native `open` event. */
  open(): void {
    this.readyState = OPEN;
    this.onopen?.();
  }
  /** A TRANSIENT drop: the browser is retrying this same connection itself. */
  transientError(): void {
    this.readyState = CONNECTING;
    this.onerror?.();
  }
  /** A PERMANENT failure: the browser has given up on this connection for good
   * (non-200 / 401 / wrong content-type). Nothing will reopen it. */
  permanentError(): void {
    this.readyState = CLOSED;
    this.onerror?.();
  }
  emit(data: unknown): void {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

/** The `/api/events` status the auth probe will read on the next retry. */
let probeStatus = 200;
let probeCalls = 0;
/** Set to make the probe reject (offline: no answer at all, NOT a 401). */
let probeThrows = false;
/** Set to make the probe HANG — a server that accepts the socket and never
 * sends headers. Only the deadline can end it. */
let probeHangs = false;
/** Set to drop the token WHILE the probe is in flight — the only way to reach
 * `ensureSseSource`'s decline arm, since the probe itself answers 401 when the
 * token is already gone. See the test that uses it. */
let probeDropsToken = false;
/** Everything the last probe was called with, so the request's own shape (its
 * abort signal, its headers) can be asserted rather than assumed. */
let lastProbe: { url: string; init: RequestInit } | null = null;

beforeEach(() => {
  FakeEventSource.instances = [];
  probeStatus = 200;
  probeCalls = 0;
  probeThrows = false;
  probeHangs = false;
  probeDropsToken = false;
  lastProbe = null;
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("fetch", (url: string, init: RequestInit) => {
    probeCalls += 1;
    lastProbe = { url: String(url), init };
    expect(String(url)).toContain("/api/events?token=");
    if (probeThrows) return Promise.reject(new Error("offline"));
    if (probeHangs) {
      // Settle ONLY on abort — exactly what a stalled server looks like.
      return new Promise<Response>((_res, rej) => {
        init.signal?.addEventListener("abort", () =>
          rej(new Error("aborted")),
        );
      });
    }
    if (probeDropsToken) localStorage.removeItem(TOKEN_KEY);
    return Promise.resolve({ status: probeStatus } as Response);
  });
  vi.useFakeTimers();
  localStorage.setItem(TOKEN_KEY, "test-owner-jwt");
  for (const fn of Object.values(hookApi)) fn.mockClear();
});

afterEach(() => {
  // The downlink is module-level singleton state. A test that fails part way
  // through leaves a live fake connection and an armed retry behind, and every
  // later test then measures that debris — which is how one real failure turns
  // into a dozen fake ones and a mutant sweep stops naming the guard that
  // actually caught it.
  __resetSseDownlinkForTests();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.removeItem(TOKEN_KEY);
});

/** Run the pending retry timer AND let the probe's promise chain settle. */
async function runRetry(): Promise<void> {
  await vi.runOnlyPendingTimersAsync();
  await vi.runOnlyPendingTimersAsync();
}

const latest = () => FakeEventSource.instances[FakeEventSource.instances.length - 1];
/** "Is this the connection the module is still using?" — observed through
 * behaviour (it was never closed and nothing replaced it) rather than by
 * reaching into module internals. */
const sseSourceIsAlive = (es: FakeEventSource) =>
  !es.closed && latest() === es;

describe("httpApi · SSE downlink recovery from a PERMANENT failure", () => {
  it("a permanently CLOSED stream is REBUILT — the old code left the corpse in place and never reconnected", async () => {
    const off = httpApi.subscribeEvents(() => {});
    expect(FakeEventSource.instances).toHaveLength(1);
    const dead = FakeEventSource.instances[0];
    dead.open();

    dead.permanentError();
    // Nothing is retried synchronously — the backoff owns the timing.
    expect(FakeEventSource.instances).toHaveLength(1);

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(latest()).not.toBe(dead);
    expect(latest().url).toBe("/api/events?token=test-owner-jwt");
    off();
  });

  it("the rebuilt connection's FIRST open fans a FULL resync — recovery that skipped this would silently drop everything emitted during the outage", async () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    // First open of a FRESH connection: no resync (every hook fetched on mount).
    expect(seen).toEqual([]);

    FakeEventSource.instances[0].permanentError();
    await runRetry();

    // Still nothing: the new socket has not opened yet. The resync is tied to a
    // real open, never to the mere intent to reconnect.
    expect(seen).toEqual([]);

    latest().open();
    expect(seen).toEqual([...SSE_RESYNC_TOPICS]);
    off();
  });

  it("an OPEN stream that delivers nothing past the stall timeout is rebuilt, and its first open refetches the roster and the open chat", async () => {
    const view = renderHook(() => ({
      members: useMembers(),
      chat: useChat("m-open"),
    }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    const stalled = FakeEventSource.instances[0];
    act(() => stalled.open());
    expect(hookApi.listMembers).toHaveBeenCalled();
    expect(hookApi.listChat).toHaveBeenCalled();
    hookApi.listMembers.mockClear();
    hookApi.listChat.mockClear();

    await act(() => vi.advanceTimersByTimeAsync(SSE_STALL_TIMEOUT_MS - 1000));
    expect(stalled.closed).toBe(false);

    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(stalled.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(hookApi.listMembers).not.toHaveBeenCalled();

    act(() => latest().open());
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(hookApi.listMembers).toHaveBeenCalled();
    expect(hookApi.listChat).toHaveBeenCalled();
    expect(sseConnectionState()).toBe("live");
    view.unmount();
  });

  it("heartbeats keep an otherwise silent stream alive without any refetch or reconnect", async () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    const view = renderHook(() => ({
      members: useMembers(),
      chat: useChat("m-open"),
    }));
    await act(() => vi.advanceTimersByTimeAsync(0));
    const es = FakeEventSource.instances[0];
    act(() => es.open());
    hookApi.listMembers.mockClear();
    hookApi.listChat.mockClear();

    for (let i = 0; i < 8; i++) {
      await act(() => vi.advanceTimersByTimeAsync(15_000));
      act(() => es.heartbeat());
    }

    expect(8 * 15_000).toBeGreaterThan(SSE_STALL_TIMEOUT_MS);
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(es.closed).toBe(false);
    expect(seen).toEqual([]);
    expect(hookApi.listMembers).not.toHaveBeenCalled();
    expect(hookApi.listChat).not.toHaveBeenCalled();
    expect(probeCalls).toBe(0);
    view.unmount();
    off();
  });

  it("a TRANSIENT error (browser retrying by itself) does NOT tear the connection down or build a second one", async () => {
    const off = httpApi.subscribeEvents(() => {});
    const es = FakeEventSource.instances[0];
    es.open();

    es.transientError();
    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(es.closed).toBe(false);
    // No probe either: nothing has failed permanently, so there is nothing to
    // ask the server about.
    expect(probeCalls).toBe(0);
    off();
  });

  it("401 STOPS the retry loop, clears the token and fires oc-auth-expired — it never hammers a server that already said no", async () => {
    probeStatus = 401;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(expired).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBe(null);
    expect(sseConnectionState()).toBe("unauthorized");
    // No new connection, and no further attempts however long we wait.
    expect(FakeEventSource.instances).toHaveLength(1);
    await runRetry();
    await runRetry();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(probeCalls).toBe(1);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  // 🔴 THIS TEST EXISTS BECAUSE THE TABLE ABOVE USED TO LIE ABOUT IT. M3 was
  // written as `→ if (false)`, which deletes BOTH arms of the status check at
  // once; its 2 reds therefore only ever proved the 401 arm. Review round 4
  // measured the single-arm mutant instead — `if (status === 401 || status ===
  // 403)` → `if (status === 401)` — and the WHOLE TREE (2475 tests) stayed
  // green. The 403 arm was live code with nothing standing over it.
  //
  // And it is a REACHABLE 403, not a theoretical one: `/api/events` is declared
  // `Requires: principalMachine`, and `requirePrincipalClass` in server
  // authz.go answers a wrong principal class with a FLAT 403 (no 401, no
  // challenge). An owner JWT hitting a machine-only route is exactly that
  // shape. Drop the 403 arm and the client gets precisely the disease this
  // whole branch is against: an unbounded 30s retry loop against a server that
  // has already said no, a banner that can never clear because recovery is
  // impossible, and no drop to the login wall — because nothing fires
  // oc-auth-expired. Visible-forever is not better than silent; it is the same
  // stall with a decoration on it.
  it("403 STOPS the retry loop too — a flat 'wrong principal' is just as final as a 401, and hammering it forever is the bug", async () => {
    probeStatus = 403;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(expired).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBe(null);
    expect(sseConnectionState()).toBe("unauthorized");
    // No new connection, and no further attempts however long we wait — the
    // "does it stop" half is the half that separates 403 from a 5xx.
    expect(FakeEventSource.instances).toHaveLength(1);
    await runRetry();
    await runRetry();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(probeCalls).toBe(1);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  // The SECOND door onto "unauthorized", and until round 4 the two doors did
  // not agree. `attemptSseReconnect` decides on the probe's status, then asks
  // `ensureSseSource()` to rebuild — and `ensureSseSource` has a veto of its
  // own: no token, no stream. That arm used to set `sseState = "unauthorized"`
  // and stop there, which puts a banner over an app that still looks logged in
  // and never fires `oc-auth-expired`, so AuthGate never drops to the login
  // wall. Review round 4 judged the divergence unreachable through the product
  // (the probe answers 401 first when the token is gone) and called the fix
  // hygiene rather than a defect — correct, and the reason this test has to
  // force the window open by hand rather than pretend it is a user story.
  //
  // 🔑 IT IS PINNED ANYWAY BECAUSE "UNREACHABLE" IS A CLAIM ABOUT TODAY'S PROBE.
  // The arm is live code; nothing stops a later change to `probeEventsEndpoint`
  // (a cached answer, a different no-token verdict) from making it reachable,
  // and at that point the divergence is silent. Asserting the CONCLUSION both
  // doors reach costs one test and stops the two from drifting apart again.
  it("the second door reaches the SAME conclusion: a token that vanishes mid-probe logs out, it does not just paint a banner", async () => {
    // 503 so the status check falls through to the rebuild path — this is
    // explicitly NOT the 401/403 arm — while the token disappears underneath.
    probeStatus = 503;
    probeDropsToken = true;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    // No stream was built (ensureSseSource vetoed) …
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(sseConnectionState()).toBe("unauthorized");
    // … and the verdict LEFT this module, which is the whole point: the state
    // alone is invisible to AuthGate.
    expect(
      expired,
      "without handleUnauthorized() the app keeps a login-looking shell behind a banner that can never clear",
    ).toHaveBeenCalledTimes(1);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("a 5xx is a transport failure, not an auth one: it reconnects and does NOT log anyone out", async () => {
    probeStatus = 503;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(expired).not.toHaveBeenCalled();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("test-owner-jwt");
    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("an unanswered probe (offline) reconnects too — 'no answer' must never be read as 'not authorized'", async () => {
    probeThrows = true;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("repeated failures BACK OFF: the second wait is longer than the first, and a good open resets it", async () => {
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();

    // Failure #1 — retried after the first (shortest) delay.
    FakeEventSource.instances[0].permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(2);

    // Failure #2, WITHOUT a successful open in between — the same first delay
    // must NOT be enough this time.
    latest().permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(2); // still waiting
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(3);

    // A good open resets the backoff, so the NEXT outage is short again.
    latest().open();
    latest().permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(4);
    off();
  });

  it("the LAST unsubscribe during the retry window cancels the pending retry — no connection is opened for nobody", async () => {
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    off(); // the retry timer is still armed at this point

    await runRetry();
    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(probeCalls).toBe(0);
    expect(sseConnectionState()).toBe("idle");
  });

  it("rebuilding does NOT stack a second foreground listener: one visibilitychange still fans exactly ONE resync", async () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    await runRetry();
    latest().open();
    seen.length = 0;

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(seen).toEqual([...SSE_RESYNC_TOPICS]);
    off();
  });
});

describe("httpApi · the doors OTHER than the retry timer (a rebuild nobody routed)", () => {
  it("a NEW subscriber mounting during the outage must not cause a SILENT reconnect without resync", () => {
    // Adopted verbatim from the independent review, which found this on the
    // unmodified fix. It is the reason the "we owe a resync" fact is module
    // state instead of an argument: `subscribeEvents` is a SECOND door into
    // `ensureSseSource`, and during an outage `sseSource` is null, so the next
    // component to mount rebuilds the stream itself. With the debt carried as a
    // parameter that door defaulted it to false — connection back, state "live",
    // banner gone, outage deltas silently lost. Measured before the fix:
    // `seen` was [] where 13 topics were owed.
    //
    // This is NOT a hypothetical door. There is no shared fan-out layer; the
    // app holds ~24 independent subscriptions and ordinary use adds more —
    // ChatReplyCard subscribes per card (a long thread scrolls dozens into
    // view), TaskArtifactsPopover on open, useChat on every peer switch. The
    // backoff is up to 30s, and "the screen looks wrong so I start clicking" is
    // the reported user behaviour, so this window gets hit.
    const seen: string[] = [];
    const offA = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    expect(FakeEventSource.instances).toHaveLength(1);

    const offB = httpApi.subscribeEvents(() => {});
    expect(
      FakeEventSource.instances,
      "the new subscriber rebuilt the stream — that part is fine",
    ).toHaveLength(2);

    latest().open();
    expect(
      seen,
      "a rebuild through ANY door still owes the full resync",
      ).toEqual([...SSE_RESYNC_TOPICS]);
    offA();
    offB();
  });

  it("the debt is discharged ONCE: a LATER connection that owes nothing does not fan a resync", async () => {
    // 🔴 THIS TEST USED TO BE A LIE, and the shape of the lie is worth keeping
    // written down because it is the same one the owner caught elsewhere today:
    // it LOOKED like it guarded "the debt gets cleared", and it guarded nothing.
    // The old version added a second subscriber while the stream was already
    // healthy — so `ensureSseSource` early-returned, NO second connection was
    // ever built, NO second open ever happened, and the clearing line was never
    // on the path under test. Measured: deleting `sseGapPending = false`
    // outright left all 35 tests green.
    //
    // To reach the line at all the test has to get a connection to OPEN while
    // carrying no debt, which means tearing the downlink all the way down
    // (last unsubscribe) and building a fresh one.
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    await runRetry();
    latest().open(); // the rebuilt connection PAYS the debt
    expect(seen, "the outage's debt is paid on the rebuilt connection").toEqual([
      ...SSE_RESYNC_TOPICS,
    ]);

    off(); // last subscriber leaves → the whole downlink is torn down
    seen.length = 0;

    // A fresh session on a healthy station: every hook refetches on mount, so
    // this connection owes nothing and must fan NOTHING. If the debt were never
    // cleared, one permanent failure would condemn the rest of the session to a
    // full 13-topic resync on every connection it ever opens.
    const off2 = httpApi.subscribeEvents((t) => seen.push(t));
    expect(FakeEventSource.instances, "a genuinely new connection").toHaveLength(3);
    latest().open();
    expect(
      seen,
      "a first open with no outage behind it must not resync",
    ).toEqual([]);
    off2();
  });

  it("one subscriber throwing SYNCHRONOUSLY must not take down the resync, the other subscribers, or the state machine", async () => {
    // Adopted from the independent review (P3), which found this on the head
    // before this test existed. `resyncAll` fanned WITHOUT isolation while its
    // own comment claimed the subscribers' `.catch` covered it — a `.catch`
    // covers a rejected promise and is no defence against a synchronous throw.
    //
    // The third assertion is the one that made this worth blocking a merge on:
    // the throw escaped `es.onopen`, so `setSseState("live")` never ran and the
    // banner stayed up OVER A HEALTHY STREAM. This whole change exists to make
    // a dead connection visible; a banner lying in the other direction is the
    // same defect with the sign flipped.
    const offBad = httpApi.subscribeEvents(() => {
      throw new Error("a subscriber threw while handling a resync");
    });
    const seen: string[] = [];
    const offGood = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    await runRetry();
    latest().open();

    expect(
      seen,
      "one broken hook must not decide what the other subscribers learn",
    ).toEqual([...SSE_RESYNC_TOPICS]);
    expect(
      sseConnectionState(),
      "the stream is open and delivering — saying otherwise is the banner lying",
    ).toBe("live");

    // And the bookkeeping that runs after the fan is still reachable: the debt
    // was cleared, so the NEXT connection with nothing owed fans nothing.
    offBad();
    offGood();
    seen.length = 0;
    const off3 = httpApi.subscribeEvents((t) => seen.push(t));
    latest().open();
    expect(seen, "the debt was cleared despite the throw").toEqual([]);
    off3();
  });

  it("a throwing subscriber does not stop an ordinary delta reaching the others either", () => {
    const offBad = httpApi.subscribeEvents(() => {
      throw new Error("boom");
    });
    const seen: string[] = [];
    const offGood = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();

    FakeEventSource.instances[0].emit({ topic: "chat" });

    expect(seen, "the live delta path needs the same isolation as the resync").toEqual([
      "chat",
    ]);
    offBad();
    offGood();
  });
});

describe("httpApi · the probe's own contract (the parts a state-machine test does not reach)", () => {
  it("a probe that never answers is ENDED by its deadline — without one the whole recovery loop deadlocks", async () => {
    probeHangs = true;
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    // Fire the retry: the probe is now hanging, no timer, no EventSource.
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(probeCalls).toBe(1);
    expect(
      FakeEventSource.instances,
      "while the probe hangs nothing has reconnected — this is the deadlock window",
    ).toHaveLength(1);

    // The deadline is the only thing that can end it.
    await vi.advanceTimersByTimeAsync(SSE_PROBE_TIMEOUT_MS);
    await vi.advanceTimersByTimeAsync(0);
    expect(
      FakeEventSource.instances,
      "a timed-out probe reads as 'no answer' and the stream is rebuilt",
    ).toHaveLength(2);
    off();
  });

  // 🔴 THE TITLE USED TO READ「— 15s, derived from the wire contract」, which
  // round 4 read as a claim about the CONSTANT. It is not: 15000 is the
  // CEILING this asserts (spec §1's heartbeat bound), and SSE_PROBE_TIMEOUT_MS
  // is 8000, comfortably under it. A title that names a number the code does
  // not hold sends the next reader looking for a bug that is not there.
  it("the probe deadline is BOUNDED, not merely present — at most 15s, the spec §1 heartbeat ceiling", () => {
    // 🔴 A SEPARATE ASSERTION FROM THE ONE ABOVE, ON PURPOSE. That test proves a
    // deadline EXISTS, and it cannot prove anything more, because it advances
    // time BY THE CONSTANT ITSELF — push SSE_PROBE_TIMEOUT_MS to ten minutes and
    // it stays green while an eight-minute deadlock window opens up. A test that
    // imports the number it is checking is a tautology about that number.
    //
    // So the bound is written as a LITERAL, and it comes from the wire contract
    // rather than from taste: spec/sse.md §1 requires /api/events to open with
    // `: connected` immediately and to emit a heartbeat whenever the stream has
    // been quiet for 15 seconds. A probe that has not even received RESPONSE
    // HEADERS within that window is therefore outside anything a conforming
    // server is allowed to do — there is nothing left to wait for. Raising this
    // ceiling means claiming the server may take longer than its own contract
    // permits, which is a spec change, not a tuning knob.
    expect(SSE_PROBE_TIMEOUT_MS).toBeLessThanOrEqual(15000);
    expect(SSE_PROBE_TIMEOUT_MS).toBeGreaterThan(0);
  });

  it("the probe ABORTS the stream it opened — it wanted the status line, not a second live connection", async () => {
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    await runRetry();

    expect(lastProbe, "the probe ran").not.toBe(null);
    expect(
      lastProbe?.init.signal?.aborted,
      "left un-aborted, every successful probe leaks a second SSE connection for the rest of the session",
    ).toBe(true);
    // And it identifies itself as a probe by a header, which an EventSource
    // cannot set — that is what makes it distinguishable from a real stream.
    expect(
      (lastProbe?.init.headers as Record<string, string>)["X-OC-SSE-Probe"],
    ).toBe("1");
    off();
  });

  it("no token is answered as UNAUTHORIZED without a request — and that verdict must reach the auth layer, not just this module", async () => {
    // Two halves, and the second is the one that has teeth. Answering "no
    // token" as `0` (offline) instead of `401` LOOKS harmless from inside this
    // module — `ensureSseSource` declines without a token either way, so the
    // state still lands on "unauthorized" and no stream is built. The
    // difference is entirely OUTSIDE: only the 401 arm calls
    // handleUnauthorized, which is what fires oc-auth-expired and drops the app
    // to the login wall. Get it wrong and the owner sits on a frozen page,
    // correctly labelled disconnected, with nothing telling him to log back in
    // and no path that ever will.
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    localStorage.removeItem(TOKEN_KEY);
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(probeCalls, "asking the server is pointless with no credential").toBe(0);
    expect(
      expired,
      "the session being unusable has to reach the auth layer, or nobody bounces to login",
    ).toHaveBeenCalledTimes(1);
    expect(sseConnectionState()).toBe("unauthorized");
    expect(FakeEventSource.instances).toHaveLength(1);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("a handler from a connection we already replaced is IGNORED — a late error must not kill the live stream", async () => {
    const off = httpApi.subscribeEvents(() => {});
    const first = FakeEventSource.instances[0];
    first.open();
    first.permanentError();
    await runRetry();
    const second = latest();
    second.open();
    expect(sseConnectionState()).toBe("live");

    // The dead socket fires one last error, late (browsers do this).
    first.permanentError();

    expect(
      sseSourceIsAlive(second),
      "the stale handler tore down the CURRENT connection",
    ).toBe(true);
    expect(sseConnectionState()).toBe("live");
    expect(FakeEventSource.instances).toHaveLength(2);
    off();
  });
});

describe("httpApi · a CONNECTION-STATE listener that throws must not be able to freeze the app", () => {
  // The state fan-out is the THIRD place this module hands control to code it
  // does not own, and the worst-placed of them: it sits directly upstream of the
  // retry scheduler. Adopted from independent review round 3, which found both
  // of these on the head before this describe existed.
  //
  // 🔑 What made this worth fixing even though it is unreachable TODAY (the one
  // consumer is ConnectionBanner's bare setState): the failure mode is not "the
  // banner misbehaves", it is THE ORIGINAL BUG OF THIS TICKET — a cockpit frozen
  // for good, with a banner promising a reconnect that nobody scheduled.

  it("a listener throwing on 'connecting' does NOT cancel the reconnect that was about to be scheduled", async () => {
    const off = httpApi.subscribeEvents(() => {});
    const offState = httpApi.subscribeConnection((s) => {
      if (s === "connecting") throw new Error("a state listener threw");
    });
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await vi.advanceTimersByTimeAsync(60000);
    await vi.advanceTimersByTimeAsync(0);

    expect(
      FakeEventSource.instances.length,
      "sixty seconds with no reconnect is the frozen cockpit this whole ticket is about",
    ).toBeGreaterThan(1);
    offState();
    off();
  });

  it("a listener throwing on 'idle' does NOT skip the teardown", () => {
    const off = httpApi.subscribeEvents(() => {});
    const es = FakeEventSource.instances[0];
    es.open();
    const offState = httpApi.subscribeConnection((s) => {
      if (s === "idle") throw new Error("a state listener threw");
    });

    expect(() => off()).not.toThrow();

    expect(
      es.closed,
      "the connection was left open with nobody listening — a phantom the server still counts",
    ).toBe(true);
    offState();
  });

  it("a listener that throws on its very FIRST call does not throw out of subscribeConnection", () => {
    // Not a loop, so an enumeration of the fan-outs does not find it: the
    // immediate call that hands a fresh subscriber the current state is a bare
    // invocation. It runs inside the subscriber's mount effect.
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    expect(() =>
      httpApi.subscribeConnection(() => {
        throw new Error("threw on the immediate first call");
      }),
    ).not.toThrow();
    off();
  });

  it("one throwing listener does not stop the OTHERS from hearing the state change", () => {
    const heard: string[] = [];
    const off = httpApi.subscribeEvents(() => {});
    const offBad = httpApi.subscribeConnection(() => {
      throw new Error("boom");
    });
    const offGood = httpApi.subscribeConnection((s) => heard.push(s));
    heard.length = 0;

    FakeEventSource.instances[0].open();

    expect(heard).toEqual(["live"]);
    offBad();
    offGood();
    off();
  });
});

describe("httpApi · the downlink's health is PUBLISHED (a frozen page must not look like a calm one)", () => {
  it("reports live → connecting → live across a permanent failure and its recovery", async () => {
    const states: string[] = [];
    const offState = httpApi.subscribeConnection((s) => states.push(s));
    // Fires immediately with the current state — a subscriber mounting mid-
    // outage must not have to wait for the next transition to learn about it.
    expect(states).toEqual(["idle"]);

    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    expect(states).toEqual(["idle", "connecting", "live"]);

    FakeEventSource.instances[0].permanentError();
    expect(states).toEqual(["idle", "connecting", "live", "connecting"]);

    await runRetry();
    latest().open();
    expect(states).toEqual([
      "idle",
      "connecting",
      "live",
      "connecting",
      "live",
    ]);

    off();
    offState();
  });

  it("a TRANSIENT drop is reported too — the browser retrying is still a window in which the page is not receiving", () => {
    const states: string[] = [];
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    const offState = httpApi.subscribeConnection((s) => states.push(s));
    expect(states).toEqual(["live"]);

    FakeEventSource.instances[0].transientError();
    expect(states).toEqual(["live", "connecting"]);

    FakeEventSource.instances[0].open();
    expect(states).toEqual(["live", "connecting", "live"]);
    offState();
    off();
  });
});
