// CT story: the identity surfaces that carry the avatar-corner presence dot and
// the activity status icon — the left list (real MemberCard + real
// OutsourcePanel rows, each list ending with an online row that has not
// reported and an offline row; `mixed` gives every list one row per icon),
// the chat header and the detail identity card (built from the same
// PresenceAvatar / NameWithActivity ChatArea and the detail panels render;
// each with an icon, an online member without activity and an offline one),
// and the detail panel's 運行狀況 card with its 目前狀態 box (the real
// AgentDetailPanel) — at the widths the spec mounts it at; plus the state glyphs, the unknown mark and the
// moon drawn large. A row opened by a click is recorded on body[data-opened].
import { I18nProvider } from "../../src/i18n";
import { MemberCard } from "../../src/components/MemberCard";
import { OutsourcePanel } from "../../src/components/OutsourcePanel";
import { PresenceAvatar } from "../../src/components/PresenceAvatar";
import { PresenceBadge } from "../../src/components/PresenceBadge";
import { Avatar } from "../../src/components/Avatar";
import { InlineEdit } from "../../src/components/InlineEdit";
import { ActivityStateIcon, NameWithActivity } from "../../src/components/MemberActivity";
import { MemberDetailConvergenceStory } from "./AgentDetailConvergenceStory";
import type { OutsourceWorkerView } from "../../src/api/adapter";
import type { Member, MemberActivity, MemberLifecycle } from "../../src/types";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

const LONG_NAME = "Eva Rhapsody Inbox Supervisor";
const LONG_WAITS: MemberActivity["waits"] = [
  {
    kind: "background_command",
    description:
      "cd /Users/eva/.officraft/agents/ow-691b883dc2da/work/t-492-mobile/frontend && npx vite --port 5391 --strictPort --host 127.0.0.1 > /private/tmp/claude-501/-Users-eva--officraft-agents-ow-691b883dc2da/2c6e103e-787e-4fa5-943e-148a8d6d8008/scratchpad/vite.log 2>&1",
    nextTs: null,
    recurring: false,
  },
  {
    kind: "background_command",
    description: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a089f86d081884c7d659a2feaa0c55ad015",
    nextTs: null,
    recurring: false,
  },
];

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

