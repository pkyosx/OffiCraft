import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { Member } from "../types";
import type { OutsourceWorkerView } from "../api/adapter";
import { api } from "../api";
import { useMembers } from "../hooks/useMembers";
import { useMonitoring } from "../hooks/useMonitoring";
import { useOutsourceWorkers } from "../hooks/useOutsourceWorkers";
import { useIsMobile } from "../hooks/useIsMobile";
import { joinSessionRuntime, findSessionFor } from "../lib/runtime";
import { useHashRoute } from "../lib/hashRoute";
import { openChatAttachErrorScope } from "../lib/chatDraftStore";
import { updateCachedWorkerAvatar } from "../hooks/useWorkerCodenames";
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

export function OfficePage({
  restoredChatId,
  onRestoredChatGone,
}: {
  /** Set by App ONLY when this load's open chat was reopened from the
   * browser-local "last office chat" memory rather than asked for by a hash. */
  restoredChatId?: string;
  /** Raised when that restored chat's peer turns out to be gone, so App can
   * fall back to the roster. Never raised for an explicitly requested chat. */
  onRestoredChatGone?: () => void;
} = {}) {
  const { t, msg } = useI18n();
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
  // 🔴 THE STAGING REFUSAL IS THIS PAGE'S, THE DRAFT IS NOT (T-48, R14-2.1).
  // 「圖片太大」/「最多 10 個檔案」 lives in the same module-level table as the
  // draft so that a read finishing while the owner is in another room can still
  // put its sentence where they will see it. That table outlives this page too,
  // which is one lifetime too many: leave for 任務, come back ten minutes later
  // and the red line is still describing a drop from ten minutes ago. It was
  // component state before the table existed, so leaving took it with it. This
  // is that lifetime, written down instead of inherited — and it clears the
  // NOTICES only: the drafts and their staged files must survive the
  // navigation, which is the whole reason they were moved out of the composer.
  //
  // 🔴 IT IS A SCOPE, NOT AN UNMOUNT CLEAR (R16 D-2). `<StrictMode>` runs this
  // effect setup → cleanup → setup on the first mount, and the clear it used to
  // call drops EVERY peer's notice — including one raised before this mount.
  // That destroyed exactly the notice this table exists to carry (a `FileReader`
  // refusal that finished while the owner was on 任務), in dev only, so dev and
  // prod disagreed about something the owner can see. The store counts open
  // scopes and defers the close past StrictMode's synchronous remount.
  useEffect(() => openChatAttachErrorScope(), []);
  const selectedId = route.chatId ?? "";
  const detailId = route.detailId ?? null;
  const workerDetailId = route.workerId ?? null;
  // Live session telemetry — the SAME source the Monitor page reads — so the
  // member-detail panel's context/cost match the monitor row (never divergent).
  // GATED (T-ec2c): only fetched/subscribed while a detail panel is open. The
  // only reader left is the member panel (joinSessionRuntime): since T-14 item 2
  // the worker panel derives model/effort from the worker DTO alone and consumes
  // nothing from monitoring, so a worker detail still subscribes the whole fold
  // for no reader. With no panel open, merely being on the office page makes
  // zero monitoring requests — no per-heartbeat refetch of the large fold.
  const { monitoring } = useMonitoring({
    enabled: detailId !== null || workerDetailId !== null,
  });
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
    .filter((m) => m.kind === "staff")
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
  // onto one. lifecycle "online" here is a CHAT-CAPABILITY flag (it drives
  // ChatArea's composer lock), NOT a presence claim: presence display is
  // REPLACED by headerSub below, so nothing about it reaches the screen — the
  // rail's dot is the one presence surface (T-59d6 retired the old「live worker
  // 恆 online」invariant for DISPLAY). Whether an offline worker's composer
  // should also lock/queue like an offline member's is a separate owner
  // decision and is deliberately NOT changed here.
  const workerMember: Member | undefined = workerPeer
    ? {
        ...blankChatPeer(
          workerPeer.id,
          msg.outsourceLabel(workerPeer.codename),
          "outsource",
        ),
        status: "online",
        lifecycle: "online",
        model: workerPeer.model,
        avatarUrl: workerPeer.avatarUrl,
        // ChatArea snapshots this before its own mark-read clears the room
        // (T-48: the LISTING stopped writing a watermark; the clearer is
        // ChatArea's explicit POST /api/chat/mark-read).  The
        // live worker's server-computed badge is therefore part of the same
        // entry contract as a regular member's unreadCount.
        unreadCount: workerPeer.unreadCount ?? 0,
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
            ? t.office.outsource.releasedTitle
            : t.office.chatUnavailableTitle,
          isReleasedWorkerId ? "outsource" : "staff",
        )
      : undefined;
  // A chat App reopened FROM MEMORY whose peer has since gone (fired member /
  // released worker) should not strand a cold load on the read-only history
  // panel nobody asked for — hand it back to App, which returns to the roster.
  //
  // `releasedPeer` is the ONLY sound trigger here: it is already gated on BOTH
  // the roster and the live-worker list having settled, so it cannot fire while
  // the lists are merely still loading. A plain `roster.find(...)` miss would
  // fire during that window and erase the memory of a perfectly live chat.
  //
  // Explicit navigation is untouched: `restoredChatId` is undefined unless this
  // exact chat came from memory, so a deep-linked departed peer still renders
  // its read-only history (T-661b).
  const restoredChatGone =
    releasedPeer !== undefined && releasedPeer.id === restoredChatId;
  useEffect(() => {
    if (restoredChatGone) onRestoredChatGone?.();
  }, [restoredChatGone, onRestoredChatGone]);

  const rosterDetail = detailId
    ? roster.find((m) => m.id === detailId)
    : undefined;
  // Join the live session's context/cost so the detail panel shows the SAME
  // value as the Monitor page (same source), not the member DTO's honest-null.
  const detail = rosterDetail
    ? joinSessionRuntime(rosterDetail, monitoring?.sessions ?? [])
    : undefined;

  // Outsource-worker detail: resolve the live worker behind #office/worker/<id>.
  const workerDetail = workerDetailId
    ? outsource.workers.find((w) => w.id === workerDetailId)
    : undefined;
  // 🔴 A RELEASED worker (task closed) is dropped from the live list by the
  // server, so the lookup above misses and this route used to self-heal to the
  // roster — SILENTLY. That was the inconsistency owner 2026-07-31 objected to
  // (「為什麼從不同進入頁面會有不同的顯示方式?不是應該要一致嗎」): the chat entry
  // said 「已結案釋出」 for the very same worker while this entry said nothing at
  // all and dumped you somewhere else.
  //
  // The test is the SAME one the released CHAT peer below uses, for the same
  // reason — an `ow-` id (the server mints `ow-`+hex, outsource_sched.go) that
  // is not in the LIVE list once BOTH lists have settled. The `loading` gate is
  // load-bearing: without it a not-yet-loaded LIVE worker flashes as released.
  // We synthesise only what we honestly know — the id — and let the panel render
  // its released view; the codename is left blank rather than invented, and the
  // panel falls back to the honest released label.
  const releasedWorkerDetail: OutsourceWorkerView | undefined =
    workerDetailId &&
    !workerDetail &&
    workerDetailId.startsWith("ow-") &&
    !loading &&
    !outsource.loading
      ? {
          id: workerDetailId,
          codename: "",
          model: "",
          effort: "",
          status: "released",
          taskId: "",
        }
      : undefined;

  if (workerDetail) {
    return (
      <WorkerDetailPanel
        worker={workerDetail}
        // The worker's OWN telemetry row. Still handed over, but since T-14
        // item 2 nothing inside the panel reads it — the model/effort readout
        // comes from the worker DTO's reported columns, not from this session.
        session={findSessionFor(workerDetail.id, monitoring?.sessions ?? [])}
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
        // T-32e1/T-f190 lifecycle ops (owner/admin-agent floor since T-6020). Each fires the mutation; the
        // outsource_worker SSE delta refetches so the panel adopts the new state.
        onRefocus={async () => {
          await api.refocusWorker(workerDetail.id);
        }}
        // 成本歸零 (T-53, owner-only + irreversible; the panel confirms first).
        // Refetch is what returns the cell to the dash: the reset clears both
        // halves server-side, so the next read has nothing to render.
        onResetCost={async () => {
          await api.resetMemberCost(workerDetail.id);
        }}
        // The escalation ladder, same three verbs and same order as 正職
        // (T-ed79). 停止 no longer kills: it asks the worker to work its
        // 〈停止〉 and waits for its own report_stopped.
        onStop={async () => {
          await api.stopWorker(workerDetail.id);
        }}
        onAcceleratedStop={async () => {
          await api.acceleratedStopWorker(workerDetail.id);
        }}
        onForceStop={async () => {
          await api.forceStopWorker(workerDetail.id);
        }}
        // 喚醒 (T-7526). The endpoint is still `restartWorker` → POST …/restart:
        // the frozen wire keeps its name, only the owner-facing word changed.
        onWake={async () => {
          await api.restartWorker(workerDetail.id);
        }}
        onSetModel={async (runtime, model, effort) => {
          await api.setWorkerModel(workerDetail.id, {
            runtime,
            model,
            effort,
          });
        }}
        onUpdateAvatar={async (file) => {
          const avatarUrl = await api.updateMemberAvatar(workerDetail.id, file);
          updateCachedWorkerAvatar(workerDetail.id, avatarUrl);
        }}
        onRemoveAvatar={async () => {
          await api.removeMemberAvatar(workerDetail.id);
          updateCachedWorkerAvatar(workerDetail.id, "");
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

  // The released detail entry — the SAME WorkerDetailPanel, so the sentence has
  // exactly one renderer. Only onBack is wired: every lifecycle endpoint answers
  // 404 for a released worker, so any other handler would be a dead affordance.
  if (releasedWorkerDetail) {
    return (
      <WorkerDetailPanel
        worker={releasedWorkerDetail}
        onBack={backFromWorkerDetail}
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
          // The stop is dispatched; only the refresh can still fail (NIT-4 again).
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-deactivate refetch failed (the stop was sent)",
              e
            );
          }
        }}
        // Force-stop (immediate kill): the LAST rung — the robust STOP goes to
        // the warden now. Refetch and let server-driven presence surface stopped.
        // 加速停止 — the MIDDLE rung of 停止 → 加速停止 → 強制停止. Puts the
        // wind-down that is already open on the server's clock and tells the
        // member; it is NOT a kill, so the member can still finish early.
        onAcceleratedStop={async () => {
          await api.acceleratedStopMember(detail.id);
          // Same rung, same split: the clock is already running server-side.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-accelerated-stop refetch failed (the stop was sent)",
              e
            );
          }
        }}
        onForceStop={async () => {
          await api.forceStopMember(detail.id);
          // The kill has gone out — a failed refresh must not read as a live member.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-force-stop refetch failed (the kill was sent)",
              e
            );
          }
        }}
        onRefocus={async () => {
          await api.refocusMember(detail.id);
          // The refocus is delivered whatever the roster read does.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-refocus refetch failed (the refocus was sent)",
              e
            );
          }
        }}
        // 成本歸零 (T-53, owner-only + irreversible; the panel confirms first).
        onResetCost={async () => {
          await api.resetMemberCost(detail.id);
          // 歸零 is irreversible and already done: reporting it as failed would invite
          // the owner to confirm the same destructive action twice.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-cost-reset refetch failed (the cost was cleared)",
              e
            );
          }
        }}
        onRename={async (name) => {
          await api.patchMember(detail.id, { name });
          // The name is stored; the roster read only repaints it.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-rename refetch failed (the name was saved)",
              e
            );
          }
        }}
        onUpdateAvatar={async (file) => {
          await api.updateMemberAvatar(detail.id, file);
          // The avatar is uploaded whatever the roster read does next.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-avatar-upload refetch failed (the avatar was saved)",
              e
            );
          }
        }}
        onRemoveAvatar={async () => {
          await api.removeMemberAvatar(detail.id);
          // And the removal is equally done before this read is sent.
          try {
            await refetch();
          } catch (e) {
            console.warn(
              "OfficePage: post-avatar-remove refetch failed (the avatar was removed)",
              e
            );
          }
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
              // The row's task-id chip jumps to the bound task's card — the same
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

      {/* 🔴 ONE MOUNTED ChatArea PER CONVERSATION, AND THE `key` IS THE WHOLE OF
          IT (T-48, R13-5). All three branches below sit in the same position of
          the same conditional expression, so without a key React reuses ONE
          component instance and a conversation switch is just a prop change —
          every piece of per-conversation state, every in-flight read and every
          latch inside `ChatArea` and `useChat` then survives into a room it does
          not belong to. Twelve reviews found twelve instances of that, and each
          was answered by re-implementing `key` a little further inside: a visit
          token, a keyed-state hook, a keyed-record hook, a machine-checked
          census of everything that had to be keyed.

          Keying the mount is the same statement, made once, by React. A switch
          unmounts: the state goes, the DOM refs go, the setters land in a
          discarded component, and nothing has to be enumerated. What survives on
          purpose survives because it lives OUTSIDE the component — the draft and
          its staged files, in `lib/chatDraftStore`, which is where a returning
          composer reads them from anyway.

          ⚠️ Removing a key here does not break a test that names it; it silently
          reopens all twelve. `lint-chat-area-key` is what goes red instead. */}
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
              key={releasedPeer.id}
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
                    ? t.office.outsource.releasedSub
                    : t.office.chatUnavailableSub}
                </span>
              }
            />
          ) : workerMember && workerPeer ? (
            // M3 §4.2 outsource chat: the SAME ChatArea as a member chat
            // (打字/附檔/看回覆), mounted on the worker id as the chat peer.
            // Header title = 「外包 · 代號」; the subtitle is the SAME task
            // line the rail's outsource row shows — [clickable task-id chip →
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
              key={workerPeer.id}
              member={workerMember}
              members={members}
              workers={outsource.workers}
              onOpenDetail={() => setWorkerDetailId(workerPeer.id)}
              draftSeed={seedFor(workerPeer.id)}
              // T-3451: the bound task's FULL title under the 任務編號·type sub —
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
                key={selected.id}
                member={selected}
                // The full roster resolves an inter-agent message's sender id →
                // name (the sender may be a THIRD agent, not the window's peer).
                members={members}
                // The live worker list, so an ow- sender `members` did not
                // resolve still gets the codename the left rail shows. Note
                // `members` is the UNFILTERED roster (not `roster`, which is
                // kind==='staff' only), and GET /api/members does carry
                // kind='outsource' rows — so this list is a fallback, not the
                // only outsource label source. Released workers are in
                // neither; ChatArea's useWorkerCodenames covers those.
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
