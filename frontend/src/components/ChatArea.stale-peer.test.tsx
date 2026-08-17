// T-0c20 — a post-send refetch that lands AFTER the owner switched conversations
// must not repaint the window with the previous peer's thread.
//
// The shape this pins (reproduced on origin/main before the fix): send in A's
// window → the post-send `listChat(A)` is still in flight → the owner clicks B →
// A's page lands. `useChat`'s refetch had no peer guard, and its else-arm wrote
// `{ peer: A, messages: <A's thread> }` — so B's window (header, composer,
// roster selection all still B) rendered A's whole conversation, and stayed that
// way until some later event for B overwrote it.
//
// Real ChatArea + real useChat; the api seam is faked so listChat for one peer
// can be held in flight on purpose. Every case asserts on the rendered
// bubbles, because that is the thing the owner reported. Two of them send
// WITHOUT switching peer, so they exercise the arm of refetch's updater that
// does commit: the sent line must appear, and already-loaded scrollback must
// survive the merge.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

const OWNER = "owner";
const KYLE = "m-f663f3c5de9a";
const WORKER = "ow-c2ca1a3358c4";

const log: ChatMessage[] = [];
let held: null | (() => void) = null;
let holdPeer = "";

// Mirrors the server's `?with=` filter (sender OR recipient), which is the
// authority for "which messages belong to this window".
function threadOf(peer: string): ChatMessage[] {
  return log.filter((m) => m.from === peer || m.to === peer);
}

vi.mock("../api", () => ({
  api: {
    listChat: async (
      withId: string,
      limit?: number,
      cursor?: { beforeTs: number; beforeId: string },
    ) => {
      if (withId === holdPeer) {
        await new Promise<void>((r) => {
          held = r;
        });
      }
      const all = threadOf(withId);
      const page = limit ?? 30;
      if (cursor) {
        // Keyset history page, mirroring the server: strictly older than the
        // cursor, newest `limit` of those.
        const older = all.filter(
          (m) =>
            m.ts < cursor.beforeTs ||
            (m.ts === cursor.beforeTs && m.id < cursor.beforeId),
        );
        return older.slice(-page);
      }
      return all.slice(-page);
    },
    peekChat: async (withId: string) => threadOf(withId).slice(-30),
    listChatReads: async () => [],
    markChatRead: async () => {},
    postChat: async (m: { to: string; body: string }) => {
      log.push({
        id: `s${log.length}`,
        from: OWNER,
        to: m.to,
        body: m.body,
        ts: 2000 + log.length,
        attachments: [],
        replyCardId: null,
      });
      return log[log.length - 1];
    },
    subscribeEvents: () => () => {},
    getOutsourceWorker: async () => ({}),
  },
}));