function mkMember(
  id: string,
  name: string,
  activity: MemberActivity | null,
  unreadCount: number,
  lifecycle: MemberLifecycle = "online",
): Member {
  return {
    id,
    name,
    role: "r-custom",
    roleName: "Platform Reliability Lead",
    status: lifecycle === "online" ? "online" : "offline",
    lifecycle,
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

function mkWorker(
  id: string,
  codename: string,
  activity: MemberActivity | null,
  presence: MemberLifecycle = "online",
): OutsourceWorkerView {
  return {
    id,
    codename,
    model: "opus",
    effort: "high",
    taskId: "t-1",
    taskNo: "T-492",
    taskTypeName: "OffiCraft 開發",
    taskTitle: "成員運行狀況",
    presence,
    unreadCount: 120,
    activity,
  };
}

const noop = () => {};
const opened = (what: string) => () => document.body.setAttribute("data-opened", what);

export function IdentityActivityStory({
  surface,
  state,
  mobile,
  warning = false,
  mixed = false,
  longWaits = false,
}: {
  surface: "list" | "header" | "detail" | "card" | "glyphs";
  state: MemberActivity["state"];
  mobile: boolean;
  warning?: boolean;
  mixed?: boolean;
  longWaits?: boolean;
}) {
  const now = Date.now() / 1000;
  const base = activityOf(state, now);
  const activity = longWaits ? { ...base, waits: [...base.waits, ...LONG_WAITS] } : base;
  const warned = (m: Member): Member =>
    warning
      ? { ...m, runtimeLoginWarnings: [{ machineId: "mc-1", machineName: "eva-mbp", runtime: "claude", pending: false }] }
      : m;
  const member = warned(mkMember("m-long", LONG_NAME, activity, 120));
  const variants = [
    ["with", member],
    ["without", mkMember("m-long-bare", LONG_NAME, null, 0)],
    ["offline", mkMember("m-long-off", LONG_NAME, null, 0, "offline")],
  ] as const;
  return (
    <I18nProvider>
      <div className={mobile ? "office office--mobile" : "office"} style={{ height: 900 }}>
        {surface === "list" && (
          <aside className="office__members" data-testid="list">
            {mixed ? (
              <>
                <MemberCard member={mkMember("m-busy", "Mira", activityOf("busy", now), 0)} selected={false} onOpenDetail={opened("detail")} onChat={opened("chat")} />
                <MemberCard member={mkMember("m-wait", "Ana", activityOf("waiting", now), 0)} selected={false} onOpenDetail={opened("detail")} onChat={opened("chat")} />
                <MemberCard member={mkMember("m-idle", "Leo", activityOf("idle", now), 0)} selected={false} onOpenDetail={opened("detail")} onChat={opened("chat")} />
              </>
            ) : (
              <>
                <MemberCard member={member} selected={false} onOpenDetail={opened("detail")} onChat={opened("chat")} />
                <MemberCard member={warned(mkMember("m-short", "Mira", activity, 0))} selected onOpenDetail={opened("detail")} onChat={opened("chat")} />
              </>
            )}
            <MemberCard member={mkMember("m-none", "Jun", null, 0)} selected={false} onOpenDetail={opened("detail")} onChat={opened("chat")} />
            <MemberCard
              member={mkMember("m-off", "Kai", null, 0, "stopped")}
              selected={false}
              onOpenDetail={opened("detail")}
              onChat={opened("chat")}
            />
            <OutsourcePanel
              workers={[
                ...(mixed
                  ? [
                      mkWorker("ow-1", "O-413", activityOf("busy", now)),
                      mkWorker("ow-4", "O-52", activityOf("waiting", now)),
                      mkWorker("ow-5", "O-61", activityOf("idle", now)),
                    ]
                  : [mkWorker("ow-1", "O-413", activity)]),
                mkWorker("ow-2", "O-7", null),
                mkWorker("ow-3", "O-88", null, "offline"),
              ]}
              error={false}
              maxParallel={3}
              selectedId=""
              onOpenChat={opened("chat")}
              onOpenDetail={opened("detail")}
              onOpenTask={noop}
            />
          </aside>
        )}
        {!mobile && surface !== "list" && <aside className="office__members" />}
        {surface === "header" && (
          <section className="office__chat">
            <div className="chat">
              {variants.map(([key, m]) => (
                <header key={key} className="chat__header" data-testid={`header-${key}`}>
                  <PresenceAvatar presence={m.lifecycle}>
                    <Avatar size={38} kind="staff" src="" />
                  </PresenceAvatar>
                  <div className="chat__header-text">
                    <div className="chat__header-name">
                      <NameWithActivity activity={m.activity} presence={m.lifecycle} memberName={m.name} testId="chat-header-activity">
                        <span>{m.name}</span>
                      </NameWithActivity>
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
        {surface === "detail" && (
          <section className="office__chat">
            <div className="mp" data-testid="panel">
              {variants.map(([key, m]) => (
                <div key={key} className="mp-card mp-identity" data-testid={`identity-${key}`}>
                  <PresenceAvatar presence={m.lifecycle}>
                    <Avatar size={52} kind="staff" src="" />
                  </PresenceAvatar>
                  <div className="mp-identity__body">
                    <div className="mp-identity__line">
                      <NameWithActivity activity={m.activity} presence={m.lifecycle} testId="mp-identity-activity" layout="group">
                        <InlineEdit value={m.name} onCommit={noop} ariaLabel="rename" displayClassName="mp-identity__name" />
                      </NameWithActivity>
                      <span className="badge mp-identity__id">{m.id}</span>
                    </div>
                    <div className="mp-identity__status">
                      <PresenceBadge member={m} dot={false} />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}
        {surface === "glyphs" && (
          <div style={{ display: "flex", gap: 24, padding: 24 }}>
            {(["busy", "waiting", "idle"] as const).map((s) => (
              <span key={s} data-testid={`glyph-${s}`} style={{ display: "inline-flex", padding: 8 }}>
                <ActivityStateIcon state={s} size={48} />
              </span>
            ))}
            <span data-testid="glyph-unknown" style={{ display: "inline-flex", padding: 8 }}>
              <NameWithActivity activity={null} presence="online">
                <span>Jun</span>
              </NameWithActivity>
            </span>
            <span data-testid="glyph-offline" style={{ display: "inline-flex", padding: 8 }}>
              <NameWithActivity activity={null} presence="offline">
                <span>Kai</span>
              </NameWithActivity>
            </span>
          </div>
        )}
        {surface === "card" && (
          <section className="office__chat">
            <MemberDetailConvergenceStory activity={activity} />
          </section>
        )}
      </div>
    </I18nProvider>
  );
}
