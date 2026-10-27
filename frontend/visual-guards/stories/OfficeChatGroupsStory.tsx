// The office sidebar's chat groups (ChatGroupPanel) over the REAL MemberCard /
// OutsourcePanel rows, inside the real ancestor chain
// (.app > .app__main > .office). Group state is local to the story, the same
// complete-state updates the panel hands useChatGroups.
import { useEffect, useRef, useState } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import type { ThemeBundle } from "../../src/lib/themeBundle";
import halloweenTheme from "../../../themes/halloween-2026.theme.json";
import { MemberCard } from "../../src/components/MemberCard";
import { OfficeSidebarTabs } from "../../src/components/OfficeSidebarTabs";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { ChatGroupPanel } from "../../src/components/ChatGroupPanel";
import { PersonPlusIcon } from "../../src/components/icons";
import { tabGroups } from "../../src/lib/chatGroups";
import type { ChatGroup } from "../../src/lib/chatGroups";
import type { Member } from "../../src/types";
import type { OutsourceWorkerView, TaskTypeView } from "../../src/api/adapter";

function mkMember(over: Partial<Member>): Member {
  return {
    id: "mira",
    name: "Mira",
    role: "",
    roleName: "",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "staff",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    terminalAttachCommand: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  } as Member;
}

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  return {
    id: "ow-1",
    codename: "O-1",
    model: "opus",
    effort: "medium",
    taskId: "t-0000",
    taskNo: "t-0000",
    taskTypeKey: "",
    taskTypeName: "",
    taskTitle: "",
    presence: "online",
    unreadCount: 0,
    createdTs: 0,
    ...over,
  } as OutsourceWorkerView;
}

export const roster: Member[] = [
  mkMember({ id: "mira", name: "Mira", role: "assistant", roleName: "特助" }),
  mkMember({ id: "beto", name: "Beto", role: "backend", roleName: "後端工程師" }),
  mkMember({ id: "clara", name: "Clara", role: "frontend", roleName: "前端工程師", unreadCount: 3 }),
  mkMember({ id: "dora", name: "Dora", role: "qa", roleName: "QA 測試", lifecycle: "offline", status: "offline" }),
  mkMember({ id: "evan", name: "Evan", role: "pm", roleName: "產品經理", lifecycle: "waking", status: "waking" }),
  mkMember({
    id: "relay",
    name: "EvaRhapsodyInboxForwarderStandbyRelay",
    role: "ops",
    roleName: "信件轉送",
    lifecycle: "offline",
    status: "offline",
    unreadCount: 128,
  }),
];

export const taskTypes: TaskTypeView[] = [
  { typeKey: "tm-fe", displayName: "OffiCraft · 前端 UI", purpose: "" },
  { typeKey: "tm-review", displayName: "OffiCraft · PR 審查（含外部 PR 接待）", purpose: "" },
  { typeKey: "tm-eva", displayName: "Eva GF VE Tickets", purpose: "" },
  { typeKey: "tm-lessons", displayName: "Long-term Context · Lessons Refine", purpose: "" },
];

export const workers: OutsourceWorkerView[] = [
  mkWorker({
    id: "ow-a18f",
    codename: "O-161",
    taskId: "t-529a",
    taskNo: "t-529a",
    taskTypeKey: "tm-fe",
    taskTypeName: "OffiCraft · 前端 UI",
    taskTitle: "聊天清單群組（UI 原型）",
    unreadCount: 2,
  }),
  mkWorker({
    id: "ow-9c2e",
    codename: "O-158",
    taskId: "t-4f84",
    taskNo: "t-4f84",
    taskTypeKey: "tm-review",
    taskTypeName: "OffiCraft · PR 審查（含外部 PR 接待）",
    taskTitle: "審查 PR #212：chat groups migration",
  }),
  mkWorker({
    id: "ow-71d0",
    codename: "O-155",
    taskId: "t-ee95",
    taskNo: "t-ee95",
    taskTypeKey: "tm-eva",
    taskTypeName: "Eva GF VE Tickets",
    taskTitle: "ACE-13048 W/H Receiving",
    presence: "waking",
  }),
  mkWorker({
    id: "ow-5e21",
    codename: "O-154",
    taskId: "t-5e21",
    taskNo: "t-5e21",
    taskTypeKey: "tm-review",
    taskTypeName: "OffiCraft · PR 審查（含外部 PR 接待）",
    taskTitle: "審查 PR #209：任務到期日",
  }),
  mkWorker({
    id: "ow-3b6a",
    codename: "O-153",
    taskId: "t-07c1",
    taskNo: "t-07c1",
    taskTypeKey: "tm-lessons",
    taskTypeName: "Long-term Context · Lessons Refine",
    taskTitle: "整理上週的 lessons",
    presence: "offline",
  }),
];

