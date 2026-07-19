// TaskReassignDialog — 轉派 (T-160e, owner + 特助): re-point one task at a new
// executor. Two targets, a segmented toggle apart:
//
//   轉給成員 — pick ONE active staff member (the roster's assistants, minus the
//              current executor: the server 409s on a no-op reassign, so it is
//              never offered).
//   轉外包   — mint a FRESH worker on the spot; the knobs are the SAME three the
//              task type's 負責成員 carries (模型 / 投入程度 / 機器), reusing
//              ModelEffortEditor's MODEL_QUICK_PICKS + EFFORTS vocabulary and
//              useMachines' honest registry. No 雇用數量 axis: a reassign mints
//              exactly ONE worker for THIS task (copies is a per-type knob).
//
// Structure + visual language follow TaskManualsPage's AssigneeCard editor
// (segmented toggle → pick-list rows with a check dot → the 機器 note), but the
// classes are the dialog's own: AssigneeCard's manual-* rules are page-scoped to
// settings.css, and a tasks-side dialog reaching into that stylesheet would
// couple the two pages. The ConfirmModal shell IS reused — Esc/busy/error/action
// row come free, and it is already the repo's dialog language.
//
// The dialog only NAMES the target. Everything the handover implies (card
// expiry, step rewind, dismissing the old worker, minting the new one, notifying
// both sides) is the server's, and the task lands in 轉派中 — the NEW executor
// reports it back to 進行中 themselves. This dialog never flips a status.

import { useState } from "react";
import { useI18n } from "../i18n";
import type { Effort, Member } from "../types";
import type { TaskReassignInput, TaskView } from "../api/adapter";
import { useMachines } from "../hooks/useMachines";
import { useMonitoring } from "../hooks/useMonitoring";
import { ConfirmModal } from "./ConfirmModal";
import { EFFORTS, MODEL_QUICK_PICKS } from "./ModelEffortEditor";

/** One full-width segmented control — the AssigneeCard toggle's shape, in the
 * dialog's own class namespace. */
