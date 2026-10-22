// CT story: the three identity surfaces that carry the
// avatar-corner presence dot and the activity status icon — the left list
// (real MemberCard + real OutsourcePanel rows), the chat header (built from the
// same PresenceAvatar / ActivityStatusIcon ChatArea renders, beside its three
// buttons) and the detail panel's 運作狀況 card (real MemberActivityCard under an
// identity card) — at the widths the spec mounts it at.
import { I18nProvider } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { PresenceAvatar } from "../../src/components/PresenceAvatar";
import { PresenceBadge } from "../../src/components/PresenceBadge";
import { Avatar } from "../../src/components/Avatar";
import { ActivityStatusIcon, MemberActivityCard } from "../../src/components/MemberActivity";
import type { OutsourceWorkerView } from "../../src/api/adapter";
import type { Member, MemberActivity } from "../../src/types";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

const LONG_NAME = "Eva Rhapsody Inbox Supervisor";

function activityOf(state: MemberActivity["state"], now: number): MemberActivity {
  if (state === "idle") return { state, busySince: null, subagents: [], waits: [] };
  if (state === "waiting") {
    return {
      state,
      busySince: null,
      subagents: [],
      waits: [{ kind: "background_command", description: "每 30 秒檢查 CI", nextTs: null, recurring: false }],
    };
  }
  return {
    state,
    busySince: now - 180,
    subagents: [
      { id: "a1", parentId: null, name: "Explore", description: "盤點 ACE-16005 的 Backlog 票，順便把重複的合併", startedTs: now - 120 },
      { id: "a2", parentId: "a1", name: "general-purpose", description: "讀取試用站錯誤紀錄", startedTs: now - 40 },
    ],
    waits: [
      { kind: "background_command", description: "每 30 秒檢查 CI", nextTs: null, recurring: false },
      { kind: "schedule", description: "檢查 PR 審查結果", nextTs: now + 3600, recurring: true },
    ],
  };
}

function mkMember(id: string, name: string, activity: MemberActivity | null, unreadCount: number): Member {
  return {
    id,
    name,
    role: "r-custom",
    roleName: "Platform Reliability Lead",
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
    unreadCount,
    activity,
  };
}

function mkWorker(activity: MemberActivity | null): OutsourceWorkerView {
  return {
    id: "ow-1",
    codename: "O-413",
    model: "opus",
    effort: "high",
    taskId: "t-1",
    taskNo: "T-492",
    taskTypeName: "OffiCraft 開發",
    taskTitle: "成員運作狀況",
    presence: "online",
    unreadCount: 120,
    activity,
  };
}

const noop = () => {};

export function IdentityActivityStory({
  surface,
  state,
  mobile,
}: {
  surface: "list" | "header" | "card";
  state: MemberActivity["state"];
  mobile: boolean;
}) {
  const now = Date.now() / 1000;
  const activity = activityOf(state, now);
  const member = mkMember("m-long", LONG_NAME, activity, 120);
  return (
    <I18nProvider>
      <div className={mobile ? "office office--mobile" : "office"} style={{ height: 900 }}>
        {surface === "list" && (
          <aside className="office__members" data-testid="list">
            <MemberCard member={member} selected={false} onOpenDetail={noop} onChat={noop} />
            <MemberCard member={mkMember("m-short", "Mira", activity, 0)} selected onOpenDetail={noop} onChat={noop} />
            <OutsourcePanel
              workers={[mkWorker(activity)]}
              error={false}
              maxParallel={3}
              selectedId=""
              onOpenChat={noop}
              onOpenDetail={noop}
              onOpenTask={noop}
            />
          </aside>
        )}
        {!mobile && surface !== "list" && <aside className="office__members" />}
        {surface === "header" && (
          <section className="office__chat">
            <div className="chat">
              {[member, mkMember("m-long-bare", LONG_NAME, null, 0)].map((m) => (
                <header key={m.id} className="chat__header" data-testid={`header-${m.activity ? "with" : "without"}`}>
                  <PresenceAvatar presence={m.lifecycle}>
                    <Avatar size={38} kind="staff" src="" />
                  </PresenceAvatar>
                  <div className="chat__header-text">
                    <div className="chat__header-name">
                      <span>{m.name}</span>
                      <ActivityStatusIcon activity={m.activity} testId="chat-header-activity" />
                    </div>
                    <div className="chat__header-sub">
                      <PresenceBadge member={m} dot={false} />
                    </div>
                  </div>
                  <button type="button" className="chat__header-action" aria-label="tasks" />
                  <button type="button" className="chat__header-action" aria-label="role" />
                  <button type="button" className="chat__gallery-toggle" aria-label="gallery" />
                </header>
              ))}
            </div>
          </section>
        )}
        {surface === "card" && (
          <section className="office__chat">
            <div className="mp" data-testid="panel">
              <div className="mp-card mp-identity" />
              <MemberActivityCard activity={activity} testId="mp-activity" />
            </div>
          </section>
        )}
      </div>
    </I18nProvider>
  );
}
