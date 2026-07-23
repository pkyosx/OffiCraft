import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { Member } from "../types";
import { api } from "../api";
import { useMembers } from "../hooks/useMembers";
import { useMonitoring } from "../hooks/useMonitoring";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { useIsMobile } from "../hooks/useIsMobile";
import { joinSessionRuntime } from "../lib/runtime";
import { useHashRoute } from "../lib/hashRoute";
import { MemberCard } from "./MemberCard";
import { ChatArea } from "./ChatArea";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { WorkerDetailPanel } from "./WorkerDetailPanel";
import { OutsourcePanel, OutsourceTaskLine } from "./OutsourcePanel";
import { OfficeSidebarTabs } from "./OfficeSidebarTabs";
import { OutsourceCapPopover } from "./OutsourceCapPopover";
import { ChevronLeftIcon, PersonPlusIcon } from "./icons";
import "./office.css";

// A fully-shaped Member with honest-empty telemetry, for the SYNTHESIZED chat
// peers the office projects onto ChatArea's Member contract: a LIVE outsource
// worker, or a released/removed peer whose read-only history we still render.
// Defaults are OFFLINE/stopped so the composer LOCKS (read-only) and no
// presence is fabricated; callers override (e.g. a live worker → online).
function blankChatPeer(id: string, name: string, kind: Member["kind"]): Member {
  return {
    id,
    memberId: "",
    name,
    role: "",
    roleName: "",
    status: "offline",
    lifecycle: "stopped",
    model: "",
    effort: "medium",
    kind,
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

export function OfficePage() {
  const { t } = useI18n();
  // T-66a8: the sidebar switches 正職/外包 by a top text tab (owner mockup
  // 2026-07-18), replacing the old two-stacked-groups rail. Plain component
  // state (not persisted) — the tab is a view toggle, not a route.
  const [activeTab, setActiveTab] = useState<"staff" | "outsource">("staff");
  // The 外包上限設定 popover, opened by the 招攬新成員 button at the sidebar
  // bottom while the 外包 tab is active. OfficePage owns the open state + the
  // outside-click dismissal (the button and popover share an anchor wrapper).
  const [capOpen, setCapOpen] = useState(false);
  const recruitRef = useRef<HTMLDivElement>(null);
  // Roster now comes through the typed api client (mock adapter in M1), not a
  // static import. subscribeEvents inside the hook reconciles by refetch.
  const { members, loading, error, refetch } = useMembers();
  // Narrow viewport → single-page navigation (roster XOR chat). The desktop
  // master-detail two-column grid is unchanged; only the phone path pivots.
  const isMobile = useIsMobile();
  // Selections ride on the URL hash (#office/chat/<id>[/member/<id>]) so a
  // refresh restores the open chat / detail panel. A stale chatId no longer
  // silently self-heals to roster[0] (that dropped a 跳到原訊息 onto Mira,
  // T-661b): an unresolvable EXPLICIT chatId renders its own read-only history
  // (releasedPeer) instead; a stale detailId/workerId still self-heals to the
  // roster below (there is no conversation to preserve there).
  const [route, setRoute] = useHashRoute();
  const selectedId = route.chatId ?? "";
  const detailId = route.detailId ?? null;
  const workerDetailId = route.workerId ?? null;
  // Live session telemetry — the SAME source the Monitor page reads — so the
  // member-detail panel's context/cost match the monitor row (never divergent).
  // GATED (T-ec2c): only fetched/subscribed while a MEMBER detail panel is open
  // (the only consumer, joinSessionRuntime below). With no panel open, merely
  // being on the office page makes zero monitoring requests — no per-heartbeat
  // refetch of the large fold.
  const { monitoring } = useMonitoring({ enabled: detailId !== null });
  const setSelectedId = (id: string) =>
    setRoute({ page: "office", chatId: id || undefined });
  // Opening/closing the detail keeps the chat selection (both live in the hash).
  const setDetailId = (id: string | null) =>
    setRoute({
      page: "office",
      chatId: route.chatId,
      detailId: id ?? undefined,
    });
  // Outsource-worker detail rides the same office hash (worker/<id>), mutually
  // exclusive with the member detail.
  const setWorkerDetailId = (id: string | null) =>
    setRoute({
      page: "office",
      chatId: route.chatId,
      workerId: id ?? undefined,
    });
  // 返回 from a detail/worker panel (T-a706): a panel deep-linked in from
  // elsewhere (e.g. RepliesPage's card avatar) carries route.backTo — land
  // there instead of falling through to this page's own chat-view reset,
  // which would otherwise silently drop the owner into an unrelated chat
  // room (there was never a chat selected on the way in).
  const backFromDetail = () =>
    route.backTo === "replies"
      ? setRoute({ page: "replies" })
      : setDetailId(null);
  const backFromWorkerDetail = () =>
    route.backTo === "replies"
      ? setRoute({ page: "replies" })
      : setWorkerDetailId(null);
  // T-e987 compose seed: a #office/chat/<id>/compose/<taskNo> deep-link (the
  // 任務卡 負責人/建立者 label) seeds that chat's composer with "[<taskNo>] ".
  // Only the EXPLICITLY routed peer gets it (never the roster[0] fallback) —
  // gated per ChatArea below on peerId === route.chatId, same rule as msgId.
  const composeSeed = route.composeTaskNo
    ? `[${route.composeTaskNo}] `
    : undefined;
  const seedFor = (peerId: string) =>
    peerId === route.chatId ? composeSeed : undefined;

  // The office lists ONLY real AI assistants — machine-layer members (kind
  // "warden", the telemetry collector) belong to the monitoring/machine view,
  // never the office roster (Seth once mistook a warden row for an intruder).
  const roster = members
    .filter((m) => m.kind === "assistant")
    // 助理(seed assistant 角色)置頂;其餘接在後面。sort 穩定 → 各組內維持
    // ListMembers 已排好的字母序(不必再排一次名字)。
    .sort(
      (a, b) =>
        (a.role === "assistant" ? 0 : 1) - (b.role === "assistant" ? 0 : 1),
    );

  // M3 §4: the LIVE outsource workers behind the left rail's 外包 panel (and
  // the outsource chat peers — a worker id rides the SAME chatId hash slot).
  const outsource = useOutsourceWorkers();

  // T-66a8 tab badges — each tab's area unread TOTAL (the red count pill; 0 →
  // no badge). A member/worker whose chat is open + watched already reads to
  // 0 server-side, so it naturally drops out of the sum — no per-row
  // suppression needed here. Only summed once the fetch settled (honest:
  // a rejected roster never fabricates a count).
  const staffUnread =
    !error && !loading
      ? roster.reduce((sum, m) => sum + (m.unreadCount || 0), 0)
      : 0;
  const outsourceUnread = !outsource.error
    ? outsource.workers.reduce((sum, w) => sum + (w.unreadCount ?? 0), 0)
    : 0;
  // The cap display for the 外包 tab sub-line: "∞" for 無限, "N" finite; null
  // when settings are not loaded → the 「· 上限 M」 suffix is omitted.
  const capText =
    outsource.maxParallel === null
      ? null
      : outsource.maxParallel === -1
        ? "∞"
        : `${outsource.maxParallel}`;

  // Switching tabs closes an open cap popover (it belongs to the 外包 tab).
  const selectTab = (tab: "staff" | "outsource") => {
    setActiveTab(tab);
    if (tab !== "outsource") setCapOpen(false);
  };

  // The 招攬新成員 button routes by the active tab (owner mockup 2026-07-18):
  // 正職 → 角色誌 CREATE mode (#settings/roles/new, T-25b7) through the hash
  // seam; 外包 → the 外包上限設定 popover.
  const onRecruit = () => {
    if (activeTab === "outsource") {
      setCapOpen((v) => !v);
      return;
    }
    setRoute({ page: "settings", settingsRoles: true, settingsRolesNew: true });
  };

  // Outside-click dismissal for the cap popover (mirrors the old panel gear's
  // handler): the anchor wrapper holds BOTH the button and the popover, so a
  // click on the button never counts as "outside" — its own onClick toggles.
  useEffect(() => {
    if (!capOpen) return;
    function onDown(e: MouseEvent) {
      if (!recruitRef.current?.contains(e.target as Node)) setCapOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [capOpen]);

  // An outsource chat peer? The chatId hash slot carries EITHER a member id or
  // a live worker id — resolve the worker FIRST so a worker id never falls back
  // to the roster[0] member chat. A released worker (task closed) drops off the
  // live list; it is NOT self-healed to the default member chat but rendered as
  // a read-only released peer below (releasedPeer) so 跳到原訊息 still lands on
  // the origin conversation, not Mira (T-661b).
  const workerPeer = selectedId
    ? outsource.workers.find((w) => w.id === selectedId)
    : undefined;
  // The synthetic chat identity for a LIVE worker: ChatArea renders name /
  // composer-lock / unread anchors off a Member shape, so project the worker
  // onto one. lifecycle "online" is by construction — a LIVE worker is
  // chattable; presence display is REPLACED by headerSub below, so no member
  // presence is ever fabricated on screen.
  const workerMember: Member | undefined = workerPeer
    ? {
        ...blankChatPeer(
          workerPeer.id,
          t.office.outsource.label(workerPeer.codename),
          "outsource",
        ),
        status: "online",
        lifecycle: "online",
        model: workerPeer.model,
      }
    : undefined;

  // Fall back to the first member so the desktop chat pane has a selection when
  // NOTHING is explicitly picked. But an EXPLICIT chatId that resolves to no
  // roster member must NOT silently substitute roster[0]: an outsource worker
  // rides this same chatId slot and is never in the 正職 roster, so once it also
  // drops off the LIVE worker list (task closed) both lookups miss — and a
  // 跳到原訊息 on that sender's card used to land on Mira (roster[0]) instead of
  // the origin conversation (T-661b). Only the empty-selection default falls back.
  const selected = selectedId
    ? roster.find((m) => m.id === selectedId)
    : roster[0];

  // The chat history is keyed by PEER ID (listChat/adapter), independent of the
  // peer still being live — so an EXPLICIT chatId that resolves to neither a
  // roster member NOR a live worker still has a reachable conversation. Rather
  // than a dead end (blank pane, no back button on mobile) or the old
  // roster[0]=Mira wrong room, project a READ-ONLY synthetic peer and render
  // the ORIGINAL conversation under an identity that is HONEST about the peer
  // being gone. `ow-`-prefixed ids are released outsource workers (server mints
  // `ow-`+hex, outsource_sched.go); anything else is a removed member. Gated on
  // BOTH lists having settled so a not-yet-loaded live worker is never
  // mislabeled "released" (a transient flash). (T-661b review finding #1/#2.)
  const isReleasedWorkerId = selectedId.startsWith("ow-");
  const releasedPeer: Member | undefined =
    selectedId !== "" &&
    !workerPeer &&
    !selected &&
    !loading &&
    !outsource.loading
      ? blankChatPeer(
          selectedId,
          isReleasedWorkerId
            ? t.office.outsource.releasedChatTitle
            : t.office.chatUnavailableTitle,
          isReleasedWorkerId ? "outsource" : "assistant",
        )
      : undefined;
  const rosterDetail = detailId
    ? roster.find((m) => m.id === detailId)
    : undefined;
  // Join the live session's context/cost so the detail panel shows the SAME
  // value as the Monitor page (same source), not the member DTO's honest-null.
  const detail = rosterDetail
    ? joinSessionRuntime(rosterDetail, monitoring?.sessions ?? [])
    : undefined;

  // Outsource-worker detail: resolve the live worker behind #office/worker/<id>.
  // A released worker (task closed) drops off the live list → the lookup misses
  // and the page self-heals to the roster (same as a stale detailId).
  const workerDetail = workerDetailId
    ? outsource.workers.find((w) => w.id === workerDetailId)
    : undefined;

  if (workerDetail) {
    return (
      <WorkerDetailPanel
        worker={workerDetail}
        onBack={backFromWorkerDetail}
        onOpenTask={
          workerDetail.taskId
            ? () => setRoute({ page: "tasks", taskId: workerDetail.taskId })
            : undefined
        }
        // 改機器 (T-f190; admin-gated since P7c): fire the relocate; the outsource_worker SSE
        // delta refetches the worker list so the panel adopts the new placement.
        onRelocate={async (machineId) => {
          await api.relocateWorker(workerDetail.id, machineId);
        }}
        // T-32e1/T-f190 lifecycle ops (owner-only). Each fires the mutation; the
        // outsource_worker SSE delta refetches so the panel adopts the new state.
        onRefocus={async () => {
          await api.refocusWorker(workerDetail.id);
        }}
        onStop={async () => {
          await api.stopWorker(workerDetail.id);
        }}
        onRestart={async () => {
          await api.restartWorker(workerDetail.id);
        }}
        onSetModel={async (model, effort) => {
          await api.setWorkerModel(workerDetail.id, { model, effort });
        }}
        // Initial-prompt PREVIEW (T-ba6b): the server re-runs the spawn fold
        // over the CURRENT task/manual rows (no token minted) — the worker twin
        // of the member panel's /api/bootstrap role preview.
        onFetchBootContext={async () =>
          api.getWorkerBootContext(workerDetail.id)
        }
      />
    );
  }

  if (detail) {
    return (
      <MemberDetailPanel
        member={detail}
        onBack={backFromDetail}
        // Presence contract: activate writes desired_state=online INTENT only; we
        // refetch and let server-driven presence surface waking → online. No
        // optimistic green here.
        onActivate={async (machineId) => {
          // 🔴 RETURN the result (T-7fa1). `onActivate` accepts a void-returning
          // handler, so dropping it here compiles fine and silently restores the
          // original bug — the panel would never learn that nothing was
          // dispatched. Refetch first so the panel's own lifecycle read is
          // current before it acts on the verdict.
          const result = await api.activateMember(detail.id, machineId);
          // NIT-4 (review r1): a throwing refetch must not swallow the verdict —
          // it would drop the panel into the catch branch, which only rolls back
          // pending and shows nothing.
          try {
            await refetch();
          } catch {
            /* the verdict outlives a failed refresh */
          }
          return result;
        }}
        // 改機器 (placement only): re-pin the member's machine and let the server
        // reconcile a live one onto it. Unlike activate this NEVER wakes the
        // member (never touches desired_state). Refetch to surface the new pin.
        onRelocate={async (machineId) => {
          // Return the result for the same reason onActivate does (T-7fa1).
          const result = await api.relocateMember(detail.id, machineId);
          try {
            await refetch();
          } catch {
            /* the verdict outlives a failed refresh (NIT-4) */
          }
          return result;
        }}
        // Graceful stop / cancel-wake (retains the row). Refetch and let
        // server-driven presence surface stopping → stopped.
        onDeactivate={async () => {
          await api.deactivateMember(detail.id);
          await refetch();
        }}
        // Force-stop (immediate kill): escalate a *stopping* member past the 120s
        // grace — the server dispatches the robust STOP to the warden now. Refetch
        // and let server-driven presence surface stopped.
        onForceStop={async () => {
          await api.forceStopMember(detail.id);
          await refetch();
        }}
        onRefocus={async () => {
          await api.refocusMember(detail.id);
          await refetch();
        }}
        onRename={async (name) => {
          await api.patchMember(detail.id, { name });
          await refetch();
        }}
      />
    );
  }

  // On a phone the roster and chat are MUTUALLY EXCLUSIVE (single-page nav): an
  // explicit member-row tap sets selectedId → the chat opens with a "back to members"
  // button; with no explicit pick the roster shows alone. This kills the mobile
  // duplicate — desktop rendered BOTH the roster card AND the chat header for the
  // roster[0] fallback, so the same member (Mira) appeared twice once the grid
  // stacked. On desktop both panes always render (chat keeps the roster[0]
  // fallback), so the two-column layout is unchanged.
  const chatOpen = selectedId !== "";
  const showRoster = !isMobile || !chatOpen;
  const showChat = !isMobile || chatOpen;

  return (
    <div className={`office${isMobile ? " office--mobile" : ""}`}>
      {showRoster && (
        <aside className="office__members">
          {/* T-66a8 (owner mockup 2026-07-18): top 正職/外包 text-tab switcher
           * — the selected tab carries a blue underline, a red unread-count
           * badge sits beside each label, and a 「N 人」 / 「N 人 · 上限 M」
           * count sub-line sits under it. Replaces the old two-stacked-groups
           * rail (正職 collapse header + 外包 panel head with their own
           * counts). */}
          <OfficeSidebarTabs
            activeTab={activeTab}
            onSelect={selectTab}
            staffCount={roster.length}
            staffUnread={staffUnread}
            staffReady={!loading && !error}
            outsourceCount={outsource.workers.length}
            outsourceUnread={outsourceUnread}
            outsourceReady={!outsource.error}
            capText={capText}
          />
          {/* Honest load-failure notice: distinguishes a fetch reject (500/network;
           * 401 already bounced to login) from a genuinely empty office. Shown
           * on the 正職 tab (the roster's own error). */}
          {activeTab === "staff" && error && (
            <div className="office__error">{t.office.loadError}</div>
          )}
          {activeTab === "staff" ? (
            <div className="office__members-list">
              {roster.map((member) => (
                <MemberCard
                  key={member.id}
                  member={member}
                  // On mobile the roster stands alone (no persistent chat), so no
                  // fallback highlight; on desktop the roster[0] fallback stays lit
                  // next to its open chat — unless an OUTSOURCE chat is open (the
                  // highlight then belongs to the worker row below, never both).
                  selected={
                    !workerPeer &&
                    member.id === (isMobile ? selectedId : (selected?.id ?? ""))
                  }
                  onOpenDetail={() => setDetailId(member.id)}
                  onChat={() => setSelectedId(member.id)}
                />
              ))}
            </div>
          ) : (
            // M3 §4: the 外包 worker list (list-only since T-66a8 — the tab
            // owns the switch, the count moved to the tab sub-line, and the
            // cap popover moved to the recruit button below). Clicking a row
            // opens the worker's chat channel (the worker id rides the SAME
            // chatId hash slot as a member chat).
            <OutsourcePanel
              workers={outsource.workers}
              error={outsource.error}
              maxParallel={outsource.maxParallel}
              selectedId={workerPeer?.id ?? ""}
              onOpenChat={(id) => setSelectedId(id)}
              onOpenDetail={(id) => setWorkerDetailId(id)}
              // The row's T-xxxx chip jumps to the bound task's card — the same
              // #tasks/<id> locate-anchor route the reply cards use.
              onOpenTask={(taskId) => setRoute({ page: "tasks", taskId })}
            />
          )}
          {/* 招攬新成員 — pinned at the sidebar bottom (owner mockup
           * 2026-07-18), routing by the active tab: 正職 → 角色誌 create mode
           * (#settings/roles/new); 外包 → the 外包上限設定 popover, anchored
           * here and opening upward. The button + popover share this wrapper so
           * the outside-click dismissal never counts the button as "outside". */}
          <div className="office__recruit-wrap" ref={recruitRef}>
            {capOpen && activeTab === "outsource" && (
              <OutsourceCapPopover
                maxParallel={outsource.maxParallel}
                onSave={async (n) => {
                  await outsource.saveMaxParallel(n);
                  setCapOpen(false);
                }}
              />
            )}
            <button
              type="button"
              className="office__recruit"
              aria-label={t.office.recruit}
              aria-expanded={activeTab === "outsource" ? capOpen : undefined}
              data-testid="office-recruit"
              onClick={onRecruit}
            >
              <PersonPlusIcon size={16} />
              <span>{t.office.recruit}</span>
            </button>
          </div>
        </aside>
      )}

      {showChat && (workerMember || releasedPeer || selected) && (
        <section className="office__chat">
          {isMobile && (
            <button
              type="button"
              className="office__back"
              onClick={() => setSelectedId("")}
            >
              <ChevronLeftIcon size={18} />
              <span>{t.office.backToMembers}</span>
            </button>
          )}
          {releasedPeer ? (
            // T-661b: a 跳到原訊息 whose chatId resolves to neither a roster
            // member nor a LIVE worker (a released outsource worker / removed
            // member). Render the ORIGINAL conversation read-only — history is
            // keyed by peer id, so it is still reachable — with an honest
            // "已釋出 / 不在名單" subtitle instead of a fabricated presence, and
            // NO onOpenDetail (there is no live detail to open → composer stays
            // the plain locked notice). jumpToMsgId still locates the ask.
            <ChatArea
              member={releasedPeer}
              members={members}
              workers={outsource.workers}
              jumpToMsgId={route.msgId}
              draftSeed={seedFor(releasedPeer.id)}
              headerSub={
                <span
                  className="chat__header-outsource"
                  data-testid="released-chat-sub"
                >
                  {isReleasedWorkerId
                    ? t.office.outsource.releasedChatSub
                    : t.office.chatUnavailableSub}
                </span>
              }
            />
          ) : workerMember && workerPeer ? (
            // M3 §4.2 outsource chat: the SAME ChatArea as a member chat
            // (打字/附檔/看回覆), keyed on the worker id as the chat peer.
            // Header title = 「外包 · 代號」; the subtitle is the SAME task
            // line the rail's outsource row shows — [clickable T-xxxx chip →
            // task type], the shared OutsourceTaskLine (owner 2026-07-16:
            // 兩邊顯示一樣的東西; replaces the old 狀態 · 標題 pair) — instead
            // of a member presence badge. NO dot here: outsource presence
            // lives only in the rail row, the header never grows a second
            // presence source. A worker is anonymous (no presence projection,
            // no unread counter), but it HAS a lean detail panel: the header
            // opens it (same gate the roster row's avatar uses), routed to
            // #office/worker/<id>. The chip's stopPropagation keeps the task
            // jump from also opening that detail.
            <ChatArea
              member={workerMember}
              members={members}
              workers={outsource.workers}
              onOpenDetail={() => setWorkerDetailId(workerPeer.id)}
              draftSeed={seedFor(workerPeer.id)}
              // T-3451: the bound task's FULL title under the T-xxxx·type sub —
              // owner: 外包側 header 同樣顯示完整 title. Rides the wire echo.
              headerTaskTitle={workerPeer.taskTitle ?? ""}
              headerSub={
                <span
                  className="chat__header-outsource"
                  data-testid="outsource-chat-sub"
                >
                  <OutsourceTaskLine
                    worker={workerPeer}
                    onOpenTask={(taskId) => setRoute({ page: "tasks", taskId })}
                    idPrefix="outsource-chat"
                  />
                </span>
              }
            />
          ) : (
            selected && (
              <ChatArea
                member={selected}
                // The full roster resolves an inter-agent message's sender id →
                // name (the sender may be a THIRD agent, not the window's peer).
                members={members}
                // Outsource senders are never in `members` — the live worker
                // list resolves their codename the same way the left rail does.
                workers={outsource.workers}
                // Reuse the existing detailId gate: the header opens the same
                // MemberDetailPanel the left-rail MemberCard avatar opens.
                onOpenDetail={() => setDetailId(selected.id)}
                // T-dfae 聊天 header 兩個圖示 (owner 2026-07-17). Wired ONLY on
                // this branch — the outsource / released branches above pass
                // neither, so no dead jump is ever advertised. Both go through
                // the hashRoute seam like every other jump on this page.
                onOpenTasks={() =>
                  setRoute({ page: "tasks", executorId: selected.id })
                }
                // The role KEY rides in the hash only — never rendered (T-fa76).
                // A member with no role has nothing to open, so no button.
                onOpenRoleSettings={
                  selected.role
                    ? () =>
                        setRoute({
                          page: "settings",
                          settingsRoles: true,
                          roleKey: selected.role,
                        })
                    : undefined
                }
                // T-94c1 就地喚醒: same activate contract as the detail panel's
                // spawn — writes desired_state=online INTENT (default machine
                // binding), then refetch lets server-driven presence surface
                // waking → online. Wired ONLY on this live-member branch (an
                // outsource worker is spawn/task-driven, not activate-woken).
                onWake={async () => {
                  // 🔴 T-7fa1: the in-chat wake row has its own optimistic
                  // 「喚醒中…」, so it needs the verdict too — returning void
                  // here leaves the chat surface stuck exactly as before.
                  const result = await api.activateMember(selected.id);
                  try {
                    await refetch();
                  } catch {
                    /* the verdict outlives a failed refresh (NIT-4) */
                  }
                  return result;
                }}
                // B3 跳到原訊息 (#office/chat/<id>/msg/<msgId>): locate +
                // highlight the ask message. Only meaningful for the EXPLICITLY
                // routed chat — the roster[0] fallback never inherits a stale
                // msg target.
                jumpToMsgId={
                  selected.id === route.chatId ? route.msgId : undefined
                }
                draftSeed={seedFor(selected.id)}
              />
            )
          )}
        </section>
      )}
    </div>
  );
}
