// Every theme action must announce that the roster's resolved faces may have
// moved (T-698b).
//
// This is the SENDING half of the fix; `hooks/useMembers.themeAvatars.test.ts`
// pins the receiving half. They are deliberately separate files because they
// fail for different reasons: this one goes red when a new theme path forgets
// to announce, that one goes red when the roster stops listening. A single
// end-to-end test would go red for either and tell you neither.
//
// WHY IT MATTERS: a member's face is stored per (member, theme) but the wire
// carries only the answer resolved for the ACTIVE theme. All three actions
// below change that answer without writing a member row, so no `member` SSE
// delta is fanned and nothing else tells the roster to look again.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { __resetMock } from "../api/mock";
import { setToken } from "../api/auth";
import { THEME_AVATARS_CHANGED_EVENT } from "../lib/themeAvatarsChanged";

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

const AURORA = {
  id: "aurora",
  name: "Aurora",
  colors: { "--color-accent": "#00ffcc" },
};

let announced: () => void;

beforeEach(async () => {
  __resetMock();
  localStorage.clear();
  setToken("test-token");
  announced = vi.fn();
  window.addEventListener(THEME_AVATARS_CHANGED_EVENT, announced);
  await act(async () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
  });
  (announced as unknown as ReturnType<typeof vi.fn>).mockClear?.();
});

afterEach(() => {
  window.removeEventListener(THEME_AVATARS_CHANGED_EVENT, announced);
});

const calls = () =>
  (announced as unknown as ReturnType<typeof vi.fn>).mock.calls.length;

describe("theme actions announce that resolved faces may have moved", () => {
  it("switching themes announces", async () => {
    // Store it first so the switch takes the real path (a switch to a theme
    // that does not exist would announce for the wrong reason).
    await act(async () => {
      await ctx.saveTheme(AURORA);
    });
    (announced as unknown as ReturnType<typeof vi.fn>).mockClear();

    await act(async () => {
      ctx.setTheme("aurora");
    });
    // The switch alone is enough: every card is holding an id resolved against
    // the theme being left, and that id does not exist in the new pool.
    await waitFor(() => expect(calls()).toBeGreaterThanOrEqual(1));
  });

  it("saving a theme announces", async () => {
    await act(async () => {
      await ctx.saveTheme(AURORA);
    });
    // A pool edit can delete the very image a member had chosen; the server
    // prunes that association on this write.
    await waitFor(() => expect(calls()).toBeGreaterThanOrEqual(1));
  });

  it("deleting a theme announces", async () => {
    await act(async () => {
      await ctx.saveTheme(AURORA);
    });
    (announced as unknown as ReturnType<typeof vi.fn>).mockClear();

    await act(async () => {
      await ctx.removeTheme("aurora");
    });
    // Deleting a theme drops every selection recorded against it.
    await waitFor(() => expect(calls()).toBeGreaterThanOrEqual(1));
  });
});
