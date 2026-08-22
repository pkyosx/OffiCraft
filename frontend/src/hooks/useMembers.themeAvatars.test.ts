// The roster must re-read itself when a theme action changes which image each
// member resolves to (T-698b).
//
// WHY THIS IS NOT COVERED BY THE SSE PINS NEXT DOOR. A member's face is stored
// per (member, theme), but the wire carries only the resolved answer for the
// ACTIVE theme. Switching themes, editing a pool and deleting a theme all
// change that answer WITHOUT writing a member row — so the server fans no
// `member` delta and the SSE path cannot notice. Before this fix the roster
// kept the ids it resolved under the previous theme; none of them exist in the
// new theme's pool, so every card fell back to that pool's FIRST image rather
// than the one the member actually has recorded there. Correct data, wrong
// picture — the exact silent face-swap the per-theme model was built to end.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listMembers: vi.fn<(opts?: { light?: boolean }) => Promise<unknown[]>>(),
}));

vi.mock("../api", () => ({
  api: {
    listMembers: h.listMembers,
    subscribeEvents: () => () => {},
  },
}));

import { useMembers } from "./useMembers";
import { THEME_AVATARS_CHANGED_EVENT } from "../lib/themeAvatarsChanged";

beforeEach(() => {
  h.listMembers.mockReset().mockResolvedValue([]);
});

function announceThemeAvatarsChanged() {
  act(() => {
    window.dispatchEvent(new Event(THEME_AVATARS_CHANGED_EVENT));
  });
}

describe("useMembers × theme actions", () => {
  it("re-pulls the whole roster when the resolved faces may have moved", async () => {
    renderHook(() => useMembers());
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));

    announceThemeAvatarsChanged();

    // A FULL re-pull, not a per-member patch: the change is roster-wide by
    // nature (every card re-resolves against a different pool), so there is no
    // single id to patch and no delta naming one.
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(2));
  });

  it("keeps listening across repeated theme actions", async () => {
    renderHook(() => useMembers());
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));

    // Switch, edit a pool, delete a theme — three separate announcements. A
    // one-shot listener would leave the roster stale from the second one on.
    announceThemeAvatarsChanged();
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(2));
    announceThemeAvatarsChanged();
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(3));
    announceThemeAvatarsChanged();
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(4));
  });

  it("stops listening once the roster unmounts", async () => {
    const { unmount } = renderHook(() => useMembers());
    await waitFor(() => expect(h.listMembers).toHaveBeenCalledTimes(1));

    unmount();
    announceThemeAvatarsChanged();

    // No refetch after unmount: a leaked listener would keep calling the API
    // for a roster nobody is rendering.
    await new Promise((r) => setTimeout(r, 20));
    expect(h.listMembers).toHaveBeenCalledTimes(1);
  });
});
