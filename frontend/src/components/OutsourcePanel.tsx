// OutsourcePanel — the office left rail's 外包 worker LIST (SPEC §4; row shape
// per the owner's 2026-07-14 screenshot report, folded to one task line
// 2026-07-16). Lists every LIVE (non-terminal) outsource worker.
//
// T-66a8 (owner mockup 2026-07-18): the 外包 block is no longer a collapsible
// section stacked under 正職 — the sidebar now switches 正職/外包 by a top tab
// (OfficeSidebarTabs). So this component lost its head entirely: the 「N / 上限」
// count moved to the tab's sub-line, the collapse toggle is gone (the tab IS
// the switcher), and the cap-setting popover moved to the 招攬新成員 button at
// the sidebar bottom (OutsourceCapPopover). What remains is the worker list,
// the honest load-failure notice, and the 已暫停指派 annotation (cap 0).
//
// Row shape (owner 2026-07-16 — one line, folding the 2026-07-14 report's
// lines 2+3):
//   line 1  代號 (O-7 式 — the worker's name; an outsource has no other)
//   line 2  上線綠點 at the LINE START (like a 正職 member row), then the
//           任務代號 (T-xxxx) chip — CLICKABLE, routes to #tasks/<taskId> —
//           then 接到的 task type (外包沒有角色名 — the bound task's type IS
//           its role line; a LIVE worker is by definition online, the same
//           invariant the worker chat header's synthetic member uses)
// plus the member-card unread badge at the row's flex end (wire unread_count).
// NO model name (the codename already implies it), no task title, no 識別鍵.
// Rows come 依任務建立時間新→舊 (the hook sorts + joins taskNo/taskTypeKey).
//
// Interactions (§4.2): clicking a row opens a CHAT CHANNEL with that worker
// (the caller routes chatId to the worker id — same ChatArea as members); the
// panel itself has NO management actions (the server owns assignment).

import { useI18n } from "../i18n";
import { useWindowActive } from "../hooks/useWindowActive";
import type { OutsourceWorkerView } from "../api/adapter";
import { BriefcaseIcon } from "./icons";
import { CurrentTaskTitle } from "./CurrentTaskTitle";

/** The worker's ONE-LINE task line — [T-xxxx chip → task type], optionally
 * led by the 上線綠點. SHARED between the rail's outsource row (dot; owner
 * 2026-07-16: 綠點在行首, member-row parity) and the worker chat header's
 * subtitle (owner 2026-07-16: 兩邊顯示一樣的東西 — same chip + type, but NO
 * dot: outsource presence lives only in the rail; the header never grows a
 * second presence source). One component so the two renderings can't drift.
 *
 *   · The T-xxxx chip stays CLICKABLE → #tasks/<taskId> (owner 2026-07-14:
 *     點 task id 連到該任務頁); stopPropagation so the jump never also
 *     fires the enclosing row/header click. Honest: rendered only when the
 *     bound task resolved (taskNo joined by useOutsourceWorkers).
 *   · "" typeKey = ad-hoc task → the tasks page's 自由代辦 word. */