function Segmented<T extends string>({
  options,
  value,
  onPick,
  testidPrefix,
  ariaLabel,
  className,
}: {
  options: { value: T; label: string }[];
  value: T | null;
  onPick: (v: T) => void;
  testidPrefix: string;
  ariaLabel: string;
  /** Optional layout modifier (e.g. the 2x2 grid the 模型 picker uses so its 4
   * chips never leave a lone centered chip on a second row at 390px). */
  className?: string;
}) {
  return (
    <div
      className={`task-reassign__seg${className ? ` ${className}` : ""}`}
      role="radiogroup"
      aria-label={ariaLabel}
    >
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          className={`task-reassign__seg-cell${
            value === o.value ? " task-reassign__seg-cell--active" : ""
          }`}
          data-testid={`${testidPrefix}-${o.value}`}
          onClick={() => onPick(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** One radio row of a pick list (member / machine) — check dot + name + an
 * optional right-aligned state word. */
function PickRow({
  active,
  name,
  state,
  stateClass,
  testId,
  onPick,
}: {
  active: boolean;
  name: string;
  state?: string;
  stateClass?: string;
  testId: string;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      className={`task-reassign__row${
        active ? " task-reassign__row--active" : ""
      }`}
      data-testid={testId}
      onClick={onPick}
    >
      <span
        className={`task-reassign__check${
          active ? " task-reassign__check--on" : ""
        }`}
      />
      <span className="task-reassign__row-name">{name}</span>
      {state && (
        <span className={`task-reassign__row-state${stateClass ?? ""}`}>
          {state}
        </span>
      )}
    </button>
  );
}

export function TaskReassignDialog({
  task,
  members,
  onReassign,
  onClose,
}: {
  task: TaskView;
  members: Member[];
  /** Commit the reassign. A rejection surfaces inline and the dialog stays
   * open, so the owner can retry or pick a different target. */
  onReassign: (id: string, input: TaskReassignInput) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useI18n();
  // Candidates: real AI assistants (machines never execute tasks — the server
  // 400s a warden target) minus the current executor (a no-op reassign is a
  // 409; pre-filtering keeps the picker honest, the dupCandidates precedent).
  const roster = members.filter(
    (m) =>
      m.kind === "assistant" &&
      !(task.executorKind === "member" && m.id === task.executorId)
  );

  const [kindDraft, setKindDraft] = useState<"member" | "outsource">("member");
  const [memberDraft, setMemberDraft] = useState(roster[0]?.id ?? "");
  const [modelDraft, setModelDraft] = useState("");
  const [effortDraft, setEffortDraft] = useState<string>("medium");
  // No 自動分配 row: a reassign must NAME a machine (owner 2026-07-19). Opens
  // unselected so the owner makes an explicit pick; commit() blocks an empty one.
  const [machineDraft, setMachineDraft] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 機器 states: the registry's honest `online` joined with monitoring's live
  // per-machine agent count — the exact AssigneeCard rule (online + agents>0 ⇒
  // 忙碌, online + 0 ⇒ 閒置, offline ⇒ 離線). No fabricated load metric.
  const { machines } = useMachines();
  const { monitoring } = useMonitoring();
  const agentsOf = new Map(
    (monitoring?.machines ?? []).map((m) => [m.machine, m.agents])
  );
  function machineStateText(machineId: string, online: boolean): string {
    if (!online) return t.settings.assigneeMachineOffline;
    return (agentsOf.get(machineId) ?? 0) > 0
      ? t.settings.assigneeMachineBusy
      : t.settings.assigneeMachineIdle;
  }

  async function commit() {
    if (kindDraft === "member" && !memberDraft) {
      setError(t.tasks.reassignPickMember);
      return;
    }
    if (kindDraft === "outsource" && !machineDraft) {
      setError(t.tasks.reassignPickMachine);
      return;
    }
    const input: TaskReassignInput = {
      target:
        kindDraft === "member"
          ? { kind: "member", memberId: memberDraft }
          : {
              kind: "outsource",
              model: modelDraft.trim(),
              effort: effortDraft,
              machine: machineDraft,
            },
      ...(note.trim() ? { note: note.trim() } : {}),
    };
    setBusy(true);
    try {
      await onReassign(task.id, input);
      onClose();
    } catch (e) {
      console.warn("TaskReassignDialog: reassign failed", e);
      setError(t.tasks.reassignError);
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmModal
      testId="reassign"
      confirmTestId="reassign-confirm"
      body={
        <div className="task-reassign">
          <div className="task-reassign__title">
            {t.tasks.reassignTitle(task.taskNo)}
          </div>
          <div className="task-reassign__hint">{t.tasks.reassignBody}</div>

          <Segmented
            options={[
              { value: "member", label: t.tasks.reassignToMember },
              { value: "outsource", label: t.tasks.reassignToOutsource },
            ]}
            value={kindDraft}
            onPick={(v) => {
              setKindDraft(v);
              setError(null);
            }}
            testidPrefix="reassign-kind"
            ariaLabel={t.tasks.reassignTitle(task.taskNo)}
          />

          {kindDraft === "member" && (
            <div className="task-reassign__section">
              <div className="task-reassign__label">
                {t.tasks.reassignPickMember}
              </div>
              {roster.length === 0 ? (
                <div className="task-reassign__empty">
                  {t.tasks.reassignNoMembers}
                </div>
              ) : (
                <div className="task-reassign__list" role="radiogroup">
                  {roster.map((m) => (
                    <PickRow
                      key={m.id}
                      active={memberDraft === m.id}
                      name={m.name}
                      state={
                        (t.office.role as Record<string, string>)[m.role] ??
                        (m.roleName || m.role)
                      }
                      testId={`reassign-member-${m.id}`}
                      onPick={() => {
                        setMemberDraft(m.id);
                        setError(null);
                      }}
                    />
                  ))}
                </div>
              )}
            </div>
          )}

          {kindDraft === "outsource" && (
            <>
              {/* 模型 — ModelEffortEditor's quick-pick vocabulary as segmented
               * chips + the authoritative free input (blank ⇒ runtime default). */}
              <div className="task-reassign__section">
                <div className="task-reassign__label">
                  {t.settings.assigneeModelLabel}
                </div>
                <Segmented
                  options={MODEL_QUICK_PICKS.map((m) => ({
                    value: m,
                    label: m,
                  }))}
                  value={
                    (MODEL_QUICK_PICKS as readonly string[]).includes(modelDraft)
                      ? (modelDraft as (typeof MODEL_QUICK_PICKS)[number])
                      : null
                  }
                  onPick={(v) => setModelDraft(v)}
                  testidPrefix="reassign-model"
                  ariaLabel={t.settings.assigneeModelLabel}
                  className="task-reassign__seg--grid2"
                />
                <input
                  className="task-reassign__input"
                  value={modelDraft}
                  placeholder={t.settings.assigneeModelPlaceholder}
                  aria-label={t.settings.assigneeModelPlaceholder}
                  data-testid="reassign-model"
                  onChange={(e) => setModelDraft(e.target.value)}
                />
              </div>

              <div className="task-reassign__section">
                <div className="task-reassign__label">
                  {t.settings.assigneeEffort}
                </div>
                <Segmented
                  options={EFFORTS.map((e) => ({
                    value: e,
                    label: t.mp.effortLevel(e),
                  }))}
                  value={effortDraft as Effort}
                  onPick={(v) => setEffortDraft(v)}
                  testidPrefix="reassign-effort"
                  ariaLabel={t.settings.assigneeEffort}
                />
              </div>

              <div className="task-reassign__section">
                <div className="task-reassign__label">
                  {t.settings.assigneeMachineLabel}
                </div>
                <div className="task-reassign__list" role="radiogroup">
                  {machines.map((m) => (
                    <PickRow
                      key={m.machineId}
                      active={machineDraft === m.machineId}
                      name={m.displayName}
                      state={machineStateText(m.machineId, m.online)}
                      stateClass={` task-reassign__row-state--${
                        m.online
                          ? (agentsOf.get(m.machineId) ?? 0) > 0
                            ? "busy"
                            : "idle"
                          : "offline"
                      }`}
                      testId={`reassign-machine-${m.machineId}`}
                      onPick={() => setMachineDraft(m.machineId)}
                    />
                  ))}
                </div>
              </div>
            </>
          )}

          <div className="task-reassign__section">
            <div className="task-reassign__label">{t.tasks.reassignNote}</div>
            <textarea
              className="task-reassign__textarea"
              value={note}
              rows={2}
              placeholder={t.tasks.reassignNotePlaceholder}
              aria-label={t.tasks.reassignNote}
              data-testid="reassign-note"
              onChange={(e) => setNote(e.target.value)}
            />
          </div>
        </div>
      }
      error={error}
      cancelLabel={t.common.cancel}
      confirmLabel={t.tasks.reassignConfirm}
      busy={busy}
      onCancel={onClose}
      onConfirm={() => void commit()}
    />
  );
}
