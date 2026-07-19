// Inter-agent (agent↔agent) chat rendering.
//
// Three requirements are locked here:
//   1. VISIBLE TO BOTH PARTIES + correct attribution: an A→B message shown in a
//      window whose peer is B must be labeled with its TRUE sender (A), not the
//      window peer. (The backend `?with=<id>` filter already returns the message
//      to both A's and B's threads; the FE must attribute it honestly.)
//   2. COLLAPSED BY DEFAULT: consecutive inter-agent messages fold into one
//      collapsible block, collapsed on first render (owner opts in to expand).
//      Owner↔agent messages stay expanded/normal.
//   3. DIRECTION LABEL: when a message's RECIPIENT is not the owner (both
//      directions of an agent↔agent exchange), the sender label spells out
//      "Sender → Recipient" — members message DIFFERENT agents, so the plain
//      sender name is ambiguous. Recipient-is-owner messages keep the plain
//      sender name; an id missing from the roster falls back to the raw id.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, OutsourceWorkerView } from "../api/adapter";

// Window peer = agent "b" (Beto). Owner id is "owner".
let messages: ChatMessage[] = [];

// Released-worker codename cache: the REAL hook lazily fetches
// GET /api/outsource-workers/{id}; here it is a fixed map (the hook has its
// own tests) — "ow-rel" is a RELEASED worker, resolvable only through it.
vi.mock("../hooks/useWorkerCodenames", () => ({
  useWorkerCodenames: (ids: readonly string[]) =>
    new Map(ids.filter((id) => id === "ow-rel").map((id) => [id, "R-2"])),
}));
vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "b",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(id: string, name: string): Member {
  return {
    id,
    memberId: id,
    name,
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: `member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

const beto = mkMember("b", "Beto");
const alma = mkMember("a", "Alma");

const workerX1: OutsourceWorkerView = {
  id: "ow-533c0c4f9dba",
  codename: "X-1",
  model: "opus",
  effort: "high",
  taskId: "t-1",
};

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={beto} members={[beto, alma]} workers={[workerX1]} />
    </I18nProvider>,
  );
}

describe("ChatArea inter-agent thread", () => {
  beforeEach(() => {
    localStorage.clear();
    Element.prototype.scrollIntoView = vi.fn();
    messages = [];
  });

  it("collapses an agent↔agent run by default and expands on toggle", () => {
    // A→B and B→A: an inter-agent run (neither endpoint is the owner).
    messages = [
      { id: "c1", from: "a", to: "b", body: "hey Beto", ts: 1000, attachments: [], replyCardId: null },
      { id: "c2", from: "b", to: "a", body: "hi Alma", ts: 1001, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();

    // Collapsed: no message bubbles rendered, one toggle present.
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(0);
    const toggle = container.querySelector(
      ".chat__inter-toggle",
    ) as HTMLButtonElement;
    expect(toggle).not.toBeNull();
    expect(toggle.getAttribute("aria-expanded")).toBe("false");

    // Expand → bubbles appear, and the sender is attributed to the TRUE author,
    // WITH the recipient spelled out (both directions): the sender name alone
    // cannot tell WHICH agent was messaged.
    fireEvent.click(toggle);
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(2);
    const names = Array.from(
      container.querySelectorAll(".chat__msg-name"),
    ).map((n) => n.textContent);
    expect(names).toContain("Alma → Beto");
    expect(names).toContain("Beto → Alma");
  });

  it("labels a message to the owner with the plain sender name (no arrow)", () => {
    messages = [
      { id: "c1", from: "b", to: "owner", body: "done", ts: 1000, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();
    const names = Array.from(
      container.querySelectorAll(".chat__msg-name"),
    ).map((n) => n.textContent);
    expect(names).toEqual(["Beto"]);
  });

  it("falls back to the raw id when the recipient is not in the roster", () => {
    // Recipient "ghost" is not in the passed members — the label must still
    // show something honest (the raw id), never a blank.
    messages = [
      { id: "c1", from: "a", to: "ghost", body: "ping", ts: 1000, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();
    const toggle = container.querySelector(
      ".chat__inter-toggle",
    ) as HTMLButtonElement;
    fireEvent.click(toggle);
    const names = Array.from(
      container.querySelectorAll(".chat__msg-name"),
    ).map((n) => n.textContent);
    expect(names).toContain("Alma → ghost");
  });

  it("labels an outsource sender/recipient with its codename, not the raw ow- id", () => {
    // The worker id is never in the 正職 roster (server excludes
    // kind='outsource' from GET /api/members) — the label must resolve through
    // the live worker list to the SAME codename identity the left rail shows.
    messages = [
      { id: "c1", from: "ow-533c0c4f9dba", to: "owner", body: "done", ts: 1000, attachments: [], replyCardId: null },
      { id: "c2", from: "a", to: "ow-533c0c4f9dba", body: "thanks", ts: 1001, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();
    const toggle = container.querySelector(
      ".chat__inter-toggle",
    ) as HTMLButtonElement;
    fireEvent.click(toggle);
    const names = Array.from(
      container.querySelectorAll(".chat__msg-name"),
    ).map((n) => n.textContent);
    expect(names).toContain("外包 · X-1");
    expect(names).toContain("Alma → 外包 · X-1");
    expect(names.join()).not.toContain("ow-533c0c4f9dba");
  });

  it("labels a RELEASED outsource sender with the lazily-resolved codename", () => {
    // "ow-rel" is not in the live workers list (released) — the label resolves
    // through the codename cache, never the raw id.
    messages = [
      { id: "c1", from: "ow-rel", to: "owner", body: "wrapped up", ts: 1000, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();
    const names = Array.from(
      container.querySelectorAll(".chat__msg-name"),
    ).map((n) => n.textContent);
    expect(names).toEqual(["外包 · R-2"]);
  });

  it("keeps owner↔agent messages expanded/normal (not collapsed)", () => {
    messages = [
      { id: "c1", from: "owner", to: "b", body: "status?", ts: 1000, attachments: [], replyCardId: null },
      { id: "c2", from: "b", to: "owner", body: "on it", ts: 1001, attachments: [], replyCardId: null },
    ];
    const { container } = renderChat();

    // No collapse toggle; both bubbles are visible immediately.
    expect(container.querySelector(".chat__inter-toggle")).toBeNull();
    expect(container.querySelectorAll(".chat__msg-bubble").length).toBe(2);
  });
});
