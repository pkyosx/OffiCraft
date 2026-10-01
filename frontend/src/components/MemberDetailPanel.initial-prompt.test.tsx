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
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";

let bootContext: (memberId: string) => Promise<string>;

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getMemberBootContext: (memberId: string) => bootContext(memberId),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(id = "mira", name = "Mira"): Member {
  return {
    id,
    name,
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
    terminalAttachCommand: `tmux -L officraft attach -t member-${id}`,
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
const ui = (member: Member = mkMember()) => (
  <I18nProvider>
    <MemberDetailPanel member={member} onBack={() => {}} />
  </I18nProvider>
);

function renderPanel() {
  const utils = render(ui());
  return {
    ...utils,
    repaint: () => utils.rerender(ui()),
    showMember: (member: Member) => utils.rerender(ui(member)),
  };
}

beforeEach(() => {
  bootContext = () => Promise.resolve("");
});

describe("MemberDetailPanel — 初始 PROMPT card", () => {
  it("expanding the card reads the boot context by the member's id and labels it as a current preview with a boot-time caveat", async () => {
    const ids: string[] = [];
    bootContext = (memberId) => {
      ids.push(memberId);
      return Promise.resolve("Mira 的開機指示");
    };

    const { findByTestId } = renderPanel();
    const toggle = await findByTestId("mp-prompt-toggle");
    expect(toggle.textContent).toContain(zh.workerDetail.initialPromptHint);
    fireEvent.click(toggle);

    expect((await findByTestId("mp-prompt-note")).textContent).toBe(
      zh.mp.initialPromptNote,
    );
    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "Mira 的開機指示",
      ),
    );
    expect(ids).toEqual(["mira"]);
  });

  it("switching to another member of the same role re-reads and shows that member's boot context", async () => {
    const ids: string[] = [];
    bootContext = (memberId) => {
      ids.push(memberId);
      return Promise.resolve(`${memberId} 的開機指示`);
    };

    const { findByTestId, showMember } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "mira 的開機指示",
      ),
    );

    showMember(mkMember("nova", "Nova"));

    await waitFor(async () =>
      expect((await findByTestId("mp-prompt-body")).textContent).toContain(
        "nova 的開機指示",
      ),
    );
    expect((await findByTestId("mp-prompt-body")).textContent).not.toContain(
      "mira 的開機指示",
    );
    expect(ids).toEqual(["mira", "nova"]);
  });

  it("switching to another member of the same role while the first read is in flight shows the second member's boot context even when the first answer lands last", async () => {
    const pending = new Map<string, (text: string) => void>();
    bootContext = (memberId) =>
      new Promise<string>((resolve) => pending.set(memberId, resolve));

    const { findByTestId, showMember } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    expect((await findByTestId("mp-prompt-body")).textContent).toBe("載入中…");

    showMember(mkMember("nova", "Nova"));
    await waitFor(() => expect([...pending.keys()]).toEqual(["mira", "nova"]));

    await act(async () => pending.get("nova")!("nova 的開機指示"));
    await act(async () => pending.get("mira")!("mira 的開機指示"));

    expect((await findByTestId("mp-prompt-body")).textContent).toBe(
      "這是依目前設定組裝的預覽，可能與這位成員實際開機時收到的內容不同。nova 的開機指示",
    );
  });

  it("switching to another member of the same role while the first read is in flight shows the second member's boot context, not an error, when the first read fails after the second has landed", async () => {
    const pending = new Map<
      string,
      { resolve: (text: string) => void; reject: (err: Error) => void }
    >();
    bootContext = (memberId) =>
      new Promise<string>((resolve, reject) =>
        pending.set(memberId, { resolve, reject }),
      );

    const { findByTestId, showMember } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    expect((await findByTestId("mp-prompt-body")).textContent).toBe("載入中…");

    showMember(mkMember("nova", "Nova"));
    await waitFor(() => expect([...pending.keys()]).toEqual(["mira", "nova"]));

    await act(async () => pending.get("nova")!.resolve("nova 的開機指示"));
    await act(async () => pending.get("mira")!.reject(new Error("boom")));

    expect((await findByTestId("mp-prompt-body")).textContent).toBe(
      "這是依目前設定組裝的預覽，可能與這位成員實際開機時收到的內容不同。nova 的開機指示",
    );
  });

  it("switching to another member of the same role while the first read is in flight stays on 載入中… when the first answer lands before the second", async () => {
    const pending = new Map<string, (text: string) => void>();
    bootContext = (memberId) =>
      new Promise<string>((resolve) => pending.set(memberId, resolve));

    const { findByTestId, showMember } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    expect((await findByTestId("mp-prompt-body")).textContent).toBe("載入中…");

    showMember(mkMember("nova", "Nova"));
    await waitFor(() => expect([...pending.keys()]).toEqual(["mira", "nova"]));

    await act(async () => pending.get("mira")!("mira 的開機指示"));

    expect((await findByTestId("mp-prompt-body")).textContent).toBe("載入中…");
  });

  it("still shows the prompt when the panel repaints while the read is in flight", async () => {
    let calls = 0;
    let land: (v: string) => void = () => {};
    bootContext = () => {
      calls += 1;
      return new Promise<string>((resolve) => (land = resolve));
    };

    const { findByTestId, repaint } = renderPanel();
    fireEvent.click(await findByTestId("mp-prompt-toggle"));
    // Positive control: the read really is under way (not already finished, or
    // the repaint below would have nothing to interrupt).
    expect((await findByTestId("mp-prompt-body")).textContent).toContain(
      zh.mp.promptLoading,
    );

    repaint();
    land("角色開機指示");

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
    bootContext = () => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new Error("boom"))
        : Promise.resolve("角色開機指示");
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
    bootContext = () => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new Error("boom"))
        : Promise.resolve("角色開機指示");
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
