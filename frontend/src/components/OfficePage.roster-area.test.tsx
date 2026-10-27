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
import { render, waitFor, fireEvent, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockChat,
  __injectMockMember,
  __injectMockOutsourceWorker,
  __injectMockTask,
  __updateMockMember,
} from "../api/mock";
import type { TaskView } from "../api/adapter";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>
  );
}

beforeEach(() => {
  localStorage.clear();
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

async function groupIntoNew(row: Element, name: string) {
  fireEvent.keyDown(row, { key: "ContextMenu" });
  fireEvent.click(await screen.findByTestId("chat-member-menu-move-new"));
  const entry = await screen.findByTestId("chat-group-new-entry");
  fireEvent.change(within(entry).getByRole("textbox"), { target: { value: name } });
  fireEvent.submit(entry);
}

describe("OfficePage — chat groups", () => {
  it("the 篩選 counts and list follow the members' live unread, presence and activity (member SSE) while the dropdown is open", async () => {
    __injectMockMember({ id: "beto", kind: "staff", name: "Beto", presence: "offline" });
    __injectMockMember({ id: "clara", kind: "staff", name: "Clara", presence: "online" });
    const { container, findByTestId } = renderOffice();
    await waitFor(() => expect(container.querySelector('[data-chat-row="clara"]')).not.toBeNull());
    fireEvent.click(await findByTestId("chat-filter"));
    const items = () =>
      Array.from(screen.getByRole("menu", { name: "篩選" }).querySelectorAll(".chat-groups__menu-item"), (i) =>
        `${i.querySelector(".chat-groups__menu-text")!.textContent} ${i.querySelector(".chat-groups__menu-count")?.textContent ?? ""}`.trim(),
      );
    const rows = () => Array.from(container.querySelectorAll("[data-chat-row]"), (r) => r.getAttribute("data-chat-row"));
    const activity = (state: "busy" | "waiting" | "idle") => ({ state, busy_since: null, subagents: [], waits: [], ts: Date.now() / 1000 });
    await waitFor(() =>
      expect(items()).toEqual(["有未讀 0", "線上 1", "喚醒中 0", "停止中 0", "已停止 0", "離線 2", "工作中 0", "等待中 0", "閒置 0"]),
    );
    fireEvent.click(screen.getByTestId("chat-filter-menu-waiting"));
    expect(rows()).toEqual([]);

    __updateMockMember("beto", { presence: "online", activity: activity("waiting") });
    await waitFor(() =>
      expect(items()).toEqual(["有未讀 0", "線上 2", "喚醒中 0", "停止中 0", "已停止 0", "離線 1", "工作中 0", "等待中 1", "閒置 0", "清除"]),
    );
    expect(rows()).toEqual(["beto"]);

    __updateMockMember("clara", { activity: activity("idle") });
    await waitFor(() => expect(items()[8]).toBe("閒置 1"));
    fireEvent.click(screen.getByTestId("chat-filter-menu-idle"));
    expect(rows()).toEqual(["beto", "clara"]);

    __injectMockChat({ id: "m-1", from: "clara", to: "owner", body: "嗨", ts: Date.now() / 1000, attachments: [], replyCardId: null });
    __updateMockMember("clara", { presence: "offline" });
    await waitFor(() =>
      expect(items()).toEqual(["有未讀 1", "線上 1", "喚醒中 0", "停止中 0", "已停止 0", "離線 2", "工作中 0", "等待中 1", "閒置 0", "清除"]),
    );
    expect(rows()).toEqual(["beto"]);

    fireEvent.click(screen.getByTestId("chat-filter-menu-offline"));
    expect(screen.getByTestId("chat-groups-empty").textContent).toBe("沒有符合篩選的成員");
  });

  it("on desktop the default chat is the first row shown: grouping a later member puts that member first and opens its chat", async () => {
    __injectMockMember({ id: "beto", kind: "staff", name: "Beto" });
    const { container, findAllByText } = renderOffice();
    await findAllByText("Beto");
    await waitFor(() =>
      expect(container.querySelector(".chat__header-name")?.textContent).toContain("Mira"),
    );

    await groupIntoNew(container.querySelector('[data-chat-row="beto"]')!, "核心");

    await waitFor(() =>
      expect(container.querySelector(".chat__header-name")?.textContent).toContain("Beto"),
    );
    expect(
      Array.from(container.querySelectorAll(".chat-groups__heading"), (h) => h.textContent),
    ).toEqual(["核心 · 1", "未分組 · 1"]);
  });

  it("collapsing a group on one tab leaves the other tab's groups as they were, survives switching tabs, and is remembered by this browser for each tab", async () => {
    __injectMockOutsourceWorker({
      id: "ow-1",
      codename: "O-1",
      model: "",
      effort: "",
      taskId: "t-1",
      taskTitle: "",
      taskStatus: "in_progress",
      createdTs: Date.now() / 1000,
    });
    const { container, getByTestId, findByTestId, unmount } = renderOffice();
    await waitFor(() => expect(container.querySelector('[data-chat-row="mira"]')).not.toBeNull());
    await groupIntoNew(container.querySelector('[data-chat-row="mira"]')!, "核心");
    const staffHead = () => container.querySelector(".chat-groups__heading")!;
    const ungroupedHead = () => getByTestId("chat-group-heading-ungrouped");
    fireEvent.click(staffHead());
    fireEvent.click(ungroupedHead());
    expect(staffHead().getAttribute("aria-expanded")).toBe("false");
    expect(ungroupedHead().getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(getByTestId("office-tab-outsource"));
    await findByTestId("outsource-row-ow-1");
    expect(container.querySelectorAll(".chat-groups__heading")).toHaveLength(0);
    await groupIntoNew(getByTestId("outsource-row-ow-1"), "外包組");
    expect(staffHead().textContent).toBe("外包組 · 1");
    expect(staffHead().getAttribute("aria-expanded")).toBe("true");
    expect(ungroupedHead().getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(getByTestId("office-tab-staff"));
    await waitFor(() => expect(staffHead().textContent).toBe("核心 · 1"));
    expect(staffHead().getAttribute("aria-expanded")).toBe("false");
    expect(ungroupedHead().getAttribute("aria-expanded")).toBe("false");

    fireEvent.click(getByTestId("office-tab-outsource"));
    await waitFor(() => expect(staffHead().textContent).toBe("外包組 · 1"));
    fireEvent.click(staffHead());
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["cg-000000000001", "__ungrouped"]);
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_outsource")!)).toEqual(["cg-000000000002"]);
    unmount();

    const again = renderOffice();
    await waitFor(() => expect(again.container.querySelector(".chat-groups__heading")?.textContent).toBe("核心 · 1"));
    expect(again.getByTestId("chat-group-heading-ungrouped").getAttribute("aria-expanded")).toBe("false");
    expect(again.container.querySelector(".chat-groups__heading")!.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(again.getByTestId("office-tab-outsource"));
    await waitFor(() => expect(again.container.querySelector(".chat-groups__heading")?.textContent).toBe("外包組 · 1"));
    expect(again.container.querySelector(".chat-groups__heading")!.getAttribute("aria-expanded")).toBe("false");
    expect(again.getByTestId("chat-group-heading-ungrouped").getAttribute("aria-expanded")).toBe("true");
  });

  it("this browser saves a group folded before its first save has answered, once the group's id comes back, and while 篩選 is on a heading folds and saves as usual, 全部收合／展開 saves only the groups shown, and the folding stays after 清除", async () => {
    const putChatGroups = api.putChatGroups.bind(api);
    let answer = () => {};
    const answered = new Promise<void>((resolve) => (answer = resolve));
    vi.spyOn(api, "putChatGroups").mockImplementationOnce(async (body) => {
      await answered;
      return putChatGroups(body);
    });
    const { container, getByTestId } = renderOffice();
    await waitFor(() => expect(container.querySelector('[data-chat-row="mira"]')).not.toBeNull());
    await groupIntoNew(container.querySelector('[data-chat-row="mira"]')!, "核心");
    const head = () => container.querySelector<HTMLElement>(".chat-groups__heading")!;
    fireEvent.click(head());
    expect(head().getAttribute("aria-expanded")).toBe("false");
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual([]);
    answer();
    await waitFor(() =>
      expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["cg-000000000001"]),
    );

    fireEvent.click(head());
    fireEvent.click(getByTestId("chat-group-heading-ungrouped"));
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["__ungrouped"]);
    fireEvent.click(getByTestId("chat-filter"));
    fireEvent.change(await screen.findByTestId("chat-filter-search"), { target: { value: "mira" } });
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(getByTestId("chat-filter").textContent).toBe("篩選 · 1");
    const saved = () => JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!);
    fireEvent.click(head());
    expect([head().textContent, head().getAttribute("aria-expanded"), saved()]).toEqual([
      "核心 · 1",
      "false",
      ["cg-000000000001", "__ungrouped"],
    ]);
    const all = getByTestId("chat-groups-collapse-all");
    expect(all.getAttribute("aria-label")).toBe("全部展開");
    fireEvent.click(all);
    expect([head().getAttribute("aria-expanded"), saved()]).toEqual(["true", ["__ungrouped"]]);
    fireEvent.click(all);
    expect([head().getAttribute("aria-expanded"), saved()]).toEqual(["false", ["cg-000000000001", "__ungrouped"]]);
    fireEvent.click(getByTestId("chat-filter"));
    fireEvent.click(await screen.findByTestId("chat-filter-menu-clear"));
    expect(getByTestId("chat-filter").textContent).toBe("篩選");
    expect([head().getAttribute("aria-expanded"), saved()]).toEqual(["false", ["cg-000000000001", "__ungrouped"]]);
    expect(getByTestId("chat-group-heading-ungrouped").getAttribute("aria-expanded")).toBe("false");
  });

  it("this browser's folding forgets a deleted group, follows 全部收合／展開, and reads a broken saved value as nothing folded", async () => {
    localStorage.setItem("oc_chat_group_collapsed_staff", "{not json");
    localStorage.setItem("oc_chat_group_collapsed_outsource", JSON.stringify({ a: 1 }));
    const { container, getByTestId } = renderOffice();
    await waitFor(() => expect(container.querySelector('[data-chat-row="mira"]')).not.toBeNull());
    await groupIntoNew(container.querySelector('[data-chat-row="mira"]')!, "核心");
    const head = () => container.querySelector(".chat-groups__heading")!;
    await waitFor(() => expect(head().textContent).toBe("核心 · 1"));
    expect(head().getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(getByTestId("chat-groups-collapse-all"));
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["cg-000000000001", "__ungrouped"]);
    fireEvent.click(getByTestId("chat-groups-collapse-all"));
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual([]);

    fireEvent.click(head());
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["cg-000000000001"]);
    fireEvent.click(container.querySelector('[data-testid^="chat-group-more-"]')!);
    fireEvent.click(await screen.findByTestId("chat-group-menu-delete"));
    fireEvent.click(await screen.findByTestId("chat-group-delete-confirm"));
    await waitFor(() => expect(container.querySelectorAll(".chat-groups__heading")).toHaveLength(0));
    await groupIntoNew(container.querySelector('[data-chat-row="mira"]')!, "工程");
    await waitFor(() => expect(head().textContent).toBe("工程 · 1"));
    fireEvent.click(getByTestId("chat-group-heading-ungrouped"));
    expect(JSON.parse(localStorage.getItem("oc_chat_group_collapsed_staff")!)).toEqual(["__ungrouped"]);
  });

  it("grouping a later staff member and a later outsource worker moves them first in the sidebar but leaves the @-mention picker in its default order", async () => {
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

    __injectMockMember({ id: "beto", kind: "staff", name: "Beto", role_key: "backend", role_name: "後端" });
    __injectMockMember({ id: "clara", kind: "staff", name: "Clara", role_key: "frontend", role_name: "前端" });

    const { container, findByTestId, getByTestId } = renderOffice();
    await waitFor(() => expect(container.querySelector('[data-chat-row="clara"]')).not.toBeNull());
    await groupIntoNew(container.querySelector('[data-chat-row="clara"]')!, "前端");
    await waitFor(() =>
      expect(Array.from(container.querySelectorAll("[data-chat-row]"), (r) => r.getAttribute("data-chat-row"))).toEqual([
        "clara",
        "mira",
        "beto",
      ]),
    );
    fireEvent.click(getByTestId("office-tab-outsource"));
    const list = await findByTestId("outsource-list");
    const order = () =>
      Array.from(list.querySelectorAll(".outsource-row"), (r) => r.getAttribute("data-testid"));
    await waitFor(() => expect(order()).toEqual(["outsource-row-ow-new", "outsource-row-ow-old"]));
    await groupIntoNew(getByTestId("outsource-row-ow-old"), "舊單");
    await waitFor(() => expect(order()).toEqual(["outsource-row-ow-old", "outsource-row-ow-new"]));

    const input = container.querySelector("textarea.chat__input") as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: "@" } });
    expect(
      Array.from(container.querySelectorAll('[role="option"]'), (o) => o.textContent),
    ).toEqual(["Mira特助", "Beto後端", "Clara前端", "外包 · O-2", "外包 · O-1"]);
  });
});
