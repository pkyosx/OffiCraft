import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, screen, within } from "@testing-library/react";
import { useState } from "react";
import { I18nProvider } from "../i18n";
import { ChatGroupPanel } from "./ChatGroupPanel";
import type { ChatGroup, ChatTab } from "../lib/chatGroups";

const NAMES: Record<string, string> = {
  mira: "Mira",
  beto: "Beto",
  clara: "Clara",
  "ow-1": "O-1",
  "ow-2": "O-2",
  "ow-3": "O-3",
};
const UNREAD: Record<string, number> = { clara: 3, beto: 1 };
const TYPES = [
  { typeKey: "review", displayName: "PR 審查", purpose: "" },
  { typeKey: "bugfix", displayName: "修 bug", purpose: "" },
  { typeKey: "docs", displayName: "文件", purpose: "" },
];
const WORKERS = [
  { id: "ow-1", taskTypeKey: "review" },
  { id: "ow-2", taskTypeKey: "bugfix" },
  { id: "ow-3", taskTypeKey: "review" },
];

let latest: readonly ChatGroup[] = [];

function Harness({ tab, ids, initial }: { tab: ChatTab; ids: string[]; initial: ChatGroup[] }) {
  const [groups, setGroups] = useState<readonly ChatGroup[]>(initial);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  latest = groups;
  let n = 0;
  return (
    <ChatGroupPanel
      tab={tab}
      ids={ids}
      groups={groups}
      update={(change) => setGroups((g) => change(g))}
      newGroupId={() => `cg-new-${++n}`}
      nameOf={(id) => NAMES[id]}
      unreadOf={(id) => UNREAD[id] ?? 0}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      mobile={false}
      taskTypes={TYPES}
      workers={WORKERS}
      className="list"
      testId="list"
      renderRow={(id, row) => (
        <div
          role="button"
          tabIndex={0}
          data-chat-row={row.rowId}
          data-testid={`row-${id}`}
          onContextMenu={row.onContextMenu}
          onKeyDown={(e) => row.menuKey(e)}
        >
          {NAMES[id]}
        </div>
      )}
      renderOverlay={(id) => <div>{NAMES[id]}</div>}
    />
  );
}

const g = (id: string, tab: ChatTab, name: string, memberIds: string[], taskTypeKeys: string[] = []): ChatGroup => ({
  id,
  tab,
  name,
  memberIds,
  taskTypeKeys,
});

function mount(tab: ChatTab, ids: string[], initial: ChatGroup[] = []) {
  return render(
    <I18nProvider>
      <Harness tab={tab} ids={ids} initial={initial} />
    </I18nProvider>,
  );
}
const headings = () => Array.from(document.querySelectorAll(".chat-groups__heading"), (h) => h.textContent);
const rows = () => Array.from(document.querySelectorAll("[data-chat-row]"), (r) => r.getAttribute("data-chat-row"));

beforeEach(() => {
  latest = [];
});

