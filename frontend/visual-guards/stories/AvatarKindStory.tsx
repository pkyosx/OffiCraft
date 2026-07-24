// CT stories (T-3738): the avatar-KIND mapping at each render site under a
// custom theme that carries BOTH per-member-type images (member=正職,
// outsource=外包). jsdom sees the Avatar pick a `kind`, but only a real browser
// paints the resulting <img src>; these stories mount the REAL components
// (MemberCard / OutsourcePanel / ChatArea) so a mutant that hands the wrong
// `kind` at a site (member for an outsource subject, or a hard-coded glyph that
// bypasses Avatar) shows the WRONG image src and reddens the guard.
//
// The theme is driven straight through the REAL i18n context (commitCustomThemes
// — the same setter ProfileDropdown/ThemeSettings use), so activeAvatars
// resolves exactly as in production; no test-only backdoor into Avatar.
import { useEffect } from "react";
import { I18nProvider, useI18n } from "../../src/i18n";
import type { ThemeBundle } from "../../src/lib/themeBundle";
import type { Member } from "../../src/types";
import { MemberCard } from "../../src/components/MemberCard";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { WorkerDetailPanel } from "../../src/components/WorkerDetailPanel";
import { ChatArea } from "../../src/components/ChatArea";
import type { OutsourceWorkerView } from "../../src/api/adapter";
import { MEMBER_IMG, OUTSOURCE_IMG } from "./avatarKindImages";
import "../../src/components/office.css";

const THEME: ThemeBundle = {
  id: "xian",
  name: "修仙",
  colors: { "--color-accent": "#7a5cff" },
  avatars: { member: MEMBER_IMG, outsource: OUTSOURCE_IMG },
};

// Apply the avatars theme through the real context, synchronously on mount.
function ThemeSeeder({ children }: { children: React.ReactNode }) {
  const { commitCustomThemes } = useI18n();
  useEffect(() => {
    commitCustomThemes([THEME], THEME.id);
  }, [commitCustomThemes]);
  return <>{children}</>;
}

function mkMember(over: Partial<Member>): Member {
  return {
    id: "m1",
    memberId: "m1",
    name: "Mira",
    role: "assistant",
    roleName: "",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

const worker: OutsourceWorkerView = {
  id: "ow-1",
  codename: "O-67",
  model: "Opus 4.6",
  effort: "high",
  status: "active",
  taskId: "t-1",
  taskTitle: "外包任務",
  taskStatus: "in_progress",
  taskNo: "T-3738",
  taskTypeName: "OC 開發",
  presence: "online",
};

// Rail: a 正職 MemberCard and an 外包 OutsourcePanel row side by side, so the
// guard proves each site paints ITS OWN kind — the member card the member
// image, the outsource row the outsource image — and they never cross.
export function AvatarRailStory() {
  return (
    <I18nProvider>
      <ThemeSeeder>
        <div className="office" style={{ height: 480 }}>
          <aside className="office__members">
            <div className="office__members-list">
              <MemberCard
                member={mkMember({ id: "m1", name: "Mira" })}
                selected={false}
                onOpenDetail={() => {}}
                onChat={() => {}}
              />
            </div>
            <OutsourcePanel
              workers={[worker]}
              error={false}
              maxParallel={10}
              selectedId=""
              onOpenChat={() => {}}
              onOpenDetail={() => {}}
              onOpenTask={() => {}}
            />
          </aside>
        </div>
      </ThemeSeeder>
    </I18nProvider>
  );
}

// Worker DETAIL panel (T-3738) — its identity card must paint the role-level
// outsource image, the same kind the rail row does (no per-worker avatar, but
// the theme's 外包 image; a theme without one falls back to the built-in glyph).
export function WorkerDetailStory() {
  return (
    <I18nProvider>
      <ThemeSeeder>
        <div className="office" style={{ height: 640 }}>
          <section className="office__chat">
            <WorkerDetailPanel worker={worker} onBack={() => {}} />
          </section>
        </div>
      </ThemeSeeder>
    </I18nProvider>
  );
}

// Chat header for an OUTSOURCE peer (ow- id) — must paint the outsource image.
export function ChatHeaderOutsourceStory() {
  return (
    <I18nProvider>
      <ThemeSeeder>
        <div className="office">
          <section className="office__chat">
            <ChatArea
              member={mkMember({ id: "ow-1", name: "外包 · O-67", kind: "outsource" })}
              workers={[worker]}
            />
          </section>
        </div>
      </ThemeSeeder>
    </I18nProvider>
  );
}

// Chat header for a 正職 peer — must paint the member image.
export function ChatHeaderMemberStory() {
  return (
    <I18nProvider>
      <ThemeSeeder>
        <div className="office">
          <section className="office__chat">
            <ChatArea member={mkMember({ id: "m1", name: "Mira" })} />
          </section>
        </div>
      </ThemeSeeder>
    </I18nProvider>
  );
}
