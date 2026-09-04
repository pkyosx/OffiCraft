// Fixture for the phone-width 市政廳 roster overflow guard (T-959d).
//
// It reproduces the REAL ancestor chain the phone roster renders inside —
// `.app > .app__main > .office.office--mobile > aside.office__members > …` —
// because `.app__main`'s side padding is part of the available width. A bare
// mount under-reports (frontend/.claude/rules/css-layout-traps.md).
//
// The strings are REAL data, not invented stress text: `typeName` values are
// task-manual display names that exist in the live studio today, and they are
// what the rail row renders in `.outsource-row__type`. A fixture of short
// labels goes green on a page the owner sees broken every day.
import { I18nProvider } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OfficeSidebarTabs } from "../../src/components/OfficeSidebarTabs";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { PersonPlusIcon } from "../../src/components/icons";
import type { Member } from "../../src/types";
import type { OutsourceWorkerView } from "../../src/api/adapter";

/** Longest task-type display name live in the studio (2026-08-16 roster read). */
export const REAL_LONG_TYPE = "OffiCraft · PR 審查（含外部 PR 接待）";
/** The longest unbreakable latin run a live type name carries. */
export const REAL_LATIN_TYPE = "Long-term Context · Lessons Refine";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    name: "銀月",
    role: "assistant",
    roleName: "助理",
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
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  } as Member;
}

function mkWorker(over: Partial<OutsourceWorkerView> = {}): OutsourceWorkerView {
  return {
    id: "x87",
    codename: "X-87",
    model: "opus",
    effort: "medium",
    taskId: "t-959d5291a011",
    taskNo: "t-959d5291a011",
    taskTypeKey: "",
    taskTypeName: REAL_LONG_TYPE,
    taskTitle: "審查 PR #150（T-e026 交付）：把「同一事實有多份拷貝」的檢查寫進開機說明",
    presence: "online",
    unreadCount: 1,
    createdTs: 0,
    ...over,
  } as OutsourceWorkerView;
}

/** The owner's own 外勤支援 tab: rows carrying live task-type names, and the
 *  unread pill on the rows he circled. */
const workers: OutsourceWorkerView[] = [
  mkWorker({ id: "x92", codename: "X-92", unreadCount: 1 }),
  mkWorker({ id: "x87", codename: "X-87", unreadCount: 1 }),
  mkWorker({
    id: "x85",
    codename: "X-85",
    taskNo: "t-4f845291a012",
    taskTypeName: REAL_LATIN_TYPE,
    taskTitle: "token 四件：到期前 30 分鐘提醒 agent 自己重啟",
    unreadCount: 0,
    presence: "waking",
  }),
  mkWorker({
    id: "o153",
    codename: "O-153",
    taskNo: "t-ee955291a013",
    taskTypeName: "Eva GF VE Tickets",
    taskTitle: "ACE-13048 W/H Receiving",
    unreadCount: 0,
    presence: "offline",
  }),
];

/** 正職 tab. `.member-card__name` declares no wrapping of its own, so a name
 *  carrying one unbreakable latin run is the row's min-content floor.
 *
 *  ⚠️ Honest provenance: unlike the type names above, this one is NOT a value
 *  the live roster carries today (its members are short CJK names, which have a
 *  one-character min-content and never trip this). A member name is free owner
 *  input, so this is the worst case of an editable field, not observed data —
 *  which is exactly why it is the 正職 half's only red-capable sample. */
const roster: Member[] = [
  mkMember({ id: "mira", name: "銀月", unreadCount: 1 }),
  mkMember({
    id: "tester",
    name: "OffiCraft 自動化測試員",
    status: "online",
    lifecycle: "online",
    unreadCount: 12,
  }),
  mkMember({
    id: "inbox",
    name: "EvaRhapsodyInboxForwarderStandbyRelay",
    status: "offline",
    lifecycle: "offline",
    unreadCount: 128,
  }),
];

export function OfficeRosterOverflowStory({
  tab = "outsource",
}: {
  tab?: "staff" | "outsource";
}) {
  return (
    <I18nProvider>
      <div className="app">
        <header className="topbar">
          <div className="topbar__brand">
            <span className="topbar__org">OffiCraft</span>
          </div>
        </header>
        <nav className="nav-tabs">
          <div className="nav-tabs__seg">
            <button type="button" className="nav-tab nav-tab--active">
              <span>市政廳</span>
            </button>
            <button type="button" className="nav-tab">
              <span>任務</span>
            </button>
          </div>
        </nav>
        <main className="app__main">
          <div className="office office--mobile">
            <aside className="office__members">
              <OfficeSidebarTabs
                activeTab={tab}
                onSelect={() => {}}
                staffCount={roster.length}
                staffUnread={141}
                staffReady
                outsourceCount={workers.length}
                outsourceUnread={2}
                outsourceReady
                capText="10"
              />
              {tab === "staff" ? (
                <div className="office__members-list">
                  {roster.map((m) => (
                    <MemberCard
                      key={m.id}
                      member={m}
                      selected={false}
                      onOpenDetail={() => {}}
                      onChat={() => {}}
                    />
                  ))}
                </div>
              ) : (
                <OutsourcePanel
                  workers={workers}
                  error={false}
                  maxParallel={10}
                  selectedId=""
                  onOpenChat={() => {}}
                  onOpenDetail={() => {}}
                  onOpenTask={() => {}}
                />
              )}
              <div className="office__recruit-wrap">
                <button type="button" className="office__recruit">
                  <PersonPlusIcon size={16} />
                  <span>招攬新成員</span>
                </button>
              </div>
            </aside>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}

/** Desktop shape of the SAME page — the two-column grid the `.office` fix must
 *  not disturb. */
export function OfficeRosterDesktopStory({
  tab = "outsource",
}: {
  tab?: "staff" | "outsource";
}) {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div className="office">
            <aside className="office__members">
              <OfficeSidebarTabs
                activeTab={tab}
                onSelect={() => {}}
                staffCount={roster.length}
                staffUnread={141}
                staffReady
                outsourceCount={workers.length}
                outsourceUnread={2}
                outsourceReady
                capText="10"
              />
              {tab === "staff" ? (
                <div className="office__members-list">
                  {roster.map((m) => (
                    <MemberCard
                      key={m.id}
                      member={m}
                      selected={false}
                      onOpenDetail={() => {}}
                      onChat={() => {}}
                    />
                  ))}
                </div>
              ) : (
                <OutsourcePanel
                  workers={workers}
                  error={false}
                  maxParallel={10}
                  selectedId=""
                  onOpenChat={() => {}}
                  onOpenDetail={() => {}}
                  onOpenTask={() => {}}
                />
              )}
            </aside>
            <section className="office__chat" />
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
