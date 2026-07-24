// enterShouldSend — the shared "does this Enter send?" rule for every multi-line
// composer. Pinned here so the three surfaces (chat / reply / task message) can
// only drift by breaking this one test.

import { describe, it, expect } from "vitest";
import type { KeyboardEvent } from "react";
import { enterShouldSend } from "./composerKeys";

type FakeKey = {
  key?: string;
  shiftKey?: boolean;
  keyCode?: number;
  isComposing?: boolean;
};

// Minimal stand-in for a React keyboard event — only the fields enterShouldSend
// reads. Cast to the real type at the call site so the helper's signature is
// still honoured.
function keyEvent(f: FakeKey): KeyboardEvent<HTMLTextAreaElement> {
  return {
    key: f.key ?? "Enter",
    shiftKey: f.shiftKey ?? false,
    keyCode: f.keyCode ?? 0,
    nativeEvent: { isComposing: f.isComposing ?? false },
  } as unknown as KeyboardEvent<HTMLTextAreaElement>;
}

const DESKTOP = { isMobile: false, composing: false };
const MOBILE = { isMobile: true, composing: false };

describe("enterShouldSend", () => {
  it("desktop: a bare Enter sends", () => {
    expect(enterShouldSend(keyEvent({ key: "Enter" }), DESKTOP)).toBe(true);
  });

  it("desktop: Shift+Enter does NOT send (native newline)", () => {
    expect(
      enterShouldSend(keyEvent({ key: "Enter", shiftKey: true }), DESKTOP),
    ).toBe(false);
  });

  it("desktop: a non-Enter key does not send", () => {
    expect(enterShouldSend(keyEvent({ key: "a" }), DESKTOP)).toBe(false);
  });

  it("mobile: a bare Enter does NOT send (falls through to native newline)", () => {
    expect(enterShouldSend(keyEvent({ key: "Enter" }), MOBILE)).toBe(false);
  });

  it("IME: an Enter confirming a candidate never sends — native isComposing flag", () => {
    expect(
      enterShouldSend(keyEvent({ key: "Enter", isComposing: true }), DESKTOP),
    ).toBe(false);
  });

  it("IME: an Enter with the 229 composition keyCode never sends", () => {
    expect(
      enterShouldSend(keyEvent({ key: "Enter", keyCode: 229 }), DESKTOP),
    ).toBe(false);
  });

  it("IME: the caller's composing ref gates a send even when the native flags are clear", () => {
    expect(
      enterShouldSend(keyEvent({ key: "Enter" }), {
        isMobile: false,
        composing: true,
      }),
    ).toBe(false);
  });
});
