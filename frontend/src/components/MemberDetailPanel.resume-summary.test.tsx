// MemberDetailPanel · RESUME SUMMARY card (T-8b0d).
//
// 🔴 HARD REQUIREMENT (owner-approved 2026-08-02): the panel's default load
// must not issue ANY request for this section — the fetch fires ONLY when the
// owner expands it, and expanding must cost exactly ONE request (the content
// AND its size figures ride the same `getMemberResumeSummary` response). This
// mirrors the 初始 PROMPT lazy-fetch pattern (T-7526) — see
// MemberDetailPanel.initial-prompt.test.tsx for the repaint-mid-flight defect
// class this shape guards against; this file focuses on the request-count
// contract that IS the requirement here.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";
import type { MemberResumeSummaryView } from "../api/adapter";

let resumeSummary: () => Promise<MemberResumeSummaryView>;
let resumeSummaryCalls = 0;

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    getBootstrap: () => Promise.resolve({ context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: () => Promise.resolve([]),
    getMemberResumeSummary: () => {
      resumeSummaryCalls += 1;
      return resumeSummary();
    },
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
    kind: "assistant",
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

function mkSnapshot(): MemberResumeSummaryView {
  return {
    identity: "mira",
    chat: [
      {
        id: "cm-1",
        from: "mira",
        to: "owner",
        body: "已完成 T-8b0d 的後端組裝",
        ts: 1000,
        attachments: [],
        replyCardId: null,
        replyCardStatus: null,
      },
    ],
    tasks: [
      {
        id: "t-1",
        taskNo: "T-9001",
        title: "整理 RESUME SUMMARY 面板",
        typeKey: "frontend",
        status: "in_progress",
        priority: "medium",
        waitingReason: "",
        currentStepId: "s-1",
        currentStepName: "串接 lazy fetch",
        progressDone: 1,
        progressTotal: 3,
        updatedTs: 2000,
        detailChars: 42,
      },
    ],
    // 🔴 Deliberately distinct, mutually non-substring 3-4 digit numbers —
    // NOT the literal 1/14/4/42/2 this fixture used to use, where "1" is a
    // substring of "14" and every field could satisfy `toContain` against
    // any OTHER field's digits. A per-field assertion below still queries
    // each field's own testid, but keeping the numbers non-colliding too
    // means a copy-paste of the wrong field into the wrong slot would also
    // be visible to eye, not just to the assertion.
    overview: {
      chatCount: 501,
      chatChars: 8237,
      tasksReturned: 613,
      tasksOpenTotal: 9042,
      tasksDetailChars: 375,
      cardsWaiting: 264,
      cardsAnsweredRecent: 718,
      rosterChars: 4196,
      machinesChars: 1583,
    },
    note: "BOUNDED snapshot",
    generatedAt: "2026-08-13 09:47:11 +08:00",
    chatEarlierOmitted: { omitted: false, hint: "" },
    roster: [],
    machines: null,
  };
}

// 🔴 A FRESH element every time — handing `rerender` the identical element
// object makes React bail out and never re-render the subtree, which would
// hide the T-7526 defect class entirely (see initial-prompt test's header).
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
  resumeSummaryCalls = 0;
  resumeSummary = () => Promise.resolve(mkSnapshot());
});

