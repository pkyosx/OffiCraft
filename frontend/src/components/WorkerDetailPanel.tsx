import { useState } from "react";
import { useI18n } from "../i18n";
import { workerStatusText } from "../i18n/compose";
import type { OutsourceWorkerView } from "../api/adapter";
import { useMachines } from "../hooks/useMachines";
import { AgentDetailPanel } from "./AgentDetailPanel";
import { useRelocateMachine } from "./useRelocateMachine";
import { ChevronRightIcon } from "./icons";
import { AvatarEditor } from "./AvatarEditor";
import { Avatar } from "./Avatar";
import { LifecycleDot, presenceVisual } from "./LifecycleDot";
import "./member-detail.css";

interface WorkerDetailPanelProps {
  worker: OutsourceWorkerView;
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
  /** Stop (停止 — T-f190): kill + hold down (owner-explicit; no auto-revival). */
  onStop?: () => Promise<void>;
  /** Restart (重啟 — T-f190): clear the stop + re-dispatch. */
  onRestart?: () => Promise<void>;
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
  onUpdateAvatar?: (file: File) => Promise<void>;
  onRemoveAvatar?: () => Promise<void>;
}

/**
 * The outsource-worker detail view. Since T-ba6b it renders through the SAME
 * AgentDetailPanel the member detail page uses (owner constitution:「外包只是
 * 一個系統會幫我產生跟刪除的正職員工」) — the shared cards (模型/投入度、機器/
 * Claude Account、運行狀況、最近操作、終端、初始 PROMPT) read the ONE unified
 * view model, and the worker-specific bits (外包角色頭像身分 + 任務 chip、狀態 +
 * 委託人、委託任務、改機器) plug in through the panel's slots. Everything the
 * worker has not really reported renders an honest dash / 「尚未分配」 — never a
 * fabricated value (the shared panel's honest gate, the member's).
 */
