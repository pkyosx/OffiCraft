// T-a706 owner-acceptance finding: clicking a 請示頁 card avatar correctly
// opened the member/worker detail panel, but its 返回 button then dropped the
// owner into an unrelated chat room (roster[0]'s default chat) instead of
// back on the 請示 page — there was never a chat selected on the way in, so
// OfficePage's ordinary 返回 (which resets to its own chat view) had nothing
// honest to return to.
//
// Fix: a deep link into #office/member|worker/<id> can carry a trailing
// /from/replies/<reply-card-id> segment (hashRoute.ts backTo); OfficePage's
// 返回 restores that card instead of its own chat-view reset. This spec drives
// OfficePage directly from the hash (the real post-navigation state
// RepliesPage's avatar click produces) and asserts 返回's actual destination —
// the SAME shape of test OfficePage.jump-outsource.test.tsx uses for its
// sibling regression.
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
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

describe("OfficePage — 返回 from a #from/<page> deep-linked detail panel", () => {
  it("member panel: 返回請示 lands on the source card", async () => {
    window.location.hash = "#office/member/mira/from/replies/rc-member";
    const { container, findByText } = renderOffice();

    // We actually reached the member panel (not a self-healed roster view).
    await findByText("Mira");

    const back = container.querySelector(".mp__back")!;
    expect(back.textContent).toContain(zh.mp.backToReplies);
    fireEvent.click(back);

    await waitFor(() =>
      expect(window.location.hash).toBe("#replies/card/rc-member")
    );
  });

  it("worker panel: 返回請示 lands on the source card", async () => {
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
    window.location.hash = `#office/worker/${workerId}/from/replies/rc-worker`;
    const { container, findAllByText } = renderOffice();

    // We actually reached the worker panel (not a self-healed roster view) —
    // codename renders combined as "外包 · O-77" (t.office.outsource.label,
    // matched in more than one place, e.g. panel + terminal-attach snippet).
    expect((await findAllByText("外包 · O-77")).length).toBeGreaterThan(0);

    const back = container.querySelector(".mp__back")!;
    expect(back.textContent).toContain(zh.mp.backToReplies);
    fireEvent.click(back);

    await waitFor(() =>
      expect(window.location.hash).toBe("#replies/card/rc-worker")
    );
  });

  it("member panel tagged /from/tasks/<id>: 返回任務 lands on the source task", async () => {
    window.location.hash = "#office/member/mira/from/tasks/task-member";
    const { container, findByText } = renderOffice();

    await findByText("Mira");

    const back = container.querySelector(".mp__back")!;
    expect(back.textContent).toContain(zh.mp.backToTasks);
    fireEvent.click(back);

    await waitFor(() => expect(window.location.hash).toBe("#tasks/task-member"));
  });

  it("worker panel tagged /from/tasks/<id>: 返回任務 lands on the source task", async () => {
    const workerId = "ow-task-back-test";
    __injectMockOutsourceWorker({
      id: workerId,
      codename: "O-78",
      model: "Opus 4.6",
      effort: "high",
      taskId: "task-worker",
      taskTitle: "測試工作",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000 - 300,
    });
    window.location.hash = `#office/worker/${workerId}/from/tasks/task-worker`;
    const { container, findAllByText } = renderOffice();

    expect((await findAllByText("外包 · O-78")).length).toBeGreaterThan(0);

    const back = container.querySelector(".mp__back")!;
    expect(back.textContent).toContain(zh.mp.backToTasks);
    fireEvent.click(back);

    await waitFor(() => expect(window.location.hash).toBe("#tasks/task-worker"));
  });

  it("member panel opened WITHOUT the tag: 返回 keeps the old chat-view reset (no regression to the ordinary in-office flow)", async () => {
    window.location.hash = "#office/member/mira";
    const { container, findByText } = renderOffice();

    await findByText("Mira");

    expect(container.querySelector(".mp__back")?.textContent).toContain(zh.mp.back);
    fireEvent.click(container.querySelector(".mp__back")!);

    // Ordinary behaviour is untouched: the detail closes back to the plain
    // office/chat view, NOT #replies (which nothing asked for here).
    await waitFor(() =>
      expect(window.location.hash).not.toContain("/member/")
    );
    expect(window.location.hash).not.toBe("#replies");
  });
});
