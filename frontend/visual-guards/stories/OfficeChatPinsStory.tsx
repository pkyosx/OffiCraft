// The office sidebar's 置頂 drag wiring (useChatPins + orderByPins +
// ChatPinList) over the REAL MemberCard / OutsourcePanel rows, inside the
// real ancestor chain (.app > .app__main > .office).
import { useState } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OfficeSidebarTabs } from "../../src/components/OfficeSidebarTabs";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { ChatPinList } from "../../src/components/ChatPinList";
import type { ChatPinCollapsed } from "../../src/components/ChatPinList";
import { PersonPlusIcon } from "../../src/components/icons";
import { orderByPins, pinAt, togglePin } from "../../src/lib/chatPins";
import type { Member } from "../../src/types";
import type { OutsourceWorkerView } from "../../src/api/adapter";

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

export const workers: OutsourceWorkerView[] = [
  mkWorker({
    id: "ow-a18f",
    codename: "O-161",
    taskId: "t-529a",
    taskNo: "t-529a",
    taskTypeName: "OffiCraft · 前端 UI",
    taskTitle: "聊天清單置頂與排序（UI 原型）",
    unreadCount: 2,
  }),
  mkWorker({
    id: "ow-9c2e",
    codename: "O-158",
    taskId: "t-4f84",
    taskNo: "t-4f84",
    taskTypeName: "OffiCraft · PR 審查（含外部 PR 接待）",
    taskTitle: "審查 PR #212：chat pins migration",
  }),
  mkWorker({
    id: "ow-71d0",
    codename: "O-155",
    taskId: "t-ee95",
    taskNo: "t-ee95",
    taskTypeName: "Eva GF VE Tickets",
    taskTitle: "ACE-13048 W/H Receiving",
    presence: "waking",
  }),
  mkWorker({
    id: "ow-3b6a",
    codename: "O-153",
    taskId: "t-07c1",
    taskNo: "t-07c1",
    taskTypeName: "Long-term Context · Lessons Refine",
    taskTitle: "整理上週的 lessons",
    presence: "offline",
  }),
];

function Sidebar({ mobile }: { mobile: boolean }) {
  const { t } = useI18n();
  const [tab, setTab] = useState<"staff" | "outsource">("staff");
  const [chatId, setChatId] = useState("");
  const [detailId, setDetailId] = useState("");
  const [sections, setSections] = useState<Record<"staff" | "outsource", ChatPinCollapsed>>({
    staff: { pinned: false, rest: false },
    outsource: { pinned: false, rest: false },
  });
  // Local state over the same pure helpers useChatPins writes through: the
  // story's fixture members are not on the mock server's roster.
  const [pins, setPins] = useState<string[]>(["mira"]);
  const toggle = (id: string, kind: readonly string[]) => setPins((p) => togglePin(p, id, kind));
  const reorder = (id: string, index: number, kind: readonly string[]) =>
    setPins((p) => pinAt(p, id, index, kind));
  const staffList = orderByPins(roster, pins, (m) => m.id);
  const outsourceList = orderByPins(workers, pins, (w) => w.id);
  const byId = (id: string) => roster.find((m) => m.id === id)!;
  const selected = mobile ? chatId : chatId || staffList.ordered[0]?.id;
  return (
    <aside className="office__members" data-chat={chatId} data-detail={detailId}>
      <OfficeSidebarTabs
        activeTab={tab}
        onSelect={setTab}
        staffCount={roster.length}
        staffUnread={3}
        staffReady
        outsourceCount={workers.length}
        outsourceUnread={2}
        outsourceReady
        capText="10"
      />
      {tab === "staff" ? (
        <ChatPinList
          className="office__members-list"
          ids={staffList.ordered.map((m) => m.id)}
          pinnedCount={staffList.pinnedIds.length}
          nameOf={(id) => byId(id).name}
          onReorder={(id, index) => reorder(id, index, staffList.pinnedIds)}
          restTitle={t.office.pins.restStaffTitle}
          unreadOf={(id) => byId(id).unreadCount}
          collapsed={sections.staff}
          onCollapsedChange={(next) => setSections((x) => ({ ...x, staff: next }))}
          renderRow={(id, sortable) => (
            <MemberCard
              member={byId(id)}
              selected={id === selected}
              onOpenDetail={() => setDetailId(id)}
              onChat={() => setChatId(id)}
              sortable={sortable}
              pin={{
                pinned: staffList.pinnedIds.includes(id),
                onToggle: () => toggle(id, staffList.pinnedIds),
              }}
            />
          )}
          renderOverlay={(id) => (
            <MemberCard
              member={byId(id)}
              selected={false}
              onOpenDetail={() => {}}
              onChat={() => {}}
              pin={{ pinned: true, onToggle: () => {} }}
            />
          )}
        />
      ) : (
        <OutsourcePanel
          workers={outsourceList.ordered}
          error={false}
          maxParallel={10}
          selectedId={chatId}
          onOpenChat={setChatId}
          onOpenDetail={setDetailId}
          onOpenTask={() => {}}
          pins={{
            pinnedCount: outsourceList.pinnedIds.length,
            onToggle: (id) => toggle(id, outsourceList.pinnedIds),
            onReorder: (id, index) => reorder(id, index, outsourceList.pinnedIds),
            collapsed: sections.outsource,
            onCollapsedChange: (next) => setSections((x) => ({ ...x, outsource: next })),
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

export function OfficeChatPinsStory({ mobile = false }: { mobile?: boolean }) {
  return (
    <I18nProvider>
      <div className="app" style={mobile ? undefined : { height: 860 }}>
        <main className="app__main">
          <div className={`office${mobile ? " office--mobile" : ""}`}>
            <Sidebar mobile={mobile} />
            {!mobile && <section className="office__chat" />}
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
