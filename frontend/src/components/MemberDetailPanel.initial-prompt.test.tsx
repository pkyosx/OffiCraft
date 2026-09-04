// MemberDetailPanel · 初始 PROMPT 卡的載入生命週期 (T-7526).
//
// The card lives in the SHARED AgentDetailPanel, so the same defect hit both
// detail pages: `vm.prompt.fetch` is an inline arrow rebuilt on every render, it
// sat in the effect's deps, and a repaint while the read was in flight tore the
// effect down (neither `.then` nor `.catch` could write state) while the rerun
// bailed on a "loaded" stamp that had been written at fetch START. The card
// stayed on 「載入中…」 for good — collapsing and re-expanding could not recover
// it. The worker half of this proof lives in WorkerDetailPanel.test.tsx.
//
// The repaint is staged with an explicit rerender; in the app an ordinary SSE
// delta is enough.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

let bootstrap: () => Promise<{ context: string }>;

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getBootstrap: () => bootstrap(),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

// 🔴 A FRESH element every time. Handing `rerender` the identical element object
// makes React bail out and never re-render the subtree — the repaint would not
// happen at all, and the test would pass against the unfixed panel.
const ui = () => (
  <I18nProvider>
    <MemberDetailPanel member={mkMember()} onBack={() => {}} />
  </I18nProvider>
);

function renderPanel() {
  const utils = render(ui());
  return { ...utils, repaint: () => utils.rerender(ui()) };
}

beforeEach(() => {
  bootstrap = () => Promise.resolve({ context: "" });
});

describe("MemberDetailPanel — 初始 PROMPT card", () => {
  it("still shows the prompt when the panel repaints while the read is in flight", async () => {
    let calls = 0;
    let land: (v: { context: string }) => void = () => {};
    bootstrap = () => {
      calls += 1;
      return new Promise<{ context: string }>((resolve) => (land = resolve));
    };

    const { findByTestId, repaint } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    // Positive control: the read really is under way (not already finished, or
    // the repaint below would have nothing to interrupt).
    expect((await findByTestId("mp-prompt-body")).textContent).toContain(
      zh.mp.promptLoading,
    );

    repaint();
    land({ context: "角色開機指示" });

    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "角色開機指示",
      ),
    );
    // A repaint is not a reason to re-read either — the ONE read that was
    // already under way is the one that lands.
    expect(calls).toBe(1);
  });

  it("a failed read shows the error with a retry that actually re-reads", async () => {
    let calls = 0;
    bootstrap = () => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new Error("boom"))
        : Promise.resolve({ context: "角色開機指示" });
    };

    const { findByTestId } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    const err = await findByTestId("mp-prompt-error");
    expect(err.textContent).toContain(zh.mp.promptError);

    fireEvent.click(await findByTestId("mp-prompt-retry"));
    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "角色開機指示",
      ),
    );
    expect(calls).toBe(2);
  });

  it("re-expanding after a failed read reads again instead of resurrecting 載入中", async () => {
    let calls = 0;
    bootstrap = () => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new Error("boom"))
        : Promise.resolve({ context: "角色開機指示" });
    };

    const { findByTestId } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    await findByTestId("mp-prompt-error");
    // Collapse, re-expand — the recovery path the owner actually tried.
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "角色開機指示",
      ),
    );
    expect(calls).toBe(2);
  });
});