function Sidebar({
  mobile,
  initialGroups,
  initialTab,
  initialCollapsed,
}: {
  mobile: boolean;
  initialGroups: ChatGroup[];
  initialTab: "staff" | "outsource";
  initialCollapsed: Record<string, boolean>;
}) {
  const [tab, setTab] = useState(initialTab);
  const [chatId, setChatId] = useState("");
  const [detailId, setDetailId] = useState("");
  const [groups, setGroups] = useState<readonly ChatGroup[]>(initialGroups);
  const seq = useRef(0);
  // Each update stands for one save (useChatGroups sends one PUT per update).
  const [writes, setWrites] = useState(0);
  const [collapsed, setCollapsed] = useState<Record<"staff" | "outsource", Record<string, boolean>>>({
    staff: initialCollapsed,
    outsource: {},
  });
  const byId = (id: string) => roster.find((m) => m.id === id)!;
  const grouping = {
    groups,
    update: (change: (g: readonly ChatGroup[]) => ChatGroup[]) => {
      setWrites((n) => n + 1);
      setGroups((g) => change(g));
    },
    newGroupId: () => `cg-story-${++seq.current}`,
    mobile,
  };
  return (
    <aside className="office__members" data-chat={chatId} data-detail={detailId} data-writes={writes}>
      <OfficeSidebarTabs
        activeTab={tab}
        onSelect={setTab}
        staffCount={roster.length}
        staffGroupCount={tabGroups(groups, "staff").length}
        staffUnread={131}
        staffReady
        outsourceCount={workers.length}
        outsourceUnread={2}
        outsourceReady
        capText="10"
      />
      {tab === "staff" ? (
        <ChatGroupPanel
          {...grouping}
          tab="staff"
          className="office__members-list"
          ids={roster.map((m) => m.id)}
          nameOf={(id) => byId(id).name}
          unreadOf={(id) => byId(id).unreadCount}
          collapsed={collapsed.staff}
          onCollapsedChange={(next) => setCollapsed((x) => ({ ...x, staff: next }))}
          renderRow={(id, sortable) => (
            <MemberCard
              member={byId(id)}
              selected={!mobile && id === chatId}
              onOpenDetail={() => setDetailId(id)}
              onChat={() => setChatId(id)}
              sortable={sortable}
            />
          )}
          renderOverlay={(id) => (
            <MemberCard member={byId(id)} selected={false} onOpenDetail={() => {}} onChat={() => {}} />
          )}
        />
      ) : (
        <OutsourcePanel
          workers={workers}
          error={false}
          maxParallel={10}
          selectedId={chatId}
          onOpenChat={setChatId}
          onOpenDetail={setDetailId}
          onOpenTask={() => {}}
          grouping={{
            ...grouping,
            taskTypes,
            collapsed: collapsed.outsource,
            onCollapsedChange: (next) => setCollapsed((x) => ({ ...x, outsource: next })),
          }}
        />
      )}
      <div className="office__recruit-wrap">
        <button type="button" className="office__recruit">
          <PersonPlusIcon size={16} />
          <span>招攬新成員</span>
        </button>
      </div>
    </aside>
  );
}

// The shipped Halloween bundle under a custom id (the seasonal id is reserved
// and CT runs with no seasonal schedule), worn through the provider like any
// saved theme.
const HALLOWEEN: ThemeBundle = { ...(halloweenTheme as unknown as ThemeBundle), id: "halloween-preview" };

/** Select the theme, then save its bundle once it is the one being worn (the
 * provider adopts a saved bundle only for the active theme; see AvatarKindStory). */
function ThemeDriver({ theme: want }: { theme: "dark" | "light" | "halloween" }) {
  const { theme, setTheme, saveTheme } = useI18n();
  const id = want === "dark" ? "office" : want === "light" ? "office-light" : HALLOWEEN.id;
  useEffect(() => {
    setTheme(id);
  }, [setTheme, id]);
  useEffect(() => {
    if (want === "halloween" && theme === HALLOWEEN.id) void saveTheme(HALLOWEEN);
  }, [theme, saveTheme, want]);
  return null;
}

export function OfficeChatGroupsStory({
  mobile = false,
  initialGroups = [],
  initialTab = "staff",
  initialCollapsed = {},
  theme,
}: {
  /** Omitted: the default (dark) theme, without touching the provider. */
  theme?: "dark" | "light" | "halloween";
  mobile?: boolean;
  initialGroups?: ChatGroup[];
  initialTab?: "staff" | "outsource";
  initialCollapsed?: Record<string, boolean>;
}) {
  return (
    <I18nProvider>
      {theme && <ThemeDriver theme={theme} />}
      <div className="app" style={mobile ? { height: 900 } : { height: 860 }}>
        <main className="app__main">
          <div className={`office${mobile ? " office--mobile" : ""}`}>
            <Sidebar
              mobile={mobile}
              initialGroups={initialGroups}
              initialTab={initialTab}
              initialCollapsed={initialCollapsed}
            />
            {!mobile && <section className="office__chat" />}
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
