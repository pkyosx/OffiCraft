// useReplyCards — the reply-card STATE-RACE regression (T-e862).
//
// The reported bug: the nav badge showed 「2」 but the 等我回覆 list rendered
// only ONE card; a manual refresh brought the missing card back. Two root
// causes, both pinned here:
//
//  ① LAST-WRITE-WINS on the waiting refetch. A resolve + its own reply_card
//     fan-out + a peer's new-card delta each fire an independent, unordered,
//     un-aborted refetchWaiting, each ending in a bare setWaiting(w). When an
//     EARLY-fired refetch resolves LATE with a STALER snapshot, it clobbers a
//     NEWER, fuller one already committed — silently dropping a card. The fix
//     is a per-refetch generation id: a superseded (stale) response is
//     dropped, never committed. → `keeps both cards when a stale waiting
//     refetch resolves out of order`.
//
//  ② NON-SAME-SOURCE badge vs list. The badge rode a separate count-endpoint
//     fetch on a separate hook + subscription, so it and the list sat on two
//     different snapshots and could show different numbers. The badge is now
//     literally the length of the SAME authoritative waiting array. → `the
//     badge count always equals the rendered list length (same source)`.
//
// PROOF THIS TEST BITES: with the generation guard removed from
// refetchWaiting (the pre-fix behavior), the out-of-order case commits the
// stale [B] snapshot and the first assertion reddens.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import type { ReplyCard } from "../api/adapter";

const h = vi.hoisted(() => ({
  listReplyCards: vi.fn<(status: string) => Promise<ReplyCard[]>>(),
  getReplyCardCount: vi.fn(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listReplyCards: h.listReplyCards,
    getReplyCardCount: h.getReplyCardCount,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { ReplyCardsProvider, useReplyCards, useReplyCardWaitingCount } from "./useReplyCards";

function card(id: string): ReplyCard {
  return {
    id,
    from: "mira",
    kind: "decision",
    summary: `q-${id}`,
    body: "",
    options: [],
    status: "waiting",
    attachments: [],
    createdTs: 1000,
    answeredTs: null,
    chatMessageId: `msg-${id}`,
    answer: null,
  };
}

/** A manually-resolvable promise so the test controls RESOLUTION ORDER. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

// One probe renders BOTH the page's waiting list and the badge's count off the
// shared provider, so a single DOM read compares the two sources.
function Probe() {
  const { waiting } = useReplyCards();
  const badge = useReplyCardWaitingCount();
  return (
    <div>
      <span data-testid="list-ids">{waiting.map((c) => c.id).join(",")}</span>
      <span data-testid="list-len">{waiting.length}</span>
      <span data-testid="badge">{badge}</span>
    </div>
  );
}

function renderProvider() {
  return render(
    <ReplyCardsProvider>
      <Probe />
    </ReplyCardsProvider>
  );
}

beforeEach(() => {
  h.listReplyCards.mockReset();
  h.getReplyCardCount.mockReset().mockResolvedValue({
    waiting: 0,
    answered: 0,
    expired: 0,
  });
  h.sseHandler = null;
});

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

describe("useReplyCards state race (T-e862)", () => {
  it("keeps both cards when a stale waiting refetch resolves out of order", async () => {
    // A queue of deferreds — one per listReplyCards("waiting") call — so we
    // drive the RESOLUTION ORDER independently of the CALL ORDER.
    const calls: Array<ReturnType<typeof deferred<ReplyCard[]>>> = [];
    h.listReplyCards.mockImplementation(() => {
      const d = deferred<ReplyCard[]>();
      calls.push(d);
      return d.promise;
    });

    renderProvider();

    // Mount fetch → [A]. Settle the initial state.
    await waitFor(() => expect(calls.length).toBe(1));
    await act(async () => {
      calls[0].resolve([card("A")]);
    });
    await waitFor(() =>
      expect(screen.getByTestId("list-ids").textContent).toBe("A")
    );

    // Two more refetches fire (a resolve's fan-out + a peer's new card). R_early
    // is fired FIRST, R_late SECOND.
    emit("reply_card"); // R_early
    emit("reply_card"); // R_late
    await waitFor(() => expect(calls.length).toBe(3));

    // THE RACE: the late-FIRED refetch resolves FIRST with the fuller, correct
    // snapshot [A, B]; the early-FIRED one resolves AFTER with a STALE [B].
    await act(async () => {
      calls[2].resolve([card("A"), card("B")]);
    });
    await act(async () => {
      calls[1].resolve([card("B")]); // stale — must be dropped, not committed
    });

    // With sequencing, the stale [B] is discarded and the list keeps [A, B].
    // Pre-fix (last-write-wins) this would settle to [B] and drop A → red.
    await waitFor(() =>
      expect(screen.getByTestId("list-ids").textContent).toBe("A,B")
    );
    expect(screen.getByTestId("list-len").textContent).toBe("2");
  });

  it("the badge count always equals the rendered list length (same source)", async () => {
    // Badge and list read the SAME waiting array, so whatever the list shows,
    // the badge shows its length — never a 'badge 2 / list 1' split.
    h.listReplyCards.mockResolvedValue([card("A"), card("B")]);

    renderProvider();

    await waitFor(() =>
      expect(screen.getByTestId("list-len").textContent).toBe("2")
    );
    expect(screen.getByTestId("badge").textContent).toBe(
      screen.getByTestId("list-len").textContent
    );

    // A delta that refetches to a single card keeps them in lock-step.
    h.listReplyCards.mockResolvedValue([card("A")]);
    emit("reply_card");
    await waitFor(() =>
      expect(screen.getByTestId("list-len").textContent).toBe("1")
    );
    expect(screen.getByTestId("badge").textContent).toBe("1");
  });
});