export function OutsourceTaskLine({
  worker: w,
  onOpenTask,
  dot,
  idPrefix,
}: {
  worker: OutsourceWorkerView;
  onOpenTask: (taskId: string) => void;
  /** Lead the line with the 上線綠點 (rail row only — a LIVE worker is by
   * definition online, same invariant the chat header's synthetic member
   * carries; the header itself omits the dot). */
  dot?: boolean;
  /** data-testid namespace: `${idPrefix}-task-line-<id>` / `-task-<id>` /
   * `-type-<id>` — "outsource" (rail) keeps its historical testids. */
  idPrefix: string;
}) {
  const { t } = useI18n();
  return (
    <span
      className="outsource-row__task-line"
      data-testid={`${idPrefix}-task-line-${w.id}`}
    >
      {dot && <span className="outsource-row__online-dot" />}
      {w.taskNo && (
        <button
          type="button"
          className="outsource-row__chip outsource-row__chip--task"
          title={t.office.outsource.openTask}
          aria-label={t.office.outsource.openTask}
          data-testid={`${idPrefix}-task-${w.id}`}
          onClick={(e) => {
            e.stopPropagation();
            onOpenTask(w.taskId);
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          {w.taskNo}
        </button>
      )}
      <span
        className="outsource-row__type"
        data-testid={`${idPrefix}-type-${w.id}`}
      >
        {w.taskTypeName || w.taskTypeKey || t.tasks.adhoc}
      </span>
    </span>
  );
}

export function OutsourcePanel({
  workers,
  error,
  maxParallel,
  selectedId,
  onOpenChat,
  onOpenDetail,
  onOpenTask,
}: {
  /** LIVE workers, already sorted 任務建立新→舊 + chip-joined
   * (useOutsourceWorkers). */
  workers: OutsourceWorkerView[];
  /** Honest load-failure flag — never render a dead fetch as an empty panel. */
  error: boolean;
  /** The global cap; -1 = 無限, 0 = assignment paused (annotated below). The
   * cap DISPLAY itself moved to the 外包 tab's sub-line (OfficeSidebarTabs);
   * this panel keeps maxParallel only to annotate the paused (0) state. */
  maxParallel: number | null;
  /** The worker whose chat is currently open (row highlight); "" = none. */
  selectedId: string;
  onOpenChat: (workerId: string) => void;
  /** Open the worker's lean detail panel (avatar target, mirroring MemberCard:
   * the row body opens the chat, the avatar opens the detail). */
  onOpenDetail: (workerId: string) => void;
  /** Jump to the bound task's card (#tasks/<taskId>) — the row's T-xxxx chip
   * (owner report 2026-07-14: 點 task id 連到該任務頁). */
  onOpenTask: (taskId: string) => void;
}) {
  const { t } = useI18n();
  // Badge suppression parity with MemberCard: the open, WATCHED conversation
  // consumes reads immediately, so its row never accumulates a badge — but a
  // backgrounded window genuinely accumulates unread and must show it.
  const windowActive = useWindowActive();

  return (
    <section className="outsource-panel" data-testid="outsource-panel">
      {/* 0 = 暫停指派 — annotated explicitly (Seth ruling). */}
      {maxParallel === 0 && (
        <div className="outsource-panel__paused" data-testid="outsource-paused">
          {t.office.outsource.paused}
        </div>
      )}

      {error && (
        <div className="office__error">{t.office.outsource.loadError}</div>
      )}

      <div className="outsource-panel__list" data-testid="outsource-list">
          {workers.map((w) => (
            // Click semantics mirror MemberCard: the row body opens the CHAT;
            // the avatar alone opens the worker DETAIL panel (stopPropagation so
            // it never also fires the row's chat jump). A row is a div-button so
            // the avatar button can nest inside it.
            <div
              key={w.id}
              role="button"
              tabIndex={0}
              className={`outsource-row${
                w.id === selectedId ? " outsource-row--selected" : ""
              }`}
              data-testid={`outsource-row-${w.id}`}
              onClick={() => onOpenChat(w.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onOpenChat(w.id);
                }
              }}
            >
              {/* No status dot on the avatar (MemberCard parity): the line-2
               * online dot below is the single presence signal of the row. */}
              <button
                type="button"
                className="outsource-row__avatar"
                aria-label={t.office.outsource.viewDetail}
                title={t.office.outsource.viewDetail}
                data-testid={`outsource-detail-${w.id}`}
                onClick={(e) => {
                  e.stopPropagation();
                  onOpenDetail(w.id);
                }}
                onKeyDown={(e) => e.stopPropagation()}
              >
                <BriefcaseIcon size={16} />
              </button>
              <span className="outsource-row__body">
                {/* line 1 — the worker's name (代號; an outsource has no
                 * other name), mirroring the member card's name line. */}
                <span className="outsource-row__line">
                  <span className="outsource-row__codename">
                    {t.office.outsource.label(w.codename)}
                  </span>
                </span>
                {/* line 2 — ONE line (owner 2026-07-16, second ruling):
                 * 上線綠點 at the LINE START (member-row parity), then the
                 * clickable T-xxxx chip, then 接到的 task type — the SHARED
                 * OutsourceTaskLine (the worker chat header renders the same
                 * line, minus the dot). */}
                <OutsourceTaskLine
                  worker={w}
                  onOpenTask={onOpenTask}
                  dot
                  idPrefix="outsource"
                />
                {/* line 3 (T-3451) — the bound task's real TITLE (not its type,
                 * which line 2 already shows): 2-line clamp + hover full. The
                 * wire already echoes it (OutsourceWorkerView.taskTitle); "" (a
                 * task that could not resolve) shows the muted empty state. */}
                <CurrentTaskTitle
                  title={w.taskTitle ?? ""}
                  clamp
                  testid={`outsource-task-title-${w.id}`}
                />
              </span>
              {/* Unread badge — the EXACT member-card signal (wire
               * unread_count, same watermark inverse), same flex-end slot,
               * same suppression: the open, watched chat never accumulates
               * (owner report 2026-07-14: 外包有未讀也要有紅點). */}
              {(w.unreadCount ?? 0) > 0 &&
                !(w.id === selectedId && windowActive) && (
                  <span
                    className="member-card__unread"
                    data-testid={`outsource-unread-${w.id}`}
                  >
                    {(w.unreadCount ?? 0) > 99 ? "99+" : w.unreadCount}
                  </span>
                )}
            </div>
          ))}
        </div>
    </section>
  );
}
