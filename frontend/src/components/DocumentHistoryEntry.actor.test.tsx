// DocumentHistoryEntry — WHO wrote a retained revision (T-1f39, owner ruling
// 2026-07-31 "名字跟代號並列，查不到名字就只顯示代號").
//
// The wire carries only the stable id. The roster is what turns it into a name,
// and it holds LIVE members only — so the three cases pinned here are the three
// the owner will actually meet:
//   1. a revision written by someone still on the roster → NAME and id, both,
//      on the row and again inside the modal the row opens;
//   2. a revision written by an id the roster cannot name (a released outsource
//      worker, a dismissed member) → the bare id, with no empty brackets;
//   3. a revision the OWNER wrote from the cockpit → the cockpit's own owner
//      label. He is never on the roster, so before this he was reading "owner"
//      — the one id whose name the app already knows (owner ruling, same day).

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { __resetMock, mockApi } from "../api/mock";
import type { DocumentHistoryView, Member } from "../types";
import { stubDocumentHistory } from "../test/documentHistory";

const s = zh.settings;

const KYLE = "m-f663f3c5de9a";
const RELEASED_WORKER = "ow-c975fff254f7";

function revision(actorId: string, id: number): DocumentHistoryView {
  return {
    id,
    content: { text: `第 ${id} 版` },
    createdTs: 1753776180,
    actorId,
  };
}

function roster(): Member[] {
  return [
    // Only the two fields the resolver reads; the list renders no other part
    // of a member (the house shape for roster fixtures in this suite).
    { id: KYLE, name: "Kyle" } as unknown as Member,
  ];
}

/** Mount the entry and open its list — the history is behind the button now,
 * so every actor assertion goes through the click that fetches it. */
async function openList() {
  const utils = render(
    <I18nProvider>
      <DocumentHistoryEntry
        kind="lessons"
        docKey="r-engineer::review-pr"
        title={s.historyLessonsTitle}
      />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("doc-history-entry-lessons"));
  await utils.findByTestId("doc-history-item-1");
  return utils;
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
  // Both reads are stubbed from the same fixtures — the row the list serves
  // has no text, exactly as the adapter's does.
  stubDocumentHistory(mockApi, [
    revision(KYLE, 1),
    revision(RELEASED_WORKER, 2),
    revision("owner", 3),
  ]);
  vi.spyOn(mockApi, "listMembers").mockResolvedValue(roster());
});

describe("DocumentHistoryEntry · 修改者", () => {
  it("names a roster member and keeps the id beside the name", async () => {
    const utils = await openList();
    const row = within(utils.getByTestId("doc-history-item-1"));

    // Both tokens, in one line: the name is what the owner reads, the id is
    // what the row was actually written under and what survives a rename.
    const actor = row.getByText(new RegExp(`Kyle.*${KYLE}`));
    expect(actor.textContent).toContain(s.historyByLabel);

    // The modal the row opens repeats the SAME resolved line — an id that is
    // readable on the list and raw one click deeper is the bug this replaces.
    fireEvent.click(utils.getByTestId("doc-history-open-1"));
    const modal = within(utils.getByTestId("doc-history-modal"));
    expect(modal.getByText(new RegExp(`Kyle.*${KYLE}`))).toBeTruthy();
  });

  it("shows the bare id — no empty brackets — when the roster cannot name it", async () => {
    const utils = await openList();
    const row = utils.getByTestId("doc-history-item-2");

    expect(row.textContent).toContain(RELEASED_WORKER);
    // The failure this guards is a resolver that formats first and checks
    // later: `ow-…（）` or `（ow-…）` both read as data loss to the owner.
    expect(row.textContent).not.toContain(
      `${RELEASED_WORKER}${s.historyActorLead}`
    );
    expect(row.textContent).not.toContain(s.historyActorTail);
  });

  it("calls the OWNER by the cockpit's own label, not by his wire id", async () => {
    const utils = await openList();
    const row = utils.getByTestId("doc-history-item-3");

    expect(row.textContent).toContain(zh.user);
    // The label REPLACES the id here — "owner" is not a name the roster failed
    // to resolve, it is the one identity the cockpit already has a word for,
    // so carrying it alongside would be the same unreadable token again.
    expect(row.textContent).not.toContain("owner");

    // …and the modal is not a second implementation of the same rule.
    fireEvent.click(utils.getByTestId("doc-history-open-3"));
    const modal = utils.getByTestId("doc-history-modal");
    expect(modal.textContent).toContain(zh.user);
  });
});
