// CT story: the office roster sidebar in its REAL layout context.
//
// The layout contract under test lives on `.office` (a CSS grid whose first
// track is the fixed-width roster rail) and on the roster's own overflow
// handling — neither of which is observable in jsdom. Mounting the full
// OfficePage would drag in useMembers/useMonitoring/useOutsourceWorkers/
// useIsMobile (live fetch + event subscription), so this story reproduces the
// sidebar's real DOM SKELETON (the same class names OfficePage emits:
// .office > aside.office__members > .office__members-list) and fills it with
// REAL <MemberCard> components against the REAL office.css. The geometry the
// guard asserts (rail width holds, cards do not overflow the rail) is a
// property of that CSS + those components, not of the fixture wiring.
//
// One card carries a pathologically long name — the case that would blow the
// 264px rail out or force a horizontal scrollbar if the roster's min-width:0 /
// ellipsis handling regressed.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OfficeSidebarTabs } from "../../src/components/OfficeSidebarTabs";
import { Avatar } from "../../src/components/Avatar";
import { PresenceBadge } from "../../src/components/PresenceBadge";
import { PersonPlusIcon } from "../../src/components/icons";
import type { Member } from "../../src/types";

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online-awake",
    model: "opus",
    effort: "medium",
    kind: "assistant",
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

const roster: Member[] = [
  mkMember({ id: "mira", name: "Mira", status: "online", lifecycle: "online-awake" }),
  mkMember({ id: "beto", name: "Beto", status: "waking", lifecycle: "waking" }),
  mkMember({
    id: "long",
    name: "一個非常非常非常長到會撐爆側欄寬度的成員名字用來壓測 overflow 行為",
    status: "offline",
    lifecycle: "offline",
    unreadCount: 3,
  }),
];

export function OfficeSidebarStory() {
  // The REAL tab switcher + recruit button (T-66a8) in the real rail context,
  // so the guard measures the actual tab layout / underline / badge overflow,
  // not a hand-drawn approximation. The long-name member still stress-tests the
  // rail's min-width:0 / ellipsis handling.
  const [tab, setTab] = useState<"staff" | "outsource">("staff");
  const staffUnread = roster.reduce((s, m) => s + (m.unreadCount || 0), 0);
  return (
    <I18nProvider>
      <div className="office" style={{ height: 480 }}>
        <aside className="office__members">
          <OfficeSidebarTabs
            activeTab={tab}
            onSelect={setTab}
            staffCount={roster.length}
            staffUnread={staffUnread}
            staffReady
            outsourceCount={1}
            outsourceUnread={1}
            outsourceReady
            capText="10"
          />
          <div className="office__members-list">
            {roster.map((m) => (
              <MemberCard
                key={m.id}
                member={m}
                selected={m.id === "mira"}
                onOpenDetail={() => {}}
                onChat={() => {}}
              />
            ))}
          </div>
          <div className="office__recruit-wrap">
            <button type="button" className="office__recruit">
              <PersonPlusIcon size={16} />
              <span>招攬新成員</span>
            </button>
          </div>
        </aside>
        {/* Right column: the REAL chat header skeleton (same classes + child
         * structure ChatArea emits — Avatar size 38 + .chat__header-name /
         * -sub inside .chat__header), so the T-5557 divider-alignment guard can
         * measure the chat header's bottom border against the sidebar tab bar's.
         * Both columns share the grid's top edge, so this reproduces the real
         * vertical relationship (a property of office.css, not of ChatArea's
         * wiring — the same philosophy the roster skeleton above follows). */}
        <section className="office__chat">
          <div className="chat">
            <header className="chat__header">
              <Avatar size={38} />
              <div className="chat__header-text">
                <div className="chat__header-name">
                  <span>{roster[0].name}</span>
                </div>
                <div className="chat__header-sub">
                  <PresenceBadge member={roster[0]} />
                </div>
              </div>
            </header>
          </div>
        </section>
      </div>
    </I18nProvider>
  );
}
