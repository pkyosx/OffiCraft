// TaskReassignDialog — 每一種轉派拒絕都要分得出來 (T-b9f6, owner 2026-08-11).
//
// The failure this file exists for: owner pressed 轉派, got the four characters
// 「轉派失敗」, pressed again, same four characters — and had to ask someone to
// read the server code to learn that 凍結 was blocking him (chat
// c-066088ffad83). The server knew the reason all along and put it on the wire;
// the dialog caught the error, logged it to a console nobody has open, and
// printed one fixed string.
//
// 🔴 The acceptance is NOT "a reason appears" — it is "refusals are TELLABLE
// APART on screen", so every case below asserts its own sentence AND the whole
// set is asserted to be pairwise distinct. A single-case test would pass just as
// happily against a version that printed the same reason for everything.
//
// ⚠️ These four are a SAMPLE, not the refusal set. This file used to call them
// "the four"; independent review counted fifteen `writeError` calls in that one
// handler — the seam is generic (whatever sentence the server sends is what the
// dialog shows), so the sample is chosen to cover the distinct STATUS classes
// (409 / 403 / 403 / 400) rather than to enumerate. For today's real list run
// `grep -n writeError server/ocserverd/api_tasks.go` inside the reassign
// handler; a list frozen into a comment goes stale with nothing turning red. Three of
// the four are unreachable through the mock from the cockpit (they depend on
// WHO is calling), so the refusal is injected at the onReassign boundary — the
// same boundary the page's real wiring rejects through.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { TaskReassignDialog } from "./TaskReassignDialog";
import { ApiError } from "../api/errors";
import { mockApiError } from "../api/errorCodes";
import type { TaskView } from "../api/adapter";

function mkTask(): TaskView {
  return {
    id: "task-1",
    taskNo: "T-1001",
    title: "任務",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "outsource",
    executorId: "",
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
  };
}

/** Drive one 轉外包 submit and return what the dialog put on screen. */
async function submitAndReadError(rejectWith: unknown): Promise<string> {
  const onReassign = vi.fn().mockRejectedValue(rejectWith);
  const { findByTestId, container, unmount } = render(
    <I18nProvider>
      <TaskReassignDialog
        task={mkTask()}
        members={[]}
        onReassign={onReassign}
        onClose={vi.fn()}
      />
    </I18nProvider>
  );
  fireEvent.click(await findByTestId("reassign-kind-outsource"));
  await waitFor(() =>
    expect(
      container.querySelector('[data-testid^="reassign-machine-"]')
    ).not.toBeNull()
  );
  fireEvent.click(
    container.querySelector<HTMLElement>('[data-testid^="reassign-machine-"]')!
  );
  fireEvent.click(await findByTestId("reassign-confirm"));

  // The dialog must STAY OPEN on a refusal — a closed dialog would take the
  // reason with it, which is the silent failure in a different costume.
  const dialog = await findByTestId("reassign");
  await waitFor(() => expect(onReassign).toHaveBeenCalledTimes(1));
  // The error line is ConfirmModal's own (`confirm-modal__error`) — the dialog
  // hands it a string and the shell renders it; there is no second error slot.
  await waitFor(() =>
    expect(dialog.querySelector(".confirm-modal__error")).not.toBeNull()
  );
  const text = dialog.querySelector(".confirm-modal__error")?.textContent ?? "";
  unmount();
  return text;
}

const REFUSALS: Array<{ name: string; err: ApiError; needle: string }> = [
  {
    name: "terminal task (409)",
    err: mockApiError("http 409 for POST /x", 409,
      "task 'task-1' is already closed (terminated)"),
    needle: "already closed (terminated)",
  },
  {
    name: "an outsource worker asking at all (403)",
    err: mockApiError("http 403 for POST /x", 403,
      "outsource workers may not reassign tasks"),
    needle: "outsource workers may not reassign tasks",
  },
  {
    name: "一般正職 naming another member (403)",
    err: mockApiError("http 403 for POST /x", 403,
      "only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead"),
    needle: "only the owner or an admin agent may reassign a task to another member",
  },
  {
    name: "an invalid target (400)",
    err: mockApiError("http 400 for POST /x", 400,
      "target member 'm-nobody' is not an active roster member"),
    needle: "is not an active roster member",
  },
];

describe("轉派被拒時,畫面說得出是哪一種", () => {
  for (const c of REFUSALS) {
    it(`shows the server's own reason: ${c.name}`, async () => {
      const shown = await submitAndReadError(c.err);
      expect(shown).toContain(c.needle);
      // The generic copy is the FALLBACK. Seeing it while the server DID say
      // why means the reason died at this seam again — the original defect.
      expect(shown).not.toContain("轉派失敗");
    });
  }

  it("tells the four refusals apart (pairwise distinct on screen)", async () => {
    const shown: string[] = [];
    for (const c of REFUSALS) shown.push(await submitAndReadError(c.err));
    expect(new Set(shown).size).toBe(REFUSALS.length);
  });

  it("falls back to our own copy when the error carries no reason", async () => {
    // Not every failure comes from the server envelope (a dropped body, a
    // proxy error page, a plain Error thrown above the adapters). An EMPTY
    // error line is worse than a generic one, so the fallback has to hold.
    expect(await submitAndReadError(new Error("boom"))).toContain("轉派失敗");
    expect(
      await submitAndReadError(
        mockApiError("http 500 for POST /x", 500, "")
      )
    ).toContain("轉派失敗");
  });
});
