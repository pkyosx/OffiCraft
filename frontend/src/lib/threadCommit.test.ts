// lib/threadCommit — the chat thread's only door (T-48).
//
// The door used to hold a page back until its WAITING reply cards were in hand.
// It no longer does: a card renders COLLAPSED at its final height from what the
// carrying message already says, so there is nothing to wait for (owner
// 2026-09-04). What survives here is the half that was never about cards — the
// generation ticket, the mirror, and the updater — plus the pin that a commit
// carrying waiting-card rows reaches the view WITHOUT reading a single card.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage, ReplyCard } from "../api/adapter";

const h = vi.hoisted(() => ({
  getReplyCard: vi.fn<(id: string) => Promise<ReplyCard>>(),
}));

vi.mock("../api", () => ({ api: { getReplyCard: h.getReplyCard } }));

import { useThreadCommit, type Thread } from "./threadCommit";

function waitingRow(id: string, cardId: string): ChatMessage {
  return {
    id,
    from: "m1",
    to: "owner",
    body: "要寄出嗎?",
    ts: 1,
    attachments: [],
    replyCardId: cardId,
    replyCardStatus: "waiting",
  };
}

const page = (...m: ChatMessage[]): Thread => ({
  messages: m,
  hasMore: false,
  gapSuspected: false,
  hasNewer: false,
});

beforeEach(() => {
  h.getReplyCard.mockReset();
});

describe("useThreadCommit", () => {
  it("puts a page carrying a WAITING card on screen without reading the card", async () => {
    // 🔴 THE STRONGEST FORM OF THE LAZY RULE, PINNED AT THE DOOR. Every message
    // in a thread reaches the view through here, so a prefill re-entering the
    // commit path — the exact shape T-48 shipped and then removed — is red here
    // before it is red anywhere else. The rows must land in the SAME turn, not
    // one await later.
    const { result } = renderHook(() => useThreadCommit());

    let landed: boolean | undefined;
    await act(async () => {
      landed = await result.current.commit(result.current.takeTicket(), () =>
        page(waitingRow("c-1", "rc-1")),
      );
    });

    expect(result.current.thread.messages.map((m) => m.id)).toEqual(["c-1"]);
    expect(landed).toBe(true);
    expect(
      h.getReplyCard,
      "a commit read a reply card — the thread is fetching cards again",
    ).not.toHaveBeenCalled();
  });

  it("drops a page a newer ticket has already committed past", async () => {
    // The caller's OWN await window (its fetch) is what puts pages out of
    // order: a load that started later can finish sooner. The commit re-asks
    // the ticket at the moment it writes, so the older page is dropped rather
    // than spliced on top of the newer thread.
    const { result } = renderHook(() => useThreadCommit());

    const older = result.current.takeTicket();
    const newer = result.current.takeTicket();

    await act(async () => {
      await result.current.commit(newer, () => page(waitingRow("c-new", "rc-2")));
    });
    await waitFor(() =>
      expect(result.current.thread.messages.map((m) => m.id)).toEqual(["c-new"]),
    );

    let olderOk: boolean | undefined;
    await act(async () => {
      olderOk = await result.current.commit(older, () =>
        page(waitingRow("c-old", "rc-1")),
      );
    });

    expect(olderOk).toBe(false);
    expect(
      result.current.thread.messages.map((m) => m.id),
      "the overtaken page must not land on top of the newer one",
    ).toEqual(["c-new"]);
  });

  it("advances the mirror at the commit, not at the next render", async () => {
    // Every consumer reads `current()` as "the freshest thread"; a walk that
    // asks twice from the same anchor because the mirror had not caught up is a
    // measured defect (the duplicate `?start_id=` pair, 8ms apart).
    const { result } = renderHook(() => useThreadCommit());
    let mirrorAfterCommit: string[] = [];

    await act(async () => {
      await result.current.commit(result.current.takeTicket(), () =>
        page(waitingRow("c-1", "rc-1")),
      );
      mirrorAfterCommit = result.current.current().messages.map((m) => m.id);
    });

    expect(mirrorAfterCommit).toEqual(["c-1"]);
  });

  it("hands React the UPDATER, so an un-ticketed history page inside the window survives", async () => {
    // `mergeHistory` takes no ticket and writes through an updater. Committing
    // the computed OBJECT would silently eat it (measured: 30 loaded rows
    // vanished). The commit's updater is re-run against React's own `prev`.
    const { result } = renderHook(() => useThreadCommit());
    const seq = result.current.takeTicket();

    await act(async () => {
      const commitP = result.current.commit(seq, (prev) => ({
        ...prev,
        messages: [...prev.messages, waitingRow("c-new", "rc-1")],
      }));
      await result.current.mergeHistory((prev) => ({
        ...prev,
        messages: [waitingRow("c-old", "rc-0"), ...prev.messages],
      }));
      await commitP;
    });

    expect(result.current.thread.messages.map((m) => m.id)).toEqual([
      "c-old",
      "c-new",
    ]);
  });

  it("clear() empties the thread synchronously and cannot express a message", () => {
    const { result } = renderHook(() => useThreadCommit());
    act(() => {
      result.current.clear();
    });
    // Synchronous by construction: `clear` takes no parameters, so there is
    // nothing for it to await — and an await here would paint one extra frame
    // of the conversation the owner has just left.
    expect(result.current.thread.messages).toEqual([]);
  });
});
