// useChatUnread END-TO-END convergence (T-b86c) — the owner-visible contract.
//
// The sibling useChatUnread.test.ts pins ONE link (which topics call refetch,
// against a FAKE subscribeEvents). http.sse-pool.test.ts pins ANOTHER (the real
// http.ts fans a full resync on foreground restore). Neither chains them: none
// asserts that, with NO manual reload, the 辦公室 badge's RENDERED count actually
// moves off its stale value to the fresh backend truth. That end-to-end path —
// SSE/foreground event → real http.ts fan → useChatUnread refetch → setState →
// count — is where a refetch-fires-but-count-never-updates wiring break hides,
// and it is exactly what owner sees (數字 stuck until reload). This file mounts
// the REAL http.ts downlink (FakeEventSource) behind useChatUnread and drives it
// with the same event shapes owner's phone produces.
//
// Why a jsdom integration test and NOT a real-browser CT: the CT config
// (playwright-ct.config.ts) exists for LAYOUT — jsdom has no layout engine, so
// geometry is invisible to it. This bug is not layout; it is event-driven state
// (visibilitychange/SSE → refetch → count). That causal core is fully reachable
// in jsdom (real dispatched events, real hook state), so a real browser buys no
// discriminating power here that jsdom lacks. What NEITHER layer can reach is
// whether a real iOS background PAUSES-not-drops the connection so this
// foreground path is the one that actually runs — that link is owner-only
// (see the acceptance card's honest gap section).
//
// Convergence direction (父≥子) is a backend invariant, not a thing this hook
// can violate: office total = Σ unread over the LIVE set ⊇ each sub-tab's set
// (api_chat.go HandleChatUnreadCount). The front-end defect is purely temporal —
// the parent going deaf to an event and lagging. So the falsifiable assertion
// below is "a total-moving event, no reload → the badge leaves its stale value
// and lands on the fresh backend truth", with a revert-the-fix mutant pinning
// each guard red.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { httpApi } from "../api/http";

// Drive useChatUnread through the REAL http.ts seam (subscribeEvents +
// getChatUnreadCount are the two api surfaces it touches). getChatUnreadCount is
// stubbed so we control the "backend truth" it converges to; subscribeEvents is
// the genuine pooled downlink under test (foreground handler, resyncAll, fan).
const h = vi.hoisted(() => ({
  getChatUnreadCount: vi.fn<() => Promise<number>>(),
}));

vi.mock("../api", () => ({
  api: {
    getChatUnreadCount: h.getChatUnreadCount,
    subscribeEvents: httpApi.subscribeEvents,
  },
}));

import { useChatUnread } from "./useChatUnread";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close(): void {
    this.closed = true;
  }
  emit(topic: string): void {
    this.onmessage?.({ data: JSON.stringify({ topic }) } as MessageEvent);
  }
}

function goForeground() {
  Object.defineProperty(document, "visibilityState", {
    value: "visible",
    configurable: true,
  });
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  localStorage.setItem("oc_token", "test-owner-jwt");
  h.getChatUnreadCount.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.removeItem("oc_token");
});

describe("useChatUnread — end-to-end convergence with no reload (T-b86c)", () => {
  // A: a roster/worker lifecycle delta (member / outsource_worker) changes the
  // office total. Pre-fix the badge subscribed to only chat/chat_read and stayed
  // stale. MUTANT: drop "member" from OFFICE_TOTAL_TOPICS → count stays 8.
  it("a 'member' delta moves the badge from its stale value to the fresh backend truth", async () => {
    h.getChatUnreadCount.mockResolvedValue(8);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(8));

    // A member is removed → backend office total is now 10. No reload happens.
    h.getChatUnreadCount.mockResolvedValue(10);
    act(() => {
      FakeEventSource.instances[0].emit("member");
    });

    await waitFor(() => expect(result.current).toBe(10));
  });

  it("an 'outsource_worker' delta also moves the badge (worker spawn/release changes live set)", async () => {
    h.getChatUnreadCount.mockResolvedValue(8);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(8));

    h.getChatUnreadCount.mockResolvedValue(9);
    act(() => {
      FakeEventSource.instances[0].emit("outsource_worker");
    });

    await waitFor(() => expect(result.current).toBe(9));
  });

  // B: the owner-reported path. The connection was PAUSED in the background (no
  // reconnect, so onopen never re-fired); deltas were missed. On return to the
  // foreground the http.ts handler fans a full resync → useChatUnread refetches
  // → the badge converges WITHOUT a reload. MUTANT: drop the visibilitychange
  // listener in http.ts → goForeground() no longer fans → count stays 8.
  it("returning to the foreground converges the badge with NO reload (owner's 切走再切回 case)", async () => {
    h.getChatUnreadCount.mockResolvedValue(8);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(8));

    // While backgrounded the true total moved to 10; no event was delivered
    // (paused connection) and the user did NOT reload.
    h.getChatUnreadCount.mockResolvedValue(10);
    goForeground();

    await waitFor(() => expect(result.current).toBe(10));
  });

  // Control: a NON-total-moving event must NOT drag the badge to a new value.
  // Without this, "the badge changed" could be an artifact of refetching on
  // everything. MUTANT: add "monitoring" to OFFICE_TOTAL_TOPICS → this reddens.
  it("a 'monitoring' delta does NOT refetch, so the badge holds its value", async () => {
    h.getChatUnreadCount.mockResolvedValue(8);
    const { result } = renderHook(() => useChatUnread());
    await waitFor(() => expect(result.current).toBe(8));

    h.getChatUnreadCount.mockResolvedValue(99);
    act(() => {
      FakeEventSource.instances[0].emit("monitoring");
    });
    await new Promise((r) => setTimeout(r, 30));

    expect(result.current).toBe(8);
  });
});
