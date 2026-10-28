import { describe, it, expect, beforeEach } from "vitest";
import { act, render, fireEvent, screen, within } from "@testing-library/react";
import { useState } from "react";
import { I18nProvider } from "../i18n";
import { ChatGroupPanel } from "./ChatGroupPanel";
import type { ChatGroup, ChatTab } from "../lib/chatGroups";
import type { RowFacts } from "../lib/chatFilter";

const NAMES: Record<string, string> = {
  mira: "Mira",
  beto: "Beto",
  clara: "Clara",
  dora: "Dora",
  evan: "Evan",
  "ow-1": "O-1",
  "ow-2": "O-2",
  "ow-3": "O-3",
};
const UNREAD: Record<string, number> = { clara: 3, beto: 1 };
const ROLES: Record<string, string> = { mira: "特助", beto: "後端", clara: "前端", dora: "QA 測試", evan: "產品經理" };
type Live = {
  unread: Record<string, number>;
  presence: Record<string, RowFacts["presence"]>;
  activity: Record<string, RowFacts["activity"]>;
};
const LIVE: Live = {
  unread: UNREAD,
  presence: { mira: "online-awake", beto: "offline", clara: "online-awake", dora: "waking", evan: "stopped" },
  activity: { mira: "idle", clara: "busy", dora: "waiting" },
};
let setLive: (change: (l: Live) => Live) => void = () => {};
const TYPES = [
  { typeKey: "review", displayName: "PR 審查", purpose: "" },
  { typeKey: "bugfix", displayName: "修 bug", purpose: "" },
  { typeKey: "docs", displayName: "", purpose: "" },
];
const WORKERS = [
  { id: "ow-1", taskTypeKey: "review" },
  { id: "ow-2", taskTypeKey: "bugfix" },
  { id: "ow-3", taskTypeKey: "review" },
];

let latest: readonly ChatGroup[] = [];
// Each update is one save (useChatGroups sends one PUT per update).
let saves = 0;

function Harness({ tab, ids, initial }: { tab: ChatTab; ids: string[]; initial: ChatGroup[] }) {
  const [groups, setGroups] = useState<readonly ChatGroup[]>(initial);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [live, changeLive] = useState(LIVE);
  setLive = changeLive;
  latest = groups;
  let n = 0;
  return (
    <ChatGroupPanel
      tab={tab}
      ids={ids}
      groups={groups}
      update={(change) => {
        saves += 1;
        setGroups((g) => change(g));
      }}
      newGroupId={() => `cg-new-${++n}`}
      nameOf={(id) => NAMES[id]}
      unreadOf={(id) => live.unread[id] ?? 0}
      factsOf={(id) => ({
        presence: live.presence[id] ?? "offline",
        activity: live.activity[id] ?? null,
        names: [NAMES[id], ROLES[id] ?? ""],
      })}
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
  saves = 0;
  localStorage.clear();
});

