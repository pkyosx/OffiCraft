import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { ApiError } from "../api/errors";
import type { OutsourceWorkerView } from "../api/adapter";
import type { MonSessionView } from "../types";
import { useMachines } from "../hooks/useMachines";
import {
  AgentDetailPanel,
  runtimeLabel,
  slot,
} from "./AgentDetailPanel";
import { pendingChangeHint, reportedMachine } from "../lib/pendingChange";
import { ModelEffortEditor } from "./ModelEffortEditor";
import { ChevronLeftIcon, ChevronRightIcon } from "./icons";
import { Avatar } from "./Avatar";
import { ResumeSummaryCard } from "./ResumeSummaryCard";
import { AvatarChooser } from "./AvatarChooser";
import { LifecycleDot, presenceVisual } from "./LifecycleDot";
import { MemberActionButtons, stopLadderStageOf } from "./MemberActionButtons";
import { ScheduledMessagesCard } from "./ScheduledMessagesCard";
// 🔴 This panel renders its settings dialog with the .machine-picker* classes,
// so it must import their stylesheet ITSELF (T-7526). Both panels used to
// free-ride on WorkerDetailPanel → useRelocateMachine → MachinePicker →
// machine-picker.css; when the worker panel stopped driving that hook, the
// last production importer went with it and BOTH dialogs rendered unstyled.
// Style ownership follows the class names, not a transitive accident.
import "./machine-picker.css";
import "./member-detail.css";

interface WorkerDetailPanelProps {
  worker: OutsourceWorkerView;
  /** This worker's live telemetry row from `GET /api/monitoring` (workers ride
   * the SAME `sessions` array as members, keyed by their `ow-` id). It — not
   * the worker DTO — is the source for the 模型 / 思考強度 STATE readout;
   * `undefined` (nothing reported) renders the honest dash. */
  session?: MonSessionView;
  onBack: () => void;
  /** Jump to the bound task (#tasks/<taskId>); only wired when the worker has a
   * resolvable task. */
  onOpenTask?: () => void;
  /** Relocate the worker to a machine (owner 改機器 — T-f190). Undefined ⇒ the
   * 改機器 affordance is hidden (the office entry always wires it; a caller that
   * cannot relocate simply omits it). The panel leans on the outsource_worker
   * SSE refetch for the post-move refresh, so the handler need only fire. */
  onRelocate?: (machineId: string) => Promise<void>;
  /** Refocus (換手 — T-32e1): kill+respawn the session onto the SAME task. The
   * worker twin of the member refocus. Undefined ⇒ the affordance is hidden. */
  onRefocus?: () => Promise<void>;
  /** Stop (停止 — T-f190; a GRACEFUL close-out since T-ed79): hold the worker
   * down and show it the 下線程序; the 收口 is its own report_stopped. The FIRST
   * rung of 停止 → 加速停止 → 強制停止. */
  onStop?: () => Promise<void>;
  /** 加速停止 (T-ed79) — the MIDDLE rung: put the wind-down that is already open
   * on the server's clock and TELL the worker. Not a kill, so it needs no
   * confirm. Undefined ⇒ the rung renders disabled (the honesty rule). */
  onAcceleratedStop?: () => Promise<void>;
  /** 強制停止 (T-ed79) — the THIRD rung: kill the session NOW, saying nothing.
   * This panel gates it behind its own confirm. */
  onForceStop?: () => Promise<void>;
  /** Wake (喚醒 — T-7526): clear the stop + re-dispatch. ⚠️ The WIRE is still
   * `POST /api/outsource-workers/{id}/restart` — a frozen contract (§13). Only
   * the owner-facing WORD changed (owner 2026-07-31 「應該要統一」: 重啟 retired,
   * 喚醒 is the one verb on both panels). Do NOT rename the endpoint to match. */
  onWake?: () => Promise<void>;
  /** Change model/effort (換 model — T-f190): active → takes effect now,
   * assigned → next spawn. Undefined ⇒ the model cell is read-only. */
  onSetModel?: (
    runtime: "claude" | "codex",
    model: string,
    effort: string,
  ) => Promise<void>;
  /** Fetch the worker's initial-prompt PREVIEW (GET …/boot-context — T-ba6b):
   * the server re-runs the spawn fold over the CURRENT task/manual rows and
   * returns the text (no token). Undefined ⇒ the initial-prompt card is hidden
   * (a caller below the admin_agent floor omits it — T-6020). */
  onFetchBootContext?: () => Promise<string>;
  onSetThemeAvatar?: (iconId: string) => Promise<void>;
}