export function WorkerDetailPanel({
  worker,
  onBack,
  onOpenTask,
  onRelocate,
  onRefocus,
  onStop,
  onRestart,
  onSetModel,
  onFetchBootContext,
  onUpdateAvatar,
  onRemoveAvatar,
}: WorkerDetailPanelProps) {
  const { t, msg } = useI18n();
  const dash = t.workerDetail.dash;

  const { machines } = useMachines();
  // 🔴 The relocate progress signal, in the SHAPE the hook compares (review
  // E.1/E.2). This panel used to pass nothing at all, so `landed` was false by
  // construction and EVERY successful 改機器 here spun the full 30s and then
  // painted an error-styled timeout — on the very panel the owner reported.
  //
  // It cannot be `worker.machine` raw: that is a DISPLAY NAME (server-side
  // `machineDisplay`), and comparing it against `desiredMachineId` (a raw id)
  // is never equal. Resolve it back through the machine registry — the same list
  // the picker already binds — and fall through to the raw value, because
  // machineDisplay itself falls back to the id when a machine has no label.
  //
  // WHAT IT HONESTLY MEANS, stated because the hook's prop doc warns about
  // exactly this class of signal: for a worker, `machine` is the last DISPATCH
  // TARGET. So convergence proves "the server acted on the new pin and sent the
  // start there", NOT "the agent is up on it". That is the right end for
  // 「更換中…」 — it is the owner's own operation completing — and it is a real
  // transition, not a degenerate one: before the relocate this holds the OLD
  // machine, so it can only equal the new pin after a dispatch actually
  // happened. Arrival is a separate fact and is not claimed anywhere here.
  const observedMachineId = (() => {
    const shown = (worker.machine ?? "").trim();
    if (!shown) return null;
    const hit = machines.find(
      (m) => m.displayName === shown || m.machineId === shown,
    );
    return hit ? hit.machineId : shown;
  })();
  // 改機器 (shared control — the member panel's, 文案統一 to 編輯): the picker's
  // bound entry is the owner-pinned placement (raw machine id).
  const { relocateAction, relocatePicker } = useRelocateMachine({
    subjectId: worker.id,
    machines,
    boundMachineId: worker.desiredMachineId || null,
    onRelocate,
    testId: "worker-detail-relocate",
    pickerTitle: t.workerDetail.relocateTitle,
    pickerConfirmLabel: t.workerDetail.relocateConfirm,
    noOnlineTitle: t.workerDetail.noOnlineMachine,
    currentMachineId: observedMachineId,
    // The failure half: the server writes a receipt for every refused spawn, so
    // a relocate that cannot land is answerable in seconds instead of at the
    // 30s ceiling.
    dispatchReceipt: {
      at: worker.lastOpAt ?? null,
      ok: worker.lastOpOk ?? null,
      reason: worker.lastOpReason ?? "",
    },
  });

  // ── honest presence projection (A案 P6 — the ONE member vocabulary) ────────
  // presence (wire `presence`, replacing the retired spawn_state) is the
  // REAL-liveness authority, distinct from the lifecycle status: a worker whose
  // session is not actually up is never drawn as a live green row. Machine =
  // the ACTUAL dispatch target (already resolved to a display name
  // server-side); "" ⇒ never dispatched ⇒ 「尚未分配」, never a fabricated
  // machine name.
  const online = worker.presence === "online";
  const offline = worker.presence === "offline";
  // stopping/stopped are both the owner-explicit hold-down (desired offline) —
  // the toggle and the status cell treat them as one 已停止 mode.
  const stopped =
    worker.presence === "stopped" || worker.presence === "stopping";
  const machineText = worker.machine || t.workerDetail.notAssigned;
  // The 狀態 CELL's wording is deliberately NOT the dot's label: it speaks the
  // 外包 detail vocabulary (工作中/啟動中) and, when there is no presence at all,
  // falls back to the worker's lifecycle status (已釋放…) — a fallback the dot
  // cannot have. That is the ONLY split; the dot itself is the shared
  // LifecycleDot below, so colour + a11y label can never drift from the roster.
  // Exhaustive switch: presence is the five-state union, so a new state fails to
  // compile here rather than silently landing in the fallback branch.
  const statusText = ((): string => {
    switch (worker.presence) {
      case "online":
        return t.workerDetail.working;
      case "waking":
        return t.workerDetail.starting;
      case "stopping":
      case "stopped":
        return t.workerDetail.stopped;
      case "offline":
        return t.workerDetail.offline;
      case undefined:
        return worker.status ? workerStatusText(t, worker.status) : dash;
    }
  })();

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

  // The structured failure reason is surfaced in the 狀態 slot when the worker
  // reads offline (a silently-failing spawn / died session); the shared panel
  // owns the 最近操作 receipt block, so only that case's reason folds here.
  const offlineReason = (worker.lastOpReason ?? "").trim();

  // ── stop / restart toggle (停止/重啟 — owner-explicit hold-down) ─────────────
  const [stopBusy, setStopBusy] = useState(false);
  const [stopError, setStopError] = useState(false);
  async function handleStopToggle() {
    const fn = stopped ? onRestart : onStop;
    if (!fn || stopBusy) return;
    setStopBusy(true);
    setStopError(false);
    try {
      await fn();
    } catch {
      setStopError(true);
    } finally {
      setStopBusy(false);
    }
  }
  const stopToggleLabel = stopBusy
    ? stopped
      ? t.workerDetail.restarting
      : t.workerDetail.stopping
    : stopped
      ? t.workerDetail.restart
      : t.workerDetail.stop;

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

  // ── identity slot: stable worker id's personal image, then the outsource
  // theme image and glyph fallbacks, plus codename + real presence. ──────────
  const identity = (
    <div className="mp-card mp-identity">
      {onUpdateAvatar && onRemoveAvatar ? (
        <AvatarEditor
          size={52}
          kind="outsource"
          src={worker.avatarUrl}
          onUpload={onUpdateAvatar}
          onRemove={onRemoveAvatar}
        />
      ) : (
        <Avatar size={52} kind="outsource" src={worker.avatarUrl} />
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
      </div>
    </div>
  );

  // ── afterInfoCards slot: 狀態 (+停止/重啟) | 委託人. ─────────────────────────
  const statusDelegatorCard = (
    <div className="mp-card mp-info2">
      <div className="mp-field">
        <div className="mp-field__head">
          <div className="mp-field__label">{t.workerDetail.status}</div>
          {/* 停止/重啟 toggle (owner-explicit hold-down): 停止 when live, 重啟
              when already stopped. Hidden when neither handler is wired. */}
          {(onStop || onRestart) && (
            <button
              type="button"
              className="doc-btn"
              data-testid="worker-detail-stop-toggle"
              disabled={stopBusy || (stopped ? !onRestart : !onStop)}
              onClick={() => void handleStopToggle()}
            >
              {stopToggleLabel}
            </button>
          )}
        </div>
        <div
          className={`mp-field__value${offline ? " mp-field__value--warn" : ""}`}
          data-testid="worker-detail-status"
        >
          {statusText}
        </div>
        {/* On an offline (failing-spawn / died-session) worker, surface the
            structured reason (never a bare 「離線」 with no why) — honest:
            hidden when none folded. */}
        {offline && offlineReason && (
          <div
            className="mp-field__hint"
            data-testid="worker-detail-stuck-reason"
          >
            {offlineReason}
          </div>
        )}
        {stopError && (
          <div className="mp-field__hint mp-info2__error">
            {t.workerDetail.stopError}
          </div>
        )}
      </div>
      <div className="mp-field mp-field--divider">
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

  return (
    <AgentDetailPanel
      onBack={onBack}
      identity={identity}
      overlays={relocatePicker}
      afterIdentityCards={taskCard}
      afterInfoCards={statusDelegatorCard}
      vm={{
        testIdPrefix: "worker-detail",
        online,
        runtime: worker.runtime || "claude",
        model: worker.model,
        effort: worker.effort,
        modelEffortNote: t.workerDetail.modelNextSpawnNote,
        // 換 model: active+online takes effect now (server kill+respawn),
        // otherwise the next spawn bakes it in. Read-only when unwired.
        onSaveModelEffort: onSetModel
          ? async (runtime, model, effort) => {
              await onSetModel(runtime, model, effort);
            }
          : undefined,
        machineText,
        machineAction: relocateAction,
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
