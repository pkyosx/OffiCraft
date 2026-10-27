// T-66a8 辦公室側欄 正職／外包 tab 切換 (owner mockup 2026-07-18). The old
// two-stacked-groups rail (正職 collapse header + 外包 panel head) is replaced
// by a TOP text-tab switcher. Locked here through the REAL OfficePage (mock
// adapter roster):
//   · each tab shows a red unread-count badge = that area's members' total
//     unread (0 → no badge);
//   · a count sub-line sits under each tab — 正職「N 人」, 外包「N 人 · 上限 M」
//     (the 上限 suffix omitted when settings are not loaded — honest);
//   · the 招攬新成員 button at the sidebar bottom routes by the active tab:
//     正職 → #settings/roles/new (角色誌 create mode), 外包 → the cap popover.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockChat,
  __injectMockMember,
  __injectMockOutsourceWorker,
  __injectMockTask,
} from "../api/mock";
import { api } from "../api";
import type { TaskView } from "../api/adapter";

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

describe("OfficePage — 正職 tab (T-66a8)", () => {
  it("shows the 「N 人」 count sub-line (seed roster = 1 assistant)", async () => {
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("staff-tab-sub");
    await waitFor(() => expect(sub.textContent).toBe("1 人"));
  });

  it("renders NO unread badge when the 正職 area has no unread", async () => {
    const { findByTestId, queryByTestId } = renderOffice();
    // The count settles first…
    await findByTestId("staff-tab-sub");
    // …and with no injected inbound chat, the seed member's unread is 0 → the
    // red tab badge must not render at all (mockup: 0 → hidden).
    expect(queryByTestId("staff-tab-unread")).toBeNull();
  });

  it("sums the 正職 area unread into the tab badge when a member has unread", async () => {
    // Two inbound member→owner messages past the (absent) read watermark — the
    // seed member (Mira) accumulates unread the same way the roster badge does.
    for (const [id, body] of [
      ["m-1", "回報 1"],
      ["m-2", "回報 2"],
    ]) {
      __injectMockChat({
        id,
        from: "mira",
        to: "owner",
        body,
        ts: Date.now() / 1000 - 30,
        attachments: [],
        replyCardId: null,
      });
    }
    const { findByTestId } = renderOffice();
    const badge = await findByTestId("staff-tab-unread");
    await waitFor(() => expect(badge.textContent).toBe("2"));
  });
});

describe("OfficePage — 外包 tab count sub-line (T-66a8)", () => {
  it("shows 「N 人 · 上限 M」 when settings loaded (cap default 3, no workers)", async () => {
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("outsource-tab-sub");
    // 外包 0 人 — the empty area still reads honestly (mock cap default 3).
    await waitFor(() => expect(sub.textContent).toBe("0 人 · 上限 3"));
  });

  it("counts live outsource workers in the 外包 tab sub-line", async () => {
    __injectMockOutsourceWorker({
      id: "ow-1",
      codename: "O-1",
      model: "Opus 4.6",
      effort: "high",
      taskId: "t-1",
      taskTitle: "",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000 - 600,
    });
    const { findByTestId } = renderOffice();
    const sub = await findByTestId("outsource-tab-sub");
    await waitFor(() => expect(sub.textContent).toBe("1 人 · 上限 3"));
  });
});

describe("OfficePage — 招攬新成員 routing (T-66a8 req 4)", () => {
  it("routes to #settings/roles/new (角色誌 create mode) on the 正職 tab", async () => {
    const { findByTestId } = renderOffice();
    const recruit = await findByTestId("office-recruit");
    expect(recruit.querySelector("svg")).not.toBeNull();

    fireEvent.click(recruit);
    // Through the single hash seam (a refresh restores it), straight into
    // CREATE mode — the same deep link the old 正職 ➕👤 button used.
    await waitFor(() =>
      expect(window.location.hash).toBe("#settings/roles/new")
    );
  });

  it("opens the 外包上限設定 popover on the 外包 tab (not a role route)", async () => {
    const { findByTestId, getByTestId, queryByTestId } = renderOffice();
    fireEvent.click(getByTestId("office-tab-outsource"));
    // No popover until the button is pressed.
    expect(queryByTestId("outsource-cap-pop")).toBeNull();
    fireEvent.click(await findByTestId("office-recruit"));
    // The cap popover opens; the hash never changes to a role route.
    expect(getByTestId("outsource-cap-pop").textContent).toContain(
      "外包上限設定"
    );
    expect(window.location.hash).toBe("");
  });
});

describe("OfficePage — pinned chats", () => {
  it("on desktop the chat opens on the first pinned member once the pins have loaded, never on roster[0] before them", async () => {
    __injectMockMember({ id: "beto", kind: "staff", name: "Beto" });
    await api.putChatPins(["beto", "mira"]);
    let releasePins!: () => void;
    const pinsRead = new Promise<void>((r) => (releasePins = r));
    const realGet = api.getChatPins.bind(api);
    const get = vi.spyOn(api, "getChatPins").mockImplementation(async () => {
      await pinsRead;
      return realGet();
    });
    const markRead = vi.spyOn(api, "markChatRead");

    const { container, findAllByText } = renderOffice();
    await findAllByText("Beto");
    expect(container.querySelector(".chat__header-name")).toBeNull();

    releasePins();
    await waitFor(() =>
      expect(container.querySelector(".chat__header-name")?.textContent).toContain("Beto"),
    );
    expect(markRead.mock.calls.map(([m]) => m.peer)).not.toContain("mira");
    get.mockRestore();
    markRead.mockRestore();
  });

  it("pinning a later outsource worker moves it first in the sidebar but leaves the @-mention picker in its default order", async () => {
    const now = Date.now() / 1000;
    const task = (id: string, createdTs: number) =>
      ({
        id,
        taskNo: id.toUpperCase(),
        title: "",
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
        createdTs,
        updatedTs: createdTs,
        closedTs: null,
        progressDone: 0,
        progressTotal: 0,
        steps: [],
      }) as TaskView;
    __injectMockTask(task("t-old", now - 9000));
    __injectMockTask(task("t-new", now - 10));
    for (const [id, codename, taskId] of [
      ["ow-old", "O-1", "t-old"],
      ["ow-new", "O-2", "t-new"],
    ]) {
      __injectMockOutsourceWorker({
        id,
        codename,
        model: "",
        effort: "",
        taskId,
        taskTitle: "",
        taskStatus: "in_progress",
        createdTs: now,
      });
    }
    await api.putChatPins(["mira", "ow-old"]);

    const { container, findByTestId, getByTestId } = renderOffice();
    fireEvent.click(getByTestId("office-tab-outsource"));
    const list = await findByTestId("outsource-list");
    await waitFor(() =>
      expect(
        Array.from(list.querySelectorAll(".outsource-row"), (r) => r.getAttribute("data-testid")),
      ).toEqual(["outsource-row-ow-old", "outsource-row-ow-new"]),
    );

    const input = container.querySelector("textarea.chat__input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "@" } });
    expect(
      Array.from(container.querySelectorAll('[role="option"]'), (o) => o.textContent),
    ).toEqual(["Mira特助", "外包 · O-2", "外包 · O-1"]);
  });
});