/**
 * The outsource-worker detail view. Since T-ba6b it renders through the SAME
 * AgentDetailPanel the member detail page uses (owner constitution:「外包只是
 * 一個系統會幫我產生跟刪除的正職員工」) — the shared cards (模型/思考強度、機器/
 * Claude Account、運行狀況、最近操作、終端、初始 PROMPT) read the ONE unified
 * view model, and the worker-specific bits (外包角色頭像身分 + 任務 chip、
 * 委託人、委託任務) plug in through the panel's slots. Since T-7526 the shared
 * cards are READ-ONLY here too (the member panel's shape since T-927a): 模型/
 * 思考強度 and 機器 carry no in-place editor, and every edit goes through the ONE
 * dialog the identity card's action row opens — 更改 while it is running, 喚醒
 * while it is not (owner 2026-07-31). Everything the
 * worker has not really reported renders an honest dash / 「尚未分配」 — never a
 * fabricated value (the shared panel's honest gate, the member's).
 */
export function WorkerDetailPanel({
  worker,
  session,
  onBack,
  onOpenTask,
  onRelocate,
  onRefocus,
  onStop,
  onAcceleratedStop,
  onForceStop,
  onWake,
  onSetModel,
  onFetchBootContext,
  onSetThemeAvatar,
}: WorkerDetailPanelProps) {
  const { t, msg } = useI18n();
  const dash = t.workerDetail.dash;

  const { machines } = useMachines();

  // ── honest presence projection (A案 P6 — the ONE member vocabulary) ────────
  // presence (wire `presence`, replacing the retired spawn_state) is the
  // REAL-liveness authority, distinct from the lifecycle status: a worker whose
  // session is not actually up is never drawn as a live green row. Machine =
  // the ACTUAL dispatch target (already resolved to a display name
  // server-side); "" ⇒ never dispatched ⇒ 「尚未分配」, never a fabricated
  // machine name.
  const online = worker.presence === "online";
  const offline = worker.presence === "offline";
  // 🔴 stopping and stopped are NO LONGER one mode (T-ed79). They were, while
  // 停止 killed the session on the spot: `stopping` was a blink between the
  // click and the kill landing, so folding it into 已停止 cost nothing. Now 停止
  // is a close-out the worker WORKS, and `stopping` is the whole duration of it
  // — the exact window in which the owner needs the two escalation rungs. Folding
  // it into the 喚醒 arm would hide 加速停止 and 強制停止 for precisely as long as
  // they are the only things that can end the wait, which is the staff-side
  // defect T-2123 already had to fix once.
  const stoppingNow = worker.presence === "stopping";
  const stopped = worker.presence === "stopped";
  // 🔴 The action row keys off LIVENESS, not off who asked for it (T-7526). A worker
  // whose session died on its own reads `offline` — nobody pressed 停止, so the
  // old `stopped`-only test showed 停止 on a worker with nothing to stop, and the
  // ONE affordance that brings it back was nowhere on screen. Both no-live-session
  // states take the 喚醒 arm; the server's restart guard was widened the same
  // way (INTENT → LIVENESS), so the two sides now agree.
  const noLiveSession = stopped || offline;

  // 🔴 RELEASED is not a presence — it is the worker's own lifecycle end
  // (WorkerStatusReleased on the wire). It deliberately does NOT come from the
  // dot: `presence` is `undefined` for a released worker AND for one that was
  // never dispatched, so `presenceVisual` maps both to the same grey `offline`
  // and cannot tell them apart. Reading it from `status` is the only honest
  // source — and it is why `presenceVisual`'s five-state no-default switch was
  // left alone (widening it would reach the 正職 roster, which has no such state).
  const released = worker.status === "released";
  // Which of the two things the ONE settings dialog does when confirmed: wake a
  // worker that is not running, or change a running one. Same split as the
  // member panel's `online`, and it decides the dialog's title + confirm word
  // too, so the button can never promise something the click does not do.
  // 🔴 stoppingNow is deliberately on the WAKE side of this particular split,
  // which is the opposite of where the button row puts it, and both are right
  // because they answer different questions. The row asks "can the owner still
  // escalate?" (yes — the session is alive and working its close-out); this
  // asks "what does the ONE settings dialog DO when confirmed?", and for a
  // worker the owner has already held down the answer is 喚醒: /restart is
  // reachable there (its guard refuses only a worker that is BOTH not held down
  // and online), so the confirm really does revive it. That is what makes the
  // ladder's first rung — Spawn, which opens this same dialog — a genuine
  // rescue rather than a button that opens a dialog promising 更改 and then
  // changes nothing on a held-down worker.
  const wakeMode = noLiveSession || stoppingNow;
  const machineText = worker.machine || t.workerDetail.notAssigned;
  // ── the four "changed, not applied yet" hints (T-7f28) ────────────────────
  // This panel had NONE of these — not even for 機器, which the member panel
  // has had all along. Same rule, same shared helper, so the two panels cannot
  // drift apart again. `worker.machine` is the display name the server already
  // resolved, so the pin is resolved the same way before they are compared.
  //
  // 🔴 BOTH SIDES ARE RESOLVED TO DISPLAY NAMES BEFORE THEY ARE COMPARED. The
  // worker wire is asymmetric: `machine` arrives ALREADY resolved server-side
  // ("Mac Studio (mac-1)") while `desired_machine_id` and `actual_machine` are
  // raw ids. Comparing a display name against a raw id makes every correctly
  // placed worker look mid-relocation — the false-positive twin of the bug
  // this ticket exists to kill, and it would have shipped as a hint on every
  // healthy row.
  const machineDisplay = (id: string) =>
    machines.find((m) => m.machineId === id)?.displayName || id;
  const desiredMachineDisplay = machineDisplay(worker.desiredMachineId ?? "");
  const pendingMachine = pendingChangeHint(
    desiredMachineDisplay,
    reportedMachine(
      worker.machine ?? "",
      machineDisplay(worker.actualMachine ?? ""),
    ),
    msg.workerMachineMovingTo,
  );
  const pendingRuntime = pendingChangeHint(
    worker.runtime || "claude",
    worker.actualRuntime ?? "",
    msg.agentPendingChange,
    runtimeLabel(worker.runtime || "claude"),
  );
  const pendingModel = pendingChangeHint(
    worker.model,
    worker.actualModel ?? "",
    msg.agentPendingChange,
  );
  const pendingEffort = pendingChangeHint(
    worker.effort,
    worker.actualEffort ?? "",
    msg.agentPendingChange,
  );
  // 🔴 There is NO 狀態 cell any more (owner 2026-07-31:「外包為什麼需要工作狀態
  // 這個UI介面」). Four of its five words (工作中/啟動中/已停止/離線) restated what
  // the identity card's LifecycleDot already says in colour AND in its
  // aria-label — the same fact written twice, and a second place to drift from
  // `presenceVisual`. The fifth, 已釋放, is the released worker, and the chat
  // room's own banner (「已結案離隊，以下為歷史對話（唯讀）」) already says that
  // where the owner actually meets it. So the whole cell went, and with it the
  // `workerStatusText` lookup that existed only to feed it.
  // What did NOT go is the offline REASON below — that is not a restatement of
  // the dot, it is the only answer to "why is it grey".

  // 委託人 (item 2): the RESOLVED creator name replaces the former hardcoded
  // "System owner". delegatedBy carries a real member name; a blank name with
  // creator_id === "owner" is the owner's own ticket; a non-owner creator_id
  // with no resolvable name (removed member) falls back to the raw id — never a
  // fabricated name; a blank creator_id (pre-column / server-scheduled) shows
  // the honest 系統排程 fallback.
  const delegatorText = worker.delegatedBy
    ? worker.delegatedBy
    : worker.creatorId === "owner"
      ? t.workerDetail.delegatorOwner
      : worker.creatorId
        ? worker.creatorId
        : t.workerDetail.delegatorSystem;

  // The structured failure reason, surfaced under the identity card's presence
  // dot when the worker reads offline (a silently-failing spawn / died session);
  // the shared panel owns the 最近操作 receipt block, so only that case's reason
  // folds here.
  const offlineReason = (worker.lastOpReason ?? "").trim();

  // ── 停止 (owner-explicit hold-down) ────────────────────────────────────────
  // The WAKE half of the old toggle is no longer a button that fires straight
  // at an endpoint: it opens the settings dialog first (openSettings below), the
  // member panel's shape. Only 停止 still acts on click.
  const [stopBusy, setStopBusy] = useState(false);
  const [stopError, setStopError] = useState(false);
  const [forceStopConfirm, setForceStopConfirm] = useState(false);
  async function handleStop() {
    if (!onStop || stopBusy) return;
    setStopBusy(true);
    setStopError(false);
    try {
      await onStop();
    } catch {
      setStopError(true);
    } finally {
      setStopBusy(false);
    }
  }
  // The other two rungs share stopBusy/stopError: they are one escalation on one
  // row, and a per-rung spinner would let two of them look live at once.
  async function handleAcceleratedStop() {
    if (!onAcceleratedStop || stopBusy) return;
    setStopBusy(true);
    setStopError(false);
    try {
      await onAcceleratedStop();
    } catch {
      setStopError(true);
    } finally {
      setStopBusy(false);
    }
  }
  async function confirmForceStop() {
    if (!onForceStop || stopBusy) return;
    setStopBusy(true);
    setStopError(false);
    try {
      await onForceStop();
      setForceStopConfirm(false);
    } catch {
      setStopError(true);
    } finally {
      setStopBusy(false);
    }
  }

  // ── 設定區 (更改 / 喚醒 — 與正職同一套形狀, T-7526) ─────────────────────────
  // ONE dialog holds 執行環境 + 模型 + 思考強度 + 機器, opened from the identity
  // action row by BOTH 更改 (live worker) and 喚醒 (no live session). The panel
  // itself is READ-ONLY: the cells state what is currently true, every edit goes
  // through here — the member panel's shape since T-927a, now the outsource
  // panel's too.
  //
  // ⚠️ It deliberately has NO 「只儲存，不喚醒」 twin button, and that is NOT the
  // member panel's shape (the member offers one). The reason is the wire: the
  // member's 「只儲存」 is a PATCH + a placement-only relocate, neither of which
  // starts anything. The worker's relocate is not placement-only — it kills and
  // re-dispatches unless desired_state is already offline — so a worker button
  // promising "saved, not started" would be a lie for exactly the workers whose
  // session merely died (desired_state still online). Offering it would need a
  // pin-only worker endpoint, i.e. a frozen-wire change (§13).
  const onlineMachines = machines.filter((m) => m.online);
  // The pinned machine stays in the list even when it is not online — labelled
  // 離線 and disabled, MachinePicker's rule. Dropping it would silently move a
  // worker the owner deliberately parked; leaving it selectable would wind the
  // worker down onto a machine with no warden.
  const pinnedOfflineMachine =
    worker.desiredMachineId &&
    !onlineMachines.some((m) => m.machineId === worker.desiredMachineId)
      ? (machines.find((m) => m.machineId === worker.desiredMachineId) ?? {
          machineId: worker.desiredMachineId,
          displayName: worker.desiredMachineId,
        })
      : undefined;
  const settingsMachineOptions = [
    ...onlineMachines.map((m) => ({
      machineId: m.machineId,
      label: m.displayName,
      offline: false,
    })),
    ...(pinnedOfflineMachine
      ? [
          {
            machineId: pinnedOfflineMachine.machineId,
            label: msg.machineOfflineOption(pinnedOfflineMachine.displayName),
            offline: true,
          },
        ]
      : []),
  ];
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsRuntime, setSettingsRuntime] = useState<"claude" | "codex">(
    worker.runtime || "claude",
  );
  const [settingsModel, setSettingsModel] = useState(worker.model);
  const [settingsEffort, setSettingsEffort] = useState(worker.effort);
  const [settingsMachineId, setSettingsMachineId] = useState(
    worker.desiredMachineId ?? "",
  );
  const [settingsBusy, setSettingsBusy] = useState(false);
  const [settingsError, setSettingsError] = useState("");
  // Neither OfficePage nor MonitorPage passes a `key`, so switching which worker
  // the panel shows is a prop change, not a remount: an open dialog would
  // survive holding the PREVIOUS worker's draft, and one confirm would write
  // those values onto someone else. (The member panel's `[member.id]` effect.)
  useEffect(() => {
    setSettingsOpen(false);
    setSettingsError("");
  }, [worker.id]);
  // …and that reset is a RESET, not a CANCEL: a submit still in flight resolves
  // after it. Same render-time ref discipline the member panel uses.
  const shownWorkerIdRef = useRef(worker.id);
  shownWorkerIdRef.current = worker.id;

  /**
   * Seed the dialog with WHAT IS CURRENTLY TRUE — all four cells (owner
   * 2026-07-31:「我希望喚醒時，AI執行環境，模型，effort，機器，應該先預設跟原本
   * 一樣」). Every one of them stays EDITABLE (owner, same session:「將外包統一跟
   * 正職一樣，不是釘死」) — the defaults are a starting point, not a lock.
   */
  function openSettings() {
    setSettingsRuntime(worker.runtime || "claude");
    setSettingsModel(worker.model);
    setSettingsEffort(worker.effort);
    // 🔴 Seed the pin VERBATIM, carried over from the member panel's openSettings
    // together with the defect it fixes. Falling back to the first ONLINE machine
    // makes `machineChanged` unconditionally true for a worker pinned to a machine
    // that is merely ASLEEP — so opening the dialog just to edit a MODEL silently
    // re-pins the worker somewhere else. It lands hardest on "park it on my
    // sleeping laptop and save the settings for later". The pinned-but-offline
    // machine stays in `settingsMachineOptions`, labelled 離線 and disabled, so a
    // disabled <option> can still render as the current value without being
    // selectable — MachinePicker's rule, and the reason this seed is safe.
    setSettingsMachineId(
      worker.desiredMachineId || onlineMachines[0]?.machineId || "",
    );
    setSettingsError("");
    setSettingsOpen(true);
  }

  /**
   * Confirm. Two outcomes behind ONE dialog, exactly like the member panel:
   * - live worker (更改): persist the launch settings, relocate if the machine
   *   changed. Nothing is started; a no-edit confirm is a true no-op.
   * - no live session (喚醒): persist, re-pin if needed, then WAKE. Accepting
   *   the prefilled values unchanged must still wake — that is the whole point
   *   of the button — so the no-op early-return is gated on `!wakeMode`.
   *
   * 🔴 All three legs are EXISTING endpoints (`/model`, `/relocate`,
   * `/restart`); nothing new was added to the frozen wire. `/restart` is the one
   * that starts the worker, and it takes no machine — which is why the pin has
   * to be written by `/relocate` BEFORE it, not alongside it.
   */
  async function saveSettings() {
    const launchChanged =
      settingsRuntime !== (worker.runtime || "claude") ||
      settingsModel.trim() !== worker.model ||
      settingsEffort !== worker.effort;
    const machineChanged =
      settingsMachineId !== "" &&
      settingsMachineId !== (worker.desiredMachineId ?? "");
    if (!wakeMode && !launchChanged && !machineChanged) {
      setSettingsOpen(false);
      return;
    }
    setSettingsBusy(true);
    setSettingsError("");
    const firedFor = worker.id;
    try {
      // 🔴 The model op goes FIRST. A relocate kills the session and re-dispatches
      // on the new machine, so the launch intents must already be stored when it
      // fires — otherwise the fresh session comes up on the OLD model and the
      // owner's edit only takes effect one respawn later. Reversing these two
      // lines is exactly that bug (the member panel's same ordering, same reason).
      if (launchChanged) {
        await onSetModel?.(settingsRuntime, settingsModel.trim(), settingsEffort);
      }
      if (machineChanged) await onRelocate?.(settingsMachineId);
      // …and the wake goes LAST, after both intents are stored, so the session
      // that comes up is the one the owner just described. For a STOPPED worker
      // the two ops above are pure persistence (the server refuses to start
      // anything while desired_state=offline), so this is the only dispatch.
      if (wakeMode) await onWake?.();
      if (shownWorkerIdRef.current !== firedFor) return;
      setSettingsOpen(false);
    } catch (error) {
      if (shownWorkerIdRef.current !== firedFor) return;
      // The server's own envelope sentence is the only wire text fit to display;
      // ApiError.message carries the `http <status> for …` log form.
      setSettingsError(
        (error instanceof ApiError && error.serverMessage) ||
          t.mp.modelEffortError,
      );
    } finally {
      setSettingsBusy(false);
    }
  }

  // 累計總花費 = 已 banked 的歷史成本 + 當前 live session 成本 (DTO 保證兩者
  // 分開不重疊 — 與正職同一口徑，T-ba6b)。兩者皆 null ⇒ null ⇒ 誠實 dash。
  const totalCost =
    worker.cost == null && worker.bankedCost == null
      ? null
      : (worker.cost ?? 0) + (worker.bankedCost ?? 0);

  const taskStatusText = worker.taskStatus
    ? (t.tasks.status[worker.taskStatus] ?? worker.taskStatus)
    : "";
  const taskLabel = [worker.taskNo, worker.taskTitle]
    .filter(Boolean)
    .join(" · ");
  const hasTask = Boolean(worker.taskId && taskLabel);

  // ── identity slot: active-theme outsource pool + persistent index, then
  // glyph fallback, plus codename + real presence. ───────────────────────────
  const identity = (
    <div className="mp-card mp-identity">
      <Avatar size={52} kind="outsource" avatarIconId={worker.avatarIconId} />
      {onSetThemeAvatar && (
        <AvatarChooser
          value={worker.avatarIconId}
          kind="outsource"
          onSave={onSetThemeAvatar}
          label={t.workerDetail.avatarPickLabel}
          changeLabel={t.workerDetail.avatarPickChange}
          dialogTitle={t.workerDetail.avatarPickTitle}
          closeLabel={t.workerDetail.avatarPickClose}
          emptyLabel={t.workerDetail.avatarPickEmpty}
          brokenLabel={t.workerDetail.avatarPickBroken}
          savingLabel={t.workerDetail.avatarPickSaving}
          errorLabel={t.workerDetail.avatarPickError}
        />
      )}
      <div className="mp-identity__body">
        <div className="mp-identity__line">
          <span className="outsource-row__codename">
            {msg.outsourceLabel(worker.codename)}
          </span>
        </div>
        <div
          className="outsource-row__task-line"
          data-testid="worker-detail-header-task"
        >
          {/* Presence dot — the SAME shared LifecycleDot + `presenceVisual`
              the 正職 roster and the 外包 rail row render (T-59d6): five
              distinct --color-dot-* colours, role="img" + label, never a
              private colour literal and never a fabricated live green. */}
          <LifecycleDot
            status={presenceVisual(worker.presence)}
            testId="worker-detail-header-dot"
          />
          {worker.taskNo && (
            <button
              type="button"
              className="outsource-row__chip outsource-row__chip--task"
              data-testid="worker-detail-header-chip"
              disabled={!onOpenTask}
              onClick={onOpenTask}
            >
              {worker.taskNo}
            </button>
          )}
          <span className="outsource-row__type">
            {worker.taskTypeName || worker.taskTypeKey || t.tasks.adhoc}
          </span>
        </div>
        {/* The offline REASON, moved here when the 狀態 cell went (owner
            2026-07-31). It is the one thing that cell carried which the dot does
            NOT: a bare grey dot on a worker whose spawn silently failed tells
            the owner nothing, and the 最近操作 receipt only renders once a warden
            op has actually reported (`hasLastOp`), which is precisely not the
            case for a start that was never dispatched. So it now sits directly
            under the dot it explains. Honest: hidden when nothing folded. */}
        {offline && offlineReason && (
          <div className="mp-field__hint" data-testid="worker-detail-stuck-reason">
            {offlineReason}
          </div>
        )}
      </div>
      {/* Right-hand action row — 更改 ＋ 停止 side by side (owner 2026-07-31
          「全部變成左右並排」), the member panel's row in the member panel's
          place. When there is no live session the pair collapses to the single
          喚醒 button, which opens the SAME settings dialog 更改 does (the member
          panel's shape: you say what it should come up as, then it starts).
          Hidden entirely when nothing is wired — a read-only embedding gets no
          dead affordance. */}
      {(onSetModel || onRelocate || onStop || onWake) && (
        <div className="mp-identity__actions">
          <div className="mp-identity__buttons">
            {!wakeMode && (onSetModel || onRelocate) && (
              <button
                type="button"
                className="btn btn--accent-ghost"
                data-testid="worker-detail-change"
                onClick={openSettings}
              >
                {t.mp.change}
              </button>
            )}
            {!noLiveSession ? (
              /* 🔴 THE SAME LADDER AS 正職, FROM THE SAME COMPONENT (T-ed79,
                 owner 2026-08-21 「往正職靠」＋「停止 → 加速停止 → 強制停止」).
                 It renders MemberActionButtons rather than three worker-shaped
                 buttons of its own, so the labels, the ORDER, WHICH RUNGS EXIST
                 YET and the danger styling are one implementation for both panels — the thing that used to drift
                 was exactly this row. That also retires the panel-local 停止
                 label (t.workerDetail.stop): one verb, one word, both panels. */
              <MemberActionButtons
                status={stoppingNow ? "stopping" : "online-awake"}
                // 按了才出現 — the member panel's line, from the shared
                // function: the worker DTO carries the same presence /
                // desired_state / refocus_since / refocus_op, so 外包 climbs the
                // ladder on exactly the same evidence 正職 does.
                stage={stopLadderStageOf(worker)}
                // Spawn is the wedge rescue MemberActionButtons already offers
                // in `stopping`, and here it is real: wakeMode is true for a
                // held-down worker, so this dialog's confirm reaches 喚醒.
                onSpawn={stoppingNow && onWake ? openSettings : undefined}
                onStop={onStop ? () => void handleStop() : undefined}
                // No confirm on the middle rung, for the member panel's stated
                // reason: 加速停止 gives the worker a deadline it is TOLD about
                // and can still beat, so a second click costs nothing
                // irreversible.
                onAcceleratedStop={
                  onAcceleratedStop
                    ? () => void handleAcceleratedStop()
                    : undefined
                }
                // The TOP rung keeps its confirm — it is the only irreversible
                // one left on this row.
                onForceStop={
                  onForceStop ? () => setForceStopConfirm(true) : undefined
                }
                labels={
                  stopBusy ? { stop: t.workerDetail.stopping } : undefined
                }
              />
            ) : (
              onWake && (
                <button
                  type="button"
                  className="btn btn--accent-ghost"
                  data-testid="worker-detail-wake"
                  onClick={openSettings}
                >
                  {t.lifecycle.action.spawn}
                </button>
              )
            )}
          </div>
          {stopError && (
            <div className="mp-field__hint mp-info2__error">
              {t.workerDetail.stopError}
            </div>
          )}
        </div>
      )}
      {/* The TOP rung's confirm — the member panel's dialog, in the worker
          panel's place. It is the ONE irreversible button on this row now that
          停止 asks instead of kills, which is exactly why it kept its gate while
          the other two did not. */}
      {forceStopConfirm && (
        <div
          className="mp-confirm"
          data-testid="worker-detail-force-stop-confirm"
          role="dialog"
          aria-modal="true"
        >
          <div className="mp-confirm__box">
            <div className="mp-confirm__title">{t.mp.forceStopConfirmTitle}</div>
            <p className="mp-confirm__body">
              {msg.memberForceStopConfirmBody(worker.codename)}
            </p>
            <div className="mp-confirm__actions">
              <button
                type="button"
                className="btn btn--ghost"
                onClick={() => setForceStopConfirm(false)}
                disabled={stopBusy}
              >
                {t.common.cancel}
              </button>
              <button
                type="button"
                className="btn btn--danger-ghost"
                data-testid="worker-detail-force-stop-confirm-btn"
                onClick={() => void confirmForceStop()}
                disabled={stopBusy}
              >
                {stopBusy ? t.mp.forceStopBusy : t.mp.forceStopConfirmAction}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );

  // ── 設定 dialog (身分卡的「更改」開這個) ────────────────────────────────────
  const settingsDialog = settingsOpen ? (
    <div
      className="machine-picker"
      role="dialog"
      aria-modal="true"
      data-testid="worker-detail-settings-dialog"
    >
      <div className="machine-picker__box">
        {/* The word follows what the confirm ACTUALLY does (the member panel's
            same rule): 更改 on a live worker reaches only /model + /relocate,
            while on a worker with no live session the confirm also reaches the
            wake endpoint. Saying 更改 there would promise a settings-only edit
            that the click does not perform. */}
        <div className="machine-picker__title">
          {wakeMode ? t.lifecycle.action.spawn : t.mp.change}
        </div>
        {/* The worker endpoint's REAL semantics — deliberately not the member
            panel's 「下次啟動要用哪一個」, which would be false for a working
            outsource worker (its model op respawns it now). */}
        <div
          className="mp-field__hint"
          data-testid="worker-detail-settings-note"
        >
          {t.workerDetail.modelNextSpawnNote}
        </div>
        <ModelEffortEditor
          runtime={settingsRuntime}
          model={settingsModel}
          effort={settingsEffort}
          onRuntimeChange={setSettingsRuntime}
          onModelChange={setSettingsModel}
          onEffortChange={setSettingsEffort}
        />
        {onRelocate && (
          <label className="machine-picker__field">
            <span className="machine-picker__label">{t.mp.machine}</span>
            <select
              className="machine-picker__select"
              data-testid="worker-detail-settings-machine"
              value={settingsMachineId}
              onChange={(e) => setSettingsMachineId(e.target.value)}
            >
              {settingsMachineOptions.map((machine) => (
                <option
                  key={machine.machineId}
                  value={machine.machineId}
                  // The offline entry exists so the owner's own pin stays visible
                  // and unchanged, NOT so a live worker can be moved onto a
                  // machine whose warden is not there.
                  disabled={machine.offline}
                >
                  {machine.label}
                </option>
              ))}
            </select>
          </label>
        )}
        <div className="machine-picker__actions">
          <button
            type="button"
            className="btn btn--ghost"
            disabled={settingsBusy}
            onClick={() => setSettingsOpen(false)}
          >
            {t.common.cancel}
          </button>
          <button
            type="button"
            className="btn btn--accent"
            data-testid="worker-detail-settings-confirm"
            disabled={settingsBusy}
            onClick={() => void saveSettings()}
          >
            {wakeMode ? t.lifecycle.action.spawn : t.mp.change}
          </button>
        </div>
        {settingsError && (
          <div
            className="mp-field__hint mp-relocate__reason"
            data-testid="worker-detail-settings-error"
          >
            {settingsError}
          </div>
        )}
      </div>
    </div>
  ) : null;

  // ── afterInfoCards slot: 委託人 only. ──────────────────────────────────────
  // The 狀態 half of this card is GONE (owner 2026-07-31 — see the note at the
  // top of the component), and the 停止/喚醒 button that lived in its header
  // moved up to the identity card's action row. 委託人 stays: it is the one
  // thing on this card that is outsource-only and appears nowhere else — the
  // worker was minted on someone's behalf, and "whose" has no other home.
  // Plain .mp-card, not .mp-info2: that grid existed to put 狀態 and 委託人 in
  // two columns, and a `1fr 1fr` grid holding ONE field leaves the value
  // floating in the left half of an empty card. .mp-field carries the padding.
  const delegatorCard = (
    <div className="mp-card">
      <div className="mp-field">
        <div className="mp-field__label">{t.workerDetail.delegator}</div>
        <div className="mp-field__value" data-testid="worker-detail-delegator">
          {delegatorText}
        </div>
      </div>
    </div>
  );

  // ── afterIdentityCards slot: 委託任務 (clickable → #tasks/<taskId>). Moved
  // above 模型/機器 per owner 2026-07-20 截圖 (T-b0e3) — was buried after 最近操作. ──
  const taskCard = (
    <div className="mp-card mp-worker-task">
      <div className="mp-card__title">{t.workerDetail.task}</div>
      {hasTask ? (
        <button
          type="button"
          className="mp-worker-task__link"
          onClick={onOpenTask}
          disabled={!onOpenTask}
          data-testid="worker-detail-task"
        >
          <span className="mp-worker-task__label">{taskLabel}</span>
          {taskStatusText && (
            <span className="mp-worker-task__status">{taskStatusText}</span>
          )}
          <ChevronRightIcon size={16} className="mp-worker-task__chevron" />
        </button>
      ) : (
        <div className="mp-field__value">{dash}</div>
      )}
    </div>
  );

  // ── extraExpandCards slot: 定期訊息 (T-f059) ───────────────────────────────
  // 🔴 NOT member-only. A schedule may bind to an `ow-` worker — chat's own
  // recipient rule allows outsource, and the server follows it — so the worker
  // panel drives the SAME card the member panel does, from the SAME component.
  // This slot had no caller here before, which is precisely how the webhook
  // section ended up living on only one of the two panels.
  const scheduleCard = <ScheduledMessagesCard memberId={worker.id} />;

  // ── released: the worker finished its task and left ──────────────────────
  // 🔴 ONE renderer, ONE sentence (owner 2026-07-31:「為什麼從不同進入頁面會有
  // 不同的顯示方式?不是應該要一致嗎」). The chat room's header reads the SAME
  // `office.outsource.released*` leaves; nothing here restates them in its own
  // words, because a second copy is exactly the bug being fixed.
  //
  // The shared cards are NOT rendered: a released worker has no session, no
  // machine, no context%, no live cost and no boot context, so every one of them
  // would render a dash. Eight honest dashes are not more honest than one
  // sentence — they just bury it. Every lifecycle affordance is gone for the
  // same reason it must be: the server answers 404 on /stop, /restart, /model,
  // /relocate and /refocus for a released worker, so a 更改 or 喚醒 button here
  // would be a dead affordance by construction.
  if (released) {
    return (
      <div className="mp" data-testid="worker-detail-released">
        <button type="button" className="mp__back" onClick={onBack}>
          <ChevronLeftIcon size={18} />
          <span>{t.mp.back}</span>
        </button>
        <div className="mp-card mp-identity">
          <Avatar size={52} kind="outsource" avatarIconId={worker.avatarIconId} />
          <div className="mp-identity__body">
            <div className="mp-identity__line">
              {/* The codename when we still have it (the worker was in view when
                  it finished), else the honest released label — never a
                  fabricated codename for an id we can no longer resolve. */}
              <span className="outsource-row__codename">
                {worker.codename
                  ? msg.outsourceLabel(worker.codename)
                  : t.office.outsource.releasedTitle}
              </span>
            </div>
            <div className="mp-identity__status">
              <span data-testid="worker-detail-released-sub">
                {t.office.outsource.releasedSub}
              </span>
            </div>
          </div>
        </div>
        {taskCard}
      </div>
    );
  }

  return (
    <AgentDetailPanel
      onBack={onBack}
      identity={identity}
      // EVERY slot the panel offers, no exceptions (T-0b4f) — see the twin
      // literal in MemberDetailPanel.tsx. This is the side the old optional
      // props kept silently short-changing: a card added to the member panel
      // simply did not exist here, and nothing said so.
      slots={{
        overlays: slot(settingsDialog),
        afterIdentityCards: slot(taskCard),
        afterInfoCards: slot(delegatorCard),
        extraExpandCards: slot(scheduleCard),
        afterPromptCards: slot(<ResumeSummaryCard agentId={worker.id} />),
      }}
      vm={{
        testIdPrefix: "worker-detail",
        online,
        runtime: worker.runtime || "claude",
        // STATE readout = what the worker's own telemetry reported; honest ""
        // when it reported nothing — never the configured value beside it.
        // Model/effort prefer the live monitoring session and fall back to the
        // DURABLE reported column, so they survive the session ending (T-7f28);
        // runtime has no session source and reads the column directly.
        reportedRuntime: worker.actualRuntime ?? "",
        model: session?.model || (worker.actualModel ?? ""),
        effort: session?.effort || (worker.actualEffort ?? ""),
        pending: {
          runtime: pendingRuntime,
          model: pendingModel,
          effort: pendingEffort,
          machine: pendingMachine,
        },
        // NO `machineAction` (T-7526): this panel is READ-ONLY, exactly like
        // the member panel since T-927a. The cells state what is currently
        // true; every edit goes through the 更改 dialog above.
        machineText,
        // Claude Account: the RESOLVED readable name (server already applied the
        // alias/label or nulled it — the raw credential key NEVER reaches here);
        // "" ⇒ the shared panel's honest dash (T-ba6b).
        accountText: worker.account || "",
        contextPct: worker.contextPct ?? null,
        compactionCount: worker.compactionCount ?? null,
        cost: totalCost,
        onRefocus: onRefocus
          ? async () => void (await onRefocus())
          : undefined,
        refocusSince: worker.refocusSince ?? null,
        refocusOp: worker.refocusOp,
        refocusDeadline: worker.refocusDeadline,
        refocusSubmittedNote: t.workerDetail.refocusSubmittedNote,
        refocusSinceLabel: msg.workerRefocusSince,
        lastOp: worker.lastOp ?? "",
        lastOpVerb:
          worker.lastOp === "start" || worker.lastOp === "worker_start"
            ? t.workerDetail.lastOpStart
            : worker.lastOp === "stop" || worker.lastOp === "worker_stop"
              ? t.workerDetail.lastOpStop
              : (worker.lastOp ?? ""),
        lastOpOk: worker.lastOpOk ?? null,
        lastOpLog: worker.lastOpLog ?? "",
        lastOpReason: worker.lastOpReason ?? "",
        lastOpAt: worker.lastOpAt ?? null,
        tmuxSession: `member-${worker.id}`,
        terminalHint: t.workerDetail.terminalHint,
        // Initial-prompt PREVIEW (boot-context): re-fetched when the viewed
        // worker changes. The honest caveat rides the note (目前版本重組,
        // 非派工當下逐字版). Hidden when the fetch handler is unwired.
        prompt: onFetchBootContext
          ? {
              fetch: onFetchBootContext,
              cacheKey: worker.id,
              hint: t.workerDetail.initialPromptHint,
              note: t.workerDetail.initialPromptNote,
            }
          : undefined,
      }}
    />
  );
}
