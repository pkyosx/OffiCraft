// T-b0bb — the bar that makes a dead downlink VISIBLE.
//
// The whole point of the component is negative space: it must be absent when
// the stream is healthy, absent while a routine blip is being retried, and
// PRESENT once the page has genuinely stopped receiving. A banner that is
// always there is as useless as one that is never there, so both halves are
// asserted here — and the "still absent during the grace window" case is the
// one that keeps a well-meaning simplification (drop the delay, show it
// instantly) from turning the bar into wallpaper.
//
// Real ConnectionBanner + real i18n; only the api seam is faked, so the
// component is driven through exactly the `subscribeConnection` contract the
// transport implements.

// MEASURED MUTANTS (re-runnable by hand against ConnectionBanner.tsx):
//   replace the grace `setTimeout` with a bare `setShowing(true)`
//     → reddens "stays silent through a SHORT drop"
//   delete the `setShowing(false)` in the non-"connecting" arm
//     → reddens "clears the moment the stream comes back"
//   swallow the effect's cleanup — `useEffect(() => api.subscribeConnection(
//     setState), [])` → `useEffect(() => { api.subscribeConnection(setState); },
//     [])` → reddens "unsubscribes on unmount" (added round 4; see below)

// 🔴 WHY THE FAKE COUNTS INSTEAD OF REMEMBERING. The first version of this
// seam stored the callback in a single slot (`push = cb`) and cleared it on
// unsubscribe. That shape is STRUCTURALLY BLIND to the bug it was standing in
// front of: a second mount overwrites the slot, so a leaked subscriber and a
// clean one look identical, and dropping the effect's cleanup entirely left
// the whole tree green (measured, review round 4). React returns the cleanup
// from `useEffect` only if the effect RETURNS it — a stray pair of braces
// silently discards it — so every mount would leak a live subscriber and, in a
// StrictMode/remount world, call `setState` on an unmounted component forever.
// A test can only see that if the fake keeps a SET and the test asserts its
// SIZE; "did somebody unsubscribe" is a counting question, not a memory one.
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import type { SseConnectionState } from "../api/adapter";

/** Every subscriber currently registered through the fake seam — the real
 * transport keeps a Set too (api/http.ts `sseStateSubscribers`), so this is
 * the contract's shape, not a testing convenience. */
const subscribers = new Set<(s: SseConnectionState) => void>();
let current: SseConnectionState = "live";

/** Deliver a state to whoever is listening, exactly as the transport fans. */
const push = (s: SseConnectionState): void => {
  for (const cb of [...subscribers]) cb(s);
};

vi.mock("../api", () => ({
  api: {
    subscribeConnection(cb: (s: SseConnectionState) => void) {
      subscribers.add(cb);
      cb(current);
      return () => {
        subscribers.delete(cb);
      };
    },
  },
}));

import { ConnectionBanner, CONNECTION_BANNER_GRACE_MS } from "./ConnectionBanner";

function mount() {
  return render(
    <I18nProvider>
      <ConnectionBanner />
    </I18nProvider>
  );
}

function set(state: SseConnectionState): void {
  act(() => {
    push(state);
  });
}

/** Push time past the grace window and let the state settle. */
function waitOutGrace(): void {
  act(() => {
    vi.advanceTimersByTime(CONNECTION_BANNER_GRACE_MS + 1);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  current = "live";
  subscribers.clear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ConnectionBanner", () => {
  it("says NOTHING while the stream is live — a healthy cockpit carries no chrome", () => {
    const { container } = mount();
    waitOutGrace();
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("stays silent through a SHORT drop: the browser's own retry must not strobe a warning at the owner", () => {
    const { container } = mount();
    set("connecting");
    act(() => {
      vi.advanceTimersByTime(CONNECTION_BANNER_GRACE_MS - 1);
    });
    expect(container.querySelector(".status-banner")).toBe(null);

    set("live");
    waitOutGrace();
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("SHOWS the bar once the page has really stopped receiving, and says what that means for what is on screen", () => {
    const { container, getByRole } = mount();
    set("connecting");
    waitOutGrace();

    const bar = container.querySelector(".status-banner");
    expect(bar).not.toBe(null);
    // The consequence, not just the fact: "disconnected" alone leaves the owner
    // to guess whether the page in front of him is still trustworthy.
    expect(bar?.textContent).toContain("即時更新已中斷");
    expect(bar?.textContent).toContain("畫面上的內容可能不是最新的");
    // Announced, not merely drawn.
    expect(getByRole("status")).toBe(bar);
  });

  it("clears the moment the stream comes back — recovery is as visible as the loss", () => {
    const { container } = mount();
    set("connecting");
    waitOutGrace();
    expect(container.querySelector(".status-banner")).not.toBe(null);

    set("live");
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("says nothing when the session is dead: the login wall is already the message", () => {
    const { container } = mount();
    set("unauthorized");
    waitOutGrace();
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("says nothing when idle (logged out / torn down) — no subscriber is not a fault", () => {
    const { container } = mount();
    set("idle");
    waitOutGrace();
    expect(container.querySelector(".status-banner")).toBe(null);
  });

  it("unsubscribes on unmount — a leaked subscriber outlives the component and setStates into a corpse", () => {
    const { unmount } = mount();
    expect(subscribers.size, "the bar is listening while mounted").toBe(1);

    unmount();
    expect(
      subscribers.size,
      "the transport keeps a Set: whatever is left here is fanned to forever",
    ).toBe(0);

    // And the corpse is not merely absent from the Set — pushing after unmount
    // must reach nobody at all. This is the assertion the old single-slot fake
    // could not make, because overwriting one slot hides an unbounded leak.
    expect(() => push("connecting")).not.toThrow();

    // Mount/unmount repeatedly: the count must come back to zero every time,
    // not creep. One leak per mount is exactly how this fails in production,
    // where the bar mounts once per App mount and StrictMode doubles it.
    for (let i = 0; i < 3; i += 1) {
      const view = mount();
      expect(subscribers.size).toBe(1);
      view.unmount();
      expect(subscribers.size, `leak after mount ${i + 1}`).toBe(0);
    }
  });

  it("keeps the manual escape hatch one click away while the automatic one is still trying", () => {
    const reload = vi.fn();
    const original = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...original, reload },
    });
    const { getByText } = mount();
    set("connecting");
    waitOutGrace();

    act(() => {
      getByText("重新整理").click();
    });
    expect(reload).toHaveBeenCalledTimes(1);

    Object.defineProperty(window, "location", {
      configurable: true,
      value: original,
    });
  });
});