describe("ChatGroupPanel", () => {
  it("with no groups shows the rows without headings; 新增群組 asks for a name, refuses a blank or taken one, and creates the group", () => {
    mount("staff", ["mira", "beto"], [g("o", "outsource", "前端", [])]);
    expect(headings()).toEqual([]);
    expect(rows()).toEqual(["mira", "beto"]);

    fireEvent.click(screen.getByTestId("chat-group-new"));
    const entry = screen.getByTestId("chat-group-new-entry");
    const input = within(entry).getByRole("textbox");
    expect(document.activeElement).toBe(input);
    fireEvent.submit(entry);
    expect(within(entry).getByRole("alert").textContent).toBe("請輸入群組名稱");
    fireEvent.change(input, { target: { value: "字".repeat(41) } });
    fireEvent.submit(entry);
    expect(within(entry).getByRole("alert").textContent).toBe("群組名稱最多 40 字");

    fireEvent.change(input, { target: { value: " 前端 " } });
    fireEvent.submit(entry);
    expect(headings()).toEqual(["前端 · 0", "未分組 · 2"]);
    expect(latest).toEqual([g("cg-new-1", "staff", "前端", []), g("o", "outsource", "前端", [])]);

    fireEvent.click(screen.getByTestId("chat-group-new"));
    const again = screen.getByTestId("chat-group-new-entry");
    fireEvent.change(within(again).getByRole("textbox"), { target: { value: "前端" } });
    fireEvent.submit(again);
    expect(within(again).getByRole("alert").textContent).toBe("這個分頁已經有同名的群組");
    fireEvent.keyDown(within(again).getByRole("textbox"), { key: "Escape" });
    expect(screen.queryByTestId("chat-group-new-entry")).toBeNull();
  });

  it("each heading collapses and expands its own rows; a collapsed section shows its rows' summed unread", () => {
    mount("staff", ["mira", "beto", "clara"], [g("a", "staff", "核心", ["clara", "beto"])]);
    expect(headings()).toEqual(["核心 · 2", "未分組 · 1"]);
    const head = screen.getByTestId("chat-group-heading-a");
    fireEvent.click(head);
    expect(head.getAttribute("aria-expanded")).toBe("false");
    expect(rows()).toEqual(["mira"]);
    expect(headings()).toEqual(["核心 · 24", "未分組 · 1"]);

    fireEvent.click(screen.getByTestId("chat-group-heading-ungrouped"));
    expect(rows()).toEqual([]);
    fireEvent.click(head);
    expect(head.getAttribute("aria-expanded")).toBe("true");
    expect(rows()).toEqual(["clara", "beto"]);
    expect(headings()).toEqual(["核心 · 2", "未分組 · 1"]);
  });

  it("a member's menu (right-click, or Shift+F10 on the focused row) moves it to another group, to 未分組, or into a new group", () => {
    mount("staff", ["mira", "beto", "clara"], [g("a", "staff", "核心", ["clara"]), g("b", "staff", "工程", [])]);
    fireEvent.contextMenu(screen.getByTestId("row-mira"));
    expect(screen.getByRole("menu", { name: "Mira" })).toBeTruthy();
    fireEvent.click(screen.getByTestId("chat-member-menu-move"));
    const sub = screen.getByRole("menu", { name: "移到群組" });
    expect(
      within(sub)
        .getAllByRole("menuitemcheckbox")
        .map((i) => [i.textContent, i.getAttribute("aria-checked")]),
    ).toEqual([
      ["核心", "false"],
      ["工程", "false"],
      ["未分組", "true"],
    ]);
    fireEvent.click(screen.getByTestId("chat-member-menu-move-b"));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(latest.map((x) => x.memberIds)).toEqual([["clara"], ["mira"]]);

    const clara = screen.getByTestId("row-clara");
    clara.focus();
    fireEvent.keyDown(clara, { key: "F10", shiftKey: true });
    fireEvent.click(screen.getByTestId("chat-member-menu-move"));
    fireEvent.click(screen.getByTestId("chat-member-menu-move-ungrouped"));
    expect(latest.map((x) => x.memberIds)).toEqual([[], ["mira"]]);

    fireEvent.contextMenu(screen.getByTestId("row-beto"));
    fireEvent.click(screen.getByTestId("chat-member-menu-move"));
    fireEvent.click(screen.getByTestId("chat-member-menu-move-new"));
    const entry = screen.getByTestId("chat-group-new-entry");
    fireEvent.change(within(entry).getByRole("textbox"), { target: { value: "新" } });
    fireEvent.submit(entry);
    expect(latest.map((x) => [x.name, x.memberIds])).toEqual([
      ["核心", []],
      ["工程", ["mira"]],
      ["新", ["beto"]],
    ]);
  });

  it("the menu moves focus with the arrow keys, opens the submenu with ArrowRight, and Escape closes it back to where it was opened", () => {
    mount("staff", ["mira"], [g("a", "staff", "核心", [])]);
    const row = screen.getByTestId("row-mira");
    row.focus();
    fireEvent.keyDown(row, { key: "ContextMenu" });
    const move = screen.getByTestId("chat-member-menu-move");
    expect(document.activeElement).toBe(move);
    fireEvent.keyDown(move, { key: "ArrowRight" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-a"));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-ungrouped"));
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu", { name: "移到群組" })).toBeNull();
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(row);
  });

  it("a group's menu renames it inline and deletes it after confirming, its members going back to 未分組", () => {
    mount("staff", ["mira", "beto"], [g("a", "staff", "核心", ["beto"]), g("b", "staff", "工程", [])]);
    fireEvent.contextMenu(screen.getByTestId("chat-group-heading-a"));
    fireEvent.click(screen.getByTestId("chat-group-menu-rename"));
    const entry = screen.getByTestId("chat-group-rename");
    const input = within(entry).getByRole("textbox") as HTMLInputElement;
    expect(input.value).toBe("核心");
    fireEvent.change(input, { target: { value: "工程" } });
    fireEvent.submit(entry);
    expect(within(entry).getByRole("alert").textContent).toBe("這個分頁已經有同名的群組");
    fireEvent.change(input, { target: { value: "主力" } });
    fireEvent.submit(entry);
    expect(headings()).toEqual(["主力 · 1", "工程 · 0", "未分組 · 1"]);

    fireEvent.contextMenu(screen.getByTestId("chat-group-heading-a"));
    expect(screen.queryByTestId("chat-group-menu-autojoin")).toBeNull();
    fireEvent.click(screen.getByTestId("chat-group-menu-delete"));
    expect(screen.getByRole("alertdialog").textContent).toContain("刪除「主力」？成員會回到未分組。");
    fireEvent.click(screen.getByTestId("chat-group-delete-confirm"));
    expect(headings()).toEqual(["工程 · 0", "未分組 · 2"]);
    expect(latest).toEqual([g("b", "staff", "工程", [])]);
  });

  it("自動加入 lists every task type, marks one used by another group with where it is, and ticking it moves the rule there", () => {
    mount(
      "outsource",
      ["ow-1", "ow-2", "ow-3"],
      [g("x", "outsource", "審查", ["ow-1"]), g("y", "outsource", "修復", ["ow-2"], ["bugfix"])],
    );
    fireEvent.contextMenu(screen.getByTestId("chat-group-heading-x"));
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    const sub = screen.getByRole("menu", { name: "自動加入" });
    expect(
      within(sub)
        .getAllByRole("menuitemcheckbox")
        .map((i) => [i.textContent, i.getAttribute("aria-checked")]),
    ).toEqual([
      ["PR 審查", "false"],
      ["修 bug目前在「修復」", "false"],
      ["文件", "false"],
    ]);
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin-bugfix"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(latest).toEqual([
      g("x", "outsource", "審查", ["ow-1"], ["bugfix"]),
      g("y", "outsource", "修復", ["ow-2"], []),
    ]);
  });

  it("ticking a type that ungrouped workers have asks whether to bring them in: yes appends them, Escape only saves the rule", () => {
    mount("outsource", ["ow-1", "ow-2", "ow-3"], [g("x", "outsource", "審查", []), g("y", "outsource", "修復", ["ow-1"])]);
    const tick = () => {
      fireEvent.contextMenu(screen.getByTestId("chat-group-heading-x"));
      fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
      fireEvent.click(screen.getByTestId("chat-group-menu-autojoin-review"));
    };
    tick();
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("未分組裡有 1 位符合「PR 審查」的外包，要一起搬進「審查」嗎？")).toBeTruthy();
    expect(within(dialog).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["O-3"]);
    expect(document.activeElement?.textContent).toBe("一起搬進來");
    expect(latest[0].taskTypeKeys).toEqual([]);
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(latest).toEqual([g("x", "outsource", "審查", [], ["review"]), g("y", "outsource", "修復", ["ow-1"])]);

    fireEvent.contextMenu(screen.getByTestId("chat-group-heading-x"));
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin-review"));
    expect(latest[0].taskTypeKeys).toEqual([]);
    tick();
    fireEvent.click(within(screen.getByRole("dialog")).getByText("一起搬進來"));
    expect(latest).toEqual([g("x", "outsource", "審查", ["ow-3"], ["review"]), g("y", "outsource", "修復", ["ow-1"])]);
  });
});
