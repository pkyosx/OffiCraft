// AgentDetailPanel · the terminal attach command is the STATION's string (T-139).
//
// The defect this file guards against is not a crash: the panel used to render
// `tmux -L officraft attach -t <session>`, assembling the socket half from a
// literal that is only correct on the unnamespaced instance. On a namespaced
// station the owner copied a line that attached to a DIFFERENT tmux server.
//
// So what is asserted is VERBATIM-ness, in both directions the owner can reach
// it (on screen, and on the clipboard), against a command that could not
// possibly be reconstructed from anything the client holds — and the honest
// answer when a pre-T-139 station serves none.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { AgentDetailPanel, notHere, slot } from "./AgentDetailPanel";
import type { AgentDetailSlots, AgentDetailVM } from "./AgentDetailPanel";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    relocateMember: vi.fn(),
    activateMember: vi.fn(),
    patchMember: vi.fn(),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    getMemberResumeSummary: () => Promise.resolve(null),
    subscribeEvents: () => () => {},
  },
}));

// 🔴 NOTHING THE CLIENT HOLDS COULD PRODUCE THIS. Not the socket (a namespace
// the cockpit has never seen), not the session name. If it reaches the screen
// it came off the wire.
const SERVED = "tmux -L officraft-seth attach -t member-m-1a2b";

const slots: AgentDetailSlots = {
  overlays: slot(null),
  afterIdentityCards: notHere("not this test's subject"),
  afterInfoCards: notHere("not this test's subject"),
  extraExpandCards: slot(null),
  afterPromptCards: slot(null),
};

function mkVM(over: Partial<AgentDetailVM> = {}): AgentDetailVM {
  return {
    testIdPrefix: "mp",
    online: true,
    runtime: "claude",
    reportedRuntime: "",
    model: "",
    effort: "",
    machineText: "",
    accountText: "",
    contextPct: null,
    cost: null,
    refocusSince: null,
    refocusSubmittedNote: "",
    refocusSinceLabel: (x: string) => x,
    lastOp: "",
    lastOpVerb: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: null,
    terminalAttachCommand: SERVED,
    terminalHint: "hint",
    terminalUnavailable: "this server provides none",
    ...over,
  };
}

function renderPanel(vm: AgentDetailVM) {
  return render(
    <I18nProvider>
      <AgentDetailPanel
        vm={vm}
        slots={slots}
        onBack={() => {}}
        identity={null}
      />
    </I18nProvider>,
  );
}

let written: string[] = [];

beforeEach(() => {
  written = [];
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: vi.fn((text: string) => {
        written.push(text);
        return Promise.resolve();
      }),
    },
  });
});

describe("AgentDetailPanel terminal attach command", () => {
  it("renders the served command verbatim", () => {
    const { container } = renderPanel(mkVM());
    const cmd = container.querySelector(".mp-terminal__cmd");
    // normalizeWhitespace: the `$` prompt and the command are separate nodes.
    expect(cmd?.textContent?.replace(/\s+/g, " ").trim()).toBe(`$ ${SERVED}`);
  });

  it("copies the served command verbatim — nothing wrapped around it", async () => {
    const { getByTestId } = renderPanel(mkVM());
    fireEvent.click(getByTestId("mp-copy"));
    await waitFor(() => expect(written).toEqual([SERVED]));
  });

  it("says the server provides none — and offers no copy — when the field is empty", () => {
    const { getByTestId, queryByTestId, container } = renderPanel(
      mkVM({ terminalAttachCommand: "" }),
    );
    expect(getByTestId("mp-no-command").textContent).toBe(
      "this server provides none",
    );
    // 🔴 No copy button: a button that puts "" on the clipboard, or worse a
    // reconstructed command, is the failure this branch exists to prevent.
    expect(queryByTestId("mp-copy")).toBeNull();
    expect(container.querySelector(".mp-terminal__cmd")).toBeNull();
    // The block itself STAYS — an owner who saw it vanish would read that as
    // "this agent has no terminal", a different and false claim.
    expect(container.querySelector(".mp-terminal")).not.toBeNull();
  });
});

// The member panel's own leg of the same contract. It is asserted through the
// REAL MemberDetailPanel rather than a hand-built VM, because the thing that
// broke before was the wrapper's binding, not the shared panel's rendering.
describe("MemberDetailPanel terminal attach command", () => {
  it("hands the member's served command to the shared panel untouched", () => {
    const member = {
      id: "m-1a2b",
      name: "Mira",
      role: "assistant",
      status: "offline",
      lifecycle: "offline",
      runtime: "claude",
      model: "opus",
      effort: "medium",
      kind: "staff",
      desiredMachineId: "",
      machine: "",
      account: null,
      contextPct: null,
      estimatedCost: null,
      bankedCost: null,
      // 🔴 A socket the cockpit cannot reach. A wrapper that went back to
      // building `tmux -L officraft attach -t member-<id>` from the id beside
      // it would produce a DIFFERENT string here — against a main-instance
      // fixture it would produce an identical one and this test would be blind.
      terminalAttachCommand: SERVED,
      refocusSince: null,
      lastOp: "",
      lastOpOk: null,
      lastOpLog: "",
      lastOpReason: "",
      lastOpAt: null,
      unreadCount: 0,
    } as unknown as Member;

    const { container } = render(
      <I18nProvider>
        <MemberDetailPanel member={member} onBack={() => {}} />
      </I18nProvider>,
    );
    const cmd = container.querySelector(".mp-terminal__cmd");
    expect(cmd?.textContent?.replace(/\s+/g, " ").trim()).toBe(`$ ${SERVED}`);
  });
});
