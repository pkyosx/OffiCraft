// useChatUnread — which SSE topics the 辦公室 nav unread badge refetches on
// (T-b86c: 父層「辦公室」徽章落後子層「正職」, owner 手機實見, 需手動重整才校正).
//
// The office total is Σ unread over the LIVE set = non-removed members ∪ live
// outsource workers (server api_chat.go HandleChatUnreadCount's live[] filter).
// So the total moves not only on a new message / read ("chat" / "chat_read")
// but ALSO when the live SET changes — a member removed/added ("member") or a
// worker spawned/released ("outsource_worker"). The sibling 正職/外包 tab hooks
// already subscribe to those, so they self-healed; this badge subscribed to
// ONLY chat/chat_read, went deaf to roster/worker lifecycle, and the two badges
// diverged until a manual reload.
//
// These pins assert the wiring against the EXPORTED OFFICE_TOTAL_TOPICS set
// itself (fail-closed) rather than a hand-copied list: the set is the single
// source of truth for "what moves the badge", so adding a topic there is one
// edit and the coverage below picks it up automatically.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getChatUnreadCount: vi.fn<() => Promise<number>>(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    getChatUnreadCount: h.getChatUnreadCount,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useChatUnread, OFFICE_TOTAL_TOPICS } from "./useChatUnread";

beforeEach(() => {
  h.getChatUnreadCount.mockReset().mockResolvedValue(0);
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useChatUnread", () => {
  it("fetches the unread count once on mount", async () => {
    renderHook(() => useChatUnread());
    await waitFor(() =>
      expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1),
    );
  });

  // The load-bearing EXISTENCE assertion: the lifecycle topics MUST be in the
  // set. The it.each below is self-referential — it iterates the set itself, so
  // dropping "member" merely drops that case and stays green; it CANNOT catch a
  // removal. This pins the removal directly: drop "member"/"outsource_worker"
  // from OFFICE_TOTAL_TOPICS and this reddens (the pre-fix bug). The removal is
  // ALSO guarded end-to-end by useChatUnread.foreground.test.ts's hardcoded
  // emit("member") / emit("outsource_worker").
  it("OFFICE_TOTAL_TOPICS contains the lifecycle topics that move the office total", () => {
    for (const t of ["chat", "chat_read", "member", "outsource_worker"]) {
      expect(OFFICE_TOTAL_TOPICS.has(t)).toBe(true);
    }
  });

  // Fail-closed coverage of the CURRENT set: every topic the hook declares as
  // total-moving actually triggers a refetch (guards a hand-copied list from
  // drifting off the set). This proves the members that ARE present are wired;
  // catching a REMOVAL is the existence assertion above, catching a wrong
  // ADDITION is the 'monitoring' negative control below.
  it.each([...OFFICE_TOTAL_TOPICS])(
    "refetches on the '%s' delta (a topic in OFFICE_TOTAL_TOPICS)",
    async (topic) => {
      renderHook(() => useChatUnread());
      await waitFor(() =>
        expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1),
      );
      emit(topic);
      await waitFor(() =>
        expect(h.getChatUnreadCount).toHaveBeenCalledTimes(2),
      );
    },
  );

  it("does NOT refetch on 'monitoring' (a specific topic that cannot change the office total)", async () => {
    renderHook(() => useChatUnread());
    await waitFor(() =>
      expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1),
    );
    // A single NAMED counter-example — not a "no needless refetch" catch-all
    // whose name would overstate what it guards. MUTANT: add "monitoring" to
    // OFFICE_TOTAL_TOPICS and this goes red.
    emit("monitoring");
    await new Promise((r) => setTimeout(r, 20));
    expect(h.getChatUnreadCount).toHaveBeenCalledTimes(1);
  });
});