describe("MemberDetailPanel — RESUME SUMMARY card", () => {
  it("issues zero requests on the panel's default (collapsed) load", async () => {
    const { findByTestId } = renderPanel();
    // Settle every other mount-fetch on the panel (webhooks/machines/etc.)
    // before asserting — the toggle itself is proof the panel finished
    // mounting.
    await findByTestId("mp-resume-toggle");
    expect(resumeSummaryCalls).toBe(0);
  });

  it("fetches exactly once on first expand, and renders the SAME response's content and size figures", async () => {
    const { findByTestId } = renderPanel();
    fireEvent.click(await findByTestId("mp-resume-toggle"));

    const body = await waitFor(async () => {
      const el = await findByTestId("mp-resume-body");
      if (!el.textContent?.includes("T-9001")) throw new Error("not loaded yet");
      return el;
    });

    expect(resumeSummaryCalls).toBe(1);
    // content parity
    expect(body.textContent).toContain("已完成 T-8b0d 的後端組裝");
    expect(body.textContent).toContain("整理 RESUME SUMMARY 面板");
    expect(body.textContent).toContain("T-9001");
    // size figures — from the SAME response, not a second request. Each
    // assertion is scoped to ITS OWN field's testid (not the parent
    // .mp-resume-overview's whole textContent) and checked for an EXACT
    // match against a value that cannot collide with any other field's
    // digits — so a wrong-field wire-up (e.g. cardsWaiting rendering
    // tasksOpenTotal) would fail, not slip through as a substring hit.
    const snap = mkSnapshot();
    expect(
      (await findByTestId("mp-resume-stat-chatCount")).textContent,
    ).toBe(String(snap.overview.chatCount));
    expect(
      (await findByTestId("mp-resume-stat-chatChars")).textContent,
    ).toBe(String(snap.overview.chatChars));
    expect(
      (await findByTestId("mp-resume-stat-tasksReturned")).textContent,
    ).toBe(String(snap.overview.tasksReturned));
    expect(
      (await findByTestId("mp-resume-stat-tasksOpenTotal")).textContent,
    ).toBe(String(snap.overview.tasksOpenTotal));
    expect(
      (await findByTestId("mp-resume-stat-tasksDetailChars")).textContent,
    ).toBe(String(snap.overview.tasksDetailChars));
    expect(
      (await findByTestId("mp-resume-stat-cardsWaiting")).textContent,
    ).toBe(String(snap.overview.cardsWaiting));
    expect(
      (await findByTestId("mp-resume-stat-cardsAnsweredRecent")).textContent,
    ).toBe(String(snap.overview.cardsAnsweredRecent));
  });

  it("collapsing and re-expanding does not refetch (loaded-key cache)", async () => {
    const { findByTestId } = renderPanel();
    fireEvent.click(await findByTestId("mp-resume-toggle"));
    await waitFor(async () =>
      expect((await findByTestId("mp-resume-body")).textContent).toContain(
        "T-9001",
      ),
    );
    expect(resumeSummaryCalls).toBe(1);

    fireEvent.click(await findByTestId("mp-resume-toggle")); // collapse
    fireEvent.click(await findByTestId("mp-resume-toggle")); // re-expand
    await waitFor(async () =>
      expect((await findByTestId("mp-resume-body")).textContent).toContain(
        "T-9001",
      ),
    );
    expect(resumeSummaryCalls).toBe(1);
  });

  it("still shows the content when the panel repaints while the read is in flight", async () => {
    let land: (v: MemberResumeSummaryView) => void = () => {};
    resumeSummary = () =>
      new Promise<MemberResumeSummaryView>((resolve) => (land = resolve));

    const { findByTestId, repaint } = renderPanel();
    fireEvent.click(await findByTestId("mp-resume-toggle"));
    expect((await findByTestId("mp-resume-body")).textContent).toContain(
      zh.mp.resumeSummary.loading,
    );

    repaint();
    land(mkSnapshot());

    await waitFor(async () =>
      expect((await findByTestId("mp-resume-body")).textContent).toContain(
        "T-9001",
      ),
    );
    // A repaint is not a reason to re-read — the ONE read already under way
    // is the one that lands.
    expect(resumeSummaryCalls).toBe(1);
  });

  it("a failed read shows the error with a retry that actually re-reads", async () => {
    let calls = 0;
    resumeSummary = () => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new Error("boom"))
        : Promise.resolve(mkSnapshot());
    };

    const { findByTestId } = renderPanel();
    fireEvent.click(await findByTestId("mp-resume-toggle"));
    const err = await findByTestId("mp-resume-error");
    expect(err.textContent).toContain(zh.mp.resumeSummary.error);

    fireEvent.click(await findByTestId("mp-resume-retry"));
    await waitFor(async () =>
      expect((await findByTestId("mp-resume-body")).textContent).toContain(
        "T-9001",
      ),
    );
    expect(calls).toBe(2);
  });
});