function mkMember(id: string, name: string, kind: Member["kind"]): Member {
  return {
    id,
    name,
    role: "assistant",
    roleName: "",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind,
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

const kyle = mkMember(KYLE, "Kyle", "assistant");
const worker = mkMember(WORKER, "外包 · O-148", "outsource");

function view(m: Member) {
  return (
    <I18nProvider>
      <ChatArea member={m} members={[kyle]} workers={[]} onWake={undefined} />
    </I18nProvider>
  );
}

function bubbles(container: HTMLElement): (string | null)[] {
  return Array.from(container.querySelectorAll(".chat__msg-bubble")).map(
    (n) => n.textContent,
  );
}

beforeEach(() => {
  log.length = 0;
  held = null;
  holdPeer = "";
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
  // useChat takes the read-marking listChat path only while the window is
  // active; jsdom reports the document as unfocused, which would send every
  // load down peekChat instead.
  document.hasFocus = () => true;
  log.push(
    {
      id: "k1",
      from: KYLE,
      to: OWNER,
      body: "KYLE-DM",
      ts: 1000,
      attachments: [],
      replyCardId: null,
    },
    {
      id: "i1",
      from: KYLE,
      to: WORKER,
      body: "KYLE-TO-WORKER",
      ts: 1002,
      attachments: [],
      replyCardId: null,
    },
    {
      id: "w1",
      from: WORKER,
      to: OWNER,
      body: "WORKER-DM",
      ts: 1003,
      attachments: [],
      replyCardId: null,
    },
  );
});

describe("ChatArea — a stale post-send refetch never repaints this window", () => {
  it("keeps the worker window on the worker's thread when the send-refetch for the PREVIOUS peer lands after the switch", async () => {
    const { container, rerender } = render(view(kyle));
    await waitFor(() => expect(bubbles(container)).toEqual(["KYLE-DM"]));

    // Send to Kyle and hold the post-send listChat(Kyle) in flight.
    holdPeer = KYLE;
    const ta = container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "TO-KYLE" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await new Promise((r) => setTimeout(r, 10));
    });

    // Switch to the worker while Kyle's refetch is still in flight.
    await act(async () => {
      rerender(view(worker));
      await new Promise((r) => setTimeout(r, 10));
    });
    // Precondition: the switch itself landed correctly. Without this the test
    // below could pass on a window that never showed anything at all.
    expect(bubbles(container)).toEqual(["WORKER-DM"]);

    // Release Kyle's page — it lands while the owner is looking at the worker.
    await act(async () => {
      holdPeer = "";
      held?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    // The window is the worker's, so its messages are the worker's. Neither
    // Kyle's DM nor the line the owner just sent to Kyle may appear here.
    expect(bubbles(container)).toEqual(["WORKER-DM"]);
    expect(container.textContent).not.toContain("KYLE-DM");
    expect(container.textContent).not.toContain("TO-KYLE");
    expect(container.querySelector(".chat__header-name")?.textContent).toBe(
      "外包 · O-148",
    );
  });

  it("sends without switching peer: the sent line lands in this window", async () => {
    // The post-send refetch is the ONLY thing that puts a sent message on
    // screen (there is no optimistic bubble), so this exercises the arm of
    // refetch's updater that does commit — no peer switch, nothing held.
    const { container } = render(view(worker));
    await waitFor(() => expect(bubbles(container)).toEqual(["WORKER-DM"]));

    const ta = container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "SAME-PEER-SEND" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(bubbles(container)).toEqual(["WORKER-DM", "SAME-PEER-SEND"]);
  });

  it("sends after loading older history: the loaded scrollback survives the post-send refetch", async () => {
    // The refetched page is only the newest window, so committing it has to
    // MERGE (older messages kept in front) rather than replace. Seed 35
    // messages so the first page is full (hasMore) and one older page exists
    // above it.
    log.length = 0;
    for (let i = 1; i <= 35; i += 1) {
      log.push({
        id: `w${String(i).padStart(2, "0")}`,
        from: WORKER,
        to: OWNER,
        body: `W${String(i).padStart(2, "0")}`,
        ts: 100 + i,
        attachments: [],
        replyCardId: null,
      });
    }
    const { container } = render(view(worker));
    await waitFor(() => expect(bubbles(container).length).toBe(30));
    expect(bubbles(container)[0]).toBe("W06");

    // Scroll to the top to pull the one older page (jsdom has no layout, so
    // the geometry is defined here).
    const list = container.querySelector(".chat__messages") as HTMLElement;
    Object.defineProperty(list, "scrollHeight", {
      configurable: true,
      value: 1000,
    });
    Object.defineProperty(list, "clientHeight", {
      configurable: true,
      value: 500,
    });
    await act(async () => {
      fireEvent.scroll(list);
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() => expect(bubbles(container)[0]).toBe("W01"));

    const ta = container.querySelector("textarea") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "MERGE-PROBE" } });
    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
      await new Promise((r) => setTimeout(r, 30));
    });

    const after = bubbles(container);
    expect(after[0]).toBe("W01");
    expect(after[after.length - 1]).toBe("MERGE-PROBE");
    expect(after.length).toBe(36);
  });

  it("a legitimate agent-to-agent line addressed to this peer is shown in the inter-agent block", async () => {
    // `Kyle → O-148` has the worker as its recipient, so the server's `?with=`
    // filter returns it for this window by design. This case goes through the
    // initial load only (no send, no peer switch): it records that the window
    // folds that line into the collapsible inter-agent block and that
    // expanding the block shows it.
    const { container } = render(view(worker));
    await waitFor(() => expect(bubbles(container)).toEqual(["WORKER-DM"]));

    const toggle = container.querySelector(
      ".chat__inter-toggle",
    ) as HTMLButtonElement;
    expect(toggle).toBeTruthy();
    await act(async () => {
      fireEvent.click(toggle);
    });
    expect(bubbles(container)).toContain("KYLE-TO-WORKER");
  });
});
