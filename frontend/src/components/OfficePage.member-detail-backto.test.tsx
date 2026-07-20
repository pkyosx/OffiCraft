// T-a706 owner-acceptance finding: clicking a 請示頁 card avatar correctly
// opened the member/worker detail panel, but its 返回 button then dropped the
// owner into an unrelated chat room (roster[0]'s default chat) instead of
// back on the 請示 page — there was never a chat selected on the way in, so
// OfficePage's ordinary 返回 (which resets to its own chat view) had nothing
// honest to return to.
//
// Fix: a deep link into #office/member|worker/<id> can carry a trailing
// /from/replies segment (hashRoute.ts backTo); OfficePage's 返回 checks it and
// routes to #replies instead of its own chat-view reset. This spec drives
// OfficePage directly from the hash (the real post-navigation state
// RepliesPage's avatar click produces) and asserts 返回's actual destination —
// the SAME shape of test OfficePage.jump-outsource.test.tsx uses for its
// sibling regression.
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetMock, __injectMockOutsourceWorker } from "../api/mock";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage — 返回 from a #from/replies deep-linked detail panel", () => {
  it("member panel: 返回 lands on #replies, not the default chat view", async () => {
    window.location.hash = "#office/member/mira/from/replies";
    const { container, findByText } = renderOffice();

    // We actually reached the member panel (not a self-healed roster view).
    await findByText("Mira");

    fireEvent.click(container.querySelector(".mp__back")!);

    await waitFor(() => expect(window.location.hash).toBe("#replies"));
  });

  it("worker panel: 返回 lands on #replies, not the default chat view", async () => {
    const workerId = "ow-backto-test1";
    __injectMockOutsourceWorker({
      id: workerId,
      codename: "O-77",
      model: "Opus 4.6",
      effort: "high",
      taskId: "t-backto",
      taskTitle: "測試工作",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000 - 300,
    });
    window.location.hash = `#office/worker/${workerId}/from/replies`;
    const { container, findAllByText } = renderOffice();

    // We actually reached the worker panel (not a self-healed roster view) —
    // codename renders combined as "外包 · O-77" (t.office.outsource.label,
    // matched in more than one place, e.g. panel + terminal-attach snippet).
    expect((await findAllByText("外包 · O-77")).length).toBeGreaterThan(0);

    fireEvent.click(container.querySelector(".mp__back")!);

    await waitFor(() => expect(window.location.hash).toBe("#replies"));
  });

  it("member panel opened WITHOUT the tag: 返回 keeps the old chat-view reset (no regression to the ordinary in-office flow)", async () => {
    window.location.hash = "#office/member/mira";
    const { container, findByText } = renderOffice();

    await findByText("Mira");

    fireEvent.click(container.querySelector(".mp__back")!);

    // Ordinary behaviour is untouched: the detail closes back to the plain
    // office/chat view, NOT #replies (which nothing asked for here).
    await waitFor(() =>
      expect(window.location.hash).not.toContain("/member/")
    );
    expect(window.location.hash).not.toBe("#replies");
  });
});