describe("ChatGroupPanel", () => {
  it("with no groups shows the rows without headings and 全部收合 greyed out; the toolbar's 新增群組 asks for a name at the top of the list, refuses a blank or taken one, and creates the group first", () => {
    mount("staff", ["mira", "beto"], [g("o", "outsource", "前端", [])]);
    expect(headings()).toEqual([]);
    expect(rows()).toEqual(["mira", "beto"]);
    expect((screen.getByTestId("chat-groups-collapse-all") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("chat-group-new").getAttribute("aria-label")).toBe("新增群組");

    fireEvent.click(screen.getByTestId("chat-group-new"));
    const entry = screen.getByTestId("chat-group-new-entry");
    expect(screen.getByTestId("list").firstElementChild).toBe(entry);
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
    expect(screen.getByTestId("chat-group-drop-cg-new-1").textContent).toBe("把成員拖曳到這裡");
    expect(latest).toEqual([g("cg-new-1", "staff", "前端", []), g("o", "outsource", "前端", [])]);

    fireEvent.click(screen.getByTestId("chat-group-new"));
    const again = screen.getByTestId("chat-group-new-entry");
    fireEvent.change(within(again).getByRole("textbox"), { target: { value: "前端" } });
    fireEvent.submit(again);
    expect(within(again).getByRole("alert").textContent).toBe("這個分頁已經有同名的群組");
    fireEvent.change(within(again).getByRole("textbox"), { target: { value: "後端" } });
    fireEvent.submit(again);
    expect(headings()).toEqual(["後端 · 0", "前端 · 0", "未分組 · 2"]);
    expect((screen.getByTestId("chat-groups-collapse-all") as HTMLButtonElement).disabled).toBe(false);
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

    const all = screen.getByTestId("chat-groups-collapse-all");
    expect(all.getAttribute("aria-label")).toBe("全部收合");
    fireEvent.click(all);
    expect(rows()).toEqual([]);
    expect(headings()).toEqual(["核心 · 24", "未分組 · 1"]);
    expect(all.getAttribute("aria-label")).toBe("全部展開");
    expect(all.getAttribute("title")).toBe("全部展開");
    fireEvent.click(all);
    expect(rows()).toEqual(["clara", "beto", "mira"]);
    expect(all.getAttribute("aria-label")).toBe("全部收合");
    fireEvent.click(screen.getByTestId("chat-group-heading-ungrouped"));
    expect(all.getAttribute("aria-label")).toBe("全部收合");
    fireEvent.click(all);
    expect(rows()).toEqual([]);
  });

  const STAFF = ["mira", "beto", "clara", "dora", "evan"];
  const STAFF_GROUPS = () => [g("a", "staff", "核心", ["clara", "beto"]), g("b", "staff", "工程", ["dora"])];
  const filterButton = () => screen.getByTestId("chat-filter");
  const filterItems = () =>
    Array.from(
      screen.getByRole("menu", { name: "篩選" }).querySelectorAll(".chat-groups__menu-item"),
      (i) =>
        `${i.querySelector(".chat-groups__menu-text")!.textContent}${i.getAttribute("aria-checked") === "true" ? "✓" : ""} ${i.querySelector(".chat-groups__menu-count")?.textContent ?? ""}`.trim(),
    );
  const tick = (key: string) => fireEvent.click(screen.getByTestId(`chat-filter-menu-${key}`));

  const sections = () =>
    Array.from(screen.getByRole("menu", { name: "篩選" }).querySelectorAll(".chat-groups__menu-section"), (h) => h.textContent);
  const ALL = ["有未讀 2", "線上 2", "喚醒中 1", "停止中 0", "已停止 1", "離線 1", "工作中 1", "等待中 1", "閒置 1"];

  it("篩選 shows the rows matching any tick within a section and every section that has one, and the name search; each option shows its own count, groups without a match are left out, the rest fold and unfold as usual (a folded one showing its matching count and their unread), 全部收合／展開 acts on the groups shown, and 清除 brings everything back with the folding as left", () => {
    mount("staff", STAFF, STAFF_GROUPS());
    fireEvent.click(screen.getByTestId("chat-group-heading-a"));
    expect(rows()).toEqual(["dora", "mira", "evan"]);
    expect(filterButton().textContent).toBe("篩選");

    fireEvent.click(filterButton());
    expect(document.activeElement).toBe(screen.getByTestId("chat-filter-search"));
    expect(screen.getByTestId("chat-filter-search").getAttribute("placeholder")).toBe("搜尋名稱…");
    const panel = screen.getByRole("dialog", { name: "篩選" });
    expect(Array.from(panel.querySelectorAll("input, [role='menu']"), (el) => el.getAttribute("role") ?? el.tagName)).toEqual([
      "INPUT",
      "menu",
    ]);
    expect(sections()).toEqual(["未讀", "連線", "狀態"]);
    expect(filterItems()).toEqual(ALL);

    tick("unread");
    const all = screen.getByTestId("chat-groups-collapse-all") as HTMLButtonElement;
    expect(rows()).toEqual([]);
    expect(headings()).toEqual(["核心 · 24"]);
    expect(filterButton().textContent).toBe("篩選 · 1");
    expect(filterButton().className).toContain("chat-groups__filter--on");
    expect([all.disabled, all.getAttribute("aria-label")]).toEqual([false, "全部展開"]);
    fireEvent.click(all);
    expect(rows()).toEqual(["clara", "beto"]);
    expect(all.getAttribute("aria-label")).toBe("全部收合");
    fireEvent.click(all);
    expect(rows()).toEqual([]);
    fireEvent.click(screen.getByTestId("chat-group-heading-a"));
    expect(rows()).toEqual(["clara", "beto"]);
    tick("unread");

    tick("online-awake");
    tick("waking");
    expect(rows()).toEqual(["clara", "dora", "mira"]);
    expect(headings()).toEqual(["核心 · 1", "工程 · 1", "未分組 · 1"]);
    expect(filterButton().textContent).toBe("篩選 · 2");
    const eng = screen.getByTestId("chat-group-heading-b");
    const loose = screen.getByTestId("chat-group-heading-ungrouped");
    fireEvent.click(eng);
    fireEvent.click(loose);
    expect([eng, loose].map((h) => [h.getAttribute("aria-expanded"), h.getAttribute("aria-disabled")])).toEqual([
      ["false", "false"],
      ["false", null],
    ]);
    expect(rows()).toEqual(["clara"]);
    fireEvent.click(all);
    expect(headings()).toEqual(["核心 · 13", "工程 · 1", "未分組 · 1"]);
    fireEvent.click(all);
    expect(rows()).toEqual(["clara", "dora", "mira"]);

    tick("busy");
    expect(rows()).toEqual(["clara"]);
    tick("waiting");
    expect(rows()).toEqual(["clara", "dora"]);
    expect(filterButton().textContent).toBe("篩選 · 4");

    tick("unread");
    expect(rows()).toEqual(["clara"]);
    expect(filterItems()).toEqual([
      "有未讀✓ 2",
      "線上✓ 2",
      "喚醒中✓ 1",
      "停止中 0",
      "已停止 1",
      "離線 1",
      "工作中✓ 1",
      "等待中✓ 1",
      "閒置 1",
      "清除",
    ]);

    fireEvent.change(screen.getByTestId("chat-filter-search"), { target: { value: "qa" } });
    expect(rows()).toEqual([]);
    expect(headings()).toEqual([]);
    expect(screen.getByTestId("chat-groups-empty").textContent).toBe("沒有符合篩選的成員");
    expect(filterButton().textContent).toBe("篩選 · 6");

    tick("unread");
    expect(rows()).toEqual(["dora"]);
    expect(headings()).toEqual(["工程 · 1"]);
    tick("online-awake");
    tick("waking");
    tick("busy");
    tick("waiting");
    tick("stopped");
    tick("offline");
    fireEvent.change(screen.getByTestId("chat-filter-search"), { target: { value: "EVA" } });
    expect(rows()).toEqual(["evan"]);
    fireEvent.change(screen.getByTestId("chat-filter-search"), { target: { value: "後端" } });
    expect(rows()).toEqual(["beto"]);
    tick("stopped");
    tick("offline");
    tick("idle");
    fireEvent.change(screen.getByTestId("chat-filter-search"), { target: { value: "" } });
    expect(rows()).toEqual(["mira"]);
    expect(filterButton().textContent).toBe("篩選 · 1");
    fireEvent.click(screen.getByTestId("chat-group-heading-ungrouped"));
    expect(rows()).toEqual([]);

    tick("clear");
    expect(filterButton().textContent).toBe("篩選");
    expect((screen.getByTestId("chat-filter-search") as HTMLInputElement).value).toBe("");
    expect(filterItems()).toEqual(ALL);
    expect(rows()).toEqual(["clara", "beto", "dora"]);
    expect(screen.getByTestId("chat-group-heading-a").getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByTestId("chat-group-heading-b").getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByTestId("chat-group-heading-ungrouped").getAttribute("aria-expanded")).toBe("false");
    expect(latest).toEqual(STAFF_GROUPS());

    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(filterButton());

    fireEvent.click(filterButton());
    fireEvent.keyDown(screen.getByTestId("chat-filter-search"), { key: "Tab" });
    expect(screen.getByRole("dialog", { name: "篩選" })).toBeTruthy();
    fireEvent.keyDown(screen.getByTestId("chat-filter-search"), { key: "Tab", shiftKey: true });
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(filterButton());
    tick("idle");
    expect([headings(), rows()]).toEqual([["未分組 · 1"], []]);
    fireEvent.click(screen.getByTestId("chat-group-new"));
    expect(filterButton().textContent).toBe("篩選");
    expect(rows()).toEqual(["clara", "beto", "dora"]);
    expect(screen.getByTestId("list").firstElementChild).toBe(screen.getByTestId("chat-group-new-entry"));
  });

  it("this browser keeps each tab's ticks across a remount but not the name search, and drops saved entries it does not know", () => {
    localStorage.setItem("oc_chat_filter_staff", JSON.stringify(["busy", "online-awake", "unread", "online", 7]));
    const first = mount("staff", STAFF, STAFF_GROUPS());
    expect(filterButton().textContent).toBe("篩選 · 3");
    expect(rows()).toEqual(["clara"]);
    fireEvent.click(filterButton());
    tick("busy");
    tick("unread");
    fireEvent.change(screen.getByTestId("chat-filter-search"), { target: { value: "mira" } });
    expect(rows()).toEqual(["mira"]);
    first.unmount();

    const second = mount("staff", STAFF, STAFF_GROUPS());
    expect(filterButton().textContent).toBe("篩選 · 1");
    expect(rows()).toEqual(["clara", "mira"]);
    second.unmount();

    localStorage.setItem("oc_chat_filter_outsource", "not json");
    mount("outsource", ["ow-1", "ow-2"]);
    expect(filterButton().textContent).toBe("篩選");
    expect(rows()).toEqual(["ow-1", "ow-2"]);
  });

  it("with the 篩選 open, the counts and the list follow the rows' unread, presence and activity as they change", () => {
    mount("staff", STAFF, STAFF_GROUPS());
    fireEvent.click(filterButton());
    tick("waiting");
    expect(rows()).toEqual(["dora"]);

    act(() =>
      setLive((l) => ({
        unread: { ...l.unread, evan: 2 },
        presence: { ...l.presence, dora: "offline", evan: "online-awake" },
        activity: { mira: "waiting", clara: "idle", evan: "idle" },
      })),
    );
    expect(filterItems()).toEqual([
      "有未讀 3",
      "線上 3",
      "喚醒中 0",
      "停止中 0",
      "已停止 0",
      "離線 2",
      "工作中 0",
      "等待中✓ 1",
      "閒置 2",
      "清除",
    ]);
    expect(rows()).toEqual(["mira"]);
    tick("idle");
    expect(rows()).toEqual(["clara", "mira", "evan"]);
    tick("offline");
    expect(rows()).toEqual([]);
  });

  it("a right-click on a row is left to the browser and a touch long-press's own menu is held off; a member's menu (the menu key or Shift+F10 on the focused row) lists the groups under 移到群組 and moves it to another group, to 未分組, or into a new group", () => {
    mount("staff", ["mira", "beto", "clara"], [g("a", "staff", "核心", ["clara"]), g("b", "staff", "工程", [])]);
    const mira = screen.getByTestId("row-mira");
    const notPrevented = fireEvent.contextMenu(mira);
    expect(notPrevented).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
    const touchHold = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    Object.defineProperty(touchHold, "pointerType", { value: "touch" });
    mira.dispatchEvent(touchHold);
    expect(touchHold.defaultPrevented).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
    mira.focus();
    fireEvent.keyDown(mira, { key: "ContextMenu" });
    const menu = screen.getByRole("menu", { name: "Mira" });
    expect(Array.from(menu.children, (c) => c.textContent)).toEqual(["移到群組", "核心", "工程", "未分組", "", "新群組…"]);
    expect(
      Array.from(menu.querySelectorAll(".chat-groups__menu-item"), (i) => [i.textContent, i.getAttribute("aria-checked")]),
    ).toEqual([
      ["核心", "false"],
      ["工程", "false"],
      ["未分組", "true"],
      ["新群組…", null],
    ]);
    fireEvent.click(screen.getByTestId("chat-member-menu-move-b"));
    expect(screen.queryByRole("menu")).toBeNull();
    expect(latest.map((x) => x.memberIds)).toEqual([["clara"], ["mira"]]);

    const clara = screen.getByTestId("row-clara");
    clara.focus();
    fireEvent.keyDown(clara, { key: "F10", shiftKey: true });
    fireEvent.click(screen.getByTestId("chat-member-menu-move-ungrouped"));
    expect(latest.map((x) => x.memberIds)).toEqual([[], ["mira"]]);

    fireEvent.keyDown(screen.getByTestId("row-beto"), { key: "ContextMenu" });
    fireEvent.click(screen.getByTestId("chat-member-menu-move-new"));
    const entry = screen.getByTestId("chat-group-new-entry");
    fireEvent.change(within(entry).getByRole("textbox"), { target: { value: "新" } });
    fireEvent.submit(entry);
    expect(latest.map((x) => [x.name, x.memberIds])).toEqual([
      ["新", ["beto"]],
      ["核心", []],
      ["工程", ["mira"]],
    ]);
  });

  it("the menu takes focus on its first group, moves it with the arrow keys and Home/End, and Escape closes it back to where it was opened", () => {
    mount("staff", ["mira"], [g("a", "staff", "核心", [])]);
    const row = screen.getByTestId("row-mira");
    row.focus();
    fireEvent.keyDown(row, { key: "ContextMenu" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-a"));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-ungrouped"));
    fireEvent.keyDown(document.activeElement!, { key: "End" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-new"));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-member-menu-move-a"));
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(row);
  });

  it("a group heading's 「⋯」 opens that group's menu without folding the group, and closing the menu gives focus back to it; 未分組's row is its heading alone", () => {
    mount("staff", ["mira", "beto"], [g("a", "staff", "核心", ["beto"])]);
    const more = screen.getByTestId("chat-group-more-a");
    const rowButtons = (heading: HTMLElement) =>
      Array.from(heading.parentElement!.querySelectorAll("button"), (b) => b.getAttribute("aria-label") ?? b.textContent);
    expect(rowButtons(screen.getByTestId("chat-group-heading-a"))).toEqual(["核心 · 1", "群組選項：核心"]);
    expect(rowButtons(screen.getByTestId("chat-group-heading-ungrouped"))).toEqual(["未分組 · 1"]);

    more.focus();
    fireEvent.click(more);
    const menu = screen.getByRole("menu", { name: "核心" });
    expect(within(menu).getAllByRole("menuitem").map((i) => i.textContent)).toEqual(["重新命名", "刪除群組"]);
    expect(screen.getByTestId("chat-group-heading-a").getAttribute("aria-expanded")).toBe("true");
    expect(rows()).toEqual(["beto", "mira"]);

    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(more);
  });

  it("a right-click on a group heading is left to the browser; a group's menu renames it inline and deletes it after confirming, its members going back to 未分組; from the keyboard the confirmation takes focus and 取消 gives it back to the heading", () => {
    mount("staff", ["mira", "beto"], [g("a", "staff", "核心", ["beto"]), g("b", "staff", "工程", [])]);
    const notPrevented = fireEvent.contextMenu(screen.getByTestId("chat-group-heading-a"));
    expect(notPrevented).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(screen.getByTestId("chat-group-more-a"));
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

    const heading = screen.getByTestId("chat-group-heading-a");
    heading.focus();
    fireEvent.keyDown(heading, { key: "F10", shiftKey: true });
    expect(screen.queryByTestId("chat-group-menu-autojoin")).toBeNull();
    fireEvent.click(screen.getByTestId("chat-group-menu-delete"));
    expect(screen.getByRole("alertdialog").textContent).toContain("刪除「主力」？成員會回到未分組。");
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-delete-confirm"));
    fireEvent.keyDown(document.activeElement!, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-delete-cancel"));
    fireEvent.click(screen.getByTestId("chat-group-delete-cancel"));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(document.activeElement).toBe(heading);
    expect(headings()).toEqual(["主力 · 1", "工程 · 0", "未分組 · 1"]);

    fireEvent.click(screen.getByTestId("chat-group-more-a"));
    fireEvent.click(screen.getByTestId("chat-group-menu-delete"));
    fireEvent.click(screen.getByTestId("chat-group-delete-confirm"));
    expect(headings()).toEqual(["工程 · 0", "未分組 · 2"]);
    expect(latest).toEqual([g("b", "staff", "工程", [])]);
  });

  const autoJoinRows = () =>
    Array.from(screen.getByTestId("chat-group-autojoin").querySelectorAll("[role='checkbox']"), (i) => [
      i.textContent,
      i.getAttribute("aria-checked"),
    ]);

  it("自動加入… opens a dialog titled with the group, its sentence and every task type, a type owned by another group saying where it is; a tick (after 都不搬 for that group's workers) moves the rule there, an untick saves at once, and 完成 closes it with focus back on the 「⋯」", () => {
    mount(
      "outsource",
      ["ow-1", "ow-2", "ow-3"],
      [g("x", "outsource", "審查", ["ow-1"]), g("y", "outsource", "修復", ["ow-2"], ["bugfix"])],
    );
    const more = screen.getByTestId("chat-group-more-x");
    more.focus();
    fireEvent.click(more);
    expect(screen.getByTestId("chat-group-menu-autojoin").textContent).toBe("自動加入…");
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    expect(screen.queryByRole("menu")).toBeNull();
    const dialog = screen.getByRole("dialog", { name: "自動加入 · 審查" });
    expect(within(dialog).getByText("勾選的任務類型，該類型的外包會自動加入這個群組。")).toBeTruthy();
    expect(autoJoinRows()).toEqual([
      ["PR 審查", "false"],
      ["修 bug目前在「修復」", "false"],
      ["docs", "false"],
    ]);
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-autojoin-review"));

    fireEvent.click(screen.getByTestId("chat-group-autojoin-bugfix"));
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-none"));
    expect(latest).toEqual([
      g("x", "outsource", "審查", ["ow-1"], ["bugfix"]),
      g("y", "outsource", "修復", ["ow-2"], []),
    ]);
    expect(autoJoinRows()).toEqual([
      ["PR 審查", "false"],
      ["修 bug", "true"],
      ["docs", "false"],
    ]);
    fireEvent.click(screen.getByTestId("chat-group-autojoin-bugfix"));
    expect(latest[0].taskTypeKeys).toEqual([]);

    fireEvent.keyDown(document.activeElement!, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-autojoin-done"));
    fireEvent.keyDown(document.activeElement!, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-autojoin-review"));
    fireEvent.keyDown(document.activeElement!, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(screen.getByTestId("chat-group-autojoin-done"));
    fireEvent.click(screen.getByTestId("chat-group-autojoin-done"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(more);

    fireEvent.click(more);
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(more);

    fireEvent.click(more);
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    const backdrop = screen.getByTestId("chat-group-autojoin").parentElement!;
    fireEvent.pointerDown(backdrop);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fireEvent.click(document.body, { detail: 1 })).toBe(false);
    expect(fireEvent.click(more, { detail: 1 })).toBe(true);
    expect(screen.getByTestId("chat-group-menu-autojoin")).toBeTruthy();

    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    fireEvent.pointerDown(screen.getByTestId("chat-group-autojoin").parentElement!);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(fireEvent.click(more, { detail: 0 })).toBe(true);
    expect(screen.getByTestId("chat-group-menu-autojoin")).toBeTruthy();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.pointerDown(more);
    expect(fireEvent.click(more, { detail: 1 })).toBe(true);
    expect(screen.getByTestId("chat-group-menu-autojoin")).toBeTruthy();
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
  });

  it("ticking a type asks, over the 自動加入 dialog, about every worker of that type outside the group, ungrouped or in another group, all ticked; 搬進勾選的 moves the ticked ones out of where they were with the rule in one save, 都不搬 or Escape only saves the rule, the dialog stays with focus back on the box, and once they are all in no question is asked", () => {
    mount("outsource", ["ow-1", "ow-2", "ow-3"], [g("x", "outsource", "審查", []), g("y", "outsource", "修復", ["ow-1"])]);
    expect(screen.getByTestId("chat-group-drop-x").textContent).toBe("把成員拖曳到這裡");
    fireEvent.click(screen.getByTestId("chat-group-more-x"));
    fireEvent.click(screen.getByTestId("chat-group-menu-autojoin"));
    const review = screen.getByTestId("chat-group-autojoin-review");
    const asked = () =>
      Array.from(screen.getByTestId("chat-groups-bring-in").querySelectorAll("[role='checkbox']"), (row) => [
        row.querySelector(".chat-groups__autojoin-label")!.textContent,
        row.querySelector(".chat-groups__autojoin-owner")!.textContent,
        row.getAttribute("aria-checked"),
      ]);

    fireEvent.click(review);
    const ask = screen.getByTestId("chat-groups-bring-in");
    expect(within(ask).getByRole("heading").textContent).toBe("要把目前的「PR 審查」外包搬進「審查」嗎？");
    expect(asked()).toEqual([
      ["O-1", "目前在「修復」", "true"],
      ["O-3", "未分組", "true"],
    ]);
    expect(within(ask).getAllByRole("button", { name: /搬/ }).map((b) => b.textContent)).toEqual([
      "搬進勾選的",
      "都不搬，只套用到之後的新外包",
    ]);
    expect(document.activeElement).toBe(screen.getByTestId("chat-groups-bring-in-move"));
    expect(saves).toBe(0);
    expect(screen.getAllByRole("dialog").map((d) => d.getAttribute("data-testid"))).toEqual([
      "chat-group-autojoin",
      "chat-groups-bring-in",
    ]);

    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    expect(screen.queryByTestId("chat-groups-bring-in")).toBeNull();
    expect(screen.getByTestId("chat-group-autojoin")).toBeTruthy();
    expect(document.activeElement).toBe(review);
    expect(latest).toEqual([g("x", "outsource", "審查", [], ["review"]), g("y", "outsource", "修復", ["ow-1"])]);
    expect(saves).toBe(1);

    fireEvent.click(review);
    expect(latest[0].taskTypeKeys).toEqual([]);
    fireEvent.click(review);
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-ow-3"));
    expect(asked()).toEqual([
      ["O-1", "目前在「修復」", "true"],
      ["O-3", "未分組", "false"],
    ]);
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-move"));
    expect(latest).toEqual([g("x", "outsource", "審查", ["ow-1"], ["review"]), g("y", "outsource", "修復", [])]);
    expect(saves).toBe(3);
    expect(document.activeElement).toBe(review);

    fireEvent.click(review);
    fireEvent.click(review);
    expect(asked()).toEqual([["O-3", "未分組", "true"]]);
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-ow-3"));
    expect((screen.getByTestId("chat-groups-bring-in-move") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-none"));
    expect(latest).toEqual([g("x", "outsource", "審查", ["ow-1"], ["review"]), g("y", "outsource", "修復", [])]);

    fireEvent.click(review);
    fireEvent.click(review);
    fireEvent.click(screen.getByTestId("chat-groups-bring-in-move"));
    expect(latest).toEqual([g("x", "outsource", "審查", ["ow-1", "ow-3"], ["review"]), g("y", "outsource", "修復", [])]);
    fireEvent.click(review);
    const before = saves;
    fireEvent.click(review);
    expect(screen.queryByTestId("chat-groups-bring-in")).toBeNull();
    expect(saves).toBe(before + 1);
    expect(latest[0].taskTypeKeys).toEqual(["review"]);
    expect(autoJoinRows()).toEqual([
      ["PR 審查", "true"],
      ["修 bug", "false"],
      ["docs", "false"],
    ]);
  });
});
